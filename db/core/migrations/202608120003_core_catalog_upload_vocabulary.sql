-- +goose Up

-- A clean PeerGo installation must expose the same reviewed upload vocabulary
-- as the PtYes source even before the legacy importer has prepared any torrent
-- rows. The importer still validates its frozen source tables independently;
-- this migration only establishes controlled options for new uploads.
CREATE TEMP TABLE peergo_ptyes_genre_seed (
    category_id text PRIMARY KEY,
    options jsonb NOT NULL CHECK (jsonb_typeof(options) = 'array')
) ON COMMIT DROP;

INSERT INTO peergo_ptyes_genre_seed (category_id, options) VALUES
    ('movies', '["剧情","喜剧","动作","爱情","科幻","悬疑","惊悚","恐怖","犯罪","动画","奇幻","冒险","灾难","战争","传记","历史","运动","音乐","歌舞","家庭","儿童","纪录","短片","真人秀","脱口秀","西部","武侠","古装","其它"]'),
    ('tv', '["剧情","喜剧","动作","爱情","科幻","悬疑","惊悚","恐怖","犯罪","动画","奇幻","冒险","战争","历史","家庭","儿童","纪录","真人秀","武侠","古装","都市"]'),
    ('documentary', '["自然","历史","科技","人文","社会","传记","探险","美食","旅行","体育","音乐","艺术","其它"]'),
    ('anime', '["剧情","动画","热血","冒险","搞笑","恋爱","爱情","同性","校园","后宫","百合","治愈","萌系","悬疑","科幻","机战","奇幻","战斗","运动","竞技","历史","社会","恐怖","致郁","其它"]'),
    ('music', '["流行","摇滚","电子","古典","爵士","蓝调","乡村","民谣","说唱","R&B","金属","朋克","新世纪","原声","世界音乐","其它"]'),
    ('variety', '["真人秀","脱口秀","选秀","访谈","音乐","喜剧","游戏","美食","旅行","情感","亲子","其它"]'),
    ('sports', '["足球","篮球","网球","F1","WWE","UFC","拳击","高尔夫","棒球","冰球","橄榄球","电竞","其它"]'),
    ('software', '["系统工具","办公软件","图形设计","影音处理","开发工具","网络工具","安全软件","游戏","其它"]'),
    ('ebooks', '["小说","文学","历史","哲学","经济","管理","心理","科技","计算机","教育","艺术","生活","漫画","杂志","其它"]');

WITH expanded AS (
    SELECT
        seed.category_id,
        option.value AS option_key,
        option.ordinality::integer * 10 AS display_order
    FROM peergo_ptyes_genre_seed AS seed
    CROSS JOIN LATERAL jsonb_array_elements_text(seed.options)
        WITH ORDINALITY AS option(value, ordinality)
), canonical AS (
    SELECT DISTINCT ON (option_key)
        option_key,
        display_order
    FROM expanded
    ORDER BY option_key, display_order, category_id
)
INSERT INTO catalog.facet_options (
    facet_id,
    option_key,
    selection_mode,
    label,
    display_order,
    enabled,
    created_at,
    updated_at
)
SELECT
    'genre',
    option_key,
    'multi_option',
    option_key,
    display_order,
    true,
    now(),
    now()
FROM canonical
ON CONFLICT (facet_id, option_key) DO NOTHING;

WITH expanded AS (
    SELECT
        seed.category_id,
        option.value AS option_key
    FROM peergo_ptyes_genre_seed AS seed
    CROSS JOIN LATERAL jsonb_array_elements_text(seed.options)
        WITH ORDINALITY AS option(value, ordinality)
)
INSERT INTO catalog.category_facet_options (
    category_id,
    facet_id,
    option_key,
    selection_mode,
    created_at
)
SELECT
    expanded.category_id,
    'genre',
    expanded.option_key,
    'multi_option',
    now()
FROM expanded
JOIN catalog.category_facets AS binding
  ON binding.category_id = expanded.category_id
 AND binding.facet_id = 'genre'
 AND binding.selection_mode = 'multi_option'
ON CONFLICT (category_id, facet_id, option_key) DO NOTHING;

-- These three values are present in the current Rousi movie/TV vocabulary and
-- already have stable mappings in the importer. They were absent only from the
-- clean-install bootstrap data.
INSERT INTO catalog.facet_options (
    facet_id,
    option_key,
    selection_mode,
    label,
    display_order,
    enabled,
    created_at,
    updated_at
) VALUES
    ('region', 'new-zealand', 'single_option', '新西兰', 125, true, now(), now()),
    ('region', 'canada', 'single_option', '加拿大', 130, true, now(), now()),
    ('region', 'australia', 'single_option', '澳大利亚', 145, true, now(), now())
ON CONFLICT (facet_id, option_key) DO NOTHING;

INSERT INTO catalog.category_facet_options (
    category_id,
    facet_id,
    option_key,
    selection_mode,
    created_at
) VALUES
    ('movies', 'region', 'new-zealand', 'single_option', now()),
    ('movies', 'region', 'canada', 'single_option', now()),
    ('movies', 'region', 'australia', 'single_option', now()),
    ('tv', 'region', 'new-zealand', 'single_option', now()),
    ('tv', 'region', 'canada', 'single_option', now()),
    ('tv', 'region', 'australia', 'single_option', now())
ON CONFLICT (category_id, facet_id, option_key) DO NOTHING;

-- PtYes requires these three controlled attributes on new uploads. Source is
-- intentionally not marked required here because PeerGo losslessly separates
-- source-medium from release-type; a later grouped requirement must enforce
-- “one of the two” instead of incorrectly requiring both.
UPDATE catalog.category_facets
SET required = true
WHERE facet_id IN ('genre', 'region', 'resolution')
  AND category_id IN (
      'movies', 'tv', 'documentary', 'anime', 'variety', 'sports',
      'music', 'software', 'ebooks'
  );

-- Align the visible label while retaining PeerGo's normalized source-medium
-- identity and its independent release-type facet.
UPDATE catalog.facet_definitions
SET name = '来源', updated_at = now(), version = version + 1
WHERE id = 'source-medium'
  AND name IS DISTINCT FROM '来源';

-- +goose Down

UPDATE catalog.category_facets
SET required = false
WHERE facet_id IN ('genre', 'region', 'resolution')
  AND category_id IN (
      'movies', 'tv', 'documentary', 'anime', 'variety', 'sports',
      'music', 'software', 'ebooks'
  );

UPDATE catalog.facet_definitions
SET name = '来源介质', updated_at = now(), version = version + 1
WHERE id = 'source-medium'
  AND name IS DISTINCT FROM '来源介质';

-- Controlled options are not deleted on downgrade: uploaded or migrated
-- torrents may already reference them through restrictive foreign keys.
