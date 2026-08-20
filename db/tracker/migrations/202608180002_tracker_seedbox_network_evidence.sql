-- +goose Up

-- The socket address remains inside Tracker. Settlement receives only the
-- signed-policy resolution needed to reproduce accounting and speed evidence.
-- Existing pre-cutover intervals keep NULL evidence and are never guessed from
-- a current CIDR registry.
ALTER TABLE ledger.raw_session_intervals
    ADD COLUMN network_policy_sequence bigint,
    ADD COLUMN network_policy_revision text,
    ADD COLUMN network_class text,
    ADD COLUMN network_rule_id text,
    ADD COLUMN seedbox_upload_factor_basis_points integer,
    ADD COLUMN speed_limit_bytes_per_second bigint,
    ADD CONSTRAINT raw_session_interval_network_evidence_complete CHECK (
        (
            network_policy_sequence IS NULL
            AND network_policy_revision IS NULL
            AND network_class IS NULL
            AND network_rule_id IS NULL
            AND seedbox_upload_factor_basis_points IS NULL
            AND speed_limit_bytes_per_second IS NULL
        ) OR (
            network_policy_sequence >= 1
            AND network_policy_revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
            AND network_class IN ('standard', 'seedbox')
            AND seedbox_upload_factor_basis_points BETWEEN 0 AND 10000
            AND speed_limit_bytes_per_second >= 0
            AND (
                (network_class = 'standard'
                    AND network_rule_id IS NULL
                    AND seedbox_upload_factor_basis_points = 10000)
                OR
                (network_class = 'seedbox'
                    AND network_rule_id ~ '^[a-z0-9][a-z0-9._-]{0,63}$')
            )
        )
    );

-- +goose Down

ALTER TABLE ledger.raw_session_intervals
    DROP CONSTRAINT raw_session_interval_network_evidence_complete,
    DROP COLUMN speed_limit_bytes_per_second,
    DROP COLUMN seedbox_upload_factor_basis_points,
    DROP COLUMN network_rule_id,
    DROP COLUMN network_class,
    DROP COLUMN network_policy_revision,
    DROP COLUMN network_policy_sequence;
