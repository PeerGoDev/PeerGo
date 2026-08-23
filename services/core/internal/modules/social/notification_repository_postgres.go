package social

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/peergo/peergo/contracts/go/authzcontractv1"
)

func (repository *PostgresPostRepository) ListNotifications(ctx context.Context, recipientID uuid.UUID, query SocialNotificationQuery, now time.Time) (SocialNotificationPage, error) {
	if recipientID == uuid.Nil || !validSocialNotificationCategory(query.Category) || query.Limit < 1 || query.Limit > MaxSocialNotificationLimit || query.Offset < 0 || query.Offset > MaxSocialNotificationOffset {
		return SocialNotificationPage{}, ErrSocialNotificationInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return SocialNotificationPage{}, fmt.Errorf("begin social notification list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	category := string(query.Category)
	categoryPredicate := `($2='all'
        OR ($2='replies' AND notification.kind IN ('post_comment','comment_reply'))
        OR ($2='likes' AND notification.kind IN ('post_like','post_repost'))
        OR ($2='follows' AND notification.kind='follow'))`
	var page SocialNotificationPage
	if err := tx.QueryRow(ctx, `SELECT count(*)::integer FROM social.interaction_notifications AS notification WHERE notification.recipient_user_id=$1 AND `+categoryPredicate, recipientID, category).Scan(&page.Total); err != nil {
		return SocialNotificationPage{}, fmt.Errorf("count social notifications: %w", err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*)::integer FROM social.interaction_notifications WHERE recipient_user_id=$1 AND read_at IS NULL`, recipientID).Scan(&page.UnreadCount); err != nil {
		return SocialNotificationPage{}, fmt.Errorf("count unread social notifications: %w", err)
	}

	rows, err := tx.Query(ctx, `
SELECT notification.id,notification.kind,
       actor.id,actor.username,actor.display_name,
       post.public_id,comment.public_id,
       left(post.body,160),left(comment.body,160),
       notification.created_at,notification.read_at
FROM social.interaction_notifications AS notification
JOIN identity.users AS actor ON actor.id=notification.actor_user_id
LEFT JOIN social.posts AS post ON post.id=notification.post_id
LEFT JOIN social.comments AS comment ON comment.id=notification.comment_id
WHERE notification.recipient_user_id=$1 AND `+categoryPredicate+`
ORDER BY notification.created_at DESC,notification.id DESC
LIMIT $3 OFFSET $4`, recipientID, category, query.Limit, query.Offset)
	if err != nil {
		return SocialNotificationPage{}, fmt.Errorf("list social notifications: %w", err)
	}
	page.Items = make([]SocialNotification, 0, query.Limit)
	for rows.Next() {
		var item SocialNotification
		var postID, commentID pgtype.UUID
		var postPreview, commentPreview pgtype.Text
		var readAt pgtype.Timestamptz
		if err := rows.Scan(
			&item.ID, &item.Kind,
			&item.Actor.ID, &item.Actor.Username, &item.Actor.DisplayName,
			&postID, &commentID, &postPreview, &commentPreview,
			&item.CreatedAt, &readAt,
		); err != nil {
			return SocialNotificationPage{}, fmt.Errorf("scan social notification: %w", err)
		}
		if postID.Valid {
			value := uuid.UUID(postID.Bytes)
			item.PostID = &value
		}
		if commentID.Valid {
			value := uuid.UUID(commentID.Bytes)
			item.CommentID = &value
		}
		if postPreview.Valid {
			item.PostPreview = postPreview.String
		}
		if commentPreview.Valid {
			item.CommentPreview = commentPreview.String
		}
		if readAt.Valid {
			value := readAt.Time.UTC()
			item.ReadAt = &value
		}
		item.CreatedAt = item.CreatedAt.UTC()
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return SocialNotificationPage{}, fmt.Errorf("list social notifications: %w", err)
	}
	rows.Close()
	for index := range page.Items {
		if err := repository.enrichNotificationActor(ctx, tx, recipientID, &page.Items[index].Actor, now); err != nil {
			return SocialNotificationPage{}, err
		}
		if validateSocialNotification(page.Items[index]) != nil {
			return SocialNotificationPage{}, ErrSocialNotificationInvariant
		}
	}
	page.Limit = query.Limit
	page.Offset = query.Offset
	if err := tx.Commit(ctx); err != nil {
		return SocialNotificationPage{}, fmt.Errorf("commit social notification list: %w", err)
	}
	return page, nil
}

func (repository *PostgresPostRepository) NotificationSummary(ctx context.Context, recipientID uuid.UUID) (SocialNotificationSummary, error) {
	if recipientID == uuid.Nil {
		return SocialNotificationSummary{}, ErrSocialNotificationInput
	}
	var summary SocialNotificationSummary
	if err := repository.pool.QueryRow(ctx, `SELECT count(*)::integer FROM social.interaction_notifications WHERE recipient_user_id=$1 AND read_at IS NULL`, recipientID).Scan(&summary.UnreadCount); err != nil {
		return SocialNotificationSummary{}, fmt.Errorf("count unread social notifications: %w", err)
	}
	return summary, nil
}

func (repository *PostgresPostRepository) MarkNotificationRead(ctx context.Context, recipientID, notificationID uuid.UUID, now time.Time) (SocialNotificationReadReceipt, error) {
	if recipientID == uuid.Nil || notificationID == uuid.Nil || now.IsZero() {
		return SocialNotificationReadReceipt{}, ErrSocialNotificationInput
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return SocialNotificationReadReceipt{}, fmt.Errorf("begin social notification read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var existing pgtype.Timestamptz
	err = tx.QueryRow(ctx, `SELECT read_at FROM social.interaction_notifications WHERE id=$1 AND recipient_user_id=$2 FOR UPDATE`, notificationID, recipientID).Scan(&existing)
	if errors.Is(err, pgx.ErrNoRows) {
		return SocialNotificationReadReceipt{}, ErrSocialNotificationNotFound
	}
	if err != nil {
		return SocialNotificationReadReceipt{}, fmt.Errorf("lock social notification: %w", err)
	}
	receipt := SocialNotificationReadReceipt{NotificationID: notificationID, ReadAt: now.UTC()}
	if existing.Valid {
		receipt.ReadAt = existing.Time.UTC()
		receipt.AlreadyRead = true
	} else if _, err := tx.Exec(ctx, `UPDATE social.interaction_notifications SET read_at=$3 WHERE id=$1 AND recipient_user_id=$2`, notificationID, recipientID, now); err != nil {
		return SocialNotificationReadReceipt{}, fmt.Errorf("mark social notification read: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return SocialNotificationReadReceipt{}, fmt.Errorf("commit social notification read: %w", err)
	}
	return receipt, nil
}

func (repository *PostgresPostRepository) MarkAllNotificationsRead(ctx context.Context, recipientID uuid.UUID, now time.Time) (SocialNotificationReadAllReceipt, error) {
	if recipientID == uuid.Nil || now.IsZero() {
		return SocialNotificationReadAllReceipt{}, ErrSocialNotificationInput
	}
	tag, err := repository.pool.Exec(ctx, `UPDATE social.interaction_notifications SET read_at=$2 WHERE recipient_user_id=$1 AND read_at IS NULL`, recipientID, now)
	if err != nil {
		return SocialNotificationReadAllReceipt{}, fmt.Errorf("mark all social notifications read: %w", err)
	}
	return SocialNotificationReadAllReceipt{UpdatedCount: int(tag.RowsAffected()), ReadAt: now.UTC()}, nil
}

func (repository *PostgresPostRepository) enrichNotificationActor(ctx context.Context, db communityDB, viewerID uuid.UUID, actor *PostAuthor, now time.Time) error {
	actor.Medals = []AuthorMedal{}
	err := db.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM social.follows WHERE follower_id=$1 AND followee_id=$2),
       EXISTS (
           SELECT 1 FROM identity.sessions AS session
           WHERE session.user_id=$2 AND session.audience='web'
             AND session.revoked_at IS NULL AND session.expires_at>$3
             AND session.last_seen_at >= $3 - interval '15 minutes'
             AND users.status='active'
             AND NOT EXISTS (
                 SELECT 1 FROM identity.account_restrictions AS restriction
                 WHERE restriction.user_id=users.id
                   AND restriction.kind='account_access'
                   AND restriction.revoked_at IS NULL
                   AND restriction.starts_at <= $3
                   AND restriction.expires_at > $3
             )
       ),
       COALESCE(access.vip_enabled AND (access.vip_until IS NULL OR access.vip_until>$3),false),
       EXISTS (
           SELECT 1
           FROM authz.grants AS grant_record
           JOIN governance.mandates AS mandate
             ON mandate.id=grant_record.mandate_id AND mandate.subject_id=grant_record.subject_id
           WHERE grant_record.subject_id=$2 AND grant_record.role_id='site_admin'
             AND grant_record.scope_type=$4 AND grant_record.scope_id=$5
             AND grant_record.revoked_at IS NULL
             AND grant_record.valid_from <= $3 AND $3 < grant_record.valid_until
             AND mandate.status='active' AND mandate.starts_at <= $3 AND $3 < mandate.ends_at
       )
FROM identity.users AS users
LEFT JOIN identity.user_access_states AS access ON access.user_id=users.id
WHERE users.id=$2`, viewerID, actor.ID, now, authzcontractv1.SiteScopeType, authzcontractv1.SiteScopeID).Scan(
		&actor.FollowedByMe, &actor.Online, &actor.VIP, &actor.SiteAdministrator,
	)
	if err != nil {
		return fmt.Errorf("read social notification actor: %w", err)
	}
	rows, err := db.Query(ctx, `
SELECT definition.id,definition.name,COALESCE(definition.image_small_path,definition.image_large_path)
FROM economy.user_medals AS holding
JOIN economy.medal_definitions AS definition ON definition.id=holding.medal_id
WHERE holding.user_id=$1 AND definition.display_on_page
  AND (holding.expires_at IS NULL OR holding.expires_at>$2)
  AND (definition.is_workgroup OR holding.state='wearing')
ORDER BY holding.priority DESC,definition.priority DESC,definition.id`, actor.ID, now)
	if err != nil {
		return fmt.Errorf("list social notification actor medals: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var medal AuthorMedal
		if err := rows.Scan(&medal.ID, &medal.Name, &medal.ImagePath); err != nil {
			return fmt.Errorf("scan social notification actor medal: %w", err)
		}
		actor.Medals = append(actor.Medals, medal)
	}
	return rows.Err()
}

func upsertSocialInteractionNotification(ctx context.Context, tx pgx.Tx, id, recipientID, actorID uuid.UUID, kind SocialNotificationKind, postID, commentID *int64, now time.Time) error {
	if recipientID == actorID {
		return nil
	}
	var postValue, commentValue any
	if postID != nil {
		postValue = *postID
	}
	if commentID != nil {
		commentValue = *commentID
	}
	_, err := tx.Exec(ctx, `
INSERT INTO social.interaction_notifications (
    id,recipient_user_id,actor_user_id,kind,post_id,comment_id,created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (
    recipient_user_id,actor_user_id,kind,
    (COALESCE(post_id,0)),(COALESCE(comment_id,0))
) DO UPDATE SET created_at=EXCLUDED.created_at,read_at=NULL`, id, recipientID, actorID, kind, postValue, commentValue, now)
	if err != nil {
		return fmt.Errorf("project social interaction notification: %w", err)
	}
	return nil
}

func validateSocialNotification(item SocialNotification) error {
	if item.ID == uuid.Nil || item.Actor.ID == uuid.Nil || item.Actor.Username == "" || item.Actor.DisplayName == "" || item.CreatedAt.IsZero() {
		return ErrSocialNotificationInvariant
	}
	if item.ReadAt != nil && item.ReadAt.Before(item.CreatedAt) {
		return ErrSocialNotificationInvariant
	}
	switch item.Kind {
	case SocialNotificationFollow:
		if item.PostID != nil || item.CommentID != nil {
			return ErrSocialNotificationInvariant
		}
	case SocialNotificationPostLike, SocialNotificationPostRepost:
		if item.PostID == nil || item.CommentID != nil {
			return ErrSocialNotificationInvariant
		}
	case SocialNotificationPostComment, SocialNotificationCommentReply:
		if item.PostID == nil || item.CommentID == nil {
			return ErrSocialNotificationInvariant
		}
	default:
		return ErrSocialNotificationInvariant
	}
	return nil
}
