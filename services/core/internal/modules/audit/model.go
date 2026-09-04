// Package audit owns Core audit-event construction and the reliable outbox.
// It does not provide staff read/delete APIs; projections and the independent
// sink consume immutable events through explicit contracts.
package audit

import (
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
)

const (
	DecisionRecordedEventType              = "authz.decision.recorded"
	DecisionRecordedSchemaVersion          = "1.0.0"
	GrantRevocationEventType               = "authz.grant-revocation.recorded"
	GrantRevocationSchemaVersion           = "1.0.0"
	CategoryChangeEventType                = "catalog.category-change.recorded"
	CategoryChangeSchemaVersion            = "1.0.0"
	AnnouncementChangeEventType            = "catalog.announcement-change.recorded"
	AnnouncementChangeSchemaVersion        = "1.0.0"
	SiteDisplaySettingsChangeEventType     = "catalog.site-display-settings-change.recorded"
	SiteDisplaySettingsChangeSchemaVersion = "1.0.0"
	RegistrationPolicyChangeEventType      = "identity.registration-policy-change.recorded"
	RegistrationPolicyChangeSchemaVersion  = "1.0.0"
	AccountRestrictionEventType            = "identity.account-restriction.recorded"
	AccountRestrictionSchemaVersion        = "1.0.0"
	StaffBootstrapEventType                = "identity.staff-credential-bootstrap.recorded"
	StaffBootstrapSchemaVersion            = "1.0.0"
	RegistrationCompletedEventType         = "identity.registration.completed"
	RegistrationCompletedSchemaVersion     = "1.0.0"
	EmailVerifiedEventType                 = "identity.email.verified"
	EmailVerifiedSchemaVersion             = "1.0.0"
	PasswordRecoveredEventType             = "identity.password.recovered"
	PasswordRecoveredSchemaVersion         = "1.0.0"
	SessionRevocationEventType             = "identity.session-revocation.recorded"
	SessionRevocationSchemaVersion         = "1.0.0"
	TwoFactorChangeEventType               = "identity.two-factor-change.recorded"
	TwoFactorChangeSchemaVersion           = "1.0.0"
	TorrentReviewEventType                 = "review.torrent-review.recorded"
	TorrentReviewSchemaVersion             = "2.0.0"
	TorrentLifecycleEventType              = "torrents.torrent-lifecycle-change.recorded"
	TorrentLifecycleSchemaVersion          = "1.0.0"
	PromotionCampaignEventType             = "promotion.campaign.recorded"
	PromotionCampaignSchemaVersion         = "1.0.0"
	HNRPolicyRevisionEventType             = "hnr.policy-revision.recorded"
	HNRPolicyRevisionSchemaVersion         = "1.0.0"
	CommentModerationDecisionEventType     = "social.comment-moderation-decision.recorded"
	CommentModerationDecisionSchemaVersion = "4.0.0"
	SeedingRewardRetryEventType            = "economy.seeding-reward-retry.recorded"
	SeedingRewardRetrySchemaVersion        = "1.0.0"
	MaxEventPayloadBytes                   = auditevent.MaxPayloadBytes
)

// Event remains an alias at the audit boundary for compatibility; the shared
// envelope lets another module commit business state and evidence atomically
// without importing audit's implementation package.
type Event = auditevent.Event

// PendingEvent includes mutable delivery metadata without exposing it inside
// the evidence payload sent to Audit Sink.
type PendingEvent struct {
	Event
	Attempts int32
}

// SeedingRewardRetryRecordedV1 proves that a terminal hourly reward item was
// deliberately returned to the worker by the operator-only recovery command.
// Mutable operator prose and the user UUID remain in Core and leave it only as
// digests or a keyed pseudonym.
type SeedingRewardRetryRecordedV1 struct {
	SchemaVersion           string    `json:"schema_version"`
	EventType               string    `json:"event_type"`
	EventID                 uuid.UUID `json:"event_id"`
	OccurredAt              time.Time `json:"occurred_at"`
	RetryID                 uuid.UUID `json:"retry_id"`
	WindowStart             time.Time `json:"window_start"`
	UserPseudonym           string    `json:"user_pseudonym"`
	PseudonymKeyEpoch       string    `json:"pseudonym_key_epoch"`
	PreviousAttempts        int32     `json:"previous_attempts"`
	PreviousErrorCode       string    `json:"previous_error_code"`
	OperatorReferenceSHA256 string    `json:"operator_reference_sha256"`
	ReasonSHA256            string    `json:"reason_sha256"`
	Result                  string    `json:"result"`
}

