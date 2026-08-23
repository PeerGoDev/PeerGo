package social

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type socialNotificationRepositoryFixture struct {
	*postRepositoryFixture
	query       SocialNotificationQuery
	page        SocialNotificationPage
	summary     SocialNotificationSummary
	readReceipt SocialNotificationReadReceipt
	allReceipt  SocialNotificationReadAllReceipt
	recipientID uuid.UUID
}

func (fixture *socialNotificationRepositoryFixture) ListNotifications(_ context.Context, recipientID uuid.UUID, query SocialNotificationQuery, _ time.Time) (SocialNotificationPage, error) {
	fixture.recipientID, fixture.query = recipientID, query
	return fixture.page, nil
}

func (fixture *socialNotificationRepositoryFixture) NotificationSummary(_ context.Context, recipientID uuid.UUID) (SocialNotificationSummary, error) {
	fixture.recipientID = recipientID
	return fixture.summary, nil
}

func (fixture *socialNotificationRepositoryFixture) MarkNotificationRead(_ context.Context, recipientID, _ uuid.UUID, _ time.Time) (SocialNotificationReadReceipt, error) {
	fixture.recipientID = recipientID
	return fixture.readReceipt, nil
}

func (fixture *socialNotificationRepositoryFixture) MarkAllNotificationsRead(_ context.Context, recipientID uuid.UUID, _ time.Time) (SocialNotificationReadAllReceipt, error) {
	fixture.recipientID = recipientID
	return fixture.allReceipt, nil
}

func TestPostServiceKeepsSocialNotificationReadsAndStateWritesSeparate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC)
	userID, notificationID := uuid.New(), uuid.New()
	authenticator := &postAuthenticatorFixture{
		session: identity.WebSession{User: identity.User{ID: userID}},
	}
	authorizer := &postAuthorizerFixture{}
	repository := &socialNotificationRepositoryFixture{
		postRepositoryFixture: &postRepositoryFixture{},
		page: SocialNotificationPage{
			Items: []SocialNotification{{ID: notificationID}}, Total: 1, UnreadCount: 1,
		},
		summary:     SocialNotificationSummary{UnreadCount: 1},
		readReceipt: SocialNotificationReadReceipt{NotificationID: notificationID, ReadAt: now},
		allReceipt:  SocialNotificationReadAllReceipt{UpdatedCount: 1, ReadAt: now},
	}
	service, err := NewPostService(authenticator, authorizer, repository, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	page, err := service.ListNotifications(context.Background(), "read-cookie", SocialNotificationQuery{
		Category: SocialNotificationReplies, Limit: 20,
	})
	if err != nil || len(page.Items) != 1 || repository.recipientID != userID || repository.query.Category != SocialNotificationReplies {
		t.Fatalf("ListNotifications() page=%+v query=%+v recipient=%v error=%v", page, repository.query, repository.recipientID, err)
	}
	if authenticator.readCalls != 1 || authenticator.writeCalls != 0 || authorizer.requests[0].Action != authz.ActionNotificationReadSelf {
		t.Fatalf("notification read auth=%+v decisions=%+v", authenticator, authorizer.requests)
	}

	receipt, err := service.MarkNotificationRead(context.Background(), "write-cookie", "csrf", notificationID)
	if err != nil || receipt.NotificationID != notificationID {
		t.Fatalf("MarkNotificationRead() receipt=%+v error=%v", receipt, err)
	}
	if authenticator.writeCalls != 1 || authenticator.writeCookie != "write-cookie" || authenticator.writeCSRF != "csrf" || authorizer.requests[1].Action != authz.ActionNotificationReadStateWriteSelf {
		t.Fatalf("notification write auth=%+v decisions=%+v", authenticator, authorizer.requests)
	}
}

func TestPostServiceRejectsInvalidSocialNotificationQueryBeforeAuthentication(t *testing.T) {
	t.Parallel()
	authenticator := &postAuthenticatorFixture{}
	repository := &socialNotificationRepositoryFixture{postRepositoryFixture: &postRepositoryFixture{}}
	service, err := NewPostService(authenticator, &postAuthorizerFixture{}, repository, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ListNotifications(context.Background(), "cookie", SocialNotificationQuery{
		Category: "unknown", Limit: 20,
	})
	if err != ErrSocialNotificationInput {
		t.Fatalf("ListNotifications() error=%v", err)
	}
	if authenticator.readCalls != 0 || authenticator.writeCalls != 0 {
		t.Fatalf("invalid notification query authenticated: %+v", authenticator)
	}
}
