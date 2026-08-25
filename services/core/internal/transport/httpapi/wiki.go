package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/wiki"
)

func (h *Handler) ListWikiPages(ctx context.Context, request generated.ListWikiPagesRequestObject) (generated.ListWikiPagesResponseObject, error) {
	page, err := h.wiki.List(ctx, sessionTokenFromContext(ctx), wiki.ListInput{
		Query: optionalWikiQuery(request.Params.Query),
		Limit: wikiLimit(request.Params.Limit), Offset: wikiOffset(request.Params.Offset),
	})
	if err != nil {
		problem, handled := wikiProblem(ctx, err)
		if !handled {
			return nil, err
		}
		switch problem.Status {
		case http.StatusBadRequest:
			return generated.ListWikiPages400ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
			}, nil
		case http.StatusUnauthorized:
			return generated.ListWikiPages401ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return generated.ListWikiPages403ApplicationProblemPlusJSONResponse(problem), nil
		}
	}
	return generated.ListWikiPages200JSONResponse(wikiPageListDTO(page)), nil
}

func (h *Handler) GetWikiPage(ctx context.Context, request generated.GetWikiPageRequestObject) (generated.GetWikiPageResponseObject, error) {
	page, err := h.wiki.Get(ctx, sessionTokenFromContext(ctx), string(request.Slug))
	if err != nil {
		problem, handled := wikiProblem(ctx, err)
		if !handled {
			return nil, err
		}
		switch problem.Status {
		case http.StatusBadRequest:
			return generated.GetWikiPage400ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
			}, nil
		case http.StatusUnauthorized:
			return generated.GetWikiPage401ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusForbidden:
			return generated.GetWikiPage403ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return generated.GetWikiPage404ApplicationProblemPlusJSONResponse(problem), nil
		}
	}
	return generated.GetWikiPage200JSONResponse(wikiPageDTO(page)), nil
}

func (h *Handler) UpdateAssignedWikiPage(ctx context.Context, request generated.UpdateAssignedWikiPageRequestObject) (generated.UpdateAssignedWikiPageResponseObject, error) {
	if request.Body == nil {
		problem := invalidWikiProblem(ctx)
		return generated.UpdateAssignedWikiPage400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	page, err := h.wiki.UpdateAssigned(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), wiki.UpdateAssignedInput{
		PageID: uuid.UUID(request.PageId), Title: request.Body.Title, Summary: request.Body.Summary,
		Body: request.Body.Body, ExpectedVersion: request.Body.ExpectedVersion, Reason: request.Body.Reason,
	})
	if err != nil {
		problem, handled := wikiProblem(ctx, err)
		if !handled {
			return nil, err
		}
		switch problem.Status {
		case http.StatusBadRequest:
			return generated.UpdateAssignedWikiPage400ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
			}, nil
		case http.StatusUnauthorized:
			return generated.UpdateAssignedWikiPage401ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusForbidden:
			return generated.UpdateAssignedWikiPage403ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusNotFound:
			return generated.UpdateAssignedWikiPage404ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return generated.UpdateAssignedWikiPage409ApplicationProblemPlusJSONResponse(problem), nil
		}
	}
	return generated.UpdateAssignedWikiPage200JSONResponse(wikiPageDTO(page)), nil
}

func (h *Handler) ListManagedWikiPages(ctx context.Context, request generated.ListManagedWikiPagesRequestObject) (generated.ListManagedWikiPagesResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListManagedWikiPages401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListManagedWikiPages403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	includeArchived := true
	if request.Params.IncludeArchived != nil {
		includeArchived = *request.Params.IncludeArchived
	}
	page, err := h.wiki.ListManaged(ctx, staffActor(session), wiki.ListInput{
		Query: optionalWikiQuery(request.Params.Query), Limit: wikiLimit(request.Params.Limit),
		Offset: wikiOffset(request.Params.Offset), IncludeArchived: includeArchived,
	})
	if err != nil {
		mapped, handled := wikiProblem(ctx, err)
		if !handled {
			return nil, err
		}
		if mapped.Status == http.StatusBadRequest {
			return generated.ListManagedWikiPages400ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(mapped),
			}, nil
		}
		return generated.ListManagedWikiPages403ApplicationProblemPlusJSONResponse(mapped), nil
	}
	return generated.ListManagedWikiPages200JSONResponse(wikiPageListDTO(page)), nil
}

