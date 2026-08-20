package economy

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

type recordingRepository struct {
	command RecordCommand
	result  Transaction
	err     error
}

func (repository *recordingRepository) Record(_ context.Context, command RecordCommand) (Transaction, error) {
	repository.command = command
	return repository.result, repository.err
}

func TestServiceNormalizesAndRecordsBalancedIntegerTransaction(t *testing.T) {
	repository := &recordingRepository{result: Transaction{ID: uuid.New()}}
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	memberID := uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff")
	now := time.Date(2026, 8, 15, 8, 30, 0, 123456789, time.FixedZone("CST", 8*60*60))
	digest := sha256.Sum256([]byte("activity:member:100"))
	_, err = service.Record(context.Background(), RecordCommand{
		TransactionID: uuid.New(), TransactionType: TransactionActivityReward,
		IdempotencyKey: " activity:member:2026-08-15 ", SourceReference: " activity:member:2026-08-15 ",
		PolicyRevision: " peergo-activity-v1 ", PayloadSHA256: digest,
		OccurredAt: now, RecordedAt: now.Add(time.Second),
		Postings: []PostingInput{
			{AccountID: memberID, Amount: 100},
			{AccountID: ActivityMintAccountID(), Amount: -100},
		},
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if repository.command.IdempotencyKey != "activity:member:2026-08-15" ||
		repository.command.SourceReference != "activity:member:2026-08-15" ||
		repository.command.PolicyRevision != "peergo-activity-v1" {
		t.Fatalf("normalized strings = %+v", repository.command)
	}
	if repository.command.OccurredAt.Location() != time.UTC || repository.command.OccurredAt.Nanosecond() != 123456000 {
		t.Fatalf("occurred_at = %s", repository.command.OccurredAt)
	}
	if repository.command.Postings[0].AccountID != ActivityMintAccountID() || repository.command.Postings[1].AccountID != memberID {
		t.Fatalf("postings not canonicalized: %+v", repository.command.Postings)
	}
}

func TestServiceRejectsInvalidMagicTransactions(t *testing.T) {
	service, err := NewService(&recordingRepository{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	now := time.Now().UTC()
	digest := sha256.Sum256([]byte("valid"))
	valid := RecordCommand{
		TransactionID: uuid.New(), TransactionType: TransactionTip,
		IdempotencyKey: "tip:019fcd83", SourceReference: "tip:019fcd83",
		PayloadSHA256: digest, OccurredAt: now, RecordedAt: now,
		Postings: []PostingInput{
			{AccountID: uuid.New(), Amount: -10},
			{AccountID: uuid.New(), Amount: 10},
		},
	}
	tests := map[string]func(RecordCommand) RecordCommand{
		"nil transaction": func(command RecordCommand) RecordCommand { command.TransactionID = uuid.Nil; return command },
		"unknown type":    func(command RecordCommand) RecordCommand { command.TransactionType = "legacy_opening"; return command },
		"unbalanced":      func(command RecordCommand) RecordCommand { command.Postings[1].Amount = 9; return command },
		"duplicate account": func(command RecordCommand) RecordCommand {
			command.Postings[1].AccountID = command.Postings[0].AccountID
			return command
		},
		"zero posting": func(command RecordCommand) RecordCommand { command.Postings[1].Amount = 0; return command },
		"zero digest":  func(command RecordCommand) RecordCommand { command.PayloadSHA256 = [32]byte{}; return command },
		"bad key":      func(command RecordCommand) RecordCommand { command.IdempotencyKey = "Tip With Spaces"; return command },
		"recorded before occurrence": func(command RecordCommand) RecordCommand {
			command.RecordedAt = command.OccurredAt.Add(-time.Second)
			return command
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			command := valid
			command.Postings = append([]PostingInput(nil), valid.Postings...)
			if _, err := service.Record(context.Background(), mutate(command)); !errors.Is(err, ErrInput) {
				t.Fatalf("Record() error = %v, want ErrInput", err)
			}
		})
	}
}

func TestAddInt64RejectsOverflow(t *testing.T) {
	if result, ok := addInt64(1, 2); !ok || result != 3 {
		t.Fatal("ordinary addition rejected")
	}
	if _, ok := addInt64(math.MaxInt64, 1); ok {
		t.Fatal("positive overflow accepted")
	}
	if _, ok := addInt64(math.MinInt64, -1); ok {
		t.Fatal("negative overflow accepted")
	}
}
