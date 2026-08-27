package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/vaultoperations"
	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/economy"
	"github.com/peergo/peergo/services/core/internal/modules/economy/attendance"
	"github.com/peergo/peergo/services/core/internal/modules/economy/contenttip"
	"github.com/peergo/peergo/services/core/internal/modules/economy/membergift"
	"github.com/peergo/peergo/services/core/internal/modules/economy/seedingreward"
	"github.com/peergo/peergo/services/core/internal/modules/economy/torrentpurchase"
	"github.com/peergo/peergo/services/core/internal/modules/hnradmin"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/newcomer"
	"github.com/peergo/peergo/services/core/internal/modules/notifications"
	"github.com/peergo/peergo/services/core/internal/modules/operations"
	"github.com/peergo/peergo/services/core/internal/modules/progression"
	"github.com/peergo/peergo/services/core/internal/modules/promotions"
	"github.com/peergo/peergo/services/core/internal/modules/ratiowatch"
	"github.com/peergo/peergo/services/core/internal/modules/review"
	"github.com/peergo/peergo/services/core/internal/modules/rss"
	"github.com/peergo/peergo/services/core/internal/modules/social"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/modules/trackercontrol"
	"github.com/peergo/peergo/services/core/internal/modules/traffic"
	"github.com/peergo/peergo/services/core/internal/modules/wiki"
	"github.com/peergo/peergo/services/core/internal/modules/workgroups"
	"github.com/peergo/peergo/services/core/internal/transport/httpapi"
)

type unavailableIdentityService struct{}

func (unavailableIdentityService) Login(context.Context, identity.LoginInput) (identity.WebSession, error) {
	return identity.WebSession{}, identity.ErrInvalidCredentials
}

func (unavailableIdentityService) CurrentSession(context.Context, string) (identity.WebSession, error) {
	return identity.WebSession{}, identity.ErrSessionNotFound
}

func (unavailableIdentityService) AuthenticateWrite(context.Context, string, string) (identity.WebSession, error) {
	return identity.WebSession{}, identity.ErrSessionNotFound
}

func (unavailableIdentityService) PublicProfile(context.Context, string, string) (identity.PublicUserProfile, error) {
	return identity.PublicUserProfile{}, identity.ErrSessionNotFound
}

func (unavailableIdentityService) UpdateProfile(context.Context, string, string, identity.UpdateMyProfileInput) (identity.User, error) {
	return identity.User{}, identity.ErrSessionNotFound
}

func (unavailableIdentityService) UpdateAvatar(context.Context, string, string, io.Reader) (identity.AvatarRevision, error) {
	return identity.AvatarRevision{}, identity.ErrSessionNotFound
}

func (unavailableIdentityService) PublicAvatar(context.Context, string, string) (identity.PublicAvatar, error) {
	return identity.PublicAvatar{}, identity.ErrSessionNotFound
}

func (unavailableIdentityService) Logout(context.Context, string, string) error {
	return identity.ErrSessionNotFound
}

func (unavailableIdentityService) InspectAccountAccess(context.Context, identity.InspectAccountAccessInput) (identity.AccountAccessStatus, error) {
	return identity.AccountAccessStatus{}, identity.ErrInvalidCredentials
}

func (unavailableIdentityService) SubmitAccountAccessAppeal(context.Context, identity.SubmitAccountAccessAppealInput) (identity.AccountAccessAppeal, error) {
	return identity.AccountAccessAppeal{}, identity.ErrInvalidCredentials
}

func (unavailableIdentityService) AccountAccessAppeals(context.Context, authz.StaffActor, identity.AccountAccessAppealQuery) (identity.AccountAccessAppealPage, error) {
	return identity.AccountAccessAppealPage{}, authz.ErrForbidden
}

func (unavailableIdentityService) DecideAccountAccessAppeal(context.Context, authz.StaffActor, identity.DecideAccountAccessAppealInput) (identity.AccountAccessAppeal, error) {
	return identity.AccountAccessAppeal{}, authz.ErrForbidden
}

func (unavailableIdentityService) MyDownloadRestriction(context.Context, string) (identity.DownloadRestrictionStatus, error) {
	return identity.DownloadRestrictionStatus{}, identity.ErrSessionNotFound
}

func (unavailableIdentityService) SubmitDownloadRestrictionAppeal(context.Context, string, string, identity.SubmitDownloadRestrictionAppealInput) (identity.AccountAccessAppeal, error) {
	return identity.AccountAccessAppeal{}, identity.ErrSessionNotFound
}

type unavailableRegistrationService struct{}

func (unavailableRegistrationService) PublicPolicy(context.Context) (identity.RegistrationPublicPolicy, error) {
	return identity.RegistrationPublicPolicy{
		Mode: identity.RegistrationModeInvite, UsernameMinCharacters: 3,
		UsernameMaxCharacters: 32, EmailDomainMode: identity.EmailDomainModeAny,
		HumanVerificationProvider: identity.HumanVerificationProviderDisabled,
	}, nil
}

func (unavailableRegistrationService) Policy(context.Context, authz.StaffActor) (identity.RegistrationPolicy, error) {
	return identity.RegistrationPolicy{}, identity.ErrRegistrationServiceUnavailable
}

func (unavailableRegistrationService) UpdatePolicy(context.Context, authz.StaffActor, identity.UpdateRegistrationPolicyInput) (identity.RegistrationPolicy, error) {
	return identity.RegistrationPolicy{}, identity.ErrRegistrationServiceUnavailable
}

type recordingRegistrationService struct {
	input        identity.RegistrationInput
	result       identity.RegistrationResult
	publicPolicy identity.RegistrationPublicPolicy
	err          error
}

func (service *recordingRegistrationService) PublicPolicy(context.Context) (identity.RegistrationPublicPolicy, error) {
	if service.publicPolicy.Mode != "" {
		return service.publicPolicy, service.err
	}
	return identity.RegistrationPublicPolicy{
		Mode: identity.RegistrationModeInvite, UsernameMinCharacters: 3,
		UsernameMaxCharacters: 32, EmailDomainMode: identity.EmailDomainModeAny,
		HumanVerificationProvider: identity.HumanVerificationProviderDisabled,
	}, service.err
}

type recordingHumanVerificationVerifier struct {
	flow  identity.HumanVerificationFlow
	token string
	calls int
	err   error
}

func (verifier *recordingHumanVerificationVerifier) Verify(_ context.Context, flow identity.HumanVerificationFlow, token string) error {
	verifier.flow = flow
	verifier.token = token
	verifier.calls++
	return verifier.err
}

func (*recordingHumanVerificationVerifier) Configured() bool { return true }

func (service *recordingRegistrationService) Policy(context.Context, authz.StaffActor) (identity.RegistrationPolicy, error) {
	return identity.RegistrationPolicy{}, service.err
}

func (service *recordingRegistrationService) UpdatePolicy(context.Context, authz.StaffActor, identity.UpdateRegistrationPolicyInput) (identity.RegistrationPolicy, error) {
	return identity.RegistrationPolicy{}, service.err
}

func (service *recordingRegistrationService) Register(_ context.Context, input identity.RegistrationInput) (identity.RegistrationResult, error) {
	service.input = input
	return service.result, service.err
}

func (unavailableRegistrationService) Register(context.Context, identity.RegistrationInput) (identity.RegistrationResult, error) {
	return identity.RegistrationResult{}, identity.ErrRegistrationServiceUnavailable
}

type unavailableInvitationService struct{}

func (unavailableInvitationService) Overview(context.Context, string, int, int) (identity.InvitationOverview, error) {
	return identity.InvitationOverview{}, identity.ErrSessionNotFound
}

func (unavailableInvitationService) Issue(context.Context, string, string, string) (identity.InvitationIssueResult, error) {
	return identity.InvitationIssueResult{}, identity.ErrSessionNotFound
}

func (unavailableInvitationService) Revoke(context.Context, string, string, uuid.UUID) (identity.MemberInvitation, error) {
	return identity.MemberInvitation{}, identity.ErrSessionNotFound
}

type unavailableEmailVerificationService struct{}

func (unavailableEmailVerificationService) Request(context.Context, string, string, string) (identity.EmailVerificationDispatch, error) {
	return identity.EmailVerificationDispatch{}, identity.ErrEmailVerificationServiceUnavailable
}

func (unavailableEmailVerificationService) Confirm(context.Context, string) (identity.EmailVerificationCompletion, error) {
	return identity.EmailVerificationCompletion{}, identity.ErrEmailVerificationServiceUnavailable
}

type unavailablePasswordRecoveryService struct{}

func (unavailablePasswordRecoveryService) Request(context.Context, string) (identity.PasswordRecoveryDispatch, error) {
	return identity.PasswordRecoveryDispatch{}, identity.ErrPasswordRecoveryServiceUnavailable
}

func (unavailablePasswordRecoveryService) Confirm(context.Context, string, string) (identity.PasswordRecoveryCompletion, error) {
	return identity.PasswordRecoveryCompletion{}, identity.ErrPasswordRecoveryServiceUnavailable
}

type unavailableSessionSecurityService struct{}

func (unavailableSessionSecurityService) Overview(context.Context, string) (identity.AccountSecurityOverview, error) {
	return identity.AccountSecurityOverview{}, identity.ErrSessionNotFound
}

func (unavailableSessionSecurityService) ListSessions(context.Context, string) ([]identity.UserWebSession, error) {
	return nil, identity.ErrSessionNotFound
}

func (unavailableSessionSecurityService) RevokeSession(context.Context, string, string, uuid.UUID) (identity.SessionRevocationResult, error) {
	return identity.SessionRevocationResult{}, identity.ErrSessionNotFound
}

func (unavailableSessionSecurityService) RevokeOtherSessions(context.Context, string, string) (identity.SessionRevocationResult, error) {
	return identity.SessionRevocationResult{}, identity.ErrSessionNotFound
}

type unavailableTwoFactorService struct{}

func (unavailableTwoFactorService) StartEnrollment(context.Context, string, string, identity.TOTPEnrollmentCommand) (identity.TOTPEnrollmentStart, error) {
	return identity.TOTPEnrollmentStart{}, identity.ErrSessionNotFound
}

func (unavailableTwoFactorService) ConfirmEnrollment(context.Context, string, string, identity.TOTPEnrollmentConfirmationCommand) (identity.TOTPEnrollmentConfirmation, error) {
	return identity.TOTPEnrollmentConfirmation{}, identity.ErrSessionNotFound
}

func (unavailableTwoFactorService) RotateRecoveryCodes(context.Context, string, string, identity.TwoFactorReauthenticationCommand) (identity.TwoFactorVaultChange, error) {
	return identity.TwoFactorVaultChange{}, identity.ErrSessionNotFound
}

func (unavailableTwoFactorService) Disable(context.Context, string, string, identity.TwoFactorReauthenticationCommand) (identity.TwoFactorVaultChange, error) {
	return identity.TwoFactorVaultChange{}, identity.ErrSessionNotFound
}

type recordingTwoFactorService struct {
	startResult   identity.TOTPEnrollmentStart
	confirmResult identity.TOTPEnrollmentConfirmation
	rotateResult  identity.TwoFactorVaultChange
	disableResult identity.TwoFactorVaultChange
	cookieToken   string
	csrfToken     string
	enrollment    identity.TOTPEnrollmentCommand
	confirmation  identity.TOTPEnrollmentConfirmationCommand
	reauth        identity.TwoFactorReauthenticationCommand
}

func (service *recordingTwoFactorService) StartEnrollment(_ context.Context, cookieToken, csrfToken string, command identity.TOTPEnrollmentCommand) (identity.TOTPEnrollmentStart, error) {
	service.cookieToken, service.csrfToken, service.enrollment = cookieToken, csrfToken, command
	return service.startResult, nil
}

func (service *recordingTwoFactorService) ConfirmEnrollment(_ context.Context, cookieToken, csrfToken string, command identity.TOTPEnrollmentConfirmationCommand) (identity.TOTPEnrollmentConfirmation, error) {
	service.cookieToken, service.csrfToken, service.confirmation = cookieToken, csrfToken, command
	return service.confirmResult, nil
}

func (service *recordingTwoFactorService) RotateRecoveryCodes(_ context.Context, cookieToken, csrfToken string, command identity.TwoFactorReauthenticationCommand) (identity.TwoFactorVaultChange, error) {
	service.cookieToken, service.csrfToken, service.reauth = cookieToken, csrfToken, command
	return service.rotateResult, nil
}

func (service *recordingTwoFactorService) Disable(_ context.Context, cookieToken, csrfToken string, command identity.TwoFactorReauthenticationCommand) (identity.TwoFactorVaultChange, error) {
	service.cookieToken, service.csrfToken, service.reauth = cookieToken, csrfToken, command
	return service.disableResult, nil
}

type recordingSessionSecurityService struct {
	overview           identity.AccountSecurityOverview
	sessions           []identity.UserWebSession
	revokeResult       identity.SessionRevocationResult
	cookieToken        string
	csrfToken          string
	targetSessionID    uuid.UUID
	revokeOthersCalled bool
}

func (service *recordingSessionSecurityService) Overview(_ context.Context, cookieToken string) (identity.AccountSecurityOverview, error) {
	service.cookieToken = cookieToken
	return service.overview, nil
}

func (service *recordingSessionSecurityService) ListSessions(_ context.Context, cookieToken string) ([]identity.UserWebSession, error) {
	service.cookieToken = cookieToken
	return service.sessions, nil
}

func (service *recordingSessionSecurityService) RevokeSession(_ context.Context, cookieToken, csrfToken string, sessionID uuid.UUID) (identity.SessionRevocationResult, error) {
	service.cookieToken = cookieToken
	service.csrfToken = csrfToken
	service.targetSessionID = sessionID
	return service.revokeResult, nil
}

func (service *recordingSessionSecurityService) RevokeOtherSessions(_ context.Context, cookieToken, csrfToken string) (identity.SessionRevocationResult, error) {
	service.cookieToken = cookieToken
	service.csrfToken = csrfToken
	service.revokeOthersCalled = true
	return service.revokeResult, nil
}

type unavailableStaffIdentityService struct{}

func (unavailableStaffIdentityService) BeginElevation(context.Context, string, string) (identity.StaffElevationOptions, error) {
	return identity.StaffElevationOptions{}, identity.ErrStaffCredentialRequired
}

func (unavailableStaffIdentityService) CompleteElevation(context.Context, string, string, identity.CompleteStaffElevationInput) (identity.StaffSession, error) {
	return identity.StaffSession{}, identity.ErrStaffChallengeNotFound
}

func (unavailableStaffIdentityService) CurrentSession(context.Context, string) (identity.StaffSession, error) {
	return identity.StaffSession{}, identity.ErrStaffSessionNotFound
}

func (unavailableStaffIdentityService) AuthenticateWrite(context.Context, string, string) (identity.StaffSession, error) {
	return identity.StaffSession{}, identity.ErrStaffSessionNotFound
}

func (unavailableStaffIdentityService) Logout(context.Context, string, string) error {
	return identity.ErrStaffSessionNotFound
}

type unavailableStaffEnrollmentService struct{}

func (unavailableStaffEnrollmentService) Begin(context.Context, string, string, identity.BeginStaffEnrollmentInput) (identity.StaffEnrollmentOptions, error) {
	return identity.StaffEnrollmentOptions{}, identity.ErrStaffBootstrapTicketInvalid
}

func (unavailableStaffEnrollmentService) Complete(context.Context, string, string, identity.CompleteStaffEnrollmentInput) (identity.StaffCredentialEnrollment, error) {
	return identity.StaffCredentialEnrollment{}, identity.ErrStaffEnrollmentChallengeNotFound
}

type recordingStaffEnrollmentService struct {
	beginResult      identity.StaffEnrollmentOptions
	beginWebToken    string
	beginCSRF        string
	beginInput       identity.BeginStaffEnrollmentInput
	completeResult   identity.StaffCredentialEnrollment
	completeWebToken string
	completeCSRF     string
	completeInput    identity.CompleteStaffEnrollmentInput
}

func (service *recordingStaffEnrollmentService) Begin(_ context.Context, webToken, csrfToken string, input identity.BeginStaffEnrollmentInput) (identity.StaffEnrollmentOptions, error) {
	service.beginWebToken = webToken
	service.beginCSRF = csrfToken
	service.beginInput = input
	return service.beginResult, nil
}

func (service *recordingStaffEnrollmentService) Complete(_ context.Context, webToken, csrfToken string, input identity.CompleteStaffEnrollmentInput) (identity.StaffCredentialEnrollment, error) {
	service.completeWebToken = webToken
	service.completeCSRF = csrfToken
	service.completeInput = input
	return service.completeResult, nil
}

type unavailableAuthorizationService struct{}

func (unavailableAuthorizationService) Authorize(context.Context, authz.Request) (authz.Decision, error) {
	return authz.Decision{}, authz.ErrForbidden
}

func (unavailableAuthorizationService) Capabilities(context.Context, authz.Subject) (authz.CapabilitySet, error) {
	return authz.CapabilitySet{}, authz.ErrForbidden
}

func (unavailableAuthorizationService) StaffCapabilities(context.Context, authz.Subject, time.Time, authz.AuthorityBinding) (authz.CapabilitySet, error) {
	return authz.CapabilitySet{}, authz.ErrForbidden
}

type unavailableTrafficOverviewService struct{}

func (unavailableTrafficOverviewService) MyOverview(context.Context, string, int) (traffic.Overview, error) {
	return traffic.Overview{}, identity.ErrSessionNotFound
}

func (unavailableTrafficOverviewService) MyHNR(context.Context, string, traffic.HNRQuery) (traffic.HNRPage, error) {
	return traffic.HNRPage{}, identity.ErrSessionNotFound
}

func (unavailableTrafficOverviewService) SubmitHNRAppeal(context.Context, string, string, traffic.SubmitHNRAppealInput) (traffic.HNRAppeal, error) {
	return traffic.HNRAppeal{}, identity.ErrSessionNotFound
}

func (unavailableTrafficOverviewService) HNRAppeals(context.Context, authz.StaffActor, traffic.HNRAppealQuery) (traffic.HNRAppealPage, error) {
	return traffic.HNRAppealPage{}, authz.ErrForbidden
}

func (unavailableTrafficOverviewService) DecideHNRAppeal(context.Context, authz.StaffActor, traffic.DecideHNRAppealInput) (traffic.HNRAppeal, error) {
	return traffic.HNRAppeal{}, authz.ErrForbidden
}

type unavailableEconomyOverviewService struct{}

func (unavailableEconomyOverviewService) MyOverview(context.Context, string, int) (economy.Overview, error) {
	return economy.Overview{}, identity.ErrSessionNotFound
}

type unavailableAttendanceService struct{}

func (unavailableAttendanceService) MyOverview(context.Context, string) (attendance.Overview, error) {
	return attendance.Overview{}, identity.ErrSessionNotFound
}

func (unavailableAttendanceService) Claim(context.Context, string, string, uuid.UUID, attendance.Mode) (attendance.Record, error) {
	return attendance.Record{}, identity.ErrSessionNotFound
}

func (unavailableAttendanceService) ListPolicies(context.Context, authz.StaffActor, int, int) (attendance.PolicyPage, error) {
	return attendance.PolicyPage{}, authz.ErrForbidden
}

func (unavailableAttendanceService) IssuePolicy(context.Context, authz.StaffActor, uuid.UUID, attendance.PolicyRevision, string) (attendance.PublishedPolicy, error) {
	return attendance.PublishedPolicy{}, authz.ErrForbidden
}

type unavailableMemberGiftService struct{}

func (unavailableMemberGiftService) MyOverview(context.Context, string, int) (membergift.Overview, error) {
	return membergift.Overview{}, identity.ErrSessionNotFound
}

func (unavailableMemberGiftService) Create(context.Context, string, string, uuid.UUID, int64, int64, string) (membergift.Gift, error) {
	return membergift.Gift{}, identity.ErrSessionNotFound
}

func (unavailableMemberGiftService) ListPolicies(context.Context, authz.StaffActor, int, int) (membergift.PolicyPage, error) {
	return membergift.PolicyPage{}, authz.ErrForbidden
}

func (unavailableMemberGiftService) IssuePolicy(context.Context, authz.StaffActor, uuid.UUID, membergift.PolicyRevision, string) (membergift.PublishedPolicy, error) {
	return membergift.PublishedPolicy{}, authz.ErrForbidden
}

type unavailableContentTipService struct{}

func (unavailableContentTipService) MyOverview(context.Context, string, int) (contenttip.Overview, error) {
	return contenttip.Overview{}, identity.ErrSessionNotFound
}

func (unavailableContentTipService) Create(context.Context, string, string, uuid.UUID, contenttip.Target, int64) (contenttip.Tip, error) {
	return contenttip.Tip{}, identity.ErrSessionNotFound
}

func (unavailableContentTipService) ListPolicies(context.Context, authz.StaffActor, int, int) (contenttip.PolicyPage, error) {
	return contenttip.PolicyPage{}, authz.ErrForbidden
}

func (unavailableContentTipService) IssuePolicy(context.Context, authz.StaffActor, uuid.UUID, contenttip.PolicyRevision, string) (contenttip.PublishedPolicy, error) {
	return contenttip.PublishedPolicy{}, authz.ErrForbidden
}

type unavailableSeedingRewardAdministrationService struct{}

type unavailableWorkgroupService struct{}

func (unavailableWorkgroupService) MyOverview(context.Context, string) (workgroups.MyOverview, error) {
	return workgroups.MyOverview{}, identity.ErrSessionNotFound
}

func (unavailableWorkgroupService) MyContributionCycles(context.Context, string, workgroups.GroupKind, int) (workgroups.ContributionCyclePage, error) {
	return workgroups.ContributionCyclePage{}, identity.ErrSessionNotFound
}

func (unavailableWorkgroupService) MyTasks(context.Context, string, int, int) (workgroups.TaskAssignmentPage, error) {
	return workgroups.TaskAssignmentPage{}, identity.ErrSessionNotFound
}

func (unavailableWorkgroupService) SubmitTask(context.Context, string, string, uuid.UUID, uuid.UUID, string) (workgroups.TaskAssignment, error) {
	return workgroups.TaskAssignment{}, identity.ErrSessionNotFound
}

func (unavailableWorkgroupService) Apply(context.Context, string, string, uuid.UUID, workgroups.GroupKind, string) (workgroups.Application, error) {
	return workgroups.Application{}, identity.ErrSessionNotFound
}

func (unavailableWorkgroupService) AdminOverview(context.Context, authz.StaffActor) (workgroups.AdminOverview, error) {
	return workgroups.AdminOverview{}, authz.ErrForbidden
}

func (unavailableWorkgroupService) ListApplications(context.Context, authz.StaffActor, workgroups.ApplicationStatus, int, int) (workgroups.ApplicationPage, error) {
	return workgroups.ApplicationPage{}, authz.ErrForbidden
}

func (unavailableWorkgroupService) DecideApplication(context.Context, authz.StaffActor, uuid.UUID, uuid.UUID, int64, bool, string) (workgroups.Application, error) {
	return workgroups.Application{}, authz.ErrForbidden
}

func (unavailableWorkgroupService) ListMemberships(context.Context, authz.StaffActor, workgroups.GroupKind, workgroups.MembershipStatus, int, int) (workgroups.MembershipPage, error) {
	return workgroups.MembershipPage{}, authz.ErrForbidden
}

func (unavailableWorkgroupService) ContributionCycles(context.Context, authz.StaffActor, workgroups.GroupKind, uuid.UUID, int) (workgroups.ContributionCyclePage, error) {
	return workgroups.ContributionCyclePage{}, authz.ErrForbidden
}

func (unavailableWorkgroupService) GrantMembership(context.Context, authz.StaffActor, uuid.UUID, workgroups.GroupKind, int64, string) (workgroups.Membership, error) {
	return workgroups.Membership{}, authz.ErrForbidden
}

func (unavailableWorkgroupService) ChangeMembership(context.Context, authz.StaffActor, uuid.UUID, uuid.UUID, workgroups.GroupKind, int64, workgroups.MembershipTransition, string) (workgroups.Membership, error) {
	return workgroups.Membership{}, authz.ErrForbidden
}

func (unavailableWorkgroupService) ContributionPolicies(context.Context, authz.StaffActor, workgroups.GroupKind, int, int) (workgroups.ContributionPolicyPage, error) {
	return workgroups.ContributionPolicyPage{}, authz.ErrForbidden
}

func (unavailableWorkgroupService) IssueContributionPolicy(context.Context, authz.StaffActor, uuid.UUID, workgroups.GroupKind, int64, time.Time, string) (workgroups.ContributionPolicy, error) {
	return workgroups.ContributionPolicy{}, authz.ErrForbidden
}

func (unavailableWorkgroupService) IssueContributionReminder(context.Context, authz.StaffActor, uuid.UUID, workgroups.GroupKind, uuid.UUID, time.Time, string) (workgroups.ContributionReminder, error) {
	return workgroups.ContributionReminder{}, authz.ErrForbidden
}

func (unavailableWorkgroupService) Tasks(context.Context, authz.StaffActor, workgroups.GroupKind, int, int) (workgroups.TaskPage, error) {
	return workgroups.TaskPage{}, authz.ErrForbidden
}

func (unavailableWorkgroupService) PublishTask(context.Context, authz.StaffActor, uuid.UUID, workgroups.GroupKind, workgroups.TaskType, string, string, time.Time, time.Time) (workgroups.Task, error) {
	return workgroups.Task{}, authz.ErrForbidden
}

func (unavailableWorkgroupService) TaskAssignments(context.Context, authz.StaffActor, workgroups.GroupKind, uuid.UUID, int, int) (workgroups.TaskAssignmentPage, error) {
	return workgroups.TaskAssignmentPage{}, authz.ErrForbidden
}

func (unavailableWorkgroupService) ReviewTaskSubmission(context.Context, authz.StaffActor, uuid.UUID, uuid.UUID, workgroups.TaskReviewDecision, string) (workgroups.TaskAssignment, error) {
	return workgroups.TaskAssignment{}, authz.ErrForbidden
}

func (unavailableSeedingRewardAdministrationService) List(context.Context, authz.StaffActor, int, int) (seedingreward.PolicyPage, error) {
	return seedingreward.PolicyPage{}, authz.ErrForbidden
}

func (unavailableSeedingRewardAdministrationService) Preview(context.Context, authz.StaffActor, seedingreward.PolicyRevision) (seedingreward.PolicyPreview, error) {
	return seedingreward.PolicyPreview{}, authz.ErrForbidden
}

func (unavailableSeedingRewardAdministrationService) Issue(context.Context, authz.StaffActor, seedingreward.PolicyRevision, string) (seedingreward.PublishedPolicy, error) {
	return seedingreward.PublishedPolicy{}, authz.ErrForbidden
}

func (unavailableSeedingRewardAdministrationService) Overview(context.Context, authz.StaffActor) (progression.LevelPolicyOverview, error) {
	return progression.LevelPolicyOverview{}, authz.ErrForbidden
}

func (unavailableSeedingRewardAdministrationService) IssueLevelPolicy(context.Context, authz.StaffActor, progression.IssueLevelPolicyInput) (progression.LevelPolicyRevision, error) {
	return progression.LevelPolicyRevision{}, authz.ErrForbidden
}

type unavailableOperationsService struct{}

func (unavailableOperationsService) Email(context.Context, authz.StaffActor) (operations.EmailOverview, error) {
	return operations.EmailOverview{}, authz.ErrForbidden
}

func (unavailableOperationsService) TestEmail(context.Context, authz.StaffActor, string) (vaultoperations.EmailTestResult, error) {
	return vaultoperations.EmailTestResult{}, authz.ErrForbidden
}

func (unavailableOperationsService) SettlementSettings(context.Context, authz.StaffActor) (operations.SettlementSettingsOverview, error) {
	return operations.SettlementSettingsOverview{}, authz.ErrForbidden
}

func (unavailableOperationsService) EconomySettings(context.Context, authz.StaffActor) (operations.EconomySettingsOverview, error) {
	return operations.EconomySettingsOverview{}, authz.ErrForbidden
}

func (unavailableOperationsService) TorrentRules(context.Context, authz.StaffActor) (operations.TorrentRulesOverview, error) {
	return operations.TorrentRulesOverview{}, authz.ErrForbidden
}

func (unavailableOperationsService) IssueTorrentUploadPolicy(context.Context, authz.StaffActor, torrents.IssueUploadPolicyInput) (torrents.UploadPolicyRevision, error) {
	return torrents.UploadPolicyRevision{}, authz.ErrForbidden
}

func (unavailableOperationsService) TrackerRuntime(context.Context, authz.StaffActor) (operations.TrackerRuntimeOverview, error) {
	return operations.TrackerRuntimeOverview{}, authz.ErrForbidden
}

func (unavailableOperationsService) TrackerPolicy(context.Context, authz.StaffActor) (trackercontrol.RuntimePolicyRevision, error) {
	return trackercontrol.RuntimePolicyRevision{}, authz.ErrForbidden
}

func (unavailableOperationsService) IssueTrackerPolicy(context.Context, authz.StaffActor, trackercontrol.IssueRuntimePolicyInput) (trackercontrol.RuntimePolicyRevision, error) {
	return trackercontrol.RuntimePolicyRevision{}, authz.ErrForbidden
}

func (unavailableOperationsService) MySeedboxReports(context.Context, uuid.UUID, int, int) (trackercontrol.SeedboxReportPage, error) {
	return trackercontrol.SeedboxReportPage{}, authz.ErrForbidden
}
func (unavailableOperationsService) SubmitSeedboxReport(context.Context, uuid.UUID, trackercontrol.SubmitSeedboxReportInput) (trackercontrol.SeedboxReport, error) {
	return trackercontrol.SeedboxReport{}, authz.ErrForbidden
}
func (unavailableOperationsService) SeedboxReports(context.Context, authz.StaffActor, trackercontrol.SeedboxReportStatus, int, int) (trackercontrol.SeedboxReportPage, error) {
	return trackercontrol.SeedboxReportPage{}, authz.ErrForbidden
}
func (unavailableOperationsService) DecideSeedboxReport(context.Context, authz.StaffActor, trackercontrol.DecideSeedboxReportInput) (trackercontrol.SeedboxReport, error) {
	return trackercontrol.SeedboxReport{}, authz.ErrForbidden
}

func (unavailableOperationsService) Tracker(context.Context, authz.StaffActor) (operations.TrackerOverview, error) {
	return operations.TrackerOverview{}, authz.ErrForbidden
}

func (unavailableOperationsService) Workers(context.Context, authz.StaffActor) (operations.WorkerOverview, error) {
	return operations.WorkerOverview{}, authz.ErrForbidden
}

func (unavailableOperationsService) Storage(context.Context, authz.StaffActor) (operations.StorageOverview, error) {
	return operations.StorageOverview{}, authz.ErrForbidden
}

func (unavailableOperationsService) VIPProfile(context.Context, authz.StaffActor) (operations.VIPProfileOverview, error) {
	return operations.VIPProfileOverview{}, authz.ErrForbidden
}

type unavailableNotificationService struct{}

func (unavailableNotificationService) List(context.Context, string, notifications.ListQuery) (notifications.Page, error) {
	return notifications.Page{}, identity.ErrSessionNotFound
}

func (unavailableNotificationService) Summary(context.Context, string) (notifications.Summary, error) {
	return notifications.Summary{}, identity.ErrSessionNotFound
}

func (unavailableNotificationService) MarkRead(context.Context, string, string, uuid.UUID) (notifications.ReadReceipt, error) {
	return notifications.ReadReceipt{}, identity.ErrSessionNotFound
}

func (unavailableNotificationService) MarkAllRead(context.Context, string, string) (notifications.ReadAllReceipt, error) {
	return notifications.ReadAllReceipt{}, identity.ErrSessionNotFound
}

func (unavailableNotificationService) ArchiveAll(context.Context, string, string) (notifications.ArchiveAllReceipt, error) {
	return notifications.ArchiveAllReceipt{}, identity.ErrSessionNotFound
}

func (unavailableNotificationService) CreateFeedback(context.Context, string, string, notifications.CreateFeedbackInput) (notifications.FeedbackReceipt, error) {
	return notifications.FeedbackReceipt{}, identity.ErrSessionNotFound
}

type recordingNotificationService struct {
	page            notifications.Page
	summary         notifications.Summary
	readReceipt     notifications.ReadReceipt
	readAllReceipt  notifications.ReadAllReceipt
	archiveReceipt  notifications.ArchiveAllReceipt
	feedbackReceipt notifications.FeedbackReceipt
	listCookie      string
	listQuery       notifications.ListQuery
	summaryCookie   string
	readCookie      string
	readCSRF        string
	readID          uuid.UUID
	readAllCookie   string
	readAllCSRF     string
	archiveCookie   string
	archiveCSRF     string
	feedbackCookie  string
	feedbackCSRF    string
	feedbackInput   notifications.CreateFeedbackInput
}

func (service *recordingNotificationService) List(_ context.Context, cookie string, query notifications.ListQuery) (notifications.Page, error) {
	service.listCookie, service.listQuery = cookie, query
	return service.page, nil
}

func (service *recordingNotificationService) Summary(_ context.Context, cookie string) (notifications.Summary, error) {
	service.summaryCookie = cookie
	return service.summary, nil
}

func (service *recordingNotificationService) MarkRead(_ context.Context, cookie, csrf string, id uuid.UUID) (notifications.ReadReceipt, error) {
	service.readCookie, service.readCSRF, service.readID = cookie, csrf, id
	return service.readReceipt, nil
}

func (service *recordingNotificationService) MarkAllRead(_ context.Context, cookie, csrf string) (notifications.ReadAllReceipt, error) {
	service.readAllCookie, service.readAllCSRF = cookie, csrf
	return service.readAllReceipt, nil
}

func (service *recordingNotificationService) ArchiveAll(_ context.Context, cookie, csrf string) (notifications.ArchiveAllReceipt, error) {
	service.archiveCookie, service.archiveCSRF = cookie, csrf
	return service.archiveReceipt, nil
}

func (service *recordingNotificationService) CreateFeedback(_ context.Context, cookie, csrf string, input notifications.CreateFeedbackInput) (notifications.FeedbackReceipt, error) {
	service.feedbackCookie, service.feedbackCSRF, service.feedbackInput = cookie, csrf, input
	return service.feedbackReceipt, nil
}

type unavailableTorrentBookmarkService struct{}

func (unavailableTorrentBookmarkService) List(context.Context, string, int, int) (catalog.TorrentBookmarkPage, error) {
	return catalog.TorrentBookmarkPage{}, identity.ErrSessionNotFound
}

func (unavailableTorrentBookmarkService) Statuses(context.Context, string, []int64) ([]int64, error) {
	return nil, identity.ErrSessionNotFound
}

func (unavailableTorrentBookmarkService) Put(context.Context, string, string, int64) (catalog.TorrentBookmarkState, error) {
	return catalog.TorrentBookmarkState{}, identity.ErrSessionNotFound
}

func (unavailableTorrentBookmarkService) Delete(context.Context, string, string, int64) error {
	return identity.ErrSessionNotFound
}

type recordingTorrentBookmarkService struct {
	page         catalog.TorrentBookmarkPage
	statuses     []int64
	state        catalog.TorrentBookmarkState
	listCookie   string
	listLimit    int
	listOffset   int
	statusCookie string
	statusIDs    []int64
	putCookie    string
	putCSRF      string
	putID        int64
	deleteCookie string
	deleteCSRF   string
	deleteID     int64
}

func (service *recordingTorrentBookmarkService) List(_ context.Context, cookie string, limit, offset int) (catalog.TorrentBookmarkPage, error) {
	service.listCookie, service.listLimit, service.listOffset = cookie, limit, offset
	return service.page, nil
}

func (service *recordingTorrentBookmarkService) Statuses(_ context.Context, cookie string, ids []int64) ([]int64, error) {
	service.statusCookie = cookie
	service.statusIDs = append([]int64(nil), ids...)
	return service.statuses, nil
}

func (service *recordingTorrentBookmarkService) Put(_ context.Context, cookie, csrf string, torrentID int64) (catalog.TorrentBookmarkState, error) {
	service.putCookie, service.putCSRF, service.putID = cookie, csrf, torrentID
	return service.state, nil
}

func (service *recordingTorrentBookmarkService) Delete(_ context.Context, cookie, csrf string, torrentID int64) error {
	service.deleteCookie, service.deleteCSRF, service.deleteID = cookie, csrf, torrentID
	return nil
}

type unavailableCommentService struct{}

func (unavailableCommentService) ListTorrentComments(context.Context, int64, int, int) (social.CommentPage, error) {
	return social.CommentPage{}, social.ErrCommentTargetNotFound
}

func (unavailableCommentService) ListAnnouncementComments(context.Context, string, int, int) (social.CommentPage, error) {
	return social.CommentPage{}, social.ErrCommentTargetNotFound
}

func (unavailableCommentService) ListPostComments(context.Context, uuid.UUID, social.CommentThreadSort, int, int) (social.CommentThreadPage, error) {
	return social.CommentThreadPage{}, social.ErrCommentTargetNotFound
}

func (unavailableCommentService) CreateTorrentComment(context.Context, string, string, social.CreateTorrentCommentInput) (social.Comment, error) {
	return social.Comment{}, identity.ErrSessionNotFound
}

func (unavailableCommentService) CreateAnnouncementComment(context.Context, string, string, social.CreateAnnouncementCommentInput) (social.Comment, error) {
	return social.Comment{}, identity.ErrSessionNotFound
}

func (unavailableCommentService) CreatePostComment(context.Context, string, string, social.CreatePostCommentInput) (social.Comment, error) {
	return social.Comment{}, identity.ErrSessionNotFound
}

type unavailableSocialPostService struct{}

func (unavailableSocialPostService) List(context.Context, string, social.PostListQuery) (social.PostPage, error) {
	return social.PostPage{}, identity.ErrSessionNotFound
}

func (unavailableSocialPostService) FindVisible(context.Context, string, uuid.UUID) (social.Post, error) {
	return social.Post{}, identity.ErrSessionNotFound
}

func (unavailableSocialPostService) Create(context.Context, string, string, social.CreatePostInput) (social.Post, error) {
	return social.Post{}, identity.ErrSessionNotFound
}

