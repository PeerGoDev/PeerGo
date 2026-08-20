package httpapi

import (
	"context"
	"errors"
	"net/http"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
)

func (h *Handler) ListManagedAnnouncements(ctx context.Context, request generated.ListManagedAnnouncementsRequestObject) (generated.ListManagedAnnouncementsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListManagedAnnouncements401ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem),
			}, nil
		}
		return generated.ListManagedAnnouncements403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	limit, offset := announcementPageParams(request.Params.Limit, request.Params.Offset)
	page, err := h.announcementAdministration.List(ctx, staffActor(session), limit, offset)
	if err != nil {
		mapped, handled := announcementAdministrationProblem(ctx, err)
		if !handled {
			return nil, err
		}
		return generated.ListManagedAnnouncements403ApplicationProblemPlusJSONResponse(mapped), nil
	}
	return generated.ListManagedAnnouncements200JSONResponse(managedAnnouncementPageDTO(page)), nil
}

func (h *Handler) GetManagedAnnouncement(ctx context.Context, request generated.GetManagedAnnouncementRequestObject) (generated.GetManagedAnnouncementResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.GetManagedAnnouncement401ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem),
			}, nil
		}
		return generated.GetManagedAnnouncement403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	result, err := h.announcementAdministration.Get(ctx, staffActor(session), string(request.AnnouncementId))
	if err != nil {
		mapped, handled := announcementAdministrationProblem(ctx, err)
		if !handled {
			return nil, err
		}
		if mapped.Status == http.StatusNotFound || mapped.Status == http.StatusBadRequest {
			return generated.GetManagedAnnouncement404ApplicationProblemPlusJSONResponse(mapped), nil
		}
		return generated.GetManagedAnnouncement403ApplicationProblemPlusJSONResponse(mapped), nil
	}
	return generated.GetManagedAnnouncement200JSONResponse(managedAnnouncementDTO(result)), nil
}

func (h *Handler) ListManagedAnnouncementRevisions(ctx context.Context, request generated.ListManagedAnnouncementRevisionsRequestObject) (generated.ListManagedAnnouncementRevisionsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListManagedAnnouncementRevisions401ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem),
			}, nil
		}
		return generated.ListManagedAnnouncementRevisions403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	limit, offset := announcementPageParams(request.Params.Limit, request.Params.Offset)
	page, err := h.announcementAdministration.Revisions(ctx, staffActor(session), string(request.AnnouncementId), limit, offset)
	if err != nil {
		mapped, handled := announcementAdministrationProblem(ctx, err)
		if !handled {
			return nil, err
		}
		if mapped.Status == http.StatusNotFound || mapped.Status == http.StatusBadRequest {
			return generated.ListManagedAnnouncementRevisions404ApplicationProblemPlusJSONResponse(mapped), nil
		}
		return generated.ListManagedAnnouncementRevisions403ApplicationProblemPlusJSONResponse(mapped), nil
	}
	return generated.ListManagedAnnouncementRevisions200JSONResponse(announcementRevisionPageDTO(page)), nil
}

func (h *Handler) CreateManagedAnnouncement(ctx context.Context, request generated.CreateManagedAnnouncementRequestObject) (generated.CreateManagedAnnouncementResponseObject, error) {
	if request.Body == nil {
		problem := invalidAnnouncementProblem(ctx)
		return generated.CreateManagedAnnouncement400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.CreateManagedAnnouncement401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.CreateManagedAnnouncement403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	result, err := h.announcementAdministration.Create(ctx, staffActor(session), catalog.CreateAnnouncementDraftInput{
		ID: request.Body.Id, Title: request.Body.Title, Summary: request.Body.Summary,
		Body: request.Body.Body, BodyFormat: catalog.AnnouncementBodyFormat(request.Body.BodyFormat),
		Reason: request.Body.Reason,
	})
	if err != nil {
		mapped, handled := announcementAdministrationProblem(ctx, err)
		if !handled {
			return nil, err
		}
		switch mapped.Status {
		case http.StatusBadRequest:
			return generated.CreateManagedAnnouncement400ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(mapped),
			}, nil
		case http.StatusForbidden:
			return generated.CreateManagedAnnouncement403ApplicationProblemPlusJSONResponse(mapped), nil
		default:
			return generated.CreateManagedAnnouncement409ApplicationProblemPlusJSONResponse(mapped), nil
		}
	}
	return generated.CreateManagedAnnouncement201JSONResponse(managedAnnouncementDTO(result)), nil
}

