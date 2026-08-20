-- +goose Up

-- Core owns a privacy-minimized copy of each closed hourly evidence window.
-- It never reads Tracker Ledger directly. A window becomes complete only after
-- every immutable transport chunk and the exact projected item count arrive;
-- the application then verifies projection_sha256 before reward enrichment.
CREATE TABLE economy.seeding_reward_evidence_windows (
    window_start timestamptz PRIMARY KEY,
    window_end timestamptz NOT NULL,
    built_at timestamptz NOT NULL,
    window_evidence_sha256 bytea NOT NULL CHECK (octet_length(window_evidence_sha256) = 32),
    projection_sha256 bytea NOT NULL CHECK (octet_length(projection_sha256) = 32),
    snapshot_id uuid NOT NULL,
    snapshot_sequence bigint NOT NULL CHECK (snapshot_sequence > 0),
    snapshot_observed_at timestamptz NOT NULL,
    item_count integer NOT NULL CHECK (item_count >= 0),
    chunk_count integer NOT NULL CHECK (chunk_count BETWEEN 1 AND 10000),
    received_chunk_count integer NOT NULL DEFAULT 0
        CHECK (received_chunk_count BETWEEN 0 AND chunk_count),
    status text NOT NULL DEFAULT 'collecting'
        CHECK (status IN ('collecting', 'complete')),
    created_at timestamptz NOT NULL,
    completed_at timestamptz,
    CHECK (window_start = date_trunc('hour', window_start)),
    CHECK (window_end = window_start + interval '1 hour'),
    CHECK (built_at >= window_end),
    CHECK (snapshot_observed_at <= window_end),
    CHECK ((status = 'complete') = (completed_at IS NOT NULL)),
    CHECK (status <> 'complete' OR received_chunk_count = chunk_count)
);

CREATE INDEX seeding_reward_evidence_ready_idx
    ON economy.seeding_reward_evidence_windows (window_start, projection_sha256)
    WHERE status = 'complete';

CREATE TABLE economy.seeding_reward_evidence_inbox (
    event_id uuid PRIMARY KEY,
    window_start timestamptz NOT NULL,
    chunk_index integer NOT NULL CHECK (chunk_index >= 0),
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    payload_json text NOT NULL CHECK (
        octet_length(payload_json) BETWEEN 2 AND 131072
        AND jsonb_typeof(payload_json::jsonb) = 'object'
    ),
    received_at timestamptz NOT NULL,
    applied_at timestamptz NOT NULL,
    CHECK (applied_at >= received_at)
);

CREATE TABLE economy.seeding_reward_evidence_chunks (
    window_start timestamptz NOT NULL
        REFERENCES economy.seeding_reward_evidence_windows (window_start) ON DELETE RESTRICT,
    chunk_index integer NOT NULL CHECK (chunk_index >= 0),
    event_id uuid NOT NULL UNIQUE
        REFERENCES economy.seeding_reward_evidence_inbox (event_id) ON DELETE RESTRICT,
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    PRIMARY KEY (window_start, chunk_index)
);

CREATE TABLE economy.seeding_reward_evidence_items (
    window_start timestamptz NOT NULL
        REFERENCES economy.seeding_reward_evidence_windows (window_start) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    torrent_id bigint NOT NULL REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    active_seconds bigint NOT NULL CHECK (active_seconds BETWEEN 0 AND 3600),
    raw_uploaded_bytes bigint NOT NULL CHECK (raw_uploaded_bytes >= 0),
    snapshot_seeders integer NOT NULL CHECK (snapshot_seeders >= 0),
    snapshot_leechers integer NOT NULL CHECK (snapshot_leechers >= 0),
    tracker_evidence_sha256 bytea NOT NULL CHECK (octet_length(tracker_evidence_sha256) = 32),
    PRIMARY KEY (window_start, user_id, torrent_id)
);

CREATE INDEX seeding_reward_evidence_items_user_idx
    ON economy.seeding_reward_evidence_items (user_id, window_start, torrent_id);

