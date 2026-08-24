package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/legacyinvites"
	"github.com/peergo/peergo/services/core/internal/legacypersonalstate"
)

type settings struct {
	SourceDatabaseURL string
	CoreDatabaseURL   string
	RunID             uuid.UUID
	SnapshotSHA256    [sha256.Size]byte
	MappingVersion    string
	ImportedAt        time.Time
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	action := flag.String("action", "import", "import or verify")
	flag.Parse()
	if *action != "import" && *action != "verify" {
		logger.Error("unsupported legacy personal state action", "action", *action)
		os.Exit(2)
	}
	config, err := loadSettings()
	if err != nil {
		logger.Error("invalid legacy personal state configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	source, err := openPool(startupCtx, config.SourceDatabaseURL, true, "peergo-legacy-personal-state-source")
	if err != nil {
		logger.Error("open read-only PtYes source", "error", err)
		os.Exit(1)
	}
	defer source.Close()
	core, err := openPool(startupCtx, config.CoreDatabaseURL, *action == "verify", "peergo-legacy-personal-state-core")
	if err != nil {
		logger.Error("open PeerGo Core database", "error", err)
		os.Exit(1)
	}
	defer core.Close()
	importConfig := legacypersonalstate.Config{
		RunID: config.RunID, SnapshotSHA256: config.SnapshotSHA256,
		MappingVersion: config.MappingVersion, ImportedAt: config.ImportedAt,
	}
	progress := func(item legacypersonalstate.Progress) {
		logger.Info("legacy personal state progress", "phase", item.Phase, "processed", item.Processed, "expected", item.Expected)
	}
	var result legacypersonalstate.Result
	invitationConfig := legacyinvites.Config{
		RunID: config.RunID, SnapshotSHA256: config.SnapshotSHA256,
		MappingVersion: config.MappingVersion, ImportedAt: config.ImportedAt,
	}
	invitationProgress := func(item legacyinvites.Progress) {
		logger.Info("legacy invitation inventory progress", "phase", item.Phase, "processed", item.Processed, "expected", item.Expected)
	}
	var invitationResult legacyinvites.Result
	if *action == "verify" {
		result, err = legacypersonalstate.Verify(ctx, source, core, importConfig)
	} else {
		result, err = legacypersonalstate.Import(ctx, source, core, importConfig, progress)
	}
	if err != nil {
		logger.Error("legacy personal state operation failed", "action", *action, "error", err)
		os.Exit(1)
	}
	if *action == "verify" {
		invitationResult, err = legacyinvites.Verify(ctx, source, core, invitationConfig)
	} else {
		invitationResult, err = legacyinvites.Import(ctx, source, core, invitationConfig, invitationProgress)
	}
	if err != nil {
		logger.Error("legacy invitation inventory operation failed", "action", *action, "error", err)
		os.Exit(1)
	}
	logger.Info("legacy personal state operation completed",
		"action", *action, "run_id", result.RunID,
		"bookmark_source_rows", result.BookmarkSourceRows,
		"bookmark_distinct_pairs", result.BookmarkDistinctPairs,
		"bookmark_applied_rows", result.BookmarkAppliedRows,
		"bookmark_unresolved_rows", result.BookmarkUnresolvedRows,
		"invitation_source_rows", result.InvitationSourceRows,
		"invitation_relationships", result.InvitationRelationships,
		"invitation_unresolved_rows", result.InvitationUnresolvedRows,
		"harem_reward_source_rows", result.HaremRewardSourceRows,
		"harem_reward_users", result.HaremRewardUsers,
		"invite_reward_source_rows", result.InvitationRewardSourceRows,
		"invite_reward_users", result.InvitationRewardUsers,
		"invitation_balance_source_rows", invitationResult.BalanceSourceRows,
		"invitation_balance_total", invitationResult.BalanceTotal,
		"positive_invitation_balance_users", invitationResult.PositiveBalanceUsers,
		"invitation_code_source_rows", invitationResult.InvitationSourceRows,
		"claimed_invitation_code_rows", invitationResult.ClaimedInvitationRows,
		"active_legacy_invitation_tokens", invitationResult.ImportedActiveTokens,
		"duplicate", result.Duplicate,
		"invitation_inventory_duplicate", invitationResult.Duplicate,
	)
}

func loadSettings() (settings, error) {
	var result settings
	var err error
	result.SourceDatabaseURL = strings.TrimSpace(os.Getenv("PEERGO_LEGACY_SOURCE_DATABASE_URL"))
	result.CoreDatabaseURL = strings.TrimSpace(os.Getenv("PEERGO_CORE_DATABASE_URL"))
	if result.SourceDatabaseURL == "" || result.CoreDatabaseURL == "" || result.SourceDatabaseURL == result.CoreDatabaseURL {
		return settings{}, errors.New("distinct source and Core database URLs are required")
	}
	result.RunID, err = uuid.Parse(strings.TrimSpace(os.Getenv("PEERGO_LEGACY_RUN_ID")))
	if err != nil || result.RunID == uuid.Nil {
		return settings{}, errors.New("PEERGO_LEGACY_RUN_ID must be a non-zero UUID")
	}
	rawDigest := strings.TrimSpace(os.Getenv("PEERGO_LEGACY_SNAPSHOT_SHA256"))
	decoded, err := hex.DecodeString(rawDigest)
	if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != rawDigest {
		return settings{}, errors.New("PEERGO_LEGACY_SNAPSHOT_SHA256 must be 64 lowercase hex characters")
	}
	copy(result.SnapshotSHA256[:], decoded)
	result.MappingVersion = strings.TrimSpace(os.Getenv("PEERGO_LEGACY_MAPPING_VERSION"))
	if result.MappingVersion == "" {
		result.MappingVersion = "ptyes-v1"
	}
	result.ImportedAt, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(os.Getenv("PEERGO_LEGACY_OCCURRED_AT")))
	if err != nil || result.ImportedAt.IsZero() {
		return settings{}, errors.New("PEERGO_LEGACY_OCCURRED_AT must be a fixed RFC3339 timestamp")
	}
	return result, nil
}

func openPool(ctx context.Context, databaseURL string, readOnly bool, applicationName string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("database URL is invalid")
	}
	config.MaxConns = 4
	config.MinConns = 0
	config.ConnConfig.RuntimeParams["application_name"] = applicationName
	if readOnly {
		config.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}
