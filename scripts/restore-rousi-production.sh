#!/usr/bin/env bash

set -Eeuo pipefail

# Host-side production cutover launcher. It never creates, drops or truncates
# a PeerGo target database. Database ownership, backups, old-site stop-write
# and the empty dedicated legacy source database remain explicit operator
# responsibilities. All legacy parsing runs in the purpose-built cutover image.

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/.." && pwd -P)"
production_compose="${repo_root}/deploy/compose/compose.production.yaml"
cutover_compose="${repo_root}/deploy/compose/compose.cutover.yaml"
env_file="${PEERGO_PRODUCTION_ENV_FILE:-${repo_root}/.env.production}"
default_run_root="${repo_root}/.local/production-cutovers"

usage() {
    cat <<'EOF'
Usage:
  PEERGO_PRODUCTION_RESTORE_CONFIRM=PREPARE_ROUSI_PRODUCTION \
  PEERGO_CUTOVER_BACKUP_REFERENCE='<backup/PITR reference>' \
  PEERGO_CUTOVER_WRITES_STOPPED_AT='<RFC3339>' \
    scripts/restore-rousi-production.sh prepare \
      /absolute/rousi_YYYYMMDDhhmmss.sql.gz \
      /absolute/torrents.zip /absolute/uploads.zip

  PEERGO_PRODUCTION_RESTORE_CONFIRM=APPLY_ROUSI_PRODUCTION \
  PEERGO_CUTOVER_BACKUP_REFERENCE='<same reference>' \
  PEERGO_CUTOVER_WRITES_STOPPED_AT='<same RFC3339>' \
  PEERGO_CUTOVER_OPERATOR_REFERENCE='<ticket/change reference>' \
  PEERGO_ROUSI_MISSING_TORRENTS_APPROVAL='APPROVE:<candidate sha256>' \
    scripts/restore-rousi-production.sh apply <same three files>

  scripts/restore-rousi-production.sh status <same three files>

prepare restores only the dedicated legacy source database, inspects all three
immutable inputs and emits a snapshot-bound missing-torrent candidate when
needed. It does not import PeerGo users, balances, torrents or images.

apply requires the exact candidate digest when any SQL-referenced .torrent is
missing. It imports into already migrated empty PeerGo targets, drains typed
benefit/Tracker projections, creates signed snapshots and succeeds only when
acceptance writes ready_to_activate=true.
EOF
}

fail() {
    printf 'Rousi production restore: %s\n' "$*" >&2
    exit 1
}

note() {
    printf 'Rousi production restore: %s\n' "$*"
}

