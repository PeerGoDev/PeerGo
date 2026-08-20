package audit

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/catalog"
)

func TestAnnouncementChangeBuilderHashesEditableContentAndReason(t *testing.T) {
	t.Parallel()

	eventID := uuid.MustParse("019fcd83-57de-7240-a0d3-151515151515")
	now := time.Date(2026, time.August, 10, 14, 0, 0, 0, time.UTC)
	builder, err := NewAnnouncementChangeEventBuilder(RecorderConfig{
		PseudonymKey: []byte("0123456789abcdef0123456789abcdef"), PseudonymKeyEpoch: "test-2026-08",
		NewEventID: func() uuid.UUID { return eventID },
	})
	if err != nil {
		t.Fatalf("NewAnnouncementChangeEventBuilder() error = %v", err)
	}
	before := catalog.AnnouncementAuditState{
		ID: "maintenance-window", Title: "维护通知", Summary: "计划维护窗口",
		Body: "旧的维护说明。", BodyFormat: catalog.AnnouncementBodyPlainText,
		Status: catalog.ManagedAnnouncementDraft, Version: 2, RevisionNumber: 2,
		HasUnpublishedChanges: true,
	}
	after := before
	after.Title = "维护时间调整"
	after.Summary = "维护窗口调整至凌晨"
	after.Body = "新的维护说明与值班安排。"
	after.Version = 3
	after.RevisionNumber = 3
	reason := "根据值班表调整公告时间与维护说明。"

	event, err := builder.BuildAnnouncementEvent(catalog.AnnouncementAuditInput{
		Transition: catalog.AnnouncementTransitionDraftUpdated, OccurredAt: now,
		ActorID:        uuid.MustParse("019fcd83-57de-7240-a0d3-111111111111"),
		AnnouncementID: "maintenance-window", Reason: reason, ExpectedVersion: 2,
		Before: &before, After: after, Authorization: categoryAuditAllowedDecision(now),
	})
	if err != nil {
		t.Fatalf("BuildAnnouncementEvent() error = %v", err)
	}
	if event.ID != eventID || event.Type != AnnouncementChangeEventType || event.SchemaVersion != AnnouncementChangeSchemaVersion {
		t.Fatalf("event envelope = %+v", event)
	}
	for _, raw := range []string{reason, after.Title, after.Summary, after.Body} {
		if bytes.Contains(event.Payload, []byte(raw)) {
			t.Fatalf("payload contains editable raw text %q: %s", raw, event.Payload)
		}
	}
	var payload AnnouncementChangeRecordedV1
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal(payload) error = %v", err)
	}
	if payload.Transition != string(catalog.AnnouncementTransitionDraftUpdated) || payload.ExpectedVersion != 2 || payload.ResultingVersion != 3 || payload.RevisionNumber != 3 {
		t.Fatalf("payload transition = %+v", payload)
	}
	if payload.BeforeSHA256 == "" || payload.AfterSHA256 == "" || payload.ReasonSHA256 == "" {
		t.Fatalf("payload hashes = %+v", payload)
	}
}

func TestAnnouncementChangeBuilderRejectsVersionJump(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 14, 0, 0, 0, time.UTC)
	builder, err := NewAnnouncementChangeEventBuilder(RecorderConfig{
		PseudonymKey: []byte("0123456789abcdef0123456789abcdef"), PseudonymKeyEpoch: "test-2026-08",
	})
	if err != nil {
		t.Fatalf("NewAnnouncementChangeEventBuilder() error = %v", err)
	}
	before := catalog.AnnouncementAuditState{ID: "maintenance-window", Version: 2, RevisionNumber: 2}
	_, err = builder.BuildAnnouncementEvent(catalog.AnnouncementAuditInput{
		Transition: catalog.AnnouncementTransitionPublished, OccurredAt: now,
		ActorID: uuid.New(), AnnouncementID: "maintenance-window", Reason: "发布公告的完整审计理由。",
		ExpectedVersion: 2, Before: &before,
		After:         catalog.AnnouncementAuditState{ID: "maintenance-window", Version: 4, RevisionNumber: 2},
		Authorization: categoryAuditAllowedDecision(now),
	})
	if err == nil {
		t.Fatal("BuildAnnouncementEvent() error = nil")
	}
}
