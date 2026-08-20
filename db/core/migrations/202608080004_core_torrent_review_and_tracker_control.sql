-- +goose Up

CREATE SCHEMA IF NOT EXISTS review;
CREATE SCHEMA IF NOT EXISTS tracker_control;

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'torrent.review',
    '审核单个待发布种子',
    'high',
    'none',
    'staff-session',
    true,
    true
);

INSERT INTO authz.roles (id, name, description, assignable) VALUES (
    'torrent_reviewer',
    '种子审核员',
    '读取待审核种子并作出单对象通过或驳回决定；不包含分类、用户、Tracker 运维或批量处置权限。',
    true
);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('torrent_reviewer', 'torrent.review');

-- A review decision is immutable business evidence. decision_id is supplied as
-- the command idempotency key; the second uniqueness constraint ensures two
-- reviewers cannot both decide the same optimistic aggregate version.
CREATE TABLE review.torrent_decisions (
    id uuid PRIMARY KEY,
    torrent_id bigint NOT NULL
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    torrent_public_id uuid NOT NULL,
    reviewer_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    decision text NOT NULL CHECK (decision IN ('approve', 'reject')),
    reason_code text NOT NULL CHECK (reason_code IN (
        'meets_requirements',
        'metadata_incomplete',
        'duplicate_or_superseded',
        'content_policy_violation',
        'quality_requirements_not_met',
        'uploader_action_required',
        'other'
    )),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    expected_torrent_version bigint NOT NULL CHECK (expected_torrent_version > 0),
    resulting_torrent_version bigint NOT NULL CHECK (
        resulting_torrent_version = expected_torrent_version + 1
    ),
    resulting_state text NOT NULL CHECK (resulting_state IN ('published', 'rejected')),
    authorization_decision_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (torrent_id, expected_torrent_version),
    CHECK (
        (decision = 'approve'
            AND reason_code = 'meets_requirements'
            AND resulting_state = 'published')
        OR
        (decision = 'reject'
            AND reason_code <> 'meets_requirements'
            AND resulting_state = 'rejected')
    )
);

CREATE INDEX torrent_decisions_torrent_history_idx
    ON review.torrent_decisions (torrent_id, occurred_at DESC, id DESC);

-- +goose StatementBegin
CREATE FUNCTION review.reject_torrent_decision_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'torrent review decisions are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER torrent_decisions_immutable
BEFORE UPDATE OR DELETE ON review.torrent_decisions
FOR EACH ROW EXECUTE FUNCTION review.reject_torrent_decision_mutation();

