package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type registrationPolicyAdministrationRepositoryStub struct {
	current       RegistrationPolicy
	updated       RegistrationPolicy
	updateCommand UpdateRegistrationPolicyCommand
	getCalls      int
}

func (stub *registrationPolicyAdministrationRepositoryStub) GetRegistrationPolicy(context.Context) (RegistrationPolicy, error) {
	stub.getCalls++
	return stub.current, nil
}

func (stub *registrationPolicyAdministrationRepositoryStub) UpdateRegistrationPolicy(_ context.Context, command UpdateRegistrationPolicyCommand) (RegistrationPolicy, error) {
	stub.updateCommand = command
	return stub.updated, nil
}

func TestRegistrationPolicyAdministrationUsesDedicatedReadPermission(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	repository := &registrationPolicyAdministrationRepositoryStub{
		current: RegistrationPolicy{Mode: RegistrationModeInvite, Version: 3, UpdatedAt: now},
	}
	authorizer := &userAdministrationAuthorizerStub{}
	service, err := NewRegistrationPolicyAdministrationService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewRegistrationPolicyAdministrationService() error = %v", err)
	}

	result, err := service.Get(context.Background(), userAdministrationActor(now))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if result.Mode != RegistrationModeInvite || repository.getCalls != 1 {
		t.Fatalf("result=%+v getCalls=%d", result, repository.getCalls)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionSiteRegistrationManageRead ||
		authorizer.requests[0].Context.Purpose != "identity-registration-policy-administration" {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestRegistrationPolicyAdministrationNormalizesAndCarriesVersion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	repository := &registrationPolicyAdministrationRepositoryStub{
		updated: RegistrationPolicy{Mode: RegistrationModeClosed, Version: 4, UpdatedAt: now},
	}
	authorizer := &userAdministrationAuthorizerStub{}
	service, err := NewRegistrationPolicyAdministrationService(
		repository,
		authorizer,
		func() time.Time { return now },
		RegistrationPolicyAdministrationServiceConfig{HumanVerificationSecretConfigured: true},
	)
	if err != nil {
		t.Fatalf("NewRegistrationPolicyAdministrationService() error = %v", err)
	}

	result, err := service.Update(context.Background(), userAdministrationActor(now), UpdateRegistrationPolicyInput{
		Mode: RegistrationModeClosed, MemberInvitesEnabled: true,
		InviteValidDays: 7, MaxInvitesPerMember: 5,
		MinimumInviteAccountAgeDays: 30, MinimumInviteLevel: 2,
		UsernameMinCharacters: 3, UsernameMaxCharacters: 20,
		ReservedUsernames: []string{" Root ", "admin", "root"},
		EmailDomainMode:   EmailDomainModeBlocklist, EmailDomains: []string{" Trash.Example ", "trash.example"},
		SessionValidHours: 168, RememberSessionValidHours: 720,
		HumanVerificationProvider:            HumanVerificationProviderTurnstile,
		HumanVerificationSiteKey:             " public-site-key ",
		HumanVerificationRegistrationEnabled: true,
		ExpectedVersion:                      3,
		Reason:                               " 维护期间暂时停止创建新账户。 ",
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	command := repository.updateCommand
	if result.Version != 4 || command.Mode != RegistrationModeClosed || command.ExpectedVersion != 3 ||
		!command.MemberInvitesEnabled || command.InviteValidDays != 7 || command.MaxInvitesPerMember != 5 ||
		command.MinimumInviteAccountAgeDays != 30 || command.MinimumInviteLevel != 2 ||
		command.UsernameMinCharacters != 3 || command.UsernameMaxCharacters != 20 ||
		len(command.ReservedUsernames) != 2 || command.ReservedUsernames[0] != "admin" || command.ReservedUsernames[1] != "root" ||
		command.EmailDomainMode != EmailDomainModeBlocklist || len(command.EmailDomains) != 1 || command.EmailDomains[0] != "trash.example" ||
		command.SessionValidHours != 168 || command.RememberSessionValidHours != 720 ||
		command.HumanVerificationProvider != HumanVerificationProviderTurnstile ||
		command.HumanVerificationSiteKey != "public-site-key" ||
		!command.HumanVerificationRegistrationEnabled ||
		command.Reason != "维护期间暂时停止创建新账户。" || !command.OccurredAt.Equal(now) {
		t.Fatalf("result=%+v command=%+v", result, command)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionSiteRegistrationUpdate {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestRegistrationPolicyAdministrationRejectsTurnstileWithoutRuntimeSecret(t *testing.T) {
	t.Parallel()

	repository := &registrationPolicyAdministrationRepositoryStub{}
	authorizer := &userAdministrationAuthorizerStub{err: errors.New("must not be called")}
	service, err := NewRegistrationPolicyAdministrationService(repository, authorizer, time.Now)
	if err != nil {
		t.Fatalf("NewRegistrationPolicyAdministrationService() error = %v", err)
	}

	_, err = service.Update(context.Background(), userAdministrationActor(time.Now()), UpdateRegistrationPolicyInput{
		Mode:                          RegistrationModeOpen,
		InviteValidDays:               7,
		MaxInvitesPerMember:           5,
		MinimumInviteAccountAgeDays:   30,
		MinimumInviteLevel:            2,
		UsernameMinCharacters:         3,
		UsernameMaxCharacters:         20,
		EmailDomainMode:               EmailDomainModeAny,
		SessionValidHours:             168,
		RememberSessionValidHours:     720,
		HumanVerificationProvider:     HumanVerificationProviderTurnstile,
		HumanVerificationSiteKey:      "public-site-key",
		HumanVerificationLoginEnabled: true,
		ExpectedVersion:               1,
		Reason:                        "启用登录人机验证以降低自动化攻击。",
	})
	if !errors.Is(err, ErrRegistrationPolicyInput) {
		t.Fatalf("Update() error = %v, want ErrRegistrationPolicyInput", err)
	}
	if len(authorizer.requests) != 0 {
		t.Fatalf("authorization requests = %+v, want none", authorizer.requests)
	}
}

func TestRegistrationPolicyAdministrationRejectsInvalidInputBeforeAuthorization(t *testing.T) {
	t.Parallel()

	repository := &registrationPolicyAdministrationRepositoryStub{}
	authorizer := &userAdministrationAuthorizerStub{err: errors.New("must not be called")}
	service, err := NewRegistrationPolicyAdministrationService(repository, authorizer, time.Now)
	if err != nil {
		t.Fatalf("NewRegistrationPolicyAdministrationService() error = %v", err)
	}

	_, err = service.Update(context.Background(), userAdministrationActor(time.Now()), UpdateRegistrationPolicyInput{
		Mode: RegistrationMode("unknown"), ExpectedVersion: 1, Reason: "理由长度足够但模式无效。",
	})
	if !errors.Is(err, ErrRegistrationPolicyInput) {
		t.Fatalf("Update() error = %v, want ErrRegistrationPolicyInput", err)
	}
	if len(authorizer.requests) != 0 {
		t.Fatalf("authorization requests = %+v, want none", authorizer.requests)
	}
}
