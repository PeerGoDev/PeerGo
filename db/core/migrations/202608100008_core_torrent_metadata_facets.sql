-- +goose Up

-- Top-level categories remain stable browsing concepts. Shared technical and
-- editorial dimensions live in reusable controlled facets, avoiding PtYes's
-- repeated per-category JSON definitions and spelling drift.
CREATE TABLE catalog.facet_definitions (
    id text PRIMARY KEY
        CHECK (id ~ '^[a-z0-9][a-z0-9-]{0,63}$'),
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 40),
    selection_mode text NOT NULL
        CHECK (selection_mode IN ('single_option', 'multi_option')),
    display_order integer NOT NULL CHECK (display_order BETWEEN 0 AND 1000000),
    enabled boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (id, selection_mode),
    CHECK (updated_at >= created_at)
);

CREATE INDEX facet_definitions_display_idx
    ON catalog.facet_definitions (display_order, id);

-- option_key is a stable canonical value, while label is presentation text.
-- Chinese editorial values can therefore remain lossless without turning a
-- localized label into an application enum or inventing opaque per-row JSON.
CREATE TABLE catalog.facet_options (
    facet_id text NOT NULL,
    option_key text NOT NULL
        CHECK (char_length(btrim(option_key)) BETWEEN 1 AND 80),
    selection_mode text NOT NULL,
    label text NOT NULL CHECK (char_length(btrim(label)) BETWEEN 1 AND 80),
    display_order integer NOT NULL CHECK (display_order BETWEEN 0 AND 1000000),
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (facet_id, option_key),
    UNIQUE (facet_id, option_key, selection_mode),
    FOREIGN KEY (facet_id, selection_mode)
        REFERENCES catalog.facet_definitions (id, selection_mode)
        ON DELETE RESTRICT,
    CHECK (updated_at >= created_at)
);

CREATE INDEX facet_options_display_idx
    ON catalog.facet_options (facet_id, display_order, option_key);

CREATE TABLE catalog.category_facets (
    category_id text NOT NULL
        REFERENCES catalog.categories (id) ON DELETE RESTRICT,
    facet_id text NOT NULL,
    selection_mode text NOT NULL,
    required boolean NOT NULL DEFAULT false,
    display_order integer NOT NULL CHECK (display_order BETWEEN 0 AND 1000000),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (category_id, facet_id),
    UNIQUE (category_id, facet_id, selection_mode),
    FOREIGN KEY (facet_id, selection_mode)
        REFERENCES catalog.facet_definitions (id, selection_mode)
        ON DELETE RESTRICT
);

-- A category explicitly opts in to each allowed option. Importers cannot
-- manufacture a live option from an unknown source string; unknown values are
-- recorded as migration discrepancies until a mapping is reviewed.
CREATE TABLE catalog.category_facet_options (
    category_id text NOT NULL,
    facet_id text NOT NULL,
    option_key text NOT NULL,
    selection_mode text NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (category_id, facet_id, option_key),
    UNIQUE (category_id, facet_id, option_key, selection_mode),
    FOREIGN KEY (category_id, facet_id, selection_mode)
        REFERENCES catalog.category_facets (category_id, facet_id, selection_mode)
        ON DELETE RESTRICT,
    FOREIGN KEY (facet_id, option_key, selection_mode)
        REFERENCES catalog.facet_options (facet_id, option_key, selection_mode)
        ON DELETE RESTRICT
);

INSERT INTO catalog.categories (
    id, name, display_order, enabled, created_at, updated_at
) VALUES
    ('movies', '电影', 10, true, now(), now()),
    ('tv', '剧集', 20, true, now(), now()),
    ('documentary', '纪录片', 30, true, now(), now()),
    ('anime', '动漫', 40, true, now(), now()),
    ('variety', '综艺', 50, true, now(), now()),
    ('sports', '体育', 60, true, now(), now()),
    ('music', '音乐', 70, true, now(), now()),
    ('games', '游戏', 80, true, now(), now()),
    ('software', '软件', 90, true, now(), now()),
    ('ebooks', '电子书', 100, true, now(), now()),
    ('9kg', '9KG', 110, true, now(), now()),
    ('other', '其它', 120, true, now(), now())
ON CONFLICT (id) DO NOTHING;

INSERT INTO catalog.facet_definitions (
    id, name, selection_mode, display_order, created_at, updated_at
) VALUES
    ('genre', '类型', 'multi_option', 10, now(), now()),
    ('region', '地区', 'single_option', 20, now(), now()),
    ('resolution', '分辨率', 'single_option', 30, now(), now()),
    ('source-medium', '来源介质', 'single_option', 40, now(), now()),
    ('release-type', '发布类型', 'single_option', 50, now(), now()),
    ('format', '格式', 'single_option', 60, now(), now()),
    ('platform', '平台', 'multi_option', 70, now(), now()),
    ('themes', '主题', 'multi_option', 80, now(), now()),
    ('behaviors', '行为', 'multi_option', 90, now(), now()),
    ('distribution-channel', '来源渠道', 'single_option', 100, now(), now());

