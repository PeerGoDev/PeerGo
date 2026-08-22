-- +goose Up

-- Site presence is derived from the already-coalesced Web session activity.
-- This index keeps the rolling 15-minute distinct-user read bounded without
-- creating a second presence table or a request-by-request activity ledger.
CREATE INDEX sessions_online_web_activity_idx
    ON identity.sessions (last_seen_at DESC, user_id)
    INCLUDE (expires_at)
    WHERE audience = 'web' AND revoked_at IS NULL;

COMMENT ON COLUMN catalog.site_profile.online_users IS
    'Legacy fixture/import value; public online presence is derived from active Web sessions.';

-- +goose Down

COMMENT ON COLUMN catalog.site_profile.online_users IS NULL;
DROP INDEX identity.sessions_online_web_activity_idx;
