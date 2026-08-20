-- name: GetCredentialByLookupHMAC :one
SELECT credential.credential_ref, credential.password_hash
FROM vault.direct_identifiers AS identifier
JOIN vault.credentials AS credential
    ON credential.credential_ref = identifier.credential_ref
LEFT JOIN vault.registration_provisions AS provision
    ON provision.credential_ref = credential.credential_ref
WHERE identifier.lookup_hmac = $1
  AND credential.disabled_at IS NULL
  AND (identifier.kind = 'username' OR identifier.verified_at IS NOT NULL)
  AND (provision.registration_id IS NULL OR provision.status = 'active');

-- name: GetCredentialByReference :one
SELECT credential_ref, password_hash
FROM vault.credentials
WHERE credential_ref = sqlc.arg(credential_ref)
  AND disabled_at IS NULL;

-- name: RehashPasswordIfCurrent :execrows
UPDATE vault.credentials
SET
    password_hash = sqlc.arg(replacement_hash),
    password_algorithm = 'argon2id',
    password_rehashed_at = sqlc.arg(rehashed_at),
    updated_at = sqlc.arg(rehashed_at)
WHERE credential_ref = sqlc.arg(credential_ref)
  AND password_hash = sqlc.arg(expected_hash)
  AND disabled_at IS NULL;

-- name: GetLoginFailureBucket :one
SELECT
    identifier_lookup_hmac,
    failed_attempts,
    window_started_at,
    last_failed_at,
    blocked_until,
    updated_at
FROM vault.login_failure_buckets
WHERE identifier_lookup_hmac = sqlc.arg(identifier_lookup_hmac);

-- name: LockLoginFailureBucket :one
SELECT
    identifier_lookup_hmac,
    failed_attempts,
    window_started_at,
    last_failed_at,
    blocked_until,
    updated_at
FROM vault.login_failure_buckets
WHERE identifier_lookup_hmac = sqlc.arg(identifier_lookup_hmac)
FOR UPDATE;

-- name: InsertLoginFailureBucket :execrows
INSERT INTO vault.login_failure_buckets (
    identifier_lookup_hmac,
    failed_attempts,
    window_started_at,
    last_failed_at,
    blocked_until,
    updated_at
) VALUES (
    sqlc.arg(identifier_lookup_hmac),
    sqlc.arg(failed_attempts),
    sqlc.arg(window_started_at),
    sqlc.arg(last_failed_at),
    sqlc.arg(blocked_until),
    sqlc.arg(updated_at)
)
ON CONFLICT (identifier_lookup_hmac) DO NOTHING;

-- name: UpdateLoginFailureBucket :exec
UPDATE vault.login_failure_buckets
SET
    failed_attempts = sqlc.arg(failed_attempts),
    window_started_at = sqlc.arg(window_started_at),
    last_failed_at = sqlc.arg(last_failed_at),
    blocked_until = sqlc.arg(blocked_until),
    updated_at = sqlc.arg(updated_at)
WHERE identifier_lookup_hmac = sqlc.arg(identifier_lookup_hmac);

-- name: DeleteLoginFailureBucket :exec
DELETE FROM vault.login_failure_buckets
WHERE identifier_lookup_hmac = sqlc.arg(identifier_lookup_hmac);

-- name: GetTwoFactorStatus :one
SELECT
    factor.enabled_at,
    COUNT(code.ordinal) FILTER (
        WHERE code.used_at IS NULL AND code.revoked_at IS NULL
    )::bigint AS recovery_codes_remaining
FROM vault.credentials AS credential
LEFT JOIN vault.totp_factors AS factor
    ON factor.credential_ref = credential.credential_ref
   AND factor.disabled_at IS NULL
LEFT JOIN vault.totp_recovery_codes AS code
    ON code.credential_ref = factor.credential_ref
WHERE credential.credential_ref = sqlc.arg(credential_ref)
  AND credential.disabled_at IS NULL
GROUP BY factor.enabled_at;

-- name: LockCredentialForTwoFactor :one
SELECT credential_ref, password_hash
FROM vault.credentials
WHERE credential_ref = sqlc.arg(credential_ref)
  AND disabled_at IS NULL
FOR UPDATE;

