package ingest

import (
	"context"
	"errors"
)

var ErrSourceInvariant = errors.New("Settlement message source invariant failed")

type Delivery struct {
	Stream        string
	Subject       string
	Sequence      uint64
	DeliveryCount uint64
	Payload       []byte
}

type ProcessResult struct {
	EventID   string
	Outcome   Outcome
	Duplicate bool
}

type Processor interface {
	Process(context.Context, Delivery) (ProcessResult, error)
}

type BatchProcessor interface {
	ProcessBatch(context.Context, []Delivery) ([]ProcessResult, error)
}

func IsPermanent(err error) bool {
	return errors.Is(err, ErrInvalidInput) || errors.Is(err, ErrSessionInvariant) ||
		errors.Is(err, ErrEventConflict) || errors.Is(err, ErrSourceInvariant)
}
