-- name: GetUserByCredentialRef :one
SELECT id, credential_ref, username, display_name, status, email_verified_at
FROM identity.users
WHERE credential_ref = sqlc.arg(credential_ref)
  AND NOT EXISTS (
      SELECT 1
      FROM identity.account_restrictions AS restriction
      WHERE restriction.user_id = identity.users.id
        AND restriction.kind = 'account_access'
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= sqlc.arg(as_of)
        AND restriction.expires_at > sqlc.arg(as_of)
  );

-- name: GetPublicUserProfileByUsername :one
SELECT
    users.username,
    users.display_name,
    users.created_at AS joined_at,
    count(torrent.id) FILTER (
        WHERE torrent.state = 'published' AND NOT torrent.anonymous
    )::bigint AS published_torrent_count
FROM identity.users AS users
LEFT JOIN torrents.torrents AS torrent ON torrent.uploader_id = users.id
WHERE lower(users.username) = lower(sqlc.arg(username))
  AND users.status = 'active'
  AND NOT EXISTS (
      SELECT 1
      FROM identity.account_restrictions AS restriction
      WHERE restriction.user_id = users.id
        AND restriction.kind = 'account_access'
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= sqlc.arg(as_of)
        AND restriction.expires_at > sqlc.arg(as_of)
  )
GROUP BY users.id, users.username, users.display_name, users.created_at;

-- name: UpdateMyDisplayName :one
UPDATE identity.users AS users
SET
    display_name = sqlc.arg(display_name),
    updated_at = CASE
        WHEN users.display_name IS DISTINCT FROM sqlc.arg(display_name)::text
        THEN GREATEST(users.updated_at, sqlc.arg(updated_at)::timestamptz)
        ELSE users.updated_at
    END
WHERE users.id = sqlc.arg(user_id)
  AND users.status = 'active'
  AND NOT EXISTS (
      SELECT 1
      FROM identity.account_restrictions AS restriction
      WHERE restriction.user_id = users.id
        AND restriction.kind = 'account_access'
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= sqlc.arg(updated_at)
        AND restriction.expires_at > sqlc.arg(updated_at)
  )
RETURNING id, credential_ref, username, display_name, email_verified_at;

-- name: ResolveUserAvatarObject :one
WITH inserted AS (
    INSERT INTO identity.user_avatar_objects (
        id,
        content_sha256,
        byte_length,
        content_type,
        extension,
        width,
        height,
        created_at
    ) VALUES (
        sqlc.arg(object_id),
        sqlc.arg(content_sha256),
        sqlc.arg(byte_length),
        sqlc.arg(content_type),
        sqlc.arg(extension),
        sqlc.arg(width),
        sqlc.arg(height),
        sqlc.arg(created_at)
    )
    ON CONFLICT (content_sha256) DO NOTHING
    RETURNING id
)
SELECT id FROM inserted
UNION ALL
SELECT existing.id
FROM identity.user_avatar_objects AS existing
WHERE existing.content_sha256 = sqlc.arg(content_sha256)
  AND existing.byte_length = sqlc.arg(byte_length)
  AND existing.content_type = sqlc.arg(content_type)
  AND existing.extension = sqlc.arg(extension)
  AND existing.width = sqlc.arg(width)
  AND existing.height = sqlc.arg(height)
LIMIT 1;

-- name: InsertUserAvatarLocation :execrows
INSERT INTO identity.user_avatar_object_locations (
    object_id,
    backend_id,
    object_key,
    version_id,
    observed_byte_length,
    observed_sha256,
    verified_at
) VALUES (
    sqlc.arg(object_id),
    sqlc.arg(backend_id),
    sqlc.arg(object_key),
    sqlc.arg(version_id),
    sqlc.arg(observed_byte_length),
    sqlc.arg(observed_sha256),
    sqlc.arg(verified_at)
)
ON CONFLICT (object_id, backend_id) DO UPDATE SET
    version_id = CASE
        WHEN identity.user_avatar_object_locations.object_key = EXCLUDED.object_key
          AND identity.user_avatar_object_locations.observed_byte_length = EXCLUDED.observed_byte_length
          AND identity.user_avatar_object_locations.observed_sha256 = EXCLUDED.observed_sha256
        THEN COALESCE(EXCLUDED.version_id, identity.user_avatar_object_locations.version_id)
        ELSE identity.user_avatar_object_locations.version_id
    END,
    verified_at = CASE
        WHEN identity.user_avatar_object_locations.object_key = EXCLUDED.object_key
          AND identity.user_avatar_object_locations.observed_byte_length = EXCLUDED.observed_byte_length
          AND identity.user_avatar_object_locations.observed_sha256 = EXCLUDED.observed_sha256
        THEN GREATEST(identity.user_avatar_object_locations.verified_at, EXCLUDED.verified_at)
        ELSE identity.user_avatar_object_locations.verified_at
    END
WHERE identity.user_avatar_object_locations.object_key = EXCLUDED.object_key
  AND identity.user_avatar_object_locations.observed_byte_length = EXCLUDED.observed_byte_length
  AND identity.user_avatar_object_locations.observed_sha256 = EXCLUDED.observed_sha256;

-- name: SetCurrentUserAvatar :execrows
INSERT INTO identity.user_avatars (user_id, object_id, updated_at)
SELECT users.id, sqlc.arg(object_id), sqlc.arg(updated_at)
FROM identity.users AS users
WHERE users.id = sqlc.arg(user_id)
  AND users.status = 'active'
  AND NOT EXISTS (
      SELECT 1
      FROM identity.account_restrictions AS restriction
      WHERE restriction.user_id = users.id
        AND restriction.kind = 'account_access'
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= sqlc.arg(updated_at)
        AND restriction.expires_at > sqlc.arg(updated_at)
  )
ON CONFLICT (user_id) DO UPDATE SET
    object_id = EXCLUDED.object_id,
    updated_at = EXCLUDED.updated_at;

-- name: GetPublicUserAvatar :one
SELECT
	object_record.id AS object_id,
    object_record.content_sha256,
    object_record.byte_length,
    object_record.content_type,
    object_record.extension,
    object_record.width,
    object_record.height,
    location.backend_id,
    location.object_key,
    location.version_id,
    avatar.updated_at
FROM identity.users AS users
JOIN identity.user_avatars AS avatar ON avatar.user_id = users.id
JOIN identity.user_avatar_objects AS object_record ON object_record.id = avatar.object_id
JOIN LATERAL (
    SELECT candidate.backend_id, candidate.object_key, candidate.version_id
    FROM identity.user_avatar_object_locations AS candidate
    WHERE candidate.object_id = object_record.id
      AND candidate.observed_byte_length = object_record.byte_length
      AND candidate.observed_sha256 = object_record.content_sha256
      AND candidate.state IN ('verified', 'retiring')
    ORDER BY candidate.is_preferred DESC, (candidate.state = 'verified') DESC,
             candidate.verified_at DESC, candidate.backend_id
    LIMIT 1
) AS location ON true
WHERE lower(users.username) = lower(sqlc.arg(username))
  AND users.status = 'active'
  AND NOT EXISTS (
      SELECT 1
      FROM identity.account_restrictions AS restriction
      WHERE restriction.user_id = users.id
        AND restriction.kind = 'account_access'
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= sqlc.arg(as_of)
        AND restriction.expires_at > sqlc.arg(as_of)
  );

-- name: GetRegistrationPolicy :one
SELECT
    mode,
    member_invites_enabled,
    invite_valid_days,
    max_invites_per_member,
    minimum_invite_account_age_days,
    minimum_invite_level,
    username_min_characters,
    username_max_characters,
    reserved_usernames,
    email_domain_mode,
    email_domains,
    session_valid_hours,
    remember_session_valid_hours,
    human_verification_provider,
    human_verification_site_key,
    human_verification_registration_enabled,
    human_verification_login_enabled,
    human_verification_password_recovery_enabled,
    version,
    updated_at
