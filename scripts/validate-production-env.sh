#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/.." && pwd -P)"
env_file="${PEERGO_PRODUCTION_ENV_FILE:-${repo_root}/.env.production}"

fail() {
    printf 'PeerGo production readiness: %s\n' "$*" >&2
    exit 1
}

[[ -f "${env_file}" && ! -L "${env_file}" ]] || fail "missing non-symlink environment file: ${env_file}"

env_get() {
    local name="$1"
    awk -v wanted="${name}" '
        $0 ~ "^[[:space:]]*" wanted "[[:space:]]*=" {
            line = $0
            sub("^[[:space:]]*" wanted "[[:space:]]*=[[:space:]]*", "", line)
            sub("[[:space:]]*$", "", line)
            print line
            exit
        }
    ' "${env_file}"
}

require_value() {
    local name="$1"
    local value
    value="$(env_get "${name}")"
    [[ -n "${value}" && "${value}" != "CHANGE_ME" ]] || fail "${name} must be configured"
}

[[ "$(env_get PEERGO_ENV)" == production ]] || fail "PEERGO_ENV must be production"
[[ "$(env_get PEERGO_PUBLIC_ORIGIN)" == https://* ]] || fail "PEERGO_PUBLIC_ORIGIN must use HTTPS"

runtime_layout="$(env_get PEERGO_RUNTIME_LAYOUT)"
runtime_layout="${runtime_layout:-compact}"
[[ "${runtime_layout}" == compact || "${runtime_layout}" == split ]] ||
    fail "PEERGO_RUNTIME_LAYOUT must be compact or split"

docker_log_max_size="$(env_get PEERGO_DOCKER_LOG_MAX_SIZE)"
if [[ "${docker_log_max_size}" =~ ^([1-9][0-9]*)([kKmMgG])$ ]]; then
    docker_log_quantity=$((10#${BASH_REMATCH[1]}))
    case "${BASH_REMATCH[2],,}" in
        k) docker_log_max_bytes=$((docker_log_quantity * 1024)) ;;
        m) docker_log_max_bytes=$((docker_log_quantity * 1024 * 1024)) ;;
        g) docker_log_max_bytes=$((docker_log_quantity * 1024 * 1024 * 1024)) ;;
    esac
else
    fail "PEERGO_DOCKER_LOG_MAX_SIZE must use a positive k, m or g suffix"
fi
((docker_log_max_bytes >= 1024 * 1024 && docker_log_max_bytes <= 1024 * 1024 * 1024)) ||
    fail "PEERGO_DOCKER_LOG_MAX_SIZE must be between 1m and 1g"
docker_log_max_files="$(env_get PEERGO_DOCKER_LOG_MAX_FILES)"
[[ "${docker_log_max_files}" =~ ^[1-9][0-9]?$ ]] && ((10#${docker_log_max_files} <= 10)) ||
    fail "PEERGO_DOCKER_LOG_MAX_FILES must be between 1 and 10"

if [[ "${runtime_layout}" == compact ]]; then
    [[ "$(env_get PEERGO_WEB_BIND_ADDRESS)" == 127.0.0.1 ]] ||
        fail "compact PEERGO_WEB_BIND_ADDRESS must be 127.0.0.1"
    api_host_port="$(env_get PEERGO_API_HOST_PORT)"
    [[ "${api_host_port}" =~ ^[1-9][0-9]{0,4}$ ]] && ((10#${api_host_port} <= 65535)) ||
        fail "PEERGO_API_HOST_PORT must be an integer between 1 and 65535"
    web_root="$(env_get PEERGO_WEB_ROOT)"
    [[ "${web_root}" = /* && "${web_root}" != "/" ]] ||
        fail "PEERGO_WEB_ROOT must be an absolute host path other than /"
    [[ -d "${web_root}" && ! -L "${web_root}" ]] ||
        fail "PEERGO_WEB_ROOT must be an existing non-symlink directory"
fi

require_value PEERGO_SMTP_HOST
require_value PEERGO_SMTP_USERNAME
require_value PEERGO_SMTP_PASSWORD
require_value PEERGO_SMTP_FROM_ADDRESS
require_value PEERGO_TRACKER_TRUSTED_PROXY_CIDRS
[[ "$(env_get PEERGO_SMTP_HOST)" != smtp.example.com ]] || fail "replace the example SMTP host"

tracker_announce_producer_id="$(env_get PEERGO_TRACKER_ANNOUNCE_PRODUCER_ID)"
[[ "${tracker_announce_producer_id}" =~ ^[a-z][a-z0-9-]{0,62}$ ]] ||
    fail "PEERGO_TRACKER_ANNOUNCE_PRODUCER_ID must be a stable lowercase identifier"

seeding_start="$(env_get PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT)"
[[ "${seeding_start}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:00:00Z$ ]] ||
    fail "PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT must be the exact UTC Tracker cutover hour"

duration_seconds() {
    local value="$1"
    if [[ "${value}" =~ ^([0-9]+)(s|m|h)$ ]]; then
        case "${BASH_REMATCH[2]}" in
            s) printf '%s' "$((10#${BASH_REMATCH[1]}))" ;;
            m) printf '%s' "$((10#${BASH_REMATCH[1]} * 60))" ;;
            h) printf '%s' "$((10#${BASH_REMATCH[1]} * 3600))" ;;
        esac
        return 0
    fi
    return 1
}

seeding_closure_raw="$(env_get PEERGO_SETTLEMENT_SEEDING_EVIDENCE_CLOSURE_DELAY)"
seeding_credit_raw="$(env_get PEERGO_SETTLEMENT_SEEDING_EVIDENCE_MAX_INTERVAL_CREDIT)"
seeding_closure_seconds="$(duration_seconds "${seeding_closure_raw}")" ||
    fail "PEERGO_SETTLEMENT_SEEDING_EVIDENCE_CLOSURE_DELAY must use one whole s, m or h unit"
seeding_credit_seconds="$(duration_seconds "${seeding_credit_raw}")" ||
    fail "PEERGO_SETTLEMENT_SEEDING_EVIDENCE_MAX_INTERVAL_CREDIT must use one whole s, m or h unit"
((seeding_closure_seconds >= 60 && seeding_closure_seconds <= 3600)) ||
    fail "seeding evidence closure delay must be between 1m and 1h"
((seeding_credit_seconds >= 60 && seeding_credit_seconds <= 3600)) ||
    fail "seeding evidence maximum interval credit must be between 1m and 1h"
((seeding_closure_seconds >= seeding_credit_seconds)) ||
    fail "seeding evidence closure delay must be at least its maximum interval credit"

settlement_policy_concurrency="$(env_get PEERGO_SETTLEMENT_POLICY_CONCURRENCY)"
[[ "${settlement_policy_concurrency}" =~ ^[1-9][0-9]?$ ]] &&
    ((10#${settlement_policy_concurrency} <= 32)) ||
    fail "PEERGO_SETTLEMENT_POLICY_CONCURRENCY must be an integer between 1 and 32"

settlement_batch_size="$(env_get PEERGO_SETTLEMENT_BATCH_SIZE)"
[[ "${settlement_batch_size}" =~ ^[1-9][0-9]{0,2}$ ]] &&
    ((10#${settlement_batch_size} <= 512)) ||
    fail "PEERGO_SETTLEMENT_BATCH_SIZE must be an integer between 1 and 512"

storage_interval_seconds="$(duration_seconds "$(env_get PEERGO_SETTLEMENT_STORAGE_CLEANUP_INTERVAL)")" ||
    fail "PEERGO_SETTLEMENT_STORAGE_CLEANUP_INTERVAL must use one whole s, m or h unit"
storage_terminal_seconds="$(duration_seconds "$(env_get PEERGO_SETTLEMENT_STORAGE_TERMINAL_RETENTION)")" ||
    fail "PEERGO_SETTLEMENT_STORAGE_TERMINAL_RETENTION must use one whole s, m or h unit"
storage_session_seconds="$(duration_seconds "$(env_get PEERGO_SETTLEMENT_STORAGE_SESSION_RETENTION)")" ||
    fail "PEERGO_SETTLEMENT_STORAGE_SESSION_RETENTION must use one whole s, m or h unit"
storage_detail_seconds="$(duration_seconds "$(env_get PEERGO_SETTLEMENT_STORAGE_DETAIL_RETENTION)")" ||
    fail "PEERGO_SETTLEMENT_STORAGE_DETAIL_RETENTION must use one whole s, m or h unit"
storage_anomaly_seconds="$(duration_seconds "$(env_get PEERGO_SETTLEMENT_STORAGE_ANOMALY_RETENTION)")" ||
    fail "PEERGO_SETTLEMENT_STORAGE_ANOMALY_RETENTION must use one whole s, m or h unit"
((storage_interval_seconds >= 10 && storage_interval_seconds <= 3600)) ||
    fail "Settlement storage cleanup interval must be between 10s and 1h"
((storage_terminal_seconds >= 3 * 3600 && storage_terminal_seconds <= 30 * 24 * 3600)) ||
    fail "Settlement terminal retention must be between 3h and 720h"
((storage_session_seconds >= 12 * 3600 && storage_session_seconds <= 30 * 24 * 3600)) ||
    fail "Settlement session retention must be between 12h and 720h"
((storage_detail_seconds >= 12 * 3600 && storage_detail_seconds <= 90 * 24 * 3600)) ||
    fail "Settlement detail retention must be between 12h and 2160h"
((storage_anomaly_seconds >= 30 * 24 * 3600 && storage_anomaly_seconds <= 365 * 24 * 3600)) ||
    fail "Settlement anomaly retention must be between 720h and 8760h"
((storage_detail_seconds >= storage_terminal_seconds && storage_detail_seconds >= storage_session_seconds)) ||
    fail "Settlement detail retention must cover terminal and session retention"
storage_batch_size="$(env_get PEERGO_SETTLEMENT_STORAGE_BATCH_SIZE)"
[[ "${storage_batch_size}" =~ ^[1-9][0-9]{2,4}$ ]] &&
    ((10#${storage_batch_size} >= 100 && 10#${storage_batch_size} <= 10000)) ||
    fail "PEERGO_SETTLEMENT_STORAGE_BATCH_SIZE must be an integer between 100 and 10000"

core_storage_interval_seconds="$(duration_seconds "$(env_get PEERGO_CORE_STORAGE_CLEANUP_INTERVAL)")" ||
    fail "PEERGO_CORE_STORAGE_CLEANUP_INTERVAL must use one whole s, m or h unit"
core_storage_detail_seconds="$(duration_seconds "$(env_get PEERGO_CORE_STORAGE_DETAIL_RETENTION)")" ||
    fail "PEERGO_CORE_STORAGE_DETAIL_RETENTION must use one whole s, m or h unit"
core_storage_history_seconds="$(duration_seconds "$(env_get PEERGO_CORE_STORAGE_HISTORY_RETENTION)")" ||
    fail "PEERGO_CORE_STORAGE_HISTORY_RETENTION must use one whole s, m or h unit"
((core_storage_interval_seconds >= 10 && core_storage_interval_seconds <= 3600)) ||
    fail "Core storage cleanup interval must be between 10s and 1h"
((core_storage_detail_seconds >= 12 * 3600 && core_storage_detail_seconds <= 7 * 24 * 3600)) ||
    fail "Core traffic detail retention must be between 12h and 168h"
((core_storage_history_seconds >= 30 * 24 * 3600 && core_storage_history_seconds <= 365 * 24 * 3600)) ||
    fail "Core traffic history retention must be between 720h and 8760h"
core_storage_batch_size="$(env_get PEERGO_CORE_STORAGE_BATCH_SIZE)"
[[ "${core_storage_batch_size}" =~ ^[1-9][0-9]{2,4}$ ]] &&
    ((10#${core_storage_batch_size} >= 100 && 10#${core_storage_batch_size} <= 10000)) ||
    fail "PEERGO_CORE_STORAGE_BATCH_SIZE must be an integer between 100 and 10000"

settlement_traffic_outbox_concurrency="$(env_get PEERGO_SETTLEMENT_TRAFFIC_OUTBOX_CONCURRENCY)"
[[ "${settlement_traffic_outbox_concurrency}" =~ ^[1-9][0-9]?$ ]] &&
    ((10#${settlement_traffic_outbox_concurrency} <= 32)) ||
    fail "PEERGO_SETTLEMENT_TRAFFIC_OUTBOX_CONCURRENCY must be an integer between 1 and 32"

core_traffic_concurrency="$(env_get PEERGO_CORE_TRAFFIC_CONCURRENCY)"
[[ "${core_traffic_concurrency}" =~ ^[1-9][0-9]?$ ]] &&
    ((10#${core_traffic_concurrency} <= 32)) ||
    fail "PEERGO_CORE_TRAFFIC_CONCURRENCY must be an integer between 1 and 32"

deployment_mode="$(env_get PEERGO_DEPLOYMENT_MODE)"
deployment_mode="${deployment_mode:-cluster}"
case "${deployment_mode}" in
    cluster) ;;
    single-server)
        network_name="$(env_get PEERGO_SINGLE_SERVER_NETWORK)"
        [[ "${network_name}" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] ||
            fail "PEERGO_SINGLE_SERVER_NETWORK is invalid"
        docker network inspect "${network_name}" >/dev/null 2>&1 ||
            fail "single-server Docker network does not exist: ${network_name}"
        expected_proxy_cidrs=()
        while IFS= read -r gateway; do
            [[ -z "${gateway}" ]] && continue
            [[ "${gateway}" != *[[:space:],/]* ]] ||
                fail "Docker network ${network_name} returned an invalid gateway"
            if [[ "${gateway}" == *:* ]]; then
                expected_proxy_cidrs+=("${gateway}/128")
            elif [[ "${gateway}" == *.* ]]; then
                expected_proxy_cidrs+=("${gateway}/32")
            else
                fail "Docker network ${network_name} returned an invalid gateway"
            fi
        done < <(docker network inspect --format '{{range .IPAM.Config}}{{println .Gateway}}{{end}}' "${network_name}")
        ((${#expected_proxy_cidrs[@]} > 0)) ||
            fail "Docker network ${network_name} has no gateway for the host HTTPS proxy"
        expected_proxy_value="$(IFS=,; printf '%s' "${expected_proxy_cidrs[*]}")"
        [[ "$(env_get PEERGO_TRACKER_TRUSTED_PROXY_CIDRS)" == "${expected_proxy_value}" ]] ||
            fail "PEERGO_TRACKER_TRUSTED_PROXY_CIDRS must equal the exact ${network_name} gateway CIDR(s): ${expected_proxy_value}"
        [[ "$(env_get PEERGO_CORE_TRUSTED_PROXY_CIDRS)" == "${expected_proxy_value}" ]] ||
            fail "PEERGO_CORE_TRUSTED_PROXY_CIDRS must equal the exact ${network_name} gateway CIDR(s): ${expected_proxy_value}"

        directory_names=(
            PEERGO_OBJECTS_VOLUME_SOURCE
            PEERGO_TRACKER_VOLUME_SOURCE
            PEERGO_AUDIT_VOLUME_SOURCE
            PEERGO_IMAGE_DERIVATIVES_VOLUME_SOURCE
            PEERGO_SINGLE_SERVER_NATS_DATA_PATH
            PEERGO_SECRET_DIR
        )
        for name in "${directory_names[@]}"; do
            value="$(env_get "${name}")"
            [[ "${value}" = /* && "${value}" != "/" ]] || fail "${name} must be an absolute host path other than /"
            [[ -d "${value}" && ! -L "${value}" ]] || fail "${name} must be an existing non-symlink directory"
        done
        nats_config="$(env_get PEERGO_SINGLE_SERVER_NATS_CONFIG_PATH)"
        [[ "${nats_config}" = /* && "${nats_config}" != "/" ]] || fail "PEERGO_SINGLE_SERVER_NATS_CONFIG_PATH must be an absolute host path"
        [[ -f "${nats_config}" && ! -L "${nats_config}" ]] || fail "NATS server config must be a non-symlink regular file"
        nats_credentials="$(env_get PEERGO_SECRET_DIR)/peergo-single-server-nats.creds"
        [[ -f "${nats_credentials}" && ! -L "${nats_credentials}" ]] || fail "NATS client credentials must be a non-symlink regular file"

        # JetStream reserves configured stream maxima when admitting a new
        # stream. Keep this synchronized with bootstrap's 200 GB one-node
        # file-store ceiling and stop before Compose starts any service.
        nats_stream_budget_names=(
            PEERGO_TRACKER_ANNOUNCE_STREAM_MAX_BYTES
            PEERGO_TRACKER_SWARM_STREAM_MAX_BYTES
            PEERGO_SETTLEMENT_SEEDING_EVIDENCE_STREAM_MAX_BYTES
            PEERGO_SETTLEMENT_TRAFFIC_STREAM_MAX_BYTES
            PEERGO_SETTLEMENT_HNR_STREAM_MAX_BYTES
        )
        nats_stream_reserved_bytes=0
        for name in "${nats_stream_budget_names[@]}"; do
            stream_max_bytes="$(env_get "${name}")"
            [[ "${stream_max_bytes}" =~ ^[1-9][0-9]*$ ]] ||
                fail "${name} must be a positive integer byte count"
            nats_stream_reserved_bytes=$((nats_stream_reserved_bytes + 10#${stream_max_bytes}))
        done
        nats_file_store_bytes=200000000000
        nats_required_headroom_bytes=10000000000
        ((nats_stream_reserved_bytes <= nats_file_store_bytes - nats_required_headroom_bytes)) ||
            fail "JetStream stream reservations total ${nats_stream_reserved_bytes} bytes; single-server permits at most $((nats_file_store_bytes - nats_required_headroom_bytes)) bytes so the 200 GB store retains 10 GB headroom"
        ;;
    *) fail "PEERGO_DEPLOYMENT_MODE must be cluster or single-server" ;;
esac

printf 'PeerGo production readiness: environment checks passed\n'
