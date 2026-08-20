-- +goose Up

-- Category IDs are the stable catalog identities used by torrent rows and API
-- filters. Only align member-facing labels and display order with the PtYes
-- catalog so an existing migration keeps every relationship intact.
UPDATE catalog.categories AS category
SET
    name = presentation.name,
    display_order = presentation.display_order,
    updated_at = now()
FROM (
    VALUES
        ('movies', '电影', 10),
        ('tv', '电视剧', 20),
        ('documentary', '纪录片', 30),
        ('anime', '动漫', 40),
        ('music', '音乐', 50),
        ('variety', '综艺', 60),
        ('sports', '体育', 70),
        ('games', '游戏', 80),
        ('software', '软件', 90),
        ('ebooks', '电子书', 100),
        ('9kg', '9KG', 110),
        ('other', '其它', 120)
) AS presentation(id, name, display_order)
WHERE category.id = presentation.id
  AND (
      category.name IS DISTINCT FROM presentation.name
      OR category.display_order IS DISTINCT FROM presentation.display_order
  );

-- +goose Down

-- Restore the original PeerGo bootstrap presentation. Stable category IDs and
-- torrent relationships remain unchanged in both directions.
UPDATE catalog.categories AS category
SET
    name = presentation.name,
    display_order = presentation.display_order,
    updated_at = now()
FROM (
    VALUES
        ('movies', '电影', 10),
        ('tv', '剧集', 20),
        ('documentary', '纪录片', 30),
        ('anime', '动漫', 40),
        ('variety', '综艺', 50),
        ('sports', '体育', 60),
        ('music', '音乐', 70),
        ('games', '游戏', 80),
        ('software', '软件', 90),
        ('ebooks', '电子书', 100),
        ('9kg', '9KG', 110),
        ('other', '其它', 120)
) AS presentation(id, name, display_order)
WHERE category.id = presentation.id
  AND (
      category.name IS DISTINCT FROM presentation.name
      OR category.display_order IS DISTINCT FROM presentation.display_order
  );
