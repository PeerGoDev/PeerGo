-- +goose Up
-- A consumed invitation is permanently bound to one registration even if an
-- operator later inspects or expires the invitation row. Runtime commands do
-- not reset claims, and this constraint is the final database-side guard.
ALTER TABLE identity.registrations
    ADD CONSTRAINT registrations_invitation_unique UNIQUE (invitation_id);

-- +goose Down
ALTER TABLE identity.registrations
    DROP CONSTRAINT registrations_invitation_unique;
