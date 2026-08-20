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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/privacy-vault/internal/credentials"
	"github.com/peergo/peergo/services/privacy-vault/internal/platform/config"
	"github.com/peergo/peergo/services/privacy-vault/internal/platform/httpserver"
	platformpostgres "github.com/peergo/peergo/services/privacy-vault/internal/platform/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	settings, err := config.Load()
	if err != nil {
		logger.Error("invalid vault configuration", "error", err)
		os.Exit(1)
	}

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()
	pool, err := pgxpool.New(startupCtx, settings.DatabaseURL)
	if err != nil {
		logger.Error("open vault database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := pool.Ping(startupCtx); err != nil {
		logger.Error("ping vault database", "error", err)
		os.Exit(1)
	}
	if err := platformpostgres.RequireCurrentMigration(startupCtx, pool); err != nil {
		logger.Error("vault database is not ready", "error", err)
		os.Exit(1)
	}

	repository := credentials.NewPostgresRepository(pool)
	secretProtector, err := credentials.NewSecretProtector(
		settings.TOTPEncryptionKey,
		settings.TOTPKeyEpoch,
		nil,
	)
	if err != nil {
		logger.Error("compose TOTP secret protector", "error", err)
		os.Exit(1)
	}
	twoFactorService, err := credentials.NewTwoFactorService(
		repository,
		secretProtector,
		settings.IdentifierKey,
		credentials.TwoFactorServiceConfig{},
	)
	if err != nil {
		logger.Error("compose two-factor service", "error", err)
		os.Exit(1)
	}
	trackerPasskeyProtector, err := credentials.NewSecretProtector(
		settings.TrackerPasskeyEncryptionKey,
		settings.TrackerPasskeyKeyEpoch,
		nil,
	)
	if err != nil {
		logger.Error("compose Tracker passkey protector", "error", err)
		os.Exit(1)
	}
	trackerCredentialService, err := credentials.NewTrackerCredentialService(
		repository,
		trackerPasskeyProtector,
		settings.TrackerPasskeyLookupKey,
		credentials.TrackerCredentialServiceConfig{},
	)
	if err != nil {
		logger.Error("compose Tracker credential service", "error", err)
		os.Exit(1)
	}
	service, err := credentials.NewService(repository, twoFactorService, settings.IdentifierKey)
	if err != nil {
		logger.Error("compose credential service", "error", err)
		os.Exit(1)
	}
	var emailSender credentials.TransactionalEmailSender
	if settings.Environment == "development" {
		emailSender, err = credentials.NewDevelopmentEmailOutboxSender(settings.EmailOutboxPath)
	} else {
		emailSender, err = credentials.NewRelayTransactionalEmailSender(
			settings.EmailDeliveryURL,
			settings.EmailDeliveryServiceToken,
			3*time.Second,
		)
	}
	if err != nil {
		logger.Error("compose transactional email sender", "error", err)
		os.Exit(1)
	}
	emailVerificationService, err := credentials.NewEmailVerificationService(
		repository,
		settings.IdentifierKey,
		emailSender,
		credentials.EmailVerificationServiceConfig{PublicURL: settings.EmailVerificationPublicURL},
	)
	if err != nil {
		logger.Error("compose email verification service", "error", err)
		os.Exit(1)
	}
	passwordRecoveryService, err := credentials.NewPasswordRecoveryService(
		repository,
		settings.IdentifierKey,
		emailSender,
		credentials.PasswordRecoveryServiceConfig{PublicURL: settings.PasswordRecoveryPublicURL},
	)
	if err != nil {
		logger.Error("compose password recovery service", "error", err)
		os.Exit(1)
	}
	deliveryMode := "https_relay"
	if settings.Environment == "development" {
		deliveryMode = "development_outbox"
	}
	emailOperationsService, err := credentials.NewEmailOperationsService(repository, emailSender, credentials.EmailOperationsRuntime{
		DeliveryMode:               deliveryMode,
		EmailVerificationPublicURL: settings.EmailVerificationPublicURL,
		PasswordRecoveryPublicURL:  settings.PasswordRecoveryPublicURL,
	}, time.Now)
	if err != nil {
		logger.Error("compose email operations service", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              settings.Address,
		Handler:           httpserver.New(service, emailVerificationService, passwordRecoveryService, emailOperationsService, twoFactorService, trackerCredentialService, repository, pool, settings.ServiceToken, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		logger.Info("privacy vault listening", "address", settings.Address)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("privacy vault stopped unexpectedly", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("privacy vault shutdown failed", "error", err)
		os.Exit(1)
	}
}
