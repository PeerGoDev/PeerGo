package identity

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	defaultManagedUserPageSize          = 20
	maxManagedUserPageSize              = 50
	maxManagedUserPage                  = 100_000
	maxManagedUserQueryRunes            = 64
	minAccountRestrictionReasonRunes    = 10
	maxAccountRestrictionReasonRunes    = 240
	maxRestrictionRevocationReasonRunes = 500
	maxAccountRestrictionDurationHours  = 7 * 24
	maxVIPDurationDays                  = 10 * 365
	managedUserNetworkRetentionDays     = 180
	managedUserNetworkMaximumItems      = 20
)

var managedUserDecimalAmountPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

type UserAdministrationRepository interface {
	ListManagedUsers(context.Context, ManagedUserListQuery) (ManagedUserPage, error)
	GetManagedUser(context.Context, uuid.UUID, time.Time) (ManagedUserDetail, error)
}

type AccountRestrictionCommandRepository interface {
	CreateAccountRestriction(context.Context, CreateAccountRestrictionCommand) (ManagedUserDetail, error)
	RevokeAccountRestriction(context.Context, RevokeAccountRestrictionCommand) (ManagedUserDetail, error)
	CreateManualDownloadRestriction(context.Context, ManualDownloadRestrictionCommand) (ManagedUserDetail, error)
	UpdateManualDownloadRestriction(context.Context, ManualDownloadRestrictionCommand) (ManagedUserDetail, error)
	RevokeManualDownloadRestriction(context.Context, ManualDownloadRestrictionCommand) (ManagedUserDetail, error)
	ChangeVIP(context.Context, ChangeVIPCommand) (ManagedUserDetail, error)
	ManagedUserReactivationPreflight(context.Context, uuid.UUID) (ManagedUserReactivationPreflight, error)
	ReactivateManagedUser(context.Context, ReactivateManagedUserCommand) (ManagedUserDetail, error)
}

type ManagedUserContactDirectory interface {
	Emails(context.Context, []uuid.UUID) (map[uuid.UUID]string, error)
}

type ManagedUserDataAdministrationRepository interface {
	AdjustManagedUser(context.Context, ManagedUserAdjustmentCommand) error
	ListManagedUserNetworkHistory(context.Context, uuid.UUID, time.Time, int) ([]ManagedUserNetworkObservation, error)
}

type ManagedUserCredentialLifecycle interface {
	EnableAfterAccountAppeal(context.Context, uuid.UUID) error
}

type UserAdministrationService struct {
	repository            UserAdministrationRepository
	restrictionRepository AccountRestrictionCommandRepository
	authorizer            authz.Authorizer
	contactDirectory      ManagedUserContactDirectory
	credentialLifecycle   ManagedUserCredentialLifecycle
	dataRepository        ManagedUserDataAdministrationRepository
	now                   func() time.Time
}

func NewUserAdministrationServiceWithData(
	repository UserAdministrationRepository,
	restrictionRepository AccountRestrictionCommandRepository,
	dataRepository ManagedUserDataAdministrationRepository,
	authorizer authz.Authorizer,
	now func() time.Time,
	contactDirectories ...ManagedUserContactDirectory,
) (*UserAdministrationService, error) {
	if dataRepository == nil {
		return nil, errors.New("managed user data administration repository is required")
	}
	service, err := NewUserAdministrationService(
		repository, restrictionRepository, authorizer, now, contactDirectories...,
	)
	if err != nil {
		return nil, err
	}
	service.dataRepository = dataRepository
	return service, nil
}

