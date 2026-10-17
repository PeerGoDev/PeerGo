-- +goose NO TRANSACTION
-- +goose Up

-- Constant keys deduplicate the exact delivered-row count into a small index.
-- Neither monitoring query needs to scan retained audit payloads. Concurrent
-- builds leave business audit writes available throughout the rollout.
CREATE INDEX CONCURRENTLY audit_outbox_completed_count_idx
    ON audit.outbox ((1)) WHERE delivered_at IS NOT NULL;

CREATE INDEX CONCURRENTLY audit_outbox_last_error_idx
    ON audit.outbox (available_at DESC) WHERE last_error IS NOT NULL;

-- +goose Down
DROP INDEX CONCURRENTLY audit.audit_outbox_last_error_idx;
DROP INDEX CONCURRENTLY audit.audit_outbox_completed_count_idx;