func (h *Handler) CreateManagedWikiPage(ctx context.Context, request generated.CreateManagedWikiPageRequestObject) (generated.CreateManagedWikiPageResponseObject, error) {
	if request.Body == nil {
		problem := invalidWikiProblem(ctx)
		return generated.CreateManagedWikiPage400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.CreateManagedWikiPage401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.CreateManagedWikiPage403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	page, err := h.wiki.CreateManaged(ctx, staffActor(session), wiki.CreateManagedInput{
		Slug: request.Body.Slug, Title: request.Body.Title, Summary: request.Body.Summary,
		Body: request.Body.Body, Visibility: wiki.Visibility(request.Body.Visibility),
		SortOrder: request.Body.SortOrder, EditorNumericIDs: request.Body.EditorNumericIds,
		Reason: request.Body.Reason,
	})
	if err != nil {
		mapped, handled := wikiProblem(ctx, err)
		if !handled {
			return nil, err
		}
		switch mapped.Status {
		case http.StatusBadRequest:
			return generated.CreateManagedWikiPage400ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(mapped),
			}, nil
		case http.StatusForbidden:
			return generated.CreateManagedWikiPage403ApplicationProblemPlusJSONResponse(mapped), nil
		default:
			return generated.CreateManagedWikiPage409ApplicationProblemPlusJSONResponse(mapped), nil
		}
	}
	return generated.CreateManagedWikiPage201JSONResponse(wikiPageDTO(page)), nil
}

func (h *Handler) GetManagedWikiPage(ctx context.Context, request generated.GetManagedWikiPageRequestObject) (generated.GetManagedWikiPageResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.GetManagedWikiPage401ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem),
			}, nil
		}
		return generated.GetManagedWikiPage403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	page, err := h.wiki.GetManaged(ctx, staffActor(session), uuid.UUID(request.PageId))
	if err != nil {
		mapped, handled := wikiProblem(ctx, err)
		if !handled {
			return nil, err
		}
		if mapped.Status == http.StatusNotFound || mapped.Status == http.StatusBadRequest {
			return generated.GetManagedWikiPage404ApplicationProblemPlusJSONResponse(mapped), nil
		}
		return generated.GetManagedWikiPage403ApplicationProblemPlusJSONResponse(mapped), nil
	}
	return generated.GetManagedWikiPage200JSONResponse(wikiPageDTO(page)), nil
}

func (h *Handler) UpdateManagedWikiPage(ctx context.Context, request generated.UpdateManagedWikiPageRequestObject) (generated.UpdateManagedWikiPageResponseObject, error) {
	if request.Body == nil {
		problem := invalidWikiProblem(ctx)
		return generated.UpdateManagedWikiPage400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.UpdateManagedWikiPage401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.UpdateManagedWikiPage403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	page, err := h.wiki.UpdateManaged(ctx, staffActor(session), wiki.UpdateManagedInput{
		PageID: uuid.UUID(request.PageId), Slug: request.Body.Slug, Title: request.Body.Title,
		Summary: request.Body.Summary, Body: request.Body.Body,
		Visibility: wiki.Visibility(request.Body.Visibility), SortOrder: request.Body.SortOrder,
		Archived: request.Body.Archived, EditorNumericIDs: request.Body.EditorNumericIds,
		ExpectedVersion: request.Body.ExpectedVersion, Reason: request.Body.Reason,
	})
	if err != nil {
		mapped, handled := wikiProblem(ctx, err)
		if !handled {
			return nil, err
		}
		switch mapped.Status {
		case http.StatusBadRequest:
			return generated.UpdateManagedWikiPage400ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(mapped),
			}, nil
		case http.StatusForbidden:
			return generated.UpdateManagedWikiPage403ApplicationProblemPlusJSONResponse(mapped), nil
		case http.StatusNotFound:
			return generated.UpdateManagedWikiPage404ApplicationProblemPlusJSONResponse(mapped), nil
		default:
			return generated.UpdateManagedWikiPage409ApplicationProblemPlusJSONResponse(mapped), nil
		}
	}
	return generated.UpdateManagedWikiPage200JSONResponse(wikiPageDTO(page)), nil
}

