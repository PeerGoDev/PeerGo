# Lossless historical seeding evidence packs

Migration `202610170001` adds one immutable pack per user/hour in Tracker and
Core. Version 1 arrays retain every original torrent item, SHA-256, byte count,
timestamp and (for v3) exact active ranges/source commitment. Native TOAST
compresses repeated values; arrays omit repeated column names and parent keys.
Core user/hour summaries retain the exact sum used by newcomer assessments and
workgroup metrics. Reward enrichment, transport and compensation read the union
of hot items and unpacked history. No rounding, reward formula or H&R rule changes.

## Rollout

1. Apply both migrations **as the corresponding database owner**. Packing starts
   disabled; migration alone retires no rows. Both schema contracts become
   `202610170001`, so rebuild all Core and Settlement startup consumers, including
   preflight. Deploy compatible API, workers and operator/replay binaries together.
2. Run `scripts/storage/evidence_pack_vacuum.sql` with `psql` as an administrator
   able to maintain both databases. It performs ordinary online vacuum (heap truncation disabled)
   and tunes the two hot item tables to vacuum/analyze after 1% plus 50,000
   changes. The default 20% threshold can leave millions of dead index entries
   after packing; oldest-hour discovery then repeatedly scans those entries and
   reaches its deadline. This physical maintenance changes no live evidence or
   archive guards. Verify recent ingest, reward projection, duplicate replay
   and historical reads after it completes.
3. Enable `ledger.seeding_evidence_packing_control.enabled` on Tracker and
   `economy.seeding_reward_evidence_packing_control.enabled` on Core. Each existing
   maintenance pass moves up to 64 user groups, with a 10 second client deadline
   and a 2 second lock timeout. Progress keeps the existing bounded catch-up
   cadence active. There is no per-item archive queue.
4. Historical catch-up may also call `*_pack_batch(128)` in separate bounded
   transactions. Use a statement timeout, pause between bursts and monitor API
   latency, I/O, WAL and available disk. Never put the complete history in one
   transaction. Turning the control off stops future automated movement.

Packing only considers closed hours older than 72 hours. Tracker also requires
all source links to have retired under their existing rules, no anomaly or
unresolved reconciliation, and no unpublished evidence outbox. Core requires a
complete projection. The original source, raw, H&R and coverage retention rules
are unchanged.

## Integrity and recovery

An insertion guard locks the parent hour and compares every packed value and
summary against its original hot group. A checksum constraint rejects damaged
payloads. Deletion is allowed only when its complete transition table exactly
matches the immutable pack; a deferred constraint rejects committed overlap.
Late inserts into a packed group, updates and truncation are rejected. A crash or
timeout rolls back the entire batch. Core keeps user/window foreign keys and one
permanent torrent reference per archived torrent, preserving deletion restrictions.

Once enabled, old binaries that read only hot tables are **not** a valid rollback.
Keep compatible readers and disable packing while repairing forward. A downgrade
or unpack must be an explicit verified migration; `goose down` fails closed.
Archive tables are durable proof and must be included in database backups.

`scripts/storage/test_evidence_packs.py` applies all real migrations to labelled,
network-isolated disposable PostgreSQL databases. It checks v1, v3, microsecond
timestamps, disjoint ranges, near-bigint-limit counters, checksums, exact roundtrip,
assessment sums, interruption rollback, partial deletion, late insertion,
immutability and reconciliation protection. It cannot target production.

## Capacity acceptance

Report hot relation bytes, pack relation bytes (including indexes/TOAST), oldest
eligible hot hour and actual filesystem free bytes separately. Deleted pages are
reusable inside PostgreSQL; they are not necessarily returned to the filesystem.
Physical rewrites require their own capacity, locking, backup and restore checks.
The existing online index maintenance allowlist is not silently widened.

This is lossless compression, not a lifetime storage cap: durable financial/audit
evidence still grows with real activity. Keep the recent raw horizon bounded and
retire terminal dependencies; investigate retained pins instead of hiding gaps or
deleting final accounting proof to meet a disk quota.
