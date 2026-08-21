// Package ingest owns the first Settlement boundary: turning ordered absolute
// Tracker counters into immutable raw intervals. Economic policy is applied by
// a later stage so delayed events can use the policy revision effective at the
// event time instead of an invented current/default multiplier.
package ingest

import (
	"errors"
	"fmt"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
)

var (
	ErrInvalidInput     = errors.New("Settlement ingest input is invalid")
	ErrSessionInvariant = errors.New("Settlement session identity invariant failed")
	ErrEventConflict    = errors.New("Settlement event ID has conflicting payload")
)

type Outcome string

const (
	OutcomeBaseline         Outcome = "baseline"
	OutcomeInterval         Outcome = "interval"
	OutcomeCounterReset     Outcome = "counter_reset"
	OutcomeOutOfOrder       Outcome = "out_of_order"
	OutcomeReopenedBaseline Outcome = "reopened_baseline"
)

type Session struct {
	UserID                 string
	TorrentID              int64
	InfoHashV1             string
	SessionToken           string
	Epoch                  int64
	Version                int64
	LastEventID            string
	LastReceivedAt         time.Time
	LastEventKind          string
	LastUploaded           int64
	LastDownloaded         int64
	LastLeft               int64
	LastAddressFamily      int
	LastCredentialVersion  int64
	TorrentControlSequence int64
	SubjectControlSequence int64
}

type Interval struct {
	EventID                string
	PreviousEventID        string
	UserID                 string
	TorrentID              int64
	InfoHashV1             string
	SessionToken           string
	SessionEpoch           int64
	StartsAt               time.Time
	EndsAt                 time.Time
	EventKind              string
	AddressFamily          int
	CredentialVersion      int64
	TorrentControlSequence int64
	SubjectControlSequence int64
	PreviousUploaded       int64
	CurrentUploaded        int64
	PreviousDownloaded     int64
	CurrentDownloaded      int64
	PreviousLeft           int64
	CurrentLeft            int64
	RawUploaded            int64
	RawDownloaded          int64
	CompletedTransition    bool
	CompletionID           string
	NetworkEvidence        *trackerannouncev1.NetworkEvidence
}

type Transition struct {
	Outcome  Outcome
	Epoch    int64
	Update   bool
	State    Session
	Interval *Interval
}

