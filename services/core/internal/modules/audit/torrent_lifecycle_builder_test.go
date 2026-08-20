package audit

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

func TestTorrentLifecycleBuilderHashesReasonAndState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	eventID := uuid.MustParse("0198f20a-6da8-7e51-9c64-252525252525")
	builder, err := NewTorrentLifecycleEventBuilder(RecorderConfig{
		PseudonymKey: []byte("0123456789abcdef0123456789abcdef"), PseudonymKeyEpoch: "test-2026-08",
		NewEventID: func() uuid.UUID { return eventID },
	})
	if err != nil {
		t.Fatalf("NewTorrentLifecycleEventBuilder() error = %v", err)
	}
	reason := "该种子内容需要暂时下架并重新核对。"
	event, err := builder.BuildTorrentLifecycleEvent(torrents.TorrentLifecycleAuditInput{
		ChangeID: uuid.MustParse("0198f20a-6da8-7e51-9c64-212121212121"),
		ActorID:  uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111"),
		Action:   torrents.TorrentAvailabilityDisable, Reason: reason, OccurredAt: now,
		Authorization: torrentLifecycleAllowedDecision(now),
		Before:        torrents.TorrentLifecycleAuditState{TorrentID: 1234, State: torrents.StatePublished, Version: 7, TrackerEligible: true},
		After:         torrents.TorrentLifecycleAuditState{TorrentID: 1234, State: torrents.StateDisabled, Version: 8, TrackerEligible: false},
	})
	if err != nil {
		t.Fatalf("BuildTorrentLifecycleEvent() error = %v", err)
	}
	if event.ID != eventID || event.Type != TorrentLifecycleEventType || event.SchemaVersion != TorrentLifecycleSchemaVersion {
		t.Fatalf("event envelope = %+v", event)
	}
	if bytes.Contains(event.Payload, []byte(reason)) || bytes.Contains(event.Payload, []byte("published")) {
		t.Fatalf("payload contains operational text or unhashed state: %s", event.Payload)
	}
	var payload TorrentLifecycleChangeRecordedV1
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal(payload) error = %v", err)
	}
	if payload.TorrentID != 1234 || payload.ExpectedVersion != 7 || payload.ResultingVersion != 8 ||
		payload.ReasonSHA256 == "" || payload.BeforeSHA256 == "" || payload.AfterSHA256 == "" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestTorrentLifecycleBuilderRejectsTrackerInconsistentTransition(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	builder, err := NewTorrentLifecycleEventBuilder(RecorderConfig{
		PseudonymKey: []byte("0123456789abcdef0123456789abcdef"), PseudonymKeyEpoch: "test-2026-08",
	})
	if err != nil {
		t.Fatalf("NewTorrentLifecycleEventBuilder() error = %v", err)
	}
	_, err = builder.BuildTorrentLifecycleEvent(torrents.TorrentLifecycleAuditInput{
		ChangeID: uuid.New(), ActorID: uuid.New(), Action: torrents.TorrentAvailabilityRestore,
		Reason: "恢复已经核验完成的种子并重新开放 Tracker。", OccurredAt: now,
		Authorization: torrentLifecycleAllowedDecision(now),
		Before:        torrents.TorrentLifecycleAuditState{TorrentID: 1234, State: torrents.StateDisabled, Version: 8, TrackerEligible: false},
		After:         torrents.TorrentLifecycleAuditState{TorrentID: 1234, State: torrents.StatePublished, Version: 9, TrackerEligible: false},
	})
	if err == nil {
		t.Fatal("BuildTorrentLifecycleEvent() error = nil")
	}
}

func TestTorrentLifecycleBuilderAcceptsWithdrawalTimeline(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 18, 8, 0, 0, 0, time.UTC)
	builder, err := NewTorrentLifecycleEventBuilder(RecorderConfig{
		PseudonymKey: []byte("0123456789abcdef0123456789abcdef"), PseudonymKeyEpoch: "test-2026-08",
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name          string
		action        torrents.TorrentAvailabilityAction
		before, after torrents.TorrentLifecycleAuditState
	}{
		{
			name: "request", action: torrents.TorrentAvailabilityWithdrawRequest,
			before: torrents.TorrentLifecycleAuditState{TorrentID: 42, State: torrents.StatePublished, Version: 7, TrackerEligible: true},
			after:  torrents.TorrentLifecycleAuditState{TorrentID: 42, State: torrents.StateDisabled, Version: 8, TrackerEligible: false},
		},
		{
			name: "approve", action: torrents.TorrentAvailabilityWithdrawApprove,
			before: torrents.TorrentLifecycleAuditState{TorrentID: 42, State: torrents.StateDisabled, Version: 8, TrackerEligible: false},
			after:  torrents.TorrentLifecycleAuditState{TorrentID: 42, State: torrents.StateDeleted, Version: 9, TrackerEligible: false},
		},
		{
			name: "reject", action: torrents.TorrentAvailabilityWithdrawReject,
			before: torrents.TorrentLifecycleAuditState{TorrentID: 42, State: torrents.StateDisabled, Version: 8, TrackerEligible: false},
			after:  torrents.TorrentLifecycleAuditState{TorrentID: 42, State: torrents.StatePublished, Version: 9, TrackerEligible: true},
		},
		{
			name: "report disable", action: torrents.TorrentAvailabilityReportDisable,
			before: torrents.TorrentLifecycleAuditState{TorrentID: 42, State: torrents.StatePublished, Version: 9, TrackerEligible: true},
			after:  torrents.TorrentLifecycleAuditState{TorrentID: 42, State: torrents.StateDisabled, Version: 10, TrackerEligible: false},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := builder.BuildTorrentLifecycleEvent(torrents.TorrentLifecycleAuditInput{
				ChangeID: uuid.New(), ActorID: uuid.New(), Action: test.action,
				Reason: "撤回流程测试使用的完整操作说明。", OccurredAt: now,
				Authorization: torrentLifecycleAllowedDecision(now), Before: test.before, After: test.after,
			}); err != nil {
				t.Fatalf("BuildTorrentLifecycleEvent() error=%v", err)
			}
		})
	}
}

func torrentLifecycleAllowedDecision(now time.Time) authz.Decision {
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.New(), GrantVersion: 1, RoleID: "site_admin", MandateID: uuid.New(), EffectiveUntil: now.Add(time.Hour),
	}
}
