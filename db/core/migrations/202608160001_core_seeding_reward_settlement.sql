-- +goose Up

-- A reward must use the benefits that were effective at the beginning of its
-- closed UTC hour.  Current account state is deliberately not queried during
-- replay.  The opening row gives every account a known post-cutover baseline;
-- future VIP/medal writers must append a revision instead of rewriting it.
CREATE TABLE identity.user_reward_benefit_revisions (
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    revision bigint NOT NULL CHECK (revision > 0),
    effective_from timestamptz NOT NULL,
    vip_enabled boolean NOT NULL,
    vip_until timestamptz,
    medal_bonus_bps bigint NOT NULL DEFAULT 0 CHECK (medal_bonus_bps BETWEEN 0 AND 100000),
    source_kind text NOT NULL CHECK (source_kind IN ('cutover_opening', 'runtime')),
    source_reference text NOT NULL CHECK (
        source_reference ~ '^[a-z0-9][a-z0-9:._-]{0,127}$'
    ),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, revision),
    UNIQUE (user_id, effective_from),
    CHECK (
        (source_kind = 'cutover_opening' AND revision = 1 AND effective_from = '-infinity'::timestamptz)
        OR (source_kind = 'runtime' AND effective_from = date_trunc('hour', effective_from) AND created_at <= effective_from)
    )
);

-- +goose StatementBegin
CREATE FUNCTION identity.require_user_reward_benefit_append()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    latest_revision bigint;
    latest_effective_from timestamptz;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('peergo-user-reward-benefit:' || NEW.user_id::text, 0));
    SELECT revision, effective_from
    INTO latest_revision, latest_effective_from
    FROM identity.user_reward_benefit_revisions
    WHERE user_id = NEW.user_id
    ORDER BY revision DESC
    LIMIT 1;

    IF latest_revision IS NOT NULL AND (
        NEW.revision <> latest_revision + 1
        OR NEW.effective_from <= latest_effective_from
    ) THEN
        RAISE EXCEPTION 'user reward benefit timeline must append in order';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER user_reward_benefit_append_only
BEFORE INSERT ON identity.user_reward_benefit_revisions
FOR EACH ROW EXECUTE FUNCTION identity.require_user_reward_benefit_append();

CREATE TRIGGER user_reward_benefit_immutable
BEFORE UPDATE OR DELETE ON identity.user_reward_benefit_revisions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

-- New native PeerGo accounts start with an explicit no-benefit opening.  This
-- trigger is intentionally narrow; a later VIP/medal command appends revision
-- 2+ through its own service transaction.
-- +goose StatementBegin
CREATE FUNCTION identity.open_user_reward_benefits()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO identity.user_reward_benefit_revisions (
        user_id, revision, effective_from, vip_enabled, vip_until,
        medal_bonus_bps, source_kind, source_reference, created_at
    ) VALUES (
        NEW.id, 1, '-infinity', false, NULL,
        0, 'cutover_opening', 'native-opening:' || NEW.id::text, clock_timestamp()
    );
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER identity_user_reward_benefit_opening
AFTER INSERT ON identity.users
FOR EACH ROW EXECUTE FUNCTION identity.open_user_reward_benefits();

INSERT INTO identity.user_reward_benefit_revisions (
    user_id, revision, effective_from, vip_enabled, vip_until,
    medal_bonus_bps, source_kind, source_reference, created_at
)
SELECT
    users.id,
    1,
    '-infinity',
    COALESCE(access.vip_enabled, false),
    access.vip_until,
    0,
    'cutover_opening',
    CASE
        WHEN mapping.legacy_user_id IS NOT NULL THEN 'rousi-opening:' || mapping.legacy_user_id::text
        ELSE 'native-opening:' || users.id::text
    END,
    clock_timestamp()
FROM identity.users AS users
LEFT JOIN identity.user_access_states AS access ON access.user_id = users.id
LEFT JOIN migration.user_id_map AS mapping
  ON mapping.user_id = users.id AND mapping.source_system = 'ptyes'
ORDER BY users.id;