-- +goose StatementBegin
CREATE FUNCTION economy.protect_seeding_reward_evidence_window()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'Core seeding reward evidence window cannot be deleted';
    END IF;
    IF OLD.window_start IS DISTINCT FROM NEW.window_start
        OR OLD.window_end IS DISTINCT FROM NEW.window_end
        OR OLD.built_at IS DISTINCT FROM NEW.built_at
        OR OLD.window_evidence_sha256 IS DISTINCT FROM NEW.window_evidence_sha256
        OR OLD.projection_sha256 IS DISTINCT FROM NEW.projection_sha256
        OR OLD.snapshot_id IS DISTINCT FROM NEW.snapshot_id
        OR OLD.snapshot_sequence IS DISTINCT FROM NEW.snapshot_sequence
        OR OLD.snapshot_observed_at IS DISTINCT FROM NEW.snapshot_observed_at
        OR OLD.item_count IS DISTINCT FROM NEW.item_count
        OR OLD.chunk_count IS DISTINCT FROM NEW.chunk_count
        OR OLD.created_at IS DISTINCT FROM NEW.created_at
        OR NEW.received_chunk_count < OLD.received_chunk_count THEN
        RAISE EXCEPTION 'Core seeding reward evidence header is immutable';
    END IF;
    IF OLD.status = 'complete' AND OLD IS DISTINCT FROM NEW THEN
        RAISE EXCEPTION 'complete Core seeding reward evidence is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER seeding_reward_evidence_window_protected
BEFORE UPDATE OR DELETE ON economy.seeding_reward_evidence_windows
FOR EACH ROW EXECUTE FUNCTION economy.protect_seeding_reward_evidence_window();

-- +goose StatementBegin
CREATE FUNCTION economy.reject_seeding_reward_evidence_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'Core seeding reward evidence is immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER seeding_reward_evidence_inbox_immutable
BEFORE UPDATE OR DELETE ON economy.seeding_reward_evidence_inbox
FOR EACH ROW EXECUTE FUNCTION economy.reject_seeding_reward_evidence_mutation();
CREATE TRIGGER seeding_reward_evidence_chunks_immutable
BEFORE UPDATE OR DELETE ON economy.seeding_reward_evidence_chunks
FOR EACH ROW EXECUTE FUNCTION economy.reject_seeding_reward_evidence_mutation();
CREATE TRIGGER seeding_reward_evidence_items_immutable
BEFORE UPDATE OR DELETE ON economy.seeding_reward_evidence_items
FOR EACH ROW EXECUTE FUNCTION economy.reject_seeding_reward_evidence_mutation();

REVOKE ALL ON economy.seeding_reward_evidence_windows FROM PUBLIC;
REVOKE ALL ON economy.seeding_reward_evidence_inbox FROM PUBLIC;
REVOKE ALL ON economy.seeding_reward_evidence_chunks FROM PUBLIC;
REVOKE ALL ON economy.seeding_reward_evidence_items FROM PUBLIC;

-- +goose Down

DROP TRIGGER seeding_reward_evidence_items_immutable
    ON economy.seeding_reward_evidence_items;
DROP TRIGGER seeding_reward_evidence_chunks_immutable
    ON economy.seeding_reward_evidence_chunks;
DROP TRIGGER seeding_reward_evidence_inbox_immutable
    ON economy.seeding_reward_evidence_inbox;
DROP FUNCTION economy.reject_seeding_reward_evidence_mutation();
DROP TRIGGER seeding_reward_evidence_window_protected
    ON economy.seeding_reward_evidence_windows;
DROP FUNCTION economy.protect_seeding_reward_evidence_window();
DROP INDEX economy.seeding_reward_evidence_items_user_idx;
DROP TABLE economy.seeding_reward_evidence_items;
DROP TABLE economy.seeding_reward_evidence_chunks;
DROP TABLE economy.seeding_reward_evidence_inbox;
DROP INDEX economy.seeding_reward_evidence_ready_idx;
DROP TABLE economy.seeding_reward_evidence_windows;
