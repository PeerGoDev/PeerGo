package notifications

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/generated/notificationdb"
	"github.com/peergo/peergo/services/core/internal/modules/economy/contenttip"
	"github.com/peergo/peergo/services/core/internal/modules/ratiowatch"
	"github.com/peergo/peergo/services/core/internal/modules/review"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/modules/traffic"
	"github.com/peergo/peergo/services/core/internal/modules/workgroups"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, errors.New("notification database is required")
	}
	return &PostgresRepository{pool: pool}, nil
}

// List keeps totals and rows in one repeatable-read snapshot. A window count
// cannot report totals for an empty page whose offset is beyond the last row.
func (repository *PostgresRepository) List(ctx context.Context, userID uuid.UUID, query ListQuery) (Page, error) {
	if userID == uuid.Nil || query.Limit < 1 || query.Limit > MaximumLimit || query.Offset < 0 || query.Offset > MaximumOffset {
		return Page{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Page{}, fmt.Errorf("begin notification list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := notificationdb.New(tx)
	counts, err := queries.CountMyNotifications(ctx, notificationdb.CountMyNotificationsParams{
		RecipientUserID: userID,
		UnreadOnly:      query.UnreadOnly,
	})
	if err != nil {
		return Page{}, fmt.Errorf("count notifications: %w", err)
	}
	if counts.TotalCount < 0 || counts.TotalCount > math.MaxInt || counts.UnreadCount < 0 || counts.UnreadCount > math.MaxInt {
		return Page{}, ErrInvariant
	}
	rows, err := queries.ListMyNotifications(ctx, notificationdb.ListMyNotificationsParams{
		RecipientUserID: userID, UnreadOnly: query.UnreadOnly,
		ResultLimit: int32(query.Limit), ResultOffset: int32(query.Offset),
	})
	if err != nil {
		return Page{}, fmt.Errorf("list notifications: %w", err)
	}
	items := make([]Notification, 0, len(rows))
	for _, row := range rows {
		if !row.CreatedAt.Valid {
			return Page{}, ErrInvariant
		}
		var readAt *time.Time
		if row.ReadAt.Valid {
			value := row.ReadAt.Time.UTC()
			readAt = &value
		}
		item := Notification{
			ID: row.ID, Kind: Kind(row.Kind), CreatedAt: row.CreatedAt.Time.UTC(), ReadAt: readAt,
		}
		switch item.Kind {
		case KindTorrentReview:
			if !row.TorrentID.Valid || !row.TorrentTitle.Valid || !row.Outcome.Valid ||
				!row.ReasonCode.Valid || !row.Reason.Valid {
				return Page{}, ErrInvariant
			}
			item.TorrentReview = &TorrentReviewNotification{
				TorrentID: torrents.TorrentID(row.TorrentID.Int64), TorrentTitle: strings.TrimSpace(row.TorrentTitle.String),
				Outcome: torrents.State(row.Outcome.String), ReasonCode: review.ReasonCode(row.ReasonCode.String),
				Reason: strings.TrimSpace(row.Reason.String),
			}
		case KindRatioWatch:
			if !row.RatioWatchStatus.Valid || !row.RatioBasisPoints.Valid ||
				!row.MinimumRatioBasisPoints.Valid || !row.RestrictionRatioBasisPoints.Valid ||
				!row.DeadlineAt.Valid {
				return Page{}, ErrInvariant
			}
			item.RatioWatch = &RatioWatchNotification{
				Status:                      ratiowatch.AssessmentStatus(row.RatioWatchStatus.String),
				RatioBasisPoints:            row.RatioBasisPoints.Int64,
				MinimumRatioBasisPoints:     row.MinimumRatioBasisPoints.Int64,
				RestrictionRatioBasisPoints: row.RestrictionRatioBasisPoints.Int64,
				DeadlineAt:                  row.DeadlineAt.Time.UTC(),
			}
		case KindRatioAppeal:
			if !row.RatioAppealStatus.Valid || !row.RatioAppealResponse.Valid {
				return Page{}, ErrInvariant
			}
			item.RatioAppeal = &RatioAppealNotification{
				Status:   ratiowatch.AppealStatus(row.RatioAppealStatus.String),
				Response: strings.TrimSpace(row.RatioAppealResponse.String),
			}
		case KindHNR:
			if !row.HnrTorrentID.Valid || !row.HnrTorrentTitle.Valid ||
				!row.HnrStatus.Valid || !row.HnrGraceEndsAt.Valid {
				return Page{}, ErrInvariant
			}
			item.HNR = &HNRNotification{
				TorrentID:    torrents.TorrentID(row.HnrTorrentID.Int64),
				TorrentTitle: strings.TrimSpace(row.HnrTorrentTitle.String),
				Event:        traffic.HNRNotificationEvent(row.HnrStatus.String),
				GraceEndsAt:  row.HnrGraceEndsAt.Time.UTC(),
			}
		case KindHNRAppeal:
			if !row.HnrAppealTorrentID.Valid || !row.HnrAppealTorrentTitle.Valid ||
				!row.HnrAppealStatus.Valid || !row.HnrAppealResponse.Valid ||
				!row.HnrAppealGraceEndsAt.Valid {
				return Page{}, ErrInvariant
			}
			item.HNR = &HNRNotification{
				TorrentID:    torrents.TorrentID(row.HnrAppealTorrentID.Int64),
				TorrentTitle: strings.TrimSpace(row.HnrAppealTorrentTitle.String),
				Event:        traffic.HNRNotificationEvent("appeal_" + row.HnrAppealStatus.String),
				GraceEndsAt:  row.HnrAppealGraceEndsAt.Time.UTC(),
				Response:     strings.TrimSpace(row.HnrAppealResponse.String),
			}
		case KindWorkgroupContribution:
			if row.WorkgroupGroupKind == "" || row.WorkgroupMetric == "" ||
				row.WorkgroupPolicyRevision < 1 || !row.WorkgroupPeriodStartsAt.Valid ||
				!row.WorkgroupPeriodEndsAt.Valid || !row.WorkgroupObservedAt.Valid ||
				row.WorkgroupEvidenceState == "" || row.WorkgroupTargetValue < 1 ||
				row.WorkgroupAssessmentState == "" || row.WorkgroupExplanationCode == "" ||
				row.WorkgroupReason == "" {
				return Page{}, ErrInvariant
			}
			payload := &WorkgroupContributionNotification{
				GroupKind:       workgroups.GroupKind(row.WorkgroupGroupKind),
				Metric:          workgroups.ContributionMetric(row.WorkgroupMetric),
				PolicyRevision:  row.WorkgroupPolicyRevision,
				PeriodStartsAt:  row.WorkgroupPeriodStartsAt.Time.UTC(),
				PeriodEndsAt:    row.WorkgroupPeriodEndsAt.Time.UTC(),
				ObservedAt:      row.WorkgroupObservedAt.Time.UTC(),
				EvidenceState:   workgroups.ContributionEvidenceState(row.WorkgroupEvidenceState),
				CurrentValue:    row.WorkgroupCurrentValue,
				TargetValue:     row.WorkgroupTargetValue,
				AssessmentState: workgroups.ContributionAssessmentState(row.WorkgroupAssessmentState),
				ExplanationCode: workgroups.ContributionExplanationCode(row.WorkgroupExplanationCode),
				Reason:          strings.TrimSpace(row.WorkgroupReason),
			}
			if row.WorkgroupMissCount != 0 || row.WorkgroupAllowedMisses != 0 || row.WorkgroupDisciplinaryAction != "" {
				if row.WorkgroupMissCount < 1 || row.WorkgroupAllowedMisses < 1 || row.WorkgroupDisciplinaryAction == "" {
					return Page{}, ErrInvariant
				}
				missCount := row.WorkgroupMissCount
				allowedMisses := row.WorkgroupAllowedMisses
				action := workgroups.ContributionDisciplinaryAction(row.WorkgroupDisciplinaryAction)
				payload.MissCount = &missCount
				payload.AllowedMisses = &allowedMisses
				payload.DisciplinaryAction = &action
			}
			if !validWorkgroupContributionPayload(payload) {
				return Page{}, ErrInvariant
			}
			item.WorkgroupContribution = payload
		case KindMemberGift:
			if !row.MemberGiftSenderNumericID.Valid || !row.MemberGiftSenderUsername.Valid ||
				!row.MemberGiftSenderDisplayName.Valid || !row.MemberGiftNetAmount.Valid ||
				!row.MemberGiftMessage.Valid {
				return Page{}, ErrInvariant
			}
			payload := &MemberGiftNotification{
				SenderNumericID:   row.MemberGiftSenderNumericID.Int64,
				SenderUsername:    strings.TrimSpace(row.MemberGiftSenderUsername.String),
				SenderDisplayName: strings.TrimSpace(row.MemberGiftSenderDisplayName.String),
				NetAmount:         row.MemberGiftNetAmount.Int64,
				Message:           strings.TrimSpace(row.MemberGiftMessage.String),
			}
			if !validMemberGiftNotification(payload) {
				return Page{}, ErrInvariant
			}
			item.MemberGift = payload
		case KindContentTip:
			if !row.ContentTipSenderNumericID.Valid || !row.ContentTipSenderUsername.Valid ||
				!row.ContentTipSenderDisplayName.Valid || !row.ContentTipNetAmount.Valid ||
				!row.ContentTipTargetKind.Valid || !row.ContentTipTargetTitle.Valid {
				return Page{}, ErrInvariant
			}
			payload := &ContentTipNotification{
				SenderNumericID:   row.ContentTipSenderNumericID.Int64,
				SenderUsername:    strings.TrimSpace(row.ContentTipSenderUsername.String),
				SenderDisplayName: strings.TrimSpace(row.ContentTipSenderDisplayName.String),
				NetAmount:         row.ContentTipNetAmount.Int64,
				Target:            contenttip.Target{Kind: contenttip.TargetKind(row.ContentTipTargetKind.String), Title: strings.TrimSpace(row.ContentTipTargetTitle.String)},
			}
			if row.ContentTipTorrentID.Valid {
				payload.Target.TorrentID = row.ContentTipTorrentID.Int64
			}
			if row.ContentTipPostID.Valid {
				payload.Target.PostID = uuid.UUID(row.ContentTipPostID.Bytes)
			}
			if row.ContentTipCommentID.Valid {
				payload.Target.CommentID = uuid.UUID(row.ContentTipCommentID.Bytes)
			}
			if !validContentTipNotification(payload) {
				return Page{}, ErrInvariant
			}
			item.ContentTip = payload
		default:
			return Page{}, ErrInvariant
		}
		items = append(items, item)
	}
	if err := tx.Commit(ctx); err != nil {
		return Page{}, fmt.Errorf("commit notification list: %w", err)
	}
	return Page{Items: items, Total: int(counts.TotalCount), UnreadCount: int(counts.UnreadCount), Limit: query.Limit, Offset: query.Offset}, nil
}

func validContentTipNotification(payload *ContentTipNotification) bool {
	if payload == nil || payload.SenderNumericID < 1 || payload.SenderUsername == "" ||
		payload.SenderDisplayName == "" || payload.NetAmount < 1 || payload.Target.Title == "" {
		return false
	}
	switch payload.Target.Kind {
	case contenttip.TargetTorrent:
		return payload.Target.TorrentID > 0 && payload.Target.PostID == uuid.Nil && payload.Target.CommentID == uuid.Nil
	case contenttip.TargetPost:
		return payload.Target.TorrentID == 0 && payload.Target.PostID != uuid.Nil && payload.Target.CommentID == uuid.Nil
	case contenttip.TargetComment:
		return payload.Target.TorrentID == 0 && payload.Target.PostID == uuid.Nil && payload.Target.CommentID != uuid.Nil
	default:
		return false
	}
}

func validWorkgroupContributionPayload(payload *WorkgroupContributionNotification) bool {
	if payload == nil || payload.PolicyRevision < 1 || payload.CurrentValue < 0 ||
		payload.TargetValue < 1 || payload.CurrentValue >= payload.TargetValue ||
		payload.Reason == "" || !payload.PeriodEndsAt.After(payload.PeriodStartsAt) ||
		payload.ObservedAt.Before(payload.PeriodStartsAt) || payload.ObservedAt.After(payload.PeriodEndsAt) {
		return false
	}
	validKindMetric :=
		(payload.GroupKind == workgroups.GroupReseed && payload.Metric == workgroups.MetricTrustedTorrentsPublished) ||
			(payload.GroupKind == workgroups.GroupReview && payload.Metric == workgroups.MetricTorrentReviewVotes) ||
			(payload.GroupKind == workgroups.GroupRetention && payload.Metric == workgroups.MetricSeedingActiveSeconds)
	validEnforcement := payload.MissCount == nil && payload.AllowedMisses == nil && payload.DisciplinaryAction == nil
	if payload.MissCount != nil || payload.AllowedMisses != nil || payload.DisciplinaryAction != nil {
		validEnforcement = payload.MissCount != nil && payload.AllowedMisses != nil && payload.DisciplinaryAction != nil &&
			*payload.MissCount > 0 && *payload.AllowedMisses > 0 &&
			((*payload.MissCount <= *payload.AllowedMisses && *payload.DisciplinaryAction == workgroups.ContributionDisciplinaryMarked) ||
				(*payload.MissCount > *payload.AllowedMisses && *payload.DisciplinaryAction == workgroups.ContributionDisciplinaryMembershipEnded)) &&
			payload.EvidenceState == workgroups.ContributionEvidenceComplete &&
			payload.AssessmentState == workgroups.ContributionAssessmentNotMet
	}
	return validKindMetric && validEnforcement &&
		(payload.EvidenceState == workgroups.ContributionEvidenceCollecting ||
			payload.EvidenceState == workgroups.ContributionEvidenceComplete) &&
		(payload.AssessmentState == workgroups.ContributionAssessmentInProgress ||
			payload.AssessmentState == workgroups.ContributionAssessmentNotMet)
}

func (repository *PostgresRepository) Summary(ctx context.Context, userID uuid.UUID) (Summary, error) {
	if userID == uuid.Nil {
		return Summary{}, ErrInput
	}
	counts, err := notificationdb.New(repository.pool).CountMyNotifications(ctx, notificationdb.CountMyNotificationsParams{
		RecipientUserID: userID,
		UnreadOnly:      false,
	})
	if err != nil {
		return Summary{}, fmt.Errorf("summarize notifications: %w", err)
	}
	if counts.UnreadCount < 0 || counts.UnreadCount > math.MaxInt {
		return Summary{}, ErrInvariant
	}
	return Summary{UnreadCount: int(counts.UnreadCount)}, nil
}

func (repository *PostgresRepository) MarkRead(ctx context.Context, userID, notificationID uuid.UUID, readAt time.Time) (ReadReceipt, error) {
	if userID == uuid.Nil || notificationID == uuid.Nil || readAt.IsZero() {
		return ReadReceipt{}, ErrInput
	}
	row, err := notificationdb.New(repository.pool).MarkNotificationRead(ctx, notificationdb.MarkNotificationReadParams{
		NotificationID: notificationID, RecipientUserID: userID, ReadAt: notificationTimestamp(readAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ReadReceipt{}, ErrNotFound
	}
	if err != nil {
		return ReadReceipt{}, fmt.Errorf("mark notification read: %w", err)
	}
	if !row.ReadAt.Valid {
		return ReadReceipt{}, ErrInvariant
	}
	return ReadReceipt{NotificationID: row.ID, ReadAt: row.ReadAt.Time.UTC(), AlreadyRead: row.AlreadyRead}, nil
}

func (repository *PostgresRepository) MarkAllRead(ctx context.Context, userID uuid.UUID, readAt time.Time) (ReadAllReceipt, error) {
	if userID == uuid.Nil || readAt.IsZero() {
		return ReadAllReceipt{}, ErrInput
	}
	updated, err := notificationdb.New(repository.pool).MarkAllNotificationsRead(ctx, notificationdb.MarkAllNotificationsReadParams{
		ReadAt: notificationTimestamp(readAt), RecipientUserID: userID,
	})
	if err != nil {
		return ReadAllReceipt{}, fmt.Errorf("mark all notifications read: %w", err)
	}
	if updated < 0 || updated > math.MaxInt {
		return ReadAllReceipt{}, ErrInvariant
	}
	return ReadAllReceipt{UpdatedCount: int(updated), ReadAt: readAt.UTC()}, nil
}

func (repository *PostgresRepository) ArchiveAll(ctx context.Context, userID uuid.UUID, archivedAt time.Time) (ArchiveAllReceipt, error) {
	if userID == uuid.Nil || archivedAt.IsZero() {
		return ArchiveAllReceipt{}, ErrInput
	}
	updated, err := notificationdb.New(repository.pool).ArchiveAllNotifications(ctx, notificationdb.ArchiveAllNotificationsParams{
		ArchivedAt: notificationTimestamp(archivedAt), RecipientUserID: userID,
	})
	if err != nil {
		return ArchiveAllReceipt{}, fmt.Errorf("archive all notifications: %w", err)
	}
	if updated < 0 || updated > math.MaxInt {
		return ArchiveAllReceipt{}, ErrInvariant
	}
	return ArchiveAllReceipt{UpdatedCount: int(updated), ArchivedAt: archivedAt.UTC()}, nil
}

func (repository *PostgresRepository) CreateFeedback(ctx context.Context, userID uuid.UUID, input CreateFeedbackInput, createdAt time.Time) (FeedbackReceipt, error) {
	if userID == uuid.Nil || createdAt.IsZero() {
		return FeedbackReceipt{}, ErrInput
	}
	row, err := notificationdb.New(repository.pool).InsertSupportFeedback(ctx, notificationdb.InsertSupportFeedbackParams{
		SenderUserID: userID, Title: input.Title, Content: input.Content,
		CreatedAt: notificationTimestamp(createdAt),
	})
	if err != nil {
		return FeedbackReceipt{}, fmt.Errorf("insert support feedback: %w", err)
	}
	if row.ID == uuid.Nil || !row.CreatedAt.Valid {
		return FeedbackReceipt{}, ErrInvariant
	}
	return FeedbackReceipt{FeedbackID: row.ID, CreatedAt: row.CreatedAt.Time.UTC()}, nil
}

// PostgresReviewAppender is transaction-scoped. Its INSERT derives recipient,
// target and timestamp from the just-persisted immutable review decision, so a
// caller cannot accidentally notify a different user or torrent.
type PostgresReviewAppender struct {
	queries *notificationdb.Queries
}

func NewPostgresReviewAppender(tx pgx.Tx) *PostgresReviewAppender {
	return &PostgresReviewAppender{queries: notificationdb.New(tx)}
}

func (appender *PostgresReviewAppender) AppendTorrentReviewNotification(ctx context.Context, decisionID uuid.UUID) error {
	if appender == nil || appender.queries == nil || decisionID == uuid.Nil {
		return ErrInput
	}
	notificationID, err := appender.queries.InsertTorrentReviewNotification(ctx, decisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvariant
	}
	if err != nil {
		return fmt.Errorf("insert torrent review notification: %w", err)
	}
	if notificationID == uuid.Nil {
		return ErrInvariant
	}
	return nil
}

func notificationTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

var _ review.NotificationAppender = (*PostgresReviewAppender)(nil)
