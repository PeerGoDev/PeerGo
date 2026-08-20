-- +goose Up

-- Live uploads remain bounded by the Go admission policy at 2 MiB and do not
-- accept GIF. The wider persisted-object envelope is migration-only: the
-- audited PtYes snapshot contains source PNG/JPEG files up to 9,107,476 bytes
-- and animated GIF evidence which must not be silently flattened.
ALTER TABLE torrents.torrent_screenshot_objects
    DROP CONSTRAINT torrent_screenshot_objects_byte_length_check,
    DROP CONSTRAINT torrent_screenshot_objects_content_type_check,
    DROP CONSTRAINT torrent_screenshot_objects_check,
    ADD CONSTRAINT torrent_screenshot_objects_byte_length_check
        CHECK (byte_length BETWEEN 1 AND 16777216),
    ADD CONSTRAINT torrent_screenshot_objects_content_type_check
        CHECK (content_type IN ('image/jpeg', 'image/png', 'image/webp', 'image/gif')),
    ADD CONSTRAINT torrent_screenshot_objects_pixel_count_check
        CHECK (width::bigint * height::bigint <= 100000000);

ALTER TABLE migration.source_rows
    DROP CONSTRAINT source_rows_entity_kind_check,
    ADD CONSTRAINT source_rows_entity_kind_check CHECK (
        entity_kind IN (
            'user', 'torrent', 'torrent_object', 'torrent_image', 'torrent_poster'
        )
    );

ALTER TABLE migration.run_artifacts
    DROP CONSTRAINT run_artifacts_kind_check,
    ADD CONSTRAINT run_artifacts_kind_check CHECK (
        kind IN (
            'database_dump', 'torrent_manifest', 'image_archive', 'image_manifest'
        )
    );

-- A map row connects one finite PtYes image record to the existing immutable
-- screenshot object model. It stores no host path, provider URL or credential.
CREATE TABLE migration.torrent_image_map (
    source_system text NOT NULL CHECK (source_system = 'ptyes'),
    entity_kind text NOT NULL CHECK (
        entity_kind IN ('torrent_image', 'torrent_poster')
    ),
    legacy_id bigint NOT NULL CHECK (legacy_id > 0),
    torrent_id bigint NOT NULL
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    object_id uuid NOT NULL
        REFERENCES torrents.torrent_screenshot_objects (id) ON DELETE RESTRICT,
    position smallint NOT NULL CHECK (position BETWEEN 0 AND 5),
    legacy_path text NOT NULL CHECK (
        legacy_path ~ '^/uploads/images/[0-9a-f]{2}/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(jpg|png|webp|gif)$'
        AND substring(legacy_path FROM 17 FOR 2) = substring(legacy_path FROM 20 FOR 2)
    ),
    original_sha256 bytea NOT NULL CHECK (octet_length(original_sha256) = 32),
    first_run_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (source_system, entity_kind, legacy_id),
    UNIQUE (torrent_id, position),
    FOREIGN KEY (first_run_id, entity_kind, legacy_id)
        REFERENCES migration.source_rows (run_id, entity_kind, legacy_id)
        ON DELETE RESTRICT
);

CREATE INDEX torrent_image_map_run_idx
    ON migration.torrent_image_map (first_run_id, entity_kind, legacy_id);

-- Historical descriptions still contain /uploads/images/... references. The
-- alias resolves those stable paths to the same verified screenshot objects;
-- it does not duplicate image bytes or expose physical storage placement.
CREATE TABLE torrents.legacy_image_aliases (
    legacy_path text PRIMARY KEY CHECK (
        legacy_path ~ '^/uploads/images/[0-9a-f]{2}/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(jpg|png|webp|gif)$'
        AND substring(legacy_path FROM 17 FOR 2) = substring(legacy_path FROM 20 FOR 2)
    ),
    object_id uuid NOT NULL
        REFERENCES torrents.torrent_screenshot_objects (id) ON DELETE RESTRICT,
    first_run_id uuid NOT NULL
        REFERENCES migration.runs (id) ON DELETE RESTRICT,
    original_sha256 bytea NOT NULL CHECK (octet_length(original_sha256) = 32),
    original_byte_length bigint NOT NULL CHECK (
        original_byte_length BETWEEN 1 AND 33554432
    ),
    created_at timestamptz NOT NULL
);

CREATE INDEX legacy_image_aliases_object_idx
    ON torrents.legacy_image_aliases (object_id, legacy_path);

-- +goose StatementBegin
CREATE FUNCTION migration.reject_legacy_image_mapping_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'legacy image mappings are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER torrent_image_map_immutable
BEFORE UPDATE OR DELETE ON migration.torrent_image_map
FOR EACH ROW EXECUTE FUNCTION migration.reject_legacy_image_mapping_mutation();

CREATE TRIGGER legacy_image_aliases_immutable
BEFORE UPDATE OR DELETE ON torrents.legacy_image_aliases
FOR EACH ROW EXECUTE FUNCTION migration.reject_legacy_image_mapping_mutation();

-- +goose Down

DROP TRIGGER legacy_image_aliases_immutable ON torrents.legacy_image_aliases;
DROP TRIGGER torrent_image_map_immutable ON migration.torrent_image_map;
DROP FUNCTION migration.reject_legacy_image_mapping_mutation();
DROP INDEX torrents.legacy_image_aliases_object_idx;
DROP TABLE torrents.legacy_image_aliases;
DROP INDEX migration.torrent_image_map_run_idx;
DROP TABLE migration.torrent_image_map;

ALTER TABLE migration.source_rows
    DROP CONSTRAINT source_rows_entity_kind_check,
    ADD CONSTRAINT source_rows_entity_kind_check CHECK (
        entity_kind IN ('user', 'torrent', 'torrent_object', 'torrent_image')
    );

ALTER TABLE migration.run_artifacts
    DROP CONSTRAINT run_artifacts_kind_check,
    ADD CONSTRAINT run_artifacts_kind_check CHECK (
        kind IN ('database_dump', 'torrent_manifest', 'image_manifest')
    );

ALTER TABLE torrents.torrent_screenshot_objects
    DROP CONSTRAINT torrent_screenshot_objects_byte_length_check,
    DROP CONSTRAINT torrent_screenshot_objects_content_type_check,
    DROP CONSTRAINT torrent_screenshot_objects_pixel_count_check,
    ADD CONSTRAINT torrent_screenshot_objects_byte_length_check
        CHECK (byte_length BETWEEN 1 AND 5242880),
    ADD CONSTRAINT torrent_screenshot_objects_content_type_check
        CHECK (content_type IN ('image/jpeg', 'image/png', 'image/webp')),
    ADD CONSTRAINT torrent_screenshot_objects_check
        CHECK (width::bigint * height::bigint <= 25000000);
