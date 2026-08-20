-- +goose Up

-- Rousi represented operational workgroup membership with special medals.
-- PeerGo keeps the medal for display and historical benefits, but also needs a
-- typed membership timeline so trusted publishing, review voting and retention
-- download accounting are enforced by their owning domains.  A migration
-- opening has no staff actor or authorization decision; its immutable receipt
-- points back to the accepted cutover run instead.
ALTER TABLE workgroups.memberships
    DROP CONSTRAINT memberships_source_check,
    ADD CONSTRAINT memberships_source_check CHECK (
        source IN ('application', 'staff', 'legacy_migration')
    );

ALTER TABLE workgroups.membership_transitions
    DROP CONSTRAINT membership_transitions_source_check,
    ALTER COLUMN actor_id DROP NOT NULL,
    ALTER COLUMN authorization_decision_id DROP NOT NULL,
    ADD CONSTRAINT membership_transitions_source_check CHECK (
        source IN ('application', 'staff', 'legacy_migration')
    ),
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

CREATE TABLE migration.workgroup_membership_openings (
    source_system text NOT NULL CHECK (source_system = 'ptyes'),
    legacy_user_id bigint NOT NULL CHECK (legacy_user_id > 0),
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    group_kind text NOT NULL
        REFERENCES workgroups.definitions (kind) ON DELETE RESTRICT,
    membership_id uuid NOT NULL
        REFERENCES workgroups.memberships (id) ON DELETE RESTRICT,
    transition_id uuid NOT NULL
        REFERENCES workgroups.membership_transitions (id) ON DELETE RESTRICT,
    legacy_user_medal_ids bigint[] NOT NULL CHECK (
        cardinality(legacy_user_medal_ids) > 0
        AND array_position(legacy_user_medal_ids, NULL) IS NULL
    ),
    legacy_medal_ids bigint[] NOT NULL CHECK (
        cardinality(legacy_medal_ids) = cardinality(legacy_user_medal_ids)
        AND array_position(legacy_medal_ids, NULL) IS NULL
    ),
    started_at timestamptz NOT NULL,
    source_fingerprint bytea NOT NULL CHECK (octet_length(source_fingerprint) = 32),
    first_run_id uuid NOT NULL
        REFERENCES migration.runs (id) ON DELETE RESTRICT,
    imported_at timestamptz NOT NULL,
    PRIMARY KEY (source_system, legacy_user_id, group_kind),
    UNIQUE (membership_id),
    UNIQUE (transition_id),
    FOREIGN KEY (source_system, legacy_user_id, user_id)
        REFERENCES migration.user_id_map (source_system, legacy_user_id, user_id)
        ON DELETE RESTRICT
);

CREATE TRIGGER migration_workgroup_membership_openings_immutable
BEFORE UPDATE OR DELETE ON migration.workgroup_membership_openings
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

REVOKE ALL ON migration.workgroup_membership_openings FROM PUBLIC;

-- +goose Down

-- Once a cutover opening exists, its retention entitlement may already have
-- reached Settlement.  Refuse a schema-only rollback instead of silently
-- deleting an accounting input.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM migration.workgroup_membership_openings) THEN
        RAISE EXCEPTION 'cannot roll back legacy workgroup membership schema after import';
    END IF;
END;
$$;

DROP TRIGGER migration_workgroup_membership_openings_immutable
    ON migration.workgroup_membership_openings;
DROP TABLE migration.workgroup_membership_openings;

ALTER TABLE workgroups.membership_transitions
    DROP CONSTRAINT membership_transitions_source_authority_check,
    DROP CONSTRAINT membership_transitions_source_check,
    ALTER COLUMN actor_id SET NOT NULL,
    ALTER COLUMN authorization_decision_id SET NOT NULL,
    ADD CONSTRAINT membership_transitions_source_check CHECK (
        source IN ('application', 'staff')
    );

ALTER TABLE workgroups.memberships
    DROP CONSTRAINT memberships_source_check,
    ADD CONSTRAINT memberships_source_check CHECK (
        source IN ('application', 'staff')
    );
