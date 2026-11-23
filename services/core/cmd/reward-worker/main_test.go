package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunWorkersCancelsPeerOnUnexpectedExit(t *testing.T) {
	workerFailure := errors.New("settlement failed")
	for _, returnErr := range []error{workerFailure, nil} {
		name := "failure"
		if returnErr == nil {
			name = "early return"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			peerStarted := make(chan struct{})
			peerStopped := make(chan struct{})
			err := runWorkers(ctx, func(context.Context) error {
				<-peerStarted
				return returnErr
			}, func(ctx context.Context) error {
				close(peerStarted)
				<-ctx.Done()
				close(peerStopped)
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), "seeding reward worker") ||
				(returnErr != nil && !errors.Is(err, returnErr)) {
				t.Fatalf("runWorkers() error = %v", err)
			}
			if ctx.Err() != nil {
				t.Fatalf("runWorkers() waited for the caller deadline: %v", ctx.Err())
			}
			select {
			case <-peerStopped:
			default:
				t.Fatal("runWorkers() returned before the contribution experience worker stopped")
			}
		})
	}
}

func TestRunWorkersWaitsForBothOnShutdown(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	started := make(chan struct{}, 2)
	stopped := make(chan struct{}, 2)
	run := func(ctx context.Context) error {
		started <- struct{}{}
		<-ctx.Done()
		stopped <- struct{}{}
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- runWorkers(ctx, run, run) }()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("both workers did not start")
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil || len(stopped) != 2 {
			t.Fatalf("runWorkers() stopped=%d error=%v", len(stopped), err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runWorkers() did not return after cancellation")
	}
}
