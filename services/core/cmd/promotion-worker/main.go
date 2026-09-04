package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/services/core/internal/modules/economy/haremreward"
	"github.com/peergo/peergo/services/core/internal/modules/hnradmin"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/newcomer"
	"github.com/peergo/peergo/services/core/internal/modules/promotions"
	"github.com/peergo/peergo/services/core/internal/modules/ratiowatch"
	"github.com/peergo/peergo/services/core/internal/modules/traffic"
	"github.com/peergo/peergo/services/core/internal/modules/workgroups"
	"github.com/peergo/peergo/services/core/internal/platform/config"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
	"github.com/peergo/peergo/services/core/internal/platform/promotionledger"
	"github.com/peergo/peergo/services/core/internal/platform/settlementcontrol"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	drainWorkgroupBenefits := flag.Bool(
		"drain-workgroup-benefits",
		false,
		"deliver every currently available workgroup benefit command and exit",
	)
	flag.Parse()
	settings, err := config.LoadPromotionWorker()
	if err != nil {
		logger.Error("invalid promotion worker configuration", "error", err)
		os.Exit(1)
	}
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()
	pool, err := pgxpool.New(startupCtx, settings.DatabaseURL)
	if err != nil {
		logger.Error("open Core database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := pool.Ping(startupCtx); err != nil {
		logger.Error("ping Core database", "error", err)
		os.Exit(1)
	}
	if err := platformpostgres.RequireCurrentMigration(startupCtx, pool); err != nil {
		logger.Error("Core database is not ready", "error", err)
		os.Exit(1)
	}
	repository, err := promotions.NewPostgresDeliveryRepository(pool)
	if err != nil {
		logger.Error("compose promotion delivery repository", "error", err)
		os.Exit(1)
	}
	client, err := promotionledger.NewClient(settings.SettlementURL, settings.SettlementToken, 5*time.Second)
	if err != nil {
		logger.Error("compose Settlement promotion client", "error", err)
		os.Exit(1)
	}
	dispatcher, err := promotions.NewDispatcher(repository, client, promotions.DispatcherConfig{
		Label: "promotion", FailureCode: "settlement_delivery_failed",
	}, logger)
	if err != nil {
		logger.Error("compose promotion dispatcher", "error", err)
		os.Exit(1)
	}
	hnrRepository, err := hnradmin.NewPostgresDeliveryRepository(pool)
	if err != nil {
		logger.Error("compose H&R policy delivery repository", "error", err)
		os.Exit(1)
	}
	hnrClient, err := settlementcontrol.NewClient(
		settings.SettlementURL, "/internal/v1/settlement/hnr-policy-revisions",
		settings.SettlementToken, 5*time.Second,
	)
	if err != nil {
		logger.Error("compose Settlement H&R policy client", "error", err)
		os.Exit(1)
	}
	hnrDispatcher, err := settlementcontrol.NewDispatcher(hnrRepository, hnrClient, settlementcontrol.DispatcherConfig{
		Label: "H&R policy", FailureCode: "settlement_delivery_failed",
	}, logger)
	if err != nil {
		logger.Error("compose H&R policy dispatcher", "error", err)
		os.Exit(1)
	}
	benefitRepository, err := workgroups.NewPostgresBenefitDeliveryRepository(pool)
	if err != nil {
		logger.Error("compose workgroup benefit delivery repository", "error", err)
		os.Exit(1)
	}
	benefitBackfilled, err := benefitRepository.BackfillMissing(startupCtx)
	if err != nil {
		logger.Error("backfill workgroup benefit deliveries", "error", err)
		os.Exit(1)
	}
	if benefitBackfilled > 0 {
		logger.Info("backfilled workgroup benefit deliveries", "count", benefitBackfilled)
	}
	benefitClient, err := settlementcontrol.NewClient(
		settings.SettlementURL, "/internal/v1/settlement/workgroup-benefit-transitions",
		settings.SettlementToken, 5*time.Second,
	)
	if err != nil {
		logger.Error("compose Settlement workgroup benefit client", "error", err)
		os.Exit(1)
	}
	benefitDispatcher, err := settlementcontrol.NewDispatcher(benefitRepository, benefitClient, settlementcontrol.DispatcherConfig{
		Label: "workgroup benefit", FailureCode: "settlement_delivery_failed",
	}, logger)
	if err != nil {
		logger.Error("compose workgroup benefit dispatcher", "error", err)
		os.Exit(1)
	}
	if *drainWorkgroupBenefits {
		delivered := 0
		for {
			processed, drainErr := benefitDispatcher.RunOnce(context.Background())
			if drainErr != nil {
				logger.Error("drain workgroup benefit deliveries", "error", drainErr)
				os.Exit(1)
			}
			delivered += processed
			if processed == 0 {
				logger.Info("workgroup benefit deliveries drained", "processed", delivered)
				return
			}
		}
	}
	vipBenefitRepository, err := identity.NewPostgresVIPBenefitDeliveryRepository(pool)
	if err != nil {
		logger.Error("compose VIP benefit delivery repository", "error", err)
		os.Exit(1)
	}
	vipBenefitBackfilled, err := vipBenefitRepository.BackfillMissing(startupCtx)
	if err != nil {
		logger.Error("backfill VIP benefit deliveries", "error", err)
		os.Exit(1)
	}
	if vipBenefitBackfilled > 0 {
		logger.Info("backfilled VIP benefit deliveries", "count", vipBenefitBackfilled)
	}
	vipBenefitClient, err := settlementcontrol.NewClient(
		settings.SettlementURL, "/internal/v1/settlement/vip-benefit-transitions",
		settings.SettlementToken, 5*time.Second,
	)
	if err != nil {
		logger.Error("compose Settlement VIP benefit client", "error", err)
		os.Exit(1)
	}
	vipBenefitDispatcher, err := settlementcontrol.NewDispatcher(vipBenefitRepository, vipBenefitClient, settlementcontrol.DispatcherConfig{
		Label: "VIP benefit", FailureCode: "settlement_delivery_failed",
	}, logger)
	if err != nil {
		logger.Error("compose VIP benefit dispatcher", "error", err)
		os.Exit(1)
	}
	ratioRepository, err := ratiowatch.NewPostgresRepository(pool)
	if err != nil {
		logger.Error("compose ratio watch repository", "error", err)
		os.Exit(1)
	}
	ratioRunner, err := ratiowatch.NewRunner(
		ratioRepository, ratioRepository,
		settings.RatioWatchInterval, settings.RatioWatchBatch,
		logger, time.Now,
	)
	if err != nil {
		logger.Error("compose ratio watch runner", "error", err)
		os.Exit(1)
	}
	newcomerRepository, err := newcomer.NewPostgresRepository(pool)
	if err != nil {
		logger.Error("compose newcomer assessment repository", "error", err)
		os.Exit(1)
	}
	newcomerRunner, err := newcomer.NewRunner(
		newcomerRepository, newcomerRepository,
		settings.RatioWatchInterval, settings.RatioWatchBatch,
		logger, time.Now,
	)
	if err != nil {
		logger.Error("compose newcomer assessment runner", "error", err)
		os.Exit(1)
	}
	hnrEnforcementRepository, err := traffic.NewPostgresRepository(pool, time.Now)
	if err != nil {
		logger.Error("compose H&R enforcement repository", "error", err)
		os.Exit(1)
	}
	hnrEnforcementRunner, err := traffic.NewHNREnforcementRunner(
		hnrEnforcementRepository, hnrEnforcementRepository,
		settings.RatioWatchInterval, settings.RatioWatchBatch,
		logger, time.Now,
	)
	if err != nil {
		logger.Error("compose H&R enforcement runner", "error", err)
		os.Exit(1)
	}
	workgroupRepository := workgroups.NewPostgresRepository(pool)
	workgroupEnforcementRunner, err := workgroups.NewContributionEnforcementRunner(
		workgroupRepository, workgroupRepository,
		settings.WorkgroupEnforcementInterval, settings.WorkgroupEnforcementBatch,
		logger, time.Now,
	)
	if err != nil {
		logger.Error("compose workgroup contribution enforcement runner", "error", err)
		os.Exit(1)
	}
	haremRewardRepository, err := haremreward.NewPostgresRepository(pool)
	if err != nil {
		logger.Error("compose harem reward repository", "error", err)
		os.Exit(1)
	}
	haremRewardRunner, err := haremreward.NewRunner(
		haremRewardRepository,
		settings.HaremRewardInterval, settings.HaremRewardBatch,
		logger, time.Now,
	)
	if err != nil {
		logger.Error("compose harem reward runner", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Info("Core policy worker started", "controls", []string{"promotion", "hnr_policy", "workgroup_benefit", "vip_benefit", "ratio_watch", "newcomer_assessment", "hnr_enforcement", "workgroup_contribution_enforcement", "harem_reward"})
	errors := make(chan error, 9)
	go func() { errors <- dispatcher.Run(ctx) }()
	go func() { errors <- hnrDispatcher.Run(ctx) }()
	go func() { errors <- benefitDispatcher.Run(ctx) }()
	go func() { errors <- vipBenefitDispatcher.Run(ctx) }()
	go func() { errors <- ratioRunner.Run(ctx) }()
	go func() { errors <- newcomerRunner.Run(ctx) }()
	go func() { errors <- hnrEnforcementRunner.Run(ctx) }()
	go func() { errors <- workgroupEnforcementRunner.Run(ctx) }()
	go func() { errors <- haremRewardRunner.Run(ctx) }()
	if err := <-errors; err != nil {
		logger.Error("Core policy worker stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}