-- name: GetActiveTOTPFactor :one
SELECT
    credential_ref,
    enrollment_id,
    secret_ciphertext,
    secret_nonce,
    key_epoch,
    enabled_at,
    last_used_step,
    updated_at
FROM vault.totp_factors
WHERE credential_ref = sqlc.arg(credential_ref)
  AND disabled_at IS NULL;

-- name: GetActiveTOTPFactorForUpdate :one
SELECT
    credential_ref,
    enrollment_id,
    secret_ciphertext,
    secret_nonce,
    key_epoch,
    enabled_at,
    last_used_step,
    updated_at
FROM vault.totp_factors
WHERE credential_ref = sqlc.arg(credential_ref)
  AND disabled_at IS NULL
FOR UPDATE;

-- name: SupersedePendingTOTPEnrollments :exec
UPDATE vault.totp_enrollments
SET superseded_at = sqlc.arg(superseded_at)
WHERE credential_ref = sqlc.arg(credential_ref)
  AND confirmed_at IS NULL
  AND superseded_at IS NULL;

-- name: InsertTOTPEnrollment :exec
INSERT INTO vault.totp_enrollments (
    id,
    credential_ref,
    secret_ciphertext,
    secret_nonce,
    key_epoch,
    created_at,
    expires_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(credential_ref),
    sqlc.arg(secret_ciphertext),
    sqlc.arg(secret_nonce),
    sqlc.arg(key_epoch),
    sqlc.arg(created_at),
    sqlc.arg(expires_at)
);

-- name: GetTOTPEnrollment :one
SELECT
    id,
    credential_ref,
    secret_ciphertext,
    secret_nonce,
    key_epoch,
    created_at,
    expires_at,
    confirmed_at,
    superseded_at,
    recovery_bundle_ciphertext,
    recovery_bundle_nonce,
    recovery_bundle_expires_at
FROM vault.totp_enrollments
WHERE id = sqlc.arg(id)
  AND credential_ref = sqlc.arg(credential_ref);

-- name: GetTOTPEnrollmentForUpdate :one
SELECT
    id,
    credential_ref,
    secret_ciphertext,
    secret_nonce,
    key_epoch,
    created_at,
    expires_at,
    confirmed_at,
    superseded_at,
    recovery_bundle_ciphertext,
    recovery_bundle_nonce,
    recovery_bundle_expires_at
FROM vault.totp_enrollments
WHERE id = sqlc.arg(id)
  AND credential_ref = sqlc.arg(credential_ref)
FOR UPDATE;

-- name: UpsertTOTPFactor :exec
INSERT INTO vault.totp_factors (
    credential_ref,
    enrollment_id,
    secret_ciphertext,
    secret_nonce,
    key_epoch,
    enabled_at,
    last_used_step,
    updated_at
) VALUES (
    sqlc.arg(credential_ref),
    sqlc.arg(enrollment_id),
    sqlc.arg(secret_ciphertext),
    sqlc.arg(secret_nonce),
    sqlc.arg(key_epoch),
    sqlc.arg(enabled_at),
    -1,
    sqlc.arg(enabled_at)
)
ON CONFLICT (credential_ref) DO UPDATE SET
    enrollment_id = EXCLUDED.enrollment_id,
    secret_ciphertext = EXCLUDED.secret_ciphertext,
    secret_nonce = EXCLUDED.secret_nonce,
    key_epoch = EXCLUDED.key_epoch,
    enabled_at = EXCLUDED.enabled_at,
    disabled_at = NULL,
    last_used_step = -1,
    updated_at = EXCLUDED.updated_at;

-- name: CompleteTOTPEnrollment :execrows
UPDATE vault.totp_enrollments
SET
    confirmed_at = sqlc.arg(confirmed_at),
    recovery_bundle_ciphertext = sqlc.arg(recovery_bundle_ciphertext),
    recovery_bundle_nonce = sqlc.arg(recovery_bundle_nonce),
    recovery_bundle_expires_at = sqlc.arg(recovery_bundle_expires_at)
WHERE id = sqlc.arg(id)
  AND credential_ref = sqlc.arg(credential_ref)
  AND confirmed_at IS NULL
  AND superseded_at IS NULL
  AND expires_at > sqlc.arg(confirmed_at);

