package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/peergo/peergo/services/core/internal/contracts/vaultoperations"
	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/economy"
	"github.com/peergo/peergo/services/core/internal/modules/economy/attendance"
	"github.com/peergo/peergo/services/core/internal/modules/economy/contenttip"
	"github.com/peergo/peergo/services/core/internal/modules/economy/medals"
	"github.com/peergo/peergo/services/core/internal/modules/economy/membergift"
	"github.com/peergo/peergo/services/core/internal/modules/economy/seedingreward"
	"github.com/peergo/peergo/services/core/internal/modules/hnradmin"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/moviepilot"
	"github.com/peergo/peergo/services/core/internal/modules/newcomer"
	"github.com/peergo/peergo/services/core/internal/modules/notifications"
	"github.com/peergo/peergo/services/core/internal/modules/operations"
	"github.com/peergo/peergo/services/core/internal/modules/personalapikey"
	"github.com/peergo/peergo/services/core/internal/modules/progression"
	"github.com/peergo/peergo/services/core/internal/modules/promotions"
	"github.com/peergo/peergo/services/core/internal/modules/ratiowatch"
	"github.com/peergo/peergo/services/core/internal/modules/rss"
	"github.com/peergo/peergo/services/core/internal/modules/social"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/modules/trackercontrol"
	"github.com/peergo/peergo/services/core/internal/modules/traffic"
	"github.com/peergo/peergo/services/core/internal/modules/wiki"
	"github.com/peergo/peergo/services/core/internal/modules/workgroups"
)

// IdentityService is the use-case surface required by the HTTP adapter.
type IdentityService interface {
	Login(context.Context, identity.LoginInput) (identity.WebSession, error)
	CurrentSession(context.Context, string) (identity.WebSession, error)
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
	PublicProfile(context.Context, string, string) (identity.PublicUserProfile, error)
	UpdateProfile(context.Context, string, string, identity.UpdateMyProfileInput) (identity.User, error)
	UpdateAvatar(context.Context, string, string, io.Reader) (identity.AvatarRevision, error)
	PublicAvatar(context.Context, string, string) (identity.PublicAvatar, error)
	Logout(context.Context, string, string) error
	InspectAccountAccess(context.Context, identity.InspectAccountAccessInput) (identity.AccountAccessStatus, error)
	SubmitAccountAccessAppeal(context.Context, identity.SubmitAccountAccessAppealInput) (identity.AccountAccessAppeal, error)
	AccountAccessAppeals(context.Context, authz.StaffActor, identity.AccountAccessAppealQuery) (identity.AccountAccessAppealPage, error)
	DecideAccountAccessAppeal(context.Context, authz.StaffActor, identity.DecideAccountAccessAppealInput) (identity.AccountAccessAppeal, error)
	MyDownloadRestriction(context.Context, string) (identity.DownloadRestrictionStatus, error)
	SubmitDownloadRestrictionAppeal(context.Context, string, string, identity.SubmitDownloadRestrictionAppealInput) (identity.AccountAccessAppeal, error)
}

// RegistrationService is the anonymous admission surface. It deliberately
// remains separate from Web-session creation so registration policy and the
// cross-store state machine cannot leak into ordinary login code.
type RegistrationService interface {
	PublicPolicy(context.Context) (identity.RegistrationPublicPolicy, error)
	Register(context.Context, identity.RegistrationInput) (identity.RegistrationResult, error)
	Policy(context.Context, authz.StaffActor) (identity.RegistrationPolicy, error)
	UpdatePolicy(context.Context, authz.StaffActor, identity.UpdateRegistrationPolicyInput) (identity.RegistrationPolicy, error)
}

// InvitationService exposes only the current member's invitation projection.
// The issue result is the sole surface allowed to return a raw bearer token.
type InvitationService interface {
	Overview(context.Context, string, int, int) (identity.InvitationOverview, error)
	Issue(context.Context, string, string, string) (identity.InvitationIssueResult, error)
	Revoke(context.Context, string, string, uuid.UUID) (identity.MemberInvitation, error)
}

// EmailVerificationService owns the authenticated resend and anonymous token
// confirmation use cases without exposing Vault identifiers to HTTP DTOs.
type EmailVerificationService interface {
	Request(context.Context, string, string, string) (identity.EmailVerificationDispatch, error)
	Confirm(context.Context, string) (identity.EmailVerificationCompletion, error)
}

// PasswordRecoveryService owns the anonymous, enumeration-safe request and
// single-purpose confirmation flow. It never returns account identity data.
type PasswordRecoveryService interface {
	Request(context.Context, string) (identity.PasswordRecoveryDispatch, error)
	Confirm(context.Context, string, string) (identity.PasswordRecoveryCompletion, error)
}

// SessionSecurityService exposes only the current user's redacted security
// projection and audited self-service revocation commands.
type SessionSecurityService interface {
	Overview(context.Context, string) (identity.AccountSecurityOverview, error)
	ListSessions(context.Context, string) ([]identity.UserWebSession, error)
	RevokeSession(context.Context, string, string, uuid.UUID) (identity.SessionRevocationResult, error)
	RevokeOtherSessions(context.Context, string, string) (identity.SessionRevocationResult, error)
}

type TwoFactorService interface {
	StartEnrollment(context.Context, string, string, identity.TOTPEnrollmentCommand) (identity.TOTPEnrollmentStart, error)
	ConfirmEnrollment(context.Context, string, string, identity.TOTPEnrollmentConfirmationCommand) (identity.TOTPEnrollmentConfirmation, error)
	RotateRecoveryCodes(context.Context, string, string, identity.TwoFactorReauthenticationCommand) (identity.TwoFactorVaultChange, error)
	Disable(context.Context, string, string, identity.TwoFactorReauthenticationCommand) (identity.TwoFactorVaultChange, error)
}

// StaffIdentityService is a separate credential audience. Implementations
// require an ordinary Web session only to run elevation and accept only the
// staff cookie for session introspection and revocation.
type StaffIdentityService interface {
	BeginElevation(context.Context, string, string) (identity.StaffElevationOptions, error)
	CompleteElevation(context.Context, string, string, identity.CompleteStaffElevationInput) (identity.StaffSession, error)
	CurrentSession(context.Context, string) (identity.StaffSession, error)
	AuthenticateWrite(context.Context, string, string) (identity.StaffSession, error)
	Logout(context.Context, string, string) error
}

// StaffEnrollmentService is the controlled credential-provisioning surface.
// It accepts only the ordinary Web cookie plus the one-time operator ticket;
// no staff session can bootstrap another credential through these routes.
type StaffEnrollmentService interface {
	Begin(context.Context, string, string, identity.BeginStaffEnrollmentInput) (identity.StaffEnrollmentOptions, error)
	Complete(context.Context, string, string, identity.CompleteStaffEnrollmentInput) (identity.StaffCredentialEnrollment, error)
}

// AuthorizationService is the default-deny capability boundary required by
// the HTTP adapter. It returns only the caller's discoverable projection.
type AuthorizationService interface {
	Authorize(context.Context, authz.Request) (authz.Decision, error)
	Capabilities(context.Context, authz.Subject) (authz.CapabilitySet, error)
	StaffCapabilities(context.Context, authz.Subject, time.Time, authz.AuthorityBinding) (authz.CapabilitySet, error)
}

// GrantAdministrationService exposes only the governance use cases needed by
// this adapter. Every method performs its own typed authorization decision.
type GrantAdministrationService interface {
	Overview(context.Context, authz.GrantAdministrationActor) (authz.GrantAdministrationOverview, error)
	ProposeRevocation(context.Context, authz.GrantAdministrationActor, authz.ProposeGrantRevocationInput) (authz.GrantRevocationRequest, error)
	ReviewRevocation(context.Context, authz.GrantAdministrationActor, authz.ReviewGrantRevocationInput) (authz.GrantRevocationRequest, error)
}

// CategoryAdministrationService is the catalog-owned staff surface. Read and
// write methods both perform typed authorization inside the use case; HTTP
// session checks only establish the credential audience and verified actor.
type CategoryAdministrationService interface {
	List(context.Context, authz.StaffActor) ([]catalog.ManagedCategory, error)
	Create(context.Context, authz.StaffActor, catalog.CreateCategoryInput) (catalog.ManagedCategory, error)
	Update(context.Context, authz.StaffActor, catalog.UpdateCategoryInput) (catalog.ManagedCategory, error)
	UpsertFacet(context.Context, authz.StaffActor, catalog.UpsertCategoryFacetInput) (catalog.ManagedCategoryFacet, error)
	UpsertFacetOption(context.Context, authz.StaffActor, catalog.UpsertCategoryFacetOptionInput) (catalog.ManagedCategoryFacetOption, error)
}

// AnnouncementAdministrationService owns the editorial aggregate. Preview
// reads and every mutation stay behind the independent staff-session audience.
type AnnouncementAdministrationService interface {
	List(context.Context, authz.StaffActor, int, int) (catalog.ManagedAnnouncementPage, error)
	Get(context.Context, authz.StaffActor, string) (catalog.ManagedAnnouncement, error)
	Revisions(context.Context, authz.StaffActor, string, int, int) (catalog.AnnouncementRevisionPage, error)
	Create(context.Context, authz.StaffActor, catalog.CreateAnnouncementDraftInput) (catalog.ManagedAnnouncement, error)
	UpdateDraft(context.Context, authz.StaffActor, catalog.UpdateAnnouncementDraftInput) (catalog.ManagedAnnouncement, error)
	ChangePublication(context.Context, authz.StaffActor, catalog.ChangeAnnouncementPublicationInput) (catalog.ManagedAnnouncement, error)
}

// WikiService exposes public/member reads, assigned-editor writes and the
// independently authenticated staff editorial surface.
type WikiService interface {
	List(context.Context, string, wiki.ListInput) (wiki.PageList, error)
	Get(context.Context, string, string) (wiki.Page, error)
	UpdateAssigned(context.Context, string, string, wiki.UpdateAssignedInput) (wiki.Page, error)
	ListManaged(context.Context, authz.StaffActor, wiki.ListInput) (wiki.PageList, error)
	GetManaged(context.Context, authz.StaffActor, uuid.UUID) (wiki.Page, error)
	CreateManaged(context.Context, authz.StaffActor, wiki.CreateManagedInput) (wiki.Page, error)
	UpdateManaged(context.Context, authz.StaffActor, wiki.UpdateManagedInput) (wiki.Page, error)
	Revisions(context.Context, authz.StaffActor, uuid.UUID, int, int) (wiki.RevisionPage, error)
	RestoreManaged(context.Context, authz.StaffActor, wiki.RestoreManagedInput) (wiki.Page, error)
}

// SiteDisplaySettingsService is the catalog-owned typed settings section. It
// intentionally exposes one bounded value object rather than arbitrary keys.
type SiteDisplaySettingsService interface {
	Get(context.Context, authz.StaffActor) (catalog.SiteDisplaySettings, error)
	Update(context.Context, authz.StaffActor, catalog.UpdateSiteDisplaySettingsInput) (catalog.SiteDisplaySettings, error)
}

// RSSService owns both ordinary-session subscription management and the
// separate token audience used by RSS clients. Raw tokens never pass through
// another module or a generic settings map.
type RSSService interface {
	List(context.Context, string) ([]rss.Subscription, error)
	Create(context.Context, string, string, rss.SubscriptionInput) (rss.IssuedSubscription, error)
	Update(context.Context, string, string, rss.UpdateSubscriptionInput) (rss.Subscription, error)
	Rotate(context.Context, string, string, rss.SubscriptionVersionInput) (rss.IssuedSubscription, error)
	Revoke(context.Context, string, string, rss.SubscriptionVersionInput) error
	Feed(context.Context, string) (rss.FeedDocument, error)
	Download(context.Context, string, int64) (torrents.TorrentDownloadResult, error)
	Settings(context.Context, authz.StaffActor) (rss.Settings, error)
	UpdateSettings(context.Context, authz.StaffActor, rss.UpdateSettingsInput) (rss.Settings, error)
}

// PersonalAPIKeyService owns the shared user credential used by external
// adapters. Raw keys cross this boundary only once, when created or rotated.
type PersonalAPIKeyService interface {
	Status(context.Context, string) (personalapikey.Status, error)
	Rotate(context.Context, string, string, *int64, []personalapikey.Scope) (personalapikey.IssuedCredential, error)
	Revoke(context.Context, string, string, int64) error
	Authenticate(context.Context, string) (personalapikey.AuthenticatedCredential, error)
}

// MoviePilotService is the canonical compatibility projection shared by
// MoviePilot and PT-depiler. It consumes an already authenticated personal key
// and owns no key rows.
type MoviePilotService interface {
	Profile(context.Context, personalapikey.AuthenticatedCredential) (moviepilot.Profile, error)
	SeedingReward(context.Context, personalapikey.AuthenticatedCredential) (int64, error)
	ListTorrents(context.Context, personalapikey.AuthenticatedCredential, int, int, string, string) (moviepilot.TorrentPage, error)
	Torrent(context.Context, personalapikey.AuthenticatedCredential, int64) (moviepilot.TorrentDownloadDescriptor, error)
	Download(context.Context, int64, string) (torrents.TorrentDownloadResult, error)
	DownloadWithCredential(context.Context, personalapikey.AuthenticatedCredential, string) (torrents.TorrentDownloadResult, error)
	AttendanceOverview(context.Context, personalapikey.AuthenticatedCredential) (attendance.Overview, error)
	ClaimAttendance(context.Context, personalapikey.AuthenticatedCredential, attendance.Mode) (attendance.Record, error)
}

// UserAdministrationService exposes the authorized operational projection and
// one bounded account-access restriction command. The user-account reader may
// receive a Vault-sourced email, but DTOs cannot carry credentials, IPs,
// passkeys, Tracker secrets or ledger evidence.
type UserAdministrationService interface {
	List(context.Context, authz.StaffActor, identity.ListManagedUsersInput) (identity.ManagedUserPage, error)
	Get(context.Context, authz.StaffActor, uuid.UUID) (identity.ManagedUserDetail, error)
	Adjust(context.Context, authz.StaffActor, identity.ManagedUserAdjustmentInput) (identity.ManagedUserDetail, error)
	NetworkHistory(context.Context, authz.StaffActor, uuid.UUID) (identity.ManagedUserNetworkHistory, error)
	CreateRestriction(context.Context, authz.StaffActor, identity.CreateAccountRestrictionInput) (identity.ManagedUserDetail, error)
	RevokeRestriction(context.Context, authz.StaffActor, identity.RevokeAccountRestrictionInput) (identity.ManagedUserDetail, error)
	Reactivate(context.Context, authz.StaffActor, identity.ReactivateManagedUserInput) (identity.ManagedUserDetail, error)
	CreateManualDownloadRestriction(context.Context, authz.StaffActor, identity.CreateManualDownloadRestrictionInput) (identity.ManagedUserDetail, error)
	UpdateManualDownloadRestriction(context.Context, authz.StaffActor, identity.UpdateManualDownloadRestrictionInput) (identity.ManagedUserDetail, error)
	RevokeManualDownloadRestriction(context.Context, authz.StaffActor, identity.RevokeManualDownloadRestrictionInput) (identity.ManagedUserDetail, error)
	ChangeVIP(context.Context, authz.StaffActor, identity.ChangeVIPInput) (identity.ManagedUserDetail, error)
}

// TrafficOverviewService exposes only the authenticated user's final Core
// projection. The transport has no interface for Tracker evidence or policy
// evaluation, so it cannot accidentally recalculate historical settlements.
type TrafficOverviewService interface {
	MyOverview(context.Context, string, int) (traffic.Overview, error)
	MyHNR(context.Context, string, traffic.HNRQuery) (traffic.HNRPage, error)
	SubmitHNRAppeal(context.Context, string, string, traffic.SubmitHNRAppealInput) (traffic.HNRAppeal, error)
	HNRAppeals(context.Context, authz.StaffActor, traffic.HNRAppealQuery) (traffic.HNRAppealPage, error)
	DecideHNRAppeal(context.Context, authz.StaffActor, traffic.DecideHNRAppealInput) (traffic.HNRAppeal, error)
}

type EconomyOverviewService interface {
	MyOverview(context.Context, string, int) (economy.Overview, error)
}

type AttendanceService interface {
	MyOverview(context.Context, string) (attendance.Overview, error)
	Claim(context.Context, string, string, uuid.UUID, attendance.Mode) (attendance.Record, error)
	ListPolicies(context.Context, authz.StaffActor, int, int) (attendance.PolicyPage, error)
	IssuePolicy(context.Context, authz.StaffActor, uuid.UUID, attendance.PolicyRevision, string) (attendance.PublishedPolicy, error)
}

type MemberGiftService interface {
	MyOverview(context.Context, string, int) (membergift.Overview, error)
	Create(context.Context, string, string, uuid.UUID, int64, int64, string) (membergift.Gift, error)
	ListPolicies(context.Context, authz.StaffActor, int, int) (membergift.PolicyPage, error)
	IssuePolicy(context.Context, authz.StaffActor, uuid.UUID, membergift.PolicyRevision, string) (membergift.PublishedPolicy, error)
}

type ContentTipService interface {
	MyOverview(context.Context, string, int) (contenttip.Overview, error)
	Create(context.Context, string, string, uuid.UUID, contenttip.Target, int64) (contenttip.Tip, error)
	ListPolicies(context.Context, authz.StaffActor, int, int) (contenttip.PolicyPage, error)
	IssuePolicy(context.Context, authz.StaffActor, uuid.UUID, contenttip.PolicyRevision, string) (contenttip.PublishedPolicy, error)
}

type WorkgroupService interface {
	MyOverview(context.Context, string) (workgroups.MyOverview, error)
	MyContributionCycles(context.Context, string, workgroups.GroupKind, int) (workgroups.ContributionCyclePage, error)
	MyTasks(context.Context, string, int, int) (workgroups.TaskAssignmentPage, error)
	SubmitTask(context.Context, string, string, uuid.UUID, uuid.UUID, string) (workgroups.TaskAssignment, error)
	Apply(context.Context, string, string, uuid.UUID, workgroups.GroupKind, string) (workgroups.Application, error)
	AdminOverview(context.Context, authz.StaffActor) (workgroups.AdminOverview, error)
	ListApplications(context.Context, authz.StaffActor, workgroups.ApplicationStatus, int, int) (workgroups.ApplicationPage, error)
	DecideApplication(context.Context, authz.StaffActor, uuid.UUID, uuid.UUID, int64, bool, string) (workgroups.Application, error)
	ListMemberships(context.Context, authz.StaffActor, workgroups.GroupKind, workgroups.MembershipStatus, int, int) (workgroups.MembershipPage, error)
	ContributionCycles(context.Context, authz.StaffActor, workgroups.GroupKind, uuid.UUID, int) (workgroups.ContributionCyclePage, error)
	GrantMembership(context.Context, authz.StaffActor, uuid.UUID, workgroups.GroupKind, int64, string) (workgroups.Membership, error)
	ChangeMembership(context.Context, authz.StaffActor, uuid.UUID, uuid.UUID, workgroups.GroupKind, int64, workgroups.MembershipTransition, string) (workgroups.Membership, error)
	ContributionPolicies(context.Context, authz.StaffActor, workgroups.GroupKind, int, int) (workgroups.ContributionPolicyPage, error)
	IssueContributionPolicy(context.Context, authz.StaffActor, uuid.UUID, workgroups.GroupKind, int64, time.Time, string) (workgroups.ContributionPolicy, error)
	IssueContributionReminder(context.Context, authz.StaffActor, uuid.UUID, workgroups.GroupKind, uuid.UUID, time.Time, string) (workgroups.ContributionReminder, error)
	Tasks(context.Context, authz.StaffActor, workgroups.GroupKind, int, int) (workgroups.TaskPage, error)
	PublishTask(context.Context, authz.StaffActor, uuid.UUID, workgroups.GroupKind, workgroups.TaskType, string, string, time.Time, time.Time) (workgroups.Task, error)
	TaskAssignments(context.Context, authz.StaffActor, workgroups.GroupKind, uuid.UUID, int, int) (workgroups.TaskAssignmentPage, error)
	ReviewTaskSubmission(context.Context, authz.StaffActor, uuid.UUID, uuid.UUID, workgroups.TaskReviewDecision, string) (workgroups.TaskAssignment, error)
}

type SeedingRewardAdministrationService interface {
	List(context.Context, authz.StaffActor, int, int) (seedingreward.PolicyPage, error)
	Preview(context.Context, authz.StaffActor, seedingreward.PolicyRevision) (seedingreward.PolicyPreview, error)
	Issue(context.Context, authz.StaffActor, seedingreward.PolicyRevision, string) (seedingreward.PublishedPolicy, error)
}

type LevelPolicyAdministrationService interface {
	Overview(context.Context, authz.StaffActor) (progression.LevelPolicyOverview, error)
	IssueLevelPolicy(context.Context, authz.StaffActor, progression.IssueLevelPolicyInput) (progression.LevelPolicyRevision, error)
}

type ContributionExperiencePolicyService interface {
	List(context.Context, authz.StaffActor, int, int) (progression.ContributionExperiencePolicyPage, error)
	Issue(context.Context, authz.StaffActor, progression.ContributionExperiencePolicyInput, string) (progression.ContributionExperiencePolicy, error)
}

type MedalAdministrationService interface {
	Overview(context.Context, authz.StaffActor) (medals.Overview, error)
	UpdateSettings(context.Context, authz.StaffActor, medals.SettingsInput) (medals.Settings, error)
	Create(context.Context, authz.StaffActor, medals.DefinitionInput) (medals.Definition, error)
	Update(context.Context, authz.StaffActor, int64, int64, medals.DefinitionInput) (medals.Definition, error)
}

type MemberMedalService interface {
	MyOverview(context.Context, string) (medals.MemberOverview, error)
	Purchase(context.Context, string, string, uuid.UUID, int64) (medals.PurchaseReceipt, error)
	SetWearing(context.Context, string, string, int64, int64, bool) (medals.Holding, error)
	MovePriority(context.Context, string, string, int64, int64, medals.PriorityDirection) (medals.Holding, error)
}

type HNRPolicyAdministrationService interface {
	List(context.Context, authz.StaffActor, int, int) (hnradmin.Page, error)
	Preview(context.Context, authz.StaffActor, hnradmin.PolicyInput) (hnradmin.Preview, error)
	Issue(context.Context, authz.StaffActor, hnradmin.IssueInput) (hnradmin.Revision, error)
}

