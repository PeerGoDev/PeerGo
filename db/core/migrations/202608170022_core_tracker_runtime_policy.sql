-- +goose Up

-- Tracker request-path policy is an immutable timeline. Capacity limits remain
-- deployment configuration and secrets never enter this table or its signed
-- publication artifact.
CREATE TABLE tracker_control.runtime_policy_revisions (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    revision text NOT NULL UNIQUE CHECK (revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    announce_interval_seconds integer NOT NULL CHECK (announce_interval_seconds BETWEEN 60 AND 86400),
    min_announce_interval_seconds integer NOT NULL CHECK (
        min_announce_interval_seconds BETWEEN 30 AND announce_interval_seconds
    ),
    default_numwant integer NOT NULL CHECK (default_numwant BETWEEN 0 AND 500),
    max_numwant integer NOT NULL CHECK (max_numwant BETWEEN 1 AND 500 AND default_numwant <= max_numwant),
    scrape_enabled boolean NOT NULL,
    max_scrape_hashes integer NOT NULL CHECK (max_scrape_hashes BETWEEN 1 AND 100),
    client_mode text NOT NULL CHECK (client_mode IN ('allow_all', 'allow_list')),
    allowed_clients jsonb NOT NULL CHECK (
        jsonb_typeof(allowed_clients) = 'array'
        AND jsonb_array_length(allowed_clients) <= 16
        AND (
            (client_mode = 'allow_all' AND jsonb_array_length(allowed_clients) = 0)
            OR (client_mode = 'allow_list' AND jsonb_array_length(allowed_clients) > 0)
        )
    ),
    user_requests_per_minute integer NOT NULL CHECK (user_requests_per_minute BETWEEN 1 AND 600),
    user_burst integer NOT NULL CHECK (user_burst BETWEEN 1 AND 1200),
    address_requests_per_minute integer NOT NULL CHECK (address_requests_per_minute BETWEEN 1 AND 5000),
    address_burst integer NOT NULL CHECK (address_burst BETWEEN 1 AND 10000),
    issued_by uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    created_at timestamptz NOT NULL UNIQUE,
    CHECK (
        (issued_by IS NULL AND authorization_decision_id IS NULL)
        OR (issued_by IS NOT NULL AND authorization_decision_id IS NOT NULL)
    )
);

CREATE INDEX tracker_runtime_policy_recent_idx
    ON tracker_control.runtime_policy_revisions (sequence DESC);

-- Existing PtYes-compatible cadence is the safe migration baseline. Client
-- filtering starts permissive; staff can review real client fingerprints
-- before switching to an allowlist without interrupting migrated users.
INSERT INTO tracker_control.runtime_policy_revisions (
    revision, announce_interval_seconds, min_announce_interval_seconds,
    default_numwant, max_numwant, scrape_enabled, max_scrape_hashes,
    client_mode, allowed_clients, user_requests_per_minute, user_burst,
    address_requests_per_minute, address_burst, issued_by,
    authorization_decision_id, reason, created_at
) VALUES (
    'tracker-runtime-default-v1', 1800, 900, 50, 100, true, 50,
    'allow_all', '[]'::jsonb, 30, 60, 120, 240,
    NULL, NULL, 'PeerGo Tracker 上线基线，沿用迁移站点的 announce 节奏',
    '2026-08-17T00:00:02Z'
);

CREATE TRIGGER tracker_runtime_policy_immutable
BEFORE UPDATE OR DELETE ON tracker_control.runtime_policy_revisions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('tracker.policy.issue', '签发 Tracker announce、scrape、客户端与请求频率政策', 'high', 'none', 'staff-session', true, true),
    ('tracker.policy.read', '读取 Tracker 运行政策与签名发布状态', 'medium', 'none', 'staff-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_admin', 'tracker.policy.issue'),
    ('site_admin', 'tracker.policy.read');

REVOKE ALL ON tracker_control.runtime_policy_revisions FROM PUBLIC;

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'site_admin' AND action IN ('tracker.policy.issue', 'tracker.policy.read');
DELETE FROM authz.permissions
WHERE action IN ('tracker.policy.issue', 'tracker.policy.read');
DROP TRIGGER tracker_runtime_policy_immutable ON tracker_control.runtime_policy_revisions;
DROP INDEX tracker_control.tracker_runtime_policy_recent_idx;
DROP TABLE tracker_control.runtime_policy_revisions;
