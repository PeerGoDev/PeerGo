#!/usr/bin/env bash

set -Eeuo pipefail

# Destructive development-only rehearsal for the finite PtYes/Rousi cutover.
# The typed importers remain the only code allowed to interpret or write legacy
# rows. This wrapper only prepares isolated local databases, freezes one run
# identity, preserves recoverable backups, and executes the existing gates in
# their required order.

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/.." && pwd -P)"
compose_file="${repo_root}/deploy/compose/compose.yaml"
local_root="${repo_root}/.local"
restore_root="${local_root}/legacy-restores"
object_root="${local_root}/objects"
tracker_root="${local_root}/tracker"

core_database_url='postgres://peergo_core:peergo_local_only@127.0.0.1:5432/peergo_core?sslmode=disable'
source_database_url='postgres://peergo_core:peergo_local_only@127.0.0.1:5432/peergo_legacy_source?sslmode=disable'
vault_database_url='postgres://peergo_vault:peergo_vault_local_only@127.0.0.1:5433/peergo_vault?sslmode=disable'
tracker_database_url='postgres://peergo_tracker:peergo_tracker_local_only@127.0.0.1:5434/peergo_tracker?sslmode=disable'

usage() {
    cat <<'EOF'
Usage:
  PEERGO_LOCAL_RESTORE_CONFIRM=RESET_PEERGO_LOCAL \
    scripts/restore-rousi-local.sh <rousi.sql.gz> <torrents.zip> <uploads.zip>

Resume the same immutable run after an incidental failure:
  PEERGO_LOCAL_RESTORE_CONFIRM=RESET_PEERGO_LOCAL \
  PEERGO_LOCAL_RESTORE_RESUME_DIR=/absolute/.local/legacy-restores/<run> \
    scripts/restore-rousi-local.sh <rousi.sql.gz> <torrents.zip> <uploads.zip>

This command is intentionally limited to PeerGo's loopback development
Compose project. It:
  1. verifies the three absolute input files and required tools;
  2. saves private pg_dump backups and moves current local object/snapshot
     directories into the same recoverable backup directory;
  3. removes only PeerGo Compose volumes, recreates and migrates the databases,
     then appends the deterministic normal settlement baseline;
  4. restores the Rousi dump into an isolated read-only source database;
  5. creates a snapshot-bound missing-.torrent candidate and, for this local
     rehearsal only, uses it after the explicit confirmation above;
  6. imports users, complete medal definitions/ownership/benefits, .torrent
     objects/file trees, integer prices, completed purchase rights, posters
     and galleries;
  7. reconciles, drains migrated workgroup benefits and Tracker control
     projection, signs snapshots and runs the full read-back acceptance gate.

The old site archives are never modified or extracted in place. User avatars,
community images and unreferenced upload objects remain outside migration.
EOF
}

fail() {
    printf 'Rousi local restore: %s\n' "$*" >&2
    exit 1
}

note() {
    printf 'Rousi local restore: %s\n' "$*"
}

