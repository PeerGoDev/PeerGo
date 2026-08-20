package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func TestStaffBootstrapEnrollmentEventContainsOnlyHashedCredentialEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 13, 0, 0, 0, time.UTC)
	eventID := uuid.New()
	operatorReference := "operator-change-2026-08-05"
	operatorDigest := sha256.Sum256([]byte(operatorReference))
	credentialID := []byte("credential-id-must-not-leak")
	label := "Zero 的 Mac 通行密钥"
	builder, err := NewStaffBootstrapEventBuilder(RecorderConfig{
		PseudonymKey:      bytes.Repeat([]byte{0x35}, 32),
		PseudonymKeyEpoch: "test-2026-08",
		NewEventID:        func() uuid.UUID { return eventID },
	})
	if err != nil {
		t.Fatalf("NewStaffBootstrapEventBuilder() error = %v", err)
	}
	input := identity.StaffBootstrapAuditInput{
		Transition:              identity.StaffBootstrapCredentialEnrolled,
		OccurredAt:              now,
		TicketID:                uuid.New(),
		TargetUserID:            uuid.New(),
		OperatorReferenceSHA256: operatorDigest[:],
		ExpiresAt:               now.Add(15 * time.Minute),
		ChallengeID:             uuid.New(),
		CredentialID:            credentialID,
		Label:                   label,
		Authorization: authz.Decision{
			ID:             uuid.New(),
			Allow:          true,
			Reason:         authz.ReasonAllowed,
			PolicyVersion:  authz.PolicyVersion,
			RoleID:         "staff_access",
			GrantID:        uuid.New(),
			GrantVersion:   4,
			MandateID:      uuid.New(),
			EffectiveUntil: now.Add(time.Hour),
		},
	}
	event, err := builder.BuildStaffBootstrapEvent(input)
	if err != nil {
		t.Fatalf("BuildStaffBootstrapEvent() error = %v", err)
	}
	if event.ID != eventID || event.Type != StaffBootstrapEventType || event.SchemaVersion != StaffBootstrapSchemaVersion {
		t.Fatalf("event envelope = %+v", event)
	}
	for _, secret := range [][]byte{[]byte(operatorReference), credentialID, []byte(label)} {
		if bytes.Contains(event.Payload, secret) {
			t.Fatalf("event payload leaked sensitive enrollment evidence: %s", event.Payload)
		}
	}
	var payload StaffCredentialBootstrapRecordedV1
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal(payload) error = %v", err)
	}
	if payload.Transition != string(identity.StaffBootstrapCredentialEnrolled) || payload.ChallengeID == nil || *payload.ChallengeID != input.ChallengeID || payload.DecisionID == nil || *payload.DecisionID != input.Authorization.ID || payload.Authority == nil {
		t.Fatalf("payload enrollment evidence = %+v", payload)
	}
	if payload.OperatorReferenceSHA256 == "" || payload.CredentialIDSHA256 == "" || payload.LabelSHA256 == "" {
		t.Fatalf("payload hashes = operator=%q credential=%q label=%q", payload.OperatorReferenceSHA256, payload.CredentialIDSHA256, payload.LabelSHA256)
	}
	if err := validateEvent(event); err != nil {
		t.Fatalf("validateEvent() error = %v", err)
	}
}

func TestStaffBootstrapTicketEventRejectsEnrollmentEvidence(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	builder, err := NewStaffBootstrapEventBuilder(RecorderConfig{
		PseudonymKey:      bytes.Repeat([]byte{0x35}, 32),
		PseudonymKeyEpoch: "test-2026-08",
	})
	if err != nil {
		t.Fatalf("NewStaffBootstrapEventBuilder() error = %v", err)
	}
	_, err = builder.BuildStaffBootstrapEvent(identity.StaffBootstrapAuditInput{
		Transition:        identity.StaffBootstrapTicketIssued,
		OccurredAt:        now,
		TicketID:          uuid.New(),
		TargetUserID:      uuid.New(),
		OperatorReference: "operator-ticket-42",
		ExpiresAt:         now.Add(15 * time.Minute),
		ChallengeID:       uuid.New(),
	})
	if err == nil {
		t.Fatal("BuildStaffBootstrapEvent() error = nil")
	}
}
