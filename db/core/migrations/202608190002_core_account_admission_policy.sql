-- +goose Up

-- Account naming, email admission and ordinary Web-session lifetime are one
-- identity boundary. Keeping them on the existing versioned singleton makes
-- every staff edit immediately visible to both registration and login without
-- introducing a second generic-settings source.
ALTER TABLE identity.registration_policy
    ADD COLUMN username_min_characters smallint NOT NULL DEFAULT 3
        CHECK (username_min_characters BETWEEN 3 AND 32),
    ADD COLUMN username_max_characters smallint NOT NULL DEFAULT 20
        CHECK (username_max_characters BETWEEN 3 AND 32),
    ADD COLUMN reserved_usernames text[] NOT NULL DEFAULT ARRAY[
        'admin', 'administrator', 'root', 'system', 'moderator',
        'staff', 'support', 'peergo', 'rousi'
    ]::text[]
        CHECK (cardinality(reserved_usernames) <= 200)
        CHECK (octet_length(array_to_string(reserved_usernames, ',')) <= 8000),
    ADD COLUMN email_domain_mode text NOT NULL DEFAULT 'any'
        CHECK (email_domain_mode IN ('any', 'allowlist', 'blocklist')),
    ADD COLUMN email_domains text[] NOT NULL DEFAULT ARRAY[]::text[]
        CHECK (cardinality(email_domains) <= 100)
        CHECK (octet_length(array_to_string(email_domains, ',')) <= 8000),
    ADD COLUMN session_valid_hours smallint NOT NULL DEFAULT 168
        CHECK (session_valid_hours BETWEEN 1 AND 720),
    ADD COLUMN remember_session_valid_hours smallint NOT NULL DEFAULT 720
        CHECK (remember_session_valid_hours BETWEEN 24 AND 2160),
    ADD CONSTRAINT registration_policy_username_length_order
        CHECK (username_min_characters <= username_max_characters),
    ADD CONSTRAINT registration_policy_session_duration_order
        CHECK (session_valid_hours <= remember_session_valid_hours);

-- The migration changes the effective defaults, so it is itself a new policy
-- revision even before an administrator performs the first explicit edit.
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
    DROP CONSTRAINT registration_policy_session_duration_order,
    DROP CONSTRAINT registration_policy_username_length_order,
    DROP COLUMN remember_session_valid_hours,
    DROP COLUMN session_valid_hours,
    DROP COLUMN email_domains,
    DROP COLUMN email_domain_mode,
    DROP COLUMN reserved_usernames,
    DROP COLUMN username_max_characters,
    DROP COLUMN username_min_characters;
