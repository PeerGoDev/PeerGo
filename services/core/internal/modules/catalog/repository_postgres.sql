-- name: GetSiteInfo :one
SELECT
    site.name,
    site.description,
    (
        SELECT count(DISTINCT session.user_id)::integer
        FROM identity.sessions AS session
        INNER JOIN identity.users AS users ON users.id = session.user_id
        WHERE session.audience = 'web'
          AND session.revoked_at IS NULL
          AND session.expires_at > sqlc.arg(as_of)::timestamptz
          AND session.last_seen_at >= sqlc.arg(as_of)::timestamptz - interval '15 minutes'
          AND users.status = 'active'
          AND NOT EXISTS (
              SELECT 1
              FROM identity.account_restrictions AS restriction
              WHERE restriction.user_id = users.id
                AND restriction.kind = 'account_access'
                AND restriction.revoked_at IS NULL
                AND restriction.starts_at <= sqlc.arg(as_of)::timestamptz
                AND restriction.expires_at > sqlc.arg(as_of)::timestamptz
          )
    ) AS online_users,
    site.default_torrent_view,
    site.show_latest_announcement,
    site.custom_navigation_items
FROM catalog.site_profile AS site
WHERE site.singleton = true;

-- name: GetLatestAnnouncement :one
SELECT
    announcement.id,
    announcement.title,
    announcement.summary,
    announcement.published_at
FROM catalog.public_announcement_projection AS announcement
CROSS JOIN catalog.site_profile AS site
WHERE site.singleton = true
  AND site.show_latest_announcement = true
ORDER BY announcement.published_at DESC, announcement.id DESC
LIMIT 1;

-- name: GetPublishedAnnouncement :one
SELECT
    announcement.id,
    announcement.title,
    announcement.summary,
    announcement.body,
    announcement.body_format,
    announcement.version,
    announcement.published_at,
    announcement.updated_at
FROM catalog.public_announcement_projection AS announcement
WHERE announcement.id = sqlc.arg(announcement_id)::text
LIMIT 1;

-- name: CountPublishedAnnouncements :one
SELECT count(*)::bigint
FROM catalog.public_announcement_projection;

-- name: ListPublishedAnnouncements :many
SELECT
    announcement.id,
    announcement.title,
    announcement.summary,
    announcement.published_at
FROM catalog.public_announcement_projection AS announcement
ORDER BY announcement.published_at DESC, announcement.id DESC
LIMIT sqlc.arg(result_limit)::integer
OFFSET sqlc.arg(result_offset)::integer;

-- name: ListManagedAnnouncements :many
SELECT
    projection.id,
    projection.title,
    projection.summary,
    projection.body,
    projection.body_format,
    projection.version,
    projection.revision_number,
    projection.has_unpublished_changes,
    projection.has_published_revision,
    projection.has_scheduled_revision,
    projection.published_at,
    projection.scheduled_for,
    projection.withdrawn_at,
    projection.created_at,
    projection.updated_at,
    count(*) OVER ()::bigint AS total_count
FROM catalog.managed_announcement_projection AS projection
ORDER BY projection.updated_at DESC, projection.id
LIMIT sqlc.arg(result_limit)::integer
OFFSET sqlc.arg(result_offset)::integer;

-- name: CountManagedAnnouncements :one
SELECT count(*)::bigint
FROM catalog.announcements;

-- name: GetManagedAnnouncement :one
SELECT
    id,
    title,
    summary,
    body,
    body_format,
    version,
    revision_number,
    has_unpublished_changes,
    has_published_revision,
    has_scheduled_revision,
    published_at,
    scheduled_for,
    withdrawn_at,
    created_at,
    updated_at
FROM catalog.managed_announcement_projection
WHERE id = sqlc.arg(announcement_id)::text;

-- name: ListAnnouncementRevisions :many
SELECT
    revision.revision_number,
    revision.title,
    revision.summary,
    revision.body_format,
    revision.origin,
    editor.display_name AS editor_display_name,
    coalesce(announcement.draft_revision_id = revision.id, false)::boolean AS is_draft,
    coalesce(announcement.published_revision_id = revision.id, false)::boolean AS is_published,
    coalesce(announcement.scheduled_revision_id = revision.id, false)::boolean AS is_scheduled,
    revision.created_at,
    count(*) OVER ()::bigint AS total_count
