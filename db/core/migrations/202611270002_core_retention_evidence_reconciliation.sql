-- +goose Up

-- Recovery receipts are scoped to retention assessment. The ordinary Core
-- evidence and its reward/newcomer subscribers keep their original history.
CREATE TABLE workgroups.retention_reconciled_windows (
    window_start timestamptz PRIMARY KEY,
    window_end timestamptz NOT NULL,
    source_schema text NOT NULL CHECK (source_schema = 'seeding.evidence.v3'),
    source_evidence_sha256 bytea NOT NULL CHECK (octet_length(source_evidence_sha256) = 32),
    source_item_count integer NOT NULL CHECK (source_item_count >= 0),
    artifact_sha256 bytea NOT NULL CHECK (octet_length(artifact_sha256) = 32),
    operator_reference text NOT NULL CHECK (char_length(btrim(operator_reference)) BETWEEN 1 AND 256),
    reconciled_at timestamptz NOT NULL DEFAULT now(),
    CHECK (window_start = date_trunc('hour', window_start AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'),
    CHECK (window_end = window_start + interval '1 hour'),
    CHECK (reconciled_at >= window_end)
);

CREATE TABLE workgroups.retention_reconciled_items (
    window_start timestamptz NOT NULL REFERENCES workgroups.retention_reconciled_windows(window_start),
    user_id uuid NOT NULL REFERENCES identity.users(id),
    torrent_id bigint NOT NULL REFERENCES torrents.torrents(id),
    active_seconds bigint NOT NULL CHECK (active_seconds BETWEEN 0 AND 3600),
    source_evidence_sha256 bytea NOT NULL CHECK (octet_length(source_evidence_sha256) = 32),
    PRIMARY KEY (window_start, user_id, torrent_id)
);
CREATE INDEX retention_reconciled_items_user_time_idx
ON workgroups.retention_reconciled_items (user_id, window_start, torrent_id);

CREATE TRIGGER retention_reconciled_windows_immutable
BEFORE UPDATE OR DELETE OR TRUNCATE ON workgroups.retention_reconciled_windows
FOR EACH STATEMENT EXECUTE FUNCTION workgroups.reject_history_mutation();
CREATE TRIGGER retention_reconciled_items_immutable
BEFORE UPDATE OR DELETE OR TRUNCATE ON workgroups.retention_reconciled_items
FOR EACH STATEMENT EXECUTE FUNCTION workgroups.reject_history_mutation();

-- +goose StatementBegin
CREATE FUNCTION workgroups.check_retention_reconciliation_item_count() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    IF NEW.source_item_count <> (SELECT count(*) FROM workgroups.retention_reconciled_items
                                WHERE window_start = NEW.window_start) THEN
        RAISE EXCEPTION 'retention reconciliation item count does not match sealed source';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER retention_reconciled_window_requires_all_items
AFTER INSERT ON workgroups.retention_reconciled_windows
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
EXECUTE FUNCTION workgroups.check_retention_reconciliation_item_count();

-- A complete ordinary window always takes precedence. An incomplete later
-- projection never shadows a sealed recovery or supplies duplicate seconds.
CREATE VIEW workgroups.retention_evidence_windows AS
SELECT regular.window_start, regular.window_end, regular.status, false AS reconciled
FROM economy.seeding_reward_evidence_windows regular
WHERE regular.status = 'complete' OR NOT EXISTS (
    SELECT 1 FROM workgroups.retention_reconciled_windows recovered
    WHERE recovered.window_start = regular.window_start
)
UNION ALL
SELECT recovered.window_start, recovered.window_end, 'complete'::text, true
FROM workgroups.retention_reconciled_windows recovered
WHERE NOT EXISTS (
    SELECT 1 FROM economy.seeding_reward_evidence_windows regular
    WHERE regular.window_start = recovered.window_start AND regular.status = 'complete'
);

CREATE VIEW workgroups.retention_evidence_items AS
SELECT item.window_start, item.user_id, item.torrent_id, item.active_seconds
FROM economy.seeding_reward_evidence_items_read item
JOIN economy.seeding_reward_evidence_windows regular USING (window_start)
WHERE regular.status = 'complete'
UNION ALL
SELECT recovered.window_start, recovered.user_id, recovered.torrent_id, recovered.active_seconds
FROM workgroups.retention_reconciled_items recovered
WHERE NOT EXISTS (
    SELECT 1 FROM economy.seeding_reward_evidence_windows regular
    WHERE regular.window_start = recovered.window_start AND regular.status = 'complete'
);

REVOKE ALL ON workgroups.retention_reconciled_windows, workgroups.retention_reconciled_items,
    workgroups.retention_evidence_windows, workgroups.retention_evidence_items FROM PUBLIC;
REVOKE ALL ON FUNCTION workgroups.check_retention_reconciliation_item_count() FROM PUBLIC;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM workgroups.retention_reconciled_windows) THEN
        RAISE EXCEPTION 'retention assessment already relies on reconciled source evidence';
    END IF;
END;
$$;
-- +goose StatementEnd
DROP VIEW workgroups.retention_evidence_items, workgroups.retention_evidence_windows;
DROP TABLE workgroups.retention_reconciled_items, workgroups.retention_reconciled_windows;
DROP FUNCTION workgroups.check_retention_reconciliation_item_count();
