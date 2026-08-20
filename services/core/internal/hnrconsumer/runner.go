// Package hnrconsumer binds the shared Core projection transport to the H&R
// projector. NATS source, consumer drift checks and ACK mechanics are reused
// from the established traffic transport.
package hnrconsumer

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/peergo/peergo/services/core/internal/modules/traffic"
	"github.com/peergo/peergo/services/core/internal/projectionrunner"
	"github.com/peergo/peergo/services/core/internal/trafficconsumer"
)

var ErrConfig = errors.New("Core H&R consumer configuration is invalid")

type RunnerConfig = projectionrunner.Config

type Runner struct {
	inner *projectionrunner.Runner[traffic.HNRApplyResult]
}

func NewRunner(source trafficconsumer.Source, projector traffic.HNRProjector, config RunnerConfig, now func() time.Time, logger *slog.Logger) (*Runner, error) {
	if projector == nil {
		return nil, ErrConfig
	}
	inner, err := projectionrunner.New(source, projector.ApplyHNR, config, projectionrunner.Semantics[traffic.HNRApplyResult]{
		Name: "H&R", EventID: func(result traffic.HNRApplyResult) any { return result.EventID },
		Duplicate: func(result traffic.HNRApplyResult) bool { return result.Duplicate },
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

func (runner *Runner) ProcessMessage(ctx context.Context, message trafficconsumer.Message) error {
	return runner.inner.ProcessMessage(ctx, message)
}
