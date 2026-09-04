package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type categoryAdministrationRepositoryStub struct {
	listed        []ManagedCategory
	created       ManagedCategory
	updated       ManagedCategory
	upsertedFacet ManagedCategoryFacet
	upserted      ManagedCategoryFacetOption
	createCommand CreateCategoryCommand
	updateCommand UpdateCategoryCommand
	facetCommand  UpsertCategoryFacetCommand
	upsertCommand UpsertCategoryFacetOptionCommand
	listCalls     int
}

func (stub *categoryAdministrationRepositoryStub) ListManagedCategories(context.Context) ([]ManagedCategory, error) {
	stub.listCalls++
	return stub.listed, nil
}

func (stub *categoryAdministrationRepositoryStub) CreateCategory(_ context.Context, command CreateCategoryCommand) (ManagedCategory, error) {
	stub.createCommand = command
	return stub.created, nil
}

func (stub *categoryAdministrationRepositoryStub) UpdateCategory(_ context.Context, command UpdateCategoryCommand) (ManagedCategory, error) {
	stub.updateCommand = command
	return stub.updated, nil
}

func (stub *categoryAdministrationRepositoryStub) UpsertCategoryFacet(_ context.Context, command UpsertCategoryFacetCommand) (ManagedCategoryFacet, error) {
	stub.facetCommand = command
	return stub.upsertedFacet, nil
}

func (stub *categoryAdministrationRepositoryStub) UpsertCategoryFacetOption(_ context.Context, command UpsertCategoryFacetOptionCommand) (ManagedCategoryFacetOption, error) {
	stub.upsertCommand = command
	return stub.upserted, nil
}

type categoryAuthorizerStub struct {
	decision authz.Decision
	err      error
	requests []authz.Request
}

func (stub *categoryAuthorizerStub) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	stub.requests = append(stub.requests, request)
	return stub.decision, stub.err
}