// DecisionRecordedV1 is the reviewed JSON contract for one authorization
// enforcement decision. User UUIDs are replaced by keyed pseudonyms before the
// payload leaves Core; case and authority IDs remain restricted audit data.
type DecisionRecordedV1 struct {
	SchemaVersion      string               `json:"schema_version"`
	EventType          string               `json:"event_type"`
	EventID            uuid.UUID            `json:"event_id"`
	OccurredAt         time.Time            `json:"occurred_at"`
	DecisionID         uuid.UUID            `json:"decision_id"`
	ActorPseudonym     string               `json:"actor_pseudonym"`
	TargetPseudonym    string               `json:"target_pseudonym,omitempty"`
	PseudonymKeyEpoch  string               `json:"pseudonym_key_epoch"`
	Action             string               `json:"action"`
	CredentialAudience string               `json:"credential_audience"`
	Scope              DecisionScopeV1      `json:"scope"`
	Purpose            string               `json:"purpose,omitempty"`
	PurposeSHA256      string               `json:"purpose_sha256,omitempty"`
	CaseID             *uuid.UUID           `json:"case_id,omitempty"`
	PolicyVersion      string               `json:"policy_version"`
	Result             string               `json:"result"`
	Reason             string               `json:"reason"`
	Authority          *DecisionAuthorityV1 `json:"authority,omitempty"`
}

type DecisionScopeV1 struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type DecisionAuthorityV1 struct {
	RoleID         string    `json:"role_id"`
	GrantID        uuid.UUID `json:"grant_id"`
	GrantVersion   int64     `json:"grant_version"`
	MandateID      uuid.UUID `json:"mandate_id"`
	EffectiveUntil time.Time `json:"effective_until"`
}

// GrantRevocationRecordedV1 is emitted from the same PostgreSQL transaction as
// the request, review or grant mutation. Reasons are hashed because the Core
// database remains the owning store for the human-readable governance record.
type GrantRevocationRecordedV1 struct {
	SchemaVersion         string                   `json:"schema_version"`
	EventType             string                   `json:"event_type"`
	EventID               uuid.UUID                `json:"event_id"`
	OccurredAt            time.Time                `json:"occurred_at"`
	RequestID             uuid.UUID                `json:"request_id"`
	GrantID               uuid.UUID                `json:"grant_id"`
	ExpectedGrantVersion  int64                    `json:"expected_grant_version"`
	ResultingGrantVersion int64                    `json:"resulting_grant_version,omitempty"`
	ActorPseudonym        string                   `json:"actor_pseudonym"`
	TargetPseudonym       string                   `json:"target_pseudonym"`
	PseudonymKeyEpoch     string                   `json:"pseudonym_key_epoch"`
	Transition            string                   `json:"transition"`
	ReasonSHA256          string                   `json:"reason_sha256"`
	BeforeSHA256          string                   `json:"before_sha256"`
	AfterSHA256           string                   `json:"after_sha256"`
	DecisionID            uuid.UUID                `json:"decision_id"`
	PolicyVersion         string                   `json:"policy_version"`
	Authority             DecisionAuthorityV1      `json:"authority"`
	Review                *GrantRevocationReviewV1 `json:"review,omitempty"`
}

type GrantRevocationReviewV1 struct {
	ID       uuid.UUID `json:"id"`
	Domain   string    `json:"domain"`
	Decision string    `json:"decision"`
}

// CategoryChangeRecordedV1 records the exact authorized transition while
// hashing editable labels and human-entered reasons. CategoryID remains clear
// because it is already a public, stable catalog identifier.
type CategoryChangeRecordedV1 struct {
	SchemaVersion     string              `json:"schema_version"`
	EventType         string              `json:"event_type"`
	EventID           uuid.UUID           `json:"event_id"`
	OccurredAt        time.Time           `json:"occurred_at"`
	CategoryID        string              `json:"category_id"`
	Transition        string              `json:"transition"`
	ActorPseudonym    string              `json:"actor_pseudonym"`
	PseudonymKeyEpoch string              `json:"pseudonym_key_epoch"`
	ReasonSHA256      string              `json:"reason_sha256"`
	ExpectedVersion   int64               `json:"expected_version,omitempty"`
	ResultingVersion  int64               `json:"resulting_version"`
	BeforeSHA256      string              `json:"before_sha256,omitempty"`
	AfterSHA256       string              `json:"after_sha256"`
	DecisionID        uuid.UUID           `json:"decision_id"`
	PolicyVersion     string              `json:"policy_version"`
	Authority         DecisionAuthorityV1 `json:"authority"`
}

