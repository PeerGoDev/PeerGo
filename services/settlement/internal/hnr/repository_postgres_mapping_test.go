package hnr

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/peergo/peergo/services/settlement/internal/generated/ledgerdb"
	"github.com/peergo/peergo/services/settlement/internal/hnrpolicy"
)

func TestSameCompletionAssessmentAcceptsStableIdentityRetry(t *testing.T) {
	t.Parallel()
	completedAt := time.Date(2026, 8, 25, 18, 44, 13, 0, time.UTC)
	userID := uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111")
	completionID := bytes.Repeat([]byte{0xa4}, 32)
	row := validCompletionAssessmentRow(t, completedAt, userID, completionID)

	retry := rawWork{
		EventID: uuid.MustParse("0198f20a-6da8-7e51-9c64-222222222222"), UserID: userID, TorrentID: row.TorrentID,
		StartsAt: completedAt.Add(2 * time.Second), EndsAt: completedAt.Add(30 * time.Second),
		TorrentControlSequence: row.TorrentControlSequence, SubjectControlSequence: row.SubjectControlSequence + 1,
		CurrentUploaded: row.InitialUploaded + 1024, CurrentDownloaded: row.RawDownloaded,
		CompletedTransition: true, CompletionID: append([]byte(nil), completionID...), CompletionIdentityVersion: 1,
	}
	if !sameCompletionAssessment(row, retry) {
		t.Fatal("stable completion retry was rejected because event metadata changed")
	}
}

func TestSameCompletionAssessmentRejectsDifferentSubject(t *testing.T) {
	t.Parallel()
	completedAt := time.Date(2026, 8, 25, 18, 44, 13, 0, time.UTC)
	userID := uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111")
	completionID := bytes.Repeat([]byte{0xa4}, 32)
	row := validCompletionAssessmentRow(t, completedAt, userID, completionID)
	retry := rawWork{
		EventID: uuid.MustParse("0198f20a-6da8-7e51-9c64-222222222222"), UserID: uuid.New(), TorrentID: row.TorrentID,
		StartsAt: completedAt, EndsAt: completedAt.Add(time.Second), CompletedTransition: true,
		CompletionID: append([]byte(nil), completionID...), CompletionIdentityVersion: 1,
	}
	if sameCompletionAssessment(row, retry) {
		t.Fatal("completion identity replay for a different user was accepted")
	}
	retry.UserID = userID
	retry.TorrentID++
	if sameCompletionAssessment(row, retry) {
		t.Fatal("completion identity replay for a different torrent was accepted")
	}
}

func validCompletionAssessmentRow(t *testing.T, completedAt time.Time, userID uuid.UUID, completionID []byte) ledgerdb.LedgerHnrCompletionAssessment {
	t.Helper()
	policy := hnrpolicy.Policy{
		Rule: hnrpolicy.RuleRef{ID: "global-default", Version: 1},
		Mode: hnrpolicy.ModeDisabled,
	}
	digest, err := hnrpolicy.SHA256(policy)
	if err != nil {
		t.Fatal(err)
	}
	timestamp := func(value time.Time) pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: value, Valid: true}
	}
	return ledgerdb.LedgerHnrCompletionAssessment{
		ID:                uuid.MustParse("0198f20a-6da8-7e51-9c64-333333333333"),
		CompletionID:      append([]byte(nil), completionID...),
		CompletionEventID: uuid.MustParse("0198f20a-6da8-7e51-9c64-444444444444"),
		UserID:            userID, TorrentID: 9835, TorrentControlSequence: 8779, SubjectControlSequence: 13095,
		CompletedAt:      timestamp(completedAt),
		PolicyRevisionID: uuid.MustParse("0198f20a-6da8-7e51-9c64-555555555555"),
		PolicyRuleID:     policy.Rule.ID, PolicyRuleVersion: policy.Rule.Version,
		PolicySha256: digest[:], PolicyMode: string(policy.Mode),
		RequiredSeedSeconds:      policy.RequiredSeedSeconds,
		RequiredRatioBasisPoints: policy.RequiredRatioBasisPoints,
		MaxIntervalCreditSeconds: policy.MaxIntervalCreditSeconds,
		AssessmentDueAt:          timestamp(completedAt), GraceEndsAt: timestamp(completedAt),
		InitialUploaded: 1_108_529_425, RawDownloaded: 5_058_822_144,
		DecidedAt: timestamp(completedAt.Add(time.Second)),
	}
}
