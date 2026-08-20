# Tracker control events v1

Core commits each event in `tracker_control.outbox` in the same PostgreSQL
transaction as the owning torrent transition. The database assigns a global
`sequence`; the sequence is delivery metadata and therefore is not embedded in
the immutable JSON payload.

The Core projector applies only the earliest unprojected event, updates the
allowlist projection idempotently by torrent version, and advances a contiguous
watermark. Tracker Edge does not query this table: a later snapshot/incremental
consumer will verify a signed artifact and atomically replace local entries.