func (unavailableSocialPostService) UpdateMyPost(context.Context, string, string, social.UpdatePostInput) (social.Post, error) {
	return social.Post{}, identity.ErrSessionNotFound
}

func (unavailableSocialPostService) DeleteMyPost(context.Context, string, string, social.DeletePostInput) error {
	return identity.ErrSessionNotFound
}

func (unavailableSocialPostService) Overview(context.Context, string) (social.CommunityOverview, error) {
	return social.CommunityOverview{}, identity.ErrSessionNotFound
}
func (unavailableSocialPostService) UploadMedia(context.Context, string, string, []byte) (social.PostMedia, error) {
	return social.PostMedia{}, identity.ErrSessionNotFound
}
func (unavailableSocialPostService) ReadMedia(context.Context, string, uuid.UUID) (social.MediaObject, error) {
	return social.MediaObject{}, identity.ErrSessionNotFound
}
func (unavailableSocialPostService) SetLike(context.Context, string, string, uuid.UUID, bool) (social.InteractionState, error) {
	return social.InteractionState{}, identity.ErrSessionNotFound
}
func (unavailableSocialPostService) SetRepost(context.Context, string, string, uuid.UUID, bool) (social.InteractionState, error) {
	return social.InteractionState{}, identity.ErrSessionNotFound
}
func (unavailableSocialPostService) SetFollow(context.Context, string, string, string, bool) (social.FollowState, error) {
	return social.FollowState{}, identity.ErrSessionNotFound
}
func (unavailableSocialPostService) Vote(context.Context, string, string, uuid.UUID, uuid.UUID) (social.Poll, error) {
	return social.Poll{}, identity.ErrSessionNotFound
}
func (unavailableSocialPostService) ClaimRedPacket(context.Context, string, string, uuid.UUID, uuid.UUID) (social.RedPacketClaim, error) {
	return social.RedPacketClaim{}, identity.ErrSessionNotFound
}
func (unavailableSocialPostService) ListNotifications(context.Context, string, social.SocialNotificationQuery) (social.SocialNotificationPage, error) {
	return social.SocialNotificationPage{}, identity.ErrSessionNotFound
}
func (unavailableSocialPostService) NotificationSummary(context.Context, string) (social.SocialNotificationSummary, error) {
	return social.SocialNotificationSummary{}, identity.ErrSessionNotFound
}
func (unavailableSocialPostService) MarkNotificationRead(context.Context, string, string, uuid.UUID) (social.SocialNotificationReadReceipt, error) {
	return social.SocialNotificationReadReceipt{}, identity.ErrSessionNotFound
}
func (unavailableSocialPostService) MarkAllNotificationsRead(context.Context, string, string) (social.SocialNotificationReadAllReceipt, error) {
	return social.SocialNotificationReadAllReceipt{}, identity.ErrSessionNotFound
}
func (unavailableSocialPostService) ListManagedBoards(context.Context, authz.StaffActor) ([]social.Board, error) {
	return nil, authz.ErrForbidden
}
func (unavailableSocialPostService) CreateManagedBoard(context.Context, authz.StaffActor, social.CreateBoardInput) (social.Board, error) {
	return social.Board{}, authz.ErrForbidden
}
func (unavailableSocialPostService) UpdateManagedBoard(context.Context, authz.StaffActor, social.UpdateBoardInput) (social.Board, error) {
	return social.Board{}, authz.ErrForbidden
}
func (unavailableSocialPostService) ListManagedPosts(context.Context, authz.StaffActor, social.PostListQuery) (social.PostPage, error) {
	return social.PostPage{}, authz.ErrForbidden
}
func (unavailableSocialPostService) ModeratePost(context.Context, authz.StaffActor, social.ModeratePostInput) (social.Post, error) {
	return social.Post{}, authz.ErrForbidden
}

func (unavailableCommentService) UpdateMyComment(context.Context, string, string, social.UpdateCommentInput) (social.Comment, error) {
	return social.Comment{}, identity.ErrSessionNotFound
}

func (unavailableCommentService) DeleteMyComment(context.Context, string, string, social.DeleteCommentInput) error {
	return identity.ErrSessionNotFound
}

type unavailableCommentModerationService struct{}

func (unavailableCommentModerationService) CreateReport(context.Context, string, string, social.CreateCommentReportInput) (social.CommentReportReceipt, error) {
	return social.CommentReportReceipt{}, identity.ErrSessionNotFound
}

func (unavailableCommentModerationService) ListOpenCases(context.Context, authz.StaffActor, int, int) (social.CommentModerationCasePage, error) {
	return social.CommentModerationCasePage{}, authz.ErrForbidden
}

func (unavailableCommentModerationService) Decide(context.Context, authz.StaffActor, social.DecideCommentModerationCaseInput) (social.CommentModerationDecisionResult, error) {
	return social.CommentModerationDecisionResult{}, authz.ErrForbidden
}

type recordingCommentService struct {
	page                    social.CommentPage
	threadPage              social.CommentThreadPage
	created                 social.Comment
	updated                 social.Comment
	listTorrentID           int64
	listPostID              uuid.UUID
	listAnnouncementID      string
	listLimit               int
	listOffset              int
	listThreadSort          social.CommentThreadSort
	createCookie            string
	createCSRF              string
	createInput             social.CreateTorrentCommentInput
	createAnnouncementInput social.CreateAnnouncementCommentInput
	updateCookie            string
	updateCSRF              string
	updateInput             social.UpdateCommentInput
	deleteCookie            string
	deleteCSRF              string
	deleteInput             social.DeleteCommentInput
}

type recordingCommentModerationService struct {
	receipt      social.CommentReportReceipt
	page         social.CommentModerationCasePage
	result       social.CommentModerationDecisionResult
	reportCookie string
	reportCSRF   string
	reportInput  social.CreateCommentReportInput
	listActor    authz.StaffActor
	listLimit    int
	listOffset   int
	decideActor  authz.StaffActor
	decideInput  social.DecideCommentModerationCaseInput
}

func (service *recordingCommentModerationService) CreateReport(_ context.Context, cookie, csrf string, input social.CreateCommentReportInput) (social.CommentReportReceipt, error) {
	service.reportCookie, service.reportCSRF, service.reportInput = cookie, csrf, input
	return service.receipt, nil
}

func (service *recordingCommentModerationService) ListOpenCases(_ context.Context, actor authz.StaffActor, limit, offset int) (social.CommentModerationCasePage, error) {
	service.listActor, service.listLimit, service.listOffset = actor, limit, offset
	return service.page, nil
}

func (service *recordingCommentModerationService) Decide(_ context.Context, actor authz.StaffActor, input social.DecideCommentModerationCaseInput) (social.CommentModerationDecisionResult, error) {
	service.decideActor, service.decideInput = actor, input
	return service.result, nil
}

func (service *recordingCommentService) ListTorrentComments(_ context.Context, torrentID int64, limit, offset int) (social.CommentPage, error) {
	service.listTorrentID, service.listLimit, service.listOffset = torrentID, limit, offset
	return service.page, nil
}

func (service *recordingCommentService) ListAnnouncementComments(_ context.Context, announcementID string, limit, offset int) (social.CommentPage, error) {
	service.listAnnouncementID, service.listLimit, service.listOffset = announcementID, limit, offset
	return service.page, nil
}

func (service *recordingCommentService) ListPostComments(_ context.Context, postID uuid.UUID, sort social.CommentThreadSort, limit, offset int) (social.CommentThreadPage, error) {
	service.listPostID, service.listThreadSort, service.listLimit, service.listOffset = postID, sort, limit, offset
	return service.threadPage, nil
}

func (service *recordingCommentService) CreateTorrentComment(_ context.Context, cookie, csrf string, input social.CreateTorrentCommentInput) (social.Comment, error) {
	service.createCookie, service.createCSRF, service.createInput = cookie, csrf, input
	return service.created, nil
}

func (service *recordingCommentService) CreateAnnouncementComment(_ context.Context, cookie, csrf string, input social.CreateAnnouncementCommentInput) (social.Comment, error) {
	service.createCookie, service.createCSRF, service.createAnnouncementInput = cookie, csrf, input
	return service.created, nil
}

func (service *recordingCommentService) CreatePostComment(_ context.Context, cookie, csrf string, input social.CreatePostCommentInput) (social.Comment, error) {
	service.createCookie, service.createCSRF = cookie, csrf
	return service.created, nil
}

func (service *recordingCommentService) UpdateMyComment(_ context.Context, cookie, csrf string, input social.UpdateCommentInput) (social.Comment, error) {
	service.updateCookie, service.updateCSRF, service.updateInput = cookie, csrf, input
	return service.updated, nil
}

func (service *recordingCommentService) DeleteMyComment(_ context.Context, cookie, csrf string, input social.DeleteCommentInput) error {
	service.deleteCookie, service.deleteCSRF, service.deleteInput = cookie, csrf, input
	return nil
}

type recordingTrafficOverviewService struct {
	cookieToken  string
	limit        int
	result       traffic.Overview
	err          error
	calls        int
	hnrQuery     traffic.HNRQuery
	hnrResult    traffic.HNRPage
	hnrErr       error
	hnrCalls     int
	appealCookie string
	appealCSRF   string
	appealInput  traffic.SubmitHNRAppealInput
	appealResult traffic.HNRAppeal
	appealCalls  int
}

func (service *recordingTrafficOverviewService) MyOverview(_ context.Context, cookieToken string, limit int) (traffic.Overview, error) {
	service.calls++
	service.cookieToken, service.limit = cookieToken, limit
	return service.result, service.err
}

func (service *recordingTrafficOverviewService) MyHNR(_ context.Context, cookieToken string, query traffic.HNRQuery) (traffic.HNRPage, error) {
	service.cookieToken, service.hnrQuery = cookieToken, query
	service.hnrCalls++
	return service.hnrResult, service.hnrErr
}

func (service *recordingTrafficOverviewService) SubmitHNRAppeal(_ context.Context, cookie, csrf string, input traffic.SubmitHNRAppealInput) (traffic.HNRAppeal, error) {
	service.appealCookie, service.appealCSRF, service.appealInput = cookie, csrf, input
	service.appealCalls++
	return service.appealResult, service.hnrErr
}

func (service *recordingTrafficOverviewService) HNRAppeals(context.Context, authz.StaffActor, traffic.HNRAppealQuery) (traffic.HNRAppealPage, error) {
	return traffic.HNRAppealPage{}, service.hnrErr
}

func (service *recordingTrafficOverviewService) DecideHNRAppeal(context.Context, authz.StaffActor, traffic.DecideHNRAppealInput) (traffic.HNRAppeal, error) {
	return traffic.HNRAppeal{}, service.hnrErr
}

type unavailableTorrentUploadService struct{}

func (unavailableTorrentUploadService) Submit(context.Context, string, string, torrents.TorrentUploadInput) (torrents.TorrentUploadResult, error) {
	return torrents.TorrentUploadResult{}, torrents.ErrTorrentUploadStorageUnavailable
}

type unavailableTorrentReadService struct{}

func (unavailableTorrentReadService) Detail(context.Context, torrents.TorrentID) (torrents.PublicDetail, error) {
	return torrents.PublicDetail{}, torrents.ErrTorrentReadNotFound
}

func (unavailableTorrentReadService) Cover(context.Context, torrents.TorrentID) (torrents.PublicCover, error) {
	return torrents.PublicCover{}, torrents.ErrTorrentCoverNotFound
}

func (unavailableTorrentReadService) Screenshot(context.Context, torrents.TorrentID, int) (torrents.PublicScreenshot, error) {
	return torrents.PublicScreenshot{}, torrents.ErrTorrentScreenshotNotFound
}

func (unavailableTorrentReadService) Content(context.Context, torrents.TorrentID) (torrents.PublicContent, error) {
	return torrents.PublicContent{}, torrents.ErrTorrentReadNotFound
}

func (unavailableTorrentReadService) RelatedVersions(context.Context, torrents.TorrentID) ([]catalog.TorrentSummary, error) {
	return nil, torrents.ErrTorrentReadNotFound
}

func (unavailableTorrentReadService) Files(context.Context, torrents.TorrentID, int, int) (torrents.PublicFilePage, error) {
	return torrents.PublicFilePage{}, torrents.ErrTorrentReadNotFound
}

func (unavailableTorrentReadService) MySubmissions(context.Context, string, int) (torrents.MySubmissionPage, error) {
	return torrents.MySubmissionPage{}, identity.ErrSessionNotFound
}

func (unavailableTorrentReadService) ActivePeers(context.Context, string, torrents.TorrentID) (torrents.ManagedTorrentPeerList, error) {
	return torrents.ManagedTorrentPeerList{}, identity.ErrSessionNotFound
}

func (unavailableTorrentReadService) MyTrackerActivity(context.Context, string) (torrents.UserTrackerActivity, error) {
	return torrents.UserTrackerActivity{}, identity.ErrSessionNotFound
}

func (unavailableTorrentReadService) ListManaged(context.Context, authz.StaffActor, torrents.ManagedTorrentQuery) (torrents.ManagedTorrentPage, error) {
	return torrents.ManagedTorrentPage{}, authz.ErrForbidden
}

func (unavailableTorrentReadService) ManagedActivePeers(context.Context, authz.StaffActor, torrents.TorrentID) (torrents.ManagedTorrentPeerList, error) {
	return torrents.ManagedTorrentPeerList{}, authz.ErrForbidden
}

func (unavailableTorrentReadService) ManagedUserTrackerActivity(context.Context, authz.StaffActor, uuid.UUID) (torrents.UserTrackerActivity, error) {
	return torrents.UserTrackerActivity{}, authz.ErrForbidden
}

func (unavailableTorrentReadService) ChangeAvailability(context.Context, authz.StaffActor, torrents.ChangeTorrentAvailabilityInput) (torrents.TorrentAvailabilityResult, error) {
	return torrents.TorrentAvailabilityResult{}, authz.ErrForbidden
}

type recordingTorrentReadService struct {
	detail           torrents.PublicDetail
	cover            torrents.PublicCover
	screenshot       torrents.PublicScreenshot
	content          torrents.PublicContent
	related          []catalog.TorrentSummary
	files            torrents.PublicFilePage
	submissions      torrents.MySubmissionPage
	detailID         torrents.TorrentID
	coverID          torrents.TorrentID
	screenshotID     torrents.TorrentID
	screenshotPos    int
	contentID        torrents.TorrentID
	relatedID        torrents.TorrentID
	fileID           torrents.TorrentID
	fileLimit        int
	fileOffset       int
	submissionCookie string
	submissionLimit  int
}

func (service *recordingTorrentReadService) Detail(_ context.Context, id torrents.TorrentID) (torrents.PublicDetail, error) {
	service.detailID = id
	return service.detail, nil
}

func (service *recordingTorrentReadService) Cover(_ context.Context, id torrents.TorrentID) (torrents.PublicCover, error) {
	service.coverID = id
	return service.cover, nil
}

func (service *recordingTorrentReadService) Screenshot(_ context.Context, id torrents.TorrentID, position int) (torrents.PublicScreenshot, error) {
	service.screenshotID, service.screenshotPos = id, position
	return service.screenshot, nil
}

func (service *recordingTorrentReadService) Content(_ context.Context, id torrents.TorrentID) (torrents.PublicContent, error) {
	service.contentID = id
	return service.content, nil
}

func (service *recordingTorrentReadService) RelatedVersions(_ context.Context, id torrents.TorrentID) ([]catalog.TorrentSummary, error) {
	service.relatedID = id
	return service.related, nil
}

func (service *recordingTorrentReadService) Files(_ context.Context, id torrents.TorrentID, limit, offset int) (torrents.PublicFilePage, error) {
	service.fileID, service.fileLimit, service.fileOffset = id, limit, offset
	return service.files, nil
}

func (service *recordingTorrentReadService) MySubmissions(_ context.Context, cookieToken string, limit int) (torrents.MySubmissionPage, error) {
	service.submissionCookie, service.submissionLimit = cookieToken, limit
	return service.submissions, nil
}

func (service *recordingTorrentReadService) ActivePeers(context.Context, string, torrents.TorrentID) (torrents.ManagedTorrentPeerList, error) {
	return torrents.ManagedTorrentPeerList{}, nil
}

func (service *recordingTorrentReadService) MyTrackerActivity(context.Context, string) (torrents.UserTrackerActivity, error) {
	return torrents.UserTrackerActivity{}, nil
}

func (service *recordingTorrentReadService) ListManaged(context.Context, authz.StaffActor, torrents.ManagedTorrentQuery) (torrents.ManagedTorrentPage, error) {
	return torrents.ManagedTorrentPage{}, nil
}

func (service *recordingTorrentReadService) ManagedActivePeers(context.Context, authz.StaffActor, torrents.TorrentID) (torrents.ManagedTorrentPeerList, error) {
	return torrents.ManagedTorrentPeerList{}, nil
}

func (service *recordingTorrentReadService) ManagedUserTrackerActivity(context.Context, authz.StaffActor, uuid.UUID) (torrents.UserTrackerActivity, error) {
	return torrents.UserTrackerActivity{}, nil
}

func (service *recordingTorrentReadService) ChangeAvailability(context.Context, authz.StaffActor, torrents.ChangeTorrentAvailabilityInput) (torrents.TorrentAvailabilityResult, error) {
	return torrents.TorrentAvailabilityResult{}, nil
}

type unavailableTorrentDownloadService struct{}

func (unavailableTorrentDownloadService) Download(context.Context, string, torrents.TorrentID) (torrents.TorrentDownloadResult, error) {
	return torrents.TorrentDownloadResult{}, torrents.ErrTorrentDownloadStorageUnavailable
}

func (unavailableTorrentDownloadService) MyPurchaseStatus(context.Context, string, torrents.TorrentID) (torrentpurchase.Status, error) {
	return torrentpurchase.Status{}, torrentpurchase.ErrNotFound
}

func (unavailableTorrentDownloadService) MyPurchaseHistory(context.Context, string, int, int) (torrentpurchase.HistoryPage, error) {
	return torrentpurchase.HistoryPage{}, torrentpurchase.ErrNotFound
}

func (unavailableTorrentDownloadService) Purchase(context.Context, string, string, uuid.UUID, torrents.TorrentID) (torrentpurchase.Receipt, error) {
	return torrentpurchase.Receipt{}, torrentpurchase.ErrNotFound
}

func (unavailableTorrentDownloadService) PurchasePolicy(context.Context, authz.StaffActor) (torrentpurchase.PolicySettings, error) {
	return torrentpurchase.PolicySettings{}, authz.ErrForbidden
}

func (unavailableTorrentDownloadService) UpdatePurchasePolicy(context.Context, authz.StaffActor, torrentpurchase.UpdatePolicyCommand) (torrentpurchase.PolicySettings, error) {
	return torrentpurchase.PolicySettings{}, authz.ErrForbidden
}

func (unavailableTorrentDownloadService) UpdateTorrentPrice(context.Context, authz.StaffActor, torrentpurchase.UpdatePriceCommand) (torrentpurchase.PriceChange, error) {
	return torrentpurchase.PriceChange{}, authz.ErrForbidden
}

func (unavailableTorrentDownloadService) AdminPurchaseHistory(context.Context, authz.StaffActor, torrentpurchase.AdminPurchaseQuery) (torrentpurchase.AdminPurchasePage, error) {
	return torrentpurchase.AdminPurchasePage{}, authz.ErrForbidden
}

func (unavailableTorrentDownloadService) RefundPurchase(context.Context, authz.StaffActor, torrentpurchase.RefundCommand) (torrentpurchase.RefundReceipt, error) {
	return torrentpurchase.RefundReceipt{}, authz.ErrForbidden
}

type recordingTorrentDownloadService struct {
	cookieToken string
	torrentID   torrents.TorrentID
	result      torrents.TorrentDownloadResult
	err         error
}

func (service *recordingTorrentDownloadService) Download(_ context.Context, cookieToken string, torrentID torrents.TorrentID) (torrents.TorrentDownloadResult, error) {
	service.cookieToken = cookieToken
	service.torrentID = torrentID
	return service.result, service.err
}

func (service *recordingTorrentDownloadService) MyPurchaseStatus(context.Context, string, torrents.TorrentID) (torrentpurchase.Status, error) {
	return torrentpurchase.Status{}, torrentpurchase.ErrNotFound
}

func (service *recordingTorrentDownloadService) MyPurchaseHistory(context.Context, string, int, int) (torrentpurchase.HistoryPage, error) {
	return torrentpurchase.HistoryPage{}, torrentpurchase.ErrNotFound
}

func (service *recordingTorrentDownloadService) Purchase(context.Context, string, string, uuid.UUID, torrents.TorrentID) (torrentpurchase.Receipt, error) {
	return torrentpurchase.Receipt{}, torrentpurchase.ErrNotFound
}

func (service *recordingTorrentDownloadService) PurchasePolicy(context.Context, authz.StaffActor) (torrentpurchase.PolicySettings, error) {
	return torrentpurchase.PolicySettings{}, service.err
}

func (service *recordingTorrentDownloadService) UpdatePurchasePolicy(context.Context, authz.StaffActor, torrentpurchase.UpdatePolicyCommand) (torrentpurchase.PolicySettings, error) {
	return torrentpurchase.PolicySettings{}, service.err
}

func (service *recordingTorrentDownloadService) UpdateTorrentPrice(context.Context, authz.StaffActor, torrentpurchase.UpdatePriceCommand) (torrentpurchase.PriceChange, error) {
	return torrentpurchase.PriceChange{}, service.err
}

func (service *recordingTorrentDownloadService) AdminPurchaseHistory(context.Context, authz.StaffActor, torrentpurchase.AdminPurchaseQuery) (torrentpurchase.AdminPurchasePage, error) {
	return torrentpurchase.AdminPurchasePage{}, service.err
}

func (service *recordingTorrentDownloadService) RefundPurchase(context.Context, authz.StaffActor, torrentpurchase.RefundCommand) (torrentpurchase.RefundReceipt, error) {
	return torrentpurchase.RefundReceipt{}, service.err
}

type unavailableTorrentReviewService struct{}

func (unavailableTorrentReviewService) ListAssignments(context.Context, string, int) (review.ReviewAssignmentPage, error) {
	return review.ReviewAssignmentPage{}, authz.ErrForbidden
}

func (unavailableTorrentReviewService) GetAssignment(context.Context, string, torrents.TorrentID) (review.ReviewDetail, error) {
	return review.ReviewDetail{}, authz.ErrForbidden
}

func (unavailableTorrentReviewService) ListReviewed(context.Context, string, int) (review.ReviewedTorrentPage, error) {
	return review.ReviewedTorrentPage{}, authz.ErrForbidden
}

func (unavailableTorrentReviewService) AssignmentFiles(context.Context, string, torrents.TorrentID, int, int) (torrents.PublicFilePage, error) {
	return torrents.PublicFilePage{}, authz.ErrForbidden
}

func (unavailableTorrentReviewService) AssignmentCover(context.Context, string, torrents.TorrentID) (torrents.PublicCover, error) {
	return torrents.PublicCover{}, authz.ErrForbidden
}

func (unavailableTorrentReviewService) AssignmentScreenshot(context.Context, string, torrents.TorrentID, int) (torrents.PublicScreenshot, error) {
	return torrents.PublicScreenshot{}, authz.ErrForbidden
}

func (unavailableTorrentReviewService) Vote(context.Context, string, string, review.VoteInput) (review.VoteResult, error) {
	return review.VoteResult{}, authz.ErrForbidden
}

func (unavailableTorrentReviewService) ListPending(context.Context, authz.StaffActor, int) (review.PendingTorrentPage, error) {
	return review.PendingTorrentPage{}, authz.ErrForbidden
}

func (unavailableTorrentReviewService) Decide(context.Context, authz.StaffActor, review.DecideInput) (review.DecisionResult, error) {
	return review.DecisionResult{}, authz.ErrForbidden
}

type recordingTorrentReviewService struct {
	page        review.PendingTorrentPage
	listActor   authz.StaffActor
	listLimit   int
	decideActor authz.StaffActor
	decideInput review.DecideInput
	result      review.DecisionResult
	err         error
}

func (service *recordingTorrentReviewService) ListAssignments(context.Context, string, int) (review.ReviewAssignmentPage, error) {
	return review.ReviewAssignmentPage{}, service.err
}

func (service *recordingTorrentReviewService) GetAssignment(context.Context, string, torrents.TorrentID) (review.ReviewDetail, error) {
	return review.ReviewDetail{}, service.err
}

func (service *recordingTorrentReviewService) ListReviewed(context.Context, string, int) (review.ReviewedTorrentPage, error) {
	return review.ReviewedTorrentPage{}, service.err
}

func (service *recordingTorrentReviewService) AssignmentFiles(context.Context, string, torrents.TorrentID, int, int) (torrents.PublicFilePage, error) {
	return torrents.PublicFilePage{}, service.err
}

func (service *recordingTorrentReviewService) AssignmentCover(context.Context, string, torrents.TorrentID) (torrents.PublicCover, error) {
	return torrents.PublicCover{}, service.err
}

func (service *recordingTorrentReviewService) AssignmentScreenshot(context.Context, string, torrents.TorrentID, int) (torrents.PublicScreenshot, error) {
	return torrents.PublicScreenshot{}, service.err
}

func (service *recordingTorrentReviewService) Vote(context.Context, string, string, review.VoteInput) (review.VoteResult, error) {
	return review.VoteResult{}, service.err
}

func (service *recordingTorrentReviewService) ListPending(_ context.Context, actor authz.StaffActor, limit int) (review.PendingTorrentPage, error) {
	service.listActor = actor
	service.listLimit = limit
	return service.page, service.err
}

func (service *recordingTorrentReviewService) Decide(_ context.Context, actor authz.StaffActor, input review.DecideInput) (review.DecisionResult, error) {
	service.decideActor = actor
	service.decideInput = input
	return service.result, service.err
}

type unavailableTorrentResubmissionService struct{}

func (unavailableTorrentResubmissionService) Resubmit(context.Context, string, string, review.ResubmitInput) (review.ResubmissionResult, error) {
	return review.ResubmissionResult{}, authz.ErrForbidden
}

type unavailableTorrentMaintenanceService struct{}

func (unavailableTorrentMaintenanceService) UpdatePublishedMetadata(context.Context, string, string, torrents.UpdatePublishedMetadataInput) (torrents.PublishedMetadataRevision, error) {
	return torrents.PublishedMetadataRevision{}, authz.ErrForbidden
}

func (unavailableTorrentMaintenanceService) SubmitPublishedContentChange(context.Context, string, string, torrents.SubmitPublishedContentChangeInput) (torrents.PublishedContentChange, error) {
	return torrents.PublishedContentChange{}, authz.ErrForbidden
}

func (unavailableTorrentMaintenanceService) ListPublishedContentChanges(context.Context, authz.StaffActor, torrents.PublishedContentChangeQuery) (torrents.ManagedPublishedContentChangePage, error) {
	return torrents.ManagedPublishedContentChangePage{}, authz.ErrForbidden
}

func (unavailableTorrentMaintenanceService) DecidePublishedContentChange(context.Context, authz.StaffActor, torrents.DecidePublishedContentChangeInput) (torrents.PublishedContentChangeDecisionResult, error) {
	return torrents.PublishedContentChangeDecisionResult{}, authz.ErrForbidden
}

func (unavailableTorrentMaintenanceService) SubmitPublishedScreenshotChange(context.Context, string, string, torrents.SubmitPublishedScreenshotChangeInput) (torrents.PublishedScreenshotChange, error) {
	return torrents.PublishedScreenshotChange{}, authz.ErrForbidden
}

func (unavailableTorrentMaintenanceService) ListPublishedScreenshotChanges(context.Context, authz.StaffActor, torrents.PublishedScreenshotChangeQuery) (torrents.ManagedPublishedScreenshotChangePage, error) {
	return torrents.ManagedPublishedScreenshotChangePage{}, authz.ErrForbidden
}

func (unavailableTorrentMaintenanceService) DecidePublishedScreenshotChange(context.Context, authz.StaffActor, torrents.DecidePublishedScreenshotChangeInput) (torrents.PublishedScreenshotChangeDecisionResult, error) {
	return torrents.PublishedScreenshotChangeDecisionResult{}, authz.ErrForbidden
}

func (unavailableTorrentMaintenanceService) PublishedScreenshotChangeImage(context.Context, authz.StaffActor, uuid.UUID, torrents.ScreenshotChangeSide, int) (torrents.PublicScreenshot, error) {
	return torrents.PublicScreenshot{}, authz.ErrForbidden
}

func (unavailableTorrentMaintenanceService) SubmitTorrentWithdrawal(context.Context, string, string, torrents.SubmitTorrentWithdrawalInput) (torrents.TorrentWithdrawalRequest, error) {
	return torrents.TorrentWithdrawalRequest{}, authz.ErrForbidden
}

func (unavailableTorrentMaintenanceService) ListTorrentWithdrawals(context.Context, authz.StaffActor, torrents.TorrentWithdrawalQuery) (torrents.ManagedTorrentWithdrawalPage, error) {
	return torrents.ManagedTorrentWithdrawalPage{}, authz.ErrForbidden
}

func (unavailableTorrentMaintenanceService) DecideTorrentWithdrawal(context.Context, authz.StaffActor, torrents.DecideTorrentWithdrawalInput) (torrents.TorrentWithdrawalDecisionResult, error) {
	return torrents.TorrentWithdrawalDecisionResult{}, authz.ErrForbidden
}

func (unavailableTorrentMaintenanceService) CreateTorrentReport(context.Context, string, string, torrents.CreateTorrentReportInput) (torrents.TorrentReportReceipt, error) {
	return torrents.TorrentReportReceipt{}, authz.ErrForbidden
}

func (unavailableTorrentMaintenanceService) ListTorrentReportCases(context.Context, authz.StaffActor, torrents.TorrentReportCaseQuery) (torrents.ManagedTorrentReportCasePage, error) {
	return torrents.ManagedTorrentReportCasePage{}, authz.ErrForbidden
}

func (unavailableTorrentMaintenanceService) DecideTorrentReportCase(context.Context, authz.StaffActor, torrents.DecideTorrentReportCaseInput) (torrents.TorrentReportDecisionResult, error) {
	return torrents.TorrentReportDecisionResult{}, authz.ErrForbidden
}

type unavailablePromotionAdministrationService struct{}

func (unavailablePromotionAdministrationService) List(context.Context, authz.StaffActor, int, int) (promotions.Page, error) {
	return promotions.Page{}, authz.ErrForbidden
}

func (unavailablePromotionAdministrationService) Schedule(context.Context, authz.StaffActor, promotions.ScheduleInput) (promotions.Campaign, error) {
	return promotions.Campaign{}, authz.ErrForbidden
}

func (unavailablePromotionAdministrationService) Offer(context.Context, string, int64) (promotions.ProductOffer, error) {
	return promotions.ProductOffer{}, authz.ErrForbidden
}

func (unavailablePromotionAdministrationService) Purchase(context.Context, string, string, uuid.UUID, int64, promotions.ProductSelection) (promotions.ProductOrder, error) {
	return promotions.ProductOrder{}, authz.ErrForbidden
}

func (unavailablePromotionAdministrationService) MyOrders(context.Context, string, int, int) (promotions.ProductOrderPage, error) {
	return promotions.ProductOrderPage{}, authz.ErrForbidden
}

func (unavailablePromotionAdministrationService) ProductPolicy(context.Context, authz.StaffActor) (promotions.ProductPolicy, error) {
	return promotions.ProductPolicy{}, authz.ErrForbidden
}

func (unavailablePromotionAdministrationService) UpdateProductPolicy(context.Context, authz.StaffActor, promotions.UpdateProductPolicyCommand) (promotions.ProductPolicy, error) {
	return promotions.ProductPolicy{}, authz.ErrForbidden
}

func (unavailablePromotionAdministrationService) AdminOrders(context.Context, authz.StaffActor, promotions.ProductOrderQuery) (promotions.ProductOrderPage, error) {
	return promotions.ProductOrderPage{}, authz.ErrForbidden
}

type unavailableRatioWatchAdministrationService struct{}

func (unavailableRatioWatchAdministrationService) MyStatus(context.Context, string) (ratiowatch.MyStatus, error) {
	return ratiowatch.MyStatus{}, authz.ErrForbidden
}

func (unavailableRatioWatchAdministrationService) SubmitAppeal(context.Context, string, string, ratiowatch.SubmitAppealInput) (ratiowatch.Appeal, error) {
	return ratiowatch.Appeal{}, authz.ErrForbidden
}

func (unavailableRatioWatchAdministrationService) Policies(context.Context, authz.StaffActor, int, int) (ratiowatch.PolicyPage, error) {
	return ratiowatch.PolicyPage{}, authz.ErrForbidden
}

func (unavailableRatioWatchAdministrationService) Preview(context.Context, authz.StaffActor, ratiowatch.PolicyInput) (ratiowatch.ImpactPreview, error) {
	return ratiowatch.ImpactPreview{}, authz.ErrForbidden
}

func (unavailableRatioWatchAdministrationService) Issue(context.Context, authz.StaffActor, ratiowatch.IssueInput) (ratiowatch.PolicyRevision, error) {
	return ratiowatch.PolicyRevision{}, authz.ErrForbidden
}

func (unavailableRatioWatchAdministrationService) Assessments(context.Context, authz.StaffActor, ratiowatch.AssessmentQuery) (ratiowatch.AssessmentPage, error) {
	return ratiowatch.AssessmentPage{}, authz.ErrForbidden
}

func (unavailableRatioWatchAdministrationService) Clear(context.Context, authz.StaffActor, ratiowatch.ClearInput) (ratiowatch.Assessment, error) {
	return ratiowatch.Assessment{}, authz.ErrForbidden
}

func (unavailableRatioWatchAdministrationService) Appeals(context.Context, authz.StaffActor, ratiowatch.AppealQuery) (ratiowatch.AppealPage, error) {
	return ratiowatch.AppealPage{}, authz.ErrForbidden
}

func (unavailableRatioWatchAdministrationService) DecideAppeal(context.Context, authz.StaffActor, ratiowatch.DecideAppealInput) (ratiowatch.Appeal, error) {
	return ratiowatch.Appeal{}, authz.ErrForbidden
}

type unavailableNewcomerAdministrationService struct{}

func (unavailableNewcomerAdministrationService) MyStatus(context.Context, string) (newcomer.MyStatus, error) {
	return newcomer.MyStatus{}, authz.ErrForbidden
}

func (unavailableNewcomerAdministrationService) Policies(context.Context, authz.StaffActor, int, int) (newcomer.PolicyPage, error) {
	return newcomer.PolicyPage{}, authz.ErrForbidden
}

func (unavailableNewcomerAdministrationService) Issue(context.Context, authz.StaffActor, newcomer.IssueInput) (newcomer.PolicyRevision, error) {
	return newcomer.PolicyRevision{}, authz.ErrForbidden
}

func (unavailableNewcomerAdministrationService) Assessments(context.Context, authz.StaffActor, newcomer.AssessmentQuery) (newcomer.AssessmentPage, error) {
	return newcomer.AssessmentPage{}, authz.ErrForbidden
}

func (unavailableNewcomerAdministrationService) Assign(context.Context, authz.StaffActor, newcomer.AssignInput) (newcomer.Assessment, error) {
	return newcomer.Assessment{}, authz.ErrForbidden
}

func (unavailableNewcomerAdministrationService) Exempt(context.Context, authz.StaffActor, newcomer.ExemptInput) (newcomer.Assessment, error) {
	return newcomer.Assessment{}, authz.ErrForbidden
}

type unavailableHNRPolicyAdministrationService struct{}

func (unavailableHNRPolicyAdministrationService) List(context.Context, authz.StaffActor, int, int) (hnradmin.Page, error) {
	return hnradmin.Page{}, authz.ErrForbidden
}

func (unavailableHNRPolicyAdministrationService) Preview(context.Context, authz.StaffActor, hnradmin.PolicyInput) (hnradmin.Preview, error) {
	return hnradmin.Preview{}, authz.ErrForbidden
}

func (unavailableHNRPolicyAdministrationService) Issue(context.Context, authz.StaffActor, hnradmin.IssueInput) (hnradmin.Revision, error) {
	return hnradmin.Revision{}, authz.ErrForbidden
}

type recordingTorrentResubmissionService struct {
	cookieToken string
	csrfToken   string
	input       review.ResubmitInput
	result      review.ResubmissionResult
	err         error
}

type recordingTorrentMaintenanceService struct {
	cookieToken           string
	csrfToken             string
	input                 torrents.UpdatePublishedMetadataInput
	result                torrents.PublishedMetadataRevision
	contentInput          torrents.SubmitPublishedContentChangeInput
	contentResult         torrents.PublishedContentChange
	contentListActor      authz.StaffActor
	contentListQuery      torrents.PublishedContentChangeQuery
	contentListResult     torrents.ManagedPublishedContentChangePage
	contentDecideActor    authz.StaffActor
	contentDecideInput    torrents.DecidePublishedContentChangeInput
	contentDecision       torrents.PublishedContentChangeDecisionResult
	withdrawalInput       torrents.SubmitTorrentWithdrawalInput
	withdrawalResult      torrents.TorrentWithdrawalRequest
	withdrawalListActor   authz.StaffActor
	withdrawalListQuery   torrents.TorrentWithdrawalQuery
	withdrawalListResult  torrents.ManagedTorrentWithdrawalPage
	withdrawalDecideActor authz.StaffActor
	withdrawalDecideInput torrents.DecideTorrentWithdrawalInput
	withdrawalDecision    torrents.TorrentWithdrawalDecisionResult
	reportInput           torrents.CreateTorrentReportInput
	reportReceipt         torrents.TorrentReportReceipt
	reportListActor       authz.StaffActor
	reportListQuery       torrents.TorrentReportCaseQuery
	reportListResult      torrents.ManagedTorrentReportCasePage
	reportDecideActor     authz.StaffActor
	reportDecideInput     torrents.DecideTorrentReportCaseInput
	reportDecision        torrents.TorrentReportDecisionResult
	err                   error
}

func (service *recordingTorrentMaintenanceService) SubmitPublishedScreenshotChange(context.Context, string, string, torrents.SubmitPublishedScreenshotChangeInput) (torrents.PublishedScreenshotChange, error) {
	return torrents.PublishedScreenshotChange{}, service.err
}