FROM identity.registration_policy
WHERE singleton = true;

-- name: GetRegistrationPolicyForUpdate :one
SELECT
    mode,
    member_invites_enabled,
    invite_valid_days,
    max_invites_per_member,
    minimum_invite_account_age_days,
    minimum_invite_level,
    username_min_characters,
    username_max_characters,
    reserved_usernames,
    email_domain_mode,
    email_domains,
    session_valid_hours,
    remember_session_valid_hours,
    human_verification_provider,
    human_verification_site_key,
    human_verification_registration_enabled,
    human_verification_login_enabled,
    human_verification_password_recovery_enabled,
    version,
    updated_at
FROM identity.registration_policy
WHERE singleton = true
FOR UPDATE;

-- name: UpdateRegistrationPolicy :one
UPDATE identity.registration_policy
SET
    mode = sqlc.arg(mode),
    member_invites_enabled = sqlc.arg(member_invites_enabled),
    invite_valid_days = sqlc.arg(invite_valid_days),
    max_invites_per_member = sqlc.arg(max_invites_per_member),
    minimum_invite_account_age_days = sqlc.arg(minimum_invite_account_age_days),
    minimum_invite_level = sqlc.arg(minimum_invite_level),
    username_min_characters = sqlc.arg(username_min_characters),
    username_max_characters = sqlc.arg(username_max_characters),
    reserved_usernames = sqlc.arg(reserved_usernames),
    email_domain_mode = sqlc.arg(email_domain_mode),
    email_domains = sqlc.arg(email_domains),
    session_valid_hours = sqlc.arg(session_valid_hours),
    remember_session_valid_hours = sqlc.arg(remember_session_valid_hours),
    human_verification_provider = sqlc.arg(human_verification_provider),
    human_verification_site_key = sqlc.arg(human_verification_site_key),
    human_verification_registration_enabled = sqlc.arg(human_verification_registration_enabled),
    human_verification_login_enabled = sqlc.arg(human_verification_login_enabled),
    human_verification_password_recovery_enabled = sqlc.arg(human_verification_password_recovery_enabled),
    version = version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE singleton = true
  AND version = sqlc.arg(expected_version)
RETURNING
    mode,
    member_invites_enabled,
    invite_valid_days,
    max_invites_per_member,
    minimum_invite_account_age_days,
    minimum_invite_level,
    username_min_characters,
    username_max_characters,
    reserved_usernames,
    email_domain_mode,
    email_domains,
    session_valid_hours,
    remember_session_valid_hours,
    human_verification_provider,
    human_verification_site_key,
    human_verification_registration_enabled,
    human_verification_login_enabled,
    human_verification_password_recovery_enabled,
    version,
    updated_at;

-- name: GetWebSessionPolicy :one
SELECT session_valid_hours, remember_session_valid_hours
FROM identity.registration_policy
WHERE singleton = true;

-- name: GetInvitationIssuerContext :one
SELECT
    policy.member_invites_enabled,
    policy.invite_valid_days,
    policy.max_invites_per_member,
    policy.minimum_invite_account_age_days,
    policy.minimum_invite_level,
    users.status,
    (users.email_verified_at IS NOT NULL)::boolean AS email_verified,
    users.created_at,
    COALESCE(progress.level, 1)::smallint AS current_level,
    EXISTS (
        SELECT 1
        FROM identity.account_restrictions AS restriction
        WHERE restriction.user_id = users.id
          AND restriction.kind = 'account_access'
          AND restriction.revoked_at IS NULL
          AND restriction.starts_at <= sqlc.arg(as_of)::timestamptz
          AND restriction.expires_at > sqlc.arg(as_of)::timestamptz
    ) AS account_restricted
FROM identity.registration_policy AS policy
CROSS JOIN identity.users AS users
LEFT JOIN progression.user_progress AS progress ON progress.user_id = users.id
WHERE policy.singleton = true
  AND users.id = sqlc.arg(user_id);

-- name: GetInvitationIssuerContextForUpdate :one
SELECT
    policy.member_invites_enabled,
    policy.invite_valid_days,
    policy.max_invites_per_member,
    policy.minimum_invite_account_age_days,
    policy.minimum_invite_level,
    users.status,
    (users.email_verified_at IS NOT NULL)::boolean AS email_verified,
    users.created_at,
    COALESCE(progress.level, 1)::smallint AS current_level,
    EXISTS (
        SELECT 1
        FROM identity.account_restrictions AS restriction
        WHERE restriction.user_id = users.id
          AND restriction.kind = 'account_access'
          AND restriction.revoked_at IS NULL
          AND restriction.starts_at <= sqlc.arg(as_of)::timestamptz
          AND restriction.expires_at > sqlc.arg(as_of)::timestamptz
    ) AS account_restricted
FROM identity.registration_policy AS policy
CROSS JOIN identity.users AS users
LEFT JOIN progression.user_progress AS progress ON progress.user_id = users.id
WHERE policy.singleton = true
  AND users.id = sqlc.arg(user_id)
FOR UPDATE OF policy, users;

-- name: CountInvitationQuotaUsage :one
SELECT count(*)::bigint
FROM identity.registration_invitations
WHERE issuer_user_id = sqlc.arg(user_id)
  AND source_kind = 'member'
  AND revoked_at IS NULL
  AND (
      consumed_at IS NOT NULL
      OR claimed_by IS NOT NULL
      OR expires_at > sqlc.arg(as_of)::timestamptz
  );

-- name: CountInvitationHistory :one
SELECT count(*)::bigint
FROM identity.registration_invitations
WHERE issuer_user_id = sqlc.arg(user_id)
  AND source_kind = 'member';

-- name: ListInvitationHistory :many
SELECT
    invitation.id,
    invitation.created_at,
    invitation.expires_at,
    invitation.claimed_at,
    invitation.consumed_at,
    invitation.revoked_at,
    CASE
        WHEN invitation.revoked_at IS NOT NULL THEN 'revoked'
        WHEN invitation.consumed_at IS NOT NULL THEN 'used'
        WHEN invitation.claimed_by IS NOT NULL THEN 'claimed'
        WHEN invitation.expires_at <= sqlc.arg(as_of)::timestamptz THEN 'expired'
        ELSE 'available'
    END::text AS status,
    COALESCE(CASE
        WHEN invitation.consumed_at IS NOT NULL THEN registration.username
        ELSE NULL
    END, '')::text AS invitee_username
FROM identity.registration_invitations AS invitation
LEFT JOIN identity.registrations AS registration ON registration.id = invitation.claimed_by
WHERE invitation.issuer_user_id = sqlc.arg(user_id)
  AND invitation.source_kind = 'member'
ORDER BY invitation.created_at DESC, invitation.id DESC
LIMIT sqlc.arg(result_limit) OFFSET sqlc.arg(result_offset);

-- name: InsertMemberInvitation :one
INSERT INTO identity.registration_invitations (
    id,
    token_sha256,
    note,
    expires_at,
    issuer_user_id,
    source_kind,
    issued_authorization_decision_id,
    created_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(token_sha256),
    '',
    sqlc.arg(expires_at),
    sqlc.arg(user_id),
    'member',
    sqlc.arg(authorization_decision_id),
    sqlc.arg(created_at)
)
RETURNING id, created_at, expires_at;

-- name: RevokeMemberInvitation :one
UPDATE identity.registration_invitations
SET
    revoked_at = sqlc.arg(revoked_at),
    revoked_by = sqlc.arg(user_id),
    revoked_authorization_decision_id = sqlc.arg(authorization_decision_id)
WHERE id = sqlc.arg(id)
  AND issuer_user_id = sqlc.arg(user_id)
  AND source_kind = 'member'
  AND claimed_by IS NULL
  AND consumed_at IS NULL
  AND revoked_at IS NULL
