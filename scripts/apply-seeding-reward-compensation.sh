#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/.." && pwd -P)"
output_dir="${PEERGO_COMPENSATION_OUTPUT_DIR:-/opt/peergo/compensations}"
artifact="${PEERGO_COMPENSATION_ARTIFACT:-}"
approved_sha256="${PEERGO_COMPENSATION_APPROVE_SHA256:-}"
operator_reference="${PEERGO_COMPENSATION_OPERATOR_REFERENCE:-}"
confirmation="${CONFIRM_PEERGO_SEEDING_REWARD_COMPENSATION:-}"
batch_size="${PEERGO_COMPENSATION_BATCH_SIZE:-50}"

fail() {
    printf 'PeerGo seeding compensation apply: %s\n' "$*" >&2
    exit 1
}

[[ "${output_dir}" = /* ]] || fail "PEERGO_COMPENSATION_OUTPUT_DIR must be absolute"
case "${output_dir}" in
    / | /opt | /opt/peergo) fail "refusing a broad compensation directory: ${output_dir}" ;;
esac
[[ -d "${output_dir}" && ! -L "${output_dir}" ]] || fail "compensation directory must be a real directory"
[[ "${artifact}" = /* && -f "${artifact}" && ! -L "${artifact}" ]] || fail "PEERGO_COMPENSATION_ARTIFACT must be an absolute regular file"

canonical_dir="$(realpath "${output_dir}")"
canonical_artifact="$(realpath "${artifact}")"
case "${canonical_artifact}" in
    "${canonical_dir}"/*) ;;
    *) fail "artifact must be inside ${canonical_dir}" ;;
esac
[[ "$(stat -c '%a' "${canonical_artifact}")" = "600" ]] || fail "artifact permissions must be exactly 0600"
[[ "${approved_sha256}" =~ ^[0-9a-f]{64}$ && "${approved_sha256}" != "$(printf '0%.0s' {1..64})" ]] || fail "approved SHA-256 is invalid"
[[ "${confirmation}" = "APPLY:${approved_sha256}" ]] || fail "explicit confirmation must equal APPLY:<approved SHA-256>"
[[ "${operator_reference}" =~ ^[a-z0-9][a-z0-9:._-]{0,127}$ ]] || fail "operator reference is invalid"
[[ "${batch_size}" =~ ^[0-9]+$ && "${batch_size}" -ge 1 && "${batch_size}" -le 250 ]] || fail "batch size must be between 1 and 250"

actual_sha256="$(sha256sum "${canonical_artifact}" | awk '{print $1}')"
[[ "${actual_sha256}" = "${approved_sha256}" ]] || fail "artifact SHA-256 differs from explicit approval"

# The apply binary refuses a stale schema. Run only the idempotent Core
# migration job first; long-running services are not recreated by this script.
"${repo_root}/scripts/production-compose.sh" run --rm --no-deps core-migrate

container_artifact="/compensation/$(basename "${canonical_artifact}")"
"${repo_root}/scripts/production-compose.sh" run --rm --no-deps \
    --volume "${canonical_dir}:/compensation:ro" \
    --entrypoint core-seeding-reward-compensation-apply \
    core-api \
    --artifact "${container_artifact}" \
    --approve-sha256 "${approved_sha256}" \
    --operator-reference "${operator_reference}" \
    --confirm "${confirmation}" \
    --batch-size "${batch_size}"