func (service *recordingTorrentMaintenanceService) ListPublishedScreenshotChanges(context.Context, authz.StaffActor, torrents.PublishedScreenshotChangeQuery) (torrents.ManagedPublishedScreenshotChangePage, error) {
	return torrents.ManagedPublishedScreenshotChangePage{}, service.err
}

func (service *recordingTorrentMaintenanceService) DecidePublishedScreenshotChange(context.Context, authz.StaffActor, torrents.DecidePublishedScreenshotChangeInput) (torrents.PublishedScreenshotChangeDecisionResult, error) {
	return torrents.PublishedScreenshotChangeDecisionResult{}, service.err
}

func (service *recordingTorrentMaintenanceService) PublishedScreenshotChangeImage(context.Context, authz.StaffActor, uuid.UUID, torrents.ScreenshotChangeSide, int) (torrents.PublicScreenshot, error) {
	return torrents.PublicScreenshot{}, service.err
}

func (service *recordingTorrentMaintenanceService) SubmitPublishedContentChange(
	_ context.Context,
	cookieToken string,
	csrfToken string,
	input torrents.SubmitPublishedContentChangeInput,
) (torrents.PublishedContentChange, error) {
	service.cookieToken = cookieToken
	service.csrfToken = csrfToken
	service.contentInput = input
	return service.contentResult, service.err
}

func (service *recordingTorrentMaintenanceService) ListPublishedContentChanges(
	_ context.Context,
	actor authz.StaffActor,
	query torrents.PublishedContentChangeQuery,
) (torrents.ManagedPublishedContentChangePage, error) {
	service.contentListActor = actor
	service.contentListQuery = query
	return service.contentListResult, service.err
}

func (service *recordingTorrentMaintenanceService) DecidePublishedContentChange(
	_ context.Context,
	actor authz.StaffActor,
	input torrents.DecidePublishedContentChangeInput,
) (torrents.PublishedContentChangeDecisionResult, error) {
	service.contentDecideActor = actor
	service.contentDecideInput = input
	return service.contentDecision, service.err
}

func (service *recordingTorrentMaintenanceService) UpdatePublishedMetadata(
	_ context.Context,
	cookieToken string,
	csrfToken string,
	input torrents.UpdatePublishedMetadataInput,
) (torrents.PublishedMetadataRevision, error) {
	service.cookieToken = cookieToken
	service.csrfToken = csrfToken
	service.input = input
	return service.result, service.err
}

func (service *recordingTorrentMaintenanceService) SubmitTorrentWithdrawal(
	_ context.Context,
	cookieToken string,
	csrfToken string,
	input torrents.SubmitTorrentWithdrawalInput,
) (torrents.TorrentWithdrawalRequest, error) {
	service.cookieToken = cookieToken
	service.csrfToken = csrfToken
	service.withdrawalInput = input
	return service.withdrawalResult, service.err
}

func (service *recordingTorrentMaintenanceService) ListTorrentWithdrawals(
	_ context.Context,
	actor authz.StaffActor,
	query torrents.TorrentWithdrawalQuery,
) (torrents.ManagedTorrentWithdrawalPage, error) {
	service.withdrawalListActor = actor
	service.withdrawalListQuery = query
	return service.withdrawalListResult, service.err
}

func (service *recordingTorrentMaintenanceService) DecideTorrentWithdrawal(
	_ context.Context,
	actor authz.StaffActor,
	input torrents.DecideTorrentWithdrawalInput,
) (torrents.TorrentWithdrawalDecisionResult, error) {
	service.withdrawalDecideActor = actor
	service.withdrawalDecideInput = input
	return service.withdrawalDecision, service.err
}

func (service *recordingTorrentMaintenanceService) CreateTorrentReport(
	_ context.Context,
	cookieToken string,
	csrfToken string,
	input torrents.CreateTorrentReportInput,
) (torrents.TorrentReportReceipt, error) {
	service.cookieToken = cookieToken
	service.csrfToken = csrfToken
	service.reportInput = input
	return service.reportReceipt, service.err
}

func (service *recordingTorrentMaintenanceService) ListTorrentReportCases(
	_ context.Context,
	actor authz.StaffActor,
	query torrents.TorrentReportCaseQuery,
) (torrents.ManagedTorrentReportCasePage, error) {
	service.reportListActor = actor
	service.reportListQuery = query
	return service.reportListResult, service.err
}

func (service *recordingTorrentMaintenanceService) DecideTorrentReportCase(
	_ context.Context,
	actor authz.StaffActor,
	input torrents.DecideTorrentReportCaseInput,
) (torrents.TorrentReportDecisionResult, error) {
	service.reportDecideActor = actor
	service.reportDecideInput = input
	return service.reportDecision, service.err
}

func (service *recordingTorrentResubmissionService) Resubmit(
	_ context.Context,
	cookieToken string,
	csrfToken string,
	input review.ResubmitInput,
) (review.ResubmissionResult, error) {
	service.cookieToken = cookieToken
	service.csrfToken = csrfToken
	service.input = input
	return service.result, service.err
}

type recordingTorrentUploadService struct {
	cookieToken string
	csrfToken   string
	input       torrents.TorrentUploadInput
	result      torrents.TorrentUploadResult
	err         error
}

func (service *recordingTorrentUploadService) Submit(_ context.Context, cookieToken, csrfToken string, input torrents.TorrentUploadInput) (torrents.TorrentUploadResult, error) {
	service.cookieToken = cookieToken
	service.csrfToken = csrfToken
	service.input = input
	return service.result, service.err
}

type unavailableGrantAdministrationService struct{}

func (unavailableGrantAdministrationService) Overview(context.Context, authz.GrantAdministrationActor) (authz.GrantAdministrationOverview, error) {
	return authz.GrantAdministrationOverview{}, authz.ErrForbidden
}

func (unavailableGrantAdministrationService) ProposeRevocation(context.Context, authz.GrantAdministrationActor, authz.ProposeGrantRevocationInput) (authz.GrantRevocationRequest, error) {
	return authz.GrantRevocationRequest{}, authz.ErrForbidden
}

func (unavailableGrantAdministrationService) ReviewRevocation(context.Context, authz.GrantAdministrationActor, authz.ReviewGrantRevocationInput) (authz.GrantRevocationRequest, error) {
	return authz.GrantRevocationRequest{}, authz.ErrForbidden
}

type unavailableCategoryAdministrationService struct{}

func (unavailableCategoryAdministrationService) List(context.Context, authz.StaffActor) ([]catalog.ManagedCategory, error) {
	return nil, authz.ErrForbidden
}

func (unavailableCategoryAdministrationService) Create(context.Context, authz.StaffActor, catalog.CreateCategoryInput) (catalog.ManagedCategory, error) {
	return catalog.ManagedCategory{}, authz.ErrForbidden
}

func (unavailableCategoryAdministrationService) Update(context.Context, authz.StaffActor, catalog.UpdateCategoryInput) (catalog.ManagedCategory, error) {
	return catalog.ManagedCategory{}, authz.ErrForbidden
}

func (unavailableCategoryAdministrationService) UpsertFacet(context.Context, authz.StaffActor, catalog.UpsertCategoryFacetInput) (catalog.ManagedCategoryFacet, error) {
	return catalog.ManagedCategoryFacet{}, authz.ErrForbidden
}

func (unavailableCategoryAdministrationService) UpsertFacetOption(context.Context, authz.StaffActor, catalog.UpsertCategoryFacetOptionInput) (catalog.ManagedCategoryFacetOption, error) {
	return catalog.ManagedCategoryFacetOption{}, authz.ErrForbidden
}

type unavailableAnnouncementAdministrationService struct{}

func (unavailableAnnouncementAdministrationService) List(context.Context, authz.StaffActor, int, int) (catalog.ManagedAnnouncementPage, error) {
	return catalog.ManagedAnnouncementPage{}, authz.ErrForbidden
}

func (unavailableAnnouncementAdministrationService) Get(context.Context, authz.StaffActor, string) (catalog.ManagedAnnouncement, error) {
	return catalog.ManagedAnnouncement{}, authz.ErrForbidden
}

func (unavailableAnnouncementAdministrationService) Revisions(context.Context, authz.StaffActor, string, int, int) (catalog.AnnouncementRevisionPage, error) {
	return catalog.AnnouncementRevisionPage{}, authz.ErrForbidden
}

func (unavailableAnnouncementAdministrationService) Create(context.Context, authz.StaffActor, catalog.CreateAnnouncementDraftInput) (catalog.ManagedAnnouncement, error) {
	return catalog.ManagedAnnouncement{}, authz.ErrForbidden
}

func (unavailableAnnouncementAdministrationService) UpdateDraft(context.Context, authz.StaffActor, catalog.UpdateAnnouncementDraftInput) (catalog.ManagedAnnouncement, error) {
	return catalog.ManagedAnnouncement{}, authz.ErrForbidden
}

func (unavailableAnnouncementAdministrationService) ChangePublication(context.Context, authz.StaffActor, catalog.ChangeAnnouncementPublicationInput) (catalog.ManagedAnnouncement, error) {
	return catalog.ManagedAnnouncement{}, authz.ErrForbidden
}

type unavailableWikiService struct{}

func (unavailableWikiService) List(context.Context, string, wiki.ListInput) (wiki.PageList, error) {
	return wiki.PageList{}, identity.ErrSessionNotFound
}

func (unavailableWikiService) Get(context.Context, string, string) (wiki.Page, error) {
	return wiki.Page{}, wiki.ErrPageNotFound
}

func (unavailableWikiService) UpdateAssigned(context.Context, string, string, wiki.UpdateAssignedInput) (wiki.Page, error) {
	return wiki.Page{}, authz.ErrForbidden
}

func (unavailableWikiService) ListManaged(context.Context, authz.StaffActor, wiki.ListInput) (wiki.PageList, error) {
	return wiki.PageList{}, authz.ErrForbidden
}

func (unavailableWikiService) GetManaged(context.Context, authz.StaffActor, uuid.UUID) (wiki.Page, error) {
	return wiki.Page{}, authz.ErrForbidden
}

func (unavailableWikiService) CreateManaged(context.Context, authz.StaffActor, wiki.CreateManagedInput) (wiki.Page, error) {
	return wiki.Page{}, authz.ErrForbidden
}

func (unavailableWikiService) UpdateManaged(context.Context, authz.StaffActor, wiki.UpdateManagedInput) (wiki.Page, error) {
	return wiki.Page{}, authz.ErrForbidden
}

func (unavailableWikiService) Revisions(context.Context, authz.StaffActor, uuid.UUID, int, int) (wiki.RevisionPage, error) {
	return wiki.RevisionPage{}, authz.ErrForbidden
}

func (unavailableWikiService) RestoreManaged(context.Context, authz.StaffActor, wiki.RestoreManagedInput) (wiki.Page, error) {
	return wiki.Page{}, authz.ErrForbidden
}

type unavailableSiteDisplaySettingsService struct{}

func (unavailableSiteDisplaySettingsService) Get(context.Context, authz.StaffActor) (catalog.SiteDisplaySettings, error) {
	return catalog.SiteDisplaySettings{}, authz.ErrForbidden
}

func (unavailableSiteDisplaySettingsService) Update(context.Context, authz.StaffActor, catalog.UpdateSiteDisplaySettingsInput) (catalog.SiteDisplaySettings, error) {
	return catalog.SiteDisplaySettings{}, authz.ErrForbidden
}

type unavailableRSSService struct{}

func (unavailableRSSService) List(context.Context, string) ([]rss.Subscription, error) {
	return nil, identity.ErrSessionNotFound
}

func (unavailableRSSService) Create(context.Context, string, string, rss.SubscriptionInput) (rss.IssuedSubscription, error) {
	return rss.IssuedSubscription{}, identity.ErrSessionNotFound
}

func (unavailableRSSService) Update(context.Context, string, string, rss.UpdateSubscriptionInput) (rss.Subscription, error) {
	return rss.Subscription{}, identity.ErrSessionNotFound
}

func (unavailableRSSService) Rotate(context.Context, string, string, rss.SubscriptionVersionInput) (rss.IssuedSubscription, error) {
	return rss.IssuedSubscription{}, identity.ErrSessionNotFound
}

func (unavailableRSSService) Revoke(context.Context, string, string, rss.SubscriptionVersionInput) error {
	return identity.ErrSessionNotFound
}

func (unavailableRSSService) Feed(context.Context, string) (rss.FeedDocument, error) {
	return rss.FeedDocument{}, rss.ErrTokenInvalid
}

func (unavailableRSSService) Download(context.Context, string, int64) (torrents.TorrentDownloadResult, error) {
	return torrents.TorrentDownloadResult{}, rss.ErrTokenInvalid
}

func (unavailableRSSService) Settings(context.Context, authz.StaffActor) (rss.Settings, error) {
	return rss.Settings{}, authz.ErrForbidden
}

func (unavailableRSSService) UpdateSettings(context.Context, authz.StaffActor, rss.UpdateSettingsInput) (rss.Settings, error) {
	return rss.Settings{}, authz.ErrForbidden
}

type unavailableUserAdministrationService struct{}

func (unavailableUserAdministrationService) List(context.Context, authz.StaffActor, identity.ListManagedUsersInput) (identity.ManagedUserPage, error) {
	return identity.ManagedUserPage{}, authz.ErrForbidden
}

func (unavailableUserAdministrationService) Get(context.Context, authz.StaffActor, uuid.UUID) (identity.ManagedUserDetail, error) {
	return identity.ManagedUserDetail{}, authz.ErrForbidden
}

func (unavailableUserAdministrationService) CreateRestriction(context.Context, authz.StaffActor, identity.CreateAccountRestrictionInput) (identity.ManagedUserDetail, error) {
	return identity.ManagedUserDetail{}, authz.ErrForbidden
}

func (unavailableUserAdministrationService) RevokeRestriction(context.Context, authz.StaffActor, identity.RevokeAccountRestrictionInput) (identity.ManagedUserDetail, error) {
	return identity.ManagedUserDetail{}, authz.ErrForbidden
}

func (unavailableUserAdministrationService) Reactivate(context.Context, authz.StaffActor, identity.ReactivateManagedUserInput) (identity.ManagedUserDetail, error) {
	return identity.ManagedUserDetail{}, authz.ErrForbidden
}

func (unavailableUserAdministrationService) CreateManualDownloadRestriction(context.Context, authz.StaffActor, identity.CreateManualDownloadRestrictionInput) (identity.ManagedUserDetail, error) {
	return identity.ManagedUserDetail{}, authz.ErrForbidden
}

func (unavailableUserAdministrationService) UpdateManualDownloadRestriction(context.Context, authz.StaffActor, identity.UpdateManualDownloadRestrictionInput) (identity.ManagedUserDetail, error) {
	return identity.ManagedUserDetail{}, authz.ErrForbidden
}

func (unavailableUserAdministrationService) RevokeManualDownloadRestriction(context.Context, authz.StaffActor, identity.RevokeManualDownloadRestrictionInput) (identity.ManagedUserDetail, error) {
	return identity.ManagedUserDetail{}, authz.ErrForbidden
}

func (unavailableUserAdministrationService) ChangeVIP(context.Context, authz.StaffActor, identity.ChangeVIPInput) (identity.ManagedUserDetail, error) {
	return identity.ManagedUserDetail{}, authz.ErrForbidden
}

type recordingUserAdministrationService struct {
	listResult        identity.ManagedUserPage
	detail            identity.ManagedUserDetail
	createResult      identity.ManagedUserDetail
	revokeResult      identity.ManagedUserDetail
	listActor         authz.StaffActor
	listInput         identity.ListManagedUsersInput
	getActor          authz.StaffActor
	getUserID         uuid.UUID
	createActor       authz.StaffActor
	createInput       identity.CreateAccountRestrictionInput
	revokeActor       authz.StaffActor
	revokeInput       identity.RevokeAccountRestrictionInput
	manualCreateInput identity.CreateManualDownloadRestrictionInput
	manualUpdateInput identity.UpdateManualDownloadRestrictionInput
	manualRevokeInput identity.RevokeManualDownloadRestrictionInput
	vipInput          identity.ChangeVIPInput
	err               error
}

func (service *recordingUserAdministrationService) List(_ context.Context, actor authz.StaffActor, input identity.ListManagedUsersInput) (identity.ManagedUserPage, error) {
	service.listActor = actor
	service.listInput = input
	return service.listResult, nil
}

func (service *recordingUserAdministrationService) Get(_ context.Context, actor authz.StaffActor, userID uuid.UUID) (identity.ManagedUserDetail, error) {
	service.getActor = actor
	service.getUserID = userID
	return service.detail, service.err
}

func (service *recordingUserAdministrationService) CreateRestriction(_ context.Context, actor authz.StaffActor, input identity.CreateAccountRestrictionInput) (identity.ManagedUserDetail, error) {
	service.createActor = actor
	service.createInput = input
	return service.createResult, service.err
}

func (service *recordingUserAdministrationService) RevokeRestriction(_ context.Context, actor authz.StaffActor, input identity.RevokeAccountRestrictionInput) (identity.ManagedUserDetail, error) {
	service.revokeActor = actor
	service.revokeInput = input
	return service.revokeResult, service.err
}

func (service *recordingUserAdministrationService) Reactivate(_ context.Context, actor authz.StaffActor, input identity.ReactivateManagedUserInput) (identity.ManagedUserDetail, error) {
	service.revokeActor = actor
	return service.revokeResult, service.err
}

func (service *recordingUserAdministrationService) CreateManualDownloadRestriction(_ context.Context, actor authz.StaffActor, input identity.CreateManualDownloadRestrictionInput) (identity.ManagedUserDetail, error) {
	service.createActor = actor
	service.manualCreateInput = input
	return service.createResult, service.err
}

func (service *recordingUserAdministrationService) UpdateManualDownloadRestriction(_ context.Context, actor authz.StaffActor, input identity.UpdateManualDownloadRestrictionInput) (identity.ManagedUserDetail, error) {
	service.createActor = actor
	service.manualUpdateInput = input
	return service.createResult, service.err
}

func (service *recordingUserAdministrationService) RevokeManualDownloadRestriction(_ context.Context, actor authz.StaffActor, input identity.RevokeManualDownloadRestrictionInput) (identity.ManagedUserDetail, error) {
	service.revokeActor = actor
	service.manualRevokeInput = input
	return service.revokeResult, service.err
}

func (service *recordingUserAdministrationService) ChangeVIP(_ context.Context, actor authz.StaffActor, input identity.ChangeVIPInput) (identity.ManagedUserDetail, error) {
	service.createActor = actor
	service.vipInput = input
	return service.createResult, service.err
}

type recordingSiteDisplaySettingsService struct {
	getResult    catalog.SiteDisplaySettings
	updateResult catalog.SiteDisplaySettings
	getActor     authz.StaffActor
	updateActor  authz.StaffActor
	updateInput  catalog.UpdateSiteDisplaySettingsInput
	err          error
}

func (service *recordingSiteDisplaySettingsService) Get(_ context.Context, actor authz.StaffActor) (catalog.SiteDisplaySettings, error) {
	service.getActor = actor
	return service.getResult, service.err
}

func (service *recordingSiteDisplaySettingsService) Update(_ context.Context, actor authz.StaffActor, input catalog.UpdateSiteDisplaySettingsInput) (catalog.SiteDisplaySettings, error) {
	service.updateActor = actor
	service.updateInput = input
	return service.updateResult, service.err
}

type recordingCategoryAdministrationService struct {
	listResult   []catalog.ManagedCategory
	createResult catalog.ManagedCategory
	updateResult catalog.ManagedCategory
	listActor    authz.StaffActor
	createActor  authz.StaffActor
	updateActor  authz.StaffActor
	createInput  catalog.CreateCategoryInput
	updateInput  catalog.UpdateCategoryInput
	err          error
}

func (service *recordingCategoryAdministrationService) List(_ context.Context, actor authz.StaffActor) ([]catalog.ManagedCategory, error) {
	service.listActor = actor
	return service.listResult, service.err
}

func (service *recordingCategoryAdministrationService) Create(_ context.Context, actor authz.StaffActor, input catalog.CreateCategoryInput) (catalog.ManagedCategory, error) {
	service.createActor = actor
	service.createInput = input
	return service.createResult, service.err
}

func (service *recordingCategoryAdministrationService) Update(_ context.Context, actor authz.StaffActor, input catalog.UpdateCategoryInput) (catalog.ManagedCategory, error) {
	service.updateActor = actor
	service.updateInput = input
	return service.updateResult, service.err
}

func (service *recordingCategoryAdministrationService) UpsertFacet(_ context.Context, actor authz.StaffActor, input catalog.UpsertCategoryFacetInput) (catalog.ManagedCategoryFacet, error) {
	service.updateActor = actor
	return catalog.ManagedCategoryFacet{}, service.err
}

func (service *recordingCategoryAdministrationService) UpsertFacetOption(_ context.Context, actor authz.StaffActor, input catalog.UpsertCategoryFacetOptionInput) (catalog.ManagedCategoryFacetOption, error) {
	service.updateActor = actor
	return catalog.ManagedCategoryFacetOption{}, service.err
}

type recordingGrantAdministrationService struct {
	overviewResult authz.GrantAdministrationOverview
	overviewActor  authz.GrantAdministrationActor
	proposeResult  authz.GrantRevocationRequest
	proposeActor   authz.GrantAdministrationActor
	proposeInput   authz.ProposeGrantRevocationInput
	reviewResult   authz.GrantRevocationRequest
	reviewActor    authz.GrantAdministrationActor
	reviewInput    authz.ReviewGrantRevocationInput
	err            error
}

func (service *recordingGrantAdministrationService) Overview(_ context.Context, actor authz.GrantAdministrationActor) (authz.GrantAdministrationOverview, error) {
	service.overviewActor = actor
	return service.overviewResult, service.err
}

func (service *recordingGrantAdministrationService) ProposeRevocation(_ context.Context, actor authz.GrantAdministrationActor, input authz.ProposeGrantRevocationInput) (authz.GrantRevocationRequest, error) {
	service.proposeActor = actor
	service.proposeInput = input
	return service.proposeResult, service.err
}

func (service *recordingGrantAdministrationService) ReviewRevocation(_ context.Context, actor authz.GrantAdministrationActor, input authz.ReviewGrantRevocationInput) (authz.GrantRevocationRequest, error) {
	service.reviewActor = actor
	service.reviewInput = input
	return service.reviewResult, service.err
}

type recordingIdentityService struct {
	loginResult               identity.WebSession
	loginErr                  error
	loginInput                identity.LoginInput
	loginCalls                int
	current                   identity.WebSession
	currentErr                error
	currentToken              string
	profile                   identity.PublicUserProfile
	profileErr                error
	profileToken              string
	profileName               string
	updateResult              identity.User
	updateErr                 error
	updateToken               string
	updateCSRF                string
	updateInput               identity.UpdateMyProfileInput
	avatarResult              identity.AvatarRevision
	avatarErr                 error
	avatarToken               string
	avatarCSRF                string
	avatarData                []byte
	publicAvatar              identity.PublicAvatar
	publicAvatarErr           error
	publicAvatarToken         string
	publicAvatarName          string
	logoutToken               string
	logoutCSRF                string
	accountAccessInspectInput identity.InspectAccountAccessInput
	accountAccessStatus       identity.AccountAccessStatus
	accountAccessSubmitInput  identity.SubmitAccountAccessAppealInput
	accountAccessAppeal       identity.AccountAccessAppeal
	accountAccessQuery        identity.AccountAccessAppealQuery
	accountAccessPage         identity.AccountAccessAppealPage
	accountAccessDecision     identity.DecideAccountAccessAppealInput
	accountAccessActor        authz.StaffActor
	accountAccessErr          error
	downloadRestrictionToken  string
	downloadRestrictionCSRF   string
	downloadRestrictionInput  identity.SubmitDownloadRestrictionAppealInput
	downloadRestriction       identity.DownloadRestrictionStatus
}

type recordingStaffIdentityService struct {
	beginResult       identity.StaffElevationOptions
	beginErr          error
	beginWebToken     string
	beginCSRF         string
	completeResult    identity.StaffSession
	completeErr       error
	completeWebToken  string
	completeCSRF      string
	completeInput     identity.CompleteStaffElevationInput
	currentResult     identity.StaffSession
	currentErr        error
	currentStaffToken string
	writeStaffToken   string
	writeCSRF         string
	logoutStaffToken  string
	logoutCSRF        string
}

func (service *recordingStaffIdentityService) BeginElevation(_ context.Context, webToken, csrfToken string) (identity.StaffElevationOptions, error) {
	service.beginWebToken = webToken
	service.beginCSRF = csrfToken
	return service.beginResult, service.beginErr
}

func (service *recordingStaffIdentityService) CompleteElevation(_ context.Context, webToken, csrfToken string, input identity.CompleteStaffElevationInput) (identity.StaffSession, error) {
	service.completeWebToken = webToken
	service.completeCSRF = csrfToken
	service.completeInput = input
	return service.completeResult, service.completeErr
}

func (service *recordingStaffIdentityService) CurrentSession(_ context.Context, staffToken string) (identity.StaffSession, error) {
	service.currentStaffToken = staffToken
	return service.currentResult, service.currentErr
}

func (service *recordingStaffIdentityService) AuthenticateWrite(_ context.Context, staffToken, csrfToken string) (identity.StaffSession, error) {
	service.writeStaffToken = staffToken
	service.writeCSRF = csrfToken
	return service.currentResult, service.currentErr
}

func (service *recordingStaffIdentityService) Logout(_ context.Context, staffToken, csrfToken string) error {
	service.logoutStaffToken = staffToken
	service.logoutCSRF = csrfToken
	return nil
}

func (service *recordingIdentityService) Login(_ context.Context, input identity.LoginInput) (identity.WebSession, error) {
	service.loginCalls++
	service.loginInput = input
	return service.loginResult, service.loginErr
}

type recordingPasswordRecoveryService struct {
	dispatch     identity.PasswordRecoveryDispatch
	completion   identity.PasswordRecoveryCompletion
	email        string
	token        string
	newPassword  string
	requestCalls int
	confirmCalls int
	err          error
}

func (service *recordingPasswordRecoveryService) Request(_ context.Context, email string) (identity.PasswordRecoveryDispatch, error) {
	service.requestCalls++
	service.email = email
	return service.dispatch, service.err
}

func (service *recordingPasswordRecoveryService) Confirm(_ context.Context, token, newPassword string) (identity.PasswordRecoveryCompletion, error) {
	service.confirmCalls++
	service.token = token
	service.newPassword = newPassword
	return service.completion, service.err
}

func (service *recordingIdentityService) CurrentSession(_ context.Context, cookieToken string) (identity.WebSession, error) {
	service.currentToken = cookieToken
	return service.current, service.currentErr
}

func (service *recordingIdentityService) AuthenticateWrite(_ context.Context, cookieToken, _ string) (identity.WebSession, error) {
	service.currentToken = cookieToken
	return service.current, service.currentErr
}

func (service *recordingIdentityService) PublicProfile(_ context.Context, cookieToken, username string) (identity.PublicUserProfile, error) {
	service.profileToken = cookieToken
	service.profileName = username
	return service.profile, service.profileErr
}

func (service *recordingIdentityService) UpdateProfile(_ context.Context, cookieToken, csrfToken string, input identity.UpdateMyProfileInput) (identity.User, error) {
	service.updateToken = cookieToken
	service.updateCSRF = csrfToken
	service.updateInput = input
	return service.updateResult, service.updateErr
}

func (service *recordingIdentityService) UpdateAvatar(_ context.Context, cookieToken, csrfToken string, source io.Reader) (identity.AvatarRevision, error) {
	service.avatarToken, service.avatarCSRF = cookieToken, csrfToken
	service.avatarData, _ = io.ReadAll(source)
	return service.avatarResult, service.avatarErr
}

func (service *recordingIdentityService) PublicAvatar(_ context.Context, cookieToken, username string) (identity.PublicAvatar, error) {
	service.publicAvatarToken, service.publicAvatarName = cookieToken, username
	return service.publicAvatar, service.publicAvatarErr
}

func (service *recordingIdentityService) Logout(_ context.Context, cookieToken, csrfToken string) error {
	service.logoutToken = cookieToken
	service.logoutCSRF = csrfToken
	return nil
}

func (service *recordingIdentityService) InspectAccountAccess(_ context.Context, input identity.InspectAccountAccessInput) (identity.AccountAccessStatus, error) {
	service.accountAccessInspectInput = input
	return service.accountAccessStatus, service.accountAccessErr
}

func (service *recordingIdentityService) SubmitAccountAccessAppeal(_ context.Context, input identity.SubmitAccountAccessAppealInput) (identity.AccountAccessAppeal, error) {
	service.accountAccessSubmitInput = input
	return service.accountAccessAppeal, service.accountAccessErr
}

func (service *recordingIdentityService) AccountAccessAppeals(_ context.Context, actor authz.StaffActor, query identity.AccountAccessAppealQuery) (identity.AccountAccessAppealPage, error) {
	service.accountAccessActor = actor
	service.accountAccessQuery = query
	return service.accountAccessPage, service.accountAccessErr
}

func (service *recordingIdentityService) DecideAccountAccessAppeal(_ context.Context, actor authz.StaffActor, input identity.DecideAccountAccessAppealInput) (identity.AccountAccessAppeal, error) {
	service.accountAccessActor = actor
	service.accountAccessDecision = input
	return service.accountAccessAppeal, service.accountAccessErr
}

func (service *recordingIdentityService) MyDownloadRestriction(_ context.Context, cookieToken string) (identity.DownloadRestrictionStatus, error) {
	service.downloadRestrictionToken = cookieToken
	return service.downloadRestriction, service.accountAccessErr
}

func (service *recordingIdentityService) SubmitDownloadRestrictionAppeal(_ context.Context, cookieToken, csrfToken string, input identity.SubmitDownloadRestrictionAppealInput) (identity.AccountAccessAppeal, error) {
	service.downloadRestrictionToken = cookieToken
	service.downloadRestrictionCSRF = csrfToken
	service.downloadRestrictionInput = input
	return service.accountAccessAppeal, service.accountAccessErr
}

type recordingAuthorizationService struct {
	result         authz.CapabilitySet
	decision       authz.Decision
	authorizeInput authz.Request
	subject        authz.Subject
	staffMFAAt     time.Time
	staffAuthority authz.AuthorityBinding
	err            error
}

func (service *recordingAuthorizationService) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	service.authorizeInput = request
	return service.decision, service.err
}

func (service *recordingAuthorizationService) Capabilities(_ context.Context, subject authz.Subject) (authz.CapabilitySet, error) {
	service.subject = subject
	return service.result, service.err
}

func (service *recordingAuthorizationService) StaffCapabilities(_ context.Context, subject authz.Subject, mfaAt time.Time, authority authz.AuthorityBinding) (authz.CapabilitySet, error) {
	service.subject = subject
	service.staffMFAAt = mfaAt
	service.staffAuthority = authority
	return service.result, service.err
}

func TestCatalogEndpointsUseGeneratedContract(t *testing.T) {
	t.Parallel()

	handler := testHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/torrents?query=orchestra&limit=5&offset=0&category_id=music&promotion=none", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}

	var body generated.TorrentList
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Total != 1 || body.Limit != 5 || body.Offset != 0 || len(body.Items) != 1 || body.Items[0].Id != 5 {
		t.Fatalf("unexpected response: %+v", body)
	}

	announcementListRequest := httptest.NewRequest(http.MethodGet, "/api/v1/announcements?limit=20&offset=0", nil)
	announcementListResponse := httptest.NewRecorder()
	handler.ServeHTTP(announcementListResponse, announcementListRequest)
	if announcementListResponse.Code != http.StatusOK {
		t.Fatalf("announcement list status=%d body=%s", announcementListResponse.Code, announcementListResponse.Body.String())
	}
	var announcementPage generated.AnnouncementPage
	if err := json.NewDecoder(announcementListResponse.Body).Decode(&announcementPage); err != nil {
		t.Fatalf("decode announcement list: %v", err)
	}
	if announcementPage.Total != 1 || announcementPage.Limit != 20 || announcementPage.Offset != 0 || len(announcementPage.Items) != 1 || announcementPage.Items[0].Id != "welcome-to-peergo" {
		t.Fatalf("unexpected announcement page: %+v", announcementPage)
	}

	announcementRequest := httptest.NewRequest(http.MethodGet, "/api/v1/announcements/welcome-to-peergo", nil)
	announcementResponse := httptest.NewRecorder()
	handler.ServeHTTP(announcementResponse, announcementRequest)
	if announcementResponse.Code != http.StatusOK {
		t.Fatalf("announcement status=%d body=%s", announcementResponse.Code, announcementResponse.Body.String())
	}
	var announcement generated.AnnouncementDetail
	if err := json.NewDecoder(announcementResponse.Body).Decode(&announcement); err != nil {
		t.Fatalf("decode announcement: %v", err)
	}
	if announcement.Id != "welcome-to-peergo" || announcement.Version != 1 || announcement.Body == "" ||
		announcement.BodyFormat != generated.AnnouncementBodyFormatPlainText {
		t.Fatalf("unexpected announcement: %+v", announcement)
	}

	missingRequest := httptest.NewRequest(http.MethodGet, "/api/v1/announcements/not-published", nil)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingRequest)
	if missingResponse.Code != http.StatusNotFound || !strings.Contains(missingResponse.Body.String(), "announcement_not_found") {
		t.Fatalf("missing announcement status=%d body=%s", missingResponse.Code, missingResponse.Body.String())
	}
}

func TestTorrentSubmissionUsesMultipartContractAndWebSessionBoundary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 22, 0, 0, 0, time.UTC)
	idempotencyKey := uuid.New()
	var infoHash torrents.InfoHashV1
	copy(infoHash[:], bytes.Repeat([]byte{0xab}, len(infoHash)))
	service := &recordingTorrentUploadService{result: torrents.TorrentUploadResult{
		ID: 7, InfoHashV1: infoHash, State: torrents.StatePendingReview,
		ContentName: "release.bin", TotalSizeBytes: 42, FileCount: 1, SubmittedAt: now,
	}}
	handler := testHandlerWithTorrentUpload(t, service, 4<<20)
	csrf := strings.Repeat("c", 43)
	rawMetainfo := []byte("raw private torrent fixture")
	request := newTorrentSubmissionRequest(t, idempotencyKey, csrf, rawMetainfo)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q body=%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
	var body generated.TorrentSubmission
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Id != 7 || body.InfoHashV1 != infoHash.Hex() || body.State != generated.TorrentSubmissionStatePendingReview || body.ContentName != "release.bin" || body.TotalSizeBytes != 42 || body.FileCount != 1 || !body.SubmittedAt.Equal(now) {
		t.Fatalf("response body = %+v", body)
	}
	if service.cookieToken != "web-token" || service.csrfToken != csrf || service.input.ID != idempotencyKey ||
		service.input.CategoryID != "movies" || service.input.Title != "Release 2026" || service.input.Subtitle != "First edition" ||
		service.input.Description != "Release description" || service.input.MediaInfo != "General" || !service.input.Anonymous ||
		!reflect.DeepEqual(service.input.ExternalIdentifiers, []torrents.ExternalIdentifier{
			{Provider: "imdb", ExternalID: "tt1234567"},
			{Provider: "tmdb", ExternalID: "123"},
			{Provider: "douban", ExternalID: "456"},
		}) || !reflect.DeepEqual(service.input.FacetSelections, []torrents.FacetSelection{
		{FacetID: "genre", OptionKeys: []string{"drama", "action"}},
	}) || len(service.input.Screenshots) != 1 || !bytes.Equal(service.input.Screenshots[0].Raw, []byte("screenshot fixture")) ||
		!bytes.Equal(service.input.RawMetainfo, rawMetainfo) {
		t.Fatalf("upload boundary cookie=%q csrf=%q input=%+v", service.cookieToken, service.csrfToken, service.input)
	}

	service.err = torrents.ErrTorrentUploadDuplicate
	conflictRequest := newTorrentSubmissionRequest(t, uuid.New(), csrf, rawMetainfo)
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflictRequest)
	if conflictResponse.Code != http.StatusConflict || !strings.Contains(conflictResponse.Body.String(), "torrent_already_exists") {
		t.Fatalf("conflict status=%d body=%s", conflictResponse.Code, conflictResponse.Body.String())
	}
}

