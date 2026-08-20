-- +goose Up

-- Owners may ask to withdraw their own published torrent, but the irreversible
-- tombstone remains a separate staff decision.  The request immediately moves
-- the aggregate to disabled, matching the old PtYes user-visible behaviour,
-- while every object and accounting record remains intact.
INSERT INTO authz.permissions (
    action, description, risk_level, relationship, credential_audience,
    grantable, discoverable
) VALUES
    ('torrent.withdraw.request.self', '申请撤回自己已发布的种子', 'high', 'self', 'web-session', true, true),
    ('torrent.withdraw.review', '审核已发布种子的撤回申请', 'high', 'none', 'staff-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'torrent.withdraw.request.self'),
    ('site_admin', 'torrent.withdraw.review');

CREATE TABLE torrents.torrent_withdrawal_requests (
    id uuid PRIMARY KEY,
    torrent_id bigint NOT NULL
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    uploader_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    torrent_title text NOT NULL CHECK (char_length(btrim(torrent_title)) BETWEEN 1 AND 240),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    expected_torrent_version bigint NOT NULL CHECK (expected_torrent_version > 0),
    disabled_torrent_version bigint NOT NULL CHECK (
        disabled_torrent_version = expected_torrent_version + 1
    ),
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected')),
    version bigint NOT NULL DEFAULT 1 CHECK (version IN (1, 2)),
    authorization_decision_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    decided_at timestamptz,
    UNIQUE (torrent_id, expected_torrent_version),
    CHECK (
        (status = 'pending' AND version = 1 AND decided_at IS NULL)
        OR (status IN ('approved', 'rejected') AND version = 2 AND decided_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX torrent_withdrawal_one_pending_idx
    ON torrents.torrent_withdrawal_requests (torrent_id)
    WHERE status = 'pending';

CREATE INDEX torrent_withdrawal_staff_queue_idx
    ON torrents.torrent_withdrawal_requests (status, created_at, id);

CREATE INDEX torrent_withdrawal_uploader_history_idx
    ON torrents.torrent_withdrawal_requests (uploader_id, created_at DESC, id DESC);

CREATE TABLE torrents.torrent_withdrawal_decisions (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL UNIQUE
        REFERENCES torrents.torrent_withdrawal_requests (id) ON DELETE RESTRICT,
    torrent_id bigint NOT NULL
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    reviewer_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    decision text NOT NULL CHECK (decision IN ('approve', 'reject')),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    expected_request_version bigint NOT NULL CHECK (expected_request_version = 1),
    resulting_request_version bigint NOT NULL CHECK (resulting_request_version = 2),
    expected_torrent_version bigint NOT NULL CHECK (expected_torrent_version > 0),
    resulting_torrent_version bigint NOT NULL CHECK (
        resulting_torrent_version = expected_torrent_version + 1
    ),
    authorization_decision_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL,
    UNIQUE (torrent_id, expected_torrent_version)
);

-- +goose StatementBegin
CREATE FUNCTION torrents.protect_torrent_withdrawal_request()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'torrent withdrawal requests cannot be deleted';
    END IF;
    IF OLD.id IS DISTINCT FROM NEW.id
        OR OLD.torrent_id IS DISTINCT FROM NEW.torrent_id
        OR OLD.uploader_id IS DISTINCT FROM NEW.uploader_id
        OR OLD.torrent_title IS DISTINCT FROM NEW.torrent_title
        OR OLD.reason IS DISTINCT FROM NEW.reason
        OR OLD.expected_torrent_version IS DISTINCT FROM NEW.expected_torrent_version
        OR OLD.disabled_torrent_version IS DISTINCT FROM NEW.disabled_torrent_version
        OR OLD.authorization_decision_id IS DISTINCT FROM NEW.authorization_decision_id
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'torrent withdrawal request evidence is immutable';
    END IF;
    IF OLD.status <> 'pending' OR NEW.status NOT IN ('approved', 'rejected')
        OR NEW.version <> OLD.version + 1 OR NEW.decided_at IS NULL THEN
        RAISE EXCEPTION 'invalid torrent withdrawal request transition';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER torrent_withdrawal_request_guard
BEFORE UPDATE OR DELETE ON torrents.torrent_withdrawal_requests
FOR EACH ROW EXECUTE FUNCTION torrents.protect_torrent_withdrawal_request();

CREATE TRIGGER torrent_withdrawal_decisions_immutable
BEFORE UPDATE OR DELETE ON torrents.torrent_withdrawal_decisions
FOR EACH ROW EXECUTE FUNCTION torrents.reject_immutable_evidence_mutation();

-- +goose Down

DROP TRIGGER torrent_withdrawal_decisions_immutable
    ON torrents.torrent_withdrawal_decisions;
DROP TRIGGER torrent_withdrawal_request_guard
    ON torrents.torrent_withdrawal_requests;
DROP FUNCTION torrents.protect_torrent_withdrawal_request();
DROP TABLE torrents.torrent_withdrawal_decisions;
DROP TABLE torrents.torrent_withdrawal_requests;

DELETE FROM authz.role_permissions
WHERE (role_id = 'member' AND action = 'torrent.withdraw.request.self')
   OR (role_id = 'site_admin' AND action = 'torrent.withdraw.review');
DELETE FROM authz.permissions
WHERE action IN ('torrent.withdraw.request.self', 'torrent.withdraw.review');
