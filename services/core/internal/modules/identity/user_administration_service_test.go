package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type userAdministrationRepositoryStub struct {
	listResult        ManagedUserPage
	detail            ManagedUserDetail
	listQuery         ManagedUserListQuery
	getUserID         uuid.UUID
	getAsOf           time.Time
	createCommand     CreateAccountRestrictionCommand
	revokeCommand     RevokeAccountRestrictionCommand
	manualCommand     ManualDownloadRestrictionCommand
	manualMutation    string
	vipCommand        ChangeVIPCommand
	preflight         ManagedUserReactivationPreflight
	reactivateCommand ReactivateManagedUserCommand
	commandResult     ManagedUserDetail
	commandErr        error
}

func (stub *userAdministrationRepositoryStub) ManagedUserReactivationPreflight(context.Context, uuid.UUID) (ManagedUserReactivationPreflight, error) {
	return stub.preflight, stub.commandErr
}

func (stub *userAdministrationRepositoryStub) ReactivateManagedUser(_ context.Context, command ReactivateManagedUserCommand) (ManagedUserDetail, error) {
	stub.reactivateCommand = command
	return stub.commandResult, stub.commandErr
}

type managedUserLifecycleStub struct {
	enabled uuid.UUID
}

type managedUserDataRepositoryStub struct {
	adjustCommand ManagedUserAdjustmentCommand
	networkUserID uuid.UUID
	networkCutoff time.Time
	networkLimit  int
	networkResult []ManagedUserNetworkObservation
	err           error
}

func (stub *managedUserDataRepositoryStub) AdjustManagedUser(_ context.Context, command ManagedUserAdjustmentCommand) error {
	stub.adjustCommand = command
	return stub.err
}

func (stub *managedUserDataRepositoryStub) ListManagedUserNetworkHistory(_ context.Context, userID uuid.UUID, cutoff time.Time, limit int) ([]ManagedUserNetworkObservation, error) {
	stub.networkUserID = userID
	stub.networkCutoff = cutoff
	stub.networkLimit = limit
	return stub.networkResult, stub.err
}

func (stub *managedUserLifecycleStub) EnableAfterAccountAppeal(_ context.Context, credentialRef uuid.UUID) error {
	stub.enabled = credentialRef
	return nil
}

func (stub *managedUserLifecycleStub) Emails(_ context.Context, refs []uuid.UUID) (map[uuid.UUID]string, error) {
	result := make(map[uuid.UUID]string, len(refs))
	for _, ref := range refs {
		result[ref] = "member@example.com"
	}
	return result, nil
}

func (stub *userAdministrationRepositoryStub) ChangeVIP(_ context.Context, command ChangeVIPCommand) (ManagedUserDetail, error) {
	stub.vipCommand = command
	return stub.commandResult, stub.commandErr
}

func (stub *userAdministrationRepositoryStub) ListManagedUsers(_ context.Context, query ManagedUserListQuery) (ManagedUserPage, error) {
	stub.listQuery = query
	return stub.listResult, nil
}

func (stub *userAdministrationRepositoryStub) GetManagedUser(_ context.Context, userID uuid.UUID, asOf time.Time) (ManagedUserDetail, error) {
	stub.getUserID = userID
	stub.getAsOf = asOf
	return stub.detail, nil
}

func (stub *userAdministrationRepositoryStub) CreateAccountRestriction(_ context.Context, command CreateAccountRestrictionCommand) (ManagedUserDetail, error) {
	stub.createCommand = command
	return stub.commandResult, stub.commandErr
}

func (stub *userAdministrationRepositoryStub) RevokeAccountRestriction(_ context.Context, command RevokeAccountRestrictionCommand) (ManagedUserDetail, error) {
	stub.revokeCommand = command
	return stub.commandResult, stub.commandErr
}

func (stub *userAdministrationRepositoryStub) CreateManualDownloadRestriction(_ context.Context, command ManualDownloadRestrictionCommand) (ManagedUserDetail, error) {
	stub.manualCommand = command
	stub.manualMutation = "create"
	return stub.commandResult, stub.commandErr
}

