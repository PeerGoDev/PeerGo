package operations

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/contracts/go/settlementoperationsv1"
	"github.com/peergo/peergo/contracts/go/trackeroperationsv1"
	"github.com/peergo/peergo/services/core/internal/contracts/vaultoperations"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/imaging"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/modules/trackercontrol"
)

type Repository interface {
	Tracker(context.Context, time.Time) (TrackerOverview, error)
	Workers(context.Context, time.Time) (WorkerOverview, error)
	Storage(context.Context, time.Time, string) (StorageInventory, error)
	StorageMigrations(context.Context) ([]StorageMigrationOverview, error)
	VIPProfile(context.Context, time.Time) (VIPProfileStats, VIPBenefits, error)
	EconomySettings(context.Context, time.Time) (EconomyTransactionCounts, error)
	TorrentPurchaseRules(context.Context, time.Time) (TorrentPurchaseRules, error)
}

type TrackerRuntimeReader interface {
	Runtime(context.Context) (trackeroperationsv1.Runtime, error)
}

type SettlementSettingsReader interface {
	Settings(context.Context) (settlementoperationsv1.Settings, error)
}

type ImageDerivativeOverviewReader interface {
	Overview(context.Context) (imaging.QueueOverview, error)
}

type TrackerPolicyAdministration interface {
	Current(context.Context, authz.StaffActor) (trackercontrol.RuntimePolicyRevision, error)
	Issue(context.Context, authz.StaffActor, trackercontrol.IssueRuntimePolicyInput) (trackercontrol.RuntimePolicyRevision, error)
	MySeedboxReports(context.Context, uuid.UUID, int, int) (trackercontrol.SeedboxReportPage, error)
	SubmitSeedboxReport(context.Context, uuid.UUID, trackercontrol.SubmitSeedboxReportInput) (trackercontrol.SeedboxReport, error)
	SeedboxReports(context.Context, authz.StaffActor, trackercontrol.SeedboxReportStatus, int, int) (trackercontrol.SeedboxReportPage, error)
	DecideSeedboxReport(context.Context, authz.StaffActor, trackercontrol.DecideSeedboxReportInput) (trackercontrol.SeedboxReport, error)
}

type Service struct {
	repository       Repository
	authorizer       authz.Authorizer
	storage          StorageRuntime
	imageDerivatives ImageDerivativeOverviewReader
	email            vaultoperations.EmailOperationsClient
	trackerRuntime   TrackerRuntimeReader
	trackerPolicy    TrackerPolicyAdministration
	settlement       SettlementSettingsReader
	uploadPolicies   *torrents.UploadPolicyService
	now              func() time.Time
}

type ServiceConfig struct {
	UploadPolicies *torrents.UploadPolicyService
}

func NewService(repository Repository, authorizer authz.Authorizer, storage StorageRuntime, imageDerivatives ImageDerivativeOverviewReader, email vaultoperations.EmailOperationsClient, trackerRuntime TrackerRuntimeReader, trackerPolicy TrackerPolicyAdministration, settlement SettlementSettingsReader, now func() time.Time, configs ...ServiceConfig) (*Service, error) {
	if repository == nil || authorizer == nil || storage.BackendID == "" ||
		(storage.Driver != "filesystem" && storage.Driver != "s3") ||
		storage.TorrentUploadMaxBytes < 1 || storage.ScreenshotMaxBytes < 1 || storage.AvatarMaxBytes < 1 || imageDerivatives == nil || email == nil || trackerRuntime == nil || trackerPolicy == nil || settlement == nil {
		return nil, errors.New("operations service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	var config ServiceConfig
	if len(configs) > 0 {
		config = configs[0]
	}
	return &Service{repository: repository, authorizer: authorizer, storage: storage, imageDerivatives: imageDerivatives, email: email, trackerRuntime: trackerRuntime, trackerPolicy: trackerPolicy, settlement: settlement, uploadPolicies: config.UploadPolicies, now: now}, nil
}

func (service *Service) TrackerPolicy(ctx context.Context, actor authz.StaffActor) (trackercontrol.RuntimePolicyRevision, error) {
	return service.trackerPolicy.Current(ctx, actor)
}

func (service *Service) IssueTrackerPolicy(ctx context.Context, actor authz.StaffActor, input trackercontrol.IssueRuntimePolicyInput) (trackercontrol.RuntimePolicyRevision, error) {
	return service.trackerPolicy.Issue(ctx, actor, input)
}

func (service *Service) MySeedboxReports(ctx context.Context, userID uuid.UUID, limit, offset int) (trackercontrol.SeedboxReportPage, error) {
	return service.trackerPolicy.MySeedboxReports(ctx, userID, limit, offset)
}

func (service *Service) SubmitSeedboxReport(ctx context.Context, userID uuid.UUID, input trackercontrol.SubmitSeedboxReportInput) (trackercontrol.SeedboxReport, error) {
	return service.trackerPolicy.SubmitSeedboxReport(ctx, userID, input)
}

func (service *Service) SeedboxReports(ctx context.Context, actor authz.StaffActor, status trackercontrol.SeedboxReportStatus, limit, offset int) (trackercontrol.SeedboxReportPage, error) {
	return service.trackerPolicy.SeedboxReports(ctx, actor, status, limit, offset)
}

func (service *Service) DecideSeedboxReport(ctx context.Context, actor authz.StaffActor, input trackercontrol.DecideSeedboxReportInput) (trackercontrol.SeedboxReport, error) {
	return service.trackerPolicy.DecideSeedboxReport(ctx, actor, input)
}

func (service *Service) SettlementSettings(ctx context.Context, actor authz.StaffActor) (SettlementSettingsOverview, error) {
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionOperationsMonitorRead, authz.SiteScope(), now, "settlement-settings-read"); err != nil {
		return SettlementSettingsOverview{}, err
	}
	return service.settlement.Settings(ctx)
}

func (service *Service) TrackerRuntime(ctx context.Context, actor authz.StaffActor) (TrackerRuntimeOverview, error) {
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTrackerPolicyRead, authz.SiteScope(), now, "tracker-settings-read"); err != nil {
		return TrackerRuntimeOverview{}, err
	}
	return service.trackerRuntime.Runtime(ctx)
}

