package notifications

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/ratiowatch"
	"github.com/peergo/peergo/services/core/internal/modules/review"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/modules/traffic"
	"github.com/peergo/peergo/services/core/internal/modules/workgroups"
)

type notificationAuthenticatorFixture struct {
	session       identity.WebSession
	currentCalls  int
	writeCalls    int
	currentCookie string
	writeCookie   string
	writeCSRF     string
}

func (fixture *notificationAuthenticatorFixture) CurrentSession(_ context.Context, cookie string) (identity.WebSession, error) {
	fixture.currentCalls++
	fixture.currentCookie = cookie
	return fixture.session, nil
}

func (fixture *notificationAuthenticatorFixture) AuthenticateWrite(_ context.Context, cookie, csrf string) (identity.WebSession, error) {
	fixture.writeCalls++
	fixture.writeCookie, fixture.writeCSRF = cookie, csrf
	return fixture.session, nil
}

type notificationAuthorizerFixture struct {
	requests []authz.Request
}

func (fixture *notificationAuthorizerFixture) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	fixture.requests = append(fixture.requests, request)
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: "test",
		GrantID: uuid.New(), GrantVersion: 1, RoleID: "member", MandateID: uuid.New(),
		EffectiveUntil: request.Context.Now.Add(time.Hour),
	}, nil
}

type notificationRepositoryFixture struct {
	page            Page
	summary         Summary
	readReceipt     ReadReceipt
	readAllReceipt  ReadAllReceipt
	archiveReceipt  ArchiveAllReceipt
	feedbackReceipt FeedbackReceipt
	listUserID      uuid.UUID
	listQuery       ListQuery
	readUserID      uuid.UUID
	readID          uuid.UUID
	readAt          time.Time
	readAllUserID   uuid.UUID
	readAllAt       time.Time
	archiveUserID   uuid.UUID
	archiveAt       time.Time
	feedbackUserID  uuid.UUID
	feedbackInput   CreateFeedbackInput
	feedbackAt      time.Time
}

func (fixture *notificationRepositoryFixture) List(_ context.Context, userID uuid.UUID, query ListQuery) (Page, error) {
	fixture.listUserID, fixture.listQuery = userID, query
	return fixture.page, nil
}

func (fixture *notificationRepositoryFixture) Summary(context.Context, uuid.UUID) (Summary, error) {
	return fixture.summary, nil
}

func (fixture *notificationRepositoryFixture) MarkRead(_ context.Context, userID, notificationID uuid.UUID, readAt time.Time) (ReadReceipt, error) {
	fixture.readUserID, fixture.readID, fixture.readAt = userID, notificationID, readAt
	return fixture.readReceipt, nil
}

func (fixture *notificationRepositoryFixture) MarkAllRead(_ context.Context, userID uuid.UUID, readAt time.Time) (ReadAllReceipt, error) {
	fixture.readAllUserID, fixture.readAllAt = userID, readAt
	return fixture.readAllReceipt, nil
}

func (fixture *notificationRepositoryFixture) ArchiveAll(_ context.Context, userID uuid.UUID, archivedAt time.Time) (ArchiveAllReceipt, error) {
	fixture.archiveUserID, fixture.archiveAt = userID, archivedAt
	return fixture.archiveReceipt, nil
}

func (fixture *notificationRepositoryFixture) CreateFeedback(_ context.Context, userID uuid.UUID, input CreateFeedbackInput, createdAt time.Time) (FeedbackReceipt, error) {
	fixture.feedbackUserID, fixture.feedbackInput, fixture.feedbackAt = userID, input, createdAt
	return fixture.feedbackReceipt, nil
}

