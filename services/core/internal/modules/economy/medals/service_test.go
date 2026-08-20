package medals

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type medalAuthorizerStub struct {
	requests []authz.Request
}

func (stub *medalAuthorizerStub) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	stub.requests = append(stub.requests, request)
	return authz.Decision{
		ID: uuid.New(), Allow: true, GrantID: uuid.New(), GrantVersion: 1,
		MandateID: uuid.New(), RoleID: "site_admin", EffectiveUntil: request.Context.Now.Add(time.Hour),
	}, nil
}

type medalRepositoryStub struct {
	overviewCalls int
	settings      UpdateSettingsCommand
	create        CreateCommand
	update        UpdateCommand
}

func (stub *medalRepositoryStub) UpdateSettings(_ context.Context, command UpdateSettingsCommand) (Settings, error) {
	stub.settings = command
	return Settings{Enabled: command.Enabled, Version: command.ExpectedVersion + 1}, nil
}

func (stub *medalRepositoryStub) Overview(context.Context) (Overview, error) {
	stub.overviewCalls++
	return Overview{Items: []Definition{{ID: 1}}}, nil
}

func (stub *medalRepositoryStub) Create(_ context.Context, command CreateCommand) (Definition, error) {
	stub.create = command
	return Definition{ID: 2, Name: command.Name}, nil
}

func (stub *medalRepositoryStub) Update(_ context.Context, command UpdateCommand) (Definition, error) {
	stub.update = command
	return Definition{ID: command.ID, Name: command.Name}, nil
}

func TestServiceUsesTypedMedalActionsAndNormalizesCreate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 18, 8, 30, 0, 123456789, time.UTC)
	repository := &medalRepositoryStub{}
	authorizer := &medalAuthorizerStub{}
	service, err := NewService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	if _, err := service.Overview(context.Background(), actor); err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	description := "  迁移后的站点贡献勋章  "
	image := "  /uploads/medals/supporter.webp  "
	_, err = service.Create(context.Background(), actor, DefinitionInput{
		Name: "  站点贡献者  ", Description: &description, ImageSmallPath: &image,
		AcquisitionMethod: AcquisitionSponsor, DisplayOnPage: true,
		Reason: "补充旧站勋章图片和展示说明。",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if repository.overviewCalls != 1 || len(authorizer.requests) != 2 {
		t.Fatalf("overview calls = %d, authorization = %+v", repository.overviewCalls, authorizer.requests)
	}
	if authorizer.requests[0].Action != authz.ActionEconomyMedalManageRead ||
		authorizer.requests[1].Action != authz.ActionEconomyMedalCreate {
		t.Fatalf("authorization actions = %+v", authorizer.requests)
	}
	if repository.create.Name != "站点贡献者" ||
		repository.create.Description == nil || *repository.create.Description != "迁移后的站点贡献勋章" ||
		repository.create.ImageSmallPath == nil || *repository.create.ImageSmallPath != "/uploads/medals/supporter.webp" {
		t.Fatalf("create command = %+v", repository.create)
	}
	if !repository.create.OccurredAt.Equal(now.Truncate(time.Microsecond)) ||
		repository.create.ActorID != actor.Subject.ID || repository.create.Authorization.ID == uuid.Nil {
		t.Fatalf("create provenance = %+v", repository.create)
	}
}

func TestServiceRejectsUnsafeImageWithoutAuthorizationOrWrite(t *testing.T) {
	t.Parallel()
	repository := &medalRepositoryStub{}
	authorizer := &medalAuthorizerStub{}
	service, err := NewService(repository, authorizer, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	image := "data:image/svg+xml,<svg/>"
	_, err = service.Update(context.Background(), authz.StaffActor{}, 7, 2, DefinitionInput{
		Name: "测试勋章", ImageLargePath: &image, AcquisitionMethod: AcquisitionGrant,
		Reason: "尝试写入不安全的图片地址。",
	})
	if err != ErrInput {
		t.Fatalf("Update() error = %v, want ErrInput", err)
	}
	if len(authorizer.requests) != 0 || repository.update.ID != 0 {
		t.Fatalf("unsafe input reached dependencies: auth=%+v update=%+v", authorizer.requests, repository.update)
	}
}

func TestServiceUpdateCarriesOptimisticVersion(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 18, 9, 0, 0, 0, time.UTC)
	repository := &medalRepositoryStub{}
	authorizer := &medalAuthorizerStub{}
	service, err := NewService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	_, err = service.Update(context.Background(), actor, 12, 4, DefinitionInput{
		Name: "种审组", AcquisitionMethod: AcquisitionWorkgroup,
		MagicBonusBPS: 1500, Reason: "校正种审工作组勋章的魔力值加成。",
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if repository.update.ID != 12 || repository.update.ExpectedVersion != 4 ||
		repository.update.MagicBonusBPS != 1500 || repository.update.Authorization.ID == uuid.Nil {
		t.Fatalf("update command = %+v", repository.update)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionEconomyMedalUpdate {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestServiceUpdateSettingsNormalizesAndAuthorizes(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 19, 9, 0, 0, 123456789, time.UTC)
	repository := &medalRepositoryStub{}
	authorizer := &medalAuthorizerStub{}
	service, err := NewService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	result, err := service.UpdateSettings(context.Background(), actor, SettingsInput{
		Enabled: true, MaximumWearCount: 5, MaximumUploadBonusBPS: 10000,
		MaximumDownloadDiscountBPS: 8000, MaximumMagicBonusBPS: 12000,
		MaximumInviteBonus: 10,
		ExpectedVersion:    3, Reason: "  调整全站勋章权益上限并保持旧账不回算。  ",
	})
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}
	if result.Version != 4 || repository.settings.Reason != "调整全站勋章权益上限并保持旧账不回算。" ||
		repository.settings.ActorID != actor.Subject.ID || repository.settings.Authorization.ID == uuid.Nil ||
		!repository.settings.OccurredAt.Equal(now.Truncate(time.Microsecond)) {
		t.Fatalf("settings command = %+v, result = %+v", repository.settings, result)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionEconomyMedalUpdate {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestServiceRejectsInvalidSettingsBeforeAuthorization(t *testing.T) {
	t.Parallel()
	repository := &medalRepositoryStub{}
	authorizer := &medalAuthorizerStub{}
	service, err := NewService(repository, authorizer, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.UpdateSettings(context.Background(), authz.StaffActor{}, SettingsInput{
		Enabled: true, MaximumWearCount: 101, MaximumUploadBonusBPS: 100,
		MaximumDownloadDiscountBPS: 100, MaximumMagicBonusBPS: 100,
		ExpectedVersion: 1, Reason: "这个修改说明已经足够长但参数不合法。",
	})
	if err != ErrInput {
		t.Fatalf("UpdateSettings() error = %v, want ErrInput", err)
	}
	if len(authorizer.requests) != 0 || repository.settings.ExpectedVersion != 0 {
		t.Fatalf("invalid settings reached dependencies: auth=%+v settings=%+v", authorizer.requests, repository.settings)
	}
}