-- The Core transaction appends an eligibility event instead of calling a
-- Tracker synchronously. sequence is the control-plane ordering watermark;
-- event bytes remain immutable while the projector may update lease metadata.
CREATE TABLE tracker_control.outbox (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY CHECK (sequence > 0),
    event_id uuid NOT NULL UNIQUE,
    event_type text NOT NULL CHECK (
        event_type = 'tracker.torrent-eligibility.changed'
    ),
    schema_version text NOT NULL CHECK (schema_version = '1.0.0'),
    aggregate_id bigint NOT NULL
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    aggregate_version bigint NOT NULL CHECK (aggregate_version > 0),
    occurred_at timestamptz NOT NULL,
    payload_json text NOT NULL CHECK (
        octet_length(payload_json) BETWEEN 2 AND 65536
        AND jsonb_typeof(payload_json::jsonb) = 'object'
    ),
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    available_at timestamptz NOT NULL,
    lease_token uuid,
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error_code text CHECK (
        last_error_code IS NULL
        OR last_error_code ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    projected_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (aggregate_id, aggregate_version),
    CHECK ((lease_token IS NULL) = (lease_until IS NULL)),
    CHECK (projected_at IS NULL OR (lease_token IS NULL AND last_error_code IS NULL))
);

CREATE INDEX tracker_control_outbox_pending_idx
    ON tracker_control.outbox (sequence)
    WHERE projected_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION tracker_control.protect_outbox_evidence()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'Tracker control outbox evidence cannot be deleted';
    END IF;

    IF OLD.sequence IS DISTINCT FROM NEW.sequence
        OR OLD.event_id IS DISTINCT FROM NEW.event_id
        OR OLD.event_type IS DISTINCT FROM NEW.event_type
        OR OLD.schema_version IS DISTINCT FROM NEW.schema_version
        OR OLD.aggregate_id IS DISTINCT FROM NEW.aggregate_id
        OR OLD.aggregate_version IS DISTINCT FROM NEW.aggregate_version
        OR OLD.occurred_at IS DISTINCT FROM NEW.occurred_at
        OR OLD.payload_json IS DISTINCT FROM NEW.payload_json
        OR OLD.payload_sha256 IS DISTINCT FROM NEW.payload_sha256
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'Tracker control outbox evidence is immutable';
    END IF;

    IF OLD.projected_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN
        RAISE EXCEPTION 'projected Tracker control events are terminal';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER tracker_control_outbox_evidence_immutable
BEFORE UPDATE OR DELETE ON tracker_control.outbox
FOR EACH ROW EXECUTE FUNCTION tracker_control.protect_outbox_evidence();

-- This is the Core control-plane projection used to build Tracker snapshots.
-- Tracker Edge will load a signed snapshot/incremental stream and never query
-- this table (or any other Core table) from announce/scrape.
CREATE TABLE tracker_control.torrent_allowlist_projection (
    torrent_id bigint PRIMARY KEY
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    torrent_public_id uuid NOT NULL UNIQUE,
    info_hash_v1 bytea NOT NULL UNIQUE CHECK (octet_length(info_hash_v1) = 20),
    total_size_bytes bigint NOT NULL CHECK (total_size_bytes > 0),
    enabled boolean NOT NULL,
    torrent_version bigint NOT NULL CHECK (torrent_version > 0),
    control_sequence bigint NOT NULL UNIQUE CHECK (control_sequence > 0),
    updated_at timestamptz NOT NULL
);

-- A single row exposes a contiguous applied watermark. The projector only
-- claims the earliest unprojected outbox row, so a later sequence cannot make
-- a snapshot appear current while an earlier event is still retrying.
CREATE TABLE tracker_control.projection_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    last_sequence bigint NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    updated_at timestamptz
);

INSERT INTO tracker_control.projection_state (singleton, last_sequence)
VALUES (true, 0);

-- +goose StatementBegin
CREATE FUNCTION tracker_control.protect_allowlist_projection()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'Tracker allowlist projection uses explicit disabled entries';
    END IF;

    IF OLD.torrent_id IS DISTINCT FROM NEW.torrent_id
        OR OLD.torrent_public_id IS DISTINCT FROM NEW.torrent_public_id
        OR OLD.info_hash_v1 IS DISTINCT FROM NEW.info_hash_v1
        OR OLD.total_size_bytes IS DISTINCT FROM NEW.total_size_bytes THEN
        RAISE EXCEPTION 'Tracker allowlist swarm identity is immutable';
    END IF;

    IF NEW.torrent_version <= OLD.torrent_version
        OR NEW.control_sequence <= OLD.control_sequence
        OR NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'Tracker allowlist projection must advance monotonically';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER tracker_allowlist_projection_monotonic
BEFORE UPDATE OR DELETE ON tracker_control.torrent_allowlist_projection
FOR EACH ROW EXECUTE FUNCTION tracker_control.protect_allowlist_projection();

-- +goose Down
DELETE FROM authz.grants WHERE role_id = 'torrent_reviewer';
DELETE FROM authz.role_permissions WHERE role_id = 'torrent_reviewer';
DELETE FROM authz.roles WHERE id = 'torrent_reviewer';
DELETE FROM authz.permissions WHERE action = 'torrent.review';

DROP SCHEMA IF EXISTS tracker_control CASCADE;
DROP SCHEMA IF EXISTS review CASCADE;
