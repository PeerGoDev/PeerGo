-- +goose Up

-- User feedback is an inbound support source, not an inbox notification.
-- Keeping it separate prevents arbitrary user text from entering the typed,
-- immutable torrent-review notification projection.
CREATE TABLE community.support_feedbacks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sender_user_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    title text NOT NULL,
    content text NOT NULL,
    status text NOT NULL DEFAULT 'open',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (char_length(title) BETWEEN 1 AND 100),
    CHECK (char_length(content) BETWEEN 1 AND 2000),
    CHECK (title = btrim(title)),
    CHECK (content = btrim(content)),
    CHECK (status IN ('open', 'answered')),
    CHECK (updated_at >= created_at)
);

CREATE INDEX support_feedbacks_open_recent_idx
    ON community.support_feedbacks (created_at DESC, id DESC)
    WHERE status = 'open';

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'notification.feedback.create.self',
    '向站点管理员提交反馈',
    'low',
    'self',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'notification.feedback.create.self');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'member'
  AND action = 'notification.feedback.create.self';

DELETE FROM authz.permissions
WHERE action = 'notification.feedback.create.self';

DROP TABLE IF EXISTS community.support_feedbacks;