FROM catalog.announcement_revisions AS revision
JOIN catalog.announcements AS announcement
  ON announcement.id = revision.announcement_id
LEFT JOIN identity.users AS editor
  ON editor.id = revision.created_by_user_id
WHERE revision.announcement_id = sqlc.arg(announcement_id)::text
ORDER BY revision.revision_number DESC
LIMIT sqlc.arg(result_limit)::integer
OFFSET sqlc.arg(result_offset)::integer;

-- name: CountAnnouncementRevisions :one
SELECT count(*)::bigint
FROM catalog.announcement_revisions
WHERE announcement_id = sqlc.arg(announcement_id)::text;

-- name: CreateAnnouncementAggregate :one
INSERT INTO catalog.announcements (
    id,
    version,
    latest_revision_number,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(announcement_id)::text,
    1,
    0,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(occurred_at)::timestamptz
)
RETURNING id;

-- name: InsertAnnouncementRevision :one
INSERT INTO catalog.announcement_revisions (
    announcement_id,
    revision_number,
    title,
    summary,
    body,
    body_format,
    created_by_user_id,
    origin,
    created_at
) VALUES (
    sqlc.arg(announcement_id)::text,
    sqlc.arg(revision_number)::bigint,
    sqlc.arg(title)::text,
    sqlc.arg(summary)::text,
    sqlc.arg(body)::text,
    sqlc.arg(body_format)::text,
    sqlc.arg(created_by_user_id)::uuid,
    'staff',
    sqlc.arg(occurred_at)::timestamptz
)
RETURNING id;

-- name: AttachInitialAnnouncementDraft :execrows
UPDATE catalog.announcements
SET
    draft_revision_id = sqlc.arg(revision_id)::bigint,
    latest_revision_number = 1,
    updated_at = sqlc.arg(occurred_at)::timestamptz
WHERE id = sqlc.arg(announcement_id)::text
  AND version = 1
  AND latest_revision_number = 0;

-- name: GetAnnouncementAggregateForUpdate :one
SELECT
    id,
    version,
    latest_revision_number,
    draft_revision_id,
    published_revision_id,
    scheduled_revision_id,
    published_at,
    scheduled_for,
    withdrawn_at,
    created_at,
    updated_at
FROM catalog.announcements
WHERE id = sqlc.arg(announcement_id)::text
FOR UPDATE;

-- name: GetAnnouncementRevisionByID :one
SELECT
    id,
    announcement_id,
    revision_number,
    title,
    summary,
    body,
    body_format,
    created_at
FROM catalog.announcement_revisions
WHERE id = sqlc.arg(revision_id)::bigint;

-- name: UpdateAnnouncementDraftPointer :execrows
UPDATE catalog.announcements
SET
    draft_revision_id = sqlc.arg(revision_id)::bigint,
    latest_revision_number = sqlc.arg(revision_number)::bigint,
    version = version + 1,
    updated_at = sqlc.arg(occurred_at)::timestamptz
WHERE id = sqlc.arg(announcement_id)::text
  AND version = sqlc.arg(expected_version)::bigint;

-- name: PublishAnnouncementDraftNow :execrows
UPDATE catalog.announcements
SET
    published_revision_id = draft_revision_id,
    published_at = sqlc.arg(occurred_at)::timestamptz,
    draft_revision_id = NULL,
    scheduled_revision_id = NULL,
    scheduled_for = NULL,
    withdrawn_at = NULL,
    version = version + 1,
    updated_at = sqlc.arg(occurred_at)::timestamptz
WHERE id = sqlc.arg(announcement_id)::text
  AND version = sqlc.arg(expected_version)::bigint
  AND draft_revision_id IS NOT NULL;

-- name: ScheduleAnnouncementDraft :execrows
UPDATE catalog.announcements
SET
    published_revision_id = CASE
        WHEN withdrawn_at IS NOT NULL THEN NULL
        ELSE published_revision_id
    END,
    published_at = CASE
        WHEN withdrawn_at IS NOT NULL THEN NULL
        ELSE published_at
    END,
    scheduled_revision_id = draft_revision_id,
    scheduled_for = sqlc.arg(scheduled_for)::timestamptz,
    draft_revision_id = NULL,
    withdrawn_at = NULL,
    version = version + 1,
    updated_at = sqlc.arg(occurred_at)::timestamptz