type RatioWatchAdministrationService interface {
	MyStatus(context.Context, string) (ratiowatch.MyStatus, error)
	SubmitAppeal(context.Context, string, string, ratiowatch.SubmitAppealInput) (ratiowatch.Appeal, error)
	Policies(context.Context, authz.StaffActor, int, int) (ratiowatch.PolicyPage, error)
	Preview(context.Context, authz.StaffActor, ratiowatch.PolicyInput) (ratiowatch.ImpactPreview, error)
	Issue(context.Context, authz.StaffActor, ratiowatch.IssueInput) (ratiowatch.PolicyRevision, error)
	Assessments(context.Context, authz.StaffActor, ratiowatch.AssessmentQuery) (ratiowatch.AssessmentPage, error)
	Clear(context.Context, authz.StaffActor, ratiowatch.ClearInput) (ratiowatch.Assessment, error)
	Appeals(context.Context, authz.StaffActor, ratiowatch.AppealQuery) (ratiowatch.AppealPage, error)
	DecideAppeal(context.Context, authz.StaffActor, ratiowatch.DecideAppealInput) (ratiowatch.Appeal, error)
}

type NewcomerAdministrationService interface {
	MyStatus(context.Context, string) (newcomer.MyStatus, error)
	Policies(context.Context, authz.StaffActor, int, int) (newcomer.PolicyPage, error)
	Issue(context.Context, authz.StaffActor, newcomer.IssueInput) (newcomer.PolicyRevision, error)
	Assessments(context.Context, authz.StaffActor, newcomer.AssessmentQuery) (newcomer.AssessmentPage, error)
	Assign(context.Context, authz.StaffActor, newcomer.AssignInput) (newcomer.Assessment, error)
	Exempt(context.Context, authz.StaffActor, newcomer.ExemptInput) (newcomer.Assessment, error)
}

type OperationsService interface {
	Email(context.Context, authz.StaffActor) (operations.EmailOverview, error)
	TestEmail(context.Context, authz.StaffActor, string) (vaultoperations.EmailTestResult, error)
	EconomySettings(context.Context, authz.StaffActor) (operations.EconomySettingsOverview, error)
	SettlementSettings(context.Context, authz.StaffActor) (operations.SettlementSettingsOverview, error)
	TorrentRules(context.Context, authz.StaffActor) (operations.TorrentRulesOverview, error)
	IssueTorrentUploadPolicy(context.Context, authz.StaffActor, torrents.IssueUploadPolicyInput) (torrents.UploadPolicyRevision, error)
	TrackerRuntime(context.Context, authz.StaffActor) (operations.TrackerRuntimeOverview, error)
	TrackerPolicy(context.Context, authz.StaffActor) (trackercontrol.RuntimePolicyRevision, error)
	IssueTrackerPolicy(context.Context, authz.StaffActor, trackercontrol.IssueRuntimePolicyInput) (trackercontrol.RuntimePolicyRevision, error)
	MySeedboxReports(context.Context, uuid.UUID, int, int) (trackercontrol.SeedboxReportPage, error)
	SubmitSeedboxReport(context.Context, uuid.UUID, trackercontrol.SubmitSeedboxReportInput) (trackercontrol.SeedboxReport, error)
	SeedboxReports(context.Context, authz.StaffActor, trackercontrol.SeedboxReportStatus, int, int) (trackercontrol.SeedboxReportPage, error)
	DecideSeedboxReport(context.Context, authz.StaffActor, trackercontrol.DecideSeedboxReportInput) (trackercontrol.SeedboxReport, error)
	Tracker(context.Context, authz.StaffActor) (operations.TrackerOverview, error)
	Workers(context.Context, authz.StaffActor) (operations.WorkerOverview, error)
	Storage(context.Context, authz.StaffActor) (operations.StorageOverview, error)
	VIPProfile(context.Context, authz.StaffActor) (operations.VIPProfileOverview, error)
}

// PromotionAdministrationService is Core's operator control plane. Settlement
// delivery remains asynchronous and its state is included in every campaign.
type PromotionAdministrationService interface {
	List(context.Context, authz.StaffActor, int, int) (promotions.Page, error)
	Schedule(context.Context, authz.StaffActor, promotions.ScheduleInput) (promotions.Campaign, error)
	Offer(context.Context, string, int64) (promotions.ProductOffer, error)
	Purchase(context.Context, string, string, uuid.UUID, int64, promotions.ProductSelection) (promotions.ProductOrder, error)
	MyOrders(context.Context, string, int, int) (promotions.ProductOrderPage, error)
	ProductPolicy(context.Context, authz.StaffActor) (promotions.ProductPolicy, error)
	UpdateProductPolicy(context.Context, authz.StaffActor, promotions.UpdateProductPolicyCommand) (promotions.ProductPolicy, error)
	AdminOrders(context.Context, authz.StaffActor, promotions.ProductOrderQuery) (promotions.ProductOrderPage, error)
}

// TorrentBookmarkService is the private catalog surface for one user's saved
// torrents. Public summaries stay on catalog.Service so bookmark state never
// leaks into anonymous cacheable DTOs.
type TorrentBookmarkService interface {
	List(context.Context, string, int, int) (catalog.TorrentBookmarkPage, error)
	Statuses(context.Context, string, []int64) ([]int64, error)
	Put(context.Context, string, string, int64) (catalog.TorrentBookmarkState, error)
	Delete(context.Context, string, string, int64) error
}

// CommentService is the Core social surface used by the public torrent detail
// page. Ownership checks and capability decisions remain inside the use case;
// the transport only binds cookies, CSRF headers and contract DTOs.
type CommentService interface {
	ListTorrentComments(context.Context, int64, int, int) (social.CommentPage, error)
	ListAnnouncementComments(context.Context, string, int, int) (social.CommentPage, error)
	ListPostComments(context.Context, uuid.UUID, social.CommentThreadSort, int, int) (social.CommentThreadPage, error)
	CreateTorrentComment(context.Context, string, string, social.CreateTorrentCommentInput) (social.Comment, error)
	CreateAnnouncementComment(context.Context, string, string, social.CreateAnnouncementCommentInput) (social.Comment, error)
	CreatePostComment(context.Context, string, string, social.CreatePostCommentInput) (social.Comment, error)
	UpdateMyComment(context.Context, string, string, social.UpdateCommentInput) (social.Comment, error)
	DeleteMyComment(context.Context, string, string, social.DeleteCommentInput) error
}

// SocialPostService owns the member-only dynamic feed. Comments remain a
// separate reusable social aggregate and are injected through CommentService.
type SocialPostService interface {
	List(context.Context, string, social.PostListQuery) (social.PostPage, error)
	FindVisible(context.Context, string, uuid.UUID) (social.Post, error)
	Create(context.Context, string, string, social.CreatePostInput) (social.Post, error)
	UpdateMyPost(context.Context, string, string, social.UpdatePostInput) (social.Post, error)
	DeleteMyPost(context.Context, string, string, social.DeletePostInput) error
	Overview(context.Context, string) (social.CommunityOverview, error)
	UploadMedia(context.Context, string, string, []byte) (social.PostMedia, error)
	ReadMedia(context.Context, string, uuid.UUID) (social.MediaObject, error)
	SetLike(context.Context, string, string, uuid.UUID, bool) (social.InteractionState, error)
	SetRepost(context.Context, string, string, uuid.UUID, bool) (social.InteractionState, error)
	SetFollow(context.Context, string, string, string, bool) (social.FollowState, error)
	Vote(context.Context, string, string, uuid.UUID, uuid.UUID) (social.Poll, error)
	ClaimRedPacket(context.Context, string, string, uuid.UUID, uuid.UUID) (social.RedPacketClaim, error)
	ListNotifications(context.Context, string, social.SocialNotificationQuery) (social.SocialNotificationPage, error)
	NotificationSummary(context.Context, string) (social.SocialNotificationSummary, error)
	MarkNotificationRead(context.Context, string, string, uuid.UUID) (social.SocialNotificationReadReceipt, error)
	MarkAllNotificationsRead(context.Context, string, string) (social.SocialNotificationReadAllReceipt, error)
	ListManagedBoards(context.Context, authz.StaffActor) ([]social.Board, error)
	CreateManagedBoard(context.Context, authz.StaffActor, social.CreateBoardInput) (social.Board, error)
	UpdateManagedBoard(context.Context, authz.StaffActor, social.UpdateBoardInput) (social.Board, error)
	ListManagedPosts(context.Context, authz.StaffActor, social.PostListQuery) (social.PostPage, error)
	ModeratePost(context.Context, authz.StaffActor, social.ModeratePostInput) (social.Post, error)
}

// CommentModerationService keeps the public reporter and staff moderator
// credential audiences distinct. Both methods still re-authorize inside the
// social use case; transport authentication is only an adapter boundary.
type CommentModerationService interface {
	CreateReport(context.Context, string, string, social.CreateCommentReportInput) (social.CommentReportReceipt, error)
	ListOpenCases(context.Context, authz.StaffActor, int, int) (social.CommentModerationCasePage, error)
	Decide(context.Context, authz.StaffActor, social.DecideCommentModerationCaseInput) (social.CommentModerationDecisionResult, error)
}

// NotificationService exposes only the current user's private inbox and its
// monotonic read state. Source creation stays inside the originating business
// transaction and is not an HTTP command.
type NotificationService interface {
	List(context.Context, string, notifications.ListQuery) (notifications.Page, error)
	Summary(context.Context, string) (notifications.Summary, error)
	MarkRead(context.Context, string, string, uuid.UUID) (notifications.ReadReceipt, error)
	MarkAllRead(context.Context, string, string) (notifications.ReadAllReceipt, error)
	ArchiveAll(context.Context, string, string) (notifications.ArchiveAllReceipt, error)
	CreateFeedback(context.Context, string, string, notifications.CreateFeedbackInput) (notifications.FeedbackReceipt, error)
}

// SessionCookieConfig holds deployment-specific cookie attributes.
type SessionCookieConfig struct {
	Name   string
	Path   string
	Secure bool
}

// Handler is the generated strict-server adapter. Domain values are translated
// here so neither OpenAPI DTOs nor HTTP concerns leak into the catalog module.
type Handler struct {
	catalog                     *catalog.Service
	identity                    IdentityService
	registration                RegistrationService
	humanVerification           identity.HumanVerificationVerifier
	invitations                 InvitationService
	emailVerification           EmailVerificationService
	passwordRecovery            PasswordRecoveryService
	sessionSecurity             SessionSecurityService
	twoFactor                   TwoFactorService
	staffIdentity               StaffIdentityService
	staffEnrollment             StaffEnrollmentService
	authorization               AuthorizationService
	grantAdministration         GrantAdministrationService
	categoryAdministration      CategoryAdministrationService
	announcementAdministration  AnnouncementAdministrationService
	wiki                        WikiService
	siteDisplaySettings         SiteDisplaySettingsService
	userAdministration          UserAdministrationService
	notifications               NotificationService
	trafficOverview             TrafficOverviewService
	economyOverview             EconomyOverviewService
	attendance                  AttendanceService
	memberGifts                 MemberGiftService
	contentTips                 ContentTipService
	workgroups                  WorkgroupService
	seedingRewardAdministration SeedingRewardAdministrationService
	levelPolicyAdministration   LevelPolicyAdministrationService
	contributionExperience      ContributionExperiencePolicyService
	medalAdministration         MedalAdministrationService
	memberMedals                MemberMedalService
	hnrPolicyAdministration     HNRPolicyAdministrationService
	ratioWatchAdministration    RatioWatchAdministrationService
	newcomerAdministration      NewcomerAdministrationService
	operations                  OperationsService
	torrentBookmarks            TorrentBookmarkService
	comments                    CommentService
	socialPosts                 SocialPostService
	commentModeration           CommentModerationService
	torrentRead                 TorrentReadService
	torrentUpload               TorrentUploadService
	torrentDownload             TorrentDownloadService
	torrentReview               TorrentReviewService
	torrentResubmission         TorrentResubmissionService
	torrentMaintenance          TorrentMaintenanceService
	promotionAdministration     PromotionAdministrationService
	personalAPIKeys             PersonalAPIKeyService
	moviePilot                  MoviePilotService
	rss                         RSSService
	sessionCookie               SessionCookieConfig
	staffSessionCookie          SessionCookieConfig
}

// NewHandler creates the Core HTTP adapter.
func NewHandler(catalogService *catalog.Service, identityService IdentityService, registrationService RegistrationService, humanVerificationService identity.HumanVerificationVerifier, invitationService InvitationService, emailVerificationService EmailVerificationService, passwordRecoveryService PasswordRecoveryService, sessionSecurityService SessionSecurityService, twoFactorService TwoFactorService, staffIdentityService StaffIdentityService, staffEnrollmentService StaffEnrollmentService, authorizationService AuthorizationService, grantAdministrationService GrantAdministrationService, categoryAdministrationService CategoryAdministrationService, announcementAdministrationService AnnouncementAdministrationService, wikiService WikiService, siteDisplaySettingsService SiteDisplaySettingsService, userAdministrationService UserAdministrationService, notificationService NotificationService, trafficOverviewService TrafficOverviewService, economyOverviewService EconomyOverviewService, attendanceService AttendanceService, memberGiftService MemberGiftService, contentTipService ContentTipService, workgroupService WorkgroupService, seedingRewardAdministrationService SeedingRewardAdministrationService, levelPolicyAdministrationService LevelPolicyAdministrationService, contributionExperiencePolicyService ContributionExperiencePolicyService, medalAdministrationService MedalAdministrationService, memberMedalService MemberMedalService, hnrPolicyAdministrationService HNRPolicyAdministrationService, ratioWatchAdministrationService RatioWatchAdministrationService, newcomerAdministrationService NewcomerAdministrationService, operationsService OperationsService, torrentBookmarkService TorrentBookmarkService, commentService CommentService, socialPostService SocialPostService, commentModerationService CommentModerationService, torrentReadService TorrentReadService, torrentUploadService TorrentUploadService, torrentDownloadService TorrentDownloadService, torrentReviewService TorrentReviewService, torrentResubmissionService TorrentResubmissionService, torrentMaintenanceService TorrentMaintenanceService, promotionAdministrationService PromotionAdministrationService, personalAPIKeyService PersonalAPIKeyService, moviePilotService MoviePilotService, rssService RSSService, sessionCookie, staffSessionCookie SessionCookieConfig) *Handler {
	return &Handler{
		catalog:                     catalogService,
		identity:                    identityService,
		registration:                registrationService,
		humanVerification:           humanVerificationService,
		invitations:                 invitationService,
		emailVerification:           emailVerificationService,
		passwordRecovery:            passwordRecoveryService,
		sessionSecurity:             sessionSecurityService,
		twoFactor:                   twoFactorService,
		staffIdentity:               staffIdentityService,
		staffEnrollment:             staffEnrollmentService,
		authorization:               authorizationService,
		grantAdministration:         grantAdministrationService,
		categoryAdministration:      categoryAdministrationService,
		announcementAdministration:  announcementAdministrationService,
		wiki:                        wikiService,
		siteDisplaySettings:         siteDisplaySettingsService,
		userAdministration:          userAdministrationService,
		notifications:               notificationService,
		trafficOverview:             trafficOverviewService,
		economyOverview:             economyOverviewService,
		attendance:                  attendanceService,
		memberGifts:                 memberGiftService,
		contentTips:                 contentTipService,
		workgroups:                  workgroupService,
		seedingRewardAdministration: seedingRewardAdministrationService,
		levelPolicyAdministration:   levelPolicyAdministrationService,
		contributionExperience:      contributionExperiencePolicyService,
		medalAdministration:         medalAdministrationService,
		memberMedals:                memberMedalService,
		hnrPolicyAdministration:     hnrPolicyAdministrationService,
		ratioWatchAdministration:    ratioWatchAdministrationService,
		newcomerAdministration:      newcomerAdministrationService,
		operations:                  operationsService,
		torrentBookmarks:            torrentBookmarkService,
		comments:                    commentService,
		socialPosts:                 socialPostService,
		commentModeration:           commentModerationService,
		torrentRead:                 torrentReadService,
		torrentUpload:               torrentUploadService,
		torrentDownload:             torrentDownloadService,
		torrentReview:               torrentReviewService,
		torrentResubmission:         torrentResubmissionService,
		torrentMaintenance:          torrentMaintenanceService,
		promotionAdministration:     promotionAdministrationService,
		personalAPIKeys:             personalAPIKeyService,
		moviePilot:                  moviePilotService,
		rss:                         rssService,
		sessionCookie:               sessionCookie,
		staffSessionCookie:          staffSessionCookie,
	}
}

func (h *Handler) verifyHumanIdentityCommand(ctx context.Context, flow identity.HumanVerificationFlow, token string) error {
	policy, err := h.registration.PublicPolicy(ctx)
	if err != nil {
		return errHumanVerificationPolicyUnavailable
	}
	enabled := false
	switch flow {
	case identity.HumanVerificationFlowRegistration:
		enabled = policy.HumanVerificationRegistrationEnabled
	case identity.HumanVerificationFlowLogin:
		enabled = policy.HumanVerificationLoginEnabled
	case identity.HumanVerificationFlowPasswordRecovery:
		enabled = policy.HumanVerificationPasswordRecoveryEnabled
	default:
		return identity.ErrHumanVerificationFailed
	}
	if !enabled {
		return nil
	}
	if policy.HumanVerificationProvider != identity.HumanVerificationProviderTurnstile || h.humanVerification == nil {
		return identity.ErrHumanVerificationUnavailable
	}
	return h.humanVerification.Verify(ctx, flow, token)
}

var errHumanVerificationPolicyUnavailable = errors.New("human verification policy is unavailable")

func humanVerificationProblem(ctx context.Context, err error) (int, generated.Problem) {
	if errors.Is(err, identity.ErrHumanVerificationRequired) {
		status := http.StatusForbidden
		return status, newProblemFromContext(ctx, status, "human_verification_required", "需要完成人机验证", "请完成人机验证后重新提交。")
	}
	if errors.Is(err, identity.ErrHumanVerificationFailed) {
		status := http.StatusForbidden
		return status, newProblemFromContext(ctx, status, "human_verification_failed", "人机验证未通过", "验证可能已经过期或被使用，请刷新验证后重试。")
	}
	status := http.StatusServiceUnavailable
	return status, newProblemFromContext(ctx, status, "human_verification_unavailable", "人机验证暂时不可用", "请稍后重试；当前请求尚未进入账户处理流程。")
}

// GetMyTraffic returns exact final credited/charged values from Core's
// projection. Decimal strings preserve int64 byte precision in TypeScript; the
// response deliberately omits numeric Tracker IDs and evidence digests.
func (h *Handler) GetMyTraffic(ctx context.Context, request generated.GetMyTrafficRequestObject) (generated.GetMyTrafficResponseObject, error) {
	cookieToken := sessionTokenFromContext(ctx)
	if cookieToken == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请登录后查看自己的流量结算记录。")
		return generated.GetMyTraffic401ApplicationProblemPlusJSONResponse(problem), nil
	}
	limit := traffic.DefaultOverviewLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	result, err := h.trafficOverview.MyOverview(ctx, cookieToken, limit)
	switch {
	case errors.Is(err, traffic.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_traffic_query", "流量查询无效", "最近结算条目的返回数量必须在 1 到 50 之间。")
		return generated.GetMyTraffic400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后查看自己的流量结算记录。")
		return generated.GetMyTraffic401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "traffic_read_denied", "无法查看流量账本", "当前账号没有 traffic.read.self 能力。")
		return generated.GetMyTraffic403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GetMyTraffic200JSONResponse{
		Body:    trafficOverviewDTO(result),
		Headers: generated.GetMyTraffic200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

// GetPublicUserProfile exposes only the member-directory projection after a
// valid Web session has established the member audience. The identity use case
// deliberately combines inactive, restricted and unknown targets into 404.
func (h *Handler) GetPublicUserProfile(ctx context.Context, request generated.GetPublicUserProfileRequestObject) (generated.GetPublicUserProfileResponseObject, error) {
	cookieToken := sessionTokenFromContext(ctx)
	if cookieToken == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请登录后查看成员资料。")
		return generated.GetPublicUserProfile401ApplicationProblemPlusJSONResponse(problem), nil
	}
	profile, err := h.identity.PublicProfile(ctx, cookieToken, request.Username)
	switch {
	case errors.Is(err, identity.ErrInvalidInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_username", "用户名无效", "请检查成员资料地址中的用户名。")
		return generated.GetPublicUserProfile400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后查看成员资料。")
		return generated.GetPublicUserProfile401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrPublicUserNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "user_profile_not_found", "没有找到该成员", "成员不存在或资料当前不可访问。")
		return generated.GetPublicUserProfile404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "user_profile_read_denied", "无法查看成员资料", "当前账号没有 user.profile.read.member 能力。")
		return generated.GetPublicUserProfiledefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusForbidden}, nil
	case err != nil:
		return nil, err
	}
	cacheControl := "private, max-age=60"
	return generated.GetPublicUserProfile200JSONResponse{
		Body: publicUserProfileDTO(profile),
		Headers: generated.GetPublicUserProfile200ResponseHeaders{
			CacheControl: &cacheControl,
		},
	}, nil
}

// UpdateMyProfile changes only the bounded public nickname. Authentication,
// CSRF validation and the dedicated self permission remain inside the use
// case; transport maps domain failures without exposing account internals.
func (h *Handler) UpdateMyProfile(ctx context.Context, request generated.UpdateMyProfileRequestObject) (generated.UpdateMyProfileResponseObject, error) {
	cookieToken := sessionTokenFromContext(ctx)
	if cookieToken == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后修改个人资料。")
		return generated.UpdateMyProfile401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_profile", "个人资料无效", "请填写昵称后重试。")
		return generated.UpdateMyProfile400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	user, err := h.identity.UpdateProfile(ctx, cookieToken, request.Params.XCSRFToken, identity.UpdateMyProfileInput{
		DisplayName: request.Body.DisplayName,
	})
	switch {
	case errors.Is(err, identity.ErrInvalidInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_profile", "个人资料无效", "昵称需要包含 1 到 40 个字符。")
		return generated.UpdateMyProfile400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后修改个人资料。")
		return generated.UpdateMyProfile401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.UpdateMyProfile403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "profile_update_denied", "无法修改个人资料", "当前账户没有有效的本人资料修改权限。")
		return generated.UpdateMyProfile403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.UpdateMyProfile200JSONResponse(sessionUserDTO(user)), nil
}

// UpdateMyAvatar keeps binary media outside JSON DTOs. The use case performs
// decoded-image validation plus immutable storage verification before this
// transport returns a cache revision.
func (h *Handler) UpdateMyAvatar(ctx context.Context, request generated.UpdateMyAvatarRequestObject) (generated.UpdateMyAvatarResponseObject, error) {
	cookieToken := sessionTokenFromContext(ctx)
	if cookieToken == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后上传头像。")
		return generated.UpdateMyAvatar401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_avatar", "头像无效", "请选择有效的正方形 JPG 图片。")
		return generated.UpdateMyAvatar400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	revision, err := h.identity.UpdateAvatar(ctx, cookieToken, request.Params.XCSRFToken, request.Body)
	switch {
	case errors.Is(err, identity.ErrAvatarTooLarge):
		problem := newProblemFromContext(ctx, http.StatusRequestEntityTooLarge, "avatar_too_large", "头像文件过大", "处理后的头像不能超过 1MB。")
		return generated.UpdateMyAvatar413ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_avatar", "头像无效", "请上传 32 到 1024 像素的正方形 JPG 图片。")
		return generated.UpdateMyAvatar400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后上传头像。")
		return generated.UpdateMyAvatar401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.UpdateMyAvatar403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "avatar_update_denied", "无法修改头像", "当前账户没有有效的本人资料修改权限。")
		return generated.UpdateMyAvatar403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrAvatarStorageUnavailable), errors.Is(err, identity.ErrAvatarConflict):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "avatar_storage_unavailable", "头像暂时无法保存", "存储校验没有完成，请稍后重试。")
		return generated.UpdateMyAvatardefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	case err != nil:
		return nil, err
	}
	return generated.UpdateMyAvatar200JSONResponse{Revision: revision.Revision, UpdatedAt: revision.UpdatedAt}, nil
}

