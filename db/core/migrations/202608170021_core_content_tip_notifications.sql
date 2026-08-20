-- +goose Up

-- One incoming content tip produces one recipient-only inbox envelope. All
-- descriptive fields continue to come from the immutable source receipt,
-- typed target binding and identity row; inbox state owns only read/archive.
CREATE TABLE community.content_tip_notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    content_tip_id uuid NOT NULL UNIQUE REFERENCES economy.content_tips (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    read_at timestamptz,
    archived_at timestamptz,
    CHECK (read_at IS NULL OR read_at >= created_at),
    CHECK (archived_at IS NULL OR archived_at >= created_at)
);

CREATE INDEX content_tip_notifications_recipient_recent_idx
    ON community.content_tip_notifications (recipient_user_id, created_at DESC, id DESC)
    WHERE archived_at IS NULL;
CREATE INDEX content_tip_notifications_recipient_unread_idx
    ON community.content_tip_notifications (recipient_user_id, created_at DESC, id DESC)
    WHERE read_at IS NULL AND archived_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION community.protect_content_tip_notification()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_recipient_user_id uuid;
    source_created_at timestamptz;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'content tip notifications cannot be deleted';
    END IF;
    IF TG_OP = 'INSERT' THEN
        SELECT recipient_user_id, occurred_at
        INTO STRICT source_recipient_user_id, source_created_at
        FROM economy.content_tips WHERE id = NEW.content_tip_id;
        IF source_recipient_user_id <> NEW.recipient_user_id
           OR source_created_at <> NEW.created_at THEN
            RAISE EXCEPTION 'invalid content tip notification source';
        END IF;
        RETURN NEW;
    END IF;
    IF OLD.id IS DISTINCT FROM NEW.id
       OR OLD.recipient_user_id IS DISTINCT FROM NEW.recipient_user_id
       OR OLD.content_tip_id IS DISTINCT FROM NEW.content_tip_id
       OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'content tip notification source is immutable';
    END IF;
    IF OLD.read_at IS NOT NULL AND OLD.read_at IS DISTINCT FROM NEW.read_at THEN
        RAISE EXCEPTION 'notification read state is monotonic';
    END IF;
    IF OLD.archived_at IS NOT NULL AND OLD.archived_at IS DISTINCT FROM NEW.archived_at THEN
        RAISE EXCEPTION 'notification archive state is monotonic';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER content_tip_notifications_protected
BEFORE INSERT OR UPDATE OR DELETE ON community.content_tip_notifications
FOR EACH ROW EXECUTE FUNCTION community.protect_content_tip_notification();

INSERT INTO community.content_tip_notifications (recipient_user_id, content_tip_id, created_at)
SELECT recipient_user_id, id, occurred_at FROM economy.content_tips;

-- +goose StatementBegin
CREATE FUNCTION community.project_content_tip_notification()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO community.content_tip_notifications (
        recipient_user_id, content_tip_id, created_at
    ) VALUES (NEW.recipient_user_id, NEW.id, NEW.occurred_at);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER content_tip_notification_projected
AFTER INSERT ON economy.content_tips
FOR EACH ROW EXECUTE FUNCTION community.project_content_tip_notification();

REVOKE ALL ON community.content_tip_notifications FROM PUBLIC;

-- +goose Down

DROP TRIGGER content_tip_notification_projected ON economy.content_tips;
DROP FUNCTION community.project_content_tip_notification();
DROP TRIGGER content_tip_notifications_protected ON community.content_tip_notifications;
DROP FUNCTION community.protect_content_tip_notification();
DROP INDEX community.content_tip_notifications_recipient_unread_idx;
DROP INDEX community.content_tip_notifications_recipient_recent_idx;
DROP TABLE community.content_tip_notifications;
