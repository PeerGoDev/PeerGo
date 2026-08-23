#!/usr/bin/env bash

set -Eeuo pipefail

fail() {
    printf 'Legacy reviewer import: %s\n' "$*" >&2
    exit 1
}

required_env() {
    local name="$1"
    [[ -n "${!name:-}" ]] || fail "${name} is required"
}

required_env PEERGO_LEGACY_SOURCE_DATABASE_URL
required_env PEERGO_CORE_DATABASE_URL
required_env PEERGO_LEGACY_RUN_ID
required_env PEERGO_LEGACY_OCCURRED_AT
command -v psql >/dev/null 2>&1 || fail "psql is required"
case "${PEERGO_LEGACY_REVIEWER_IMPORT_DRY_RUN:-false}" in
    true|false) ;;
    *) fail "PEERGO_LEGACY_REVIEWER_IMPORT_DRY_RUN must be true or false" ;;
esac

reviewer_csv="$(mktemp /tmp/peergo-legacy-reviewers.XXXXXX.csv)"
cleanup() {
    rm -f -- "${reviewer_csv}"
}
trap cleanup EXIT INT TERM

# The temporary CSV contains operational metadata only. PostgreSQL performs
# both reads directly; no application credential, email or password is copied.
psql "${PEERGO_LEGACY_SOURCE_DATABASE_URL}" -X -v ON_ERROR_STOP=1 -q -c "
COPY (
    SELECT
        reviewer.id,
        reviewer.user_id,
        reviewer.status,
        reviewer.total_reviews,
        reviewer.accurate_count,
        reviewer.joined_at,
        reviewer.removed_at,
        reviewer.remove_reason,
        reviewer.last_activity_at,
        reviewer.activity_status,
        reviewer.created_at,
        reviewer.updated_at,
        encode(sha256(convert_to(jsonb_build_array(
            reviewer.id,
            reviewer.user_id,
            reviewer.status,
            reviewer.total_reviews,
            reviewer.accurate_count,
            reviewer.joined_at,
            reviewer.removed_at,
            reviewer.remove_reason,
            reviewer.last_activity_at,
            reviewer.activity_status,
            reviewer.created_at,
            reviewer.updated_at
        )::text, 'UTF8')), 'hex') AS source_fingerprint_hex
    FROM reviewers AS reviewer
    ORDER BY reviewer.id
) TO STDOUT WITH (FORMAT csv, HEADER true)" >"${reviewer_csv}"