func NewUserAdministrationService(repository UserAdministrationRepository, restrictionRepository AccountRestrictionCommandRepository, authorizer authz.Authorizer, now func() time.Time, contactDirectories ...ManagedUserContactDirectory) (*UserAdministrationService, error) {
	if repository == nil || restrictionRepository == nil || authorizer == nil {
		return nil, errors.New("user administration dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	if len(contactDirectories) > 1 {
		return nil, errors.New("at most one managed user contact directory is allowed")
	}
	var contactDirectory ManagedUserContactDirectory
	if len(contactDirectories) == 1 {
		if contactDirectories[0] == nil {
			return nil, errors.New("managed user contact directory is invalid")
		}
		contactDirectory = contactDirectories[0]
	}
	var credentialLifecycle ManagedUserCredentialLifecycle
	if contactDirectory != nil {
		credentialLifecycle, _ = contactDirectory.(ManagedUserCredentialLifecycle)
	}
	return &UserAdministrationService{
		repository: repository, restrictionRepository: restrictionRepository,
		authorizer: authorizer, contactDirectory: contactDirectory,
		credentialLifecycle: credentialLifecycle, now: now,
	}, nil
}

func (service *UserAdministrationService) Reactivate(ctx context.Context, actor authz.StaffActor, input ReactivateManagedUserInput) (ManagedUserDetail, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" {
		input.Reason = "管理员手动解除账户封禁"
	}
	if input.ReactivationID == uuid.Nil || input.UserID == uuid.Nil || input.ExpectedUserVersion < 1 || !validAdministrationReason(input.Reason) {
		return ManagedUserDetail{}, ErrUserAdministrationInput
	}
	if input.UserID == actor.Subject.ID {
		return ManagedUserDetail{}, ErrAccountRestrictionSelfTarget
	}
	if service.credentialLifecycle == nil {
		return ManagedUserDetail{}, ErrManagedUserCredentialUnavailable
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionUserAccountRestrictionRevoke, authz.SiteScope(), now, "identity-account-reactivation")
	if err != nil {
		return ManagedUserDetail{}, err
	}
	preflight, err := service.restrictionRepository.ManagedUserReactivationPreflight(ctx, input.UserID)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	if preflight.Version != input.ExpectedUserVersion {
		// A completed idempotent replay has already advanced the version once.
		if preflight.Status != AccountStatusActive || preflight.Version != input.ExpectedUserVersion+1 {
			return ManagedUserDetail{}, ErrManagedUserVersionConflict
		}
		result, err := service.restrictionRepository.ReactivateManagedUser(ctx, ReactivateManagedUserCommand{
			ReactivateManagedUserInput: input, ActorID: actor.Subject.ID, OccurredAt: now, Authorization: decision,
		})
		if err != nil {
			return ManagedUserDetail{}, fmt.Errorf("replay managed user reactivation: %w", err)
		}
		if err := service.enrichManagedUser(ctx, &result.ManagedUserSummary); err != nil {
			return ManagedUserDetail{}, err
		}
		return result, nil
	}
	if preflight.Status != AccountStatusDisabled {
		return ManagedUserDetail{}, ErrManagedUserNotDisabled
	}
	// Vault first and Core second is fail-closed: a Core race still leaves the
	// disabled account projection blocking login and the command can be retried.
	if err := service.credentialLifecycle.EnableAfterAccountAppeal(ctx, preflight.CredentialRef); err != nil {
		return ManagedUserDetail{}, fmt.Errorf("%w: %v", ErrManagedUserCredentialUnavailable, err)
	}
	result, err := service.restrictionRepository.ReactivateManagedUser(ctx, ReactivateManagedUserCommand{
		ReactivateManagedUserInput: input, ActorID: actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("reactivate managed user: %w", err)
	}
	if err := service.enrichManagedUser(ctx, &result.ManagedUserSummary); err != nil {
		return ManagedUserDetail{}, err
	}
	return result, nil
}

func (service *UserAdministrationService) List(ctx context.Context, actor authz.StaffActor, input ListManagedUsersInput) (ManagedUserPage, error) {
	normalized, err := normalizeManagedUserListInput(input)
	if err != nil {
		return ManagedUserPage{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionUserAccountRead, authz.SiteScope(), now, "identity-user-administration"); err != nil {
		return ManagedUserPage{}, err
	}
	result, err := service.repository.ListManagedUsers(ctx, ManagedUserListQuery{
		Query: normalized.Query, Filter: normalized.Filter,
		Page: normalized.Page, PageSize: normalized.PageSize,
		Offset: (normalized.Page - 1) * normalized.PageSize, AsOf: now,
	})
	if err != nil {
		return ManagedUserPage{}, fmt.Errorf("list managed users: %w", err)
	}
	if err := service.enrichManagedUsers(ctx, result.Items); err != nil {
		return ManagedUserPage{}, err
	}
	return result, nil
}

func (service *UserAdministrationService) Get(ctx context.Context, actor authz.StaffActor, userID uuid.UUID) (ManagedUserDetail, error) {
	if userID == uuid.Nil {
		return ManagedUserDetail{}, ErrUserAdministrationInput
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionUserAccountRead, authz.SiteScope(), now, "identity-user-administration"); err != nil {
		return ManagedUserDetail{}, err
	}
	result, err := service.repository.GetManagedUser(ctx, userID, now)
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("get managed user: %w", err)
	}
	if err := service.enrichManagedUser(ctx, &result.ManagedUserSummary); err != nil {
		return ManagedUserDetail{}, err
	}
	return result, nil
}

func (service *UserAdministrationService) Adjust(ctx context.Context, actor authz.StaffActor, input ManagedUserAdjustmentInput) (ManagedUserDetail, error) {
	normalized, err := normalizeManagedUserAdjustmentInput(input)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	if service.dataRepository == nil {
		return ManagedUserDetail{}, ErrManagedUserDataUnavailable
	}
	if normalized.UserID == actor.Subject.ID {
		return ManagedUserDetail{}, ErrAccountRestrictionSelfTarget
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeStaffAction(
		ctx, service.authorizer, actor, authz.ActionUserAccountAdjust,
		authz.SiteScope(), now, "identity-user-data-administration",
	)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	if err := service.dataRepository.AdjustManagedUser(ctx, ManagedUserAdjustmentCommand{
		AdjustmentID:        normalized.AdjustmentID,
		UserID:              normalized.UserID,
		ActorID:             actor.Subject.ID,
		Field:               normalized.Field,
		Delta:               normalized.Amount,
		Reason:              normalized.Reason,
		ExpectedUserVersion: normalized.ExpectedUserVersion,
		OccurredAt:          now,
		Authorization:       decision,
	}); err != nil {
		return ManagedUserDetail{}, fmt.Errorf("adjust managed user data: %w", err)
	}
	result, err := service.repository.GetManagedUser(ctx, normalized.UserID, now)
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("read adjusted managed user: %w", err)
	}
	if err := service.enrichManagedUser(ctx, &result.ManagedUserSummary); err != nil {
		return ManagedUserDetail{}, err
	}
	return result, nil
}

func (service *UserAdministrationService) NetworkHistory(ctx context.Context, actor authz.StaffActor, userID uuid.UUID) (ManagedUserNetworkHistory, error) {
	if userID == uuid.Nil {
		return ManagedUserNetworkHistory{}, ErrUserAdministrationInput
	}
	if service.dataRepository == nil {
		return ManagedUserNetworkHistory{}, ErrManagedUserDataUnavailable
	}
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(
		ctx, service.authorizer, actor, authz.ActionUserNetworkRead,
		authz.SiteScope(), now, "identity-user-network-history",
	); err != nil {
		return ManagedUserNetworkHistory{}, err
	}
	items, err := service.dataRepository.ListManagedUserNetworkHistory(
		ctx, userID, now.AddDate(0, 0, -managedUserNetworkRetentionDays),
		managedUserNetworkMaximumItems,
	)
	if err != nil {
		return ManagedUserNetworkHistory{}, fmt.Errorf("list managed user network history: %w", err)
	}
	return ManagedUserNetworkHistory{
		Items:         items,
		RetentionDays: managedUserNetworkRetentionDays,
		MaximumItems:  managedUserNetworkMaximumItems,
	}, nil
}

func (service *UserAdministrationService) CreateRestriction(ctx context.Context, actor authz.StaffActor, input CreateAccountRestrictionInput) (ManagedUserDetail, error) {
	normalized, err := normalizeCreateAccountRestrictionInput(input)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	if actor.Subject.ID == normalized.UserID {
		return ManagedUserDetail{}, ErrAccountRestrictionSelfTarget
	}
	now := service.now().UTC()
	decision, err := authz.AuthorizeStaffAction(
		ctx, service.authorizer, actor, authz.ActionUserAccountRestrict,
		authz.SiteScope(), now, "identity-account-restriction-administration",
	)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	result, err := service.restrictionRepository.CreateAccountRestriction(ctx, CreateAccountRestrictionCommand{
		CreateAccountRestrictionInput: normalized,
		ActorID:                       actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("create account restriction: %w", err)
	}
	if err := service.enrichManagedUser(ctx, &result.ManagedUserSummary); err != nil {
		return ManagedUserDetail{}, err
	}
	return result, nil
}

func (service *UserAdministrationService) RevokeRestriction(ctx context.Context, actor authz.StaffActor, input RevokeAccountRestrictionInput) (ManagedUserDetail, error) {
	normalized, err := normalizeRevokeAccountRestrictionInput(input)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	if actor.Subject.ID == normalized.UserID {
		return ManagedUserDetail{}, ErrAccountRestrictionSelfTarget
	}
	now := service.now().UTC()
	decision, err := authz.AuthorizeStaffAction(
		ctx, service.authorizer, actor, authz.ActionUserAccountRestrictionRevoke,
		authz.SiteScope(), now, "identity-account-restriction-administration",
	)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	result, err := service.restrictionRepository.RevokeAccountRestriction(ctx, RevokeAccountRestrictionCommand{
		RevokeAccountRestrictionInput: normalized,
		ActorID:                       actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("revoke account restriction: %w", err)
	}
	if err := service.enrichManagedUser(ctx, &result.ManagedUserSummary); err != nil {
		return ManagedUserDetail{}, err
	}
	return result, nil
}

func (service *UserAdministrationService) CreateManualDownloadRestriction(ctx context.Context, actor authz.StaffActor, input CreateManualDownloadRestrictionInput) (ManagedUserDetail, error) {
	normalized, err := normalizeManualDownloadRestrictionInput(input.UserID, input.ReasonCode, input.Reason, input.ExpectedUserVersion, input.ExpectedStateVersion)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	return service.changeManualDownloadRestriction(ctx, actor, normalized, authz.ActionUserDownloadRestrictionRestrict, service.restrictionRepository.CreateManualDownloadRestriction)
}

func (service *UserAdministrationService) UpdateManualDownloadRestriction(ctx context.Context, actor authz.StaffActor, input UpdateManualDownloadRestrictionInput) (ManagedUserDetail, error) {
	normalized, err := normalizeManualDownloadRestrictionInput(input.UserID, input.ReasonCode, input.Reason, input.ExpectedUserVersion, input.ExpectedStateVersion)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	return service.changeManualDownloadRestriction(ctx, actor, normalized, authz.ActionUserDownloadRestrictionRestrict, service.restrictionRepository.UpdateManualDownloadRestriction)
}

func (service *UserAdministrationService) RevokeManualDownloadRestriction(ctx context.Context, actor authz.StaffActor, input RevokeManualDownloadRestrictionInput) (ManagedUserDetail, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	validReasonCode := input.ReasonCode == ManualDownloadRestrictionRevocationReviewCompleted ||
		input.ReasonCode == ManualDownloadRestrictionRevocationNoLongerRequired
	if input.UserID == uuid.Nil || input.ExpectedUserVersion < 1 || input.ExpectedStateVersion < 1 ||
		!validReasonCode || !validAdministrationReason(input.Reason) {
		return ManagedUserDetail{}, ErrUserAdministrationInput
	}
	command := ManualDownloadRestrictionCommand{
		UserID: input.UserID, ReasonCode: string(input.ReasonCode), Reason: input.Reason,
		ExpectedUserVersion: input.ExpectedUserVersion, ExpectedStateVersion: input.ExpectedStateVersion,
	}
	return service.changeManualDownloadRestriction(ctx, actor, command, authz.ActionUserDownloadRestrictionRevoke, service.restrictionRepository.RevokeManualDownloadRestriction)
}

func (service *UserAdministrationService) ChangeVIP(ctx context.Context, actor authz.StaffActor, input ChangeVIPInput) (ManagedUserDetail, error) {
	normalized, err := normalizeChangeVIPInput(input)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	if actor.Subject.ID == normalized.UserID {
		return ManagedUserDetail{}, ErrAccountRestrictionSelfTarget
	}
	now := service.now().UTC()
	decision, err := authz.AuthorizeStaffAction(
		ctx, service.authorizer, actor, authz.ActionUserVIPManage,
		authz.SiteScope(), now, "identity-vip-administration",
	)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	result, err := service.restrictionRepository.ChangeVIP(ctx, ChangeVIPCommand{
		ChangeVIPInput: normalized, ActorID: actor.Subject.ID,
		OccurredAt: now, Authorization: decision,
	})
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("change VIP: %w", err)
	}
	if err := service.enrichManagedUser(ctx, &result.ManagedUserSummary); err != nil {
		return ManagedUserDetail{}, err
	}
	return result, nil
}

type manualDownloadRestrictionMutationFunc func(context.Context, ManualDownloadRestrictionCommand) (ManagedUserDetail, error)

func (service *UserAdministrationService) changeManualDownloadRestriction(ctx context.Context, actor authz.StaffActor, command ManualDownloadRestrictionCommand, action authz.Action, mutate manualDownloadRestrictionMutationFunc) (ManagedUserDetail, error) {
	if actor.Subject.ID == command.UserID {
		return ManagedUserDetail{}, ErrAccountRestrictionSelfTarget
	}
	now := service.now().UTC()
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, action, authz.SiteScope(), now, "identity-manual-download-restriction-administration")
	if err != nil {
		return ManagedUserDetail{}, err
	}
	command.ActorID = actor.Subject.ID
	command.OccurredAt = now
	command.Authorization = decision
	result, err := mutate(ctx, command)
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("change manual download restriction: %w", err)
	}
	if err := service.enrichManagedUser(ctx, &result.ManagedUserSummary); err != nil {
		return ManagedUserDetail{}, err
	}
	return result, nil
}

