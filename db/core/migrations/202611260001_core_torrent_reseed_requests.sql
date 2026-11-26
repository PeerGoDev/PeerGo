-- +goose Up
INSERT INTO authz.permissions (
    action, description, risk_level, relationship, credential_audience,
    grantable, discoverable
) VALUES (
    'torrent.reseed.request.self', '请求为暂无做种者的种子补种',
    'low', 'self', 'web-session', true, true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'torrent.reseed.request.self');

CREATE TABLE torrents.reseed_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    torrent_id bigint NOT NULL REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    torrent_title text NOT NULL CHECK (char_length(btrim(torrent_title)) BETWEEN 1 AND 240),
    opened_at timestamptz NOT NULL,
    zero_observed_at timestamptz NOT NULL,
    restored_at timestamptz,
    CHECK (restored_at IS NULL OR restored_at >= opened_at)
);

CREATE UNIQUE INDEX reseed_requests_open_torrent_idx
    ON torrents.reseed_requests (torrent_id) WHERE restored_at IS NULL;

CREATE TABLE torrents.reseed_request_members (
    request_id uuid NOT NULL REFERENCES torrents.reseed_requests (id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    PRIMARY KEY (request_id, user_id)
);

CREATE TABLE community.torrent_reseed_notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id uuid NOT NULL REFERENCES torrents.reseed_requests (id) ON DELETE RESTRICT,
    recipient_user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    kind text NOT NULL CHECK (kind IN ('torrent_reseed_requested', 'torrent_reseed_restored')),
    created_at timestamptz NOT NULL,
    read_at timestamptz,
    archived_at timestamptz,
    UNIQUE (request_id, recipient_user_id, kind),
    CHECK (read_at IS NULL OR read_at >= created_at),
    CHECK (archived_at IS NULL OR archived_at >= created_at)
);

CREATE INDEX torrent_reseed_notifications_recipient_recent_idx
    ON community.torrent_reseed_notifications (recipient_user_id, created_at DESC, id DESC)
    WHERE archived_at IS NULL;

-- Version 1 packs encode torrent ID and active seeding seconds in slots 0 and 1.
-- Index only torrent IDs with positive evidence and leave the pack itself intact.
-- +goose StatementBegin
CREATE FUNCTION economy.seeding_reward_pack_seeded_torrents(packed_items jsonb)
RETURNS bigint[] LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
    SELECT ARRAY(
        SELECT (item->>0)::bigint
        FROM jsonb_array_elements(packed_items) AS entry(item)
        WHERE (item->>1)::integer > 0
    );
$$;
-- +goose StatementEnd

REVOKE ALL ON torrents.reseed_requests, torrents.reseed_request_members,
    community.torrent_reseed_notifications FROM PUBLIC;

-- +goose Down
DROP FUNCTION economy.seeding_reward_pack_seeded_torrents(jsonb);
DROP TABLE community.torrent_reseed_notifications;
DROP TABLE torrents.reseed_request_members;
DROP TABLE torrents.reseed_requests;
DELETE FROM authz.role_permissions WHERE action = 'torrent.reseed.request.self';
DELETE FROM authz.permissions WHERE action = 'torrent.reseed.request.self';