WHERE id = sqlc.arg(announcement_id)::text
  AND version = sqlc.arg(expected_version)::bigint
  AND draft_revision_id IS NOT NULL;

-- name: CancelAnnouncementSchedule :execrows
UPDATE catalog.announcements
SET
    draft_revision_id = scheduled_revision_id,
    scheduled_revision_id = NULL,
    scheduled_for = NULL,
    version = version + 1,
    updated_at = sqlc.arg(occurred_at)::timestamptz
WHERE id = sqlc.arg(announcement_id)::text
  AND version = sqlc.arg(expected_version)::bigint
  AND draft_revision_id IS NULL
  AND scheduled_revision_id IS NOT NULL
  AND scheduled_for > sqlc.arg(occurred_at)::timestamptz;

-- name: WithdrawAnnouncement :execrows
UPDATE catalog.announcements
SET
    draft_revision_id = COALESCE(draft_revision_id, scheduled_revision_id),
    scheduled_revision_id = NULL,
    scheduled_for = NULL,
    withdrawn_at = sqlc.arg(occurred_at)::timestamptz,
    version = version + 1,
    updated_at = sqlc.arg(occurred_at)::timestamptz
WHERE id = sqlc.arg(announcement_id)::text
  AND version = sqlc.arg(expected_version)::bigint
  AND (
      published_revision_id IS NOT NULL
      OR scheduled_revision_id IS NOT NULL
  );

-- name: GetSiteDisplaySettings :one
SELECT
    name,
    description,
    torrent_filename_prefix,
    custom_navigation_items,
    default_torrent_view,
    show_latest_announcement,
    version,
    effective_at,
    updated_at
FROM catalog.site_profile
WHERE singleton = true;

-- name: GetSiteDisplaySettingsForUpdate :one
SELECT
    name,
    description,
    torrent_filename_prefix,
    custom_navigation_items,
    default_torrent_view,
    show_latest_announcement,
    version,
    effective_at,
    updated_at
FROM catalog.site_profile
WHERE singleton = true
FOR UPDATE;

-- name: UpdateSiteDisplaySettings :one
UPDATE catalog.site_profile
SET
    name = sqlc.arg(site_name)::text,
    description = sqlc.arg(site_description)::text,
    torrent_filename_prefix = sqlc.arg(torrent_filename_prefix)::text,
    custom_navigation_items = sqlc.arg(custom_navigation_items)::jsonb,
    default_torrent_view = sqlc.arg(default_torrent_view)::text,
    show_latest_announcement = sqlc.arg(show_latest_announcement)::boolean,
    version = version + 1,
    effective_at = sqlc.arg(occurred_at)::timestamptz,
    updated_at = sqlc.arg(occurred_at)::timestamptz
WHERE singleton = true
  AND version = sqlc.arg(expected_version)::bigint
RETURNING
    name,
    description,
    torrent_filename_prefix,
    custom_navigation_items,
    default_torrent_view,
    show_latest_announcement,
    version,
    effective_at,
    updated_at;

-- name: ListEnabledCategories :many
-- Public counts follow the write-side aggregate, not an orphaned read-model
-- row. This keeps category totals aligned with torrents that can open a real
-- detail page and resolve immutable torrent/image objects.
SELECT
    category.id,
    category.name,
    count(torrent.id)::bigint AS torrent_count
FROM catalog.categories AS category
LEFT JOIN torrents.torrents AS torrent
  ON torrent.category_id = category.id
 AND torrent.state = 'published'
WHERE category.enabled = true
GROUP BY category.id
ORDER BY category.display_order, category.id;

-- name: EnabledCategoryExists :one
SELECT EXISTS (
    SELECT 1
    FROM catalog.categories AS category
    WHERE category.id = sqlc.arg(category_id)::text
      AND category.enabled = true
)::boolean AS exists;

-- name: ListEnabledCategoryFacetOptions :many
SELECT
    facet.id AS facet_id,
    COALESCE(binding.name_override, facet.name)::text AS facet_name,
    binding.selection_mode,
    binding.required,
    COALESCE(binding.requirement_group, '')::text AS requirement_group,
    option.option_key,
    COALESCE(allowed.label_override, option.label)::text AS option_label
FROM catalog.categories AS category
JOIN catalog.category_facets AS binding
  ON binding.category_id = category.id
JOIN catalog.facet_definitions AS facet
  ON facet.id = binding.facet_id
 AND facet.selection_mode = binding.selection_mode
 AND facet.enabled = true
