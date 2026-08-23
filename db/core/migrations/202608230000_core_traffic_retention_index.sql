-- +goose NO TRANSACTION
-- +goose Up

-- Build outside a transaction so the ten-million-row legacy table remains
-- writable while PostgreSQL prepares the bounded cleanup access path.
CREATE INDEX CONCURRENTLY user_traffic_entries_retention_idx
    ON traffic.user_traffic_entries (applied_at, projection_sequence);

-- +goose Down

DROP INDEX CONCURRENTLY traffic.user_traffic_entries_retention_idx;
