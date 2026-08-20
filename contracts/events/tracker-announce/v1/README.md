# Tracker announce event v1

This event is appended to durable Tracker WAL before a successful HTTP
announce response. Counters are client-reported non-negative absolute values;
Settlement, not Tracker Edge, derives trusted deltas and applies promotions,
VIP, Seedbox, H&R and anti-cheat policy.

The event binds the request to the exact immutable torrent and subject snapshot
sequences used for admission. `session_token` is a one-way SHA-256 token over
the info hash, stable user ID, peer ID and client key. It is stable across IPv4
and IPv6 so dual-stack endpoints share one accounting identity.

The ordinary event never contains a passkey, full request URL, IP address,
port, username, email or raw peer ID. `address_family` records only `4` or `6`.
Endpoint evidence and rotating network HMACs belong to a separate future,
access-controlled security event.

The initial local WAL frames canonical JSON as:

```text
ASCII("PGW1") || uint32_be(payload_length) || payload_json || sha256(payload_json)
```

The correctness-first adapter fsyncs every record. The current publisher sends
the unchanged canonical bytes to one literal JetStream subject, sets
`Nats-Msg-Id` to `event_id`, requires the configured stream, and advances the
durable local checkpoint only after a synchronous storage acknowledgement.

The checkpoint binds the exact WAL boundary to the acknowledged event ID and
payload digest. Startup rejects a checkpoint that is not a valid record
boundary. Fully acknowledged files are reclaimed only after the checkpoint is
durably reset to zero, so a crash may replay an acknowledged event but cannot
skip an unacknowledged one. Partial-prefix compaction and group commit are not
part of this adapter. JetStream deduplication is bounded by the stream duplicate
window; consumers must still use a durable unique inbox for exactly-once
business effects.