RETURNING id, created_at, expires_at, revoked_at;

-- name: LockUserForEmailVerification :one
SELECT id, credential_ref, username, display_name, email_verified_at
FROM identity.users
WHERE credential_ref = sqlc.arg(credential_ref)
  AND status = 'active'
FOR UPDATE;

-- name: MarkCoreUserEmailVerified :one
UPDATE identity.users
SET
    email_verified_at = COALESCE(email_verified_at, sqlc.arg(verified_at)::timestamptz),
    updated_at = GREATEST(updated_at, sqlc.arg(verified_at)::timestamptz)
WHERE id = sqlc.arg(user_id)
  AND credential_ref = sqlc.arg(credential_ref)
RETURNING id, credential_ref, username, display_name, email_verified_at;

-- name: LockUserForPasswordRecovery :one
SELECT
    id,
    credential_ref,
    username,
    display_name,
    email_verified_at,
    password_changed_at,
    last_password_recovery_id
FROM identity.users
WHERE credential_ref = sqlc.arg(credential_ref)
  AND status IN ('active', 'disabled')
FOR UPDATE;

-- name: MarkCoreUserPasswordRecovered :one
UPDATE identity.users
SET
    password_changed_at = sqlc.arg(password_changed_at),
    last_password_recovery_id = sqlc.arg(recovery_id),
    updated_at = GREATEST(updated_at, sqlc.arg(password_changed_at)::timestamptz)
WHERE id = sqlc.arg(user_id)
  AND credential_ref = sqlc.arg(credential_ref)
RETURNING id, credential_ref, username, display_name, email_verified_at,
    password_changed_at, last_password_recovery_id;

-- name: RevokeAllUserSessionsForPasswordRecovery :execrows
UPDATE identity.sessions
SET revoked_at = sqlc.arg(revoked_at)
WHERE user_id = sqlc.arg(user_id)
  AND revoked_at IS NULL;

-- name: UpsertTrackerPasskeyProjection :one
INSERT INTO identity.tracker_passkey_hmac (
    user_id,
    credential_ref,
    lookup_hmac,
    vault_version,
    created_at,
    updated_at
)
SELECT
    users.id,
    users.credential_ref,
    sqlc.arg(lookup_hmac)::bytea,
    sqlc.arg(vault_version)::bigint,
    sqlc.arg(bound_at)::timestamptz,
    sqlc.arg(bound_at)::timestamptz
FROM identity.users AS users
WHERE users.id = sqlc.arg(user_id)::uuid
  AND users.credential_ref = sqlc.arg(credential_ref)::uuid
  AND users.status = 'active'
ON CONFLICT (user_id) DO UPDATE SET
    lookup_hmac = CASE
        WHEN identity.tracker_passkey_hmac.vault_version < EXCLUDED.vault_version
        THEN EXCLUDED.lookup_hmac
        ELSE identity.tracker_passkey_hmac.lookup_hmac
    END,
    vault_version = GREATEST(
        identity.tracker_passkey_hmac.vault_version,
        EXCLUDED.vault_version
    ),
    updated_at = GREATEST(
        identity.tracker_passkey_hmac.updated_at,
        EXCLUDED.updated_at
    )
WHERE identity.tracker_passkey_hmac.credential_ref = EXCLUDED.credential_ref
  AND (
      identity.tracker_passkey_hmac.vault_version < EXCLUDED.vault_version
      OR (
          identity.tracker_passkey_hmac.vault_version = EXCLUDED.vault_version
          AND identity.tracker_passkey_hmac.lookup_hmac = EXCLUDED.lookup_hmac
      )
  )
RETURNING
    user_id,
    credential_ref,
    lookup_hmac,
    vault_version,
    created_at,
    updated_at;

-- name: GetAccountSecurityOverview :one
SELECT email_verified_at, password_changed_at
FROM identity.users
WHERE id = sqlc.arg(user_id)
  AND status = 'active';

-- name: ListActiveUserWebSessions :many
SELECT
    id,
    created_at,
    last_seen_at,
    expires_at,
    token_hash = sqlc.arg(current_token_hash)::bytea AS is_current
FROM identity.sessions
WHERE user_id = sqlc.arg(user_id)
  AND audience = 'web'
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg(as_of)
ORDER BY is_current DESC, last_seen_at DESC, created_at DESC, id;

-- name: RevokeUserWebSessionByID :one
UPDATE identity.sessions
SET revoked_at = sqlc.arg(revoked_at)
WHERE id = sqlc.arg(session_id)
  AND user_id = sqlc.arg(user_id)
  AND audience = 'web'
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg(revoked_at)
RETURNING token_hash, token_hash = sqlc.arg(current_token_hash)::bytea AS was_current;

-- name: RevokeStaffSessionsByParent :execrows
UPDATE identity.sessions
SET revoked_at = sqlc.arg(revoked_at)
WHERE user_id = sqlc.arg(user_id)
  AND audience = 'staff'
  AND parent_token_hash = sqlc.arg(parent_token_hash)
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg(revoked_at);

-- name: RevokeOtherUserWebSessions :execrows
UPDATE identity.sessions
SET revoked_at = sqlc.arg(revoked_at)
WHERE user_id = sqlc.arg(user_id)
  AND audience = 'web'
  AND token_hash <> sqlc.arg(current_token_hash)
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg(revoked_at);

-- name: RevokeOtherUserStaffSessions :execrows
UPDATE identity.sessions
SET revoked_at = sqlc.arg(revoked_at)
WHERE user_id = sqlc.arg(user_id)
  AND audience = 'staff'
  AND parent_token_hash <> sqlc.arg(current_token_hash)
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg(revoked_at);

-- name: ReserveTwoFactorChange :one
INSERT INTO identity.two_factor_changes (
    id,
    user_id,
    kind,
    occurred_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(user_id),
    sqlc.arg(kind),
    sqlc.arg(occurred_at)
)
ON CONFLICT (id) DO NOTHING
RETURNING id, user_id, kind, occurred_at, revoked_web_sessions, revoked_staff_sessions;

-- name: GetTwoFactorChangeForUpdate :one
SELECT id, user_id, kind, occurred_at, revoked_web_sessions, revoked_staff_sessions
FROM identity.two_factor_changes
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: CompleteTwoFactorChange :one
UPDATE identity.two_factor_changes
SET
    revoked_web_sessions = sqlc.arg(revoked_web_sessions),
    revoked_staff_sessions = sqlc.arg(revoked_staff_sessions)
WHERE id = sqlc.arg(id)
  AND user_id = sqlc.arg(user_id)
  AND kind = sqlc.arg(kind)
RETURNING revoked_web_sessions, revoked_staff_sessions;

-- name: LockRegistrationPolicy :one
SELECT
    mode,
    member_invites_enabled,
    invite_valid_days,
    max_invites_per_member,
    minimum_invite_account_age_days,
    minimum_invite_level,
    username_min_characters,
    username_max_characters,
    reserved_usernames,
    email_domain_mode,
    email_domains,
    session_valid_hours,
    remember_session_valid_hours,
    human_verification_provider,
    human_verification_site_key,
    human_verification_registration_enabled,
    human_verification_login_enabled,
    human_verification_password_recovery_enabled,
    version,
    updated_at
FROM identity.registration_policy
WHERE singleton = true
FOR UPDATE;

-- name: GetRegistrationForUpdate :one
SELECT
    id, user_id, username, display_name, admission_mode, invitation_id,
    credential_ref, state, created_at, updated_at, completed_at
FROM identity.registrations
WHERE id = sqlc.arg(registration_id)
FOR UPDATE;

