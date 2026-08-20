package operations

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/settlementoperationsv1"
	"github.com/peergo/peergo/contracts/go/trackeroperationsv1"
	"github.com/peergo/peergo/services/core/internal/contracts/vaultoperations"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/imaging"
	"github.com/peergo/peergo/services/core/internal/modules/trackercontrol"
)

type operationsRepositoryStub struct {
	trackerAt  time.Time
	workerAt   time.Time
	storageAt  time.Time
	vipAt      time.Time
	economyAt  time.Time
	purchaseAt time.Time
}

func (stub *operationsRepositoryStub) Tracker(_ context.Context, now time.Time) (TrackerOverview, error) {
	stub.trackerAt = now
	return TrackerOverview{GeneratedAt: now}, nil
}

func (stub *operationsRepositoryStub) Workers(_ context.Context, now time.Time) (WorkerOverview, error) {
	stub.workerAt = now
	return WorkerOverview{GeneratedAt: now}, nil
}

func (stub *operationsRepositoryStub) Storage(_ context.Context, now time.Time, backendID string) (StorageInventory, error) {
	stub.storageAt = now
	return StorageInventory{PreferredOnActiveBackend: int64(len(backendID))}, nil
}

func (stub *operationsRepositoryStub) StorageMigrations(context.Context) ([]StorageMigrationOverview, error) {
	return nil, nil
}

func (stub *operationsRepositoryStub) VIPProfile(_ context.Context, now time.Time) (VIPProfileStats, VIPBenefits, error) {
	stub.vipAt = now
	return VIPProfileStats{ActiveVIP: 43}, VIPBenefits{SeedingRewardBonusBPS: 2_000}, nil
}

func (stub *operationsRepositoryStub) EconomySettings(_ context.Context, now time.Time) (EconomyTransactionCounts, error) {
	stub.economyAt = now
	return EconomyTransactionCounts{LegacyOpening: 12_327, SeedingReward: 42}, nil
}

func (stub *operationsRepositoryStub) TorrentPurchaseRules(_ context.Context, now time.Time) (TorrentPurchaseRules, error) {
	stub.purchaseAt = now
	return TorrentPurchaseRules{Enabled: true, CurrencyName: "魔力值", WholeUnitsOnly: true, TaxBasisPoints: 1000}, nil
}

type operationsAuthorizerStub struct {
	now      time.Time
	requests []authz.Request
}

type emailStatusReaderStub struct {
	status     vaultoperations.EmailStatus
	testResult vaultoperations.EmailTestResult
	recipient  *string
}

func (stub emailStatusReaderStub) EmailOperations(context.Context) (vaultoperations.EmailStatus, error) {
	return stub.status, nil
}

func (stub emailStatusReaderStub) TestEmail(_ context.Context, recipient string) (vaultoperations.EmailTestResult, error) {
	if stub.recipient != nil {
		*stub.recipient = recipient
	}
	return stub.testResult, nil
}

type trackerRuntimeReaderStub struct {
	runtime trackeroperationsv1.Runtime
}

type settlementSettingsReaderStub struct {
	settings settlementoperationsv1.Settings
}

type imageDerivativeOverviewReaderStub struct{}

func (imageDerivativeOverviewReaderStub) Overview(context.Context) (imaging.QueueOverview, error) {
	return imaging.QueueOverview{PolicyVersion: imaging.PolicyVersion, Ready: 24, OutputBytes: 4096}, nil
}

type trackerPolicyAdministrationStub struct{}

func (trackerPolicyAdministrationStub) Current(context.Context, authz.StaffActor) (trackercontrol.RuntimePolicyRevision, error) {
	return trackercontrol.RuntimePolicyRevision{}, nil
}

func (trackerPolicyAdministrationStub) Issue(context.Context, authz.StaffActor, trackercontrol.IssueRuntimePolicyInput) (trackercontrol.RuntimePolicyRevision, error) {
	return trackercontrol.RuntimePolicyRevision{}, nil
}

func (trackerPolicyAdministrationStub) MySeedboxReports(context.Context, uuid.UUID, int, int) (trackercontrol.SeedboxReportPage, error) {
	return trackercontrol.SeedboxReportPage{}, nil
}
func (trackerPolicyAdministrationStub) SubmitSeedboxReport(context.Context, uuid.UUID, trackercontrol.SubmitSeedboxReportInput) (trackercontrol.SeedboxReport, error) {
	return trackercontrol.SeedboxReport{}, nil
}
func (trackerPolicyAdministrationStub) SeedboxReports(context.Context, authz.StaffActor, trackercontrol.SeedboxReportStatus, int, int) (trackercontrol.SeedboxReportPage, error) {
	return trackercontrol.SeedboxReportPage{}, nil
}
func (trackerPolicyAdministrationStub) DecideSeedboxReport(context.Context, authz.StaffActor, trackercontrol.DecideSeedboxReportInput) (trackercontrol.SeedboxReport, error) {
	return trackercontrol.SeedboxReport{}, nil
}

func (stub settlementSettingsReaderStub) Settings(context.Context) (settlementoperationsv1.Settings, error) {
	return stub.settings, nil
}

func (stub trackerRuntimeReaderStub) Runtime(context.Context) (trackeroperationsv1.Runtime, error) {
	return stub.runtime, nil
}

func (stub *operationsAuthorizerStub) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	stub.requests = append(stub.requests, request)
	return authz.Decision{
		Allow: true, GrantID: uuid.New(), GrantVersion: 1,
		MandateID: uuid.New(), RoleID: "site_admin", EffectiveUntil: stub.now.Add(time.Hour),
	}, nil
}