func TestCategoryAdministrationCreateNormalizesAndAuthorizes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 13, 0, 0, 0, time.UTC)
	actor := categoryTestActor(now)
	repository := &categoryAdministrationRepositoryStub{created: ManagedCategory{ID: "software", Name: "软件", Version: 1}}
	authorizer := &categoryAuthorizerStub{decision: categoryAllowedDecision(now)}
	service, err := NewCategoryAdministrationService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewCategoryAdministrationService() error = %v", err)
	}

	result, err := service.Create(context.Background(), actor, CreateCategoryInput{
		ID: " software ", Name: " 软件 ", DisplayOrder: 60, Enabled: true,
		Reason: " 增加软件分类以承载后续正式内容。 ",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.ID != "software" || repository.createCommand.ID != "software" || repository.createCommand.Name != "软件" {
		t.Fatalf("result=%+v command=%+v", result, repository.createCommand)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionCategoryCreate || authorizer.requests[0].CredentialAudience != authz.AudienceStaffSession {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
	if repository.createCommand.Authorization.ID != authorizer.decision.ID || repository.createCommand.ActorID != actor.Subject.ID {
		t.Fatalf("create command evidence = %+v", repository.createCommand)
	}
}

func TestCategoryAdministrationUpdateCarriesExpectedVersion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 13, 0, 0, 0, time.UTC)
	repository := &categoryAdministrationRepositoryStub{updated: ManagedCategory{ID: "movies", Name: "电影与短片", Version: 4}}
	authorizer := &categoryAuthorizerStub{decision: categoryAllowedDecision(now)}
	service, err := NewCategoryAdministrationService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewCategoryAdministrationService() error = %v", err)
	}

	_, err = service.Update(context.Background(), categoryTestActor(now), UpdateCategoryInput{
		ID: "movies", Name: "电影与短片", DisplayOrder: 10, Enabled: false,
		ExpectedVersion: 3, Reason: "暂时停用分类并核对其中已有种子影响。",
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if repository.updateCommand.ExpectedVersion != 3 || repository.updateCommand.Enabled {
		t.Fatalf("update command = %+v", repository.updateCommand)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionCategoryUpdate {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestCategoryAdministrationRejectsInvalidInputBeforeAuthorization(t *testing.T) {
	t.Parallel()

	repository := &categoryAdministrationRepositoryStub{}
	authorizer := &categoryAuthorizerStub{err: errors.New("must not be called")}
	service, err := NewCategoryAdministrationService(repository, authorizer, time.Now)
	if err != nil {
		t.Fatalf("NewCategoryAdministrationService() error = %v", err)
	}

	_, err = service.Create(context.Background(), categoryTestActor(time.Now()), CreateCategoryInput{
		ID: "Invalid ID", Name: "软件", DisplayOrder: 1, Enabled: true, Reason: "理由长度足够但标识不合法。",
	})
	if !errors.Is(err, ErrCategoryAdministrationInput) {
		t.Fatalf("Create() error = %v, want ErrCategoryAdministrationInput", err)
	}
	if len(authorizer.requests) != 0 {
		t.Fatalf("authorization requests = %+v, want none", authorizer.requests)
	}
}

func TestCategoryAdministrationUpsertFacetOptionNormalizesAndAudits(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 23, 15, 0, 0, 0, time.UTC)
	repository := &categoryAdministrationRepositoryStub{upserted: ManagedCategoryFacetOption{Key: "action", Label: "动作", Version: 2}}
	authorizer := &categoryAuthorizerStub{decision: categoryAllowedDecision(now)}
	service, err := NewCategoryAdministrationService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	actor := categoryTestActor(now)
	result, err := service.UpsertFacetOption(context.Background(), actor, UpsertCategoryFacetOptionInput{
		CategoryID: " movies ", FacetID: " genre ", OptionKey: " action ", Label: " 动作 ",
		DisplayOrder: 10, Enabled: true, ExpectedVersion: 1,
		Reason: " 调整电影分类下动作类型的显示顺序。 ",
	})
	if err != nil {
		t.Fatalf("UpsertFacetOption() error = %v", err)
	}
	if result.Key != "action" || repository.upsertCommand.CategoryID != "movies" ||
		repository.upsertCommand.FacetID != "genre" || repository.upsertCommand.OptionKey != "action" ||
		repository.upsertCommand.Label != "动作" || repository.upsertCommand.ChangeID == uuid.Nil ||
		repository.upsertCommand.Authorization.ID != authorizer.decision.ID {
		t.Fatalf("result=%+v command=%+v", result, repository.upsertCommand)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionCategoryUpdate {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestCategoryAdministrationUpsertFacetNormalizesAndDefaultsReason(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 27, 16, 0, 0, 0, time.UTC)
	repository := &categoryAdministrationRepositoryStub{upsertedFacet: ManagedCategoryFacet{ID: "resolution", Name: "分辨率", Version: 1}}
	authorizer := &categoryAuthorizerStub{decision: categoryAllowedDecision(now)}
	service, err := NewCategoryAdministrationService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.UpsertFacet(context.Background(), categoryTestActor(now), UpsertCategoryFacetInput{
		CategoryID: " movies ", FacetID: " resolution ", Name: " 分辨率 ",
		SelectionMode: FacetSelectionSingle, Required: true,
		DisplayOrder: 20, Enabled: true, ExpectedVersion: 0,
	})
	if err != nil {
		t.Fatalf("UpsertFacet() error = %v", err)
	}
	if result.ID != "resolution" || repository.facetCommand.CategoryID != "movies" ||
		repository.facetCommand.Name != "分辨率" || repository.facetCommand.ChangeID == uuid.Nil ||
		repository.facetCommand.Reason != defaultCategoryFacetReason ||
		repository.facetCommand.Authorization.ID != authorizer.decision.ID {
		t.Fatalf("result=%+v command=%+v", result, repository.facetCommand)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionCategoryUpdate {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func categoryTestActor(now time.Time) authz.StaffActor {
	return authz.StaffActor{
		Subject:            authz.Subject{ID: uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111"), Status: authz.SubjectActive},
		MFAAuthenticatedAt: now.Add(-time.Minute),
	}
}

func categoryAllowedDecision(now time.Time) authz.Decision {
	return authz.Decision{
		ID: uuid.MustParse("0198f20a-6da8-7e51-9c64-121212121212"), Allow: true,
		Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.MustParse("0198f20a-6da8-7e51-9c64-131313131313"), GrantVersion: 2,
		RoleID: "category_manager", MandateID: uuid.MustParse("0198f20a-6da8-7e51-9c64-141414141414"),
		EffectiveUntil: now.Add(time.Hour),
	}
}
