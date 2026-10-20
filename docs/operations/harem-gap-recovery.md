# Invitation reward gap recovery

The settlement scheduler enumerates the original policy-at-start windows from the first policy. Existing settled end boundaries remain authoritative. It selects the oldest fully sourced, unsettled and non-overlapping window, rather than requiring every earlier window to finish first. This preserves the 05:00 source anchor and changes future durations only at the next canonical start.

A window is eligible only when every hourly Core evidence window is complete and no source reward job is pending, processing or dead. Calculation counts and payouts are then read again under the original serializable settlement transaction and advisory lock. Reward percentages, per-hour caps, eligibility, rounding, deterministic transaction IDs and immutable receipts are unchanged.

Unsettled holes remain discoverable after later windows are paid. If their sources are recovered, they are selected automatically before later ready work. No missing evidence is filled with zero or inferred from today's swarm. The scheduler adds no per-item task table or persistent cursor that could forget an old gap.

An old source wait is recorded as `source_coverage_pending` after a two-hour observation grace and logged separately from caught-up idle. This grace affects reporting only; it does not relax evidence eligibility or change the source closure rules. Runtime readiness and business coverage must be monitored separately.

Real PostgreSQL integration checks cover the existing six-hour cap, missing evidence, pending and dead work, later-ready progress, old-hole backfill, repeated execution, transaction rollback after ledger writes, future policy duration changes and overlap rejection. Each integration test needs its own fresh migrated database; run only in an isolated test environment through PEERGO_TEST_CORE_DATABASE_URL.

Only core-policy-worker needs a new binary. No schema or API contract changes are required. Keep independent control and snapshot publishers alive when replacing the combined worker. Verify two fresh snapshots from the replacement worker and observe Tracker readiness for a full snapshot lifetime after removing the helpers. Compare the first restored payout period with a read-only preview, verify new receipts and balanced ledger postings, and retain older missing-source periods for recovery.