// AnnouncementChangeRecordedV1 proves editorial and publication transitions
// without copying draft or public prose into the external audit stream.
type AnnouncementChangeRecordedV1 struct {
	SchemaVersion     string              `json:"schema_version"`
	EventType         string              `json:"event_type"`
	EventID           uuid.UUID           `json:"event_id"`
	OccurredAt        time.Time           `json:"occurred_at"`
	AnnouncementID    string              `json:"announcement_id"`
	Transition        string              `json:"transition"`
	ActorPseudonym    string              `json:"actor_pseudonym"`
	PseudonymKeyEpoch string              `json:"pseudonym_key_epoch"`
	ReasonSHA256      string              `json:"reason_sha256"`
	ExpectedVersion   int64               `json:"expected_version,omitempty"`
	ResultingVersion  int64               `json:"resulting_version"`
	RevisionNumber    int64               `json:"revision_number"`
	BeforeSHA256      string              `json:"before_sha256,omitempty"`
	AfterSHA256       string              `json:"after_sha256"`
	DecisionID        uuid.UUID           `json:"decision_id"`
	PolicyVersion     string              `json:"policy_version"`
	Authority         DecisionAuthorityV1 `json:"authority"`
}

// TorrentReviewRecordedV2 proves one optimistic review transition. The canonical
// numeric torrent ID and stable reason code remain clear for reconciliation; actor,
// uploader and editable human text are pseudonymized or hashed.
type TorrentReviewRecordedV2 struct {
	SchemaVersion           string              `json:"schema_version"`
	EventType               string              `json:"event_type"`
	EventID                 uuid.UUID           `json:"event_id"`
	OccurredAt              time.Time           `json:"occurred_at"`
	ReviewDecisionID        uuid.UUID           `json:"review_decision_id"`
	TorrentID               int64               `json:"torrent_id"`
	ReviewDecision          string              `json:"review_decision"`
	ReasonCode              string              `json:"reason_code"`
	ReasonSHA256            string              `json:"reason_sha256"`
	ReviewerPseudonym       string              `json:"reviewer_pseudonym"`
	UploaderPseudonym       string              `json:"uploader_pseudonym"`
	PseudonymKeyEpoch       string              `json:"pseudonym_key_epoch"`
	ExpectedVersion         int64               `json:"expected_version"`
	ResultingVersion        int64               `json:"resulting_version"`
	BeforeSHA256            string              `json:"before_sha256"`
	AfterSHA256             string              `json:"after_sha256"`
	AuthorizationDecisionID uuid.UUID           `json:"authorization_decision_id"`
	PolicyVersion           string              `json:"policy_version"`
	Authority               DecisionAuthorityV1 `json:"authority"`
}

// TorrentLifecycleChangeRecordedV1 proves an operational availability change.
// The human reason remains in Core; only its digest leaves the owning store.
type TorrentLifecycleChangeRecordedV1 struct {
	SchemaVersion           string              `json:"schema_version"`
	EventType               string              `json:"event_type"`
	EventID                 uuid.UUID           `json:"event_id"`
	OccurredAt              time.Time           `json:"occurred_at"`
	ChangeID                uuid.UUID           `json:"change_id"`
	TorrentID               int64               `json:"torrent_id"`
	Action                  string              `json:"action"`
	ReasonSHA256            string              `json:"reason_sha256"`
	ActorPseudonym          string              `json:"actor_pseudonym"`
	PseudonymKeyEpoch       string              `json:"pseudonym_key_epoch"`
	ExpectedVersion         int64               `json:"expected_version"`
	ResultingVersion        int64               `json:"resulting_version"`
	BeforeSHA256            string              `json:"before_sha256"`
	AfterSHA256             string              `json:"after_sha256"`
	AuthorizationDecisionID uuid.UUID           `json:"authorization_decision_id"`
	PolicyVersion           string              `json:"policy_version"`
	Authority               DecisionAuthorityV1 `json:"authority"`
}