func (h *Handler) UpdateManagedAnnouncementDraft(ctx context.Context, request generated.UpdateManagedAnnouncementDraftRequestObject) (generated.UpdateManagedAnnouncementDraftResponseObject, error) {
	if request.Body == nil {
		problem := invalidAnnouncementProblem(ctx)
		return generated.UpdateManagedAnnouncementDraft400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.UpdateManagedAnnouncementDraft401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.UpdateManagedAnnouncementDraft403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	result, err := h.announcementAdministration.UpdateDraft(ctx, staffActor(session), catalog.UpdateAnnouncementDraftInput{
		ID: string(request.AnnouncementId), Title: request.Body.Title, Summary: request.Body.Summary,
		Body: request.Body.Body, BodyFormat: catalog.AnnouncementBodyFormat(request.Body.BodyFormat),
		ExpectedVersion: request.Body.ExpectedVersion, Reason: request.Body.Reason,
	})
	if err != nil {
		mapped, handled := announcementAdministrationProblem(ctx, err)
		if !handled {
			return nil, err
		}
		switch mapped.Status {
		case http.StatusBadRequest:
			return generated.UpdateManagedAnnouncementDraft400ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(mapped),
			}, nil
		case http.StatusForbidden:
			return generated.UpdateManagedAnnouncementDraft403ApplicationProblemPlusJSONResponse(mapped), nil
		case http.StatusNotFound:
			return generated.UpdateManagedAnnouncementDraft404ApplicationProblemPlusJSONResponse(mapped), nil
		default:
			return generated.UpdateManagedAnnouncementDraft409ApplicationProblemPlusJSONResponse(mapped), nil
		}
	}
	return generated.UpdateManagedAnnouncementDraft200JSONResponse(managedAnnouncementDTO(result)), nil
}

func (h *Handler) ChangeManagedAnnouncementPublication(ctx context.Context, request generated.ChangeManagedAnnouncementPublicationRequestObject) (generated.ChangeManagedAnnouncementPublicationResponseObject, error) {
	if request.Body == nil {
		problem := invalidAnnouncementProblem(ctx)
		return generated.ChangeManagedAnnouncementPublication400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ChangeManagedAnnouncementPublication401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ChangeManagedAnnouncementPublication403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	result, err := h.announcementAdministration.ChangePublication(ctx, staffActor(session), catalog.ChangeAnnouncementPublicationInput{
		ID: string(request.AnnouncementId), Action: catalog.AnnouncementPublicationAction(request.Body.Action),
		ExpectedVersion: request.Body.ExpectedVersion, ScheduledFor: request.Body.ScheduledFor,
		Reason: request.Body.Reason,
	})
	if err != nil {
		mapped, handled := announcementAdministrationProblem(ctx, err)
		if !handled {
			return nil, err
		}
		switch mapped.Status {
		case http.StatusBadRequest:
			return generated.ChangeManagedAnnouncementPublication400ApplicationProblemPlusJSONResponse{
				ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(mapped),
			}, nil
		case http.StatusForbidden:
			return generated.ChangeManagedAnnouncementPublication403ApplicationProblemPlusJSONResponse(mapped), nil
		case http.StatusNotFound:
			return generated.ChangeManagedAnnouncementPublication404ApplicationProblemPlusJSONResponse(mapped), nil
		default:
			return generated.ChangeManagedAnnouncementPublication409ApplicationProblemPlusJSONResponse(mapped), nil
		}
	}
	return generated.ChangeManagedAnnouncementPublication200JSONResponse(managedAnnouncementDTO(result)), nil
}

func managedAnnouncementPageDTO(page catalog.ManagedAnnouncementPage) generated.ManagedAnnouncementPage {
	items := make([]generated.ManagedAnnouncementSummary, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, managedAnnouncementSummaryDTO(item))
	}
	return generated.ManagedAnnouncementPage{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}
}

