-- +goose Up

CREATE SCHEMA IF NOT EXISTS community;

-- The first notification source is deliberately concrete. A row can only be
-- created from one immutable torrent review decision and for that torrent's
-- uploader; there is no loose source_type/source_id pair or arbitrary payload.
CREATE TABLE community.torrent_review_notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_user_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    review_decision_id uuid NOT NULL UNIQUE
        REFERENCES review.torrent_decisions (id) ON DELETE RESTRICT,
    torrent_public_id uuid NOT NULL
        REFERENCES torrents.torrents (public_id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    read_at timestamptz,
    CHECK (read_at IS NULL OR read_at >= created_at)
);

CREATE INDEX torrent_review_notifications_recipient_recent_idx
    ON community.torrent_review_notifications (
        recipient_user_id,
        created_at DESC,
        id DESC
    );

CREATE INDEX torrent_review_notifications_recipient_unread_idx
    ON community.torrent_review_notifications (
        recipient_user_id,
        created_at DESC,
        id DESC
    )
    WHERE read_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION community.protect_torrent_review_notification()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'torrent review notifications cannot be deleted';
    END IF;

    IF OLD.id IS DISTINCT FROM NEW.id
        OR OLD.recipient_user_id IS DISTINCT FROM NEW.recipient_user_id
        OR OLD.review_decision_id IS DISTINCT FROM NEW.review_decision_id
        OR OLD.torrent_public_id IS DISTINCT FROM NEW.torrent_public_id
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'torrent review notification source is immutable';
    END IF;

    IF OLD.read_at IS NOT NULL AND OLD.read_at IS DISTINCT FROM NEW.read_at THEN
        RAISE EXCEPTION 'notification read state is monotonic';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER torrent_review_notifications_protected
BEFORE UPDATE OR DELETE ON community.torrent_review_notifications
FOR EACH ROW EXECUTE FUNCTION community.protect_torrent_review_notification();

-- Existing immutable review decisions are complete source facts, so the first
-- deployment can backfill them without reading old application DTOs or calling
-- a legacy service. The same typed join used by live writes derives recipient,
-- target and occurrence time.
INSERT INTO community.torrent_review_notifications (
    recipient_user_id,
    review_decision_id,
    torrent_public_id,
    created_at
)
SELECT
    torrent.uploader_id,
    decision.id,
    decision.torrent_public_id,
    decision.occurred_at
FROM review.torrent_decisions AS decision
JOIN torrents.torrents AS torrent
  ON torrent.id = decision.torrent_id
 AND torrent.public_id = decision.torrent_public_id;

-- Reading a private inbox and advancing its monotonic read state remain
-- separate capabilities. Losing write authority must not hide old messages.
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
        'notification.read.self',
        '查看自己的站内通知',
        'low',
        'self',
        'web-session',
        true,
        true
    ),
    (
        'notification.read.state.write.self',
        '更新自己的通知已读状态',
        'low',
        'self',
        'web-session',
        true,
        true
    );

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'notification.read.self'),
    ('member', 'notification.read.state.write.self');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'member'
  AND action IN (
      'notification.read.self',
      'notification.read.state.write.self'
  );

DELETE FROM authz.permissions
WHERE action IN (
    'notification.read.self',
    'notification.read.state.write.self'
);

DROP TRIGGER IF EXISTS torrent_review_notifications_protected
    ON community.torrent_review_notifications;
DROP FUNCTION IF EXISTS community.protect_torrent_review_notification();
DROP TABLE IF EXISTS community.torrent_review_notifications;
DROP SCHEMA IF EXISTS community;