func (service *Service) TorrentRules(ctx context.Context, actor authz.StaffActor) (TorrentRulesOverview, error) {
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentManageRead, authz.SiteScope(), now, "torrent-settings-read"); err != nil {
		return TorrentRulesOverview{}, err
	}
	purchase, err := service.repository.TorrentPurchaseRules(ctx, now)
	if err != nil {
		return TorrentRulesOverview{}, err
	}
	policyOverview := torrents.UploadPolicyOverview{Active: torrents.DefaultUploadPolicyRevision(int(service.storage.TorrentUploadMaxBytes))}
	if service.uploadPolicies != nil {
		policyOverview, err = service.uploadPolicies.Overview(ctx, actor)
		if err != nil {
			return TorrentRulesOverview{}, err
		}
	}
	active := policyOverview.Active.Settings
	return TorrentRulesOverview{
		GeneratedAt: now,
		Upload: TorrentUploadRules{
			MetainfoMaxBytes: int64(active.MetainfoMaxBytes),
			MaxFiles:         active.MaxFiles, RequiredPrivate: true,
			SupportedProtocol: "bittorrent-v1", DuplicateSwarmRejected: true,
			InitialState: string(torrents.StatePendingReview),
		},
		Screenshots: TorrentScreenshotRules{
			MaxCount:        active.ScreenshotMaxCount,
			MaxBytesPerFile: int64(active.ScreenshotMaxBytes),
			Formats:         active.ScreenshotFormats, FirstIsCover: true,
		},
		Object: TorrentObjectRules{
			OriginalStoredImmutable: true, AnnounceRewrittenOnDownload: true,
			LegacyImportProfile: string(torrents.ValidationProfileLegacyImport),
			NewUploadProfile:    string(torrents.ValidationProfileStrictUpload),
		},
		Purchase: purchase, ActiveUploadPolicy: policyOverview.Active,
		ScheduledUploadPolicies: policyOverview.Scheduled,
	}, nil
}

func (service *Service) IssueTorrentUploadPolicy(ctx context.Context, actor authz.StaffActor, input torrents.IssueUploadPolicyInput) (torrents.UploadPolicyRevision, error) {
	if service.uploadPolicies == nil {
		return torrents.UploadPolicyRevision{}, torrents.ErrUploadPolicyNotFound
	}
	return service.uploadPolicies.Issue(ctx, actor, input)
}

