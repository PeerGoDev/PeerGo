package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
	"github.com/peergo/peergo/services/core/internal/contracts/trackerevent"
	"github.com/peergo/peergo/services/core/internal/modules/audit"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/economy"
	"github.com/peergo/peergo/services/core/internal/modules/economy/attendance"
	"github.com/peergo/peergo/services/core/internal/modules/economy/contenttip"
	"github.com/peergo/peergo/services/core/internal/modules/economy/medals"
	"github.com/peergo/peergo/services/core/internal/modules/economy/membergift"
	"github.com/peergo/peergo/services/core/internal/modules/economy/seedingreward"
	"github.com/peergo/peergo/services/core/internal/modules/economy/torrentpurchase"
	"github.com/peergo/peergo/services/core/internal/modules/hnradmin"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/imaging"
	"github.com/peergo/peergo/services/core/internal/modules/newcomer"
	"github.com/peergo/peergo/services/core/internal/modules/notifications"
	"github.com/peergo/peergo/services/core/internal/modules/operations"
	"github.com/peergo/peergo/services/core/internal/modules/progression"
	"github.com/peergo/peergo/services/core/internal/modules/promotions"
	"github.com/peergo/peergo/services/core/internal/modules/ratiowatch"
	"github.com/peergo/peergo/services/core/internal/modules/review"
	"github.com/peergo/peergo/services/core/internal/modules/rss"
	"github.com/peergo/peergo/services/core/internal/modules/social"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/modules/trackercontrol"
	"github.com/peergo/peergo/services/core/internal/modules/traffic"
	"github.com/peergo/peergo/services/core/internal/modules/workgroups"
	"github.com/peergo/peergo/services/core/internal/platform/config"
	"github.com/peergo/peergo/services/core/internal/platform/httpserver"
	"github.com/peergo/peergo/services/core/internal/platform/objectstore"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
	"github.com/peergo/peergo/services/core/internal/platform/settlementoperations"
	"github.com/peergo/peergo/services/core/internal/platform/trackeroperations"
	"github.com/peergo/peergo/services/core/internal/transport/httpapi"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	settings, err := config.Load()
	if err != nil {
		logger.Error("invalid core configuration", "error", err)
		os.Exit(1)
	}

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()
	pool, err := pgxpool.New(startupCtx, settings.DatabaseURL)
	if err != nil {
		logger.Error("open core database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := pool.Ping(startupCtx); err != nil {
		logger.Error("ping core database", "error", err)
		os.Exit(1)
	}
	if err := platformpostgres.RequireCurrentMigration(startupCtx, pool); err != nil {
		logger.Error("core database is not ready", "error", err)
		os.Exit(1)
	}

	catalogService := catalog.NewService(catalog.NewPostgresRepository(pool), time.Now)
	vaultClient, err := identity.NewVaultClient(settings.VaultURL, settings.VaultServiceToken, 3*time.Second)
	if err != nil {
		logger.Error("compose vault client", "error", err)
		os.Exit(1)
	}
	identityRepository := identity.NewPostgresRepository(pool)
	auditRepository := audit.NewPostgresRepository(pool)
	auditConfig := audit.RecorderConfig{
		PseudonymKey:      settings.AuditPseudonymKey,
		PseudonymKeyEpoch: settings.AuditKeyEpoch,
	}
	decisionRecorder, err := audit.NewDecisionRecorder(auditRepository, auditConfig)
	if err != nil {
		logger.Error("compose decision audit recorder", "error", err)
		os.Exit(1)
	}
	authorizationService, err := authz.NewService(authz.NewPostgresRepository(pool), decisionRecorder, time.Now)
	if err != nil {
		logger.Error("compose authorization service", "error", err)
		os.Exit(1)
	}
	if err := authorizationService.ValidateCatalog(startupCtx); err != nil {
		logger.Error("authorization catalog is not ready", "error", err)
		os.Exit(1)
	}
	identityService, err := identity.NewService(
		identityRepository,
		vaultClient,
		identity.ServiceConfig{CSRFKey: settings.SessionCSRFKey},
	)
	if err != nil {
		logger.Error("compose identity service", "error", err)
		os.Exit(1)
	}
	workgroupService, err := workgroups.NewService(
		identityService, workgroups.NewPostgresRepository(pool), authorizationService, time.Now,
	)
	if err != nil {
		logger.Error("compose workgroup service", "error", err)
		os.Exit(1)
	}
	publicUserProfileService, err := identity.NewPublicUserProfileService(
		identityService, identityRepository, authorizationService, time.Now,
	)
	if err != nil {
		logger.Error("compose public user profile service", "error", err)
		os.Exit(1)
	}
	accountProfileService, err := identity.NewAccountProfileService(
		identityService, identityRepository, authorizationService, time.Now,
	)
	if err != nil {
		logger.Error("compose account profile service", "error", err)
		os.Exit(1)
	}
	contentStore, err := objectstore.NewConfigured(startupCtx, settings.TorrentObjectStore)
	if err != nil {
		logger.Error("compose content object store", "error", err)
		os.Exit(1)
	}
	contentStores, err := objectstorage.NewRegistry(contentStore)
	if err != nil {
		logger.Error("compose content object store registry", "error", err)
		os.Exit(1)
	}
	imageDerivativeRepository, err := imaging.NewPostgresRepository(pool)
	if err != nil {
		logger.Error("compose image derivative repository", "error", err)
		os.Exit(1)
	}
	avatarService, err := identity.NewAvatarService(
		identityService, identityRepository, authorizationService, contentStores,
		identity.AvatarServiceConfig{
			ActiveBackendID: contentStore.BackendID(), Derivatives: imageDerivativeRepository,
		},
	)
	if err != nil {
		logger.Error("compose avatar service", "error", err)
		os.Exit(1)
	}
	accountAccessAppealService, err := identity.NewAccountAccessAppealService(
		vaultClient, identityRepository, authorizationService, time.Now,
	)
	if err != nil {
		logger.Error("compose account access appeal service", "error", err)
		os.Exit(1)
	}
	downloadRestrictionAppealService, err := identity.NewDownloadRestrictionAppealService(
		identityService, identityRepository, authorizationService, time.Now,
	)
	if err != nil {
		logger.Error("compose download restriction appeal service", "error", err)
		os.Exit(1)
	}
	webIdentityService, err := identity.NewWebAPIService(
		identityService, publicUserProfileService, accountProfileService,
		avatarService, accountAccessAppealService, downloadRestrictionAppealService,
	)
	if err != nil {
		logger.Error("compose Web identity API service", "error", err)
		os.Exit(1)
	}
	notificationRepository, err := notifications.NewPostgresRepository(pool)
	if err != nil {
		logger.Error("compose notification repository", "error", err)
		os.Exit(1)
	}
	notificationService, err := notifications.NewService(identityService, authorizationService, notificationRepository, time.Now)
	if err != nil {
		logger.Error("compose notification service", "error", err)
		os.Exit(1)
	}
	torrentBookmarkRepository, err := catalog.NewPostgresTorrentBookmarkRepository(pool)
	if err != nil {
		logger.Error("compose torrent bookmark repository", "error", err)
		os.Exit(1)
	}
	torrentBookmarkService, err := catalog.NewTorrentBookmarkService(
		identityService,
		authorizationService,
		torrentBookmarkRepository,
		time.Now,
	)
	if err != nil {
		logger.Error("compose torrent bookmark service", "error", err)
		os.Exit(1)
	}
	commentRepository, err := social.NewPostgresCommentRepository(pool)
	if err != nil {
		logger.Error("compose comment repository", "error", err)
		os.Exit(1)
	}
	commentService, err := social.NewCommentService(identityService, authorizationService, commentRepository, time.Now)
	if err != nil {
		logger.Error("compose comment service", "error", err)
		os.Exit(1)
	}
	socialPostRepository, err := social.NewPostgresPostRepository(pool)
	if err != nil {
		logger.Error("compose social post repository", "error", err)
		os.Exit(1)
	}
	socialPostService, err := social.NewPostService(identityService, authorizationService, socialPostRepository, time.Now)
	if err != nil {
		logger.Error("compose social post service", "error", err)
		os.Exit(1)
	}
	commentModerationEventBuilder, err := audit.NewCommentModerationDecisionEventBuilder(auditConfig)
	if err != nil {
		logger.Error("compose comment moderation audit builder", "error", err)
		os.Exit(1)
	}
	commentModerationRepository, err := social.NewPostgresCommentModerationRepository(
		pool,
		commentModerationEventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		logger.Error("compose comment moderation repository", "error", err)
		os.Exit(1)
	}
	commentModerationService, err := social.NewCommentModerationService(identityService, authorizationService, commentModerationRepository, time.Now)
	if err != nil {
		logger.Error("compose comment moderation service", "error", err)
		os.Exit(1)
	}
	trafficRepository, err := traffic.NewPostgresRepository(pool, time.Now)
	if err != nil {
		logger.Error("compose traffic overview repository", "error", err)
		os.Exit(1)
	}
	trafficOverviewService, err := traffic.NewService(identityService, authorizationService, trafficRepository, time.Now)
	if err != nil {
		logger.Error("compose traffic overview service", "error", err)
		os.Exit(1)
	}
	economyOverviewRepository, err := economy.NewPostgresOverviewRepository(pool)
	if err != nil {
		logger.Error("compose economy overview repository", "error", err)
		os.Exit(1)
	}
	economyOverviewService, err := economy.NewOverviewService(identityService, authorizationService, economyOverviewRepository, time.Now)
	if err != nil {
		logger.Error("compose economy overview service", "error", err)
		os.Exit(1)
	}
	attendanceRepository, err := attendance.NewPostgresRepository(pool)
	if err != nil {
		logger.Error("compose attendance repository", "error", err)
		os.Exit(1)
	}
	attendanceService, err := attendance.NewService(identityService, attendanceRepository, authorizationService, time.Now)
	if err != nil {
		logger.Error("compose attendance service", "error", err)
		os.Exit(1)
	}
	memberGiftRepository, err := membergift.NewPostgresRepository(pool)
	if err != nil {
		logger.Error("compose member gift repository", "error", err)
		os.Exit(1)
	}
	memberGiftService, err := membergift.NewService(identityService, memberGiftRepository, authorizationService, time.Now)
	if err != nil {
		logger.Error("compose member gift service", "error", err)
		os.Exit(1)
	}
	contentTipRepository, err := contenttip.NewPostgresRepository(pool)
	if err != nil {
		logger.Error("compose content tip repository", "error", err)
		os.Exit(1)
	}
	contentTipService, err := contenttip.NewService(identityService, contentTipRepository, authorizationService, time.Now)
	if err != nil {
		logger.Error("compose content tip service", "error", err)
		os.Exit(1)
	}
	seedingRewardTimelineRepository, err := seedingreward.NewPostgresTimelineRepository(pool)
	if err != nil {
		logger.Error("compose seeding reward policy repository", "error", err)
		os.Exit(1)
	}
	seedingRewardAdministrationService, err := seedingreward.NewAdministrationService(seedingRewardTimelineRepository, authorizationService, time.Now)
	if err != nil {
		logger.Error("compose seeding reward administration service", "error", err)
		os.Exit(1)
	}
	levelPolicyRepository, err := progression.NewPostgresLevelPolicyRepository(pool)
	if err != nil {
		logger.Error("compose level policy repository", "error", err)
		os.Exit(1)
	}
	levelPolicyAdministrationService, err := progression.NewLevelPolicyService(levelPolicyRepository, authorizationService, time.Now)
	if err != nil {
		logger.Error("compose level policy administration service", "error", err)
		os.Exit(1)
	}
	contributionExperienceRepository, err := progression.NewPostgresContributionExperiencePolicyRepository(pool)
	if err != nil {
		logger.Error("compose contribution experience policy repository", "error", err)
		os.Exit(1)
	}
	contributionExperienceService, err := progression.NewContributionExperiencePolicyService(contributionExperienceRepository, authorizationService, time.Now)
	if err != nil {
		logger.Error("compose contribution experience policy service", "error", err)
		os.Exit(1)
	}
	medalRepository, err := medals.NewPostgresRepository(pool)
	if err != nil {
		logger.Error("compose medal administration repository", "error", err)
		os.Exit(1)
	}
	medalAdministrationService, err := medals.NewService(medalRepository, authorizationService, time.Now)
	if err != nil {
		logger.Error("compose medal administration service", "error", err)
		os.Exit(1)
	}
	memberMedalService, err := medals.NewMemberService(identityService, medalRepository, authorizationService, time.Now)
	if err != nil {
		logger.Error("compose member medal service", "error", err)
		os.Exit(1)
	}
	operationsRepository, err := operations.NewPostgresRepository(
		pool,
		settings.SeedingEvidenceStartAt,
		settings.SeedingEvidenceClosureDelay,
	)
	if err != nil {
		logger.Error("compose operations repository", "error", err)
		os.Exit(1)
	}
	trackerOperationsClient, err := trackeroperations.NewClient(settings.TrackerOperationsOrigin, settings.TrackerServiceToken, 3*time.Second)
	if err != nil {
		logger.Error("compose Tracker operations client", "error", err)
		os.Exit(1)
	}
	trackerRuntimePolicyRepository, err := trackercontrol.NewPostgresRuntimePolicyRepository(pool)
	if err != nil {
		logger.Error("compose Tracker runtime policy repository", "error", err)
		os.Exit(1)
	}
	trackerRuntimePolicyService, err := trackercontrol.NewRuntimePolicyService(trackerRuntimePolicyRepository, authorizationService, time.Now)
	if err != nil {
		logger.Error("compose Tracker runtime policy service", "error", err)
		os.Exit(1)
	}
	settlementOperationsClient, err := settlementoperations.NewClient(settings.SettlementControlURL, settings.SettlementServiceToken, 3*time.Second)
	if err != nil {
		logger.Error("compose Settlement operations client", "error", err)
		os.Exit(1)
	}
	hnrPolicyRevisionEventBuilder, err := audit.NewHNRPolicyRevisionEventBuilder(auditConfig)
	if err != nil {
		logger.Error("compose H&R policy revision audit builder", "error", err)
		os.Exit(1)
	}
	hnrPolicyAdministrationRepository, err := hnradmin.NewPostgresRepository(
		pool,
		hnrPolicyRevisionEventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		logger.Error("compose H&R policy administration repository", "error", err)
		os.Exit(1)
	}
	hnrPolicyAdministrationService, err := hnradmin.NewService(
		hnrPolicyAdministrationRepository,
		authorizationService,
		settlementOperationsClient,
		time.Now,
	)
	if err != nil {
		logger.Error("compose H&R policy administration service", "error", err)
		os.Exit(1)
	}
	ratioWatchRepository, err := ratiowatch.NewPostgresRepository(pool)
	if err != nil {
		logger.Error("compose ratio watch repository", "error", err)
		os.Exit(1)
	}
	ratioWatchAdministrationService, err := ratiowatch.NewService(
		ratioWatchRepository, identityService, authorizationService, time.Now,
	)
	if err != nil {
		logger.Error("compose ratio watch administration service", "error", err)
		os.Exit(1)
	}
	newcomerRepository, err := newcomer.NewPostgresRepository(pool)
	if err != nil {
		logger.Error("compose newcomer assessment repository", "error", err)
		os.Exit(1)
	}
	newcomerAdministrationService, err := newcomer.NewService(
		newcomerRepository, identityService, authorizationService, time.Now,
	)
	if err != nil {
		logger.Error("compose newcomer assessment service", "error", err)
		os.Exit(1)
	}
	uploadPolicyRepository, err := torrents.NewPostgresUploadPolicyRepository(pool)
	if err != nil {
		logger.Error("compose torrent upload policy repository", "error", err)
		os.Exit(1)
	}
	uploadPolicyService, err := torrents.NewUploadPolicyService(uploadPolicyRepository, authorizationService, time.Now, settings.TorrentUploadMaxBytes)
	if err != nil {
		logger.Error("compose torrent upload policy service", "error", err)
		os.Exit(1)
	}
	operationsService, err := operations.NewService(operationsRepository, authorizationService, operations.StorageRuntime{
		BackendID:             settings.TorrentObjectStore.BackendID,
		Driver:                settings.TorrentObjectStore.Driver,
		TorrentUploadMaxBytes: int64(settings.TorrentUploadMaxBytes),
		ScreenshotMaxBytes:    torrents.MaxTorrentScreenshotBytes,
		AvatarMaxBytes:        identity.MaxAvatarBytes,
	}, imageDerivativeRepository, vaultClient, trackerOperationsClient, trackerRuntimePolicyService, settlementOperationsClient, time.Now, operations.ServiceConfig{UploadPolicies: uploadPolicyService})
	if err != nil {
		logger.Error("compose operations service", "error", err)
		os.Exit(1)
	}
	torrentStore := contentStore
	torrentStores := contentStores
	torrentUploadRepository, err := torrents.NewPostgresTorrentUploadRepository(pool)
	if err != nil {
		logger.Error("compose torrent upload repository", "error", err)
		os.Exit(1)
	}
	torrentReadRepository, err := torrents.NewPostgresTorrentReadRepository(pool)
	if err != nil {
		logger.Error("compose torrent read repository", "error", err)
		os.Exit(1)
	}
	torrentLifecycleEventBuilder, err := audit.NewTorrentLifecycleEventBuilder(auditConfig)
	if err != nil {
		logger.Error("compose torrent lifecycle audit builder", "error", err)
		os.Exit(1)
	}
	promotionCampaignEventBuilder, err := audit.NewPromotionCampaignEventBuilder(auditConfig)
	if err != nil {
		logger.Error("compose promotion campaign audit builder", "error", err)
		os.Exit(1)
	}
	promotionRepository, err := promotions.NewPostgresRepository(
		pool,
		promotionCampaignEventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		logger.Error("compose promotion campaign repository", "error", err)
		os.Exit(1)
	}
	promotionService, err := promotions.NewService(promotionRepository, authorizationService, time.Now)
	if err != nil {
		logger.Error("compose promotion campaign service", "error", err)
		os.Exit(1)
	}
	promotionProductService, err := promotions.NewProductService(identityService, promotionRepository, authorizationService, time.Now)
	if err != nil {
		logger.Error("compose promotion product service", "error", err)
		os.Exit(1)
	}
	promotionApplication, err := promotions.NewApplication(promotionService, promotionProductService)
	if err != nil {
		logger.Error("compose promotion application", "error", err)
		os.Exit(1)
	}
	torrentAdministrationRepository, err := torrents.NewPostgresTorrentAdministrationRepository(
		pool,
		torrentLifecycleEventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
		trackercontrol.NewEligibilityEventBuilder(nil),
		func(tx pgx.Tx) trackerevent.Appender { return trackercontrol.NewPostgresOutbox(tx) },
	)
	if err != nil {
		logger.Error("compose torrent administration repository", "error", err)
		os.Exit(1)
	}
	torrentAdministrationService, err := torrents.NewTorrentAdministrationService(
		torrentAdministrationRepository,
		authorizationService,
		time.Now,
		trackerOperationsClient,
	)
	if err != nil {
		logger.Error("compose torrent administration service", "error", err)
		os.Exit(1)
	}
	torrentReadService, err := torrents.NewTorrentReadService(
		identityService,
		authorizationService,
		torrentReadRepository,
		torrentStores,
		imageDerivativeRepository,
		time.Now,
		torrentAdministrationService,
	)
	if err != nil {
		logger.Error("compose torrent read service", "error", err)
		os.Exit(1)
	}
	trackerCredentialService, err := identity.NewTrackerCredentialService(vaultClient, identityRepository, time.Now)
	if err != nil {
		logger.Error("compose Tracker credential service", "error", err)
		os.Exit(1)
	}
	torrentDownloadRepository, err := torrents.NewPostgresTorrentDownloadRepository(pool)
	if err != nil {
		logger.Error("compose torrent download repository", "error", err)
		os.Exit(1)
	}
	torrentPurchaseRepository, err := torrentpurchase.NewPostgresRepository(pool)
	if err != nil {
		logger.Error("compose torrent purchase repository", "error", err)
		os.Exit(1)
	}
	torrentPurchaseService, err := torrentpurchase.NewService(identityService, torrentPurchaseRepository, authorizationService, time.Now)
	if err != nil {
		logger.Error("compose torrent purchase service", "error", err)
		os.Exit(1)
	}
	torrentDownloadService, err := torrents.NewTorrentDownloadService(
		identityService,
		authorizationService,
		torrentDownloadRepository,
		torrentPurchaseService,
		trackerCredentialService,
		torrentStores,
		torrents.TorrentDownloadServiceConfig{
			CanonicalTrackerOrigin: settings.TrackerCanonicalOrigin,
		},
	)
	if err != nil {
		logger.Error("compose torrent download service", "error", err)
		os.Exit(1)
	}
	rssRepository, err := rss.NewPostgresRepository(pool)
	if err != nil {
		logger.Error("compose RSS repository", "error", err)
		os.Exit(1)
	}
	rssService, err := rss.NewService(rssRepository, identityService, authorizationService, torrentDownloadService, rss.ServiceConfig{PublicOrigin: settings.PublicOrigin})
	if err != nil {
		logger.Error("compose RSS service", "error", err)
		os.Exit(1)
	}
	torrentReviewEventBuilder, err := audit.NewTorrentReviewEventBuilder(auditConfig)
	if err != nil {
		logger.Error("compose torrent review audit builder", "error", err)
		os.Exit(1)
	}
	torrentReviewRepository, err := review.NewPostgresRepository(
		pool,
		torrentReviewEventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
		trackercontrol.NewEligibilityEventBuilder(nil),
		func(tx pgx.Tx) trackerevent.Appender { return trackercontrol.NewPostgresOutbox(tx) },
		func(tx pgx.Tx) review.NotificationAppender { return notifications.NewPostgresReviewAppender(tx) },
	)
	if err != nil {
		logger.Error("compose torrent review repository", "error", err)
		os.Exit(1)
	}
	torrentUploadService, err := torrents.NewTorrentUploadService(
		identityService,
		authorizationService,
		torrentUploadRepository,
		torrentStores,
		torrents.TorrentUploadServiceConfig{
			ActiveBackendID: torrentStore.BackendID(), MaxMetainfoBytes: settings.TorrentUploadMaxBytes,
			TrustedPublishEntitlements: workgroupService, TrustedPublisher: torrentReviewRepository,
			UploadPolicies: uploadPolicyService,
		},
	)
	if err != nil {
		logger.Error("compose torrent upload service", "error", err)
		os.Exit(1)
	}
	torrentReviewService, err := review.NewService(
		identityService, torrentReviewRepository, authorizationService, workgroupService, time.Now,
	)
	if err != nil {
		logger.Error("compose torrent review service", "error", err)
		os.Exit(1)
	}
	torrentResubmissionRepository, err := review.NewPostgresResubmissionRepository(pool)
	if err != nil {
		logger.Error("compose torrent resubmission repository", "error", err)
		os.Exit(1)
	}
	torrentResubmissionService, err := review.NewResubmissionService(
		identityService,
		authorizationService,
		torrentResubmissionRepository,
		time.Now,
	)
	if err != nil {
		logger.Error("compose torrent resubmission service", "error", err)
		os.Exit(1)
	}
	torrentMaintenanceRepository, err := torrents.NewPostgresTorrentMaintenanceRepository(
		pool,
		torrentLifecycleEventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
		trackercontrol.NewEligibilityEventBuilder(nil),
		func(tx pgx.Tx) trackerevent.Appender { return trackercontrol.NewPostgresOutbox(tx) },
	)
	if err != nil {
		logger.Error("compose torrent maintenance repository", "error", err)
		os.Exit(1)
	}
	torrentMaintenanceService, err := torrents.NewTorrentMaintenanceService(
		identityService,
		authorizationService,
		torrentMaintenanceRepository,
		time.Now,
		torrents.TorrentMaintenanceServiceConfig{
			Stores: torrentStores, ActiveBackendID: torrentStore.BackendID(), UploadPolicies: uploadPolicyService,
		},
	)
	if err != nil {
		logger.Error("compose torrent maintenance service", "error", err)
		os.Exit(1)
	}
	grantRevocationEventBuilder, err := audit.NewGrantRevocationEventBuilder(auditConfig)
	if err != nil {
		logger.Error("compose grant revocation audit builder", "error", err)
		os.Exit(1)
	}
	staffBootstrapEventBuilder, err := audit.NewStaffBootstrapEventBuilder(auditConfig)
	if err != nil {
		logger.Error("compose staff bootstrap audit builder", "error", err)
		os.Exit(1)
	}
	categoryChangeEventBuilder, err := audit.NewCategoryChangeEventBuilder(auditConfig)
	if err != nil {
		logger.Error("compose category change audit builder", "error", err)
		os.Exit(1)
	}
	announcementChangeEventBuilder, err := audit.NewAnnouncementChangeEventBuilder(auditConfig)
	if err != nil {
		logger.Error("compose announcement change audit builder", "error", err)
		os.Exit(1)
	}
	siteDisplaySettingsChangeEventBuilder, err := audit.NewSiteDisplaySettingsChangeEventBuilder(auditConfig)
	if err != nil {
		logger.Error("compose site display settings audit builder", "error", err)
		os.Exit(1)
	}
	registrationPolicyChangeEventBuilder, err := audit.NewRegistrationPolicyChangeEventBuilder(auditConfig)
	if err != nil {
		logger.Error("compose registration policy audit builder", "error", err)
		os.Exit(1)
	}
	accountRestrictionEventBuilder, err := audit.NewAccountRestrictionEventBuilder(auditConfig)
	if err != nil {
		logger.Error("compose account restriction audit builder", "error", err)
		os.Exit(1)
	}
	registrationEventBuilder, err := audit.NewRegistrationCompletedEventBuilder(auditConfig)
	if err != nil {
		logger.Error("compose registration audit builder", "error", err)
		os.Exit(1)
	}
	registrationRepository, err := identity.NewPostgresRegistrationRepository(
		pool,
		registrationEventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		logger.Error("compose registration repository", "error", err)
		os.Exit(1)
	}
	registrationPolicyRepository, err := identity.NewPostgresRegistrationPolicyAdministrationRepository(
		pool,
		registrationPolicyChangeEventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		logger.Error("compose registration policy administration repository", "error", err)
		os.Exit(1)
	}
	humanVerification := identity.NewUnavailableHumanVerificationVerifier()
	if settings.TurnstileSecretKey != "" {
		humanVerification, err = identity.NewTurnstileVerifier(settings.TurnstileSecretKey, nil)
		if err != nil {
			logger.Error("compose Turnstile verifier", "error", err)
			os.Exit(1)
		}
	}
	registrationPolicyService, err := identity.NewRegistrationPolicyAdministrationService(
		registrationPolicyRepository,
		authorizationService,
		time.Now,
		identity.RegistrationPolicyAdministrationServiceConfig{
			HumanVerificationSecretConfigured: humanVerification.Configured(),
		},
	)
	if err != nil {
		logger.Error("compose registration policy administration service", "error", err)
		os.Exit(1)
	}
	registrationService, err := identity.NewRegistrationService(
		registrationRepository,
		vaultClient,
		time.Now,
		registrationPolicyService,
	)
	if err != nil {
		logger.Error("compose registration service", "error", err)
		os.Exit(1)
	}
	publicRegistrationPolicy, err := registrationService.PublicPolicy(startupCtx)
	if err != nil {
		logger.Error("load registration policy at startup", "error", err)
		os.Exit(1)
	}
	if publicRegistrationPolicy.HumanVerificationProvider == identity.HumanVerificationProviderTurnstile && !humanVerification.Configured() {
		logger.Error("registration policy requires Turnstile but PEERGO_TURNSTILE_SECRET_KEY is not configured")
		os.Exit(1)
	}
	invitationRepository, err := identity.NewPostgresInvitationRepository(pool)
	if err != nil {
		logger.Error("compose invitation repository", "error", err)
		os.Exit(1)
	}
	invitationService, err := identity.NewInvitationService(
		identityService,
		authorizationService,
		invitationRepository,
		time.Now,
	)
	if err != nil {
		logger.Error("compose invitation service", "error", err)
		os.Exit(1)
	}
	emailVerifiedEventBuilder, err := audit.NewEmailVerifiedEventBuilder(auditConfig)
	if err != nil {
		logger.Error("compose email verified audit builder", "error", err)
		os.Exit(1)
	}
	emailVerificationRepository, err := identity.NewPostgresEmailVerificationRepository(
		pool,
		emailVerifiedEventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		logger.Error("compose email verification repository", "error", err)
		os.Exit(1)
	}
	emailVerificationService, err := identity.NewEmailVerificationService(identityService, vaultClient, emailVerificationRepository)
	if err != nil {
		logger.Error("compose email verification service", "error", err)
		os.Exit(1)
	}
	passwordRecoveredEventBuilder, err := audit.NewPasswordRecoveredEventBuilder(auditConfig)
	if err != nil {
		logger.Error("compose password recovered audit builder", "error", err)
		os.Exit(1)
	}
	passwordRecoveryRepository, err := identity.NewPostgresPasswordRecoveryRepository(
		pool,
		passwordRecoveredEventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		logger.Error("compose password recovery repository", "error", err)
		os.Exit(1)
	}
	passwordRecoveryService, err := identity.NewPasswordRecoveryService(vaultClient, passwordRecoveryRepository)
	if err != nil {
		logger.Error("compose password recovery service", "error", err)
		os.Exit(1)
	}
	sessionRevocationEventBuilder, err := audit.NewSessionRevocationEventBuilder(auditConfig)
	if err != nil {
		logger.Error("compose session revocation audit builder", "error", err)
		os.Exit(1)
	}
	sessionSecurityRepository, err := identity.NewPostgresSessionSecurityRepository(
		pool,
		sessionRevocationEventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		logger.Error("compose session security repository", "error", err)
		os.Exit(1)
	}
	twoFactorEventBuilder, err := audit.NewTwoFactorChangeEventBuilder(auditConfig)
	if err != nil {
		logger.Error("compose two-factor change audit builder", "error", err)
		os.Exit(1)
	}
	twoFactorRepository, err := identity.NewPostgresTwoFactorChangeRepository(
		pool,
		twoFactorEventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		logger.Error("compose two-factor change repository", "error", err)
		os.Exit(1)
	}
	twoFactorService, err := identity.NewTwoFactorService(
		identityService,
		vaultClient,
		twoFactorRepository,
		authorizationService,
		identity.TwoFactorServiceConfig{},
	)
	if err != nil {
		logger.Error("compose two-factor service", "error", err)
		os.Exit(1)
	}
	sessionSecurityService, err := identity.NewSessionSecurityService(
		identityService,
		sessionSecurityRepository,
		vaultClient,
		authorizationService,
		identity.SessionSecurityServiceConfig{},
	)
	if err != nil {
		logger.Error("compose session security service", "error", err)
		os.Exit(1)
	}
	accountRestrictionRepository, err := identity.NewPostgresAccountRestrictionRepository(
		pool,
		accountRestrictionEventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		logger.Error("compose account restriction repository", "error", err)
		os.Exit(1)
	}
	userAdministrationService, err := identity.NewUserAdministrationService(
		identityRepository,
		accountRestrictionRepository,
		authorizationService,
		time.Now,
		vaultClient,
	)
	if err != nil {
		logger.Error("compose user administration service", "error", err)
		os.Exit(1)
	}
	categoryAdministrationRepository, err := catalog.NewPostgresCategoryAdministrationRepository(
		pool,
		categoryChangeEventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		logger.Error("compose category administration repository", "error", err)
		os.Exit(1)
	}
	categoryAdministrationService, err := catalog.NewCategoryAdministrationService(categoryAdministrationRepository, authorizationService, time.Now)
	if err != nil {
		logger.Error("compose category administration service", "error", err)
		os.Exit(1)
	}
	announcementAdministrationRepository, err := catalog.NewPostgresAnnouncementAdministrationRepository(
		pool,
		announcementChangeEventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		logger.Error("compose announcement administration repository", "error", err)
		os.Exit(1)
	}
	announcementAdministrationService, err := catalog.NewAnnouncementAdministrationService(announcementAdministrationRepository, authorizationService, time.Now)
	if err != nil {
		logger.Error("compose announcement administration service", "error", err)
		os.Exit(1)
	}
	siteDisplaySettingsRepository, err := catalog.NewPostgresSiteDisplaySettingsRepository(
		pool,
		siteDisplaySettingsChangeEventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		logger.Error("compose site display settings repository", "error", err)
		os.Exit(1)
	}
	siteDisplaySettingsService, err := catalog.NewSiteDisplaySettingsService(siteDisplaySettingsRepository, authorizationService, time.Now)
	if err != nil {
		logger.Error("compose site display settings service", "error", err)
		os.Exit(1)
	}
	staffEnrollmentRepository, err := identity.NewPostgresStaffEnrollmentRepository(
		pool,
		staffBootstrapEventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		logger.Error("compose staff enrollment repository", "error", err)
		os.Exit(1)
	}
	grantAdministrationRepository, err := authz.NewPostgresGrantAdministrationRepository(
		pool,
		grantRevocationEventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		logger.Error("compose grant administration repository", "error", err)
		os.Exit(1)
	}
	grantAdministrationService, err := authz.NewGrantAdministrationService(grantAdministrationRepository, authorizationService, time.Now, nil)
	if err != nil {
		logger.Error("compose grant administration service", "error", err)
		os.Exit(1)
	}
	staffRecordProtector, err := identity.NewRecordProtector(settings.WebAuthnRecordKey, settings.WebAuthnKeyEpoch, nil)
	if err != nil {
		logger.Error("compose staff WebAuthn record protection", "error", err)
		os.Exit(1)
	}
	staffWebAuthn, err := identity.NewGoWebAuthnCeremony(
		settings.WebAuthnRPID,
		"PeerGo Staff",
		settings.WebAuthnOrigins,
		5*time.Minute,
	)
	if err != nil {
		logger.Error("compose staff WebAuthn relying party", "error", err)
		os.Exit(1)
	}
	staffIdentityService, err := identity.NewStaffService(
		identityService,
		identityRepository,
		staffWebAuthn,
		staffRecordProtector,
		authorizationService,
		identity.StaffServiceConfig{CSRFKey: settings.SessionCSRFKey},
	)
	if err != nil {
		logger.Error("compose staff identity service", "error", err)
		os.Exit(1)
	}
	staffEnrollmentService, err := identity.NewStaffEnrollmentService(
		identityService,
		staffEnrollmentRepository,
		identityRepository,
		staffWebAuthn,
		staffRecordProtector,
		authorizationService,
		identity.StaffEnrollmentServiceConfig{},
	)
	if err != nil {
		logger.Error("compose staff enrollment service", "error", err)
		os.Exit(1)
	}
	handler, err := httpserver.New(httpserver.Dependencies{
		Readiness:                   pool.Ping,
		Catalog:                     catalogService,
		Identity:                    webIdentityService,
		Registration:                registrationService,
		HumanVerification:           humanVerification,
		Invitations:                 invitationService,
		EmailVerification:           emailVerificationService,
		PasswordRecovery:            passwordRecoveryService,
		SessionSecurity:             sessionSecurityService,
		TwoFactor:                   twoFactorService,
		StaffIdentity:               staffIdentityService,
		StaffEnrollment:             staffEnrollmentService,
		Authorization:               authorizationService,
		GrantAdministration:         grantAdministrationService,
		CategoryAdministration:      categoryAdministrationService,
		AnnouncementAdministration:  announcementAdministrationService,
		SiteDisplaySettings:         siteDisplaySettingsService,
		UserAdministration:          userAdministrationService,
		Notifications:               notificationService,
		TrafficOverview:             trafficOverviewService,
		EconomyOverview:             economyOverviewService,
		Attendance:                  attendanceService,
		MemberGifts:                 memberGiftService,
		ContentTips:                 contentTipService,
		Workgroups:                  workgroupService,
		SeedingRewardAdministration: seedingRewardAdministrationService,
		LevelPolicyAdministration:   levelPolicyAdministrationService,
		ContributionExperience:      contributionExperienceService,
		MedalAdministration:         medalAdministrationService,
		MemberMedals:                memberMedalService,
		HNRPolicyAdministration:     hnrPolicyAdministrationService,
		RatioWatchAdministration:    ratioWatchAdministrationService,
		NewcomerAdministration:      newcomerAdministrationService,
		Operations:                  operationsService,
		TorrentBookmarks:            torrentBookmarkService,
		Comments:                    commentService,
		SocialPosts:                 socialPostService,
		CommentModeration:           commentModerationService,
		TorrentRead:                 torrentReadService,
		TorrentUpload:               torrentUploadService,
		TorrentDownload:             torrentDownloadService,
		TorrentReview:               torrentReviewService,
		TorrentResubmission:         torrentResubmissionService,
		TorrentMaintenance:          torrentMaintenanceService,
		PromotionAdministration:     promotionApplication,
		RSS:                         rssService,
		TorrentUploadMaxBytes:       settings.TorrentUploadMaxBytes,
		SessionCookie: httpapi.SessionCookieConfig{
			Name:   settings.CookieName,
			Path:   "/",
			Secure: settings.CookieSecure,
		},
		StaffSessionCookie: httpapi.SessionCookieConfig{
			Name:   settings.StaffCookieName,
			Path:   "/api/v1/admin",
			Secure: settings.CookieSecure,
		},
		AllowedOrigins: settings.AllowedOrigins,
	}, logger)
	if err != nil {
		logger.Error("failed to compose core http server", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              settings.Address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("core api listening", "address", settings.Address)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("core api stopped unexpectedly", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("core api shutdown failed", "error", err)
		os.Exit(1)
	}
}
