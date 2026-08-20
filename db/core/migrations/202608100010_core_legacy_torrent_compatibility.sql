-- +goose Up

-- One audited PtYes metainfo contains two file entries with the same relative
-- path. The duplicate participates in the info dictionary, file count and
-- total size, so removing it would make the database disagree with the
-- immutable swarm identity. New uploads still reject duplicate paths in the
-- strict parser; only legacy_import may persist them, distinguished by index.
ALTER TABLE torrents.torrent_files
    DROP CONSTRAINT torrent_files_torrent_id_display_path_key;

-- The largest audited PtYes MediaInfo value is 5,581,981 bytes. Preserve it
-- losslessly under a bounded 16 MiB ceiling instead of truncating the source
-- or turning a display concern into a migration failure.
ALTER TABLE torrents.torrents
    DROP CONSTRAINT torrents_media_info_check,
    ADD CONSTRAINT torrents_media_info_check
        CHECK (octet_length(media_info) <= 16777216);

-- Resource groups are a torrent browsing dependency, not imported community
-- content. Keeping their external identifiers here preserves grouping without
-- retaining PtYes's untyped JSON object as a live application model.
CREATE TABLE torrents.resource_group_external_identifiers (
    resource_group_id bigint NOT NULL
        REFERENCES torrents.resource_groups (id) ON DELETE RESTRICT,
    provider text NOT NULL
        CHECK (provider IN ('imdb', 'tmdb', 'douban', 'bangumi', 'steam')),
    external_id text NOT NULL CHECK (char_length(external_id) BETWEEN 1 AND 64),
    origin text NOT NULL CHECK (origin = 'legacy_import'),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (resource_group_id, provider),
    CHECK (
        (provider = 'imdb' AND external_id ~ '^tt[0-9]{7,10}$')
        OR (provider IN ('tmdb', 'douban', 'bangumi', 'steam')
            AND external_id ~ '^[0-9]{1,20}$')
    )
);

CREATE INDEX resource_group_external_identifiers_lookup_idx
    ON torrents.resource_group_external_identifiers (
        provider, external_id, resource_group_id
    );

-- +goose Down

DROP TABLE IF EXISTS torrents.resource_group_external_identifiers;

ALTER TABLE torrents.torrents
    DROP CONSTRAINT torrents_media_info_check,
    ADD CONSTRAINT torrents_media_info_check
        CHECK (octet_length(media_info) <= 4194304);

-- This intentionally refuses to roll back while duplicate legacy rows exist;
-- an operator must first prove that no imported swarm depends on them.
ALTER TABLE torrents.torrent_files
    ADD CONSTRAINT torrent_files_torrent_id_display_path_key
        UNIQUE (torrent_id, display_path);
