-- +goose Up

INSERT INTO authz.permissions (
    action, description, risk_level, relationship, credential_audience,
    grantable, discoverable
) VALUES
    ('torrent.upload.policy.issue', '签发新种子上传与截图限制版本', 'high', 'none', 'staff-session', true, true),
    ('torrent.screenshot.change.review', '审核已发布种子的截图附件修改', 'high', 'none', 'staff-session', true, true),
    ('torrent.screenshot.change.submit.self', '提交自己已发布种子的截图附件修改', 'medium', 'self', 'web-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_admin', 'torrent.upload.policy.issue'),
    ('site_admin', 'torrent.screenshot.change.review'),
    ('torrent_reviewer', 'torrent.screenshot.change.review'),
    ('member', 'torrent.screenshot.change.submit.self');

-- Business limits are an immutable effective-time policy. Hard parser/image
-- safety ceilings remain in Core code, so a staff revision cannot expand the
-- attack surface beyond the bounds reviewed with the binary.
CREATE TABLE torrents.torrent_upload_policy_revisions (
    id uuid PRIMARY KEY,
    sequence bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    request_id uuid UNIQUE,
    effective_at timestamptz NOT NULL,
    metainfo_max_bytes integer NOT NULL CHECK (
        metainfo_max_bytes BETWEEN 65536 AND 16777216
    ),
    max_files integer NOT NULL CHECK (max_files BETWEEN 1 AND 100000),
    screenshot_max_count smallint NOT NULL CHECK (
        screenshot_max_count BETWEEN 0 AND 6
    ),
    screenshot_max_bytes integer NOT NULL CHECK (
        screenshot_max_bytes BETWEEN 65536 AND 2097152
    ),
    screenshot_max_pixels integer NOT NULL CHECK (
        screenshot_max_pixels BETWEEN 65536 AND 25000000
    ),
    screenshot_formats text[] NOT NULL CHECK (
        cardinality(screenshot_formats) BETWEEN 1 AND 3
        AND screenshot_formats <@ ARRAY['jpeg','png','webp']::text[]
    ),
    issued_by uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    created_at timestamptz NOT NULL,
    CHECK (
        (issued_by IS NULL AND authorization_decision_id IS NULL AND request_id IS NULL)
        OR
        (issued_by IS NOT NULL AND authorization_decision_id IS NOT NULL AND request_id IS NOT NULL)
    )
);

CREATE INDEX torrent_upload_policy_effective_idx
    ON torrents.torrent_upload_policy_revisions (effective_at DESC, sequence DESC);

CREATE TRIGGER torrent_upload_policy_revisions_immutable
BEFORE UPDATE OR DELETE ON torrents.torrent_upload_policy_revisions
FOR EACH ROW EXECUTE FUNCTION torrents.reject_immutable_evidence_mutation();

INSERT INTO torrents.torrent_upload_policy_revisions (
    id, effective_at, metainfo_max_bytes, max_files,
    screenshot_max_count, screenshot_max_bytes, screenshot_max_pixels,
    screenshot_formats, reason, created_at
) VALUES (
    '00000000-0000-0000-0000-000000000013',
    '1970-01-01 00:00:00+00', 4194304, 100000,
    6, 2097152, 25000000, ARRAY['jpeg','png','webp']::text[],
    'PeerGo 首版新上传与截图安全基线。',
    '1970-01-01 00:00:00+00'
);

-- Every upload reservation freezes the policy selected when it first claims
-- the swarm. A response-loss retry keeps that revision even if a newer policy
-- has become effective in the meantime.
ALTER TABLE torrents.torrent_uploads
    ADD COLUMN upload_policy_revision_id uuid
        REFERENCES torrents.torrent_upload_policy_revisions (id) ON DELETE RESTRICT;

UPDATE torrents.torrent_uploads
SET upload_policy_revision_id = '00000000-0000-0000-0000-000000000013';

ALTER TABLE torrents.torrent_uploads
    ALTER COLUMN upload_policy_revision_id SET NOT NULL;

CREATE TABLE torrents.torrent_upload_policy_bindings (
    torrent_id bigint PRIMARY KEY
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    policy_revision_id uuid NOT NULL
        REFERENCES torrents.torrent_upload_policy_revisions (id) ON DELETE RESTRICT,
    bound_at timestamptz NOT NULL
);