required_command() {
    command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

absolute_regular_file() {
    local input="$1"
    [[ "${input}" = /* ]] || fail "input paths must be absolute: ${input}"
    [[ -f "${input}" && ! -L "${input}" ]] || fail "input must be a non-symlink regular file: ${input}"
    local parent
    parent="$(cd "$(dirname "${input}")" && pwd -P)" || fail "input parent is unavailable"
    printf '%s/%s\n' "${parent}" "$(basename "${input}")"
}

sha256_file() {
    if command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | awk '{print $1}'
        return
    fi
    sha256sum "$1" | awk '{print $1}'
}

run_id_from_snapshot() {
    local digest="$1"
    printf '%s-%s-5%s-8%s-%s\n' \
        "${digest:0:8}" "${digest:8:4}" "${digest:13:3}" \
        "${digest:17:3}" "${digest:20:12}"
}

occurred_at_from_dump_name() {
    local name stamp
    name="$(basename "$1")"
    if [[ "${name}" =~ ([0-9]{14}) ]]; then
        stamp="${BASH_REMATCH[1]}"
        printf '%s-%s-%sT%s:%s:%s+08:00\n' \
            "${stamp:0:4}" "${stamp:4:2}" "${stamp:6:2}" \
            "${stamp:8:2}" "${stamp:10:2}" "${stamp:12:2}"
        return
    fi
    fail "dump filename must contain its fixed Asia/Shanghai YYYYMMDDhhmmss snapshot time"
}

assert_manifest_line() {
    local file="$1"
    local expected="$2"
    grep -Fqx -- "${expected}" "${file}" ||
        fail "existing run directory belongs to different inputs: ${expected%%=*}"
}

write_or_verify_host_manifest() {
    local manifest="${run_dir}/host-inputs.env"
    if [[ -f "${manifest}" ]]; then
        assert_manifest_line "${manifest}" "schema=peergo.rousi-production-host-inputs.v1"
        assert_manifest_line "${manifest}" "run_id=${run_id}"
        assert_manifest_line "${manifest}" "dump_sha256=${dump_sha256}"
        assert_manifest_line "${manifest}" "torrent_sha256=${torrent_sha256}"
        assert_manifest_line "${manifest}" "image_sha256=${image_sha256}"
        return
    fi
    {
        printf 'schema=peergo.rousi-production-host-inputs.v1\n'
        printf 'run_id=%s\n' "${run_id}"
        printf 'occurred_at=%s\n' "${occurred_at}"
        printf 'dump_sha256=%s\n' "${dump_sha256}"
        printf 'torrent_sha256=%s\n' "${torrent_sha256}"
        printf 'image_sha256=%s\n' "${image_sha256}"
    } >"${manifest}"
    chmod 600 "${manifest}"
}

compose() {
    docker compose \
        --env-file "${env_file}" \
        -f "${production_compose}" \
        -f "${cutover_compose}" \
        "$@"
}

[[ "$#" == '4' ]] || {
    usage >&2
    exit 2
}

action="$1"
case "${action}" in
    prepare|apply|status) ;;
    *) usage >&2; exit 2 ;;
esac

required_command docker
required_command gzip
required_command unzip
[[ -f "${env_file}" && ! -L "${env_file}" ]] ||
    fail "production environment file is unavailable or is a symlink: ${env_file}"

dump_path="$(absolute_regular_file "$2")"
torrent_path="$(absolute_regular_file "$3")"
image_path="$(absolute_regular_file "$4")"
[[ "${dump_path}" == *.gz ]] || fail "database dump must be gzip wrapped"
[[ "${torrent_path}" == *.zip ]] || fail "torrent input must be a ZIP archive"
[[ "${image_path}" == *.zip ]] || fail "image input must be a ZIP archive"
[[ "${torrent_path}" != "${image_path}" ]] || fail "three-package mode requires distinct ZIP files"

gzip -t "${dump_path}"
unzip -tq "${torrent_path}" >/dev/null
unzip -tq "${image_path}" >/dev/null
note "hashing the final immutable three-package snapshot"
dump_sha256="$(sha256_file "${dump_path}")"
torrent_sha256="$(sha256_file "${torrent_path}")"
image_sha256="$(sha256_file "${image_path}")"
run_id="$(run_id_from_snapshot "${dump_sha256}")"
occurred_at="$(occurred_at_from_dump_name "${dump_path}")"

run_dir="${PEERGO_PRODUCTION_CUTOVER_RUN_DIR:-${default_run_root}/${run_id}}"
[[ "${run_dir}" = /* && ! -L "${run_dir}" ]] || fail "cutover run directory must be absolute and not a symlink"
mkdir -p "${run_dir}"
chmod 700 "${run_dir}"
write_or_verify_host_manifest

case "${action}" in
    prepare)
        [[ "${PEERGO_PRODUCTION_RESTORE_CONFIRM:-}" == 'PREPARE_ROUSI_PRODUCTION' ]] ||
            fail "set PEERGO_PRODUCTION_RESTORE_CONFIRM=PREPARE_ROUSI_PRODUCTION"
        ;;
    apply)
        [[ "${PEERGO_PRODUCTION_RESTORE_CONFIRM:-}" == 'APPLY_ROUSI_PRODUCTION' ]] ||
            fail "set PEERGO_PRODUCTION_RESTORE_CONFIRM=APPLY_ROUSI_PRODUCTION"
        [[ -n "${PEERGO_CUTOVER_OPERATOR_REFERENCE:-}" ]] ||
            fail "PEERGO_CUTOVER_OPERATOR_REFERENCE is required for apply"
        ;;
esac
if [[ "${action}" != 'status' ]]; then
    [[ -n "${PEERGO_CUTOVER_BACKUP_REFERENCE:-}" ]] ||
        fail "PEERGO_CUTOVER_BACKUP_REFERENCE is required"
    [[ "${PEERGO_CUTOVER_WRITES_STOPPED_AT:-}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(Z|[+-][0-9]{2}:[0-9]{2})$ ]] ||
        fail "PEERGO_CUTOVER_WRITES_STOPPED_AT must be an RFC3339 timestamp"
fi

if [[ "${action}" == 'apply' && -f "${run_dir}/torrent-exclusions.candidate.sha256" ]]; then
    candidate_sha256="$(tr -d '[:space:]' <"${run_dir}/torrent-exclusions.candidate.sha256")"
    expected_approval="APPROVE:${candidate_sha256}"
    [[ "${PEERGO_ROUSI_MISSING_TORRENTS_APPROVAL:-}" == "${expected_approval}" ]] ||
        fail "review ${run_dir}/torrent-exclusions.candidate.tsv, then set PEERGO_ROUSI_MISSING_TORRENTS_APPROVAL=${expected_approval}"
fi

export PEERGO_ENV_FILE="${env_file}"
export PEERGO_CUTOVER_DUMP_HOST_PATH="${dump_path}"
export PEERGO_CUTOVER_TORRENTS_HOST_PATH="${torrent_path}"
export PEERGO_CUTOVER_UPLOADS_HOST_PATH="${image_path}"
export PEERGO_CUTOVER_OUTPUT_HOST_PATH="${run_dir}"
export PEERGO_CUTOVER_HOST_UID="$(id -u)"
export PEERGO_CUTOVER_HOST_GID="$(id -g)"
export PEERGO_LEGACY_RUN_ID="${run_id}"
export PEERGO_LEGACY_SNAPSHOT_SHA256="${dump_sha256}"
export PEERGO_LEGACY_OCCURRED_AT="${occurred_at}"
export PEERGO_LEGACY_RECONCILED_AT="${occurred_at}"
export PEERGO_CUTOVER_BACKUP_REFERENCE="${PEERGO_CUTOVER_BACKUP_REFERENCE:-status-only}"
export PEERGO_CUTOVER_WRITES_STOPPED_AT="${PEERGO_CUTOVER_WRITES_STOPPED_AT:-1970-01-01T00:00:00Z}"
export PEERGO_CUTOVER_OPERATOR_REFERENCE="${PEERGO_CUTOVER_OPERATOR_REFERENCE:-status-only}"
export PEERGO_ROUSI_MISSING_TORRENTS_APPROVAL="${PEERGO_ROUSI_MISSING_TORRENTS_APPROVAL:-}"

note "validating production and cutover Compose configuration"
compose --profile cutover config --quiet
note "building immutable runtime and cutover images"
compose --profile cutover build core-migrate rousi-cutover

if [[ "${action}" != 'status' ]]; then
    note "applying idempotent Core, Vault and Tracker Ledger schema migrations"
    compose run --rm --no-deps core-migrate
    compose run --rm --no-deps vault-migrate
    compose run --rm --no-deps tracker-migrate
fi

compose --profile cutover run --rm --no-deps rousi-cutover "${action}"

case "${action}" in
    prepare)
        if [[ -f "${run_dir}/torrent-exclusions.candidate.sha256" ]]; then
            candidate_sha256="$(tr -d '[:space:]' <"${run_dir}/torrent-exclusions.candidate.sha256")"
            note "prepare complete; review ${run_dir}/torrent-exclusions.candidate.tsv"
            note "apply approval value: APPROVE:${candidate_sha256}"
        else
            note "prepare complete; no missing .torrent approval is required"
        fi
        ;;
    apply)
        [[ -f "${run_dir}/ready-to-activate.env" ]] ||
            fail "cutover container exited without ready-to-activate evidence"
        grep -Fqx 'ready_to_activate=true' "${run_dir}/ready-to-activate.env" ||
            fail "ready-to-activate evidence is invalid"
        note "migration acceptance passed: ${run_dir}/ready-to-activate.env"
        note "do not switch public traffic until production-up, admin review and production-activation-check also pass"
        ;;
esac