// PromotionCampaignRecordedV1 proves the exact accounting window authorized
// in Core without copying the human reason into the external audit stream.
type PromotionCampaignRecordedV1 struct {
	SchemaVersion      string              `json:"schema_version"`
	EventType          string              `json:"event_type"`
	EventID            uuid.UUID           `json:"event_id"`
	OccurredAt         time.Time           `json:"occurred_at"`
	CampaignID         uuid.UUID           `json:"campaign_id"`
	Scope              string              `json:"scope"`
	TorrentID          *int64              `json:"torrent_id,omitempty"`
	Promotion          string              `json:"promotion"`
	StartsAt           time.Time           `json:"starts_at"`
	EndsAt             time.Time           `json:"ends_at"`
	OverrideLowerScope bool                `json:"override_lower_scopes"`
	ReasonSHA256       string              `json:"reason_sha256"`
	ActorPseudonym     string              `json:"actor_pseudonym"`
	PseudonymKeyEpoch  string              `json:"pseudonym_key_epoch"`
	DecisionID         uuid.UUID           `json:"decision_id"`
	PolicyVersion      string              `json:"policy_version"`
	Authority          DecisionAuthorityV1 `json:"authority"`
}

// HNRPolicyRevisionRecordedV1 proves which immutable rule was authorized
// without copying the administrator's free-form reason outside Core.
type HNRPolicyRevisionRecordedV1 struct {
	SchemaVersion     string              `json:"schema_version"`
	EventType         string              `json:"event_type"`
	EventID           uuid.UUID           `json:"event_id"`
	OccurredAt        time.Time           `json:"occurred_at"`
	RevisionID        uuid.UUID           `json:"revision_id"`
	RuleID            string              `json:"rule_id"`
	RuleVersion       int64               `json:"rule_version"`
	Mode              string              `json:"mode"`
	EffectiveAt       time.Time           `json:"effective_at"`
	CommandSHA256     string              `json:"command_sha256"`
	ReasonSHA256      string              `json:"reason_sha256"`
	ActorPseudonym    string              `json:"actor_pseudonym"`
	PseudonymKeyEpoch string              `json:"pseudonym_key_epoch"`
	DecisionID        uuid.UUID           `json:"decision_id"`
	PolicyVersion     string              `json:"policy_version"`
	Authority         DecisionAuthorityV1 `json:"authority"`
}

// CommentModerationDecisionRecordedV4 carries the canonical numeric torrent
// ID for torrent targets while retaining the same pseudonymized evidence and
// decision semantics for announcements and posts.
type CommentModerationDecisionRecordedV4 struct {
	SchemaVersion           string              `json:"schema_version"`
	EventType               string              `json:"event_type"`
	EventID                 uuid.UUID           `json:"event_id"`
	OccurredAt              time.Time           `json:"occurred_at"`
	ModerationDecisionID    uuid.UUID           `json:"moderation_decision_id"`
	CaseID                  uuid.UUID           `json:"case_id"`
	CommentID               uuid.UUID           `json:"comment_id"`
	TargetKind              string              `json:"target_kind"`
	TorrentID               *int64              `json:"torrent_id,omitempty"`
	AnnouncementID          string              `json:"announcement_id,omitempty"`
	PostID                  *uuid.UUID          `json:"post_id,omitempty"`
	Decision                string              `json:"decision"`
	ReasonCode              string              `json:"reason_code"`
	NoteSHA256              string              `json:"note_sha256"`
	ModeratorPseudonym      string              `json:"moderator_pseudonym"`
	CommentAuthorPseudonym  string              `json:"comment_author_pseudonym"`
	PseudonymKeyEpoch       string              `json:"pseudonym_key_epoch"`
	ReportCount             int64               `json:"report_count"`
	ExpectedCaseVersion     int64               `json:"expected_case_version"`
	ResultingCaseVersion    int64               `json:"resulting_case_version"`
	ExpectedCommentVersion  int64               `json:"expected_comment_version"`
	ResultingCommentVersion int64               `json:"resulting_comment_version"`
	BeforeSHA256            string              `json:"before_sha256"`
	AfterSHA256             string              `json:"after_sha256"`
	AuthorizationDecisionID uuid.UUID           `json:"authorization_decision_id"`
	PolicyVersion           string              `json:"policy_version"`
	Authority               DecisionAuthorityV1 `json:"authority"`
}