INSERT INTO torrents.torrent_upload_policy_bindings (
    torrent_id, policy_revision_id, bound_at
)
SELECT id, '00000000-0000-0000-0000-000000000013', submitted_at
FROM torrents.torrents;

CREATE TRIGGER torrent_upload_policy_bindings_immutable
BEFORE UPDATE OR DELETE ON torrents.torrent_upload_policy_bindings
FOR EACH ROW EXECUTE FUNCTION torrents.reject_immutable_evidence_mutation();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION torrents.protect_torrent_upload()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'torrent upload evidence must not be deleted';
    END IF;

    IF OLD.id IS DISTINCT FROM NEW.id
        OR OLD.uploader_id IS DISTINCT FROM NEW.uploader_id
        OR OLD.request_fingerprint IS DISTINCT FROM NEW.request_fingerprint
        OR OLD.object_id IS DISTINCT FROM NEW.object_id
        OR OLD.category_id IS DISTINCT FROM NEW.category_id
        OR OLD.info_hash_v1 IS DISTINCT FROM NEW.info_hash_v1
        OR OLD.content_sha256 IS DISTINCT FROM NEW.content_sha256
        OR OLD.byte_length IS DISTINCT FROM NEW.byte_length
        OR OLD.backend_id IS DISTINCT FROM NEW.backend_id
        OR OLD.object_key IS DISTINCT FROM NEW.object_key
        OR OLD.upload_policy_revision_id IS DISTINCT FROM NEW.upload_policy_revision_id
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'torrent upload request identity is immutable';
    END IF;

    IF NEW.version <> OLD.version + 1 THEN
        RAISE EXCEPTION 'torrent upload version must increment exactly once';
    END IF;

    IF OLD.state IN ('completed', 'abandoned') THEN
        RAISE EXCEPTION 'terminal torrent upload evidence is immutable';
    END IF;

    IF OLD.object_verified_at IS NOT NULL AND (
        OLD.object_verified_at IS DISTINCT FROM NEW.object_verified_at
        OR OLD.object_created IS DISTINCT FROM NEW.object_created
        OR OLD.storage_version_id IS DISTINCT FROM NEW.storage_version_id
    ) THEN
        RAISE EXCEPTION 'verified torrent upload object observation is immutable';
    END IF;

    IF OLD.state IS DISTINCT FROM NEW.state AND NOT (
        (OLD.state = 'reserved' AND NEW.state IN ('object_verified', 'cleaning'))
        OR (OLD.state = 'object_verified' AND NEW.state IN ('completed', 'cleaning'))
        OR (OLD.state = 'cleaning' AND NEW.state IN ('reserved', 'object_verified', 'abandoned'))
    ) THEN
        RAISE EXCEPTION 'torrent upload transition from % to % is invalid', OLD.state, NEW.state;
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Source image bytes remain globally deduplicated and immutable. Attachment
-- sets record only an ordered snapshot; approval changes one versioned head.
CREATE TABLE torrents.torrent_screenshot_sets (
    id uuid PRIMARY KEY,
    torrent_id bigint NOT NULL
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    origin text NOT NULL CHECK (origin IN ('initial', 'change')),
    created_at timestamptz NOT NULL,
    UNIQUE (torrent_id, id),
    UNIQUE (torrent_id, origin) DEFERRABLE INITIALLY IMMEDIATE
);

-- More than one reviewed change set is allowed over time, so the broad unique
-- constraint above is replaced by an initial-only partial constraint.
ALTER TABLE torrents.torrent_screenshot_sets
    DROP CONSTRAINT torrent_screenshot_sets_torrent_id_origin_key;
CREATE UNIQUE INDEX torrent_screenshot_sets_one_initial_idx
    ON torrents.torrent_screenshot_sets (torrent_id)
    WHERE origin = 'initial';

CREATE TABLE torrents.torrent_screenshot_set_items (
    set_id uuid NOT NULL
        REFERENCES torrents.torrent_screenshot_sets (id) ON DELETE RESTRICT,
    object_id uuid NOT NULL
        REFERENCES torrents.torrent_screenshot_objects (id) ON DELETE RESTRICT,
    position smallint NOT NULL CHECK (position BETWEEN 0 AND 5),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (set_id, position),
    UNIQUE (set_id, object_id)
);

CREATE INDEX torrent_screenshot_set_items_object_idx
    ON torrents.torrent_screenshot_set_items (object_id, set_id);

