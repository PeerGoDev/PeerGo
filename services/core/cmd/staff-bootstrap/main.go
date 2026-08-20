package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/audit"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/platform/config"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

type output struct {
	TicketID       string    `json:"ticket_id"`
	Username       string    `json:"username"`
	BootstrapToken string    `json:"bootstrap_token"`
	ExpiresAt      time.Time `json:"expires_at"`
}

func main() {
	username := flag.String("username", "", "existing active username that may enroll a staff credential")
	operatorReference := flag.String("operator-reference", "", "change, incident, or access-request reference for the issuing operator")
	lifetime := flag.Duration("ttl", 15*time.Minute, "ticket lifetime from 5m through 30m")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	settings, err := config.LoadStaffBootstrap()
	if err != nil {
		fail(logger, "invalid staff bootstrap configuration", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, settings.DatabaseURL)
	if err != nil {
		fail(logger, "open Core database", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		fail(logger, "ping Core database", err)
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		fail(logger, "Core database is not ready", err)
	}
	auditConfig := audit.RecorderConfig{
		PseudonymKey:      settings.AuditPseudonymKey,
		PseudonymKeyEpoch: settings.AuditKeyEpoch,
	}
	eventBuilder, err := audit.NewStaffBootstrapEventBuilder(auditConfig)
	if err != nil {
		fail(logger, "compose staff bootstrap audit builder", err)
	}
	repository, err := identity.NewPostgresStaffEnrollmentRepository(
		pool,
		eventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		fail(logger, "compose staff bootstrap repository", err)
	}
	issuer, err := identity.NewStaffBootstrapIssuer(repository, identity.StaffBootstrapIssuerConfig{})
	if err != nil {
		fail(logger, "compose staff bootstrap issuer", err)
	}
	ticket, err := issuer.Issue(ctx, identity.IssueStaffBootstrapTicketInput{
		Username:          *username,
		OperatorReference: *operatorReference,
		Lifetime:          *lifetime,
	})
	if err != nil {
		fail(logger, "issue staff bootstrap ticket", err)
	}
	// stdout is the single delivery channel for the raw token. All diagnostics
	// go to stderr and the audit event contains only its operator reference hash.
	if err := json.NewEncoder(os.Stdout).Encode(output{
		TicketID:       ticket.ID.String(),
		Username:       ticket.Username,
		BootstrapToken: ticket.RawToken,
		ExpiresAt:      ticket.ExpiresAt,
	}); err != nil {
		fail(logger, "write staff bootstrap ticket", fmt.Errorf("encode one-time output: %w", err))
	}
}

func fail(logger *slog.Logger, message string, err error) {
	logger.Error(message, "error", err)
	os.Exit(1)
}
