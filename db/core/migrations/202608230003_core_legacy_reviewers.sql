-- +goose Up

-- PtYes stored review membership in its reviewers table independently from
-- medals. Let that authoritative source open a review membership while the
-- existing medal importer keeps its original evidence shape.
ALTER TABLE migration.workgroup_membership_openings
    DROP CONSTRAINT workgroup_membership_openings_legacy_user_medal_ids_check,
    DROP CONSTRAINT workgroup_membership_openings_check,
    ADD COLUMN source_kind text NOT NULL DEFAULT 'medal'
        CHECK (source_kind IN ('medal', 'reviewer')),
    ADD CONSTRAINT workgroup_membership_openings_source_payload_check CHECK (
        (
            source_kind = 'medal'
            AND cardinality(legacy_user_medal_ids) > 0
            AND cardinality(legacy_medal_ids) = cardinality(legacy_user_medal_ids)
            AND array_position(legacy_user_medal_ids, NULL) IS NULL
            AND array_position(legacy_medal_ids, NULL) IS NULL
        ) OR (
            source_kind = 'reviewer'
            AND group_kind = 'review'
            AND cardinality(legacy_user_medal_ids) = 0
            AND cardinality(legacy_medal_ids) = 0
        )
    );

ALTER TABLE workgroups.membership_transitions
    DROP CONSTRAINT membership_transitions_source_authority_check,
    ADD CONSTRAINT membership_transitions_source_authority_check CHECK (
        (
            source = 'legacy_migration'
            AND actor_id IS NULL
            AND authorization_decision_id IS NULL
            AND source_application_id IS NULL
            AND (
                (
                    transition = 'joined'
                    AND from_status IS NULL
                    AND to_status = 'active'
                    AND state_version = 1
                ) OR (
                    transition = 'suspended'
                    AND from_status = 'active'
                    AND to_status = 'suspended'
                    AND state_version = 2
                ) OR (
                    transition = 'ended'
                    AND from_status = 'active'
                    AND to_status = 'ended'
                    AND state_version = 2
                )
            )
        ) OR (
            source IN ('application', 'staff')
            AND actor_id IS NOT NULL
            AND authorization_decision_id IS NOT NULL
        )
    );

CREATE TABLE migration.legacy_reviewer_openings (
    source_system text NOT NULL CHECK (source_system = 'ptyes'),
    legacy_reviewer_id bigint NOT NULL CHECK (legacy_reviewer_id > 0),
    legacy_user_id bigint NOT NULL CHECK (legacy_user_id > 0),
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    membership_id uuid NOT NULL UNIQUE
        REFERENCES migration.workgroup_membership_openings (membership_id)
        ON DELETE RESTRICT,
    joined_transition_id uuid NOT NULL UNIQUE
        REFERENCES workgroups.membership_transitions (id) ON DELETE RESTRICT,
    status_transition_id uuid UNIQUE
        REFERENCES workgroups.membership_transitions (id) ON DELETE RESTRICT,
    source_status text NOT NULL
        CHECK (source_status IN ('active', 'suspended', 'removed')),
    source_activity_status text NOT NULL
        CHECK (source_activity_status IN ('active', 'inactive', 'dormant')),
    source_total_reviews bigint NOT NULL CHECK (source_total_reviews >= 0),
    source_accurate_count bigint NOT NULL CHECK (
        source_accurate_count >= 0
        AND source_accurate_count <= source_total_reviews
    ),
    source_joined_at timestamptz NOT NULL,
    source_removed_at timestamptz,
    source_remove_reason text,
    source_last_activity_at timestamptz,
    source_created_at timestamptz NOT NULL,
    source_updated_at timestamptz NOT NULL,
    source_fingerprint bytea NOT NULL CHECK (octet_length(source_fingerprint) = 32),
    first_run_id uuid NOT NULL REFERENCES migration.runs (id) ON DELETE RESTRICT,
    imported_at timestamptz NOT NULL,
    PRIMARY KEY (source_system, legacy_reviewer_id),
    UNIQUE (source_system, legacy_user_id),
    FOREIGN KEY (source_system, legacy_user_id, user_id)
        REFERENCES migration.user_id_map (source_system, legacy_user_id, user_id)
        ON DELETE RESTRICT,
    CHECK (source_updated_at >= source_created_at),
    CHECK (
        (source_status = 'active' AND status_transition_id IS NULL)
        OR (source_status IN ('suspended', 'removed') AND status_transition_id IS NOT NULL)
    )
);

CREATE INDEX legacy_reviewer_openings_status_idx
    ON migration.legacy_reviewer_openings (
        source_status, source_activity_status, legacy_reviewer_id
    );

CREATE TRIGGER migration_legacy_reviewer_openings_immutable
BEFORE UPDATE OR DELETE ON migration.legacy_reviewer_openings
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

REVOKE ALL ON migration.legacy_reviewer_openings FROM PUBLIC;

-- +goose Down

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM migration.legacy_reviewer_openings) THEN
        RAISE EXCEPTION 'cannot roll back legacy reviewer schema after import';
    END IF;
END;
$$;

DROP TRIGGER migration_legacy_reviewer_openings_immutable
    ON migration.legacy_reviewer_openings;
DROP TABLE migration.legacy_reviewer_openings;

ALTER TABLE workgroups.membership_transitions
    DROP CONSTRAINT membership_transitions_source_authority_check,
    ADD CONSTRAINT membership_transitions_source_authority_check CHECK (
        (
            source = 'legacy_migration'
            AND actor_id IS NULL
            AND authorization_decision_id IS NULL
            AND source_application_id IS NULL
            AND transition = 'joined'
            AND from_status IS NULL
            AND to_status = 'active'
            AND state_version = 1
        ) OR (
            source IN ('application', 'staff')
            AND actor_id IS NOT NULL
            AND authorization_decision_id IS NOT NULL
        )
    );

ALTER TABLE migration.workgroup_membership_openings
    DROP CONSTRAINT workgroup_membership_openings_source_payload_check,
    DROP COLUMN source_kind,
    ADD CONSTRAINT workgroup_membership_openings_legacy_user_medal_ids_check CHECK (
        cardinality(legacy_user_medal_ids) > 0
        AND array_position(legacy_user_medal_ids, NULL) IS NULL
    ),
    ADD CONSTRAINT workgroup_membership_openings_check CHECK (
        cardinality(legacy_medal_ids) = cardinality(legacy_user_medal_ids)
        AND array_position(legacy_medal_ids, NULL) IS NULL
    );