CREATE TABLE torrents.torrent_screenshot_set_heads (
    torrent_id bigint PRIMARY KEY
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    active_set_id uuid NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL,
    FOREIGN KEY (torrent_id, active_set_id)
        REFERENCES torrents.torrent_screenshot_sets (torrent_id, id)
        ON DELETE RESTRICT
);

CREATE TRIGGER torrent_screenshot_sets_immutable
BEFORE UPDATE OR DELETE ON torrents.torrent_screenshot_sets
FOR EACH ROW EXECUTE FUNCTION torrents.reject_immutable_evidence_mutation();

CREATE TRIGGER torrent_screenshot_set_items_immutable
BEFORE UPDATE OR DELETE ON torrents.torrent_screenshot_set_items
FOR EACH ROW EXECUTE FUNCTION torrents.reject_immutable_evidence_mutation();

-- +goose StatementBegin
CREATE FUNCTION torrents.protect_torrent_screenshot_set_head()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'torrent screenshot set heads cannot be deleted';
    END IF;
    IF OLD.torrent_id IS DISTINCT FROM NEW.torrent_id
        OR NEW.version <> OLD.version + 1
        OR OLD.active_set_id IS NOT DISTINCT FROM NEW.active_set_id
        OR NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'invalid torrent screenshot set switch';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER torrent_screenshot_set_head_guard
BEFORE UPDATE OR DELETE ON torrents.torrent_screenshot_set_heads
FOR EACH ROW EXECUTE FUNCTION torrents.protect_torrent_screenshot_set_head();

-- Existing PeerGo and migrated Rousi attachments become the immutable initial
-- set without rewriting their object or mapping evidence.
INSERT INTO torrents.torrent_screenshot_sets (id, torrent_id, origin, created_at)
SELECT gen_random_uuid(), screenshot.torrent_id, 'initial', min(screenshot.created_at)
FROM torrents.torrent_screenshots AS screenshot
GROUP BY screenshot.torrent_id;

INSERT INTO torrents.torrent_screenshot_set_items (
    set_id, object_id, position, created_at
)
SELECT set.id, screenshot.object_id, screenshot.position, screenshot.created_at
FROM torrents.torrent_screenshots AS screenshot
JOIN torrents.torrent_screenshot_sets AS set
  ON set.torrent_id = screenshot.torrent_id
 AND set.origin = 'initial';

INSERT INTO torrents.torrent_screenshot_set_heads (
    torrent_id, active_set_id, version, updated_at
)
SELECT set.torrent_id, set.id, 1, set.created_at
FROM torrents.torrent_screenshot_sets AS set
WHERE set.origin = 'initial';

-- Keep the established upload/import repository compatible: every new row in
-- the original immutable initial mapping is mirrored into its initial set.
-- +goose StatementBegin
CREATE FUNCTION torrents.mirror_initial_torrent_screenshot_set()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_set_id uuid;
BEGIN
    SELECT id INTO target_set_id
    FROM torrents.torrent_screenshot_sets
    WHERE torrent_id = NEW.torrent_id AND origin = 'initial';

    IF target_set_id IS NULL THEN
        target_set_id := gen_random_uuid();
        INSERT INTO torrents.torrent_screenshot_sets (
            id, torrent_id, origin, created_at
        ) VALUES (target_set_id, NEW.torrent_id, 'initial', NEW.created_at);
        INSERT INTO torrents.torrent_screenshot_set_heads (
            torrent_id, active_set_id, version, updated_at
        ) VALUES (NEW.torrent_id, target_set_id, 1, NEW.created_at);
    END IF;

    INSERT INTO torrents.torrent_screenshot_set_items (
        set_id, object_id, position, created_at
    ) VALUES (target_set_id, NEW.object_id, NEW.position, NEW.created_at);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER torrent_screenshot_initial_set_mirror
AFTER INSERT ON torrents.torrent_screenshots
FOR EACH ROW EXECUTE FUNCTION torrents.mirror_initial_torrent_screenshot_set();