// SiteDisplaySettingsChangeRecordedV1 contains only authorization evidence,
// monotonic versions and hashes. Public copy remains available from catalog,
// but the immutable audit stream does not duplicate mutable site text.
type SiteDisplaySettingsChangeRecordedV1 struct {
	SchemaVersion     string              `json:"schema_version"`
	EventType         string              `json:"event_type"`
	EventID           uuid.UUID           `json:"event_id"`
	OccurredAt        time.Time           `json:"occurred_at"`
	SettingsSection   string              `json:"settings_section"`
	ActorPseudonym    string              `json:"actor_pseudonym"`
	PseudonymKeyEpoch string              `json:"pseudonym_key_epoch"`
	ReasonSHA256      string              `json:"reason_sha256"`
	ExpectedVersion   int64               `json:"expected_version"`
	ResultingVersion  int64               `json:"resulting_version"`
	BeforeSHA256      string              `json:"before_sha256"`
	AfterSHA256       string              `json:"after_sha256"`
	DecisionID        uuid.UUID           `json:"decision_id"`
	PolicyVersion     string              `json:"policy_version"`
	Authority         DecisionAuthorityV1 `json:"authority"`
}

// RegistrationPolicyChangeRecordedV1 proves which version of the admission
// policy replaced which predecessor without duplicating the human reason.
type RegistrationPolicyChangeRecordedV1 struct {
	SchemaVersion     string              `json:"schema_version"`
	EventType         string              `json:"event_type"`
	EventID           uuid.UUID           `json:"event_id"`
	OccurredAt        time.Time           `json:"occurred_at"`
	SettingsSection   string              `json:"settings_section"`
	ActorPseudonym    string              `json:"actor_pseudonym"`
	PseudonymKeyEpoch string              `json:"pseudonym_key_epoch"`
	ReasonSHA256      string              `json:"reason_sha256"`
	ExpectedVersion   int64               `json:"expected_version"`
	ResultingVersion  int64               `json:"resulting_version"`
	BeforeSHA256      string              `json:"before_sha256"`
	AfterSHA256       string              `json:"after_sha256"`
	DecisionID        uuid.UUID           `json:"decision_id"`
	PolicyVersion     string              `json:"policy_version"`
	Authority         DecisionAuthorityV1 `json:"authority"`
}

// AccountRestrictionRecordedV1 proves one bounded access-state transition.
// Account identities are pseudonymized and human-entered text is represented
// only by hashes; clear reason codes remain stable operational categories.
type AccountRestrictionRecordedV1 struct {
	SchemaVersion               string              `json:"schema_version"`
	EventType                   string              `json:"event_type"`
	EventID                     uuid.UUID           `json:"event_id"`
	OccurredAt                  time.Time           `json:"occurred_at"`
	RestrictionID               uuid.UUID           `json:"restriction_id"`
	Transition                  string              `json:"transition"`
	ActorPseudonym              string              `json:"actor_pseudonym"`
	TargetPseudonym             string              `json:"target_pseudonym"`
	PseudonymKeyEpoch           string              `json:"pseudonym_key_epoch"`
	CommandReasonCode           string              `json:"command_reason_code"`
	ReasonSHA256                string              `json:"reason_sha256"`
	ExpectedUserVersion         int64               `json:"expected_user_version"`
	ResultingUserVersion        int64               `json:"resulting_user_version"`
	ExpectedRestrictionVersion  int64               `json:"expected_restriction_version,omitempty"`
	ResultingRestrictionVersion int64               `json:"resulting_restriction_version"`
	BeforeSHA256                string              `json:"before_sha256,omitempty"`
	AfterSHA256                 string              `json:"after_sha256"`
	DecisionID                  uuid.UUID           `json:"decision_id"`
	PolicyVersion               string              `json:"policy_version"`
	Authority                   DecisionAuthorityV1 `json:"authority"`
}

// StaffCredentialBootstrapRecordedV1 links the offline operator ticket to the
// credential that was eventually enrolled without ever serializing either the
// raw ticket or the human-readable operator reference/credential label.
type StaffCredentialBootstrapRecordedV1 struct {
	SchemaVersion           string               `json:"schema_version"`
	EventType               string               `json:"event_type"`
	EventID                 uuid.UUID            `json:"event_id"`
	OccurredAt              time.Time            `json:"occurred_at"`
	TicketID                uuid.UUID            `json:"ticket_id"`
	Transition              string               `json:"transition"`
	TargetPseudonym         string               `json:"target_pseudonym"`
	PseudonymKeyEpoch       string               `json:"pseudonym_key_epoch"`
	OperatorReferenceSHA256 string               `json:"operator_reference_sha256"`
	TicketExpiresAt         time.Time            `json:"ticket_expires_at"`
	ChallengeID             *uuid.UUID           `json:"challenge_id,omitempty"`
	CredentialIDSHA256      string               `json:"credential_id_sha256,omitempty"`
	LabelSHA256             string               `json:"label_sha256,omitempty"`
	DecisionID              *uuid.UUID           `json:"decision_id,omitempty"`
	PolicyVersion           string               `json:"policy_version,omitempty"`
	Authority               *DecisionAuthorityV1 `json:"authority,omitempty"`
}

