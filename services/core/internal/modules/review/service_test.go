package review

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/modules/workgroups"
)

type recordingRepository struct {
	listLimit  int32
	page       PendingTorrentPage
	assignment ReviewAssignment
	command    DecideCommand
	result     DecisionResult
	err        error
}

func (repository *recordingRepository) ListPending(_ context.Context, limit int32) (PendingTorrentPage, error) {
	repository.listLimit = limit
	return repository.page, repository.err
}

func (repository *recordingRepository) ListAssignments(context.Context, uuid.UUID, int32) (ReviewAssignmentPage, error) {
	return ReviewAssignmentPage{}, repository.err
}

func (repository *recordingRepository) GetAssignment(context.Context, uuid.UUID, torrents.TorrentID) (ReviewAssignment, error) {
	return repository.assignment, repository.err
}

func (repository *recordingRepository) ListReviewed(context.Context, uuid.UUID, int32) (ReviewedTorrentPage, error) {
	return ReviewedTorrentPage{}, repository.err
}

func (repository *recordingRepository) Decide(_ context.Context, command DecideCommand) (DecisionResult, error) {
	repository.command = command
	return repository.result, repository.err
}

func (repository *recordingRepository) Vote(context.Context, VoteCommand) (VoteResult, error) {
	return VoteResult{}, repository.err
}

type sessionAuthenticatorStub struct {
	session identity.WebSession
}

func (stub sessionAuthenticatorStub) CurrentSession(context.Context, string) (identity.WebSession, error) {
	return stub.session, nil
}

func (stub sessionAuthenticatorStub) AuthenticateWrite(context.Context, string, string) (identity.WebSession, error) {
	return stub.session, nil
}

type reviewEvidenceReaderStub struct {
	evidence torrents.PendingReviewEvidence
}

func (stub reviewEvidenceReaderStub) PendingReviewEvidence(context.Context, torrents.TorrentID) (torrents.PendingReviewEvidence, error) {
	return stub.evidence, nil
}

func (reviewEvidenceReaderStub) PendingReviewFiles(context.Context, torrents.TorrentID, int, int) (torrents.PublicFilePage, error) {
	return torrents.PublicFilePage{}, nil
}

func (reviewEvidenceReaderStub) PendingReviewCover(context.Context, torrents.TorrentID) (torrents.PublicCover, error) {
	return torrents.PublicCover{}, nil
}

func (reviewEvidenceReaderStub) PendingReviewScreenshot(context.Context, torrents.TorrentID, int) (torrents.PublicScreenshot, error) {
	return torrents.PublicScreenshot{}, nil
}

type entitlementCheckerStub struct{}

func (entitlementCheckerStub) HasEntitlementAt(context.Context, uuid.UUID, workgroups.Entitlement, time.Time) (bool, error) {
	return true, nil
}

type recordingAuthorizer struct {
	request  authz.Request
	decision authz.Decision
	err      error
}

func (authorizer *recordingAuthorizer) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	authorizer.request = request
	return authorizer.decision, authorizer.err
}