JOIN catalog.category_facet_options AS allowed
  ON allowed.category_id = binding.category_id
 AND allowed.facet_id = binding.facet_id
 AND allowed.selection_mode = binding.selection_mode
JOIN catalog.facet_options AS option
  ON option.facet_id = allowed.facet_id
 AND option.option_key = allowed.option_key
 AND option.selection_mode = allowed.selection_mode
 AND option.enabled = true
WHERE category.id = sqlc.arg(category_id)::text
  AND category.enabled = true
  AND binding.enabled = true
  AND allowed.enabled = true
ORDER BY binding.display_order, facet.id, allowed.display_order, option.option_key;

-- name: ListManagedCategories :many
-- Staff impact counts include every aggregate state because disabling a
-- category affects review, moderation, and historical records as well as the
-- currently public catalog.
SELECT
    category.id,
    category.name,
    category.display_order,
    category.enabled,
    category.version,
    category.created_at,
    category.updated_at,
    count(torrent.id)::bigint AS torrent_count
FROM catalog.categories AS category
LEFT JOIN torrents.torrents AS torrent ON torrent.category_id = category.id
GROUP BY category.id
ORDER BY category.display_order, category.id;

-- name: ListManagedCategoryFacets :many
SELECT
    binding.category_id,
    facet.id AS facet_id,
    COALESCE(binding.name_override, facet.name)::text AS facet_name,
    facet.name AS canonical_name,
    binding.selection_mode,
    binding.required,
    COALESCE(binding.requirement_group, '')::text AS requirement_group,
    binding.display_order,
    binding.enabled,
    binding.version,
    binding.created_at,
    binding.updated_at,
    count(DISTINCT value.torrent_id)::bigint AS torrent_count
FROM catalog.category_facets AS binding
JOIN catalog.facet_definitions AS facet
  ON facet.id = binding.facet_id
 AND facet.selection_mode = binding.selection_mode
LEFT JOIN torrents.torrent_facet_values AS value
  ON value.category_id = binding.category_id
 AND value.facet_id = binding.facet_id
 AND value.selection_mode = binding.selection_mode
GROUP BY binding.category_id, facet.id, facet.name, binding.name_override,
         binding.selection_mode, binding.required, binding.requirement_group,
         binding.display_order, binding.enabled, binding.version,
         binding.created_at, binding.updated_at
ORDER BY binding.category_id, binding.display_order, facet.id;

-- name: ListManagedCategoryFacetOptions :many
SELECT
    binding.category_id,
    facet.id AS facet_id,
    COALESCE(binding.name_override, facet.name)::text AS facet_name,
    binding.selection_mode,
    binding.required,
    COALESCE(binding.requirement_group, '')::text AS requirement_group,
    binding.display_order AS facet_display_order,
    allowed.option_key,
    COALESCE(allowed.label_override, option.label)::text AS option_label,
    option.label AS canonical_label,
    allowed.display_order AS option_display_order,
    allowed.enabled,
    allowed.version,
    allowed.created_at,
    allowed.updated_at,
    count(value.torrent_id)::bigint AS torrent_count
FROM catalog.category_facets AS binding
JOIN catalog.facet_definitions AS facet
  ON facet.id = binding.facet_id
 AND facet.selection_mode = binding.selection_mode
JOIN catalog.category_facet_options AS allowed
  ON allowed.category_id = binding.category_id
 AND allowed.facet_id = binding.facet_id
 AND allowed.selection_mode = binding.selection_mode
JOIN catalog.facet_options AS option
  ON option.facet_id = allowed.facet_id
 AND option.option_key = allowed.option_key
 AND option.selection_mode = allowed.selection_mode
LEFT JOIN torrents.torrent_facet_values AS value
  ON value.category_id = allowed.category_id
 AND value.facet_id = allowed.facet_id
 AND value.option_key = allowed.option_key
 AND value.selection_mode = allowed.selection_mode
GROUP BY binding.category_id, facet.id, facet.name, binding.name_override, binding.selection_mode,
         binding.required, binding.requirement_group, binding.display_order,
         allowed.option_key, allowed.label_override, option.label,
         allowed.display_order, allowed.enabled, allowed.version,
         allowed.created_at, allowed.updated_at
ORDER BY binding.category_id, binding.display_order, facet.id,
         allowed.display_order, allowed.option_key;

