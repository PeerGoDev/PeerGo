-- +goose Up
UPDATE authz.permissions AS permission
SET credential_audience = 'web-session', description = requested.description
FROM (VALUES
    ('announcement.read', '读取已发布公告'),
    ('category.read', '读取种子分类'),
    ('torrent.read', '读取已发布种子'),
    ('wiki.page.read', '查看已发布 Wiki 页面')
) AS requested(action, description)
WHERE permission.action = requested.action;

-- +goose Down
UPDATE authz.permissions AS permission
SET credential_audience = 'anonymous', description = previous.description
FROM (VALUES
    ('announcement.read', '读取公开公告'),
    ('category.read', '读取公开种子分类'),
    ('torrent.read', '读取公开种子摘要'),
    ('wiki.page.read', '查看公开 Wiki 页面')
) AS previous(action, description)
WHERE permission.action = previous.action;
