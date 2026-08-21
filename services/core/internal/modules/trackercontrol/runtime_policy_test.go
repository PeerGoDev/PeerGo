package trackercontrol

import (
	"context"
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/contracts/go/trackerruntimepolicyv1"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type runtimePolicyRepositoryStub struct {
	latest         RuntimePolicyRevision
	issued         RuntimePolicyRevision
	command        issueRuntimePolicyCommand
	submittedBox   submitSeedboxReportCommand
	decidedBox     decideSeedboxReportCommand
	seedboxReports SeedboxReportPage
}

func (stub *runtimePolicyRepositoryStub) LatestRuntimePolicy(context.Context) (RuntimePolicyRevision, error) {
	return stub.latest, nil
}

func (stub *runtimePolicyRepositoryStub) IssueRuntimePolicy(_ context.Context, command issueRuntimePolicyCommand) (RuntimePolicyRevision, error) {
	stub.command = command
	stub.issued = RuntimePolicyRevision{
		Sequence: command.ExpectedSequence + 1, Policy: command.Policy,
		Reason: command.Reason, CreatedAt: command.OccurredAt,
	}
	return stub.issued, nil
}

func (stub *runtimePolicyRepositoryStub) ListMySeedboxReports(context.Context, uuid.UUID, int, int) (SeedboxReportPage, error) {
	return stub.seedboxReports, nil
}

func (stub *runtimePolicyRepositoryStub) SubmitSeedboxReport(_ context.Context, command submitSeedboxReportCommand) (SeedboxReport, error) {
	stub.submittedBox = command
	return SeedboxReport{ID: command.ReportID, UserID: command.UserID, Address: command.Address}, nil
}

func (stub *runtimePolicyRepositoryStub) ListSeedboxReports(context.Context, SeedboxReportStatus, int, int) (SeedboxReportPage, error) {
	return stub.seedboxReports, nil
}

func (stub *runtimePolicyRepositoryStub) DecideSeedboxReport(_ context.Context, command decideSeedboxReportCommand) (SeedboxReport, error) {
	stub.decidedBox = command
	return SeedboxReport{ID: command.ReportID, Status: SeedboxReportApproved}, nil
}

type runtimePolicyAuthorizerStub struct{ requests []authz.Request }

func (stub *runtimePolicyAuthorizerStub) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	stub.requests = append(stub.requests, request)
	return authz.Decision{
		ID: uuid.New(), Allow: true, GrantID: uuid.New(), GrantVersion: 1,
		MandateID: uuid.New(), RoleID: "site_admin", EffectiveUntil: request.Context.Now.Add(time.Hour),
	}, nil
}