-- name: GetCategoryFacetForOptionAdministration :one
SELECT
    binding.category_id,
    binding.facet_id,
    binding.selection_mode,
    COALESCE(binding.name_override, facet.name)::text AS facet_name
FROM catalog.category_facets AS binding
JOIN catalog.facet_definitions AS facet
  ON facet.id = binding.facet_id
 AND facet.selection_mode = binding.selection_mode
WHERE binding.category_id = sqlc.arg(category_id)::text
  AND binding.facet_id = sqlc.arg(facet_id)::text
FOR UPDATE OF binding;

-- name: CountManagedCategoryFacets :one
SELECT count(*)::bigint
FROM catalog.category_facets
WHERE category_id = sqlc.arg(category_id)::text;

-- name: GetFacetDefinitionForCategoryAdministration :one
SELECT id, name, selection_mode, enabled, display_order
FROM catalog.facet_definitions
WHERE id = sqlc.arg(facet_id)::text
FOR UPDATE;

-- name: InsertFacetDefinitionForCategoryAdministration :one
INSERT INTO catalog.facet_definitions (
    id, name, selection_mode, display_order, enabled, version,
    created_at, updated_at
) VALUES (
    sqlc.arg(facet_id)::text,
    sqlc.arg(facet_name)::text,
    sqlc.arg(selection_mode)::text,
    sqlc.arg(display_order)::integer,
    true,
    1,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(occurred_at)::timestamptz
)
ON CONFLICT DO NOTHING
RETURNING id, name, selection_mode, enabled, display_order;

-- name: GetManagedCategoryFacetForUpdate :one
SELECT
    binding.category_id,
    binding.facet_id,
    COALESCE(binding.name_override, facet.name)::text AS facet_name,
    facet.name AS canonical_name,
    binding.selection_mode,
    binding.required,
    COALESCE(binding.requirement_group, '')::text AS requirement_group,
    binding.display_order,
    binding.enabled,
    binding.version,
    binding.created_at,
    binding.updated_at,
    (SELECT count(DISTINCT value.torrent_id)::bigint
       FROM torrents.torrent_facet_values AS value
      WHERE value.category_id = binding.category_id
        AND value.facet_id = binding.facet_id
        AND value.selection_mode = binding.selection_mode) AS torrent_count
FROM catalog.category_facets AS binding
JOIN catalog.facet_definitions AS facet
  ON facet.id = binding.facet_id
 AND facet.selection_mode = binding.selection_mode
WHERE binding.category_id = sqlc.arg(category_id)::text
  AND binding.facet_id = sqlc.arg(facet_id)::text
FOR UPDATE OF binding;

-- name: InsertManagedCategoryFacet :one
INSERT INTO catalog.category_facets (
    category_id, facet_id, selection_mode, required, requirement_group,
    display_order, name_override, enabled, version, created_at, updated_at
) VALUES (
    sqlc.arg(category_id)::text,
    sqlc.arg(facet_id)::text,
    sqlc.arg(selection_mode)::text,
    sqlc.arg(required)::boolean,
    sqlc.narg(requirement_group)::text,
    sqlc.arg(display_order)::integer,
    sqlc.narg(name_override)::text,
    sqlc.arg(enabled)::boolean,
    1,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(occurred_at)::timestamptz
)
RETURNING category_id, facet_id, selection_mode, required,
          requirement_group, display_order, name_override, enabled,
          version, created_at, updated_at;

-- name: UpdateManagedCategoryFacet :one
UPDATE catalog.category_facets
SET required = sqlc.arg(required)::boolean,
    requirement_group = sqlc.narg(requirement_group)::text,
    display_order = sqlc.arg(display_order)::integer,
    name_override = sqlc.narg(name_override)::text,
    enabled = sqlc.arg(enabled)::boolean,
    version = version + 1,
    updated_at = sqlc.arg(occurred_at)::timestamptz
WHERE category_id = sqlc.arg(category_id)::text
  AND facet_id = sqlc.arg(facet_id)::text
  AND version = sqlc.arg(expected_version)::bigint
RETURNING category_id, facet_id, selection_mode, required,
          requirement_group, display_order, name_override, enabled,
          version, created_at, updated_at;

