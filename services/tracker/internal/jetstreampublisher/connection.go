package jetstreampublisher

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type ConnectionConfig struct {
	URLs            []string
	CredentialsFile string
	RootCAFile      string
	ConnectTimeout  time.Duration
	ReconnectWait   time.Duration
}

func Connect(config ConnectionConfig, logger *slog.Logger) (*nats.Conn, jetstream.JetStream, error) {
	if len(config.URLs) == 0 || config.ConnectTimeout < time.Millisecond || config.ConnectTimeout > time.Minute ||
		config.ReconnectWait < 10*time.Millisecond || config.ReconnectWait > time.Minute {
		return nil, nil, ErrConfig
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	options := []nats.Option{
		nats.Name("peergo-tracker-announce-publisher"),
		nats.Timeout(config.ConnectTimeout),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(config.ReconnectWait),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			logger.Warn("Tracker NATS connection interrupted", "error", err)
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			logger.Info("Tracker NATS connection restored")
		}),
		nats.ClosedHandler(func(connection *nats.Conn) {
			if err := connection.LastError(); err != nil {
				logger.Warn("Tracker NATS connection closed", "error", err)
			}
		}),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			logger.Warn("Tracker NATS asynchronous error", "error", err)
		}),
	}
	if config.CredentialsFile != "" {
		options = append(options, nats.UserCredentials(config.CredentialsFile))
	}
	if config.RootCAFile != "" {
		options = append(options, nats.RootCAs(config.RootCAFile))
	}
	connection, err := nats.Connect(strings.Join(config.URLs, ","), options...)
	if err != nil {
		return nil, nil, errors.Join(ErrConfig, err)
	}
	js, err := jetstream.New(connection)
	if err != nil {
		connection.Close()
		return nil, nil, errors.Join(ErrConfig, err)
	}
	return connection, js, nil
}
