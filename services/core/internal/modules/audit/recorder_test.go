package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type memoryAppender struct {
	events []Event
	err    error
}

func (appender *memoryAppender) Append(_ context.Context, event Event) error {
	if appender.err != nil {
		return appender.err
	}
	event.Payload = append([]byte(nil), event.Payload...)
	appender.events = append(appender.events, event)
	return nil
}

func TestDecisionRecorderPseudonymizesAndCapturesEffectiveAuthority(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	actorID := uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111")
	targetID := uuid.MustParse("0198f20a-6da8-7e51-9c64-222222222222")
	eventID := uuid.MustParse("0198f20a-6da8-7e51-9c64-555555555555")
	decisionID := uuid.MustParse("0198f20a-6da8-7e51-9c64-666666666666")
	grantID := uuid.MustParse("0198f20a-6da8-7e51-9c64-777777777777")
	mandateID := uuid.MustParse("0198f20a-6da8-7e51-9c64-888888888888")
	appender := &memoryAppender{}
	recorder, err := NewDecisionRecorder(appender, RecorderConfig{
		PseudonymKey:      []byte("peergo-test-audit-pseudonym-key-2026"),
		PseudonymKeyEpoch: "2026-08",
		NewEventID:        func() uuid.UUID { return eventID },
	})
	if err != nil {
		t.Fatalf("NewDecisionRecorder() error = %v", err)
	}
	request := authz.Request{
		Subject:            authz.Subject{ID: actorID, Status: authz.SubjectActive},
		Action:             authz.ActionCapabilityReadSelf,
		CredentialAudience: authz.AudienceWebSession,
		Resource: authz.Resource{
			OwnerID: targetID,
			Scope:   authz.SiteScope(),
		},
		Context: authz.EvaluationContext{Now: now, Purpose: "  权限自查  "},
	}
	decision := authz.Decision{
		ID: decisionID, Allow: true, Reason: authz.ReasonAllowed,
		PolicyVersion: authz.PolicyVersion, GrantID: grantID, GrantVersion: 3,
		RoleID: "member", MandateID: mandateID, EffectiveUntil: now.Add(time.Hour),
	}
	if err := recorder.RecordDecision(context.Background(), request, decision); err != nil {
		t.Fatalf("RecordDecision() error = %v", err)
	}
	if len(appender.events) != 1 {
		t.Fatalf("events = %d, want 1", len(appender.events))
	}
	stored := appender.events[0]
	if stored.ID != eventID || stored.Type != DecisionRecordedEventType || stored.SchemaVersion != DecisionRecordedSchemaVersion {
		t.Fatalf("stored event metadata = %+v", stored)
	}
	digest := sha256.Sum256(stored.Payload)
	if !bytes.Equal(digest[:], stored.PayloadSHA256[:]) {
		t.Fatal("stored payload digest does not match")
	}
	if strings.Contains(string(stored.Payload), actorID.String()) || strings.Contains(string(stored.Payload), targetID.String()) {
		t.Fatalf("payload contains raw subject UUID: %s", stored.Payload)
	}
	var payload DecisionRecordedV1
	if err := json.Unmarshal(stored.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload.ActorPseudonym == "" || payload.TargetPseudonym == "" || payload.ActorPseudonym == payload.TargetPseudonym {
		t.Fatalf("subject pseudonyms = (%q, %q)", payload.ActorPseudonym, payload.TargetPseudonym)
	}
	if payload.Result != "allow" || payload.Purpose != "权限自查" || payload.Authority == nil || payload.Authority.GrantID != grantID || payload.Authority.MandateID != mandateID || payload.Authority.GrantVersion != 3 {
		t.Fatalf("decision payload = %+v", payload)
	}
}

func TestDecisionRecorderOmitsAuthorityForDenialAndPropagatesOutboxFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	subjectID := uuid.New()
	appender := &memoryAppender{}
	recorder, err := NewDecisionRecorder(appender, RecorderConfig{
		PseudonymKey:      []byte("peergo-test-audit-pseudonym-key-2026"),
		PseudonymKeyEpoch: "2026-08",
		NewEventID:        func() uuid.UUID { return uuid.New() },
	})
	if err != nil {
		t.Fatalf("NewDecisionRecorder() error = %v", err)
	}
	request := authz.Request{
		Subject: authz.Subject{ID: subjectID, Status: authz.SubjectActive},
		Action:  authz.ActionCapabilityReadSelf, CredentialAudience: authz.AudienceWebSession,
		Resource: authz.Resource{OwnerID: subjectID, Scope: authz.SiteScope()},
		Context:  authz.EvaluationContext{Now: now},
	}
	decision := authz.Decision{ID: uuid.New(), Reason: authz.ReasonGrantMissing, PolicyVersion: authz.PolicyVersion}
	if err := recorder.RecordDecision(context.Background(), request, decision); err != nil {
		t.Fatalf("RecordDecision(deny) error = %v", err)
	}
	var payload DecisionRecordedV1
	if err := json.Unmarshal(appender.events[0].Payload, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload.Result != "deny" || payload.Authority != nil {
		t.Fatalf("denied payload = %+v", payload)
	}

	wantErr := errors.New("database unavailable")
	appender.err = wantErr
	if err := recorder.RecordDecision(context.Background(), request, decision); !errors.Is(err, wantErr) {
		t.Fatalf("RecordDecision(outbox failure) error = %v, want %v", err, wantErr)
	}
}

func TestDecisionRecorderSafelyRepresentsMalformedDeniedRequest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	appender := &memoryAppender{}
	recorder, err := NewDecisionRecorder(appender, RecorderConfig{
		PseudonymKey:      []byte("peergo-test-audit-pseudonym-key-2026"),
		PseudonymKeyEpoch: "2026-08",
	})
	if err != nil {
		t.Fatalf("NewDecisionRecorder() error = %v", err)
	}
	request := authz.Request{
		Action:             authz.Action("\nnot a typed action"),
		CredentialAudience: authz.CredentialAudience("unexpected"),
		Resource:           authz.Resource{Scope: authz.Scope{ID: "*"}},
		Context: authz.EvaluationContext{
			Now:     now,
			Purpose: strings.Repeat("用途", maxPurposeRunes+1),
		},
	}
	decision := authz.Decision{ID: uuid.New(), Reason: authz.ReasonActionUnknown, PolicyVersion: authz.PolicyVersion}
	if err := recorder.RecordDecision(context.Background(), request, decision); err != nil {
		t.Fatalf("RecordDecision() error = %v", err)
	}
	var payload DecisionRecordedV1
	if err := json.Unmarshal(appender.events[0].Payload, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !strings.HasPrefix(payload.Action, "invalid_sha256_") || payload.CredentialAudience != "invalid" || payload.Scope.Type != "invalid" || !strings.HasPrefix(payload.Scope.ID, "invalid_sha256_") || payload.Purpose != "" || !strings.HasPrefix(payload.PurposeSHA256, "sha256_") {
		t.Fatalf("normalized malformed denial = %+v", payload)
	}
}