-- name: InsertCategoryFacetChange :exec
INSERT INTO catalog.category_facet_changes (
    id, category_id, facet_id, transition, actor_id, reason,
    expected_version, resulting_version, before_state, after_state,
    authorization_decision_id, occurred_at
) VALUES (
    sqlc.arg(change_id)::uuid,
    sqlc.arg(category_id)::text,
    sqlc.arg(facet_id)::text,
    sqlc.arg(transition)::text,
    sqlc.arg(actor_id)::uuid,
    sqlc.arg(reason)::text,
    sqlc.arg(expected_version)::bigint,
    sqlc.arg(resulting_version)::bigint,
    sqlc.narg(before_state)::jsonb,
    sqlc.arg(after_state)::jsonb,
    sqlc.arg(authorization_decision_id)::uuid,
    sqlc.arg(occurred_at)::timestamptz
);

-- name: GetCanonicalFacetOption :one
SELECT selection_mode, label, enabled
FROM catalog.facet_options
WHERE facet_id = sqlc.arg(facet_id)::text
  AND option_key = sqlc.arg(option_key)::text;

-- name: InsertCanonicalFacetOption :execrows
INSERT INTO catalog.facet_options (
    facet_id, option_key, selection_mode, label, display_order,
    enabled, created_at, updated_at
) VALUES (
    sqlc.arg(facet_id)::text,
    sqlc.arg(option_key)::text,
    sqlc.arg(selection_mode)::text,
    sqlc.arg(option_label)::text,
    sqlc.arg(display_order)::integer,
    true,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(occurred_at)::timestamptz
)
ON CONFLICT DO NOTHING;

-- name: GetManagedCategoryFacetOptionForUpdate :one
SELECT
    allowed.category_id,
    allowed.facet_id,
    allowed.option_key,
    allowed.selection_mode,
    COALESCE(allowed.label_override, option.label)::text AS option_label,
    option.label AS canonical_label,
    option.enabled AS canonical_enabled,
    allowed.display_order,
    allowed.enabled,
    allowed.version,
    allowed.created_at,
    allowed.updated_at,
    (SELECT count(*)::bigint
       FROM torrents.torrent_facet_values AS value
      WHERE value.category_id = allowed.category_id
        AND value.facet_id = allowed.facet_id
        AND value.option_key = allowed.option_key
        AND value.selection_mode = allowed.selection_mode) AS torrent_count
FROM catalog.category_facet_options AS allowed
JOIN catalog.facet_options AS option
  ON option.facet_id = allowed.facet_id
 AND option.option_key = allowed.option_key
 AND option.selection_mode = allowed.selection_mode
WHERE allowed.category_id = sqlc.arg(category_id)::text
  AND allowed.facet_id = sqlc.arg(facet_id)::text
  AND allowed.option_key = sqlc.arg(option_key)::text
FOR UPDATE OF allowed;

-- name: CountManagedCategoryFacetOptions :one
SELECT count(*)::bigint
FROM catalog.category_facet_options
WHERE category_id = sqlc.arg(category_id)::text
  AND facet_id = sqlc.arg(facet_id)::text;

-- name: InsertManagedCategoryFacetOption :one
INSERT INTO catalog.category_facet_options (
    category_id, facet_id, option_key, selection_mode, label_override,
    display_order, enabled, version, created_at, updated_at
) VALUES (
    sqlc.arg(category_id)::text,
    sqlc.arg(facet_id)::text,
    sqlc.arg(option_key)::text,
    sqlc.arg(selection_mode)::text,
    sqlc.narg(label_override)::text,
    sqlc.arg(display_order)::integer,
    sqlc.arg(enabled)::boolean,
    1,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(occurred_at)::timestamptz
)
RETURNING category_id, facet_id, option_key, selection_mode,
          label_override, display_order, enabled, version, created_at, updated_at;

-- name: UpdateManagedCategoryFacetOption :one
UPDATE catalog.category_facet_options
SET label_override = sqlc.narg(label_override)::text,
    display_order = sqlc.arg(display_order)::integer,
    enabled = sqlc.arg(enabled)::boolean,
    version = version + 1,
    updated_at = sqlc.arg(occurred_at)::timestamptz
WHERE category_id = sqlc.arg(category_id)::text
  AND facet_id = sqlc.arg(facet_id)::text
  AND option_key = sqlc.arg(option_key)::text
  AND version = sqlc.arg(expected_version)::bigint
RETURNING category_id, facet_id, option_key, selection_mode,
          label_override, display_order, enabled, version, created_at, updated_at;

