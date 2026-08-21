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

require_value PEERGO_SMTP_HOST
require_value PEERGO_SMTP_USERNAME
require_value PEERGO_SMTP_PASSWORD
require_value PEERGO_SMTP_FROM_ADDRESS
require_value PEERGO_TRACKER_TRUSTED_PROXY_CIDRS
[[ "$(env_get PEERGO_SMTP_HOST)" != smtp.example.com ]] || fail "replace the example SMTP host"

seeding_start="$(env_get PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT)"
[[ "${seeding_start}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:00:00Z$ ]] ||
    fail "PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT must be the exact UTC Tracker cutover hour"

settlement_policy_concurrency="$(env_get PEERGO_SETTLEMENT_POLICY_CONCURRENCY)"
[[ "${settlement_policy_concurrency}" =~ ^[1-9][0-9]?$ ]] &&
    ((10#${settlement_policy_concurrency} <= 32)) ||
    fail "PEERGO_SETTLEMENT_POLICY_CONCURRENCY must be an integer between 1 and 32"

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