-- PtYes's level reward table is copied as explicit integer policy data.  Only
-- levels present in PeerGo's current rousi-v1 level policy are seeded here.
CREATE TABLE progression.seeding_reward_level_benefits (
    policy_version text NOT NULL,
    level smallint NOT NULL,
    karma_bonus_bps bigint NOT NULL CHECK (karma_bonus_bps BETWEEN 0 AND 100000),
    seeding_count_bonus integer NOT NULL CHECK (seeding_count_bonus BETWEEN 0 AND 100000),
    PRIMARY KEY (policy_version, level),
    FOREIGN KEY (policy_version, level)
        REFERENCES progression.level_definitions (policy_version, level)
        ON DELETE RESTRICT
);

INSERT INTO progression.seeding_reward_level_benefits (
    policy_version, level, karma_bonus_bps, seeding_count_bonus
) VALUES
    ('rousi-v1', 1,    0,  0),
    ('rousi-v1', 2,  200,  0),
    ('rousi-v1', 3,  400,  5),
    ('rousi-v1', 4,  600, 10),
    ('rousi-v1', 5,  800, 15),
    ('rousi-v1', 6, 1000, 20),
    ('rousi-v1', 7, 1300, 30),
    ('rousi-v1', 8, 1600, 40),
    ('rousi-v1', 9, 2000, 55);

CREATE TRIGGER seeding_reward_level_benefits_immutable
BEFORE UPDATE OR DELETE ON progression.seeding_reward_level_benefits
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

-- Every seeding policy also defines the exact experience conversion for that
-- source.  Keeping the same revision and digest lets the atomic reward writer
-- prove that magic and experience used one signed formula.
-- +goose StatementBegin
CREATE FUNCTION progression.open_seeding_reward_experience_policy()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO progression.experience_policy_revisions (
        revision, source_kind, effective_from, payload_sha256, created_at
    ) VALUES (
        NEW.revision, 'seeding_reward', NEW.effective_from,
        NEW.snapshot_sha256, NEW.created_at
    );
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER seeding_reward_experience_policy_opening
AFTER INSERT ON economy.seeding_reward_policy_revisions
FOR EACH ROW EXECUTE FUNCTION progression.open_seeding_reward_experience_policy();

INSERT INTO progression.experience_policy_revisions (
    revision, source_kind, effective_from, payload_sha256, created_at
)
SELECT
    policy.revision, 'seeding_reward', policy.effective_from,
    policy.snapshot_sha256, policy.created_at
FROM economy.seeding_reward_policy_revisions AS policy
ON CONFLICT (revision) DO NOTHING;

-- Enrichment is copied into immutable per-window snapshots before calculation.
-- Torrent size and first publication time are already immutable in torrents,
-- but the copy and digest make an individual reward independently replayable.
CREATE TABLE economy.seeding_reward_metadata_snapshots (
    window_start timestamptz NOT NULL,
    torrent_id bigint NOT NULL REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    size_bytes bigint NOT NULL CHECK (size_bytes > 0),
    published_at timestamptz NOT NULL,
    official boolean NOT NULL,
    metadata_sha256 bytea NOT NULL CHECK (octet_length(metadata_sha256) = 32),
    captured_at timestamptz NOT NULL,
    PRIMARY KEY (window_start, torrent_id),
    FOREIGN KEY (window_start)
        REFERENCES economy.seeding_reward_evidence_windows (window_start)
        ON DELETE RESTRICT,
    CHECK (published_at <= window_start + interval '1 hour')
);