{
cat <<'SQL'
BEGIN;

CREATE TEMP TABLE legacy_reviewer_import_config (
    run_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL
) ON COMMIT DROP;

INSERT INTO legacy_reviewer_import_config (run_id, occurred_at)
VALUES (:'run_id'::uuid, :'occurred_at'::timestamptz);

CREATE TEMP TABLE legacy_reviewer_stage (
    legacy_reviewer_id bigint PRIMARY KEY CHECK (legacy_reviewer_id > 0),
    legacy_user_id bigint NOT NULL UNIQUE CHECK (legacy_user_id > 0),
    source_status text NOT NULL CHECK (source_status IN ('active', 'suspended', 'removed')),
    source_total_reviews bigint NOT NULL CHECK (source_total_reviews >= 0),
    source_accurate_count bigint NOT NULL CHECK (
        source_accurate_count >= 0
        AND source_accurate_count <= source_total_reviews
    ),
    source_joined_at timestamptz NOT NULL,
    source_removed_at timestamptz,
    source_remove_reason text,
    source_last_activity_at timestamptz,
    source_activity_status text NOT NULL
        CHECK (source_activity_status IN ('active', 'inactive', 'dormant')),
    source_created_at timestamptz NOT NULL,
    source_updated_at timestamptz NOT NULL,
    source_fingerprint_hex text NOT NULL CHECK (
        source_fingerprint_hex ~ '^[0-9a-f]{64}$'
    ),
    user_id uuid,
    membership_id uuid NOT NULL DEFAULT gen_random_uuid(),
    joined_transition_id uuid NOT NULL DEFAULT gen_random_uuid(),
    status_transition_id uuid
) ON COMMIT DROP;

SQL
printf "\\copy legacy_reviewer_stage (legacy_reviewer_id, legacy_user_id, source_status, source_total_reviews, source_accurate_count, source_joined_at, source_removed_at, source_remove_reason, source_last_activity_at, source_activity_status, source_created_at, source_updated_at, source_fingerprint_hex) FROM '%s' WITH (FORMAT csv, HEADER true)\n" "${reviewer_csv}"
cat <<'SQL'
UPDATE legacy_reviewer_stage AS stage
SET user_id = mapping.user_id
FROM migration.user_id_map AS mapping
WHERE mapping.source_system = 'ptyes'
  AND mapping.legacy_user_id = stage.legacy_user_id;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM legacy_reviewer_stage) THEN
        RAISE EXCEPTION 'the PtYes reviewers table is empty';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM legacy_reviewer_stage
        WHERE user_id IS NULL
           OR source_updated_at < source_created_at
    ) THEN
        RAISE EXCEPTION 'legacy reviewer rows are unmapped or contain invalid timestamps';
    END IF;
    IF NOT EXISTS (
        SELECT 1
        FROM migration.runs AS run
        JOIN legacy_reviewer_import_config AS config ON config.run_id = run.id
    ) THEN
        RAISE EXCEPTION 'the accepted PtYes migration run does not exist';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM legacy_reviewer_stage AS stage
        JOIN migration.legacy_reviewer_openings AS opening
          ON opening.source_system = 'ptyes'
         AND opening.legacy_reviewer_id = stage.legacy_reviewer_id
        WHERE opening.source_fingerprint IS DISTINCT FROM decode(stage.source_fingerprint_hex, 'hex')
           OR opening.legacy_user_id IS DISTINCT FROM stage.legacy_user_id
           OR opening.user_id IS DISTINCT FROM stage.user_id
    ) THEN
        RAISE EXCEPTION 'legacy reviewer retry conflicts with immutable imported evidence';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM legacy_reviewer_stage AS stage
        JOIN workgroups.memberships AS membership
          ON membership.group_kind = 'review'
         AND membership.user_id = stage.user_id
        LEFT JOIN migration.legacy_reviewer_openings AS opening
          ON opening.source_system = 'ptyes'
         AND opening.legacy_reviewer_id = stage.legacy_reviewer_id
        WHERE opening.membership_id IS NULL
           OR opening.membership_id IS DISTINCT FROM membership.id
    ) THEN
        RAISE EXCEPTION 'a reviewer conflicts with an existing unmanaged review membership';
    END IF;
END;
$$;

UPDATE legacy_reviewer_stage AS stage
SET membership_id = opening.membership_id,
    joined_transition_id = opening.joined_transition_id,
    status_transition_id = opening.status_transition_id
FROM migration.legacy_reviewer_openings AS opening
WHERE opening.source_system = 'ptyes'
  AND opening.legacy_reviewer_id = stage.legacy_reviewer_id;

UPDATE legacy_reviewer_stage
SET status_transition_id = gen_random_uuid()
WHERE source_status IN ('suspended', 'removed')
  AND status_transition_id IS NULL;

INSERT INTO workgroups.memberships (
    id,
    group_kind,
    user_id,
    status,
    source,
    source_application_id,
    version,
    started_at,
    ended_at,
    updated_at
)
SELECT
    stage.membership_id,
    'review',
    stage.user_id,
    CASE stage.source_status
        WHEN 'active' THEN 'active'
        WHEN 'suspended' THEN 'suspended'
        ELSE 'ended'
    END,
    'legacy_migration',
    NULL,
    CASE stage.source_status WHEN 'active' THEN 1 ELSE 2 END,
    stage.source_joined_at,
    CASE WHEN stage.source_status = 'removed' THEN
        GREATEST(stage.source_joined_at, COALESCE(stage.source_removed_at, stage.source_updated_at))
    END,
    GREATEST(stage.source_joined_at, stage.source_updated_at)
FROM legacy_reviewer_stage AS stage
LEFT JOIN migration.legacy_reviewer_openings AS opening
  ON opening.source_system = 'ptyes'
 AND opening.legacy_reviewer_id = stage.legacy_reviewer_id
WHERE opening.legacy_reviewer_id IS NULL
ORDER BY stage.legacy_reviewer_id;

INSERT INTO workgroups.membership_transitions (
    id,
    membership_id,
    group_kind,
    user_id,
    transition,
    from_status,
    to_status,
    actor_id,
    source,
    source_application_id,
    reason,
    authorization_decision_id,
    state_version,
    occurred_at
)
SELECT
    stage.joined_transition_id,
    stage.membership_id,
    'review',
    stage.user_id,
    'joined',
    NULL,
    'active',
    NULL,
    'legacy_migration',
    NULL,
    'Rousi 原种审成员迁移开账。',
    NULL,
    1,
    stage.source_joined_at