func TestServiceSeparatesNotificationReadAndCSRFBoundReadState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	userID, notificationID := uuid.New(), uuid.New()
	authenticator := &notificationAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: userID}}}
	authorizer := &notificationAuthorizerFixture{}
	repository := &notificationRepositoryFixture{
		page: Page{
			Items: []Notification{{
				ID: notificationID, Kind: KindTorrentReview, CreatedAt: now.Add(-time.Hour),
				TorrentReview: &TorrentReviewNotification{
					TorrentID: 44, TorrentTitle: "Release 2026", Outcome: torrents.StateRejected,
					ReasonCode: review.ReasonMetadataIncomplete, Reason: "请补充完整的发布说明后重新提交。",
				},
			}},
			Total: 1, UnreadCount: 1,
		},
		readReceipt:     ReadReceipt{NotificationID: notificationID, ReadAt: now},
		readAllReceipt:  ReadAllReceipt{UpdatedCount: 1, ReadAt: now},
		archiveReceipt:  ArchiveAllReceipt{UpdatedCount: 1, ArchivedAt: now},
		feedbackReceipt: FeedbackReceipt{FeedbackID: uuid.New(), CreatedAt: now},
	}
	service, err := NewService(authenticator, authorizer, repository, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	query := ListQuery{Limit: 20, Offset: 0, UnreadOnly: true}
	page, err := service.List(context.Background(), "read-cookie", query)
	if err != nil || len(page.Items) != 1 || page.UnreadCount != 1 || page.Limit != 20 {
		t.Fatalf("List() page=%+v error=%v", page, err)
	}
	if repository.listUserID != userID || repository.listQuery != query || authenticator.currentCalls != 1 ||
		authorizer.requests[0].Action != authz.ActionNotificationReadSelf {
		t.Fatalf("read boundary repository=%+v authenticator=%+v requests=%+v", repository, authenticator, authorizer.requests)
	}

	receipt, err := service.MarkRead(context.Background(), "write-cookie", "csrf-token", notificationID)
	if err != nil || receipt.NotificationID != notificationID || !receipt.ReadAt.Equal(now) {
		t.Fatalf("MarkRead() receipt=%+v error=%v", receipt, err)
	}
	if authenticator.writeCalls != 1 || authenticator.writeCookie != "write-cookie" || authenticator.writeCSRF != "csrf-token" ||
		repository.readUserID != userID || repository.readID != notificationID ||
		authorizer.requests[1].Action != authz.ActionNotificationReadStateWriteSelf {
		t.Fatalf("write boundary repository=%+v authenticator=%+v requests=%+v", repository, authenticator, authorizer.requests)
	}

	archiveReceipt, err := service.ArchiveAll(context.Background(), "archive-cookie", "archive-csrf")
	if err != nil || archiveReceipt.UpdatedCount != 1 || !archiveReceipt.ArchivedAt.Equal(now) {
		t.Fatalf("ArchiveAll() receipt=%+v error=%v", archiveReceipt, err)
	}
	if repository.archiveUserID != userID || !repository.archiveAt.Equal(now) ||
		authorizer.requests[2].Action != authz.ActionNotificationArchiveSelf {
		t.Fatalf("archive boundary repository=%+v requests=%+v", repository, authorizer.requests)
	}

	feedbackReceipt, err := service.CreateFeedback(context.Background(), "feedback-cookie", "feedback-csrf", CreateFeedbackInput{
		Title: "  页面建议  ", Content: "  请改进移动端消息列表。  ",
	})
	if err != nil || feedbackReceipt.FeedbackID == uuid.Nil || !feedbackReceipt.CreatedAt.Equal(now) {
		t.Fatalf("CreateFeedback() receipt=%+v error=%v", feedbackReceipt, err)
	}
	if repository.feedbackUserID != userID || repository.feedbackInput.Title != "页面建议" ||
		repository.feedbackInput.Content != "请改进移动端消息列表。" ||
		authorizer.requests[3].Action != authz.ActionNotificationFeedbackCreateSelf {
		t.Fatalf("feedback boundary repository=%+v requests=%+v", repository, authorizer.requests)
	}
}