func TestRuntimePolicyServiceUsesDedicatedPermissionsAndCanonicalRevision(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	repository := &runtimePolicyRepositoryStub{latest: RuntimePolicyRevision{Sequence: 1, Policy: testRuntimePolicy()}}
	authorizer := &runtimePolicyAuthorizerStub{}
	service, err := NewRuntimePolicyService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	if _, err := service.Current(context.Background(), actor); err != nil {
		t.Fatal(err)
	}
	requestID := uuid.MustParse("01990f6f-fd80-7000-8000-000000000001")
	inputPolicy := testRuntimePolicy()
	inputPolicy.Revision = "" // Revision is server-owned and absent from the API DTO.
	issued, err := service.Issue(context.Background(), actor, IssueRuntimePolicyInput{
		RequestID: requestID, ExpectedSequence: 1, Policy: inputPolicy,
		Reason: "  调整限频。  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(authorizer.requests) != 2 || authorizer.requests[0].Action != authz.ActionTrackerPolicyRead ||
		authorizer.requests[1].Action != authz.ActionTrackerPolicyIssue {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
	if issued.Sequence != 2 || issued.Policy.Revision != "tracker-runtime-01990f6ffd8070008000000000000001" ||
		issued.Reason != "调整限频。" || repository.command.ActorID != actor.Subject.ID {
		t.Fatalf("issued=%+v command=%+v", issued, repository.command)
	}

	inputPolicy = testRuntimePolicy()
	inputPolicy.Revision = ""
	if _, err := service.Issue(context.Background(), actor, IssueRuntimePolicyInput{
		RequestID: uuid.New(), ExpectedSequence: 2, Policy: inputPolicy, Reason: "调整限频",
	}); !errors.Is(err, ErrRuntimePolicyInput) {
		t.Fatalf("four-rune reason error = %v, want ErrRuntimePolicyInput", err)
	}
}

func TestSeedboxReportSubmissionCanonicalizesSingleHostAndUsesSelfPermission(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 18, 3, 0, 0, 0, time.UTC)
	repository := &runtimePolicyRepositoryStub{}
	authorizer := &runtimePolicyAuthorizerStub{}
	service, err := NewRuntimePolicyService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	userID := uuid.New()
	requestID := uuid.New()
	_, err = service.SubmitSeedboxReport(context.Background(), userID, SubmitSeedboxReportInput{
		RequestID: requestID, Address: " 203.0.113.8 ", Provider: " Example Seedbox ",
		BandwidthMbps: 1000, Statement: " 仅用于受控盒子申报测试。 ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.submittedBox.Address != "203.0.113.8" || repository.submittedBox.Network != "203.0.113.8/32" ||
		repository.submittedBox.Provider != "Example Seedbox" || repository.submittedBox.Statement != "仅用于受控盒子申报测试。" {
		t.Fatalf("submission command = %+v", repository.submittedBox)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionTrackerSeedboxReportCreateSelf ||
		authorizer.requests[0].Subject.ID != userID {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
	if _, err := service.SubmitSeedboxReport(context.Background(), userID, SubmitSeedboxReportInput{
		RequestID: uuid.New(), Address: "203.0.113.0/24", Provider: "Example Seedbox",
		BandwidthMbps: 1000, Statement: "不允许成员申报整个共享网段。",
	}); !errors.Is(err, ErrSeedboxReportInput) {
		t.Fatalf("CIDR submission error = %v, want ErrSeedboxReportInput", err)
	}
}

func TestSeedboxDecisionUsesDedicatedStaffPermission(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 18, 3, 0, 0, 0, time.UTC)
	repository := &runtimePolicyRepositoryStub{}
	authorizer := &runtimePolicyAuthorizerStub{}
	service, err := NewRuntimePolicyService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	reportID := uuid.New()
	_, err = service.DecideSeedboxReport(context.Background(), actor, DecideSeedboxReportInput{
		RequestID: uuid.New(), ReportID: reportID, ExpectedVersion: 1,
		Decision: SeedboxDecisionApprove, Reason: "确认该单一主机地址归当前成员使用。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.decidedBox.ReportID != reportID || repository.decidedBox.ActorID != actor.Subject.ID ||
		len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionTrackerSeedboxReportDecide {
		t.Fatalf("decision=%+v authorization=%+v", repository.decidedBox, authorizer.requests)
	}
}

type runtimePolicyPublisherStub struct {
	artifact trackerruntimepolicyv1.SignedArtifact
}

func (stub *runtimePolicyPublisherStub) PublishRuntimePolicy(_ context.Context, artifact trackerruntimepolicyv1.SignedArtifact) (SnapshotPublication, error) {
	stub.artifact = artifact
	return SnapshotPublication{Published: true}, nil
}

func TestRuntimePolicySnapshotBuilderSignsLatestImmutableRevision(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 17, 13, 0, 0, 123, time.FixedZone("UTC+8", 8*60*60))
	repository := &runtimePolicyRepositoryStub{latest: RuntimePolicyRevision{Sequence: 7, Policy: testRuntimePolicy()}}
	publisher := &runtimePolicyPublisherStub{}
	privateKey := snapshotTestPrivateKey(0x57)
	builder, err := NewRuntimePolicySnapshotBuilder(repository, publisher, "active", privateKey, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	result, err := builder.BuildAndPublish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	verified, err := trackerruntimepolicyv1.Verify(publisher.artifact.Bytes, map[string]ed25519.PublicKey{
		"active": privateKey.Public().(ed25519.PublicKey),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Published || result.ControlSequence != 7 || result.Revision != "tracker-runtime-test-v1" ||
		!result.GeneratedAt.Equal(now.UTC()) || verified.Snapshot.Policy.MaxScrapeHashes != 50 {
		t.Fatalf("result=%+v snapshot=%+v", result, verified.Snapshot)
	}
}

func TestDecodeRuntimeSeedboxPolicyDefaultsLegacyDownloadFactor(t *testing.T) {
	t.Parallel()
	var policy trackerruntimepolicyv1.SeedboxPolicy
	err := decodeRuntimeSeedboxPolicy([]byte(`{"enabled":false,"upload_factor_basis_points":5000,"seedbox_speed_limit_bytes_per_second":0,"standard_speed_limit_bytes_per_second":0,"rules":[]}`), &policy)
	if err != nil {
		t.Fatal(err)
	}
	if policy.DownloadFactorBasisPoints != 10_000 {
		t.Fatalf("download factor = %d, want historical 1x", policy.DownloadFactorBasisPoints)
	}
}

func testRuntimePolicy() trackerruntimepolicyv1.Policy {
	return trackerruntimepolicyv1.Policy{
		Revision: "tracker-runtime-test-v1", AnnounceIntervalSeconds: 1800,
		MinAnnounceIntervalSeconds: 900, DefaultNumWant: 50, MaxNumWant: 100,
		ScrapeEnabled: true, MaxScrapeHashes: 50, ClientMode: trackerruntimepolicyv1.ClientModeAllowAll,
		AllowedClients: []trackerruntimepolicyv1.ClientRule{}, UserRequestsPerMinute: 30,
		UserBurst: 60, AddressRequestsPerMinute: 120, AddressBurst: 240,
		Seedbox: trackerruntimepolicyv1.SeedboxPolicy{
			UploadFactorBasisPoints: 5_000, DownloadFactorBasisPoints: 10_000,
			Rules: []trackerruntimepolicyv1.SeedboxRule{},
		},
	}
}
