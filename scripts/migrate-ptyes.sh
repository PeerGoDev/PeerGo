#!/usr/bin/env bash

set -Eeuo pipefail

# Finite PtYes cutover orchestrator. It deliberately never creates, drops, or
# truncates a database. Operators restore into an explicitly prepared empty
# source database, then reuse one immutable snapshot/run identity for every
# resumable phase.
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/.." && pwd -P)"
inputs_inspected=false
preflight_passed=false

usage() {
    cat <<'EOF'
Usage: scripts/migrate-ptyes.sh <command>

Commands:
  inspect            Verify dump/ZIP integrity and print their SHA-256 values.
  preflight          Verify cutover databases/storage and write a new manifest.
  status             Read run-scoped Core checkpoints and target evidence.
  restore            Restore the gzip-wrapped pg_dump into an empty source DB.
  users              Validate/import users and immutable attendance openings,
                     then prove an all-skipped retry.
  medals             Import and fully reconcile medal definitions, ownership,
                     wear/expiry state, settings, reward benefit openings, and
                     typed workgroup memberships derived from reviewed medals.
  seedboxes           Import and verify user-bound box IP/CIDR rules, then
                     append the 0.5x upload / 2x download Tracker policy.
  torrents-exclusions
                     Write a missing-object candidate TSV for human review.
  torrents-validate  Inventory and validate every SQL-referenced .torrent.
  torrents-import    Import verified torrent objects and parsed file trees.
  torrent-purchases  Import integer prices and existing permanent purchase rights.
  media-validate     Validate every torrent gallery/poster image reference.
  media-import       Preserve and import validated torrent source images.
  media-reconcile    Fully read back every imported torrent image.
  image-derivatives  Generate and verify three WebP variants from every source.
  reconcile          Prove stable torrent/image retries and close the run.
  acceptance         Verify the reconciled target, stored bytes, and signed
                     Tracker snapshots; write a non-overwriting final gate.
  all                Run users, medals, seedboxes, torrents, purchase rights,
                     media, reconciliation, and verified WebP derivatives.
                     It intentionally stops before Tracker projection/acceptance.

inspect requires:
  PEERGO_LEGACY_DUMP_PATH
  PEERGO_LEGACY_TORRENT_ROOT       absolute torrent_dir or torrents.zip
  PEERGO_LEGACY_IMAGE_ROOT         absolute uploads.zip, or the same combined
                                    assets.zip used for the torrent root

status additionally requires:
  PEERGO_CORE_DATABASE_URL
  PEERGO_LEGACY_RUN_ID

preflight and every mutation-capable phase additionally require:
  PEERGO_LEGACY_PREFLIGHT_OUTPUT   absolute new JSON path; never overwritten

acceptance additionally requires:
  PEERGO_LEGACY_PREFLIGHT_MANIFEST absolute preflight JSON (defaults to the
                                    preflight output when it is still exported)
  PEERGO_LEGACY_ACCEPTANCE_OUTPUT  absolute new JSON path; never overwritten
  PEERGO_TRACKER_SNAPSHOT_PATH
  PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH
  PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH
  PEERGO_TRACKER_SNAPSHOT_TRUSTED_KEYS
  PEERGO_TRACKER_SNAPSHOT_MAX_AGE
  PEERGO_TRACKER_SUBJECT_SNAPSHOT_MAX_AGE
  PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_MAX_AGE
  PEERGO_TRACKER_SNAPSHOT_MAX_FUTURE_SKEW

Mutation-capable phases additionally require:
  PEERGO_LEGACY_SOURCE_DATABASE_URL
  PEERGO_VAULT_DATABASE_URL
  PEERGO_LEGACY_OCCURRED_AT

Optional fail-closed exception input:
  PEERGO_LEGACY_TORRENT_EXCLUSIONS  absolute snapshot-bound reviewed TSV
  PEERGO_LEGACY_TORRENT_EXCLUSIONS_OUTPUT
                                    new absolute candidate path; never overwritten

Image import uses the existing PeerGo object-storage configuration and retains
the exact validated source bytes. image-derivatives additionally requires
libvips; PEERGO_IMAGE_DERIVATIVE_TEMP_DIR defaults to a run-scoped directory
below /tmp and PEERGO_IMAGE_DERIVATIVE_VIPS_BINARY defaults to vips. User
avatars and unrelated upload images are intentionally not imported.

restore additionally requires PEERGO_LEGACY_RESTORE_DATABASE_URL. User keys,
Vault keys, Tracker lookup key, and torrent storage settings are consumed by
the existing typed migration commands. PEERGO_LEGACY_SNAPSHOT_SHA256 may be
omitted; this script computes and exports the compressed dump digest.
EOF
}

