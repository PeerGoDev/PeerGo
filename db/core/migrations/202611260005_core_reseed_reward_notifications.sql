-- +goose Up
ALTER TABLE community.torrent_reseed_notifications
    DROP CONSTRAINT torrent_reseed_notifications_kind_check,
    ADD CONSTRAINT torrent_reseed_notifications_kind_check
        CHECK (kind IN ('torrent_reseed_requested', 'torrent_reseed_restored', 'torrent_reseed_rewarded'));

CREATE VIEW economy.reseed_reward_round_offers AS
SELECT round.request_id, round.torrent_id, round.ended_at,
       policy.required_hours, policy.magic_amount,
       CASE WHEN award.request_id IS NOT NULL THEN 'awarded'
            WHEN round.ended_at IS NOT NULL THEN 'ended'
            ELSE 'active' END AS status
FROM economy.reseed_reward_rounds AS round
JOIN economy.reseed_reward_policies AS policy ON policy.revision = round.policy_revision
LEFT JOIN economy.reseed_reward_awards AS award ON award.request_id = round.request_id;

CREATE VIEW economy.current_reseed_reward_offer AS
SELECT latest.required_hours, latest.magic_amount
FROM (SELECT enabled, required_hours, magic_amount FROM economy.reseed_reward_policies
      ORDER BY revision DESC LIMIT 1) AS latest
WHERE latest.enabled;

-- The award row is inserted in the ledger transaction. A failed award rolls
-- back the notification as well, and a replay cannot insert a second award.
-- +goose StatementBegin
CREATE FUNCTION community.notify_reseed_reward() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, public AS $$
BEGIN
    INSERT INTO community.torrent_reseed_notifications (request_id, recipient_user_id, kind, created_at)
    SELECT NEW.request_id, NEW.user_id, 'torrent_reseed_rewarded', ledger.recorded_at
    FROM economy.magic_transactions AS ledger WHERE ledger.id = NEW.magic_transaction_id;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER torrent_reseed_rewarded_notification
AFTER INSERT ON economy.reseed_reward_awards
FOR EACH ROW EXECUTE FUNCTION community.notify_reseed_reward();

INSERT INTO community.torrent_reseed_notifications (request_id, recipient_user_id, kind, created_at)
SELECT award.request_id, award.user_id, 'torrent_reseed_rewarded', ledger.recorded_at
FROM economy.reseed_reward_awards AS award
JOIN economy.magic_transactions AS ledger ON ledger.id = award.magic_transaction_id
ON CONFLICT (request_id, recipient_user_id, kind) DO NOTHING;

-- +goose Down
DROP TRIGGER torrent_reseed_rewarded_notification ON economy.reseed_reward_awards;
DROP FUNCTION community.notify_reseed_reward();
DROP VIEW economy.current_reseed_reward_offer;
DROP VIEW economy.reseed_reward_round_offers;
DELETE FROM community.torrent_reseed_notifications WHERE kind = 'torrent_reseed_rewarded';
ALTER TABLE community.torrent_reseed_notifications
    DROP CONSTRAINT torrent_reseed_notifications_kind_check,
    ADD CONSTRAINT torrent_reseed_notifications_kind_check
        CHECK (kind IN ('torrent_reseed_requested', 'torrent_reseed_restored'));