// GetPublicUserAvatar is member-scoped despite its public-profile purpose. It
// serves only fully verified bytes and never exposes object keys or backends.
func (h *Handler) GetPublicUserAvatar(ctx context.Context, request generated.GetPublicUserAvatarRequestObject) (generated.GetPublicUserAvatarResponseObject, error) {
	cookieToken := sessionTokenFromContext(ctx)
	if cookieToken == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请登录后查看成员头像。")
		return generated.GetPublicUserAvatar401ApplicationProblemPlusJSONResponse(problem), nil
	}
	avatar, err := h.identity.PublicAvatar(ctx, cookieToken, request.Username)
	switch {
	case errors.Is(err, identity.ErrInvalidInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_username", "用户名无效", "请检查成员用户名。")
		return generated.GetPublicUserAvatar400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后查看成员头像。")
		return generated.GetPublicUserAvatar401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "avatar_read_denied", "无法查看成员头像", "当前账号没有查看成员资料的权限。")
		return generated.GetPublicUserAvatar403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrAvatarNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "avatar_not_found", "没有头像", "该成员还没有上传 PeerGo 头像。")
		return generated.GetPublicUserAvatar404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrAvatarStorageUnavailable), errors.Is(err, identity.ErrAvatarConflict):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "avatar_storage_unavailable", "头像暂时无法读取", "存储校验没有完成，请稍后重试。")
		return generated.GetPublicUserAvatardefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	case err != nil:
		return nil, err
	}
	headers := generated.GetPublicUserAvatar200ResponseHeaders{CacheControl: "private, no-cache", ETag: `"` + avatar.Revision + `"`}
	reader := bytes.NewReader(avatar.Data)
	switch avatar.ContentType {
	case "image/jpeg":
		return generated.GetPublicUserAvatar200ImagejpegResponse{Body: reader, Headers: headers, ContentLength: int64(len(avatar.Data))}, nil
	case "image/png":
		return generated.GetPublicUserAvatar200ImagepngResponse{Body: reader, Headers: headers, ContentLength: int64(len(avatar.Data))}, nil
	case "image/webp":
		return generated.GetPublicUserAvatar200ImagewebpResponse{Body: reader, Headers: headers, ContentLength: int64(len(avatar.Data))}, nil
	default:
		return nil, identity.ErrAvatarConflict
	}
}

// ListMyHitAndRuns exposes only the current user's actionable Core projection.
// The cursor contains a versioned keyset position; Tracker evidence, internal
// policy identity and historical PtYes activity cannot be requested here.
func (h *Handler) ListMyHitAndRuns(ctx context.Context, request generated.ListMyHitAndRunsRequestObject) (generated.ListMyHitAndRunsResponseObject, error) {
	cookieToken := sessionTokenFromContext(ctx)
	if cookieToken == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请登录后查看自己的 H&R 记录。")
		return generated.ListMyHitAndRuns401ApplicationProblemPlusJSONResponse(problem), nil
	}
	query := traffic.HNRQuery{Filter: traffic.HNRFilterOpen, Limit: traffic.DefaultHNRLimit}
	if request.Params.Status != nil {
		query.Filter = traffic.HNRFilter(*request.Params.Status)
	}
	if request.Params.Limit != nil {
		query.Limit = *request.Params.Limit
	}
	if request.Params.Cursor != nil {
		cursor, err := traffic.DecodeHNRCursor(*request.Params.Cursor)
		if err != nil {
			problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_hnr_cursor", "分页位置无效", "请从 H&R 页面重新加载列表。")
			return generated.ListMyHitAndRuns400ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
			}, nil
		}
		query.Cursor = cursor
	}
	result, err := h.trafficOverview.MyHNR(ctx, cookieToken, query)
	switch {
	case errors.Is(err, traffic.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_hnr_query", "H&R 查询无效", "请检查状态筛选、每页数量或分页位置。")
		return generated.ListMyHitAndRuns400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后查看自己的 H&R 记录。")
		return generated.ListMyHitAndRuns401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "hnr_read_denied", "无法查看 H&R", "当前账号不能查看这项记录。")
		return generated.ListMyHitAndRuns403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	body, err := hnrPageDTO(result)
	if err != nil {
		return nil, err
	}
	return generated.ListMyHitAndRuns200JSONResponse{
		Body: body, Headers: generated.ListMyHitAndRuns200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

// RequestPasswordRecovery exposes no match signal. Vault performs email lookup,
// cooldown and delivery; the response remains identical for an unknown address.
func (h *Handler) RequestPasswordRecovery(ctx context.Context, request generated.RequestPasswordRecoveryRequestObject) (generated.RequestPasswordRecoveryResponseObject, error) {
	if request.Body == nil || request.Body.Email == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_password_recovery", "邮箱地址无效", "请输入注册时使用的有效邮箱地址。")
		return generated.RequestPasswordRecovery400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	humanVerificationToken := ""
	if request.Body.HumanVerificationToken != nil {
		humanVerificationToken = *request.Body.HumanVerificationToken
	}
	if err := h.verifyHumanIdentityCommand(ctx, identity.HumanVerificationFlowPasswordRecovery, humanVerificationToken); err != nil {
		status, problem := humanVerificationProblem(ctx, err)
		return generated.RequestPasswordRecoverydefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: status}, nil
	}
	result, err := h.passwordRecovery.Request(ctx, string(*request.Body.Email))
	switch {
	case errors.Is(err, identity.ErrInvalidInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_password_recovery", "邮箱地址无效", "请输入注册时使用的有效邮箱地址。")
		return generated.RequestPasswordRecovery400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrPasswordRecoveryServiceUnavailable):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "password_recovery_unavailable", "暂时无法处理找回请求", "请稍后重试；响应不会透露邮箱是否已注册。")
		return generated.RequestPasswordRecoverydefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	case err != nil:
		return nil, err
	}
	return generated.RequestPasswordRecovery202JSONResponse{
		AcceptedAt: result.AcceptedAt, NextRequestAt: result.NextRequestAt,
	}, nil
}

// ConfirmPasswordRecovery accepts only the action token and replacement
// password. It returns no user projection and never creates a login session.
func (h *Handler) ConfirmPasswordRecovery(ctx context.Context, request generated.ConfirmPasswordRecoveryRequestObject) (generated.ConfirmPasswordRecoveryResponseObject, error) {
	if request.Body == nil || request.Body.Token == nil || request.Body.NewPassword == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_password_recovery", "重置内容无效", "请从最新邮件打开完整链接，并设置至少 12 位的新密码。")
		return generated.ConfirmPasswordRecovery400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	result, err := h.passwordRecovery.Confirm(ctx, *request.Body.Token, *request.Body.NewPassword)
	switch {
	case errors.Is(err, identity.ErrInvalidInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_password_recovery", "重置内容无效", "请从最新邮件打开完整链接，并设置至少 12 位的新密码。")
		return generated.ConfirmPasswordRecovery400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrPasswordRecoveryTokenInvalid):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "password_recovery_unavailable", "重置链接不可用", "链接可能已过期、已使用或已被新的找回邮件替换。")
		return generated.ConfirmPasswordRecovery404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrPasswordRecoveryServiceUnavailable), errors.Is(err, identity.ErrPasswordRecoveryStateConflict):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "password_recovery_unavailable", "暂时无法完成密码重置", "请保留当前页面并稍后重试。")
		return generated.ConfirmPasswordRecoverydefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	case err != nil:
		return nil, err
	}
	return generated.ConfirmPasswordRecovery200JSONResponse{
		CompletedAt: result.PasswordChangedAt, AlreadyCompleted: !result.Changed,
	}, nil
}

// RequestEmailVerification requires the ordinary session and CSRF token. The
// success DTO deliberately says only that the request was processed; address
// matching remains a Vault-owned, enumeration-safe decision.
func (h *Handler) RequestEmailVerification(ctx context.Context, request generated.RequestEmailVerificationRequestObject) (generated.RequestEmailVerificationResponseObject, error) {
	if request.Body == nil || request.Body.Email == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_email_verification", "邮箱地址无效", "请重新输入当前账户保存的邮箱地址。")
		return generated.RequestEmailVerification400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	result, err := h.emailVerification.Request(
		ctx,
		sessionTokenFromContext(ctx),
		request.Params.XCSRFToken,
		string(*request.Body.Email),
	)
	var cooldown *identity.EmailVerificationCooldownError
	switch {
	case errors.Is(err, identity.ErrInvalidInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_email_verification", "邮箱地址无效", "请重新输入当前账户保存的邮箱地址。")
		return generated.RequestEmailVerification400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "web_session_required", "需要登录", "请先使用用户名登录后再请求验证邮件。")
		return generated.RequestEmailVerification401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "无法确认当前页面", "请刷新页面后重新提交。")
		return generated.RequestEmailVerification403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.As(err, &cooldown):
		problem := newProblemFromContext(ctx, http.StatusTooManyRequests, "email_verification_cooldown", "请稍后再发送", "验证邮件存在两分钟重发冷却，请稍后重试。")
		return generated.RequestEmailVerification429ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrEmailVerificationDeliveryUnavailable), errors.Is(err, identity.ErrEmailVerificationServiceUnavailable):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "email_verification_unavailable", "暂时无法发送验证邮件", "请稍后重试；已经创建的账户不受影响。")
		return generated.RequestEmailVerificationdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	case err != nil:
		return nil, err
	}
	return generated.RequestEmailVerification202JSONResponse{
		AcceptedAt:      result.AcceptedAt,
		NextRequestAt:   result.NextRequestAt,
		AlreadyVerified: result.AlreadyVerified,
	}, nil
}

// ConfirmEmailVerification accepts only the single-purpose token. It never
// requires or upgrades a browser session, so link handling cannot become an
// ambient-login mechanism.
func (h *Handler) ConfirmEmailVerification(ctx context.Context, request generated.ConfirmEmailVerificationRequestObject) (generated.ConfirmEmailVerificationResponseObject, error) {
	if request.Body == nil || request.Body.Token == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_email_verification", "验证链接无效", "请从最新一封验证邮件重新打开链接。")
		return generated.ConfirmEmailVerification400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	result, err := h.emailVerification.Confirm(ctx, *request.Body.Token)
	switch {
	case errors.Is(err, identity.ErrInvalidInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_email_verification", "验证链接无效", "请从最新一封验证邮件重新打开链接。")
		return generated.ConfirmEmailVerification400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrEmailVerificationTokenInvalid):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "email_verification_unavailable", "验证链接不可用", "链接可能已过期或已被新的验证邮件替换。")
		return generated.ConfirmEmailVerification404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrEmailVerificationServiceUnavailable), errors.Is(err, identity.ErrEmailVerificationStateConflict):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "email_verification_unavailable", "暂时无法完成验证", "请保留当前页面并稍后重试。")
		return generated.ConfirmEmailVerificationdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	case err != nil:
		return nil, err
	}
	return generated.ConfirmEmailVerification200JSONResponse{
		User:            sessionUserDTO(result.User),
		VerifiedAt:      result.VerifiedAt,
		AlreadyVerified: !result.Changed,
	}, nil
}

// ListManagedUsers returns the operational account projection after both the
// independent staff session and user.account.read have been checked. Core asks
// Privacy Vault for the full email only after this authorization succeeds.
func (h *Handler) ListManagedUsers(ctx context.Context, request generated.ListManagedUsersRequestObject) (generated.ListManagedUsersResponseObject, error) {
	session, authenticationProblem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.ListManagedUsers401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.ListManagedUsers403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	input := identity.ListManagedUsersInput{}
	if request.Params.Query != nil {
		input.Query = *request.Params.Query
	}
	if request.Params.Filter != nil {
		input.Filter = identity.ManagedUserDirectoryFilter(*request.Params.Filter)
	}
	if request.Params.Page != nil {
		input.Page = *request.Params.Page
	}
	if request.Params.PageSize != nil {
		input.PageSize = *request.Params.PageSize
	}
	result, err := h.userAdministration.List(ctx, staffActor(session), input)
	if errors.Is(err, identity.ErrUserAdministrationInput) {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_user_query", "账户查询无效", "请检查搜索词、状态和分页参数。")
		return generated.ListManagedUsers400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "user_account_read_denied", "无法查看账户", "当前后台身份没有 user.account.read。")
		return generated.ListManagedUsers403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]generated.ManagedUserSummary, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, managedUserSummaryDTO(item))
	}
	return generated.ListManagedUsers200JSONResponse{
		Items: items, Total: result.Total, Page: result.Page, PageSize: result.PageSize,
		Summary: generated.ManagedUserDirectorySummary{
			Total: result.Summary.Total, Active: result.Summary.Active,
			Banned: result.Summary.Banned, Vip: result.Summary.VIP,
			DownloadRestricted: result.Summary.DownloadRestricted,
			Unverified:         result.Summary.Unverified,
		},
	}, nil
}

// GetManagedUser uses a UUID path key and does not accept arbitrary selectors
// that could become a side channel for private identifiers.
func (h *Handler) GetManagedUser(ctx context.Context, request generated.GetManagedUserRequestObject) (generated.GetManagedUserResponseObject, error) {
	session, authenticationProblem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.GetManagedUser401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.GetManagedUser403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.userAdministration.Get(ctx, staffActor(session), request.UserId)
	if errors.Is(err, identity.ErrUserAdministrationInput) {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_user_id", "账户标识无效", "请使用有效的账户 UUID。")
		return generated.GetManagedUser400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "user_account_read_denied", "无法查看账户", "当前后台身份没有 user.account.read。")
		return generated.GetManagedUser403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrManagedUserNotFound) {
		problem := newProblemFromContext(ctx, http.StatusNotFound, "managed_user_not_found", "账户不存在", "目标账户不存在或已被移除。")
		return generated.GetManagedUser404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.GetManagedUser200JSONResponse(managedUserDetailDTO(result)), nil
}

func (h *Handler) AdjustManagedUserData(ctx context.Context, request generated.AdjustManagedUserDataRequestObject) (generated.AdjustManagedUserDataResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_user_adjustment", "用户数据变更无效", "请检查字段、方向、数值和账户版本。")
		return generated.AdjustManagedUserData400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.AdjustManagedUserData401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.AdjustManagedUserData403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	reason := ""
	if request.Body.Reason != nil {
		reason = *request.Body.Reason
	}
	result, err := h.userAdministration.Adjust(ctx, staffActor(session), identity.ManagedUserAdjustmentInput{
		AdjustmentID:        request.Params.IdempotencyKey,
		UserID:              request.UserId,
		Field:               identity.ManagedUserAdjustmentField(request.Body.Field),
		Operation:           identity.ManagedUserAdjustmentOperation(request.Body.Operation),
		Amount:              request.Body.Amount,
		Reason:              reason,
		ExpectedUserVersion: request.Body.ExpectedUserVersion,
	})
	switch {
	case errors.Is(err, identity.ErrUserAdministrationInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_user_adjustment", "用户数据变更无效", "流量、魔力值和邀请必须是整数；捐赠最多两位小数，经验最多二十位小数。")
		return generated.AdjustManagedUserData400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, authz.ErrForbidden), errors.Is(err, identity.ErrAccountRestrictionSelfTarget):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "user_adjustment_denied", "无法变更用户数据", "当前后台身份没有 user.account.adjust，或不能调整自己的数据。")
		return generated.AdjustManagedUserData403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrManagedUserNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "managed_user_not_found", "账户不存在", "目标账户不存在或已被移除。")
		return generated.AdjustManagedUserData404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrManagedUserVersionConflict),
		errors.Is(err, identity.ErrManagedUserAdjustmentConflict),
		errors.Is(err, identity.ErrManagedUserAdjustmentInsufficient):
		problem := newProblemFromContext(ctx, http.StatusConflict, "user_adjustment_conflict", "用户数据已经变化", "余额不足、数值超出范围或账户版本已更新；请刷新详情后重试。")
		return generated.AdjustManagedUserData409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrManagedUserDataUnavailable):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "user_adjustment_unavailable", "用户数据管理暂时不可用", "请稍后重试；本次变更没有提交。")
		return generated.AdjustManagedUserDatadefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	case err != nil:
		return nil, err
	}
	return generated.AdjustManagedUserData200JSONResponse(managedUserDetailDTO(result)), nil
}

func (h *Handler) GetManagedUserNetworkHistory(ctx context.Context, request generated.GetManagedUserNetworkHistoryRequestObject) (generated.GetManagedUserNetworkHistoryResponseObject, error) {
	session, authenticationProblem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.GetManagedUserNetworkHistory401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.GetManagedUserNetworkHistory403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.userAdministration.NetworkHistory(ctx, staffActor(session), request.UserId)
	switch {
	case errors.Is(err, identity.ErrUserAdministrationInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_user_id", "账户标识无效", "请使用有效的账户 UUID。")
		return generated.GetManagedUserNetworkHistory400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "user_network_read_denied", "无法查看 IP 历史", "当前后台身份没有 user.network.read。")
		return generated.GetManagedUserNetworkHistory403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrManagedUserNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "managed_user_not_found", "账户不存在", "目标账户不存在或已被移除。")
		return generated.GetManagedUserNetworkHistory404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrManagedUserDataUnavailable):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "user_network_history_unavailable", "IP 历史暂时不可用", "请稍后重试；账户登录与其他管理功能不受影响。")
		return generated.GetManagedUserNetworkHistorydefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.ManagedUserNetworkObservation, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, generated.ManagedUserNetworkObservation{
			Address:          item.Address,
			FirstSeenAt:      item.FirstSeenAt,
			LastSeenAt:       item.LastSeenAt,
			SeenCount:        item.SeenCount,
			RelatedUserCount: item.RelatedUserCount,
		})
	}
	return generated.GetManagedUserNetworkHistory200JSONResponse{
		Items: items, RetentionDays: result.RetentionDays, MaximumItems: result.MaximumItems,
	}, nil
}

// CreateManagedUserAccountRestriction requires the staff CSRF token before the
// owning use case performs typed authorization. Its expected user version is
// checked under row lock; a successful command also invalidates live sessions.
func (h *Handler) CreateManagedUserAccountRestriction(ctx context.Context, request generated.CreateManagedUserAccountRestrictionRequestObject) (generated.CreateManagedUserAccountRestrictionResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_account_restriction", "账户访问限制无效", "请检查处置类别、时长、账户版本和人工理由。")
		return generated.CreateManagedUserAccountRestriction400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.CreateManagedUserAccountRestriction401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.CreateManagedUserAccountRestriction403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.userAdministration.CreateRestriction(ctx, staffActor(session), identity.CreateAccountRestrictionInput{
		UserID: request.UserId, ReasonCode: identity.AccountRestrictionReasonCode(request.Body.ReasonCode),
		Reason: request.Body.Reason, DurationHours: request.Body.DurationHours,
		ExpectedUserVersion: request.Body.ExpectedUserVersion,
	})
	if errors.Is(err, identity.ErrUserAdministrationInput) {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_account_restriction", "账户访问限制无效", "请检查处置类别、时长、账户版本和人工理由。")
		return generated.CreateManagedUserAccountRestriction400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "user_account_restrict_denied", "无法限制账户访问", "当前后台身份没有 user.account.restrict。")
		return generated.CreateManagedUserAccountRestriction403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrManagedUserNotFound) {
		problem := newProblemFromContext(ctx, http.StatusNotFound, "managed_user_not_found", "账户不存在", "目标账户不存在或已被移除。")
		return generated.CreateManagedUserAccountRestriction404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if isAccountRestrictionConflict(err) {
		code, title, detail := accountRestrictionConflict(err)
		problem := newProblemFromContext(ctx, http.StatusConflict, code, title, detail)
		return generated.CreateManagedUserAccountRestriction409ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.CreateManagedUserAccountRestriction201JSONResponse(managedUserDetailDTO(result)), nil
}

// RevokeManagedUserAccountRestriction never treats expiry as an implicit
// mutation. Only the specified current restriction and observed versions can
// produce an explicit, audited revocation record.
func (h *Handler) RevokeManagedUserAccountRestriction(ctx context.Context, request generated.RevokeManagedUserAccountRestrictionRequestObject) (generated.RevokeManagedUserAccountRestrictionResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_account_restriction_revocation", "撤销理由无效", "请检查撤销类别、账户版本、限制版本和人工理由。")
		return generated.RevokeManagedUserAccountRestriction400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.RevokeManagedUserAccountRestriction401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.RevokeManagedUserAccountRestriction403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.userAdministration.RevokeRestriction(ctx, staffActor(session), identity.RevokeAccountRestrictionInput{
		UserID: request.UserId, RestrictionID: request.RestrictionId,
		ReasonCode: identity.AccountRestrictionRevocationReasonCode(request.Body.ReasonCode),
		Reason:     request.Body.Reason, ExpectedUserVersion: request.Body.ExpectedUserVersion,
		ExpectedRestrictionVersion: request.Body.ExpectedRestrictionVersion,
	})
	if errors.Is(err, identity.ErrUserAdministrationInput) {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_account_restriction_revocation", "撤销理由无效", "请检查撤销类别、账户版本、限制版本和人工理由。")
		return generated.RevokeManagedUserAccountRestriction400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "user_account_restriction_revoke_denied", "无法撤销账户限制", "当前后台身份没有 user.account.restriction.revoke。")
		return generated.RevokeManagedUserAccountRestriction403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrManagedUserNotFound) {
		problem := newProblemFromContext(ctx, http.StatusNotFound, "managed_user_not_found", "账户不存在", "目标账户不存在或已被移除。")
		return generated.RevokeManagedUserAccountRestriction404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if isAccountRestrictionConflict(err) {
		code, title, detail := accountRestrictionConflict(err)
		problem := newProblemFromContext(ctx, http.StatusConflict, code, title, detail)
		return generated.RevokeManagedUserAccountRestriction409ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.RevokeManagedUserAccountRestriction200JSONResponse(managedUserDetailDTO(result)), nil
}

func (h *Handler) ReactivateManagedUser(ctx context.Context, request generated.ReactivateManagedUserRequestObject) (generated.ReactivateManagedUserResponseObject, error) {
	if request.Body == nil {
		return managedUserReactivationBadRequest(ctx), nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.ReactivateManagedUser401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.ReactivateManagedUser403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	reason := ""
	if request.Body.Reason != nil {
		reason = *request.Body.Reason
	}
	result, err := h.userAdministration.Reactivate(ctx, staffActor(session), identity.ReactivateManagedUserInput{
		ReactivationID: request.Params.IdempotencyKey, UserID: request.UserId,
		Reason: reason, ExpectedUserVersion: request.Body.ExpectedUserVersion,
	})
	switch {
	case errors.Is(err, identity.ErrUserAdministrationInput):
		return managedUserReactivationBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden), errors.Is(err, identity.ErrAccountRestrictionSelfTarget):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "user_reactivation_denied", "无法解除账户封禁", "当前后台身份无权解封该账户。")
		return generated.ReactivateManagedUser403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrManagedUserNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "managed_user_not_found", "账户不存在", "目标账户不存在或已被移除。")
		return generated.ReactivateManagedUser404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrManagedUserNotDisabled), errors.Is(err, identity.ErrManagedUserVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "managed_user_reactivation_conflict", "账户状态已变化", "该账户已不是封禁状态，或账户版本已经变化；请刷新详情后重试。")
		return generated.ReactivateManagedUser409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrManagedUserCredentialUnavailable):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "managed_user_credential_unavailable", "暂时无法恢复登录凭据", "隐私凭据服务暂时不可用，账户仍保持封禁；请稍后重试。")
		return generated.ReactivateManagedUserdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	case err != nil:
		return nil, err
	}
	return generated.ReactivateManagedUser200JSONResponse(managedUserDetailDTO(result)), nil
}

