package economy

import (
	"bytes"
	"context"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	minPostings = 2
	maxPostings = 32
)

var (
	idempotencyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9:._-]{0,191}$`)
	sourcePattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9:._-]{0,127}$`)
	policyPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	zeroSHA256         [32]byte
)

type Repository interface {
	Record(context.Context, RecordCommand) (Transaction, error)
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

func (service *Service) Record(ctx context.Context, command RecordCommand) (Transaction, error) {
	normalized, err := normalizeRecordCommand(command)
	if err != nil {
		return Transaction{}, err
	}
	return service.repository.Record(ctx, normalized)
}

func normalizeRecordCommand(command RecordCommand) (RecordCommand, error) {
	command.IdempotencyKey = strings.TrimSpace(command.IdempotencyKey)
	command.SourceReference = strings.TrimSpace(command.SourceReference)
	command.PolicyRevision = strings.TrimSpace(command.PolicyRevision)
	command.OccurredAt = command.OccurredAt.UTC().Truncate(time.Microsecond)
	command.RecordedAt = command.RecordedAt.UTC().Truncate(time.Microsecond)
	if command.TransactionID == uuid.Nil || !validTransactionType(command.TransactionType) ||
		!utf8.ValidString(command.IdempotencyKey) || !idempotencyPattern.MatchString(command.IdempotencyKey) ||
		!utf8.ValidString(command.SourceReference) || !sourcePattern.MatchString(command.SourceReference) ||
		(command.PolicyRevision != "" && (!utf8.ValidString(command.PolicyRevision) || !policyPattern.MatchString(command.PolicyRevision))) ||
		command.OccurredAt.IsZero() || command.RecordedAt.Before(command.OccurredAt) ||
		len(command.Postings) < minPostings || len(command.Postings) > maxPostings ||
		command.PayloadSHA256 == zeroSHA256 {
		return RecordCommand{}, ErrInput
	}

	postings := slices.Clone(command.Postings)
	slices.SortFunc(postings, func(left, right PostingInput) int {
		return bytes.Compare(left.AccountID[:], right.AccountID[:])
	})
	total := new(big.Int)
	for index, posting := range postings {
		if posting.AccountID == uuid.Nil || posting.Amount == 0 ||
			(index > 0 && posting.AccountID == postings[index-1].AccountID) {
			return RecordCommand{}, ErrInput
		}
		total.Add(total, big.NewInt(posting.Amount))
	}
	if total.Sign() != 0 {
		return RecordCommand{}, ErrInput
	}
	command.Postings = postings
	return command, nil
}

func validTransactionType(value TransactionType) bool {
	switch value {
	case TransactionSeedingReward, TransactionActivityReward, TransactionTorrentBuy,
		TransactionPromotionBuy, TransactionMedalBuy, TransactionMemberGift, TransactionTip,
		TransactionSocialRedPacketFund, TransactionSocialRedPacketClaim, TransactionRefund, TransactionAdjustment:
		return true
	default:
		return false
	}
}