// Evaluate is deliberately deterministic and side-effect free. The repository
// serializes one session before calling it and persists the inbox row, session
// update, and optional raw interval in one PostgreSQL transaction.
func Evaluate(previous *Session, event trackerannouncev1.Event) (Transition, error) {
	if trackerannouncev1.Validate(event) != nil {
		return Transition{}, ErrInvalidInput
	}
	// PostgreSQL timestamptz stores microseconds. Compare and persist the exact
	// same canonical precision so two announces inside one microsecond cannot
	// look ordered in Go and then collapse to an equal timestamp in the
	// database, where the monotonic-session trigger must reject them.
	event.ReceivedAt = canonicalIngestTime(event.ReceivedAt)
	if previous == nil {
		state := baseline(event, 1, 1)
		return Transition{Outcome: OutcomeBaseline, Epoch: state.Epoch, Update: true, State: state}, nil
	}
	canonicalPrevious := *previous
	canonicalPrevious.LastReceivedAt = canonicalIngestTime(canonicalPrevious.LastReceivedAt)
	previous = &canonicalPrevious
	if err := validateSession(*previous); err != nil {
		return Transition{}, err
	}
	if previous.UserID != event.UserID || previous.TorrentID != event.TorrentID ||
		previous.InfoHashV1 != event.InfoHashV1 || previous.SessionToken != event.SessionToken {
		return Transition{}, ErrSessionInvariant
	}
	if !event.ReceivedAt.After(previous.LastReceivedAt) {
		return Transition{Outcome: OutcomeOutOfOrder, Epoch: previous.Epoch, State: *previous}, nil
	}

	if previous.LastEventKind == "stopped" {
		state := baseline(event, previous.Epoch+1, previous.Version+1)
		return Transition{Outcome: OutcomeReopenedBaseline, Epoch: state.Epoch, Update: true, State: state}, nil
	}
	if event.Uploaded < previous.LastUploaded || event.Downloaded < previous.LastDownloaded {
		state := baseline(event, previous.Epoch+1, previous.Version+1)
		return Transition{Outcome: OutcomeCounterReset, Epoch: state.Epoch, Update: true, State: state}, nil
	}

	state := baseline(event, previous.Epoch, previous.Version+1)
	interval := &Interval{
		EventID: event.EventID, PreviousEventID: previous.LastEventID,
		UserID: event.UserID, TorrentID: event.TorrentID, InfoHashV1: event.InfoHashV1,
		SessionToken: event.SessionToken, SessionEpoch: previous.Epoch,
		StartsAt: previous.LastReceivedAt, EndsAt: event.ReceivedAt,
		EventKind: event.Event, AddressFamily: event.AddressFamily,
		CredentialVersion:      event.CredentialVersion,
		TorrentControlSequence: event.TorrentControlSequence,
		SubjectControlSequence: event.SubjectControlSequence,
		PreviousUploaded:       previous.LastUploaded, CurrentUploaded: event.Uploaded,
		PreviousDownloaded: previous.LastDownloaded, CurrentDownloaded: event.Downloaded,
		PreviousLeft: previous.LastLeft, CurrentLeft: event.Left,
		RawUploaded:   event.Uploaded - previous.LastUploaded,
		RawDownloaded: event.Downloaded - previous.LastDownloaded,
		// A left > 0 -> 0 counter change is a trustworthy completion only
		// when the Swarm Engine also supplied its stable identity. The engine
		// deliberately omits that identity after peer expiry or process restart,
		// so those intervals must still enter the traffic ledger without creating
		// a completion count or H&R obligation.
		CompletedTransition: previous.LastLeft > 0 && event.Left == 0 && event.CompletionID != "",
		NetworkEvidence:     cloneNetworkEvidence(event.NetworkEvidence),
	}
	if interval.CompletedTransition {
		// Repeated completed announces may carry a client retry identity but do
		// not become another H&R completion fact once the persisted session is
		// already at left == 0.
		interval.CompletionID = event.CompletionID
	}
	return Transition{Outcome: OutcomeInterval, Epoch: state.Epoch, Update: true, State: state, Interval: interval}, nil
}

func canonicalIngestTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

func cloneNetworkEvidence(evidence *trackerannouncev1.NetworkEvidence) *trackerannouncev1.NetworkEvidence {
	if evidence == nil {
		return nil
	}
	copy := *evidence
	if evidence.DownloadFactorBasisPoints != nil {
		factor := *evidence.DownloadFactorBasisPoints
		copy.DownloadFactorBasisPoints = &factor
	}
	return &copy
}

func baseline(event trackerannouncev1.Event, epoch, version int64) Session {
	return Session{
		UserID: event.UserID, TorrentID: event.TorrentID, InfoHashV1: event.InfoHashV1,
		SessionToken: event.SessionToken, Epoch: epoch, Version: version,
		LastEventID: event.EventID, LastReceivedAt: event.ReceivedAt,
		LastEventKind: event.Event, LastUploaded: event.Uploaded,
		LastDownloaded: event.Downloaded, LastLeft: event.Left,
		LastAddressFamily: event.AddressFamily, LastCredentialVersion: event.CredentialVersion,
		TorrentControlSequence: event.TorrentControlSequence,
		SubjectControlSequence: event.SubjectControlSequence,
	}
}

func validateSession(session Session) error {
	if session.Epoch < 1 || session.Version < 1 || session.LastEventID == "" || session.LastReceivedAt.IsZero() ||
		session.LastUploaded < 0 || session.LastDownloaded < 0 || session.LastLeft < 0 ||
		session.LastCredentialVersion < 1 || session.TorrentControlSequence < 1 || session.SubjectControlSequence < 1 ||
		(session.LastAddressFamily != 4 && session.LastAddressFamily != 6) {
		return fmt.Errorf("%w: invalid persisted state", ErrSessionInvariant)
	}
	return nil
}