func (h *Handler) ListManagedWikiRevisions(ctx context.Context, request generated.ListManagedWikiRevisionsRequestObject) (generated.ListManagedWikiRevisionsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListManagedWikiRevisions401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListManagedWikiRevisions403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	limit := wiki.MaximumRevisions
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	page, err := h.wiki.Revisions(ctx, staffActor(session), uuid.UUID(request.PageId), limit, wikiOffset(request.Params.Offset))
	if err != nil {
		mapped, handled := wikiProblem(ctx, err)
		if !handled {
			return nil, err
		}
		if mapped.Status == http.StatusBadRequest {
			return generated.ListManagedWikiRevisions400ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(mapped),
			}, nil
		}
		if mapped.Status == http.StatusNotFound {
			return generated.ListManagedWikiRevisions404ApplicationProblemPlusJSONResponse(mapped), nil
		}
		return generated.ListManagedWikiRevisions403ApplicationProblemPlusJSONResponse(mapped), nil
	}
	return generated.ListManagedWikiRevisions200JSONResponse(wikiRevisionPageDTO(page)), nil
}

func (h *Handler) RestoreManagedWikiRevision(ctx context.Context, request generated.RestoreManagedWikiRevisionRequestObject) (generated.RestoreManagedWikiRevisionResponseObject, error) {
	if request.Body == nil {
		problem := invalidWikiProblem(ctx)
		return generated.RestoreManagedWikiRevision400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.RestoreManagedWikiRevision401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.RestoreManagedWikiRevision403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	page, err := h.wiki.RestoreManaged(ctx, staffActor(session), wiki.RestoreManagedInput{
		PageID: uuid.UUID(request.PageId), RevisionNumber: int64(request.RevisionNumber),
		ExpectedVersion: request.Body.ExpectedVersion, Reason: request.Body.Reason,
	})
	if err != nil {
		mapped, handled := wikiProblem(ctx, err)
		if !handled {
			return nil, err
		}
		switch mapped.Status {
		case http.StatusBadRequest:
			return generated.RestoreManagedWikiRevision400ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(mapped),
			}, nil
		case http.StatusForbidden:
			return generated.RestoreManagedWikiRevision403ApplicationProblemPlusJSONResponse(mapped), nil
		case http.StatusNotFound:
			return generated.RestoreManagedWikiRevision404ApplicationProblemPlusJSONResponse(mapped), nil
		default:
			return generated.RestoreManagedWikiRevision409ApplicationProblemPlusJSONResponse(mapped), nil
		}
	}
	return generated.RestoreManagedWikiRevision200JSONResponse(wikiPageDTO(page)), nil
}

func wikiPageListDTO(page wiki.PageList) generated.WikiPageList {
	items := make([]generated.WikiPageSummary, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, generated.WikiPageSummary{
			Id: item.ID, Slug: item.Slug, Title: item.Title, Summary: item.Summary,
			Visibility: generated.WikiVisibility(item.Visibility), SortOrder: item.SortOrder,
			Version: item.Version, RevisionNumber: item.RevisionNumber, CanEdit: item.CanEdit,
			Archived: item.Archived, UpdatedAt: item.UpdatedAt,
		})
	}
	return generated.WikiPageList{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}
}

