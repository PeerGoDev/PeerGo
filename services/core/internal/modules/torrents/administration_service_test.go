package torrents

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/contracts/go/trackeroperationsv1"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type torrentAdministrationRepositoryStub struct {
	page           ManagedTorrentPage
	result         TorrentAvailabilityResult
	listQuery      ManagedTorrentQuery
	changeCommand  ChangeTorrentAvailabilityCommand
	listCalls      int
	changeCalls    int
	peerTarget     ManagedTorrentPeerTarget
	peerIdentities []ManagedTorrentPeerIdentity
}

func (stub *torrentAdministrationRepositoryStub) ListManaged(_ context.Context, query ManagedTorrentQuery) (ManagedTorrentPage, error) {
	stub.listCalls++
	stub.listQuery = query
	return stub.page, nil
}

func (stub *torrentAdministrationRepositoryStub) ChangeAvailability(_ context.Context, command ChangeTorrentAvailabilityCommand) (TorrentAvailabilityResult, error) {
	stub.changeCalls++
	stub.changeCommand = command
	return stub.result, nil
}

func (stub *torrentAdministrationRepositoryStub) ManagedPeerTarget(_ context.Context, torrentID TorrentID) (ManagedTorrentPeerTarget, error) {
	stub.peerTarget.TorrentID = torrentID
	return stub.peerTarget, nil
}

func (stub *torrentAdministrationRepositoryStub) ManagedPeerIdentities(_ context.Context, _ []uuid.UUID) ([]ManagedTorrentPeerIdentity, error) {
	return stub.peerIdentities, nil
}

type trackerPeerReaderStub struct {
	page trackeroperationsv1.ActivePeerPage
}

func (stub trackerPeerReaderStub) ActivePeers(context.Context, string, int) (trackeroperationsv1.ActivePeerPage, error) {
	return stub.page, nil
}

type torrentAdministrationAuthorizerStub struct {
	decision authz.Decision
	err      error
	requests []authz.Request
}

func (stub *torrentAdministrationAuthorizerStub) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	stub.requests = append(stub.requests, request)
	return stub.decision, stub.err
}

