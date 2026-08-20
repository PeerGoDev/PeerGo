-- +goose Up
CREATE TABLE catalog.site_profile (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    registration_mode text NOT NULL
        CHECK (registration_mode IN ('open', 'invite', 'closed')),
    online_users integer NOT NULL DEFAULT 0 CHECK (online_users >= 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS catalog.site_profile;
