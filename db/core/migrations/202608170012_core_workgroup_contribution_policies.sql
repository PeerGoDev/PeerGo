-- +goose Up

-- Workgroup activity goals are typed policy, not arbitrary task JSON. Each
-- group has one metric backed by immutable domain evidence; opening policies
-- are observation-only so a fresh deployment cannot silently suspend members.
CREATE TABLE workgroups.contribution_policy_revisions (
    group_kind text NOT NULL
        REFERENCES workgroups.definitions (kind) ON DELETE RESTRICT,
    revision bigint NOT NULL CHECK (revision > 0),
    metric text NOT NULL CHECK (metric IN (
        'trusted_torrents_published',
        'torrent_review_votes',
        'seeding_active_seconds'
    )),
    period_kind text NOT NULL CHECK (period_kind = 'calendar_month'),
    target_value bigint NOT NULL CHECK (target_value > 0),
    enforcement_mode text NOT NULL CHECK (enforcement_mode IN ('observe')),
    effective_from timestamptz NOT NULL,
    source_kind text NOT NULL CHECK (source_kind IN ('cutover_opening', 'staff')),
    source_reference text NOT NULL CHECK (
        source_reference ~ '^[a-z0-9][a-z0-9:._-]{0,127}$'
    ),
    authorization_decision_id uuid,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (group_kind, revision),
    UNIQUE (group_kind, effective_from),
    CHECK (
        (group_kind = 'reseed' AND metric = 'trusted_torrents_published')
        OR (group_kind = 'review' AND metric = 'torrent_review_votes')
        OR (group_kind = 'retention' AND metric = 'seeding_active_seconds')
    ),
    CHECK (
        (source_kind = 'cutover_opening'
            AND revision = 1
            AND effective_from = '-infinity'::timestamptz
            AND authorization_decision_id IS NULL)
        OR (source_kind = 'staff'
            AND effective_from = date_trunc('month', effective_from)
            AND authorization_decision_id IS NOT NULL)
    )
);

INSERT INTO workgroups.contribution_policy_revisions (
    group_kind, revision, metric, period_kind, target_value,
    enforcement_mode, effective_from, source_kind, source_reference, created_at
) VALUES
    ('reseed', 1, 'trusted_torrents_published', 'calendar_month', 2,
     'observe', '-infinity', 'cutover_opening', 'peergo-opening:reseed-v1', now()),
    ('review', 1, 'torrent_review_votes', 'calendar_month', 20,
     'observe', '-infinity', 'cutover_opening', 'peergo-opening:review-v1', now()),
    ('retention', 1, 'seeding_active_seconds', 'calendar_month', 604800,
     'observe', '-infinity', 'cutover_opening', 'peergo-opening:retention-v1', now());

-- +goose StatementBegin
CREATE FUNCTION workgroups.require_contribution_policy_append()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    latest_revision bigint;
    latest_effective_from timestamptz;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(
        'peergo-workgroup-contribution-policy:' || NEW.group_kind, 0
    ));
    SELECT revision, effective_from
    INTO latest_revision, latest_effective_from
    FROM workgroups.contribution_policy_revisions
    WHERE group_kind = NEW.group_kind
    ORDER BY revision DESC
    LIMIT 1;

    IF latest_revision IS NOT NULL AND (
        NEW.revision <> latest_revision + 1
        OR NEW.effective_from <= latest_effective_from
    ) THEN
        RAISE EXCEPTION 'workgroup contribution policy must append in order';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER workgroup_contribution_policy_append_only
BEFORE INSERT ON workgroups.contribution_policy_revisions
FOR EACH ROW EXECUTE FUNCTION workgroups.require_contribution_policy_append();

CREATE TRIGGER workgroup_contribution_policy_immutable
BEFORE UPDATE OR DELETE ON workgroups.contribution_policy_revisions
FOR EACH ROW EXECUTE FUNCTION workgroups.reject_history_mutation();

REVOKE ALL ON workgroups.contribution_policy_revisions FROM PUBLIC;

-- +goose Down

DROP TRIGGER workgroup_contribution_policy_immutable
    ON workgroups.contribution_policy_revisions;
DROP TRIGGER workgroup_contribution_policy_append_only
    ON workgroups.contribution_policy_revisions;
DROP FUNCTION workgroups.require_contribution_policy_append();
DROP TABLE workgroups.contribution_policy_revisions;
