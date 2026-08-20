BEGIN;

INSERT INTO catalog.site_profile (
    singleton,
    name,
    description,
    online_users,
    default_torrent_view,
    show_latest_announcement,
    updated_at
) VALUES (
    true,
    'PeerGo',
    '面向真实协作与长期治理的私有分享社区。',
    222,
    'list',
    true,
    now()
)
ON CONFLICT (singleton) DO UPDATE SET
    -- Repeatable development seeding refreshes volatile demo projections but
    -- must not bypass the versioned settings command or its audit evidence.
    online_users = EXCLUDED.online_users,
    updated_at = EXCLUDED.updated_at;

INSERT INTO identity.registration_policy (singleton, mode, updated_at)
VALUES (true, 'invite', now())
ON CONFLICT (singleton) DO UPDATE SET
    mode = EXCLUDED.mode,
    updated_at = EXCLUDED.updated_at;

-- Raw development token (never store this in production data):
-- cGVlcmdvLWRldmVsb3BtZW50LWludml0ZS12MSEhISE
INSERT INTO identity.registration_invitations (
    id,
    token_sha256,
    note,
    expires_at
) VALUES (
    '0198f20a-6da8-7e51-9c64-abcdabcdabcd',
    decode('c4e6c67fa73f234b2208ed9efaa4b82b8bf78f8d602637ed3673b6ce550694a2', 'hex'),
    'development-only registration walkthrough',
    now() + interval '365 days'
)
ON CONFLICT (id) DO UPDATE SET
    expires_at = CASE
        WHEN identity.registration_invitations.claimed_by IS NULL THEN EXCLUDED.expires_at
        ELSE identity.registration_invitations.expires_at
    END;

INSERT INTO catalog.categories (id, name, display_order, enabled) VALUES
    ('movies', '电影', 10, true),
    ('tv', '剧集', 20, true),
    ('anime', '动漫', 30, true),
    ('documentary', '纪录片', 40, true),
    ('music', '音乐', 50, true)
-- Once category administration exists, repeatable seeding must not silently
-- overwrite a versioned staff edit without its authorization and audit event.
ON CONFLICT (id) DO NOTHING;

