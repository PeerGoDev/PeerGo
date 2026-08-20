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

	"github.com/peergo/peergo/services/email-relay/internal/config"
	"github.com/peergo/peergo/services/email-relay/internal/delivery"
	"github.com/peergo/peergo/services/email-relay/internal/httpserver"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	settings, err := config.Load()
	if err != nil {
		logger.Error("invalid email Relay configuration", "error", err)
		os.Exit(1)
	}
	renderer, err := delivery.NewRenderer(settings.SiteName)
	if err != nil {
		logger.Error("compose email templates", "error", err)
		os.Exit(1)
	}
	mailer, err := delivery.NewSMTPSender(delivery.SMTPSettings{
		Host: settings.SMTP.Host, Port: settings.SMTP.Port, Username: settings.SMTP.Username, Password: settings.SMTP.Password,
		FromAddress: settings.SMTP.FromAddress, FromName: settings.SMTP.FromName, TLSMode: settings.SMTP.TLSMode, Timeout: settings.SMTP.Timeout,
	})
	if err != nil {
		logger.Error("compose SMTP sender", "error", err)
		os.Exit(1)
	}
	handler, err := httpserver.New(renderer, mailer, settings.ServiceToken, logger)
	if err != nil {
		logger.Error("compose email Relay HTTP server", "error", err)
		os.Exit(1)
	}
	server := &http.Server{Addr: settings.Address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		logger.Info("email Relay listening", "address", settings.Address)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("email Relay stopped unexpectedly", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("email Relay shutdown failed", "error", err)
		os.Exit(1)
	}
}
