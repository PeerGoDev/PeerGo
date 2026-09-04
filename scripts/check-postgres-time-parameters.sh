#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repository_root}"

pattern='\$[0-9]+\s*[+-]\s*(?:interval\b|\([^)]*\binterval\b)'
set +e
matches="$(rg --pcre2 --line-number --glob '*.go' --glob '*.sql' \
    "${pattern}" services db libraries tools)"
status=$?
set -e

if [[ ${status} -eq 0 ]]; then
    printf '%s\n' 'Untyped PostgreSQL time parameter arithmetic is forbidden.' >&2
    printf '%s\n' 'Cast the timestamp operand explicitly, for example $1::timestamptz - interval ...' >&2
    printf '%s\n' "${matches}" >&2
    exit 1
fi

if [[ ${status} -ne 1 ]]; then
    exit "${status}"
fi
