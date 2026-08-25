-- +goose Up

-- Member invitations may be bound to one normalized email address without
-- persisting that address in Core. The HMAC key is the one-time bearer token,
-- which is never stored; a database reader therefore cannot enumerate emails.
ALTER TABLE identity.registration_invitations
    ADD COLUMN email_binding_hmac bytea
        CHECK (
            email_binding_hmac IS NULL
            OR octet_length(email_binding_hmac) = 32
        );

-- +goose Down

ALTER TABLE identity.registration_invitations
    DROP COLUMN email_binding_hmac;
