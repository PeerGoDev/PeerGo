package audit

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/economy/seedingreward"
)

type SeedingRewardRetryEventBuilder struct {
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewSeedingRewardRetryEventBuilder(config RecorderConfig) (*SeedingRewardRetryEventBuilder, error) {
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &SeedingRewardRetryEventBuilder{
		pseudonymKey: config.PseudonymKey, pseudonymKeyEpoch: config.PseudonymKeyEpoch,
		newEventID: config.NewEventID,
	}, nil
}

func (builder *SeedingRewardRetryEventBuilder) BuildSeedingRewardRetryEvent(
	input seedingreward.DeadWorkRetryAuditInput,
) (auditevent.Event, error) {
	command, result := input.Command, input.Result
	if command.ID == uuid.Nil || command.UserID == uuid.Nil || command.WindowStart.IsZero() ||
		command.OccurredAt.IsZero() || command.ExpectedAttempts < 1 ||
		command.ExpectedErrorCode == "" || command.OperatorReference == "" || command.Reason == "" ||
		result.RetryID != command.ID || result.UserID != command.UserID ||
		!result.WindowStart.Equal(command.WindowStart) || !result.RequeuedAt.Equal(command.OccurredAt) ||
		result.PreviousAttempts != command.ExpectedAttempts ||
		result.PreviousErrorCode != command.ExpectedErrorCode {
		return auditevent.Event{}, errors.New("seeding reward retry event is missing required evidence")
	}
	eventID := builder.newEventID()
	if eventID == uuid.Nil {
		return auditevent.Event{}, errors.New("audit event id generator returned nil")
	}
	payloadValue := SeedingRewardRetryRecordedV1{
		SchemaVersion: SeedingRewardRetrySchemaVersion, EventType: SeedingRewardRetryEventType,
		EventID: eventID, OccurredAt: command.OccurredAt.UTC(), RetryID: command.ID,
		WindowStart:       command.WindowStart.UTC(),
		UserPseudonym:     subjectPseudonym(builder.pseudonymKey, command.UserID),
		PseudonymKeyEpoch: builder.pseudonymKeyEpoch,
		PreviousAttempts:  command.ExpectedAttempts, PreviousErrorCode: command.ExpectedErrorCode,
		OperatorReferenceSHA256: digestLabel(command.OperatorReference),
		ReasonSHA256:            digestLabel(command.Reason), Result: "requeued",
	}
	payload, err := json.Marshal(payloadValue)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("encode seeding reward retry event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return auditevent.Event{}, errors.New("seeding reward retry event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return auditevent.Event{
		ID: eventID, Type: SeedingRewardRetryEventType,
		SchemaVersion: SeedingRewardRetrySchemaVersion,
		OccurredAt:    command.OccurredAt.UTC(), Payload: payload, PayloadSHA256: digest,
	}, nil
}

var _ seedingreward.DeadWorkRetryEventBuilder = (*SeedingRewardRetryEventBuilder)(nil)
