-- +goose Up

-- Core stores only the user-safe latest H&R projection. Tracker completion
-- identities, session evidence and policy provenance remain in Tracker Ledger.
CREATE TABLE traffic.hnr_projection_inbox (
    event_id uuid PRIMARY KEY,
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    payload_json text NOT NULL CHECK (
        octet_length(payload_json) BETWEEN 2 AND 8192
        AND jsonb_typeof(payload_json::jsonb) = 'object'
    ),
    obligation_id uuid NOT NULL,
    obligation_version bigint NOT NULL CHECK (obligation_version > 0),
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL,
    applied_at timestamptz NOT NULL,
    UNIQUE (obligation_id, obligation_version),
    CHECK (applied_at >= received_at)
);

CREATE TABLE traffic.user_hnr_obligations (
    obligation_id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    torrent_id bigint NOT NULL REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    completed_at timestamptz NOT NULL,
    state text NOT NULL CHECK (state IN ('tracking', 'satisfied', 'exempt')),
    seeded_seconds bigint NOT NULL CHECK (seeded_seconds >= 0),
    required_seed_seconds bigint NOT NULL CHECK (required_seed_seconds >= 0),
    raw_uploaded bigint NOT NULL CHECK (raw_uploaded >= 0),
    raw_downloaded bigint NOT NULL CHECK (raw_downloaded >= 0),
    raw_ratio_basis_points bigint NOT NULL CHECK (raw_ratio_basis_points >= 0),
    required_ratio_basis_points bigint NOT NULL CHECK (required_ratio_basis_points >= 0),
    assessment_due_at timestamptz NOT NULL,
    grace_ends_at timestamptz NOT NULL,
    satisfied_by text CHECK (satisfied_by IS NULL OR satisfied_by IN ('seed_time', 'raw_ratio', 'exempt')),
    satisfied_at timestamptz,
    version bigint NOT NULL CHECK (version > 0),
    occurred_at timestamptz NOT NULL,
    applied_at timestamptz NOT NULL,
    CHECK (assessment_due_at >= completed_at),
    CHECK (grace_ends_at >= assessment_due_at),
    CHECK (
        (state = 'tracking' AND satisfied_by IS NULL AND satisfied_at IS NULL)
        OR (state = 'satisfied' AND satisfied_by IN ('seed_time', 'raw_ratio') AND satisfied_at IS NOT NULL)
        OR (state = 'exempt' AND satisfied_by = 'exempt' AND satisfied_at = completed_at)
    )
);

CREATE INDEX user_hnr_obligations_user_recent_idx
    ON traffic.user_hnr_obligations (user_id, completed_at DESC, obligation_id DESC);

CREATE INDEX user_hnr_obligations_user_open_idx
    ON traffic.user_hnr_obligations (user_id, grace_ends_at, obligation_id)
    WHERE state = 'tracking';

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'hnr.read.self',
    '查看自己的 H&R 义务与达标进度',
    'low',
    'self',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'hnr.read.self');

-- +goose StatementBegin
CREATE FUNCTION traffic.protect_hnr_projection_inbox()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'Core H&R projection inbox is immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER hnr_projection_inbox_immutable
BEFORE UPDATE OR DELETE ON traffic.hnr_projection_inbox
FOR EACH ROW EXECUTE FUNCTION traffic.protect_hnr_projection_inbox();

-- +goose StatementBegin
CREATE FUNCTION traffic.protect_hnr_projection_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'Core H&R projection cannot be deleted';
    END IF;
    IF OLD.obligation_id IS DISTINCT FROM NEW.obligation_id
        OR OLD.user_id IS DISTINCT FROM NEW.user_id
        OR OLD.torrent_id IS DISTINCT FROM NEW.torrent_id
        OR OLD.completed_at IS DISTINCT FROM NEW.completed_at
        OR OLD.required_seed_seconds IS DISTINCT FROM NEW.required_seed_seconds
        OR OLD.raw_downloaded IS DISTINCT FROM NEW.raw_downloaded
        OR OLD.required_ratio_basis_points IS DISTINCT FROM NEW.required_ratio_basis_points
        OR OLD.assessment_due_at IS DISTINCT FROM NEW.assessment_due_at
        OR OLD.grace_ends_at IS DISTINCT FROM NEW.grace_ends_at THEN
        RAISE EXCEPTION 'Core H&R obligation identity and requirements are immutable';
    END IF;
    IF NEW.version <> OLD.version + 1
        OR NEW.seeded_seconds < OLD.seeded_seconds
        OR NEW.raw_uploaded < OLD.raw_uploaded
        OR NEW.raw_ratio_basis_points < OLD.raw_ratio_basis_points
        OR NEW.occurred_at < OLD.occurred_at
        OR NEW.applied_at < OLD.applied_at THEN
        RAISE EXCEPTION 'Core H&R projection must advance monotonically';
    END IF;
    IF OLD.state <> 'tracking' OR NEW.state NOT IN ('tracking', 'satisfied') THEN
        RAISE EXCEPTION 'Core H&R terminal state cannot change';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER user_hnr_projection_monotonic
BEFORE UPDATE OR DELETE ON traffic.user_hnr_obligations
FOR EACH ROW EXECUTE FUNCTION traffic.protect_hnr_projection_transition();

-- +goose Down
DROP TRIGGER IF EXISTS user_hnr_projection_monotonic ON traffic.user_hnr_obligations;
DROP TRIGGER IF EXISTS hnr_projection_inbox_immutable ON traffic.hnr_projection_inbox;
DROP FUNCTION IF EXISTS traffic.protect_hnr_projection_transition();
DROP FUNCTION IF EXISTS traffic.protect_hnr_projection_inbox();
DELETE FROM authz.role_permissions
WHERE role_id = 'member' AND action = 'hnr.read.self';
DELETE FROM authz.permissions WHERE action = 'hnr.read.self';
DROP TABLE IF EXISTS traffic.user_hnr_obligations;
DROP TABLE IF EXISTS traffic.hnr_projection_inbox;
