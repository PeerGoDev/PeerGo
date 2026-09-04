package audit

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/economy/seedingreward"
)

func TestSeedingRewardRetryBuilderPseudonymizesTargetAndHashesOperatorText(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 31, 5, 30, 0, 0, time.UTC)
	eventID := uuid.MustParse("0198f20a-6da8-7e51-9c64-313131313131")
	retryID := uuid.MustParse("0198f20a-6da8-7e51-9c64-323232323232")
	userID := uuid.MustParse("0198f20a-6da8-7e51-9c64-333333333333")
	builder, err := NewSeedingRewardRetryEventBuilder(RecorderConfig{
		PseudonymKey:      []byte("0123456789abcdef0123456789abcdef"),
		PseudonymKeyEpoch: "test-2026-08", NewEventID: func() uuid.UUID { return eventID },
	})
	if err != nil {
		t.Fatal(err)
	}
	operatorReference := "incident:reward-zero-eligible"
	reason := "修复审核中种子产生零可奖励窗口后的结算误判。"
	command := seedingreward.DeadWorkRetryCommand{
		ID: retryID, WindowStart: now.Add(-time.Hour), UserID: userID,
		ExpectedAttempts: 10, ExpectedErrorCode: "invariant_failed",
		OperatorReference: operatorReference, Reason: reason, OccurredAt: now,
	}
	event, err := builder.BuildSeedingRewardRetryEvent(seedingreward.DeadWorkRetryAuditInput{
		Command: command,
		Result: seedingreward.DeadWorkRetryResult{
			RetryID: retryID, WindowStart: command.WindowStart, UserID: userID,
			PreviousAttempts: 10, PreviousErrorCode: "invariant_failed", RequeuedAt: now,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.ID != eventID || event.Type != SeedingRewardRetryEventType ||
		event.SchemaVersion != SeedingRewardRetrySchemaVersion {
		t.Fatalf("event envelope = %+v", event)
	}
	if bytes.Contains(event.Payload, []byte(userID.String())) ||
		bytes.Contains(event.Payload, []byte(operatorReference)) || bytes.Contains(event.Payload, []byte(reason)) {
		t.Fatalf("audit payload leaked target or operator text: %s", event.Payload)
	}
	var payload SeedingRewardRetryRecordedV1
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RetryID != retryID || payload.PreviousAttempts != 10 ||
		payload.PreviousErrorCode != "invariant_failed" || payload.Result != "requeued" ||
		payload.UserPseudonym == "" || payload.OperatorReferenceSHA256 == "" || payload.ReasonSHA256 == "" {
		t.Fatalf("payload = %+v", payload)
	}
}
