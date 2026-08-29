-- +goose Up

-- PT-depiler can retain an old PtYes public UUID in a saved search result and
-- later hand that URL directly to qBittorrent. PeerGo keeps the numeric
-- torrent ID canonical: this table stores only a one-way digest of the old
-- route value and resolves it to the existing numeric aggregate.
CREATE TABLE migration.legacy_torrent_route_aliases (
    alias_sha256 bytea PRIMARY KEY
        CHECK (octet_length(alias_sha256) = 32),
    torrent_id bigint NOT NULL UNIQUE
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION migration.protect_legacy_torrent_route_alias()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'legacy torrent route aliases are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER migration_legacy_torrent_route_alias_immutable
BEFORE UPDATE OR DELETE ON migration.legacy_torrent_route_aliases
FOR EACH ROW EXECUTE FUNCTION migration.protect_legacy_torrent_route_alias();

REVOKE ALL ON migration.legacy_torrent_route_aliases FROM PUBLIC;

-- +goose Down

DROP TRIGGER migration_legacy_torrent_route_alias_immutable
    ON migration.legacy_torrent_route_aliases;
DROP FUNCTION migration.protect_legacy_torrent_route_alias();
DROP TABLE migration.legacy_torrent_route_aliases;
