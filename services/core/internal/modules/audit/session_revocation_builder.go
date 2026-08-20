package audit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

const sessionPseudonymDomain = "peergo:audit:session-pseudonym:v1\x00"

type SessionRevocationEventBuilder struct {
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewSessionRevocationEventBuilder(config RecorderConfig) (*SessionRevocationEventBuilder, error) {
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &SessionRevocationEventBuilder{
		pseudonymKey: config.PseudonymKey, pseudonymKeyEpoch: config.PseudonymKeyEpoch,
		newEventID: config.NewEventID,
	}, nil
}

func (builder *SessionRevocationEventBuilder) BuildSessionRevocationEvent(input identity.SessionRevocationAuditInput) (auditevent.Event, error) {
	command, result := input.Command, input.Result
	decision := command.Authorization
	if command.ID == uuid.Nil || command.UserID == uuid.Nil || command.OccurredAt.IsZero() ||
		result.RevokedWebSessions < 0 || result.RevokedStaffSessions < 0 ||
		!decision.Allow || decision.ID == uuid.Nil || decision.PolicyVersion == "" ||
		decision.RoleID == "" || decision.GrantID == uuid.Nil || decision.GrantVersion < 1 ||
		decision.MandateID == uuid.Nil || decision.EffectiveUntil.IsZero() {
		return auditevent.Event{}, errors.New("session revocation event is missing required evidence")
	}
	if command.Scope != identity.SessionRevocationSingle && command.Scope != identity.SessionRevocationOthers {
		return auditevent.Event{}, errors.New("session revocation event scope is invalid")
	}
	if (command.Scope == identity.SessionRevocationSingle) != (command.TargetSessionID != uuid.Nil) {
		return auditevent.Event{}, errors.New("session revocation event target disagrees with scope")
	}
	eventID := builder.newEventID()
	if eventID == uuid.Nil {
		return auditevent.Event{}, errors.New("audit event id generator returned nil")
	}
	payloadValue := SessionRevocationRecordedV1{
		SchemaVersion: SessionRevocationSchemaVersion, EventType: SessionRevocationEventType,
		EventID: eventID, OccurredAt: command.OccurredAt.UTC(), RevocationID: command.ID,
		Scope: string(command.Scope), UserPseudonym: subjectPseudonym(builder.pseudonymKey, command.UserID),
		PseudonymKeyEpoch:  builder.pseudonymKeyEpoch,
		RevokedWebSessions: result.RevokedWebSessions, RevokedStaffSessions: result.RevokedStaffSessions,
		CurrentSessionRevoked: result.CurrentSessionRevoked,
		DecisionID:            decision.ID, PolicyVersion: decision.PolicyVersion,
		Authority: DecisionAuthorityV1{
			RoleID: decision.RoleID, GrantID: decision.GrantID, GrantVersion: decision.GrantVersion,
			MandateID: decision.MandateID, EffectiveUntil: decision.EffectiveUntil.UTC(),
		},
	}
	if command.TargetSessionID != uuid.Nil {
		payloadValue.TargetSessionPseudonym = sessionPseudonym(builder.pseudonymKey, command.TargetSessionID)
	}
	payload, err := json.Marshal(payloadValue)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("encode session revocation event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return auditevent.Event{}, errors.New("session revocation event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return auditevent.Event{
		ID: eventID, Type: SessionRevocationEventType, SchemaVersion: SessionRevocationSchemaVersion,
		OccurredAt: command.OccurredAt.UTC(), Payload: payload, PayloadSHA256: digest,
	}, nil
}

func sessionPseudonym(key []byte, sessionID uuid.UUID) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(sessionPseudonymDomain))
	_, _ = mac.Write(sessionID[:])
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

var _ identity.SessionRevocationEventBuilder = (*SessionRevocationEventBuilder)(nil)
