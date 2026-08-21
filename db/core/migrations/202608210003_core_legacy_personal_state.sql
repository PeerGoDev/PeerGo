-- +goose Up

-- Invitation ancestry is durable account state, not an attribute of an
-- invitation token.  Persisting the relationship separately lets completed
-- registrations and finite legacy imports share one graph without retaining
-- recoverable old invitation credentials.
CREATE TABLE identity.invitation_relationships (
    invitee_user_id uuid PRIMARY KEY
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    inviter_user_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    invitation_id uuid UNIQUE
        REFERENCES identity.registration_invitations (id) ON DELETE RESTRICT,
    source_kind text NOT NULL
        CHECK (source_kind IN ('registration', 'legacy_import')),
    source_reference text NOT NULL UNIQUE
        CHECK (source_reference ~ '^[a-z0-9][a-z0-9:._-]{0,127}$'),
    source_run_id uuid REFERENCES migration.runs (id) ON DELETE RESTRICT,
    source_fingerprint bytea,
    established_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    CHECK (invitee_user_id <> inviter_user_id),
    CHECK (recorded_at >= established_at),
    CHECK (
        (source_kind = 'registration'
            AND invitation_id IS NOT NULL
            AND source_run_id IS NULL
            AND source_fingerprint IS NULL)
        OR
        (source_kind = 'legacy_import'
            AND invitation_id IS NULL
            AND source_run_id IS NOT NULL
            AND octet_length(source_fingerprint) = 32)
    )
);

CREATE INDEX invitation_relationships_inviter_idx
    ON identity.invitation_relationships
        (inviter_user_id, established_at DESC, invitee_user_id);

-- Backfill any native member registrations completed before this migration.
-- Operator/development invitations have no member issuer and intentionally do
-- not create an ancestry edge.
INSERT INTO identity.invitation_relationships (
    invitee_user_id, inviter_user_id, invitation_id,
    source_kind, source_reference, established_at, recorded_at
)
SELECT
    registration.user_id,
    invitation.issuer_user_id,
    invitation.id,
    'registration',
    'member-invitation:' || invitation.id::text,
    registration.completed_at,
    registration.completed_at
FROM identity.registrations AS registration
JOIN identity.registration_invitations AS invitation
  ON invitation.id = registration.invitation_id
WHERE registration.state = 'completed'
  AND invitation.source_kind = 'member'
  AND invitation.issuer_user_id IS NOT NULL;

-- Every legacy source row receives append-only evidence.  The bookmark target
-- itself remains mutable user state: a replay verifies this receipt and never
-- resurrects a bookmark the member removed after cutover.
CREATE TABLE migration.legacy_torrent_bookmark_openings (
    legacy_bookmark_id bigint PRIMARY KEY CHECK (legacy_bookmark_id > 0),
    first_run_id uuid NOT NULL REFERENCES migration.runs (id) ON DELETE RESTRICT,
    legacy_user_id bigint NOT NULL CHECK (legacy_user_id > 0),
    user_id uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    legacy_torrent_id bigint NOT NULL CHECK (legacy_torrent_id > 0),
    torrent_id bigint REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    disposition text NOT NULL CHECK (disposition IN (
        'imported', 'already_present', 'duplicate',
        'unmapped_user', 'unmapped_torrent', 'unavailable_torrent'
    )),
    source_fingerprint bytea NOT NULL CHECK (octet_length(source_fingerprint) = 32),
    bookmarked_at timestamptz NOT NULL,
    imported_at timestamptz NOT NULL
);

CREATE INDEX legacy_torrent_bookmark_openings_run_idx
    ON migration.legacy_torrent_bookmark_openings (first_run_id, disposition);

CREATE TABLE migration.legacy_invitation_relationship_openings (
    legacy_invitation_id bigint PRIMARY KEY CHECK (legacy_invitation_id > 0),
    first_run_id uuid NOT NULL REFERENCES migration.runs (id) ON DELETE RESTRICT,
    legacy_inviter_id bigint NOT NULL CHECK (legacy_inviter_id > 0),
    inviter_user_id uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    legacy_invitee_id bigint NOT NULL CHECK (legacy_invitee_id > 0),
    invitee_user_id uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    disposition text NOT NULL CHECK (disposition IN (
        'imported', 'already_present', 'unmapped_inviter', 'unmapped_invitee'
    )),
    source_fingerprint bytea NOT NULL CHECK (octet_length(source_fingerprint) = 32),
    established_at timestamptz NOT NULL,
    source_created_at timestamptz NOT NULL,
    imported_at timestamptz NOT NULL
);

CREATE INDEX legacy_invitation_relationship_openings_run_idx
    ON migration.legacy_invitation_relationship_openings (first_run_id, disposition);