func (h *Handler) CreateManagedUserManualDownloadRestriction(ctx context.Context, request generated.CreateManagedUserManualDownloadRestrictionRequestObject) (generated.CreateManagedUserManualDownloadRestrictionResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_manual_download_restriction", "人工下载限制无效", "请检查处置类别、账户版本、限制版本和人工理由。")
		return generated.CreateManagedUserManualDownloadRestriction400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.CreateManagedUserManualDownloadRestriction401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.CreateManagedUserManualDownloadRestriction403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.userAdministration.CreateManualDownloadRestriction(ctx, staffActor(session), identity.CreateManualDownloadRestrictionInput{
		UserID:     request.UserId,
		ReasonCode: identity.ManualDownloadRestrictionReasonCode(request.Body.ReasonCode),
		Reason:     request.Body.Reason, ExpectedUserVersion: request.Body.ExpectedUserVersion,
		ExpectedStateVersion: request.Body.ExpectedStateVersion,
	})
	if errors.Is(err, identity.ErrUserAdministrationInput) {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_manual_download_restriction", "人工下载限制无效", "请检查处置类别、账户版本、限制版本和人工理由。")
		return generated.CreateManagedUserManualDownloadRestriction400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "manual_download_restriction_denied", "无法签发下载限制", "当前后台身份没有 user.downloadrestriction.restrict。")
		return generated.CreateManagedUserManualDownloadRestriction403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrManagedUserNotFound) {
		problem := newProblemFromContext(ctx, http.StatusNotFound, "managed_user_not_found", "账户不存在", "目标账户不存在或已被移除。")
		return generated.CreateManagedUserManualDownloadRestriction404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if isManualDownloadRestrictionConflict(err) {
		code, title, detail := manualDownloadRestrictionConflict(err)
		problem := newProblemFromContext(ctx, http.StatusConflict, code, title, detail)
		return generated.CreateManagedUserManualDownloadRestriction409ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.CreateManagedUserManualDownloadRestriction201JSONResponse(managedUserDetailDTO(result)), nil
}

func (h *Handler) UpdateManagedUserManualDownloadRestriction(ctx context.Context, request generated.UpdateManagedUserManualDownloadRestrictionRequestObject) (generated.UpdateManagedUserManualDownloadRestrictionResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_manual_download_restriction", "人工下载限制无效", "请检查处置类别、账户版本、限制版本和人工理由。")
		return generated.UpdateManagedUserManualDownloadRestriction400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.UpdateManagedUserManualDownloadRestriction401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.UpdateManagedUserManualDownloadRestriction403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.userAdministration.UpdateManualDownloadRestriction(ctx, staffActor(session), identity.UpdateManualDownloadRestrictionInput{
		UserID:     request.UserId,
		ReasonCode: identity.ManualDownloadRestrictionReasonCode(request.Body.ReasonCode),
		Reason:     request.Body.Reason, ExpectedUserVersion: request.Body.ExpectedUserVersion,
		ExpectedStateVersion: request.Body.ExpectedStateVersion,
	})
	if errors.Is(err, identity.ErrUserAdministrationInput) {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_manual_download_restriction", "人工下载限制无效", "请检查处置类别、账户版本、限制版本和人工理由。")
		return generated.UpdateManagedUserManualDownloadRestriction400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "manual_download_restriction_denied", "无法修改下载限制", "当前后台身份没有 user.downloadrestriction.restrict。")
		return generated.UpdateManagedUserManualDownloadRestriction403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrManagedUserNotFound) {
		problem := newProblemFromContext(ctx, http.StatusNotFound, "managed_user_not_found", "账户不存在", "目标账户不存在或已被移除。")
		return generated.UpdateManagedUserManualDownloadRestriction404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if isManualDownloadRestrictionConflict(err) {
		code, title, detail := manualDownloadRestrictionConflict(err)
		problem := newProblemFromContext(ctx, http.StatusConflict, code, title, detail)
		return generated.UpdateManagedUserManualDownloadRestriction409ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.UpdateManagedUserManualDownloadRestriction200JSONResponse(managedUserDetailDTO(result)), nil
}

func (h *Handler) RevokeManagedUserManualDownloadRestriction(ctx context.Context, request generated.RevokeManagedUserManualDownloadRestrictionRequestObject) (generated.RevokeManagedUserManualDownloadRestrictionResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_manual_download_restriction_revocation", "解除理由无效", "请检查解除类别、账户版本、限制版本和人工理由。")
		return generated.RevokeManagedUserManualDownloadRestriction400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.RevokeManagedUserManualDownloadRestriction401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.RevokeManagedUserManualDownloadRestriction403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.userAdministration.RevokeManualDownloadRestriction(ctx, staffActor(session), identity.RevokeManualDownloadRestrictionInput{
		UserID:     request.UserId,
		ReasonCode: identity.ManualDownloadRestrictionRevocationReasonCode(request.Body.ReasonCode),
		Reason:     request.Body.Reason, ExpectedUserVersion: request.Body.ExpectedUserVersion,
		ExpectedStateVersion: request.Body.ExpectedStateVersion,
	})
	if errors.Is(err, identity.ErrUserAdministrationInput) {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_manual_download_restriction_revocation", "解除理由无效", "请检查解除类别、账户版本、限制版本和人工理由。")
		return generated.RevokeManagedUserManualDownloadRestriction400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "manual_download_restriction_revoke_denied", "无法解除下载限制", "当前后台身份没有 user.downloadrestriction.revoke。")
		return generated.RevokeManagedUserManualDownloadRestriction403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrManagedUserNotFound) {
		problem := newProblemFromContext(ctx, http.StatusNotFound, "managed_user_not_found", "账户不存在", "目标账户不存在或已被移除。")
		return generated.RevokeManagedUserManualDownloadRestriction404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if isManualDownloadRestrictionConflict(err) {
		code, title, detail := manualDownloadRestrictionConflict(err)
		problem := newProblemFromContext(ctx, http.StatusConflict, code, title, detail)
		return generated.RevokeManagedUserManualDownloadRestriction409ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.RevokeManagedUserManualDownloadRestriction200JSONResponse(managedUserDetailDTO(result)), nil
}

func (h *Handler) ChangeManagedUserVIP(ctx context.Context, request generated.ChangeManagedUserVIPRequestObject) (generated.ChangeManagedUserVIPResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_vip_change", "VIP 变更无效", "请检查期限、账户版本和状态版本。")
		return generated.ChangeManagedUserVIP400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.ChangeManagedUserVIP401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.ChangeManagedUserVIP403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.userAdministration.ChangeVIP(ctx, staffActor(session), identity.ChangeVIPInput{
		UserID: request.UserId, Enabled: request.Body.Enabled,
		DurationDays: request.Body.DurationDays, Reason: request.Body.Reason,
		ExpectedUserVersion:  request.Body.ExpectedUserVersion,
		ExpectedStateVersion: request.Body.ExpectedStateVersion,
	})
	if errors.Is(err, identity.ErrUserAdministrationInput) {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_vip_change", "VIP 变更无效", "限期 VIP 为 1–3650 天；永久 VIP 不填写期限。")
		return generated.ChangeManagedUserVIP400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "vip_manage_denied", "无法变更 VIP", "当前后台身份没有 user.vip.manage。")
		return generated.ChangeManagedUserVIP403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrManagedUserNotFound) {
		problem := newProblemFromContext(ctx, http.StatusNotFound, "managed_user_not_found", "账户不存在", "目标账户不存在或已被移除。")
		return generated.ChangeManagedUserVIP404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if isVIPChangeConflict(err) {
		code, title, detail := vipChangeConflict(err)
		problem := newProblemFromContext(ctx, http.StatusConflict, code, title, detail)
		return generated.ChangeManagedUserVIP409ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.ChangeManagedUserVIP200JSONResponse(managedUserDetailDTO(result)), nil
}

func isVIPChangeConflict(err error) bool {
	return errors.Is(err, identity.ErrManagedUserNotActive) ||
		errors.Is(err, identity.ErrManagedUserVersionConflict) ||
		errors.Is(err, identity.ErrVIPStateConflict) ||
		errors.Is(err, identity.ErrVIPAlreadyActive) ||
		errors.Is(err, identity.ErrVIPNotActive) ||
		errors.Is(err, identity.ErrAccountRestrictionSelfTarget)
}

func vipChangeConflict(err error) (string, string, string) {
	switch {
	case errors.Is(err, identity.ErrManagedUserNotActive):
		return "managed_user_not_active", "账户当前不可签发", "只有正常账户可以签发或续期 VIP；已停用账户仍可撤销。"
	case errors.Is(err, identity.ErrManagedUserVersionConflict):
		return "managed_user_version_conflict", "账户状态已变化", "请刷新账户详情后重新审阅。"
	case errors.Is(err, identity.ErrVIPStateConflict):
		return "vip_state_version_conflict", "VIP 状态已变化", "请刷新账户详情，并基于最新状态版本重新提交。"
	case errors.Is(err, identity.ErrVIPAlreadyActive):
		return "vip_already_active", "VIP 无需重复签发", "当前账户已经具有相同的永久 VIP 身份。"
	case errors.Is(err, identity.ErrVIPNotActive):
		return "vip_not_active", "没有可撤销的 VIP", "目标账户当前没有 VIP 授予记录。"
	default:
		return "vip_self_target", "不能变更自己的 VIP", "请由另一名具备对应职责的员工执行该操作。"
	}
}

func isManualDownloadRestrictionConflict(err error) bool {
	return errors.Is(err, identity.ErrManagedUserNotActive) ||
		errors.Is(err, identity.ErrManagedUserVersionConflict) ||
		errors.Is(err, identity.ErrManualDownloadRestrictionActive) ||
		errors.Is(err, identity.ErrManualDownloadRestrictionInactive) ||
		errors.Is(err, identity.ErrManualDownloadRestrictionConflict) ||
		errors.Is(err, identity.ErrAccountRestrictionSelfTarget)
}

func manualDownloadRestrictionConflict(err error) (string, string, string) {
	switch {
	case errors.Is(err, identity.ErrManagedUserNotActive):
		return "managed_user_not_active", "账户当前不可限制", "只有正常账户可以新增或修改人工下载限制。"
	case errors.Is(err, identity.ErrManagedUserVersionConflict):
		return "managed_user_version_conflict", "账户状态已变化", "请刷新账户详情，并基于最新账户版本重新审阅。"
	case errors.Is(err, identity.ErrManualDownloadRestrictionActive):
		return "manual_download_restriction_active", "人工下载限制已经生效", "请使用修改操作，或刷新账户详情。"
	case errors.Is(err, identity.ErrManualDownloadRestrictionInactive):
		return "manual_download_restriction_inactive", "当前没有人工下载限制", "目标限制可能已被解除，请刷新账户详情。"
	case errors.Is(err, identity.ErrManualDownloadRestrictionConflict):
		return "manual_download_restriction_version_conflict", "下载限制状态已变化", "请刷新账户详情，并基于最新限制版本重新审阅。"
	default:
		return "manual_download_restriction_self_target", "不能处置自己的下载权限", "请由另一名具备对应职责的员工执行该操作。"
	}
}

func isAccountRestrictionConflict(err error) bool {
	return errors.Is(err, identity.ErrManagedUserNotActive) ||
		errors.Is(err, identity.ErrManagedUserVersionConflict) ||
		errors.Is(err, identity.ErrAccountRestrictionAlreadyActive) ||
		errors.Is(err, identity.ErrAccountRestrictionNotActive) ||
		errors.Is(err, identity.ErrAccountRestrictionVersionConflict) ||
		errors.Is(err, identity.ErrAccountRestrictionSelfTarget)
}

func managedUserReactivationBadRequest(ctx context.Context) generated.ReactivateManagedUser400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_managed_user_reactivation", "解封参数无效", "请刷新账户版本后重试；原因可留空，由系统自动记录。")
	return generated.ReactivateManagedUser400ApplicationProblemPlusJSONResponse{
		ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
	}
}

func accountRestrictionConflict(err error) (string, string, string) {
	switch {
	case errors.Is(err, identity.ErrManagedUserNotActive):
		return "managed_user_not_active", "账户当前不可限制", "只有 active 账户可以新增临时访问限制；账户状态与限制必须分开处理。"
	case errors.Is(err, identity.ErrManagedUserVersionConflict):
		return "managed_user_version_conflict", "账户状态已变化", "请刷新账户详情，并基于最新账户版本重新审阅。"
	case errors.Is(err, identity.ErrAccountRestrictionAlreadyActive):
		return "account_restriction_already_active", "账户已有重叠限制", "同一账户不能存在重叠的账户访问限制，请先查看或撤销当前记录。"
	case errors.Is(err, identity.ErrAccountRestrictionNotActive):
		return "account_restriction_not_active", "限制已不再有效", "目标限制可能已撤销或到期，请刷新账户详情。"
	case errors.Is(err, identity.ErrAccountRestrictionVersionConflict):
		return "account_restriction_version_conflict", "限制状态已变化", "请刷新账户详情，并基于最新限制版本重新审阅。"
	default:
		return "account_restriction_self_target", "不能处置自己的账户", "请由另一名具备对应职责的员工执行该操作。"
	}
}

// GetSiteDisplaySettings authenticates the independent staff audience before
// the owning use case evaluates the exact read capability.
func (h *Handler) GetSiteDisplaySettings(ctx context.Context, _ generated.GetSiteDisplaySettingsRequestObject) (generated.GetSiteDisplaySettingsResponseObject, error) {
	session, authenticationProblem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.GetSiteDisplaySettings401ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*authenticationProblem),
			}, nil
		}
		return generated.GetSiteDisplaySettings403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.siteDisplaySettings.Get(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "site_display_manage_read_denied", "无法查看站点与展示设置", "当前后台身份没有 site.display.manage.read。")
		return generated.GetSiteDisplaySettings403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.GetSiteDisplaySettings200JSONResponse(siteDisplaySettingsDTO(result)), nil
}

// UpdateSiteDisplaySettings uses the version observed by the editor. The
// repository locks that singleton and commits its outbox evidence atomically.
func (h *Handler) UpdateSiteDisplaySettings(ctx context.Context, request generated.UpdateSiteDisplaySettingsRequestObject) (generated.UpdateSiteDisplaySettingsResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_site_display_settings", "站点与展示设置无效", "请检查名称、说明、种子文件名前缀、自定义菜单、默认视图和版本。")
		return generated.UpdateSiteDisplaySettings400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.UpdateSiteDisplaySettings401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.UpdateSiteDisplaySettings403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.siteDisplaySettings.Update(ctx, staffActor(session), catalog.UpdateSiteDisplaySettingsInput{
		Name: request.Body.Name, Description: request.Body.Description,
		TorrentFilenamePrefix:  request.Body.TorrentFilenamePrefix,
		DefaultTorrentView:     catalog.TorrentView(request.Body.DefaultTorrentView),
		ShowLatestAnnouncement: request.Body.ShowLatestAnnouncement,
		CustomNavigationItems:  customNavigationItemsInput(request.Body.CustomNavigationItems),
		ExpectedVersion:        request.Body.ExpectedVersion, Reason: request.Body.Reason,
	})
	if errors.Is(err, catalog.ErrSiteDisplaySettingsInput) {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_site_display_settings", "站点与展示设置无效", "请检查名称、说明、种子文件名前缀、自定义菜单、默认视图和版本。")
		return generated.UpdateSiteDisplaySettings400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "site_display_update_denied", "没有权限更新站点与展示设置", "当前后台身份没有 site.display.update。")
		return generated.UpdateSiteDisplaySettings403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, catalog.ErrSiteDisplaySettingsVersionConflict) {
		problem := newProblemFromContext(ctx, http.StatusConflict, "site_display_settings_version_conflict", "设置版本已经变化", "当前编辑基于旧版本，请重新载入后再提交。")
		return generated.UpdateSiteDisplaySettings409ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.UpdateSiteDisplaySettings200JSONResponse(siteDisplaySettingsDTO(result)), nil
}

// GetRegistrationPolicySettings authenticates the staff audience before the
// identity use case evaluates the dedicated policy-read permission.
func (h *Handler) GetRegistrationPolicySettings(ctx context.Context, _ generated.GetRegistrationPolicySettingsRequestObject) (generated.GetRegistrationPolicySettingsResponseObject, error) {
	session, authenticationProblem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.GetRegistrationPolicySettings401ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*authenticationProblem),
			}, nil
		}
		return generated.GetRegistrationPolicySettings403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.registration.Policy(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "site_registration_manage_read_denied", "无法查看注册设置", "当前后台身份没有 site.registration.manage.read。")
		return generated.GetRegistrationPolicySettings403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.GetRegistrationPolicySettings200JSONResponse(registrationPolicySettingsDTO(result)), nil
}

// UpdateRegistrationPolicySettings applies only the exact version reviewed by
// the staff editor; identity commits the row and audit evidence atomically.
func (h *Handler) UpdateRegistrationPolicySettings(ctx context.Context, request generated.UpdateRegistrationPolicySettingsRequestObject) (generated.UpdateRegistrationPolicySettingsResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_registration_policy_settings", "注册设置无效", "请检查注册模式、版本和变更理由。")
		return generated.UpdateRegistrationPolicySettings400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.UpdateRegistrationPolicySettings401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.UpdateRegistrationPolicySettings403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.registration.UpdatePolicy(ctx, staffActor(session), identity.UpdateRegistrationPolicyInput{
		Mode:                                     identity.RegistrationMode(request.Body.Mode),
		MemberInvitesEnabled:                     request.Body.MemberInvitesEnabled,
		InviteValidDays:                          request.Body.InviteValidDays,
		MaxInvitesPerMember:                      request.Body.MaxInvitesPerMember,
		MinimumInviteAccountAgeDays:              request.Body.MinimumInviteAccountAgeDays,
		MinimumInviteLevel:                       request.Body.MinimumInviteLevel,
		UsernameMinCharacters:                    request.Body.UsernameMinCharacters,
		UsernameMaxCharacters:                    request.Body.UsernameMaxCharacters,
		ReservedUsernames:                        append(make([]string, 0, len(request.Body.ReservedUsernames)), request.Body.ReservedUsernames...),
		EmailDomainMode:                          identity.EmailDomainMode(request.Body.EmailDomainMode),
		EmailDomains:                             append(make([]string, 0, len(request.Body.EmailDomains)), request.Body.EmailDomains...),
		SessionValidHours:                        request.Body.SessionValidHours,
		RememberSessionValidHours:                request.Body.RememberSessionValidHours,
		HumanVerificationProvider:                identity.HumanVerificationProvider(request.Body.HumanVerificationProvider),
		HumanVerificationSiteKey:                 request.Body.HumanVerificationSiteKey,
		HumanVerificationRegistrationEnabled:     request.Body.HumanVerificationRegistrationEnabled,
		HumanVerificationLoginEnabled:            request.Body.HumanVerificationLoginEnabled,
		HumanVerificationPasswordRecoveryEnabled: request.Body.HumanVerificationPasswordRecoveryEnabled,
		ExpectedVersion:                          request.Body.ExpectedVersion,
		Reason:                                   request.Body.Reason,
	})
	if errors.Is(err, identity.ErrRegistrationPolicyInput) {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_registration_policy_settings", "注册设置无效", "请检查注册模式、版本和变更理由。")
		return generated.UpdateRegistrationPolicySettings400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "site_registration_update_denied", "没有权限更新注册设置", "当前后台身份没有 site.registration.update。")
		return generated.UpdateRegistrationPolicySettings403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrRegistrationPolicyVersionConflict) {
		problem := newProblemFromContext(ctx, http.StatusConflict, "registration_policy_settings_version_conflict", "注册设置版本已经变化", "当前编辑基于旧版本，请重新载入后再提交。")
		return generated.UpdateRegistrationPolicySettings409ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.UpdateRegistrationPolicySettings200JSONResponse(registrationPolicySettingsDTO(result)), nil
}

// BeginStaffEnrollment verifies Web session/CSRF, typed enrollment authority
// and the one-time ticket before returning browser-safe registration options.
func (h *Handler) BeginStaffEnrollment(ctx context.Context, request generated.BeginStaffEnrollmentRequestObject) (generated.BeginStaffEnrollmentResponseObject, error) {
	webCookie := sessionTokenFromContext(ctx)
	if webCookie == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请先建立普通 Web 会话。")
		return generated.BeginStaffEnrollment401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if request.Body == nil || request.Body.BootstrapToken == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_staff_enrollment", "注册信息无效", "请检查一次性票据与凭据名称。")
		return generated.BeginStaffEnrollment400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	result, err := h.staffEnrollment.Begin(ctx, webCookie, request.Params.XCSRFToken, identity.BeginStaffEnrollmentInput{
		BootstrapToken: *request.Body.BootstrapToken,
		Label:          request.Body.Label,
	})
	if errors.Is(err, identity.ErrInvalidInput) {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_staff_enrollment", "注册信息无效", "请检查一次性票据与凭据名称。")
		return generated.BeginStaffEnrollment400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, identity.ErrSessionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后配置后台安全凭据。")
		return generated.BeginStaffEnrollment401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrStaffBootstrapTicketInvalid) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "staff_bootstrap_ticket_invalid", "一次性票据不可用", "票据无效、已过期、已撤销或不属于当前账号。")
		return generated.BeginStaffEnrollment401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrInvalidCSRF) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.BeginStaffEnrollment403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "staff_enrollment_denied", "无法配置后台凭据", "当前账号没有有效的后台凭据注册授权。")
		return generated.BeginStaffEnrollment403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	var publicKey generated.WebAuthnCreationOptions
	if err := json.Unmarshal(result.PublicKey, &publicKey); err != nil {
		return nil, errors.New("staff WebAuthn registration options do not match the HTTP contract")
	}
	return generated.BeginStaffEnrollment200JSONResponse{
		ChallengeId: result.ChallengeID,
		ExpiresAt:   result.ExpiresAt,
		PublicKey:   publicKey,
	}, nil
}

