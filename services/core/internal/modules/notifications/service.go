package notifications

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/ratiowatch"
	"github.com/peergo/peergo/services/core/internal/modules/review"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/modules/traffic"
)

type Repository interface {
	List(context.Context, uuid.UUID, ListQuery) (Page, error)
	Summary(context.Context, uuid.UUID) (Summary, error)
	MarkRead(context.Context, uuid.UUID, uuid.UUID, time.Time) (ReadReceipt, error)
	MarkAllRead(context.Context, uuid.UUID, time.Time) (ReadAllReceipt, error)
	ArchiveAll(context.Context, uuid.UUID, time.Time) (ArchiveAllReceipt, error)
	CreateFeedback(context.Context, uuid.UUID, CreateFeedbackInput, time.Time) (FeedbackReceipt, error)
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
		return nil, errors.New("notification service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{authenticator: authenticator, authorizer: authorizer, repository: repository, now: now}, nil
}

func (service *Service) List(ctx context.Context, cookieToken string, query ListQuery) (Page, error) {
	if query.Limit < 1 || query.Limit > MaximumLimit || query.Offset < 0 || query.Offset > MaximumOffset {
		return Page{}, ErrInput
	}
	userID, err := service.authorizeRead(ctx, cookieToken)
	if err != nil {
		return Page{}, err
	}
	page, err := service.repository.List(ctx, userID, query)
	if err != nil {
		return Page{}, err
	}
	if page.Total < 0 || page.UnreadCount < 0 || page.UnreadCount > page.Total ||
		len(page.Items) > query.Limit || (len(page.Items) > 0 && query.Offset+len(page.Items) > page.Total) {
		return Page{}, ErrInvariant
	}
	page.Limit, page.Offset = query.Limit, query.Offset
	for index := range page.Items {
		if !validNotification(page.Items[index]) {
			return Page{}, ErrInvariant
		}
	}
	return page, nil
}

func (service *Service) Summary(ctx context.Context, cookieToken string) (Summary, error) {
	userID, err := service.authorizeRead(ctx, cookieToken)
	if err != nil {
		return Summary{}, err
	}
	summary, err := service.repository.Summary(ctx, userID)
	if err != nil {
		return Summary{}, err
	}
	if summary.UnreadCount < 0 {
		return Summary{}, ErrInvariant
	}
	return summary, nil
}

func (service *Service) MarkRead(ctx context.Context, cookieToken, csrfToken string, notificationID uuid.UUID) (ReadReceipt, error) {
	if notificationID == uuid.Nil {
		return ReadReceipt{}, ErrInput
	}
	userID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken, authz.ActionNotificationReadStateWriteSelf)
	if err != nil {
		return ReadReceipt{}, err
	}
	receipt, err := service.repository.MarkRead(ctx, userID, notificationID, now)
	if err != nil {
		return ReadReceipt{}, err
	}
	if receipt.NotificationID != notificationID || receipt.ReadAt.IsZero() || receipt.ReadAt.After(now) {
		return ReadReceipt{}, ErrInvariant
	}
	return receipt, nil
}

func (service *Service) MarkAllRead(ctx context.Context, cookieToken, csrfToken string) (ReadAllReceipt, error) {
	userID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken, authz.ActionNotificationReadStateWriteSelf)
	if err != nil {
		return ReadAllReceipt{}, err
	}
	receipt, err := service.repository.MarkAllRead(ctx, userID, now)
	if err != nil {
		return ReadAllReceipt{}, err
	}
	if receipt.UpdatedCount < 0 || receipt.ReadAt.IsZero() || receipt.ReadAt.After(now) {
		return ReadAllReceipt{}, ErrInvariant
	}
	return receipt, nil
}

// ArchiveAll removes messages from the user's inbox without deleting the
// immutable review notification or its audit source. The repository advances a
// monotonic archived_at marker so a retry is safe and an archive cannot undo.
func (service *Service) ArchiveAll(ctx context.Context, cookieToken, csrfToken string) (ArchiveAllReceipt, error) {
	userID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken, authz.ActionNotificationArchiveSelf)
	if err != nil {
		return ArchiveAllReceipt{}, err
	}
	receipt, err := service.repository.ArchiveAll(ctx, userID, now)
	if err != nil {
		return ArchiveAllReceipt{}, err
	}
	if receipt.UpdatedCount < 0 || receipt.ArchivedAt.IsZero() || receipt.ArchivedAt.After(now) {
		return ArchiveAllReceipt{}, ErrInvariant
	}
	return receipt, nil
}

