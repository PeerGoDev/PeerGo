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
[[ "$(env_get PEERGO_SMTP_HOST)" != smtp.example.com ]] || fail "replace the example SMTP host"

seeding_start="$(env_get PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT)"
[[ "${seeding_start}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:00:00Z$ ]] ||
    fail "PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT must be the exact UTC Tracker cutover hour"

deployment_mode="$(env_get PEERGO_DEPLOYMENT_MODE)"
deployment_mode="${deployment_mode:-cluster}"
case "${deployment_mode}" in
    cluster) ;;
    single-server)
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
        ;;
    *) fail "PEERGO_DEPLOYMENT_MODE must be cluster or single-server" ;;
esac

printf 'PeerGo production readiness: environment checks passed\n'