func (service *UserAdministrationService) enrichManagedUsers(ctx context.Context, users []ManagedUserSummary) error {
	if service.contactDirectory == nil || len(users) == 0 {
		return nil
	}
	references := make([]uuid.UUID, 0, len(users))
	for _, user := range users {
		if user.credentialRef == uuid.Nil {
			return errors.New("managed user is missing its contact reference")
		}
		references = append(references, user.credentialRef)
	}
	emails, err := service.contactDirectory.Emails(ctx, references)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrManagedUserContactUnavailable, err)
	}
	for index := range users {
		users[index].Email = emails[users[index].credentialRef]
	}
	return nil
}

func (service *UserAdministrationService) enrichManagedUser(ctx context.Context, user *ManagedUserSummary) error {
	if service.contactDirectory == nil {
		return nil
	}
	if user == nil || user.credentialRef == uuid.Nil {
		return errors.New("managed user is missing its contact reference")
	}
	emails, err := service.contactDirectory.Emails(ctx, []uuid.UUID{user.credentialRef})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrManagedUserContactUnavailable, err)
	}
	user.Email = emails[user.credentialRef]
	return nil
}

func normalizeManagedUserListInput(input ListManagedUsersInput) (ListManagedUsersInput, error) {
	input.Query = strings.TrimSpace(input.Query)
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PageSize == 0 {
		input.PageSize = defaultManagedUserPageSize
	}
	validFilter := input.Filter == "" || input.Filter == ManagedUserFilterActive ||
		input.Filter == ManagedUserFilterBanned || input.Filter == ManagedUserFilterPending ||
		input.Filter == ManagedUserFilterVIP || input.Filter == ManagedUserFilterDownloadRestricted ||
		input.Filter == ManagedUserFilterUnverified
	if !utf8.ValidString(input.Query) || utf8.RuneCountInString(input.Query) > maxManagedUserQueryRunes ||
		!validFilter || input.Page < 1 || input.Page > maxManagedUserPage ||
		input.PageSize < 1 || input.PageSize > maxManagedUserPageSize {
		return ListManagedUsersInput{}, ErrUserAdministrationInput
	}
	return input, nil
}

