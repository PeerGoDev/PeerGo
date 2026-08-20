-- +goose Up
-- Browser-visible session identifiers are deliberately independent from the
-- SHA-256 bearer-token digest. The UUID is safe to place in a URL while the
-- digest remains an internal lookup key that is never serialized or logged.
ALTER TABLE identity.sessions
    ADD COLUMN id uuid NOT NULL DEFAULT gen_random_uuid(),
    ADD CONSTRAINT sessions_id_unique UNIQUE (id);

-- +goose Down
ALTER TABLE identity.sessions
    DROP CONSTRAINT sessions_id_unique,
    DROP COLUMN id;