-- name: RegistrationUsernameUnavailable :one
SELECT EXISTS (
    SELECT 1
    FROM identity.users AS existing_user
    WHERE lower(existing_user.username) = lower(sqlc.arg(username)::text)
    UNION ALL
    SELECT 1
    FROM identity.registrations AS existing_registration
    WHERE lower(existing_registration.username) = lower(sqlc.arg(username)::text)
      AND existing_registration.id <> sqlc.arg(registration_id)
) AS unavailable;

-- name: GetAvailableRegistrationInvitationForUpdate :one
SELECT id
FROM identity.registration_invitations
WHERE token_sha256 = sqlc.arg(token_sha256)
  AND expires_at > sqlc.arg(as_of)::timestamptz
  AND revoked_at IS NULL
  AND consumed_at IS NULL
  AND claimed_by IS NULL
FOR UPDATE;

-- name: RegistrationInvitationMatches :one
SELECT EXISTS (
    SELECT 1
    FROM identity.registration_invitations
    WHERE id = sqlc.arg(invitation_id)
      AND token_sha256 = sqlc.arg(token_sha256)
      AND claimed_by = sqlc.arg(registration_id)
) AS matches;

-- name: InsertRegistration :one
INSERT INTO identity.registrations (
    id, user_id, username, display_name, admission_mode, invitation_id,
    state, created_at, updated_at
) VALUES (
    sqlc.arg(registration_id), sqlc.arg(user_id), sqlc.arg(username),
    sqlc.arg(display_name), sqlc.arg(admission_mode), sqlc.narg(invitation_id),
    'reserved', sqlc.arg(occurred_at), sqlc.arg(occurred_at)
)
RETURNING
    id, user_id, username, display_name, admission_mode, invitation_id,
    credential_ref, state, created_at, updated_at, completed_at;

-- name: ClaimRegistrationInvitation :execrows
UPDATE identity.registration_invitations
SET claimed_by = sqlc.arg(registration_id), claimed_at = sqlc.arg(claimed_at)
WHERE id = sqlc.arg(invitation_id)
  AND claimed_by IS NULL
  AND consumed_at IS NULL;

-- name: InsertPendingRegisteredUser :exec
INSERT INTO identity.users (
    id, credential_ref, username, display_name, status,
    created_at, updated_at, email_verified_at
) VALUES (
    sqlc.arg(user_id), sqlc.arg(credential_ref), sqlc.arg(username),
    sqlc.arg(display_name), 'pending', sqlc.arg(occurred_at),
    sqlc.arg(occurred_at), NULL
);

-- name: SetRegistrationCredential :one
UPDATE identity.registrations
SET credential_ref = sqlc.arg(credential_ref),
    state = 'credential_provisioned',
    updated_at = sqlc.arg(occurred_at)
WHERE id = sqlc.arg(registration_id)
  AND state = 'reserved'
  AND credential_ref IS NULL
RETURNING
    id, user_id, username, display_name, admission_mode, invitation_id,
    credential_ref, state, created_at, updated_at, completed_at;

-- name: ActivateRegisteredUser :execrows
UPDATE identity.users
SET status = 'active', updated_at = sqlc.arg(occurred_at)
WHERE id = sqlc.arg(user_id)
  AND credential_ref = sqlc.arg(credential_ref)
  AND status IN ('pending', 'active');

-- name: CompleteRegistration :one
UPDATE identity.registrations
SET state = 'completed',
    completed_at = COALESCE(completed_at, sqlc.arg(occurred_at)::timestamptz),
    updated_at = sqlc.arg(occurred_at)::timestamptz
WHERE id = sqlc.arg(registration_id)
  AND state = 'credential_provisioned'
RETURNING
    id, user_id, username, display_name, admission_mode, invitation_id,
    credential_ref, state, created_at, updated_at, completed_at;

-- name: ConsumeRegistrationInvitation :execrows
UPDATE identity.registration_invitations
SET consumed_at = COALESCE(consumed_at, sqlc.arg(consumed_at)::timestamptz)
WHERE id = sqlc.arg(invitation_id)
  AND claimed_by = sqlc.arg(registration_id)
  AND consumed_at IS NULL;

-- name: GetActiveUserByUsernameForStaffBootstrap :one
SELECT id, username, display_name
FROM identity.users
WHERE lower(username) = lower(sqlc.arg(username))
  AND status = 'active'
  AND NOT EXISTS (
      SELECT 1
      FROM identity.account_restrictions AS restriction
      WHERE restriction.user_id = identity.users.id
        AND restriction.kind = 'account_access'
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= sqlc.arg(as_of)
        AND restriction.expires_at > sqlc.arg(as_of)
  )
FOR UPDATE;

-- name: CreateWebSession :exec
INSERT INTO identity.sessions (
    token_hash,
    user_id,
    audience,
    created_at,
    last_seen_at,
    expires_at
) VALUES (
    sqlc.arg(token_hash),
    sqlc.arg(user_id),
    'web',
    sqlc.arg(created_at),
    sqlc.arg(created_at),
    sqlc.arg(expires_at)
);

-- name: GetActiveWebSession :one
SELECT
    session.token_hash,
    session.created_at,
    session.expires_at,
    users.id AS user_id,
    users.credential_ref,
    users.username,
    users.display_name,
    users.status,
    users.email_verified_at
FROM identity.sessions AS session
JOIN identity.users AS users ON users.id = session.user_id
WHERE session.token_hash = sqlc.arg(token_hash)
  AND session.audience = 'web'
  AND session.revoked_at IS NULL
  AND session.expires_at > sqlc.arg(as_of)
  AND users.status = 'active'
  AND NOT EXISTS (
      SELECT 1
      FROM identity.account_restrictions AS restriction
      WHERE restriction.user_id = users.id
        AND restriction.kind = 'account_access'
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= sqlc.arg(as_of)
        AND restriction.expires_at > sqlc.arg(as_of)
  );

-- name: TouchActiveWebSession :exec
-- Activity writes are coalesced to five-minute buckets. This keeps the user
-- projection useful without turning every authenticated request into a row
-- update or collecting a detailed browsing timeline.
UPDATE identity.sessions
SET last_seen_at = sqlc.arg(seen_at)
WHERE token_hash = sqlc.arg(token_hash)
  AND audience = 'web'
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg(seen_at)
  AND last_seen_at <= sqlc.arg(seen_at)::timestamptz - interval '5 minutes';

-- name: RevokeWebSession :execrows
UPDATE identity.sessions
SET revoked_at = sqlc.arg(revoked_at)
WHERE (
    (
        token_hash = sqlc.arg(token_hash)
        AND audience = 'web'
    ) OR (
        parent_token_hash = sqlc.arg(token_hash)
        AND audience = 'staff'
    )
  )
  AND revoked_at IS NULL;

-- name: ListActiveStaffWebAuthnCredentials :many
SELECT
    credential_id,
    user_id,
    record_ciphertext,
    record_nonce,
    key_epoch
FROM identity.staff_webauthn_credentials
WHERE user_id = sqlc.arg(user_id)
  AND status = 'active'
  AND revoked_at IS NULL
ORDER BY created_at, credential_id;

-- name: CreateStaffWebAuthnChallenge :exec
WITH consumed_previous AS (
    UPDATE identity.staff_webauthn_challenges
    SET consumed_at = GREATEST(sqlc.arg(created_at), created_at)
    WHERE parent_token_hash = sqlc.arg(parent_token_hash)
      AND consumed_at IS NULL
)
INSERT INTO identity.staff_webauthn_challenges (
    id,
    user_id,
    parent_token_hash,
    session_ciphertext,
    session_nonce,
    key_epoch,
    created_at,
    expires_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(user_id),
    sqlc.arg(parent_token_hash),
    sqlc.arg(session_ciphertext),
    sqlc.arg(session_nonce),
    sqlc.arg(key_epoch),
    sqlc.arg(created_at),
    sqlc.arg(expires_at)
);