func normalizeManagedUserAdjustmentInput(input ManagedUserAdjustmentInput) (ManagedUserAdjustmentInput, error) {
	if input.AdjustmentID == uuid.Nil || input.UserID == uuid.Nil || input.ExpectedUserVersion < 1 ||
		(input.Operation != ManagedUserAdjustmentIncrease && input.Operation != ManagedUserAdjustmentDecrease) {
		return ManagedUserAdjustmentInput{}, ErrUserAdministrationInput
	}
	amount, err := canonicalManagedUserAdjustmentAmount(input.Field, input.Amount)
	if err != nil {
		return ManagedUserAdjustmentInput{}, err
	}
	if input.Operation == ManagedUserAdjustmentDecrease {
		amount = "-" + amount
	}
	input.Amount = amount
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" {
		input.Reason = "管理员调整" + managedUserAdjustmentFieldLabel(input.Field)
	}
	reasonRunes := utf8.RuneCountInString(input.Reason)
	if !utf8.ValidString(input.Reason) || reasonRunes < 1 || reasonRunes > maxRestrictionRevocationReasonRunes {
		return ManagedUserAdjustmentInput{}, ErrUserAdministrationInput
	}
	return input, nil
}

func canonicalManagedUserAdjustmentAmount(field ManagedUserAdjustmentField, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if !managedUserDecimalAmountPattern.MatchString(value) {
		return "", ErrUserAdministrationInput
	}
	parts := strings.SplitN(value, ".", 2)
	integer := strings.TrimLeft(parts[0], "0")
	if integer == "" {
		integer = "0"
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = strings.TrimRight(parts[1], "0")
	}
	canonical := integer
	if fraction != "" {
		canonical += "." + fraction
	}
	if canonical == "0" {
		return "", ErrUserAdministrationInput
	}

	switch field {
	case ManagedUserAdjustmentUploadedBytes, ManagedUserAdjustmentDownloadedBytes, ManagedUserAdjustmentMagicBalance:
		if fraction != "" {
			return "", ErrUserAdministrationInput
		}
		if _, err := strconv.ParseInt(integer, 10, 64); err != nil {
			return "", ErrUserAdministrationInput
		}
	case ManagedUserAdjustmentRemainingInvites:
		if fraction != "" {
			return "", ErrUserAdministrationInput
		}
		amount, err := strconv.ParseInt(integer, 10, 32)
		if err != nil || amount > 1_000_000 {
			return "", ErrUserAdministrationInput
		}
	case ManagedUserAdjustmentExperience:
		if len(integer) > 18 || len(fraction) > 20 {
			return "", ErrUserAdministrationInput
		}
	case ManagedUserAdjustmentDonationAmount:
		if len(integer) > 10 || len(fraction) > 2 {
			return "", ErrUserAdministrationInput
		}
	default:
		return "", ErrUserAdministrationInput
	}
	return canonical, nil
}

