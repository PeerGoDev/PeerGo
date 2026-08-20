// Package progression owns PeerGo's exact experience ledger and current level
// projection. Experience is not spendable currency, but it is still recorded
// through one immutable, idempotent entry point instead of direct user updates.
package progression

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInput               = errors.New("progression entry input is invalid")
	ErrUserNotFound        = errors.New("progression user was not found")
	ErrPolicyNotFound      = errors.New("experience policy revision was not found")
	ErrPolicyMismatch      = errors.New("experience policy does not match its source")
	ErrLevelPolicyNotFound = errors.New("level policy was not found")
	ErrInsufficientXP      = errors.New("experience balance cannot become negative")
	ErrIdempotencyConflict = errors.New("progression idempotency key was reused")
	ErrInvariant           = errors.New("progression ledger invariant failed")
)

var amountPattern = regexp.MustCompile(`^[+-]?[0-9]+(?:\.[0-9]{1,20})?$`)

// Amount is the canonical string form of PostgreSQL numeric(38,20). Keeping
// the representation private prevents float64 from entering persistence APIs.
type Amount struct {
	canonical string
}

func ParseAmount(raw string) (Amount, error) {
	value := strings.TrimSpace(raw)
	if !amountPattern.MatchString(value) {
		return Amount{}, ErrInput
	}
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimPrefix(strings.TrimPrefix(value, "+"), "-")
	parts := strings.SplitN(value, ".", 2)
	integer := strings.TrimLeft(parts[0], "0")
	if integer == "" {
		integer = "0"
	}
	if len(integer) > 18 {
		return Amount{}, ErrInput
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = strings.TrimRight(parts[1], "0")
	}
	canonical := integer
	if fraction != "" {
		canonical += "." + fraction
	}
	if negative && canonical != "0" {
		canonical = "-" + canonical
	}
	return Amount{canonical: canonical}, nil
}

func (amount Amount) String() string { return amount.canonical }

func (amount Amount) Sign() int {
	if amount.canonical == "" || amount.canonical == "0" {
		return 0
	}
	if strings.HasPrefix(amount.canonical, "-") {
		return -1
	}
	return 1
}

type EntryType string

const (
	EntryEarn       EntryType = "earn"
	EntryReversal   EntryType = "reversal"
	EntryAdjustment EntryType = "adjustment"
)

type SourceKind string

const (
	SourceSeedingReward       SourceKind = "seeding_reward"
	SourceTorrentPublish      SourceKind = "torrent_publish"
	SourceActivity            SourceKind = "activity"
	SourceAssessment          SourceKind = "assessment"
	SourceAdministratorAdjust SourceKind = "administrator_adjustment"
)

// RecordCommand is the only runtime write boundary for experience and level
// projection changes. Callers provide exact decimal text through Amount.
type RecordCommand struct {
	EntryID            uuid.UUID
	IdempotencyKey     string
	UserID             uuid.UUID
	EntryType          EntryType
	Amount             Amount
	SourceReference    string
	SourceKind         SourceKind
	PolicyRevision     string
	LevelPolicyVersion string
	PayloadSHA256      [32]byte
	MagicTransactionID uuid.UUID
	OccurredAt         time.Time
	RecordedAt         time.Time
}

type Entry struct {
	ID                 uuid.UUID
	EntrySequence      int64
	IdempotencyKey     string
	UserID             uuid.UUID
	EntryType          EntryType
	Amount             Amount
	BalanceAfter       Amount
	SourceReference    string
	SourceKind         SourceKind
	PolicyRevision     string
	LevelPolicyVersion string
	LevelAfter         int16
	PayloadSHA256      [32]byte
	MagicTransactionID uuid.UUID
	OccurredAt         time.Time
	RecordedAt         time.Time
	LevelTransition    bool
	Replayed           bool
}
