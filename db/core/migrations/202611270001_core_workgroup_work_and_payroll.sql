-- +goose Up

INSERT INTO authz.permissions (action, description, risk_level, relationship, credential_audience, grantable, discoverable)
VALUES ('workgroup.payroll.issue', '确认工作组月结工资并发放魔力值', 'high', 'none', 'staff-session', true, true);
INSERT INTO authz.role_permissions (role_id, action) VALUES ('site_admin', 'workgroup.payroll.issue');

CREATE TABLE workgroups.retention_targets (
    torrent_id bigint PRIMARY KEY REFERENCES torrents.torrents(id),
    required_hours integer NOT NULL CHECK (required_hours BETWEEN 1 AND 744),
    listed_at timestamptz NOT NULL,
    listed_by uuid NOT NULL REFERENCES identity.users(id),
    authorization_decision_id uuid NOT NULL,
    retired_at timestamptz,
    retired_by uuid REFERENCES identity.users(id),
    retirement_authorization_decision_id uuid,
    CHECK (retired_at IS NULL OR retired_at >= listed_at),
    CHECK ((retired_at IS NULL) = (retired_by IS NULL)),
    CHECK ((retired_at IS NULL) = (retirement_authorization_decision_id IS NULL))
);

CREATE TABLE workgroups.retention_claims (
    membership_id uuid NOT NULL REFERENCES workgroups.memberships(id),
    torrent_id bigint NOT NULL REFERENCES workgroups.retention_targets(torrent_id),
    claimed_at timestamptz NOT NULL,
    authorization_decision_id uuid NOT NULL,
    PRIMARY KEY (membership_id, torrent_id)
);
CREATE TRIGGER retention_claims_immutable BEFORE UPDATE OR DELETE ON workgroups.retention_claims
FOR EACH ROW EXECUTE FUNCTION economy.reject_seeding_reward_evidence_mutation();

-- Attribution uses the original submission time and the membership transition at
-- that instant. A later decision confirms the contribution in its original month.
-- +goose StatementBegin
CREATE FUNCTION workgroups.reseed_publications(
    member_id uuid, from_at timestamptz, until_at timestamptz, observed_at timestamptz
)
RETURNS TABLE(torrent_id bigint, title text, category_id text, submitted_at timestamptz,
    published_at timestamptz, resolution_source text, contribution_status text, reason_code text)
LANGUAGE sql STABLE SET search_path = pg_catalog AS $$
    SELECT torrent.id, torrent.title, torrent.category_id, torrent.submitted_at, torrent.published_at,
           COALESCE(decision.resolution_source, ''),
           CASE WHEN torrent.state = 'published' AND decision.decision = 'approve' THEN 'counted'
                WHEN torrent.state = 'pending_review' THEN 'pending'
                ELSE 'excluded' END,
           COALESCE(decision.reason_code, '')
    FROM workgroups.memberships AS member
    JOIN torrents.torrents AS torrent ON torrent.uploader_id = member.user_id
    JOIN LATERAL (
        SELECT transition.membership_id, transition.to_status
        FROM workgroups.membership_transitions AS transition
        WHERE transition.user_id = member.user_id AND transition.group_kind = 'reseed'
          AND transition.occurred_at <= torrent.submitted_at
        ORDER BY transition.occurred_at DESC, transition.state_version DESC, transition.id DESC
        LIMIT 1
    ) AS tenure ON tenure.membership_id = member.id AND tenure.to_status = 'active'
    LEFT JOIN LATERAL (
        SELECT proof.decision, proof.resolution_source, proof.reason_code
        FROM review.torrent_decisions AS proof
        WHERE proof.torrent_id = torrent.id AND proof.occurred_at <= observed_at
        ORDER BY proof.occurred_at DESC, proof.resulting_torrent_version DESC, proof.id DESC
        LIMIT 1
    ) AS decision ON true
    WHERE member.id = member_id AND member.group_kind = 'reseed'
      AND torrent.submitted_at >= from_at AND torrent.submitted_at < least(until_at, observed_at);
$$;
-- +goose StatementEnd

INSERT INTO economy.magic_accounts (id, user_id, account_kind, account_code, balance, version, updated_at)
VALUES ('00000000-0000-7000-8000-000000000016', NULL, 'system', 'system:mint:workgroup-payroll', 0, 1, clock_timestamp());

CREATE TABLE workgroups.payroll_payments (
    group_kind text NOT NULL REFERENCES workgroups.definitions(kind),
    user_id uuid NOT NULL REFERENCES identity.users(id),
    membership_id uuid NOT NULL REFERENCES workgroups.memberships(id),
    period_starts_at timestamptz NOT NULL,
    amount bigint NOT NULL CHECK (amount BETWEEN 1 AND 1000000),
    preview_digest bytea NOT NULL CHECK (octet_length(preview_digest) = 32),
    preview_snapshot jsonb NOT NULL CHECK (jsonb_typeof(preview_snapshot) = 'object'),
    issued_by uuid NOT NULL REFERENCES identity.users(id),
    authorization_decision_id uuid NOT NULL,
    paid_at timestamptz NOT NULL,
    magic_transaction_id uuid NOT NULL UNIQUE REFERENCES economy.magic_transactions(id),
    PRIMARY KEY (group_kind, user_id, period_starts_at),
    CHECK (period_starts_at = date_trunc('month', period_starts_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'),
    CHECK (paid_at >= ((period_starts_at AT TIME ZONE 'UTC') + interval '1 month') AT TIME ZONE 'UTC')
);
CREATE TRIGGER payroll_payments_immutable BEFORE UPDATE OR DELETE ON workgroups.payroll_payments
FOR EACH ROW EXECUTE FUNCTION economy.reject_seeding_reward_evidence_mutation();
REVOKE ALL ON workgroups.retention_targets, workgroups.retention_claims, workgroups.payroll_payments FROM PUBLIC;
REVOKE ALL ON FUNCTION workgroups.reseed_publications(uuid, timestamptz, timestamptz, timestamptz) FROM PUBLIC;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM workgroups.payroll_payments) THEN
        RAISE EXCEPTION 'workgroup payroll has been recorded in the immutable magic ledger';
    END IF;
END;
$$;
-- +goose StatementEnd
DROP TABLE workgroups.payroll_payments, workgroups.retention_claims, workgroups.retention_targets;
DROP FUNCTION workgroups.reseed_publications(uuid, timestamptz, timestamptz, timestamptz);
DELETE FROM economy.magic_accounts WHERE id = '00000000-0000-7000-8000-000000000016';
DELETE FROM authz.role_permissions WHERE action = 'workgroup.payroll.issue';
DELETE FROM authz.permissions WHERE action = 'workgroup.payroll.issue';
