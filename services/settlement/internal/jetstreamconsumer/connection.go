package jetstreamconsumer

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

// Connect is shared by the Settlement runtime and its operator-only consumer
// provisioner. Credentials still come from their separate configuration
// fields, so the runtime never receives JetStream management authority.
func Connect(config ConnectionConfig, clientName string, logger *slog.Logger) (*nats.Conn, jetstream.JetStream, error) {
	if len(config.URLs) == 0 || strings.TrimSpace(clientName) == "" ||
		config.ConnectTimeout < 100*time.Millisecond || config.ConnectTimeout > time.Minute ||
		config.ReconnectWait < 100*time.Millisecond || config.ReconnectWait > time.Minute {
		return nil, nil, ErrConfig
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	options := []nats.Option{
		nats.Name(clientName),
		nats.Timeout(config.ConnectTimeout),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(config.ReconnectWait),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				logger.Warn("Settlement NATS connection interrupted", "error", err)
			}
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			logger.Info("Settlement NATS connection restored")
		}),
		nats.ClosedHandler(func(connection *nats.Conn) {
			if err := connection.LastError(); err != nil {
				logger.Warn("Settlement NATS connection closed", "error", err)
			}
		}),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			logger.Warn("Settlement NATS asynchronous error", "error", err)
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