func managedAnnouncementSummaryDTO(value catalog.ManagedAnnouncement) generated.ManagedAnnouncementSummary {
	return generated.ManagedAnnouncementSummary{
		Id: value.ID, Title: value.Title, Summary: value.Summary,
		Status: generated.ManagedAnnouncementStatus(value.Status), Version: value.Version,
		RevisionNumber: value.RevisionNumber, HasUnpublishedChanges: value.HasUnpublishedChanges,
		PublishedAt: value.PublishedAt, ScheduledFor: value.ScheduledFor,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func managedAnnouncementDTO(value catalog.ManagedAnnouncement) generated.ManagedAnnouncement {
	return generated.ManagedAnnouncement{
		Id: value.ID, Title: value.Title, Summary: value.Summary, Body: value.Body,
		BodyFormat: generated.AnnouncementBodyFormat(value.BodyFormat),
		Status:     generated.ManagedAnnouncementStatus(value.Status), Version: value.Version,
		RevisionNumber: value.RevisionNumber, HasUnpublishedChanges: value.HasUnpublishedChanges,
		PublishedAt: value.PublishedAt, ScheduledFor: value.ScheduledFor,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func announcementRevisionPageDTO(page catalog.AnnouncementRevisionPage) generated.AnnouncementRevisionPage {
	items := make([]generated.AnnouncementRevisionSummary, 0, len(page.Items))
	for _, item := range page.Items {
		var editor *string
		if item.EditorDisplayName != "" {
			value := item.EditorDisplayName
			editor = &value
		}
		items = append(items, generated.AnnouncementRevisionSummary{
			RevisionNumber: item.RevisionNumber, Title: item.Title, Summary: item.Summary,
			BodyFormat: generated.AnnouncementBodyFormat(item.BodyFormat),
			Origin:     generated.AnnouncementRevisionSummaryOrigin(item.Origin), EditorDisplayName: editor,
			IsDraft: item.IsDraft, IsPublished: item.IsPublished, IsScheduled: item.IsScheduled,
			CreatedAt: item.CreatedAt,
		})
	}
	return generated.AnnouncementRevisionPage{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}
}

func announcementAdministrationProblem(ctx context.Context, err error) (generated.Problem, bool) {
	switch {
	case errors.Is(err, catalog.ErrAnnouncementAdministrationInput):
		return invalidAnnouncementProblem(ctx), true
	case errors.Is(err, authz.ErrForbidden):
		return newProblemFromContext(ctx, http.StatusForbidden, "announcement_change_denied", "没有权限管理公告", "当前后台身份缺少对应的 typed permission。"), true
	case errors.Is(err, catalog.ErrManagedAnnouncementNotFound):
		return newProblemFromContext(ctx, http.StatusNotFound, "announcement_not_found", "公告不存在", "目标公告已经不存在，请刷新列表。"), true
	case errors.Is(err, catalog.ErrAnnouncementAlreadyExists):
		return newProblemFromContext(ctx, http.StatusConflict, "announcement_exists", "公告标识已被使用", "公开路由键创建后不可复用或修改。"), true
	case errors.Is(err, catalog.ErrAnnouncementVersionConflict):
		return newProblemFromContext(ctx, http.StatusConflict, "announcement_version_conflict", "公告版本已经变化", "当前操作基于旧版本，请重新载入后再提交。"), true
	case errors.Is(err, catalog.ErrAnnouncementPublicationConflict):
		return newProblemFromContext(ctx, http.StatusConflict, "announcement_publication_conflict", "当前发布状态不接受该操作", "请核对草稿、排期或公开状态后重试。"), true
	case errors.Is(err, catalog.ErrAnnouncementNoChanges):
		return newProblemFromContext(ctx, http.StatusConflict, "announcement_no_changes", "正文没有变化", "标题、摘要、正文和格式均未变化，无需创建空修订。"), true
	default:
		return generated.Problem{}, false
	}
}

func invalidAnnouncementProblem(ctx context.Context) generated.Problem {
	return newProblemFromContext(ctx, http.StatusBadRequest, "invalid_announcement", "公告内容无效", "请检查路由键、正文、版本、排期时间和变更理由。")
}
