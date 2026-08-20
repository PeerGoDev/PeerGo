-- +goose Up

-- Promotion administration has its own permission family. Reading delivery
-- state does not imply authority to create traffic-affecting campaigns.
INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('promotion.manage.read', '读取优惠政策时间线与投递状态', 'medium', 'none', 'staff-session', true, true),
    ('promotion.schedule', '签发全站或单种子优惠政策', 'high', 'none', 'staff-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_admin', 'promotion.manage.read'),
    ('site_admin', 'promotion.schedule');

CREATE SCHEMA promotion;

-- Human reasons and authorization evidence stay in Core. command_json is the
-- privacy-minimized, canonical accounting command sent to Settlement.
CREATE TABLE promotion.campaigns (
    id uuid PRIMARY KEY,
    scope_type text NOT NULL CHECK (scope_type IN ('global', 'torrent')),
    torrent_id bigint REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    promotion text NOT NULL CHECK (promotion IN (
        'free', 'double_upload', 'double_upload_free', 'half_download',
        'double_upload_half_download', 'thirty_percent_download'
    )),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    override_lower_scopes boolean NOT NULL,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    actor_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid NOT NULL,
    command_json text NOT NULL CHECK (
        octet_length(command_json) BETWEEN 2 AND 4096
        AND jsonb_typeof(command_json::jsonb) = 'object'
    ),
    command_sha256 bytea NOT NULL CHECK (octet_length(command_sha256) = 32),
    created_at timestamptz NOT NULL,
    CHECK (ends_at > starts_at),
    CHECK (
        (scope_type = 'global' AND torrent_id IS NULL AND override_lower_scopes)
        OR
        (scope_type = 'torrent' AND torrent_id IS NOT NULL AND NOT override_lower_scopes)
    )
);

CREATE INDEX promotion_campaigns_timeline_idx
    ON promotion.campaigns (starts_at DESC, id DESC);
CREATE INDEX promotion_campaigns_torrent_timeline_idx
    ON promotion.campaigns (torrent_id, starts_at, ends_at)
    WHERE torrent_id IS NOT NULL;

-- Delivery metadata is mutable but separate from the immutable campaign. The
-- command uses at-least-once HTTP delivery; Settlement verifies ID plus SHA-256
-- and turns an exact retry into a successful no-op.
CREATE TABLE promotion.delivery_outbox (
    campaign_id uuid PRIMARY KEY REFERENCES promotion.campaigns (id) ON DELETE RESTRICT,
    available_at timestamptz NOT NULL,
	lease_token uuid,
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error_code text CHECK (
        last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    delivered_at timestamptz,
    created_at timestamptz NOT NULL,
	CHECK ((lease_token IS NULL) = (lease_until IS NULL)),
	CHECK (delivered_at IS NULL OR (lease_token IS NULL AND last_error_code IS NULL))
);

CREATE INDEX promotion_delivery_ready_idx
    ON promotion.delivery_outbox (available_at, campaign_id)
    WHERE delivered_at IS NULL;

-- This SQL function is the single current-time display projection used by
-- public and staff catalog reads. Only Settlement-acknowledged campaigns are
-- visible; an active global campaign wins and the torrent rule automatically
-- reappears after the global interval ends.
CREATE FUNCTION promotion.effective_for_torrent(
    target_torrent_id bigint,
    effective_at timestamptz
)
RETURNS TABLE (
    campaign_id uuid,
    promotion text,
    ends_at timestamptz,
    scope_type text
)
LANGUAGE sql
STABLE
AS $$
    SELECT campaign.id, campaign.promotion, campaign.ends_at, campaign.scope_type
    FROM promotion.campaigns AS campaign
    JOIN promotion.delivery_outbox AS delivery
      ON delivery.campaign_id = campaign.id
     AND delivery.delivered_at IS NOT NULL
    WHERE campaign.starts_at <= effective_at
      AND campaign.ends_at > effective_at
      AND (campaign.scope_type = 'global' OR campaign.torrent_id = target_torrent_id)
    ORDER BY
        CASE campaign.scope_type WHEN 'global' THEN 0 ELSE 1 END,
        campaign.starts_at DESC,
        campaign.id DESC
    LIMIT 1
$$;

-- +goose StatementBegin
CREATE FUNCTION promotion.reject_campaign_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'promotion campaigns are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER promotion_campaigns_immutable
BEFORE UPDATE OR DELETE ON promotion.campaigns
FOR EACH ROW EXECUTE FUNCTION promotion.reject_campaign_mutation();

-- +goose Down

DROP TRIGGER promotion_campaigns_immutable ON promotion.campaigns;
DROP FUNCTION promotion.reject_campaign_mutation();
DROP FUNCTION promotion.effective_for_torrent(bigint, timestamptz);
DROP INDEX promotion.promotion_delivery_ready_idx;
DROP TABLE promotion.delivery_outbox;
DROP INDEX promotion.promotion_campaigns_torrent_timeline_idx;
DROP INDEX promotion.promotion_campaigns_timeline_idx;
DROP TABLE promotion.campaigns;
DROP SCHEMA promotion;

DELETE FROM authz.role_permissions
WHERE role_id = 'site_admin'
  AND action IN ('promotion.manage.read', 'promotion.schedule');

DELETE FROM authz.permissions
WHERE action IN ('promotion.manage.read', 'promotion.schedule');
