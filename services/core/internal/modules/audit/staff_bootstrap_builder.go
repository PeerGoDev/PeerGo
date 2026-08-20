package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

// StaffBootstrapEventBuilder owns the immutable bootstrap evidence contract.
// It hashes operator references, credential IDs and labels before payload
// construction; the raw bootstrap token is not accepted by this boundary.
type StaffBootstrapEventBuilder struct {
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewStaffBootstrapEventBuilder(config RecorderConfig) (*StaffBootstrapEventBuilder, error) {
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &StaffBootstrapEventBuilder{
		pseudonymKey:      config.PseudonymKey,
		pseudonymKeyEpoch: config.PseudonymKeyEpoch,
		newEventID:        config.NewEventID,
	}, nil
}

func (builder *StaffBootstrapEventBuilder) BuildStaffBootstrapEvent(input identity.StaffBootstrapAuditInput) (auditevent.Event, error) {
	if err := validateStaffBootstrapAuditInput(input); err != nil {
		return auditevent.Event{}, err
	}
	eventID := builder.newEventID()
	if eventID == uuid.Nil {
		return auditevent.Event{}, errors.New("audit event id generator returned nil")
	}
	operatorDigest := input.OperatorReferenceSHA256
	if input.Transition == identity.StaffBootstrapTicketIssued {
		digest := sha256.Sum256([]byte(strings.TrimSpace(input.OperatorReference)))
		operatorDigest = digest[:]
	}
	event := StaffCredentialBootstrapRecordedV1{
		SchemaVersion:           StaffBootstrapSchemaVersion,
		EventType:               StaffBootstrapEventType,
		EventID:                 eventID,
		OccurredAt:              input.OccurredAt.UTC(),
		TicketID:                input.TicketID,
		Transition:              string(input.Transition),
		TargetPseudonym:         subjectPseudonym(builder.pseudonymKey, input.TargetUserID),
		PseudonymKeyEpoch:       builder.pseudonymKeyEpoch,
		OperatorReferenceSHA256: digestBytesLabel(operatorDigest),
		TicketExpiresAt:         input.ExpiresAt.UTC(),
	}
	if input.Transition == identity.StaffBootstrapCredentialEnrolled {
		challengeID := input.ChallengeID
		decisionID := input.Authorization.ID
		event.ChallengeID = &challengeID
		event.CredentialIDSHA256 = digestBytesLabel(input.CredentialID)
		event.LabelSHA256 = digestLabel(input.Label)
		event.DecisionID = &decisionID
		event.PolicyVersion = input.Authorization.PolicyVersion
		event.Authority = &DecisionAuthorityV1{
			RoleID:         input.Authorization.RoleID,
			GrantID:        input.Authorization.GrantID,
			GrantVersion:   input.Authorization.GrantVersion,
			MandateID:      input.Authorization.MandateID,
			EffectiveUntil: input.Authorization.EffectiveUntil.UTC(),
		}
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return auditevent.Event{}, fmt.Errorf("encode staff bootstrap event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return auditevent.Event{}, errors.New("staff bootstrap event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return auditevent.Event{
		ID:            eventID,
		Type:          StaffBootstrapEventType,
		SchemaVersion: StaffBootstrapSchemaVersion,
		OccurredAt:    input.OccurredAt.UTC(),
		Payload:       payload,
		PayloadSHA256: digest,
	}, nil
}

func validateStaffBootstrapAuditInput(input identity.StaffBootstrapAuditInput) error {
	if input.OccurredAt.IsZero() || input.TicketID == uuid.Nil || input.TargetUserID == uuid.Nil || input.ExpiresAt.IsZero() || !input.ExpiresAt.After(input.OccurredAt) {
		return errors.New("staff bootstrap event is missing required metadata")
	}
	switch input.Transition {
	case identity.StaffBootstrapTicketIssued:
		reference := strings.TrimSpace(input.OperatorReference)
		if !utf8.ValidString(reference) || utf8.RuneCountInString(reference) < 3 || utf8.RuneCountInString(reference) > 200 ||
			len(input.OperatorReferenceSHA256) != 0 || input.ChallengeID != uuid.Nil || len(input.CredentialID) != 0 || input.Label != "" || input.Authorization.ID != uuid.Nil {
			return errors.New("staff bootstrap ticket event has invalid issuance evidence")
		}
	case identity.StaffBootstrapCredentialEnrolled:
		decision := input.Authorization
		if input.OperatorReference != "" || len(input.OperatorReferenceSHA256) != sha256.Size || input.ChallengeID == uuid.Nil || len(input.CredentialID) == 0 || len(input.CredentialID) > 1024 || strings.TrimSpace(input.Label) == "" ||
			!decision.Allow || decision.Reason != authz.ReasonAllowed || decision.ID == uuid.Nil || decision.PolicyVersion == "" || decision.GrantID == uuid.Nil || decision.GrantVersion < 1 || decision.RoleID == "" || decision.MandateID == uuid.Nil || !decision.EffectiveUntil.After(input.OccurredAt) {
			return errors.New("staff bootstrap event has invalid enrollment evidence")
		}
	default:
		return errors.New("staff bootstrap event has an unknown transition")
	}
	return nil
}

func digestBytesLabel(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256_" + hex.EncodeToString(digest[:])
}

var _ identity.StaffBootstrapEventBuilder = (*StaffBootstrapEventBuilder)(nil)