func (stub *userAdministrationRepositoryStub) UpdateManualDownloadRestriction(_ context.Context, command ManualDownloadRestrictionCommand) (ManagedUserDetail, error) {
	stub.manualCommand = command
	stub.manualMutation = "update"
	return stub.commandResult, stub.commandErr
}

func (stub *userAdministrationRepositoryStub) RevokeManualDownloadRestriction(_ context.Context, command ManualDownloadRestrictionCommand) (ManagedUserDetail, error) {
	stub.manualCommand = command
	stub.manualMutation = "revoke"
	return stub.commandResult, stub.commandErr
}

type userAdministrationAuthorizerStub struct {
	requests []authz.Request
	err      error
}

func (stub *userAdministrationAuthorizerStub) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	stub.requests = append(stub.requests, request)
	if stub.err != nil {
		return authz.Decision{}, stub.err
	}
	return authz.Decision{
		Allow: true, GrantID: uuid.New(), GrantVersion: 1, MandateID: uuid.New(),
		RoleID: "user_reader", EffectiveUntil: request.Context.Now.Add(time.Hour),
	}, nil
}

func TestUserAdministrationListNormalizesAndAuthorizesTypedRead(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 9, 0, 0, 0, time.UTC)
	repository := &userAdministrationRepositoryStub{listResult: ManagedUserPage{Total: 2}}
	authorizer := &userAdministrationAuthorizerStub{}
	service, err := NewUserAdministrationService(repository, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewUserAdministrationService() error = %v", err)
	}

	result, err := service.List(context.Background(), userAdministrationActor(now), ListManagedUsersInput{Query: " demo "})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.Total != 2 || repository.listQuery.Query != "demo" || repository.listQuery.Page != 1 ||
		repository.listQuery.PageSize != defaultManagedUserPageSize || repository.listQuery.Offset != 0 ||
		!repository.listQuery.AsOf.Equal(now) {
		t.Fatalf("result=%+v query=%+v", result, repository.listQuery)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionUserAccountRead ||
		authorizer.requests[0].CredentialAudience != authz.AudienceStaffSession ||
		authorizer.requests[0].Context.Purpose != "identity-user-administration" {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestUserAdministrationGetUsesOneProjectionInstant(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 9, 0, 0, 0, time.UTC)
	userID := uuid.New()
	repository := &userAdministrationRepositoryStub{detail: ManagedUserDetail{
		ManagedUserSummary: ManagedUserSummary{ID: userID, Username: "demo"},
	}}
	authorizer := &userAdministrationAuthorizerStub{}
	service, err := NewUserAdministrationService(repository, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewUserAdministrationService() error = %v", err)
	}

	result, err := service.Get(context.Background(), userAdministrationActor(now), userID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if result.ID != userID || repository.getUserID != userID || !repository.getAsOf.Equal(now) {
		t.Fatalf("result=%+v user_id=%s as_of=%v", result, repository.getUserID, repository.getAsOf)
	}
}

func TestUserAdministrationAdjustNormalizesAuditsAndReloadsDetail(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 28, 10, 30, 0, 123, time.UTC)
	targetID, adjustmentID := uuid.New(), uuid.New()
	repository := &userAdministrationRepositoryStub{detail: ManagedUserDetail{
		ManagedUserSummary: ManagedUserSummary{ID: targetID, Version: 8},
		DonationAmount:     "41.20",
	}}
	dataRepository := &managedUserDataRepositoryStub{}
	authorizer := &userAdministrationAuthorizerStub{}
	service, err := NewUserAdministrationServiceWithData(
		repository, repository, dataRepository, authorizer,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewUserAdministrationServiceWithData() error = %v", err)
	}
	actor := userAdministrationActor(now)
	result, err := service.Adjust(context.Background(), actor, ManagedUserAdjustmentInput{
		AdjustmentID: adjustmentID, UserID: targetID,
		Field: ManagedUserAdjustmentDonationAmount, Operation: ManagedUserAdjustmentDecrease,
		Amount: "001.20", ExpectedUserVersion: 7,
	})
	if err != nil {
		t.Fatalf("Adjust() error = %v", err)
	}
	command := dataRepository.adjustCommand
	if result.DonationAmount != "41.20" || command.AdjustmentID != adjustmentID ||
		command.UserID != targetID || command.ActorID != actor.Subject.ID ||
		command.Field != ManagedUserAdjustmentDonationAmount || command.Delta != "-1.2" ||
		command.Reason != "管理员调整用户捐赠金额" || command.ExpectedUserVersion != 7 ||
		!command.OccurredAt.Equal(now.Round(0)) || repository.getUserID != targetID ||
		!repository.getAsOf.Equal(now.Round(0)) {
		t.Fatalf("result=%+v command=%+v repository=%+v", result, command, repository)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionUserAccountAdjust ||
		authorizer.requests[0].Context.Purpose != "identity-user-data-administration" {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestUserAdministrationAdjustRejectsInvalidAndSelfTargetBeforeWriting(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 28, 11, 0, 0, 0, time.UTC)
	repository := &userAdministrationRepositoryStub{}
	dataRepository := &managedUserDataRepositoryStub{}
	authorizer := &userAdministrationAuthorizerStub{}
	service, err := NewUserAdministrationServiceWithData(repository, repository, dataRepository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewUserAdministrationServiceWithData() error = %v", err)
	}
	actor := userAdministrationActor(now)
	base := ManagedUserAdjustmentInput{
		AdjustmentID: uuid.New(), UserID: uuid.New(), Field: ManagedUserAdjustmentUploadedBytes,
		Operation: ManagedUserAdjustmentIncrease, Amount: "1.5", ExpectedUserVersion: 1,
	}
	if _, err := service.Adjust(context.Background(), actor, base); !errors.Is(err, ErrUserAdministrationInput) {
		t.Fatalf("fractional byte Adjust() error = %v", err)
	}
	base.Field = ManagedUserAdjustmentMagicBalance
	base.Amount = "1"
	base.UserID = actor.Subject.ID
	if _, err := service.Adjust(context.Background(), actor, base); !errors.Is(err, ErrAccountRestrictionSelfTarget) {
		t.Fatalf("self-target Adjust() error = %v", err)
	}
	if len(authorizer.requests) != 0 || dataRepository.adjustCommand.AdjustmentID != uuid.Nil {
		t.Fatalf("authorizations=%+v command=%+v", authorizer.requests, dataRepository.adjustCommand)
	}
}

func TestUserAdministrationNetworkHistoryUsesPrivatePermissionAndBoundedWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	targetID := uuid.New()
	repository := &userAdministrationRepositoryStub{}
	dataRepository := &managedUserDataRepositoryStub{networkResult: []ManagedUserNetworkObservation{{
		Address: "203.0.113.8", FirstSeenAt: now.Add(-time.Hour), LastSeenAt: now, SeenCount: 2, RelatedUserCount: 1,
	}}}
	authorizer := &userAdministrationAuthorizerStub{}
	service, err := NewUserAdministrationServiceWithData(repository, repository, dataRepository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewUserAdministrationServiceWithData() error = %v", err)
	}
	result, err := service.NetworkHistory(context.Background(), userAdministrationActor(now), targetID)
	if err != nil {
		t.Fatalf("NetworkHistory() error = %v", err)
	}
	if len(result.Items) != 1 || result.RetentionDays != 180 || result.MaximumItems != 20 ||
		dataRepository.networkUserID != targetID || dataRepository.networkLimit != 20 ||
		!dataRepository.networkCutoff.Equal(now.Add(-180*24*time.Hour)) {
		t.Fatalf("result=%+v repository=%+v", result, dataRepository)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionUserNetworkRead ||
		authorizer.requests[0].Context.Purpose != "identity-user-network-history" {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestUserAdministrationReactivationRestoresCredentialAndDefaultsReason(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	targetID, credentialRef := uuid.New(), uuid.New()
	repository := &userAdministrationRepositoryStub{
		preflight: ManagedUserReactivationPreflight{
			CredentialRef: credentialRef, Status: AccountStatusDisabled, Version: 4,
		},
		commandResult: ManagedUserDetail{ManagedUserSummary: ManagedUserSummary{
			ID: targetID, credentialRef: credentialRef, Status: AccountStatusActive, Version: 5,
		}},
	}
	lifecycle := &managedUserLifecycleStub{}
	service, err := NewUserAdministrationService(repository, repository, &userAdministrationAuthorizerStub{}, func() time.Time { return now }, lifecycle)
	if err != nil {
		t.Fatalf("NewUserAdministrationService() error = %v", err)
	}
	result, err := service.Reactivate(context.Background(), userAdministrationActor(now), ReactivateManagedUserInput{
		ReactivationID: uuid.New(), UserID: targetID, ExpectedUserVersion: 4,
	})
	if err != nil {
		t.Fatalf("Reactivate() error = %v", err)
	}
	if lifecycle.enabled != credentialRef || repository.reactivateCommand.Reason != "管理员手动解除账户封禁" ||
		repository.reactivateCommand.ExpectedUserVersion != 4 || result.Status != AccountStatusActive {
		t.Fatalf("lifecycle=%+v command=%+v result=%+v", lifecycle, repository.reactivateCommand, result)
	}
}

func TestUserAdministrationRejectsInvalidInputBeforeAuthorization(t *testing.T) {
	t.Parallel()

	repository := &userAdministrationRepositoryStub{}
	authorizer := &userAdministrationAuthorizerStub{err: errors.New("must not be called")}
	service, err := NewUserAdministrationService(repository, repository, authorizer, time.Now)
	if err != nil {
		t.Fatalf("NewUserAdministrationService() error = %v", err)
	}

	_, err = service.List(context.Background(), userAdministrationActor(time.Now()), ListManagedUsersInput{PageSize: maxManagedUserPageSize + 1})
	if !errors.Is(err, ErrUserAdministrationInput) {
		t.Fatalf("List() error = %v, want ErrUserAdministrationInput", err)
	}
	if len(authorizer.requests) != 0 {
		t.Fatalf("authorization requests = %+v, want none", authorizer.requests)
	}
}

func TestUserAdministrationCreateRestrictionNormalizesAuthorizesAndBuildsCommand(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 11, 0, 0, 0, time.UTC)
	targetID := uuid.New()
	repository := &userAdministrationRepositoryStub{commandResult: ManagedUserDetail{
		ManagedUserSummary: ManagedUserSummary{ID: targetID, Version: 4},
	}}
	authorizer := &userAdministrationAuthorizerStub{}
	service, err := NewUserAdministrationService(repository, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewUserAdministrationService() error = %v", err)
	}
	actor := userAdministrationActor(now)
	result, err := service.CreateRestriction(context.Background(), actor, CreateAccountRestrictionInput{
		UserID: targetID, ReasonCode: AccountRestrictionReasonManualReview,
		Reason:        "  该账户需要完成短期人工复核后再恢复访问。  ",
		DurationHours: 72, ExpectedUserVersion: 3,
	})
	if err != nil {
		t.Fatalf("CreateRestriction() error = %v", err)
	}
	if result.Version != 4 || repository.createCommand.UserID != targetID ||
		repository.createCommand.ActorID != actor.Subject.ID || repository.createCommand.Reason != "该账户需要完成短期人工复核后再恢复访问。" ||
		repository.createCommand.DurationHours != 72 || !repository.createCommand.OccurredAt.Equal(now) {
		t.Fatalf("result=%+v command=%+v", result, repository.createCommand)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionUserAccountRestrict ||
		authorizer.requests[0].Context.Purpose != "identity-account-restriction-administration" {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestUserAdministrationRevokeRestrictionUsesIndependentTypedAction(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	targetID := uuid.New()
	restrictionID := uuid.New()
	repository := &userAdministrationRepositoryStub{}
	authorizer := &userAdministrationAuthorizerStub{}
	service, err := NewUserAdministrationService(repository, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewUserAdministrationService() error = %v", err)
	}
	actor := userAdministrationActor(now)
	_, err = service.RevokeRestriction(context.Background(), actor, RevokeAccountRestrictionInput{
		UserID: targetID, RestrictionID: restrictionID,
		ReasonCode:          AccountRestrictionRevocationReviewCompleted,
		Reason:              "人工复核已经完成，可以恢复账户访问。",
		ExpectedUserVersion: 4, ExpectedRestrictionVersion: 2,
	})
	if err != nil {
		t.Fatalf("RevokeRestriction() error = %v", err)
	}
	if repository.revokeCommand.RestrictionID != restrictionID ||
		repository.revokeCommand.ExpectedUserVersion != 4 || repository.revokeCommand.ExpectedRestrictionVersion != 2 ||
		len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionUserAccountRestrictionRevoke {
		t.Fatalf("command=%+v authorization=%+v", repository.revokeCommand, authorizer.requests)
	}
}

func TestUserAdministrationRestrictionRejectsSelfTargetAndOversizedWindowBeforeAuthorization(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	repository := &userAdministrationRepositoryStub{}
	authorizer := &userAdministrationAuthorizerStub{}
	service, err := NewUserAdministrationService(repository, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewUserAdministrationService() error = %v", err)
	}
	actor := userAdministrationActor(now)
	_, err = service.CreateRestriction(context.Background(), actor, CreateAccountRestrictionInput{
		UserID: actor.Subject.ID, ReasonCode: AccountRestrictionReasonManualReview,
		Reason: "该操作用于验证禁止员工限制自己的账户。", DurationHours: 24, ExpectedUserVersion: 1,
	})
	if !errors.Is(err, ErrAccountRestrictionSelfTarget) {
		t.Fatalf("self CreateRestriction() error = %v", err)
	}
	_, err = service.CreateRestriction(context.Background(), actor, CreateAccountRestrictionInput{
		UserID: uuid.New(), ReasonCode: AccountRestrictionReasonManualReview,
		Reason: "该操作用于验证最长限制时长边界。", DurationHours: maxAccountRestrictionDurationHours + 1,
		ExpectedUserVersion: 1,
	})
	if !errors.Is(err, ErrUserAdministrationInput) {
		t.Fatalf("oversized CreateRestriction() error = %v", err)
	}
	if len(authorizer.requests) != 0 {
		t.Fatalf("authorization requests = %+v, want none", authorizer.requests)
	}
}

func TestUserAdministrationManualDownloadRestrictionUsesIndependentActionsAndVersions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 17, 11, 0, 0, 0, time.UTC)
	targetID := uuid.New()
	repository := &userAdministrationRepositoryStub{}
	authorizer := &userAdministrationAuthorizerStub{}
	service, err := NewUserAdministrationService(repository, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewUserAdministrationService() error = %v", err)
	}
	actor := userAdministrationActor(now)
	_, err = service.CreateManualDownloadRestriction(context.Background(), actor, CreateManualDownloadRestrictionInput{
		UserID: targetID, ReasonCode: ManualDownloadRestrictionReasonPolicyViolation,
		Reason:              "  用户下载行为违反站点规则，等待人工复核后再恢复。  ",
		ExpectedUserVersion: 4, ExpectedStateVersion: 0,
	})
	if err != nil {
		t.Fatalf("CreateManualDownloadRestriction() error = %v", err)
	}
	if repository.manualMutation != "create" || repository.manualCommand.UserID != targetID ||
		repository.manualCommand.ExpectedUserVersion != 4 || repository.manualCommand.ExpectedStateVersion != 0 ||
		repository.manualCommand.Reason != "用户下载行为违反站点规则，等待人工复核后再恢复。" ||
		repository.manualCommand.ActorID != actor.Subject.ID ||
		len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionUserDownloadRestrictionRestrict {
		t.Fatalf("command=%+v authorization=%+v", repository.manualCommand, authorizer.requests)
	}

	authorizer.requests = nil
	_, err = service.RevokeManualDownloadRestriction(context.Background(), actor, RevokeManualDownloadRestrictionInput{
		UserID: targetID, ReasonCode: ManualDownloadRestrictionRevocationReviewCompleted,
		Reason:              "复核已经完成，确认可以解除人工下载限制。",
		ExpectedUserVersion: 5, ExpectedStateVersion: 1,
	})
	if err != nil {
		t.Fatalf("RevokeManualDownloadRestriction() error = %v", err)
	}
	if repository.manualMutation != "revoke" || repository.manualCommand.ExpectedStateVersion != 1 ||
		len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionUserDownloadRestrictionRevoke {
		t.Fatalf("command=%+v authorization=%+v", repository.manualCommand, authorizer.requests)
	}
}

func TestUserAdministrationChangeVIPNormalizesAndUsesDedicatedAction(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 18, 2, 0, 0, 0, time.UTC)
	targetID := uuid.New()
	duration := 90
	repository := &userAdministrationRepositoryStub{commandResult: ManagedUserDetail{
		ManagedUserSummary: ManagedUserSummary{ID: targetID, Version: 5},
	}}
	authorizer := &userAdministrationAuthorizerStub{}
	service, err := NewUserAdministrationService(repository, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewUserAdministrationService() error = %v", err)
	}
	actor := userAdministrationActor(now)
	result, err := service.ChangeVIP(context.Background(), actor, ChangeVIPInput{
		UserID: targetID, Enabled: true, DurationDays: &duration,
		Reason:              "  用户符合站点活动的 VIP 签发条件。  ",
		ExpectedUserVersion: 4, ExpectedStateVersion: 2,
	})
	if err != nil {
		t.Fatalf("ChangeVIP() error = %v", err)
	}
	if result.Version != 5 || repository.vipCommand.UserID != targetID ||
		repository.vipCommand.ActorID != actor.Subject.ID ||
		repository.vipCommand.Reason != "用户符合站点活动的 VIP 签发条件。" ||
		repository.vipCommand.DurationDays == nil || *repository.vipCommand.DurationDays != 90 ||
		!repository.vipCommand.OccurredAt.Equal(now) || len(authorizer.requests) != 1 ||
		authorizer.requests[0].Action != authz.ActionUserVIPManage ||
		authorizer.requests[0].Context.Purpose != "identity-vip-administration" {
		t.Fatalf("result=%+v command=%+v authorization=%+v", result, repository.vipCommand, authorizer.requests)
	}
}

func TestUserAdministrationChangeVIPRejectsSelfAndInvalidRevocation(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	repository := &userAdministrationRepositoryStub{}
	authorizer := &userAdministrationAuthorizerStub{}
	service, err := NewUserAdministrationService(repository, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewUserAdministrationService() error = %v", err)
	}
	actor := userAdministrationActor(now)
	_, err = service.ChangeVIP(context.Background(), actor, ChangeVIPInput{
		UserID: actor.Subject.ID, Enabled: true,
		Reason:              "不能由管理员为自己签发 VIP 身份。",
		ExpectedUserVersion: 1,
	})
	if !errors.Is(err, ErrAccountRestrictionSelfTarget) {
		t.Fatalf("self ChangeVIP() error = %v", err)
	}
	duration := 30
	_, err = service.ChangeVIP(context.Background(), actor, ChangeVIPInput{
		UserID: uuid.New(), Enabled: false, DurationDays: &duration,
		Reason:              "撤销 VIP 时不能同时提交新的有效期限。",
		ExpectedUserVersion: 1,
	})
	if !errors.Is(err, ErrUserAdministrationInput) {
		t.Fatalf("invalid ChangeVIP() error = %v", err)
	}
	if len(authorizer.requests) != 0 {
		t.Fatalf("authorization requests = %+v, want none", authorizer.requests)
	}
}

func userAdministrationActor(now time.Time) authz.StaffActor {
	return authz.StaffActor{
		Subject:            authz.Subject{ID: uuid.New(), Status: authz.SubjectActive},
		MFAAuthenticatedAt: now.Add(-time.Minute),
	}
}
