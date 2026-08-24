-- +goose Up

-- PtYes invitation slots are a per-member consumable balance.  Keep the
-- mutable projection small and record every native debit/refund separately so
-- the current value can always be reconciled to the immutable legacy opening.
CREATE TABLE identity.invitation_accounts (
    user_id uuid PRIMARY KEY
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    remaining_invites integer NOT NULL
        CHECK (remaining_invites BETWEEN 0 AND 1000000),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL
);

CREATE TABLE identity.invitation_balance_events (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL
        REFERENCES identity.invitation_accounts (user_id) ON DELETE RESTRICT,
    invitation_id uuid NOT NULL
        REFERENCES identity.registration_invitations (id) ON DELETE RESTRICT,
    event_kind text NOT NULL CHECK (event_kind IN ('issued', 'revoked')),
    delta smallint NOT NULL CHECK (
        (event_kind = 'issued' AND delta = -1)
        OR (event_kind = 'revoked' AND delta = 1)
    ),
    balance_after integer NOT NULL
        CHECK (balance_after BETWEEN 0 AND 1000000),
    authorization_decision_id uuid NOT NULL,
    source_reference text NOT NULL UNIQUE
        CHECK (source_reference ~ '^member-invitation:[0-9a-f-]{36}:(issued|revoked)$'),
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (invitation_id, event_kind),
    CHECK (recorded_at >= occurred_at)
);

CREATE INDEX invitation_balance_events_user_time_idx
    ON identity.invitation_balance_events (user_id, occurred_at DESC, id DESC);

-- A still-valid PtYes token is imported by digest only.  Claimed and expired
-- legacy rows remain history evidence and never retain their bearer digest.
ALTER TABLE identity.registration_invitations
    DROP CONSTRAINT registration_invitations_source_kind_check,
    DROP CONSTRAINT registration_invitations_source_valid,
    ADD CONSTRAINT registration_invitations_source_kind_check
        CHECK (source_kind IN ('development', 'member', 'operator', 'legacy')),
    ADD CONSTRAINT registration_invitations_source_valid CHECK (
        (source_kind = 'member'
            AND issuer_user_id IS NOT NULL
            AND issued_authorization_decision_id IS NOT NULL)
        OR
        (source_kind = 'legacy'
            AND issuer_user_id IS NOT NULL
            AND issued_authorization_decision_id IS NULL)
        OR
        (source_kind IN ('development', 'operator'))
    );

CREATE TABLE migration.legacy_invitation_balance_openings (
    source_system text NOT NULL DEFAULT 'ptyes' CHECK (source_system = 'ptyes'),
    legacy_user_id bigint PRIMARY KEY CHECK (legacy_user_id > 0),
    user_id uuid NOT NULL UNIQUE
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    remaining_invites integer NOT NULL
        CHECK (remaining_invites BETWEEN 0 AND 1000000),
    source_updated_at timestamptz NOT NULL,
    source_fingerprint bytea NOT NULL CHECK (octet_length(source_fingerprint) = 32),
    first_run_id uuid NOT NULL
        REFERENCES migration.runs (id) ON DELETE RESTRICT,
    imported_at timestamptz NOT NULL,
    FOREIGN KEY (source_system, legacy_user_id, user_id)
        REFERENCES migration.user_id_map (source_system, legacy_user_id, user_id)
        ON DELETE RESTRICT,
    CHECK (imported_at >= source_updated_at)
);

CREATE INDEX legacy_invitation_balance_openings_run_idx
    ON migration.legacy_invitation_balance_openings (first_run_id, legacy_user_id);

CREATE TABLE migration.legacy_invitation_code_openings (
    legacy_invitation_id bigint PRIMARY KEY CHECK (legacy_invitation_id > 0),
    invitation_id uuid NOT NULL UNIQUE,
    first_run_id uuid NOT NULL
        REFERENCES migration.runs (id) ON DELETE RESTRICT,
    legacy_inviter_id bigint NOT NULL CHECK (legacy_inviter_id >= 0),
    inviter_user_id uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    legacy_invitee_id bigint CHECK (legacy_invitee_id IS NULL OR legacy_invitee_id > 0),
    invitee_user_id uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    source_claimed boolean NOT NULL,
    source_role text NOT NULL CHECK (char_length(source_role) BETWEEN 1 AND 32),
    source_token_sha256 bytea,
    registration_invitation_id uuid UNIQUE
        REFERENCES identity.registration_invitations (id) ON DELETE RESTRICT,
    source_fingerprint bytea NOT NULL CHECK (octet_length(source_fingerprint) = 32),
    source_created_at timestamptz NOT NULL,
    source_valid_until timestamptz NOT NULL,
    source_claimed_at timestamptz,
    observed_at timestamptz NOT NULL,
    imported_at timestamptz NOT NULL,
    CHECK (source_valid_until > source_created_at),
    CHECK (
        (source_claimed AND legacy_invitee_id IS NOT NULL AND source_claimed_at IS NOT NULL)
        OR
        (NOT source_claimed AND legacy_invitee_id IS NULL AND source_claimed_at IS NULL)
    ),
    CHECK (source_claimed_at IS NULL OR source_claimed_at >= source_created_at),
    CHECK ((inviter_user_id IS NULL) = (legacy_inviter_id = 0)),
    CHECK ((invitee_user_id IS NULL) OR source_claimed),
    CHECK (
        (registration_invitation_id IS NULL AND source_token_sha256 IS NULL)
        OR
        (registration_invitation_id = invitation_id
            AND NOT source_claimed
            AND source_valid_until > observed_at
            AND octet_length(source_token_sha256) = 32)
    ),
    CHECK (imported_at >= observed_at)
);