func TestTorrentAdministrationListNormalizesAndAuthorizes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)
	repository := &torrentAdministrationRepositoryStub{page: ManagedTorrentPage{Total: 1, Limit: 20}}
	authorizer := &torrentAdministrationAuthorizerStub{decision: torrentAdministrationAllowedDecision(now)}
	service, err := NewTorrentAdministrationService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewTorrentAdministrationService() error = %v", err)
	}

	page, err := service.ListManaged(context.Background(), torrentAdministrationTestActor(), ManagedTorrentQuery{
		Query: "  1234  ", State: StatePublished, CategoryID: "movies", Limit: 20,
	})
	if err != nil || page.Total != 1 {
		t.Fatalf("ListManaged() = %+v, %v", page, err)
	}
	if repository.listQuery.Query != "1234" || repository.listQuery.CategoryID != "movies" {
		t.Fatalf("normalized query = %+v", repository.listQuery)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionTorrentManageRead ||
		authorizer.requests[0].CredentialAudience != authz.AudienceStaffSession {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestTorrentAdministrationChangeCarriesAuthorizationEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 9, 30, 0, 0, time.UTC)
	changeID := uuid.MustParse("0198f20a-6da8-7e51-9c64-212121212121")
	repository := &torrentAdministrationRepositoryStub{result: TorrentAvailabilityResult{
		ChangeID: changeID, TorrentID: 1234, Action: TorrentAvailabilityDisable, State: StateDisabled, Version: 8, ChangedAt: now,
	}}
	authorizer := &torrentAdministrationAuthorizerStub{decision: torrentAdministrationAllowedDecision(now)}
	service, err := NewTorrentAdministrationService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewTorrentAdministrationService() error = %v", err)
	}

	result, err := service.ChangeAvailability(context.Background(), torrentAdministrationTestActor(), ChangeTorrentAvailabilityInput{
		ChangeID: changeID, TorrentID: 1234, ExpectedVersion: 7,
		Action: TorrentAvailabilityDisable, Reason: "  该种子内容需要暂时下架并重新核对。  ",
	})
	if err != nil || result.State != StateDisabled {
		t.Fatalf("ChangeAvailability() = %+v, %v", result, err)
	}
	command := repository.changeCommand
	if command.ActorID != torrentAdministrationTestActor().Subject.ID || command.OccurredAt != now ||
		command.Authorization.ID != authorizer.decision.ID || command.Reason != "该种子内容需要暂时下架并重新核对。" {
		t.Fatalf("change command = %+v", command)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionTorrentLifecycleUpdate {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestTorrentAdministrationRejectsInvalidInputBeforeAuthorization(t *testing.T) {
	t.Parallel()

	repository := &torrentAdministrationRepositoryStub{}
	authorizer := &torrentAdministrationAuthorizerStub{err: errors.New("must not be called")}
	service, err := NewTorrentAdministrationService(repository, authorizer, time.Now)
	if err != nil {
		t.Fatalf("NewTorrentAdministrationService() error = %v", err)
	}

	_, err = service.ChangeAvailability(context.Background(), torrentAdministrationTestActor(), ChangeTorrentAvailabilityInput{
		ChangeID: uuid.New(), TorrentID: 1, ExpectedVersion: 1,
		Action: TorrentAvailabilityDisable, Reason: "太短",
	})
	if !errors.Is(err, ErrTorrentAdministrationInput) {
		t.Fatalf("ChangeAvailability() error = %v, want ErrTorrentAdministrationInput", err)
	}
	if repository.changeCalls != 0 || len(authorizer.requests) != 0 {
		t.Fatalf("invalid input reached dependencies: repository=%d authorization=%d", repository.changeCalls, len(authorizer.requests))
	}
}

func TestTorrentAdministrationAggregatesBoundedTrackerPeersByUser(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 22, 12, 0, 0, 0, time.UTC)
	uploaderID := uuid.MustParse("0198f20a-6da8-7e51-9c64-222222222222")
	leecherID := uuid.MustParse("0198f20a-6da8-7e51-9c64-333333333333")
	repository := &torrentAdministrationRepositoryStub{
		peerTarget: ManagedTorrentPeerTarget{InfoHashV1: InfoHashV1{1}, TotalSizeBytes: 1_000, UploaderID: uploaderID, Anonymous: true},
		peerIdentities: []ManagedTorrentPeerIdentity{
			{UserID: uploaderID, NumericID: 7, Username: "uploader", DisplayName: "发布者"},
			{UserID: leecherID, NumericID: 8, Username: "leecher", DisplayName: "下载者"},
		},
	}
	tracker := trackerPeerReaderStub{page: trackeroperationsv1.ActivePeerPage{
		GeneratedAt: now,
		Items: []trackeroperationsv1.ActivePeer{
			{UserID: uploaderID.String(), ClientFamily: "qbittorrent", AddressFamily: 4, Seedbox: true, Uploaded: 500, Downloaded: 100, UploadSpeed: 10, Left: 0, LastAnnounce: now.Add(-time.Minute)},
			{UserID: uploaderID.String(), ClientFamily: "qbittorrent", AddressFamily: 6, Seedbox: true, Uploaded: 500, Downloaded: 100, UploadSpeed: 20, Left: 0, LastAnnounce: now.Add(-2 * time.Minute)},
			{UserID: leecherID.String(), ClientFamily: "transmission", AddressFamily: 4, Uploaded: 20, Downloaded: 700, DownloadSpeed: 30, Left: 300, LastAnnounce: now.Add(-3 * time.Minute)},
		},
	}}
	authorizer := &torrentAdministrationAuthorizerStub{decision: torrentAdministrationAllowedDecision(now)}
	service, err := NewTorrentAdministrationService(repository, authorizer, func() time.Time { return now }, tracker)
	if err != nil {
		t.Fatal(err)
	}
	page, err := service.ActivePeers(context.Background(), torrentAdministrationTestActor(), 42)
	if err != nil || page.TotalConnections != 3 || len(page.Items) != 2 || !page.Items[0].Uploader ||
		page.Items[0].ActiveConnections != 2 || page.Items[0].UploadSpeed != 30 || !page.Items[0].Seedbox ||
		!page.Items[0].AnonymousUploader || len(page.Items[0].AddressFamilies) != 2 || page.Items[1].ProgressBasisPoints != 7000 {
		t.Fatalf("ActivePeers() = %+v, err = %v", page, err)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionTorrentManageRead ||
		authorizer.requests[0].CredentialAudience != authz.AudienceStaffSession {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func torrentAdministrationTestActor() authz.StaffActor {
	return authz.StaffActor{Subject: authz.Subject{
		ID: uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111"), Status: authz.SubjectActive,
	}}
}

func torrentAdministrationAllowedDecision(now time.Time) authz.Decision {
	return authz.Decision{
		ID: uuid.MustParse("0198f20a-6da8-7e51-9c64-121212121212"), Allow: true,
		Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.MustParse("0198f20a-6da8-7e51-9c64-131313131313"), GrantVersion: 1,
		RoleID: "site_admin", MandateID: uuid.MustParse("0198f20a-6da8-7e51-9c64-141414141414"),
		EffectiveUntil: now.Add(time.Hour),
	}
}