-- name: RevokeActiveTOTPRecoveryCodes :exec
UPDATE vault.totp_recovery_codes
SET revoked_at = sqlc.arg(revoked_at)
WHERE credential_ref = sqlc.arg(credential_ref)
  AND used_at IS NULL
  AND revoked_at IS NULL;

-- name: InsertTOTPRecoveryCode :exec
INSERT INTO vault.totp_recovery_codes (
    credential_ref,
    generation_id,
    ordinal,
    code_hmac,
    created_at
) VALUES (
    sqlc.arg(credential_ref),
    sqlc.arg(generation_id),
    sqlc.arg(ordinal),
    sqlc.arg(code_hmac),
    sqlc.arg(created_at)
);

-- name: AdvanceTOTPTimeStep :execrows
UPDATE vault.totp_factors
SET
    last_used_step = sqlc.arg(last_used_step),
    updated_at = sqlc.arg(updated_at)
WHERE credential_ref = sqlc.arg(credential_ref)
  AND disabled_at IS NULL
  AND last_used_step < sqlc.arg(last_used_step);

-- name: ConsumeTOTPRecoveryCode :execrows
UPDATE vault.totp_recovery_codes
SET used_at = sqlc.arg(used_at)
WHERE credential_ref = sqlc.arg(credential_ref)
  AND code_hmac = sqlc.arg(code_hmac)
  AND used_at IS NULL
  AND revoked_at IS NULL;

-- name: DisableTOTPFactor :execrows
UPDATE vault.totp_factors
SET
    disabled_at = sqlc.arg(disabled_at),
    updated_at = sqlc.arg(disabled_at)
WHERE credential_ref = sqlc.arg(credential_ref)
  AND disabled_at IS NULL;

-- name: GetTOTPChange :one
SELECT
    id,
    credential_ref,
    kind,
    changed_at,
    recovery_bundle_ciphertext,
    recovery_bundle_nonce,
    recovery_bundle_key_epoch,
    recovery_bundle_expires_at
FROM vault.totp_changes
WHERE id = sqlc.arg(id);

-- name: InsertTOTPRecoveryRotationChange :exec
INSERT INTO vault.totp_changes (
    id,
    credential_ref,
    kind,
    changed_at,
    recovery_bundle_ciphertext,
    recovery_bundle_nonce,
    recovery_bundle_key_epoch,
    recovery_bundle_expires_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(credential_ref),
    'recovery_codes_rotated',
    sqlc.arg(changed_at),
    sqlc.arg(recovery_bundle_ciphertext),
    sqlc.arg(recovery_bundle_nonce),
    sqlc.arg(recovery_bundle_key_epoch),
    sqlc.arg(recovery_bundle_expires_at)
);

-- name: InsertTOTPDisableChange :exec
INSERT INTO vault.totp_changes (
    id,
    credential_ref,
    kind,
    changed_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(credential_ref),
    'disabled',
    sqlc.arg(changed_at)
);

-- name: ReserveRegistrationProvision :one
INSERT INTO vault.registration_provisions (
    registration_id,
    credential_ref,
    request_hmac,
    status,
    expires_at,
    created_at
) VALUES (
    sqlc.arg(registration_id),
    sqlc.arg(credential_ref),
    sqlc.arg(request_hmac),
    'provisional',
    sqlc.arg(expires_at),
    sqlc.arg(created_at)
)
ON CONFLICT (registration_id) DO NOTHING
RETURNING credential_ref, request_hmac, status, expires_at;

-- name: GetRegistrationProvisionForUpdate :one
SELECT credential_ref, request_hmac, status, expires_at
FROM vault.registration_provisions
WHERE registration_id = sqlc.arg(registration_id)
FOR UPDATE;

-- name: InsertRegistrationCredential :exec
INSERT INTO vault.credentials (
    credential_ref,
    password_hash,
    password_updated_at,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(credential_ref),
    sqlc.arg(password_hash),
    sqlc.arg(created_at),
    sqlc.arg(created_at),
    sqlc.arg(created_at)
);