CREATE INDEX legacy_invitation_code_openings_inviter_history_idx
    ON migration.legacy_invitation_code_openings
        (inviter_user_id, source_created_at DESC, invitation_id DESC)
    WHERE inviter_user_id IS NOT NULL;

CREATE TABLE migration.legacy_invitation_inventory_imports (
    run_id uuid PRIMARY KEY REFERENCES migration.runs (id) ON DELETE RESTRICT,
    source_evidence_sha256 bytea NOT NULL CHECK (octet_length(source_evidence_sha256) = 32),
    observed_at timestamptz NOT NULL,
    balance_source_rows bigint NOT NULL CHECK (balance_source_rows >= 0),
    balance_total bigint NOT NULL CHECK (balance_total >= 0),
    positive_balance_users bigint NOT NULL CHECK (
        positive_balance_users BETWEEN 0 AND balance_source_rows
    ),
    invitation_source_rows bigint NOT NULL CHECK (invitation_source_rows >= 0),
    claimed_invitation_rows bigint NOT NULL CHECK (
        claimed_invitation_rows BETWEEN 0 AND invitation_source_rows
    ),
    active_invitation_rows bigint NOT NULL CHECK (
        active_invitation_rows BETWEEN 0 AND invitation_source_rows
    ),
    imported_at timestamptz NOT NULL,
    CHECK (imported_at >= observed_at)
);

CREATE TRIGGER invitation_balance_events_immutable
BEFORE UPDATE OR DELETE ON identity.invitation_balance_events
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER legacy_invitation_balance_openings_immutable
BEFORE UPDATE OR DELETE ON migration.legacy_invitation_balance_openings
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER legacy_invitation_code_openings_immutable
BEFORE UPDATE OR DELETE ON migration.legacy_invitation_code_openings
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER legacy_invitation_inventory_imports_immutable
BEFORE UPDATE OR DELETE ON migration.legacy_invitation_inventory_imports
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

REVOKE ALL ON identity.invitation_accounts FROM PUBLIC;
REVOKE ALL ON identity.invitation_balance_events FROM PUBLIC;
REVOKE ALL ON migration.legacy_invitation_balance_openings FROM PUBLIC;
REVOKE ALL ON migration.legacy_invitation_code_openings FROM PUBLIC;
REVOKE ALL ON migration.legacy_invitation_inventory_imports FROM PUBLIC;

-- +goose Down

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM migration.legacy_invitation_inventory_imports)
       OR EXISTS (SELECT 1 FROM identity.invitation_balance_events)
       OR EXISTS (
            SELECT 1 FROM identity.registration_invitations
            WHERE source_kind = 'legacy'
       ) THEN
        RAISE EXCEPTION '202608240005 cannot roll back after invitation balances were imported or used';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER legacy_invitation_inventory_imports_immutable
    ON migration.legacy_invitation_inventory_imports;
DROP TRIGGER legacy_invitation_code_openings_immutable
    ON migration.legacy_invitation_code_openings;
DROP TRIGGER legacy_invitation_balance_openings_immutable
    ON migration.legacy_invitation_balance_openings;
DROP TRIGGER invitation_balance_events_immutable
    ON identity.invitation_balance_events;

DROP TABLE migration.legacy_invitation_inventory_imports;
DROP INDEX migration.legacy_invitation_code_openings_inviter_history_idx;
DROP TABLE migration.legacy_invitation_code_openings;
DROP INDEX migration.legacy_invitation_balance_openings_run_idx;
DROP TABLE migration.legacy_invitation_balance_openings;

ALTER TABLE identity.registration_invitations
    DROP CONSTRAINT registration_invitations_source_valid,
    DROP CONSTRAINT registration_invitations_source_kind_check,
    ADD CONSTRAINT registration_invitations_source_kind_check
        CHECK (source_kind IN ('development', 'member', 'operator')),
    ADD CONSTRAINT registration_invitations_source_valid CHECK (
        (source_kind = 'member'
            AND issuer_user_id IS NOT NULL
            AND issued_authorization_decision_id IS NOT NULL)
        OR
        (source_kind IN ('development', 'operator'))
    );

DROP INDEX identity.invitation_balance_events_user_time_idx;
DROP TABLE identity.invitation_balance_events;
DROP TABLE identity.invitation_accounts;
