# Operations monitor query performance

The worker and incidents pages poll every 15 seconds. Audit queue state must
read pending rows through the pending index, use the constant-key partial
index for exact completed counts, and use the small error index for the latest
error. These counts remain exact in the overview's repeatable-read snapshot;
they do not use planner estimates or omit historical errors. The completed
count still scales with retained row count, but avoids reading event payloads.

Retention workgroup contributions first select requested memberships and closed
hours, resolve the latest historical membership transition, then read each
user/hour from hot or packed evidence. Keep both equality keys inside the
lateral view lookup: flattening the grouped view can aggregate all users before
filtering. Current membership state must not replace historical transitions.

Migration 202610170002 builds both audit indexes concurrently, outside a
transaction. Deploy rebuilt Core binaries with the matching schema contract.
An interrupted concurrent build can leave an invalid index; inspect and remove
only that invalid index before retrying the migration. Do not skip it merely
because its name exists. Keep the independent control projector and snapshot
publisher alive during any combined-worker replacement, following
[evidence packing maintenance](evidence-packing.md).

Run the query equivalence regression against a disposable PostgreSQL 18
container with network mode `none`, no published ports or data mounts, a tmpfs
at `/var/lib/postgresql`, and label
`peergo.task=operations-performance-20261017`:

```sh
python3 scripts/operations/test_monitor_queries.py --container CONTAINER --root .
```

The harness refuses production/networked/mounted containers. It exercises the
actual Go SQL and migration against isolated fixtures, compares legacy results,
and checks leases, retries, historical errors, exact totals, hot/packed evidence,
suspension/rejoin ordering, completed-hour boundaries, and empty requests. Its
reference SQL files are regression oracles, not executable production helpers.
Verify authenticated Workers, Incidents, Tracker and workgroup pages after
release; public homepage health alone does not prove admin query performance.