// CompleteStaffEnrollment consumes the challenge before verification. The
// domain repository then commits credential, ticket consumption and audit as
// one unit; this adapter never receives the decrypted credential record.
func (h *Handler) CompleteStaffEnrollment(ctx context.Context, request generated.CompleteStaffEnrollmentRequestObject) (generated.CompleteStaffEnrollmentResponseObject, error) {
	webCookie := sessionTokenFromContext(ctx)
	if webCookie == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请先建立普通 Web 会话。")
		return generated.CompleteStaffEnrollment401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if request.Body == nil || request.Body.BootstrapToken == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_staff_enrollment", "安全凭据响应无效", "请重新发起后台凭据注册。")
		return generated.CompleteStaffEnrollment400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	credential, err := json.Marshal(request.Body.Credential)
	if err != nil {
		return nil, err
	}
	result, err := h.staffEnrollment.Complete(ctx, webCookie, request.Params.XCSRFToken, identity.CompleteStaffEnrollmentInput{
		BootstrapToken: *request.Body.BootstrapToken,
		ChallengeID:    request.Body.ChallengeId,
		Credential:     credential,
	})
	if errors.Is(err, identity.ErrInvalidInput) {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_staff_enrollment", "安全凭据响应无效", "请重新发起后台凭据注册。")
		return generated.CompleteStaffEnrollment400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, identity.ErrSessionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后配置后台安全凭据。")
		return generated.CompleteStaffEnrollment401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrStaffBootstrapTicketInvalid) || errors.Is(err, identity.ErrStaffEnrollmentChallengeNotFound) || errors.Is(err, identity.ErrStaffEnrollmentVerification) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "staff_enrollment_failed", "后台凭据注册失败", "票据或 registration 已失效，请重新开始。")
		return generated.CompleteStaffEnrollment401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrInvalidCSRF) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.CompleteStaffEnrollment403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "staff_enrollment_denied", "无法配置后台凭据", "当前账号没有有效的后台凭据注册授权。")
		return generated.CompleteStaffEnrollment403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrStaffCredentialAlreadyEnrolled) {
		problem := newProblemFromContext(ctx, http.StatusConflict, "staff_credential_exists", "安全凭据已经存在", "该票据或 authenticator 已经完成注册。")
		return generated.CompleteStaffEnrollment409ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.CompleteStaffEnrollment201JSONResponse{
		Label:      result.Label,
		EnrolledAt: result.EnrolledAt,
	}, nil
}

var _ generated.StrictServerInterface = (*Handler)(nil)

// GetMyCapabilities implements generated.StrictServerInterface.
func (h *Handler) GetMyCapabilities(ctx context.Context, _ generated.GetMyCapabilitiesRequestObject) (generated.GetMyCapabilitiesResponseObject, error) {
	cookieToken := sessionTokenFromContext(ctx)
	if cookieToken == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请登录后查看自己的权限。")
		return generated.GetMyCapabilities401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}

	session, err := h.identity.CurrentSession(ctx, cookieToken)
	if errors.Is(err, identity.ErrSessionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后继续。")
		return generated.GetMyCapabilities401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if err != nil {
		return nil, err
	}

	result, err := h.authorization.Capabilities(ctx, authz.Subject{ID: session.User.ID, Status: authz.SubjectActive})
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "capability_discovery_denied", "无法查看权限", "当前账号没有权限发现能力。")
		return generated.GetMyCapabilities403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}

	return generated.GetMyCapabilities200JSONResponse(capabilitySetDTO(result)), nil
}

// GetMyStaffCapabilities supports both the simple account-session admin path
// and the optional passkey session. Both paths bind capability discovery to
// the exact site-admin eligibility grant evaluated for the current request.
func (h *Handler) GetMyStaffCapabilities(ctx context.Context, _ generated.GetMyStaffCapabilitiesRequestObject) (generated.GetMyStaffCapabilitiesResponseObject, error) {
	session, authenticationProblem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.GetMyStaffCapabilities401ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*authenticationProblem),
			}, nil
		}
		return generated.GetMyStaffCapabilities403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.authorization.StaffCapabilities(
		ctx,
		authz.Subject{ID: session.User.ID, Status: authz.SubjectActive},
		session.WebAuthnAuthenticatedAt,
		session.Authority,
	)
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "staff_capability_discovery_denied", "无法查看后台能力", "当前后台会话没有能力发现权限。")
		return generated.GetMyStaffCapabilities403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.GetMyStaffCapabilities200JSONResponse(capabilitySetDTO(result)), nil
}

// GetGrantAdministration returns the bounded governance projection only after
// both the staff credential binding and authz.grant.read have been evaluated.
func (h *Handler) GetGrantAdministration(ctx context.Context, _ generated.GetGrantAdministrationRequestObject) (generated.GetGrantAdministrationResponseObject, error) {
	session, authenticationProblem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.GetGrantAdministration401ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*authenticationProblem),
			}, nil
		}
		return generated.GetGrantAdministration403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.grantAdministration.Overview(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "grant_read_denied", "无法查看权限与任期", "当前后台身份没有 authz.grant.read。")
		return generated.GetGrantAdministration403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.GetGrantAdministration200JSONResponse(grantAdministrationOverviewDTO(result)), nil
}

// CreateGrantRevocation creates a reducing-only change request. The target and
// proposer relationship is checked again inside the locked database transaction.
func (h *Handler) CreateGrantRevocation(ctx context.Context, request generated.CreateGrantRevocationRequestObject) (generated.CreateGrantRevocationResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_grant_revocation", "撤权申请无效", "请填写目标 grant、当前版本和完整理由。")
		return generated.CreateGrantRevocation400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	actor, problem, err := h.authenticateGrantAdministrationWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		switch problem.Status {
		case http.StatusUnauthorized:
			return generated.CreateGrantRevocation401ApplicationProblemPlusJSONResponse(*problem), nil
		default:
			return generated.CreateGrantRevocation403ApplicationProblemPlusJSONResponse(*problem), nil
		}
	}
	result, err := h.grantAdministration.ProposeRevocation(ctx, actor, authz.ProposeGrantRevocationInput{
		GrantID:              request.Body.GrantId,
		ExpectedGrantVersion: request.Body.ExpectedGrantVersion,
		Reason:               request.Body.Reason,
	})
	if err != nil {
		problem, handled := grantAdministrationProblem(ctx, err)
		if !handled {
			return nil, err
		}
		switch problem.Status {
		case http.StatusBadRequest:
			return generated.CreateGrantRevocation400ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
			}, nil
		case http.StatusForbidden:
			return generated.CreateGrantRevocation403ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusNotFound:
			return generated.CreateGrantRevocation404ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return generated.CreateGrantRevocation409ApplicationProblemPlusJSONResponse(problem), nil
		}
	}
	return generated.CreateGrantRevocation201JSONResponse(grantRevocationRequestDTO(result)), nil
}

func (h *Handler) ReviewGrantRevocationAsGovernance(ctx context.Context, request generated.ReviewGrantRevocationAsGovernanceRequestObject) (generated.ReviewGrantRevocationAsGovernanceResponseObject, error) {
	result, problem, err := h.reviewGrantRevocation(ctx, request.RequestId, request.Params.XCSRFToken, request.Body, authz.GrantReviewGovernance)
	if err != nil {
		return nil, err
	}
	if problem == nil {
		return generated.ReviewGrantRevocationAsGovernance200JSONResponse(result), nil
	}
	switch problem.Status {
	case http.StatusBadRequest:
		return generated.ReviewGrantRevocationAsGovernance400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem),
		}, nil
	case http.StatusUnauthorized:
		return generated.ReviewGrantRevocationAsGovernance401ApplicationProblemPlusJSONResponse(*problem), nil
	case http.StatusForbidden:
		return generated.ReviewGrantRevocationAsGovernance403ApplicationProblemPlusJSONResponse(*problem), nil
	case http.StatusNotFound:
		return generated.ReviewGrantRevocationAsGovernance404ApplicationProblemPlusJSONResponse(*problem), nil
	default:
		return generated.ReviewGrantRevocationAsGovernance409ApplicationProblemPlusJSONResponse(*problem), nil
	}
}

func (h *Handler) ReviewGrantRevocationAsSecurity(ctx context.Context, request generated.ReviewGrantRevocationAsSecurityRequestObject) (generated.ReviewGrantRevocationAsSecurityResponseObject, error) {
	result, problem, err := h.reviewGrantRevocation(ctx, request.RequestId, request.Params.XCSRFToken, request.Body, authz.GrantReviewSecurity)
	if err != nil {
		return nil, err
	}
	if problem == nil {
		return generated.ReviewGrantRevocationAsSecurity200JSONResponse(result), nil
	}
	switch problem.Status {
	case http.StatusBadRequest:
		return generated.ReviewGrantRevocationAsSecurity400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem),
		}, nil
	case http.StatusUnauthorized:
		return generated.ReviewGrantRevocationAsSecurity401ApplicationProblemPlusJSONResponse(*problem), nil
	case http.StatusForbidden:
		return generated.ReviewGrantRevocationAsSecurity403ApplicationProblemPlusJSONResponse(*problem), nil
	case http.StatusNotFound:
		return generated.ReviewGrantRevocationAsSecurity404ApplicationProblemPlusJSONResponse(*problem), nil
	default:
		return generated.ReviewGrantRevocationAsSecurity409ApplicationProblemPlusJSONResponse(*problem), nil
	}
}

func (h *Handler) reviewGrantRevocation(ctx context.Context, requestID uuid.UUID, csrfToken string, body *generated.ReviewGrantRevocationRequest, domain authz.GrantReviewDomain) (generated.GrantRevocationRequest, *generated.Problem, error) {
	if body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_grant_review", "复核内容无效", "请选择批准或拒绝并填写完整理由。")
		return generated.GrantRevocationRequest{}, &problem, nil
	}
	actor, problem, err := h.authenticateGrantAdministrationWrite(ctx, csrfToken)
	if err != nil || problem != nil {
		return generated.GrantRevocationRequest{}, problem, err
	}
	result, err := h.grantAdministration.ReviewRevocation(ctx, actor, authz.ReviewGrantRevocationInput{
		RequestID: requestID,
		Domain:    domain,
		Decision:  authz.GrantReviewDecision(body.Decision),
		Reason:    body.Reason,
	})
	if err != nil {
		problem, handled := grantAdministrationProblem(ctx, err)
		if !handled {
			return generated.GrantRevocationRequest{}, nil, err
		}
		return generated.GrantRevocationRequest{}, &problem, nil
	}
	return grantRevocationRequestDTO(result), nil, nil
}

func (h *Handler) authenticateGrantAdministrationWrite(ctx context.Context, csrfToken string) (authz.GrantAdministrationActor, *generated.Problem, error) {
	session, problem, err := h.authenticateStaffWrite(ctx, csrfToken)
	if err != nil || problem != nil {
		return authz.StaffActor{}, problem, err
	}
	return staffActor(session), nil, nil
}

func (h *Handler) authenticateStaffRead(ctx context.Context) (identity.StaffSession, *generated.Problem, error) {
	staffCookie := staffSessionTokenFromContext(ctx)
	if staffCookie != "" {
		session, err := h.staffIdentity.CurrentSession(ctx, staffCookie)
		if err == nil {
			return session, nil, nil
		}
		if !errors.Is(err, identity.ErrStaffSessionNotFound) && !errors.Is(err, authz.ErrForbidden) {
			return identity.StaffSession{}, nil, err
		}
	}
	session, err := h.accountAdministratorSession(ctx, "")
	if errors.Is(err, identity.ErrSessionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请先登录站点账号。")
		return identity.StaffSession{}, &problem, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "admin_access_denied", "没有后台权限", "当前账号不是站点管理员。")
		return identity.StaffSession{}, &problem, nil
	}
	if err != nil {
		return identity.StaffSession{}, nil, err
	}
	return session, nil, nil
}

func (h *Handler) authenticateStaffWrite(ctx context.Context, csrfToken string) (identity.StaffSession, *generated.Problem, error) {
	staffCookie := staffSessionTokenFromContext(ctx)
	if staffCookie != "" {
		session, err := h.staffIdentity.AuthenticateWrite(ctx, staffCookie, csrfToken)
		if err == nil {
			return session, nil, nil
		}
		if errors.Is(err, identity.ErrInvalidCSRF) {
			problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新后台页面后重试。")
			return identity.StaffSession{}, &problem, nil
		}
		if !errors.Is(err, identity.ErrStaffSessionNotFound) && !errors.Is(err, authz.ErrForbidden) {
			return identity.StaffSession{}, nil, err
		}
	}
	session, err := h.accountAdministratorSession(ctx, csrfToken)
	if errors.Is(err, identity.ErrSessionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请先登录站点账号。")
		return identity.StaffSession{}, &problem, nil
	}
	if errors.Is(err, identity.ErrInvalidCSRF) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新后台页面后重试。")
		return identity.StaffSession{}, &problem, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "admin_access_denied", "没有后台权限", "当前账号不是站点管理员。")
		return identity.StaffSession{}, &problem, nil
	}
	if err != nil {
		return identity.StaffSession{}, nil, err
	}
	return session, nil, nil
}

// accountAdministratorSession converts an already authenticated Web account
// into request-scoped admin identity without issuing a second credential. The
// eligibility decision is still audited and every business use case performs
// its own staff-audience permission check, so this is not an authorization
// shortcut. A non-empty csrfToken also authenticates the write request.
func (h *Handler) accountAdministratorSession(ctx context.Context, csrfToken string) (identity.StaffSession, error) {
	webCookie := sessionTokenFromContext(ctx)
	if webCookie == "" {
		return identity.StaffSession{}, identity.ErrSessionNotFound
	}
	var (
		webSession identity.WebSession
		err        error
	)
	if csrfToken == "" {
		webSession, err = h.identity.CurrentSession(ctx, webCookie)
	} else {
		webSession, err = h.identity.AuthenticateWrite(ctx, webCookie, csrfToken)
	}
	if err != nil {
		return identity.StaffSession{}, err
	}
	decision, err := h.authorization.Authorize(ctx, authz.Request{
		Subject:            authz.Subject{ID: webSession.User.ID, Status: authz.SubjectActive},
		Action:             authz.ActionStaffSessionCreateSelf,
		CredentialAudience: authz.AudienceWebSession,
		Resource: authz.Resource{
			OwnerID: webSession.User.ID,
			Scope:   authz.SiteScope(),
		},
	})
	if err != nil {
		return identity.StaffSession{}, err
	}
	expiresAt := webSession.ExpiresAt
	if decision.EffectiveUntil.Before(expiresAt) {
		expiresAt = decision.EffectiveUntil
	}
	return identity.StaffSession{
		User: webSession.User, Authority: decision.AuthorityBinding(),
		CreatedAt: webSession.CreatedAt, ExpiresAt: expiresAt,
		AuthenticationMethod: identity.StaffAuthenticationAccountSession,
		AuthenticatedAt:      webSession.CreatedAt,
		CSRFToken:            webSession.CSRFToken,
	}, nil
}

func grantAdministrationProblem(ctx context.Context, err error) (generated.Problem, bool) {
	switch {
	case errors.Is(err, authz.ErrGrantAdministrationInput):
		return newProblemFromContext(ctx, http.StatusBadRequest, "invalid_grant_change", "权限变更内容无效", "请检查版本、决定和理由长度。"), true
	case errors.Is(err, authz.ErrForbidden):
		return newProblemFromContext(ctx, http.StatusForbidden, "grant_change_denied", "没有权限执行此操作", "当前后台身份缺少对应的 typed permission。"), true
	case errors.Is(err, authz.ErrGrantNotFound), errors.Is(err, authz.ErrGrantRevocationNotFound):
		return newProblemFromContext(ctx, http.StatusNotFound, "grant_change_not_found", "权限记录不存在", "目标 grant 或撤权申请已经不存在。"), true
	case errors.Is(err, authz.ErrSeparationOfDuties):
		return newProblemFromContext(ctx, http.StatusConflict, "separation_of_duties_violation", "职责分离校验失败", "不能撤销自己、复核自己的申请或由目标本人复核。"), true
	case errors.Is(err, authz.ErrGrantAlreadyRevoked):
		return newProblemFromContext(ctx, http.StatusConflict, "grant_already_revoked", "权限已经撤销", "请刷新页面获取最新 grant 状态。"), true
	case errors.Is(err, authz.ErrGrantVersionConflict):
		return newProblemFromContext(ctx, http.StatusConflict, "grant_version_conflict", "权限版本已经变化", "申请基于旧版本，请刷新后重新发起。"), true
	case errors.Is(err, authz.ErrGrantRevocationPending):
		return newProblemFromContext(ctx, http.StatusConflict, "grant_revocation_pending", "已有待复核申请", "同一个 grant 同时只能存在一个有效撤权申请。"), true
	case errors.Is(err, authz.ErrGrantRevocationClosed):
		return newProblemFromContext(ctx, http.StatusConflict, "grant_revocation_closed", "撤权申请已经关闭", "已拒绝、生效、冲突或过期的申请不能再次复核。"), true
	case errors.Is(err, authz.ErrGrantReviewExists):
		return newProblemFromContext(ctx, http.StatusConflict, "grant_review_exists", "该职责已经完成复核", "同一职责域和同一复核人只能记录一次决定。"), true
	default:
		return generated.Problem{}, false
	}
}

// ListManagedCategories returns disabled rows and aggregate reference counts;
// the public category endpoint remains enabled-only and anonymous.
func (h *Handler) ListManagedCategories(ctx context.Context, _ generated.ListManagedCategoriesRequestObject) (generated.ListManagedCategoriesResponseObject, error) {
	session, authenticationProblem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.ListManagedCategories401ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*authenticationProblem),
			}, nil
		}
		return generated.ListManagedCategories403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.categoryAdministration.List(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "category_manage_read_denied", "无法查看分类管理", "当前后台身份没有 category.manage.read。")
		return generated.ListManagedCategories403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	response := make(generated.ManagedCategoryList, 0, len(result))
	for _, category := range result {
		response = append(response, managedCategoryDTO(category))
	}
	return generated.ListManagedCategories200JSONResponse(response), nil
}

// CreateManagedCategory authenticates the independent staff cookie and CSRF
// token before the catalog use case evaluates category.create.
func (h *Handler) CreateManagedCategory(ctx context.Context, request generated.CreateManagedCategoryRequestObject) (generated.CreateManagedCategoryResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_category", "分类内容无效", "请检查稳定标识、名称、排序和变更理由。")
		return generated.CreateManagedCategory400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.CreateManagedCategory401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.CreateManagedCategory403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.categoryAdministration.Create(ctx, staffActor(session), catalog.CreateCategoryInput{
		ID: request.Body.Id, Name: request.Body.Name, DisplayOrder: request.Body.DisplayOrder,
		Enabled: request.Body.Enabled, Reason: request.Body.Reason,
	})
	if err != nil {
		problem, handled := categoryAdministrationProblem(ctx, err)
		if !handled {
			return nil, err
		}
		switch problem.Status {
		case http.StatusBadRequest:
			return generated.CreateManagedCategory400ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
			}, nil
		case http.StatusForbidden:
			return generated.CreateManagedCategory403ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return generated.CreateManagedCategory409ApplicationProblemPlusJSONResponse(problem), nil
		}
	}
	return generated.CreateManagedCategory201JSONResponse(managedCategoryDTO(result)), nil
}

// UpdateManagedCategory keeps the stable ID immutable and requires the exact
// version observed by the editor; stale writes receive an explicit conflict.
func (h *Handler) UpdateManagedCategory(ctx context.Context, request generated.UpdateManagedCategoryRequestObject) (generated.UpdateManagedCategoryResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_category", "分类内容无效", "请检查名称、排序、状态、版本和变更理由。")
		return generated.UpdateManagedCategory400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.UpdateManagedCategory401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.UpdateManagedCategory403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.categoryAdministration.Update(ctx, staffActor(session), catalog.UpdateCategoryInput{
		ID: string(request.CategoryId), Name: request.Body.Name, DisplayOrder: request.Body.DisplayOrder,
		Enabled: request.Body.Enabled, ExpectedVersion: request.Body.ExpectedVersion,
		Reason: request.Body.Reason,
	})
	if err != nil {
		problem, handled := categoryAdministrationProblem(ctx, err)
		if !handled {
			return nil, err
		}
		switch problem.Status {
		case http.StatusBadRequest:
			return generated.UpdateManagedCategory400ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
			}, nil
		case http.StatusForbidden:
			return generated.UpdateManagedCategory403ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusNotFound:
			return generated.UpdateManagedCategory404ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return generated.UpdateManagedCategory409ApplicationProblemPlusJSONResponse(problem), nil
		}
	}
	return generated.UpdateManagedCategory200JSONResponse(managedCategoryDTO(result)), nil
}

// UpsertManagedCategoryFacet restores the missing PtYes category-attribute
// administration surface. Stable IDs and selection modes become immutable
// after creation; category-local label, requirement, order and availability
// remain editable with optimistic concurrency.
func (h *Handler) UpsertManagedCategoryFacet(ctx context.Context, request generated.UpsertManagedCategoryFacetRequestObject) (generated.UpsertManagedCategoryFacetResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_category_facet", "分类属性无效", "请检查名称、选择模式、必填规则、排序、状态和版本。")
		return generated.UpsertManagedCategoryFacet400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.UpsertManagedCategoryFacet401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.UpsertManagedCategoryFacet403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	requirementGroup := ""
	if request.Body.RequirementGroup != nil {
		requirementGroup = *request.Body.RequirementGroup
	}
	result, err := h.categoryAdministration.UpsertFacet(ctx, staffActor(session), catalog.UpsertCategoryFacetInput{
		CategoryID: string(request.CategoryId), FacetID: request.FacetId,
		Name: request.Body.Name, SelectionMode: catalog.FacetSelectionMode(request.Body.SelectionMode),
		Required: request.Body.Required, RequirementGroup: requirementGroup,
		DisplayOrder: request.Body.DisplayOrder, Enabled: request.Body.Enabled,
		ExpectedVersion: request.Body.ExpectedVersion, Reason: request.Body.Reason,
	})
	if err != nil {
		problem, handled := categoryAdministrationProblem(ctx, err)
		if !handled {
			return nil, err
		}
		switch problem.Status {
		case http.StatusBadRequest:
			return generated.UpsertManagedCategoryFacet400ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
			}, nil
		case http.StatusForbidden:
			return generated.UpsertManagedCategoryFacet403ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusNotFound:
			return generated.UpsertManagedCategoryFacet404ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return generated.UpsertManagedCategoryFacet409ApplicationProblemPlusJSONResponse(problem), nil
		}
	}
	return generated.UpsertManagedCategoryFacet200JSONResponse(managedCategoryFacetDTO(result)), nil
}

