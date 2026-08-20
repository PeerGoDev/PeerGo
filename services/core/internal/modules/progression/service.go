package progression

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	progressionIdempotencyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9:._-]{0,191}$`)
	progressionSourcePattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9:._-]{0,127}$`)
	progressionPolicyPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	zeroProgressionSHA256         [32]byte
)

type Repository interface {
	Record(context.Context, RecordCommand) (Entry, error)
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, ErrInput
	}
	return &Service{repository: repository}, nil
}

func (service *Service) Record(ctx context.Context, command RecordCommand) (Entry, error) {
	normalized, err := normalizeRecordCommand(command)
	if err != nil {
		return Entry{}, err
	}
	return service.repository.Record(ctx, normalized)
}

func normalizeRecordCommand(command RecordCommand) (RecordCommand, error) {
	command.IdempotencyKey = strings.TrimSpace(command.IdempotencyKey)
	command.SourceReference = strings.TrimSpace(command.SourceReference)
	command.PolicyRevision = strings.TrimSpace(command.PolicyRevision)
	command.LevelPolicyVersion = strings.TrimSpace(command.LevelPolicyVersion)
	command.OccurredAt = command.OccurredAt.UTC().Truncate(time.Microsecond)
	command.RecordedAt = command.RecordedAt.UTC().Truncate(time.Microsecond)
	if command.EntryID == uuid.Nil || command.UserID == uuid.Nil ||
		!validEntryType(command.EntryType) || !validSourceKind(command.SourceKind) ||
		!utf8.ValidString(command.IdempotencyKey) || !progressionIdempotencyPattern.MatchString(command.IdempotencyKey) ||
		!utf8.ValidString(command.SourceReference) || !progressionSourcePattern.MatchString(command.SourceReference) ||
		!utf8.ValidString(command.PolicyRevision) || !progressionPolicyPattern.MatchString(command.PolicyRevision) ||
		!utf8.ValidString(command.LevelPolicyVersion) || !progressionPolicyPattern.MatchString(command.LevelPolicyVersion) ||
		command.Amount.Sign() == 0 || command.PayloadSHA256 == zeroProgressionSHA256 ||
		command.OccurredAt.IsZero() || command.RecordedAt.Before(command.OccurredAt) {
		return RecordCommand{}, ErrInput
	}
	if (command.EntryType == EntryEarn && command.Amount.Sign() < 0) ||
		(command.EntryType == EntryReversal && command.Amount.Sign() > 0) ||
		(command.SourceKind == SourceAdministratorAdjust && command.EntryType != EntryAdjustment) ||
		(command.SourceKind != SourceAdministratorAdjust && command.EntryType == EntryAdjustment) ||
		(command.SourceKind == SourceSeedingReward && command.MagicTransactionID == uuid.Nil) {
		return RecordCommand{}, ErrInput
	}
	return command, nil
}

func validEntryType(value EntryType) bool {
	switch value {
	case EntryEarn, EntryReversal, EntryAdjustment:
		return true
	default:
		return false
	}
}

func validSourceKind(value SourceKind) bool {
	switch value {
	case SourceSeedingReward, SourceTorrentPublish, SourceActivity,
		SourceAssessment, SourceAdministratorAdjust:
		return true
	default:
		return false
	}
}
