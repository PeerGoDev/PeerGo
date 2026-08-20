# Tracker subject control snapshot v1

This contract carries the complete current user admission allowlist from Core
to a Tracker node. It is independent from torrent eligibility and Settlement
policy. The payload contains no plaintext passkey, username, email, profile
field, restriction reason, promotion, ratio, H&R or accounting multiplier.

## Admission semantics

Core includes a subject only when all of the following are true at the signed
`generated_at` instant:

- a Core `identity.tracker_passkey_hmac` projection exists;
- the account status is `active`;
- no non-revoked `account_access` restriction is active.

`lookup_hmac` is the lowercase hex encoding of the 32-byte, domain-separated
HMAC created by Privacy Vault. Tracker receives the same lookup key through its
secret manager, validates the 32-character lowercase-hex route passkey, derives
the HMAC in memory, and performs a map lookup. Neither the passkey nor the key
is stored in this artifact.

Each build reserves a fresh positive `control_sequence` and reads the complete
allowlist in the same repeatable-read PostgreSQL transaction. A new sequence is
reserved even when rows did not change because a time-bounded restriction can
expire without a database mutation. Gaps caused by a failed publisher are
valid; rollback and different state at the same sequence are not.

The first adapter depends on frequent full rebuilds plus Tracker freshness
readiness. A high-priority incremental subject revocation channel remains a
later hardening step for passkey reset and urgent disablement.

## Encoding and authentication

The envelope uses the shared `signedsnapshotv1` framing and Ed25519 key-rotation
rules. Its signature input is independently domain separated:

```text
UTF8("peergo:tracker-subject-control-snapshot-signature:v1\\0")
|| uint16_be(byte_length(key_id))
|| UTF8(key_id)
|| sha256(payload_json)
```

Subjects are strictly ordered by raw `lookup_hmac`; user IDs and lookup HMACs
are unique. The semantic `state_sha256` input is:

```text
UTF8("peergo:tracker-subject-control-snapshot-state:v1\\0")
|| uint64_be(control_sequence)
|| uint64_be(subject_count)
|| for each subject ordered by lookup_hmac:
     16 raw user UUID bytes
  || 32 raw lookup HMAC bytes
  || int64_as_uint64_be(credential_version)
```

The artifact and payload limits are 64 MiB and 63 MiB; the payload allows at
most 1,000,000 entries. Unknown fields, non-canonical JSON, unknown keys,
checksum mismatch, signature mismatch and semantic digest mismatch fail closed.
