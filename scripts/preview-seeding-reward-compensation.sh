#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/.." && pwd -P)"
output_dir="${PEERGO_COMPENSATION_OUTPUT_DIR:-/opt/peergo/compensations}"

fail() {
    printf 'PeerGo seeding compensation preview: %s\n' "$*" >&2
    exit 1
}

[[ "${output_dir}" = /* ]] || fail "PEERGO_COMPENSATION_OUTPUT_DIR must be absolute"
case "${output_dir}" in
    / | /opt | /opt/peergo) fail "refusing a broad output directory: ${output_dir}" ;;
esac
if [[ -e "${output_dir}" && ( ! -d "${output_dir}" || -L "${output_dir}" ) ]]; then
    fail "output directory must be a real directory: ${output_dir}"
fi

# The runtime image uses uid/gid 10001. The artifact remains mode 0600, so
# only that service account and the host root operator can read member IDs.
install -d -m 0700 -o 10001 -g 10001 "${output_dir}"
name="seeding-reward-compensation-preview-$(date -u '+%Y%m%dT%H%M%SZ').jsonl"
host_output="${output_dir}/${name}"
container_output="/compensation/${name}"

[[ ! -e "${host_output}" ]] || fail "refusing to overwrite ${host_output}"
"${repo_root}/scripts/production-compose.sh" run --rm --no-deps \
    --volume "${output_dir}:/compensation" \
    --entrypoint core-seeding-reward-compensation-preview \
    core-api \
    --output "${container_output}"

[[ -f "${host_output}" && ! -L "${host_output}" ]] || fail "preview artifact was not created"
chmod 0600 "${host_output}"
printf 'PeerGo seeding compensation preview: host artifact=%s\n' "${host_output}"
sha256sum "${host_output}"