-- name: InsertRegistrationIdentifier :exec
INSERT INTO vault.direct_identifiers (
    credential_ref,
    kind,
    lookup_hmac,
    masked_value,
    verified_at,
    created_at
) VALUES (
    sqlc.arg(credential_ref),
    sqlc.arg(kind),
    sqlc.arg(lookup_hmac),
    sqlc.arg(masked_value),
    sqlc.arg(verified_at),
    sqlc.arg(created_at)
);

-- name: ActivateRegistrationProvision :one
UPDATE vault.registration_provisions
SET
    status = 'active',
    activated_at = COALESCE(activated_at, sqlc.arg(activated_at)::timestamptz)
WHERE registration_id = sqlc.arg(registration_id)
  AND (status = 'active' OR expires_at > sqlc.arg(activated_at)::timestamptz)
RETURNING credential_ref, status, activated_at;

-- name: LockEmailIdentifierForVerification :one
SELECT identifier.lookup_hmac, identifier.verified_at
FROM vault.direct_identifiers AS identifier
LEFT JOIN vault.registration_provisions AS provision
    ON provision.credential_ref = identifier.credential_ref
WHERE identifier.credential_ref = sqlc.arg(credential_ref)
  AND identifier.kind = 'email'
  AND (provision.registration_id IS NULL OR provision.status = 'active')
FOR UPDATE OF identifier;

-- name: GetEmailVerificationChallengeForUpdate :one
SELECT
    id,
    credential_ref,
    token_sha256,
    email_lookup_hmac,
    delivery_status,
    issued_at,
    expires_at,
    next_request_at,
    delivered_at,
    verified_at
FROM vault.email_verification_challenges
WHERE credential_ref = sqlc.arg(credential_ref)
FOR UPDATE;

-- name: UpsertEmailVerificationChallenge :exec
INSERT INTO vault.email_verification_challenges (
    id,
    credential_ref,
    token_sha256,
    email_lookup_hmac,
    delivery_status,
    issued_at,
    expires_at,
    next_request_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(credential_ref),
    sqlc.arg(token_sha256),
    sqlc.arg(email_lookup_hmac),
    'pending',
    sqlc.arg(issued_at),
    sqlc.arg(expires_at),
    sqlc.arg(next_request_at)
)
ON CONFLICT (credential_ref) DO UPDATE SET
    id = EXCLUDED.id,
    token_sha256 = EXCLUDED.token_sha256,
    email_lookup_hmac = EXCLUDED.email_lookup_hmac,
    delivery_status = 'pending',
    issued_at = EXCLUDED.issued_at,
    expires_at = EXCLUDED.expires_at,
    next_request_at = EXCLUDED.next_request_at,
    delivered_at = NULL,
    verified_at = NULL;

-- name: MarkEmailVerificationDelivered :execrows
UPDATE vault.email_verification_challenges
SET delivery_status = 'sent', delivered_at = sqlc.arg(delivered_at)
WHERE id = sqlc.arg(id)
  AND delivery_status = 'pending';

-- name: MarkEmailVerificationDeliveryFailed :execrows
UPDATE vault.email_verification_challenges
SET delivery_status = 'failed', delivered_at = NULL
WHERE id = sqlc.arg(id)
  AND delivery_status = 'pending';

-- name: GetEmailVerificationChallengeByTokenForUpdate :one
SELECT
    id,
    credential_ref,
    token_sha256,
    email_lookup_hmac,
    delivery_status,
    issued_at,
    expires_at,
    next_request_at,
    delivered_at,
    verified_at
FROM vault.email_verification_challenges
WHERE token_sha256 = sqlc.arg(token_sha256)
FOR UPDATE;

-- name: MarkEmailIdentifierVerified :one
UPDATE vault.direct_identifiers
SET verified_at = COALESCE(verified_at, sqlc.arg(verified_at)::timestamptz)
WHERE credential_ref = sqlc.arg(credential_ref)
  AND kind = 'email'
  AND lookup_hmac = sqlc.arg(email_lookup_hmac)
RETURNING verified_at;

-- name: CompleteEmailVerificationChallenge :one
UPDATE vault.email_verification_challenges
SET verified_at = COALESCE(verified_at, sqlc.arg(verified_at)::timestamptz)
WHERE id = sqlc.arg(id)
RETURNING verified_at;