-- name: ConsumeStaffWebAuthnChallenge :one
UPDATE identity.staff_webauthn_challenges
SET consumed_at = sqlc.arg(consumed_at)
WHERE id = sqlc.arg(id)
  AND user_id = sqlc.arg(user_id)
  AND parent_token_hash = sqlc.arg(parent_token_hash)
  AND consumed_at IS NULL
  AND expires_at > sqlc.arg(consumed_at)
RETURNING
    id,
    user_id,
    parent_token_hash,
    session_ciphertext,
    session_nonce,
    key_epoch,
    created_at,
    expires_at;

-- name: RevokeExistingStaffSessions :exec
UPDATE identity.sessions
SET revoked_at = sqlc.arg(revoked_at)
WHERE user_id = sqlc.arg(user_id)
  AND audience = 'staff'
  AND revoked_at IS NULL;

-- name: UpdateStaffWebAuthnCredential :execrows
UPDATE identity.staff_webauthn_credentials
SET record_ciphertext = sqlc.arg(record_ciphertext),
    record_nonce = sqlc.arg(record_nonce),
    key_epoch = sqlc.arg(key_epoch),
    last_used_at = sqlc.arg(last_used_at),
    updated_at = sqlc.arg(updated_at)
WHERE credential_id = sqlc.arg(credential_id)
  AND user_id = sqlc.arg(user_id)
  AND status = 'active'
  AND revoked_at IS NULL;

-- name: LockActiveWebSessionForStaff :one
SELECT expires_at
FROM identity.sessions AS session
WHERE session.token_hash = sqlc.arg(parent_token_hash)
  AND session.user_id = sqlc.arg(user_id)
  AND session.audience = 'web'
  AND session.revoked_at IS NULL
  AND session.expires_at > sqlc.arg(as_of)
  AND NOT EXISTS (
      SELECT 1
      FROM identity.account_restrictions AS restriction
      WHERE restriction.user_id = session.user_id
        AND restriction.kind = 'account_access'
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= sqlc.arg(as_of)
        AND restriction.expires_at > sqlc.arg(as_of)
  )
FOR UPDATE;

-- name: InsertStaffSession :one
INSERT INTO identity.sessions (
    token_hash,
    user_id,
    audience,
    parent_token_hash,
    staff_credential_id,
    webauthn_authenticated_at,
    authority_grant_id,
    authority_grant_version,
    authority_mandate_id,
    created_at,
    last_seen_at,
    expires_at
) VALUES (
    sqlc.arg(token_hash),
    sqlc.arg(user_id),
    'staff',
    sqlc.arg(parent_token_hash),
    sqlc.arg(staff_credential_id),
    sqlc.arg(webauthn_authenticated_at),
    sqlc.arg(authority_grant_id),
    sqlc.arg(authority_grant_version),
    sqlc.arg(authority_mandate_id),
    sqlc.arg(created_at),
    sqlc.arg(created_at),
    sqlc.arg(expires_at)
)
RETURNING expires_at;

-- name: GetActiveStaffSession :one
SELECT
    staff.token_hash,
    staff.parent_token_hash,
    staff.staff_credential_id,
    staff.authority_grant_id,
    staff.authority_grant_version,
    staff.authority_mandate_id,
    staff.created_at,
    staff.expires_at,
    staff.webauthn_authenticated_at,
    users.id AS user_id,
    users.credential_ref,
    users.username,
    users.display_name,
    users.status,
    users.email_verified_at
FROM identity.sessions AS staff
JOIN identity.sessions AS parent
    ON parent.token_hash = staff.parent_token_hash
   AND parent.user_id = staff.user_id
JOIN identity.users AS users ON users.id = staff.user_id
JOIN identity.staff_webauthn_credentials AS credential
    ON credential.credential_id = staff.staff_credential_id
   AND credential.user_id = staff.user_id
WHERE staff.token_hash = sqlc.arg(token_hash)
  AND staff.audience = 'staff'
  AND staff.revoked_at IS NULL
  AND staff.expires_at > sqlc.arg(as_of)
  AND parent.audience = 'web'
  AND parent.revoked_at IS NULL
  AND parent.expires_at > sqlc.arg(as_of)
  AND credential.status = 'active'
  AND credential.revoked_at IS NULL
  AND users.status = 'active'
  AND NOT EXISTS (
      SELECT 1
      FROM identity.account_restrictions AS restriction
      WHERE restriction.user_id = users.id
        AND restriction.kind = 'account_access'
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= sqlc.arg(as_of)
        AND restriction.expires_at > sqlc.arg(as_of)
  );

-- name: RevokeStaffSession :execrows
UPDATE identity.sessions
SET revoked_at = sqlc.arg(revoked_at)
WHERE token_hash = sqlc.arg(token_hash)
  AND audience = 'staff'
  AND revoked_at IS NULL;

-- name: RevokeActiveStaffBootstrapTickets :exec
UPDATE identity.staff_credential_bootstrap_tickets
SET revoked_at = GREATEST(sqlc.arg(revoked_at), created_at)
WHERE user_id = sqlc.arg(user_id)
  AND consumed_at IS NULL
  AND revoked_at IS NULL;

-- name: InsertStaffBootstrapTicket :one
INSERT INTO identity.staff_credential_bootstrap_tickets (
    id,
    user_id,
    token_hash,
    operator_reference_sha256,
    created_at,
    expires_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(user_id),
    sqlc.arg(token_hash),
    sqlc.arg(operator_reference_sha256),
    sqlc.arg(created_at),
    sqlc.arg(expires_at)
)
RETURNING id, user_id, token_hash, operator_reference_sha256, created_at, expires_at;

-- name: GetStaffBootstrapTicketForUpdate :one
SELECT id, user_id, token_hash, operator_reference_sha256, created_at, expires_at
FROM identity.staff_credential_bootstrap_tickets
WHERE token_hash = sqlc.arg(token_hash)
  AND user_id = sqlc.arg(user_id)
  AND consumed_at IS NULL
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg(as_of)
FOR UPDATE;

-- name: LockActiveWebSessionForEnrollment :one
SELECT expires_at
FROM identity.sessions AS session
WHERE session.token_hash = sqlc.arg(parent_token_hash)
  AND session.user_id = sqlc.arg(user_id)
  AND session.audience = 'web'
  AND session.revoked_at IS NULL
  AND session.expires_at > sqlc.arg(as_of)
  AND NOT EXISTS (
      SELECT 1
      FROM identity.account_restrictions AS restriction
      WHERE restriction.user_id = session.user_id
        AND restriction.kind = 'account_access'
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= sqlc.arg(as_of)
        AND restriction.expires_at > sqlc.arg(as_of)
  )
FOR UPDATE;

-- name: ConsumePreviousStaffEnrollmentChallenges :exec
UPDATE identity.staff_webauthn_enrollment_challenges
SET consumed_at = GREATEST(sqlc.arg(consumed_at), created_at)
WHERE (ticket_id = sqlc.arg(ticket_id)
       OR parent_token_hash = sqlc.arg(parent_token_hash))
  AND consumed_at IS NULL;

-- name: InsertStaffEnrollmentChallenge :exec
INSERT INTO identity.staff_webauthn_enrollment_challenges (
    id,
    ticket_id,
    user_id,
    parent_token_hash,
    label,
    session_ciphertext,
    session_nonce,
    key_epoch,
    created_at,
    expires_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(ticket_id),
    sqlc.arg(user_id),
    sqlc.arg(parent_token_hash),
    sqlc.arg(label),
    sqlc.arg(session_ciphertext),
    sqlc.arg(session_nonce),
    sqlc.arg(key_epoch),
    sqlc.arg(created_at),
    sqlc.arg(expires_at)
);