func TestTorrentResubmissionUsesBoundedJSONContractAndWebSessionBoundary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 15, 0, 0, 0, time.UTC)
	const torrentID int64 = 42
	requestID := uuid.New()
	service := &recordingTorrentResubmissionService{result: review.ResubmissionResult{
		ID: requestID, TorrentID: torrents.TorrentID(torrentID), State: torrents.StatePendingReview,
		Version: 3, Metadata: torrents.EditableMetadata{
			CategoryID: "tv", Title: "Corrected release", Subtitle: "Updated subtitle",
		}, ReviewRequestedAt: now,
	}}
	handler := testHandlerWithTorrentResubmission(t, service)
	csrf := strings.Repeat("c", 43)
	request := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/me/torrent-submissions/%d/resubmit", torrentID),
		strings.NewReader(`{"expected_version":2,"category_id":"tv","title":"Corrected release","subtitle":"Updated subtitle","correction_note":"已按审核反馈补充并修正发布信息。"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://peergo.test")
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("Idempotency-Key", requestID.String())
	request.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q body=%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
	var body generated.TorrentResubmission
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil || body.Id != requestID ||
		body.TorrentId != torrentID || body.State != generated.TorrentResubmissionStatePendingReview ||
		body.Version != 3 || body.CategoryId != "tv" || !body.ReviewRequestedAt.Equal(now) {
		t.Fatalf("response body=%+v error=%v", body, err)
	}
	if service.cookieToken != "web-token" || service.csrfToken != csrf ||
		service.input.ID != requestID || int64(service.input.TorrentID) != torrentID ||
		service.input.ExpectedVersion != 2 || service.input.CategoryID != "tv" ||
		service.input.Title != "Corrected release" || service.input.Subtitle != "Updated subtitle" ||
		service.input.CorrectionNote != "已按审核反馈补充并修正发布信息。" {
		t.Fatalf("resubmission boundary cookie=%q csrf=%q input=%+v", service.cookieToken, service.csrfToken, service.input)
	}
	if strings.Contains(response.Body.String(), "correction_note") || strings.Contains(response.Body.String(), "info_hash") {
		t.Fatalf("private correction evidence or immutable identity leaked into response: %s", response.Body.String())
	}

	service.err = review.ErrTorrentResubmissionNotAllowed
	conflictRequest := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/me/torrent-submissions/%d/resubmit", torrentID),
		strings.NewReader(`{"expected_version":2,"category_id":"tv","title":"Corrected release","subtitle":"Updated subtitle","correction_note":"已按审核反馈补充并修正发布信息。"}`),
	)
	conflictRequest.Header.Set("Content-Type", "application/json")
	conflictRequest.Header.Set("Origin", "http://peergo.test")
	conflictRequest.Header.Set("X-CSRF-Token", csrf)
	conflictRequest.Header.Set("Idempotency-Key", uuid.NewString())
	conflictRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflictRequest)
	if conflictResponse.Code != http.StatusConflict ||
		!strings.Contains(conflictResponse.Body.String(), "torrent_resubmission_not_allowed") {
		t.Fatalf("conflict status=%d body=%s", conflictResponse.Code, conflictResponse.Body.String())
	}
}

func TestPublishedTorrentMetadataUpdateUsesBoundedJSONContractAndWebSessionBoundary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 18, 16, 0, 0, 0, time.UTC)
	const torrentID int64 = 42
	requestID := uuid.New()
	service := &recordingTorrentMaintenanceService{result: torrents.PublishedMetadataRevision{
		ID: requestID, TorrentID: torrents.TorrentID(torrentID), Version: 8,
		Metadata: torrents.EditableMetadata{
			CategoryID: "tv", Title: "Corrected published release", Subtitle: "Updated subtitle",
		},
		Reason: "修正已发布种子的标题与分类说明。", UpdatedAt: now,
	}}
	handler := testHandlerWithTorrentMaintenance(t, service)
	csrf := strings.Repeat("c", 43)
	request := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/me/torrent-submissions/%d/metadata", torrentID),
		strings.NewReader(`{"expected_version":7,"category_id":"tv","title":"Corrected published release","subtitle":"Updated subtitle","reason":"修正已发布种子的标题与分类说明。"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://peergo.test")
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("Idempotency-Key", requestID.String())
	request.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q body=%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
	var body generated.PublishedTorrentMetadataRevision
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil || body.Id != requestID ||
		body.TorrentId != torrentID || body.Version != 8 || body.CategoryId != "tv" ||
		body.Title != "Corrected published release" || body.Subtitle != "Updated subtitle" ||
		body.Reason != "修正已发布种子的标题与分类说明。" || !body.UpdatedAt.Equal(now) {
		t.Fatalf("response body=%+v error=%v", body, err)
	}
	if service.cookieToken != "web-token" || service.csrfToken != csrf ||
		service.input.RequestID != requestID || int64(service.input.TorrentID) != torrentID ||
		service.input.ExpectedVersion != 7 || service.input.CategoryID != "tv" ||
		service.input.Title != "Corrected published release" || service.input.Subtitle != "Updated subtitle" ||
		service.input.Reason != "修正已发布种子的标题与分类说明。" {
		t.Fatalf("maintenance boundary cookie=%q csrf=%q input=%+v", service.cookieToken, service.csrfToken, service.input)
	}
	if strings.Contains(response.Body.String(), "info_hash") || strings.Contains(response.Body.String(), "object") {
		t.Fatalf("immutable torrent identity or storage detail leaked into response: %s", response.Body.String())
	}

	service.err = torrents.ErrTorrentMetadataUpdateVersionConflict
	conflictRequest := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/me/torrent-submissions/%d/metadata", torrentID),
		strings.NewReader(`{"expected_version":7,"category_id":"tv","title":"Corrected published release","subtitle":"Updated subtitle","reason":"修正已发布种子的标题与分类说明。"}`),
	)
	conflictRequest.Header.Set("Content-Type", "application/json")
	conflictRequest.Header.Set("Origin", "http://peergo.test")
	conflictRequest.Header.Set("X-CSRF-Token", csrf)
	conflictRequest.Header.Set("Idempotency-Key", uuid.NewString())
	conflictRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflictRequest)
	if conflictResponse.Code != http.StatusConflict ||
		!strings.Contains(conflictResponse.Body.String(), "torrent_metadata_update_version_conflict") {
		t.Fatalf("conflict status=%d body=%s", conflictResponse.Code, conflictResponse.Body.String())
	}
}

func TestPublishedTorrentContentChangeFreezesCandidateThroughWebSessionBoundary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 18, 17, 0, 0, 0, time.UTC)
	const torrentID int64 = 42
	requestID := uuid.New()
	service := &recordingTorrentMaintenanceService{contentResult: torrents.PublishedContentChange{
		ID: requestID, TorrentID: torrents.TorrentID(torrentID), BaseTorrentVersion: 7,
		Base: torrents.PublishedContentSnapshot{Description: "旧简介", MediaInfo: "旧 MediaInfo"},
		Candidate: torrents.PublishedContentSnapshot{
			Description: "新的完整简介", MediaInfo: "General\nComplete name",
			ExternalIdentifiers: []torrents.ExternalIdentifier{{Provider: "imdb", ExternalID: "tt1234567"}},
		},
		Reason: "补充完整简介、MediaInfo 与 IMDb 编号。", Status: torrents.PublishedContentChangePending,
		Version: 1, CreatedAt: now,
	}}
	handler := testHandlerWithTorrentMaintenance(t, service)
	csrf := strings.Repeat("c", 43)
	request := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/v1/me/torrent-submissions/%d/content-change", torrentID),
		strings.NewReader(`{"expected_version":7,"description":"新的完整简介","media_info":"General\nComplete name","external_identifiers":[{"provider":"imdb","external_id":"tt1234567"}],"reason":"补充完整简介、MediaInfo 与 IMDb 编号。"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://peergo.test")
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("Idempotency-Key", requestID.String())
	request.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q body=%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
	var body generated.PublishedTorrentContentChange
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil || body.Id != requestID ||
		body.TorrentId != torrentID || body.BaseTorrentVersion != 7 ||
		body.Status != generated.PublishedTorrentContentChangeStatusPending || body.Version != 1 ||
		body.Candidate.Description != "新的完整简介" || len(body.Candidate.ExternalIdentifiers) != 1 {
		t.Fatalf("response body=%+v error=%v", body, err)
	}
	if service.cookieToken != "web-token" || service.csrfToken != csrf ||
		service.contentInput.RequestID != requestID || int64(service.contentInput.TorrentID) != torrentID ||
		service.contentInput.ExpectedVersion != 7 || service.contentInput.Description != "新的完整简介" ||
		service.contentInput.MediaInfo != "General\nComplete name" ||
		!reflect.DeepEqual(service.contentInput.ExternalIdentifiers, []torrents.ExternalIdentifier{{Provider: "imdb", ExternalID: "tt1234567"}}) {
		t.Fatalf("content change boundary cookie=%q csrf=%q input=%+v", service.cookieToken, service.csrfToken, service.contentInput)
	}
	if strings.Contains(response.Body.String(), "uploader") || strings.Contains(response.Body.String(), "authorization") {
		t.Fatalf("private identity or authorization evidence leaked: %s", response.Body.String())
	}
}

func TestTorrentWithdrawalUsesWebSessionBoundaryAndReturnsSafeRequest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 18, 18, 0, 0, 0, time.UTC)
	const torrentID int64 = 42
	requestID := uuid.New()
	service := &recordingTorrentMaintenanceService{withdrawalResult: torrents.TorrentWithdrawalRequest{
		ID: requestID, TorrentID: torrents.TorrentID(torrentID), TorrentTitle: "Release 2026",
		Reason:                 "该发布已有更完整的替代版本，因此申请撤回。",
		ExpectedTorrentVersion: 7, DisabledTorrentVersion: 8,
		Status: torrents.TorrentWithdrawalPending, Version: 1, CreatedAt: now,
	}}
	handler := testHandlerWithTorrentMaintenance(t, service)
	csrf := strings.Repeat("c", 43)
	request := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/v1/me/torrent-submissions/%d/withdrawal", torrentID),
		strings.NewReader(`{"expected_version":7,"reason":"该发布已有更完整的替代版本，因此申请撤回。"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://peergo.test")
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("Idempotency-Key", requestID.String())
	request.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q body=%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
	var body generated.TorrentWithdrawalRequest
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil || body.Id != requestID ||
		body.TorrentId != torrentID || body.Status != generated.TorrentWithdrawalStatusPending ||
		body.ExpectedTorrentVersion != 7 || body.DisabledTorrentVersion != 8 || body.Version != 1 {
		t.Fatalf("response body=%+v error=%v", body, err)
	}
	if service.cookieToken != "web-token" || service.csrfToken != csrf ||
		service.withdrawalInput.RequestID != requestID || int64(service.withdrawalInput.TorrentID) != torrentID ||
		service.withdrawalInput.ExpectedVersion != 7 || service.withdrawalInput.Reason != "该发布已有更完整的替代版本，因此申请撤回。" {
		t.Fatalf("withdrawal boundary cookie=%q csrf=%q input=%+v", service.cookieToken, service.csrfToken, service.withdrawalInput)
	}
	if strings.Contains(response.Body.String(), "uploader") || strings.Contains(response.Body.String(), "authorization") ||
		strings.Contains(response.Body.String(), "info_hash") || strings.Contains(response.Body.String(), "object") {
		t.Fatalf("private identity or storage evidence leaked: %s", response.Body.String())
	}
}

func TestTorrentReportUsesWebSessionBoundaryAndReturnsSafeReceipt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 18, 19, 0, 0, 0, time.UTC)
	const torrentID int64 = 42
	requestID, reportID, caseID := uuid.New(), uuid.New(), uuid.New()
	service := &recordingTorrentMaintenanceService{reportReceipt: torrents.TorrentReportReceipt{
		ID: reportID, CaseID: caseID, TorrentID: torrents.TorrentID(torrentID),
		ReasonCode: torrents.TorrentReportMalicious, CreatedAt: now,
	}}
	handler := testHandlerWithTorrentMaintenance(t, service)
	csrf := strings.Repeat("c", 43)
	request := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/v1/torrents/%d/reports", torrentID),
		strings.NewReader(`{"reason_code":"malicious","details":"解压后的可执行文件与发布说明不符，请管理员复核。"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://peergo.test")
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("Idempotency-Key", requestID.String())
	request.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q body=%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
	var body generated.TorrentReportReceipt
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil || body.Id != reportID ||
		body.CaseId != caseID || body.TorrentId != torrentID || body.ReasonCode != generated.TorrentReportReasonCodeMalicious {
		t.Fatalf("response body=%+v error=%v", body, err)
	}
	if service.cookieToken != "web-token" || service.csrfToken != csrf ||
		service.reportInput.RequestID != requestID || int64(service.reportInput.TorrentID) != torrentID ||
		service.reportInput.ReasonCode != torrents.TorrentReportMalicious ||
		service.reportInput.Details != "解压后的可执行文件与发布说明不符，请管理员复核。" {
		t.Fatalf("report boundary cookie=%q csrf=%q input=%+v", service.cookieToken, service.csrfToken, service.reportInput)
	}
	if strings.Contains(response.Body.String(), "reporter") || strings.Contains(response.Body.String(), "uploader") ||
		strings.Contains(response.Body.String(), "authorization") || strings.Contains(response.Body.String(), "info_hash") {
		t.Fatalf("private report evidence leaked: %s", response.Body.String())
	}
}

func TestTorrentDownloadUsesBinaryContractAndKeepsCredentialOutOfTheRequest(t *testing.T) {
	t.Parallel()

	const torrentID torrents.TorrentID = 42
	passkey := strings.Repeat("a", 32)
	rawMetainfo := []byte("d8:announce63:https://tracker.peergo.test/tracker/" + passkey + "/announce4:infode")
	service := &recordingTorrentDownloadService{result: torrents.TorrentDownloadResult{
		Filename: "[ROUSI].电影 2026.torrent",
		Data:     rawMetainfo,
	}}
	handler := testHandlerWithTorrentDownload(t, service)
	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/torrents/%d/download", torrentID), nil)
	request.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/x-bittorrent" {
		t.Fatalf("status=%d content_type=%q body=%s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("cache=%q nosniff=%q", response.Header().Get("Cache-Control"), response.Header().Get("X-Content-Type-Options"))
	}
	_, parameters, err := mime.ParseMediaType(response.Header().Get("Content-Disposition"))
	if err != nil || parameters["filename"] != "[ROUSI].电影 2026.torrent" {
		t.Fatalf("content disposition=%q parameters=%v err=%v", response.Header().Get("Content-Disposition"), parameters, err)
	}
	if service.cookieToken != "web-token" || service.torrentID != torrentID || !bytes.Equal(response.Body.Bytes(), rawMetainfo) {
		t.Fatalf("download boundary token=%q torrent_id=%d body=%q", service.cookieToken, service.torrentID, response.Body.Bytes())
	}
	if strings.Contains(request.URL.String(), passkey) {
		t.Fatal("tracker credential leaked into the download request URL")
	}

	service.err = torrents.ErrTorrentDownloadObjectConflict
	unavailableRequest := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/torrents/%d/download", torrentID), nil)
	unavailableRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	unavailableResponse := httptest.NewRecorder()
	handler.ServeHTTP(unavailableResponse, unavailableRequest)
	if unavailableResponse.Code != http.StatusServiceUnavailable ||
		!strings.Contains(unavailableResponse.Body.String(), "torrent_download_unavailable") ||
		strings.Contains(unavailableResponse.Body.String(), passkey) {
		t.Fatalf("unavailable status=%d body=%s", unavailableResponse.Code, unavailableResponse.Body.String())
	}
}

func TestTorrentReadEndpointsKeepPublicAndPrivateProjectionsSeparate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 16, 0, 0, 0, time.UTC)
	const torrentID torrents.TorrentID = 42
	var infoHash torrents.InfoHashV1
	copy(infoHash[:], bytes.Repeat([]byte{0x24}, len(infoHash)))
	service := &recordingTorrentReadService{
		cover: torrents.PublicCover{
			Data: []byte("valid-png-fixture"), ContentType: "image/png",
			ETag: `"sha256-cover-fixture"`,
		},
		screenshot: torrents.PublicScreenshot{
			Data: []byte("valid-webp-screenshot"), ContentType: "image/webp",
			ETag: `"sha256-screenshot-fixture"`,
		},
		detail: torrents.PublicDetail{
			ID: torrentID, Category: catalog.Category{ID: "movies", Name: "电影"},
			Title: "Release 2026", Subtitle: "First edition", ContentName: "release",
			UploaderDisplayName: "上传者", InfoHashV1: infoHash,
			Facets: []torrents.PublicFacet{{
				FacetID: "resolution", FacetName: "分辨率", OptionKey: "1080p", OptionLabel: "1080p",
			}},
			ExternalIdentifiers: []torrents.ExternalIdentifier{{Provider: "imdb", ExternalID: "tt1234567"}},
			TotalSizeBytes:      4096, PayloadSizeBytes: 4000, FileCount: 3, PaddingFileCount: 1, ScreenshotCount: 2,
			PieceLengthBytes: 16384, PieceCount: 1, State: torrents.StatePublished,
			SubmittedAt: now.Add(-time.Hour), PublishedAt: now,
		},
		content: torrents.PublicContent{
			TorrentID: torrentID, Description: "**发布说明**",
			DescriptionFormat: "markdown", MediaInfo: "General\nFormat: Matroska",
		},
		related: []catalog.TorrentSummary{{Torrent: catalog.Torrent{
			ID: 43, Name: "Release 2026 4K", Category: catalog.Category{ID: "movies", Name: "电影"},
			SizeBytes: 8192, Promotion: catalog.PromotionFree, UploadedAt: now.Add(-2 * time.Hour),
			Swarm: catalog.SwarmStats{ObservedAt: time.Unix(0, 0).UTC()},
		}, SwarmStale: true}},
		files: torrents.PublicFilePage{
			TorrentID: torrentID, Total: 3, Limit: 2, Offset: 1,
			Items: []torrents.PublicFile{{Index: 1, DisplayPath: "release/movie.mkv", SizeBytes: 4000}},
		},
		submissions: torrents.MySubmissionPage{
			Total: 1, Limit: 10,
			Items: []torrents.MySubmission{{
				ID: torrentID, Category: catalog.Category{ID: "movies", Name: "电影"},
				Title: "Release 2026", ContentName: "release", InfoHashV1: infoHash,
				TotalSizeBytes: 4096, FileCount: 3, State: torrents.StatePublished, Version: 2,
				SubmittedAt: now.Add(-time.Hour), PublishedAt: &now, StateChangedAt: now,
				LatestReview: &torrents.ReviewFeedback{
					Outcome: torrents.StatePublished, ReasonCode: "meets_requirements",
					Reason: "已核对文件清单和发布规则，同意正式发布。", DecidedAt: now,
				},
			}},
		},
	}
	handler := testHandlerWithTorrentRead(t, service, catalog.NewService(
		catalog.NewMemoryRepository(catalog.MemoryData{Torrents: []catalog.Torrent{{
			ID: int64(torrentID), Swarm: catalog.SwarmStats{
				Seeders: 18, Leechers: 5, Completed: 61, ObservedAt: now.Add(-time.Minute),
			},
		}}}),
		func() time.Time { return now },
	))

	detailResponse := httptest.NewRecorder()
	handler.ServeHTTP(detailResponse, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/torrents/%d", torrentID), nil))
	if detailResponse.Code != http.StatusOK || detailResponse.Header().Get("Cache-Control") != "" {
		t.Fatalf("detail status=%d cache=%q body=%s", detailResponse.Code, detailResponse.Header().Get("Cache-Control"), detailResponse.Body.String())
	}
	var detail generated.TorrentPublicDetail
	if err := json.NewDecoder(detailResponse.Body).Decode(&detail); err != nil || detail.Id != int64(torrentID) ||
		detail.InfoHashV1 != infoHash.Hex() || detail.UploaderDisplayName != "上传者" ||
		len(detail.Facets) != 1 || len(detail.ExternalIdentifiers) != 1 || detail.ScreenshotCount != 2 {
		t.Fatalf("detail = %+v, error=%v", detail, err)
	}

	screenshotResponse := httptest.NewRecorder()
	handler.ServeHTTP(screenshotResponse, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/torrents/%d/screenshots/1", torrentID), nil))
	if screenshotResponse.Code != http.StatusOK || screenshotResponse.Header().Get("Content-Type") != "image/webp" ||
		screenshotResponse.Header().Get("ETag") != `"sha256-screenshot-fixture"` ||
		screenshotResponse.Header().Get("Cache-Control") != "public, max-age=300, stale-while-revalidate=86400" ||
		screenshotResponse.Body.String() != "valid-webp-screenshot" || service.screenshotID != torrentID || service.screenshotPos != 1 {
		t.Fatalf("screenshot status=%d type=%q etag=%q cache=%q body=%q id=%d position=%d", screenshotResponse.Code,
			screenshotResponse.Header().Get("Content-Type"), screenshotResponse.Header().Get("ETag"),
			screenshotResponse.Header().Get("Cache-Control"), screenshotResponse.Body.String(), service.screenshotID, service.screenshotPos)
	}
	if service.detailID != torrentID {
		t.Fatalf("detail id = %d", service.detailID)
	}

	coverResponse := httptest.NewRecorder()
	handler.ServeHTTP(coverResponse, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/torrents/%d/cover", torrentID), nil))
	if coverResponse.Code != http.StatusOK || coverResponse.Header().Get("Content-Type") != "image/png" ||
		coverResponse.Header().Get("ETag") != `"sha256-cover-fixture"` ||
		coverResponse.Header().Get("Cache-Control") != "public, max-age=300, stale-while-revalidate=86400" ||
		coverResponse.Body.String() != "valid-png-fixture" || service.coverID != torrentID {
		t.Fatalf("cover status=%d type=%q etag=%q cache=%q body=%q id=%d", coverResponse.Code,
			coverResponse.Header().Get("Content-Type"), coverResponse.Header().Get("ETag"),
			coverResponse.Header().Get("Cache-Control"), coverResponse.Body.String(), service.coverID)
	}

	contentResponse := httptest.NewRecorder()
	handler.ServeHTTP(contentResponse, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/torrents/%d/content", torrentID), nil))
	var content generated.TorrentPublicContent
	if err := json.NewDecoder(contentResponse.Body).Decode(&content); err != nil || contentResponse.Code != http.StatusOK ||
		content.TorrentId != int64(torrentID) || content.Description != "**发布说明**" || content.MediaInfo == "" {
		t.Fatalf("content status=%d content=%+v error=%v", contentResponse.Code, content, err)
	}
	if service.contentID != torrentID {
		t.Fatalf("content id = %d", service.contentID)
	}

	relatedResponse := httptest.NewRecorder()
	handler.ServeHTTP(relatedResponse, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/torrents/%d/related", torrentID), nil))
	var related generated.TorrentRelatedVersions
	if err := json.NewDecoder(relatedResponse.Body).Decode(&related); err != nil || relatedResponse.Code != http.StatusOK ||
		len(related.Items) != 1 || related.Items[0].Name != "Release 2026 4K" {
		t.Fatalf("related status=%d related=%+v error=%v", relatedResponse.Code, related, err)
	}
	if service.relatedID != torrentID {
		t.Fatalf("related id = %d", service.relatedID)
	}

	swarmResponse := httptest.NewRecorder()
	handler.ServeHTTP(swarmResponse, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/torrents/%d/swarm", torrentID), nil))
	var swarm generated.TorrentSwarmOverview
	if err := json.NewDecoder(swarmResponse.Body).Decode(&swarm); err != nil || swarmResponse.Code != http.StatusOK ||
		swarm.TorrentId != int64(torrentID) || swarm.Seeders != 18 || swarm.Leechers != 5 || swarm.Completed != 61 ||
		swarm.Confidence != generated.TorrentSwarmOverviewConfidenceFresh || swarm.Stale || swarm.ObservedAt == nil {
		t.Fatalf("swarm status=%d overview=%+v error=%v", swarmResponse.Code, swarm, err)
	}

	filesResponse := httptest.NewRecorder()
	handler.ServeHTTP(filesResponse, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/torrents/%d/files?limit=2&offset=1", torrentID), nil))
	var files generated.TorrentFilePage
	if err := json.NewDecoder(filesResponse.Body).Decode(&files); err != nil || filesResponse.Code != http.StatusOK ||
		files.TorrentId != int64(torrentID) || files.Limit != 2 || files.Offset != 1 || len(files.Items) != 1 {
		t.Fatalf("files status=%d page=%+v error=%v", filesResponse.Code, files, err)
	}
	if service.fileID != torrentID || service.fileLimit != 2 || service.fileOffset != 1 {
		t.Fatalf("file boundary id=%d limit=%d offset=%d", service.fileID, service.fileLimit, service.fileOffset)
	}

	submissionRequest := httptest.NewRequest(http.MethodGet, "/api/v1/me/torrent-submissions?limit=10", nil)
	submissionRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	submissionResponse := httptest.NewRecorder()
	handler.ServeHTTP(submissionResponse, submissionRequest)
	var submissions generated.MyTorrentSubmissionPage
	if err := json.NewDecoder(submissionResponse.Body).Decode(&submissions); err != nil || submissionResponse.Code != http.StatusOK ||
		submissionResponse.Header().Get("Cache-Control") != "no-store" || submissionResponse.Header().Get("X-Content-Type-Options") != "nosniff" ||
		len(submissions.Items) != 1 || submissions.Items[0].LatestReview == nil || submissions.Items[0].LatestReview.ReasonCode != generated.TorrentReviewReasonCodeMeetsRequirements {
		t.Fatalf("submissions status=%d cache=%q body=%+v error=%v", submissionResponse.Code, submissionResponse.Header().Get("Cache-Control"), submissions, err)
	}
	if service.submissionCookie != "web-token" || service.submissionLimit != 10 {
		t.Fatalf("submission boundary cookie=%q limit=%d", service.submissionCookie, service.submissionLimit)
	}
}

func TestTorrentReviewUsesStaffAudienceIdempotencyAndOptimisticVersion(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 8, 23, 0, 0, 0, time.UTC)
	reviewerID := uuid.New()
	uploaderID := uuid.New()
	const torrentID torrents.TorrentID = 7
	decisionID := uuid.New()
	var infoHash torrents.InfoHashV1
	copy(infoHash[:], bytes.Repeat([]byte{0x42}, len(infoHash)))
	staffCSRF := strings.Repeat("s", 43)
	staffService := &recordingStaffIdentityService{currentResult: identity.StaffSession{
		User:                    identity.User{ID: reviewerID, Username: "reviewer", DisplayName: "种子审核员"},
		WebAuthnAuthenticatedAt: now.Add(-time.Minute),
	}}
	reviewService := &recordingTorrentReviewService{
		page: review.PendingTorrentPage{Total: 1, Items: []review.PendingTorrent{{
			ID: torrentID, UploaderID: uploaderID, UploaderDisplayName: "上传者",
			CategoryID: "movies", CategoryName: "电影", Title: "Release 2026",
			Subtitle: "First edition", ContentName: "release.bin", InfoHashV1: infoHash,
			TotalSizeBytes: 42, FileCount: 1, Version: 1, SubmittedAt: now.Add(-time.Hour),
			ReviewRequestedAt: now.Add(-time.Hour),
		}}},
		result: review.DecisionResult{
			DecisionID: decisionID, TorrentID: torrentID,
			Decision: review.DecisionApprove, ReasonCode: review.ReasonMeetsRequirements,
			State: torrents.StatePublished, Version: 2, OccurredAt: now,
		},
	}
	handler := testHandlerWithTorrentReview(t, staffService, reviewService)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/torrent-reviews?limit=10", nil)
	listRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || listResponse.Header().Get("Cache-Control") != "no-store" || !strings.Contains(listResponse.Body.String(), infoHash.Hex()) {
		t.Fatalf("list status=%d cache=%q body=%s", listResponse.Code, listResponse.Header().Get("Cache-Control"), listResponse.Body.String())
	}
	if reviewService.listLimit != 10 || reviewService.listActor.Subject.ID != reviewerID || staffService.currentStaffToken != "staff-token" {
		t.Fatalf("list boundary limit=%d actor=%+v token=%q", reviewService.listLimit, reviewService.listActor, staffService.currentStaffToken)
	}
	reviewService.err = review.ErrTorrentReviewInput
	invalidListRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/torrent-reviews?limit=10", nil)
	invalidListRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	invalidListResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidListResponse, invalidListRequest)
	if invalidListResponse.Code != http.StatusBadRequest || !strings.Contains(invalidListResponse.Body.String(), "invalid_torrent_review_query") {
		t.Fatalf("invalid list status=%d body=%s", invalidListResponse.Code, invalidListResponse.Body.String())
	}
	reviewService.err = nil

	decisionRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/torrents/%d/review-decisions", torrentID), strings.NewReader(`{"expected_version":1,"decision":"approve","reason_code":"meets_requirements","reason":"已核对文件清单和发布规则，同意正式发布。"}`))
	decisionRequest.Header.Set("Content-Type", "application/json")
	decisionRequest.Header.Set("Origin", "http://peergo.test")
	decisionRequest.Header.Set("X-CSRF-Token", staffCSRF)
	decisionRequest.Header.Set("Idempotency-Key", decisionID.String())
	decisionRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	decisionResponse := httptest.NewRecorder()
	handler.ServeHTTP(decisionResponse, decisionRequest)
	if decisionResponse.Code != http.StatusCreated || decisionResponse.Header().Get("Cache-Control") != "no-store" || !strings.Contains(decisionResponse.Body.String(), `"state":"published"`) {
		t.Fatalf("decision status=%d cache=%q body=%s", decisionResponse.Code, decisionResponse.Header().Get("Cache-Control"), decisionResponse.Body.String())
	}
	if staffService.writeCSRF != staffCSRF || reviewService.decideActor.Subject.ID != reviewerID ||
		reviewService.decideInput.DecisionID != decisionID || reviewService.decideInput.TorrentID != torrentID ||
		reviewService.decideInput.ExpectedVersion != 1 || reviewService.decideInput.Decision != review.DecisionApprove {
		t.Fatalf("decision boundary csrf=%q actor=%+v input=%+v", staffService.writeCSRF, reviewService.decideActor, reviewService.decideInput)
	}

	reviewService.err = review.ErrTorrentReviewSelf
	selfRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/torrents/%d/review-decisions", torrentID), strings.NewReader(`{"expected_version":1,"decision":"reject","reason_code":"other","reason":"由另一名审核员处理该提交，避免利益冲突。"}`))
	selfRequest.Header.Set("Content-Type", "application/json")
	selfRequest.Header.Set("Origin", "http://peergo.test")
	selfRequest.Header.Set("X-CSRF-Token", staffCSRF)
	selfRequest.Header.Set("Idempotency-Key", uuid.NewString())
	selfRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	selfResponse := httptest.NewRecorder()
	handler.ServeHTTP(selfResponse, selfRequest)
	if selfResponse.Code != http.StatusForbidden || !strings.Contains(selfResponse.Body.String(), "torrent_self_review_denied") {
		t.Fatalf("self-review status=%d body=%s", selfResponse.Code, selfResponse.Body.String())
	}
}

func TestTorrentSubmissionBodyLimitRunsBeforeContractBuffering(t *testing.T) {
	t.Parallel()

	called := false
	handler := limitTorrentUploadBody(1)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/torrents",
		strings.NewReader(strings.Repeat("x", int(torrentScreenshotEnvelopeAllowance+torrentMultipartEnvelopeAllowance)+2)),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if called || response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), "torrent_upload_too_large") {
		t.Fatalf("called=%t status=%d body=%s", called, response.Code, response.Body.String())
	}
}

func TestAccountSecurityEndpointsUseRedactedContractAndSessionCookieBoundary(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	targetSessionID := uuid.New()
	service := &recordingSessionSecurityService{
		overview: identity.AccountSecurityOverview{
			EmailVerified: true, PasswordChangedAt: now.Add(-time.Hour),
			TwoFactor: identity.TwoFactorStatus{Enabled: true, EnabledAt: &now, RecoveryCodesRemaining: 8},
		},
		sessions: []identity.UserWebSession{{
			ID: targetSessionID, Current: true, CreatedAt: now.Add(-time.Hour),
			LastSeenAt: now, ExpiresAt: now.Add(time.Hour),
		}},
		revokeResult: identity.SessionRevocationResult{RevokedWebSessions: 1, CurrentSessionRevoked: true},
	}
	handler := testHandlerWithSessionSecurity(t, service)

	securityRequest := httptest.NewRequest(http.MethodGet, "/api/v1/me/security", nil)
	securityRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	securityResponse := httptest.NewRecorder()
	handler.ServeHTTP(securityResponse, securityRequest)
	if securityResponse.Code != http.StatusOK || strings.Contains(securityResponse.Body.String(), "credential_ref") || strings.Contains(securityResponse.Body.String(), "token_hash") {
		t.Fatalf("security response status=%d body=%s", securityResponse.Code, securityResponse.Body.String())
	}
	var overview generated.AccountSecurityOverview
	if err := json.NewDecoder(securityResponse.Body).Decode(&overview); err != nil || !overview.EmailVerified || !overview.PasswordChangedAt.Equal(now.Add(-time.Hour)) || !overview.TwoFactor.Enabled || overview.TwoFactor.RecoveryCodesRemaining != 8 {
		t.Fatalf("security overview = %+v, error=%v", overview, err)
	}
	if cacheControl := securityResponse.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("security Cache-Control = %q, want no-store", cacheControl)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/me/sessions", nil)
	listRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), targetSessionID.String()) || strings.Contains(listResponse.Body.String(), "web-token") {
		t.Fatalf("session list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}

	csrf := strings.Repeat("c", 43)
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/me/sessions/"+targetSessionID.String(), nil)
	deleteRequest.Header.Set("Origin", "http://peergo.test")
	deleteRequest.Header.Set("X-CSRF-Token", csrf)
	deleteRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	deleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent || !strings.Contains(deleteResponse.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("delete session status=%d cookie=%q body=%s", deleteResponse.Code, deleteResponse.Header().Get("Set-Cookie"), deleteResponse.Body.String())
	}
	if service.cookieToken != "web-token" || service.csrfToken != csrf || service.targetSessionID != targetSessionID {
		t.Fatalf("delete session input = cookie %q csrf %q target %s", service.cookieToken, service.csrfToken, service.targetSessionID)
	}
}

func TestTwoFactorEndpointsKeepSecretsTransientAndPropagateIdempotencyKeys(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	enrollmentID, enableChangeID := uuid.New(), uuid.New()
	rotateChangeID, disableChangeID := uuid.New(), uuid.New()
	service := &recordingTwoFactorService{
		startResult: identity.TOTPEnrollmentStart{
			EnrollmentID: enrollmentID, Secret: "BASE32SECRET",
			ProvisioningURI: "otpauth://totp/PeerGo:demo?secret=BASE32SECRET", ExpiresAt: now.Add(10 * time.Minute),
		},
		confirmResult: identity.TOTPEnrollmentConfirmation{
			ChangeID: enableChangeID, EnabledAt: now,
			RecoveryCodes: []string{"ABCD-EFGH-JKLM"},
		},
		rotateResult: identity.TwoFactorVaultChange{
			ChangeID: rotateChangeID, ChangedAt: now.Add(time.Minute),
			RecoveryCodes: []string{"BCDE-FGHJ-KLMN"},
		},
		disableResult: identity.TwoFactorVaultChange{ChangeID: disableChangeID, ChangedAt: now.Add(2 * time.Minute)},
	}
	handler := testHandlerWithTwoFactor(t, service)
	csrf := strings.Repeat("c", 43)

	startRequest := httptest.NewRequest(http.MethodPost, "/api/v1/me/totp/enrollments", strings.NewReader(`{"password":"current-password"}`))
	startRequest.Header.Set("Content-Type", "application/json")
	startRequest.Header.Set("Origin", "http://peergo.test")
	startRequest.Header.Set("X-CSRF-Token", csrf)
	startRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	startResponse := httptest.NewRecorder()
	handler.ServeHTTP(startResponse, startRequest)
	if startResponse.Code != http.StatusOK || !strings.Contains(startResponse.Body.String(), "BASE32SECRET") || startResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("start status=%d cache=%q body=%s", startResponse.Code, startResponse.Header().Get("Cache-Control"), startResponse.Body.String())
	}

	confirmRequest := httptest.NewRequest(http.MethodPost, "/api/v1/me/totp/enrollments/"+enrollmentID.String()+"/confirm", strings.NewReader(`{"code":"123456"}`))
	confirmRequest.Header.Set("Content-Type", "application/json")
	confirmRequest.Header.Set("Origin", "http://peergo.test")
	confirmRequest.Header.Set("X-CSRF-Token", csrf)
	confirmRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	confirmResponse := httptest.NewRecorder()
	handler.ServeHTTP(confirmResponse, confirmRequest)
	if confirmResponse.Code != http.StatusOK || !strings.Contains(confirmResponse.Body.String(), "ABCD-EFGH-JKLM") || service.confirmation.EnrollmentID != enrollmentID || service.confirmation.Code != "123456" {
		t.Fatalf("confirm status=%d command=%+v body=%s", confirmResponse.Code, service.confirmation, confirmResponse.Body.String())
	}

	rotateRequest := httptest.NewRequest(http.MethodPost, "/api/v1/me/totp/recovery-code-rotations", strings.NewReader(`{"password":"current-password","second_factor_code":"654321"}`))
	rotateRequest.Header.Set("Content-Type", "application/json")
	rotateRequest.Header.Set("Origin", "http://peergo.test")
	rotateRequest.Header.Set("X-CSRF-Token", csrf)
	rotateRequest.Header.Set("Idempotency-Key", rotateChangeID.String())
	rotateRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	rotateResponse := httptest.NewRecorder()
	handler.ServeHTTP(rotateResponse, rotateRequest)
	if rotateResponse.Code != http.StatusOK || service.reauth.ChangeID != rotateChangeID || !strings.Contains(rotateResponse.Body.String(), "BCDE-FGHJ-KLMN") {
		t.Fatalf("rotate status=%d command=%+v body=%s", rotateResponse.Code, service.reauth, rotateResponse.Body.String())
	}

	disableRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/me/totp", strings.NewReader(`{"password":"current-password","second_factor_code":"654321"}`))
	disableRequest.Header.Set("Content-Type", "application/json")
	disableRequest.Header.Set("Origin", "http://peergo.test")
	disableRequest.Header.Set("X-CSRF-Token", csrf)
	disableRequest.Header.Set("Idempotency-Key", disableChangeID.String())
	disableRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	disableResponse := httptest.NewRecorder()
	handler.ServeHTTP(disableResponse, disableRequest)
	if disableResponse.Code != http.StatusOK || service.reauth.ChangeID != disableChangeID || strings.Contains(disableResponse.Body.String(), "recovery_codes") {
		t.Fatalf("disable status=%d command=%+v body=%s", disableResponse.Code, service.reauth, disableResponse.Body.String())
	}
}

func TestCreateWebSessionReturnsSecondFactorChallengeWithoutCookie(t *testing.T) {
	t.Parallel()
	identityService := &recordingIdentityService{loginErr: identity.ErrSecondFactorRequired}
	handler := testHandlerWithIdentity(t, identityService)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"identifier":"demo","password":"correct-password","remember_me":false}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://peergo.test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusPreconditionRequired || response.Header().Get("Set-Cookie") != "" || !strings.Contains(response.Body.String(), "second_factor_required") {
		t.Fatalf("status=%d cookie=%q body=%s", response.Code, response.Header().Get("Set-Cookie"), response.Body.String())
	}
}

func TestGetPublicUserProfileUsesMemberSessionAndReturnsOnlyPublicProjection(t *testing.T) {
	t.Parallel()
	joinedAt := time.Date(2024, time.March, 18, 9, 30, 0, 0, time.UTC)
	identityService := &recordingIdentityService{profile: identity.PublicUserProfile{
		Username:              "Legacy-User",
		DisplayName:           "迁移用户",
		JoinedAt:              joinedAt,
		PublishedTorrentCount: 12,
	}}
	handler := testHandlerWithIdentity(t, identityService)

	withoutSession := httptest.NewRecorder()
	handler.ServeHTTP(withoutSession, httptest.NewRequest(http.MethodGet, "/api/v1/users/Legacy-User", nil))
	if withoutSession.Code != http.StatusUnauthorized {
		t.Fatalf("without session status=%d body=%s", withoutSession.Code, withoutSession.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/legacy-user", nil)
	request.AddCookie(&http.Cookie{Name: "peergo_session", Value: "member-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if identityService.profileToken != "member-token" || identityService.profileName != "legacy-user" {
		t.Fatalf("PublicProfile token=%q username=%q", identityService.profileToken, identityService.profileName)
	}
	if response.Header().Get("Cache-Control") != "private, max-age=60" {
		t.Fatalf("cache-control=%q", response.Header().Get("Cache-Control"))
	}
	var body generated.PublicUserProfile
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Username != "Legacy-User" || body.DisplayName != "迁移用户" || body.JoinedAt != joinedAt || body.PublishedTorrentCount != 12 {
		t.Fatalf("body=%+v", body)
	}
	for _, privateField := range []string{"email", "credential", "ip", "restriction", "traffic", "user_id"} {
		if strings.Contains(response.Body.String(), privateField) {
			t.Fatalf("public profile leaked %q: %s", privateField, response.Body.String())
		}
	}
}

func TestOpenAPIValidationAcceptsCatalogLimit100AndRejects101(t *testing.T) {
	t.Parallel()

	handler := testHandler(t)
	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, httptest.NewRequest(http.MethodGet, "/api/v1/torrents?limit=100", nil))
	if accepted.Code != http.StatusOK {
		t.Fatalf("limit=100 status = %d, want %d; body=%s", accepted.Code, http.StatusOK, accepted.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/torrents?limit=101", nil)
	request.Header.Set("X-Request-Id", "attacker-controlled-request-id")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", contentType)
	}

	var body generated.Problem
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if body.Code != "contract_validation_failed" {
		t.Fatalf("problem code = %q, want contract_validation_failed", body.Code)
	}
	if _, err := uuid.Parse(body.RequestId); err != nil {
		t.Fatalf("request_id = %q, want opaque UUID: %v", body.RequestId, err)
	}
	if body.RequestId == "attacker-controlled-request-id" || strings.Contains(body.RequestId, "/") {
		t.Fatalf("request_id = %q, want server-generated ID without hostname", body.RequestId)
	}
}

func TestProductionRegistrationPolicyUpdatePassesOpenAPIValidation(t *testing.T) {
	t.Parallel()

	body := `{
		"mode":"invite",
		"member_invites_enabled":true,
		"invite_valid_days":7,
		"max_invites_per_member":5,
		"minimum_invite_account_age_days":30,
		"minimum_invite_level":2,
		"username_min_characters":3,
		"username_max_characters":32,
		"reserved_usernames":[],
		"email_domain_mode":"any",
		"email_domains":[],
		"session_valid_hours":24,
		"remember_session_valid_hours":720,
		"human_verification_provider":"disabled",
		"human_verification_site_key":"",
		"human_verification_registration_enabled":false,
		"human_verification_login_enabled":false,
		"human_verification_password_recovery_enabled":false,
		"expected_version":1,
		"reason":""
	}`
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings/registration",
		strings.NewReader(body),
	)
	request.Host = "rousi.pro"
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://rousi.pro")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-CSRF-Token", "opaque-csrf-token")
	request.Header.Set("Cookie", "__Host-peergo_session=opaque-session-token")
	response := httptest.NewRecorder()

	testHandler(t).ServeHTTP(response, request)

	if response.Code == http.StatusBadRequest && strings.Contains(response.Body.String(), "contract_validation_failed") {
		t.Fatalf("production registration update failed contract validation: %s", response.Body.String())
	}
}

func TestCreateWebSessionRejectsWriteWithoutTrustedOrigin(t *testing.T) {
	t.Parallel()

	identityService := &recordingIdentityService{}
	handler := testHandlerWithIdentity(t, identityService)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/session",
		strings.NewReader(`{"identifier":"demo","password":"secret","remember_me":false}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusForbidden, response.Body.String())
	}
	if identityService.loginCalls != 0 {
		t.Fatalf("Login() calls = %d, want 0", identityService.loginCalls)
	}
	var body generated.Problem
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if body.Code != "origin_not_allowed" {
		t.Fatalf("problem code = %q, want origin_not_allowed", body.Code)
	}
}