func (service *Service) CreateFeedback(ctx context.Context, cookieToken, csrfToken string, input CreateFeedbackInput) (FeedbackReceipt, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Content = strings.TrimSpace(input.Content)
	if !utf8.ValidString(input.Title) || !utf8.ValidString(input.Content) ||
		utf8.RuneCountInString(input.Title) < 1 || utf8.RuneCountInString(input.Title) > 100 ||
		utf8.RuneCountInString(input.Content) < 1 || utf8.RuneCountInString(input.Content) > 2000 {
		return FeedbackReceipt{}, ErrInput
	}
	userID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken, authz.ActionNotificationFeedbackCreateSelf)
	if err != nil {
		return FeedbackReceipt{}, err
	}
	receipt, err := service.repository.CreateFeedback(ctx, userID, input, now)
	if err != nil {
		return FeedbackReceipt{}, err
	}
	if receipt.FeedbackID == uuid.Nil || receipt.CreatedAt.IsZero() || receipt.CreatedAt.After(now) {
		return FeedbackReceipt{}, ErrInvariant
	}
	return receipt, nil
}

func (service *Service) authorizeRead(ctx context.Context, cookieToken string) (uuid.UUID, error) {
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionNotificationReadSelf, service.now().UTC()); err != nil {
		return uuid.Nil, err
	}
	return session.User.ID, nil
}

func (service *Service) authorizeWrite(ctx context.Context, cookieToken, csrfToken string, action authz.Action) (uuid.UUID, time.Time, error) {
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return uuid.Nil, time.Time{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, action, now); err != nil {
		return uuid.Nil, time.Time{}, err
	}
	return session.User.ID, now, nil
}

func validNotification(notification Notification) bool {
	if notification.ID == uuid.Nil || notification.CreatedAt.IsZero() ||
		(notification.ReadAt != nil && notification.ReadAt.Before(notification.CreatedAt)) {
		return false
	}
	if notificationPayloadCount(notification) != 1 {
		return false
	}
	switch notification.Kind {
	case KindTorrentReview:
		return validTorrentReviewNotification(notification.TorrentReview)
	case KindRatioWatch:
		return validRatioWatchNotification(notification.RatioWatch)
	case KindRatioAppeal:
		return validRatioAppealNotification(notification.RatioAppeal)
	case KindHNR:
		return validHNRNotification(notification.HNR)
	case KindHNRAppeal:
		return validHNRAppealNotification(notification.HNR)
	case KindWorkgroupContribution:
		return validWorkgroupContributionPayload(notification.WorkgroupContribution)
	case KindMemberGift:
		return validMemberGiftNotification(notification.MemberGift)
	default:
		return false
	}
}

func notificationPayloadCount(notification Notification) int {
	count := 0
	for _, present := range []bool{
		notification.TorrentReview != nil,
		notification.RatioWatch != nil,
		notification.RatioAppeal != nil,
		notification.HNR != nil,
		notification.WorkgroupContribution != nil,
		notification.MemberGift != nil,
	} {
		if present {
			count++
		}
	}
	return count
}

func validMemberGiftNotification(notification *MemberGiftNotification) bool {
	if notification == nil || notification.SenderNumericID < 1 || notification.NetAmount < 1 {
		return false
	}
	usernameRunes := utf8.RuneCountInString(notification.SenderUsername)
	displayNameRunes := utf8.RuneCountInString(notification.SenderDisplayName)
	messageRunes := utf8.RuneCountInString(notification.Message)
	return utf8.ValidString(notification.SenderUsername) &&
		notification.SenderUsername == strings.TrimSpace(notification.SenderUsername) &&
		usernameRunes >= 1 && usernameRunes <= 64 &&
		utf8.ValidString(notification.SenderDisplayName) &&
		notification.SenderDisplayName == strings.TrimSpace(notification.SenderDisplayName) &&
		displayNameRunes >= 1 && displayNameRunes <= 80 &&
		utf8.ValidString(notification.Message) &&
		notification.Message == strings.TrimSpace(notification.Message) && messageRunes <= 200
}