-- name: ConsumeStaffEnrollmentChallenge :one
UPDATE identity.staff_webauthn_enrollment_challenges AS challenge
SET consumed_at = sqlc.arg(consumed_at)
FROM identity.staff_credential_bootstrap_tickets AS ticket
WHERE challenge.id = sqlc.arg(challenge_id)
  AND challenge.ticket_id = ticket.id
  AND challenge.user_id = sqlc.arg(user_id)
  AND challenge.parent_token_hash = sqlc.arg(parent_token_hash)
  AND challenge.consumed_at IS NULL
  AND challenge.expires_at > sqlc.arg(consumed_at)
  AND ticket.token_hash = sqlc.arg(token_hash)
  AND ticket.user_id = challenge.user_id
  AND ticket.consumed_at IS NULL
  AND ticket.revoked_at IS NULL
  AND ticket.expires_at > sqlc.arg(consumed_at)
RETURNING
    challenge.id,
    challenge.ticket_id,
    challenge.user_id,
    challenge.parent_token_hash,
    challenge.label,
    challenge.session_ciphertext,
    challenge.session_nonce,
    challenge.key_epoch,
    challenge.created_at,
    challenge.expires_at,
    challenge.consumed_at;

-- name: GetStaffBootstrapTicketByIDForUpdate :one
SELECT id, user_id, token_hash, operator_reference_sha256, created_at, expires_at
FROM identity.staff_credential_bootstrap_tickets
WHERE id = sqlc.arg(ticket_id)
  AND token_hash = sqlc.arg(token_hash)
  AND user_id = sqlc.arg(user_id)
  AND consumed_at IS NULL
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg(as_of)
FOR UPDATE;

-- name: GetConsumedStaffEnrollmentChallengeForUpdate :one
SELECT id
FROM identity.staff_webauthn_enrollment_challenges
WHERE id = sqlc.arg(challenge_id)
  AND ticket_id = sqlc.arg(ticket_id)
  AND user_id = sqlc.arg(user_id)
  AND parent_token_hash = sqlc.arg(parent_token_hash)
  AND consumed_at IS NOT NULL
FOR UPDATE;

-- name: InsertStaffWebAuthnCredential :exec
INSERT INTO identity.staff_webauthn_credentials (
    credential_id,
    user_id,
    label,
    record_ciphertext,
    record_nonce,
    key_epoch,
    enrollment_source,
    bootstrap_ticket_id,
    status,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(credential_id),
    sqlc.arg(user_id),
    sqlc.arg(label),
    sqlc.arg(record_ciphertext),
    sqlc.arg(record_nonce),
    sqlc.arg(key_epoch),
    'bootstrap',
    sqlc.arg(bootstrap_ticket_id),
    'active',
    sqlc.arg(created_at),
    sqlc.arg(created_at)
);

-- name: ConsumeStaffBootstrapTicket :execrows
UPDATE identity.staff_credential_bootstrap_tickets
SET consumed_at = sqlc.arg(consumed_at)
WHERE id = sqlc.arg(ticket_id)
  AND user_id = sqlc.arg(user_id)
  AND consumed_at IS NULL
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg(consumed_at);

-- name: CountManagedUsers :one
SELECT count(*)
FROM identity.users AS users
LEFT JOIN identity.user_access_states AS access ON access.user_id = users.id
WHERE (
    sqlc.arg(search_query)::text = ''
    OR users.numeric_id::text = sqlc.arg(search_query)::text
    OR users.id::text = sqlc.arg(search_query)::text
    OR users.username ILIKE '%' || sqlc.arg(search_query)::text || '%'
    OR users.display_name ILIKE '%' || sqlc.arg(search_query)::text || '%'
)
AND (
    sqlc.arg(directory_filter)::text = ''
    OR (sqlc.arg(directory_filter)::text = 'active' AND users.status = 'active')
    OR (sqlc.arg(directory_filter)::text = 'banned' AND users.status = 'disabled')
    OR (sqlc.arg(directory_filter)::text = 'pending' AND users.status = 'pending')
    OR (
        sqlc.arg(directory_filter)::text = 'vip'
        AND access.vip_enabled
        AND (access.vip_until IS NULL OR access.vip_until > sqlc.arg(as_of))
    )
    OR (
        sqlc.arg(directory_filter)::text = 'download_restricted'
        AND identity.is_download_restricted(users.id)
    )
    OR (
        sqlc.arg(directory_filter)::text = 'unverified'
        AND users.email_verified_at IS NULL
    )
);

-- name: GetManagedUserDirectorySummary :one
SELECT
    count(*)::bigint AS total,
    count(*) FILTER (WHERE users.status = 'active')::bigint AS active,
    count(*) FILTER (WHERE users.status = 'disabled')::bigint AS banned,
    count(*) FILTER (
        WHERE access.vip_enabled
          AND (access.vip_until IS NULL OR access.vip_until > sqlc.arg(as_of))
    )::bigint AS vip,
    count(*) FILTER (
        WHERE identity.is_download_restricted(users.id)
    )::bigint AS download_restricted,
    count(*) FILTER (WHERE users.email_verified_at IS NULL)::bigint AS unverified
FROM identity.users AS users
LEFT JOIN identity.user_access_states AS access ON access.user_id = users.id;

-- name: ListManagedUsers :many
SELECT
    users.id,
    users.numeric_id,
    users.credential_ref,
    users.username,
    users.display_name,
    users.status,
    users.administration_version,
    users.created_at,
    users.updated_at,
    (users.email_verified_at IS NOT NULL)::boolean AS email_verified,
    users.status = 'disabled' AS banned,
    count(restrictions.id) AS active_restriction_count,
    COALESCE(traffic.raw_uploaded, 0)::bigint AS uploaded_bytes,
    COALESCE(traffic.raw_downloaded, 0)::bigint AS downloaded_bytes,
    COALESCE(magic.balance, 0)::bigint AS magic_balance,
    COALESCE(progress.level, 1)::integer AS level,
    activity.last_active_at,
    identity.is_download_restricted(users.id) AS download_restricted,
    COALESCE(access.vip_enabled, false) AS vip_enabled,
    COALESCE(
        access.vip_enabled AND (access.vip_until IS NULL OR access.vip_until > sqlc.arg(as_of)),
        false
    )::boolean AS vip_active,
    access.vip_until,
    COALESCE(role_projection.role_names, ARRAY[]::text[])::text[] AS role_names
FROM identity.users AS users
LEFT JOIN identity.account_restrictions AS restrictions
    ON restrictions.user_id = users.id
   AND restrictions.revoked_at IS NULL
   AND restrictions.starts_at <= sqlc.arg(as_of)
   AND restrictions.expires_at > sqlc.arg(as_of)
LEFT JOIN traffic.user_totals AS traffic ON traffic.user_id = users.id
LEFT JOIN economy.magic_accounts AS magic ON magic.user_id = users.id
LEFT JOIN progression.user_progress AS progress ON progress.user_id = users.id
LEFT JOIN identity.user_activity AS activity ON activity.user_id = users.id
LEFT JOIN identity.user_access_states AS access ON access.user_id = users.id
LEFT JOIN LATERAL (
    SELECT array_agg(DISTINCT role.name ORDER BY role.name)::text[] AS role_names
    FROM authz.grants AS user_grant
    JOIN governance.mandates AS mandate
      ON mandate.id = user_grant.mandate_id
     AND mandate.subject_id = user_grant.subject_id
    JOIN authz.roles AS role ON role.id = user_grant.role_id
    WHERE user_grant.subject_id = users.id
      AND user_grant.scope_type = 'site'
      AND user_grant.scope_id = 'peergo'
      AND user_grant.revoked_at IS NULL
      AND user_grant.valid_from <= sqlc.arg(as_of)
      AND user_grant.valid_until > sqlc.arg(as_of)
      AND mandate.status = 'active'
      AND mandate.starts_at <= sqlc.arg(as_of)
      AND mandate.ends_at > sqlc.arg(as_of)
) AS role_projection ON true
WHERE (
    sqlc.arg(search_query)::text = ''
    OR users.numeric_id::text = sqlc.arg(search_query)::text
    OR users.id::text = sqlc.arg(search_query)::text
    OR users.username ILIKE '%' || sqlc.arg(search_query)::text || '%'
    OR users.display_name ILIKE '%' || sqlc.arg(search_query)::text || '%'
)
AND (
    sqlc.arg(directory_filter)::text = ''
    OR (sqlc.arg(directory_filter)::text = 'active' AND users.status = 'active')
    OR (sqlc.arg(directory_filter)::text = 'banned' AND users.status = 'disabled')
    OR (sqlc.arg(directory_filter)::text = 'pending' AND users.status = 'pending')
    OR (
        sqlc.arg(directory_filter)::text = 'vip'
        AND access.vip_enabled
        AND (access.vip_until IS NULL OR access.vip_until > sqlc.arg(as_of))
    )
    OR (
        sqlc.arg(directory_filter)::text = 'download_restricted'
        AND identity.is_download_restricted(users.id)
    )
    OR (
        sqlc.arg(directory_filter)::text = 'unverified'
        AND users.email_verified_at IS NULL
    )
)
GROUP BY users.id, traffic.user_id, magic.user_id, magic.balance, progress.user_id,
    activity.user_id, access.user_id, role_projection.role_names
