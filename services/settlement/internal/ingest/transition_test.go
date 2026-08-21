package ingest

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
)

func TestEvaluateBuildsBaselineThenRawInterval(t *testing.T) {
	t.Parallel()
	first := ingestTestEvent(0, 100, 200, 300, "started")
	baseline, err := Evaluate(nil, first)
	if err != nil || baseline.Outcome != OutcomeBaseline || !baseline.Update || baseline.Interval != nil {
		t.Fatalf("baseline = %+v, %v", baseline, err)
	}
	second := ingestTestEvent(time.Minute, 150, 240, 0, "completed")
	settled, err := Evaluate(&baseline.State, second)
	if err != nil || settled.Outcome != OutcomeInterval || settled.Interval == nil {
		t.Fatalf("interval = %+v, %v", settled, err)
	}
	if settled.Interval.RawUploaded != 50 || settled.Interval.RawDownloaded != 40 || !settled.Interval.CompletedTransition {
		t.Fatalf("raw interval = %+v", settled.Interval)
	}
}

func TestEvaluateCopiesAnnounceNetworkEvidenceIntoRawInterval(t *testing.T) {
	t.Parallel()
	first := ingestTestEvent(0, 100, 200, 300, "started")
	baseline, err := Evaluate(nil, first)
	if err != nil {
		t.Fatal(err)
	}
	second := ingestTestEvent(time.Minute, 150, 240, 200, "")
	downloadFactor := int64(20_000)
	second.NetworkEvidence = &trackerannouncev1.NetworkEvidence{
		PolicySequence: 9, PolicyRevision: "seedbox-v9", Class: trackerannouncev1.NetworkClassSeedbox,
		RuleID: "trusted-box", UploadFactorBasisPoints: 5_000,
		DownloadFactorBasisPoints: &downloadFactor, SpeedLimitBytesPerSecond: 100 << 20,
	}
	result, err := Evaluate(&baseline.State, second)
	if err != nil || result.Interval == nil || result.Interval.NetworkEvidence == nil {
		t.Fatalf("interval = %+v, %v", result.Interval, err)
	}
	if !reflect.DeepEqual(result.Interval.NetworkEvidence, second.NetworkEvidence) {
		t.Fatalf("network evidence = %+v, want %+v", result.Interval.NetworkEvidence, second.NetworkEvidence)
	}
	second.NetworkEvidence.RuleID = "mutated"
	*second.NetworkEvidence.DownloadFactorBasisPoints = 30_000
	if result.Interval.NetworkEvidence.RuleID != "trusted-box" ||
		*result.Interval.NetworkEvidence.DownloadFactorBasisPoints != 20_000 {
		t.Fatal("raw interval retained a mutable event evidence pointer")
	}
}

func TestEvaluateDualStackRepeatDoesNotDoubleCount(t *testing.T) {
	t.Parallel()
	first, err := Evaluate(nil, ingestTestEvent(0, 100, 200, 300, ""))
	if err != nil {
		t.Fatal(err)
	}
	second := ingestTestEvent(time.Second, 100, 200, 300, "")
	second.AddressFamily = 6
	result, err := Evaluate(&first.State, second)
	if err != nil || result.Interval == nil || result.Interval.RawUploaded != 0 || result.Interval.RawDownloaded != 0 {
		t.Fatalf("dual-stack interval = %+v, %v", result, err)
	}
}

func TestEvaluateCounterRegressionStartsNewEpochWithoutNegativeDelta(t *testing.T) {
	t.Parallel()
	first, err := Evaluate(nil, ingestTestEvent(0, 500, 600, 10, ""))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Evaluate(&first.State, ingestTestEvent(time.Minute, 10, 20, 10, "started"))
	if err != nil || result.Outcome != OutcomeCounterReset || result.Interval != nil || result.State.Epoch != 2 {
		t.Fatalf("counter reset = %+v, %v", result, err)
	}
}