INSERT INTO catalog.facet_options (
    facet_id, option_key, selection_mode, label, display_order, created_at, updated_at
) VALUES
    ('resolution', '2160p', 'single_option', '4K / 2160p', 10, now(), now()),
    ('resolution', '1080p', 'single_option', '1080p', 20, now(), now()),
    ('resolution', '1080i', 'single_option', '1080i', 30, now(), now()),
    ('resolution', '720p', 'single_option', '720p', 40, now(), now()),
    ('resolution', 'sd', 'single_option', 'SD', 50, now(), now()),
    ('resolution', 'other', 'single_option', '其它', 999, now(), now()),
    ('source-medium', 'uhd-blu-ray', 'single_option', 'UHD Blu-ray', 10, now(), now()),
    ('source-medium', 'blu-ray', 'single_option', 'Blu-ray', 20, now(), now()),
    ('source-medium', 'dvd', 'single_option', 'DVD', 30, now(), now()),
    ('source-medium', 'web', 'single_option', 'Web', 40, now(), now()),
    ('source-medium', 'broadcast', 'single_option', '广播 / 电视', 50, now(), now()),
    ('source-medium', 'other', 'single_option', '其它', 999, now(), now()),
    ('release-type', 'full-disc', 'single_option', 'Full Disc', 10, now(), now()),
    ('release-type', 'remux', 'single_option', 'Remux', 20, now(), now()),
    ('release-type', 'encode', 'single_option', 'Encode', 30, now(), now()),
    ('release-type', 'web-dl', 'single_option', 'WEB-DL', 40, now(), now()),
    ('release-type', 'webrip', 'single_option', 'WEBRip', 50, now(), now()),
    ('release-type', 'hdtv', 'single_option', 'HDTV', 60, now(), now()),
    ('release-type', 'dvdrip', 'single_option', 'DVDRip', 70, now(), now()),
    ('release-type', 'hdrip', 'single_option', 'HDRip', 80, now(), now()),
    ('release-type', 'cam', 'single_option', 'CAM', 90, now(), now()),
    ('release-type', 'other', 'single_option', '其它', 999, now(), now()),
    ('region', 'mainland-china', 'single_option', '大陆', 10, now(), now()),
    ('region', 'hong-kong', 'single_option', '香港', 20, now(), now()),
    ('region', 'taiwan', 'single_option', '台湾', 30, now(), now()),
    ('region', 'japan', 'single_option', '日本', 40, now(), now()),
    ('region', 'south-korea', 'single_option', '韩国', 50, now(), now()),
    ('region', 'usa', 'single_option', '美国', 60, now(), now()),
    ('region', 'uk', 'single_option', '英国', 70, now(), now()),
    ('region', 'france', 'single_option', '法国', 80, now(), now()),
    ('region', 'germany', 'single_option', '德国', 90, now(), now()),
    ('region', 'italy', 'single_option', '意大利', 100, now(), now()),
    ('region', 'spain', 'single_option', '西班牙', 110, now(), now()),
    ('region', 'russia', 'single_option', '俄罗斯', 120, now(), now()),
    ('region', 'india', 'single_option', '印度', 130, now(), now()),
    ('region', 'thailand', 'single_option', '泰国', 140, now(), now()),
    ('region', 'other', 'single_option', '其它', 999, now(), now()),
    ('format', 'flac', 'single_option', 'FLAC', 10, now(), now()),
    ('format', 'ape', 'single_option', 'APE', 20, now(), now()),
    ('format', 'wav', 'single_option', 'WAV', 30, now(), now()),
    ('format', 'dsd', 'single_option', 'DSD', 40, now(), now()),
    ('format', 'mp3', 'single_option', 'MP3', 50, now(), now()),
    ('format', 'aac', 'single_option', 'AAC', 60, now(), now()),
    ('format', 'epub', 'single_option', 'EPUB', 70, now(), now()),
    ('format', 'mobi', 'single_option', 'MOBI', 80, now(), now()),
    ('format', 'pdf', 'single_option', 'PDF', 90, now(), now()),
    ('format', 'azw3', 'single_option', 'AZW3', 100, now(), now()),
    ('format', 'txt', 'single_option', 'TXT', 110, now(), now()),
    ('format', 'other', 'single_option', '其它', 999, now(), now()),
    ('platform', 'windows', 'multi_option', 'Windows', 10, now(), now()),
    ('platform', 'macos', 'multi_option', 'macOS', 20, now(), now()),
    ('platform', 'linux', 'multi_option', 'Linux', 30, now(), now()),
    ('platform', 'android', 'multi_option', 'Android', 40, now(), now()),
    ('platform', 'ios', 'multi_option', 'iOS', 50, now(), now()),
    ('platform', 'playstation', 'multi_option', 'PlayStation', 60, now(), now()),
    ('platform', 'xbox', 'multi_option', 'Xbox', 70, now(), now()),
    ('platform', 'nintendo', 'multi_option', 'Nintendo', 80, now(), now()),
    ('platform', 'other', 'multi_option', '其它', 999, now(), now());