ORDER BY users.created_at DESC, users.id DESC
LIMIT sqlc.arg(page_size)::integer
OFFSET sqlc.arg(page_offset)::integer;

-- name: GetManagedUser :one
SELECT
    users.id,
    users.numeric_id,
    users.credential_ref,
    users.username,
    users.display_name,
    users.status,
    users.administration_version,
    users.created_at,
    users.updated_at,
    (users.email_verified_at IS NOT NULL)::boolean AS email_verified,
    users.status = 'disabled' AS banned,
    count(restrictions.id) AS active_restriction_count,
    COALESCE(traffic.raw_uploaded, 0)::bigint AS uploaded_bytes,
    COALESCE(traffic.raw_downloaded, 0)::bigint AS downloaded_bytes,
    COALESCE(magic.balance, 0)::bigint AS magic_balance,
    COALESCE(progress.level, 1)::integer AS level,
    activity.last_active_at,
    identity.is_download_restricted(users.id) AS download_restricted,
    COALESCE(access.vip_enabled, false) AS vip_enabled,
    COALESCE(
        access.vip_enabled AND (access.vip_until IS NULL OR access.vip_until > sqlc.arg(as_of)),
        false
    )::boolean AS vip_active,
    access.vip_until,
    COALESCE(role_projection.role_names, ARRAY[]::text[])::text[] AS role_names
FROM identity.users AS users
LEFT JOIN identity.account_restrictions AS restrictions
    ON restrictions.user_id = users.id
   AND restrictions.revoked_at IS NULL
   AND restrictions.starts_at <= sqlc.arg(as_of)
   AND restrictions.expires_at > sqlc.arg(as_of)
LEFT JOIN traffic.user_totals AS traffic ON traffic.user_id = users.id
LEFT JOIN economy.magic_accounts AS magic ON magic.user_id = users.id
LEFT JOIN progression.user_progress AS progress ON progress.user_id = users.id
LEFT JOIN identity.user_activity AS activity ON activity.user_id = users.id
LEFT JOIN identity.user_access_states AS access ON access.user_id = users.id
LEFT JOIN LATERAL (
    SELECT array_agg(DISTINCT role.name ORDER BY role.name)::text[] AS role_names
    FROM authz.grants AS user_grant
    JOIN governance.mandates AS mandate
      ON mandate.id = user_grant.mandate_id
     AND mandate.subject_id = user_grant.subject_id
    JOIN authz.roles AS role ON role.id = user_grant.role_id
    WHERE user_grant.subject_id = users.id
      AND user_grant.scope_type = 'site'
      AND user_grant.scope_id = 'peergo'
      AND user_grant.revoked_at IS NULL
      AND user_grant.valid_from <= sqlc.arg(as_of)
      AND user_grant.valid_until > sqlc.arg(as_of)
      AND mandate.status = 'active'
      AND mandate.starts_at <= sqlc.arg(as_of)
      AND mandate.ends_at > sqlc.arg(as_of)
) AS role_projection ON true
WHERE users.id = sqlc.arg(user_id)
GROUP BY users.id, traffic.user_id, magic.user_id, magic.balance, progress.user_id,
    activity.user_id, access.user_id, role_projection.role_names;

-- name: ListCurrentAccountRestrictions :many
SELECT id, kind, reason_code, reason_summary, starts_at, expires_at, version
FROM identity.account_restrictions
WHERE user_id = sqlc.arg(user_id)
  AND revoked_at IS NULL
  AND starts_at <= sqlc.arg(as_of)
  AND expires_at > sqlc.arg(as_of)
ORDER BY expires_at, id;

-- name: LockManagedUserForAccountRestriction :one
SELECT
    id,
    username,
    display_name,
    status,
    administration_version,
    created_at,
    updated_at
FROM identity.users
WHERE id = sqlc.arg(user_id)
FOR UPDATE;

-- name: GetOverlappingAccountRestrictionForUpdate :one
SELECT id, kind, reason_code, reason_summary, starts_at, expires_at, version
FROM identity.account_restrictions
WHERE user_id = sqlc.arg(user_id)
  AND kind = 'account_access'
  AND revoked_at IS NULL
  AND starts_at < sqlc.arg(expires_at)
  AND expires_at > sqlc.arg(starts_at)
ORDER BY starts_at, id
LIMIT 1
FOR UPDATE;

-- name: InsertAccountAccessRestriction :one
INSERT INTO identity.account_restrictions (
    id,
    user_id,
    kind,
    reason_code,
    reason_summary,
    starts_at,
    expires_at,
    created_by,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(restriction_id),
    sqlc.arg(user_id),
    'account_access',
    sqlc.arg(reason_code),
    sqlc.arg(reason_summary),
    sqlc.arg(starts_at),
    sqlc.arg(expires_at),
    sqlc.arg(actor_id),
    sqlc.arg(starts_at),
    sqlc.arg(starts_at)
)
RETURNING id, kind, reason_code, reason_summary, starts_at, expires_at, version;

-- name: GetAccountRestrictionForUpdate :one
SELECT
    id,
    kind,
    reason_code,
    reason_summary,
    starts_at,
    expires_at,
    revoked_at,
    revocation_reason_code,
    revocation_reason,
    version
FROM identity.account_restrictions
WHERE id = sqlc.arg(restriction_id)
  AND user_id = sqlc.arg(user_id)
  AND kind = 'account_access'
FOR UPDATE;

-- name: RevokeAccountAccessRestriction :one
UPDATE identity.account_restrictions
SET
    revoked_at = sqlc.arg(revoked_at),
    revoked_by = sqlc.arg(actor_id),
    revocation_reason_code = sqlc.arg(revocation_reason_code),
    revocation_reason = sqlc.arg(revocation_reason),
    version = version + 1,
    updated_at = sqlc.arg(revoked_at)
WHERE id = sqlc.arg(restriction_id)
  AND user_id = sqlc.arg(user_id)
  AND version = sqlc.arg(expected_version)
  AND revoked_at IS NULL
RETURNING
    id,
    kind,
    reason_code,
    reason_summary,
    starts_at,
    expires_at,
    revoked_at,
    revocation_reason_code,
    revocation_reason,
    version;

-- name: AdvanceUserAdministrationVersion :one
UPDATE identity.users
SET
    administration_version = administration_version + 1,
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(user_id)
  AND administration_version = sqlc.arg(expected_version)
RETURNING administration_version, updated_at;

-- name: RevokeUserSessionsForAccountRestriction :execrows
UPDATE identity.sessions
SET revoked_at = sqlc.arg(revoked_at)
WHERE user_id = sqlc.arg(user_id)
  AND revoked_at IS NULL;