func TestServiceListsPendingWithTypedStaffAuthorization(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 8, 14, 0, 0, 0, time.UTC)
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}, MFAAuthenticatedAt: now.Add(-time.Minute)}
	repository := &recordingRepository{page: PendingTorrentPage{Total: 3}}
	authorizer := &recordingAuthorizer{decision: allowedReviewDecision(now)}
	service, err := NewService(sessionAuthenticatorStub{}, repository, authorizer, entitlementCheckerStub{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	page, err := service.ListPending(context.Background(), actor, 20)
	if err != nil || page.Total != 3 || repository.listLimit != 20 {
		t.Fatalf("ListPending() page=%+v limit=%d error=%v", page, repository.listLimit, err)
	}
	request := authorizer.request
	if request.Action != authz.ActionTorrentReview || request.CredentialAudience != authz.AudienceStaffSession ||
		request.Subject.ID != actor.Subject.ID || request.Resource.Scope != authz.SiteScope() || request.Context.Purpose != "torrent-review" {
		t.Fatalf("authorization request = %+v", request)
	}
}

func TestServiceNormalizesAndForwardsReviewDecision(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 8, 14, 30, 0, 0, time.UTC)
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}, MFAAuthenticatedAt: now.Add(-time.Minute)}
	decisionID := uuid.New()
	want := DecisionResult{
		DecisionID: decisionID, TorrentID: 44,
		Decision: DecisionApprove, ReasonCode: ReasonMeetsRequirements,
		State: torrents.StatePublished, Version: 2, OccurredAt: now,
	}
	repository := &recordingRepository{result: want}
	authorizer := &recordingAuthorizer{decision: allowedReviewDecision(now)}
	service, err := NewService(sessionAuthenticatorStub{}, repository, authorizer, entitlementCheckerStub{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.Decide(context.Background(), actor, DecideInput{
		DecisionID: decisionID, TorrentID: 44, ExpectedVersion: 1,
		Decision: DecisionApprove, ReasonCode: ReasonMeetsRequirements,
		Reason: "  已核对文件清单与发布规则，可以进入正式发布。  ",
	})
	if err != nil || got != want {
		t.Fatalf("Decide() = %+v, %v", got, err)
	}
	command := repository.command
	if command.ReviewerID != actor.Subject.ID || command.OccurredAt != now || command.Authorization.ID == uuid.Nil ||
		command.Reason != "已核对文件清单与发布规则，可以进入正式发布。" || command.ExpectedVersion != 1 {
		t.Fatalf("repository command = %+v", command)
	}
}

func TestServiceRejectsInvalidDecisionBeforeAuthorizationOrRepository(t *testing.T) {
	t.Parallel()
	repository := &recordingRepository{}
	authorizer := &recordingAuthorizer{}
	service, err := NewService(sessionAuthenticatorStub{}, repository, authorizer, entitlementCheckerStub{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Decide(context.Background(), authz.StaffActor{}, DecideInput{
		DecisionID: uuid.New(), TorrentID: 44, ExpectedVersion: 1,
		Decision: DecisionApprove, ReasonCode: ReasonMetadataIncomplete,
		Reason: "通过决定不能使用驳回原因类别。",
	})
	if !errors.Is(err, ErrTorrentReviewInput) {
		t.Fatalf("Decide() error = %v", err)
	}
	if authorizer.request.Action != "" || repository.command.DecisionID != uuid.Nil {
		t.Fatal("invalid input reached authorization or repository")
	}
}

func TestServiceRefusesToComposeDifferentReviewEvidenceRevision(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 24, 1, 0, 0, 0, time.UTC)
	reviewerID := uuid.New()
	repository := &recordingRepository{assignment: ReviewAssignment{
		PendingTorrent: PendingTorrent{ID: 42, Version: 3},
		RequiredVotes:  RequiredReviewVotes, MaximumVotes: MaximumReviewVotes,
	}}
	authenticator := sessionAuthenticatorStub{session: identity.WebSession{User: identity.User{ID: reviewerID}}}
	authorizer := &recordingAuthorizer{decision: allowedReviewDecision(now)}
	service, err := NewService(
		authenticator,
		repository,
		authorizer,
		entitlementCheckerStub{},
		func() time.Time { return now },
		reviewEvidenceReaderStub{evidence: torrents.PendingReviewEvidence{ID: 42, Version: 4}},
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.GetAssignment(context.Background(), "session", 42)
	if !errors.Is(err, ErrTorrentReviewNotFound) {
		t.Fatalf("GetAssignment() error = %v", err)
	}
}

func allowedReviewDecision(now time.Time) authz.Decision {
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.New(), GrantVersion: 2, RoleID: "torrent_reviewer", MandateID: uuid.New(),
		EffectiveUntil: now.Add(time.Hour),
	}
}
