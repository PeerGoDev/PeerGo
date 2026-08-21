-- +goose Up

-- PtYes represents a claimed system-issued invitation with inviting_user=0.
-- This is a source sentinel, not a PeerGo user ID: the importer retains the
-- row as unresolved ancestry evidence and never creates a synthetic account.
ALTER TABLE migration.legacy_invitation_relationship_openings
    DROP CONSTRAINT legacy_invitation_relationship_openings_legacy_inviter_id_check;

ALTER TABLE migration.legacy_invitation_relationship_openings
    ADD CONSTRAINT legacy_invitation_relationship_openings_legacy_inviter_id_check
    CHECK (legacy_inviter_id >= 0);

-- +goose Down

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM migration.legacy_invitation_relationship_openings
        WHERE legacy_inviter_id = 0
    ) THEN
        RAISE EXCEPTION '202608210004 cannot roll back after system invitation evidence was recorded';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE migration.legacy_invitation_relationship_openings
    DROP CONSTRAINT legacy_invitation_relationship_openings_legacy_inviter_id_check;

ALTER TABLE migration.legacy_invitation_relationship_openings
    ADD CONSTRAINT legacy_invitation_relationship_openings_legacy_inviter_id_check
    CHECK (legacy_inviter_id > 0);
