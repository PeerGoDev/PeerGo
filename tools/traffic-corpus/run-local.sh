#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
log_dir="$repo_root/.local/traffic-corpus"
worker_pids=""

cleanup() {
    for worker_pid in $worker_pids; do
        kill "$worker_pid" 2>/dev/null || true
    done
    for worker_pid in $worker_pids; do
        wait "$worker_pid" 2>/dev/null || true
    done
}

finish() {
    status=$?
    trap - EXIT HUP INT TERM
    cleanup
    if [ "$status" -ne 0 ]; then
        for worker_log in "$log_dir"/*.log; do
            if [ -f "$worker_log" ]; then
                echo "==> $worker_log" >&2
                tail -n 40 "$worker_log" >&2
            fi
        done
    fi
    exit "$status"
}

start_worker() {
    target=$1
    make "$target" >"$log_dir/$target.log" 2>&1 &
    worker_pids="$worker_pids $!"
}

trap finish EXIT HUP INT TERM
cd "$repo_root"
mkdir -p "$log_dir"

make compose-up
make db-migrate
make db-seed

make dev-settlement-policy-timeline ARGS="--id 019fcd83-57de-7240-a0d3-95908cdb4201 --snapshot-file $repo_root/examples/settlement/policy-snapshot.peergo-v1-double-free.json --effective-at 2026-08-08T23:00:00Z --user-id 0198f20a-6da8-7e51-9c64-111111111111"
make dev-settlement-policy-timeline ARGS="--id 019fcd83-57de-7240-a0d3-95908cdb4202 --snapshot-file $repo_root/examples/settlement/policy-snapshot.peergo-v1-normal.json --effective-at 2026-08-09T00:15:00Z --user-id 0198f20a-6da8-7e51-9c64-111111111111"

start_worker dev-settlement
start_worker dev-settlement-policy
start_worker dev-settlement-traffic-dispatcher
start_worker dev-core-traffic-projector

PEERGO_ENV=development \
PEERGO_TRAFFIC_CORPUS_CORE_DATABASE_URL="${CORE_DATABASE_URL:-postgres://peergo_core:peergo_local_only@127.0.0.1:5432/peergo_core?sslmode=disable}" \
PEERGO_TRAFFIC_CORPUS_TRACKER_DATABASE_URL="${TRACKER_DATABASE_URL:-postgres://peergo_tracker:peergo_tracker_local_only@127.0.0.1:5434/peergo_tracker?sslmode=disable}" \
PEERGO_TRAFFIC_CORPUS_NATS_URL="${LOCAL_TRACKER_NATS_URLS:-nats://127.0.0.1:4222}" \
PEERGO_TRAFFIC_CORPUS_STREAM="${LOCAL_TRACKER_ANNOUNCE_STREAM:-PEERGO_TRACKER_ANNOUNCE_V1}" \
PEERGO_TRAFFIC_CORPUS_SUBJECT="${LOCAL_TRACKER_ANNOUNCE_SUBJECT:-peergo.tracker.announce.v1}" \
PEERGO_TRAFFIC_CORPUS_WAIT_TIMEOUT="60s" \
go -C tools/traffic-corpus run .
