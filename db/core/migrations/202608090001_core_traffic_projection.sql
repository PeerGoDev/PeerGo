-- +goose Up

CREATE SCHEMA IF NOT EXISTS traffic;

-- Core receives only the privacy-minimized final result event. The immutable
-- Tracker Ledger remains the explanation source for policy applications and
-- raw session evidence; Core owns user-facing read models and never joins to
-- Tracker's database from request handlers.
CREATE TABLE traffic.settlement_inbox (
    event_id uuid PRIMARY KEY,
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    payload_json text NOT NULL CHECK (
        octet_length(payload_json) BETWEEN 2 AND 8192
        AND jsonb_typeof(payload_json::jsonb) = 'object'
    ),
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL,
    applied_at timestamptz NOT NULL,
    CHECK (applied_at >= received_at)
);

CREATE TABLE traffic.user_traffic_entries (
    settlement_id uuid PRIMARY KEY
        REFERENCES traffic.settlement_inbox (event_id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    torrent_id bigint NOT NULL REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    interval_starts_at timestamptz NOT NULL,
    interval_ends_at timestamptz NOT NULL,
    raw_uploaded bigint NOT NULL CHECK (raw_uploaded >= 0),
    raw_downloaded bigint NOT NULL CHECK (raw_downloaded >= 0),
    credited_uploaded bigint NOT NULL CHECK (credited_uploaded >= 0),
    charged_downloaded bigint NOT NULL CHECK (charged_downloaded >= 0),
    settlement_sha256 bytea NOT NULL CHECK (octet_length(settlement_sha256) = 32),
    occurred_at timestamptz NOT NULL,
    applied_at timestamptz NOT NULL,
    CHECK (interval_ends_at > interval_starts_at)
);

CREATE INDEX user_traffic_entries_user_time_idx
    ON traffic.user_traffic_entries (user_id, interval_ends_at DESC, settlement_id DESC);

CREATE TABLE traffic.user_totals (
    user_id uuid PRIMARY KEY REFERENCES identity.users (id) ON DELETE RESTRICT,
    raw_uploaded bigint NOT NULL DEFAULT 0 CHECK (raw_uploaded >= 0),
    raw_downloaded bigint NOT NULL DEFAULT 0 CHECK (raw_downloaded >= 0),
    credited_uploaded bigint NOT NULL DEFAULT 0 CHECK (credited_uploaded >= 0),
    charged_downloaded bigint NOT NULL DEFAULT 0 CHECK (charged_downloaded >= 0),
    entry_count bigint NOT NULL DEFAULT 0 CHECK (entry_count >= 0),
    version bigint NOT NULL DEFAULT 0 CHECK (version >= 0),
    last_occurred_at timestamptz,
    updated_at timestamptz NOT NULL
);

CREATE TABLE traffic.user_torrent_totals (
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    torrent_id bigint NOT NULL REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    raw_uploaded bigint NOT NULL DEFAULT 0 CHECK (raw_uploaded >= 0),
    raw_downloaded bigint NOT NULL DEFAULT 0 CHECK (raw_downloaded >= 0),
    credited_uploaded bigint NOT NULL DEFAULT 0 CHECK (credited_uploaded >= 0),
    charged_downloaded bigint NOT NULL DEFAULT 0 CHECK (charged_downloaded >= 0),
    entry_count bigint NOT NULL DEFAULT 0 CHECK (entry_count >= 0),
    version bigint NOT NULL DEFAULT 0 CHECK (version >= 0),
    last_occurred_at timestamptz,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, torrent_id)
);

-- +goose StatementBegin
CREATE FUNCTION traffic.reject_projection_evidence_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'Core traffic projection evidence is immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER traffic_settlement_inbox_immutable
BEFORE UPDATE OR DELETE ON traffic.settlement_inbox
FOR EACH ROW EXECUTE FUNCTION traffic.reject_projection_evidence_mutation();

CREATE TRIGGER traffic_user_entries_immutable
BEFORE UPDATE OR DELETE ON traffic.user_traffic_entries
FOR EACH ROW EXECUTE FUNCTION traffic.reject_projection_evidence_mutation();

-- +goose Down

DROP TRIGGER IF EXISTS traffic_user_entries_immutable ON traffic.user_traffic_entries;
DROP TRIGGER IF EXISTS traffic_settlement_inbox_immutable ON traffic.settlement_inbox;
DROP FUNCTION IF EXISTS traffic.reject_projection_evidence_mutation();
DROP SCHEMA IF EXISTS traffic CASCADE;
