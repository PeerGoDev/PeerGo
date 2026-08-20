-- +goose Up

-- A bookmark is private user state over the published catalog projection. It
-- deliberately references catalog.torrents instead of the Tracker/write-side
-- aggregate so reads cannot couple user navigation to announce availability.
CREATE TABLE catalog.torrent_bookmarks (
    user_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE CASCADE,
    torrent_id text NOT NULL
        REFERENCES catalog.torrents (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, torrent_id),
    CHECK (torrent_id ~ '^[a-z0-9][a-z0-9-]{0,63}$')
);

CREATE INDEX torrent_bookmarks_user_recent_idx
    ON catalog.torrent_bookmarks (user_id, created_at DESC, torrent_id DESC);

-- Reading existing personal state and changing it are separate capabilities:
-- removing future write authority must not hide the user's saved collection.
INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES
    (
        'torrent.bookmark.read.self',
        '查看自己的种子收藏',
        'low',
        'self',
        'web-session',
        true,
        true
    ),
    (
        'torrent.bookmark.write.self',
        '添加或取消自己的种子收藏',
        'low',
        'self',
        'web-session',
        true,
        true
    );

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'torrent.bookmark.read.self'),
    ('member', 'torrent.bookmark.write.self');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'member'
  AND action IN ('torrent.bookmark.read.self', 'torrent.bookmark.write.self');

DELETE FROM authz.permissions
WHERE action IN ('torrent.bookmark.read.self', 'torrent.bookmark.write.self');

DROP TABLE IF EXISTS catalog.torrent_bookmarks;
