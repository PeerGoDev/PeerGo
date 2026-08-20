-- +goose Up

-- Tracker eligibility events are immutable. Keep the original v1 contract
-- admissible for historical rows while allowing every new producer to append
-- the current v2 envelope. The application contract accepts and emits v2;
-- projected v1 rows remain retained as migration/audit history.
ALTER TABLE tracker_control.outbox
    DROP CONSTRAINT outbox_schema_version_check;

ALTER TABLE tracker_control.outbox
    ADD CONSTRAINT outbox_schema_version_check
        CHECK (schema_version IN ('1.0.0', '2.0.0'));

-- +goose Down

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM tracker_control.outbox
        WHERE schema_version = '2.0.0'
    ) THEN
        RAISE EXCEPTION '202608180016 cannot roll back after a v2 Tracker control event was appended';
    END IF;
END;
$$;

ALTER TABLE tracker_control.outbox
    DROP CONSTRAINT outbox_schema_version_check;

ALTER TABLE tracker_control.outbox
    ADD CONSTRAINT outbox_schema_version_check
        CHECK (schema_version = '1.0.0');