-- name: InsertCategoryFacetOptionChange :exec
INSERT INTO catalog.category_facet_option_changes (
    id, category_id, facet_id, option_key, transition, actor_id, reason,
    expected_version, resulting_version, before_state, after_state,
    authorization_decision_id, occurred_at
) VALUES (
    sqlc.arg(change_id)::uuid,
    sqlc.arg(category_id)::text,
    sqlc.arg(facet_id)::text,
    sqlc.arg(option_key)::text,
    sqlc.arg(transition)::text,
    sqlc.arg(actor_id)::uuid,
    sqlc.arg(reason)::text,
    sqlc.arg(expected_version)::bigint,
    sqlc.arg(resulting_version)::bigint,
    sqlc.narg(before_state)::jsonb,
    sqlc.arg(after_state)::jsonb,
    sqlc.arg(authorization_decision_id)::uuid,
    sqlc.arg(occurred_at)::timestamptz
);

-- name: CreateManagedCategory :one
INSERT INTO catalog.categories (
    id,
    name,
    display_order,
    enabled,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(category_id)::text,
    sqlc.arg(category_name)::text,
    sqlc.arg(display_order)::integer,
    sqlc.arg(enabled)::boolean,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(occurred_at)::timestamptz
)
RETURNING id, name, display_order, enabled, version, created_at, updated_at;

-- name: GetManagedCategoryForUpdate :one
SELECT id, name, display_order, enabled, version, created_at, updated_at
FROM catalog.categories
WHERE id = sqlc.arg(category_id)::text
FOR UPDATE;

-- name: UpdateManagedCategory :one
UPDATE catalog.categories
SET
    name = sqlc.arg(category_name)::text,
    display_order = sqlc.arg(display_order)::integer,
    enabled = sqlc.arg(enabled)::boolean,
    version = version + 1,
    updated_at = sqlc.arg(occurred_at)::timestamptz
WHERE id = sqlc.arg(category_id)::text
  AND version = sqlc.arg(expected_version)::bigint
RETURNING id, name, display_order, enabled, version, created_at, updated_at;

-- name: CountCategoryTorrents :one
SELECT count(*)::bigint
FROM torrents.torrents
WHERE category_id = sqlc.arg(category_id)::text;

-- name: ListPublishedTorrents :many
-- catalog.torrents supplies the denormalized public presentation, while the
-- aggregate join is the publication authority. Never publish projection-only
-- fixtures: they have no valid detail, download, cover, or Tracker identity.
SELECT
    torrent.id,
    torrent.name,
    torrent.subtitle,
    torrent.size_bytes,
    coalesce(effective.promotion, CASE
        WHEN torrent.promotion_ends_at IS NOT NULL
         AND torrent.promotion_ends_at <= CURRENT_TIMESTAMP THEN 'none'
        ELSE torrent.promotion
    END)::text AS promotion,
	sticky.sticky_ends_at AS sticky_until,
    torrent.published_at,
    category.id AS category_id,
    category.name AS category_name,
    coalesce(swarm.seeders, 0)::integer AS seeders,
    coalesce(swarm.leechers, 0)::integer AS leechers,
    coalesce(completion.completed, swarm.completed, 0)::integer AS completed,
    coalesce(swarm.observed_at, to_timestamp(0))::timestamptz AS observed_at
FROM catalog.torrents AS torrent
JOIN torrents.torrents AS aggregate
  ON aggregate.id = torrent.id
 AND aggregate.state = 'published'
JOIN catalog.categories AS category
    ON category.id = torrent.category_id
   AND category.enabled = true
LEFT JOIN catalog.torrent_swarm_stats AS swarm
    ON swarm.torrent_id = torrent.id
LEFT JOIN catalog.torrent_completion_stats AS completion
    ON completion.torrent_id = torrent.id