-- name: LockVerifiedEmailIdentifierByLookup :one
SELECT identifier.credential_ref, identifier.lookup_hmac
FROM vault.direct_identifiers AS identifier
JOIN vault.credentials AS credential
    ON credential.credential_ref = identifier.credential_ref
LEFT JOIN vault.registration_provisions AS provision
    ON provision.credential_ref = identifier.credential_ref
WHERE identifier.kind = 'email'
  AND identifier.lookup_hmac = sqlc.arg(email_lookup_hmac)
  AND identifier.verified_at IS NOT NULL
  AND credential.disabled_at IS NULL
  AND (provision.registration_id IS NULL OR provision.status = 'active')
FOR UPDATE OF identifier;

-- name: GetPasswordRecoveryRateLimitForUpdate :one
SELECT credential_ref, next_issue_at, updated_at
FROM vault.password_recovery_rate_limits
WHERE credential_ref = sqlc.arg(credential_ref)
FOR UPDATE;

-- name: UpsertPasswordRecoveryRateLimit :exec
INSERT INTO vault.password_recovery_rate_limits (
    credential_ref,
    next_issue_at,
    updated_at
) VALUES (
    sqlc.arg(credential_ref),
    sqlc.arg(next_issue_at),
    sqlc.arg(updated_at)
)
ON CONFLICT (credential_ref) DO UPDATE SET
    next_issue_at = EXCLUDED.next_issue_at,
    updated_at = EXCLUDED.updated_at;

-- name: SupersedeLivePasswordRecoveryChallenges :exec
UPDATE vault.password_recovery_challenges
SET superseded_at = sqlc.arg(superseded_at)
WHERE credential_ref = sqlc.arg(credential_ref)
  AND superseded_at IS NULL
  AND consumed_at IS NULL;

-- name: InsertPasswordRecoveryChallenge :exec
INSERT INTO vault.password_recovery_challenges (
    id,
    credential_ref,
    token_sha256,
    email_lookup_hmac,
    delivery_status,
    issued_at,
    expires_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(credential_ref),
    sqlc.arg(token_sha256),
    sqlc.arg(email_lookup_hmac),
    'pending',
    sqlc.arg(issued_at),
    sqlc.arg(expires_at)
);

-- name: MarkPasswordRecoveryDelivered :execrows
UPDATE vault.password_recovery_challenges
SET delivery_status = 'sent', delivered_at = sqlc.arg(delivered_at)
WHERE id = sqlc.arg(id)
  AND delivery_status = 'pending'
  AND superseded_at IS NULL
  AND consumed_at IS NULL;

-- name: MarkPasswordRecoveryDeliveryFailed :execrows
UPDATE vault.password_recovery_challenges
SET delivery_status = 'failed', delivered_at = NULL
WHERE id = sqlc.arg(id)
  AND delivery_status = 'pending'
  AND superseded_at IS NULL
  AND consumed_at IS NULL;

-- name: GetPasswordRecoveryChallengeByToken :one
SELECT
    id,
    credential_ref,
    token_sha256,
    email_lookup_hmac,
    delivery_status,
    issued_at,
    expires_at,
    delivered_at,
    superseded_at,
    consumed_at,
    password_changed_at
FROM vault.password_recovery_challenges
WHERE token_sha256 = sqlc.arg(token_sha256);

-- name: LockEmailIdentifierForPasswordRecovery :one
SELECT lookup_hmac
FROM vault.direct_identifiers
WHERE credential_ref = sqlc.arg(credential_ref)
  AND kind = 'email'
  AND verified_at IS NOT NULL
FOR UPDATE;

-- name: GetPasswordRecoveryChallengeByTokenForUpdate :one
SELECT
    id,
    credential_ref,
    token_sha256,
    email_lookup_hmac,
    delivery_status,
    issued_at,
    expires_at,
    delivered_at,
    superseded_at,
    consumed_at,
    password_changed_at
FROM vault.password_recovery_challenges
WHERE token_sha256 = sqlc.arg(token_sha256)
FOR UPDATE;

-- name: ReplaceCredentialPassword :one
UPDATE vault.credentials
SET
    password_hash = sqlc.arg(password_hash),
    password_algorithm = 'argon2id',
    password_rehashed_at = CASE
        WHEN password_algorithm = 'bcrypt_ptyes_cost10'
            THEN sqlc.arg(password_changed_at)
        ELSE password_rehashed_at
    END,
    password_updated_at = sqlc.arg(password_changed_at),
    updated_at = sqlc.arg(password_changed_at)
