#!/usr/bin/env bash

set -Eeuo pipefail

# Runs only inside the dedicated production cutover image. The host wrapper
# binds exactly three immutable inputs read-only and a private artifact
# directory read-write. Application containers never receive the legacy source
# database owner credential or the raw archives.

migration_script=/usr/local/libexec/peergo/migrate-ptyes.sh
binary_root=/usr/local/bin
policy_snapshot=/usr/local/share/peergo/policy-snapshot.peergo-v1-normal.json
output_root=/cutover/output
peergo_uid=10001
peergo_gid=10001

fail() {
    printf 'Rousi production cutover: %s\n' "$*" >&2
    exit 1
}

note() {
    printf 'Rousi production cutover: %s\n' "$*"
}

required_env() {
    local name="$1"
    [[ -n "${!name:-}" ]] || fail "${name} is required"
}

sha256_file() {
    sha256sum "$1" | awk '{print $1}'
}

run_id_from_snapshot() {
    local digest="$1"
    printf '%s-%s-5%s-8%s-%s\n' \
        "${digest:0:8}" "${digest:8:4}" "${digest:13:3}" \
        "${digest:17:3}" "${digest:20:12}"
}

finalize_permissions() {
    local exit_code=$?
    if [[ "$(id -u)" == '0' ]]; then
        chown -R "${peergo_uid}:${peergo_gid}" \
            /var/lib/peergo/objects \
            /var/lib/peergo/tracker \
            /var/lib/peergo/image-derivative-tmp 2>/dev/null || true
        if [[ "${PEERGO_CUTOVER_HOST_UID:-}" =~ ^[0-9]+$ &&
              "${PEERGO_CUTOVER_HOST_GID:-}" =~ ^[0-9]+$ ]]; then
            chown -R "${PEERGO_CUTOVER_HOST_UID}:${PEERGO_CUTOVER_HOST_GID}" \
                "${output_root}" 2>/dev/null || true
        fi
    fi
    return "${exit_code}"
}

trap finalize_permissions EXIT

assert_manifest_line() {
    local file="$1"
    local expected="$2"
    grep -Fqx -- "${expected}" "${file}" ||
        fail "cutover artifact does not match this immutable input: ${expected%%=*}"
}

write_or_verify_input_manifest() {
    local manifest="${output_root}/inputs.env"
    local backup_reference_sha256
    backup_reference_sha256="$(printf '%s' "${PEERGO_CUTOVER_BACKUP_REFERENCE}" | sha256sum | awk '{print $1}')"
    if [[ -f "${manifest}" ]]; then
        assert_manifest_line "${manifest}" "schema=peergo.rousi-production-inputs.v1"
        assert_manifest_line "${manifest}" "run_id=${PEERGO_LEGACY_RUN_ID}"
        assert_manifest_line "${manifest}" "occurred_at=${PEERGO_LEGACY_OCCURRED_AT}"
        assert_manifest_line "${manifest}" "dump_sha256=${dump_sha256}"
        assert_manifest_line "${manifest}" "torrent_sha256=${torrent_sha256}"
        assert_manifest_line "${manifest}" "image_sha256=${image_sha256}"
        if [[ "${action:-}" != 'status' ]]; then
            assert_manifest_line "${manifest}" "backup_reference_sha256=${backup_reference_sha256}"
            assert_manifest_line "${manifest}" "writes_stopped_at=${PEERGO_CUTOVER_WRITES_STOPPED_AT}"
        fi
        return
    fi
    [[ "${action:-}" != 'status' ]] || fail "prepare evidence is not present for this input snapshot"
    {
        printf 'schema=peergo.rousi-production-inputs.v1\n'
        printf 'run_id=%s\n' "${PEERGO_LEGACY_RUN_ID}"
        printf 'occurred_at=%s\n' "${PEERGO_LEGACY_OCCURRED_AT}"
        printf 'dump_sha256=%s\n' "${dump_sha256}"
        printf 'torrent_sha256=%s\n' "${torrent_sha256}"
        printf 'image_sha256=%s\n' "${image_sha256}"
        printf 'backup_reference_sha256=%s\n' "${backup_reference_sha256}"
        printf 'writes_stopped_at=%s\n' "${PEERGO_CUTOVER_WRITES_STOPPED_AT}"
    } >"${manifest}"
    chmod 600 "${manifest}"
}