LEFT JOIN LATERAL promotion.effective_for_torrent(torrent.id, CURRENT_TIMESTAMP) AS effective ON true
LEFT JOIN LATERAL (
    SELECT max(product_order.sticky_ends_at)::timestamptz AS sticky_ends_at
    FROM promotion.product_orders AS product_order
    WHERE product_order.torrent_id = torrent.id
      AND product_order.sticky_starts_at <= CURRENT_TIMESTAMP
      AND product_order.sticky_ends_at > CURRENT_TIMESTAMP
) AS sticky ON true
WHERE
    (
        sqlc.arg(search_text)::text = ''
        OR CASE sqlc.arg(search_scope)::text
            WHEN 'title' THEN position(lower(sqlc.arg(search_text)::text) IN lower(torrent.name)) > 0
            WHEN 'subtitle' THEN position(lower(sqlc.arg(search_text)::text) IN lower(torrent.subtitle)) > 0
            ELSE position(lower(sqlc.arg(search_text)::text) IN lower(torrent.name || ' ' || torrent.subtitle)) > 0
        END
    )
    AND (
        sqlc.arg(category_id)::text = ''
        OR torrent.category_id = sqlc.arg(category_id)::text
    )
    AND (
        sqlc.arg(promotion)::text = ''
        OR coalesce(effective.promotion, CASE
            WHEN torrent.promotion_ends_at IS NOT NULL
             AND torrent.promotion_ends_at <= CURRENT_TIMESTAMP THEN 'none'
            ELSE torrent.promotion
        END) = sqlc.arg(promotion)::text
    )
ORDER BY
	(sticky.sticky_ends_at IS NOT NULL) DESC,
	sticky.sticky_ends_at DESC,
    CASE WHEN sqlc.arg(sort_order)::text = 'published_desc' THEN torrent.published_at END DESC,
    CASE WHEN sqlc.arg(sort_order)::text = 'published_asc' THEN torrent.published_at END ASC,
    CASE WHEN sqlc.arg(sort_order)::text = 'size_desc' THEN torrent.size_bytes END DESC,
    CASE WHEN sqlc.arg(sort_order)::text = 'size_asc' THEN torrent.size_bytes END ASC,
    CASE WHEN sqlc.arg(sort_order)::text = 'completed_desc' THEN coalesce(completion.completed, swarm.completed, 0) END DESC,
    torrent.published_at DESC,
    torrent.id DESC
LIMIT sqlc.arg(result_limit)::integer
OFFSET sqlc.arg(result_offset)::integer;

-- name: CountPublishedTorrents :one
SELECT count(*)::bigint
FROM catalog.torrents AS torrent
JOIN torrents.torrents AS aggregate
  ON aggregate.id = torrent.id
 AND aggregate.state = 'published'
JOIN catalog.categories AS category
    ON category.id = torrent.category_id
   AND category.enabled = true
LEFT JOIN LATERAL promotion.effective_for_torrent(torrent.id, CURRENT_TIMESTAMP) AS effective ON true
WHERE
    (
        sqlc.arg(search_text)::text = ''
        OR CASE sqlc.arg(search_scope)::text
            WHEN 'title' THEN position(lower(sqlc.arg(search_text)::text) IN lower(torrent.name)) > 0
            WHEN 'subtitle' THEN position(lower(sqlc.arg(search_text)::text) IN lower(torrent.subtitle)) > 0
            ELSE position(lower(sqlc.arg(search_text)::text) IN lower(torrent.name || ' ' || torrent.subtitle)) > 0
        END
    )
    AND (
        sqlc.arg(category_id)::text = ''
        OR torrent.category_id = sqlc.arg(category_id)::text
    )
    AND (
        sqlc.arg(promotion)::text = ''
        OR coalesce(effective.promotion, CASE
            WHEN torrent.promotion_ends_at IS NOT NULL
             AND torrent.promotion_ends_at <= CURRENT_TIMESTAMP THEN 'none'
            ELSE torrent.promotion
        END) = sqlc.arg(promotion)::text
    );

-- name: GetPublishedTorrentSwarm :one
SELECT
    torrent.id,
    coalesce(swarm.seeders, 0)::integer AS seeders,
    coalesce(swarm.leechers, 0)::integer AS leechers,
    coalesce(completion.completed, swarm.completed, 0)::integer AS completed,
    swarm.observed_at,
    (swarm.torrent_id IS NOT NULL)::boolean AS stats_available
FROM catalog.torrents AS torrent
JOIN torrents.torrents AS aggregate
  ON aggregate.id = torrent.id
 AND aggregate.state = 'published'
LEFT JOIN catalog.torrent_swarm_stats AS swarm
    ON swarm.torrent_id = torrent.id
LEFT JOIN catalog.torrent_completion_stats AS completion
    ON completion.torrent_id = torrent.id
WHERE torrent.id = sqlc.arg(torrent_id)::bigint;
