-- +goose Up

INSERT INTO authz.permissions (
    action, description, risk_level, relationship, credential_audience,
    grantable, discoverable
) VALUES (
    'torrent.metadata.update.self',
    '修改自己已发布种子的基础发布资料',
    'medium',
    'self',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'torrent.metadata.update.self');

-- The aggregate row is the current read model; every accepted edit is also
-- frozen here so a later moderation or deletion case can reconstruct exactly
-- which uploader-visible fields changed. Swarm identity and original objects
-- are deliberately absent from this command surface.
CREATE TABLE torrents.torrent_metadata_revisions (
    id uuid PRIMARY KEY,
    torrent_id bigint NOT NULL
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    uploader_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    expected_torrent_version bigint NOT NULL CHECK (expected_torrent_version > 0),
    resulting_torrent_version bigint NOT NULL CHECK (
        resulting_torrent_version = expected_torrent_version + 1
    ),
    category_id text NOT NULL
        REFERENCES catalog.categories (id) ON DELETE RESTRICT,
    title text NOT NULL CHECK (char_length(btrim(title)) BETWEEN 1 AND 240),
    subtitle text NOT NULL DEFAULT '' CHECK (char_length(subtitle) <= 300),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    authorization_decision_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (torrent_id, expected_torrent_version),
    UNIQUE (torrent_id, resulting_torrent_version)
);

CREATE INDEX torrent_metadata_revisions_history_idx
    ON torrents.torrent_metadata_revisions (torrent_id, occurred_at DESC, id DESC);

-- +goose StatementBegin
CREATE FUNCTION torrents.reject_torrent_metadata_revision_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'torrent metadata revisions are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER torrent_metadata_revisions_immutable
BEFORE UPDATE OR DELETE ON torrents.torrent_metadata_revisions
FOR EACH ROW EXECUTE FUNCTION torrents.reject_torrent_metadata_revision_mutation();

-- +goose Down

DROP TRIGGER IF EXISTS torrent_metadata_revisions_immutable
    ON torrents.torrent_metadata_revisions;
DROP TABLE IF EXISTS torrents.torrent_metadata_revisions;
DROP FUNCTION IF EXISTS torrents.reject_torrent_metadata_revision_mutation();

DELETE FROM authz.role_permissions
WHERE role_id = 'member' AND action = 'torrent.metadata.update.self';
DELETE FROM authz.permissions
WHERE action = 'torrent.metadata.update.self';
