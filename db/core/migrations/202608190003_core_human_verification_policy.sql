-- +goose Up

-- Human-verification routing is part of the same versioned identity policy as
-- registration and session admission. The public Turnstile site key is safe to
-- persist here; the secret key remains deployment-only Core configuration and
-- is never stored in PostgreSQL or returned by an API.
ALTER TABLE identity.registration_policy
    ADD COLUMN human_verification_provider text NOT NULL DEFAULT 'disabled'
        CHECK (human_verification_provider IN ('disabled', 'turnstile')),
    ADD COLUMN human_verification_site_key text NOT NULL DEFAULT ''
        CHECK (octet_length(human_verification_site_key) <= 128),
    ADD COLUMN human_verification_registration_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN human_verification_login_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN human_verification_password_recovery_enabled boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT registration_policy_human_verification_consistency CHECK (
        (
            human_verification_provider = 'disabled'
            AND human_verification_site_key = ''
            AND NOT human_verification_registration_enabled
            AND NOT human_verification_login_enabled
            AND NOT human_verification_password_recovery_enabled
        ) OR (
            human_verification_provider = 'turnstile'
            AND human_verification_site_key <> ''
            AND (
                human_verification_registration_enabled
                OR human_verification_login_enabled
                OR human_verification_password_recovery_enabled
            )
        )
    );

UPDATE identity.registration_policy
SET version = version + 1,
    updated_at = now()
WHERE singleton = true;

-- +goose Down

UPDATE identity.registration_policy
SET version = version + 1,
    updated_at = now()
WHERE singleton = true;

ALTER TABLE identity.registration_policy
    DROP CONSTRAINT registration_policy_human_verification_consistency,
    DROP COLUMN human_verification_password_recovery_enabled,
    DROP COLUMN human_verification_login_enabled,
    DROP COLUMN human_verification_registration_enabled,
    DROP COLUMN human_verification_site_key,
    DROP COLUMN human_verification_provider;
