// Command tracker-rate-policy issues a narrowly scoped Tracker runtime-policy
// revision from the production host. It exists for recovery situations where
// an overly strict request budget prevents normal PT clients from reconnecting.
// The command still resolves a real active administrator, executes the normal
// authorization service and records the same immutable policy/audit evidence
// as the staff HTTP API; it never updates the policy table directly.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/contracts/go/trackerruntimepolicyv1"
	"github.com/peergo/peergo/services/core/internal/modules/audit"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/trackercontrol"
	"github.com/peergo/peergo/services/core/internal/platform/config"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

const confirmation = "APPLY_TRACKER_RATE_POLICY"

type rateTargets struct {
	UserRequestsPerMinute    int
	UserBurst                int
	AddressRequestsPerMinute int
	AddressBurst             int
}

type output struct {
	Username                 string `json:"username"`
	Sequence                 int64  `json:"sequence"`
	Revision                 string `json:"revision"`
	Changed                  bool   `json:"changed"`
	UserRequestsPerMinute    int    `json:"user_requests_per_minute"`
	UserBurst                int    `json:"user_burst"`
	AddressRequestsPerMinute int    `json:"address_requests_per_minute"`
	AddressBurst             int    `json:"address_burst"`
}

func main() {
	username := flag.String("username", "", "existing active site administrator")
	reason := flag.String("reason", "", "reviewed operator reason (5-1000 Unicode characters)")
	confirm := flag.String("confirm", "", "must equal "+confirmation)
	targets := rateTargets{}
	flag.IntVar(&targets.UserRequestsPerMinute, "user-requests-per-minute", 600, "per-user sustained request budget")
	flag.IntVar(&targets.UserBurst, "user-burst", 1200, "per-user reconnect burst budget")
	flag.IntVar(&targets.AddressRequestsPerMinute, "address-requests-per-minute", 5000, "per-address sustained request budget")
	flag.IntVar(&targets.AddressBurst, "address-burst", 10000, "per-address reconnect burst budget")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if flag.NArg() != 0 || strings.TrimSpace(*username) == "" || strings.TrimSpace(*reason) == "" ||
		strings.TrimSpace(*confirm) != confirmation || !targets.valid() {
		fail(logger, "invalid Tracker rate-policy command", errors.New("username, reason, exact confirmation and bounded rate values are required"))
	}

	settings, err := config.Load()
	if err != nil {
		fail(logger, "invalid Core configuration", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	actor, canonicalUsername, err := resolveActor(ctx, pool, *username)
	if err != nil {
		fail(logger, "resolve Tracker policy administrator", err)
	}
	auditRecorder, err := audit.NewDecisionRecorder(audit.NewPostgresRepository(pool), audit.RecorderConfig{
		PseudonymKey: settings.AuditPseudonymKey, PseudonymKeyEpoch: settings.AuditKeyEpoch,
	})
	if err != nil {
		fail(logger, "compose authorization audit recorder", err)
	}
	authorizer, err := authz.NewService(authz.NewPostgresRepository(pool), auditRecorder, time.Now)
	if err != nil {
		fail(logger, "compose authorization service", err)
	}
	if err := authorizer.ValidateCatalog(ctx); err != nil {
		fail(logger, "authorization catalog is not ready", err)
	}
	repository, err := trackercontrol.NewPostgresRuntimePolicyRepository(pool)
	if err != nil {
		fail(logger, "compose Tracker runtime-policy repository", err)
	}
	service, err := trackercontrol.NewRuntimePolicyService(repository, authorizer, time.Now)
	if err != nil {
		fail(logger, "compose Tracker runtime-policy service", err)
	}
	current, err := service.Current(ctx, actor)
	if err != nil {
		fail(logger, "authorize and read Tracker runtime policy", err)
	}
	policy, changed := targets.apply(current.Policy)
	issued := current
	if changed {
		issued, err = service.Issue(ctx, actor, trackercontrol.IssueRuntimePolicyInput{
			RequestID: uuid.New(), ExpectedSequence: current.Sequence,
			Policy: policy, Reason: strings.TrimSpace(*reason),
		})
		if err != nil {
			fail(logger, "issue Tracker rate-policy revision", err)
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(output{
		Username: canonicalUsername, Sequence: issued.Sequence, Revision: issued.Policy.Revision, Changed: changed,
		UserRequestsPerMinute: issued.Policy.UserRequestsPerMinute, UserBurst: issued.Policy.UserBurst,
		AddressRequestsPerMinute: issued.Policy.AddressRequestsPerMinute, AddressBurst: issued.Policy.AddressBurst,
	}); err != nil {
		fail(logger, "write Tracker rate-policy result", err)
	}
}

func (targets rateTargets) valid() bool {
	return targets.UserRequestsPerMinute >= 1 && targets.UserRequestsPerMinute <= 600 &&
		targets.UserBurst >= targets.UserRequestsPerMinute && targets.UserBurst <= 1200 &&
		targets.AddressRequestsPerMinute >= targets.UserRequestsPerMinute && targets.AddressRequestsPerMinute <= 5000 &&
		targets.AddressBurst >= targets.AddressRequestsPerMinute && targets.AddressBurst <= 10000
}

func (targets rateTargets) apply(policy trackerruntimepolicyv1.Policy) (trackerruntimepolicyv1.Policy, bool) {
	changed := policy.UserRequestsPerMinute != targets.UserRequestsPerMinute ||
		policy.UserBurst != targets.UserBurst ||
		policy.AddressRequestsPerMinute != targets.AddressRequestsPerMinute ||
		policy.AddressBurst != targets.AddressBurst
	policy.UserRequestsPerMinute = targets.UserRequestsPerMinute
	policy.UserBurst = targets.UserBurst
	policy.AddressRequestsPerMinute = targets.AddressRequestsPerMinute
	policy.AddressBurst = targets.AddressBurst
	return policy, changed
}

func resolveActor(ctx context.Context, pool *pgxpool.Pool, username string) (authz.StaffActor, string, error) {
	var userID uuid.UUID
	var canonicalUsername, status string
	err := pool.QueryRow(ctx, `
SELECT id, username, status
FROM identity.users
WHERE lower(username) = lower($1)`, strings.TrimSpace(username)).Scan(&userID, &canonicalUsername, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return authz.StaffActor{}, "", fmt.Errorf("username %q does not exist", strings.TrimSpace(username))
	}
	if err != nil {
		return authz.StaffActor{}, "", fmt.Errorf("read administrator: %w", err)
	}
	if status != string(authz.SubjectActive) {
		return authz.StaffActor{}, "", fmt.Errorf("username %q is not active", canonicalUsername)
	}
	return authz.StaffActor{Subject: authz.Subject{ID: userID, Status: authz.SubjectActive}}, canonicalUsername, nil
}

func fail(logger *slog.Logger, message string, err error) {
	logger.Error(message, "error", err)
	os.Exit(1)
}