func managedUserAdjustmentFieldLabel(field ManagedUserAdjustmentField) string {
	switch field {
	case ManagedUserAdjustmentUploadedBytes:
		return "用户上传量"
	case ManagedUserAdjustmentDownloadedBytes:
		return "用户下载量"
	case ManagedUserAdjustmentMagicBalance:
		return "用户魔力值"
	case ManagedUserAdjustmentExperience:
		return "用户经验值"
	case ManagedUserAdjustmentRemainingInvites:
		return "用户可用邀请"
	case ManagedUserAdjustmentDonationAmount:
		return "用户捐赠金额"
	default:
		return "用户数据"
	}
}

func normalizeCreateAccountRestrictionInput(input CreateAccountRestrictionInput) (CreateAccountRestrictionInput, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	reasonRunes := utf8.RuneCountInString(input.Reason)
	validReasonCode := input.ReasonCode == AccountRestrictionReasonManualReview ||
		input.ReasonCode == AccountRestrictionReasonSecurityIncident
	if input.UserID == uuid.Nil || input.ExpectedUserVersion < 1 || !validReasonCode ||
		input.DurationHours < 1 || input.DurationHours > maxAccountRestrictionDurationHours ||
		!utf8.ValidString(input.Reason) || reasonRunes < minAccountRestrictionReasonRunes ||
		reasonRunes > maxAccountRestrictionReasonRunes {
		return CreateAccountRestrictionInput{}, ErrUserAdministrationInput
	}
	return input, nil
}

