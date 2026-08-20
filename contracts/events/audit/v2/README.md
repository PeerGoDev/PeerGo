# Audit event contracts v2

`comment-moderation-decision-recorded.schema.json` preserves the v1 decision,
authorization, pseudonym and state-hash evidence while replacing its
torrent-only identity with an explicit typed target. Exactly one of
`torrent_public_id` or `announcement_id` is present and must match
`target_kind`; reporter identity and human-entered report/decision text remain
inside Core.