-- Category-to-facet bindings reflect browsing and upload semantics, not every
-- possible metadata field. All are optional during legacy import; the future
-- upload policy may require a subset per category without corrupting old rows.
INSERT INTO catalog.category_facets (
    category_id, facet_id, selection_mode, required, display_order, created_at
) VALUES
    ('movies', 'genre', 'multi_option', false, 10, now()),
    ('movies', 'region', 'single_option', false, 20, now()),
    ('movies', 'resolution', 'single_option', false, 30, now()),
    ('movies', 'source-medium', 'single_option', false, 40, now()),
    ('movies', 'release-type', 'single_option', false, 50, now()),
    ('tv', 'genre', 'multi_option', false, 10, now()),
    ('tv', 'region', 'single_option', false, 20, now()),
    ('tv', 'resolution', 'single_option', false, 30, now()),
    ('tv', 'source-medium', 'single_option', false, 40, now()),
    ('tv', 'release-type', 'single_option', false, 50, now()),
    ('documentary', 'genre', 'multi_option', false, 10, now()),
    ('documentary', 'region', 'single_option', false, 20, now()),
    ('documentary', 'resolution', 'single_option', false, 30, now()),
    ('documentary', 'source-medium', 'single_option', false, 40, now()),
    ('documentary', 'release-type', 'single_option', false, 50, now()),
    ('anime', 'genre', 'multi_option', false, 10, now()),
    ('anime', 'region', 'single_option', false, 20, now()),
    ('anime', 'resolution', 'single_option', false, 30, now()),
    ('anime', 'source-medium', 'single_option', false, 40, now()),
    ('anime', 'release-type', 'single_option', false, 50, now()),
    ('variety', 'genre', 'multi_option', false, 10, now()),
    ('variety', 'region', 'single_option', false, 20, now()),
    ('variety', 'resolution', 'single_option', false, 30, now()),
    ('variety', 'source-medium', 'single_option', false, 40, now()),
    ('variety', 'release-type', 'single_option', false, 50, now()),
    ('sports', 'genre', 'multi_option', false, 10, now()),
    ('sports', 'resolution', 'single_option', false, 30, now()),
    ('sports', 'source-medium', 'single_option', false, 40, now()),
    ('sports', 'release-type', 'single_option', false, 50, now()),
    ('music', 'genre', 'multi_option', false, 10, now()),
    ('music', 'format', 'single_option', false, 20, now()),
    ('games', 'genre', 'multi_option', false, 10, now()),
    ('games', 'platform', 'multi_option', false, 20, now()),
    ('software', 'genre', 'multi_option', false, 10, now()),
    ('software', 'platform', 'multi_option', false, 20, now()),
    ('ebooks', 'genre', 'multi_option', false, 10, now()),
    ('ebooks', 'format', 'single_option', false, 20, now()),
    ('9kg', 'genre', 'multi_option', false, 10, now()),
    ('9kg', 'themes', 'multi_option', false, 20, now()),
    ('9kg', 'behaviors', 'multi_option', false, 30, now()),
    ('9kg', 'resolution', 'single_option', false, 40, now()),
    ('9kg', 'distribution-channel', 'single_option', false, 50, now());

-- Every seeded technical option is initially usable by each category that
-- opted into its facet. Editorial options (genre/themes/behaviors/channel) are
-- loaded only from an explicit reviewed mapping manifest.
INSERT INTO catalog.category_facet_options (
    category_id, facet_id, option_key, selection_mode, created_at
)
SELECT
    binding.category_id,
    option.facet_id,
    option.option_key,
    option.selection_mode,
    now()
FROM catalog.category_facets AS binding
JOIN catalog.facet_options AS option
  ON option.facet_id = binding.facet_id
 AND option.selection_mode = binding.selection_mode;

-- PtYes resource grouping is useful torrent metadata and may retain its legacy
-- numeric ID. It is not an imported community/comment object.
CREATE TABLE torrents.resource_groups (
    id bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY CHECK (id > 0),
    public_id uuid NOT NULL UNIQUE,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (updated_at >= created_at)
);