WHERE credential_ref = sqlc.arg(credential_ref)
  AND disabled_at IS NULL
RETURNING password_updated_at;

-- name: CompletePasswordRecoveryChallenge :one
UPDATE vault.password_recovery_challenges
SET
    consumed_at = sqlc.arg(consumed_at),
    password_changed_at = sqlc.arg(password_changed_at)
WHERE id = sqlc.arg(id)
  AND consumed_at IS NULL
  AND superseded_at IS NULL
RETURNING consumed_at, password_changed_at;

-- name: ClearCredentialLoginFailureBuckets :exec
DELETE FROM vault.login_failure_buckets AS bucket
USING vault.direct_identifiers AS identifier
WHERE identifier.credential_ref = sqlc.arg(credential_ref)
  AND bucket.identifier_lookup_hmac = identifier.lookup_hmac;

-- name: InsertTrackerPasskeyIfAbsent :one
INSERT INTO vault.tracker_passkeys (
    credential_ref,
    ciphertext,
    nonce,
    encryption_key_epoch,
    lookup_hmac,
    format_profile,
    version,
    created_at,
    updated_at
)
SELECT
    credential.credential_ref,
    sqlc.arg(ciphertext)::bytea,
    sqlc.arg(nonce)::bytea,
    sqlc.arg(encryption_key_epoch)::text,
    sqlc.arg(lookup_hmac)::bytea,
    sqlc.arg(format_profile)::text,
    1,
    sqlc.arg(created_at)::timestamptz,
    sqlc.arg(created_at)::timestamptz
FROM vault.credentials AS credential
LEFT JOIN vault.registration_provisions AS provision
    ON provision.credential_ref = credential.credential_ref
WHERE credential.credential_ref = sqlc.arg(credential_ref)::uuid
  AND credential.disabled_at IS NULL
  AND (provision.registration_id IS NULL OR provision.status = 'active')
ON CONFLICT (credential_ref) DO NOTHING
RETURNING
    credential_ref,
    ciphertext,
    nonce,
    encryption_key_epoch,
    lookup_hmac,
    format_profile,
    version,
    created_at;

-- name: GetTrackerPasskey :one
SELECT
    passkey.credential_ref,
    passkey.ciphertext,
    passkey.nonce,
    passkey.encryption_key_epoch,
    passkey.lookup_hmac,
    passkey.format_profile,
    passkey.version,
    passkey.created_at
FROM vault.tracker_passkeys AS passkey
JOIN vault.credentials AS credential
    ON credential.credential_ref = passkey.credential_ref
LEFT JOIN vault.registration_provisions AS provision
    ON provision.credential_ref = credential.credential_ref
WHERE passkey.credential_ref = sqlc.arg(credential_ref)::uuid
  AND credential.disabled_at IS NULL
  AND (provision.registration_id IS NULL OR provision.status = 'active');

-- name: UpsertDevelopmentCredential :exec
INSERT INTO vault.credentials (
    credential_ref,
    password_hash,
    password_updated_at
) VALUES (
    sqlc.arg(credential_ref),
    sqlc.arg(password_hash),
    sqlc.arg(password_updated_at)
)
ON CONFLICT (credential_ref) DO UPDATE SET
    password_hash = EXCLUDED.password_hash,
    password_algorithm = 'argon2id',
    password_updated_at = EXCLUDED.password_updated_at,
    disabled_at = NULL,
    updated_at = now();

-- name: UpsertDevelopmentIdentifier :exec
INSERT INTO vault.direct_identifiers (
    credential_ref,
    kind,
    lookup_hmac,
    masked_value,
    verified_at
) VALUES (
    sqlc.arg(credential_ref),
    sqlc.arg(kind),
    sqlc.arg(lookup_hmac),
    sqlc.arg(masked_value),
    sqlc.arg(verified_at)
)
ON CONFLICT (credential_ref, kind) DO UPDATE SET
    lookup_hmac = EXCLUDED.lookup_hmac,
    masked_value = EXCLUDED.masked_value,
    verified_at = EXCLUDED.verified_at;
