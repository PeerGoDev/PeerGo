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

func TestSiteDisplaySettingsChangeBuilderHashesEditableValuesAndReason(t *testing.T) {
	t.Parallel()

	eventID := uuid.MustParse("0198f20a-6da8-7e51-9c64-161616161616")
	now := time.Date(2026, time.August, 6, 9, 0, 0, 0, time.UTC)
	builder, err := NewSiteDisplaySettingsChangeEventBuilder(RecorderConfig{
		PseudonymKey: []byte("0123456789abcdef0123456789abcdef"), PseudonymKeyEpoch: "test-2026-08",
		NewEventID: func() uuid.UUID { return eventID },
	})
	if err != nil {
		t.Fatalf("NewSiteDisplaySettingsChangeEventBuilder() error = %v", err)
	}
	before := catalog.SiteDisplaySettingsAuditState{
		Name: "PeerGo", Description: "旧说明", DefaultTorrentView: catalog.TorrentViewList,
		ShowLatestAnnouncement: true, Version: 4,
	}
	after := catalog.SiteDisplaySettingsAuditState{
		Name: "PeerGo Club", Description: "新说明", DefaultTorrentView: catalog.TorrentViewPoster,
		ShowLatestAnnouncement: false, Version: 5,
	}
	reason := "调整公开站点展示以匹配当前社区定位。"
	event, err := builder.BuildSiteDisplaySettingsEvent(catalog.SiteDisplaySettingsAuditInput{
		OccurredAt: now, ActorID: uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111"),
		Reason: reason, ExpectedVersion: 4, Before: before, After: after,
		Authorization: authz.Decision{
			ID: uuid.MustParse("0198f20a-6da8-7e51-9c64-121212121212"), Allow: true,
			Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
			GrantID: uuid.MustParse("0198f20a-6da8-7e51-9c64-131313131313"), GrantVersion: 2,
			RoleID: "site_display_manager", MandateID: uuid.MustParse("0198f20a-6da8-7e51-9c64-141414141414"),
			EffectiveUntil: now.Add(time.Hour),
		},
	})
	if err != nil {
		t.Fatalf("BuildSiteDisplaySettingsEvent() error = %v", err)
	}
	if event.ID != eventID || event.Type != SiteDisplaySettingsChangeEventType || event.SchemaVersion != SiteDisplaySettingsChangeSchemaVersion {
		t.Fatalf("event envelope = %+v", event)
	}
	for _, raw := range []string{before.Name, before.Description, after.Name, after.Description, reason} {
		if bytes.Contains(event.Payload, []byte(raw)) {
			t.Fatalf("payload contains editable raw text %q: %s", raw, event.Payload)
		}
	}
	var payload SiteDisplaySettingsChangeRecordedV1
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal(payload) error = %v", err)
	}
	if payload.SettingsSection != catalog.SiteDisplaySettingsSection || payload.ExpectedVersion != 4 || payload.ResultingVersion != 5 || payload.BeforeSHA256 == "" || payload.AfterSHA256 == "" || payload.ReasonSHA256 == "" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestSiteDisplaySettingsChangeBuilderRejectsVersionJump(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 9, 0, 0, 0, time.UTC)
	builder, err := NewSiteDisplaySettingsChangeEventBuilder(RecorderConfig{
		PseudonymKey: []byte("0123456789abcdef0123456789abcdef"), PseudonymKeyEpoch: "test-2026-08",
	})
	if err != nil {
		t.Fatalf("NewSiteDisplaySettingsChangeEventBuilder() error = %v", err)
	}
	_, err = builder.BuildSiteDisplaySettingsEvent(catalog.SiteDisplaySettingsAuditInput{
		OccurredAt: now, ActorID: uuid.New(), Reason: "版本跃迁必须被审计构建器拒绝。",
		ExpectedVersion: 2,
		Before:          catalog.SiteDisplaySettingsAuditState{Version: 2},
		After:           catalog.SiteDisplaySettingsAuditState{Version: 4},
		Authorization:   categoryAuditAllowedDecision(now),
	})
	if err == nil {
		t.Fatal("BuildSiteDisplaySettingsEvent() error = nil")
	}
}