func TestCreateRegistrationMapsIdempotencyAndKeepsSecretsOutOfResponse(t *testing.T) {
	t.Parallel()
	registrationID := uuid.MustParse("0198f20a-6da8-7e51-9c64-616161616161")
	userID := uuid.MustParse("0198f20a-6da8-7e51-9c64-626262626262")
	now := time.Date(2026, time.August, 6, 9, 30, 0, 0, time.UTC)
	service := &recordingRegistrationService{result: identity.RegistrationResult{
		UserID: userID, Username: "new_member", DisplayName: "新成员",
		RegistrationMode:          identity.RegistrationModeInvite,
		EmailVerificationRequired: true, CompletedAt: now,
	}}
	handler := testHandlerWithRegistration(t, service)
	body := `{"username":"new_member","display_name":"新成员","email":"member@example.com","password":"PeerGo-member-2026!","invitation_token":"cGVlcmdvLWRldmVsb3BtZW50LWludml0ZS12MSEhISE"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/registrations", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://peergo.test")
	request.Header.Set("Idempotency-Key", registrationID.String())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if service.input.ID != registrationID || service.input.Email != "member@example.com" || service.input.InvitationToken == "" {
		t.Fatalf("registration input = %+v", service.input)
	}
	for _, secret := range []string{"member@example.com", "PeerGo-member-2026!", "cGVlcmdvLWRldmVsb3BtZW50LWludml0ZS12MSEhISE"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("registration response leaked secret %q: %s", secret, response.Body.String())
		}
	}
	if !strings.Contains(response.Body.String(), `"username":"new_member"`) || !strings.Contains(response.Body.String(), `"email_verification_required":true`) {
		t.Fatalf("response body = %s", response.Body.String())
	}
}

func TestCreateRegistrationRequiresConfiguredHumanVerificationBeforeAccountWork(t *testing.T) {
	t.Parallel()

	registrationID := uuid.MustParse("0198f20a-6da8-7e51-9c64-636363636363")
	service := &recordingRegistrationService{
		publicPolicy: identity.RegistrationPublicPolicy{
			Mode:                                 identity.RegistrationModeOpen,
			UsernameMinCharacters:                3,
			UsernameMaxCharacters:                32,
			EmailDomainMode:                      identity.EmailDomainModeAny,
			HumanVerificationProvider:            identity.HumanVerificationProviderTurnstile,
			HumanVerificationRegistrationEnabled: true,
		},
		result: identity.RegistrationResult{
			UserID:   uuid.MustParse("0198f20a-6da8-7e51-9c64-646464646464"),
			Username: "verified_member", DisplayName: "已验证成员",
			RegistrationMode: identity.RegistrationModeOpen, CompletedAt: time.Now(),
		},
	}
	verifier := &recordingHumanVerificationVerifier{err: identity.ErrHumanVerificationRequired}
	handler := testHandlerWithRegistrationAndHumanVerification(t, service, verifier)
	baseBody := `{"username":"verified_member","display_name":"已验证成员","email":"verified@example.com","password":"PeerGo-member-2026!"}`

	missingRequest := httptest.NewRequest(http.MethodPost, "/api/v1/registrations", strings.NewReader(baseBody))
	missingRequest.Header.Set("Content-Type", "application/json")
	missingRequest.Header.Set("Origin", "http://peergo.test")
	missingRequest.Header.Set("Idempotency-Key", registrationID.String())
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingRequest)
	if missingResponse.Code != http.StatusForbidden || !strings.Contains(missingResponse.Body.String(), "human_verification_required") || service.input.ID != uuid.Nil {
		t.Fatalf("missing status=%d input=%+v body=%s", missingResponse.Code, service.input, missingResponse.Body.String())
	}

	verifier.err = nil
	verifiedBody := strings.TrimSuffix(baseBody, "}") + `,"human_verification_token":"single-use-browser-token"}`
	verifiedRequest := httptest.NewRequest(http.MethodPost, "/api/v1/registrations", strings.NewReader(verifiedBody))
	verifiedRequest.Header.Set("Content-Type", "application/json")
	verifiedRequest.Header.Set("Origin", "http://peergo.test")
	verifiedRequest.Header.Set("Idempotency-Key", registrationID.String())
	verifiedResponse := httptest.NewRecorder()
	handler.ServeHTTP(verifiedResponse, verifiedRequest)
	if verifiedResponse.Code != http.StatusCreated || service.input.ID != registrationID || verifier.flow != identity.HumanVerificationFlowRegistration || verifier.token != "single-use-browser-token" || verifier.calls != 2 {
		t.Fatalf("verified status=%d verifier=%+v input=%+v body=%s", verifiedResponse.Code, verifier, service.input, verifiedResponse.Body.String())
	}
}

func TestCreateWebSessionKeepsRawTokenOutOfJSON(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	csrfToken := strings.Repeat("c", 43)
	identityService := &recordingIdentityService{loginResult: identity.WebSession{
		User: identity.User{
			ID:          uuid.MustParse("018f934d-fbe4-7f1d-a920-1ad928bdb0ab"),
			Username:    "demo",
			DisplayName: "PeerGo Demo",
		},
		CreatedAt:   now,
		ExpiresAt:   now.Add(12 * time.Hour),
		CSRFToken:   csrfToken,
		CookieToken: "raw-browser-session-token",
	}}
	handler := testHandlerWithIdentity(t, identityService)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/session",
		strings.NewReader(`{"identifier":"demo","password":"secret","remember_me":true}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://peergo.test")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if identityService.loginInput.Identifier != "demo" || identityService.loginInput.Password != "secret" || !identityService.loginInput.RememberMe {
		t.Fatalf("unexpected login input: %+v", identityService.loginInput)
	}
	setCookie := response.Header().Get("Set-Cookie")
	for _, required := range []string{
		"peergo_session=raw-browser-session-token",
		"Path=/",
		"HttpOnly",
		"SameSite=Strict",
	} {
		if !strings.Contains(setCookie, required) {
			t.Fatalf("Set-Cookie = %q, missing %q", setCookie, required)
		}
	}
	if strings.Contains(response.Body.String(), "raw-browser-session-token") {
		t.Fatal("raw browser token leaked into JSON response")
	}

	var body generated.WebSession
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.User.Username != "demo" || body.CsrfToken != csrfToken {
		t.Fatalf("unexpected response: %+v", body)
	}
	if cacheControl := response.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cacheControl)
	}
}

func TestCreateWebSessionMapsPersistentThrottleWithoutAccountSignal(t *testing.T) {
	t.Parallel()
	identityService := &recordingIdentityService{loginErr: &identity.LoginThrottleError{
		RetryAt: time.Date(2026, time.August, 6, 22, 0, 0, 0, time.UTC),
	}}
	handler := testHandlerWithIdentity(t, identityService)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"identifier":"unknown@example.com","password":"wrong","remember_me":false}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://peergo.test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), "login_throttled") || strings.Contains(response.Body.String(), "unknown@example.com") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPasswordRecoveryEndpointsUseEnumerationSafeGeneratedContract(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 6, 22, 0, 0, 0, time.UTC)
	recovery := &recordingPasswordRecoveryService{
		dispatch:   identity.PasswordRecoveryDispatch{AcceptedAt: now, NextRequestAt: now.Add(2 * time.Minute)},
		completion: identity.PasswordRecoveryCompletion{RecoveryID: uuid.New(), PasswordChangedAt: now.Add(time.Minute), RevokedSessions: 2, Changed: true},
	}
	handler := testHandlerWithPasswordRecovery(t, recovery)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/password-recovery-requests", strings.NewReader(`{"email":"member@example.com"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://peergo.test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || recovery.email != "member@example.com" || strings.Contains(response.Body.String(), "member@example.com") {
		t.Fatalf("request status=%d body=%s recovery=%+v", response.Code, response.Body.String(), recovery)
	}

	token := strings.Repeat("a", 43)
	confirmRequest := httptest.NewRequest(http.MethodPost, "/api/v1/password-recoveries/confirm", strings.NewReader(`{"token":"`+token+`","new_password":"PeerGo-new-password-2026!"}`))
	confirmRequest.Header.Set("Content-Type", "application/json")
	confirmRequest.Header.Set("Origin", "http://peergo.test")
	confirmResponse := httptest.NewRecorder()
	handler.ServeHTTP(confirmResponse, confirmRequest)
	if confirmResponse.Code != http.StatusOK || recovery.token != token || recovery.newPassword != "PeerGo-new-password-2026!" || !strings.Contains(confirmResponse.Body.String(), `"already_completed":false`) {
		t.Fatalf("confirm status=%d body=%s recovery=%+v", confirmResponse.Code, confirmResponse.Body.String(), recovery)
	}
}

func TestDeleteWebSessionRequiresCookieAndCSRFThenExpiresCookie(t *testing.T) {
	t.Parallel()

	csrfToken := strings.Repeat("c", 43)
	identityService := &recordingIdentityService{}
	handler := testHandlerWithIdentity(t, identityService)
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/session", nil)
	request.Header.Set("Origin", "http://peergo.test")
	request.Header.Set("X-CSRF-Token", csrfToken)
	request.AddCookie(&http.Cookie{Name: "peergo_session", Value: "raw-browser-session-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if identityService.logoutToken != "raw-browser-session-token" || identityService.logoutCSRF != csrfToken {
		t.Fatalf("Logout() received token=%q csrf=%q", identityService.logoutToken, identityService.logoutCSRF)
	}
	setCookie := response.Header().Get("Set-Cookie")
	for _, required := range []string{"peergo_session=", "Max-Age=0", "HttpOnly", "SameSite=Strict"} {
		if !strings.Contains(setCookie, required) {
			t.Fatalf("Set-Cookie = %q, missing %q", setCookie, required)
		}
	}
}

func TestUpdateMyProfileUsesGeneratedContractAndSessionBoundWrite(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	csrfToken := strings.Repeat("c", 43)
	identityService := &recordingIdentityService{updateResult: identity.User{
		ID: userID, Username: "demo", DisplayName: "星河新昵称",
	}}
	handler := testHandlerWithIdentity(t, identityService)
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/me/profile", strings.NewReader(`{"display_name":"星河新昵称"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://peergo.test")
	request.Header.Set("X-CSRF-Token", csrfToken)
	request.AddCookie(&http.Cookie{Name: "peergo_session", Value: "raw-browser-session-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"display_name":"星河新昵称"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if identityService.updateToken != "raw-browser-session-token" || identityService.updateCSRF != csrfToken || identityService.updateInput.DisplayName != "星河新昵称" {
		t.Fatalf("update input = token %q csrf %q body %+v", identityService.updateToken, identityService.updateCSRF, identityService.updateInput)
	}
	if cacheControl := response.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cacheControl)
	}
}

func TestUserAvatarEndpointsUseBinaryContractAndPrivateVerifiedResponse(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 13, 10, 30, 0, 0, time.UTC)
	csrfToken := strings.Repeat("c", 43)
	identityService := &recordingIdentityService{
		avatarResult: identity.AvatarRevision{Revision: strings.Repeat("a", 64), UpdatedAt: now},
		publicAvatar: identity.PublicAvatar{Data: []byte("verified-avatar"), ContentType: "image/jpeg", Revision: strings.Repeat("a", 64), UpdatedAt: now},
	}
	handler := testHandlerWithIdentity(t, identityService)
	upload := httptest.NewRequest(http.MethodPut, "/api/v1/me/avatar", strings.NewReader("jpeg-avatar"))
	upload.Header.Set("Content-Type", "image/jpeg")
	upload.Header.Set("Origin", "http://peergo.test")
	upload.Header.Set("X-CSRF-Token", csrfToken)
	upload.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-avatar-token"})
	uploadResponse := httptest.NewRecorder()
	handler.ServeHTTP(uploadResponse, upload)
	if uploadResponse.Code != http.StatusOK || uploadResponse.Header().Get("Cache-Control") != "no-store" ||
		identityService.avatarToken != "web-avatar-token" || identityService.avatarCSRF != csrfToken ||
		string(identityService.avatarData) != "jpeg-avatar" || !strings.Contains(uploadResponse.Body.String(), strings.Repeat("a", 64)) {
		t.Fatalf("upload status=%d headers=%v service=%+v body=%s", uploadResponse.Code, uploadResponse.Header(), identityService, uploadResponse.Body.String())
	}

	read := httptest.NewRequest(http.MethodGet, "/api/v1/users/member/avatar", nil)
	read.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-avatar-token"})
	readResponse := httptest.NewRecorder()
	handler.ServeHTTP(readResponse, read)
	if readResponse.Code != http.StatusOK || readResponse.Header().Get("Content-Type") != "image/jpeg" ||
		readResponse.Header().Get("Cache-Control") != "private, no-cache" ||
		readResponse.Header().Get("ETag") != `"`+strings.Repeat("a", 64)+`"` ||
		readResponse.Body.String() != "verified-avatar" || identityService.publicAvatarToken != "web-avatar-token" || identityService.publicAvatarName != "member" {
		t.Fatalf("read status=%d headers=%v service=%+v body=%s", readResponse.Code, readResponse.Header(), identityService, readResponse.Body.String())
	}
}

func TestAvatarBodyLimitRunsBeforeContractBuffering(t *testing.T) {
	t.Parallel()
	handler := limitUploadBody(4 << 20)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("oversized avatar reached next handler")
	}))
	request := httptest.NewRequest(http.MethodPut, "/api/v1/me/avatar", strings.NewReader("oversized"))
	request.ContentLength = int64(identity.MaxAvatarBytes + 1)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), "avatar_too_large") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGetMyCapabilitiesRequiresSessionAndReturnsOnlyProjection(t *testing.T) {
	t.Parallel()

	withoutSession := httptest.NewRecorder()
	testHandler(t).ServeHTTP(withoutSession, httptest.NewRequest(http.MethodGet, "/api/v1/me/capabilities", nil))
	if withoutSession.Code != http.StatusUnauthorized {
		t.Fatalf("without session status = %d, want %d", withoutSession.Code, http.StatusUnauthorized)
	}

	userID := uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111")
	expiresAt := time.Date(2027, time.August, 5, 12, 0, 0, 0, time.UTC)
	identityService := &recordingIdentityService{current: identity.WebSession{
		User: identity.User{ID: userID, Username: "demo", DisplayName: "PeerGo Demo"},
	}}
	authorizationService := &recordingAuthorizationService{result: authz.CapabilitySet{
		PolicyVersion: authz.PolicyVersion,
		Items: []authz.Capability{{
			Action:      authz.ActionCapabilityReadSelf,
			Description: "查看自己的当前有效权限",
			Scope:       authz.SiteScope(),
			ExpiresAt:   expiresAt,
		}},
	}}
	handler := testHandlerWithServices(t, identityService, authorizationService)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/me/capabilities", nil)
	request.AddCookie(&http.Cookie{Name: "peergo_session", Value: "raw-browser-session-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if identityService.currentToken != "raw-browser-session-token" {
		t.Fatalf("CurrentSession() token = %q", identityService.currentToken)
	}
	if authorizationService.subject.ID != userID || authorizationService.subject.Status != authz.SubjectActive {
		t.Fatalf("Capabilities() subject = %+v", authorizationService.subject)
	}
	if cacheControl := response.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cacheControl)
	}
	if strings.Contains(response.Body.String(), "grant_id") || strings.Contains(response.Body.String(), "mandate_id") {
		t.Fatal("authorization internals leaked into capability projection")
	}
	var body generated.CapabilityList
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.PolicyVersion != authz.PolicyVersion || len(body.Items) != 1 || body.Items[0].Action != generated.CapabilityActionAuthzCapabilityReadSelf {
		t.Fatalf("unexpected response: %+v", body)
	}
}

func TestStaffElevationUsesWebAudienceAndReturnsNarrowStaffCookie(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	challengeID := uuid.MustParse("0198f20a-6da8-7e51-9c64-333333333333")
	csrfToken := strings.Repeat("c", 43)
	staffCSRF := strings.Repeat("s", 43)
	staffService := &recordingStaffIdentityService{
		beginResult: identity.StaffElevationOptions{
			ChallengeID: challengeID,
			ExpiresAt:   now.Add(5 * time.Minute),
			PublicKey: json.RawMessage(`{
				"challenge":"Y2hhbGxlbmdlLW11c3QtYmUtbG9uZw",
				"timeout":300000,
				"rpId":"peergo.test",
				"allowCredentials":[{"type":"public-key","id":"Y3JlZGVudGlhbC1vbmU"}],
				"userVerification":"required"
			}`),
		},
		completeResult: identity.StaffSession{
			User: identity.User{
				ID:          uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111"),
				Username:    "staff-demo",
				DisplayName: "Staff Demo",
			},
			CreatedAt:               now,
			ExpiresAt:               now.Add(15 * time.Minute),
			WebAuthnAuthenticatedAt: now,
			CSRFToken:               staffCSRF,
			CookieToken:             "raw-staff-session-token",
		},
	}
	handler := testHandlerWithAllServices(t, unavailableIdentityService{}, staffService, unavailableAuthorizationService{})

	beginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/staff/elevation/options", nil)
	beginRequest.Header.Set("Origin", "http://peergo.test")
	beginRequest.Header.Set("X-CSRF-Token", csrfToken)
	beginRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "raw-web-session-token"})
	beginRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "untrusted-existing-staff-token"})
	beginResponse := httptest.NewRecorder()
	handler.ServeHTTP(beginResponse, beginRequest)

	if beginResponse.Code != http.StatusOK {
		t.Fatalf("begin status = %d, want %d; body=%s", beginResponse.Code, http.StatusOK, beginResponse.Body.String())
	}
	if staffService.beginWebToken != "raw-web-session-token" || staffService.beginCSRF != csrfToken {
		t.Fatalf("BeginElevation() web token=%q csrf=%q", staffService.beginWebToken, staffService.beginCSRF)
	}
	if cacheControl := beginResponse.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("begin Cache-Control = %q, want no-store", cacheControl)
	}
	var options generated.StaffElevationOptions
	if err := json.NewDecoder(beginResponse.Body).Decode(&options); err != nil {
		t.Fatalf("decode begin response: %v", err)
	}
	if options.ChallengeId != challengeID || options.PublicKey.UserVerification != generated.WebAuthnRequestOptionsUserVerificationRequired {
		t.Fatalf("begin response = %+v", options)
	}

	completeBody := `{
		"challenge_id":"0198f20a-6da8-7e51-9c64-333333333333",
		"credential":{
			"id":"Y3JlZGVudGlhbC1vbmU",
			"rawId":"Y3JlZGVudGlhbC1vbmU",
			"type":"public-key",
			"response":{"clientDataJSON":"YQ","authenticatorData":"YQ","signature":"YQ"},
			"clientExtensionResults":{}
		}
	}`
	completeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/staff/elevation", strings.NewReader(completeBody))
	completeRequest.Header.Set("Content-Type", "application/json")
	completeRequest.Header.Set("Origin", "http://peergo.test")
	completeRequest.Header.Set("X-CSRF-Token", csrfToken)
	completeRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "raw-web-session-token"})
	completeRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "untrusted-existing-staff-token"})
	completeResponse := httptest.NewRecorder()
	handler.ServeHTTP(completeResponse, completeRequest)

	if completeResponse.Code != http.StatusOK {
		t.Fatalf("complete status = %d, want %d; body=%s", completeResponse.Code, http.StatusOK, completeResponse.Body.String())
	}
	if staffService.completeWebToken != "raw-web-session-token" || staffService.completeCSRF != csrfToken || staffService.completeInput.ChallengeID != challengeID {
		t.Fatalf("CompleteElevation() token=%q csrf=%q input=%+v", staffService.completeWebToken, staffService.completeCSRF, staffService.completeInput)
	}
	if strings.Contains(completeResponse.Body.String(), "raw-staff-session-token") {
		t.Fatal("raw staff token leaked into JSON response")
	}
	setCookie := completeResponse.Header().Get("Set-Cookie")
	for _, required := range []string{
		"peergo_staff_session=raw-staff-session-token",
		"Path=/api/v1/admin",
		"HttpOnly",
		"SameSite=Strict",
	} {
		if !strings.Contains(setCookie, required) {
			t.Fatalf("staff Set-Cookie = %q, missing %q", setCookie, required)
		}
	}
	if strings.Contains(setCookie, "Path=/;") {
		t.Fatalf("staff cookie escaped its admin path: %q", setCookie)
	}
	var session generated.StaffSession
	if err := json.NewDecoder(completeResponse.Body).Decode(&session); err != nil {
		t.Fatalf("decode complete response: %v", err)
	}
	if session.CsrfToken != staffCSRF || session.User.Username != "staff-demo" {
		t.Fatalf("complete response = %+v", session)
	}
}

func TestSiteAdministratorUsesExistingAccountSessionWithoutPasskey(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 3, 0, 0, 0, time.UTC)
	userID := uuid.MustParse("2dfa5298-53ac-41e1-95ae-bba6ad927359")
	authority := authz.AuthorityBinding{
		GrantID: uuid.New(), GrantVersion: 1, MandateID: uuid.New(),
	}
	identityService := &recordingIdentityService{current: identity.WebSession{
		User: identity.User{
			ID: userID, Username: "admin", DisplayName: "管理员",
		},
		CreatedAt: now,
		ExpiresAt: now.Add(12 * time.Hour),
		CSRFToken: strings.Repeat("w", 43),
	}}
	authorizationService := &recordingAuthorizationService{decision: authz.Decision{
		Allow: true, GrantID: authority.GrantID, GrantVersion: authority.GrantVersion,
		MandateID: authority.MandateID, RoleID: "site_admin",
		EffectiveUntil: now.AddDate(100, 0, 0),
	}}
	handler := testHandlerWithServices(t, identityService, authorizationService)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/session", nil)
	request.AddCookie(&http.Cookie{Name: "peergo_session", Value: "existing-account-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if identityService.currentToken != "existing-account-token" {
		t.Fatalf("CurrentSession() token = %q", identityService.currentToken)
	}
	if authorizationService.authorizeInput.Action != authz.ActionStaffSessionCreateSelf ||
		authorizationService.authorizeInput.CredentialAudience != authz.AudienceWebSession ||
		authorizationService.authorizeInput.Subject.ID != userID {
		t.Fatalf("Authorize() request = %+v", authorizationService.authorizeInput)
	}
	var session generated.StaffSession
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatalf("decode account administrator session: %v", err)
	}
	if session.User.Username != "admin" || session.AuthenticationMethod != generated.AccountSession ||
		!session.AuthenticatedAt.Equal(now) || session.WebauthnAuthenticatedAt != nil ||
		session.CsrfToken != strings.Repeat("w", 43) {
		t.Fatalf("account administrator session = %+v", session)
	}
	if setCookie := response.Header().Get("Set-Cookie"); setCookie != "" {
		t.Fatalf("account administrator unexpectedly issued a second cookie: %q", setCookie)
	}
}

func TestStaffEnrollmentUsesWebAudienceAndKeepsTicketOutOfResponses(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	challengeID := uuid.New()
	bootstrapToken := strings.Repeat("b", 43)
	csrfToken := strings.Repeat("c", 43)
	enrollmentService := &recordingStaffEnrollmentService{
		beginResult: identity.StaffEnrollmentOptions{
			ChallengeID: challengeID,
			ExpiresAt:   now.Add(5 * time.Minute),
			PublicKey: json.RawMessage(`{
				"rp":{"id":"peergo.test","name":"PeerGo"},
				"user":{"id":"dXNlci1pZA","name":"demo","displayName":"Demo"},
				"challenge":"Y2hhbGxlbmdlLW11c3QtYmUtbG9uZw",
				"pubKeyCredParams":[{"type":"public-key","alg":-7}],
				"timeout":300000,
				"authenticatorSelection":{"residentKey":"preferred","requireResidentKey":false,"userVerification":"required"},
				"attestation":"none"
			}`),
		},
		completeResult: identity.StaffCredentialEnrollment{
			CredentialID: []byte("credential-never-exposed"),
			Label:        "Mac passkey",
			EnrolledAt:   now,
		},
	}
	handler := testHandlerWithEveryService(
		t,
		unavailableIdentityService{},
		unavailableStaffIdentityService{},
		enrollmentService,
		unavailableAuthorizationService{},
		unavailableGrantAdministrationService{},
		unavailableCategoryAdministrationService{},
		unavailableSiteDisplaySettingsService{},
	)

	beginBody := `{"bootstrap_token":"` + bootstrapToken + `","label":"Mac passkey"}`
	beginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/staff/enrollment/options", strings.NewReader(beginBody))
	beginRequest.Header.Set("Content-Type", "application/json")
	beginRequest.Header.Set("Origin", "http://peergo.test")
	beginRequest.Header.Set("X-CSRF-Token", csrfToken)
	beginRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "ordinary-web-token"})
	beginRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "must-not-bootstrap"})
	beginResponse := httptest.NewRecorder()
	handler.ServeHTTP(beginResponse, beginRequest)
	if beginResponse.Code != http.StatusOK {
		t.Fatalf("begin status = %d, body=%s", beginResponse.Code, beginResponse.Body.String())
	}
	if enrollmentService.beginWebToken != "ordinary-web-token" || enrollmentService.beginCSRF != csrfToken || enrollmentService.beginInput.BootstrapToken != bootstrapToken || enrollmentService.beginInput.Label != "Mac passkey" {
		t.Fatalf("Begin() token=%q csrf=%q input=%+v", enrollmentService.beginWebToken, enrollmentService.beginCSRF, enrollmentService.beginInput)
	}
	if strings.Contains(beginResponse.Body.String(), bootstrapToken) {
		t.Fatal("bootstrap token leaked into enrollment options response")
	}

	completeBody := `{
		"bootstrap_token":"` + bootstrapToken + `",
		"challenge_id":"` + challengeID.String() + `",
		"credential":{
			"id":"Y3JlZGVudGlhbC1uZXc",
			"rawId":"Y3JlZGVudGlhbC1uZXc",
			"type":"public-key",
			"response":{"clientDataJSON":"YQ","attestationObject":"YQ"},
			"clientExtensionResults":{}
		}
	}`
	completeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/staff/enrollment", strings.NewReader(completeBody))
	completeRequest.Header.Set("Content-Type", "application/json")
	completeRequest.Header.Set("Origin", "http://peergo.test")
	completeRequest.Header.Set("X-CSRF-Token", csrfToken)
	completeRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "ordinary-web-token"})
	completeResponse := httptest.NewRecorder()
	handler.ServeHTTP(completeResponse, completeRequest)
	if completeResponse.Code != http.StatusCreated {
		t.Fatalf("complete status = %d, body=%s", completeResponse.Code, completeResponse.Body.String())
	}
	if enrollmentService.completeWebToken != "ordinary-web-token" || enrollmentService.completeCSRF != csrfToken || enrollmentService.completeInput.ChallengeID != challengeID || enrollmentService.completeInput.BootstrapToken != bootstrapToken {
		t.Fatalf("Complete() token=%q csrf=%q input=%+v", enrollmentService.completeWebToken, enrollmentService.completeCSRF, enrollmentService.completeInput)
	}
	if strings.Contains(completeResponse.Body.String(), bootstrapToken) || strings.Contains(completeResponse.Body.String(), "credential-never-exposed") {
		t.Fatal("enrollment secret material leaked into completion response")
	}
	var result generated.StaffCredentialEnrollment
	if err := json.NewDecoder(completeResponse.Body).Decode(&result); err != nil {
		t.Fatalf("decode completion response: %v", err)
	}
	if result.Label != "Mac passkey" || !result.EnrolledAt.Equal(now) {
		t.Fatalf("completion response = %+v", result)
	}
}

func TestStaffCapabilitiesUseBoundStaffAuthority(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	authority := authz.AuthorityBinding{GrantID: uuid.New(), GrantVersion: 5, MandateID: uuid.New()}
	staffService := &recordingStaffIdentityService{currentResult: identity.StaffSession{
		User:                    identity.User{ID: userID, Username: "staff-demo", DisplayName: "Staff Demo"},
		WebAuthnAuthenticatedAt: now.Add(-time.Minute),
		Authority:               authority,
	}}
	authorizationService := &recordingAuthorizationService{result: authz.CapabilitySet{
		PolicyVersion: authz.PolicyVersion,
		Items: []authz.Capability{{
			Action: authz.ActionGrantRead, Description: "读取权限、任期与撤权审批状态",
			Scope: authz.SiteScope(), ExpiresAt: now.Add(time.Hour),
		}},
	}}
	handler := testHandlerWithAllServices(t, unavailableIdentityService{}, staffService, authorizationService)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/me/capabilities", nil)
	request.AddCookie(&http.Cookie{Name: "peergo_session", Value: "ordinary-token"})
	request.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if staffService.currentStaffToken != "staff-token" || authorizationService.subject.ID != userID || !authorizationService.staffMFAAt.Equal(now.Add(-time.Minute)) || authorizationService.staffAuthority != authority {
		t.Fatalf("staff token=%q subject=%+v mfa=%v authority=%+v", staffService.currentStaffToken, authorizationService.subject, authorizationService.staffMFAAt, authorizationService.staffAuthority)
	}
	if strings.Contains(response.Body.String(), authority.GrantID.String()) || strings.Contains(response.Body.String(), authority.MandateID.String()) {
		t.Fatal("bound authority leaked into staff capability projection")
	}
	var body generated.CapabilityList
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode staff capabilities: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].Action != generated.CapabilityActionAuthzGrantRead {
		t.Fatalf("staff capabilities response = %+v", body)
	}
}

func TestAdminSessionSelectsOnlyStaffAudienceAndExpiresSamePath(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	staffCSRF := strings.Repeat("s", 43)
	staffService := &recordingStaffIdentityService{currentResult: identity.StaffSession{
		User: identity.User{
			ID:          uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111"),
			Username:    "staff-demo",
			DisplayName: "Staff Demo",
		},
		CreatedAt:               now,
		ExpiresAt:               now.Add(15 * time.Minute),
		WebAuthnAuthenticatedAt: now,
		CSRFToken:               staffCSRF,
	}}
	handler := testHandlerWithAllServices(t, unavailableIdentityService{}, staffService, unavailableAuthorizationService{})

	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/session", nil)
	getRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "ordinary-web-token"})
	getRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "privileged-staff-token"})
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d; body=%s", getResponse.Code, http.StatusOK, getResponse.Body.String())
	}
	if staffService.currentStaffToken != "privileged-staff-token" {
		t.Fatalf("CurrentSession() token = %q, want staff cookie", staffService.currentStaffToken)
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/session", nil)
	deleteRequest.Header.Set("Origin", "http://peergo.test")
	deleteRequest.Header.Set("X-CSRF-Token", staffCSRF)
	deleteRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "ordinary-web-token"})
	deleteRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "privileged-staff-token"})
	deleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d; body=%s", deleteResponse.Code, http.StatusNoContent, deleteResponse.Body.String())
	}
	if staffService.logoutStaffToken != "privileged-staff-token" || staffService.logoutCSRF != staffCSRF {
		t.Fatalf("Logout() token=%q csrf=%q", staffService.logoutStaffToken, staffService.logoutCSRF)
	}
	setCookie := deleteResponse.Header().Get("Set-Cookie")
	for _, required := range []string{"peergo_staff_session=", "Path=/api/v1/admin", "Max-Age=0", "HttpOnly", "SameSite=Strict"} {
		if !strings.Contains(setCookie, required) {
			t.Fatalf("expired staff Set-Cookie = %q, missing %q", setCookie, required)
		}
	}
}

func TestUserAdministrationMapsAuthorizedOperationalListAndCurrentRestrictions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 9, 30, 0, 0, time.UTC)
	staffID := uuid.New()
	managedUserID := uuid.New()
	restrictionID := uuid.New()
	staffService := &recordingStaffIdentityService{currentResult: identity.StaffSession{
		User:                    identity.User{ID: staffID, Username: "reader", DisplayName: "用户只读员"},
		WebAuthnAuthenticatedAt: now.Add(-time.Minute),
	}}
	userService := &recordingUserAdministrationService{
		listResult: identity.ManagedUserPage{
			Items: []identity.ManagedUserSummary{{
				ID: managedUserID, NumericID: 12327, Username: "demo-target", DisplayName: "演示目标",
				Email: "demo-target@example.com", RoleNames: []string{"member"},
				Status: identity.AccountStatusActive, Version: 3, ActiveRestrictionCount: 1,
				CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now,
			}},
			Total: 1, Page: 2, PageSize: 10,
		},
		detail: identity.ManagedUserDetail{
			ManagedUserSummary: identity.ManagedUserSummary{
				ID: managedUserID, NumericID: 12327, Username: "demo-target", DisplayName: "演示目标",
				Email: "demo-target@example.com", RoleNames: []string{"member"},
				Status: identity.AccountStatusActive, Version: 3, ActiveRestrictionCount: 1,
				CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now,
			},
			ActiveRestrictions: []identity.CurrentAccountRestriction{{
				ID: restrictionID, Kind: identity.AccountRestrictionAccountAccess,
				ReasonCode: "legacy_import", ReasonSummary: "旧站迁移演示：临时限制账户访问，等待人工复核。",
				StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(7 * 24 * time.Hour), Version: 2,
			}},
		},
	}
	handler := testHandlerWithUserAdministration(t, staffService, userService)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?query=demo&filter=active&page=2&page_size=10", nil)
	listRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", listResponse.Code, listResponse.Body.String())
	}
	if userService.listActor.Subject.ID != staffID || userService.listInput.Query != "demo" ||
		userService.listInput.Filter != identity.ManagedUserFilterActive || userService.listInput.Page != 2 ||
		userService.listInput.PageSize != 10 {
		t.Fatalf("list actor=%+v input=%+v", userService.listActor, userService.listInput)
	}
	assertSafeUserAdministrationJSON(t, listResponse.Body.String())
	if !strings.Contains(listResponse.Body.String(), `"email":"demo-target@example.com"`) {
		t.Fatalf("list response must expose the authorized full email: %s", listResponse.Body.String())
	}
	if !strings.Contains(listResponse.Body.String(), `"numeric_id":12327`) ||
		!strings.Contains(listResponse.Body.String(), managedUserID.String()) {
		t.Fatalf("list response must expose both user identifiers: %s", listResponse.Body.String())
	}

	detailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/"+managedUserID.String(), nil)
	detailRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	detailResponse := httptest.NewRecorder()
	handler.ServeHTTP(detailResponse, detailRequest)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body=%s", detailResponse.Code, detailResponse.Body.String())
	}
	if userService.getActor.Subject.ID != staffID || userService.getUserID != managedUserID ||
		!strings.Contains(detailResponse.Body.String(), `"reason_code":"legacy_import"`) {
		t.Fatalf("detail actor=%+v user_id=%s body=%s", userService.getActor, userService.getUserID, detailResponse.Body.String())
	}
	assertSafeUserAdministrationJSON(t, detailResponse.Body.String())
}

