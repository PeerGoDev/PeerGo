package audit

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
)

func TestCategoryChangeBuilderHashesEditableStateAndReason(t *testing.T) {
	t.Parallel()

	eventID := uuid.MustParse("0198f20a-6da8-7e51-9c64-151515151515")
	now := time.Date(2026, time.August, 5, 14, 0, 0, 0, time.UTC)
	builder, err := NewCategoryChangeEventBuilder(RecorderConfig{
		PseudonymKey: []byte("0123456789abcdef0123456789abcdef"), PseudonymKeyEpoch: "test-2026-08",
		NewEventID: func() uuid.UUID { return eventID },
	})
	if err != nil {
		t.Fatalf("NewCategoryChangeEventBuilder() error = %v", err)
	}
	before := catalog.CategoryAuditState{ID: "movies", Name: "电影", DisplayOrder: 10, Enabled: true, Version: 3}
	after := catalog.CategoryAuditState{ID: "movies", Name: "电影与短片", DisplayOrder: 10, Enabled: false, Version: 4}
	reason := "停用分类并核对其中已有种子的展示影响。"
	event, err := builder.BuildCategoryEvent(catalog.CategoryAuditInput{
		Transition: catalog.CategoryTransitionUpdated, OccurredAt: now,
		ActorID: uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111"), CategoryID: "movies",
		Reason: reason, ExpectedVersion: 3, Before: &before, After: after,
		Authorization: authz.Decision{
			ID: uuid.MustParse("0198f20a-6da8-7e51-9c64-121212121212"), Allow: true,
			Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
			GrantID: uuid.MustParse("0198f20a-6da8-7e51-9c64-131313131313"), GrantVersion: 2,
			RoleID: "category_manager", MandateID: uuid.MustParse("0198f20a-6da8-7e51-9c64-141414141414"),
			EffectiveUntil: now.Add(time.Hour),
		},
	})
	if err != nil {
		t.Fatalf("BuildCategoryEvent() error = %v", err)
	}
	if event.ID != eventID || event.Type != CategoryChangeEventType || event.SchemaVersion != CategoryChangeSchemaVersion {
		t.Fatalf("event envelope = %+v", event)
	}
	if bytes.Contains(event.Payload, []byte(reason)) || bytes.Contains(event.Payload, []byte(after.Name)) {
		t.Fatalf("payload contains editable raw text: %s", event.Payload)
	}
	var payload CategoryChangeRecordedV1
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal(payload) error = %v", err)
	}
	if payload.ExpectedVersion != 3 || payload.ResultingVersion != 4 || payload.BeforeSHA256 == "" || payload.AfterSHA256 == "" || payload.ReasonSHA256 == "" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestCategoryChangeBuilderRejectsInvalidVersionTransition(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 14, 0, 0, 0, time.UTC)
	builder, err := NewCategoryChangeEventBuilder(RecorderConfig{
		PseudonymKey: []byte("0123456789abcdef0123456789abcdef"), PseudonymKeyEpoch: "test-2026-08",
	})
	if err != nil {
		t.Fatalf("NewCategoryChangeEventBuilder() error = %v", err)
	}
	_, err = builder.BuildCategoryEvent(catalog.CategoryAuditInput{
		Transition: catalog.CategoryTransitionCreated, OccurredAt: now,
		ActorID: uuid.New(), CategoryID: "movies", Reason: "创建分类的完整审计理由。",
		After:         catalog.CategoryAuditState{ID: "movies", Name: "电影", Version: 2},
		Authorization: categoryAuditAllowedDecision(now),
	})
	if err == nil {
		t.Fatal("BuildCategoryEvent() error = nil")
	}
}

func categoryAuditAllowedDecision(now time.Time) authz.Decision {
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.New(), GrantVersion: 1, RoleID: "category_manager", MandateID: uuid.New(),
		EffectiveUntil: now.Add(time.Hour),
	}
}
