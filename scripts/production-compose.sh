#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/.." && pwd -P)"
env_file="${PEERGO_PRODUCTION_ENV_FILE:-${repo_root}/.env.production}"
include_cutover=false

if [[ "${1:-}" == "--cutover" ]]; then
    include_cutover=true
    shift
fi

[[ -f "${env_file}" && ! -L "${env_file}" ]] || {
    printf 'PeerGo production compose: environment file is unavailable or is a symlink: %s\n' "${env_file}" >&2
    exit 1
}

read_env_value() {
    local name="$1"
    awk -v wanted="${name}" '
        $0 ~ "^[[:space:]]*" wanted "[[:space:]]*=" {
            line = $0
            sub("^[[:space:]]*" wanted "[[:space:]]*=[[:space:]]*", "", line)
            sub("[[:space:]]*$", "", line)
            if ((substr(line, 1, 1) == "\"" && substr(line, length(line), 1) == "\"") ||
                (substr(line, 1, 1) == "\047" && substr(line, length(line), 1) == "\047")) {
                line = substr(line, 2, length(line) - 2)
            }
            print line
            exit
        }
    ' "${env_file}"
}

deployment_mode="$(read_env_value PEERGO_DEPLOYMENT_MODE)"
deployment_mode="${deployment_mode:-cluster}"
runtime_layout="$(read_env_value PEERGO_RUNTIME_LAYOUT)"
runtime_layout="${runtime_layout:-compact}"
compose_files=(-f "${repo_root}/deploy/compose/compose.production.yaml")

case "${deployment_mode}" in
    cluster) ;;
    single-server)
        compose_files+=(-f "${repo_root}/deploy/compose/compose.single-server.yaml")
        ;;
    *)
        printf 'PeerGo production compose: invalid PEERGO_DEPLOYMENT_MODE=%s\n' "${deployment_mode}" >&2
        exit 1
        ;;
esac

case "${runtime_layout}" in
    compact)
        runtime_profile=compact-runtime
        ;;
    split)
        runtime_profile=split-runtime
        ;;
    *)
        printf 'PeerGo production compose: invalid PEERGO_RUNTIME_LAYOUT=%s\n' "${runtime_layout}" >&2
        exit 1
        ;;
esac

if [[ "${include_cutover}" == true ]]; then
    compose_files+=(-f "${repo_root}/deploy/compose/compose.cutover.yaml")
fi

export PEERGO_ENV_FILE="${env_file}"
exec docker compose --env-file "${env_file}" "${compose_files[@]}" --profile "${runtime_profile}" "$@"
