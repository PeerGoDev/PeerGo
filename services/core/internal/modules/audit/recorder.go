package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	decisionPseudonymDomain = "peergo:audit:subject-pseudonym:v1\x00"
	maxPurposeRunes         = 500
	maxPseudonymEpochBytes  = 64
)

// EventAppender is implemented by Core's audit outbox. A future domain
// transaction can construct the same adapter with its pgx transaction so the
// evidence and business mutation commit atomically.
type EventAppender = auditevent.Appender

type RecorderConfig struct {
	PseudonymKey      []byte
	PseudonymKeyEpoch string
	NewEventID        func() uuid.UUID
}

// DecisionRecorder converts authorization values into the reviewed v1 event
// contract, pseudonymizes subjects and appends the canonical bytes to Core's
// reliable outbox.
type DecisionRecorder struct {
	appender          EventAppender
	pseudonymKey      []byte
	pseudonymKeyEpoch string
	newEventID        func() uuid.UUID
}

func NewDecisionRecorder(appender EventAppender, config RecorderConfig) (*DecisionRecorder, error) {
	if appender == nil {
		return nil, errors.New("audit event appender is required")
	}
	config, err := validatedRecorderConfig(config)
	if err != nil {
		return nil, err
	}
	return &DecisionRecorder{
		appender:          appender,
		pseudonymKey:      config.PseudonymKey,
		pseudonymKeyEpoch: config.PseudonymKeyEpoch,
		newEventID:        config.NewEventID,
	}, nil
}

func validatedRecorderConfig(config RecorderConfig) (RecorderConfig, error) {
	if len(config.PseudonymKey) < sha256.Size {
		return RecorderConfig{}, errors.New("audit pseudonym key must contain at least 32 bytes")
	}
	config.PseudonymKeyEpoch = strings.TrimSpace(config.PseudonymKeyEpoch)
	if !validEpoch(config.PseudonymKeyEpoch) {
		return RecorderConfig{}, errors.New("audit pseudonym key epoch is invalid")
	}
	if config.NewEventID == nil {
		config.NewEventID = uuid.New
	}
	config.PseudonymKey = append([]byte(nil), config.PseudonymKey...)
	return config, nil
}

func (recorder *DecisionRecorder) RecordDecision(ctx context.Context, request authz.Request, decision authz.Decision) error {
	if decision.ID == uuid.Nil || decision.PolicyVersion == "" || request.Context.Now.IsZero() {
		return errors.New("authorization decision is missing required audit metadata")
	}
	if (decision.Allow && decision.Reason != authz.ReasonAllowed) || (!decision.Allow && decision.Reason == authz.ReasonAllowed) {
		return errors.New("authorization decision result and reason disagree")
	}

	eventID := recorder.newEventID()
	if eventID == uuid.Nil {
		return errors.New("audit event id generator returned nil")
	}
	event := DecisionRecordedV1{
		SchemaVersion:      DecisionRecordedSchemaVersion,
		EventType:          DecisionRecordedEventType,
		EventID:            eventID,
		OccurredAt:         request.Context.Now.UTC(),
		DecisionID:         decision.ID,
		ActorPseudonym:     subjectPseudonym(recorder.pseudonymKey, request.Subject.ID),
		PseudonymKeyEpoch:  recorder.pseudonymKeyEpoch,
		Action:             normalizedAction(request.Action),
		CredentialAudience: normalizedAudience(request.CredentialAudience),
		Scope: DecisionScopeV1{
			Type: normalizedScopeType(request.Resource.Scope.Type),
			ID:   normalizedScopeID(request.Resource.Scope.ID),
		},
		PolicyVersion: decision.PolicyVersion,
		Result:        "deny",
		Reason:        string(decision.Reason),
	}
	purpose := strings.TrimSpace(request.Context.Purpose)
	if utf8.RuneCountInString(purpose) <= maxPurposeRunes {
		event.Purpose = purpose
	} else {
		event.PurposeSHA256 = digestLabel(purpose)
	}
	if request.Resource.OwnerID != uuid.Nil {
		event.TargetPseudonym = subjectPseudonym(recorder.pseudonymKey, request.Resource.OwnerID)
	}
	if request.Context.CaseID != uuid.Nil {
		caseID := request.Context.CaseID
		event.CaseID = &caseID
	}
	if decision.Allow {
		if decision.GrantID == uuid.Nil || decision.MandateID == uuid.Nil || decision.RoleID == "" || decision.GrantVersion < 1 || decision.EffectiveUntil.IsZero() {
			return errors.New("allowed authorization decision is missing authority evidence")
		}
		event.Result = "allow"
		event.Authority = &DecisionAuthorityV1{
			RoleID:         decision.RoleID,
			GrantID:        decision.GrantID,
			GrantVersion:   decision.GrantVersion,
			MandateID:      decision.MandateID,
			EffectiveUntil: decision.EffectiveUntil.UTC(),
		}
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode authorization decision event: %w", err)
	}
	if len(payload) > MaxEventPayloadBytes {
		return errors.New("authorization decision event exceeds outbox limit")
	}
	digest := sha256.Sum256(payload)
	return recorder.appender.Append(ctx, Event{
		ID:            eventID,
		Type:          DecisionRecordedEventType,
		SchemaVersion: DecisionRecordedSchemaVersion,
		OccurredAt:    request.Context.Now.UTC(),
		Payload:       payload,
		PayloadSHA256: digest,
	})
}

func subjectPseudonym(key []byte, subjectID uuid.UUID) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(decisionPseudonymDomain))
	_, _ = mac.Write(subjectID[:])
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func validEpoch(value string) bool {
	if value == "" || len(value) > maxPseudonymEpochBytes {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func normalizedAction(action authz.Action) string {
	value := string(action)
	parts := strings.Split(value, ".")
	valid := len(parts) >= 2 && len(value) <= 160
	for _, part := range parts {
		if part == "" || part[0] < 'a' || part[0] > 'z' {
			valid = false
			break
		}
		for _, character := range part[1:] {
			if !((character >= 'a' && character <= 'z') || (character >= '0' && character <= '9')) {
				valid = false
				break
			}
		}
	}
	if valid {
		return value
	}
	return "invalid_" + digestLabel(value)
}

func normalizedAudience(audience authz.CredentialAudience) string {
	switch audience {
	case authz.AudienceAnonymous, authz.AudienceWebSession, authz.AudienceStaffSession, authz.AudienceService:
		return string(audience)
	default:
		return "invalid"
	}
}

func normalizedScopeType(scopeType authz.ScopeType) string {
	switch scopeType {
	case authz.ScopeSite, authz.ScopeCategory:
		return string(scopeType)
	default:
		return "invalid"
	}
}

func normalizedScopeID(value string) string {
	if value != "" && len(value) <= 128 && !strings.Contains(value, "*") && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0 {
		return value
	}
	return "invalid_" + digestLabel(value)
}

func digestLabel(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256_" + hex.EncodeToString(digest[:])
}

var _ authz.DecisionRecorder = (*DecisionRecorder)(nil)