func TestServiceRejectsInvalidNotificationPagingBeforeAuthentication(t *testing.T) {
	t.Parallel()

	authenticator := &notificationAuthenticatorFixture{}
	service, err := NewService(
		authenticator,
		&notificationAuthorizerFixture{},
		&notificationRepositoryFixture{},
		time.Now,
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if _, err := service.List(context.Background(), "cookie", ListQuery{Limit: MaximumLimit + 1}); !errors.Is(err, ErrInput) {
		t.Fatalf("List() error = %v, want ErrInput", err)
	}
	if authenticator.currentCalls != 0 || authenticator.writeCalls != 0 {
		t.Fatalf("invalid input reached authentication: %+v", authenticator)
	}
}

func TestServiceRejectsMismatchedReviewProjection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	authenticator := &notificationAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: uuid.New()}}}
	repository := &notificationRepositoryFixture{page: Page{
		Items: []Notification{{
			ID: uuid.New(), Kind: KindTorrentReview, CreatedAt: now,
			TorrentReview: &TorrentReviewNotification{
				TorrentID: 44, TorrentTitle: "Broken", Outcome: torrents.StatePublished,
				ReasonCode: review.ReasonMetadataIncomplete, Reason: "错误的审核结果组合不应离开服务边界。",
			},
		}},
		Total: 1, UnreadCount: 1,
	}}
	service, err := NewService(authenticator, &notificationAuthorizerFixture{}, repository, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if _, err := service.List(context.Background(), "cookie", ListQuery{Limit: 20}); !errors.Is(err, ErrInvariant) {
		t.Fatalf("List() error = %v, want ErrInvariant", err)
	}
}