-- Versioned announcement administration forbids a repeatable development seed
-- from overwriting staff edits. A fresh database gets one immutable fixture;
-- an existing aggregate is left entirely untouched.
INSERT INTO catalog.announcements (
    id,
    version,
    latest_revision_number,
    created_at,
    updated_at
) VALUES (
    'welcome-to-peergo',
    1,
    0,
    now() - interval '2 hours',
    now() - interval '2 hours'
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO catalog.announcement_revisions (
    announcement_id,
    revision_number,
    title,
    summary,
    body,
    body_format,
    origin,
    created_at
)
SELECT
    announcement.id,
    1,
    '欢迎来到 PeerGo',
    '请在发布和下载前阅读站点规则，共同维护长期、稳定的分享环境。',
    E'PeerGo 希望建立一个重视内容质量、长期做种与友善交流的分享社区。\n\n发布资源前请核对文件、标题与分类；下载完成后请尽量保持做种。讨论时请围绕内容本身友善交流，不公开他人的个人信息。\n\n如遇到资源信息有误、文件异常或其他需要站务协助的情况，请通过页面提供的举报或反馈入口联系管理团队。',
    'plain_text',
    'development_seed',
    announcement.created_at
FROM catalog.announcements AS announcement
WHERE announcement.id = 'welcome-to-peergo'
  AND announcement.latest_revision_number = 0
ON CONFLICT (announcement_id, revision_number) DO NOTHING;

UPDATE catalog.announcements AS announcement
SET
    published_revision_id = revision.id,
    published_at = announcement.created_at,
    latest_revision_number = 1
FROM catalog.announcement_revisions AS revision
WHERE announcement.id = 'welcome-to-peergo'
  AND announcement.latest_revision_number = 0
  AND revision.announcement_id = announcement.id
  AND revision.revision_number = 1;

-- Early development databases published implementation notes in the welcome
-- announcement. Revisions are immutable, so the compatibility path appends a
-- corrected public revision instead of mutating history. Every source field,
-- aggregate pointer and workflow state is matched exactly; staff-authored,
-- imported or otherwise customized announcements can never enter this path.
WITH legacy_welcome AS (
    SELECT
        announcement.id AS announcement_id,
        announcement.latest_revision_number + 1 AS next_revision_number
    FROM catalog.announcements AS announcement
    JOIN catalog.announcement_revisions AS published
      ON published.id = announcement.published_revision_id
    WHERE announcement.id = 'welcome-to-peergo'
      AND announcement.latest_revision_number = published.revision_number
      AND published.revision_number <= 2
      AND published.title = '欢迎来到 PeerGo'
      AND published.summary = '请在发布和下载前阅读站点规则，共同维护长期、稳定的分享环境。'
      AND published.body = E'PeerGo 希望建立一个重视内容质量、长期做种与友善交流的分享社区。\n\n发布资源前请核对文件、标题与分类；下载完成后请尽量保持做种。讨论时围绕内容本身交流，不公开他人的个人信息。\n\n首版公告正文保持纯文本；从 PtYes 迁移的旧公告会保留 legacy_bbcode 格式标记，但不会直接执行旧站 HTML 或 BBCode。'
      AND published.body_format = 'plain_text'
      AND published.origin IN ('migration', 'development_seed')
      AND published.created_by_user_id IS NULL
      AND announcement.draft_revision_id IS NULL
      AND announcement.scheduled_revision_id IS NULL
      AND announcement.withdrawn_at IS NULL
), corrected_welcome AS (
    INSERT INTO catalog.announcement_revisions (
        announcement_id,
        revision_number,
        title,
        summary,
        body,
        body_format,
        origin,
        created_at
    )
    SELECT
        legacy.announcement_id,
        legacy.next_revision_number,
        '欢迎来到 PeerGo',
        '请在发布和下载前阅读站点规则，共同维护长期、稳定的分享环境。',
        E'PeerGo 希望建立一个重视内容质量、长期做种与友善交流的分享社区。\n\n发布资源前请核对文件、标题与分类；下载完成后请尽量保持做种。讨论时请围绕内容本身友善交流，不公开他人的个人信息。\n\n如遇到资源信息有误、文件异常或其他需要站务协助的情况，请通过页面提供的举报或反馈入口联系管理团队。',
        'plain_text',
        'development_seed',
        now()
    FROM legacy_welcome AS legacy
    ON CONFLICT (announcement_id, revision_number) DO NOTHING
    RETURNING id, announcement_id, revision_number, created_at
)
UPDATE catalog.announcements AS announcement
SET
    published_revision_id = corrected.id,
    latest_revision_number = corrected.revision_number,
    version = version + 1,
    updated_at = corrected.created_at
FROM corrected_welcome AS corrected
WHERE announcement.id = corrected.announcement_id;

-- Early UI work used six catalog-only rows with slug IDs. They could populate
-- a dense table but had no torrent aggregate, object, detail page or Tracker
-- identity, so they also inflated category counts. Remove only the exact
-- synthetic rows; the Core devseed below creates one real published aggregate
-- through upload, review and object storage instead.
WITH obsolete_fixture(id, name) AS (
    VALUES
        ('cosmos-restored', 'Cosmos Restored 2026 2160p WEB-DL HDR'),
        ('night-train-s2', 'Night Train S02 1080p WEB-DL H.265 AAC'),
        ('paper-cranes', 'Paper Cranes 2025 BluRay 1080p AVC DTS-HD MA'),
        ('harbor-lights', 'Harbor Lights 1968 Restored 1080p BluRay'),
        ('tiny-orchestra', 'Tiny Orchestra Live 2026 FLAC 24bit 96kHz'),
        ('sky-garden', 'Sky Garden Complete Collection 1080p')
)
DELETE FROM catalog.torrents AS torrent
USING obsolete_fixture AS fixture
WHERE torrent.id = fixture.id
  AND torrent.name = fixture.name;

-- This user points at the matching synthetic credential created by the Vault
-- dev seed command. It contains no email, password hash, passkey or real data.
INSERT INTO identity.users (
    id,
    credential_ref,
    username,
    display_name,
    status,
    email_verified_at
) VALUES (
    '0198f20a-6da8-7e51-9c64-111111111111',
    '0198f20a-6da8-7e51-9c64-222222222222',
    'demo',
    '星河旅人',
    'active',
    now()
)
ON CONFLICT (id) DO UPDATE SET
    credential_ref = EXCLUDED.credential_ref,
    username = EXCLUDED.username,
    display_name = EXCLUDED.display_name,
    status = EXCLUDED.status,
    email_verified_at = EXCLUDED.email_verified_at,
    updated_at = now();

-- A third non-login member creates the moderation fixture without making the
-- demo staff operator the comment author or reporter of the same case.
INSERT INTO identity.users (
    id,
    credential_ref,
    username,
    display_name,
    status,
    email_verified_at
) VALUES (
    '019fcd83-57de-7240-a0d3-95908cdb4520',
    '019fcd83-57de-7240-a0d3-95908cdb4521',
    'moderation-reporter',
    '评论举报演示成员',
    'active',
    now()
)
ON CONFLICT (id) DO UPDATE SET
    credential_ref = EXCLUDED.credential_ref,
    username = EXCLUDED.username,
    display_name = EXCLUDED.display_name,
    status = EXCLUDED.status,
    email_verified_at = EXCLUDED.email_verified_at,
    updated_at = now();

-- The post-announce traffic corpus needs a stable public torrent reference so
-- Core can project a title without reaching into Tracker Ledger. This metadata
-- fixture is deliberately pending review and has no object location: it cannot
-- be downloaded or admitted by Tracker, and no accounting row is seeded here.
INSERT INTO torrents.torrent_objects (
    id,
    content_sha256,
    byte_length,
    parser_version,
    validation_profile,
    compatibility_flags,
    info_offset,
    info_length,
    created_at
) VALUES (
    '019fcd83-57de-7240-a0d3-95908cdb4302',
    decode('c9614db8b55871b5c987b0f4f0cd5f58025f8477a72b6b03871705119cd20f1b', 'hex'),
    256,
    'corpus-v1',
    'legacy_import',
    ARRAY['development_corpus'],
    0,
    128,
    '2026-08-08T23:00:00Z'
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO torrents.torrents AS existing (
    id,
    uploader_id,
    category_id,
    object_id,
    info_hash_v1,
    content_name,
    title,
    subtitle,
    total_size_bytes,
    payload_size_bytes,
    file_count,
    padding_file_count,
    piece_length_bytes,
    piece_count,
    state,
    version,
    submitted_at,
    state_changed_at,
    updated_at
) VALUES (
    9000000001,
    '0198f20a-6da8-7e51-9c64-111111111111',
    'movies',
    '019fcd83-57de-7240-a0d3-95908cdb4302',
    decode('745607c7da40fbc7b073eb91bc5c595e46d81c49', 'hex'),
    'peergo-traffic-corpus.bin',
    'Harbor Echoes 2026 2160p WEB-DL HDR',
    '港湾回声 · 简繁字幕 · 双语音轨',
    3221225472,
    3221225472,
    1,
    0,
    1048576,
    3072,
    'pending_review',
    1,
    '2026-08-08T23:00:00Z',
    '2026-08-08T23:00:00Z',
    '2026-08-08T23:00:00Z'
)
ON CONFLICT (id) DO UPDATE SET
    title = EXCLUDED.title,
    subtitle = EXCLUDED.subtitle,
    version = existing.version + 1,
    updated_at = GREATEST(existing.updated_at, now())
WHERE existing.object_id = EXCLUDED.object_id
  AND (
      existing.title IS DISTINCT FROM EXCLUDED.title
      OR existing.subtitle IS DISTINCT FROM EXCLUDED.subtitle
  );

INSERT INTO torrents.torrent_files (
    torrent_id,
    file_index,
    path_components,
    display_path,
    size_bytes,
    is_padding,
    created_at
)
SELECT
    torrent.id,
    0,
    ARRAY['peergo-traffic-corpus.bin'],
    'peergo-traffic-corpus.bin',
    3221225472,
    false,
    '2026-08-08T23:00:00Z'
FROM torrents.torrents AS torrent
WHERE torrent.id = 9000000001
ON CONFLICT (torrent_id, file_index) DO NOTHING;

-- A second synthetic account gives the local grant-proposer flow a target
-- that is not the operator. It has no Vault credential and cannot log in.
INSERT INTO identity.users (
    id,
    credential_ref,
    username,
    display_name,
    status,
    email_verified_at
) VALUES (
    '0198f20a-6da8-7e51-9c64-666666666666',
    '0198f20a-6da8-7e51-9c64-777777777777',
    'demo-target',
    '远岸',
    'active',
    now()
)
ON CONFLICT (id) DO UPDATE SET
    credential_ref = EXCLUDED.credential_ref,
    username = EXCLUDED.username,
    display_name = EXCLUDED.display_name,
    status = EXCLUDED.status,
    email_verified_at = EXCLUDED.email_verified_at,
    updated_at = now();

-- Synthetic, development-only legacy evidence for the read projection. It is
-- reset directly so local demos always have a current restriction to inspect;
-- production writes must use the audited command and never run this seed.
INSERT INTO identity.account_restrictions (
    id,
    user_id,
    kind,
    reason_code,
    reason_summary,
    starts_at,
    expires_at,
    created_by,
    version
) VALUES (
    '0198f20a-6da8-7e51-9c64-eeeeeeeeeeee',
    '0198f20a-6da8-7e51-9c64-666666666666',
    'account_access',
    'legacy_import',
    '旧站迁移演示：临时限制账户访问，等待人工复核。',
    now() - interval '1 day',
    now() + interval '6 days',
    '0198f20a-6da8-7e51-9c64-111111111111',
    1
)
ON CONFLICT (id) DO UPDATE SET
    starts_at = EXCLUDED.starts_at,
    expires_at = EXCLUDED.expires_at,
    revoked_at = NULL,
    revoked_by = NULL,
    revocation_reason_code = NULL,
    revocation_reason = NULL,
    version = identity.account_restrictions.version + 1,
    updated_at = now();

-- The fixture reset above is an account-administration change too. Advancing
-- the user-side version keeps optimistic concurrency honest across re-seeds.
UPDATE identity.users
SET
    administration_version = administration_version + 1,
    updated_at = now()
WHERE id = '0198f20a-6da8-7e51-9c64-666666666666';

INSERT INTO governance.mandates (
    id,
    subject_id,
    source_type,
    source_reference,
    scope_type,
    scope_id,
    starts_at,
    ends_at,
    status
) VALUES (
    '0198f20a-6da8-7e51-9c64-888888888888',
    '0198f20a-6da8-7e51-9c64-666666666666',
    'bootstrap',
    'development-governance-target',
    'site',
    'peergo',
    now() - interval '1 day',
    now() + interval '365 days',
    'active'
)
ON CONFLICT (id) DO UPDATE SET
    starts_at = EXCLUDED.starts_at,
    ends_at = EXCLUDED.ends_at,
    status = EXCLUDED.status,
    updated_at = now();

INSERT INTO governance.mandates (
    id,
    subject_id,
    source_type,
    source_reference,
    scope_type,
    scope_id,
    starts_at,
    ends_at,
    status
) VALUES (
    '019fcd83-57de-7240-a0d3-95908cdb4522',
    '019fcd83-57de-7240-a0d3-95908cdb4520',
    'bootstrap',
    'development-comment-moderation-reporter',
    'site',
    'peergo',
    now() - interval '1 day',
    now() + interval '365 days',
    'active'
)
ON CONFLICT (id) DO UPDATE SET
    starts_at = EXCLUDED.starts_at,
    ends_at = EXCLUDED.ends_at,
    status = EXCLUDED.status,
    updated_at = now();

-- The synthetic member grant proves the complete mandate -> role -> permission
-- path. Its finite window is refreshed by repeatable development seeding.
INSERT INTO governance.mandates (
    id,
    subject_id,
    source_type,
    source_reference,
    scope_type,
    scope_id,
    starts_at,
    ends_at,
    status
) VALUES (
    '0198f20a-6da8-7e51-9c64-333333333333',
    '0198f20a-6da8-7e51-9c64-111111111111',
    'bootstrap',
    'development-seed',
    'site',
    'peergo',
    now() - interval '1 day',
    now() + interval '365 days',
    'active'
)
ON CONFLICT (id) DO UPDATE SET
    starts_at = EXCLUDED.starts_at,
    ends_at = EXCLUDED.ends_at,
    status = EXCLUDED.status,
    updated_at = now();

-- The read-only role remains useful for staff who may inspect but never mutate
-- restrictions. It does not imply access to private identifiers or ledgers.
INSERT INTO authz.grants (
    id,
    subject_id,
    role_id,
    mandate_id,
    scope_type,
    scope_id,
    valid_from,
    valid_until,
    constraints,
    version
) VALUES (
    '0198f20a-6da8-7e51-9c64-dddddddddddd',
    '0198f20a-6da8-7e51-9c64-111111111111',
    'user_reader',
    '0198f20a-6da8-7e51-9c64-333333333333',
    'site',
    'peergo',
    now() - interval '1 day',
    now() + interval '365 days',
    '{"mfa_max_age_seconds":900}'::jsonb,
    1
)
ON CONFLICT (id) DO UPDATE SET
    valid_from = EXCLUDED.valid_from,
    valid_until = EXCLUDED.valid_until,
    constraints = EXCLUDED.constraints,
    version = authz.grants.version + 1,
    revoked_at = NULL,
    updated_at = now();

-- Torrent review is intentionally its own finite staff grant. The demo
-- reviewer may decide one pending torrent at a time, but receives no category
-- administration, Tracker operations, storage cleanup or batch power from it.
INSERT INTO authz.grants (
    id,
    subject_id,
    role_id,
    mandate_id,
    scope_type,
    scope_id,
    valid_from,
    valid_until,
    constraints,
    version
) VALUES (
    '0198f20a-6da8-7e51-9c64-121212121212',
    '0198f20a-6da8-7e51-9c64-111111111111',
    'torrent_reviewer',
    '0198f20a-6da8-7e51-9c64-333333333333',
    'site',
    'peergo',
    now() - interval '1 day',
    now() + interval '365 days',
    '{"mfa_max_age_seconds":900}'::jsonb,
    1
)
ON CONFLICT (id) DO UPDATE SET
    valid_from = EXCLUDED.valid_from,
    valid_until = EXCLUDED.valid_until,
    constraints = EXCLUDED.constraints,
    version = authz.grants.version + 1,
    revoked_at = NULL,
    updated_at = now();

-- Comment moderation is grouped under the content domain and cannot mutate
-- users, categories, torrents, settings, grants or audit evidence.
INSERT INTO authz.grants (
    id,
    subject_id,
    role_id,
    mandate_id,
    scope_type,
    scope_id,
    valid_from,
    valid_until,
    constraints,
    version
) VALUES (
    '019fcd83-57de-7240-a0d3-95908cdb4530',
    '0198f20a-6da8-7e51-9c64-111111111111',
    'community_moderator',
    '0198f20a-6da8-7e51-9c64-333333333333',
    'site',
    'peergo',
    now() - interval '1 day',
    now() + interval '365 days',
    '{"mfa_max_age_seconds":900}'::jsonb,
    1
)
ON CONFLICT (id) DO UPDATE SET
    valid_from = EXCLUDED.valid_from,
    valid_until = EXCLUDED.valid_until,
    constraints = EXCLUDED.constraints,
    version = authz.grants.version + 1,
    revoked_at = NULL,
    updated_at = now();

-- A separate finite role demonstrates the bounded create/revoke command. It
-- adds no permanent-ban, privacy, credential, traffic or economy capability.
INSERT INTO authz.grants (
    id,
    subject_id,
    role_id,
    mandate_id,
    scope_type,
    scope_id,
    valid_from,
    valid_until,
    constraints,
    version
) VALUES (
    '0198f20a-6da8-7e51-9c64-ffffffffffff',
    '0198f20a-6da8-7e51-9c64-111111111111',
    'user_access_operator',
    '0198f20a-6da8-7e51-9c64-333333333333',
    'site',
    'peergo',
    now() - interval '1 day',
    now() + interval '365 days',
    '{"mfa_max_age_seconds":900}'::jsonb,
    1
)
ON CONFLICT (id) DO UPDATE SET
    valid_from = EXCLUDED.valid_from,
    valid_until = EXCLUDED.valid_until,
    constraints = EXCLUDED.constraints,
    version = authz.grants.version + 1,
    revoked_at = NULL,
    updated_at = now();

INSERT INTO authz.grants (
    id,
    subject_id,
    role_id,
    mandate_id,
    scope_type,
    scope_id,
    valid_from,
    valid_until,
    constraints,
    version
) VALUES (
    '0198f20a-6da8-7e51-9c64-444444444444',
    '0198f20a-6da8-7e51-9c64-111111111111',
    'member',
    '0198f20a-6da8-7e51-9c64-333333333333',
    'site',
    'peergo',
    now() - interval '1 day',
    now() + interval '365 days',
    '{}'::jsonb,
    1
)
ON CONFLICT (id) DO UPDATE SET
    valid_from = EXCLUDED.valid_from,
    valid_until = EXCLUDED.valid_until,
    constraints = EXCLUDED.constraints,
    version = authz.grants.version + 1,
    revoked_at = NULL,
    updated_at = now();

-- This grant only makes the synthetic account eligible to start a staff
-- WebAuthn assertion. It contains no business administration permission, and
-- no staff session can be created until a bootstrap/imported credential exists.
INSERT INTO authz.grants (
    id,
    subject_id,
    role_id,
    mandate_id,
    scope_type,
    scope_id,
    valid_from,
    valid_until,
    constraints,
    version
) VALUES (
    '0198f20a-6da8-7e51-9c64-555555555555',
    '0198f20a-6da8-7e51-9c64-111111111111',
    'staff_access',
    '0198f20a-6da8-7e51-9c64-333333333333',
    'site',
    'peergo',
    now() - interval '1 day',
    now() + interval '365 days',
    '{}'::jsonb,
    1
)
ON CONFLICT (id) DO UPDATE SET
    valid_from = EXCLUDED.valid_from,
    valid_until = EXCLUDED.valid_until,
    constraints = EXCLUDED.constraints,
    version = authz.grants.version + 1,
    revoked_at = NULL,
    updated_at = now();

-- The demo staff can read grants and propose a reduction, but cannot approve
-- it in either duty domain. This keeps the local UI useful without weakening
-- the two-account review constraints or granting a write bypass.
INSERT INTO authz.grants (
    id,
    subject_id,
    role_id,
    mandate_id,
    scope_type,
    scope_id,
    valid_from,
    valid_until,
    constraints,
    version
) VALUES (
    '0198f20a-6da8-7e51-9c64-999999999999',
    '0198f20a-6da8-7e51-9c64-111111111111',
    'grant_proposer',
    '0198f20a-6da8-7e51-9c64-333333333333',
    'site',
    'peergo',
    now() - interval '1 day',
    now() + interval '365 days',
    '{"mfa_max_age_seconds":900}'::jsonb,
    1
)
ON CONFLICT (id) DO UPDATE SET
    valid_from = EXCLUDED.valid_from,
    valid_until = EXCLUDED.valid_until,
    constraints = EXCLUDED.constraints,
    version = authz.grants.version + 1,
    revoked_at = NULL,
    updated_at = now();

-- Category administration is a separate finite grant. It does not expand the
-- grant-proposer role or make the staff-access eligibility grant authoritative
-- for catalog writes.
INSERT INTO authz.grants (
    id,
    subject_id,
    role_id,
    mandate_id,
    scope_type,
    scope_id,
    valid_from,
    valid_until,
    constraints,
    version
) VALUES (
    '0198f20a-6da8-7e51-9c64-bbbbbbbbbbbb',
    '0198f20a-6da8-7e51-9c64-111111111111',
    'category_manager',
    '0198f20a-6da8-7e51-9c64-333333333333',
    'site',
    'peergo',
    now() - interval '1 day',
    now() + interval '365 days',
    '{"mfa_max_age_seconds":900}'::jsonb,
    1
)
ON CONFLICT (id) DO UPDATE SET
    valid_from = EXCLUDED.valid_from,
    valid_until = EXCLUDED.valid_until,
    constraints = EXCLUDED.constraints,
    version = authz.grants.version + 1,
    revoked_at = NULL,
    updated_at = now();

-- Announcement editing and publication use their own finite content grant.
-- It cannot change categories, site settings, comments, users or grants.
INSERT INTO authz.grants (
    id,
    subject_id,
    role_id,
    mandate_id,
    scope_type,
    scope_id,
    valid_from,
    valid_until,
    constraints,
    version
) VALUES (
    '019fcd83-57de-7240-a0d3-d1d1d1d1d1d1',
    '0198f20a-6da8-7e51-9c64-111111111111',
    'announcement_manager',
    '0198f20a-6da8-7e51-9c64-333333333333',
    'site',
    'peergo',
    now() - interval '1 day',
    now() + interval '365 days',
    '{"mfa_max_age_seconds":900}'::jsonb,
    1
)
ON CONFLICT (id) DO UPDATE SET
    valid_from = EXCLUDED.valid_from,
    valid_until = EXCLUDED.valid_until,
    constraints = EXCLUDED.constraints,
    version = authz.grants.version + 1,
    revoked_at = NULL,
    updated_at = now();

-- Site/display administration is isolated from category administration and
-- identity admission. It only grants the low-risk typed settings section.
INSERT INTO authz.grants (
    id,
    subject_id,
    role_id,
    mandate_id,
    scope_type,
    scope_id,
    valid_from,
    valid_until,
    constraints,
    version
) VALUES (
    '0198f20a-6da8-7e51-9c64-cccccccccccc',
    '0198f20a-6da8-7e51-9c64-111111111111',
    'site_display_manager',
    '0198f20a-6da8-7e51-9c64-333333333333',
    'site',
    'peergo',
    now() - interval '1 day',
    now() + interval '365 days',
    '{"mfa_max_age_seconds":900}'::jsonb,
    1
)
ON CONFLICT (id) DO UPDATE SET
    valid_from = EXCLUDED.valid_from,
    valid_until = EXCLUDED.valid_until,
    constraints = EXCLUDED.constraints,
    version = authz.grants.version + 1,
    revoked_at = NULL,
    updated_at = now();

-- The non-login target account owns the development torrent fixture. Its
-- ordinary member role exercises real upload/comment authorization without
-- expanding staff authority or creating a second reusable credential.
INSERT INTO authz.grants (
    id,
    subject_id,
    role_id,
    mandate_id,
    scope_type,
    scope_id,
    valid_from,
    valid_until,
    constraints,
    version
) VALUES (
    '0198f20a-6da8-7e51-9c64-aaaaaaaaaaaa',
    '0198f20a-6da8-7e51-9c64-666666666666',
    'member',
    '0198f20a-6da8-7e51-9c64-888888888888',
    'site',
    'peergo',
    now() - interval '1 day',
    now() + interval '365 days',
    '{}'::jsonb,
    1
)
ON CONFLICT (id) DO UPDATE SET
    valid_from = EXCLUDED.valid_from,
    valid_until = EXCLUDED.valid_until,
    constraints = EXCLUDED.constraints,
    version = authz.grants.version + 1,
    revoked_at = NULL,
    updated_at = now();

-- The reporter fixture is an ordinary member and receives no staff grant.
INSERT INTO authz.grants (
    id,
    subject_id,
    role_id,
    mandate_id,
    scope_type,
    scope_id,
    valid_from,
    valid_until,
    constraints,
    version
) VALUES (
    '019fcd83-57de-7240-a0d3-95908cdb4523',
    '019fcd83-57de-7240-a0d3-95908cdb4520',
    'member',
    '019fcd83-57de-7240-a0d3-95908cdb4522',
    'site',
    'peergo',
    now() - interval '1 day',
    now() + interval '365 days',
    '{}'::jsonb,
    1
)
ON CONFLICT (id) DO UPDATE SET
    valid_from = EXCLUDED.valid_from,
    valid_until = EXCLUDED.valid_until,
    constraints = EXCLUDED.constraints,
    version = authz.grants.version + 1,
    revoked_at = NULL,
    updated_at = now();

COMMIT;
