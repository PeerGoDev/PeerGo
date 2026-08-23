-- +goose Up

-- Appeal responses are validated and persisted as 10-1000 characters. An
-- approved manual-download appeal also records that same response in its
-- immutable transition timeline, whose older 500-character constraint caused
-- otherwise-valid approvals to fail after the state update was attempted.
ALTER TABLE identity.manual_download_restriction_transitions
    DROP CONSTRAINT manual_download_restriction_transitions_reason_check,
    ADD CONSTRAINT manual_download_restriction_transitions_reason_check CHECK (
        char_length(btrim(reason)) BETWEEN 10 AND 1000
    );

-- +goose Down

-- Do not silently truncate immutable appeal evidence during rollback.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM identity.manual_download_restriction_transitions
        WHERE char_length(btrim(reason)) > 500
    ) THEN
        RAISE EXCEPTION
            'cannot restore 500-character transition constraint while longer appeal evidence exists';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE identity.manual_download_restriction_transitions
    DROP CONSTRAINT manual_download_restriction_transitions_reason_check,
    ADD CONSTRAINT manual_download_restriction_transitions_reason_check CHECK (
        char_length(btrim(reason)) BETWEEN 10 AND 500
    );
