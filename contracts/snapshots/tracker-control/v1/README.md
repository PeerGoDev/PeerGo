# Tracker control snapshot v1

This contract carries the complete torrent eligibility view from Core to a
Tracker node. It is a signed, immutable control-plane artifact; it is not an
announce request, a user entitlement token, or a promotion snapshot.

## Security boundary

- Core owns the Ed25519 private signing key.
- Tracker nodes receive one or more trusted Ed25519 public keys, identified by
  `key_id`, so keys can overlap during rotation.
- Tracker must verify the envelope, payload checksum, signature, strict payload
  shape and semantic state digest before publishing a new in-memory view.
- A node rejects sequence rollback and rejects different state at the same
  sequence. Re-signing the same state at a later `generated_at` is allowed.
- `InspectUnverified` is only suitable for a service-owned publisher deciding
  whether an existing local artifact can be atomically replaced. It must never
  authorize Tracker traffic.

`torrents` contains only swarm identity and eligibility. Freeleech, discount,
H&R, client policy and per-user authorization deliberately remain separate
versioned controls: a promotion never implies Tracker admission.

## Encoding

Both envelope and payload use UTF-8 JSON. JSON byte strings (`payload` and
`signature`) use standard padded Base64. Hex digests and info hashes are
lowercase. Unknown JSON fields and trailing JSON values are invalid.

The artifact is limited to 64 MiB, the decoded payload to 63 MiB, and the
snapshot to 1,000,000 torrent entries. Torrents are strictly ordered by
`info_hash_v1`; torrent IDs, public IDs and info hashes are unique.

The envelope signature input is:

```text
UTF8("peergo:tracker-control-snapshot-signature:v1\\0")
|| uint16_be(byte_length(key_id))
|| UTF8(key_id)
|| sha256(payload_json)
```

The semantic `state_sha256` input is:

```text
UTF8("peergo:tracker-control-snapshot-state:v1\\0")
|| uint64_be(control_sequence)
|| uint64_be(torrent_count)
|| for each torrent ordered by info_hash_v1:
     int64_as_uint64_be(torrent_id)
  || int64_as_uint64_be(total_size_bytes)
  || int64_as_uint64_be(torrent_version)
  || int64_as_uint64_be(control_sequence)
  || 16 raw UUID bytes
  || 20 raw info-hash bytes
```

All integer fields are validated as non-negative (and entry fields as
positive) before encoding. `generated_at` is intentionally excluded from the
semantic digest, but is covered by the signed payload checksum.

## Publication and loading

The initial adapter publishes one service-owned local file by writing and
syncing a temporary file in the same directory, renaming it atomically, then
syncing the directory. Tracker verifies the complete artifact and builds a new
immutable lookup map before one atomic pointer swap. Announce-side lookup must
not call Core, PostgreSQL or object storage.

Object-storage publication is a future adapter of the same signed contract.
It should publish immutable digest-addressed objects and update a small pointer
object conditionally; changing the transport must not change snapshot
semantics.