// RegistrationCompletedV1 records only the opaque saga identifier, admission
// mode and a keyed user pseudonym. Direct identifiers and credential material
// never enter the audit stream.
type RegistrationCompletedV1 struct {
	SchemaVersion     string     `json:"schema_version"`
	EventType         string     `json:"event_type"`
	EventID           uuid.UUID  `json:"event_id"`
	OccurredAt        time.Time  `json:"occurred_at"`
	RegistrationID    uuid.UUID  `json:"registration_id"`
	UserPseudonym     string     `json:"user_pseudonym"`
	PseudonymKeyEpoch string     `json:"pseudonym_key_epoch"`
	AdmissionMode     string     `json:"admission_mode"`
	InvitationID      *uuid.UUID `json:"invitation_id,omitempty"`
}

// EmailVerifiedV1 proves the state transition without exposing the email,
// credential reference, token or token digest to the external audit stream.
type EmailVerifiedV1 struct {
	SchemaVersion     string    `json:"schema_version"`
	EventType         string    `json:"event_type"`
	EventID           uuid.UUID `json:"event_id"`
	OccurredAt        time.Time `json:"occurred_at"`
	VerificationID    uuid.UUID `json:"verification_id"`
	UserPseudonym     string    `json:"user_pseudonym"`
	PseudonymKeyEpoch string    `json:"pseudonym_key_epoch"`
}

// PasswordRecoveredV1 proves the recovery transition and its session blast
// radius without exposing the email, credential reference, password material
// or bearer-token digest.
type PasswordRecoveredV1 struct {
	SchemaVersion     string    `json:"schema_version"`
	EventType         string    `json:"event_type"`
	EventID           uuid.UUID `json:"event_id"`
	OccurredAt        time.Time `json:"occurred_at"`
	RecoveryID        uuid.UUID `json:"recovery_id"`
	UserPseudonym     string    `json:"user_pseudonym"`
	PseudonymKeyEpoch string    `json:"pseudonym_key_epoch"`
	RevokedSessions   int64     `json:"revoked_sessions"`
}

// SessionRevocationRecordedV1 proves an authorized self-service revocation
// without exposing bearer-token digests or a linkable public session UUID.
// The target pseudonym is present only for the single-session scope.
type SessionRevocationRecordedV1 struct {
	SchemaVersion          string              `json:"schema_version"`
	EventType              string              `json:"event_type"`
	EventID                uuid.UUID           `json:"event_id"`
	OccurredAt             time.Time           `json:"occurred_at"`
	RevocationID           uuid.UUID           `json:"revocation_id"`
	Scope                  string              `json:"scope"`
	UserPseudonym          string              `json:"user_pseudonym"`
	TargetSessionPseudonym string              `json:"target_session_pseudonym,omitempty"`
	PseudonymKeyEpoch      string              `json:"pseudonym_key_epoch"`
	RevokedWebSessions     int64               `json:"revoked_web_sessions"`
	RevokedStaffSessions   int64               `json:"revoked_staff_sessions"`
	CurrentSessionRevoked  bool                `json:"current_session_revoked"`
	DecisionID             uuid.UUID           `json:"decision_id"`
	PolicyVersion          string              `json:"policy_version"`
	Authority              DecisionAuthorityV1 `json:"authority"`
}

// TwoFactorChangeRecordedV1 proves the lifecycle change and resulting session
// invalidation without copying a seed, recovery code, credential reference or
// public session identifier into the external audit stream.
type TwoFactorChangeRecordedV1 struct {
	SchemaVersion        string              `json:"schema_version"`
	EventType            string              `json:"event_type"`
	EventID              uuid.UUID           `json:"event_id"`
	OccurredAt           time.Time           `json:"occurred_at"`
	ChangeID             uuid.UUID           `json:"change_id"`
	Kind                 string              `json:"kind"`
	UserPseudonym        string              `json:"user_pseudonym"`
	PseudonymKeyEpoch    string              `json:"pseudonym_key_epoch"`
	RevokedWebSessions   int64               `json:"revoked_web_sessions"`
	RevokedStaffSessions int64               `json:"revoked_staff_sessions"`
	DecisionID           uuid.UUID           `json:"decision_id"`
	PolicyVersion        string              `json:"policy_version"`
	Authority            DecisionAuthorityV1 `json:"authority"`
}