CREATE TABLE torrents.torrent_screenshot_change_requests (
    id uuid PRIMARY KEY,
    torrent_id bigint NOT NULL
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    uploader_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    base_torrent_version bigint NOT NULL CHECK (base_torrent_version > 0),
    base_set_id uuid REFERENCES torrents.torrent_screenshot_sets (id) ON DELETE RESTRICT,
    base_set_version bigint NOT NULL CHECK (base_set_version >= 0),
    candidate_set_id uuid NOT NULL UNIQUE
        REFERENCES torrents.torrent_screenshot_sets (id) ON DELETE RESTRICT,
    base_set_sha256 bytea NOT NULL CHECK (octet_length(base_set_sha256) = 32),
    candidate_set_sha256 bytea NOT NULL CHECK (octet_length(candidate_set_sha256) = 32),
    request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint) = 32),
    upload_policy_revision_id uuid NOT NULL
        REFERENCES torrents.torrent_upload_policy_revisions (id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected')),
    version bigint NOT NULL DEFAULT 1 CHECK (version IN (1, 2)),
    authorization_decision_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    decided_at timestamptz,
    CHECK (
        (base_set_id IS NULL AND base_set_version = 0)
        OR (base_set_id IS NOT NULL AND base_set_version > 0)
    ),
    CHECK (
        (status = 'pending' AND version = 1 AND decided_at IS NULL)
        OR (status IN ('approved', 'rejected') AND version = 2 AND decided_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX torrent_screenshot_change_one_pending_idx
    ON torrents.torrent_screenshot_change_requests (torrent_id)
    WHERE status = 'pending';
CREATE INDEX torrent_screenshot_change_staff_queue_idx
    ON torrents.torrent_screenshot_change_requests (status, created_at, id);
CREATE INDEX torrent_screenshot_change_uploader_history_idx
    ON torrents.torrent_screenshot_change_requests (uploader_id, created_at DESC, id DESC);

CREATE TABLE torrents.torrent_screenshot_change_decisions (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL UNIQUE
        REFERENCES torrents.torrent_screenshot_change_requests (id) ON DELETE RESTRICT,
    reviewer_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    decision text NOT NULL CHECK (decision IN ('approve', 'reject')),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    expected_request_version bigint NOT NULL CHECK (expected_request_version = 1),
    resulting_request_version bigint NOT NULL CHECK (resulting_request_version = 2),
    observed_set_version bigint NOT NULL CHECK (observed_set_version >= 0),
    resulting_set_version bigint NOT NULL CHECK (resulting_set_version >= 0),
    authorization_decision_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL,
    CHECK (
        (decision = 'approve' AND resulting_set_version = observed_set_version + 1)
        OR (decision = 'reject' AND resulting_set_version = observed_set_version)
    )
);

-- +goose StatementBegin
CREATE FUNCTION torrents.protect_torrent_screenshot_change_request()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'torrent screenshot change requests cannot be deleted';
    END IF;
    IF OLD.id IS DISTINCT FROM NEW.id
        OR OLD.torrent_id IS DISTINCT FROM NEW.torrent_id
        OR OLD.uploader_id IS DISTINCT FROM NEW.uploader_id
        OR OLD.base_torrent_version IS DISTINCT FROM NEW.base_torrent_version
        OR OLD.base_set_id IS DISTINCT FROM NEW.base_set_id
        OR OLD.base_set_version IS DISTINCT FROM NEW.base_set_version
        OR OLD.candidate_set_id IS DISTINCT FROM NEW.candidate_set_id
        OR OLD.base_set_sha256 IS DISTINCT FROM NEW.base_set_sha256
        OR OLD.candidate_set_sha256 IS DISTINCT FROM NEW.candidate_set_sha256
        OR OLD.request_fingerprint IS DISTINCT FROM NEW.request_fingerprint
        OR OLD.upload_policy_revision_id IS DISTINCT FROM NEW.upload_policy_revision_id
        OR OLD.reason IS DISTINCT FROM NEW.reason
        OR OLD.authorization_decision_id IS DISTINCT FROM NEW.authorization_decision_id
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'torrent screenshot change candidate is immutable';
    END IF;
    IF OLD.status <> 'pending' OR NEW.status NOT IN ('approved', 'rejected')
        OR NEW.version <> OLD.version + 1 OR NEW.decided_at IS NULL THEN
        RAISE EXCEPTION 'invalid torrent screenshot change transition';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER torrent_screenshot_change_request_guard
BEFORE UPDATE OR DELETE ON torrents.torrent_screenshot_change_requests
FOR EACH ROW EXECUTE FUNCTION torrents.protect_torrent_screenshot_change_request();

CREATE TRIGGER torrent_screenshot_change_decisions_immutable
BEFORE UPDATE OR DELETE ON torrents.torrent_screenshot_change_decisions
FOR EACH ROW EXECUTE FUNCTION torrents.reject_immutable_evidence_mutation();

-- +goose Down

DROP TRIGGER torrent_screenshot_change_decisions_immutable ON torrents.torrent_screenshot_change_decisions;
DROP TRIGGER torrent_screenshot_change_request_guard ON torrents.torrent_screenshot_change_requests;
DROP FUNCTION torrents.protect_torrent_screenshot_change_request();
DROP TABLE torrents.torrent_screenshot_change_decisions;
DROP TABLE torrents.torrent_screenshot_change_requests;
DROP TRIGGER torrent_screenshot_initial_set_mirror ON torrents.torrent_screenshots;
DROP FUNCTION torrents.mirror_initial_torrent_screenshot_set();
DROP TRIGGER torrent_screenshot_set_head_guard ON torrents.torrent_screenshot_set_heads;
DROP FUNCTION torrents.protect_torrent_screenshot_set_head();
DROP TRIGGER torrent_screenshot_set_items_immutable ON torrents.torrent_screenshot_set_items;
DROP TRIGGER torrent_screenshot_sets_immutable ON torrents.torrent_screenshot_sets;
DROP TABLE torrents.torrent_screenshot_set_heads;
DROP INDEX torrents.torrent_screenshot_set_items_object_idx;
DROP TABLE torrents.torrent_screenshot_set_items;
DROP INDEX torrents.torrent_screenshot_sets_one_initial_idx;
DROP TABLE torrents.torrent_screenshot_sets;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION torrents.protect_torrent_upload()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'torrent upload evidence must not be deleted'; END IF;
    IF OLD.id IS DISTINCT FROM NEW.id OR OLD.uploader_id IS DISTINCT FROM NEW.uploader_id
        OR OLD.request_fingerprint IS DISTINCT FROM NEW.request_fingerprint
        OR OLD.object_id IS DISTINCT FROM NEW.object_id OR OLD.category_id IS DISTINCT FROM NEW.category_id
        OR OLD.info_hash_v1 IS DISTINCT FROM NEW.info_hash_v1 OR OLD.content_sha256 IS DISTINCT FROM NEW.content_sha256
        OR OLD.byte_length IS DISTINCT FROM NEW.byte_length OR OLD.backend_id IS DISTINCT FROM NEW.backend_id
        OR OLD.object_key IS DISTINCT FROM NEW.object_key OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'torrent upload request identity is immutable';
    END IF;
    IF NEW.version <> OLD.version + 1 THEN RAISE EXCEPTION 'torrent upload version must increment exactly once'; END IF;
    IF OLD.state IN ('completed', 'abandoned') THEN RAISE EXCEPTION 'terminal torrent upload evidence is immutable'; END IF;
    IF OLD.object_verified_at IS NOT NULL AND (OLD.object_verified_at IS DISTINCT FROM NEW.object_verified_at
        OR OLD.object_created IS DISTINCT FROM NEW.object_created OR OLD.storage_version_id IS DISTINCT FROM NEW.storage_version_id) THEN
        RAISE EXCEPTION 'verified torrent upload object observation is immutable';
    END IF;
    IF OLD.state IS DISTINCT FROM NEW.state AND NOT ((OLD.state = 'reserved' AND NEW.state IN ('object_verified', 'cleaning'))
        OR (OLD.state = 'object_verified' AND NEW.state IN ('completed', 'cleaning'))
        OR (OLD.state = 'cleaning' AND NEW.state IN ('reserved', 'object_verified', 'abandoned'))) THEN
        RAISE EXCEPTION 'torrent upload transition from % to % is invalid', OLD.state, NEW.state;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER torrent_upload_policy_bindings_immutable ON torrents.torrent_upload_policy_bindings;
DROP TABLE torrents.torrent_upload_policy_bindings;
ALTER TABLE torrents.torrent_uploads DROP COLUMN upload_policy_revision_id;
DROP TRIGGER torrent_upload_policy_revisions_immutable ON torrents.torrent_upload_policy_revisions;
DROP INDEX torrents.torrent_upload_policy_effective_idx;
DROP TABLE torrents.torrent_upload_policy_revisions;

DELETE FROM authz.role_permissions
WHERE action IN (
    'torrent.upload.policy.issue',
    'torrent.screenshot.change.review',
    'torrent.screenshot.change.submit.self'
);
DELETE FROM authz.permissions
WHERE action IN (
    'torrent.upload.policy.issue',
    'torrent.screenshot.change.review',
    'torrent.screenshot.change.submit.self'
);