ALTER TABLE torrents.torrents
    ADD COLUMN description text NOT NULL DEFAULT ''
        CHECK (octet_length(description) <= 4194304),
    ADD COLUMN description_format text NOT NULL DEFAULT 'markdown'
        CHECK (description_format IN ('markdown', 'plain_text')),
    ADD COLUMN media_info text NOT NULL DEFAULT ''
        CHECK (octet_length(media_info) <= 4194304),
    ADD COLUMN anonymous boolean NOT NULL DEFAULT false,
    ADD COLUMN resource_group_id bigint
        REFERENCES torrents.resource_groups (id) ON DELETE RESTRICT,
    ADD CONSTRAINT torrents_id_category_unique UNIQUE (id, category_id);

CREATE INDEX torrents_resource_group_idx
    ON torrents.torrents (resource_group_id, id)
    WHERE resource_group_id IS NOT NULL;

CREATE TABLE torrents.torrent_external_identifiers (
    torrent_id bigint NOT NULL
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    provider text NOT NULL
        CHECK (provider IN ('imdb', 'tmdb', 'douban', 'bangumi', 'steam')),
    external_id text NOT NULL CHECK (char_length(external_id) BETWEEN 1 AND 64),
    origin text NOT NULL
        CHECK (origin IN ('legacy_import', 'user', 'metadata_provider')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (torrent_id, provider),
    CHECK (updated_at >= created_at),
    CHECK (
        (provider = 'imdb' AND external_id ~ '^tt[0-9]{7,10}$')
        OR (provider IN ('tmdb', 'douban', 'bangumi', 'steam')
            AND external_id ~ '^[0-9]{1,20}$')
    )
);

CREATE INDEX torrent_external_identifiers_lookup_idx
    ON torrents.torrent_external_identifiers (provider, external_id, torrent_id);

-- Redundant category_id and selection_mode are both protected by foreign keys.
-- They make the database capable of enforcing category option eligibility and
-- one-value single facets without trusting importer/application branching.
CREATE TABLE torrents.torrent_facet_values (
    torrent_id bigint NOT NULL,
    category_id text NOT NULL,
    facet_id text NOT NULL,
    option_key text NOT NULL,
    selection_mode text NOT NULL,
    position integer NOT NULL CHECK (position BETWEEN 0 AND 31),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (torrent_id, facet_id, option_key),
    UNIQUE (torrent_id, facet_id, position),
    FOREIGN KEY (torrent_id, category_id)
        REFERENCES torrents.torrents (id, category_id) ON DELETE RESTRICT,
    FOREIGN KEY (category_id, facet_id, option_key, selection_mode)
        REFERENCES catalog.category_facet_options (
            category_id, facet_id, option_key, selection_mode
        ) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX torrent_single_facet_value_idx
    ON torrents.torrent_facet_values (torrent_id, facet_id)
    WHERE selection_mode = 'single_option';

-- Group IDs and public IDs are allocated once, just like user/torrent IDs.
-- This table contains no group prose or legacy JSON.
CREATE TABLE migration.torrent_group_id_map (
    source_system text NOT NULL CHECK (source_system = 'ptyes'),
    legacy_group_id bigint NOT NULL CHECK (legacy_group_id > 0),
    resource_group_id bigint NOT NULL UNIQUE CHECK (resource_group_id > 0),
    public_id uuid NOT NULL UNIQUE,
    first_run_id uuid NOT NULL
        REFERENCES migration.runs (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (source_system, legacy_group_id),
    CHECK (resource_group_id = legacy_group_id)
);

CREATE TRIGGER migration_torrent_group_id_map_immutable
BEFORE UPDATE OR DELETE ON migration.torrent_group_id_map
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

-- +goose Down

DROP TRIGGER IF EXISTS migration_torrent_group_id_map_immutable
    ON migration.torrent_group_id_map;
DROP TABLE IF EXISTS migration.torrent_group_id_map;
DROP TABLE IF EXISTS torrents.torrent_facet_values;
DROP TABLE IF EXISTS torrents.torrent_external_identifiers;
DROP INDEX IF EXISTS torrents.torrents_resource_group_idx;
ALTER TABLE torrents.torrents
    DROP CONSTRAINT torrents_id_category_unique,
    DROP COLUMN resource_group_id,
    DROP COLUMN anonymous,
    DROP COLUMN media_info,
    DROP COLUMN description_format,
    DROP COLUMN description;
DROP TABLE IF EXISTS torrents.resource_groups;
DROP TABLE IF EXISTS catalog.category_facet_options;
DROP TABLE IF EXISTS catalog.category_facets;
DROP TABLE IF EXISTS catalog.facet_options;
DROP TABLE IF EXISTS catalog.facet_definitions;