func TestServiceAcceptsTypedRatioWatchNotification(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	authenticator := &notificationAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: uuid.New()}}}
	repository := &notificationRepositoryFixture{page: Page{
		Items: []Notification{{
			ID: uuid.New(), Kind: KindRatioWatch, CreatedAt: now,
			RatioWatch: &RatioWatchNotification{
				Status:           ratiowatch.AssessmentDownloadRestricted,
				RatioBasisPoints: 2500, MinimumRatioBasisPoints: 4000,
				RestrictionRatioBasisPoints: 3000, DeadlineAt: now.Add(-time.Hour),
			},
		}},
		Total: 1, UnreadCount: 1,
	}}
	service, err := NewService(authenticator, &notificationAuthorizerFixture{}, repository, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	page, err := service.List(context.Background(), "cookie", ListQuery{Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].RatioWatch == nil ||
		page.Items[0].RatioWatch.Status != ratiowatch.AssessmentDownloadRestricted {
		t.Fatalf("List() page=%+v error=%v", page, err)
	}
}

func TestServiceAcceptsTypedRatioAppealNotification(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	authenticator := &notificationAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: uuid.New()}}}
	repository := &notificationRepositoryFixture{page: Page{
		Items: []Notification{{
			ID: uuid.New(), Kind: KindRatioAppeal, CreatedAt: now,
			RatioAppeal: &RatioAppealNotification{
				Status:   ratiowatch.AppealRejected,
				Response: "已核对有效流量记录，未发现异常，本期考核继续。",
			},
		}},
		Total: 1, UnreadCount: 1,
	}}
	service, err := NewService(authenticator, &notificationAuthorizerFixture{}, repository, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	page, err := service.List(context.Background(), "cookie", ListQuery{Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].RatioAppeal == nil ||
		page.Items[0].RatioAppeal.Status != ratiowatch.AppealRejected {
		t.Fatalf("List() page=%+v error=%v", page, err)
	}
}

func TestServiceAcceptsTypedHNRNotification(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 17, 13, 0, 0, 0, time.UTC)
	authenticator := &notificationAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: uuid.New()}}}
	repository := &notificationRepositoryFixture{page: Page{
		Items: []Notification{{
			ID: uuid.New(), Kind: KindHNR, CreatedAt: now,
			HNR: &HNRNotification{
				TorrentID: 9527, TorrentTitle: "H&R 待补做测试种子",
				Event:       traffic.HNRNotificationDownloadRestricted,
				GraceEndsAt: now,
			},
		}},
		Total: 1, UnreadCount: 1,
	}}
	service, err := NewService(authenticator, &notificationAuthorizerFixture{}, repository, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	page, err := service.List(context.Background(), "cookie", ListQuery{Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].HNR == nil ||
		page.Items[0].HNR.Event != traffic.HNRNotificationDownloadRestricted {
		t.Fatalf("List() page=%+v error=%v", page, err)
	}
}

func TestServiceAcceptsTypedHNRAppealNotification(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 17, 14, 0, 0, 0, time.UTC)
	authenticator := &notificationAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: uuid.New()}}}
	repository := &notificationRepositoryFixture{page: Page{
		Items: []Notification{{
			ID: uuid.New(), Kind: KindHNRAppeal, CreatedAt: now,
			HNR: &HNRNotification{
				TorrentID: 9527, TorrentTitle: "H&R 申诉测试种子",
				Event:       traffic.HNRNotificationAppealApproved,
				GraceEndsAt: now.Add(-time.Hour),
				Response:    "已核对异常记录，批准本条 H&R 义务豁免。",
			},
		}},
		Total: 1, UnreadCount: 1,
	}}
	service, err := NewService(authenticator, &notificationAuthorizerFixture{}, repository, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	page, err := service.List(context.Background(), "cookie", ListQuery{Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].HNR == nil ||
		page.Items[0].HNR.Event != traffic.HNRNotificationAppealApproved ||
		page.Items[0].HNR.Response == "" {
		t.Fatalf("List() page=%+v error=%v", page, err)
	}
}

func TestServiceAcceptsTypedWorkgroupAndMemberGiftNotifications(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 17, 15, 0, 0, 0, time.UTC)
	authenticator := &notificationAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: uuid.New()}}}
	repository := &notificationRepositoryFixture{page: Page{
		Items: []Notification{
			{
				ID: uuid.New(), Kind: KindWorkgroupContribution, CreatedAt: now,
				WorkgroupContribution: &WorkgroupContributionNotification{
					GroupKind: workgroups.GroupRetention, Metric: workgroups.MetricSeedingActiveSeconds,
					PolicyRevision: 1, PeriodStartsAt: now.Add(-17 * 24 * time.Hour),
					PeriodEndsAt: now.Add(14 * 24 * time.Hour), ObservedAt: now,
					EvidenceState: workgroups.ContributionEvidenceCollecting,
					CurrentValue:  86400, TargetValue: 604800,
					AssessmentState: workgroups.ContributionAssessmentInProgress,
					ExplanationCode: workgroups.ContributionExplanationPeriodInProgress,
					Reason:          "请继续保持本月做种并关注剩余贡献时长。",
				},
			},
			{
				ID: uuid.New(), Kind: KindMemberGift, CreatedAt: now,
				MemberGift: &MemberGiftNotification{
					SenderNumericID: 42, SenderUsername: "member42",
					SenderDisplayName: "四十二号成员", NetAmount: 9007199254740992,
					Message: "感谢保种",
				},
			},
		},
		Total: 2, UnreadCount: 2,
	}}
	service, err := NewService(authenticator, &notificationAuthorizerFixture{}, repository, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	page, err := service.List(context.Background(), "cookie", ListQuery{Limit: 20})
	if err != nil || len(page.Items) != 2 || page.Items[0].WorkgroupContribution == nil ||
		page.Items[1].MemberGift == nil || page.Items[1].MemberGift.SenderNumericID != 42 ||
		page.Items[1].MemberGift.NetAmount != 9007199254740992 {
		t.Fatalf("List() page=%+v error=%v", page, err)
	}
}

func TestServiceRejectsNotificationWithMultipleTypedPayloads(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 17, 15, 0, 0, 0, time.UTC)
	authenticator := &notificationAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: uuid.New()}}}
	repository := &notificationRepositoryFixture{page: Page{
		Items: []Notification{{
			ID: uuid.New(), Kind: KindMemberGift, CreatedAt: now,
			MemberGift: &MemberGiftNotification{
				SenderNumericID: 42, SenderUsername: "member42",
				SenderDisplayName: "四十二号成员", NetAmount: 10,
			},
			RatioAppeal: &RatioAppealNotification{
				Status:   ratiowatch.AppealRejected,
				Response: "这条多来源消息必须在服务边界被拒绝。",
			},
		}},
		Total: 1, UnreadCount: 1,
	}}
	service, err := NewService(authenticator, &notificationAuthorizerFixture{}, repository, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if _, err := service.List(context.Background(), "cookie", ListQuery{Limit: 20}); !errors.Is(err, ErrInvariant) {
		t.Fatalf("List() error = %v, want ErrInvariant", err)
	}
}
