-- +goose Up

-- The shared personal key now fronts the complete legacy API surface. Existing
-- credentials keep their exact scopes; users must explicitly rotate a key to
-- grant upload or purchase-write authority.
ALTER TABLE identity.personal_api_keys
    DROP CONSTRAINT personal_api_keys_scopes_values_check,
    DROP CONSTRAINT personal_api_keys_scopes_count_check;

ALTER TABLE identity.personal_api_keys
    ADD CONSTRAINT personal_api_keys_scopes_count_check
        CHECK (
            cardinality(scopes) BETWEEN 1 AND 8
            AND array_position(scopes, NULL) IS NULL
        ),
    ADD CONSTRAINT personal_api_keys_scopes_values_check
        CHECK (scopes <@ ARRAY[
            'profile:read',
            'torrent:read',
            'torrent:download',
            'torrent:upload',
            'torrent:purchase:read',
            'torrent:purchase:write',
            'attendance:read',
            'attendance:claim'
        ]::text[]);

-- +goose Down

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM identity.personal_api_keys
        WHERE scopes && ARRAY[
            'torrent:upload',
            'torrent:purchase:read',
            'torrent:purchase:write'
        ]::text[]
    ) THEN
        RAISE EXCEPTION
            'cannot remove legacy API scopes while personal API keys still grant them';
    END IF;
END;
$$;

ALTER TABLE identity.personal_api_keys
    DROP CONSTRAINT personal_api_keys_scopes_values_check,
    DROP CONSTRAINT personal_api_keys_scopes_count_check;

ALTER TABLE identity.personal_api_keys
    ADD CONSTRAINT personal_api_keys_scopes_count_check
        CHECK (
            cardinality(scopes) BETWEEN 1 AND 5
            AND array_position(scopes, NULL) IS NULL
        ),
    ADD CONSTRAINT personal_api_keys_scopes_values_check
        CHECK (scopes <@ ARRAY[
            'profile:read',
            'torrent:read',
            'torrent:download',
            'attendance:read',
            'attendance:claim'
        ]::text[]);