// UpsertManagedCategoryFacetOption restores PtYes-style category attribute
// option management without duplicating the public upload vocabulary.
func (h *Handler) UpsertManagedCategoryFacetOption(ctx context.Context, request generated.UpsertManagedCategoryFacetOptionRequestObject) (generated.UpsertManagedCategoryFacetOptionResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_category_option", "类型选项无效", "请检查名称、排序、状态、版本和变更理由。")
		return generated.UpsertManagedCategoryFacetOption400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.UpsertManagedCategoryFacetOption401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.UpsertManagedCategoryFacetOption403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.categoryAdministration.UpsertFacetOption(ctx, staffActor(session), catalog.UpsertCategoryFacetOptionInput{
		CategoryID: string(request.CategoryId), FacetID: request.FacetId, OptionKey: request.OptionKey,
		Label: request.Body.Label, DisplayOrder: request.Body.DisplayOrder, Enabled: request.Body.Enabled,
		ExpectedVersion: request.Body.ExpectedVersion, Reason: request.Body.Reason,
	})
	if err != nil {
		problem, handled := categoryAdministrationProblem(ctx, err)
		if !handled {
			return nil, err
		}
		switch problem.Status {
		case http.StatusBadRequest:
			return generated.UpsertManagedCategoryFacetOption400ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
			}, nil
		case http.StatusForbidden:
			return generated.UpsertManagedCategoryFacetOption403ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusNotFound:
			return generated.UpsertManagedCategoryFacetOption404ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return generated.UpsertManagedCategoryFacetOption409ApplicationProblemPlusJSONResponse(problem), nil
		}
	}
	return generated.UpsertManagedCategoryFacetOption200JSONResponse(managedCategoryFacetOptionDTO(result)), nil
}

func categoryAdministrationProblem(ctx context.Context, err error) (generated.Problem, bool) {
	switch {
	case errors.Is(err, catalog.ErrCategoryAdministrationInput):
		return newProblemFromContext(ctx, http.StatusBadRequest, "invalid_category", "分类内容无效", "请检查稳定标识、名称、排序、版本和变更理由。"), true
	case errors.Is(err, authz.ErrForbidden):
		return newProblemFromContext(ctx, http.StatusForbidden, "category_change_denied", "没有权限修改分类", "当前后台身份缺少对应的 typed permission。"), true
	case errors.Is(err, catalog.ErrCategoryNotFound):
		return newProblemFromContext(ctx, http.StatusNotFound, "category_not_found", "分类不存在", "目标分类已经不存在，请刷新列表。"), true
	case errors.Is(err, catalog.ErrCategoryAlreadyExists):
		return newProblemFromContext(ctx, http.StatusConflict, "category_exists", "稳定标识已被使用", "分类稳定标识创建后不可复用或修改。"), true
	case errors.Is(err, catalog.ErrCategoryVersionConflict):
		return newProblemFromContext(ctx, http.StatusConflict, "category_version_conflict", "分类版本已经变化", "当前编辑基于旧版本，请重新载入后再提交。"), true
	case errors.Is(err, catalog.ErrCategoryFacetNotFound):
		return newProblemFromContext(ctx, http.StatusNotFound, "category_facet_not_found", "分类属性不存在", "目标分类没有这个属性，请刷新列表。"), true
	case errors.Is(err, catalog.ErrCategoryFacetAlreadyExists):
		return newProblemFromContext(ctx, http.StatusConflict, "category_facet_exists", "分类属性已经存在", "相同稳定标识已经绑定到这个分类。"), true
	case errors.Is(err, catalog.ErrCategoryFacetUnavailable):
		return newProblemFromContext(ctx, http.StatusConflict, "category_facet_unavailable", "分类属性不可用", "同名规范属性已停用，或创建后不能再改变单选/多选模式。"), true
	case errors.Is(err, catalog.ErrCategoryFacetVersionConflict):
		return newProblemFromContext(ctx, http.StatusConflict, "category_facet_version_conflict", "分类属性版本已经变化", "当前编辑基于旧版本，请刷新后再提交。"), true
	case errors.Is(err, catalog.ErrCategoryFacetLimitReached):
		return newProblemFromContext(ctx, http.StatusConflict, "category_facet_limit_reached", "分类属性数量已达上限", "每个分类最多配置 20 个发种属性，请停用或复用已有属性。"), true
	case errors.Is(err, catalog.ErrCategoryOptionNotFound):
		return newProblemFromContext(ctx, http.StatusNotFound, "category_option_not_found", "类型选项不存在", "目标选项已经不存在，请刷新列表。"), true
	case errors.Is(err, catalog.ErrCategoryOptionAlreadyExists):
		return newProblemFromContext(ctx, http.StatusConflict, "category_option_exists", "类型选项已经存在", "相同稳定值已经绑定到这个分类。"), true
	case errors.Is(err, catalog.ErrCategoryOptionUnavailable):
		return newProblemFromContext(ctx, http.StatusConflict, "category_option_unavailable", "类型选项不可用", "同名全局选项已停用或属性模式不一致。"), true
	case errors.Is(err, catalog.ErrCategoryOptionVersionConflict):
		return newProblemFromContext(ctx, http.StatusConflict, "category_option_version_conflict", "类型选项版本已经变化", "当前编辑基于旧版本，请刷新后再提交。"), true
	case errors.Is(err, catalog.ErrCategoryOptionLimitReached):
		return newProblemFromContext(ctx, http.StatusConflict, "category_option_limit_reached", "类型选项数量已达上限", "每个分类属性最多配置 200 个选项，请复用或停用已有选项。"), true
	default:
		return generated.Problem{}, false
	}
}

// CreateRegistration implements the anonymous, idempotent admission command.
// Transport maps fields explicitly so credential material cannot enter a
// reusable DTO, log field or Core persistence adapter by accident.
func (h *Handler) CreateRegistration(ctx context.Context, request generated.CreateRegistrationRequestObject) (generated.CreateRegistrationResponseObject, error) {
	if request.Body == nil || request.Body.Email == nil || request.Body.Password == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_registration", "注册信息无效", "请完整填写用户名、显示名称、邮箱和密码。")
		return generated.CreateRegistration400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	invitationToken := ""
	if request.Body.InvitationToken != nil {
		invitationToken = *request.Body.InvitationToken
	}
	humanVerificationToken := ""
	if request.Body.HumanVerificationToken != nil {
		humanVerificationToken = *request.Body.HumanVerificationToken
	}
	if err := h.verifyHumanIdentityCommand(ctx, identity.HumanVerificationFlowRegistration, humanVerificationToken); err != nil {
		status, problem := humanVerificationProblem(ctx, err)
		return generated.CreateRegistrationdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: status}, nil
	}
	result, err := h.registration.Register(ctx, identity.RegistrationInput{
		ID:              request.Params.IdempotencyKey,
		Username:        request.Body.Username,
		DisplayName:     request.Body.DisplayName,
		Email:           string(*request.Body.Email),
		Password:        *request.Body.Password,
		InvitationToken: invitationToken,
	})
	switch {
	case errors.Is(err, identity.ErrInvalidInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_registration", "注册信息无效", "请检查用户名格式、邮箱和至少 12 位的密码。")
		return generated.CreateRegistration400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrRegistrationClosed):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "registration_closed", "暂未开放注册", "当前站点准入策略不接受新账户。")
		return generated.CreateRegistration403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrRegistrationInvitationInvalid):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "invitation_unavailable", "邀请凭证不可用", "邀请可能无效、已被使用、已撤销或已经过期；若邀请绑定了邮箱，请填写签发时指定的邮箱。")
		return generated.CreateRegistration403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrRegistrationUnavailable):
		problem := newProblemFromContext(ctx, http.StatusConflict, "registration_unavailable", "无法使用这些注册信息", "用户名或邮箱已被使用，请更换后重试；如本次使用邀请码，系统已释放本次未完成的占用。")
		return generated.CreateRegistration409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrRegistrationIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "idempotency_conflict", "本次注册内容已经变化", "请刷新页面后重新开始注册。")
		return generated.CreateRegistration409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrRegistrationStateConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "registration_state_conflict", "注册状态需要恢复", "请使用同一页面重试；若持续失败，请附上 request_id 联系管理员。")
		return generated.CreateRegistration409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrRegistrationServiceUnavailable):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "registration_unavailable", "注册服务暂时不可用", "请保留当前页面并稍后重试。")
		return generated.CreateRegistrationdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	case err != nil:
		return nil, err
	}
	return generated.CreateRegistration201JSONResponse{
		User:                      generated.SessionUser{Id: result.UserID, Username: result.Username, DisplayName: result.DisplayName, EmailVerified: false},
		AdmissionMode:             generated.RegistrationMode(result.RegistrationMode),
		EmailVerificationRequired: result.EmailVerificationRequired,
		CompletedAt:               result.CompletedAt,
	}, nil
}

// GetWebSession implements generated.StrictServerInterface.
func (h *Handler) GetWebSession(ctx context.Context, _ generated.GetWebSessionRequestObject) (generated.GetWebSessionResponseObject, error) {
	cookieToken := sessionTokenFromContext(ctx)
	if cookieToken == "" {
		return generated.GetWebSession204Response{}, nil
	}

	session, err := h.identity.CurrentSession(ctx, cookieToken)
	if errors.Is(err, identity.ErrSessionNotFound) {
		return generated.GetWebSession204Response{}, nil
	}
	if err != nil {
		return nil, err
	}
	return generated.GetWebSession200JSONResponse(webSessionDTO(session)), nil
}

// CreateWebSession implements generated.StrictServerInterface.
func (h *Handler) CreateWebSession(ctx context.Context, request generated.CreateWebSessionRequestObject) (generated.CreateWebSessionResponseObject, error) {
	if request.Body == nil || request.Body.Password == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_login", "登录信息无效", "请填写用户名和密码。")
		return generated.CreateWebSession400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	secondFactorCode := ""
	if request.Body.SecondFactorCode != nil {
		secondFactorCode = *request.Body.SecondFactorCode
	}
	humanVerificationToken := ""
	if request.Body.HumanVerificationToken != nil {
		humanVerificationToken = *request.Body.HumanVerificationToken
	}
	if err := h.verifyHumanIdentityCommand(ctx, identity.HumanVerificationFlowLogin, humanVerificationToken); err != nil {
		status, problem := humanVerificationProblem(ctx, err)
		return generated.CreateWebSessiondefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: status}, nil
	}

	session, err := h.identity.Login(ctx, identity.LoginInput{
		Identifier:       request.Body.Identifier,
		Password:         *request.Body.Password,
		SecondFactorCode: secondFactorCode,
		RememberMe:       request.Body.RememberMe,
		ClientAddress:    clientAddressFromContext(ctx),
	})
	if errors.Is(err, identity.ErrInvalidInput) {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_login", "登录信息无效", "请检查输入长度和验证码格式。")
		return generated.CreateWebSession400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, identity.ErrInvalidCredentials) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误", "请检查登录信息后重试。")
		return generated.CreateWebSession401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrSecondFactorRequired) {
		problem := newProblemFromContext(ctx, http.StatusPreconditionRequired, "second_factor_required", "需要两步验证码", "密码已验证，请继续输入验证器中的六位验证码或一次性恢复码。")
		return generated.CreateWebSession428ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrLoginThrottled) {
		problem := newProblemFromContext(ctx, http.StatusTooManyRequests, "login_throttled", "登录尝试过于频繁", "请短暂等待后再试；该限制不会说明账户是否存在。")
		return generated.CreateWebSession429ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrCredentialVerifierUnavailable) {
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "identity_unavailable", "身份服务暂时不可用", "请稍后重试。")
		return generated.CreateWebSessiondefaultApplicationProblemPlusJSONResponse{
			Body: problem, StatusCode: http.StatusServiceUnavailable,
		}, nil
	}
	if err != nil {
		return nil, err
	}

	sessionCookie := newSessionCookie(h.sessionCookie, session.CookieToken, session.CreatedAt, session.ExpiresAt)
	cookie := sessionCookie.String()
	return generated.CreateWebSession200JSONResponse{
		Body: webSessionDTO(session),
		Headers: generated.CreateWebSession200ResponseHeaders{
			SetCookie: &cookie,
		},
	}, nil
}

// DeleteWebSession implements generated.StrictServerInterface.
func (h *Handler) DeleteWebSession(ctx context.Context, request generated.DeleteWebSessionRequestObject) (generated.DeleteWebSessionResponseObject, error) {
	cookieToken := sessionTokenFromContext(ctx)
	if cookieToken == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "当前没有可撤销的会话。")
		return generated.DeleteWebSession401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}

	err := h.identity.Logout(ctx, cookieToken, request.Params.XCSRFToken)
	if errors.Is(err, identity.ErrSessionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "当前会话已经失效。")
		return generated.DeleteWebSession401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, identity.ErrInvalidCSRF) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.DeleteWebSession403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}

	expiredCookie := expiredSessionCookie(h.sessionCookie)
	cookie := expiredCookie.String()
	return generated.DeleteWebSession204Response{
		Headers: generated.DeleteWebSession204ResponseHeaders{SetCookie: &cookie},
	}, nil
}

// GetAccountSecurity combines Core timestamps with Vault's deliberately
// redacted factor status. Secret material never enters this projection.
func (h *Handler) GetAccountSecurity(ctx context.Context, _ generated.GetAccountSecurityRequestObject) (generated.GetAccountSecurityResponseObject, error) {
	cookieToken := sessionTokenFromContext(ctx)
	if cookieToken == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请先建立有效的 Web 会话。")
		return generated.GetAccountSecurity401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	result, err := h.sessionSecurity.Overview(ctx, cookieToken)
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后查看账户安全状态。")
		return generated.GetAccountSecurity401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "session_security_forbidden", "无法查看账户安全状态", "当前账户没有有效的本人会话读取权限。")
		return generated.GetAccountSecurity403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GetAccountSecurity200JSONResponse{
		EmailVerified: result.EmailVerified, PasswordChangedAt: result.PasswordChangedAt,
		TwoFactor: generated.TwoFactorStatus{
			Enabled: result.TwoFactor.Enabled, EnabledAt: result.TwoFactor.EnabledAt,
			RecoveryCodesRemaining: result.TwoFactor.RecoveryCodesRemaining,
		},
	}, nil
}

// StartMyTOTPEnrollment requires both the session-bound CSRF token and the
// current password. The returned seed is transient and is never persisted by
// Core or included in an audit event.
func (h *Handler) StartMyTOTPEnrollment(ctx context.Context, request generated.StartMyTOTPEnrollmentRequestObject) (generated.StartMyTOTPEnrollmentResponseObject, error) {
	if sessionTokenFromContext(ctx) == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后设置两步验证。")
		return generated.StartMyTOTPEnrollment401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if request.Body == nil || request.Body.Password == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_two_factor_enrollment", "登记信息无效", "请输入当前密码。")
		return generated.StartMyTOTPEnrollment400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	result, err := h.twoFactor.StartEnrollment(ctx, sessionTokenFromContext(ctx), request.Params.XCSRFToken, identity.TOTPEnrollmentCommand{Password: *request.Body.Password})
	if err != nil {
		problem, status, mapped := twoFactorProblem(ctx, err)
		if !mapped {
			return nil, err
		}
		switch status {
		case http.StatusBadRequest:
			return generated.StartMyTOTPEnrollment400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
		case http.StatusUnauthorized:
			return generated.StartMyTOTPEnrollment401ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusForbidden:
			return generated.StartMyTOTPEnrollment403ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusConflict:
			return generated.StartMyTOTPEnrollment409ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusTooManyRequests:
			return generated.StartMyTOTPEnrollment429ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return generated.StartMyTOTPEnrollmentdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: status}, nil
		}
	}
	secret, provisioningURI := result.Secret, result.ProvisioningURI
	return generated.StartMyTOTPEnrollment200JSONResponse{
		EnrollmentId: result.EnrollmentID, Secret: &secret,
		ProvisioningUri: &provisioningURI, ExpiresAt: result.ExpiresAt,
	}, nil
}

func (h *Handler) ConfirmMyTOTPEnrollment(ctx context.Context, request generated.ConfirmMyTOTPEnrollmentRequestObject) (generated.ConfirmMyTOTPEnrollmentResponseObject, error) {
	if sessionTokenFromContext(ctx) == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后完成两步验证登记。")
		return generated.ConfirmMyTOTPEnrollment401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if request.Body == nil || request.Body.Code == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_two_factor_confirmation", "验证码无效", "请输入验证器当前显示的六位验证码。")
		return generated.ConfirmMyTOTPEnrollment400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	result, err := h.twoFactor.ConfirmEnrollment(ctx, sessionTokenFromContext(ctx), request.Params.XCSRFToken, identity.TOTPEnrollmentConfirmationCommand{
		EnrollmentID: request.EnrollmentId, Code: *request.Body.Code,
	})
	if err != nil {
		problem, status, mapped := twoFactorProblem(ctx, err)
		if !mapped {
			return nil, err
		}
		switch status {
		case http.StatusBadRequest:
			return generated.ConfirmMyTOTPEnrollment400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
		case http.StatusUnauthorized:
			return generated.ConfirmMyTOTPEnrollment401ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusForbidden:
			return generated.ConfirmMyTOTPEnrollment403ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusNotFound:
			return generated.ConfirmMyTOTPEnrollment404ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusConflict:
			return generated.ConfirmMyTOTPEnrollment409ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusGone:
			return generated.ConfirmMyTOTPEnrollment410ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusTooManyRequests:
			return generated.ConfirmMyTOTPEnrollment429ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return generated.ConfirmMyTOTPEnrollmentdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: status}, nil
		}
	}
	return generated.ConfirmMyTOTPEnrollment200JSONResponse{
		ChangeId: result.ChangeID, EnabledAt: result.EnabledAt,
		RecoveryCodes: append(generated.RecoveryCodeList(nil), result.RecoveryCodes...),
	}, nil
}

func (h *Handler) RotateMyTOTPRecoveryCodes(ctx context.Context, request generated.RotateMyTOTPRecoveryCodesRequestObject) (generated.RotateMyTOTPRecoveryCodesResponseObject, error) {
	if sessionTokenFromContext(ctx) == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后轮换恢复码。")
		return generated.RotateMyTOTPRecoveryCodes401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if request.Body == nil || request.Body.Password == nil || request.Body.SecondFactorCode == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_two_factor_reauthentication", "重新验证信息无效", "请输入当前密码以及 TOTP 或一次性恢复码。")
		return generated.RotateMyTOTPRecoveryCodes400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	result, err := h.twoFactor.RotateRecoveryCodes(ctx, sessionTokenFromContext(ctx), request.Params.XCSRFToken, identity.TwoFactorReauthenticationCommand{
		ChangeID: request.Params.IdempotencyKey,
		Password: *request.Body.Password, SecondFactorCode: *request.Body.SecondFactorCode,
	})
	if err != nil {
		problem, status, mapped := twoFactorProblem(ctx, err)
		if !mapped {
			return nil, err
		}
		switch status {
		case http.StatusBadRequest:
			return generated.RotateMyTOTPRecoveryCodes400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
		case http.StatusUnauthorized:
			return generated.RotateMyTOTPRecoveryCodes401ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusForbidden:
			return generated.RotateMyTOTPRecoveryCodes403ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusConflict:
			return generated.RotateMyTOTPRecoveryCodes409ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusGone:
			return generated.RotateMyTOTPRecoveryCodes410ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusTooManyRequests:
			return generated.RotateMyTOTPRecoveryCodes429ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return generated.RotateMyTOTPRecoveryCodesdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: status}, nil
		}
	}
	return generated.RotateMyTOTPRecoveryCodes200JSONResponse{
		ChangeId: result.ChangeID, ChangedAt: result.ChangedAt,
		RecoveryCodes: append(generated.RecoveryCodeList(nil), result.RecoveryCodes...),
	}, nil
}

func (h *Handler) DisableMyTOTP(ctx context.Context, request generated.DisableMyTOTPRequestObject) (generated.DisableMyTOTPResponseObject, error) {
	if sessionTokenFromContext(ctx) == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后关闭两步验证。")
		return generated.DisableMyTOTP401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if request.Body == nil || request.Body.Password == nil || request.Body.SecondFactorCode == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_two_factor_reauthentication", "重新验证信息无效", "请输入当前密码以及 TOTP 或一次性恢复码。")
		return generated.DisableMyTOTP400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	result, err := h.twoFactor.Disable(ctx, sessionTokenFromContext(ctx), request.Params.XCSRFToken, identity.TwoFactorReauthenticationCommand{
		ChangeID: request.Params.IdempotencyKey,
		Password: *request.Body.Password, SecondFactorCode: *request.Body.SecondFactorCode,
	})
	if err != nil {
		problem, status, mapped := twoFactorProblem(ctx, err)
		if !mapped {
			return nil, err
		}
		switch status {
		case http.StatusBadRequest:
			return generated.DisableMyTOTP400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
		case http.StatusUnauthorized:
			return generated.DisableMyTOTP401ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusForbidden:
			return generated.DisableMyTOTP403ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusConflict:
			return generated.DisableMyTOTP409ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusTooManyRequests:
			return generated.DisableMyTOTP429ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return generated.DisableMyTOTPdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: status}, nil
		}
	}
	return generated.DisableMyTOTP200JSONResponse{ChangeId: result.ChangeID, ChangedAt: result.ChangedAt}, nil
}

// twoFactorProblem is the shared domain-to-HTTP policy for factor lifecycle
// commands. Endpoint-specific wrappers remain generated from OpenAPI, while
// this function keeps security-sensitive failures consistent across them.
func twoFactorProblem(ctx context.Context, err error) (generated.Problem, int, bool) {
	switch {
	case errors.Is(err, identity.ErrInvalidInput):
		return newProblemFromContext(ctx, http.StatusBadRequest, "invalid_two_factor_request", "两步验证信息无效", "请检查密码、验证码和请求内容。"), http.StatusBadRequest, true
	case errors.Is(err, identity.ErrSessionNotFound):
		return newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后继续。"), http.StatusUnauthorized, true
	case errors.Is(err, identity.ErrTwoFactorVerification):
		return newProblemFromContext(ctx, http.StatusUnauthorized, "two_factor_verification_failed", "重新验证失败", "密码或验证码无效，请检查后重试。"), http.StatusUnauthorized, true
	case errors.Is(err, identity.ErrInvalidCSRF):
		return newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。"), http.StatusForbidden, true
	case errors.Is(err, authz.ErrForbidden):
		return newProblemFromContext(ctx, http.StatusForbidden, "two_factor_forbidden", "无法管理两步验证", "当前账户没有有效的本人管理权限。"), http.StatusForbidden, true
	case errors.Is(err, identity.ErrTwoFactorAlreadyEnabled):
		return newProblemFromContext(ctx, http.StatusConflict, "two_factor_already_enabled", "两步验证已经启用", "请刷新账户安全状态。"), http.StatusConflict, true
	case errors.Is(err, identity.ErrTwoFactorNotEnabled):
		return newProblemFromContext(ctx, http.StatusConflict, "two_factor_not_enabled", "两步验证尚未启用", "请刷新账户安全状态。"), http.StatusConflict, true
	case errors.Is(err, identity.ErrTwoFactorIdempotencyConflict):
		return newProblemFromContext(ctx, http.StatusConflict, "idempotency_conflict", "本次变更内容已经变化", "请关闭对话框并重新开始操作。"), http.StatusConflict, true
	case errors.Is(err, identity.ErrTwoFactorEnrollmentNotFound):
		return newProblemFromContext(ctx, http.StatusNotFound, "two_factor_enrollment_unavailable", "登记已不可用", "登记可能已过期或被新的登记替代，请重新开始。"), http.StatusNotFound, true
	case errors.Is(err, identity.ErrRecoveryCodeBundleUnavailable):
		return newProblemFromContext(ctx, http.StatusGone, "recovery_codes_unavailable", "恢复码已无法再次显示", "请在账户安全页重新生成一组恢复码。"), http.StatusGone, true
	case errors.Is(err, identity.ErrLoginThrottled):
		return newProblemFromContext(ctx, http.StatusTooManyRequests, "two_factor_throttled", "验证尝试过于频繁", "请短暂等待后重试。"), http.StatusTooManyRequests, true
	case errors.Is(err, identity.ErrTwoFactorServiceUnavailable):
		return newProblemFromContext(ctx, http.StatusServiceUnavailable, "two_factor_unavailable", "两步验证服务暂时不可用", "请保留当前页面并稍后重试。"), http.StatusServiceUnavailable, true
	default:
		return generated.Problem{}, 0, false
	}
}

