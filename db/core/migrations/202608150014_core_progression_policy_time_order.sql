-- +goose Up

ALTER TABLE progression.experience_policy_revisions
    DROP CONSTRAINT experience_policy_revisions_check,
    ADD CONSTRAINT experience_policy_revision_time_order CHECK (
        (
            source_kind = 'legacy_opening'
            AND effective_from = '-infinity'::timestamptz
        )
        OR created_at <= effective_from
    );

-- +goose Down

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM progression.experience_policy_revisions
        WHERE created_at < effective_from
    ) THEN
        RAISE EXCEPTION '202608150014 cannot roll back after future experience policies exist';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE progression.experience_policy_revisions
    DROP CONSTRAINT experience_policy_revision_time_order,
    ADD CONSTRAINT experience_policy_revisions_check CHECK (
        created_at >= effective_from
    );
