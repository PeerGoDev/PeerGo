// Package outboxdispatch owns the shared lease/publish/retry loop for typed
// Settlement outboxes. Each event family still owns its codec, PostgreSQL
// validation and JetStream publisher; only scheduling is shared.
package outboxdispatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

var ErrInput = errors.New("Settlement outbox dispatcher input is invalid")

var errorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func ValidErrorCode(value string) bool { return errorCodePattern.MatchString(value) }

type Config struct {
	LeaseDuration  time.Duration
	IdleInterval   time.Duration
	RetryBase      time.Duration
	PublishTimeout time.Duration
}

type Repository[T any] interface {
	ClaimNext(context.Context, time.Time, time.Duration) (T, bool, error)
	MarkPublished(context.Context, T, time.Time) error
	Release(context.Context, T, time.Time, string) error
}

type Publisher[T any] interface {
	Publish(context.Context, T) error
}

type Semantics[T any] struct {
	Name        string
	RetryCode   string
	EventID     func(T) any
	IsPermanent func(error) bool
}

type Dispatcher[T any] struct {
	repository Repository[T]
	publisher  Publisher[T]
	config     Config
	semantics  Semantics[T]
	now        func() time.Time
	logger     *slog.Logger
}

func New[T any](repository Repository[T], publisher Publisher[T], config Config, semantics Semantics[T], now func() time.Time, logger *slog.Logger) (*Dispatcher[T], error) {
	if repository == nil || publisher == nil || strings.TrimSpace(semantics.Name) == "" ||
		!ValidErrorCode(semantics.RetryCode) || semantics.EventID == nil || semantics.IsPermanent == nil {
		return nil, ErrInput
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = time.Minute
	}
	if config.IdleInterval == 0 {
		config.IdleInterval = time.Second
	}
	if config.RetryBase == 0 {
		config.RetryBase = 2 * time.Second
	}
	if config.PublishTimeout == 0 {
		config.PublishTimeout = 10 * time.Second
	}
	if config.LeaseDuration < time.Second || config.LeaseDuration > 10*time.Minute ||
		config.IdleInterval < 50*time.Millisecond || config.IdleInterval > time.Minute ||
		config.RetryBase < 100*time.Millisecond || config.RetryBase > time.Minute ||
		config.PublishTimeout < 100*time.Millisecond || config.PublishTimeout > time.Minute {
		return nil, ErrInput
	}
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Dispatcher[T]{
		repository: repository, publisher: publisher, config: config,
		semantics: semantics, now: now, logger: logger,
	}, nil
}

func (dispatcher *Dispatcher[T]) RunOnce(ctx context.Context) (bool, error) {
	now := dispatcher.now().UTC().Round(0)
	pending, found, err := dispatcher.repository.ClaimNext(ctx, now, dispatcher.config.LeaseDuration)
	if err != nil {
		return false, fmt.Errorf("claim %s outbox event: %w", dispatcher.semantics.Name, err)
	}
	if !found {
		return false, nil
	}
	eventID := dispatcher.semantics.EventID(pending)
	publishCtx, cancelPublish := context.WithTimeout(ctx, dispatcher.config.PublishTimeout)
	err = dispatcher.publisher.Publish(publishCtx, pending)
	cancelPublish()
	if err != nil {
		if dispatcher.semantics.IsPermanent(err) {
			return false, fmt.Errorf("permanent %s outbox failure: %w", dispatcher.semantics.Name, err)
		}
		if releaseErr := dispatcher.repository.Release(ctx, pending, now.Add(dispatcher.retryDelay(attemptsFromEvent(pending))), dispatcher.semantics.RetryCode); releaseErr != nil {
			return false, fmt.Errorf("release failed %s outbox event: %w", dispatcher.semantics.Name, releaseErr)
		}
		dispatcher.logger.Warn("Settlement outbox publish failed and will retry",
			"family", dispatcher.semantics.Name, "event_id", eventID, "error", err)
		return true, nil
	}
	if err := dispatcher.repository.MarkPublished(ctx, pending, now); err != nil {
		// A storage ACK may already exist. Leaving the row unmarked is safe: the
		// family publisher must reuse a stable Msg-Id and Core has an inbox.
		return false, fmt.Errorf("record %s outbox publish: %w", dispatcher.semantics.Name, err)
	}
	dispatcher.logger.Debug("Settlement outbox event published", "family", dispatcher.semantics.Name, "event_id", eventID)
	return true, nil
}

func (dispatcher *Dispatcher[T]) Run(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		processed, err := dispatcher.RunOnce(ctx)
		if err != nil {
			return err
		}
		if processed {
			timer.Reset(0)
		} else {
			timer.Reset(dispatcher.config.IdleInterval)
		}
	}
}

// attemptsFromEvent is intentionally optional: repositories already encode
// attempts in their typed pending value, but generics cannot name that field.
// Event families implement RetryAttempt when exponential backoff is desired.
func attemptsFromEvent[T any](event T) int32 {
	if value, ok := any(event).(interface{ RetryAttempt() int32 }); ok {
		return value.RetryAttempt()
	}
	return 1
}

func (dispatcher *Dispatcher[T]) retryDelay(attempt int32) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 6 {
		shift = 6
	}
	delay := dispatcher.config.RetryBase * time.Duration(1<<shift)
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}
