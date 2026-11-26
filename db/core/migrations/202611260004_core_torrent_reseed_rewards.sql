-- +goose Up
INSERT INTO economy.magic_accounts (id, user_id, account_kind, account_code, balance, version, updated_at)
VALUES ('00000000-0000-7000-8000-000000000015', NULL, 'system', 'system:mint:reseed', 0, 1, clock_timestamp());

CREATE TABLE economy.reseed_reward_policies (
    revision bigint PRIMARY KEY CHECK (revision >= 1),
    enabled boolean NOT NULL,
    required_hours integer NOT NULL CHECK (required_hours BETWEEN 1 AND 720),
    magic_amount bigint NOT NULL CHECK (magic_amount BETWEEN 1 AND 1000000),
    issued_at timestamptz NOT NULL,
    issued_by uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid,
    CHECK ((issued_by IS NULL) = (authorization_decision_id IS NULL))
);

INSERT INTO economy.reseed_reward_policies (revision, enabled, required_hours, magic_amount, issued_at)
VALUES (1, false, 24, 1000, clock_timestamp());

CREATE TRIGGER reseed_reward_policies_immutable
BEFORE UPDATE OR DELETE ON economy.reseed_reward_policies
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TABLE economy.reseed_reward_rounds (
    request_id uuid PRIMARY KEY REFERENCES torrents.reseed_requests (id) ON DELETE RESTRICT,
    torrent_id bigint NOT NULL REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    policy_revision bigint NOT NULL REFERENCES economy.reseed_reward_policies (revision) ON DELETE RESTRICT,
    started_at timestamptz NOT NULL,
    ended_at timestamptz CHECK (ended_at >= started_at)
);

CREATE UNIQUE INDEX reseed_reward_rounds_open_torrent_idx
    ON economy.reseed_reward_rounds (torrent_id) WHERE ended_at IS NULL;

CREATE TABLE economy.reseed_reward_pending_windows (
    window_start timestamptz PRIMARY KEY
        REFERENCES economy.seeding_reward_evidence_windows (window_start) ON DELETE RESTRICT
);

CREATE TABLE economy.reseed_reward_progress (
    request_id uuid NOT NULL REFERENCES economy.reseed_reward_rounds (request_id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    active_seconds bigint NOT NULL CHECK (active_seconds > 0),
    PRIMARY KEY (request_id, user_id)
);

CREATE TABLE economy.reseed_reward_awards (
    request_id uuid PRIMARY KEY REFERENCES economy.reseed_reward_rounds (request_id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    window_start timestamptz NOT NULL
        REFERENCES economy.seeding_reward_evidence_windows (window_start) ON DELETE RESTRICT,
    active_seconds bigint NOT NULL CHECK (active_seconds > 0),
    magic_transaction_id uuid NOT NULL UNIQUE REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT
);

CREATE TRIGGER reseed_reward_awards_immutable
BEFORE UPDATE OR DELETE ON economy.reseed_reward_awards
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

-- +goose StatementBegin
CREATE FUNCTION economy.queue_reseed_reward_window() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, public AS $$
BEGIN
    -- The evidence consumer and swarm consumer can deliver in either order.
    -- The reward worker waits for the swarm watermark through the hour's end.
    INSERT INTO economy.reseed_reward_pending_windows (window_start) VALUES (NEW.window_start);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER reseed_reward_queue_window
AFTER UPDATE OF status ON economy.seeding_reward_evidence_windows
FOR EACH ROW WHEN (OLD.status = 'collecting' AND NEW.status = 'complete')
EXECUTE FUNCTION economy.queue_reseed_reward_window();

REVOKE ALL ON economy.reseed_reward_policies, economy.reseed_reward_rounds, economy.reseed_reward_progress,
    economy.reseed_reward_pending_windows, economy.reseed_reward_awards FROM PUBLIC;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM economy.reseed_reward_awards) THEN
        RAISE EXCEPTION 'reseed rewards have been recorded in the immutable magic ledger';
    END IF;
END $$;
-- +goose StatementEnd
DROP TRIGGER reseed_reward_queue_window ON economy.seeding_reward_evidence_windows;
DROP FUNCTION economy.queue_reseed_reward_window();
DROP TABLE economy.reseed_reward_awards, economy.reseed_reward_progress, economy.reseed_reward_pending_windows;
DROP TABLE economy.reseed_reward_rounds;
DROP TABLE economy.reseed_reward_policies;
DELETE FROM economy.magic_accounts WHERE id = '00000000-0000-7000-8000-000000000015';
