package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/services/settlement/internal/config"
	"github.com/peergo/peergo/services/settlement/internal/hnr"
	"github.com/peergo/peergo/services/settlement/internal/hnrpolicy"
	"github.com/peergo/peergo/services/settlement/internal/operatorinput"
	platformpostgres "github.com/peergo/peergo/services/settlement/internal/platform/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger, os.Args[1:]); err != nil {
		logger.Error("Settlement H&R policy timeline append failed", "error", err)
		os.Exit(1)
	}
}

// run accepts one complete canonical H&R policy. It cannot edit an existing
// assessment or infer exemptions from promotion/freeleech configuration.
func run(logger *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("hnr-policy-timeline-append", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	entryID := flags.String("id", "", "immutable timeline entry UUID")
	policyFile := flags.String("policy-file", "", "canonical H&R policy JSON file")
	effectiveAt := flags.String("effective-at", "", "RFC3339 effective timestamp")
	userID := flags.String("user-id", "", "optional exact user UUID selector")
	torrentID := flags.String("torrent-id", "", "optional exact torrent numeric ID selector")
	torrentSequence := flags.String("torrent-control-sequence", "", "optional exact Tracker torrent control sequence")
	subjectSequence := flags.String("subject-control-sequence", "", "optional exact Tracker subject control sequence")
	if err := flags.Parse(args); err != nil {
		return err
	}
	revision, err := parseRevision(*entryID, *policyFile, *effectiveAt, *userID, *torrentID, *torrentSequence, *subjectSequence)
	if err != nil {
		return err
	}
	settings, err := config.LoadTrackerLedgerProcess()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, settings.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open Tracker Ledger database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping Tracker Ledger database: %w", err)
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		return err
	}
	repository, err := hnr.NewPostgresRepository(pool)
	if err != nil {
		return err
	}
	created, err := repository.AppendRevision(ctx, revision, time.Now().UTC())
	if err != nil {
		return err
	}
	logger.Info("Settlement immutable H&R policy timeline revision verified",
		"id", revision.ID, "created", created, "effective_at", revision.EffectiveAt, "mode", revision.Policy.Mode)
	return nil
}

func parseRevision(idValue, policyFile, effectiveAtValue, userIDValue, torrentIDValue, torrentSequenceValue, subjectSequenceValue string) (hnrpolicy.Revision, error) {
	id, err := operatorinput.RequiredUUID(idValue, "--id")
	if err != nil {
		return hnrpolicy.Revision{}, err
	}
	effectiveAt, err := operatorinput.RequiredTime(effectiveAtValue, "--effective-at")
	if err != nil {
		return hnrpolicy.Revision{}, err
	}
	encoded, err := os.ReadFile(strings.TrimSpace(policyFile))
	if err != nil {
		return hnrpolicy.Revision{}, fmt.Errorf("read --policy-file: %w", err)
	}
	policy, err := hnrpolicy.Decode(encoded)
	if err != nil {
		return hnrpolicy.Revision{}, fmt.Errorf("decode --policy-file: %w", err)
	}
	scope, err := operatorinput.ParseScope(operatorinput.ScopeValues{
		UserID: userIDValue, TorrentID: torrentIDValue,
		TorrentControlSequence: torrentSequenceValue, SubjectControlSequence: subjectSequenceValue,
	})
	if err != nil {
		return hnrpolicy.Revision{}, err
	}
	revision := hnrpolicy.Revision{ID: id, Scope: scope, EffectiveAt: effectiveAt, Policy: policy}
	if err := hnrpolicy.ValidateRevision(revision); err != nil {
		return hnrpolicy.Revision{}, err
	}
	return revision, nil
}
