-- +goose Up

-- Tracker classifies the socket address once and carries both accounting
-- factors into immutable Settlement evidence. Existing evidence predates the
-- explicit download multiplier and therefore keeps neutral 1x semantics.
ALTER TABLE ledger.raw_session_intervals
    DROP CONSTRAINT raw_session_interval_network_evidence_complete,
    ADD COLUMN seedbox_download_factor_basis_points integer,
    ADD COLUMN seedbox_download_factor_explicit boolean;

UPDATE ledger.raw_session_intervals
SET seedbox_download_factor_basis_points = 10000,
    seedbox_download_factor_explicit = false
WHERE network_policy_sequence IS NOT NULL;

ALTER TABLE ledger.raw_session_intervals
    ADD CONSTRAINT raw_session_interval_network_evidence_complete CHECK (
        (
            network_policy_sequence IS NULL
            AND network_policy_revision IS NULL
            AND network_class IS NULL
            AND network_rule_id IS NULL
            AND seedbox_upload_factor_basis_points IS NULL
            AND seedbox_download_factor_basis_points IS NULL
            AND seedbox_download_factor_explicit IS NULL
            AND speed_limit_bytes_per_second IS NULL
        ) OR (
            network_policy_sequence >= 1
            AND network_policy_revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
            AND network_class IN ('standard', 'seedbox')
            AND seedbox_upload_factor_basis_points BETWEEN 0 AND 10000
            AND seedbox_download_factor_basis_points BETWEEN 0 AND 100000
            AND seedbox_download_factor_explicit IS NOT NULL
            AND speed_limit_bytes_per_second >= 0
            AND (
                (network_class = 'standard'
                    AND network_rule_id IS NULL
                    AND seedbox_upload_factor_basis_points = 10000
                    AND seedbox_download_factor_basis_points = 10000)
                OR
                (network_class = 'seedbox'
                    AND network_rule_id ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
                    AND seedbox_download_factor_basis_points >= 10000)
            )
        )
    );

-- +goose Down

ALTER TABLE ledger.raw_session_intervals
    DROP CONSTRAINT raw_session_interval_network_evidence_complete,
    DROP COLUMN seedbox_download_factor_explicit,
    DROP COLUMN seedbox_download_factor_basis_points,
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
