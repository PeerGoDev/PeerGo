package wiki

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/platform/audittext"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,95}$`)

type Repository interface {
	List(context.Context, ListInput, *uuid.UUID, bool) (PageList, error)
	GetBySlug(context.Context, string, *uuid.UUID, bool) (Page, error)
	GetManaged(context.Context, uuid.UUID, uuid.UUID) (Page, error)
	Create(context.Context, createCommand) (Page, error)
	UpdateManaged(context.Context, updateManagedCommand) (Page, error)
	UpdateAssigned(context.Context, updateAssignedCommand) (Page, error)
	ListRevisions(context.Context, uuid.UUID, int, int) (RevisionPage, error)
	Restore(context.Context, restoreCommand) (Page, error)
}

type SessionAuthenticator interface {
	CurrentSession(context.Context, string) (identity.WebSession, error)
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

type Service struct {
	authenticator SessionAuthenticator
	authorizer    authz.Authorizer
	repository    Repository
	now           func() time.Time
}

func NewService(authenticator SessionAuthenticator, authorizer authz.Authorizer, repository Repository, now func() time.Time) (*Service, error) {
	if authenticator == nil || authorizer == nil || repository == nil {
		return nil, errors.New("wiki service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{authenticator: authenticator, authorizer: authorizer, repository: repository, now: now}, nil
}

// List returns public pages anonymously and includes member-only pages only
// after an ordinary Web session and the typed member permission are verified.
func (service *Service) List(ctx context.Context, cookieToken string, input ListInput) (PageList, error) {
	normalized, err := normalizeListInput(input, false)
	if err != nil {
		return PageList{}, err
	}
	viewerID, member, err := service.optionalMember(ctx, cookieToken)
	if err != nil {
		return PageList{}, err
	}
	return service.repository.List(ctx, normalized, viewerID, member)
}

func (service *Service) Get(ctx context.Context, cookieToken, slug string) (Page, error) {
	slug = normalizeSlug(slug)
	if !validSlug(slug) {
		return Page{}, ErrInput
	}
	viewerID, member, err := service.optionalMember(ctx, cookieToken)
	if err != nil {
		return Page{}, err
	}
	return service.repository.GetBySlug(ctx, slug, viewerID, member)
}

func (service *Service) UpdateAssigned(ctx context.Context, cookieToken, csrfToken string, input UpdateAssignedInput) (Page, error) {
	normalized, err := normalizeAssignedInput(input)
	if err != nil {
		return Page{}, err
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return Page{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebMemberAction(ctx, service.authorizer, session.User.ID, authz.ActionWikiPageUpdateAssigned, now); err != nil {
		return Page{}, err
	}
	return service.repository.UpdateAssigned(ctx, updateAssignedCommand{
		UpdateAssignedInput: normalized, ActorID: session.User.ID, UpdatedAt: now,
	})
}

func (service *Service) ListManaged(ctx context.Context, actor authz.StaffActor, input ListInput) (PageList, error) {
	normalized, err := normalizeListInput(input, true)
	if err != nil {
		return PageList{}, err
	}
	if err := service.authorizeStaff(ctx, actor, authz.ActionWikiPageManageRead); err != nil {
		return PageList{}, err
	}
	viewerID := actor.Subject.ID
	return service.repository.List(ctx, normalized, &viewerID, true)
}

func (service *Service) GetManaged(ctx context.Context, actor authz.StaffActor, pageID uuid.UUID) (Page, error) {
	if pageID == uuid.Nil {
		return Page{}, ErrInput
	}
	if err := service.authorizeStaff(ctx, actor, authz.ActionWikiPageManageRead); err != nil {
		return Page{}, err
	}
	return service.repository.GetManaged(ctx, pageID, actor.Subject.ID)
}

func (service *Service) CreateManaged(ctx context.Context, actor authz.StaffActor, input CreateManagedInput) (Page, error) {
	normalized, err := normalizeCreateInput(input)
	if err != nil {
		return Page{}, err
	}
	if err := service.authorizeStaff(ctx, actor, authz.ActionWikiPageCreate); err != nil {
		return Page{}, err
	}
	return service.repository.Create(ctx, createCommand{
		CreateManagedInput: normalized, PageID: uuid.New(), ActorID: actor.Subject.ID,
		CreatedAt: service.now().UTC(),
	})
}

func (service *Service) UpdateManaged(ctx context.Context, actor authz.StaffActor, input UpdateManagedInput) (Page, error) {
	normalized, err := normalizeManagedInput(input)
	if err != nil {
		return Page{}, err
	}
	if err := service.authorizeStaff(ctx, actor, authz.ActionWikiPageUpdate); err != nil {
		return Page{}, err
	}
	return service.repository.UpdateManaged(ctx, updateManagedCommand{
		UpdateManagedInput: normalized, ActorID: actor.Subject.ID, UpdatedAt: service.now().UTC(),
	})
}

func (service *Service) Revisions(ctx context.Context, actor authz.StaffActor, pageID uuid.UUID, limit, offset int) (RevisionPage, error) {
	if pageID == uuid.Nil || limit < 1 || limit > MaximumRevisions || offset < 0 || offset > MaximumPageOffset {
		return RevisionPage{}, ErrInput
	}
	if err := service.authorizeStaff(ctx, actor, authz.ActionWikiPageManageRead); err != nil {
		return RevisionPage{}, err
	}
	return service.repository.ListRevisions(ctx, pageID, limit, offset)
}

func (service *Service) RestoreManaged(ctx context.Context, actor authz.StaffActor, input RestoreManagedInput) (Page, error) {
	input.Reason = normalizeReason(input.Reason)
	if input.PageID == uuid.Nil || input.RevisionNumber < 1 || input.ExpectedVersion < 1 || !validReason(input.Reason) {
		return Page{}, ErrInput
	}
	if err := service.authorizeStaff(ctx, actor, authz.ActionWikiPageRestore); err != nil {
		return Page{}, err
	}
	return service.repository.Restore(ctx, restoreCommand{
		RestoreManagedInput: input, ActorID: actor.Subject.ID, RestoredAt: service.now().UTC(),
	})
}

func (service *Service) optionalMember(ctx context.Context, cookieToken string) (*uuid.UUID, bool, error) {
	if strings.TrimSpace(cookieToken) == "" {
		return nil, false, nil
	}
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return nil, false, err
	}
	if _, err := authz.AuthorizeWebMemberAction(ctx, service.authorizer, session.User.ID, authz.ActionWikiPageReadMember, service.now().UTC()); err != nil {
		return nil, false, err
	}
	viewerID := session.User.ID
	return &viewerID, true, nil
}

func (service *Service) authorizeStaff(ctx context.Context, actor authz.StaffActor, action authz.Action) error {
	_, err := authz.AuthorizeStaffAction(
		ctx, service.authorizer, actor, action, authz.SiteScope(), service.now().UTC(), "wiki-administration",
	)
	return err
}

func normalizeListInput(input ListInput, managed bool) (ListInput, error) {
	input.Query = strings.TrimSpace(input.Query)
	if input.Limit == 0 {
		input.Limit = DefaultPageLimit
	}
	if !managed {
		input.IncludeArchived = false
	}
	if !utf8.ValidString(input.Query) || utf8.RuneCountInString(input.Query) > MaximumQueryRunes ||
		input.Limit < 1 || input.Limit > MaximumPageLimit || input.Offset < 0 || input.Offset > MaximumPageOffset {
		return ListInput{}, ErrInput
	}
	return input, nil
}

func normalizeCreateInput(input CreateManagedInput) (CreateManagedInput, error) {
	input.Slug = normalizeSlug(input.Slug)
	input.Title = strings.TrimSpace(input.Title)
	input.Summary = strings.TrimSpace(input.Summary)
	input.Body = normalizeBody(input.Body)
	input.EditorNumericIDs = normalizeEditorIDs(input.EditorNumericIDs)
	input.Reason = normalizeReason(input.Reason)
	if !validManagedFields(input.Slug, input.Title, input.Summary, input.Body, input.Visibility, input.SortOrder, input.EditorNumericIDs, input.Reason) {
		return CreateManagedInput{}, ErrInput
	}
	return input, nil
}

func normalizeManagedInput(input UpdateManagedInput) (UpdateManagedInput, error) {
	created, err := normalizeCreateInput(CreateManagedInput{
		Slug: input.Slug, Title: input.Title, Summary: input.Summary, Body: input.Body,
		Visibility: input.Visibility, SortOrder: input.SortOrder,
		EditorNumericIDs: input.EditorNumericIDs, Reason: input.Reason,
	})
	if err != nil || input.PageID == uuid.Nil || input.ExpectedVersion < 1 {
		return UpdateManagedInput{}, ErrInput
	}
	input.Slug = created.Slug
	input.Title = created.Title
	input.Summary = created.Summary
	input.Body = created.Body
	input.Visibility = created.Visibility
	input.SortOrder = created.SortOrder
	input.EditorNumericIDs = created.EditorNumericIDs
	input.Reason = created.Reason
	return input, nil
}

func normalizeAssignedInput(input UpdateAssignedInput) (UpdateAssignedInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Summary = strings.TrimSpace(input.Summary)
	input.Body = normalizeBody(input.Body)
	input.Reason = normalizeReason(input.Reason)
	if input.PageID == uuid.Nil || input.ExpectedVersion < 1 || !validText(input.Title, 1, MaximumTitleRunes) ||
		!validText(input.Summary, 0, MaximumSummaryRunes) || !validBody(input.Body) || !validReason(input.Reason) {
		return UpdateAssignedInput{}, ErrInput
	}
	return input, nil
}

func validManagedFields(slug, title, summary, body string, visibility Visibility, sortOrder int, editorIDs []int64, reason string) bool {
	return validSlug(slug) && validText(title, 1, MaximumTitleRunes) && validText(summary, 0, MaximumSummaryRunes) &&
		validBody(body) && (visibility == VisibilityPublic || visibility == VisibilityMembers) &&
		sortOrder >= -100_000 && sortOrder <= 100_000 && validEditorIDs(editorIDs) && validReason(reason)
}

func normalizeSlug(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validSlug(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= MaximumSlugRunes && slugPattern.MatchString(value)
}

func normalizeBody(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}

func validBody(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= MaximumBodyRunes
}

func validText(value string, minimum, maximum int) bool {
	if !utf8.ValidString(value) {
		return false
	}
	count := utf8.RuneCountInString(value)
	return count >= minimum && count <= maximum
}

func normalizeEditorIDs(values []int64) []int64 {
	result := append([]int64(nil), values...)
	slices.Sort(result)
	return slices.Compact(result)
}

func validEditorIDs(values []int64) bool {
	if len(values) > MaximumEditors {
		return false
	}
	for _, value := range values {
		if value < 1 {
			return false
		}
	}
	return true
}

func normalizeReason(value string) string {
	return audittext.Reason(value)
}

func validReason(value string) bool {
	return validText(value, audittext.MinimumPersistedRunes, MaximumReasonRunes)
}
