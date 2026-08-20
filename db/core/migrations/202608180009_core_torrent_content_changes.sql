-- +goose Up

INSERT INTO authz.permissions (
    action, description, risk_level, relationship, credential_audience,
    grantable, discoverable
) VALUES
    ('torrent.content.change.review', '审核已发布种子的内容资料修改', 'high', 'none', 'staff-session', true, true),
    ('torrent.content.change.submit.self', '提交自己已发布种子的内容资料修改', 'medium', 'self', 'web-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'torrent.content.change.submit.self'),
    ('site_admin', 'torrent.content.change.review'),
    ('torrent_reviewer', 'torrent.content.change.review');

-- Align the original aggregate constraint with the existing 16 MiB domain and
-- OpenAPI MediaInfo limit. Previously the service accepted 16 MiB while an
-- unnamed historical check rejected values above 4 MiB at commit time.
ALTER TABLE torrents.torrents
    DROP CONSTRAINT IF EXISTS torrents_media_info_check,
    ADD CONSTRAINT torrents_media_info_byte_limit
        CHECK (octet_length(media_info) <= 16777216);

-- The currently published content stays live while a candidate is reviewed.
-- Both sides are frozen so an approval can prove exactly what was replaced;
-- price, promotion, swarm identity, object locations and screenshots are not
-- part of this command surface.
CREATE TABLE torrents.torrent_content_change_requests (
    id uuid PRIMARY KEY,
    torrent_id bigint NOT NULL
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    uploader_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    base_torrent_version bigint NOT NULL CHECK (base_torrent_version > 0),
    base_content_sha256 bytea NOT NULL CHECK (octet_length(base_content_sha256) = 32),
    base_description text NOT NULL CHECK (octet_length(base_description) <= 4194304),
    base_media_info text NOT NULL CHECK (octet_length(base_media_info) <= 16777216),
    candidate_description text NOT NULL CHECK (
        char_length(btrim(candidate_description)) >= 1
        AND octet_length(candidate_description) <= 4194304
    ),
    candidate_media_info text NOT NULL CHECK (octet_length(candidate_media_info) <= 16777216),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected')),
    version bigint NOT NULL DEFAULT 1 CHECK (version IN (1, 2)),
    authorization_decision_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    decided_at timestamptz,
    CHECK (
        (status = 'pending' AND version = 1 AND decided_at IS NULL)
        OR (status IN ('approved', 'rejected') AND version = 2 AND decided_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX torrent_content_change_one_pending_idx
    ON torrents.torrent_content_change_requests (torrent_id)
    WHERE status = 'pending';

CREATE INDEX torrent_content_change_staff_queue_idx
    ON torrents.torrent_content_change_requests (status, created_at, id);

CREATE INDEX torrent_content_change_uploader_history_idx
    ON torrents.torrent_content_change_requests (uploader_id, created_at DESC, id DESC);

CREATE TABLE torrents.torrent_content_change_identifiers (
    request_id uuid NOT NULL
        REFERENCES torrents.torrent_content_change_requests (id) ON DELETE RESTRICT,
    revision_side text NOT NULL CHECK (revision_side IN ('base', 'candidate')),
    provider text NOT NULL
        CHECK (provider IN ('imdb', 'tmdb', 'douban', 'bangumi', 'steam')),
    external_id text NOT NULL CHECK (char_length(external_id) BETWEEN 1 AND 64),
    PRIMARY KEY (request_id, revision_side, provider),
    CHECK (
        (provider = 'imdb' AND external_id ~ '^tt[0-9]{7,10}$')
        OR (provider IN ('tmdb', 'douban', 'bangumi', 'steam')
            AND external_id ~ '^[0-9]{1,20}$')
    )
);

CREATE TABLE torrents.torrent_content_change_decisions (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL UNIQUE
        REFERENCES torrents.torrent_content_change_requests (id) ON DELETE RESTRICT,
    reviewer_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    decision text NOT NULL CHECK (decision IN ('approve', 'reject')),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    expected_request_version bigint NOT NULL CHECK (expected_request_version = 1),
    resulting_request_version bigint NOT NULL CHECK (resulting_request_version = 2),
    observed_torrent_version bigint NOT NULL CHECK (observed_torrent_version > 0),
    resulting_torrent_version bigint NOT NULL CHECK (resulting_torrent_version > 0),
    authorization_decision_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL,
    CHECK (
        (decision = 'approve' AND resulting_torrent_version = observed_torrent_version + 1)
        OR (decision = 'reject' AND resulting_torrent_version = observed_torrent_version)
    )
);

-- +goose StatementBegin
CREATE FUNCTION torrents.protect_torrent_content_change_request()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'torrent content change requests cannot be deleted';
    END IF;
    IF OLD.id IS DISTINCT FROM NEW.id
        OR OLD.torrent_id IS DISTINCT FROM NEW.torrent_id
        OR OLD.uploader_id IS DISTINCT FROM NEW.uploader_id
        OR OLD.base_torrent_version IS DISTINCT FROM NEW.base_torrent_version
        OR OLD.base_content_sha256 IS DISTINCT FROM NEW.base_content_sha256
        OR OLD.base_description IS DISTINCT FROM NEW.base_description
        OR OLD.base_media_info IS DISTINCT FROM NEW.base_media_info
        OR OLD.candidate_description IS DISTINCT FROM NEW.candidate_description
        OR OLD.candidate_media_info IS DISTINCT FROM NEW.candidate_media_info
        OR OLD.reason IS DISTINCT FROM NEW.reason
        OR OLD.authorization_decision_id IS DISTINCT FROM NEW.authorization_decision_id
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'torrent content change candidate is immutable';
    END IF;
    IF OLD.status <> 'pending' OR NEW.status NOT IN ('approved', 'rejected')
        OR NEW.version <> OLD.version + 1 OR NEW.decided_at IS NULL THEN
        RAISE EXCEPTION 'invalid torrent content change transition';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER torrent_content_change_request_guard
BEFORE UPDATE OR DELETE ON torrents.torrent_content_change_requests
FOR EACH ROW EXECUTE FUNCTION torrents.protect_torrent_content_change_request();

CREATE TRIGGER torrent_content_change_identifiers_immutable
BEFORE UPDATE OR DELETE ON torrents.torrent_content_change_identifiers
FOR EACH ROW EXECUTE FUNCTION torrents.reject_immutable_evidence_mutation();

CREATE TRIGGER torrent_content_change_decisions_immutable
BEFORE UPDATE OR DELETE ON torrents.torrent_content_change_decisions
FOR EACH ROW EXECUTE FUNCTION torrents.reject_immutable_evidence_mutation();

-- +goose Down

DROP TRIGGER torrent_content_change_decisions_immutable
    ON torrents.torrent_content_change_decisions;
DROP TRIGGER torrent_content_change_identifiers_immutable
    ON torrents.torrent_content_change_identifiers;
DROP TRIGGER torrent_content_change_request_guard
    ON torrents.torrent_content_change_requests;
DROP FUNCTION torrents.protect_torrent_content_change_request();
DROP TABLE torrents.torrent_content_change_decisions;
DROP TABLE torrents.torrent_content_change_identifiers;
DROP TABLE torrents.torrent_content_change_requests;

ALTER TABLE torrents.torrents
    DROP CONSTRAINT torrents_media_info_byte_limit,
    ADD CONSTRAINT torrents_media_info_check
        CHECK (octet_length(media_info) <= 4194304);

DELETE FROM authz.role_permissions
WHERE (role_id = 'member' AND action = 'torrent.content.change.submit.self')
   OR (role_id IN ('site_admin', 'torrent_reviewer') AND action = 'torrent.content.change.review');
DELETE FROM authz.permissions
WHERE action IN ('torrent.content.change.review', 'torrent.content.change.submit.self');
