-- +goose Up

-- Inbox deletion is a user-visible archive operation. The immutable review
-- notification and its business source remain present for audit and recovery.
ALTER TABLE community.torrent_review_notifications
    ADD COLUMN archived_at timestamptz,
    ADD CONSTRAINT torrent_review_notifications_archived_after_creation
        CHECK (archived_at IS NULL OR archived_at >= created_at);

DROP INDEX community.torrent_review_notifications_recipient_recent_idx;
DROP INDEX community.torrent_review_notifications_recipient_unread_idx;

CREATE INDEX torrent_review_notifications_recipient_recent_idx
    ON community.torrent_review_notifications (
        recipient_user_id,
        created_at DESC,
        id DESC
    )
    WHERE archived_at IS NULL;

CREATE INDEX torrent_review_notifications_recipient_unread_idx
    ON community.torrent_review_notifications (
        recipient_user_id,
        created_at DESC,
        id DESC
    )
    WHERE read_at IS NULL AND archived_at IS NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION community.protect_torrent_review_notification()
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

    IF OLD.archived_at IS NOT NULL
        AND OLD.archived_at IS DISTINCT FROM NEW.archived_at THEN
        RAISE EXCEPTION 'notification archive state is monotonic';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'notification.archive.self',
    '归档自己的站内通知',
    'low',
    'self',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'notification.archive.self');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'member'
  AND action = 'notification.archive.self';

DELETE FROM authz.permissions
WHERE action = 'notification.archive.self';

DROP INDEX community.torrent_review_notifications_recipient_recent_idx;
DROP INDEX community.torrent_review_notifications_recipient_unread_idx;

ALTER TABLE community.torrent_review_notifications
    DROP CONSTRAINT torrent_review_notifications_archived_after_creation,
    DROP COLUMN archived_at;

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
CREATE OR REPLACE FUNCTION community.protect_torrent_review_notification()
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