func TestEvaluateStoppedSessionReopensFromBaseline(t *testing.T) {
	t.Parallel()
	stopped, err := Evaluate(nil, ingestTestEvent(0, 10, 20, 0, "stopped"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Evaluate(&stopped.State, ingestTestEvent(time.Minute, 20, 30, 0, "started"))
	if err != nil || result.Outcome != OutcomeReopenedBaseline || result.Interval != nil || result.State.Epoch != 2 {
		t.Fatalf("reopened = %+v, %v", result, err)
	}
}

func TestEvaluateKeepsOutOfOrderEvidenceWithoutMovingSession(t *testing.T) {
	t.Parallel()
	first, err := Evaluate(nil, ingestTestEvent(time.Minute, 10, 20, 30, ""))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Evaluate(&first.State, ingestTestEvent(0, 15, 25, 20, ""))
	if err != nil || result.Outcome != OutcomeOutOfOrder || result.Update || result.Interval != nil || result.State != first.State {
		t.Fatalf("out-of-order = %+v, %v", result, err)
	}
}

func TestEvaluateTreatsSubMicrosecondReorderingAsOutOfOrder(t *testing.T) {
	t.Parallel()
	firstEvent := ingestTestEvent(350*time.Nanosecond, 0, 0, 0, "")
	first, err := Evaluate(nil, firstEvent)
	if err != nil {
		t.Fatal(err)
	}
	secondEvent := ingestTestEvent(319*time.Nanosecond, 0, 0, 0, "")
	secondEvent.EventID = "0198f20a-6da8-7e51-9c64-444444444444"
	result, err := Evaluate(&first.State, secondEvent)
	if err != nil || result.Outcome != OutcomeOutOfOrder || result.Update || result.Interval != nil {
		t.Fatalf("sub-microsecond reorder = %+v, %v", result, err)
	}
	want := firstEvent.ReceivedAt.UTC().Truncate(time.Microsecond)
	if !first.State.LastReceivedAt.Equal(want) || !result.State.LastReceivedAt.Equal(want) {
		t.Fatalf("canonical timestamps = %v and %v, want %v", first.State.LastReceivedAt, result.State.LastReceivedAt, want)
	}
}

func TestEvaluateRejectsSessionIdentityMutation(t *testing.T) {
	t.Parallel()
	first, err := Evaluate(nil, ingestTestEvent(0, 10, 20, 30, ""))
	if err != nil {
		t.Fatal(err)
	}
	second := ingestTestEvent(time.Minute, 15, 25, 20, "")
	second.InfoHashV1 = "0300000000000000000000000000000000000000"
	if _, err := Evaluate(&first.State, second); !errors.Is(err, ErrSessionInvariant) {
		t.Fatalf("identity mutation error = %v", err)
	}
}

func TestEvaluateRejectsCompletionTransitionWithoutStableIdentity(t *testing.T) {
	t.Parallel()
	baseline, err := Evaluate(nil, ingestTestEvent(0, 100, 200, 300, "started"))
	if err != nil {
		t.Fatal(err)
	}
	completed := ingestTestEvent(time.Minute, 150, 240, 0, "completed")
	completed.CompletionID = ""
	if _, err := Evaluate(&baseline.State, completed); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing completion identity error = %v", err)
	}
}

func ingestTestEvent(offset time.Duration, uploaded, downloaded, left int64, eventKind string) trackerannouncev1.Event {
	at := time.Date(2026, 8, 8, 20, 0, 0, 0, time.UTC).Add(offset)
	eventID := "0198f20a-6da8-7e51-9c64-111111111111"
	if offset != 0 {
		eventID = "0198f20a-6da8-7e51-9c64-222222222222"
	}
	event := trackerannouncev1.Event{
		SchemaVersion: trackerannouncev1.SchemaVersion, EventID: eventID, ReceivedAt: at,
		UserID: "0198f20a-6da8-7e51-9c64-333333333333", TorrentID: 42,
		InfoHashV1:    "0100000000000000000000000000000000000000",
		SessionToken:  "0200000000000000000000000000000000000000000000000000000000000000",
		AddressFamily: 4, Event: eventKind, Uploaded: uploaded, Downloaded: downloaded, Left: left,
		CredentialVersion: 1, TorrentControlSequence: 4, SubjectControlSequence: 5,
	}
	if eventKind == "completed" && left == 0 {
		event.CompletionID = "0300000000000000000000000000000000000000000000000000000000000000"
	}
	return event
}
