package hnroutbox

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/peergo/peergo/services/settlement/internal/outboxdispatch"
)

type DispatcherConfig = outboxdispatch.Config

type Dispatcher struct {
	inner *outboxdispatch.Dispatcher[PendingEvent]
}

func NewDispatcher(repository Repository, publisher Publisher, config DispatcherConfig, now func() time.Time, logger *slog.Logger) (*Dispatcher, error) {
	inner, err := outboxdispatch.New(repository, publisher, config, outboxdispatch.Semantics[PendingEvent]{
		Name: "Settlement H&R", RetryCode: "hnr_publish_failed",
		EventID: func(event PendingEvent) any { return event.EventID },
		IsPermanent: func(err error) bool {
			return errors.Is(err, ErrInput) || errors.Is(err, ErrInvariant)
		},
	}, now, logger)
	if err != nil {
		return nil, ErrInput
	}
	return &Dispatcher{inner: inner}, nil
}

func (dispatcher *Dispatcher) RunOnce(ctx context.Context) (bool, error) {
	return dispatcher.inner.RunOnce(ctx)
}

func (dispatcher *Dispatcher) Run(ctx context.Context) error {
	return dispatcher.inner.Run(ctx)
}
