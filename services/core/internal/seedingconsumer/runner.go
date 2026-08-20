// Package seedingconsumer binds the shared ordered Core projection transport
// to the closed hourly seeding evidence assembler.
package seedingconsumer

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/peergo/peergo/services/core/internal/modules/economy/seedingreward"
	"github.com/peergo/peergo/services/core/internal/projectionrunner"
	"github.com/peergo/peergo/services/core/internal/trafficconsumer"
)

var ErrConfig = errors.New("Core seeding evidence consumer configuration is invalid")

type RunnerConfig = projectionrunner.Config

type Runner struct {
	inner *projectionrunner.Runner[seedingreward.EvidenceApplyResult]
}

func NewRunner(source trafficconsumer.Source, projector seedingreward.EvidenceProjector, config RunnerConfig, now func() time.Time, logger *slog.Logger) (*Runner, error) {
	if projector == nil {
		return nil, ErrConfig
	}
	inner, err := projectionrunner.New(source, projector.ApplyEvidence, config, projectionrunner.Semantics[seedingreward.EvidenceApplyResult]{
		Name: "seeding evidence", EventID: func(result seedingreward.EvidenceApplyResult) any { return result.EventID },
		Duplicate: func(result seedingreward.EvidenceApplyResult) bool { return result.Duplicate },
		IsPermanent: func(err error) bool {
			return errors.Is(err, seedingreward.ErrInput) || errors.Is(err, seedingreward.ErrEvidenceConflict) || errors.Is(err, seedingreward.ErrInvariant)
		},
		Invariant: seedingreward.ErrInvariant,
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
