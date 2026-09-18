#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/.." && pwd -P)"
source_config="${repo_root}/deploy/observability/peergo-host-logrotate.conf"
source_cron="${repo_root}/deploy/observability/peergo-host-logrotate.cron"
target_config="/etc/logrotate.d/peergo-host"
target_cron="/etc/cron.d/peergo-host-logrotate"

[[ -f "${source_config}" ]] || {
    printf 'PeerGo log rotation: config is unavailable: %s\n' "${source_config}" >&2
    exit 1
}
[[ -f "${source_cron}" ]] || {
    printf 'PeerGo log rotation: cron config is unavailable: %s\n' "${source_cron}" >&2
    exit 1
}

# Local development and CI do not own the host's /etc.  Production runs this
# target as root; making the non-root case a no-op keeps `make production-up`
# usable for local Compose without weakening the production installation.
if [[ "${EUID}" -ne 0 ]]; then
    printf 'PeerGo log rotation: skipped (root is required to install %s)\n' "${target_config}"
    exit 0
fi

command -v logrotate >/dev/null 2>&1 || {
    printf 'PeerGo log rotation: logrotate is not installed\n' >&2
    exit 1
}

install -o root -g root -m 0644 "${source_config}" "${target_config}"
install -o root -g root -m 0644 "${source_cron}" "${target_cron}"
# Debug mode parses the complete stanza without rotating or truncating a file.
logrotate -d "${target_config}" >/dev/null 2>&1
printf 'PeerGo log rotation: installed %s and hourly check %s (200 MiB, 7 rotations)\n' \
    "${target_config}" "${target_cron}"