-- name: GetManualDownloadRestrictionState :one
SELECT
    download_restricted,
    version,
    download_restriction_origin,
    download_restriction_reason_code,
    download_restriction_reason,
    download_restriction_started_at
FROM identity.user_access_states
WHERE user_id = sqlc.arg(user_id);

-- name: LockManualDownloadRestrictionState :one
SELECT
    download_restricted,
    version,
    download_restriction_origin,
    download_restriction_reason_code,
    download_restriction_reason,
    download_restriction_started_at
FROM identity.user_access_states
WHERE user_id = sqlc.arg(user_id)
FOR UPDATE;

-- name: InsertManualDownloadRestrictionState :one
INSERT INTO identity.user_access_states (
    user_id,
    download_restricted,
    vip_enabled,
    download_restriction_origin,
    download_restriction_reason_code,
    download_restriction_reason,
    download_restriction_started_at,
    download_restriction_created_by,
    version,
    updated_at
) VALUES (
    sqlc.arg(user_id),
    true,
    false,
    'staff',
    sqlc.arg(reason_code),
    sqlc.arg(reason),
    sqlc.arg(occurred_at),
    sqlc.arg(actor_id),
    1,
    sqlc.arg(occurred_at)
)
RETURNING download_restricted, version;

-- name: ActivateManualDownloadRestrictionState :one
UPDATE identity.user_access_states
SET
    download_restricted = true,
    download_restriction_origin = 'staff',
    download_restriction_reason_code = sqlc.arg(reason_code),
    download_restriction_reason = sqlc.arg(reason),
    download_restriction_started_at = sqlc.arg(occurred_at),
    download_restriction_created_by = sqlc.arg(actor_id),
    version = version + 1,
    updated_at = sqlc.arg(occurred_at)
WHERE user_id = sqlc.arg(user_id)
  AND NOT download_restricted
  AND version = sqlc.arg(expected_state_version)
RETURNING download_restricted, version;

-- name: UpdateManualDownloadRestrictionState :one
UPDATE identity.user_access_states
SET
    download_restriction_origin = 'staff',
    download_restriction_reason_code = sqlc.arg(reason_code),
    download_restriction_reason = sqlc.arg(reason),
    download_restriction_started_at = sqlc.arg(occurred_at),
    download_restriction_created_by = sqlc.arg(actor_id),
    version = version + 1,
    updated_at = sqlc.arg(occurred_at)
WHERE user_id = sqlc.arg(user_id)
  AND download_restricted
  AND version = sqlc.arg(expected_state_version)
RETURNING download_restricted, version;

-- name: RevokeManualDownloadRestrictionState :one
UPDATE identity.user_access_states
SET
    download_restricted = false,
    download_restriction_origin = NULL,
    download_restriction_reason_code = NULL,
    download_restriction_reason = NULL,
    download_restriction_started_at = NULL,
    download_restriction_created_by = NULL,
    version = version + 1,
    updated_at = sqlc.arg(occurred_at)
WHERE user_id = sqlc.arg(user_id)
  AND download_restricted
  AND version = sqlc.arg(expected_state_version)
RETURNING download_restricted, version;

-- name: InsertManualDownloadRestrictionTransition :exec
INSERT INTO identity.manual_download_restriction_transitions (
    id,
    user_id,
    transition,
    origin,
    reason_code,
    reason,
    actor_id,
    appeal_id,
    from_restricted,
    to_restricted,
    from_state_version,
    state_version,
    occurred_at
) VALUES (
    sqlc.arg(transition_id),
    sqlc.arg(user_id),
    sqlc.arg(transition),
    sqlc.arg(origin),
    sqlc.arg(reason_code),
    sqlc.arg(reason),
    sqlc.narg(actor_id),
    sqlc.narg(appeal_id),
    sqlc.arg(from_restricted),
    sqlc.arg(to_restricted),
    sqlc.arg(from_state_version),
    sqlc.arg(state_version),
    sqlc.arg(occurred_at)
);

-- name: ListManualDownloadRestrictionTransitions :many
SELECT
    transition.transition,
    transition.origin,
    transition.reason_code,
    transition.reason,
    transition.state_version,
    transition.occurred_at,
    actor.numeric_id AS actor_numeric_id,
    actor.username AS actor_username
FROM identity.manual_download_restriction_transitions AS transition
LEFT JOIN identity.users AS actor ON actor.id = transition.actor_id
WHERE transition.user_id = sqlc.arg(user_id)
ORDER BY transition.occurred_at DESC, transition.id DESC
LIMIT 20;

-- name: GetVIPState :one
SELECT vip_enabled, vip_until, version
FROM identity.user_access_states
WHERE user_id = sqlc.arg(user_id);

-- name: LockVIPState :one
SELECT vip_enabled, vip_until, version
FROM identity.user_access_states
WHERE user_id = sqlc.arg(user_id)
FOR UPDATE;

-- name: InsertVIPState :one
INSERT INTO identity.user_access_states (
    user_id, download_restricted, vip_enabled, vip_until, version, updated_at
) VALUES (
    sqlc.arg(user_id), false, true, sqlc.narg(vip_until), 1,
    sqlc.arg(occurred_at)
)
RETURNING vip_enabled, vip_until, version;

-- name: UpdateVIPState :one
UPDATE identity.user_access_states
SET
    vip_enabled = sqlc.arg(vip_enabled),
    vip_until = sqlc.narg(vip_until),
    version = version + 1,
    updated_at = sqlc.arg(occurred_at)
WHERE user_id = sqlc.arg(user_id)
  AND version = sqlc.arg(expected_state_version)
RETURNING vip_enabled, vip_until, version;

-- name: InsertVIPTransition :exec
INSERT INTO identity.user_vip_transitions (
    id, user_id, transition, origin, reason, actor_id,
    from_enabled, from_until, to_enabled, to_until,
    from_state_version, state_version, occurred_at
) VALUES (
    sqlc.arg(transition_id), sqlc.arg(user_id), sqlc.arg(transition),
    'staff', sqlc.arg(reason), sqlc.arg(actor_id),
    sqlc.arg(from_enabled), sqlc.narg(from_until),
    sqlc.arg(to_enabled), sqlc.narg(to_until),
    sqlc.arg(from_state_version), sqlc.arg(state_version),
    sqlc.arg(occurred_at)
);

-- name: InsertVIPRewardBenefitRevision :one
INSERT INTO identity.user_reward_benefit_revisions (
    user_id, revision, effective_from, vip_enabled, vip_until,
    medal_bonus_bps, source_kind, source_reference, created_at
)
SELECT
    sqlc.arg(user_id),
    latest.revision + 1,
    sqlc.arg(occurred_at),
    sqlc.arg(vip_enabled),
    sqlc.narg(vip_until),
    latest.medal_bonus_bps,
    'runtime',
    'vip-transition:' || sqlc.arg(transition_id)::uuid::text,
    sqlc.arg(occurred_at)
FROM LATERAL (
    SELECT revision, medal_bonus_bps
    FROM identity.user_reward_benefit_revisions
    WHERE user_id = sqlc.arg(user_id)
    ORDER BY revision DESC
    LIMIT 1
) AS latest
RETURNING revision;

-- name: ListVIPTransitions :many
SELECT
    transition.transition,
    transition.origin,
    transition.reason,
    transition.to_enabled,
    transition.to_until,
    transition.state_version,
    transition.occurred_at,
    actor.numeric_id AS actor_numeric_id,
    actor.username AS actor_username
FROM identity.user_vip_transitions AS transition
LEFT JOIN identity.users AS actor ON actor.id = transition.actor_id
WHERE transition.user_id = sqlc.arg(user_id)
ORDER BY transition.occurred_at DESC, transition.id DESC
LIMIT 20;