FROM legacy_reviewer_stage AS stage
LEFT JOIN migration.legacy_reviewer_openings AS opening
  ON opening.source_system = 'ptyes'
 AND opening.legacy_reviewer_id = stage.legacy_reviewer_id
WHERE opening.legacy_reviewer_id IS NULL
ORDER BY stage.legacy_reviewer_id;

INSERT INTO workgroups.membership_transitions (
    id,
    membership_id,
    group_kind,
    user_id,
    transition,
    from_status,
    to_status,
    actor_id,
    source,
    source_application_id,
    reason,
    authorization_decision_id,
    state_version,
    occurred_at
)
SELECT
    stage.status_transition_id,
    stage.membership_id,
    'review',
    stage.user_id,
    CASE stage.source_status WHEN 'suspended' THEN 'suspended' ELSE 'ended' END,
    'active',
    CASE stage.source_status WHEN 'suspended' THEN 'suspended' ELSE 'ended' END,
    NULL,
    'legacy_migration',
    NULL,
    COALESCE(
        NULLIF(btrim(stage.source_remove_reason), ''),
        CASE stage.source_status
            WHEN 'suspended' THEN 'Rousi 原种审成员状态迁移：保持暂停。'
            ELSE 'Rousi 原种审成员状态迁移：保持移除。'
        END
    ),
    NULL,
    2,
    GREATEST(stage.source_joined_at, COALESCE(stage.source_removed_at, stage.source_updated_at))
FROM legacy_reviewer_stage AS stage
LEFT JOIN migration.legacy_reviewer_openings AS opening
  ON opening.source_system = 'ptyes'
 AND opening.legacy_reviewer_id = stage.legacy_reviewer_id
WHERE opening.legacy_reviewer_id IS NULL
  AND stage.source_status IN ('suspended', 'removed')
ORDER BY stage.legacy_reviewer_id;

INSERT INTO migration.workgroup_membership_openings (
    source_system,
    legacy_user_id,
    user_id,
    group_kind,
    membership_id,
    transition_id,
    legacy_user_medal_ids,
    legacy_medal_ids,
    started_at,
    source_fingerprint,
    first_run_id,
    imported_at,
    source_kind
)
SELECT
    'ptyes',
    stage.legacy_user_id,
    stage.user_id,
    'review',
    stage.membership_id,
    stage.joined_transition_id,
    ARRAY[]::bigint[],
    ARRAY[]::bigint[],
    stage.source_joined_at,
    decode(stage.source_fingerprint_hex, 'hex'),
    config.run_id,
    config.occurred_at,
    'reviewer'
FROM legacy_reviewer_stage AS stage
CROSS JOIN legacy_reviewer_import_config AS config
ORDER BY stage.legacy_reviewer_id
ON CONFLICT (source_system, legacy_user_id, group_kind) DO NOTHING;

INSERT INTO migration.legacy_reviewer_openings (
    source_system,
    legacy_reviewer_id,
    legacy_user_id,
    user_id,
    membership_id,
    joined_transition_id,
    status_transition_id,
    source_status,
    source_activity_status,
    source_total_reviews,
    source_accurate_count,
    source_joined_at,
    source_removed_at,
    source_remove_reason,
    source_last_activity_at,
    source_created_at,
    source_updated_at,
    source_fingerprint,
    first_run_id,
    imported_at
)
SELECT
    'ptyes',
    stage.legacy_reviewer_id,
    stage.legacy_user_id,
    stage.user_id,
    stage.membership_id,
    stage.joined_transition_id,
    stage.status_transition_id,
    stage.source_status,
    stage.source_activity_status,
    stage.source_total_reviews,
    stage.source_accurate_count,
    stage.source_joined_at,
    stage.source_removed_at,
    stage.source_remove_reason,
    stage.source_last_activity_at,
    stage.source_created_at,
    stage.source_updated_at,
    decode(stage.source_fingerprint_hex, 'hex'),
    config.run_id,
    config.occurred_at
FROM legacy_reviewer_stage AS stage
CROSS JOIN legacy_reviewer_import_config AS config
ORDER BY stage.legacy_reviewer_id
ON CONFLICT (source_system, legacy_reviewer_id) DO NOTHING;

DO $$
DECLARE
    source_count bigint;
    receipt_count bigint;
    conflict_count bigint;
