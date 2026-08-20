package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	minAnnouncementReasonRunes  = 10
	maxAnnouncementReasonRunes  = 500
	minAnnouncementScheduleLead = time.Minute
	maxAnnouncementScheduleLead = 365 * 24 * time.Hour
)

type AnnouncementAdministrationRepository interface {
	ListManagedAnnouncements(context.Context, int, int, time.Time) (ManagedAnnouncementPage, error)
	GetManagedAnnouncement(context.Context, string, time.Time) (ManagedAnnouncement, error)
	ListAnnouncementRevisions(context.Context, string, int, int) (AnnouncementRevisionPage, error)
	CreateAnnouncementDraft(context.Context, CreateAnnouncementDraftCommand) (ManagedAnnouncement, error)
	UpdateAnnouncementDraft(context.Context, UpdateAnnouncementDraftCommand) (ManagedAnnouncement, error)
	ChangeAnnouncementPublication(context.Context, ChangeAnnouncementPublicationCommand) (ManagedAnnouncement, error)
}

type AnnouncementAdministrationService struct {
	repository AnnouncementAdministrationRepository
	authorizer catalogStaffAuthorizer
	now        func() time.Time
}

func NewAnnouncementAdministrationService(repository AnnouncementAdministrationRepository, authorizer catalogStaffAuthorizer, now func() time.Time) (*AnnouncementAdministrationService, error) {
	if repository == nil || authorizer == nil {
		return nil, errors.New("announcement administration dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &AnnouncementAdministrationService{repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *AnnouncementAdministrationService) List(ctx context.Context, actor authz.StaffActor, limit, offset int) (ManagedAnnouncementPage, error) {
	limit, offset, err := normalizedManagedAnnouncementPage(limit, offset)
	if err != nil {
		return ManagedAnnouncementPage{}, err
	}
	now := service.now().UTC()
	if _, err := authorizeCatalogStaff(ctx, service.authorizer, actor, authz.ActionAnnouncementManageRead, now, "catalog-announcement-administration"); err != nil {
		return ManagedAnnouncementPage{}, err
	}
	result, err := service.repository.ListManagedAnnouncements(ctx, limit, offset, now)
	if err != nil {
		return ManagedAnnouncementPage{}, fmt.Errorf("list managed announcements: %w", err)
	}
	return result, nil
}

func (service *AnnouncementAdministrationService) Get(ctx context.Context, actor authz.StaffActor, announcementID string) (ManagedAnnouncement, error) {
	announcementID = strings.TrimSpace(announcementID)
	if !ValidAnnouncementID(announcementID) {
		return ManagedAnnouncement{}, ErrAnnouncementAdministrationInput
	}
	now := service.now().UTC()
	if _, err := authorizeCatalogStaff(ctx, service.authorizer, actor, authz.ActionAnnouncementManageRead, now, "catalog-announcement-administration"); err != nil {
		return ManagedAnnouncement{}, err
	}
	result, err := service.repository.GetManagedAnnouncement(ctx, announcementID, now)
	if err != nil {
		return ManagedAnnouncement{}, fmt.Errorf("get managed announcement: %w", err)
	}
	return result, nil
}

func (service *AnnouncementAdministrationService) Revisions(ctx context.Context, actor authz.StaffActor, announcementID string, limit, offset int) (AnnouncementRevisionPage, error) {
	announcementID = strings.TrimSpace(announcementID)
	limit, offset, err := normalizedManagedAnnouncementPage(limit, offset)
	if err != nil || !ValidAnnouncementID(announcementID) {
		return AnnouncementRevisionPage{}, ErrAnnouncementAdministrationInput
	}
	now := service.now().UTC()
	if _, err := authorizeCatalogStaff(ctx, service.authorizer, actor, authz.ActionAnnouncementManageRead, now, "catalog-announcement-administration"); err != nil {
		return AnnouncementRevisionPage{}, err
	}
	result, err := service.repository.ListAnnouncementRevisions(ctx, announcementID, limit, offset)
	if err != nil {
		return AnnouncementRevisionPage{}, fmt.Errorf("list announcement revisions: %w", err)
	}
	return result, nil
}

func (service *AnnouncementAdministrationService) Create(ctx context.Context, actor authz.StaffActor, input CreateAnnouncementDraftInput) (ManagedAnnouncement, error) {
	normalized, err := normalizedAnnouncementDraft(input)
	if err != nil {
		return ManagedAnnouncement{}, err
	}
	now := service.now().UTC()
	decision, err := authorizeCatalogStaff(ctx, service.authorizer, actor, authz.ActionAnnouncementCreate, now, "catalog-announcement-administration")
	if err != nil {
		return ManagedAnnouncement{}, err
	}
	result, err := service.repository.CreateAnnouncementDraft(ctx, CreateAnnouncementDraftCommand{
		CreateAnnouncementDraftInput: normalized,
		ActorID:                      actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
	if err != nil {
		return ManagedAnnouncement{}, fmt.Errorf("create announcement draft: %w", err)
	}
	return result, nil
}

func (service *AnnouncementAdministrationService) UpdateDraft(ctx context.Context, actor authz.StaffActor, input UpdateAnnouncementDraftInput) (ManagedAnnouncement, error) {
	normalized, err := normalizedAnnouncementDraftUpdate(input)
	if err != nil {
		return ManagedAnnouncement{}, err
	}
	now := service.now().UTC()
	decision, err := authorizeCatalogStaff(ctx, service.authorizer, actor, authz.ActionAnnouncementUpdate, now, "catalog-announcement-administration")
	if err != nil {
		return ManagedAnnouncement{}, err
	}
	result, err := service.repository.UpdateAnnouncementDraft(ctx, UpdateAnnouncementDraftCommand{
		UpdateAnnouncementDraftInput: normalized,
		ActorID:                      actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
	if err != nil {
		return ManagedAnnouncement{}, fmt.Errorf("update announcement draft: %w", err)
	}
	return result, nil
}

func (service *AnnouncementAdministrationService) ChangePublication(ctx context.Context, actor authz.StaffActor, input ChangeAnnouncementPublicationInput) (ManagedAnnouncement, error) {
	now := service.now().UTC()
	normalized, err := normalizedAnnouncementPublication(input, now)
	if err != nil {
		return ManagedAnnouncement{}, err
	}
	action := authz.ActionAnnouncementPublish
	if normalized.Action == AnnouncementWithdraw {
		action = authz.ActionAnnouncementWithdraw
	}
	decision, err := authorizeCatalogStaff(ctx, service.authorizer, actor, action, now, "catalog-announcement-administration")
	if err != nil {
		return ManagedAnnouncement{}, err
	}
	result, err := service.repository.ChangeAnnouncementPublication(ctx, ChangeAnnouncementPublicationCommand{
		ChangeAnnouncementPublicationInput: normalized,
		ActorID:                            actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
	if err != nil {
		return ManagedAnnouncement{}, fmt.Errorf("change announcement publication: %w", err)
	}
	return result, nil
}

func normalizedManagedAnnouncementPage(limit, offset int) (int, int, error) {
	limit, offset, valid := normalizedAnnouncementPage(limit, offset)
	if !valid {
		return 0, 0, ErrAnnouncementAdministrationInput
	}
	return limit, offset, nil
}

func normalizedAnnouncementDraft(input CreateAnnouncementDraftInput) (CreateAnnouncementDraftInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Title = strings.TrimSpace(input.Title)
	input.Summary = strings.TrimSpace(input.Summary)
	input.Body = strings.TrimSpace(input.Body)
	input.Reason = strings.TrimSpace(input.Reason)
	if !validAnnouncementDraftFields(input.ID, input.Title, input.Summary, input.Body, input.BodyFormat, input.Reason) {
		return CreateAnnouncementDraftInput{}, ErrAnnouncementAdministrationInput
	}
	return input, nil
}

func normalizedAnnouncementDraftUpdate(input UpdateAnnouncementDraftInput) (UpdateAnnouncementDraftInput, error) {
	created, err := normalizedAnnouncementDraft(CreateAnnouncementDraftInput{
		ID: input.ID, Title: input.Title, Summary: input.Summary, Body: input.Body,
		BodyFormat: input.BodyFormat, Reason: input.Reason,
	})
	if err != nil || input.ExpectedVersion < 1 {
		return UpdateAnnouncementDraftInput{}, ErrAnnouncementAdministrationInput
	}
	return UpdateAnnouncementDraftInput{
		ID: created.ID, Title: created.Title, Summary: created.Summary, Body: created.Body,
		BodyFormat: created.BodyFormat, ExpectedVersion: input.ExpectedVersion, Reason: created.Reason,
	}, nil
}

func normalizedAnnouncementPublication(input ChangeAnnouncementPublicationInput, now time.Time) (ChangeAnnouncementPublicationInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Reason = strings.TrimSpace(input.Reason)
	if !ValidAnnouncementID(input.ID) || input.ExpectedVersion < 1 || !validAnnouncementReason(input.Reason) {
		return ChangeAnnouncementPublicationInput{}, ErrAnnouncementAdministrationInput
	}
	switch input.Action {
	case AnnouncementSchedule:
		if input.ScheduledFor == nil {
			return ChangeAnnouncementPublicationInput{}, ErrAnnouncementAdministrationInput
		}
		scheduledFor := input.ScheduledFor.UTC()
		if scheduledFor.Before(now.Add(minAnnouncementScheduleLead)) || scheduledFor.After(now.Add(maxAnnouncementScheduleLead)) {
			return ChangeAnnouncementPublicationInput{}, ErrAnnouncementAdministrationInput
		}
		input.ScheduledFor = &scheduledFor
	case AnnouncementPublishNow, AnnouncementCancelSchedule, AnnouncementWithdraw:
		if input.ScheduledFor != nil {
			return ChangeAnnouncementPublicationInput{}, ErrAnnouncementAdministrationInput
		}
	default:
		return ChangeAnnouncementPublicationInput{}, ErrAnnouncementAdministrationInput
	}
	return input, nil
}

func validAnnouncementDraftFields(id, title, summary, body string, format AnnouncementBodyFormat, reason string) bool {
	return ValidAnnouncementID(id) && utf8.ValidString(title) && utf8.ValidString(summary) && utf8.ValidString(body) &&
		utf8.RuneCountInString(title) >= 1 && utf8.RuneCountInString(title) <= 160 &&
		utf8.RuneCountInString(summary) >= 1 && utf8.RuneCountInString(summary) <= 500 &&
		utf8.RuneCountInString(body) >= 1 && utf8.RuneCountInString(body) <= 20_000 &&
		(format == AnnouncementBodyPlainText || format == AnnouncementBodyLegacyBBCode) && validAnnouncementReason(reason)
}

func validAnnouncementReason(reason string) bool {
	return utf8.ValidString(reason) && utf8.RuneCountInString(reason) >= minAnnouncementReasonRunes && utf8.RuneCountInString(reason) <= maxAnnouncementReasonRunes
}
