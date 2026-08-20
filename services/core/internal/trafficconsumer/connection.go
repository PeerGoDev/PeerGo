// Package trafficconsumer contains Core's established JetStream connection,
// pull-source and provisioning adapters plus the final Settlement traffic ACK
// runner. Swarm projection reuses the transport adapters while keeping its
// domain-specific commit/ACK policy in swarmconsumer.
package trafficconsumer

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/libraries/natsauth"
)

type ConnectionConfig struct {
	URLs            []string
	CredentialsFile string
	RootCAFile      string
	ConnectTimeout  time.Duration
	ReconnectWait   time.Duration
}

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
		nats.Name(clientName), nats.Timeout(config.ConnectTimeout), nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1), nats.ReconnectWait(config.ReconnectWait),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				logger.Warn("Core NATS connection interrupted", "error", err)
			}
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) { logger.Info("Core NATS connection restored") }),
		nats.ClosedHandler(func(connection *nats.Conn) {
			if err := connection.LastError(); err != nil {
				logger.Warn("Core NATS connection closed", "error", err)
			}
		}),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			logger.Warn("Core NATS asynchronous error", "error", err)
		}),
	}
	if config.CredentialsFile != "" {
		authOption, err := natsauth.OptionFromCredentialsFile(config.CredentialsFile)
		if err != nil {
			return nil, nil, errors.Join(ErrConfig, err)
		}
		options = append(options, authOption)
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