BEGIN
    SELECT count(*) INTO source_count FROM legacy_reviewer_stage;
    SELECT count(opening.legacy_reviewer_id), count(*) FILTER (WHERE
        opening.legacy_reviewer_id IS NULL
        OR opening.legacy_user_id IS DISTINCT FROM stage.legacy_user_id
        OR opening.user_id IS DISTINCT FROM stage.user_id
        OR opening.source_status IS DISTINCT FROM stage.source_status
        OR opening.source_activity_status IS DISTINCT FROM stage.source_activity_status
        OR opening.source_total_reviews IS DISTINCT FROM stage.source_total_reviews
        OR opening.source_accurate_count IS DISTINCT FROM stage.source_accurate_count
        OR opening.source_joined_at IS DISTINCT FROM stage.source_joined_at
        OR opening.source_removed_at IS DISTINCT FROM stage.source_removed_at
        OR opening.source_remove_reason IS DISTINCT FROM stage.source_remove_reason
        OR opening.source_last_activity_at IS DISTINCT FROM stage.source_last_activity_at
        OR opening.source_created_at IS DISTINCT FROM stage.source_created_at
        OR opening.source_updated_at IS DISTINCT FROM stage.source_updated_at
        OR opening.source_fingerprint IS DISTINCT FROM decode(stage.source_fingerprint_hex, 'hex')
        OR membership.id IS DISTINCT FROM opening.membership_id
        OR membership.user_id IS DISTINCT FROM stage.user_id
        OR membership.group_kind IS DISTINCT FROM 'review'
        OR membership.source IS DISTINCT FROM 'legacy_migration'
        OR membership.status IS DISTINCT FROM CASE stage.source_status
            WHEN 'active' THEN 'active'
            WHEN 'suspended' THEN 'suspended'
            ELSE 'ended'
        END
        OR membership.version IS DISTINCT FROM CASE stage.source_status WHEN 'active' THEN 1 ELSE 2 END
        OR joined.id IS DISTINCT FROM opening.joined_transition_id
        OR joined.transition IS DISTINCT FROM 'joined'
        OR joined.to_status IS DISTINCT FROM 'active'
        OR joined.state_version IS DISTINCT FROM 1
        OR (stage.source_status = 'active' AND status_transition.id IS NOT NULL)
        OR (stage.source_status IN ('suspended', 'removed') AND status_transition.id IS NULL)
        OR workgroup_opening.source_kind IS DISTINCT FROM 'reviewer'
        OR cardinality(workgroup_opening.legacy_user_medal_ids) IS DISTINCT FROM 0
        OR cardinality(workgroup_opening.legacy_medal_ids) IS DISTINCT FROM 0
    )
    INTO receipt_count, conflict_count
    FROM legacy_reviewer_stage AS stage
    LEFT JOIN migration.legacy_reviewer_openings AS opening
      ON opening.source_system = 'ptyes'
     AND opening.legacy_reviewer_id = stage.legacy_reviewer_id
    LEFT JOIN workgroups.memberships AS membership
      ON membership.id = opening.membership_id
    LEFT JOIN workgroups.membership_transitions AS joined
      ON joined.id = opening.joined_transition_id
    LEFT JOIN workgroups.membership_transitions AS status_transition
      ON status_transition.id = opening.status_transition_id
    LEFT JOIN migration.workgroup_membership_openings AS workgroup_opening
      ON workgroup_opening.membership_id = opening.membership_id;

    IF receipt_count <> source_count OR conflict_count <> 0 THEN
        RAISE EXCEPTION 'legacy reviewer reconciliation failed: receipts=%/% conflicts=%',
            receipt_count, source_count, conflict_count;
    END IF;
END;
$$;

\if :dry_run
ROLLBACK;
\echo 'Legacy reviewer import dry run passed; all writes were rolled back.'
\else
COMMIT;
\endif

SELECT
    count(*) AS total_reviewers,
    count(*) FILTER (WHERE membership.status = 'active') AS active_reviewers,
    count(*) FILTER (WHERE membership.status = 'suspended') AS suspended_reviewers,
    count(*) FILTER (WHERE membership.status = 'ended') AS ended_reviewers
FROM migration.legacy_reviewer_openings AS opening
JOIN workgroups.memberships AS membership ON membership.id = opening.membership_id
WHERE opening.source_system = 'ptyes';
SQL
} | psql "${PEERGO_CORE_DATABASE_URL}" -X -v ON_ERROR_STOP=1 \
    -v run_id="${PEERGO_LEGACY_RUN_ID}" \
    -v occurred_at="${PEERGO_LEGACY_OCCURRED_AT}" \
    -v dry_run="${PEERGO_LEGACY_REVIEWER_IMPORT_DRY_RUN:-false}"
