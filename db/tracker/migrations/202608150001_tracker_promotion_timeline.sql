-- +goose Up

-- Promotion assignments are immutable rule facts kept separately from the
-- compiled policy timeline. This avoids materializing a torrent-specific
-- "normal" snapshot after a campaign ends, which would otherwise shadow every
-- later global policy revision for that torrent.
CREATE TABLE settlement.promotion_rules (
    id uuid PRIMARY KEY,
    scope_type text NOT NULL CHECK (scope_type IN ('global', 'torrent')),
    torrent_id bigint CHECK (torrent_id IS NULL OR torrent_id > 0),
    promotion text NOT NULL CHECK (promotion IN (
        'free',
        'double_upload',
        'double_upload_free',
        'half_download',
        'double_upload_half_download',
        'thirty_percent_download'
    )),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    override_lower_scopes boolean NOT NULL,
    reason_code text NOT NULL CHECK (reason_code ~ '^[a-z][a-z0-9_-]{0,63}$'),
    command_json text NOT NULL CHECK (
        octet_length(command_json) BETWEEN 2 AND 4096
        AND jsonb_typeof(command_json::jsonb) = 'object'
    ),
    command_sha256 bytea NOT NULL CHECK (octet_length(command_sha256) = 32),
    recorded_at timestamptz NOT NULL,
    CHECK (ends_at > starts_at),
    CHECK (
        (scope_type = 'global' AND torrent_id IS NULL AND override_lower_scopes)
        OR
        (scope_type = 'torrent' AND torrent_id IS NOT NULL AND NOT override_lower_scopes)
    )
);

CREATE INDEX promotion_rules_resolution_idx
    ON settlement.promotion_rules (torrent_id, starts_at, ends_at, id);

-- +goose StatementBegin
CREATE FUNCTION settlement.protect_promotion_rule()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'Settlement promotion rules are immutable';
    END IF;

    -- A delayed control-plane delivery may start in the past, but it must not
    -- reinterpret an interval that already has a final ledger result.
    IF EXISTS (
        SELECT 1
        FROM ledger.traffic_settlements AS settled
        WHERE settled.interval_ends_at > NEW.starts_at
          AND (NEW.scope_type = 'global' OR settled.torrent_id = NEW.torrent_id)
    ) THEN
        RAISE EXCEPTION 'promotion rule would rewrite settled traffic';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER settlement_promotion_rule_immutable
BEFORE INSERT OR UPDATE OR DELETE ON settlement.promotion_rules
FOR EACH ROW EXECUTE FUNCTION settlement.protect_promotion_rule();

-- +goose Down

DROP TRIGGER settlement_promotion_rule_immutable ON settlement.promotion_rules;
DROP FUNCTION settlement.protect_promotion_rule();
DROP INDEX settlement.promotion_rules_resolution_idx;
DROP TABLE settlement.promotion_rules;
