-- +goose Up
-- Public site information is a runtime prerequisite, not development fixture
-- data. A newly migrated production database and a legacy cutover target must
-- therefore both contain the typed singleton before the API starts serving.
-- ON CONFLICT deliberately preserves every setting already changed through
-- the audited staff command.
INSERT INTO catalog.site_profile (
    singleton,
    name,
    description,
    online_users,
    default_torrent_view,
    show_latest_announcement,
    version,
    effective_at,
    updated_at
) VALUES (
    true,
    'PeerGo',
    '私有分享社区',
    0,
    'list',
    true,
    1,
    now(),
    now()
)
ON CONFLICT (singleton) DO NOTHING;

-- +goose Down
-- Never remove an operator-customized singleton during rollback. Only the
-- untouched bootstrap value created above is safe to delete.
DELETE FROM catalog.site_profile
WHERE singleton = true
  AND name = 'PeerGo'
  AND description = '私有分享社区'
  AND online_users = 0
  AND default_torrent_view = 'list'
  AND show_latest_announcement = true
  AND version = 1;
