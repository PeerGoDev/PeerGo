-- +goose Up

-- Staff workflows need a compact, stable numeric identifier alongside the
-- canonical UUID. Migrated PtYes accounts retain their historical user ID;
-- accounts created by PeerGo receive the next sequence value. The numeric ID
-- is an administration locator only and never replaces UUID foreign keys.
CREATE SEQUENCE identity.user_numeric_id_seq AS bigint MINVALUE 1 START WITH 1;

ALTER TABLE identity.users
    ADD COLUMN numeric_id bigint;

UPDATE identity.users AS users
SET numeric_id = mapping.legacy_user_id
FROM migration.user_id_map AS mapping
WHERE mapping.source_system = 'ptyes'
  AND mapping.user_id = users.id;

-- +goose StatementBegin
DO $$
DECLARE
    maximum_numeric_id bigint;
BEGIN
    SELECT max(numeric_id) INTO maximum_numeric_id
    FROM identity.users;

    IF maximum_numeric_id IS NULL THEN
        PERFORM setval('identity.user_numeric_id_seq', 1, false);
    ELSE
        PERFORM setval('identity.user_numeric_id_seq', maximum_numeric_id, true);
    END IF;

    UPDATE identity.users
    SET numeric_id = nextval('identity.user_numeric_id_seq')
    WHERE numeric_id IS NULL;

    SELECT max(numeric_id) INTO maximum_numeric_id
    FROM identity.users;

    IF maximum_numeric_id IS NOT NULL THEN
        PERFORM setval('identity.user_numeric_id_seq', maximum_numeric_id, true);
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE identity.users
    ALTER COLUMN numeric_id SET DEFAULT nextval('identity.user_numeric_id_seq'),
    ALTER COLUMN numeric_id SET NOT NULL,
    ADD CONSTRAINT users_numeric_id_unique UNIQUE (numeric_id),
    ADD CONSTRAINT users_numeric_id_positive CHECK (numeric_id > 0);

ALTER SEQUENCE identity.user_numeric_id_seq
    OWNED BY identity.users.numeric_id;

-- +goose Down

ALTER SEQUENCE identity.user_numeric_id_seq OWNED BY NONE;

ALTER TABLE identity.users
    DROP CONSTRAINT users_numeric_id_positive,
    DROP CONSTRAINT users_numeric_id_unique,
    DROP COLUMN numeric_id;

DROP SEQUENCE identity.user_numeric_id_seq;
