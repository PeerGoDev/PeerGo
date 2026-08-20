package trafficconsumer

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/peergo/peergo/services/core/internal/modules/traffic"
	"github.com/peergo/peergo/services/core/internal/projectionrunner"
)

var (
	ErrConfig        = errors.New("Core JetStream consumer configuration is invalid")
	ErrConsumerDrift = errors.New("existing Core JetStream consumer has unsafe configuration drift")
)

type RunnerConfig = projectionrunner.Config

type Runner struct {
	inner *projectionrunner.Runner[traffic.ApplyResult]
}

func NewRunner(source Source, projector traffic.Projector, config RunnerConfig, now func() time.Time, logger *slog.Logger) (*Runner, error) {
	if projector == nil {
		return nil, ErrConfig
	}
	inner, err := projectionrunner.New(source, projector.Apply, config, projectionrunner.Semantics[traffic.ApplyResult]{
		Name: "traffic", EventID: func(result traffic.ApplyResult) any { return result.EventID },
		Duplicate: func(result traffic.ApplyResult) bool { return result.Duplicate },
		IsPermanent: func(err error) bool {
			return errors.Is(err, traffic.ErrInput) || errors.Is(err, traffic.ErrConflict) || errors.Is(err, traffic.ErrInvariant)
		},
		Invariant: traffic.ErrInvariant,
	}, now, logger)
	if err != nil {
		return nil, ErrConfig
	}
	return &Runner{inner: inner}, nil
}

func (runner *Runner) Run(ctx context.Context) error { return runner.inner.Run(ctx) }

func (runner *Runner) processMessage(ctx context.Context, message Message) error {
	return runner.inner.ProcessMessage(ctx, message)
}