CREATE TABLE economy.seeding_reward_benefit_snapshots (
    window_start timestamptz NOT NULL,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    entitlement_revision bigint NOT NULL,
    level_policy_version text NOT NULL,
    level smallint NOT NULL,
    vip_active boolean NOT NULL,
    medal_bonus_bps bigint NOT NULL CHECK (medal_bonus_bps BETWEEN 0 AND 100000),
    level_bonus_bps bigint NOT NULL CHECK (level_bonus_bps BETWEEN 0 AND 100000),
    level_seeding_count_bonus integer NOT NULL CHECK (level_seeding_count_bonus BETWEEN 0 AND 100000),
    benefit_revision text NOT NULL CHECK (
        benefit_revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    benefit_sha256 bytea NOT NULL CHECK (octet_length(benefit_sha256) = 32),
    captured_at timestamptz NOT NULL,
    PRIMARY KEY (window_start, user_id),
    FOREIGN KEY (window_start)
        REFERENCES economy.seeding_reward_evidence_windows (window_start)
        ON DELETE RESTRICT,
    FOREIGN KEY (user_id, entitlement_revision)
        REFERENCES identity.user_reward_benefit_revisions (user_id, revision)
        ON DELETE RESTRICT,
    FOREIGN KEY (level_policy_version, level)
        REFERENCES progression.seeding_reward_level_benefits (policy_version, level)
        ON DELETE RESTRICT
);

CREATE TABLE economy.seeding_reward_calculations (
    window_start timestamptz NOT NULL,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    policy_revision text NOT NULL
        REFERENCES economy.seeding_reward_policy_revisions (revision) ON DELETE RESTRICT,
    calculation_sha256 bytea NOT NULL CHECK (octet_length(calculation_sha256) = 32),
    eligible_torrent_count integer NOT NULL CHECK (eligible_torrent_count >= 0),
    value_score_micro bigint NOT NULL CHECK (value_score_micro >= 0),
    curve_reward_milli bigint NOT NULL CHECK (curve_reward_milli >= 0),
    linear_reward_milli bigint NOT NULL CHECK (linear_reward_milli >= 0),
    base_reward_milli bigint NOT NULL CHECK (base_reward_milli >= 0),
    vip_bonus_milli bigint NOT NULL CHECK (vip_bonus_milli >= 0),
    medal_bonus_milli bigint NOT NULL CHECK (medal_bonus_milli >= 0),
    level_bonus_milli bigint NOT NULL CHECK (level_bonus_milli >= 0),
    uncapped_reward bigint NOT NULL CHECK (uncapped_reward >= 0),
    reward bigint NOT NULL CHECK (reward >= 0),
    experience_amount numeric(38, 20) NOT NULL CHECK (experience_amount >= 0),
    capped boolean NOT NULL,
    magic_transaction_id uuid REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    experience_entry_id uuid REFERENCES progression.experience_entries (id) ON DELETE RESTRICT,
    calculated_at timestamptz NOT NULL,
    PRIMARY KEY (window_start, user_id),
    UNIQUE (calculation_sha256),
    CHECK (
        (reward = 0 AND magic_transaction_id IS NULL AND experience_entry_id IS NULL AND experience_amount = 0)
        OR (
            reward > 0
            AND magic_transaction_id IS NOT NULL
            AND (
                (experience_amount = 0 AND experience_entry_id IS NULL)
                OR (experience_amount > 0 AND experience_entry_id IS NOT NULL)
            )
        )
    )
);

CREATE TABLE economy.seeding_reward_work_items (
    window_start timestamptz NOT NULL,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'completed', 'dead')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 1000000),
    available_at timestamptz NOT NULL,
    lease_token uuid,
    lease_until timestamptz,
    last_error_code text CHECK (
        last_error_code IS NULL OR last_error_code ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    last_error_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (window_start, user_id),
    FOREIGN KEY (window_start)
        REFERENCES economy.seeding_reward_evidence_windows (window_start)
        ON DELETE RESTRICT,
    CHECK ((status = 'processing') = (lease_token IS NOT NULL AND lease_until IS NOT NULL)),
    CHECK ((status = 'completed') = (completed_at IS NOT NULL)),
    CHECK (last_error_at IS NOT NULL OR last_error_code IS NULL)
);

CREATE INDEX seeding_reward_work_ready_idx
    ON economy.seeding_reward_work_items (available_at, window_start, user_id)
    WHERE status IN ('pending', 'processing');