func TestServiceUsesOneReadOnlyOperationsCapability(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 16, 3, 4, 5, 6, time.FixedZone("test", 8*60*60))
	repository := &operationsRepositoryStub{}
	authorizer := &operationsAuthorizerStub{now: now.UTC()}
	var testRecipient string
	service, err := NewService(repository, authorizer, StorageRuntime{
		BackendID: "local-primary", Driver: "filesystem", TorrentUploadMaxBytes: 4 << 20,
		ScreenshotMaxBytes: 2 << 20, AvatarMaxBytes: 1 << 20,
	}, imageDerivativeOverviewReaderStub{}, emailStatusReaderStub{
		status: vaultoperations.EmailStatus{DeliveryMode: "development_outbox"},
		testResult: vaultoperations.EmailTestResult{
			AcceptedAt: now.UTC(), Template: "peergo-delivery-test-v1",
		},
		recipient: &testRecipient,
	}, trackerRuntimeReaderStub{runtime: trackeroperationsv1.Runtime{
		GeneratedAt: now.UTC(), AnnounceIntervalSeconds: 1800, MinAnnounceIntervalSeconds: 900,
		DefaultNumWant: 50, MaxNumWant: 100, PeerTTLSeconds: 2100,
		MaxSwarms: 100000, MaxPeers: 1000000, MaxPeersPerSwarm: 100000,
	}}, trackerPolicyAdministrationStub{}, settlementSettingsReaderStub{settings: settlementoperationsv1.Settings{
		GeneratedAt: now.UTC(),
		Seedbox: settlementoperationsv1.SeedboxPolicy{
			SettlementPrimitiveSupported: true, DownloadFactorBasisPoints: 10_000,
		},
	}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	if _, err := service.Tracker(context.Background(), actor); err != nil {
		t.Fatalf("Tracker() error = %v", err)
	}
	if runtime, err := service.TrackerRuntime(context.Background(), actor); err != nil || runtime.AnnounceIntervalSeconds != 1800 {
		t.Fatalf("TrackerRuntime() = %+v, error = %v", runtime, err)
	}
	rules, err := service.TorrentRules(context.Background(), actor)
	if err != nil || rules.Upload.MaxFiles != 100_000 || !rules.Upload.RequiredPrivate || rules.Screenshots.MaxCount != 6 {
		t.Fatalf("TorrentRules() = %+v, error = %v", rules, err)
	}
	settlementSettings, err := service.SettlementSettings(context.Background(), actor)
	if err != nil || !settlementSettings.Seedbox.SettlementPrimitiveSupported {
		t.Fatalf("SettlementSettings() = %+v, error = %v", settlementSettings, err)
	}
	economySettings, err := service.EconomySettings(context.Background(), actor)
	if err != nil || economySettings.Usage.CurrencyName != "魔力值" || !economySettings.Usage.WholeUnitsOnly || economySettings.Usage.PTCoinEnabled || economySettings.Transactions.LegacyOpening != 12_327 {
		t.Fatalf("EconomySettings() = %+v, error = %v", economySettings, err)
	}
	if !economySettings.Activity.LedgerSupported || !economySettings.Activity.DailyAttendanceConnected ||
		!economySettings.Activity.RandomAttendanceConnected || !economySettings.Activity.StreakRewardConnected ||
		economySettings.Activity.RetroactiveConnected {
		t.Fatalf("EconomySettings() attendance readiness = %+v", economySettings.Activity)
	}
	if _, err := service.Workers(context.Background(), actor); err != nil {
		t.Fatalf("Workers() error = %v", err)
	}
	if _, err := service.Storage(context.Background(), actor); err != nil {
		t.Fatalf("Storage() error = %v", err)
	}
	mail, err := service.Email(context.Background(), actor)
	if err != nil || mail.DeliveryMode != "development_outbox" {
		t.Fatalf("Email() = %+v, error = %v", mail, err)
	}
	mailTest, err := service.TestEmail(context.Background(), actor, "admin@example.com")
	if err != nil || mailTest.Template != "peergo-delivery-test-v1" || testRecipient != "admin@example.com" {
		t.Fatalf("TestEmail() = %+v, recipient = %q, error = %v", mailTest, testRecipient, err)
	}
	vipProfile, err := service.VIPProfile(context.Background(), actor)
	if err != nil {
		t.Fatalf("VIPProfile() error = %v", err)
	}
	if vipProfile.Stats.ActiveVIP != 43 || vipProfile.Profile.AvatarMaxBytes != 1<<20 || vipProfile.Benefits.SeedingRewardBonusBPS != 2_000 {
		t.Fatalf("VIPProfile() = %+v", vipProfile)
	}
	if len(authorizer.requests) != 10 {
		t.Fatalf("authorization request count = %d", len(authorizer.requests))
	}
	for index, request := range authorizer.requests {
		expectedAction := authz.ActionOperationsMonitorRead
		if index == 1 {
			expectedAction = authz.ActionTrackerPolicyRead
		} else if index == 2 {
			expectedAction = authz.ActionTorrentManageRead
		} else if index == 4 {
			expectedAction = authz.ActionEconomySeedingRewardPolicyRead
		} else if index == 8 {
			expectedAction = authz.ActionOperationsEmailTest
		}
		if request.Action != expectedAction || request.CredentialAudience != authz.AudienceStaffSession {
			t.Fatalf("authorization request = %+v", request)
		}
	}
	if repository.trackerAt != now.UTC() || repository.workerAt != now.UTC() || repository.storageAt != now.UTC() || repository.vipAt != now.UTC() || repository.economyAt != now.UTC() || repository.purchaseAt != now.UTC() {
		t.Fatalf("repository times tracker=%s worker=%s storage=%s vip=%s economy=%s", repository.trackerAt, repository.workerAt, repository.storageAt, repository.vipAt, repository.economyAt)
	}
}