required_command() {
    command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

absolute_regular_file() {
    local value="$1"
    [[ -n "${value}" ]] || fail "all three archive paths are required"
    local parent
    parent="$(cd "$(dirname "${value}")" 2>/dev/null && pwd -P)" ||
        fail "archive parent does not exist: ${value}"
    local resolved="${parent}/$(basename "${value}")"
    [[ -f "${resolved}" && ! -L "${resolved}" ]] ||
        fail "archive must be a regular non-symlink file: ${resolved}"
    printf '%s\n' "${resolved}"
}

sha256_file() {
    local value="$1"
    if command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "${value}" | awk '{print $1}'
        return
    fi
    required_command sha256sum
    sha256sum "${value}" | awk '{print $1}'
}

run_id_from_snapshot() {
    local digest="$1"
    # Set RFC 4122 version/variant nibbles while preserving a deterministic
    # identity for the immutable compressed dump.
    printf '%s-%s-5%s-8%s-%s\n' \
        "${digest:0:8}" "${digest:8:4}" "${digest:13:3}" \
        "${digest:17:3}" "${digest:20:12}"
}

occurred_at_from_dump_name() {
    local name
    name="$(basename "$1")"
    if [[ "${name}" =~ ([0-9]{14}) ]]; then
        local stamp="${BASH_REMATCH[1]}"
        printf '%s-%s-%sT%s:%s:%s+08:00\n' \
            "${stamp:0:4}" "${stamp:4:2}" "${stamp:6:2}" \
            "${stamp:8:2}" "${stamp:10:2}" "${stamp:12:2}"
        return
    fi
    fail "dump filename must contain its fixed Asia/Shanghai YYYYMMDDhhmmss snapshot time"
}

compose() {
    docker compose -f "${compose_file}" "$@"
}

backup_database() {
    local service="$1"
    local user="$2"
    local database="$3"
    local output="$4"
    note "backing up ${database}"
    compose exec -T "${service}" pg_dump -U "${user}" -d "${database}" -Fc >"${output}"
    [[ -s "${output}" ]] || fail "database backup is empty: ${database}"
    chmod 600 "${output}"
}

move_if_present() {
    local source="$1"
    local destination="$2"
    if [[ -e "${source}" ]]; then
        mv "${source}" "${destination}"
    fi
}

write_input_manifest() {
    local output="$1"
    {
        printf 'schema=peergo.rousi-local-inputs.v1\n'
        printf 'run_id=%s\n' "${run_id}"
        printf 'occurred_at=%s\n' "${occurred_at}"
        printf 'dump_sha256=%s\n' "${dump_sha256}"
        printf 'dump_bytes=%s\n' "$(stat -f '%z' "${dump_path}" 2>/dev/null || stat -c '%s' "${dump_path}")"
        printf 'torrent_archive=%s\n' "$(basename "${torrent_path}")"
        printf 'torrent_sha256=%s\n' "${torrent_sha256}"
        printf 'torrent_bytes=%s\n' "$(stat -f '%z' "${torrent_path}" 2>/dev/null || stat -c '%s' "${torrent_path}")"
        printf 'image_archive=%s\n' "$(basename "${image_path}")"
        printf 'image_sha256=%s\n' "${image_sha256}"
        printf 'image_bytes=%s\n' "$(stat -f '%z' "${image_path}" 2>/dev/null || stat -c '%s' "${image_path}")"
    } >"${output}"
    chmod 600 "${output}"
}

restore_source_database() {
    wait_for_database "${core_database_url}" 'PeerGo Core database'
    note "creating isolated PtYes source database"
    psql "${core_database_url}" -X -v ON_ERROR_STOP=1 -c \
        'CREATE DATABASE peergo_legacy_source TEMPLATE template0 ENCODING '\''UTF8'\''' >/dev/null
    note "restoring immutable Rousi pg_dump"
    gzip -dc "${dump_path}" |
        pg_restore --exit-on-error --no-owner --no-privileges --dbname="${source_database_url}"
    local required_tables
    required_tables="$(psql "${source_database_url}" -X -A -t -v ON_ERROR_STOP=1 -c \
        "SELECT count(*) FROM (VALUES (to_regclass('public.users')), (to_regclass('public.medals')), (to_regclass('public.user_medals')), (to_regclass('public.torrents')), (to_regclass('public.torrent_files')), (to_regclass('public.torrent_images'))) AS required(table_name) WHERE table_name IS NOT NULL")"
    [[ "${required_tables}" == "6" ]] || fail "restored source is missing required Rousi tables"
    psql "${source_database_url}" -X -A -t -v ON_ERROR_STOP=1 -c \
        "SELECT format('source users=%s medals=%s user_medals=%s torrents=%s torrent_files=%s torrent_images=%s', (SELECT count(*) FROM users), (SELECT count(*) FROM medals), (SELECT count(*) FROM user_medals), (SELECT count(*) FROM torrents), (SELECT count(*) FROM torrent_files), (SELECT count(*) FROM torrent_images))"
}

wait_for_database() {
    local database_url="$1"
    local label="$2"
    local attempt
    for attempt in {1..100}; do
        if psql "${database_url}" -X -A -t -v ON_ERROR_STOP=1 -c 'SELECT 1' >/dev/null 2>&1; then
            return
        fi
        sleep 0.1
    done
    fail "${label} did not accept connections after Compose reported healthy"
}

verify_core_runtime_defaults() {
    local observed
    observed="$(psql "${core_database_url}" -X -A -t -v ON_ERROR_STOP=1 -c \
        "WITH contribution AS (SELECT revision, effective_from, snapshot_sha256 FROM progression.contribution_experience_policy_revisions WHERE effective_from <= clock_timestamp() AND created_at <= clock_timestamp() ORDER BY effective_from DESC, revision DESC LIMIT 1) SELECT (SELECT count(*) FROM catalog.site_profile WHERE singleton)::text || ':' || (SELECT count(*) FROM identity.registration_policy WHERE singleton)::text || ':' || (SELECT count(*) FROM economy.seeding_reward_policy_revisions WHERE effective_from <= clock_timestamp())::text || ':' || (SELECT count(*) FROM economy.attendance_policy_revisions WHERE effective_from <= clock_timestamp())::text || ':' || (SELECT count(*) FROM contribution)::text || ':' || (SELECT count(*) FROM contribution JOIN progression.experience_policy_revisions AS authority ON authority.revision IN (contribution.revision || '.publish', contribution.revision || '.activity') AND authority.effective_from = contribution.effective_from AND authority.payload_sha256 = contribution.snapshot_sha256)::text")"
    IFS=':' read -r site_count registration_count seeding_count attendance_count contribution_count contribution_authority_count <<<"${observed}"
    [[ "${site_count}" == '1' && "${registration_count}" == '1' &&
       "${seeding_count}" -ge 1 && "${attendance_count}" -ge 1 &&
       "${contribution_count}" -ge 1 && "${contribution_authority_count}" == '2' ]] ||
        fail "Core runtime defaults are incomplete: site:registration:seeding:attendance:contribution:contribution-authorities=${observed}"
    note "verified Core runtime defaults: site and registration ready; seeding, attendance and contribution experience policies and authorities effective"
}

verify_legacy_member_authorizations() {
    local observed
    observed="$(psql "${core_database_url}" -X -A -t -v ON_ERROR_STOP=1 -c "
WITH migrated AS (
    SELECT mapping.user_id
    FROM migration.user_id_map AS mapping
    WHERE mapping.source_system = 'ptyes'
), authorized AS (
    SELECT DISTINCT mapping.user_id
    FROM migrated AS mapping
    JOIN governance.mandates AS mandate
      ON mandate.subject_id = mapping.user_id
     AND mandate.source_type = 'legacy_import'
     AND mandate.source_reference = 'ptyes-user-migration-v1'
     AND mandate.scope_type = 'site'
     AND mandate.scope_id = 'peergo'
     AND mandate.status = 'active'
     AND mandate.starts_at <= now()
     AND now() < mandate.ends_at
    JOIN authz.grants AS member_grant
      ON member_grant.subject_id = mapping.user_id
     AND member_grant.mandate_id = mandate.id
     AND member_grant.role_id = 'member'
     AND member_grant.scope_type = 'site'
     AND member_grant.scope_id = 'peergo'
     AND member_grant.valid_from >= mandate.starts_at
     AND member_grant.valid_until <= mandate.ends_at
     AND member_grant.valid_from <= now()
     AND now() < member_grant.valid_until
     AND member_grant.revoked_at IS NULL
)
SELECT (SELECT count(*) FROM migrated)::text || ':' || (SELECT count(*) FROM authorized)::text")"
    [[ "${observed%%:*}" == "${observed##*:}" ]] ||
        fail "legacy member authorizations are incomplete: migrated:authorized=${observed}"
    note "verified legacy member authorizations: migrated:authorized=${observed}"
}

prepare_local_exclusions() {
    local candidate="${run_dir}/torrent-exclusions.candidate.tsv"
    local approved="${run_dir}/torrent-exclusions.approved-local.tsv"
    local candidate_log="${run_dir}/torrent-exclusions.log"
    if [[ -f "${approved}" && ! -L "${approved}" ]]; then
        export PEERGO_LEGACY_TORRENT_EXCLUSIONS="${approved}"
        note "reusing the existing snapshot-bound local exclusion decision"
        return
    fi
    if [[ -f "${candidate}" && ! -L "${candidate}" ]]; then
        cp "${candidate}" "${approved}"
        chmod 600 "${approved}"
        export PEERGO_LEGACY_TORRENT_EXCLUSIONS="${approved}"
        note "promoted the existing snapshot-bound candidate for this local rehearsal only"
        return
    fi
    export PEERGO_LEGACY_TORRENT_EXCLUSIONS_OUTPUT="${candidate}"
    note "checking for physically missing .torrent objects"
    if GOWORK=off go -C "${repo_root}/services/core" run ./cmd/legacy-torrents \
        --action exclusions-template 2>&1 | tee "${candidate_log}"; then
        [[ "${PEERGO_LOCAL_RESTORE_CONFIRM:-}" == 'RESET_PEERGO_LOCAL' ]] ||
            fail "missing .torrent objects require explicit local rehearsal confirmation"
        cp "${candidate}" "${approved}"
        chmod 600 "${approved}"
        export PEERGO_LEGACY_TORRENT_EXCLUSIONS="${approved}"
        note "using snapshot-bound exclusions for this local rehearsal only"
        return
    fi
    if grep -q 'no physically missing torrent objects require an exclusion candidate' "${candidate_log}"; then
        unset PEERGO_LEGACY_TORRENT_EXCLUSIONS
        note "all SQL-referenced .torrent objects are present"
        return
    fi
    fail "could not establish the missing-.torrent decision set"
}

drain_tracker_projection() {
    local drain_log="${run_dir}/tracker-projector-drain.log"
    note "draining Tracker control outbox"
    PEERGO_ENV=development PEERGO_CORE_DATABASE_URL="${core_database_url}" \
        GOWORK=off go -C "${repo_root}/services/core" run ./cmd/projector \
        --drain --drain-timeout=30m | tee "${drain_log}"
}

# The normal deployment keeps the unified Core policy worker and Settlement
# control API running. A clean local restore intentionally starts only the
# database containers, so provide the same typed HTTP delivery boundary for a
# bounded drain instead of depending on an unrelated developer process.
drain_workgroup_benefits() (
    set -Eeuo pipefail
    local pending
    pending="$(psql "${core_database_url}" -X -A -t -v ON_ERROR_STOP=1 -c "
SELECT count(*)
FROM migration.workgroup_membership_openings AS opening
JOIN workgroups.settlement_benefit_outbox AS outbox
  ON outbox.transition_id = opening.transition_id
WHERE opening.first_run_id = '${run_id}'
  AND opening.group_kind = 'retention'
  AND outbox.delivered_at IS NULL")"
    if [[ "${pending}" == '0' ]]; then
        note "verified migrated workgroup benefits are already delivered"
        return
    fi

    local control_address='127.0.0.1:18085'
    local control_origin="http://${control_address}"
    local control_log="${run_dir}/settlement-workgroup-control.log"
    local worker_log="${run_dir}/workgroup-benefit-drain.log"
    PEERGO_ENV=development \
    PEERGO_TRACKER_DATABASE_URL="${tracker_database_url}" \
    PEERGO_SETTLEMENT_CONTROL_ADDR="${control_address}" \
    PEERGO_SETTLEMENT_CONTROL_SERVICE_TOKEN='peergo-local-settlement-control-token-v1-2026' \
    GOWORK=off go -C "${repo_root}/services/settlement" run ./cmd/promotion-control-api \
        >"${control_log}" 2>&1 &
    local control_pid=$!
    cleanup_workgroup_control() {
        kill "${control_pid}" 2>/dev/null || true
        wait "${control_pid}" 2>/dev/null || true
    }
    trap cleanup_workgroup_control EXIT INT TERM

    local ready=false
    local attempt
    for attempt in {1..100}; do
        if curl --fail --silent --show-error "${control_origin}/healthz" >/dev/null 2>&1; then
            ready=true
            break
        fi
        kill -0 "${control_pid}" 2>/dev/null || {
            cat "${control_log}" >&2
            fail "temporary Settlement workgroup control API stopped before becoming ready"
        }
        sleep 0.1
    done
    [[ "${ready}" == true ]] || fail "temporary Settlement workgroup control API did not become ready"

    note "draining ${pending} migrated workgroup benefit commands through Settlement"
    PEERGO_ENV=development \
    PEERGO_CORE_DATABASE_URL="${core_database_url}" \
    PEERGO_SETTLEMENT_CONTROL_URL="${control_origin}" \
    PEERGO_SETTLEMENT_CONTROL_SERVICE_TOKEN='peergo-local-settlement-control-token-v1-2026' \
    GOWORK=off go -C "${repo_root}/services/core" run ./cmd/promotion-worker \
        --drain-workgroup-benefits | tee "${worker_log}"

    pending="$(psql "${core_database_url}" -X -A -t -v ON_ERROR_STOP=1 -c "
SELECT count(*)
FROM migration.workgroup_membership_openings AS opening
JOIN workgroups.settlement_benefit_outbox AS outbox
  ON outbox.transition_id = opening.transition_id
WHERE opening.first_run_id = '${run_id}'
  AND opening.group_kind = 'retention'
  AND outbox.delivered_at IS NULL")"
    [[ "${pending}" == '0' ]] || fail "migrated workgroup benefits remain pending: ${pending}"
    note "verified migrated workgroup benefits are delivered"
)

write_restore_summary() {
    local output="${run_dir}/restore-summary.txt"
    {
		printf 'schema=peergo.rousi-local-restore-summary.v5\n'
        printf 'run_id=%s\n' "${run_id}"
        psql "${core_database_url}" -X -A -t -v ON_ERROR_STOP=1 -c "
SELECT format(
	'core users=%s numbered_users=%s user_openings=%s status_openings=%s attendance_openings=%s attendance_total_days=%s attendance_retroactive_cards=%s traffic_users=%s magic_member_accounts=%s magic_member_total=%s progression_users=%s activity_users=%s access_states=%s banned=%s vip_active=%s download_restricted=%s unverified=%s medal_definitions=%s user_medals=%s wearing_medals=%s medal_benefit_users=%s positive_medal_benefit_users=%s workgroup_memberships=%s reseed_memberships=%s review_memberships=%s retention_memberships=%s pending_workgroup_benefits=%s torrents=%s published=%s torrent_files=%s legacy_image_references=%s torrent_screenshot_attachments=%s image_objects=%s image_derivatives_ready=%s image_derivatives_dead=%s image_derivative_objects=%s image_derivative_bytes=%s tracker_pending=%s tracker_enabled=%s',
    (SELECT count(*) FROM identity.users),
    (SELECT count(DISTINCT numeric_id) FROM identity.users WHERE numeric_id > 0),
    (SELECT count(*) FROM migration.user_operational_openings),
    (SELECT count(*) FROM migration.user_status_openings),
	(SELECT count(*) FROM migration.user_attendance_openings),
	(SELECT COALESCE(sum(source_total_days), 0) FROM migration.user_attendance_openings),
	(SELECT COALESCE(sum(source_retroactive_cards), 0) FROM migration.user_attendance_openings),
    (SELECT count(*) FROM traffic.user_totals),
    (SELECT count(*) FROM economy.magic_accounts WHERE account_kind = 'member' AND user_id IS NOT NULL),
    (SELECT COALESCE(sum(balance), 0) FROM economy.magic_accounts WHERE account_kind = 'member' AND user_id IS NOT NULL),
    (SELECT count(*) FROM progression.user_progress),
    (SELECT count(*) FROM identity.user_activity),
    (SELECT count(*) FROM identity.user_access_states),
    (SELECT count(*) FROM identity.users WHERE status = 'disabled'),
    (SELECT count(*) FROM identity.user_access_states WHERE vip_enabled AND (vip_until IS NULL OR vip_until > CURRENT_TIMESTAMP)),
    (SELECT count(*) FROM identity.user_access_states WHERE download_restricted),
    (SELECT count(*) FROM identity.users WHERE email_verified_at IS NULL),
    (SELECT count(*) FROM economy.medal_definitions),
    (SELECT count(*) FROM economy.user_medals),
    (SELECT count(*) FROM economy.user_medals WHERE state = 'wearing'),
	(SELECT count(*) FROM migration.medal_benefit_openings WHERE source_system = 'ptyes'),
	(SELECT count(*) FROM migration.medal_benefit_openings WHERE source_system = 'ptyes' AND magic_bonus_bps > 0),
	(SELECT count(*) FROM migration.workgroup_membership_openings WHERE source_system = 'ptyes'),
	(SELECT count(*) FROM migration.workgroup_membership_openings WHERE source_system = 'ptyes' AND group_kind = 'reseed'),
	(SELECT count(*) FROM migration.workgroup_membership_openings WHERE source_system = 'ptyes' AND group_kind = 'review'),
	(SELECT count(*) FROM migration.workgroup_membership_openings WHERE source_system = 'ptyes' AND group_kind = 'retention'),
	(SELECT count(*) FROM migration.workgroup_membership_openings AS opening JOIN workgroups.settlement_benefit_outbox AS outbox ON outbox.transition_id = opening.transition_id WHERE opening.source_system = 'ptyes' AND opening.group_kind = 'retention' AND outbox.delivered_at IS NULL),
	(SELECT count(*) FROM torrents.torrents),
    (SELECT count(*) FROM torrents.torrents WHERE state = 'published'),
    (SELECT count(*) FROM torrents.torrent_files),
    (SELECT count(*) FROM migration.torrent_image_map WHERE first_run_id = '${run_id}'),
    (SELECT count(*) FROM torrents.torrent_screenshots),
    (SELECT count(*) FROM torrents.torrent_screenshot_objects),
    (SELECT count(*) FROM media.image_derivatives WHERE state = 'ready'),
    (SELECT count(*) FROM media.image_derivatives WHERE state = 'dead'),
    (SELECT count(*) FROM media.image_derivative_objects),
    (SELECT COALESCE(sum(byte_length), 0) FROM media.image_derivative_objects),
    (SELECT count(*) FROM tracker_control.outbox WHERE projected_at IS NULL),
    (SELECT count(*) FROM tracker_control.torrent_allowlist_projection WHERE enabled)
)"
        psql "${vault_database_url}" -X -A -t -v ON_ERROR_STOP=1 -c "
SELECT format(
    'vault credentials=%s emails=%s direct_identifiers=%s tracker_passkeys=%s',
    (SELECT count(*) FROM vault.credentials),
    (SELECT count(*) FROM vault.email_addresses),
    (SELECT count(*) FROM vault.direct_identifiers),
    (SELECT count(*) FROM vault.tracker_passkeys)
)"
        printf 'preflight=%s\n' "${preflight_output}"
        printf 'acceptance=%s\n' "${acceptance_output}"
        printf 'backup=%s\n' "${backup_dir}"
    } >"${output}"
    chmod 600 "${output}"
    cat "${output}"
}

[[ "$#" == "3" ]] || {
    usage >&2
    exit 2
}
[[ "${PEERGO_LOCAL_RESTORE_CONFIRM:-}" == 'RESET_PEERGO_LOCAL' ]] ||
    fail "set PEERGO_LOCAL_RESTORE_CONFIRM=RESET_PEERGO_LOCAL to acknowledge replacement of local PeerGo data"

required_command docker
required_command gzip
required_command pg_restore
required_command psql
required_command go
required_command curl
required_command unzip
required_command vips
vips --version | grep -qi vips || fail "vips is unavailable or incompatible"

dump_path="$(absolute_regular_file "$1")"
torrent_path="$(absolute_regular_file "$2")"
image_path="$(absolute_regular_file "$3")"
[[ "${dump_path}" == *.gz ]] || fail "database snapshot must be gzip wrapped"
[[ "${torrent_path}" == *.zip ]] || fail "torrent snapshot must be a ZIP"
[[ "${image_path}" == *.zip ]] || fail "image snapshot must be a ZIP"
[[ "${torrent_path}" != "${image_path}" ]] || fail "three-package mode requires distinct torrent and image ZIP files"

note "hashing the three immutable source archives"
dump_sha256="$(sha256_file "${dump_path}")"
torrent_sha256="$(sha256_file "${torrent_path}")"
image_sha256="$(sha256_file "${image_path}")"
run_id="$(run_id_from_snapshot "${dump_sha256}")"
occurred_at="$(occurred_at_from_dump_name "${dump_path}")"
started_at="$(date -u '+%Y%m%dT%H%M%SZ')"
resume=false
if [[ -n "${PEERGO_LOCAL_RESTORE_RESUME_DIR:-}" ]]; then
    resume_parent="$(cd "$(dirname "${PEERGO_LOCAL_RESTORE_RESUME_DIR}")" 2>/dev/null && pwd -P)" ||
        fail "resume directory parent does not exist"
    run_dir="${resume_parent}/$(basename "${PEERGO_LOCAL_RESTORE_RESUME_DIR}")"
    [[ -d "${run_dir}" && ! -L "${run_dir}" && "${run_dir}" == "${restore_root}/"* ]] ||
        fail "resume directory must be an existing run below ${restore_root}"
    grep -q "^run_id=${run_id}$" "${run_dir}/inputs.txt" ||
        fail "resume directory belongs to a different database snapshot"
    grep -q "^torrent_sha256=${torrent_sha256}$" "${run_dir}/inputs.txt" ||
        fail "resume directory belongs to a different torrent archive"
    grep -q "^image_sha256=${image_sha256}$" "${run_dir}/inputs.txt" ||
        fail "resume directory belongs to a different image archive"
    resume=true
    preflight_output="${run_dir}/preflight-${started_at}.json"
    acceptance_output="${run_dir}/acceptance-${started_at}.json"
    note "resuming immutable run ${run_id} without resetting databases"
else
    run_dir="${restore_root}/${run_id}-${started_at}"
    preflight_output="${run_dir}/preflight.json"
    acceptance_output="${run_dir}/acceptance.json"
fi
backup_dir="${run_dir}/backup-before-reset"
image_temp_dir="${run_dir}/image-temp"

umask 077
if [[ "${resume}" == false ]]; then
    mkdir -p "${backup_dir}" "${image_temp_dir}"
    chmod 700 "${run_dir}" "${backup_dir}" "${image_temp_dir}"
    write_input_manifest "${run_dir}/inputs.txt"

    note "starting local PostgreSQL services for recoverable backups"
    compose up -d --wait core-postgres vault-postgres tracker-postgres
    backup_database core-postgres peergo_core peergo_core "${backup_dir}/peergo_core.dump"
    backup_database vault-postgres peergo_vault peergo_vault "${backup_dir}/peergo_vault.dump"
    backup_database tracker-postgres peergo_tracker peergo_tracker "${backup_dir}/peergo_tracker.dump"
    move_if_present "${object_root}" "${backup_dir}/objects"
    move_if_present "${tracker_root}" "${backup_dir}/tracker"

    note "removing only the PeerGo local Compose volumes"
    compose down -v --remove-orphans
    note "old local databases were removed; private pg_dump/object backups are recoverable at ${backup_dir}"

    compose up -d --wait
    restore_source_database
else
    mkdir -p "${image_temp_dir}"
    chmod 700 "${image_temp_dir}"
    wait_for_database "${core_database_url}" 'PeerGo Core database'
    if ! psql "${source_database_url}" -X -A -t -v ON_ERROR_STOP=1 -c \
        "SELECT 1 FROM users LIMIT 1" >/dev/null 2>&1; then
        note "restored source database is unavailable; rebuilding it for the same immutable run"
        psql "${core_database_url}" -X -v ON_ERROR_STOP=1 -c \
            "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'peergo_legacy_source' AND pid <> pg_backend_pid()" >/dev/null
        psql "${core_database_url}" -X -v ON_ERROR_STOP=1 -c \
            'DROP DATABASE IF EXISTS peergo_legacy_source' >/dev/null
        restore_source_database
    fi
fi

note "applying current PeerGo migrations"
GOCACHE="${GOCACHE:-/private/tmp/peergo-go-cache}" make -C "${repo_root}" db-migrate
verify_core_runtime_defaults

# Traffic starts from zero at the PeerGo cutover, but Settlement still requires
# an explicit complete policy revision for every interval. Bind that revision to
# this immutable source snapshot so a resumed restore verifies the same command
# instead of creating another baseline or silently assuming a runtime default.
note "appending deterministic Settlement policy baseline"
PEERGO_ENV=development \
PEERGO_TRACKER_DATABASE_URL="${tracker_database_url}" \
GOWORK=off go -C "${repo_root}/services/settlement" run ./cmd/policy-timeline-append \
    --id "${run_id}" \
    --snapshot-file "${repo_root}/examples/settlement/policy-snapshot.peergo-v1-normal.json" \
    --effective-at "${occurred_at}" | tee "${run_dir}/settlement-policy-baseline.log"

# Preflight is read-only but still requires the provisioned destination root to
# exist. Creating the empty directory grants no object write or overwrite.
mkdir -p "${object_root}"
chmod 700 "${object_root}"

export PEERGO_ENV=development
export PEERGO_LEGACY_DUMP_PATH="${dump_path}"
export PEERGO_LEGACY_TORRENT_ROOT="${torrent_path}"
export PEERGO_LEGACY_IMAGE_ROOT="${image_path}"
export PEERGO_LEGACY_SOURCE_DATABASE_URL="${source_database_url}"
export PEERGO_CORE_DATABASE_URL="${core_database_url}"
export PEERGO_VAULT_DATABASE_URL="${vault_database_url}"
export PEERGO_LEGACY_RUN_ID="${run_id}"
export PEERGO_LEGACY_SNAPSHOT_SHA256="${dump_sha256}"
export PEERGO_LEGACY_MAPPING_VERSION=ptyes-v1
export PEERGO_LEGACY_OCCURRED_AT="${occurred_at}"
export PEERGO_LEGACY_RECONCILED_AT="${occurred_at}"
export PEERGO_LEGACY_PROGRESS_EVERY=250
export PEERGO_LEGACY_FINGERPRINT_KEY='peergo-local-legacy-fingerprint-key-v1-2026'
export PEERGO_VAULT_IDENTIFIER_KEY='peergo-local-identifier-key-v1-2026'
export PEERGO_VAULT_TRACKER_PASSKEY_ENCRYPTION_KEY='abcdef0123456789abcdef0123456789'
export PEERGO_VAULT_TRACKER_PASSKEY_KEY_EPOCH='local-2026-08'
export PEERGO_TRACKER_PASSKEY_LOOKUP_KEY='peergo-local-tracker-passkey-lookup-key-v1-2026'
export PEERGO_TORRENT_STORAGE_BACKEND_ID=local-primary
export PEERGO_TORRENT_STORAGE_DRIVER=filesystem
export PEERGO_TORRENT_STORAGE_FILESYSTEM_ROOT="${object_root}"
export PEERGO_IMAGE_DERIVATIVE_TEMP_DIR="${image_temp_dir}"
export PEERGO_IMAGE_DERIVATIVE_VIPS_BINARY="$(command -v vips)"

prepare_local_exclusions
run_state='not_started'
if [[ "${resume}" == true ]]; then
    run_state="$(psql "${core_database_url}" -X -A -t -v ON_ERROR_STOP=1 -c \
        "SELECT state FROM migration.runs WHERE id = '${run_id}'")"
fi
if [[ "${run_state}" == 'reconciled' ]]; then
    # A post-reconciliation failure must not replay the already accepted
    # user/torrent/media phases. WebP generation happens after reconciliation,
    # however, so always drain it idempotently before creating fresh signed
    # Tracker snapshots and attempting acceptance again.
    preflight_candidates="$(find "${run_dir}" -maxdepth 1 -type f -name 'preflight-*.json' -print | sort)"
    if [[ -n "${preflight_candidates}" ]]; then
        preflight_output="$(printf '%s\n' "${preflight_candidates}" | tail -n 1)"
    elif [[ -f "${run_dir}/preflight.json" ]]; then
        preflight_output="${run_dir}/preflight.json"
    else
        fail "reconciled resume has no preflight manifest"
    fi
    note "migration run is already reconciled; repairing/verifying medals before image derivatives, Tracker snapshot and acceptance"
    GOWORK=off go -C "${repo_root}/services/core" run ./cmd/legacy-medals --action import |
        tee -a "${run_dir}/migration.log"
    GOWORK=off go -C "${repo_root}/services/core" run ./cmd/legacy-medals --action import |
        tee -a "${run_dir}/migration.log"
    "${repo_root}/scripts/migrate-ptyes.sh" image-derivatives | tee -a "${run_dir}/migration.log"
else
    export PEERGO_LEGACY_PREFLIGHT_OUTPUT="${preflight_output}"
    note "running the typed user, medal, torrent and media migration pipeline"
    "${repo_root}/scripts/migrate-ptyes.sh" all | tee "${run_dir}/migration.log"
fi

verify_legacy_member_authorizations
drain_workgroup_benefits
drain_tracker_projection
mkdir -p "${tracker_root}"
chmod 700 "${tracker_root}"
PEERGO_ENV=development \
PEERGO_CORE_DATABASE_URL="${core_database_url}" \
PEERGO_TRACKER_SNAPSHOT_KEY_ID='local-2026-08' \
PEERGO_TRACKER_SNAPSHOT_SIGNING_KEY_BASE64='nWGxne/9WmC6hEr0kuwsxERJxWl7MmkZcDusAxyuf2A=' \
PEERGO_TRACKER_SNAPSHOT_PATH="${tracker_root}/control.snapshot" \
PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH="${tracker_root}/subjects.snapshot" \
PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH="${tracker_root}/runtime-policy.snapshot" \
GOWORK=off go -C "${repo_root}/services/core" run ./cmd/snapshot-builder |
    tee "${run_dir}/snapshot-builder.log"

export PEERGO_LEGACY_PREFLIGHT_MANIFEST="${preflight_output}"
export PEERGO_LEGACY_ACCEPTANCE_OUTPUT="${acceptance_output}"
export PEERGO_TRACKER_SNAPSHOT_PATH="${tracker_root}/control.snapshot"
export PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH="${tracker_root}/subjects.snapshot"
export PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH="${tracker_root}/runtime-policy.snapshot"
export PEERGO_TRACKER_SNAPSHOT_TRUSTED_KEYS='local-2026-08=11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo='
export PEERGO_TRACKER_SNAPSHOT_MAX_AGE=30m
export PEERGO_TRACKER_SUBJECT_SNAPSHOT_MAX_AGE=10m
export PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_MAX_AGE=30m
export PEERGO_TRACKER_SNAPSHOT_MAX_FUTURE_SKEW=2m
note "running full object and signed-snapshot acceptance"
"${repo_root}/scripts/migrate-ptyes.sh" acceptance | tee "${run_dir}/acceptance.log"

GOWORK=off go -C "${repo_root}/services/core" run ./cmd/legacy-torrents --action status |
    tee "${run_dir}/status.log"
write_restore_summary
note "Rousi local restore completed: ${run_dir}"