func (service *Service) EconomySettings(ctx context.Context, actor authz.StaffActor) (EconomySettingsOverview, error) {
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionEconomySeedingRewardPolicyRead, authz.SiteScope(), now, "economy-settings-read"); err != nil {
		return EconomySettingsOverview{}, err
	}
	transactions, err := service.repository.EconomySettings(ctx, now)
	if err != nil {
		return EconomySettingsOverview{}, err
	}
	// Readiness reports only workflows enforced by a real domain service. Daily
	// attendance now resolves an immutable policy and atomically records its
	// receipt, magic transaction and optional experience entry. Retroactive
	// claims remain false because v1 deliberately has no补签 settlement path.
	return EconomySettingsOverview{
		GeneratedAt: now,
		Activity: ActivityRewardReadiness{
			LedgerSupported: true, DailyAttendanceConnected: true,
			RandomAttendanceConnected: true, StreakRewardConnected: true,
		},
		Usage: MagicUsageRules{
			CurrencyName: "魔力值", WholeUnitsOnly: true, PTCoinEnabled: false,
			MemberOverdraftAllowed: false, AppendOnlyLedger: true,
			TorrentPurchaseSupported: true, TorrentPurchaseConnected: true,
			MemberGiftConnected: true, ContentTipConnected: true, RefundSupported: true,
		},
		Transactions: transactions,
	}, nil
}

func (service *Service) Email(ctx context.Context, actor authz.StaffActor) (EmailOverview, error) {
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionOperationsMonitorRead, authz.SiteScope(), now, "email-settings-read"); err != nil {
		return EmailOverview{}, err
	}
	return service.email.EmailOperations(ctx)
}

func (service *Service) TestEmail(ctx context.Context, actor authz.StaffActor, recipient string) (vaultoperations.EmailTestResult, error) {
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionOperationsEmailTest, authz.SiteScope(), now, "email-delivery-test"); err != nil {
		return vaultoperations.EmailTestResult{}, err
	}
	return service.email.TestEmail(ctx, recipient)
}

func (service *Service) Storage(ctx context.Context, actor authz.StaffActor) (StorageOverview, error) {
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionOperationsMonitorRead, authz.SiteScope(), now, "storage-operations-monitor"); err != nil {
		return StorageOverview{}, err
	}
	inventory, err := service.repository.Storage(ctx, now, service.storage.BackendID)
	if err != nil {
		return StorageOverview{}, err
	}
	derivatives, err := service.imageDerivatives.Overview(ctx)
	if err != nil {
		return StorageOverview{}, err
	}
	migrations, err := service.repository.StorageMigrations(ctx)
	if err != nil {
		return StorageOverview{}, err
	}
	return StorageOverview{
		GeneratedAt: now,
		Runtime:     service.storage,
		Inventory:   inventory,
		ImageDerivatives: ImageDerivativeOverview{
			PolicyVersion:   derivatives.PolicyVersion,
			Pending:         derivatives.Pending,
			Processing:      derivatives.Processing,
			Retrying:        derivatives.Retrying,
			Ready:           derivatives.Ready,
			Dead:            derivatives.Dead,
			SourceObjects:   derivatives.SourceObjects,
			OutputObjects:   derivatives.OutputObjects,
			OutputBytes:     derivatives.OutputBytes,
			OldestPendingAt: derivatives.OldestPendingAt,
			LastErrorCode:   derivatives.LastErrorCode,
			LastErrorAt:     derivatives.LastErrorAt,
		},
		Migrations: migrations,
	}, nil
}

func (service *Service) VIPProfile(ctx context.Context, actor authz.StaffActor) (VIPProfileOverview, error) {
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionOperationsMonitorRead, authz.SiteScope(), now, "vip-profile-settings-read"); err != nil {
		return VIPProfileOverview{}, err
	}
	stats, benefits, err := service.repository.VIPProfile(ctx, now)
	if err != nil {
		return VIPProfileOverview{}, err
	}
	// These are the limits enforced by the current account-profile and avatar
	// handlers. Keep unsupported VIP benefits false until a domain service owns
	// and enforces them; a decorative admin toggle must never imply entitlement.
	profile := ProfileRules{
		DisplayNameMinCharacters: 1,
		DisplayNameMaxCharacters: 40,
		AvatarMinPixels:          32,
		AvatarMaxPixels:          1024,
		AvatarMaxBytes:           service.storage.AvatarMaxBytes,
		AvatarFormat:             "jpeg",
	}
	return VIPProfileOverview{GeneratedAt: now, Stats: stats, Profile: profile, Benefits: benefits}, nil
}

func (service *Service) Tracker(ctx context.Context, actor authz.StaffActor) (TrackerOverview, error) {
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionOperationsMonitorRead, authz.SiteScope(), now, "tracker-operations-monitor"); err != nil {
		return TrackerOverview{}, err
	}
	return service.repository.Tracker(ctx, now)
}

func (service *Service) Workers(ctx context.Context, actor authz.StaffActor) (WorkerOverview, error) {
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionOperationsMonitorRead, authz.SiteScope(), now, "worker-operations-monitor"); err != nil {
		return WorkerOverview{}, err
	}
	return service.repository.Workers(ctx, now)
}
