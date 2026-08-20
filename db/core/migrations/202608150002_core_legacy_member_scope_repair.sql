-- +goose Up
-- The first Rousi importer version persisted the descriptive value "site" as
-- both scope type and scope ID. Core's policy kernel deliberately recognizes
-- only the canonical site ID "peergo", so those grants failed closed with
-- scope_mismatch. Restrict the repair to deterministic legacy-import member
-- authority; no staff or operator-created grant is changed.
UPDATE authz.grants AS member_grant
SET scope_id = 'peergo',
    version = member_grant.version + 1,
    updated_at = now()
FROM governance.mandates AS mandate
WHERE member_grant.mandate_id = mandate.id
  AND member_grant.subject_id = mandate.subject_id
  AND member_grant.role_id = 'member'
  AND member_grant.scope_type = 'site'
  AND member_grant.scope_id = 'site'
  AND mandate.source_type = 'legacy_import'
  AND mandate.source_reference = 'ptyes-user-migration-v1'
  AND mandate.scope_type = 'site'
  AND mandate.scope_id = 'site';

UPDATE governance.mandates
SET scope_id = 'peergo',
    updated_at = now()
WHERE source_type = 'legacy_import'
  AND source_reference = 'ptyes-user-migration-v1'
  AND scope_type = 'site'
  AND scope_id = 'site';

-- +goose Down
-- Data repair is intentionally irreversible. Reintroducing the non-canonical
-- ID would disable every migrated member action after a schema rollback.
SELECT 1;
