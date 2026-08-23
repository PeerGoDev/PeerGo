package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	maxCategoryDisplayOrder = 1_000_000
	minCategoryReasonRunes  = 10
	maxCategoryReasonRunes  = 500
)

type CategoryAdministrationRepository interface {
	ListManagedCategories(context.Context) ([]ManagedCategory, error)
	CreateCategory(context.Context, CreateCategoryCommand) (ManagedCategory, error)
	UpdateCategory(context.Context, UpdateCategoryCommand) (ManagedCategory, error)
	UpsertCategoryFacetOption(context.Context, UpsertCategoryFacetOptionCommand) (ManagedCategoryFacetOption, error)
}

func (service *CategoryAdministrationService) UpsertFacetOption(ctx context.Context, actor authz.StaffActor, input UpsertCategoryFacetOptionInput) (ManagedCategoryFacetOption, error) {
	normalized, err := normalizedUpsertCategoryFacetOptionInput(input)
	if err != nil {
		return ManagedCategoryFacetOption{}, err
	}
	now := service.now().UTC()
	decision, err := authorizeCatalogStaff(ctx, service.authorizer, actor, authz.ActionCategoryUpdate, now, "catalog-category-facet-option-administration")
	if err != nil {
		return ManagedCategoryFacetOption{}, err
	}
	result, err := service.repository.UpsertCategoryFacetOption(ctx, UpsertCategoryFacetOptionCommand{
		UpsertCategoryFacetOptionInput: normalized,
		ChangeID:                       uuid.New(), ActorID: actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
	if err != nil {
		return ManagedCategoryFacetOption{}, fmt.Errorf("upsert category facet option: %w", err)
	}
	return result, nil
}

type CategoryAdministrationService struct {
	repository CategoryAdministrationRepository
	authorizer catalogStaffAuthorizer
	now        func() time.Time
}

func NewCategoryAdministrationService(repository CategoryAdministrationRepository, authorizer catalogStaffAuthorizer, now func() time.Time) (*CategoryAdministrationService, error) {
	if repository == nil || authorizer == nil {
		return nil, errors.New("category administration dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &CategoryAdministrationService{repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *CategoryAdministrationService) List(ctx context.Context, actor authz.StaffActor) ([]ManagedCategory, error) {
	now := service.now().UTC()
	if _, err := authorizeCatalogStaff(ctx, service.authorizer, actor, authz.ActionCategoryManageRead, now, "catalog-category-administration"); err != nil {
		return nil, err
	}
	result, err := service.repository.ListManagedCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list managed categories: %w", err)
	}
	return result, nil
}

func (service *CategoryAdministrationService) Create(ctx context.Context, actor authz.StaffActor, input CreateCategoryInput) (ManagedCategory, error) {
	normalized, err := normalizedCreateCategoryInput(input)
	if err != nil {
		return ManagedCategory{}, err
	}
	now := service.now().UTC()
	decision, err := authorizeCatalogStaff(ctx, service.authorizer, actor, authz.ActionCategoryCreate, now, "catalog-category-administration")
	if err != nil {
		return ManagedCategory{}, err
	}
	result, err := service.repository.CreateCategory(ctx, CreateCategoryCommand{
		ID: normalized.ID, Name: normalized.Name, DisplayOrder: normalized.DisplayOrder,
		Enabled: normalized.Enabled, Reason: normalized.Reason, ActorID: actor.Subject.ID,
		OccurredAt: now, Authorization: decision,
	})
	if err != nil {
		return ManagedCategory{}, fmt.Errorf("create category: %w", err)
	}
	return result, nil
}

func (service *CategoryAdministrationService) Update(ctx context.Context, actor authz.StaffActor, input UpdateCategoryInput) (ManagedCategory, error) {
	normalized, err := normalizedUpdateCategoryInput(input)
	if err != nil {
		return ManagedCategory{}, err
	}
	now := service.now().UTC()
	decision, err := authorizeCatalogStaff(ctx, service.authorizer, actor, authz.ActionCategoryUpdate, now, "catalog-category-administration")
	if err != nil {
		return ManagedCategory{}, err
	}
	result, err := service.repository.UpdateCategory(ctx, UpdateCategoryCommand{
		ID: normalized.ID, Name: normalized.Name, DisplayOrder: normalized.DisplayOrder,
		Enabled: normalized.Enabled, ExpectedVersion: normalized.ExpectedVersion,
		Reason: normalized.Reason, ActorID: actor.Subject.ID, OccurredAt: now,
		Authorization: decision,
	})
	if err != nil {
		return ManagedCategory{}, fmt.Errorf("update category: %w", err)
	}
	return result, nil
}

func normalizedCreateCategoryInput(input CreateCategoryInput) (CreateCategoryInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.Reason = strings.TrimSpace(input.Reason)
	if !validCategoryFields(input.ID, input.Name, input.DisplayOrder, input.Reason) {
		return CreateCategoryInput{}, ErrCategoryAdministrationInput
	}
	return input, nil
}

func normalizedUpdateCategoryInput(input UpdateCategoryInput) (UpdateCategoryInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.ExpectedVersion < 1 || !validCategoryFields(input.ID, input.Name, input.DisplayOrder, input.Reason) {
		return UpdateCategoryInput{}, ErrCategoryAdministrationInput
	}
	return input, nil
}

func validCategoryFields(id, name string, displayOrder int, reason string) bool {
	nameRunes := utf8.RuneCountInString(name)
	reasonRunes := utf8.RuneCountInString(reason)
	return utf8.ValidString(name) && utf8.ValidString(reason) && validCatalogID(id) &&
		nameRunes >= 1 && nameRunes <= 40 && displayOrder >= 0 && displayOrder <= maxCategoryDisplayOrder &&
		reasonRunes >= minCategoryReasonRunes && reasonRunes <= maxCategoryReasonRunes
}

func normalizedUpsertCategoryFacetOptionInput(input UpsertCategoryFacetOptionInput) (UpsertCategoryFacetOptionInput, error) {
	input.CategoryID = strings.TrimSpace(input.CategoryID)
	input.FacetID = strings.TrimSpace(input.FacetID)
	input.OptionKey = strings.TrimSpace(input.OptionKey)
	input.Label = strings.TrimSpace(input.Label)
	input.Reason = strings.TrimSpace(input.Reason)
	optionKeyRunes := utf8.RuneCountInString(input.OptionKey)
	labelRunes := utf8.RuneCountInString(input.Label)
	reasonRunes := utf8.RuneCountInString(input.Reason)
	if !validCatalogID(input.CategoryID) || !validCatalogID(input.FacetID) ||
		!utf8.ValidString(input.OptionKey) || optionKeyRunes < 1 || optionKeyRunes > 80 || strings.ContainsAny(input.OptionKey, "/?#") ||
		!utf8.ValidString(input.Label) || labelRunes < 1 || labelRunes > 80 ||
		input.DisplayOrder < 0 || input.DisplayOrder > maxCategoryDisplayOrder || input.ExpectedVersion < 0 ||
		!utf8.ValidString(input.Reason) || reasonRunes < minCategoryReasonRunes || reasonRunes > maxCategoryReasonRunes {
		return UpsertCategoryFacetOptionInput{}, ErrCategoryAdministrationInput
	}
	return input, nil
}