func normalizeRevokeAccountRestrictionInput(input RevokeAccountRestrictionInput) (RevokeAccountRestrictionInput, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	reasonRunes := utf8.RuneCountInString(input.Reason)
	validReasonCode := input.ReasonCode == AccountRestrictionRevocationReviewCompleted ||
		input.ReasonCode == AccountRestrictionRevocationNoLongerRequired
	if input.UserID == uuid.Nil || input.RestrictionID == uuid.Nil ||
		input.ExpectedUserVersion < 1 || input.ExpectedRestrictionVersion < 1 ||
		!validReasonCode || !utf8.ValidString(input.Reason) ||
		reasonRunes < minAccountRestrictionReasonRunes || reasonRunes > maxRestrictionRevocationReasonRunes {
		return RevokeAccountRestrictionInput{}, ErrUserAdministrationInput
	}
	return input, nil
}

func normalizeManualDownloadRestrictionInput(userID uuid.UUID, reasonCode ManualDownloadRestrictionReasonCode, reason string, expectedUserVersion, expectedStateVersion int64) (ManualDownloadRestrictionCommand, error) {
	reason = strings.TrimSpace(reason)
	validReasonCode := reasonCode == ManualDownloadRestrictionReasonManualReview ||
		reasonCode == ManualDownloadRestrictionReasonPolicyViolation ||
		reasonCode == ManualDownloadRestrictionReasonAbusePrevention
	if userID == uuid.Nil || expectedUserVersion < 1 || expectedStateVersion < 0 ||
		!validReasonCode || !validAdministrationReason(reason) {
		return ManualDownloadRestrictionCommand{}, ErrUserAdministrationInput
	}
	return ManualDownloadRestrictionCommand{
		UserID: userID, ReasonCode: string(reasonCode), Reason: reason,
		ExpectedUserVersion: expectedUserVersion, ExpectedStateVersion: expectedStateVersion,
	}, nil
}

func validAdministrationReason(reason string) bool {
	reasonRunes := utf8.RuneCountInString(reason)
	return utf8.ValidString(reason) && reasonRunes >= minAccountRestrictionReasonRunes &&
		reasonRunes <= maxRestrictionRevocationReasonRunes
}

func normalizeChangeVIPInput(input ChangeVIPInput) (ChangeVIPInput, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	validDuration := input.DurationDays == nil ||
		(*input.DurationDays >= 1 && *input.DurationDays <= maxVIPDurationDays)
	if input.UserID == uuid.Nil || input.ExpectedUserVersion < 1 ||
		input.ExpectedStateVersion < 0 || !validDuration ||
		(!input.Enabled && input.DurationDays != nil) ||
		!validAdministrationReason(input.Reason) {
		return ChangeVIPInput{}, ErrUserAdministrationInput
	}
	return input, nil
}