-- +goose StatementBegin
CREATE FUNCTION economy.queue_completed_seeding_reward_window()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status = 'collecting' AND NEW.status = 'complete' THEN
        INSERT INTO economy.seeding_reward_work_items (
            window_start, user_id, available_at, created_at, updated_at
        )
        SELECT NEW.window_start, item.user_id, NEW.completed_at, NEW.completed_at, NEW.completed_at
        FROM economy.seeding_reward_evidence_items AS item
        WHERE item.window_start = NEW.window_start
        GROUP BY item.user_id
        ON CONFLICT (window_start, user_id) DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER seeding_reward_window_completed_queue
AFTER UPDATE OF status ON economy.seeding_reward_evidence_windows
FOR EACH ROW EXECUTE FUNCTION economy.queue_completed_seeding_reward_window();

INSERT INTO economy.seeding_reward_work_items (
    window_start, user_id, available_at, created_at, updated_at
)
SELECT
    evidence_window.window_start,
    item.user_id,
    evidence_window.completed_at,
    evidence_window.completed_at,
    evidence_window.completed_at
FROM economy.seeding_reward_evidence_windows AS evidence_window
JOIN economy.seeding_reward_evidence_items AS item
  ON item.window_start = evidence_window.window_start
WHERE evidence_window.status = 'complete'
GROUP BY evidence_window.window_start, item.user_id, evidence_window.completed_at
ON CONFLICT (window_start, user_id) DO NOTHING;

CREATE TRIGGER seeding_reward_metadata_snapshots_immutable
BEFORE UPDATE OR DELETE ON economy.seeding_reward_metadata_snapshots
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();
CREATE TRIGGER seeding_reward_benefit_snapshots_immutable
BEFORE UPDATE OR DELETE ON economy.seeding_reward_benefit_snapshots
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();
CREATE TRIGGER seeding_reward_calculations_immutable
BEFORE UPDATE OR DELETE ON economy.seeding_reward_calculations
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

REVOKE ALL ON identity.user_reward_benefit_revisions FROM PUBLIC;
REVOKE ALL ON progression.seeding_reward_level_benefits FROM PUBLIC;
REVOKE ALL ON economy.seeding_reward_metadata_snapshots FROM PUBLIC;
REVOKE ALL ON economy.seeding_reward_benefit_snapshots FROM PUBLIC;
REVOKE ALL ON economy.seeding_reward_calculations FROM PUBLIC;
REVOKE ALL ON economy.seeding_reward_work_items FROM PUBLIC;

-- +goose Down

DROP TRIGGER seeding_reward_calculations_immutable ON economy.seeding_reward_calculations;
DROP TRIGGER seeding_reward_benefit_snapshots_immutable ON economy.seeding_reward_benefit_snapshots;
DROP TRIGGER seeding_reward_metadata_snapshots_immutable ON economy.seeding_reward_metadata_snapshots;
DROP TRIGGER seeding_reward_window_completed_queue ON economy.seeding_reward_evidence_windows;
DROP FUNCTION economy.queue_completed_seeding_reward_window();
DROP INDEX economy.seeding_reward_work_ready_idx;
DROP TABLE economy.seeding_reward_work_items;
DROP TABLE economy.seeding_reward_calculations;
DROP TABLE economy.seeding_reward_benefit_snapshots;
DROP TABLE economy.seeding_reward_metadata_snapshots;
DROP TRIGGER seeding_reward_experience_policy_opening ON economy.seeding_reward_policy_revisions;
DROP FUNCTION progression.open_seeding_reward_experience_policy();
DROP TRIGGER seeding_reward_level_benefits_immutable ON progression.seeding_reward_level_benefits;
DROP TABLE progression.seeding_reward_level_benefits;
DROP TRIGGER identity_user_reward_benefit_opening ON identity.users;
DROP FUNCTION identity.open_user_reward_benefits();
DROP TRIGGER user_reward_benefit_immutable ON identity.user_reward_benefit_revisions;
DROP TRIGGER user_reward_benefit_append_only ON identity.user_reward_benefit_revisions;
DROP FUNCTION identity.require_user_reward_benefit_append();
DROP TABLE identity.user_reward_benefit_revisions;
