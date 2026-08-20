-- +goose Up

-- CURRENT_TIMESTAMP is fixed at transaction start. A satisfied projection can
-- carry an event timestamp produced later in that same transaction, so use the
-- wall clock here; otherwise the download gate clears immediately but the
-- member notification waits for the next policy-worker tick.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION community.project_hnr_notifications_from_projection()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM community.project_hnr_notifications_for_obligation(
        NEW.obligation_id,
        clock_timestamp()
    );
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION community.project_hnr_notifications_from_projection()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM community.project_hnr_notifications_for_obligation(
        NEW.obligation_id,
        CURRENT_TIMESTAMP
    );
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
