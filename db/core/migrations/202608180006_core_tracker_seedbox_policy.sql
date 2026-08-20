-- +goose Up

-- Seedbox classification is part of Tracker's signed runtime policy. Keeping
-- the reviewed prefixes and accounting settings in one immutable revision
-- prevents an address from being classified with one version while its upload
-- factor is later reconstructed from a different "current setting".
ALTER TABLE tracker_control.runtime_policy_revisions
    ADD COLUMN seedbox_policy jsonb NOT NULL DEFAULT
        '{"enabled":false,"upload_factor_basis_points":5000,"seedbox_speed_limit_bytes_per_second":0,"standard_speed_limit_bytes_per_second":0,"rules":[]}'::jsonb
    CHECK (
        jsonb_typeof(seedbox_policy) = 'object'
        AND jsonb_typeof(seedbox_policy -> 'enabled') = 'boolean'
        AND jsonb_typeof(seedbox_policy -> 'upload_factor_basis_points') = 'number'
        AND (seedbox_policy ->> 'upload_factor_basis_points')::integer BETWEEN 0 AND 10000
        AND jsonb_typeof(seedbox_policy -> 'seedbox_speed_limit_bytes_per_second') = 'number'
        AND (seedbox_policy ->> 'seedbox_speed_limit_bytes_per_second')::bigint >= 0
        AND jsonb_typeof(seedbox_policy -> 'standard_speed_limit_bytes_per_second') = 'number'
        AND (seedbox_policy ->> 'standard_speed_limit_bytes_per_second')::bigint >= 0
        AND jsonb_typeof(seedbox_policy -> 'rules') = 'array'
        AND jsonb_array_length(seedbox_policy -> 'rules') <= 4096
    );

-- +goose Down

ALTER TABLE tracker_control.runtime_policy_revisions
    DROP COLUMN seedbox_policy;