fail() {
    printf 'PtYes migration: %s\n' "$*" >&2
    exit 1
}

note() {
    printf 'PtYes migration: %s\n' "$*"
}

required_env() {
    local name="$1"
    [[ -n "${!name:-}" ]] || fail "${name} is required"
}

required_command() {
    command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

# Development runs the typed importers from source. The production cutover
# image supplies the exact same commands as immutable binaries so the server
# does not need a Go toolchain and the orchestration below remains the single
# migration order of record.
run_core_command() {
    local command_name="$1"
    shift
    if [[ -n "${PEERGO_LEGACY_BIN_DIR:-}" ]]; then
        [[ "${PEERGO_LEGACY_BIN_DIR}" = /* ]] || fail "PEERGO_LEGACY_BIN_DIR must be absolute"
        local binary="${PEERGO_LEGACY_BIN_DIR}/${command_name}"
        [[ -x "${binary}" ]] || fail "migration binary is unavailable: ${binary}"
        "${binary}" "$@"
        return
    fi
    required_command go
    go -C "${repo_root}/services/core" run "./cmd/${command_name}" "$@"
}

run_vault_command() {
    local command_name="$1"
    shift
    if [[ -n "${PEERGO_LEGACY_BIN_DIR:-}" ]]; then
        [[ "${PEERGO_LEGACY_BIN_DIR}" = /* ]] || fail "PEERGO_LEGACY_BIN_DIR must be absolute"
        local binary="${PEERGO_LEGACY_BIN_DIR}/${command_name}"
        [[ -x "${binary}" ]] || fail "migration binary is unavailable: ${binary}"
        "${binary}" "$@"
        return
    fi
    required_command go
    go -C "${repo_root}/services/privacy-vault" run "./cmd/${command_name}" "$@"
}

sha256_file() {
    local file="$1"
    if command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$file" | awk '{print $1}'
        return
    fi
    required_command sha256sum
    sha256sum "$file" | awk '{print $1}'
}

# Some historical PtYes archives produced by Go's ZIP writer are valid and
# pass the host-side integrity gate, but Alpine's patched Info-ZIP can report a
# false-positive "overlapped components" error for very large ZIP64 files.
# Retry only that exact failure, only after the read-only archive's digest has
# been matched to the digest supplied by the production cutover wrapper. The
# compatibility retry still inflates every entry and verifies its CRC; the Go
# importers additionally enforce entry counts, canonical paths, object sizes
# and exact uncompressed lengths before accepting any object.
verify_legacy_zip_integrity() {
    local archive="$1"
    local label="$2"
    local actual_sha="$3"
    local expected_sha="$4"
    local output

    if output="$(unzip -tq "${archive}" 2>&1)"; then
        return
    fi
    if [[ "${output}" == *'invalid zip file with overlapped components (possible zip bomb)'* &&
          -n "${expected_sha}" && "${actual_sha}" == "${expected_sha}" ]]; then
        note "${label}_zip_compatibility=infozip-overlap-false-positive"
        UNZIP_DISABLE_ZIPBOMB_DETECTION=TRUE unzip -tq "${archive}" >/dev/null ||
            fail "${label} ZIP failed CRC verification in compatibility mode"
        return
    fi
    printf '%s\n' "${output}" >&2
    fail "${label} ZIP failed integrity verification"
}

inspect_inputs() {
    if [[ "${inputs_inspected}" == true ]]; then
        return
    fi
    required_env PEERGO_LEGACY_DUMP_PATH
    required_env PEERGO_LEGACY_TORRENT_ROOT
    required_env PEERGO_LEGACY_IMAGE_ROOT
    local dump_path="${PEERGO_LEGACY_DUMP_PATH}"
    local torrent_source="${PEERGO_LEGACY_TORRENT_ROOT}"
    local image_source="${PEERGO_LEGACY_IMAGE_ROOT}"
    [[ "${dump_path}" = /* && -f "${dump_path}" ]] || fail "dump path must be an absolute regular file"
    [[ "${torrent_source}" = /* ]] || fail "torrent source must be absolute"
    local torrent_is_directory=false
    if [[ -d "${torrent_source}" ]]; then
        torrent_is_directory=true
    else
        [[ -f "${torrent_source}" ]] || fail "torrent source must be an absolute directory or ZIP file"
        case "${torrent_source}" in
            *.zip|*.ZIP) ;;
            *) fail "torrent source must be an absolute directory or ZIP file" ;;
        esac
    fi
    [[ "${image_source}" = /* && -f "${image_source}" ]] ||
        fail "image source must be an absolute ZIP file"
    case "${image_source}" in
        *.zip|*.ZIP) ;;
        *) fail "image source must be an absolute ZIP file" ;;
    esac
    required_command gzip
    required_command unzip
    gzip -t "$dump_path"
    local dump_sha
    dump_sha="$(sha256_file "$dump_path")"
    if [[ -n "${PEERGO_LEGACY_SNAPSHOT_SHA256:-}" && "${PEERGO_LEGACY_SNAPSHOT_SHA256}" != "${dump_sha}" ]]; then
        fail "PEERGO_LEGACY_SNAPSHOT_SHA256 does not match the dump"
    fi
    export PEERGO_LEGACY_SNAPSHOT_SHA256="${dump_sha}"
    note "database_dump_sha256=${dump_sha}"

    local image_sha
    image_sha="$(sha256_file "${image_source}")"
    if [[ -n "${PEERGO_LEGACY_IMAGE_ARCHIVE_SHA256:-}" && "${PEERGO_LEGACY_IMAGE_ARCHIVE_SHA256}" != "${image_sha}" ]]; then
        fail "PEERGO_LEGACY_IMAGE_ARCHIVE_SHA256 does not match the image archive"
    fi
    verify_legacy_zip_integrity \
        "${image_source}" image "${image_sha}" "${PEERGO_LEGACY_IMAGE_ARCHIVE_SHA256:-}"
    local image_objects
    image_objects="$(unzip -Z -1 "${image_source}" | awk '/^uploads\/images\/[0-9a-f][0-9a-f]\/[0-9a-f-]+\.(jpg|png|webp|gif)$/ { count++ } END { print count + 0 }')"
    [[ "${image_objects}" -gt 0 ]] || fail "image ZIP contains no uploads/images objects"
    export PEERGO_LEGACY_IMAGE_ARCHIVE_SHA256="${image_sha}"
    note "image_zip_sha256=${image_sha}"
    note "image_zip_objects=${image_objects}"

    if [[ "${torrent_is_directory}" == true ]]; then
        note "torrent_source=directory"
        inputs_inspected=true
        return
    fi
    local torrent_sha
    torrent_sha="$(sha256_file "${torrent_source}")"
    if [[ -n "${PEERGO_LEGACY_TORRENT_ARCHIVE_SHA256:-}" && "${PEERGO_LEGACY_TORRENT_ARCHIVE_SHA256}" != "${torrent_sha}" ]]; then
        fail "PEERGO_LEGACY_TORRENT_ARCHIVE_SHA256 does not match the torrent archive"
    fi
    if [[ "${torrent_source}" != "${image_source}" ]]; then
        verify_legacy_zip_integrity \
            "${torrent_source}" torrent "${torrent_sha}" "${PEERGO_LEGACY_TORRENT_ARCHIVE_SHA256:-}"
    fi
    local torrent_objects
    torrent_objects="$(unzip -Z -1 "${torrent_source}" | awk '/^(torrents\/)?[0-9a-f][0-9a-f]\/[0-9a-f-]+\.torrent$/ { count++ } END { print count + 0 }')"
    [[ "${torrent_objects}" -gt 0 ]] || fail "torrent ZIP contains no .torrent objects"
    note "torrent_source=zip"
    note "torrent_zip_objects=${torrent_objects}"
    note "torrent_zip_sha256=${torrent_sha}"
    inputs_inspected=true
}

require_run_identity() {
    required_env PEERGO_LEGACY_SOURCE_DATABASE_URL
    required_env PEERGO_CORE_DATABASE_URL
    required_env PEERGO_VAULT_DATABASE_URL
    required_env PEERGO_LEGACY_RUN_ID
    required_env PEERGO_LEGACY_OCCURRED_AT
    export PEERGO_LEGACY_MAPPING_VERSION="${PEERGO_LEGACY_MAPPING_VERSION:-ptyes-v1}"
    export PEERGO_LEGACY_RECONCILED_AT="${PEERGO_LEGACY_RECONCILED_AT:-${PEERGO_LEGACY_OCCURRED_AT}}"
}

require_status_identity() {
    required_env PEERGO_CORE_DATABASE_URL
    required_env PEERGO_LEGACY_RUN_ID
    export PEERGO_LEGACY_MAPPING_VERSION="${PEERGO_LEGACY_MAPPING_VERSION:-ptyes-v1}"
}

read_migration_status() {
    inspect_inputs
    require_status_identity
    note "reading run-scoped migration checkpoints and target evidence"
    run_core_command legacy-torrents --action status
}

run_cutover_preflight() {
    inspect_inputs
    require_run_identity
    required_env PEERGO_LEGACY_PREFLIGHT_OUTPUT
    [[ "${PEERGO_LEGACY_PREFLIGHT_OUTPUT}" = /* ]] || fail "PEERGO_LEGACY_PREFLIGHT_OUTPUT must be absolute"
    [[ ! -e "${PEERGO_LEGACY_PREFLIGHT_OUTPUT}" ]] || fail "preflight output already exists"
    [[ -f "${PEERGO_LEGACY_TORRENT_ROOT}" ]] || fail "formal cutover preflight requires an immutable torrents.zip"
    [[ -f "${PEERGO_LEGACY_IMAGE_ROOT}" ]] || fail "formal cutover preflight requires an immutable image ZIP"
    note "running read-only database and destination storage cutover checks"
    run_core_command legacy-torrents --action preflight
    preflight_passed=true
}

ensure_cutover_preflight() {
    if [[ "${preflight_passed}" != true ]]; then
        run_cutover_preflight
    fi
}

restore_source() {
    inspect_inputs
    required_env PEERGO_LEGACY_RESTORE_DATABASE_URL
    required_command psql
    required_command pg_restore
    local relation_count
    relation_count="$(psql "${PEERGO_LEGACY_RESTORE_DATABASE_URL}" -X -A -t -v ON_ERROR_STOP=1 -c \
        "SELECT count(*) FROM pg_catalog.pg_class AS object JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = object.relnamespace WHERE namespace.nspname NOT IN ('pg_catalog', 'information_schema') AND object.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')")"
    [[ "${relation_count}" == "0" ]] || fail "restore source database is not empty"
    note "restoring immutable PtYes snapshot into the empty source database"
    gzip -dc "${PEERGO_LEGACY_DUMP_PATH}" |
        pg_restore --exit-on-error --no-owner --no-privileges --dbname="${PEERGO_LEGACY_RESTORE_DATABASE_URL}"
    local required_tables
    required_tables="$(psql "${PEERGO_LEGACY_RESTORE_DATABASE_URL}" -X -A -t -v ON_ERROR_STOP=1 -c \
        "SELECT count(*) FROM (VALUES (to_regclass('public.users')), (to_regclass('public.user_attendance_stats')), (to_regclass('public.attendance_records')), (to_regclass('public.torrents')), (to_regclass('public.torrent_files')), (to_regclass('public.seed_boxes')), (to_regclass('public.site_settings'))) AS required(table_name) WHERE table_name IS NOT NULL")"
    [[ "${required_tables}" == "7" ]] || fail "restored source is missing required PtYes user, attendance, torrent, seedbox, or site-settings tables"
    note "source restore completed"
}

run_users() {
    ensure_cutover_preflight
    note "validating PtYes users"
    PEERGO_LEGACY_MODE=validate run_vault_command legacy-users
    note "importing PtYes users"
    PEERGO_LEGACY_MODE=import run_vault_command legacy-users
    note "verifying an idempotent all-skipped user retry"
    PEERGO_LEGACY_MODE=import run_vault_command legacy-users
    note "importing PtYes traffic, integer magic, experience, activity and attendance openings"
    run_core_command legacy-user-state
    note "verifying an idempotent user operational-state retry"
    run_core_command legacy-user-state
}

run_medals() {
    ensure_cutover_preflight
    note "importing PtYes medal definitions, user ownership and benefit openings"
    run_core_command legacy-medals --action import
    note "verifying an idempotent medal retry and full source-to-target reconciliation"
    run_core_command legacy-medals --action import
}

run_seedboxes() {
    ensure_cutover_preflight
    note "importing user-bound PtYes seedbox addresses and strict accounting factors"
    run_core_command legacy-seedboxes --action import
    note "verifying an idempotent seedbox retry and exact source-to-policy bindings"
    run_core_command legacy-seedboxes --action import
}

verify_seedboxes() {
    require_run_identity
    note "verifying migrated seedbox bindings and the latest Core runtime policy"
    run_core_command legacy-seedboxes --action verify
}

verify_medals() {
    require_run_identity
    note "verifying medal definitions, ownership and benefit openings without target writes"
    run_core_command legacy-medals --action verify
}

run_torrent_validation() {
    ensure_cutover_preflight
    note "inventorying PtYes torrent metadata"
    run_core_command legacy-torrents --action inventory
    note "validating SQL-referenced immutable torrent objects"
    run_core_command legacy-torrents --action validate
}

write_torrent_exclusion_candidate() {
    inspect_inputs
    require_run_identity
    required_env PEERGO_LEGACY_TORRENT_EXCLUSIONS_OUTPUT
    note "writing a snapshot-bound missing-object candidate for human review"
    run_core_command legacy-torrents --action exclusions-template
}

run_torrent_import() {
    ensure_cutover_preflight
    note "importing verified torrent objects and parsed file trees"
    run_core_command legacy-torrents --action import
}

run_torrent_purchases() {
    ensure_cutover_preflight
    note "importing PtYes integer torrent prices and completed purchase rights"
    run_core_command legacy-torrents --action purchases
    note "verifying an idempotent torrent purchase retry"
    run_core_command legacy-torrents --action purchases
}

run_media_validation() {
    ensure_cutover_preflight
    note "validating PtYes torrent gallery and poster image references"
    run_core_command legacy-media --action validate
}

run_media_import() {
    ensure_cutover_preflight
    note "normalizing and importing PtYes torrent images"
    run_core_command legacy-media --action import
}

run_media_reconciliation() {
    ensure_cutover_preflight
    note "fully reading back and reconciling imported torrent images"
    run_core_command legacy-media --action reconcile
}

run_image_derivatives() {
    require_run_identity
    required_command vips
    local concurrency="${PEERGO_IMAGE_DERIVATIVE_CONCURRENCY:-4}"
    [[ "${concurrency}" =~ ^[0-9]+$ ]] || fail "PEERGO_IMAGE_DERIVATIVE_CONCURRENCY must be an integer"
    (( concurrency >= 1 && concurrency <= 16 )) || fail "PEERGO_IMAGE_DERIVATIVE_CONCURRENCY must be between 1 and 16"
    export PEERGO_IMAGE_DERIVATIVE_TEMP_DIR="${PEERGO_IMAGE_DERIVATIVE_TEMP_DIR:-/tmp/peergo-image-derivatives-${PEERGO_LEGACY_RUN_ID}}"
    export PEERGO_IMAGE_DERIVATIVE_VIPS_BINARY="${PEERGO_IMAGE_DERIVATIVE_VIPS_BINARY:-$(command -v vips)}"
    [[ "${PEERGO_IMAGE_DERIVATIVE_TEMP_DIR}" = /* ]] || fail "PEERGO_IMAGE_DERIVATIVE_TEMP_DIR must be absolute"
    mkdir -p "${PEERGO_IMAGE_DERIVATIVE_TEMP_DIR}"
    chmod 700 "${PEERGO_IMAGE_DERIVATIVE_TEMP_DIR}"
    note "generating and fully verifying WebP image derivatives with ${concurrency} cooperating processors"
    run_core_command image-derivative-worker \
        --drain --drain-timeout=24h --concurrency="${concurrency}"
}

run_reconciliation() {
    ensure_cutover_preflight
    run_media_reconciliation
    note "running verify-only torrent retry and terminal Core/Vault reconciliation"
    run_core_command legacy-torrents --action reconcile
}

run_cutover_acceptance() {
    inspect_inputs
    require_run_identity
    if [[ -z "${PEERGO_LEGACY_PREFLIGHT_MANIFEST:-}" ]]; then
        required_env PEERGO_LEGACY_PREFLIGHT_OUTPUT
        export PEERGO_LEGACY_PREFLIGHT_MANIFEST="${PEERGO_LEGACY_PREFLIGHT_OUTPUT}"
    fi
    required_env PEERGO_LEGACY_ACCEPTANCE_OUTPUT
    required_env PEERGO_TRACKER_SNAPSHOT_PATH
    required_env PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH
    required_env PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH
    required_env PEERGO_TRACKER_SNAPSHOT_TRUSTED_KEYS
    required_env PEERGO_TRACKER_SNAPSHOT_MAX_AGE
    required_env PEERGO_TRACKER_SUBJECT_SNAPSHOT_MAX_AGE
    required_env PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_MAX_AGE
    required_env PEERGO_TRACKER_SNAPSHOT_MAX_FUTURE_SKEW
    [[ "${PEERGO_LEGACY_PREFLIGHT_MANIFEST}" = /* && -f "${PEERGO_LEGACY_PREFLIGHT_MANIFEST}" ]] ||
        fail "preflight manifest must be an absolute regular file"
    [[ "${PEERGO_LEGACY_ACCEPTANCE_OUTPUT}" = /* ]] ||
        fail "PEERGO_LEGACY_ACCEPTANCE_OUTPUT must be absolute"
    [[ ! -e "${PEERGO_LEGACY_ACCEPTANCE_OUTPUT}" ]] || fail "acceptance output already exists"
    [[ -f "${PEERGO_LEGACY_TORRENT_ROOT}" ]] ||
        fail "formal cutover acceptance requires the immutable torrents.zip"
    [[ -f "${PEERGO_LEGACY_IMAGE_ROOT}" ]] ||
        fail "formal cutover acceptance requires the immutable image ZIP"
    verify_medals
    verify_seedboxes
    note "running read-only post-reconciliation cutover acceptance"
    run_core_command legacy-torrents --action acceptance
}

command_name="${1:-}"
case "${command_name}" in
    inspect)
        inspect_inputs
        ;;
    status)
        read_migration_status
        ;;
    preflight)
        run_cutover_preflight
        ;;
    restore)
        restore_source
        ;;
    users)
        run_users
        ;;
    medals)
        run_medals
        ;;
    seedboxes)
        run_seedboxes
        ;;
    torrents-exclusions)
        write_torrent_exclusion_candidate
        ;;
    torrents-validate)
        run_torrent_validation
        ;;
    torrents-import)
        run_torrent_import
        ;;
    torrent-purchases)
        run_torrent_purchases
        ;;
    media-validate)
        run_media_validation
        ;;
    media-import)
        run_media_import
        ;;
    media-reconcile)
        run_media_reconciliation
        ;;
    image-derivatives)
        run_image_derivatives
        ;;
    reconcile)
        run_reconciliation
        ;;
    acceptance)
        run_cutover_acceptance
        ;;
    all)
        run_users
        run_medals
        run_seedboxes
        run_torrent_validation
        run_torrent_import
        run_torrent_purchases
        run_media_validation
        run_media_import
        run_reconciliation
        run_image_derivatives
        ;;
    -h|--help|help)
        usage
        ;;
    *)
        usage >&2
        exit 2
        ;;
esac
