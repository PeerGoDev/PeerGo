package progression

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type recordingProgressionRepository struct {
	command RecordCommand
	result  Entry
	err     error
}

func (repository *recordingProgressionRepository) Record(_ context.Context, command RecordCommand) (Entry, error) {
	repository.command = command
	return repository.result, repository.err
}

func TestParseAmountCanonicalizesExactNumeric(t *testing.T) {
	tests := map[string]string{
		"0": "0", "+00012.340000": "12.34", "-00012.340000": "-12.34",
		"-0.000000": "0", "999999999999999999.12345678901234567890": "999999999999999999.1234567890123456789",
	}
	for raw, expected := range tests {
		amount, err := ParseAmount(raw)
		if err != nil || amount.String() != expected {
			t.Fatalf("ParseAmount(%q) = %q, %v; want %q", raw, amount.String(), err, expected)
		}
	}
	for _, raw := range []string{"", "1e2", "1.", ".1", "1.000000000000000000001", "1000000000000000000"} {
		if _, err := ParseAmount(raw); !errors.Is(err, ErrInput) {
			t.Fatalf("ParseAmount(%q) error = %v, want ErrInput", raw, err)
		}
	}
}

func TestServiceNormalizesExactProgressionEntry(t *testing.T) {
	repository := &recordingProgressionRepository{result: Entry{ID: uuid.New()}}
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	amount, _ := ParseAmount("00050.2500")
	now := time.Date(2026, 8, 15, 8, 30, 0, 123456789, time.FixedZone("CST", 8*60*60))
	digest := sha256.Sum256([]byte("torrent-publish:1234"))
	_, err = service.Record(context.Background(), RecordCommand{
		EntryID: uuid.New(), IdempotencyKey: " torrent-publish:1234 ", UserID: uuid.New(),
		EntryType: EntryEarn, Amount: amount, SourceReference: " torrent:1234 ",
		SourceKind: SourceTorrentPublish, PolicyRevision: " peergo-publish-v1 ",
		LevelPolicyVersion: " rousi-v1 ", PayloadSHA256: digest,
		OccurredAt: now, RecordedAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if repository.command.Amount.String() != "50.25" ||
		repository.command.IdempotencyKey != "torrent-publish:1234" ||
		repository.command.SourceReference != "torrent:1234" ||
		repository.command.PolicyRevision != "peergo-publish-v1" ||
		repository.command.LevelPolicyVersion != "rousi-v1" {
		t.Fatalf("normalized command = %+v", repository.command)
	}
	if repository.command.OccurredAt.Location() != time.UTC || repository.command.OccurredAt.Nanosecond() != 123456000 {
		t.Fatalf("occurred_at = %s", repository.command.OccurredAt)
	}
}

func TestServiceRejectsInvalidProgressionEntries(t *testing.T) {
	service, err := NewService(&recordingProgressionRepository{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	positive, _ := ParseAmount("1")
	negative, _ := ParseAmount("-1")
	now := time.Now().UTC()
	digest := sha256.Sum256([]byte("valid"))
	valid := RecordCommand{
		EntryID: uuid.New(), IdempotencyKey: "activity:daily:1", UserID: uuid.New(),
		EntryType: EntryEarn, Amount: positive, SourceReference: "activity:daily:1",
		SourceKind: SourceActivity, PolicyRevision: "peergo-activity-v1",
		LevelPolicyVersion: "rousi-v1", PayloadSHA256: digest,
		OccurredAt: now, RecordedAt: now,
	}
	tests := map[string]func(RecordCommand) RecordCommand{
		"nil entry":                  func(command RecordCommand) RecordCommand { command.EntryID = uuid.Nil; return command },
		"nil user":                   func(command RecordCommand) RecordCommand { command.UserID = uuid.Nil; return command },
		"zero amount":                func(command RecordCommand) RecordCommand { command.Amount = Amount{}; return command },
		"negative earn":              func(command RecordCommand) RecordCommand { command.Amount = negative; return command },
		"positive reversal":          func(command RecordCommand) RecordCommand { command.EntryType = EntryReversal; return command },
		"adjustment source mismatch": func(command RecordCommand) RecordCommand { command.EntryType = EntryAdjustment; return command },
		"administrator must adjust": func(command RecordCommand) RecordCommand {
			command.SourceKind = SourceAdministratorAdjust
			return command
		},
		"seeding link missing": func(command RecordCommand) RecordCommand { command.SourceKind = SourceSeedingReward; return command },
		"bad key":              func(command RecordCommand) RecordCommand { command.IdempotencyKey = "Bad Key"; return command },
		"zero digest":          func(command RecordCommand) RecordCommand { command.PayloadSHA256 = [32]byte{}; return command },
		"recorded first":       func(command RecordCommand) RecordCommand { command.RecordedAt = now.Add(-time.Second); return command },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := service.Record(context.Background(), mutate(valid)); !errors.Is(err, ErrInput) {
				t.Fatalf("Record() error = %v, want ErrInput", err)
			}
		})
	}
}
