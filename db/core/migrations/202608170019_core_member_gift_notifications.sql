-- +goose Up

-- A member-gift notification is a private projection of one immutable gift
-- receipt. Recipient, sender-facing details, amount, message and occurrence
-- time continue to come from the source receipt and identity rows; this table
-- only owns monotonic inbox state.
CREATE TABLE community.member_gift_notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_user_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    member_gift_id uuid NOT NULL UNIQUE
        REFERENCES economy.member_gifts (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    read_at timestamptz,
    archived_at timestamptz,
    CHECK (read_at IS NULL OR read_at >= created_at),
    CHECK (archived_at IS NULL OR archived_at >= created_at)
);

CREATE INDEX member_gift_notifications_recipient_recent_idx
    ON community.member_gift_notifications (
        recipient_user_id, created_at DESC, id DESC
    ) WHERE archived_at IS NULL;

CREATE INDEX member_gift_notifications_recipient_unread_idx
    ON community.member_gift_notifications (
        recipient_user_id, created_at DESC, id DESC
    ) WHERE read_at IS NULL AND archived_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION community.protect_member_gift_notification()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_recipient_user_id uuid;
    source_created_at timestamptz;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'member gift notifications cannot be deleted';
    END IF;

    IF TG_OP = 'INSERT' THEN
        SELECT recipient_user_id, occurred_at
        INTO STRICT source_recipient_user_id, source_created_at
        FROM economy.member_gifts
        WHERE id = NEW.member_gift_id;

        IF source_recipient_user_id <> NEW.recipient_user_id
           OR source_created_at <> NEW.created_at THEN
            RAISE EXCEPTION 'invalid member gift notification source';
        END IF;
        RETURN NEW;
    END IF;

    IF OLD.id IS DISTINCT FROM NEW.id
       OR OLD.recipient_user_id IS DISTINCT FROM NEW.recipient_user_id
       OR OLD.member_gift_id IS DISTINCT FROM NEW.member_gift_id
       OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'member gift notification source is immutable';
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

CREATE TRIGGER member_gift_notifications_protected
BEFORE INSERT OR UPDATE OR DELETE
ON community.member_gift_notifications
FOR EACH ROW EXECUTE FUNCTION community.protect_member_gift_notification();

-- Existing PeerGo gift receipts are complete immutable facts. Rehearsal
-- upgrades can therefore backfill them without inventing legacy messages.
INSERT INTO community.member_gift_notifications (
    recipient_user_id, member_gift_id, created_at
)
SELECT recipient_user_id, id, occurred_at
FROM economy.member_gifts;

-- +goose StatementBegin
CREATE FUNCTION community.project_member_gift_notification()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO community.member_gift_notifications (
        recipient_user_id, member_gift_id, created_at
    ) VALUES (
        NEW.recipient_user_id, NEW.id, NEW.occurred_at
    );
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER member_gift_notification_projected
AFTER INSERT ON economy.member_gifts
FOR EACH ROW EXECUTE FUNCTION community.project_member_gift_notification();

REVOKE ALL ON community.member_gift_notifications FROM PUBLIC;

-- +goose Down

DROP TRIGGER member_gift_notification_projected ON economy.member_gifts;
DROP FUNCTION community.project_member_gift_notification();

DROP TRIGGER member_gift_notifications_protected
    ON community.member_gift_notifications;
DROP FUNCTION community.protect_member_gift_notification();
DROP INDEX community.member_gift_notifications_recipient_unread_idx;
DROP INDEX community.member_gift_notifications_recipient_recent_idx;
DROP TABLE community.member_gift_notifications;