prepare_environment() {
    required_env PEERGO_LEGACY_DUMP_PATH
    required_env PEERGO_LEGACY_TORRENT_ROOT
    required_env PEERGO_LEGACY_IMAGE_ROOT
    required_env PEERGO_LEGACY_RESTORE_DATABASE_URL
    required_env PEERGO_LEGACY_SOURCE_DATABASE_URL
    required_env PEERGO_CORE_DATABASE_URL
    required_env PEERGO_VAULT_DATABASE_URL
    required_env PEERGO_TRACKER_DATABASE_URL
    required_env PEERGO_LEGACY_RUN_ID
    required_env PEERGO_LEGACY_OCCURRED_AT
    required_env PEERGO_CUTOVER_BACKUP_REFERENCE
    required_env PEERGO_CUTOVER_WRITES_STOPPED_AT

    [[ -x "${migration_script}" ]] || fail "migration orchestrator is unavailable"
    [[ -f "${policy_snapshot}" ]] || fail "settlement baseline snapshot is unavailable"
    [[ "${PEERGO_LEGACY_DUMP_PATH}" = /* && -f "${PEERGO_LEGACY_DUMP_PATH}" ]] ||
        fail "database dump is unavailable"
    [[ "${PEERGO_LEGACY_TORRENT_ROOT}" = /* && -f "${PEERGO_LEGACY_TORRENT_ROOT}" ]] ||
        fail "torrent archive is unavailable"
    [[ "${PEERGO_LEGACY_IMAGE_ROOT}" = /* && -f "${PEERGO_LEGACY_IMAGE_ROOT}" ]] ||
        fail "image archive is unavailable"
    [[ "${PEERGO_LEGACY_TORRENT_ROOT}" != "${PEERGO_LEGACY_IMAGE_ROOT}" ]] ||
        fail "three-package cutover requires distinct torrent and image archives"
    [[ "${PEERGO_LEGACY_OCCURRED_AT}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(Z|[+-][0-9]{2}:[0-9]{2})$ ]] ||
        fail "PEERGO_LEGACY_OCCURRED_AT must be RFC3339 with seconds and an offset"

    mkdir -p "${output_root}" /var/lib/peergo/objects /var/lib/peergo/tracker \
        /var/lib/peergo/image-derivative-tmp
    chmod 700 "${output_root}"
    umask 077

    note "hashing the three immutable inputs inside the cutover container"
    dump_sha256="$(sha256_file "${PEERGO_LEGACY_DUMP_PATH}")"
    torrent_sha256="$(sha256_file "${PEERGO_LEGACY_TORRENT_ROOT}")"
    image_sha256="$(sha256_file "${PEERGO_LEGACY_IMAGE_ROOT}")"
    [[ "${PEERGO_LEGACY_SNAPSHOT_SHA256:-${dump_sha256}}" == "${dump_sha256}" ]] ||
        fail "database dump digest differs from the host manifest"
    [[ "${PEERGO_LEGACY_RUN_ID}" == "$(run_id_from_snapshot "${dump_sha256}")" ]] ||
        fail "run ID does not match the immutable database dump"

    export PEERGO_ENV=production
    export PEERGO_LEGACY_BIN_DIR="${binary_root}"
    export PEERGO_LEGACY_SNAPSHOT_SHA256="${dump_sha256}"
    # Bind the archive compatibility gate in migrate-ptyes.sh to the exact
    # read-only inputs already hashed by this cutover container.
    export PEERGO_LEGACY_TORRENT_ARCHIVE_SHA256="${torrent_sha256}"
    export PEERGO_LEGACY_IMAGE_ARCHIVE_SHA256="${image_sha256}"
    export PEERGO_LEGACY_MAPPING_VERSION="${PEERGO_LEGACY_MAPPING_VERSION:-ptyes-v1}"
    export PEERGO_LEGACY_RECONCILED_AT="${PEERGO_LEGACY_RECONCILED_AT:-${PEERGO_LEGACY_OCCURRED_AT}}"
    export PEERGO_LEGACY_PROGRESS_EVERY="${PEERGO_LEGACY_PROGRESS_EVERY:-250}"
    export PEERGO_IMAGE_DERIVATIVE_TEMP_DIR=/var/lib/peergo/image-derivative-tmp
    export PEERGO_IMAGE_DERIVATIVE_VIPS_BINARY="${PEERGO_IMAGE_DERIVATIVE_VIPS_BINARY:-vips}"

    write_or_verify_input_manifest
}

prepare_cutover() {
    "${migration_script}" inspect | tee "${output_root}/inspect.log"

    if [[ ! -f "${output_root}/source-restored.env" ]]; then
        note "restoring the immutable dump into the dedicated empty source database"
        "${migration_script}" restore | tee "${output_root}/source-restore.log"
        {
            printf 'run_id=%s\n' "${PEERGO_LEGACY_RUN_ID}"
            printf 'dump_sha256=%s\n' "${dump_sha256}"
        } >"${output_root}/source-restored.env"
        chmod 600 "${output_root}/source-restored.env"
    else
        assert_manifest_line "${output_root}/source-restored.env" "run_id=${PEERGO_LEGACY_RUN_ID}"
        assert_manifest_line "${output_root}/source-restored.env" "dump_sha256=${dump_sha256}"
        note "reusing the already restored source database for this immutable run"
    fi

    local candidate="${output_root}/torrent-exclusions.candidate.tsv"
    local candidate_log="${output_root}/torrent-exclusions.log"
    if [[ -f "${candidate}" ]]; then
        note "reusing the existing missing-torrent candidate"
    elif [[ -f "${output_root}/torrent-exclusions.none" ]]; then
        note "all SQL-referenced torrent objects were already verified present"
    else
        export PEERGO_LEGACY_TORRENT_EXCLUSIONS_OUTPUT="${candidate}"
        note "checking for SQL rows whose immutable .torrent object is absent"
        if "${migration_script}" torrents-exclusions 2>&1 | tee "${candidate_log}"; then
            [[ -s "${candidate}" ]] || fail "missing-torrent candidate is empty"
        elif grep -q 'no physically missing torrent objects require an exclusion candidate' "${candidate_log}"; then
            printf 'run_id=%s\n' "${PEERGO_LEGACY_RUN_ID}" >"${output_root}/torrent-exclusions.none"
            chmod 600 "${output_root}/torrent-exclusions.none"
        else
            fail "could not establish the missing-torrent decision set"
        fi
    fi

    if [[ -f "${candidate}" ]]; then
        local candidate_sha256
        candidate_sha256="$(sha256_file "${candidate}")"
        printf '%s\n' "${candidate_sha256}" >"${output_root}/torrent-exclusions.candidate.sha256"
        chmod 600 "${output_root}/torrent-exclusions.candidate.sha256"
        note "review required: missing-torrent candidate SHA-256=${candidate_sha256}"
    fi
    printf 'prepared_at=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" >"${output_root}/prepared.env"
    chmod 600 "${output_root}/prepared.env"
    note "prepare completed without importing PeerGo users, torrents, images or balances"
}

configure_exclusions() {
    local candidate="${output_root}/torrent-exclusions.candidate.tsv"
    local approved="${output_root}/torrent-exclusions.approved.tsv"
    if [[ -f "${candidate}" ]]; then
        local candidate_sha256 expected_approval
        candidate_sha256="$(sha256_file "${candidate}")"
        expected_approval="APPROVE:${candidate_sha256}"
        [[ "${PEERGO_ROUSI_MISSING_TORRENTS_APPROVAL:-}" == "${expected_approval}" ]] ||
            fail "review the candidate, then set PEERGO_ROUSI_MISSING_TORRENTS_APPROVAL=${expected_approval}"
        cp "${candidate}" "${approved}"
        chmod 600 "${approved}"
        export PEERGO_LEGACY_TORRENT_EXCLUSIONS="${approved}"
        {
            printf 'schema=peergo.rousi-missing-torrent-approval.v1\n'
            printf 'run_id=%s\n' "${PEERGO_LEGACY_RUN_ID}"
            printf 'candidate_sha256=%s\n' "${candidate_sha256}"
            printf 'operator_reference_sha256=%s\n' "$(printf '%s' "${PEERGO_CUTOVER_OPERATOR_REFERENCE}" | sha256sum | awk '{print $1}')"
            printf 'approved_at=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
        } >"${output_root}/torrent-exclusions.approval.env"
        chmod 600 "${output_root}/torrent-exclusions.approval.env"
        return
    fi
    [[ -f "${output_root}/torrent-exclusions.none" ]] ||
        fail "prepare did not record a missing-torrent decision"
    unset PEERGO_LEGACY_TORRENT_EXCLUSIONS
}

current_run_state() {
    psql "${PEERGO_CORE_DATABASE_URL}" -X -A -t -v ON_ERROR_STOP=1 -c \
        "SELECT COALESCE((SELECT state::text FROM migration.runs WHERE id = '${PEERGO_LEGACY_RUN_ID}'::uuid), 'not_started')"
}

append_settlement_baseline() {
    note "appending or verifying the snapshot-bound normal 1x Settlement baseline"
    "${binary_root}/settlement-policy-timeline-append" \
        --id "${PEERGO_LEGACY_RUN_ID}" \
        --snapshot-file "${policy_snapshot}" \
        --effective-at "${PEERGO_LEGACY_OCCURRED_AT}" |
        tee "${output_root}/settlement-policy-baseline.log"
}

drain_workgroup_benefits() (
    set -Eeuo pipefail
    local control_address=127.0.0.1:18085
    local control_origin="http://${control_address}"
    local control_log="${output_root}/settlement-control.log"
    export PEERGO_SETTLEMENT_CONTROL_ADDR="${control_address}"
    "${binary_root}/settlement-control-api" >"${control_log}" 2>&1 &
    local control_pid=$!
    cleanup_control() {
        kill "${control_pid}" 2>/dev/null || true
        wait "${control_pid}" 2>/dev/null || true
    }
    trap cleanup_control EXIT INT TERM

    local attempt
    for attempt in $(seq 1 100); do
        if curl --fail --silent --show-error "${control_origin}/healthz" >/dev/null 2>&1; then
            break
        fi
        kill -0 "${control_pid}" 2>/dev/null || {
            cat "${control_log}" >&2
            fail "temporary Settlement control API stopped before becoming ready"
        }
        sleep 0.1
    done
    curl --fail --silent --show-error "${control_origin}/healthz" >/dev/null ||
        fail "temporary Settlement control API did not become ready"

    note "draining migrated retention-group benefit openings"
    PEERGO_SETTLEMENT_CONTROL_URL="${control_origin}" \
        "${binary_root}/core-policy-worker" --drain-workgroup-benefits |
        tee "${output_root}/workgroup-benefit-drain.log"
)

build_tracker_snapshots() {
    local snapshot_path parent
    for snapshot_path in \
        "${PEERGO_TRACKER_SNAPSHOT_PATH}" \
        "${PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH}" \
        "${PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH}"; do
        parent="$(dirname "${snapshot_path}")"
        [[ "${parent}" != '/' && ! -L "${parent}" ]] ||
            fail "Tracker snapshot parent is unsafe"
        mkdir -p "${parent}"
        # The filesystem publisher intentionally rejects group/world-readable
        # parents. Harden an existing single-server bind mount as well as a
        # freshly created cluster volume before composing the publisher.
        chmod 0700 "${parent}"
    done
    note "building one-shot signed Tracker control snapshots"
    PEERGO_TRACKER_SNAPSHOT_PUBLISH_INTERVAL='' \
        "${binary_root}/core-snapshot-publisher" |
        tee "${output_root}/snapshot-builder.log"
}

apply_cutover() {
    required_env PEERGO_CUTOVER_OPERATOR_REFERENCE
    # Fail before the long import if the signing or verification half of the
    # Tracker handoff is incomplete. These values deliberately come from the
    # untracked production environment, never from a generated default.
    required_env PEERGO_TRACKER_SNAPSHOT_KEY_ID
    required_env PEERGO_TRACKER_SNAPSHOT_SIGNING_KEY_BASE64
    required_env PEERGO_TRACKER_SNAPSHOT_PATH
    required_env PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH
    required_env PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH
    required_env PEERGO_TRACKER_SNAPSHOT_TRUSTED_KEYS
    required_env PEERGO_TRACKER_SNAPSHOT_MAX_AGE
    required_env PEERGO_TRACKER_SUBJECT_SNAPSHOT_MAX_AGE
    required_env PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_MAX_AGE
    required_env PEERGO_TRACKER_SNAPSHOT_MAX_FUTURE_SKEW
    required_env PEERGO_SETTLEMENT_CONTROL_SERVICE_TOKEN
    [[ -f "${output_root}/prepared.env" ]] || fail "run prepare before apply"
    [[ -f "${output_root}/source-restored.env" ]] || fail "source database restore evidence is missing"
    configure_exclusions

    local attempt_id run_state preflight_manifest
    attempt_id="$(date -u '+%Y%m%dT%H%M%SZ')"
    run_state="$(current_run_state)"
    if [[ "${run_state}" == 'reconciled' ]]; then
        preflight_manifest="$(find "${output_root}" -maxdepth 1 -type f -name 'preflight-*.json' -print | sort | tail -n 1)"
        [[ -n "${preflight_manifest}" ]] || fail "reconciled resume has no preflight manifest"
        note "run is already reconciled; replaying only idempotent medal and image verification"
        "${binary_root}/legacy-medals" --action import | tee -a "${output_root}/migration.log"
        "${binary_root}/legacy-medals" --action import | tee -a "${output_root}/migration.log"
        "${migration_script}" image-derivatives | tee -a "${output_root}/migration.log"
    else
        preflight_manifest="${output_root}/preflight-${attempt_id}.json"
        export PEERGO_LEGACY_PREFLIGHT_OUTPUT="${preflight_manifest}"
        note "running the fail-closed typed migration pipeline"
        "${migration_script}" all | tee "${output_root}/migration-${attempt_id}.log"
        cp "${output_root}/migration-${attempt_id}.log" "${output_root}/migration.log"
    fi

    append_settlement_baseline
    drain_workgroup_benefits
    note "draining the Tracker eligibility projection before snapshot signing"
    "${binary_root}/core-control-projector" --drain --drain-timeout=30m |
        tee "${output_root}/tracker-projector-drain.log"
    build_tracker_snapshots

    local acceptance_output="${output_root}/acceptance-${attempt_id}.json"
    export PEERGO_LEGACY_PREFLIGHT_MANIFEST="${preflight_manifest}"
    export PEERGO_LEGACY_ACCEPTANCE_OUTPUT="${acceptance_output}"
    note "running full read-back and signed-snapshot acceptance"
    "${migration_script}" acceptance | tee "${output_root}/acceptance-${attempt_id}.log"
    jq -e '.ready_to_activate == true' "${acceptance_output}" >/dev/null ||
        fail "acceptance did not produce ready_to_activate=true"
    "${migration_script}" status | tee "${output_root}/status.log"

    local acceptance_sha256
    acceptance_sha256="$(sha256_file "${acceptance_output}")"
    {
        printf 'schema=peergo.rousi-production-ready.v1\n'
        printf 'run_id=%s\n' "${PEERGO_LEGACY_RUN_ID}"
        printf 'acceptance=%s\n' "$(basename "${acceptance_output}")"
        printf 'acceptance_sha256=%s\n' "${acceptance_sha256}"
        printf 'ready_to_activate=true\n'
        printf 'verified_at=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    } >"${output_root}/ready-to-activate.env"
    chmod 600 "${output_root}/ready-to-activate.env"
    note "apply completed with ready_to_activate=true"
}

show_status() {
    "${migration_script}" status
    if [[ -f "${output_root}/ready-to-activate.env" ]]; then
        cat "${output_root}/ready-to-activate.env"
    else
        note "ready-to-activate evidence is not present"
    fi
}

action="${1:-}"
case "${action}" in
    prepare|apply|status)
        prepare_environment
        ;;
    *)
        fail "usage: rousi-cutover-container.sh prepare|apply|status"
        ;;
esac

case "${action}" in
    prepare) prepare_cutover ;;
    apply) apply_cutover ;;
    status) show_status ;;
esac
