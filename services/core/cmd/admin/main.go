// Command admin grants or revokes PeerGo's practical site administrator role
// for one explicitly named existing account. It is intentionally a local
// operator command: no anonymous or already-logged-in HTTP caller can create
// the first administrator.
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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

type output struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Status   string `json:"status"`
	Changed  bool   `json:"changed"`
}

func main() {
	username := flag.String("username", "", "existing active username to grant or revoke as site administrator")
	revoke := flag.Bool("revoke", false, "revoke the site administrator role instead of granting it")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	databaseURL := strings.TrimSpace(os.Getenv("PEERGO_CORE_DATABASE_URL"))
	if databaseURL == "" {
		fail(logger, "invalid administrator command configuration", errors.New("PEERGO_CORE_DATABASE_URL is required"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
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

	repository, err := authz.NewSiteAdministratorRepository(pool)
	if err != nil {
		fail(logger, "compose administrator command", err)
	}
	result, err := repository.Set(ctx, *username, !*revoke, time.Now())
	switch {
	case errors.Is(err, authz.ErrSiteAdministratorInput):
		fail(logger, "invalid administrator command", errors.New("USERNAME is required and must identify one existing account"))
	case errors.Is(err, authz.ErrSiteAdministratorUser):
		fail(logger, "administrator account was not found", fmt.Errorf("username %q does not exist", strings.TrimSpace(*username)))
	case errors.Is(err, authz.ErrSiteAdministratorState):
		fail(logger, "administrator account is not active", fmt.Errorf("username %q must be active before it can be granted", strings.TrimSpace(*username)))
	case err != nil:
		fail(logger, "change site administrator", err)
	}

	status := "granted"
	if *revoke {
		status = "revoked"
	}
	if !result.Changed {
		status = "unchanged"
	}
	if err := json.NewEncoder(os.Stdout).Encode(output{
		UserID: result.UserID.String(), Username: result.Username,
		Role: "site_admin", Status: status, Changed: result.Changed,
	}); err != nil {
		fail(logger, "write administrator command result", err)
	}
}

func fail(logger *slog.Logger, message string, err error) {
	logger.Error(message, "error", err)
	os.Exit(1)
}