func TestAccountRestrictionCommandsRequireStaffWriteAndMapVersions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 13, 0, 0, 0, time.UTC)
	staffID := uuid.New()
	targetID := uuid.New()
	restrictionID := uuid.New()
	staffCSRF := strings.Repeat("s", 43)
	staffService := &recordingStaffIdentityService{currentResult: identity.StaffSession{
		User:                    identity.User{ID: staffID, Username: "operator", DisplayName: "账户访问处置员"},
		WebAuthnAuthenticatedAt: now.Add(-time.Minute), CSRFToken: staffCSRF,
	}}
	userService := &recordingUserAdministrationService{
		createResult: identity.ManagedUserDetail{ManagedUserSummary: identity.ManagedUserSummary{
			ID: targetID, NumericID: 12328, Username: "target", DisplayName: "目标账户", Status: identity.AccountStatusActive,
			Email: "target@example.com", RoleNames: []string{"member"},
			Version: 4, ActiveRestrictionCount: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
		}, ActiveRestrictions: []identity.CurrentAccountRestriction{{
			ID: restrictionID, Kind: identity.AccountRestrictionAccountAccess,
			ReasonCode:    string(identity.AccountRestrictionReasonManualReview),
			ReasonSummary: "该账户需要完成短期人工复核后再恢复访问。",
			StartsAt:      now, ExpiresAt: now.Add(72 * time.Hour), Version: 1,
		}}},
		revokeResult: identity.ManagedUserDetail{ManagedUserSummary: identity.ManagedUserSummary{
			ID: targetID, NumericID: 12328, Username: "target", DisplayName: "目标账户", Status: identity.AccountStatusActive,
			Email: "target@example.com", RoleNames: []string{"member"},
			Version: 5, ActiveRestrictionCount: 0, CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
		}},
	}
	handler := testHandlerWithUserAdministration(t, staffService, userService)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/"+targetID.String()+"/account-restrictions", strings.NewReader(`{"reason_code":"manual_review","reason":"该账户需要完成短期人工复核后再恢复访问。","duration_hours":72,"expected_user_version":3}`))
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set("Origin", "http://peergo.test")
	createRequest.Header.Set("X-CSRF-Token", staffCSRF)
	createRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated || !strings.Contains(createResponse.Body.String(), `"version":4`) {
		t.Fatalf("create status = %d, body=%s", createResponse.Code, createResponse.Body.String())
	}
	if staffService.writeStaffToken != "staff-token" || staffService.writeCSRF != staffCSRF ||
		userService.createActor.Subject.ID != staffID || userService.createInput.UserID != targetID ||
		userService.createInput.DurationHours != 72 || userService.createInput.ExpectedUserVersion != 3 {
		t.Fatalf("write token=%q csrf=%q actor=%+v input=%+v", staffService.writeStaffToken, staffService.writeCSRF, userService.createActor, userService.createInput)
	}

	revokeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/"+targetID.String()+"/account-restrictions/"+restrictionID.String()+"/revocations", strings.NewReader(`{"reason_code":"review_completed","reason":"人工复核已经完成，可以恢复账户访问。","expected_user_version":4,"expected_restriction_version":1}`))
	revokeRequest.Header.Set("Content-Type", "application/json")
	revokeRequest.Header.Set("Origin", "http://peergo.test")
	revokeRequest.Header.Set("X-CSRF-Token", staffCSRF)
	revokeRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	revokeResponse := httptest.NewRecorder()
	handler.ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusOK || !strings.Contains(revokeResponse.Body.String(), `"version":5`) {
		t.Fatalf("revoke status = %d, body=%s", revokeResponse.Code, revokeResponse.Body.String())
	}
	if userService.revokeInput.RestrictionID != restrictionID || userService.revokeInput.ExpectedUserVersion != 4 ||
		userService.revokeInput.ExpectedRestrictionVersion != 1 {
		t.Fatalf("revoke input = %+v", userService.revokeInput)
	}

	userService.err = identity.ErrManagedUserVersionConflict
	conflictRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/"+targetID.String()+"/account-restrictions", strings.NewReader(`{"reason_code":"manual_review","reason":"使用旧账户版本验证冲突响应不会静默覆盖。","duration_hours":24,"expected_user_version":3}`))
	conflictRequest.Header.Set("Content-Type", "application/json")
	conflictRequest.Header.Set("Origin", "http://peergo.test")
	conflictRequest.Header.Set("X-CSRF-Token", staffCSRF)
	conflictRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflictRequest)
	if conflictResponse.Code != http.StatusConflict || !strings.Contains(conflictResponse.Body.String(), "managed_user_version_conflict") {
		t.Fatalf("conflict status = %d, body=%s", conflictResponse.Code, conflictResponse.Body.String())
	}
}

func TestManualDownloadRestrictionCommandsKeepIndependentStateVersions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	staffID := uuid.New()
	targetID := uuid.New()
	staffCSRF := strings.Repeat("d", 43)
	staffService := &recordingStaffIdentityService{currentResult: identity.StaffSession{
		User:                    identity.User{ID: staffID, Username: "download-operator", DisplayName: "下载权限处置员"},
		WebAuthnAuthenticatedAt: now.Add(-time.Minute), CSRFToken: staffCSRF,
	}}
	activeState := identity.ManualDownloadRestrictionState{Active: true, Version: 1}
	userService := &recordingUserAdministrationService{
		createResult: identity.ManagedUserDetail{
			ManagedUserSummary: identity.ManagedUserSummary{
				ID: targetID, NumericID: 12329, Username: "download-target", DisplayName: "下载目标",
				Status: identity.AccountStatusActive, Email: "target@example.com", RoleNames: []string{"member"},
				Version: 2, DownloadRestricted: true, CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
			},
			ManualDownloadRestriction: activeState,
		},
		revokeResult: identity.ManagedUserDetail{
			ManagedUserSummary: identity.ManagedUserSummary{
				ID: targetID, NumericID: 12329, Username: "download-target", DisplayName: "下载目标",
				Status: identity.AccountStatusActive, Email: "target@example.com", RoleNames: []string{"member"},
				Version: 3, CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
			},
			ManualDownloadRestriction: identity.ManualDownloadRestrictionState{Version: 2},
		},
	}
	handler := testHandlerWithUserAdministration(t, staffService, userService)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/"+targetID.String()+"/manual-download-restriction", strings.NewReader(`{"reason_code":"policy_violation","reason":"用户下载行为违反站点规则，等待人工复核后再恢复。","expected_user_version":1,"expected_state_version":0}`))
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set("Origin", "http://peergo.test")
	createRequest.Header.Set("X-CSRF-Token", staffCSRF)
	createRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated ||
		!strings.Contains(createResponse.Body.String(), `"manual_download_restriction":{"active":true`) {
		t.Fatalf("create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	if userService.manualCreateInput.UserID != targetID || userService.manualCreateInput.ExpectedUserVersion != 1 ||
		userService.manualCreateInput.ExpectedStateVersion != 0 || userService.createActor.Subject.ID != staffID {
		t.Fatalf("manual create input=%+v actor=%+v", userService.manualCreateInput, userService.createActor)
	}

	updateRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/"+targetID.String()+"/manual-download-restriction", strings.NewReader(`{"reason_code":"manual_review","reason":"补充人工复核依据，并继续维持当前下载限制。","expected_user_version":2,"expected_state_version":1}`))
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.Header.Set("Origin", "http://peergo.test")
	updateRequest.Header.Set("X-CSRF-Token", staffCSRF)
	updateRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK || userService.manualUpdateInput.ExpectedStateVersion != 1 {
		t.Fatalf("update status=%d input=%+v body=%s", updateResponse.Code, userService.manualUpdateInput, updateResponse.Body.String())
	}

	revokeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/"+targetID.String()+"/manual-download-restriction/revocations", strings.NewReader(`{"reason_code":"review_completed","reason":"复核已经完成，确认可以解除人工下载限制。","expected_user_version":2,"expected_state_version":1}`))
	revokeRequest.Header.Set("Content-Type", "application/json")
	revokeRequest.Header.Set("Origin", "http://peergo.test")
	revokeRequest.Header.Set("X-CSRF-Token", staffCSRF)
	revokeRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	revokeResponse := httptest.NewRecorder()
	handler.ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusOK || userService.manualRevokeInput.ExpectedStateVersion != 1 ||
		!strings.Contains(revokeResponse.Body.String(), `"manual_download_restriction":{"active":false`) {
		t.Fatalf("revoke status=%d input=%+v body=%s", revokeResponse.Code, userService.manualRevokeInput, revokeResponse.Body.String())
	}
}

func TestVIPChangeRequiresStaffWriteAndMapsEntitlementVersions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 18, 2, 0, 0, 0, time.UTC)
	staffID := uuid.New()
	targetID := uuid.New()
	staffCSRF := strings.Repeat("v", 43)
	until := now.AddDate(0, 0, 90)
	staffService := &recordingStaffIdentityService{currentResult: identity.StaffSession{
		User:                    identity.User{ID: staffID, Username: "vip-operator", DisplayName: "VIP 管理员"},
		WebAuthnAuthenticatedAt: now.Add(-time.Minute), CSRFToken: staffCSRF,
	}}
	userService := &recordingUserAdministrationService{
		createResult: identity.ManagedUserDetail{
			ManagedUserSummary: identity.ManagedUserSummary{
				ID: targetID, NumericID: 12330, Username: "vip-target", DisplayName: "VIP 目标",
				Status: identity.AccountStatusActive, Email: "vip@example.com", RoleNames: []string{"member"},
				Version: 4, VIPEnabled: true, VIPActive: true, VIPUntil: &until,
				CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
			},
			VIPState: identity.VIPState{Enabled: true, Active: true, Until: &until, Version: 3},
		},
	}
	handler := testHandlerWithUserAdministration(t, staffService, userService)

	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/"+targetID.String()+"/vip", strings.NewReader(`{"enabled":true,"duration_days":90,"reason":"用户符合站点活动的 VIP 签发条件。","expected_user_version":3,"expected_state_version":2}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://peergo.test")
	request.Header.Set("X-CSRF-Token", staffCSRF)
	request.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"vip_state":{"active":true,"enabled":true`) {
		t.Fatalf("change VIP status=%d body=%s", response.Code, response.Body.String())
	}
	if staffService.writeStaffToken != "staff-token" || staffService.writeCSRF != staffCSRF ||
		userService.createActor.Subject.ID != staffID || userService.vipInput.UserID != targetID ||
		userService.vipInput.DurationDays == nil || *userService.vipInput.DurationDays != 90 ||
		userService.vipInput.ExpectedUserVersion != 3 || userService.vipInput.ExpectedStateVersion != 2 {
		t.Fatalf("write token=%q csrf=%q actor=%+v input=%+v", staffService.writeStaffToken, staffService.writeCSRF, userService.createActor, userService.vipInput)
	}

	userService.err = identity.ErrVIPStateConflict
	conflict := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/"+targetID.String()+"/vip", strings.NewReader(`{"enabled":false,"reason":"使用旧状态版本验证冲突不会覆盖新状态。","expected_user_version":3,"expected_state_version":2}`))
	conflict.Header.Set("Content-Type", "application/json")
	conflict.Header.Set("Origin", "http://peergo.test")
	conflict.Header.Set("X-CSRF-Token", staffCSRF)
	conflict.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflict)
	if conflictResponse.Code != http.StatusConflict ||
		!strings.Contains(conflictResponse.Body.String(), "vip_state_version_conflict") {
		t.Fatalf("conflict status=%d body=%s", conflictResponse.Code, conflictResponse.Body.String())
	}
}

func assertSafeUserAdministrationJSON(t *testing.T, body string) {
	t.Helper()
	for _, forbidden := range []string{"credential_ref", "ip_address", "passkey", "traffic_balance", "economy_balance"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("user administration response leaked %q: %s", forbidden, body)
		}
	}
}

func TestNotificationRoutesKeepPrivateReadAndCSRFBoundReadState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	notificationID := uuid.New()
	const torrentID torrents.TorrentID = 42
	csrf := strings.Repeat("n", 43)
	service := &recordingNotificationService{
		page: notifications.Page{
			Items: []notifications.Notification{
				{
					ID: notificationID, Kind: notifications.KindTorrentReview,
					CreatedAt: now,
					TorrentReview: &notifications.TorrentReviewNotification{
						TorrentID: torrentID, TorrentTitle: "Notification Release",
						Outcome: torrents.StateRejected, ReasonCode: review.ReasonMetadataIncomplete,
						Reason: "请补充完整的发布说明后重新提交。",
					},
				},
				{
					ID: uuid.New(), Kind: notifications.KindMemberGift,
					CreatedAt: now.Add(-time.Minute),
					MemberGift: &notifications.MemberGiftNotification{
						SenderNumericID: 42, SenderUsername: "member42",
						SenderDisplayName: "四十二号成员", NetAmount: 9007199254740992,
						Message: "感谢保种",
					},
				},
			},
			Total: 2, UnreadCount: 2, Limit: 20, Offset: 0,
		},
		summary: notifications.Summary{UnreadCount: 2},
		readReceipt: notifications.ReadReceipt{
			NotificationID: notificationID, ReadAt: now.Add(time.Minute),
		},
		readAllReceipt:  notifications.ReadAllReceipt{UpdatedCount: 1, ReadAt: now.Add(2 * time.Minute)},
		archiveReceipt:  notifications.ArchiveAllReceipt{UpdatedCount: 1, ArchivedAt: now.Add(3 * time.Minute)},
		feedbackReceipt: notifications.FeedbackReceipt{FeedbackID: uuid.New(), CreatedAt: now.Add(4 * time.Minute)},
	}
	handler := testHandlerWithNotifications(t, service)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/me/notifications?limit=20&offset=0&unread_only=true", nil)
	listRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "notification-token"})
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || listResponse.Header().Get("Cache-Control") != "no-store" ||
		service.listCookie != "notification-token" || service.listQuery != (notifications.ListQuery{Limit: 20, Offset: 0, UnreadOnly: true}) ||
		!strings.Contains(listResponse.Body.String(), `"kind":"torrent_review"`) ||
		!strings.Contains(listResponse.Body.String(), `"outcome":"rejected"`) ||
		!strings.Contains(listResponse.Body.String(), `"kind":"member_gift"`) ||
		!strings.Contains(listResponse.Body.String(), `"member_gift_sender_numeric_id":"42"`) ||
		!strings.Contains(listResponse.Body.String(), `"member_gift_net_amount":"9007199254740992"`) {
		t.Fatalf("list status=%d service=%+v body=%s", listResponse.Code, service, listResponse.Body.String())
	}

	summaryRequest := httptest.NewRequest(http.MethodGet, "/api/v1/me/notifications/summary", nil)
	summaryRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "notification-token"})
	summaryResponse := httptest.NewRecorder()
	handler.ServeHTTP(summaryResponse, summaryRequest)
	if summaryResponse.Code != http.StatusOK || !strings.Contains(summaryResponse.Body.String(), `"unread_count":2`) {
		t.Fatalf("summary status=%d body=%s", summaryResponse.Code, summaryResponse.Body.String())
	}

	readRequest := httptest.NewRequest(http.MethodPut, "/api/v1/me/notifications/"+notificationID.String()+"/read", nil)
	readRequest.Header.Set("Origin", "http://peergo.test")
	readRequest.Header.Set("X-CSRF-Token", csrf)
	readRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "notification-token"})
	readResponse := httptest.NewRecorder()
	handler.ServeHTTP(readResponse, readRequest)
	if readResponse.Code != http.StatusOK || service.readCookie != "notification-token" || service.readCSRF != csrf ||
		service.readID != notificationID || !strings.Contains(readResponse.Body.String(), notificationID.String()) {
		t.Fatalf("read status=%d service=%+v body=%s", readResponse.Code, service, readResponse.Body.String())
	}

	readAllRequest := httptest.NewRequest(http.MethodPut, "/api/v1/me/notifications/read-all", nil)
	readAllRequest.Header.Set("Origin", "http://peergo.test")
	readAllRequest.Header.Set("X-CSRF-Token", csrf)
	readAllRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "notification-token"})
	readAllResponse := httptest.NewRecorder()
	handler.ServeHTTP(readAllResponse, readAllRequest)
	if readAllResponse.Code != http.StatusOK || service.readAllCookie != "notification-token" || service.readAllCSRF != csrf ||
		!strings.Contains(readAllResponse.Body.String(), `"updated_count":1`) {
		t.Fatalf("read-all status=%d service=%+v body=%s", readAllResponse.Code, service, readAllResponse.Body.String())
	}

	archiveRequest := httptest.NewRequest(http.MethodPut, "/api/v1/me/notifications/archive-all", nil)
	archiveRequest.Header.Set("Origin", "http://peergo.test")
	archiveRequest.Header.Set("X-CSRF-Token", csrf)
	archiveRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "notification-token"})
	archiveResponse := httptest.NewRecorder()
	handler.ServeHTTP(archiveResponse, archiveRequest)
	if archiveResponse.Code != http.StatusOK || service.archiveCookie != "notification-token" || service.archiveCSRF != csrf ||
		!strings.Contains(archiveResponse.Body.String(), `"updated_count":1`) {
		t.Fatalf("archive-all status=%d service=%+v body=%s", archiveResponse.Code, service, archiveResponse.Body.String())
	}

	feedbackRequest := httptest.NewRequest(http.MethodPost, "/api/v1/me/notifications/feedback", strings.NewReader(`{"title":"页面建议","content":"请改进移动端消息列表。"}`))
	feedbackRequest.Header.Set("Content-Type", "application/json")
	feedbackRequest.Header.Set("Origin", "http://peergo.test")
	feedbackRequest.Header.Set("X-CSRF-Token", csrf)
	feedbackRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "notification-token"})
	feedbackResponse := httptest.NewRecorder()
	handler.ServeHTTP(feedbackResponse, feedbackRequest)
	if feedbackResponse.Code != http.StatusCreated || service.feedbackCookie != "notification-token" || service.feedbackCSRF != csrf ||
		service.feedbackInput.Title != "页面建议" || !strings.Contains(feedbackResponse.Body.String(), `"feedback_id"`) {
		t.Fatalf("feedback status=%d service=%+v body=%s", feedbackResponse.Code, service, feedbackResponse.Body.String())
	}
}

func TestGetMyTrafficReturnsExactPrivateCoreProjection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 15, 0, 0, 0, time.UTC)
	settlementID := uuid.New()
	const torrentID int64 = 42
	service := &recordingTrafficOverviewService{result: traffic.Overview{
		Totals: traffic.Totals{
			RawUploaded: 9_007_199_254_740_993, RawDownloaded: 4096,
			CreditedUploaded: 18_014_398_509_481_986, ChargedDownloaded: 0,
			EntryCount: 1, LastSettledAt: &now, ProjectionUpdatedAt: &now,
		},
		Entries: []traffic.Entry{{
			ID: settlementID, TorrentID: torrentID, TorrentTitle: "Final Traffic Release",
			IntervalStartedAt: now.Add(-time.Minute), IntervalEndedAt: now.Add(-30 * time.Second),
			RawUploaded: 1024, RawDownloaded: 2048, CreditedUploaded: 2048,
			ChargedDownloaded: 0, SettledAt: now,
			Explanation: traffic.Explanation{
				Status: traffic.ExplanationComplete, SegmentCount: 1,
				Segments: []traffic.ExplanationSegment{{
					Index: 0, StartsAt: now.Add(-time.Minute), EndsAt: now.Add(-30 * time.Second),
					RawUploaded: 1024, RawDownloaded: 2048, CreditedUploaded: 2048, ChargedDownloaded: 0,
				}},
			},
		}},
	}}
	handler := testHandlerWithTraffic(t, service)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/me/traffic?limit=7", nil)
	request.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-traffic-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q body=%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
	if service.cookieToken != "web-traffic-token" || service.limit != 7 || service.calls != 1 {
		t.Fatalf("service token=%q limit=%d calls=%d", service.cookieToken, service.limit, service.calls)
	}
	body := response.Body.String()
	for _, want := range []string{
		`"raw_uploaded_bytes":"9007199254740993"`,
		`"credited_uploaded_bytes":"18014398509481986"`,
		`"torrent":{"id":42`,
		`"explanation":{"segment_count":"1","segments":[`,
		`"status":"complete"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing %q: %s", want, body)
		}
	}
	for _, forbidden := range []string{"settlement_sha256", "info_hash", "torrent_id", "policy_revision", "applications", "raw_uploaded_bytes\":9007199254740993"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("traffic response leaked or rounded %q: %s", forbidden, body)
		}
	}

	withoutSession := httptest.NewRecorder()
	handler.ServeHTTP(withoutSession, httptest.NewRequest(http.MethodGet, "/api/v1/me/traffic", nil))
	if withoutSession.Code != http.StatusUnauthorized || service.calls != 1 {
		t.Fatalf("missing-session status=%d calls=%d body=%s", withoutSession.Code, service.calls, withoutSession.Body.String())
	}
}

func TestListMyHitAndRunsReturnsUserSafeKeysetPage(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 15, 0, 0, 0, time.UTC)
	obligationID, nextObligationID := uuid.New(), uuid.New()
	const torrentID int64 = 42
	service := &recordingTrafficOverviewService{hnrResult: traffic.HNRPage{
		AsOf:    now,
		Summary: traffic.HNRSummary{Total: 2, Overdue: 1, Satisfied: 1},
		Items: []traffic.HNREntry{{
			ObligationID: obligationID, TorrentID: torrentID, TorrentTitle: "Keep Seeding Release",
			CompletedAt: now.Add(-14 * 24 * time.Hour), Status: traffic.HNRStatusOverdue,
			SeededSeconds: 86_400, RequiredSeedSeconds: 259_200,
			RawUploaded: 9_007_199_254_740_993, RawDownloaded: 18_014_398_509_481_986,
			RawRatioBasisPoints: 5000, RequiredRatioBasisPoints: 10000,
			AssessmentDueAt: now.Add(-7 * 24 * time.Hour), GraceEndsAt: now.Add(-4 * 24 * time.Hour),
			UpdatedAt: now.Add(-time.Hour),
		}},
		NextCursor: &traffic.HNRCursor{CompletedAt: now.Add(-15 * 24 * time.Hour), ObligationID: nextObligationID},
	}}
	handler := testHandlerWithTraffic(t, service)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/me/hit-and-runs?status=all&limit=1", nil)
	request.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-hnr-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q body=%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
	if service.cookieToken != "web-hnr-token" || service.hnrQuery.Filter != traffic.HNRFilterAll || service.hnrQuery.Limit != 1 || service.hnrCalls != 1 {
		t.Fatalf("service token=%q query=%+v calls=%d", service.cookieToken, service.hnrQuery, service.hnrCalls)
	}
	var body generated.HNRPage
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Summary.Total != "2" || body.Summary.Overdue != "1" || len(body.Items) != 1 ||
		body.Items[0].Id != obligationID || body.Items[0].Torrent.Id != torrentID ||
		body.Items[0].SeededSeconds != "86400" || body.Items[0].RawUploadedBytes != "9007199254740993" ||
		body.NextCursor == nil || len(*body.NextCursor) != 34 {
		t.Fatalf("H&R body = %+v", body)
	}
	decodedCursor, err := traffic.DecodeHNRCursor(*body.NextCursor)
	if err != nil || decodedCursor.ObligationID != nextObligationID {
		t.Fatalf("next cursor = %+v, error %v", decodedCursor, err)
	}
	for _, forbidden := range []string{"policy_id", "policy_revision", "tracker", "session_id", "torrent_id", "version", "evidence"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("H&R response leaked %q: %s", forbidden, response.Body.String())
		}
	}

	invalidCursor := httptest.NewRequest(http.MethodGet, "/api/v1/me/hit-and-runs?cursor="+strings.Repeat("A", 34), nil)
	invalidCursor.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-hnr-token"})
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalidCursor)
	if invalidResponse.Code != http.StatusBadRequest || service.hnrCalls != 1 {
		t.Fatalf("invalid cursor status=%d calls=%d body=%s", invalidResponse.Code, service.hnrCalls, invalidResponse.Body.String())
	}

	withoutSession := httptest.NewRecorder()
	handler.ServeHTTP(withoutSession, httptest.NewRequest(http.MethodGet, "/api/v1/me/hit-and-runs", nil))
	if withoutSession.Code != http.StatusUnauthorized || service.hnrCalls != 1 {
		t.Fatalf("missing-session status=%d calls=%d body=%s", withoutSession.Code, service.hnrCalls, withoutSession.Body.String())
	}
}

