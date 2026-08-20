-- +goose Up

-- PtYes TOTP seeds/recovery codes are intentionally not copied. The public
-- identity projection retains only a non-secret UX flag so affected members
-- can be prompted to enroll a fresh factor after cutover.
ALTER TABLE identity.users
    ADD COLUMN two_factor_reenrollment_required boolean NOT NULL DEFAULT false;

-- Imported users receive only the ordinary member role. legacy_import is a
-- provenance value for that finite operator-created mandate; it does not carry
-- a PtYes role, admin badge, ban reason, or other historical authority.
ALTER TABLE governance.mandates
    DROP CONSTRAINT mandates_source_type_check,
    ADD CONSTRAINT mandates_source_type_check CHECK (
        source_type IN ('bootstrap', 'appointment', 'election', 'emergency', 'legacy_import')
    );

-- +goose Down

ALTER TABLE governance.mandates
    DROP CONSTRAINT mandates_source_type_check,
    ADD CONSTRAINT mandates_source_type_check CHECK (
        source_type IN ('bootstrap', 'appointment', 'election', 'emergency')
    );

ALTER TABLE identity.users
    DROP COLUMN two_factor_reenrollment_required;