func (h *Handler) ListMyWebSessions(ctx context.Context, _ generated.ListMyWebSessionsRequestObject) (generated.ListMyWebSessionsResponseObject, error) {
	cookieToken := sessionTokenFromContext(ctx)
	if cookieToken == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请先建立有效的 Web 会话。")
		return generated.ListMyWebSessions401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	items, err := h.sessionSecurity.ListSessions(ctx, cookieToken)
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后查看设备会话。")
		return generated.ListMyWebSessions401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "session_security_forbidden", "无法查看设备会话", "当前账户没有有效的本人会话读取权限。")
		return generated.ListMyWebSessions403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	dto := make([]generated.UserWebSession, 0, len(items))
	for _, item := range items {
		dto = append(dto, generated.UserWebSession{
			Id: item.ID, Current: item.Current, CreatedAt: item.CreatedAt,
			LastSeenAt: item.LastSeenAt, ExpiresAt: item.ExpiresAt,
		})
	}
	return generated.ListMyWebSessions200JSONResponse{Items: dto}, nil
}

func (h *Handler) DeleteMyOtherWebSessions(ctx context.Context, request generated.DeleteMyOtherWebSessionsRequestObject) (generated.DeleteMyOtherWebSessionsResponseObject, error) {
	cookieToken := sessionTokenFromContext(ctx)
	if cookieToken == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "当前没有可用于授权撤销操作的会话。")
		return generated.DeleteMyOtherWebSessions401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	_, err := h.sessionSecurity.RevokeOtherSessions(ctx, cookieToken, request.Params.XCSRFToken)
	if response := deleteOtherSessionsProblem(ctx, err); response != nil {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	return generated.DeleteMyOtherWebSessions204Response{}, nil
}

func (h *Handler) DeleteMyWebSession(ctx context.Context, request generated.DeleteMyWebSessionRequestObject) (generated.DeleteMyWebSessionResponseObject, error) {
	cookieToken := sessionTokenFromContext(ctx)
	if cookieToken == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "当前没有可用于授权撤销操作的会话。")
		return generated.DeleteMyWebSession401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	result, err := h.sessionSecurity.RevokeSession(ctx, cookieToken, request.Params.XCSRFToken, request.SessionId)
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后管理设备会话。")
		return generated.DeleteMyWebSession401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.DeleteMyWebSession403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "session_revoke_forbidden", "无法撤销设备会话", "当前账户没有有效的本人会话撤销权限。")
		return generated.DeleteMyWebSession403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	response := generated.DeleteMyWebSession204Response{}
	if result.CurrentSessionRevoked {
		expiredCookie := expiredSessionCookie(h.sessionCookie)
		cookie := expiredCookie.String()
		response.Headers.SetCookie = &cookie
	}
	return response, nil
}

func deleteOtherSessionsProblem(ctx context.Context, err error) generated.DeleteMyOtherWebSessionsResponseObject {
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后管理设备会话。")
		return generated.DeleteMyOtherWebSessions401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.DeleteMyOtherWebSessions403ApplicationProblemPlusJSONResponse(problem)
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "session_revoke_forbidden", "无法撤销其他设备会话", "当前账户没有有效的本人会话撤销权限。")
		return generated.DeleteMyOtherWebSessions403ApplicationProblemPlusJSONResponse(problem)
	default:
		return nil
	}
}

// BeginStaffElevation starts a server-side, one-time WebAuthn assertion using
// only the ordinary Web session. It never creates or returns staff authority.
func (h *Handler) BeginStaffElevation(ctx context.Context, request generated.BeginStaffElevationRequestObject) (generated.BeginStaffElevationResponseObject, error) {
	webCookie := sessionTokenFromContext(ctx)
	if webCookie == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请先建立普通 Web 会话。")
		return generated.BeginStaffElevation401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	result, err := h.staffIdentity.BeginElevation(ctx, webCookie, request.Params.XCSRFToken)
	if errors.Is(err, identity.ErrSessionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后再进入后台。")
		return generated.BeginStaffElevation401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, identity.ErrInvalidCSRF) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.BeginStaffElevation403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "staff_elevation_denied", "无法进入后台", "当前账号没有有效的后台任期或授权。")
		return generated.BeginStaffElevation403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrStaffCredentialRequired) {
		problem := newProblemFromContext(ctx, http.StatusConflict, "staff_webauthn_not_enrolled", "尚未配置后台安全凭据", "请通过受控 bootstrap 或迁移流程配置 staff WebAuthn 凭据。")
		return generated.BeginStaffElevation409ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	var publicKey generated.WebAuthnRequestOptions
	if err := json.Unmarshal(result.PublicKey, &publicKey); err != nil {
		return nil, errors.New("staff WebAuthn options do not match the HTTP contract")
	}
	return generated.BeginStaffElevation200JSONResponse{
		ChallengeId: result.ChallengeID,
		ExpiresAt:   result.ExpiresAt,
		PublicKey:   publicKey,
	}, nil
}

// CompleteStaffElevation verifies the assertion and emits a second cookie
// whose Path prevents it from being sent to ordinary Web API routes.
func (h *Handler) CompleteStaffElevation(ctx context.Context, request generated.CompleteStaffElevationRequestObject) (generated.CompleteStaffElevationResponseObject, error) {
	webCookie := sessionTokenFromContext(ctx)
	if webCookie == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请先建立普通 Web 会话。")
		return generated.CompleteStaffElevation401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_webauthn_assertion", "安全凭据响应无效", "请重新发起后台身份验证。")
		return generated.CompleteStaffElevation400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	assertion, err := json.Marshal(request.Body.Credential)
	if err != nil {
		return nil, err
	}
	session, err := h.staffIdentity.CompleteElevation(ctx, webCookie, request.Params.XCSRFToken, identity.CompleteStaffElevationInput{
		ChallengeID: request.Body.ChallengeId,
		Assertion:   assertion,
	})
	if errors.Is(err, identity.ErrInvalidInput) {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_webauthn_assertion", "安全凭据响应无效", "请重新发起后台身份验证。")
		return generated.CompleteStaffElevation400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, identity.ErrSessionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后再进入后台。")
		return generated.CompleteStaffElevation401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrStaffChallengeNotFound) || errors.Is(err, identity.ErrStaffWebAuthnVerification) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "staff_webauthn_failed", "安全凭据验证失败", "Assertion 已失效，请重新发起后台身份验证。")
		return generated.CompleteStaffElevation401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrInvalidCSRF) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.CompleteStaffElevation403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "staff_elevation_denied", "无法进入后台", "当前账号没有有效的后台任期或授权。")
		return generated.CompleteStaffElevation403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrStaffCredentialRequired) || errors.Is(err, identity.ErrStaffAuthenticatorCloneDetected) {
		problem := newProblemFromContext(ctx, http.StatusConflict, "staff_credential_unavailable", "后台安全凭据不可用", "凭据已撤销、发生计数器异常或需要重新配置。")
		return generated.CompleteStaffElevation409ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	staffCookie := newSessionCookie(h.staffSessionCookie, session.CookieToken, session.CreatedAt, session.ExpiresAt)
	cookie := staffCookie.String()
	return generated.CompleteStaffElevation200JSONResponse{
		Body:    staffSessionDTO(session),
		Headers: generated.CompleteStaffElevation200ResponseHeaders{SetCookie: &cookie},
	}, nil
}

// GetStaffSession prefers an optional passkey session, then falls back to the
// current account session when that account has the explicit site_admin role.
func (h *Handler) GetStaffSession(ctx context.Context, _ generated.GetStaffSessionRequestObject) (generated.GetStaffSessionResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if problem != nil && (problem.Status == http.StatusUnauthorized || problem.Status == http.StatusForbidden) {
		return generated.GetStaffSession204Response{}, nil
	}
	if err != nil {
		return nil, err
	}
	return generated.GetStaffSession200JSONResponse(staffSessionDTO(session)), nil
}

// DeleteStaffSession remains available after authority loss because reducing
// privilege is authenticated by the staff token and its bound CSRF value.
func (h *Handler) DeleteStaffSession(ctx context.Context, request generated.DeleteStaffSessionRequestObject) (generated.DeleteStaffSessionResponseObject, error) {
	staffCookie := staffSessionTokenFromContext(ctx)
	if staffCookie == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "staff_session_required", "需要后台会话", "当前没有可撤销的后台会话。")
		return generated.DeleteStaffSession401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	err := h.staffIdentity.Logout(ctx, staffCookie, request.Params.XCSRFToken)
	if errors.Is(err, identity.ErrStaffSessionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "staff_session_required", "后台会话已经失效", "请重新完成 WebAuthn 验证。")
		return generated.DeleteStaffSession401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, identity.ErrInvalidCSRF) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.DeleteStaffSession403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	expiredCookie := expiredSessionCookie(h.staffSessionCookie)
	cookie := expiredCookie.String()
	return generated.DeleteStaffSession204Response{
		Headers: generated.DeleteStaffSession204ResponseHeaders{SetCookie: &cookie},
	}, nil
}

// GetSiteInfo implements generated.StrictServerInterface.
func (h *Handler) GetSiteInfo(ctx context.Context, _ generated.GetSiteInfoRequestObject) (generated.GetSiteInfoResponseObject, error) {
	result, err := h.catalog.GetSiteInfo(ctx)
	if err != nil {
		return nil, err
	}
	registrationPolicy, err := h.registration.PublicPolicy(ctx)
	if err != nil {
		return nil, err
	}

	return generated.GetSiteInfo200JSONResponse{
		Name: result.Name, Description: result.Description,
		RegistrationMode:                  generated.RegistrationMode(registrationPolicy.Mode),
		RegistrationUsernameMinCharacters: registrationPolicy.UsernameMinCharacters,
		RegistrationUsernameMaxCharacters: registrationPolicy.UsernameMaxCharacters,
		RegistrationEmailDomainMode:       generated.EmailDomainMode(registrationPolicy.EmailDomainMode),
		HumanVerification: generated.HumanVerificationPublicConfig{
			Provider:                generated.HumanVerificationProvider(registrationPolicy.HumanVerificationProvider),
			SiteKey:                 registrationPolicy.HumanVerificationSiteKey,
			RegistrationEnabled:     registrationPolicy.HumanVerificationRegistrationEnabled,
			LoginEnabled:            registrationPolicy.HumanVerificationLoginEnabled,
			PasswordRecoveryEnabled: registrationPolicy.HumanVerificationPasswordRecoveryEnabled,
		},
		OnlineUsers:            result.OnlineUsers,
		DefaultTorrentView:     generated.SiteInfoDefaultTorrentView(result.DefaultTorrentView),
		ShowLatestAnnouncement: result.ShowLatestAnnouncement,
		CustomNavigationItems:  customNavigationItemsDTO(result.CustomNavigationItems),
	}, nil
}

// GetLatestAnnouncement implements generated.StrictServerInterface.
func (h *Handler) GetLatestAnnouncement(ctx context.Context, _ generated.GetLatestAnnouncementRequestObject) (generated.GetLatestAnnouncementResponseObject, error) {
	result, err := h.catalog.GetLatestAnnouncement(ctx)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return generated.GetLatestAnnouncement204Response{}, nil
	}

	return generated.GetLatestAnnouncement200JSONResponse{
		Id:          result.ID,
		Title:       result.Title,
		Summary:     result.Summary,
		PublishedAt: result.PublishedAt,
	}, nil
}

// ListAnnouncements returns only public revision summaries. Editorial state is
// intentionally absent: the catalog service and PostgreSQL projection make
// drafts, future-only schedules and withdrawals indistinguishable from absence.
func (h *Handler) ListAnnouncements(ctx context.Context, request generated.ListAnnouncementsRequestObject) (generated.ListAnnouncementsResponseObject, error) {
	limit, offset := announcementPageParams(request.Params.Limit, request.Params.Offset)
	page, err := h.catalog.ListAnnouncements(ctx, limit, offset)
	if errors.Is(err, catalog.ErrInvalidAnnouncementPage) {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_announcement_page", "公告分页参数无效", "limit 必须位于 1 到 50 之间，offset 必须位于 0 到 1000000 之间。")
		return generated.ListAnnouncements400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]generated.AnnouncementSummary, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, announcementSummaryDTO(item))
	}
	return generated.ListAnnouncements200JSONResponse{
		Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset,
	}, nil
}

