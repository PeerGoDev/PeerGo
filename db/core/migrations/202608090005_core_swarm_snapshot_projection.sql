-- +goose Up

-- Active peer counts and lifetime completions have different durability
-- semantics. Complete snapshots replace the former; completion identities
-- increment the latter exactly once even when a client retries its announce.
CREATE TABLE catalog.torrent_completion_stats (
    torrent_id text PRIMARY KEY REFERENCES catalog.torrents (id) ON DELETE CASCADE,
    completed integer NOT NULL CHECK (completed >= 0),
    observed_at timestamptz NOT NULL
);

INSERT INTO catalog.torrent_completion_stats (torrent_id, completed, observed_at)
SELECT torrent_id, completed, observed_at
FROM catalog.torrent_swarm_stats
ON CONFLICT (torrent_id) DO NOTHING;

CREATE TABLE catalog.swarm_snapshot_projection_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    source_id text NOT NULL DEFAULT '',
    routing_epoch bigint NOT NULL DEFAULT 0 CHECK (routing_epoch >= 0),
    snapshot_sequence bigint NOT NULL DEFAULT 0 CHECK (snapshot_sequence >= 0),
    snapshot_id uuid,
    observed_at timestamptz,
    applied_at timestamptz,
    CHECK ((snapshot_sequence = 0) = (snapshot_id IS NULL)),
    CHECK ((snapshot_sequence = 0) = (observed_at IS NULL)),
    CHECK ((snapshot_sequence = 0) = (applied_at IS NULL))
);

INSERT INTO catalog.swarm_snapshot_projection_state (singleton) VALUES (true);

CREATE TABLE catalog.swarm_snapshot_runs (
    snapshot_id uuid PRIMARY KEY,
    source_id text NOT NULL CHECK (source_id ~ '^[a-z][a-z0-9-]{0,62}$'),
    routing_epoch bigint NOT NULL CHECK (routing_epoch >= 1),
    snapshot_sequence bigint NOT NULL CHECK (snapshot_sequence >= 1),
    observed_at timestamptz NOT NULL,
    chunk_count integer NOT NULL CHECK (chunk_count BETWEEN 1 AND 10000),
    received_chunk_count integer NOT NULL DEFAULT 0 CHECK (received_chunk_count BETWEEN 0 AND chunk_count),
    status text NOT NULL DEFAULT 'collecting' CHECK (status IN ('collecting', 'applied', 'superseded')),
    created_at timestamptz NOT NULL,
    applied_at timestamptz,
    UNIQUE (source_id, routing_epoch, snapshot_sequence),
    CHECK ((status = 'applied') = (applied_at IS NOT NULL))
);

CREATE TABLE catalog.swarm_snapshot_inbox (
    event_id uuid PRIMARY KEY,
    snapshot_id uuid NOT NULL,
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    received_at timestamptz NOT NULL
);

CREATE TABLE catalog.swarm_snapshot_chunks (
    snapshot_id uuid NOT NULL REFERENCES catalog.swarm_snapshot_runs (snapshot_id) ON DELETE CASCADE,
    chunk_index integer NOT NULL CHECK (chunk_index >= 0),
    event_id uuid NOT NULL UNIQUE REFERENCES catalog.swarm_snapshot_inbox (event_id) ON DELETE RESTRICT,
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    PRIMARY KEY (snapshot_id, chunk_index)
);

CREATE TABLE catalog.swarm_snapshot_entries (
    snapshot_id uuid NOT NULL REFERENCES catalog.swarm_snapshot_runs (snapshot_id) ON DELETE CASCADE,
    info_hash_v1 bytea NOT NULL CHECK (octet_length(info_hash_v1) = 20),
    seeders integer NOT NULL CHECK (seeders >= 0),
    leechers integer NOT NULL CHECK (leechers >= 0),
    PRIMARY KEY (snapshot_id, info_hash_v1)
);

CREATE TABLE catalog.swarm_completion_inbox (
    event_id uuid PRIMARY KEY,
    completion_id bytea NOT NULL UNIQUE CHECK (octet_length(completion_id) = 32),
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    torrent_id bigint NOT NULL REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    occurred_at timestamptz NOT NULL,
    applied_at timestamptz NOT NULL
);

-- +goose StatementBegin
CREATE FUNCTION catalog.reject_swarm_inbox_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'swarm projection inbox evidence is immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER swarm_snapshot_inbox_immutable
BEFORE UPDATE OR DELETE ON catalog.swarm_snapshot_inbox
FOR EACH ROW EXECUTE FUNCTION catalog.reject_swarm_inbox_mutation();

CREATE TRIGGER swarm_completion_inbox_immutable
BEFORE UPDATE OR DELETE ON catalog.swarm_completion_inbox
FOR EACH ROW EXECUTE FUNCTION catalog.reject_swarm_inbox_mutation();

-- +goose Down

DROP TRIGGER IF EXISTS swarm_completion_inbox_immutable ON catalog.swarm_completion_inbox;
DROP TRIGGER IF EXISTS swarm_snapshot_inbox_immutable ON catalog.swarm_snapshot_inbox;
DROP FUNCTION IF EXISTS catalog.reject_swarm_inbox_mutation();
DROP TABLE IF EXISTS catalog.swarm_completion_inbox;
DROP TABLE IF EXISTS catalog.swarm_snapshot_entries;
DROP TABLE IF EXISTS catalog.swarm_snapshot_chunks;
DROP TABLE IF EXISTS catalog.swarm_snapshot_inbox;
DROP TABLE IF EXISTS catalog.swarm_snapshot_runs;
DROP TABLE IF EXISTS catalog.swarm_snapshot_projection_state;
DROP TABLE IF EXISTS catalog.torrent_completion_stats;
