package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type announcementAdministrationRepositoryStub struct {
	created            ManagedAnnouncement
	updated            ManagedAnnouncement
	publicationResult  ManagedAnnouncement
	createCommand      CreateAnnouncementDraftCommand
	updateCommand      UpdateAnnouncementDraftCommand
	publicationCommand ChangeAnnouncementPublicationCommand
}

func (stub *announcementAdministrationRepositoryStub) ListManagedAnnouncements(context.Context, int, int, time.Time) (ManagedAnnouncementPage, error) {
	return ManagedAnnouncementPage{}, nil
}

func (stub *announcementAdministrationRepositoryStub) GetManagedAnnouncement(context.Context, string, time.Time) (ManagedAnnouncement, error) {
	return ManagedAnnouncement{}, nil
}

func (stub *announcementAdministrationRepositoryStub) ListAnnouncementRevisions(context.Context, string, int, int) (AnnouncementRevisionPage, error) {
	return AnnouncementRevisionPage{}, nil
}

func (stub *announcementAdministrationRepositoryStub) CreateAnnouncementDraft(_ context.Context, command CreateAnnouncementDraftCommand) (ManagedAnnouncement, error) {
	stub.createCommand = command
	return stub.created, nil
}

func (stub *announcementAdministrationRepositoryStub) UpdateAnnouncementDraft(_ context.Context, command UpdateAnnouncementDraftCommand) (ManagedAnnouncement, error) {
	stub.updateCommand = command
	return stub.updated, nil
}

func (stub *announcementAdministrationRepositoryStub) ChangeAnnouncementPublication(_ context.Context, command ChangeAnnouncementPublicationCommand) (ManagedAnnouncement, error) {
	stub.publicationCommand = command
	return stub.publicationResult, nil
}

func TestAnnouncementAdministrationCreateNormalizesAndAuthorizes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
	repository := &announcementAdministrationRepositoryStub{
		created: ManagedAnnouncement{ID: "maintenance-window", Title: "维护通知", Version: 1},
	}
	authorizer := &categoryAuthorizerStub{decision: categoryAllowedDecision(now)}
	service, err := NewAnnouncementAdministrationService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAnnouncementAdministrationService() error = %v", err)
	}

	result, err := service.Create(context.Background(), categoryTestActor(now), CreateAnnouncementDraftInput{
		ID: " maintenance-window ", Title: " 维护通知 ", Summary: " 计划维护窗口 ",
		Body: " 服务将在窗口内短暂停止。 ", BodyFormat: AnnouncementBodyPlainText,
		Reason: " 建立维护公告草稿，等待值班人员复核。 ",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.ID != "maintenance-window" || repository.createCommand.ID != "maintenance-window" || repository.createCommand.Title != "维护通知" {
		t.Fatalf("result=%+v command=%+v", result, repository.createCommand)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionAnnouncementCreate || authorizer.requests[0].CredentialAudience != authz.AudienceStaffSession {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestAnnouncementAdministrationScheduleUsesPublishPermissionAndUTC(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
	scheduledFor := time.Date(2026, time.August, 11, 2, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	repository := &announcementAdministrationRepositoryStub{}
	authorizer := &categoryAuthorizerStub{decision: categoryAllowedDecision(now)}
	service, err := NewAnnouncementAdministrationService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAnnouncementAdministrationService() error = %v", err)
	}

	_, err = service.ChangePublication(context.Background(), categoryTestActor(now), ChangeAnnouncementPublicationInput{
		ID: "maintenance-window", Action: AnnouncementSchedule, ExpectedVersion: 3,
		ScheduledFor: &scheduledFor, Reason: "已完成双人复核，按维护窗口预约发布。",
	})
	if err != nil {
		t.Fatalf("ChangePublication() error = %v", err)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionAnnouncementPublish {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
	if repository.publicationCommand.ScheduledFor == nil || repository.publicationCommand.ScheduledFor.Location() != time.UTC || !repository.publicationCommand.ScheduledFor.Equal(scheduledFor) {
		t.Fatalf("publication command = %+v", repository.publicationCommand)
	}
}

func TestAnnouncementAdministrationWithdrawUsesIndependentPermission(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
	repository := &announcementAdministrationRepositoryStub{}
	authorizer := &categoryAuthorizerStub{decision: categoryAllowedDecision(now)}
	service, err := NewAnnouncementAdministrationService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAnnouncementAdministrationService() error = %v", err)
	}

	_, err = service.ChangePublication(context.Background(), categoryTestActor(now), ChangeAnnouncementPublicationInput{
		ID: "maintenance-window", Action: AnnouncementWithdraw, ExpectedVersion: 4,
		Reason: "公告内容已失效，需要立即停止公开展示。",
	})
	if err != nil {
		t.Fatalf("ChangePublication() error = %v", err)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionAnnouncementWithdraw {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestAnnouncementAdministrationRejectsInvalidScheduleBeforeAuthorization(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
	tooSoon := now.Add(30 * time.Second)
	repository := &announcementAdministrationRepositoryStub{}
	authorizer := &categoryAuthorizerStub{err: errors.New("must not be called")}
	service, err := NewAnnouncementAdministrationService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAnnouncementAdministrationService() error = %v", err)
	}

	_, err = service.ChangePublication(context.Background(), categoryTestActor(now), ChangeAnnouncementPublicationInput{
		ID: "maintenance-window", Action: AnnouncementSchedule, ExpectedVersion: 1,
		ScheduledFor: &tooSoon, Reason: "预约时间过近，必须在授权前直接拒绝。",
	})
	if !errors.Is(err, ErrAnnouncementAdministrationInput) {
		t.Fatalf("ChangePublication() error = %v, want ErrAnnouncementAdministrationInput", err)
	}
	if len(authorizer.requests) != 0 {
		t.Fatalf("authorization requests = %+v, want none", authorizer.requests)
	}
}