func TestSubmitMyHNRAppealBindsPathIdempotencyAndWriteSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 17, 15, 0, 0, 0, time.UTC)
	appealID, obligationID := uuid.New(), uuid.New()
	csrf := strings.Repeat("c", 43)
	statement := "客户端异常退出后已恢复做种，请帮助核对这条 H&R 记录。"
	service := &recordingTrafficOverviewService{appealResult: traffic.HNRAppeal{
		ID: appealID, ObligationID: obligationID, Status: traffic.HNRAppealPending,
		Statement: statement, CreatedAt: now,
	}}
	handler := testHandlerWithTraffic(t, service)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/me/hit-and-runs/"+obligationID.String()+"/appeals",
		strings.NewReader(`{"statement":"`+statement+`"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://peergo.test")
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("Idempotency-Key", appealID.String())
	request.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-hnr-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q body=%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
	if service.appealCalls != 1 || service.appealCookie != "web-hnr-token" || service.appealCSRF != csrf ||
		service.appealInput.AppealID != appealID || service.appealInput.ObligationID != obligationID ||
		service.appealInput.Statement != statement {
		t.Fatalf("appeal service = %+v", service)
	}
	var body generated.MyHNRAppeal
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != generated.HNRAppealStatusPending || body.Statement != statement || !body.SubmittedAt.Equal(now) {
		t.Fatalf("appeal response = %+v", body)
	}
}

func TestTorrentBookmarksUsePrivateBatchReadAndIdempotentWrites(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 16, 0, 0, 0, time.UTC)
	csrf := strings.Repeat("c", 43)
	service := &recordingTorrentBookmarkService{
		page: catalog.TorrentBookmarkPage{
			Items: []catalog.TorrentBookmark{{
				Torrent: catalog.TorrentSummary{
					Torrent: catalog.Torrent{
						ID: 42, Name: "Paper Cranes", Category: catalog.Category{ID: "movie", Name: "电影"},
						UploadedAt: now.Add(-time.Hour), Swarm: catalog.SwarmStats{ObservedAt: now.Add(-time.Minute)},
					},
				},
				BookmarkedAt: now.Add(-30 * time.Minute),
			}},
			Total: 1, Limit: 10, Offset: 5,
		},
		statuses: []int64{42},
		state:    catalog.TorrentBookmarkState{TorrentID: 42, BookmarkedAt: now.Add(-30 * time.Minute)},
	}
	handler := testHandlerWithTorrentBookmarks(t, service)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/me/torrent-bookmarks?limit=10&offset=5", nil)
	listRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "bookmark-cookie"})
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || listResponse.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(listResponse.Body.String(), `"bookmarked_at"`) || service.listCookie != "bookmark-cookie" ||
		service.listLimit != 10 || service.listOffset != 5 {
		t.Fatalf("list status=%d cache=%q service=%+v body=%s", listResponse.Code, listResponse.Header().Get("Cache-Control"), service, listResponse.Body.String())
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/me/torrent-bookmark-statuses?torrent_id=42&torrent_id=43", nil)
	statusRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "bookmark-cookie"})
	statusResponse := httptest.NewRecorder()
	handler.ServeHTTP(statusResponse, statusRequest)
	if statusResponse.Code != http.StatusOK || !strings.Contains(statusResponse.Body.String(), `"bookmarked_ids":[42]`) ||
		!reflect.DeepEqual(service.statusIDs, []int64{42, 43}) {
		t.Fatalf("statuses status=%d ids=%v body=%s", statusResponse.Code, service.statusIDs, statusResponse.Body.String())
	}

	putRequest := httptest.NewRequest(http.MethodPut, "/api/v1/me/torrent-bookmarks/42", nil)
	putRequest.Header.Set("Origin", "http://peergo.test")
	putRequest.Header.Set("X-CSRF-Token", csrf)
	putRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "bookmark-cookie"})
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, putRequest)
	if putResponse.Code != http.StatusOK || !strings.Contains(putResponse.Body.String(), `"bookmarked":true`) ||
		service.putCookie != "bookmark-cookie" || service.putCSRF != csrf || service.putID != 42 {
		t.Fatalf("put status=%d service=%+v body=%s", putResponse.Code, service, putResponse.Body.String())
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/me/torrent-bookmarks/42", nil)
	deleteRequest.Header.Set("Origin", "http://peergo.test")
	deleteRequest.Header.Set("X-CSRF-Token", csrf)
	deleteRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "bookmark-cookie"})
	deleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent || service.deleteCookie != "bookmark-cookie" || service.deleteCSRF != csrf || service.deleteID != 42 {
		t.Fatalf("delete status=%d service=%+v body=%s", deleteResponse.Code, service, deleteResponse.Body.String())
	}
}

func TestTorrentCommentsUsePublicReadsAndAuthenticatedOwnedWrites(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 17, 0, 0, 0, time.UTC)
	const torrentID int64 = 42
	commentID, parentID, authorID, requestID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	csrf := strings.Repeat("k", 43)
	visible := social.Comment{
		ID: commentID, Target: social.TorrentCommentTarget(torrentID), ParentCommentID: &parentID,
		Author: social.CommentAuthor{ID: authorID, DisplayName: "北岸"},
		Body:   "这份资源已经校验。", BodyFormat: social.CommentBodyPlainText,
		State: social.CommentVisible, Version: 2, CreatedAt: now.Add(-time.Hour), UpdatedAt: now, EditedAt: &now,
	}
	service := &recordingCommentService{
		page:    social.CommentPage{Target: social.TorrentCommentTarget(torrentID), Items: []social.Comment{visible}, Total: 1, Limit: 10, Offset: 2},
		created: visible,
		updated: visible,
	}
	handler := testHandlerWithComments(t, service)

	listRequest := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/torrents/%d/comments?limit=10&offset=2", torrentID), nil)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || service.listTorrentID != torrentID || service.listLimit != 10 || service.listOffset != 2 ||
		!strings.Contains(listResponse.Body.String(), `"display_name":"北岸"`) || !strings.Contains(listResponse.Body.String(), `"parent_comment_id":"`+parentID.String()+`"`) {
		t.Fatalf("list status=%d service=%+v body=%s", listResponse.Code, service, listResponse.Body.String())
	}

	createRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/torrents/%d/comments", torrentID), strings.NewReader(`{"body":"这份资源已经校验。","parent_comment_id":"`+parentID.String()+`"}`))
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set("Origin", "http://peergo.test")
	createRequest.Header.Set("X-CSRF-Token", csrf)
	createRequest.Header.Set("Idempotency-Key", requestID.String())
	createRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "comment-cookie"})
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated || createResponse.Header().Get("Cache-Control") != "no-store" ||
		service.createCookie != "comment-cookie" || service.createCSRF != csrf || service.createInput.RequestID != requestID ||
		service.createInput.TorrentID != torrentID || service.createInput.ParentCommentID == nil || *service.createInput.ParentCommentID != parentID {
		t.Fatalf("create status=%d cache=%q service=%+v body=%s", createResponse.Code, createResponse.Header().Get("Cache-Control"), service, createResponse.Body.String())
	}

	updateRequest := httptest.NewRequest(http.MethodPatch, "/api/v1/comments/"+commentID.String(), strings.NewReader(`{"body":"补充：音轨也已校验。","expected_version":2}`))
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.Header.Set("Origin", "http://peergo.test")
	updateRequest.Header.Set("X-CSRF-Token", csrf)
	updateRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "comment-cookie"})
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK || updateResponse.Header().Get("Cache-Control") != "no-store" ||
		service.updateCookie != "comment-cookie" || service.updateCSRF != csrf || service.updateInput.CommentID != commentID ||
		service.updateInput.ExpectedVersion != 2 || service.updateInput.Body != "补充：音轨也已校验。" {
		t.Fatalf("update status=%d cache=%q service=%+v body=%s", updateResponse.Code, updateResponse.Header().Get("Cache-Control"), service, updateResponse.Body.String())
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/comments/"+commentID.String()+"?expected_version=2", nil)
	deleteRequest.Header.Set("Origin", "http://peergo.test")
	deleteRequest.Header.Set("X-CSRF-Token", csrf)
	deleteRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "comment-cookie"})
	deleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent || deleteResponse.Header().Get("Cache-Control") != "no-store" ||
		service.deleteCookie != "comment-cookie" || service.deleteCSRF != csrf || service.deleteInput.CommentID != commentID || service.deleteInput.ExpectedVersion != 2 {
		t.Fatalf("delete status=%d cache=%q service=%+v body=%s", deleteResponse.Code, deleteResponse.Header().Get("Cache-Control"), service, deleteResponse.Body.String())
	}
}

func TestAnnouncementCommentsReusePublicReadsAndAuthenticatedOwnedWrites(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 14, 0, 0, 0, time.UTC)
	commentID, authorID, requestID := uuid.New(), uuid.New(), uuid.New()
	const announcementID = "welcome-to-peergo"
	csrf := strings.Repeat("a", 43)
	visible := social.Comment{
		ID: commentID, Target: social.AnnouncementCommentTarget(announcementID),
		Author: social.CommentAuthor{ID: authorID, DisplayName: "北岸"},
		Body:   "公告回复", BodyFormat: social.CommentBodyPlainText,
		State: social.CommentVisible, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	service := &recordingCommentService{
		page:    social.CommentPage{Target: social.AnnouncementCommentTarget(announcementID), Items: []social.Comment{visible}, Total: 1, Limit: 20},
		created: visible,
	}
	handler := testHandlerWithComments(t, service)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/announcements/"+announcementID+"/comments?limit=20&offset=0", nil)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || service.listAnnouncementID != announcementID ||
		!strings.Contains(listResponse.Body.String(), `"body":"公告回复"`) {
		t.Fatalf("list status=%d service=%+v body=%s", listResponse.Code, service, listResponse.Body.String())
	}

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/announcements/"+announcementID+"/comments", strings.NewReader(`{"body":"公告回复"}`))
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set("Origin", "http://peergo.test")
	createRequest.Header.Set("X-CSRF-Token", csrf)
	createRequest.Header.Set("Idempotency-Key", requestID.String())
	createRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "announcement-comment-cookie"})
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated || service.createCookie != "announcement-comment-cookie" ||
		service.createCSRF != csrf || service.createAnnouncementInput.RequestID != requestID ||
		service.createAnnouncementInput.AnnouncementID != announcementID {
		t.Fatalf("create status=%d service=%+v body=%s", createResponse.Code, service, createResponse.Body.String())
	}
}

func TestCommentModerationRoutesKeepWebReportingAndStaffResolutionSeparate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 18, 0, 0, 0, time.UTC)
	staffID, authorID := uuid.New(), uuid.New()
	commentID, reportID, caseID, decisionID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	const torrentID int64 = 42
	webCSRF, staffCSRF := strings.Repeat("w", 43), strings.Repeat("s", 43)
	comment := social.Comment{
		ID: commentID, Target: social.TorrentCommentTarget(torrentID),
		Author: social.CommentAuthor{ID: authorID, DisplayName: "评论作者"},
		Body:   "待审核评论正文", BodyFormat: social.CommentBodyPlainText,
		State: social.CommentVisible, Version: 2, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	}
	service := &recordingCommentModerationService{
		receipt: social.CommentReportReceipt{
			ID: reportID, CommentID: commentID, ReasonCode: social.CommentReportOffTopic, CreatedAt: now,
		},
		page: social.CommentModerationCasePage{
			Items: []social.CommentModerationCase{{
				ID: caseID, State: social.CommentModerationCaseOpen, Version: 1,
				Target: social.CommentModerationTarget{CommentTarget: social.TorrentCommentTarget(torrentID), Title: "审核演示种子"}, Comment: comment, ReportCount: 1,
				Reports: []social.CommentModerationReport{{
					ReasonCode: social.CommentReportOffTopic, Details: "偏离资源讨论", CreatedAt: now,
				}},
				OpenedAt: now, LatestReportedAt: now,
			}},
			Total: 1, Limit: 20, Offset: 0,
		},
		result: social.CommentModerationDecisionResult{
			DecisionID: decisionID, CaseID: caseID, CommentID: commentID,
			Decision: social.CommentModerationHideComment, ReasonCode: social.CommentModerationOffTopic,
			CaseState: social.CommentModerationCaseCommentHidden, CommentState: social.CommentModeratorHidden,
			CaseVersion: 2, CommentVersion: 3, DecidedAt: now,
		},
	}
	staffService := &recordingStaffIdentityService{currentResult: identity.StaffSession{
		User:                    identity.User{ID: staffID, Username: "moderator", DisplayName: "内容审核员"},
		WebAuthnAuthenticatedAt: now.Add(-time.Minute),
		CSRFToken:               staffCSRF,
	}}
	handler := testHandlerWithCommentModeration(t, staffService, service)

	reportRequest := httptest.NewRequest(http.MethodPost, "/api/v1/comments/"+commentID.String()+"/reports", strings.NewReader(`{"reason_code":"off_topic","details":"偏离资源讨论"}`))
	reportRequest.Header.Set("Content-Type", "application/json")
	reportRequest.Header.Set("Origin", "http://peergo.test")
	reportRequest.Header.Set("X-CSRF-Token", webCSRF)
	reportRequest.Header.Set("Idempotency-Key", reportID.String())
	reportRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-comment-token"})
	reportResponse := httptest.NewRecorder()
	handler.ServeHTTP(reportResponse, reportRequest)
	if reportResponse.Code != http.StatusCreated || reportResponse.Header().Get("Cache-Control") != "no-store" ||
		service.reportCookie != "web-comment-token" || service.reportCSRF != webCSRF ||
		service.reportInput.RequestID != reportID || service.reportInput.CommentID != commentID ||
		service.reportInput.ReasonCode != social.CommentReportOffTopic || service.reportInput.Details != "偏离资源讨论" {
		t.Fatalf("report status=%d service=%+v body=%s", reportResponse.Code, service, reportResponse.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/social/comment-moderation-cases?limit=20&offset=0", nil)
	listRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-moderation-token"})
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || listResponse.Header().Get("Cache-Control") != "no-store" ||
		service.listActor.Subject.ID != staffID || service.listLimit != 20 || service.listOffset != 0 ||
		strings.Contains(listResponse.Body.String(), "reporter") || strings.Contains(listResponse.Body.String(), "moderation-reporter") {
		t.Fatalf("list status=%d service=%+v body=%s", listResponse.Code, service, listResponse.Body.String())
	}

	decisionRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/social/comment-moderation-cases/"+caseID.String()+"/decisions", strings.NewReader(`{"expected_case_version":1,"expected_comment_version":2,"decision":"hide_comment","reason_code":"off_topic","note":"已核对上下文，确认需要隐藏正文。"}`))
	decisionRequest.Header.Set("Content-Type", "application/json")
	decisionRequest.Header.Set("Origin", "http://peergo.test")
	decisionRequest.Header.Set("X-CSRF-Token", staffCSRF)
	decisionRequest.Header.Set("Idempotency-Key", decisionID.String())
	decisionRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-moderation-token"})
	decisionResponse := httptest.NewRecorder()
	handler.ServeHTTP(decisionResponse, decisionRequest)
	if decisionResponse.Code != http.StatusCreated || decisionResponse.Header().Get("Cache-Control") != "no-store" ||
		staffService.writeStaffToken != "staff-moderation-token" || staffService.writeCSRF != staffCSRF ||
		service.decideActor.Subject.ID != staffID || service.decideInput.DecisionID != decisionID ||
		service.decideInput.CaseID != caseID || service.decideInput.ExpectedCaseVersion != 1 ||
		service.decideInput.ExpectedCommentVersion != 2 || service.decideInput.Decision != social.CommentModerationHideComment {
		t.Fatalf("decision status=%d staff=%+v service=%+v body=%s", decisionResponse.Code, staffService, service, decisionResponse.Body.String())
	}
}

func TestNewRejectsAmbiguousOrWidenedSessionCookieBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	base := Dependencies{
		Catalog:                     catalog.NewService(catalog.NewDemoRepository(now), func() time.Time { return now }),
		Identity:                    unavailableIdentityService{},
		Registration:                unavailableRegistrationService{},
		Invitations:                 unavailableInvitationService{},
		EmailVerification:           unavailableEmailVerificationService{},
		PasswordRecovery:            unavailablePasswordRecoveryService{},
		SessionSecurity:             unavailableSessionSecurityService{},
		TwoFactor:                   unavailableTwoFactorService{},
		StaffIdentity:               unavailableStaffIdentityService{},
		StaffEnrollment:             unavailableStaffEnrollmentService{},
		Authorization:               unavailableAuthorizationService{},
		GrantAdministration:         unavailableGrantAdministrationService{},
		CategoryAdministration:      unavailableCategoryAdministrationService{},
		AnnouncementAdministration:  unavailableAnnouncementAdministrationService{},
		Wiki:                        unavailableWikiService{},
		SiteDisplaySettings:         unavailableSiteDisplaySettingsService{},
		UserAdministration:          unavailableUserAdministrationService{},
		Notifications:               unavailableNotificationService{},
		TrafficOverview:             unavailableTrafficOverviewService{},
		EconomyOverview:             unavailableEconomyOverviewService{},
		Attendance:                  unavailableAttendanceService{},
		MemberGifts:                 unavailableMemberGiftService{},
		ContentTips:                 unavailableContentTipService{},
		Workgroups:                  unavailableWorkgroupService{},
		SeedingRewardAdministration: unavailableSeedingRewardAdministrationService{},
		HNRPolicyAdministration:     unavailableHNRPolicyAdministrationService{},
		Operations:                  unavailableOperationsService{},
		TorrentBookmarks:            unavailableTorrentBookmarkService{},
		Comments:                    unavailableCommentService{},
		SocialPosts:                 unavailableSocialPostService{},
		CommentModeration:           unavailableCommentModerationService{},
		TorrentRead:                 unavailableTorrentReadService{},
		TorrentUpload:               unavailableTorrentUploadService{},
		TorrentDownload:             unavailableTorrentDownloadService{},
		TorrentReview:               unavailableTorrentReviewService{},
		TorrentResubmission:         unavailableTorrentResubmissionService{},
		TorrentMaintenance:          unavailableTorrentMaintenanceService{},
		PromotionAdministration:     unavailablePromotionAdministrationService{},
		RSS:                         unavailableRSSService{},
		RatioWatchAdministration:    unavailableRatioWatchAdministrationService{},
		NewcomerAdministration:      unavailableNewcomerAdministrationService{},
		TorrentUploadMaxBytes:       4 << 20,
		SessionCookie:               httpapi.SessionCookieConfig{Name: "peergo_session", Path: "/"},
		StaffSessionCookie:          httpapi.SessionCookieConfig{Name: "peergo_staff_session", Path: "/api/v1/admin"},
		AllowedOrigins:              []string{"http://peergo.test"},
	}
	tests := []struct {
		name   string
		mutate func(*Dependencies)
	}{
		{
			name: "same cookie name",
			mutate: func(dependencies *Dependencies) {
				dependencies.StaffSessionCookie.Name = dependencies.SessionCookie.Name
			},
		},
		{
			name: "narrowed Web path",
			mutate: func(dependencies *Dependencies) {
				dependencies.SessionCookie.Path = "/api/v1"
			},
		},
		{
			name: "widened staff path",
			mutate: func(dependencies *Dependencies) {
				dependencies.StaffSessionCookie.Path = "/"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dependencies := base
			test.mutate(&dependencies)
			if _, err := New(dependencies, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
				t.Fatal("New() accepted an unsafe cookie audience boundary")
			}
		})
	}
}

func TestGrantAdministrationRequiresStaffAudienceAndMapsOverview(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	staffService := &recordingStaffIdentityService{currentResult: identity.StaffSession{
		User:                    identity.User{ID: userID, Username: "governance", DisplayName: "治理成员"},
		WebAuthnAuthenticatedAt: now.Add(-time.Minute),
	}}
	grantID := uuid.New()
	grantService := &recordingGrantAdministrationService{overviewResult: authz.GrantAdministrationOverview{
		PolicyVersion: authz.PolicyVersion,
		Grants: []authz.GrantAdministrationGrant{{
			ID: grantID, SubjectID: uuid.New(), SubjectUsername: "target", SubjectDisplayName: "目标成员",
			RoleID: "member", RoleName: "普通成员", MandateID: uuid.New(), MandateStatus: authz.MandateActive,
			Scope: authz.SiteScope(), ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour), Version: 3,
		}},
	}}
	handler := testHandlerWithCompleteServices(t, unavailableIdentityService{}, staffService, unavailableAuthorizationService{}, grantService)

	unauthorized := httptest.NewRequest(http.MethodGet, "/api/v1/admin/authz/grants", nil)
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, body=%s", unauthorizedResponse.Code, unauthorizedResponse.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/authz/grants", nil)
	request.AddCookie(&http.Cookie{Name: "peergo_session", Value: "ordinary-token"})
	request.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if staffService.currentStaffToken != "staff-token" || grantService.overviewActor.Subject.ID != userID || !grantService.overviewActor.MFAAuthenticatedAt.Equal(now.Add(-time.Minute)) {
		t.Fatalf("staff token=%q actor=%+v", staffService.currentStaffToken, grantService.overviewActor)
	}
	var body generated.GrantAdministrationOverview
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	if body.PolicyVersion != authz.PolicyVersion || len(body.Grants) != 1 || body.Grants[0].Id != grantID || body.Grants[0].Version != 3 {
		t.Fatalf("overview body = %+v", body)
	}
}

func TestGrantRevocationWriteUsesStaffCSRFAndTypedReviewDomain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	requestID := uuid.New()
	grantID := uuid.New()
	staffCSRF := strings.Repeat("s", 43)
	staffService := &recordingStaffIdentityService{currentResult: identity.StaffSession{
		User: identity.User{ID: userID}, WebAuthnAuthenticatedAt: now,
	}}
	grantService := &recordingGrantAdministrationService{
		proposeResult: authz.GrantRevocationRequest{
			ID: requestID, GrantID: grantID, ExpectedGrantVersion: 4, TargetSubjectID: uuid.New(), ProposerID: userID,
			Reason: "任期结束后撤销授权并等待独立职责复核。", Status: authz.GrantRevocationPendingStatus,
			CreatedAt: now, ExpiresAt: now.Add(time.Hour), Reviews: []authz.GrantRevocationReview{},
		},
		reviewResult: authz.GrantRevocationRequest{
			ID: requestID, GrantID: grantID, ExpectedGrantVersion: 4, TargetSubjectID: uuid.New(), ProposerID: uuid.New(),
			Reason: "任期结束后撤销授权并等待独立职责复核。", Status: authz.GrantRevocationPendingStatus,
			CreatedAt: now, ExpiresAt: now.Add(time.Hour), Reviews: []authz.GrantRevocationReview{},
		},
	}
	handler := testHandlerWithCompleteServices(t, unavailableIdentityService{}, staffService, unavailableAuthorizationService{}, grantService)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/authz/grant-revocations", strings.NewReader(`{"grant_id":"`+grantID.String()+`","expected_grant_version":4,"reason":"任期结束后撤销授权并等待独立职责复核。"}`))
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set("Origin", "http://peergo.test")
	createRequest.Header.Set("X-CSRF-Token", staffCSRF)
	createRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", createResponse.Code, createResponse.Body.String())
	}
	if staffService.writeStaffToken != "staff-token" || staffService.writeCSRF != staffCSRF || grantService.proposeInput.GrantID != grantID || grantService.proposeInput.ExpectedGrantVersion != 4 {
		t.Fatalf("write token=%q csrf=%q proposal=%+v", staffService.writeStaffToken, staffService.writeCSRF, grantService.proposeInput)
	}

	reviewRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/authz/grant-revocations/"+requestID.String()+"/security-review", strings.NewReader(`{"decision":"approve","reason":"安全职责完成独立核对并同意撤销该授权。"}`))
	reviewRequest.Header.Set("Content-Type", "application/json")
	reviewRequest.Header.Set("Origin", "http://peergo.test")
	reviewRequest.Header.Set("X-CSRF-Token", staffCSRF)
	reviewRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	reviewResponse := httptest.NewRecorder()
	handler.ServeHTTP(reviewResponse, reviewRequest)
	if reviewResponse.Code != http.StatusOK {
		t.Fatalf("review status = %d, body=%s", reviewResponse.Code, reviewResponse.Body.String())
	}
	if grantService.reviewInput.RequestID != requestID || grantService.reviewInput.Domain != authz.GrantReviewSecurity || grantService.reviewInput.Decision != authz.GrantReviewApprove {
		t.Fatalf("review input = %+v", grantService.reviewInput)
	}
}

func TestCategoryAdministrationUsesStaffAudienceAndOptimisticVersion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 15, 0, 0, 0, time.UTC)
	userID := uuid.New()
	staffCSRF := strings.Repeat("s", 43)
	staffService := &recordingStaffIdentityService{currentResult: identity.StaffSession{
		User:                    identity.User{ID: userID, Username: "catalog", DisplayName: "分类管理员"},
		WebAuthnAuthenticatedAt: now.Add(-time.Minute),
	}}
	categoryService := &recordingCategoryAdministrationService{
		listResult: []catalog.ManagedCategory{{
			ID: "movies", Name: "电影", DisplayOrder: 10, Enabled: true, Version: 3,
			TorrentCount: 2, CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
		}},
		createResult: catalog.ManagedCategory{
			ID: "software", Name: "软件", DisplayOrder: 60, Enabled: true, Version: 1,
			CreatedAt: now, UpdatedAt: now,
		},
		updateResult: catalog.ManagedCategory{
			ID: "movies", Name: "电影与短片", DisplayOrder: 10, Enabled: false, Version: 4,
			TorrentCount: 2, CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
		},
	}
	handler := testHandlerWithEveryService(
		t, unavailableIdentityService{}, staffService, unavailableStaffEnrollmentService{},
		unavailableAuthorizationService{}, unavailableGrantAdministrationService{}, categoryService,
		unavailableSiteDisplaySettingsService{},
	)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/catalog/categories", nil)
	listRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", listResponse.Code, listResponse.Body.String())
	}
	if staffService.currentStaffToken != "staff-token" || categoryService.listActor.Subject.ID != userID {
		t.Fatalf("staff token=%q actor=%+v", staffService.currentStaffToken, categoryService.listActor)
	}

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/catalog/categories", strings.NewReader(`{"id":"software","name":"软件","display_order":60,"enabled":true,"reason":"新增软件分类以承载正式内容。"}`))
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set("Origin", "http://peergo.test")
	createRequest.Header.Set("X-CSRF-Token", staffCSRF)
	createRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", createResponse.Code, createResponse.Body.String())
	}
	if staffService.writeCSRF != staffCSRF || categoryService.createInput.ID != "software" || categoryService.createInput.DisplayOrder != 60 {
		t.Fatalf("write csrf=%q input=%+v", staffService.writeCSRF, categoryService.createInput)
	}

	updateRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/catalog/categories/movies", strings.NewReader(`{"name":"电影与短片","display_order":10,"enabled":false,"expected_version":3,"reason":"停用分类并核对已有种子的展示影响。"}`))
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.Header.Set("Origin", "http://peergo.test")
	updateRequest.Header.Set("X-CSRF-Token", staffCSRF)
	updateRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update status = %d, body=%s", updateResponse.Code, updateResponse.Body.String())
	}
	if categoryService.updateInput.ID != "movies" || categoryService.updateInput.ExpectedVersion != 3 || categoryService.updateInput.Enabled {
		t.Fatalf("update input = %+v", categoryService.updateInput)
	}

	categoryService.err = catalog.ErrCategoryVersionConflict
	conflictRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/catalog/categories/movies", strings.NewReader(`{"name":"电影","display_order":10,"enabled":true,"expected_version":3,"reason":"使用旧版本提交以验证冲突响应。"}`))
	conflictRequest.Header.Set("Content-Type", "application/json")
	conflictRequest.Header.Set("Origin", "http://peergo.test")
	conflictRequest.Header.Set("X-CSRF-Token", staffCSRF)
	conflictRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflictRequest)
	if conflictResponse.Code != http.StatusConflict || !strings.Contains(conflictResponse.Body.String(), "category_version_conflict") {
		t.Fatalf("conflict status = %d, body=%s", conflictResponse.Code, conflictResponse.Body.String())
	}
}

func TestSiteDisplaySettingsUsesStaffAudienceAndOptimisticVersion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 10, 0, 0, 0, time.UTC)
	userID := uuid.New()
	staffCSRF := strings.Repeat("s", 43)
	staffService := &recordingStaffIdentityService{currentResult: identity.StaffSession{
		User:                    identity.User{ID: userID, Username: "display", DisplayName: "展示管理员"},
		WebAuthnAuthenticatedAt: now.Add(-time.Minute),
	}}
	settingsService := &recordingSiteDisplaySettingsService{
		getResult: catalog.SiteDisplaySettings{
			Name: "PeerGo", Description: "旧说明", DefaultTorrentView: catalog.TorrentViewList,
			TorrentFilenamePrefix:  "[ROUSI]",
			CustomNavigationItems:  []catalog.CustomNavigationItem{},
			ShowLatestAnnouncement: true, Version: 3, EffectiveAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
		},
		updateResult: catalog.SiteDisplaySettings{
			Name: "PeerGo Club", Description: "新说明", DefaultTorrentView: catalog.TorrentViewPoster,
			TorrentFilenamePrefix:  "[ROUSI]",
			CustomNavigationItems:  []catalog.CustomNavigationItem{{Label: "Wiki", URL: "https://wiki.example.com", OpenInNewTab: true, Enabled: true}},
			ShowLatestAnnouncement: false, Version: 4, EffectiveAt: now, UpdatedAt: now,
		},
	}
	handler := testHandlerWithEveryService(
		t, unavailableIdentityService{}, staffService, unavailableStaffEnrollmentService{},
		unavailableAuthorizationService{}, unavailableGrantAdministrationService{},
		unavailableCategoryAdministrationService{}, settingsService,
	)

	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/site", nil)
	getRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"version":3`) || !strings.Contains(getResponse.Body.String(), `"torrent_filename_prefix":"[ROUSI]"`) {
		t.Fatalf("get status = %d, body=%s", getResponse.Code, getResponse.Body.String())
	}
	if staffService.currentStaffToken != "staff-token" || settingsService.getActor.Subject.ID != userID {
		t.Fatalf("staff token=%q actor=%+v", staffService.currentStaffToken, settingsService.getActor)
	}

	updateRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/site", strings.NewReader(`{"name":"PeerGo Club","description":"新说明","torrent_filename_prefix":"[ROUSI]","default_torrent_view":"poster","show_latest_announcement":false,"custom_navigation_items":[{"label":"Wiki","url":"https://wiki.example.com","open_in_new_tab":true,"enabled":true}],"expected_version":3,"reason":"调整公开文案和默认展示以匹配当前社区定位。"}`))
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.Header.Set("Origin", "http://peergo.test")
	updateRequest.Header.Set("X-CSRF-Token", staffCSRF)
	updateRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK || !strings.Contains(updateResponse.Body.String(), `"version":4`) || !strings.Contains(updateResponse.Body.String(), `"url":"https://wiki.example.com"`) {
		t.Fatalf("update status = %d, body=%s", updateResponse.Code, updateResponse.Body.String())
	}
	if staffService.writeStaffToken != "staff-token" || staffService.writeCSRF != staffCSRF || settingsService.updateActor.Subject.ID != userID || settingsService.updateInput.ExpectedVersion != 3 || settingsService.updateInput.TorrentFilenamePrefix != "[ROUSI]" || settingsService.updateInput.DefaultTorrentView != catalog.TorrentViewPoster || settingsService.updateInput.ShowLatestAnnouncement || len(settingsService.updateInput.CustomNavigationItems) != 1 || settingsService.updateInput.CustomNavigationItems[0].URL != "https://wiki.example.com" {
		t.Fatalf("write token=%q csrf=%q actor=%+v input=%+v", staffService.writeStaffToken, staffService.writeCSRF, settingsService.updateActor, settingsService.updateInput)
	}

	settingsService.err = catalog.ErrSiteDisplaySettingsVersionConflict
	conflictRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/site", strings.NewReader(`{"name":"PeerGo","description":"旧说明","torrent_filename_prefix":"[ROUSI]","default_torrent_view":"list","show_latest_announcement":true,"custom_navigation_items":[],"expected_version":3,"reason":"使用旧版本提交以验证设置冲突响应。"}`))
	conflictRequest.Header.Set("Content-Type", "application/json")
	conflictRequest.Header.Set("Origin", "http://peergo.test")
	conflictRequest.Header.Set("X-CSRF-Token", staffCSRF)
	conflictRequest.AddCookie(&http.Cookie{Name: "peergo_staff_session", Value: "staff-token"})
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflictRequest)
	if conflictResponse.Code != http.StatusConflict || !strings.Contains(conflictResponse.Body.String(), "site_display_settings_version_conflict") {
		t.Fatalf("conflict status = %d, body=%s", conflictResponse.Code, conflictResponse.Body.String())
	}
}

