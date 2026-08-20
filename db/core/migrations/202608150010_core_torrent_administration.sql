-- +goose Up

-- Torrent management is separate from review. Reading the operational
-- workbench does not grant review authority; changing public/Tracker
-- availability is a distinct high-risk action.
INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES
    ('torrent.lifecycle.update', '下架或恢复已发布种子', 'high', 'none', 'staff-session', true, true),
    ('torrent.manage.read', '读取种子管理工作台', 'medium', 'none', 'staff-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_admin', 'torrent.lifecycle.update'),
    ('site_admin', 'torrent.manage.read');

-- Every lifecycle command is immutable and replayable by its UUID. The
-- aggregate version unique fence prevents two different commands from both
-- claiming the same transition, while the aggregate row lock remains the
-- primary concurrency boundary.
CREATE TABLE torrents.torrent_lifecycle_changes (
    id uuid PRIMARY KEY,
    torrent_id bigint NOT NULL
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    actor_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    action text NOT NULL CHECK (action IN ('disable', 'restore')),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    expected_torrent_version bigint NOT NULL CHECK (expected_torrent_version > 0),
    resulting_torrent_version bigint NOT NULL CHECK (
        resulting_torrent_version = expected_torrent_version + 1
    ),
    before_state text NOT NULL CHECK (before_state IN ('published', 'disabled')),
    after_state text NOT NULL CHECK (after_state IN ('published', 'disabled')),
    authorization_decision_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL,
    UNIQUE (torrent_id, expected_torrent_version),
    CHECK (
        (action = 'disable' AND before_state = 'published' AND after_state = 'disabled')
        OR (action = 'restore' AND before_state = 'disabled' AND after_state = 'published')
    )
);

CREATE INDEX torrent_lifecycle_changes_torrent_recent_idx
    ON torrents.torrent_lifecycle_changes (torrent_id, occurred_at DESC, id DESC);

CREATE INDEX torrents_staff_workbench_recent_idx
    ON torrents.torrents (updated_at DESC, id DESC);

-- +goose StatementBegin
CREATE FUNCTION torrents.reject_lifecycle_change_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'torrent lifecycle changes are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER torrent_lifecycle_changes_immutable
BEFORE UPDATE OR DELETE ON torrents.torrent_lifecycle_changes
FOR EACH ROW EXECUTE FUNCTION torrents.reject_lifecycle_change_mutation();

-- +goose Down

DROP TRIGGER torrent_lifecycle_changes_immutable ON torrents.torrent_lifecycle_changes;
DROP FUNCTION torrents.reject_lifecycle_change_mutation();
DROP INDEX torrents.torrents_staff_workbench_recent_idx;
DROP INDEX torrents.torrent_lifecycle_changes_torrent_recent_idx;
DROP TABLE torrents.torrent_lifecycle_changes;

DELETE FROM authz.role_permissions
WHERE role_id = 'site_admin'
  AND action IN ('torrent.lifecycle.update', 'torrent.manage.read');

DELETE FROM authz.permissions
WHERE action IN ('torrent.lifecycle.update', 'torrent.manage.read');