-- Rousi's harem and one-time invitation rewards are already part of each
-- user's migrated magic opening balance.  These rows preserve exact historical
-- totals for display and audit only; they must never create another posting.
CREATE TABLE migration.legacy_invitation_reward_openings (
    legacy_user_id bigint NOT NULL CHECK (legacy_user_id > 0),
    reward_kind text NOT NULL CHECK (reward_kind IN ('harem', 'invite_reward')),
    first_run_id uuid NOT NULL REFERENCES migration.runs (id) ON DELETE RESTRICT,
    user_id uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    source_row_count bigint NOT NULL CHECK (source_row_count > 0),
    exact_amount numeric NOT NULL CHECK (exact_amount > 0),
    rounded_amount bigint NOT NULL CHECK (rounded_amount > 0),
    source_fingerprint bytea NOT NULL CHECK (octet_length(source_fingerprint) = 32),
    first_rewarded_at timestamptz NOT NULL,
    last_rewarded_at timestamptz NOT NULL,
    disposition text NOT NULL CHECK (disposition IN ('preserved', 'unmapped_user')),
    imported_at timestamptz NOT NULL,
    PRIMARY KEY (legacy_user_id, reward_kind),
    CHECK (last_rewarded_at >= first_rewarded_at)
);

CREATE INDEX legacy_invitation_reward_openings_run_idx
    ON migration.legacy_invitation_reward_openings (first_run_id, reward_kind);

CREATE TABLE migration.legacy_personal_state_imports (
    run_id uuid PRIMARY KEY REFERENCES migration.runs (id) ON DELETE RESTRICT,
    source_snapshot_sha256 bytea NOT NULL CHECK (octet_length(source_snapshot_sha256) = 32),
    source_evidence_sha256 bytea NOT NULL CHECK (octet_length(source_evidence_sha256) = 32),
    bookmark_source_rows bigint NOT NULL CHECK (bookmark_source_rows >= 0),
    bookmark_distinct_pairs bigint NOT NULL CHECK (
        bookmark_distinct_pairs >= 0 AND bookmark_distinct_pairs <= bookmark_source_rows
    ),
    bookmark_applied_rows bigint NOT NULL CHECK (bookmark_applied_rows >= 0),
    bookmark_unresolved_rows bigint NOT NULL CHECK (bookmark_unresolved_rows >= 0),
    invitation_source_rows bigint NOT NULL CHECK (invitation_source_rows >= 0),
    invitation_relationships bigint NOT NULL CHECK (invitation_relationships >= 0),
    invitation_unresolved_rows bigint NOT NULL CHECK (invitation_unresolved_rows >= 0),
    harem_reward_source_rows bigint NOT NULL CHECK (harem_reward_source_rows >= 0),
    harem_reward_users bigint NOT NULL CHECK (harem_reward_users >= 0),
    invite_reward_source_rows bigint NOT NULL CHECK (invite_reward_source_rows >= 0),
    invite_reward_users bigint NOT NULL CHECK (invite_reward_users >= 0),
    imported_at timestamptz NOT NULL
);

CREATE TRIGGER invitation_relationships_immutable
BEFORE UPDATE OR DELETE ON identity.invitation_relationships
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER legacy_torrent_bookmark_openings_immutable
BEFORE UPDATE OR DELETE ON migration.legacy_torrent_bookmark_openings
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER legacy_invitation_relationship_openings_immutable
BEFORE UPDATE OR DELETE ON migration.legacy_invitation_relationship_openings
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER legacy_invitation_reward_openings_immutable
BEFORE UPDATE OR DELETE ON migration.legacy_invitation_reward_openings
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER legacy_personal_state_imports_immutable
BEFORE UPDATE OR DELETE ON migration.legacy_personal_state_imports
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

REVOKE ALL ON identity.invitation_relationships FROM PUBLIC;
REVOKE ALL ON migration.legacy_torrent_bookmark_openings FROM PUBLIC;
REVOKE ALL ON migration.legacy_invitation_relationship_openings FROM PUBLIC;
REVOKE ALL ON migration.legacy_invitation_reward_openings FROM PUBLIC;
REVOKE ALL ON migration.legacy_personal_state_imports FROM PUBLIC;

-- +goose Down

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM migration.legacy_personal_state_imports)
       OR EXISTS (SELECT 1 FROM identity.invitation_relationships) THEN
        RAISE EXCEPTION '202608210003 cannot roll back after invitation relationships or legacy personal state were recorded';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER legacy_personal_state_imports_immutable ON migration.legacy_personal_state_imports;
DROP TRIGGER legacy_invitation_reward_openings_immutable ON migration.legacy_invitation_reward_openings;
DROP TRIGGER legacy_invitation_relationship_openings_immutable ON migration.legacy_invitation_relationship_openings;
DROP TRIGGER legacy_torrent_bookmark_openings_immutable ON migration.legacy_torrent_bookmark_openings;
DROP TRIGGER invitation_relationships_immutable ON identity.invitation_relationships;

DROP TABLE migration.legacy_personal_state_imports;
DROP TABLE migration.legacy_invitation_reward_openings;
DROP TABLE migration.legacy_invitation_relationship_openings;
DROP TABLE migration.legacy_torrent_bookmark_openings;
DROP INDEX identity.invitation_relationships_inviter_idx;
DROP TABLE identity.invitation_relationships;