func TestAccountAccessInspectionUsesAnonymousCredentialProofAndNoStore(t *testing.T) {
	now := time.Date(2026, time.August, 17, 3, 30, 0, 0, time.UTC)
	expiresAt := now.Add(24 * time.Hour)
	identityService := &recordingIdentityService{accountAccessStatus: identity.AccountAccessStatus{
		Restricted: true,
		CanAppeal:  true,
		Restriction: &identity.AccountAccessRestriction{
			SourceKind: identity.AccountAccessSourceTemporaryRestriction,
			ReasonCode: "manual_review", ReasonSummary: "等待人工复核。",
			StartsAt: now, ExpiresAt: &expiresAt, SourceVersion: 3,
		},
	}}
	handler := testHandlerWithIdentity(t, identityService)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/account-access/status", strings.NewReader(`{
        "credentials":{"identifier":"member","password":"secret","second_factor_code":"123456"}
    }`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://peergo.test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(response.Body.String(), `"source_kind":"temporary_restriction"`) {
		t.Fatalf("inspection status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	proof := identityService.accountAccessInspectInput.Credentials
	if proof.Identifier != "member" || proof.Password != "secret" || proof.SecondFactorCode != "123456" {
		t.Fatalf("credential proof=%+v", proof)
	}
}

func TestDownloadRestrictionAppealUsesWebSessionAndKeepsSourcesSeparate(t *testing.T) {
	now := time.Date(2026, time.August, 17, 5, 40, 0, 0, time.UTC)
	userID, appealID := uuid.New(), uuid.New()
	identityService := &recordingIdentityService{
		downloadRestriction: identity.DownloadRestrictionStatus{
			Restricted: true,
			Sources: identity.DownloadRestrictionSources{
				ManualOrLegacy: true, HitAndRun: true,
			},
			Restriction: &identity.AccountAccessRestriction{
				SourceKind: identity.AccountAccessSourceManualDownload,
				ReasonCode: "legacy_download_restriction", ReasonSummary: "旧站下载限制等待复核。",
				StartsAt: now, SourceVersion: 1,
			},
			CanAppeal: true,
		},
		accountAccessAppeal: identity.AccountAccessAppeal{
			ID: appealID, UserID: userID, UserNumericID: 42, Username: "member",
			Restriction: identity.AccountAccessRestriction{
				SourceKind: identity.AccountAccessSourceManualDownload,
				ReasonCode: "legacy_download_restriction", ReasonSummary: "旧站下载限制等待复核。",
				StartsAt: now, SourceVersion: 1,
			},
			Statement: "旧站下载限制已经不符合当前情况，请单独复核。",
			Status:    identity.AccountAccessAppealPending, CreatedAt: now, SourceActive: true,
		},
	}
	handler := testHandlerWithIdentity(t, identityService)

	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/me/download-restriction", nil)
	getRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK || getResponse.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(getResponse.Body.String(), `"manual_or_legacy":true`) ||
		!strings.Contains(getResponse.Body.String(), `"hit_and_run":true`) {
		t.Fatalf("download restriction status=%d headers=%v body=%s", getResponse.Code, getResponse.Header(), getResponse.Body.String())
	}

	csrf := strings.Repeat("c", 43)
	postRequest := httptest.NewRequest(http.MethodPost, "/api/v1/me/download-restriction/appeals", strings.NewReader(`{"statement":"旧站下载限制已经不符合当前情况，请单独复核该来源。"}`))
	postRequest.Header.Set("Content-Type", "application/json")
	postRequest.Header.Set("Origin", "http://peergo.test")
	postRequest.Header.Set("X-CSRF-Token", csrf)
	postRequest.Header.Set("Idempotency-Key", appealID.String())
	postRequest.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	postResponse := httptest.NewRecorder()
	handler.ServeHTTP(postResponse, postRequest)
	if postResponse.Code != http.StatusCreated || postResponse.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(postResponse.Body.String(), `"source_kind":"manual_download_restriction"`) {
		t.Fatalf("download appeal status=%d headers=%v body=%s", postResponse.Code, postResponse.Header(), postResponse.Body.String())
	}
	if identityService.downloadRestrictionToken != "web-token" || identityService.downloadRestrictionCSRF != csrf ||
		identityService.downloadRestrictionInput.AppealID != appealID {
		t.Fatalf("token=%q csrf=%q input=%+v", identityService.downloadRestrictionToken, identityService.downloadRestrictionCSRF, identityService.downloadRestrictionInput)
	}
}

func testHandler(t *testing.T) http.Handler {
	return testHandlerWithIdentity(t, unavailableIdentityService{})
}

func testHandlerWithTorrentUpload(t *testing.T, torrentUpload httpapi.TorrentUploadService, maxBytes int) http.Handler {
	t.Helper()
	now := time.Date(2026, time.August, 8, 22, 0, 0, 0, time.UTC)
	handler, err := New(Dependencies{
		Catalog:                     catalog.NewService(catalog.NewDemoRepository(now), func() time.Time { return now }),
		Identity:                    unavailableIdentityService{},
		Registration:                unavailableRegistrationService{},
		Invitations:                 unavailableInvitationService{},
		EmailVerification:           unavailableEmailVerificationService{},
		PasswordRecovery:            unavailablePasswordRecoveryService{},
		SessionSecurity:             unavailableSessionSecurityService{},
		TwoFactor:                   unavailableTwoFactorService{},
		StaffIdentity:               unavailableStaffIdentityService{},
		StaffEnrollment:             unavailableStaffEnrollmentService{},
		Authorization:               unavailableAuthorizationService{},
		GrantAdministration:         unavailableGrantAdministrationService{},
		CategoryAdministration:      unavailableCategoryAdministrationService{},
		AnnouncementAdministration:  unavailableAnnouncementAdministrationService{},
		Wiki:                        unavailableWikiService{},
		SiteDisplaySettings:         unavailableSiteDisplaySettingsService{},
		UserAdministration:          unavailableUserAdministrationService{},
		Notifications:               unavailableNotificationService{},
		TrafficOverview:             unavailableTrafficOverviewService{},
		EconomyOverview:             unavailableEconomyOverviewService{},
		Attendance:                  unavailableAttendanceService{},
		MemberGifts:                 unavailableMemberGiftService{},
		ContentTips:                 unavailableContentTipService{},
		Workgroups:                  unavailableWorkgroupService{},
		SeedingRewardAdministration: unavailableSeedingRewardAdministrationService{},
		HNRPolicyAdministration:     unavailableHNRPolicyAdministrationService{},
		Operations:                  unavailableOperationsService{},
		TorrentBookmarks:            unavailableTorrentBookmarkService{},
		Comments:                    unavailableCommentService{},
		SocialPosts:                 unavailableSocialPostService{},
		CommentModeration:           unavailableCommentModerationService{},
		TorrentRead:                 unavailableTorrentReadService{},
		TorrentUpload:               torrentUpload,
		TorrentDownload:             unavailableTorrentDownloadService{},
		TorrentReview:               unavailableTorrentReviewService{},
		TorrentResubmission:         unavailableTorrentResubmissionService{},
		TorrentMaintenance:          unavailableTorrentMaintenanceService{},
		PromotionAdministration:     unavailablePromotionAdministrationService{},
		RSS:                         unavailableRSSService{},
		RatioWatchAdministration:    unavailableRatioWatchAdministrationService{},
		NewcomerAdministration:      unavailableNewcomerAdministrationService{},
		TorrentUploadMaxBytes:       maxBytes,
		SessionCookie:               httpapi.SessionCookieConfig{Name: "peergo_session", Path: "/"},
		StaffSessionCookie:          httpapi.SessionCookieConfig{Name: "peergo_staff_session", Path: "/api/v1/admin"},
		AllowedOrigins:              []string{"http://peergo.test"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func testHandlerWithTraffic(t *testing.T, trafficOverview httpapi.TrafficOverviewService) http.Handler {
	t.Helper()
	now := time.Date(2026, time.August, 9, 15, 0, 0, 0, time.UTC)
	handler, err := New(Dependencies{
		Catalog:                     catalog.NewService(catalog.NewDemoRepository(now), func() time.Time { return now }),
		Identity:                    unavailableIdentityService{},
		Registration:                unavailableRegistrationService{},
		Invitations:                 unavailableInvitationService{},
		EmailVerification:           unavailableEmailVerificationService{},
		PasswordRecovery:            unavailablePasswordRecoveryService{},
		SessionSecurity:             unavailableSessionSecurityService{},
		TwoFactor:                   unavailableTwoFactorService{},
		StaffIdentity:               unavailableStaffIdentityService{},
		StaffEnrollment:             unavailableStaffEnrollmentService{},
		Authorization:               unavailableAuthorizationService{},
		GrantAdministration:         unavailableGrantAdministrationService{},
		CategoryAdministration:      unavailableCategoryAdministrationService{},
		AnnouncementAdministration:  unavailableAnnouncementAdministrationService{},
		Wiki:                        unavailableWikiService{},
		SiteDisplaySettings:         unavailableSiteDisplaySettingsService{},
		UserAdministration:          unavailableUserAdministrationService{},
		Notifications:               unavailableNotificationService{},
		TrafficOverview:             trafficOverview,
		EconomyOverview:             unavailableEconomyOverviewService{},
		Attendance:                  unavailableAttendanceService{},
		MemberGifts:                 unavailableMemberGiftService{},
		ContentTips:                 unavailableContentTipService{},
		Workgroups:                  unavailableWorkgroupService{},
		SeedingRewardAdministration: unavailableSeedingRewardAdministrationService{},
		HNRPolicyAdministration:     unavailableHNRPolicyAdministrationService{},
		Operations:                  unavailableOperationsService{},
		TorrentBookmarks:            unavailableTorrentBookmarkService{},
		Comments:                    unavailableCommentService{},
		SocialPosts:                 unavailableSocialPostService{},
		CommentModeration:           unavailableCommentModerationService{},
		TorrentRead:                 unavailableTorrentReadService{},
		TorrentUpload:               unavailableTorrentUploadService{},
		TorrentDownload:             unavailableTorrentDownloadService{},
		TorrentReview:               unavailableTorrentReviewService{},
		TorrentResubmission:         unavailableTorrentResubmissionService{},
		TorrentMaintenance:          unavailableTorrentMaintenanceService{},
		PromotionAdministration:     unavailablePromotionAdministrationService{},
		RSS:                         unavailableRSSService{},
		RatioWatchAdministration:    unavailableRatioWatchAdministrationService{},
		NewcomerAdministration:      unavailableNewcomerAdministrationService{},
		TorrentUploadMaxBytes:       4 << 20,
		SessionCookie:               httpapi.SessionCookieConfig{Name: "peergo_session", Path: "/"},
		StaffSessionCookie:          httpapi.SessionCookieConfig{Name: "peergo_staff_session", Path: "/api/v1/admin"},
		AllowedOrigins:              []string{"http://peergo.test"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func testHandlerWithNotifications(t *testing.T, notificationService httpapi.NotificationService) http.Handler {
	t.Helper()
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	handler, err := New(Dependencies{
		Catalog:                     catalog.NewService(catalog.NewDemoRepository(now), func() time.Time { return now }),
		Identity:                    unavailableIdentityService{},
		Registration:                unavailableRegistrationService{},
		Invitations:                 unavailableInvitationService{},
		EmailVerification:           unavailableEmailVerificationService{},
		PasswordRecovery:            unavailablePasswordRecoveryService{},
		SessionSecurity:             unavailableSessionSecurityService{},
		TwoFactor:                   unavailableTwoFactorService{},
		StaffIdentity:               unavailableStaffIdentityService{},
		StaffEnrollment:             unavailableStaffEnrollmentService{},
		Authorization:               unavailableAuthorizationService{},
		GrantAdministration:         unavailableGrantAdministrationService{},
		CategoryAdministration:      unavailableCategoryAdministrationService{},
		AnnouncementAdministration:  unavailableAnnouncementAdministrationService{},
		Wiki:                        unavailableWikiService{},
		SiteDisplaySettings:         unavailableSiteDisplaySettingsService{},
		UserAdministration:          unavailableUserAdministrationService{},
		Notifications:               notificationService,
		TrafficOverview:             unavailableTrafficOverviewService{},
		EconomyOverview:             unavailableEconomyOverviewService{},
		Attendance:                  unavailableAttendanceService{},
		MemberGifts:                 unavailableMemberGiftService{},
		ContentTips:                 unavailableContentTipService{},
		Workgroups:                  unavailableWorkgroupService{},
		SeedingRewardAdministration: unavailableSeedingRewardAdministrationService{},
		HNRPolicyAdministration:     unavailableHNRPolicyAdministrationService{},
		Operations:                  unavailableOperationsService{},
		TorrentBookmarks:            unavailableTorrentBookmarkService{},
		Comments:                    unavailableCommentService{},
		SocialPosts:                 unavailableSocialPostService{},
		CommentModeration:           unavailableCommentModerationService{},
		TorrentRead:                 unavailableTorrentReadService{},
		TorrentUpload:               unavailableTorrentUploadService{},
		TorrentDownload:             unavailableTorrentDownloadService{},
		TorrentReview:               unavailableTorrentReviewService{},
		TorrentResubmission:         unavailableTorrentResubmissionService{},
		TorrentMaintenance:          unavailableTorrentMaintenanceService{},
		PromotionAdministration:     unavailablePromotionAdministrationService{},
		RSS:                         unavailableRSSService{},
		RatioWatchAdministration:    unavailableRatioWatchAdministrationService{},
		NewcomerAdministration:      unavailableNewcomerAdministrationService{},
		TorrentUploadMaxBytes:       4 << 20,
		SessionCookie:               httpapi.SessionCookieConfig{Name: "peergo_session", Path: "/"},
		StaffSessionCookie:          httpapi.SessionCookieConfig{Name: "peergo_staff_session", Path: "/api/v1/admin"},
		AllowedOrigins:              []string{"http://peergo.test"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func testHandlerWithTorrentBookmarks(t *testing.T, torrentBookmarks httpapi.TorrentBookmarkService) http.Handler {
	t.Helper()
	now := time.Date(2026, time.August, 9, 16, 0, 0, 0, time.UTC)
	handler, err := New(Dependencies{
		Catalog:                     catalog.NewService(catalog.NewDemoRepository(now), func() time.Time { return now }),
		Identity:                    unavailableIdentityService{},
		Registration:                unavailableRegistrationService{},
		Invitations:                 unavailableInvitationService{},
		EmailVerification:           unavailableEmailVerificationService{},
		PasswordRecovery:            unavailablePasswordRecoveryService{},
		SessionSecurity:             unavailableSessionSecurityService{},
		TwoFactor:                   unavailableTwoFactorService{},
		StaffIdentity:               unavailableStaffIdentityService{},
		StaffEnrollment:             unavailableStaffEnrollmentService{},
		Authorization:               unavailableAuthorizationService{},
		GrantAdministration:         unavailableGrantAdministrationService{},
		CategoryAdministration:      unavailableCategoryAdministrationService{},
		AnnouncementAdministration:  unavailableAnnouncementAdministrationService{},
		Wiki:                        unavailableWikiService{},
		SiteDisplaySettings:         unavailableSiteDisplaySettingsService{},
		UserAdministration:          unavailableUserAdministrationService{},
		Notifications:               unavailableNotificationService{},
		TrafficOverview:             unavailableTrafficOverviewService{},
		EconomyOverview:             unavailableEconomyOverviewService{},
		Attendance:                  unavailableAttendanceService{},
		MemberGifts:                 unavailableMemberGiftService{},
		ContentTips:                 unavailableContentTipService{},
		Workgroups:                  unavailableWorkgroupService{},
		SeedingRewardAdministration: unavailableSeedingRewardAdministrationService{},
		HNRPolicyAdministration:     unavailableHNRPolicyAdministrationService{},
		Operations:                  unavailableOperationsService{},
		TorrentBookmarks:            torrentBookmarks,
		Comments:                    unavailableCommentService{},
		SocialPosts:                 unavailableSocialPostService{},
		CommentModeration:           unavailableCommentModerationService{},
		TorrentRead:                 unavailableTorrentReadService{},
		TorrentUpload:               unavailableTorrentUploadService{},
		TorrentDownload:             unavailableTorrentDownloadService{},
		TorrentReview:               unavailableTorrentReviewService{},
		TorrentResubmission:         unavailableTorrentResubmissionService{},
		TorrentMaintenance:          unavailableTorrentMaintenanceService{},
		PromotionAdministration:     unavailablePromotionAdministrationService{},
		RSS:                         unavailableRSSService{},
		RatioWatchAdministration:    unavailableRatioWatchAdministrationService{},
		NewcomerAdministration:      unavailableNewcomerAdministrationService{},
		TorrentUploadMaxBytes:       4 << 20,
		SessionCookie:               httpapi.SessionCookieConfig{Name: "peergo_session", Path: "/"},
		StaffSessionCookie:          httpapi.SessionCookieConfig{Name: "peergo_staff_session", Path: "/api/v1/admin"},
		AllowedOrigins:              []string{"http://peergo.test"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func testHandlerWithComments(t *testing.T, comments httpapi.CommentService) http.Handler {
	t.Helper()
	now := time.Date(2026, time.August, 9, 17, 0, 0, 0, time.UTC)
	handler, err := New(Dependencies{
		Catalog:                     catalog.NewService(catalog.NewDemoRepository(now), func() time.Time { return now }),
		Identity:                    unavailableIdentityService{},
		Registration:                unavailableRegistrationService{},
		Invitations:                 unavailableInvitationService{},
		EmailVerification:           unavailableEmailVerificationService{},
		PasswordRecovery:            unavailablePasswordRecoveryService{},
		SessionSecurity:             unavailableSessionSecurityService{},
		TwoFactor:                   unavailableTwoFactorService{},
		StaffIdentity:               unavailableStaffIdentityService{},
		StaffEnrollment:             unavailableStaffEnrollmentService{},
		Authorization:               unavailableAuthorizationService{},
		GrantAdministration:         unavailableGrantAdministrationService{},
		CategoryAdministration:      unavailableCategoryAdministrationService{},
		AnnouncementAdministration:  unavailableAnnouncementAdministrationService{},
		Wiki:                        unavailableWikiService{},
		SiteDisplaySettings:         unavailableSiteDisplaySettingsService{},
		UserAdministration:          unavailableUserAdministrationService{},
		Notifications:               unavailableNotificationService{},
		TrafficOverview:             unavailableTrafficOverviewService{},
		EconomyOverview:             unavailableEconomyOverviewService{},
		Attendance:                  unavailableAttendanceService{},
		MemberGifts:                 unavailableMemberGiftService{},
		ContentTips:                 unavailableContentTipService{},
		Workgroups:                  unavailableWorkgroupService{},
		SeedingRewardAdministration: unavailableSeedingRewardAdministrationService{},
		HNRPolicyAdministration:     unavailableHNRPolicyAdministrationService{},
		Operations:                  unavailableOperationsService{},
		TorrentBookmarks:            unavailableTorrentBookmarkService{},
		Comments:                    comments,
		SocialPosts:                 unavailableSocialPostService{},
		CommentModeration:           unavailableCommentModerationService{},
		TorrentRead:                 unavailableTorrentReadService{},
		TorrentUpload:               unavailableTorrentUploadService{},
		TorrentDownload:             unavailableTorrentDownloadService{},
		TorrentReview:               unavailableTorrentReviewService{},
		TorrentResubmission:         unavailableTorrentResubmissionService{},
		TorrentMaintenance:          unavailableTorrentMaintenanceService{},
		PromotionAdministration:     unavailablePromotionAdministrationService{},
		RSS:                         unavailableRSSService{},
		RatioWatchAdministration:    unavailableRatioWatchAdministrationService{},
		NewcomerAdministration:      unavailableNewcomerAdministrationService{},
		TorrentUploadMaxBytes:       4 << 20,
		SessionCookie:               httpapi.SessionCookieConfig{Name: "peergo_session", Path: "/"},
		StaffSessionCookie:          httpapi.SessionCookieConfig{Name: "peergo_staff_session", Path: "/api/v1/admin"},
		AllowedOrigins:              []string{"http://peergo.test"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func testHandlerWithCommentModeration(t *testing.T, staffIdentity httpapi.StaffIdentityService, moderation httpapi.CommentModerationService) http.Handler {
	t.Helper()
	now := time.Date(2026, time.August, 9, 18, 0, 0, 0, time.UTC)
	handler, err := New(Dependencies{
		Catalog:                     catalog.NewService(catalog.NewDemoRepository(now), func() time.Time { return now }),
		Identity:                    unavailableIdentityService{},
		Registration:                unavailableRegistrationService{},
		Invitations:                 unavailableInvitationService{},
		EmailVerification:           unavailableEmailVerificationService{},
		PasswordRecovery:            unavailablePasswordRecoveryService{},
		SessionSecurity:             unavailableSessionSecurityService{},
		TwoFactor:                   unavailableTwoFactorService{},
		StaffIdentity:               staffIdentity,
		StaffEnrollment:             unavailableStaffEnrollmentService{},
		Authorization:               unavailableAuthorizationService{},
		GrantAdministration:         unavailableGrantAdministrationService{},
		CategoryAdministration:      unavailableCategoryAdministrationService{},
		AnnouncementAdministration:  unavailableAnnouncementAdministrationService{},
		Wiki:                        unavailableWikiService{},
		SiteDisplaySettings:         unavailableSiteDisplaySettingsService{},
		UserAdministration:          unavailableUserAdministrationService{},
		Notifications:               unavailableNotificationService{},
		TrafficOverview:             unavailableTrafficOverviewService{},
		EconomyOverview:             unavailableEconomyOverviewService{},
		Attendance:                  unavailableAttendanceService{},
		MemberGifts:                 unavailableMemberGiftService{},
		ContentTips:                 unavailableContentTipService{},
		Workgroups:                  unavailableWorkgroupService{},
		SeedingRewardAdministration: unavailableSeedingRewardAdministrationService{},
		HNRPolicyAdministration:     unavailableHNRPolicyAdministrationService{},
		Operations:                  unavailableOperationsService{},
		TorrentBookmarks:            unavailableTorrentBookmarkService{},
		Comments:                    unavailableCommentService{},
		SocialPosts:                 unavailableSocialPostService{},
		CommentModeration:           moderation,
		TorrentRead:                 unavailableTorrentReadService{},
		TorrentUpload:               unavailableTorrentUploadService{},
		TorrentDownload:             unavailableTorrentDownloadService{},
		TorrentReview:               unavailableTorrentReviewService{},
		TorrentResubmission:         unavailableTorrentResubmissionService{},
		TorrentMaintenance:          unavailableTorrentMaintenanceService{},
		PromotionAdministration:     unavailablePromotionAdministrationService{},
		RSS:                         unavailableRSSService{},
		RatioWatchAdministration:    unavailableRatioWatchAdministrationService{},
		NewcomerAdministration:      unavailableNewcomerAdministrationService{},
		TorrentUploadMaxBytes:       4 << 20,
		SessionCookie:               httpapi.SessionCookieConfig{Name: "peergo_session", Path: "/"},
		StaffSessionCookie:          httpapi.SessionCookieConfig{Name: "peergo_staff_session", Path: "/api/v1/admin"},
		AllowedOrigins:              []string{"http://peergo.test"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func testHandlerWithTorrentDownload(t *testing.T, torrentDownload httpapi.TorrentDownloadService) http.Handler {
	t.Helper()
	now := time.Date(2026, time.August, 8, 22, 0, 0, 0, time.UTC)
	handler, err := New(Dependencies{
		Catalog:                     catalog.NewService(catalog.NewDemoRepository(now), func() time.Time { return now }),
		Identity:                    unavailableIdentityService{},
		Registration:                unavailableRegistrationService{},
		Invitations:                 unavailableInvitationService{},
		EmailVerification:           unavailableEmailVerificationService{},
		PasswordRecovery:            unavailablePasswordRecoveryService{},
		SessionSecurity:             unavailableSessionSecurityService{},
		TwoFactor:                   unavailableTwoFactorService{},
		StaffIdentity:               unavailableStaffIdentityService{},
		StaffEnrollment:             unavailableStaffEnrollmentService{},
		Authorization:               unavailableAuthorizationService{},
		GrantAdministration:         unavailableGrantAdministrationService{},
		CategoryAdministration:      unavailableCategoryAdministrationService{},
		AnnouncementAdministration:  unavailableAnnouncementAdministrationService{},
		Wiki:                        unavailableWikiService{},
		SiteDisplaySettings:         unavailableSiteDisplaySettingsService{},
		UserAdministration:          unavailableUserAdministrationService{},
		Notifications:               unavailableNotificationService{},
		TrafficOverview:             unavailableTrafficOverviewService{},
		EconomyOverview:             unavailableEconomyOverviewService{},
		Attendance:                  unavailableAttendanceService{},
		MemberGifts:                 unavailableMemberGiftService{},
		ContentTips:                 unavailableContentTipService{},
		Workgroups:                  unavailableWorkgroupService{},
		SeedingRewardAdministration: unavailableSeedingRewardAdministrationService{},
		HNRPolicyAdministration:     unavailableHNRPolicyAdministrationService{},
		Operations:                  unavailableOperationsService{},
		TorrentBookmarks:            unavailableTorrentBookmarkService{},
		Comments:                    unavailableCommentService{},
		SocialPosts:                 unavailableSocialPostService{},
		CommentModeration:           unavailableCommentModerationService{},
		TorrentRead:                 unavailableTorrentReadService{},
		TorrentUpload:               unavailableTorrentUploadService{},
		TorrentDownload:             torrentDownload,
		TorrentReview:               unavailableTorrentReviewService{},
		TorrentResubmission:         unavailableTorrentResubmissionService{},
		TorrentMaintenance:          unavailableTorrentMaintenanceService{},
		PromotionAdministration:     unavailablePromotionAdministrationService{},
		RSS:                         unavailableRSSService{},
		RatioWatchAdministration:    unavailableRatioWatchAdministrationService{},
		NewcomerAdministration:      unavailableNewcomerAdministrationService{},
		TorrentUploadMaxBytes:       4 << 20,
		SessionCookie:               httpapi.SessionCookieConfig{Name: "peergo_session", Path: "/"},
		StaffSessionCookie:          httpapi.SessionCookieConfig{Name: "peergo_staff_session", Path: "/api/v1/admin"},
		AllowedOrigins:              []string{"http://peergo.test"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func testHandlerWithTorrentRead(t *testing.T, torrentRead httpapi.TorrentReadService, catalogService *catalog.Service) http.Handler {
	t.Helper()
	handler, err := New(Dependencies{
		Catalog:                     catalogService,
		Identity:                    unavailableIdentityService{},
		Registration:                unavailableRegistrationService{},
		Invitations:                 unavailableInvitationService{},
		EmailVerification:           unavailableEmailVerificationService{},
		PasswordRecovery:            unavailablePasswordRecoveryService{},
		SessionSecurity:             unavailableSessionSecurityService{},
		TwoFactor:                   unavailableTwoFactorService{},
		StaffIdentity:               unavailableStaffIdentityService{},
		StaffEnrollment:             unavailableStaffEnrollmentService{},
		Authorization:               unavailableAuthorizationService{},
		GrantAdministration:         unavailableGrantAdministrationService{},
		CategoryAdministration:      unavailableCategoryAdministrationService{},
		AnnouncementAdministration:  unavailableAnnouncementAdministrationService{},
		Wiki:                        unavailableWikiService{},
		SiteDisplaySettings:         unavailableSiteDisplaySettingsService{},
		UserAdministration:          unavailableUserAdministrationService{},
		Notifications:               unavailableNotificationService{},
		TrafficOverview:             unavailableTrafficOverviewService{},
		EconomyOverview:             unavailableEconomyOverviewService{},
		Attendance:                  unavailableAttendanceService{},
		MemberGifts:                 unavailableMemberGiftService{},
		ContentTips:                 unavailableContentTipService{},
		Workgroups:                  unavailableWorkgroupService{},
		SeedingRewardAdministration: unavailableSeedingRewardAdministrationService{},
		HNRPolicyAdministration:     unavailableHNRPolicyAdministrationService{},
		Operations:                  unavailableOperationsService{},
		TorrentBookmarks:            unavailableTorrentBookmarkService{},
		Comments:                    unavailableCommentService{},
		SocialPosts:                 unavailableSocialPostService{},
		CommentModeration:           unavailableCommentModerationService{},
		TorrentRead:                 torrentRead,
		TorrentUpload:               unavailableTorrentUploadService{},
		TorrentDownload:             unavailableTorrentDownloadService{},
		TorrentReview:               unavailableTorrentReviewService{},
		TorrentResubmission:         unavailableTorrentResubmissionService{},
		TorrentMaintenance:          unavailableTorrentMaintenanceService{},
		PromotionAdministration:     unavailablePromotionAdministrationService{},
		RSS:                         unavailableRSSService{},
		RatioWatchAdministration:    unavailableRatioWatchAdministrationService{},
		NewcomerAdministration:      unavailableNewcomerAdministrationService{},
		TorrentUploadMaxBytes:       4 << 20,
		SessionCookie:               httpapi.SessionCookieConfig{Name: "peergo_session", Path: "/"},
		StaffSessionCookie:          httpapi.SessionCookieConfig{Name: "peergo_staff_session", Path: "/api/v1/admin"},
		AllowedOrigins:              []string{"http://peergo.test"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func testHandlerWithTorrentReview(t *testing.T, staffIdentity httpapi.StaffIdentityService, torrentReview httpapi.TorrentReviewService) http.Handler {
	t.Helper()
	now := time.Date(2026, time.August, 8, 23, 0, 0, 0, time.UTC)
	handler, err := New(Dependencies{
		Catalog:                     catalog.NewService(catalog.NewDemoRepository(now), func() time.Time { return now }),
		Identity:                    unavailableIdentityService{},
		Registration:                unavailableRegistrationService{},
		Invitations:                 unavailableInvitationService{},
		EmailVerification:           unavailableEmailVerificationService{},
		PasswordRecovery:            unavailablePasswordRecoveryService{},
		SessionSecurity:             unavailableSessionSecurityService{},
		TwoFactor:                   unavailableTwoFactorService{},
		StaffIdentity:               staffIdentity,
		StaffEnrollment:             unavailableStaffEnrollmentService{},
		Authorization:               unavailableAuthorizationService{},
		GrantAdministration:         unavailableGrantAdministrationService{},
		CategoryAdministration:      unavailableCategoryAdministrationService{},
		AnnouncementAdministration:  unavailableAnnouncementAdministrationService{},
		Wiki:                        unavailableWikiService{},
		SiteDisplaySettings:         unavailableSiteDisplaySettingsService{},
		UserAdministration:          unavailableUserAdministrationService{},
		Notifications:               unavailableNotificationService{},
		TrafficOverview:             unavailableTrafficOverviewService{},
		EconomyOverview:             unavailableEconomyOverviewService{},
		Attendance:                  unavailableAttendanceService{},
		MemberGifts:                 unavailableMemberGiftService{},
		ContentTips:                 unavailableContentTipService{},
		Workgroups:                  unavailableWorkgroupService{},
		SeedingRewardAdministration: unavailableSeedingRewardAdministrationService{},
		HNRPolicyAdministration:     unavailableHNRPolicyAdministrationService{},
		Operations:                  unavailableOperationsService{},
		TorrentBookmarks:            unavailableTorrentBookmarkService{},
		Comments:                    unavailableCommentService{},
		SocialPosts:                 unavailableSocialPostService{},
		CommentModeration:           unavailableCommentModerationService{},
		TorrentRead:                 unavailableTorrentReadService{},
		TorrentUpload:               unavailableTorrentUploadService{},
		TorrentDownload:             unavailableTorrentDownloadService{},
		TorrentReview:               torrentReview,
		TorrentResubmission:         unavailableTorrentResubmissionService{},
		TorrentMaintenance:          unavailableTorrentMaintenanceService{},
		PromotionAdministration:     unavailablePromotionAdministrationService{},
		RSS:                         unavailableRSSService{},
		RatioWatchAdministration:    unavailableRatioWatchAdministrationService{},
		NewcomerAdministration:      unavailableNewcomerAdministrationService{},
		TorrentUploadMaxBytes:       4 << 20,
		SessionCookie:               httpapi.SessionCookieConfig{Name: "peergo_session", Path: "/"},
		StaffSessionCookie:          httpapi.SessionCookieConfig{Name: "peergo_staff_session", Path: "/api/v1/admin"},
		AllowedOrigins:              []string{"http://peergo.test"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func testHandlerWithTorrentResubmission(t *testing.T, torrentResubmission httpapi.TorrentResubmissionService) http.Handler {
	return testHandlerWithTorrentSubmissionMaintenance(t, torrentResubmission, unavailableTorrentMaintenanceService{})
}

func testHandlerWithTorrentMaintenance(t *testing.T, torrentMaintenance httpapi.TorrentMaintenanceService) http.Handler {
	return testHandlerWithTorrentSubmissionMaintenance(t, unavailableTorrentResubmissionService{}, torrentMaintenance)
}

func testHandlerWithTorrentSubmissionMaintenance(
	t *testing.T,
	torrentResubmission httpapi.TorrentResubmissionService,
	torrentMaintenance httpapi.TorrentMaintenanceService,
) http.Handler {
	t.Helper()
	now := time.Date(2026, time.August, 10, 15, 0, 0, 0, time.UTC)
	handler, err := New(Dependencies{
		Catalog:                     catalog.NewService(catalog.NewDemoRepository(now), func() time.Time { return now }),
		Identity:                    unavailableIdentityService{},
		Registration:                unavailableRegistrationService{},
		Invitations:                 unavailableInvitationService{},
		EmailVerification:           unavailableEmailVerificationService{},
		PasswordRecovery:            unavailablePasswordRecoveryService{},
		SessionSecurity:             unavailableSessionSecurityService{},
		TwoFactor:                   unavailableTwoFactorService{},
		StaffIdentity:               unavailableStaffIdentityService{},
		StaffEnrollment:             unavailableStaffEnrollmentService{},
		Authorization:               unavailableAuthorizationService{},
		GrantAdministration:         unavailableGrantAdministrationService{},
		CategoryAdministration:      unavailableCategoryAdministrationService{},
		AnnouncementAdministration:  unavailableAnnouncementAdministrationService{},
		Wiki:                        unavailableWikiService{},
		SiteDisplaySettings:         unavailableSiteDisplaySettingsService{},
		UserAdministration:          unavailableUserAdministrationService{},
		Notifications:               unavailableNotificationService{},
		TrafficOverview:             unavailableTrafficOverviewService{},
		EconomyOverview:             unavailableEconomyOverviewService{},
		Attendance:                  unavailableAttendanceService{},
		MemberGifts:                 unavailableMemberGiftService{},
		ContentTips:                 unavailableContentTipService{},
		Workgroups:                  unavailableWorkgroupService{},
		SeedingRewardAdministration: unavailableSeedingRewardAdministrationService{},
		HNRPolicyAdministration:     unavailableHNRPolicyAdministrationService{},
		Operations:                  unavailableOperationsService{},
		TorrentBookmarks:            unavailableTorrentBookmarkService{},
		Comments:                    unavailableCommentService{},
		SocialPosts:                 unavailableSocialPostService{},
		CommentModeration:           unavailableCommentModerationService{},
		TorrentRead:                 unavailableTorrentReadService{},
		TorrentUpload:               unavailableTorrentUploadService{},
		TorrentDownload:             unavailableTorrentDownloadService{},
		TorrentReview:               unavailableTorrentReviewService{},
		TorrentResubmission:         torrentResubmission,
		TorrentMaintenance:          torrentMaintenance,
		PromotionAdministration:     unavailablePromotionAdministrationService{},
		RSS:                         unavailableRSSService{},
		RatioWatchAdministration:    unavailableRatioWatchAdministrationService{},
		NewcomerAdministration:      unavailableNewcomerAdministrationService{},
		TorrentUploadMaxBytes:       4 << 20,
		SessionCookie:               httpapi.SessionCookieConfig{Name: "peergo_session", Path: "/"},
		StaffSessionCookie:          httpapi.SessionCookieConfig{Name: "peergo_staff_session", Path: "/api/v1/admin"},
		AllowedOrigins:              []string{"http://peergo.test"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func newTorrentSubmissionRequest(t *testing.T, idempotencyKey uuid.UUID, csrf string, rawMetainfo []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range map[string]string{
		"category_id": "movies",
		"title":       "Release 2026",
		"subtitle":    "First edition",
		"description": "Release description",
		"media_info":  "General",
		"anonymous":   "true",
		"imdb_id":     "tt1234567",
		"tmdb_id":     "123",
		"douban_id":   "456",
	} {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write multipart field %s: %v", name, err)
		}
	}
	facetPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="facet_selections"; filename="facet-selection-1.json"`},
		"Content-Type":        {"application/json"},
	})
	if err != nil {
		t.Fatalf("create multipart facet selection: %v", err)
	}
	if _, err := facetPart.Write([]byte(`{"facet_id":"genre","option_keys":["drama","action"]}`)); err != nil {
		t.Fatalf("write multipart facet selection: %v", err)
	}
	screenshot, err := writer.CreateFormFile("screenshots", "cover.png")
	if err != nil {
		t.Fatalf("create multipart screenshot: %v", err)
	}
	if _, err := screenshot.Write([]byte("screenshot fixture")); err != nil {
		t.Fatalf("write multipart screenshot: %v", err)
	}
	file, err := writer.CreateFormFile("torrent_file", "release.torrent")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := file.Write(rawMetainfo); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/torrents", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Origin", "http://peergo.test")
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("Idempotency-Key", idempotencyKey.String())
	request.AddCookie(&http.Cookie{Name: "peergo_session", Value: "web-token"})
	return request
}

func testHandlerWithIdentity(t *testing.T, identityService httpapi.IdentityService) http.Handler {
	return testHandlerWithServices(t, identityService, unavailableAuthorizationService{})
}

func testHandlerWithServices(t *testing.T, identityService httpapi.IdentityService, authorizationService httpapi.AuthorizationService) http.Handler {
	return testHandlerWithAllServices(t, identityService, unavailableStaffIdentityService{}, authorizationService)
}

func testHandlerWithAllServices(t *testing.T, identityService httpapi.IdentityService, staffIdentityService httpapi.StaffIdentityService, authorizationService httpapi.AuthorizationService) http.Handler {
	return testHandlerWithCompleteServices(t, identityService, staffIdentityService, authorizationService, unavailableGrantAdministrationService{})
}

func testHandlerWithCompleteServices(t *testing.T, identityService httpapi.IdentityService, staffIdentityService httpapi.StaffIdentityService, authorizationService httpapi.AuthorizationService, grantAdministrationService httpapi.GrantAdministrationService) http.Handler {
	return testHandlerWithEveryService(t, identityService, staffIdentityService, unavailableStaffEnrollmentService{}, authorizationService, grantAdministrationService, unavailableCategoryAdministrationService{}, unavailableSiteDisplaySettingsService{})
}

func testHandlerWithSessionSecurity(t *testing.T, sessionSecurity httpapi.SessionSecurityService) http.Handler {
	t.Helper()
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	handler, err := New(Dependencies{
		Catalog:                     catalog.NewService(catalog.NewDemoRepository(now), func() time.Time { return now }),
		Identity:                    unavailableIdentityService{},
		Registration:                unavailableRegistrationService{},
		Invitations:                 unavailableInvitationService{},
		EmailVerification:           unavailableEmailVerificationService{},
		PasswordRecovery:            unavailablePasswordRecoveryService{},
		SessionSecurity:             sessionSecurity,
		TwoFactor:                   unavailableTwoFactorService{},
		StaffIdentity:               unavailableStaffIdentityService{},
		StaffEnrollment:             unavailableStaffEnrollmentService{},
		Authorization:               unavailableAuthorizationService{},
		GrantAdministration:         unavailableGrantAdministrationService{},
		CategoryAdministration:      unavailableCategoryAdministrationService{},
		AnnouncementAdministration:  unavailableAnnouncementAdministrationService{},
		Wiki:                        unavailableWikiService{},
		SiteDisplaySettings:         unavailableSiteDisplaySettingsService{},
		UserAdministration:          unavailableUserAdministrationService{},
		Notifications:               unavailableNotificationService{},
		TrafficOverview:             unavailableTrafficOverviewService{},
		EconomyOverview:             unavailableEconomyOverviewService{},
		Attendance:                  unavailableAttendanceService{},
		MemberGifts:                 unavailableMemberGiftService{},
		ContentTips:                 unavailableContentTipService{},
		Workgroups:                  unavailableWorkgroupService{},
		SeedingRewardAdministration: unavailableSeedingRewardAdministrationService{},
		HNRPolicyAdministration:     unavailableHNRPolicyAdministrationService{},
		Operations:                  unavailableOperationsService{},
		TorrentBookmarks:            unavailableTorrentBookmarkService{},
		Comments:                    unavailableCommentService{},
		SocialPosts:                 unavailableSocialPostService{},
		CommentModeration:           unavailableCommentModerationService{},
		TorrentRead:                 unavailableTorrentReadService{},
		TorrentUpload:               unavailableTorrentUploadService{},
		TorrentDownload:             unavailableTorrentDownloadService{},
		TorrentReview:               unavailableTorrentReviewService{},
		TorrentResubmission:         unavailableTorrentResubmissionService{},
		TorrentMaintenance:          unavailableTorrentMaintenanceService{},
		PromotionAdministration:     unavailablePromotionAdministrationService{},
		RSS:                         unavailableRSSService{},
		RatioWatchAdministration:    unavailableRatioWatchAdministrationService{},
		NewcomerAdministration:      unavailableNewcomerAdministrationService{},
		TorrentUploadMaxBytes:       4 << 20,
		SessionCookie:               httpapi.SessionCookieConfig{Name: "peergo_session", Path: "/"},
		StaffSessionCookie:          httpapi.SessionCookieConfig{Name: "peergo_staff_session", Path: "/api/v1/admin"},
		AllowedOrigins:              []string{"http://peergo.test"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func testHandlerWithTwoFactor(t *testing.T, twoFactor httpapi.TwoFactorService) http.Handler {
	t.Helper()
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	handler, err := New(Dependencies{
		Catalog:                     catalog.NewService(catalog.NewDemoRepository(now), func() time.Time { return now }),
		Identity:                    unavailableIdentityService{},
		Registration:                unavailableRegistrationService{},
		Invitations:                 unavailableInvitationService{},
		EmailVerification:           unavailableEmailVerificationService{},
		PasswordRecovery:            unavailablePasswordRecoveryService{},
		SessionSecurity:             unavailableSessionSecurityService{},
		TwoFactor:                   twoFactor,
		StaffIdentity:               unavailableStaffIdentityService{},
		StaffEnrollment:             unavailableStaffEnrollmentService{},
		Authorization:               unavailableAuthorizationService{},
		GrantAdministration:         unavailableGrantAdministrationService{},
		CategoryAdministration:      unavailableCategoryAdministrationService{},
		AnnouncementAdministration:  unavailableAnnouncementAdministrationService{},
		Wiki:                        unavailableWikiService{},
		SiteDisplaySettings:         unavailableSiteDisplaySettingsService{},
		UserAdministration:          unavailableUserAdministrationService{},
		Notifications:               unavailableNotificationService{},
		TrafficOverview:             unavailableTrafficOverviewService{},
		EconomyOverview:             unavailableEconomyOverviewService{},
		Attendance:                  unavailableAttendanceService{},
		MemberGifts:                 unavailableMemberGiftService{},
		ContentTips:                 unavailableContentTipService{},
		Workgroups:                  unavailableWorkgroupService{},
		SeedingRewardAdministration: unavailableSeedingRewardAdministrationService{},
		HNRPolicyAdministration:     unavailableHNRPolicyAdministrationService{},
		Operations:                  unavailableOperationsService{},
		TorrentBookmarks:            unavailableTorrentBookmarkService{},
		Comments:                    unavailableCommentService{},
		SocialPosts:                 unavailableSocialPostService{},
		CommentModeration:           unavailableCommentModerationService{},
		TorrentRead:                 unavailableTorrentReadService{},
		TorrentUpload:               unavailableTorrentUploadService{},
		TorrentDownload:             unavailableTorrentDownloadService{},
		TorrentReview:               unavailableTorrentReviewService{},
		TorrentResubmission:         unavailableTorrentResubmissionService{},
		TorrentMaintenance:          unavailableTorrentMaintenanceService{},
		PromotionAdministration:     unavailablePromotionAdministrationService{},
		RSS:                         unavailableRSSService{},
		RatioWatchAdministration:    unavailableRatioWatchAdministrationService{},
		NewcomerAdministration:      unavailableNewcomerAdministrationService{},
		TorrentUploadMaxBytes:       4 << 20,
		SessionCookie:               httpapi.SessionCookieConfig{Name: "peergo_session", Path: "/"},
		StaffSessionCookie:          httpapi.SessionCookieConfig{Name: "peergo_staff_session", Path: "/api/v1/admin"},
		AllowedOrigins:              []string{"http://peergo.test"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func testHandlerWithEveryService(t *testing.T, identityService httpapi.IdentityService, staffIdentityService httpapi.StaffIdentityService, staffEnrollmentService httpapi.StaffEnrollmentService, authorizationService httpapi.AuthorizationService, grantAdministrationService httpapi.GrantAdministrationService, categoryAdministrationService httpapi.CategoryAdministrationService, siteDisplaySettingsService httpapi.SiteDisplaySettingsService) http.Handler {
	return testHandlerWithEveryServiceAndUsers(
		t, identityService, staffIdentityService, staffEnrollmentService, authorizationService,
		grantAdministrationService, categoryAdministrationService, siteDisplaySettingsService,
		unavailableUserAdministrationService{},
	)
}

func testHandlerWithUserAdministration(t *testing.T, staffIdentityService httpapi.StaffIdentityService, userAdministrationService httpapi.UserAdministrationService) http.Handler {
	return testHandlerWithEveryServiceAndUsers(
		t, unavailableIdentityService{}, staffIdentityService, unavailableStaffEnrollmentService{},
		unavailableAuthorizationService{}, unavailableGrantAdministrationService{},
		unavailableCategoryAdministrationService{}, unavailableSiteDisplaySettingsService{},
		userAdministrationService,
	)
}

func testHandlerWithEveryServiceAndUsers(t *testing.T, identityService httpapi.IdentityService, staffIdentityService httpapi.StaffIdentityService, staffEnrollmentService httpapi.StaffEnrollmentService, authorizationService httpapi.AuthorizationService, grantAdministrationService httpapi.GrantAdministrationService, categoryAdministrationService httpapi.CategoryAdministrationService, siteDisplaySettingsService httpapi.SiteDisplaySettingsService, userAdministrationService httpapi.UserAdministrationService) http.Handler {
	t.Helper()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	service := catalog.NewService(catalog.NewDemoRepository(now), func() time.Time { return now })
	handler, err := New(Dependencies{
		Catalog:                     service,
		Identity:                    identityService,
		Registration:                unavailableRegistrationService{},
		Invitations:                 unavailableInvitationService{},
		EmailVerification:           unavailableEmailVerificationService{},
		PasswordRecovery:            unavailablePasswordRecoveryService{},
		SessionSecurity:             unavailableSessionSecurityService{},
		TwoFactor:                   unavailableTwoFactorService{},
		StaffIdentity:               staffIdentityService,
		StaffEnrollment:             staffEnrollmentService,
		Authorization:               authorizationService,
		GrantAdministration:         grantAdministrationService,
		CategoryAdministration:      categoryAdministrationService,
		AnnouncementAdministration:  unavailableAnnouncementAdministrationService{},
		Wiki:                        unavailableWikiService{},
		SiteDisplaySettings:         siteDisplaySettingsService,
		UserAdministration:          userAdministrationService,
		Notifications:               unavailableNotificationService{},
		TrafficOverview:             unavailableTrafficOverviewService{},
		EconomyOverview:             unavailableEconomyOverviewService{},
		Attendance:                  unavailableAttendanceService{},
		MemberGifts:                 unavailableMemberGiftService{},
		ContentTips:                 unavailableContentTipService{},
		Workgroups:                  unavailableWorkgroupService{},
		SeedingRewardAdministration: unavailableSeedingRewardAdministrationService{},
		HNRPolicyAdministration:     unavailableHNRPolicyAdministrationService{},
		Operations:                  unavailableOperationsService{},
		TorrentBookmarks:            unavailableTorrentBookmarkService{},
		Comments:                    unavailableCommentService{},
		SocialPosts:                 unavailableSocialPostService{},
		CommentModeration:           unavailableCommentModerationService{},
		TorrentRead:                 unavailableTorrentReadService{},
		TorrentUpload:               unavailableTorrentUploadService{},
		TorrentDownload:             unavailableTorrentDownloadService{},
		TorrentReview:               unavailableTorrentReviewService{},
		TorrentResubmission:         unavailableTorrentResubmissionService{},
		TorrentMaintenance:          unavailableTorrentMaintenanceService{},
		PromotionAdministration:     unavailablePromotionAdministrationService{},
		RSS:                         unavailableRSSService{},
		RatioWatchAdministration:    unavailableRatioWatchAdministrationService{},
		NewcomerAdministration:      unavailableNewcomerAdministrationService{},
		TorrentUploadMaxBytes:       4 << 20,
		SessionCookie:               httpapi.SessionCookieConfig{Name: "peergo_session", Path: "/"},
		StaffSessionCookie:          httpapi.SessionCookieConfig{Name: "peergo_staff_session", Path: "/api/v1/admin"},
		AllowedOrigins:              []string{"http://peergo.test"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func testHandlerWithRegistration(t *testing.T, registrationService httpapi.RegistrationService) http.Handler {
	return testHandlerWithRegistrationAndHumanVerification(
		t,
		registrationService,
		identity.NewUnavailableHumanVerificationVerifier(),
	)
}

func testHandlerWithRegistrationAndHumanVerification(t *testing.T, registrationService httpapi.RegistrationService, humanVerification identity.HumanVerificationVerifier) http.Handler {
	t.Helper()
	now := time.Date(2026, time.August, 6, 9, 0, 0, 0, time.UTC)
	handler, err := New(Dependencies{
		Catalog:                     catalog.NewService(catalog.NewDemoRepository(now), func() time.Time { return now }),
		Identity:                    unavailableIdentityService{},
		Registration:                registrationService,
		HumanVerification:           humanVerification,
		Invitations:                 unavailableInvitationService{},
		EmailVerification:           unavailableEmailVerificationService{},
		PasswordRecovery:            unavailablePasswordRecoveryService{},
		SessionSecurity:             unavailableSessionSecurityService{},
		TwoFactor:                   unavailableTwoFactorService{},
		StaffIdentity:               unavailableStaffIdentityService{},
		StaffEnrollment:             unavailableStaffEnrollmentService{},
		Authorization:               unavailableAuthorizationService{},
		GrantAdministration:         unavailableGrantAdministrationService{},
		CategoryAdministration:      unavailableCategoryAdministrationService{},
		AnnouncementAdministration:  unavailableAnnouncementAdministrationService{},
		Wiki:                        unavailableWikiService{},
		SiteDisplaySettings:         unavailableSiteDisplaySettingsService{},
		UserAdministration:          unavailableUserAdministrationService{},
		Notifications:               unavailableNotificationService{},
		TrafficOverview:             unavailableTrafficOverviewService{},
		EconomyOverview:             unavailableEconomyOverviewService{},
		Attendance:                  unavailableAttendanceService{},
		MemberGifts:                 unavailableMemberGiftService{},
		ContentTips:                 unavailableContentTipService{},
		Workgroups:                  unavailableWorkgroupService{},
		SeedingRewardAdministration: unavailableSeedingRewardAdministrationService{},
		HNRPolicyAdministration:     unavailableHNRPolicyAdministrationService{},
		Operations:                  unavailableOperationsService{},
		TorrentBookmarks:            unavailableTorrentBookmarkService{},
		Comments:                    unavailableCommentService{},
		SocialPosts:                 unavailableSocialPostService{},
		CommentModeration:           unavailableCommentModerationService{},
		TorrentRead:                 unavailableTorrentReadService{},
		TorrentUpload:               unavailableTorrentUploadService{},
		TorrentDownload:             unavailableTorrentDownloadService{},
		TorrentReview:               unavailableTorrentReviewService{},
		TorrentResubmission:         unavailableTorrentResubmissionService{},
		TorrentMaintenance:          unavailableTorrentMaintenanceService{},
		PromotionAdministration:     unavailablePromotionAdministrationService{},
		RSS:                         unavailableRSSService{},
		RatioWatchAdministration:    unavailableRatioWatchAdministrationService{},
		NewcomerAdministration:      unavailableNewcomerAdministrationService{},
		TorrentUploadMaxBytes:       4 << 20,
		SessionCookie:               httpapi.SessionCookieConfig{Name: "peergo_session", Path: "/"},
		StaffSessionCookie:          httpapi.SessionCookieConfig{Name: "peergo_staff_session", Path: "/api/v1/admin"},
		AllowedOrigins:              []string{"http://peergo.test"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func testHandlerWithPasswordRecovery(t *testing.T, passwordRecovery httpapi.PasswordRecoveryService) http.Handler {
	t.Helper()
	now := time.Date(2026, time.August, 6, 22, 0, 0, 0, time.UTC)
	handler, err := New(Dependencies{
		Catalog:                     catalog.NewService(catalog.NewDemoRepository(now), func() time.Time { return now }),
		Identity:                    unavailableIdentityService{},
		Registration:                unavailableRegistrationService{},
		Invitations:                 unavailableInvitationService{},
		EmailVerification:           unavailableEmailVerificationService{},
		PasswordRecovery:            passwordRecovery,
		SessionSecurity:             unavailableSessionSecurityService{},
		TwoFactor:                   unavailableTwoFactorService{},
		StaffIdentity:               unavailableStaffIdentityService{},
		StaffEnrollment:             unavailableStaffEnrollmentService{},
		Authorization:               unavailableAuthorizationService{},
		GrantAdministration:         unavailableGrantAdministrationService{},
		CategoryAdministration:      unavailableCategoryAdministrationService{},
		AnnouncementAdministration:  unavailableAnnouncementAdministrationService{},
		Wiki:                        unavailableWikiService{},
		SiteDisplaySettings:         unavailableSiteDisplaySettingsService{},
		UserAdministration:          unavailableUserAdministrationService{},
		Notifications:               unavailableNotificationService{},
		TrafficOverview:             unavailableTrafficOverviewService{},
		EconomyOverview:             unavailableEconomyOverviewService{},
		Attendance:                  unavailableAttendanceService{},
		MemberGifts:                 unavailableMemberGiftService{},
		ContentTips:                 unavailableContentTipService{},
		Workgroups:                  unavailableWorkgroupService{},
		SeedingRewardAdministration: unavailableSeedingRewardAdministrationService{},
		HNRPolicyAdministration:     unavailableHNRPolicyAdministrationService{},
		Operations:                  unavailableOperationsService{},
		TorrentBookmarks:            unavailableTorrentBookmarkService{},
		Comments:                    unavailableCommentService{},
		SocialPosts:                 unavailableSocialPostService{},
		CommentModeration:           unavailableCommentModerationService{},
		TorrentRead:                 unavailableTorrentReadService{},
		TorrentUpload:               unavailableTorrentUploadService{},
		TorrentDownload:             unavailableTorrentDownloadService{},
		TorrentReview:               unavailableTorrentReviewService{},
		TorrentResubmission:         unavailableTorrentResubmissionService{},
		TorrentMaintenance:          unavailableTorrentMaintenanceService{},
		PromotionAdministration:     unavailablePromotionAdministrationService{},
		RSS:                         unavailableRSSService{},
		RatioWatchAdministration:    unavailableRatioWatchAdministrationService{},
		NewcomerAdministration:      unavailableNewcomerAdministrationService{},
		TorrentUploadMaxBytes:       4 << 20,
		SessionCookie:               httpapi.SessionCookieConfig{Name: "peergo_session", Path: "/"},
		StaffSessionCookie:          httpapi.SessionCookieConfig{Name: "peergo_staff_session", Path: "/api/v1/admin"},
		AllowedOrigins:              []string{"http://peergo.test"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}
