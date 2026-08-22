// Command seeding-reward-compensation-apply consumes one exact, private
// preview artifact after explicit SHA-256 approval. It only appends positive
// magic and experience entries through the normal Core ledger kernels.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/economy/seedingreward"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func main() {
	artifactPath := flag.String("artifact", "", "absolute mode-0600 compensation preview JSONL")
	approvedSHA256 := flag.String("approve-sha256", "", "explicitly approved lowercase artifact SHA-256")
	operatorReference := flag.String("operator-reference", "", "non-secret operator change reference")
	confirm := flag.String("confirm", "", "must equal APPLY:<approve-sha256>")
	batchSize := flag.Int("batch-size", 50, "records per atomic database batch (1-250)")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if flag.NArg() != 0 {
		fail(logger, "invalid compensation apply command", errors.New("positional arguments are not accepted"))
	}
	expectedDigest, err := seedingreward.ParseCompensationArtifactSHA256(*approvedSHA256)
	if err != nil || *confirm != "APPLY:"+*approvedSHA256 {
		fail(logger, "compensation approval was not explicit", errors.New("--confirm must exactly match the approved SHA-256"))
	}
	cleanArtifact, err := validateArtifactPath(*artifactPath)
	if err != nil {
		fail(logger, "invalid compensation artifact", err)
	}
	artifact, artifactSize, actualDigest, err := readArtifact(cleanArtifact)
	if err != nil {
		fail(logger, "read compensation artifact", err)
	}
	if actualDigest != expectedDigest {
		fail(logger, "compensation artifact approval mismatch", errors.New("artifact SHA-256 differs from explicit approval"))
	}
	coreURL := strings.TrimSpace(os.Getenv("PEERGO_CORE_DATABASE_URL"))
	if coreURL == "" {
		fail(logger, "invalid compensation database", errors.New("PEERGO_CORE_DATABASE_URL is required"))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	pool, err := openPool(startupCtx, coreURL)
	if err != nil {
		fail(logger, "open compensation Core database", err)
	}
	defer pool.Close()
	if err := platformpostgres.RequireCurrentMigration(startupCtx, pool); err != nil {
		fail(logger, "Core database is not ready", err)
	}
	repository, err := seedingreward.NewPostgresSettlementRepository(pool)
	if err != nil {
		fail(logger, "compose compensation apply", err)
	}
	progress := func(item seedingreward.CompensationApplyProgress) {
		if item.ProcessedRecords%1000 != 0 && item.ProcessedRecords != item.RecordCount {
			return
		}
		logger.Info("seeding reward compensation apply progress",
			"processed_records", item.ProcessedRecords, "record_count", item.RecordCount,
			"applied_records", item.AppliedRecords, "replayed_records", item.ReplayedRecords,
		)
	}
	result, err := repository.ApplyHistoricalCompensation(
		ctx, artifact, actualDigest, artifactSize, *operatorReference,
		time.Now().UTC(), *batchSize, progress,
	)
	if err != nil {
		fail(logger, "apply compensation artifact", err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fail(logger, "write compensation apply summary", err)
	}
}

func validateArtifactPath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed != raw || !filepath.IsAbs(trimmed) || filepath.Ext(trimmed) != ".jsonl" {
		return "", errors.New("--artifact must be an absolute .jsonl path")
	}
	cleaned := filepath.Clean(trimmed)
	if cleaned != trimmed {
		return "", errors.New("--artifact must be a clean path")
	}
	info, err := os.Lstat(cleaned)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("--artifact must be an existing regular file, not a symlink")
	}
	if info.Mode().Perm() != 0o600 {
		return "", errors.New("--artifact permissions must be exactly 0600")
	}
	return cleaned, nil
}

func readArtifact(path string) (seedingreward.CompensationArtifact, int64, [sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	file, err := os.Open(path)
	if err != nil {
		return seedingreward.CompensationArtifact{}, 0, digest, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || before.Size() < 1 || before.Size() > 1<<30 {
		return seedingreward.CompensationArtifact{}, 0, digest, errors.New("artifact size is outside the accepted range")
	}
	hasher := sha256.New()
	artifact, err := seedingreward.DecodeCompensationArtifact(io.TeeReader(file, hasher))
	if err != nil {
		return seedingreward.CompensationArtifact{}, 0, digest, err
	}
	copy(digest[:], hasher.Sum(nil))
	after, err := file.Stat()
	if err != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) ||
		before.Mode() != after.Mode() {
		return seedingreward.CompensationArtifact{}, 0, digest, errors.New("artifact changed while it was being validated")
	}
	if bytes.Equal(digest[:], make([]byte, sha256.Size)) {
		return seedingreward.CompensationArtifact{}, 0, digest, errors.New("artifact digest is invalid")
	}
	return artifact, before.Size(), digest, nil
}

func openPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("database URL is invalid")
	}
	config.MaxConns = 4
	config.MinConns = 0
	config.ConnConfig.RuntimeParams["application_name"] = "peergo-seeding-compensation-apply"
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

func fail(logger *slog.Logger, message string, err error) {
	logger.Error(message, "error", fmt.Sprint(err))
	os.Exit(1)
}
