-- +goose Up

-- The same controlled option can appear in a different editorial order for
-- each category. Keeping that order on the category binding preserves the
-- reviewed Rousi upload vocabulary without duplicating option identities.
ALTER TABLE catalog.category_facet_options
    ADD COLUMN display_order integer NOT NULL DEFAULT 1000000
        CHECK (display_order BETWEEN 0 AND 1000000);

UPDATE catalog.category_facet_options AS allowed
SET display_order = option.display_order
FROM catalog.facet_options AS option
WHERE option.facet_id = allowed.facet_id
  AND option.option_key = allowed.option_key
  AND option.selection_mode = allowed.selection_mode;

CREATE TEMP TABLE peergo_ptyes_option_order_seed (
    category_id text NOT NULL,
    facet_id text NOT NULL,
    options jsonb NOT NULL CHECK (jsonb_typeof(options) = 'array'),
    PRIMARY KEY (category_id, facet_id)
) ON COMMIT DROP;

INSERT INTO peergo_ptyes_option_order_seed (
    category_id,
    facet_id,
    options
) VALUES
    ('movies', 'genre', '["剧情","喜剧","动作","爱情","科幻","悬疑","惊悚","恐怖","犯罪","动画","奇幻","冒险","灾难","战争","传记","历史","运动","音乐","歌舞","家庭","儿童","纪录","短片","真人秀","脱口秀","西部","武侠","古装","其它"]'),
    ('tv', 'genre', '["剧情","喜剧","动作","爱情","科幻","悬疑","惊悚","恐怖","犯罪","动画","奇幻","冒险","战争","历史","家庭","儿童","纪录","真人秀","武侠","古装","都市"]'),
    ('documentary', 'genre', '["自然","历史","科技","人文","社会","传记","探险","美食","旅行","体育","音乐","艺术","其它"]'),
    ('anime', 'genre', '["剧情","动画","热血","冒险","搞笑","恋爱","爱情","同性","校园","后宫","百合","治愈","萌系","悬疑","科幻","机战","奇幻","战斗","运动","竞技","历史","社会","恐怖","致郁","其它"]'),
    ('music', 'genre', '["流行","摇滚","电子","古典","爵士","蓝调","乡村","民谣","说唱","R&B","金属","朋克","新世纪","原声","世界音乐","其它"]'),
    ('variety', 'genre', '["真人秀","脱口秀","选秀","访谈","音乐","喜剧","游戏","美食","旅行","情感","亲子","其它"]'),
    ('sports', 'genre', '["足球","篮球","网球","F1","WWE","UFC","拳击","高尔夫","棒球","冰球","橄榄球","电竞","其它"]'),
    ('software', 'genre', '["系统工具","办公软件","图形设计","影音处理","开发工具","网络工具","安全软件","游戏","其它"]'),
    ('ebooks', 'genre', '["小说","文学","历史","哲学","经济","管理","心理","科技","计算机","教育","艺术","生活","漫画","杂志","其它"]'),
    ('movies', 'region', '["mainland-china","hong-kong","taiwan","japan","south-korea","usa","uk","france","germany","italy","spain","russia","new-zealand","canada","india","thailand","australia","other"]'),
    ('tv', 'region', '["mainland-china","hong-kong","taiwan","japan","south-korea","usa","uk","france","germany","italy","spain","russia","new-zealand","canada","india","thailand","australia","other"]');

WITH expanded AS (
    SELECT
        seed.category_id,
        seed.facet_id,
        option.value AS option_key,
        option.ordinality::integer * 10 AS display_order
    FROM peergo_ptyes_option_order_seed AS seed
    CROSS JOIN LATERAL jsonb_array_elements_text(seed.options)
        WITH ORDINALITY AS option(value, ordinality)
)
UPDATE catalog.category_facet_options AS allowed
SET display_order = expanded.display_order
FROM expanded
WHERE allowed.category_id = expanded.category_id
  AND allowed.facet_id = expanded.facet_id
  AND allowed.option_key = expanded.option_key;

-- +goose Down

ALTER TABLE catalog.category_facet_options
    DROP COLUMN display_order;
