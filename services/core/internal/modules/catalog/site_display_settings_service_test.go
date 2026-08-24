package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type siteDisplaySettingsRepositoryStub struct {
	current       SiteDisplaySettings
	updated       SiteDisplaySettings
	updateCommand UpdateSiteDisplaySettingsCommand
	getCalls      int
}

func (stub *siteDisplaySettingsRepositoryStub) GetSiteDisplaySettings(context.Context) (SiteDisplaySettings, error) {
	stub.getCalls++
	return stub.current, nil
}

func (stub *siteDisplaySettingsRepositoryStub) UpdateSiteDisplaySettings(_ context.Context, command UpdateSiteDisplaySettingsCommand) (SiteDisplaySettings, error) {
	stub.updateCommand = command
	return stub.updated, nil
}

func TestSiteDisplaySettingsGetUsesTypedReadPermission(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 8, 0, 0, 0, time.UTC)
	repository := &siteDisplaySettingsRepositoryStub{current: SiteDisplaySettings{Name: "PeerGo", Version: 2}}
	authorizer := &categoryAuthorizerStub{decision: categoryAllowedDecision(now)}
	service, err := NewSiteDisplaySettingsService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewSiteDisplaySettingsService() error = %v", err)
	}

	result, err := service.Get(context.Background(), categoryTestActor(now))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if result.Version != 2 || repository.getCalls != 1 {
		t.Fatalf("result=%+v getCalls=%d", result, repository.getCalls)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionSiteDisplayManageRead || authorizer.requests[0].Context.Purpose != "catalog-site-display-settings" {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestSiteDisplaySettingsUpdateNormalizesAndCarriesVersionEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 8, 0, 0, 0, time.UTC)
	repository := &siteDisplaySettingsRepositoryStub{updated: SiteDisplaySettings{Name: "PeerGo Club", Version: 4}}
	authorizer := &categoryAuthorizerStub{decision: categoryAllowedDecision(now)}
	service, err := NewSiteDisplaySettingsService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewSiteDisplaySettingsService() error = %v", err)
	}

	result, err := service.Update(context.Background(), categoryTestActor(now), UpdateSiteDisplaySettingsInput{
		Name: " PeerGo Club ", Description: " 新的公开说明。 ",
		TorrentFilenamePrefix: " [ROUSI] ",
		DefaultTorrentView:    TorrentViewPoster, ShowLatestAnnouncement: false,
		ExpectedVersion: 3, Reason: " 调整公开文案和默认视图以匹配当前社区定位。 ",
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	command := repository.updateCommand
	if result.Version != 4 || command.Name != "PeerGo Club" || command.Description != "新的公开说明。" || command.TorrentFilenamePrefix != "[ROUSI]" || command.ExpectedVersion != 3 || command.ShowLatestAnnouncement {
		t.Fatalf("result=%+v command=%+v", result, command)
	}
	if command.ActorID == [16]byte{} || command.Authorization.ID != authorizer.decision.ID || !command.OccurredAt.Equal(now) {
		t.Fatalf("update evidence = %+v", command)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionSiteDisplayUpdate {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestSiteDisplaySettingsRejectsInvalidInputBeforeAuthorization(t *testing.T) {
	t.Parallel()

	repository := &siteDisplaySettingsRepositoryStub{}
	authorizer := &categoryAuthorizerStub{err: errors.New("must not be called")}
	service, err := NewSiteDisplaySettingsService(repository, authorizer, time.Now)
	if err != nil {
		t.Fatalf("NewSiteDisplaySettingsService() error = %v", err)
	}

	_, err = service.Update(context.Background(), categoryTestActor(time.Now()), UpdateSiteDisplaySettingsInput{
		Name: "PeerGo", DefaultTorrentView: TorrentView("grid"), ExpectedVersion: 1,
		Reason: "理由长度足够但默认视图不属于契约枚举。",
	})
	if !errors.Is(err, ErrSiteDisplaySettingsInput) {
		t.Fatalf("Update() error = %v, want ErrSiteDisplaySettingsInput", err)
	}
	if len(authorizer.requests) != 0 {
		t.Fatalf("authorization requests = %+v, want none", authorizer.requests)
	}

	_, err = service.Update(context.Background(), categoryTestActor(time.Now()), UpdateSiteDisplaySettingsInput{
		Name: "PeerGo", TorrentFilenamePrefix: `[ROU/SI]`, DefaultTorrentView: TorrentViewList, ExpectedVersion: 1,
		Reason: "文件名前缀包含路径分隔符，必须在鉴权前拒绝。",
	})
	if !errors.Is(err, ErrSiteDisplaySettingsInput) || len(authorizer.requests) != 0 {
		t.Fatalf("invalid filename prefix error=%v authorization requests=%+v", err, authorizer.requests)
	}
}