func validHNRNotification(notification *HNRNotification) bool {
	if !validHNRNotificationTarget(notification) || notification.Response != "" {
		return false
	}
	switch notification.Event {
	case traffic.HNRNotificationGraceStarted,
		traffic.HNRNotificationDownloadRestricted,
		traffic.HNRNotificationSatisfied:
		return true
	default:
		return false
	}
}

func validHNRAppealNotification(notification *HNRNotification) bool {
	if !validHNRNotificationTarget(notification) {
		return false
	}
	responseRunes := utf8.RuneCountInString(notification.Response)
	if !utf8.ValidString(notification.Response) || notification.Response != strings.TrimSpace(notification.Response) ||
		responseRunes < 10 || responseRunes > 1000 {
		return false
	}
	return notification.Event == traffic.HNRNotificationAppealApproved ||
		notification.Event == traffic.HNRNotificationAppealRejected
}

func validHNRNotificationTarget(notification *HNRNotification) bool {
	if notification == nil || notification.TorrentID < 1 || notification.GraceEndsAt.IsZero() ||
		!utf8.ValidString(notification.TorrentTitle) ||
		notification.TorrentTitle != strings.TrimSpace(notification.TorrentTitle) ||
		utf8.RuneCountInString(notification.TorrentTitle) < 1 ||
		utf8.RuneCountInString(notification.TorrentTitle) > 240 {
		return false
	}
	return true
}

func validTorrentReviewNotification(notification *TorrentReviewNotification) bool {
	if notification == nil {
		return false
	}
	titleRunes := utf8.RuneCountInString(notification.TorrentTitle)
	reasonRunes := utf8.RuneCountInString(notification.Reason)
	if notification.TorrentID < 1 || !utf8.ValidString(notification.TorrentTitle) ||
		notification.TorrentTitle != strings.TrimSpace(notification.TorrentTitle) || titleRunes < 1 || titleRunes > 240 ||
		!utf8.ValidString(notification.Reason) || notification.Reason != strings.TrimSpace(notification.Reason) ||
		reasonRunes < 10 || reasonRunes > 1000 {
		return false
	}
	switch notification.Outcome {
	case torrents.StatePublished:
		if notification.ReasonCode != review.ReasonMeetsRequirements {
			return false
		}
	case torrents.StateRejected:
		if !validRejectionReasonCode(notification.ReasonCode) {
			return false
		}
	default:
		return false
	}
	return true
}

func validRatioWatchNotification(notification *RatioWatchNotification) bool {
	if notification == nil || notification.DeadlineAt.IsZero() ||
		notification.RatioBasisPoints < 0 || notification.RatioBasisPoints > 1_000_000 ||
		notification.MinimumRatioBasisPoints < 1 || notification.MinimumRatioBasisPoints > 1_000_000 ||
		notification.RestrictionRatioBasisPoints < 1 ||
		notification.RestrictionRatioBasisPoints > notification.MinimumRatioBasisPoints {
		return false
	}
	switch notification.Status {
	case ratiowatch.AssessmentWatching, ratiowatch.AssessmentWarning,
		ratiowatch.AssessmentDownloadRestricted, ratiowatch.AssessmentSatisfied,
		ratiowatch.AssessmentManuallyCleared:
		return true
	default:
		return false
	}
}

func validRatioAppealNotification(notification *RatioAppealNotification) bool {
	if notification == nil || notification.Status != ratiowatch.AppealRejected {
		return false
	}
	responseRunes := utf8.RuneCountInString(notification.Response)
	return utf8.ValidString(notification.Response) && notification.Response == strings.TrimSpace(notification.Response) &&
		responseRunes >= 10 && responseRunes <= 1000
}

func validRejectionReasonCode(code review.ReasonCode) bool {
	switch code {
	case review.ReasonMetadataIncomplete, review.ReasonDuplicateOrSuperseded,
		review.ReasonContentPolicyViolation, review.ReasonQualityRequirementsNotMet,
		review.ReasonUploaderActionRequired, review.ReasonOther:
		return true
	default:
		return false
	}
}