func wikiPageDTO(page wiki.Page) generated.WikiPage {
	editors := make([]generated.WikiUserReference, 0, len(page.Editors))
	for _, editor := range page.Editors {
		editors = append(editors, wikiUserReferenceDTO(editor))
	}
	return generated.WikiPage{
		Id: page.ID, Slug: page.Slug, Title: page.Title, Summary: page.Summary, Body: page.Body,
		Visibility: generated.WikiVisibility(page.Visibility), SortOrder: page.SortOrder,
		Version: page.Version, RevisionNumber: page.RevisionNumber, CanEdit: page.CanEdit,
		Archived: page.ArchivedAt != nil, Creator: wikiUserReferenceDTO(page.Creator),
		Updater: wikiUserReferenceDTO(page.Updater), Editors: editors, Migrated: page.Migrated,
		CreatedAt: page.CreatedAt, UpdatedAt: page.UpdatedAt, ArchivedAt: page.ArchivedAt,
	}
}

func wikiUserReferenceDTO(user wiki.UserReference) generated.WikiUserReference {
	return generated.WikiUserReference{NumericId: user.NumericID, Username: user.Username, DisplayName: user.DisplayName}
}

func wikiRevisionPageDTO(page wiki.RevisionPage) generated.WikiRevisionPage {
	items := make([]generated.WikiRevisionSummary, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, generated.WikiRevisionSummary{
			RevisionNumber: item.RevisionNumber, Title: item.Title, Reason: item.Reason,
			Origin: generated.WikiRevisionOrigin(item.Origin), Editor: wikiUserReferenceDTO(item.Editor),
			CreatedAt: item.CreatedAt,
		})
	}
	return generated.WikiRevisionPage{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}
}

func wikiProblem(ctx context.Context, err error) (generated.Problem, bool) {
	switch {
	case errors.Is(err, wiki.ErrInput):
		return invalidWikiProblem(ctx), true
	case errors.Is(err, identity.ErrSessionNotFound):
		return newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后访问成员 Wiki。"), true
	case errors.Is(err, identity.ErrInvalidCSRF):
		return newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。"), true
	case errors.Is(err, authz.ErrForbidden), errors.Is(err, wiki.ErrEditDenied):
		return newProblemFromContext(ctx, http.StatusForbidden, "wiki_access_denied", "没有 Wiki 操作权限", "当前身份不能执行这项 Wiki 操作。"), true
	case errors.Is(err, wiki.ErrPageNotFound), errors.Is(err, wiki.ErrRevisionNotFound):
		return newProblemFromContext(ctx, http.StatusNotFound, "wiki_not_found", "Wiki 内容不存在", "目标页面或修订不存在，或者当前身份不可见。"), true
	case errors.Is(err, wiki.ErrPageExists):
		return newProblemFromContext(ctx, http.StatusConflict, "wiki_slug_exists", "Wiki 路由键已被使用", "请换一个路由键后重试。"), true
	case errors.Is(err, wiki.ErrVersionConflict):
		return newProblemFromContext(ctx, http.StatusConflict, "wiki_version_conflict", "Wiki 已被其他人修改", "请重新载入最新版本后再提交。"), true
	case errors.Is(err, wiki.ErrNoChanges):
		return newProblemFromContext(ctx, http.StatusConflict, "wiki_no_changes", "Wiki 内容没有变化", "没有必要保存一个空修订。"), true
	case errors.Is(err, wiki.ErrEditorNotFound):
		return newProblemFromContext(ctx, http.StatusBadRequest, "wiki_editor_not_found", "协作者不存在", "请检查协作者数字 ID，且只能选择当前有效账户。"), true
	default:
		return generated.Problem{}, false
	}
}

func invalidWikiProblem(ctx context.Context) generated.Problem {
	return newProblemFromContext(ctx, http.StatusBadRequest, "invalid_wiki", "Wiki 内容无效", "请检查路由键、标题、摘要、正文、排序、协作者和版本。")
}

func optionalWikiQuery(value *generated.WikiQueryParameter) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func wikiLimit(value *generated.WikiLimitParameter) int {
	if value == nil {
		return wiki.DefaultPageLimit
	}
	return int(*value)
}

func wikiOffset(value *generated.WikiOffsetParameter) int {
	if value == nil {
		return 0
	}
	return int(*value)
}