// GetAnnouncement returns one published announcement without exposing draft or
// scheduled editorial state through distinguishable responses.
func (h *Handler) GetAnnouncement(ctx context.Context, request generated.GetAnnouncementRequestObject) (generated.GetAnnouncementResponseObject, error) {
	result, err := h.catalog.GetAnnouncement(ctx, request.AnnouncementId)
	if errors.Is(err, catalog.ErrAnnouncementNotFound) {
		problem := newProblemFromContext(ctx, http.StatusNotFound, "announcement_not_found", "公告不可用", "该公告不存在或尚未公开。")
		return generated.GetAnnouncement404ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return generated.GetAnnouncement200JSONResponse{
		Id: result.ID, Title: result.Title, Summary: result.Summary, Body: result.Body,
		BodyFormat: generated.AnnouncementBodyFormat(result.BodyFormat), Version: result.Version,
		PublishedAt: result.PublishedAt, UpdatedAt: result.UpdatedAt,
	}, nil
}

func announcementSummaryDTO(value catalog.AnnouncementSummary) generated.AnnouncementSummary {
	return generated.AnnouncementSummary{
		Id: value.ID, Title: value.Title, Summary: value.Summary, PublishedAt: value.PublishedAt,
	}
}

func announcementPageParams(limit *generated.AnnouncementLimitQueryParameter, offset *generated.AnnouncementOffsetQueryParameter) (int, int) {
	resolvedLimit, resolvedOffset := 20, 0
	if limit != nil {
		resolvedLimit = int(*limit)
	}
	if offset != nil {
		resolvedOffset = int(*offset)
	}
	return resolvedLimit, resolvedOffset
}

// ListCategories implements generated.StrictServerInterface.
func (h *Handler) ListCategories(ctx context.Context, _ generated.ListCategoriesRequestObject) (generated.ListCategoriesResponseObject, error) {
	result, err := h.catalog.ListCategories(ctx)
	if err != nil {
		return nil, err
	}

	response := make(generated.ListCategories200JSONResponse, 0, len(result))
	for _, category := range result {
		response = append(response, generated.CategoryWithCount{
			Id: category.ID, Name: category.Name, TorrentCount: int64(category.TorrentCount),
		})
	}
	return response, nil
}

// ListCategoryFacets exposes only enabled, category-scoped controlled values.
// The client uses this projection to compose the upload form; final writes
// still re-resolve every selection inside the upload transaction.
func (h *Handler) ListCategoryFacets(ctx context.Context, request generated.ListCategoryFacetsRequestObject) (generated.ListCategoryFacetsResponseObject, error) {
	result, err := h.catalog.ListCategoryFacets(ctx, request.CategoryId)
	if errors.Is(err, catalog.ErrCategoryNotFound) {
		problem := newProblemFromContext(ctx, http.StatusNotFound, "category_not_found", "分类不存在", "所选分类当前不可用，请重新选择。")
		return generated.ListCategoryFacets404ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if err != nil {
		return nil, err
	}

	response := make(generated.CategoryFacetList, 0, len(result))
	for _, facet := range result {
		options := make([]generated.CategoryFacetOption, 0, len(facet.Options))
		for _, option := range facet.Options {
			options = append(options, generated.CategoryFacetOption{Key: option.Key, Label: option.Label})
		}
		item := generated.CategoryFacet{
			Id: facet.ID, Name: facet.Name,
			SelectionMode: generated.CategoryFacetSelectionMode(facet.SelectionMode),
			Required:      facet.Required, Options: options,
		}
		if facet.RequirementGroup != "" {
			item.RequirementGroup = &facet.RequirementGroup
		}
		response = append(response, item)
	}
	return generated.ListCategoryFacets200JSONResponse(response), nil
}

// ListTorrents implements generated.StrictServerInterface.
func (h *Handler) ListTorrents(ctx context.Context, request generated.ListTorrentsRequestObject) (generated.ListTorrentsResponseObject, error) {
	query := ""
	if request.Params.Query != nil {
		query = *request.Params.Query
	}
	var searchScope *catalog.TorrentSearchScope
	if request.Params.SearchScope != nil {
		value := catalog.TorrentSearchScope(*request.Params.SearchScope)
		searchScope = &value
	}
	categoryID := ""
	if request.Params.CategoryId != nil {
		categoryID = *request.Params.CategoryId
	}
	var promotion *catalog.Promotion
	if request.Params.Promotion != nil {
		value := catalog.Promotion(*request.Params.Promotion)
		promotion = &value
	}
	var sortOrder *catalog.TorrentSort
	if request.Params.Sort != nil {
		value := catalog.TorrentSort(*request.Params.Sort)
		sortOrder = &value
	}

	result, err := h.catalog.ListTorrents(ctx, catalog.TorrentListRequest{
		Limit: request.Params.Limit, Offset: request.Params.Offset, Query: query,
		SearchScope: searchScope, CategoryID: categoryID, Promotion: promotion,
		Sort: sortOrder,
	})
	if errors.Is(err, catalog.ErrInvalidLimit) || errors.Is(err, catalog.ErrInvalidQuery) ||
		errors.Is(err, catalog.ErrInvalidTorrentPage) || errors.Is(err, catalog.ErrInvalidTorrentFilter) {
		problem := generated.Problem{
			Type:      "about:blank",
			Title:     "请求参数无效",
			Status:    http.StatusBadRequest,
			Code:      "invalid_query",
			RequestId: requestIDFromContext(ctx),
		}
		return generated.ListTorrentsdefaultApplicationProblemPlusJSONResponse{
			Body:       problem,
			StatusCode: http.StatusBadRequest,
		}, nil
	}
	if err != nil {
		return nil, err
	}

	items := make([]generated.TorrentSummary, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, torrentSummaryDTO(item))
	}

	return generated.ListTorrents200JSONResponse{
		Items:  items,
		Total:  result.Total,
		Limit:  result.Limit,
		Offset: result.Offset,
	}, nil
}

func torrentSummaryDTO(item catalog.TorrentSummary) generated.TorrentSummary {
	return generated.TorrentSummary{
		Id:              item.ID,
		Name:            item.Name,
		Subtitle:        item.Subtitle,
		Category:        generated.Category{Id: item.Category.ID, Name: item.Category.Name},
		SizeBytes:       item.SizeBytes,
		Promotion:       generated.TorrentSummaryPromotion(item.Promotion),
		StickyUntil:     item.StickyUntil,
		UploadedAt:      item.UploadedAt,
		Seeders:         item.Swarm.Seeders,
		Leechers:        item.Swarm.Leechers,
		Completed:       item.Swarm.Completed,
		SwarmObservedAt: item.Swarm.ObservedAt,
		SwarmStale:      item.SwarmStale,
	}
}

// GetTorrentSwarm returns the latest complete catalog snapshot. This adapter
// has no Tracker client, so a slow or unavailable data plane cannot block the
// public torrent detail request.
func (h *Handler) GetTorrentSwarm(ctx context.Context, request generated.GetTorrentSwarmRequestObject) (generated.GetTorrentSwarmResponseObject, error) {
	result, err := h.catalog.GetTorrentSwarm(ctx, request.TorrentId)
	if errors.Is(err, catalog.ErrTorrentNotFound) {
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_not_found", "种子不可用", "该种子不存在、尚未发布或已经停止公开访问。")
		return generated.GetTorrentSwarm404ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	var observedAt *time.Time
	if !result.ObservedAt.IsZero() {
		value := result.ObservedAt
		observedAt = &value
	}
	return generated.GetTorrentSwarm200JSONResponse{
		TorrentId: request.TorrentId,
		Seeders:   result.Seeders, Leechers: result.Leechers, Completed: result.Completed,
		ObservedAt: observedAt, Stale: result.Stale,
		Confidence: generated.TorrentSwarmOverviewConfidence(result.Confidence),
	}, nil
}

func staffActor(session identity.StaffSession) authz.StaffActor {
	return authz.StaffActor{
		Subject:            authz.Subject{ID: session.User.ID, Status: authz.SubjectActive},
		MFAAuthenticatedAt: session.WebAuthnAuthenticatedAt,
	}
}

func staffAuthenticationTime(session identity.StaffSession) time.Time {
	if !session.AuthenticatedAt.IsZero() {
		return session.AuthenticatedAt
	}
	return session.WebAuthnAuthenticatedAt
}

func capabilitySetDTO(result authz.CapabilitySet) generated.CapabilityList {
	items := make([]generated.Capability, 0, len(result.Items))
	for _, capability := range result.Items {
		items = append(items, generated.Capability{
			Action:      generated.CapabilityAction(capability.Action),
			Description: capability.Description,
			ExpiresAt:   capability.ExpiresAt,
			Scope: generated.CapabilityScope{
				Type: generated.CapabilityScopeType(capability.Scope.Type),
				Id:   capability.Scope.ID,
			},
		})
	}
	return generated.CapabilityList{PolicyVersion: result.PolicyVersion, Items: items}
}

func trafficOverviewDTO(result traffic.Overview) generated.TrafficOverview {
	entries := make([]generated.TrafficEntry, 0, len(result.Entries))
	for _, entry := range result.Entries {
		segments := make([]generated.TrafficExplanationSegment, 0, len(entry.Explanation.Segments))
		for _, segment := range entry.Explanation.Segments {
			segments = append(segments, generated.TrafficExplanationSegment{
				StartedAt: segment.StartsAt, EndedAt: segment.EndsAt,
				RawUploadedBytes:       strconv.FormatInt(segment.RawUploaded, 10),
				RawDownloadedBytes:     strconv.FormatInt(segment.RawDownloaded, 10),
				CreditedUploadedBytes:  strconv.FormatInt(segment.CreditedUploaded, 10),
				ChargedDownloadedBytes: strconv.FormatInt(segment.ChargedDownloaded, 10),
			})
		}
		entries = append(entries, generated.TrafficEntry{
			Id:                     entry.ID,
			Torrent:                generated.TrafficTorrentReference{Id: entry.TorrentID, Title: entry.TorrentTitle},
			IntervalStartedAt:      entry.IntervalStartedAt,
			IntervalEndedAt:        entry.IntervalEndedAt,
			RawUploadedBytes:       strconv.FormatInt(entry.RawUploaded, 10),
			RawDownloadedBytes:     strconv.FormatInt(entry.RawDownloaded, 10),
			CreditedUploadedBytes:  strconv.FormatInt(entry.CreditedUploaded, 10),
			ChargedDownloadedBytes: strconv.FormatInt(entry.ChargedDownloaded, 10),
			Explanation: generated.TrafficSettlementExplanation{
				Status:       generated.TrafficExplanationStatus(entry.Explanation.Status),
				SegmentCount: strconv.FormatInt(int64(entry.Explanation.SegmentCount), 10),
				Segments:     segments,
			},
			SettledAt: entry.SettledAt,
		})
	}
	torrentActivity := make([]generated.TrafficTorrentActivity, 0, len(result.TorrentActivity))
	for _, item := range result.TorrentActivity {
		torrentActivity = append(torrentActivity, generated.TrafficTorrentActivity{
			Torrent:             generated.TrafficTorrentReference{Id: item.TorrentID, Title: item.TorrentTitle},
			TotalSizeBytes:      item.TotalSizeBytes,
			RawUploadedBytes:    generated.TrafficByteCount(strconv.FormatInt(item.RawUploaded, 10)),
			RawDownloadedBytes:  generated.TrafficByteCount(strconv.FormatInt(item.RawDownloaded, 10)),
			ProgressBasisPoints: item.ProgressBasisPts, Completed: item.Completed,
			LastSettledAt: item.LastSettledAt,
		})
	}
	return generated.TrafficOverview{
		Totals: generated.TrafficTotals{
			RawUploadedBytes:       strconv.FormatInt(result.Totals.RawUploaded, 10),
			RawDownloadedBytes:     strconv.FormatInt(result.Totals.RawDownloaded, 10),
			CreditedUploadedBytes:  strconv.FormatInt(result.Totals.CreditedUploaded, 10),
			ChargedDownloadedBytes: strconv.FormatInt(result.Totals.ChargedDownloaded, 10),
			EntryCount:             strconv.FormatInt(result.Totals.EntryCount, 10),
			LastSettledAt:          result.Totals.LastSettledAt,
			ProjectionUpdatedAt:    result.Totals.ProjectionUpdatedAt,
		},
		Entries: entries, TorrentActivity: torrentActivity,
	}
}

func hnrPageDTO(result traffic.HNRPage) (generated.HNRPage, error) {
	items := make([]generated.HNREntry, 0, len(result.Items))
	for _, entry := range result.Items {
		var satisfiedBy *generated.HNRSatisfiedBy
		if entry.SatisfiedBy != nil {
			value := generated.HNRSatisfiedBy(*entry.SatisfiedBy)
			satisfiedBy = &value
		}
		var appeal *generated.MyHNRAppeal
		if entry.Appeal != nil {
			value := myHNRAppealFromProjectionDTO(*entry.Appeal)
			appeal = &value
		}
		items = append(items, generated.HNREntry{
			Id:          entry.ObligationID,
			Torrent:     generated.TrafficTorrentReference{Id: entry.TorrentID, Title: entry.TorrentTitle},
			CompletedAt: entry.CompletedAt, Status: generated.HNRStatus(entry.Status),
			SeededSeconds:            strconv.FormatInt(entry.SeededSeconds, 10),
			RequiredSeedSeconds:      strconv.FormatInt(entry.RequiredSeedSeconds, 10),
			RawUploadedBytes:         strconv.FormatInt(entry.RawUploaded, 10),
			RawDownloadedBytes:       strconv.FormatInt(entry.RawDownloaded, 10),
			RawRatioBasisPoints:      strconv.FormatInt(entry.RawRatioBasisPoints, 10),
			RequiredRatioBasisPoints: strconv.FormatInt(entry.RequiredRatioBasisPoints, 10),
			AssessmentDueAt:          entry.AssessmentDueAt, GraceEndsAt: entry.GraceEndsAt,
			SatisfiedBy: satisfiedBy, SatisfiedAt: entry.SatisfiedAt, UpdatedAt: entry.UpdatedAt,
			Appeal: appeal, CanAppeal: entry.CanAppeal,
		})
	}
	var nextCursor *string
	if result.NextCursor != nil {
		encoded, err := traffic.EncodeHNRCursor(*result.NextCursor)
		if err != nil {
			return generated.HNRPage{}, traffic.ErrInvariant
		}
		nextCursor = &encoded
	}
	return generated.HNRPage{
		AsOf: result.AsOf,
		Summary: generated.HNRSummary{
			Total: strconv.FormatInt(result.Summary.Total, 10), Tracking: strconv.FormatInt(result.Summary.Tracking, 10),
			Grace: strconv.FormatInt(result.Summary.Grace, 10), Overdue: strconv.FormatInt(result.Summary.Overdue, 10),
			Satisfied: strconv.FormatInt(result.Summary.Satisfied, 10), Exempt: strconv.FormatInt(result.Summary.Exempt, 10),
		},
		Items: items, NextCursor: nextCursor,
	}, nil
}

func managedCategoryDTO(category catalog.ManagedCategory) generated.ManagedCategory {
	facets := make([]generated.ManagedCategoryFacet, 0, len(category.Facets))
	for _, facet := range category.Facets {
		facets = append(facets, managedCategoryFacetDTO(facet))
	}
	return generated.ManagedCategory{
		Id: category.ID, Name: category.Name, DisplayOrder: category.DisplayOrder,
		Enabled: category.Enabled, Version: category.Version, TorrentCount: category.TorrentCount,
		CreatedAt: category.CreatedAt, UpdatedAt: category.UpdatedAt, Facets: facets,
	}
}

func managedCategoryFacetDTO(facet catalog.ManagedCategoryFacet) generated.ManagedCategoryFacet {
	options := make([]generated.ManagedCategoryFacetOption, 0, len(facet.Options))
	for _, option := range facet.Options {
		options = append(options, managedCategoryFacetOptionDTO(option))
	}
	result := generated.ManagedCategoryFacet{
		Id: facet.ID, Name: facet.Name, SelectionMode: generated.CategoryFacetSelectionMode(facet.SelectionMode),
		Required: facet.Required, DisplayOrder: facet.DisplayOrder, Enabled: facet.Enabled,
		Version: facet.Version, TorrentCount: facet.TorrentCount,
		CreatedAt: facet.CreatedAt, UpdatedAt: facet.UpdatedAt, Options: options,
	}
	if facet.RequirementGroup != "" {
		result.RequirementGroup = &facet.RequirementGroup
	}
	return result
}

func managedCategoryFacetOptionDTO(option catalog.ManagedCategoryFacetOption) generated.ManagedCategoryFacetOption {
	return generated.ManagedCategoryFacetOption{
		Key: option.Key, Label: option.Label, CanonicalLabel: option.CanonicalLabel,
		DisplayOrder: option.DisplayOrder, Enabled: option.Enabled, Version: option.Version,
		TorrentCount: option.TorrentCount, CreatedAt: option.CreatedAt, UpdatedAt: option.UpdatedAt,
	}
}

func siteDisplaySettingsDTO(settings catalog.SiteDisplaySettings) generated.SiteDisplaySettings {
	return generated.SiteDisplaySettings{
		Name: settings.Name, Description: settings.Description,
		TorrentFilenamePrefix:  settings.TorrentFilenamePrefix,
		DefaultTorrentView:     generated.SiteDisplaySettingsDefaultTorrentView(settings.DefaultTorrentView),
		ShowLatestAnnouncement: settings.ShowLatestAnnouncement,
		CustomNavigationItems:  customNavigationItemsDTO(settings.CustomNavigationItems),
		Version:                settings.Version, EffectiveAt: settings.EffectiveAt, UpdatedAt: settings.UpdatedAt,
	}
}

func customNavigationItemsDTO(items []catalog.CustomNavigationItem) []generated.CustomNavigationItem {
	result := make([]generated.CustomNavigationItem, 0, len(items))
	for _, item := range items {
		result = append(result, generated.CustomNavigationItem{
			Label: item.Label, Url: item.URL, OpenInNewTab: item.OpenInNewTab, Enabled: item.Enabled,
		})
	}
	return result
}

func customNavigationItemsInput(items []generated.CustomNavigationItem) []catalog.CustomNavigationItem {
	result := make([]catalog.CustomNavigationItem, 0, len(items))
	for _, item := range items {
		result = append(result, catalog.CustomNavigationItem{
			Label: item.Label, URL: item.Url, OpenInNewTab: item.OpenInNewTab, Enabled: item.Enabled,
		})
	}
	return result
}

func registrationPolicySettingsDTO(policy identity.RegistrationPolicy) generated.RegistrationPolicySettings {
	return generated.RegistrationPolicySettings{
		Mode:                                     generated.RegistrationMode(policy.Mode),
		MemberInvitesEnabled:                     policy.MemberInvitesEnabled,
		InviteValidDays:                          policy.InviteValidDays,
		MaxInvitesPerMember:                      policy.MaxInvitesPerMember,
		MinimumInviteAccountAgeDays:              policy.MinimumInviteAccountAgeDays,
		MinimumInviteLevel:                       policy.MinimumInviteLevel,
		UsernameMinCharacters:                    policy.UsernameMinCharacters,
		UsernameMaxCharacters:                    policy.UsernameMaxCharacters,
		ReservedUsernames:                        append(make([]string, 0, len(policy.ReservedUsernames)), policy.ReservedUsernames...),
		EmailDomainMode:                          generated.EmailDomainMode(policy.EmailDomainMode),
		EmailDomains:                             append(make([]string, 0, len(policy.EmailDomains)), policy.EmailDomains...),
		SessionValidHours:                        policy.SessionValidHours,
		RememberSessionValidHours:                policy.RememberSessionValidHours,
		HumanVerificationProvider:                generated.HumanVerificationProvider(policy.HumanVerificationProvider),
		HumanVerificationSiteKey:                 policy.HumanVerificationSiteKey,
		HumanVerificationRegistrationEnabled:     policy.HumanVerificationRegistrationEnabled,
		HumanVerificationLoginEnabled:            policy.HumanVerificationLoginEnabled,
		HumanVerificationPasswordRecoveryEnabled: policy.HumanVerificationPasswordRecoveryEnabled,
		HumanVerificationSecretConfigured:        valuePointer(policy.HumanVerificationSecretConfigured),
		Version:                                  policy.Version, UpdatedAt: policy.UpdatedAt,
	}
}

func valuePointer[T any](value T) *T { return &value }

func managedUserSummaryDTO(user identity.ManagedUserSummary) generated.ManagedUserSummary {
	return generated.ManagedUserSummary{
		Id: user.ID, NumericId: user.NumericID, Username: user.Username, DisplayName: user.DisplayName,
		Email: openapi_types.Email(user.Email), EmailVerified: user.EmailVerified,
		Banned: user.Banned, DownloadRestricted: user.DownloadRestricted,
		VipEnabled: user.VIPEnabled, VipActive: user.VIPActive, VipUntil: user.VIPUntil,
		Status: generated.ManagedUserStatus(user.Status), Version: user.Version,
		ActiveRestrictionCount: user.ActiveRestrictionCount,
		UploadedBytes:          strconv.FormatInt(user.UploadedBytes, 10),
		DownloadedBytes:        strconv.FormatInt(user.DownloadedBytes, 10),
		MagicBalance:           strconv.FormatInt(user.MagicBalance, 10),
		Level:                  int(user.Level),
		RoleNames:              append([]string(nil), user.RoleNames...),
		LastActiveAt:           user.LastActiveAt,
		CreatedAt:              user.CreatedAt, UpdatedAt: user.UpdatedAt,
	}
}

func managedUserDetailDTO(user identity.ManagedUserDetail) generated.ManagedUserDetail {
	restrictions := make([]generated.CurrentAccountRestriction, 0, len(user.ActiveRestrictions))
	for _, restriction := range user.ActiveRestrictions {
		restrictions = append(restrictions, generated.CurrentAccountRestriction{
			Id: restriction.ID, Kind: generated.CurrentAccountRestrictionKind(restriction.Kind),
			ReasonCode: restriction.ReasonCode, ReasonSummary: restriction.ReasonSummary,
			StartsAt: restriction.StartsAt, ExpiresAt: restriction.ExpiresAt,
			Version: restriction.Version,
		})
	}
	manualHistory := make([]generated.ManualDownloadRestrictionTransition, 0, len(user.ManualDownloadRestrictionHistory))
	for _, transition := range user.ManualDownloadRestrictionHistory {
		manualHistory = append(manualHistory, generated.ManualDownloadRestrictionTransition{
			Transition: generated.ManualDownloadRestrictionTransitionTransition(transition.Transition),
			Origin:     generated.ManualDownloadRestrictionOrigin(transition.Origin),
			ReasonCode: transition.ReasonCode, ReasonSummary: transition.ReasonSummary,
			StateVersion: transition.StateVersion, OccurredAt: transition.OccurredAt,
			ActorNumericId: transition.ActorNumericID, ActorUsername: transition.ActorUsername,
		})
	}
	manualState := generated.ManualDownloadRestrictionState{
		Active:        user.ManualDownloadRestriction.Active,
		Version:       user.ManualDownloadRestriction.Version,
		ReasonCode:    user.ManualDownloadRestriction.ReasonCode,
		ReasonSummary: user.ManualDownloadRestriction.ReasonSummary,
		StartedAt:     user.ManualDownloadRestriction.StartedAt,
	}
	if user.ManualDownloadRestriction.Origin != nil {
		origin := generated.ManualDownloadRestrictionOrigin(*user.ManualDownloadRestriction.Origin)
		manualState.Origin = &origin
	}
	var registrationMode *generated.ManagedUserDetailRegistrationMode
	if user.RegistrationMode != nil {
		value := generated.ManagedUserDetailRegistrationMode(*user.RegistrationMode)
		registrationMode = &value
	}
	var registrationState *generated.ManagedUserDetailRegistrationState
	if user.RegistrationState != nil {
		value := generated.ManagedUserDetailRegistrationState(*user.RegistrationState)
		registrationState = &value
	}
	vipHistory := make([]generated.VIPTransition, 0, len(user.VIPHistory))
	for _, transition := range user.VIPHistory {
		vipHistory = append(vipHistory, generated.VIPTransition{
			Transition:    generated.VIPTransitionTransition(transition.Transition),
			Origin:        generated.VIPTransitionOrigin(transition.Origin),
			ReasonSummary: transition.ReasonSummary, Enabled: transition.Enabled,
			Until: transition.Until, StateVersion: transition.StateVersion,
			OccurredAt: transition.OccurredAt, ActorNumericId: transition.ActorNumericID,
			ActorUsername: transition.ActorUsername,
		})
	}
	return generated.ManagedUserDetail{
		Id: user.ID, NumericId: user.NumericID, Username: user.Username, DisplayName: user.DisplayName,
		Email: openapi_types.Email(user.Email), EmailVerified: user.EmailVerified,
		Banned: user.Banned, DownloadRestricted: user.DownloadRestricted,
		VipEnabled: user.VIPEnabled, VipActive: user.VIPActive, VipUntil: user.VIPUntil,
		Status: generated.ManagedUserStatus(user.Status), Version: user.Version,
		ActiveRestrictionCount: user.ActiveRestrictionCount,
		UploadedBytes:          strconv.FormatInt(user.UploadedBytes, 10),
		DownloadedBytes:        strconv.FormatInt(user.DownloadedBytes, 10),
		MagicBalance:           strconv.FormatInt(user.MagicBalance, 10),
		Level:                  int(user.Level),
		RoleNames:              append([]string(nil), user.RoleNames...),
		LastActiveAt:           user.LastActiveAt,
		CreatedAt:              user.CreatedAt, UpdatedAt: user.UpdatedAt,
		Experience: user.Experience, RemainingInvites: int(user.RemainingInvites),
		DonationAmount:            user.DonationAmount,
		SubmittedTorrentCount:     user.SubmittedTorrentCount,
		PublishedTorrentCount:     user.PublishedTorrentCount,
		PendingReviewTorrentCount: user.PendingReviewTorrentCount,
		DirectInviteCount:         user.DirectInviteCount,
		InviterNumericId:          user.InviterNumericID, InviterUsername: user.InviterUsername,
		RegistrationMode: registrationMode, RegistrationState: registrationState,
		ActiveRestrictions:               restrictions,
		ManualDownloadRestriction:        manualState,
		ManualDownloadRestrictionHistory: manualHistory,
		VipState: generated.VIPState{
			Enabled: user.VIPState.Enabled, Active: user.VIPState.Active,
			Until: user.VIPState.Until, Version: user.VIPState.Version,
		},
		VipHistory: vipHistory,
	}
}

func grantAdministrationOverviewDTO(result authz.GrantAdministrationOverview) generated.GrantAdministrationOverview {
	grants := make([]generated.GrantAdministrationGrant, 0, len(result.Grants))
	for _, grant := range result.Grants {
		grants = append(grants, generated.GrantAdministrationGrant{
			Id:                 grant.ID,
			SubjectId:          grant.SubjectID,
			SubjectUsername:    grant.SubjectUsername,
			SubjectDisplayName: grant.SubjectDisplayName,
			RoleId:             grant.RoleID,
			RoleName:           grant.RoleName,
			MandateId:          grant.MandateID,
			MandateStatus:      generated.GrantAdministrationGrantMandateStatus(grant.MandateStatus),
			Scope: generated.GrantScope{
				Type: generated.GrantScopeType(grant.Scope.Type),
				Id:   grant.Scope.ID,
			},
			ValidFrom:  grant.ValidFrom,
			ValidUntil: grant.ValidUntil,
			Version:    grant.Version,
			RevokedAt:  grant.RevokedAt,
		})
	}
	requests := make([]generated.GrantRevocationRequest, 0, len(result.Requests))
	for _, request := range result.Requests {
		requests = append(requests, grantRevocationRequestDTO(request))
	}
	return generated.GrantAdministrationOverview{
		PolicyVersion: result.PolicyVersion,
		Grants:        grants,
		Requests:      requests,
	}
}

func grantRevocationRequestDTO(request authz.GrantRevocationRequest) generated.GrantRevocationRequest {
	reviews := make([]generated.GrantRevocationReview, 0, len(request.Reviews))
	for _, review := range request.Reviews {
		reviews = append(reviews, generated.GrantRevocationReview{
			Id:         review.ID,
			ReviewerId: review.ReviewerID,
			Domain:     generated.GrantRevocationReviewDomain(review.Domain),
			Decision:   generated.GrantRevocationReviewDecision(review.Decision),
			Reason:     review.Reason,
			CreatedAt:  review.CreatedAt,
		})
	}
	var resultingVersion *int64
	if request.ResultingGrantVersion > 0 {
		value := request.ResultingGrantVersion
		resultingVersion = &value
	}
	return generated.GrantRevocationRequest{
		Id:                    request.ID,
		GrantId:               request.GrantID,
		ExpectedGrantVersion:  request.ExpectedGrantVersion,
		ResultingGrantVersion: resultingVersion,
		TargetSubjectId:       request.TargetSubjectID,
		ProposerId:            request.ProposerID,
		Reason:                request.Reason,
		Status:                generated.GrantRevocationRequestStatus(request.Status),
		CreatedAt:             request.CreatedAt,
		ExpiresAt:             request.ExpiresAt,
		ResolvedAt:            request.ResolvedAt,
		Reviews:               reviews,
	}
}

func webSessionDTO(session identity.WebSession) generated.WebSession {
	return generated.WebSession{
		User:      sessionUserDTO(session.User),
		ExpiresAt: session.ExpiresAt,
		CsrfToken: session.CSRFToken,
	}
}

func publicUserProfileDTO(profile identity.PublicUserProfile) generated.PublicUserProfile {
	published := make([]generated.PublicUserPublishedTorrent, 0, len(profile.PublishedTorrents))
	for _, torrent := range profile.PublishedTorrents {
		published = append(published, generated.PublicUserPublishedTorrent{
			Id: torrent.ID, Title: torrent.Title, Subtitle: torrent.Subtitle,
			Category:       generated.Category{Id: torrent.CategoryID, Name: torrent.CategoryName},
			TotalSizeBytes: torrent.TotalSizeBytes, PublishedAt: torrent.PublishedAt,
		})
	}
	return generated.PublicUserProfile{
		NumericId:             profile.NumericID,
		Username:              profile.Username,
		DisplayName:           profile.DisplayName,
		JoinedAt:              profile.JoinedAt,
		PublishedTorrentCount: profile.PublishedTorrentCount,
		PublishedTorrents:     published,
	}
}

func staffSessionDTO(session identity.StaffSession) generated.StaffSession {
	authenticationMethod := session.AuthenticationMethod
	if authenticationMethod == "" {
		authenticationMethod = identity.StaffAuthenticationPasskey
	}
	var webAuthnAuthenticatedAt *time.Time
	if !session.WebAuthnAuthenticatedAt.IsZero() {
		value := session.WebAuthnAuthenticatedAt
		webAuthnAuthenticatedAt = &value
	}
	return generated.StaffSession{
		User:                    sessionUserDTO(session.User),
		ExpiresAt:               session.ExpiresAt,
		AuthenticationMethod:    generated.StaffSessionAuthenticationMethod(authenticationMethod),
		AuthenticatedAt:         staffAuthenticationTime(session),
		WebauthnAuthenticatedAt: webAuthnAuthenticatedAt,
		CsrfToken:               session.CSRFToken,
	}
}

func sessionUserDTO(user identity.User) generated.SessionUser {
	return generated.SessionUser{
		Id:            user.ID,
		Username:      user.Username,
		DisplayName:   user.DisplayName,
		EmailVerified: user.EmailVerifiedAt != nil,
	}
}

func newSessionCookie(config SessionCookieConfig, token string, createdAt, expiresAt time.Time) http.Cookie {
	maxAge := int(expiresAt.Sub(createdAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	return http.Cookie{
		Name:     config.Name,
		Value:    token,
		Path:     config.Path,
		Expires:  expiresAt.UTC(),
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   config.Secure,
		SameSite: http.SameSiteStrictMode,
	}
}

func expiredSessionCookie(config SessionCookieConfig) http.Cookie {
	return http.Cookie{
		Name:     config.Name,
		Path:     config.Path,
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   config.Secure,
		SameSite: http.SameSiteStrictMode,
	}
}
