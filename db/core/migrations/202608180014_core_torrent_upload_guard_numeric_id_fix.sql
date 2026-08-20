-- +goose Up

-- 013 first shipped while one local database had already applied the former
-- UUID/public_id guard body. Keep this forward-only correction so both an
-- upgraded development database and a clean deployment enforce the canonical
-- bigint torrent identity without destructive migration replay.
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

-- +goose Down

-- 013 now contains the same canonical guard body. Reverting this compatibility
-- step therefore requires no schema or evidence mutation.
SELECT 1;
