package social

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/peergo/peergo/contracts/go/authzcontractv1"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy"
)

var socialRedPacketEscrowID = uuid.MustParse("00000000-0000-7000-8000-000000000008")
var socialRedPacketFundingNS = uuid.MustParse("65c23676-84df-5ed2-9845-9232ce37c143")
var socialRedPacketClaimNS = uuid.MustParse("b789ba60-0bfa-59fd-86c7-06db11e21b88")

func (repository *PostgresPostRepository) Overview(ctx context.Context, _ uuid.UUID) (CommunityOverview, error) {
	boards, err := repository.listBoards(ctx, repository.pool, true)
	if err != nil {
		return CommunityOverview{}, err
	}
	rows, err := repository.pool.Query(ctx, `
SELECT topic.display_topic, count(*)::bigint
FROM social.post_topics AS topic
JOIN social.posts AS post ON post.id = topic.post_id
JOIN social.boards AS board ON board.id = post.board_id
WHERE post.state = 'visible' AND board.enabled AND post.created_at >= CURRENT_TIMESTAMP - interval '30 days'
GROUP BY topic.topic, topic.display_topic
ORDER BY count(*) DESC, max(post.created_at) DESC, topic.topic
LIMIT 8`)
	if err != nil {
		return CommunityOverview{}, fmt.Errorf("list social hot topics: %w", err)
	}
	defer rows.Close()
	topics := make([]Topic, 0, 8)
	for rows.Next() {
		var topic Topic
		if err := rows.Scan(&topic.Name, &topic.PostCount); err != nil {
			return CommunityOverview{}, err
		}
		topics = append(topics, topic)
	}
	return CommunityOverview{Boards: boards, HotTopics: topics}, rows.Err()
}

func (repository *PostgresPostRepository) EnrichPosts(ctx context.Context, viewerID uuid.UUID, posts []Post, now time.Time) ([]Post, error) {
	return repository.enrichPosts(ctx, repository.pool, viewerID, posts, now)
}

type communityDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (repository *PostgresPostRepository) enrichPosts(ctx context.Context, db communityDB, viewerID uuid.UUID, posts []Post, now time.Time) ([]Post, error) {
	for index := range posts {
		post := &posts[index]
		err := db.QueryRow(ctx, `
SELECT board.id, board.name, board.description, board.icon, board.tone,
       board.display_order, board.enabled, board.allow_member_posts, board.version,
       (SELECT count(*)::bigint FROM social.posts AS counted WHERE counted.board_id = board.id AND counted.state = 'visible'),
       source.is_pinned, source.is_featured,
       (SELECT count(*)::bigint FROM social.post_likes WHERE post_id = source.id),
       (SELECT count(*)::bigint FROM social.post_reposts WHERE post_id = source.id),
       EXISTS (SELECT 1 FROM social.post_likes WHERE post_id = source.id AND user_id = $2),
       EXISTS (SELECT 1 FROM social.post_reposts WHERE post_id = source.id AND user_id = $2),
       EXISTS (SELECT 1 FROM social.follows WHERE follower_id = $2 AND followee_id = source.author_id),
       EXISTS (
           SELECT 1
           FROM identity.sessions AS session
           JOIN identity.users AS users ON users.id = session.user_id
           WHERE session.user_id = source.author_id
             AND session.audience = 'web'
             AND session.revoked_at IS NULL
             AND session.expires_at > $3
             AND session.last_seen_at >= $3 - interval '15 minutes'
             AND users.status = 'active'
             AND NOT EXISTS (
                 SELECT 1
                 FROM identity.account_restrictions AS restriction
                 WHERE restriction.user_id = users.id
                   AND restriction.kind = 'account_access'
                   AND restriction.revoked_at IS NULL
                   AND restriction.starts_at <= $3
                   AND restriction.expires_at > $3
             )
       ),
       COALESCE(access.vip_enabled AND (access.vip_until IS NULL OR access.vip_until > $3), false),
       EXISTS (
           SELECT 1
           FROM authz.grants AS grant_record
           JOIN governance.mandates AS mandate
             ON mandate.id = grant_record.mandate_id
            AND mandate.subject_id = grant_record.subject_id
           WHERE grant_record.subject_id = source.author_id
             AND grant_record.role_id = 'site_admin'
             AND grant_record.scope_type = $4
             AND grant_record.scope_id = $5
             AND grant_record.revoked_at IS NULL
             AND grant_record.valid_from <= $3
             AND $3 < grant_record.valid_until
             AND mandate.status = 'active'
             AND mandate.starts_at <= $3
             AND $3 < mandate.ends_at
       )
FROM social.posts AS source
JOIN social.boards AS board ON board.id = source.board_id
LEFT JOIN identity.user_access_states AS access ON access.user_id = source.author_id
WHERE source.public_id = $1`, post.ID, viewerID, now, authzcontractv1.SiteScopeType, authzcontractv1.SiteScopeID).Scan(
			&post.Board.ID, &post.Board.Name, &post.Board.Description, &post.Board.Icon, &post.Board.Tone,
			&post.Board.DisplayOrder, &post.Board.Enabled, &post.Board.AllowMemberPosts, &post.Board.Version, &post.Board.PostCount,
			&post.Pinned, &post.Featured, &post.LikeCount, &post.RepostCount, &post.LikedByMe, &post.RepostedByMe, &post.Author.FollowedByMe,
			&post.Author.Online, &post.Author.VIP, &post.Author.SiteAdministrator,
		)
		if err != nil {
			return nil, fmt.Errorf("enrich social post: %w", err)
		}

		medalRows, err := db.Query(ctx, `
SELECT definition.id, definition.name,
       COALESCE(definition.image_small_path, definition.image_large_path)
FROM economy.user_medals AS holding
JOIN economy.medal_definitions AS definition ON definition.id = holding.medal_id
WHERE holding.user_id = $1
  AND definition.display_on_page
  AND (holding.expires_at IS NULL OR holding.expires_at > $2)
  AND (definition.is_workgroup OR holding.state = 'wearing')
ORDER BY holding.priority DESC, definition.priority DESC, definition.id`, post.Author.ID, now)
		if err != nil {
			return nil, fmt.Errorf("list social author medals: %w", err)
		}
		post.Author.Medals = []AuthorMedal{}
		for medalRows.Next() {
			var medal AuthorMedal
			if err := medalRows.Scan(&medal.ID, &medal.Name, &medal.ImagePath); err != nil {
				medalRows.Close()
				return nil, err
			}
			post.Author.Medals = append(post.Author.Medals, medal)
		}
		if err := medalRows.Err(); err != nil {
			medalRows.Close()
			return nil, err
		}
		medalRows.Close()

		mediaRows, err := db.Query(ctx, `
SELECT media.id, media.content_type, media.width, media.height
FROM social.post_media AS media
JOIN social.posts AS source ON source.id = media.post_id
WHERE source.public_id = $1
ORDER BY media.position`, post.ID)
		if err != nil {
			return nil, err
		}
		post.Media = []PostMedia{}
		for mediaRows.Next() {
			var media PostMedia
			if err := mediaRows.Scan(&media.ID, &media.ContentType, &media.Width, &media.Height); err != nil {
				mediaRows.Close()
				return nil, err
			}
			media.URL = "/api/v1/social/media/" + media.ID.String()
			post.Media = append(post.Media, media)
		}
		if err := mediaRows.Err(); err != nil {
			mediaRows.Close()
			return nil, err
		}
		mediaRows.Close()

		post.Torrent = nil
		var sharedTorrent PostTorrent
		err = db.QueryRow(ctx, `
SELECT torrent.id,
       torrent.state = 'published' AS available,
       CASE WHEN torrent.state = 'published' THEN torrent.title ELSE '' END,
       CASE WHEN torrent.state = 'published' THEN torrent.subtitle ELSE '' END,
       CASE WHEN torrent.state = 'published' THEN torrent.total_size_bytes ELSE 0 END,
       torrent.state = 'published' AND EXISTS (
           SELECT 1
           FROM torrents.torrent_screenshot_set_heads AS head
           JOIN torrents.torrent_screenshot_set_items AS item
             ON item.set_id = head.active_set_id AND item.position = 0
           WHERE head.torrent_id = torrent.id
       ) AS cover_available
FROM social.posts AS source
JOIN torrents.torrents AS torrent ON torrent.id = source.torrent_id
WHERE source.public_id = $1`, post.ID).Scan(
			&sharedTorrent.ID, &sharedTorrent.Available, &sharedTorrent.Title,
			&sharedTorrent.Subtitle, &sharedTorrent.SizeBytes, &sharedTorrent.CoverAvailable,
		)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("read shared social torrent: %w", err)
		}
		if err == nil {
			post.Torrent = &sharedTorrent
		}

		topicRows, err := db.Query(ctx, `
SELECT topic.display_topic FROM social.post_topics AS topic
JOIN social.posts AS source ON source.id = topic.post_id
WHERE source.public_id = $1 ORDER BY topic.topic`, post.ID)
		if err != nil {
			return nil, err
		}
		post.Topics = []string{}
		for topicRows.Next() {
			var topic string
			if err := topicRows.Scan(&topic); err != nil {
				topicRows.Close()
				return nil, err
			}
			post.Topics = append(post.Topics, topic)
		}
		if err := topicRows.Err(); err != nil {
			topicRows.Close()
			return nil, err
		}
		topicRows.Close()

		poll, err := readPoll(ctx, db, post.ID, viewerID, now)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			post.Poll = &poll
		}

		var packet RedPacket
		var claimedAmount pgtype.Int8
		err = db.QueryRow(ctx, `
SELECT packet.total_amount, packet.claim_count, packet.remaining_amount, packet.remaining_claims,
       claim.amount IS NOT NULL, claim.amount
FROM social.post_red_packets AS packet
JOIN social.posts AS source ON source.id = packet.post_id
LEFT JOIN social.post_red_packet_claims AS claim ON claim.post_id = packet.post_id AND claim.claimant_id = $2
WHERE source.public_id = $1`, post.ID, viewerID).Scan(
			&packet.TotalAmount, &packet.ClaimCount, &packet.RemainingAmount, &packet.RemainingClaims, &packet.ClaimedByMe, &claimedAmount,
		)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			if claimedAmount.Valid {
				value := claimedAmount.Int64
				packet.MyClaimAmount = &value
			}
			post.RedPacket = &packet
		}
	}
	return posts, nil
}

func (repository *PostgresPostRepository) UploadMedia(ctx context.Context, uploaderID, mediaID uuid.UUID, raw []byte, contentType string, width, height int, digest [sha256.Size]byte, now time.Time) (PostMedia, error) {
	if _, err := repository.pool.Exec(ctx, `DELETE FROM social.post_media WHERE uploader_id=$1 AND post_id IS NULL AND created_at < $2`, uploaderID, now.Add(-24*time.Hour)); err != nil {
		return PostMedia{}, fmt.Errorf("expire pending social media: %w", err)
	}
	_, err := repository.pool.Exec(ctx, `
INSERT INTO social.post_media (id, uploader_id, content_type, content_sha256, byte_length, width, height, payload, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, mediaID, uploaderID, contentType, digest[:], len(raw), width, height, raw, now)
	if err != nil {
		return PostMedia{}, fmt.Errorf("store social media: %w", err)
	}
	return PostMedia{ID: mediaID, ContentType: contentType, Width: width, Height: height, URL: "/api/v1/social/media/" + mediaID.String()}, nil
}

func (repository *PostgresPostRepository) ReadMedia(ctx context.Context, mediaID uuid.UUID) (MediaObject, error) {
	var result MediaObject
	var digest []byte
	err := repository.pool.QueryRow(ctx, `
SELECT media.id, media.content_type, media.width, media.height, media.payload, media.content_sha256
FROM social.post_media AS media
JOIN social.posts AS post ON post.id = media.post_id
JOIN social.boards AS board ON board.id = post.board_id
WHERE media.id = $1 AND post.state = 'visible' AND board.enabled`, mediaID).Scan(
		&result.ID, &result.ContentType, &result.Width, &result.Height, &result.Payload, &digest,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return MediaObject{}, ErrSocialMediaNotFound
	}
	if err != nil {
		return MediaObject{}, err
	}
	if len(digest) != sha256.Size || sha256.Sum256(result.Payload) != [sha256.Size]byte(digest) {
		return MediaObject{}, ErrPostInvariant
	}
	copy(result.SHA256[:], digest)
	result.URL = "/api/v1/social/media/" + result.ID.String()
	return result, nil
}

func (repository *PostgresPostRepository) SetInteraction(ctx context.Context, userID, postID uuid.UUID, kind string, active bool, now time.Time) (InteractionState, error) {
	table := map[string]string{"like": "social.post_likes", "repost": "social.post_reposts"}[kind]
	if table == "" {
		return InteractionState{}, ErrPostInput
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return InteractionState{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var internalID int64
	var authorID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT post.id,post.author_id FROM social.posts AS post JOIN social.boards AS board ON board.id=post.board_id WHERE post.public_id=$1 AND post.state='visible' AND board.enabled`, postID).Scan(&internalID, &authorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return InteractionState{}, ErrPostNotFound
	}
	if err != nil {
		return InteractionState{}, err
	}
	if active {
		var tag pgconn.CommandTag
		tag, err = tx.Exec(ctx, `INSERT INTO `+table+` (post_id,user_id,created_at) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, internalID, userID, now)
		if err == nil && tag.RowsAffected() == 1 && userID != authorID {
			notificationKind := SocialNotificationPostLike
			if kind == "repost" {
				notificationKind = SocialNotificationPostRepost
			}
			err = upsertSocialInteractionNotification(ctx, tx, uuid.New(), authorID, userID, notificationKind, &internalID, nil, now)
		}
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM `+table+` WHERE post_id=$1 AND user_id=$2`, internalID, userID)
	}
	if err != nil {
		return InteractionState{}, err
	}
	var count int64
	if err := tx.QueryRow(ctx, `SELECT count(*)::bigint FROM `+table+` WHERE post_id=$1`, internalID).Scan(&count); err != nil {
		return InteractionState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return InteractionState{}, err
	}
	return InteractionState{Active: active, Count: count}, nil
}

func (repository *PostgresPostRepository) SetFollow(ctx context.Context, followerID uuid.UUID, username string, active bool, now time.Time) (FollowState, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return FollowState{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var followeeID uuid.UUID
	var canonical string
	err = tx.QueryRow(ctx, `SELECT id, username FROM identity.users WHERE lower(username)=lower($1) AND account_state='active'`, username).Scan(&followeeID, &canonical)
	if errors.Is(err, pgx.ErrNoRows) {
		return FollowState{}, ErrSocialMemberNotFound
	}
	if err != nil {
		return FollowState{}, err
	}
	if followerID == followeeID {
		return FollowState{}, ErrSocialSelfFollow
	}
	if active {
		var tag pgconn.CommandTag
		tag, err = tx.Exec(ctx, `INSERT INTO social.follows (follower_id,followee_id,created_at) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, followerID, followeeID, now)
		if err == nil && tag.RowsAffected() == 1 {
			err = upsertSocialInteractionNotification(ctx, tx, uuid.New(), followeeID, followerID, SocialNotificationFollow, nil, nil, now)
		}
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM social.follows WHERE follower_id=$1 AND followee_id=$2`, followerID, followeeID)
	}
	if err != nil {
		return FollowState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return FollowState{}, err
	}
	return FollowState{Username: canonical, Following: active}, nil
}

func (repository *PostgresPostRepository) Vote(ctx context.Context, voterID, postID, optionID uuid.UUID, now time.Time) (Poll, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Poll{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var internalID int64
	var closes pgtype.Timestamptz
	err = tx.QueryRow(ctx, `SELECT post.id, poll.closes_at FROM social.posts AS post JOIN social.boards AS board ON board.id=post.board_id JOIN social.post_polls AS poll ON poll.post_id=post.id JOIN social.post_poll_options AS option ON option.post_id=post.id AND option.id=$2 WHERE post.public_id=$1 AND post.state='visible' AND board.enabled FOR UPDATE OF poll`, postID, optionID).Scan(&internalID, &closes)
	if errors.Is(err, pgx.ErrNoRows) {
		return Poll{}, ErrPostNotFound
	}
	if err != nil {
		return Poll{}, err
	}
	if closes.Valid && !now.Before(closes.Time) {
		return Poll{}, ErrSocialPollClosed
	}
	_, err = tx.Exec(ctx, `INSERT INTO social.post_poll_votes (post_id,option_id,voter_id,created_at,updated_at) VALUES ($1,$2,$3,$4,$4) ON CONFLICT (post_id,voter_id) DO UPDATE SET option_id=EXCLUDED.option_id,updated_at=EXCLUDED.updated_at`, internalID, optionID, voterID, now)
	if err != nil {
		return Poll{}, err
	}
	poll, err := readPoll(ctx, tx, postID, voterID, now)
	if err != nil {
		return Poll{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Poll{}, err
	}
	return poll, nil
}

func readPoll(ctx context.Context, db communityDB, postID, viewerID uuid.UUID, now time.Time) (Poll, error) {
	var poll Poll
	var closes pgtype.Timestamptz
	var selected pgtype.UUID
	err := db.QueryRow(ctx, `SELECT poll.question,poll.closes_at,(SELECT option_id FROM social.post_poll_votes WHERE post_id=poll.post_id AND voter_id=$2) FROM social.post_polls AS poll JOIN social.posts AS post ON post.id=poll.post_id WHERE post.public_id=$1`, postID, viewerID).Scan(&poll.Question, &closes, &selected)
	if err != nil {
		return Poll{}, err
	}
	if closes.Valid {
		value := closes.Time.UTC()
		poll.ClosesAt = &value
		poll.Closed = !now.Before(value)
	}
	if selected.Valid {
		value := uuid.UUID(selected.Bytes)
		poll.SelectedOptionID = &value
	}
	rows, err := db.Query(ctx, `SELECT option.id,option.label,count(vote.voter_id)::bigint FROM social.post_poll_options AS option JOIN social.posts AS post ON post.id=option.post_id LEFT JOIN social.post_poll_votes AS vote ON vote.option_id=option.id WHERE post.public_id=$1 GROUP BY option.id,option.label,option.position ORDER BY option.position`, postID)
	if err != nil {
		return Poll{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var option PollOption
		if err := rows.Scan(&option.ID, &option.Label, &option.VoteCount); err != nil {
			return Poll{}, err
		}
		poll.TotalVotes += option.VoteCount
		poll.Options = append(poll.Options, option)
	}
	return poll, rows.Err()
}

func (repository *PostgresPostRepository) ClaimRedPacket(ctx context.Context, claimantID, postID, requestID uuid.UUID, now time.Time) (RedPacketClaim, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return RedPacketClaim{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var replay RedPacketClaim
	err = tx.QueryRow(ctx, `SELECT claim.amount,packet.remaining_amount,packet.remaining_claims FROM social.post_red_packet_claims AS claim JOIN social.post_red_packets AS packet ON packet.post_id=claim.post_id JOIN social.posts AS post ON post.id=claim.post_id WHERE post.public_id=$1 AND claim.claimant_id=$2`, postID, claimantID).Scan(&replay.Amount, &replay.RemainingAmount, &replay.RemainingClaims)
	if err == nil {
		replay.Replayed = true
		if err := tx.Commit(ctx); err != nil {
			return RedPacketClaim{}, err
		}
		return replay, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return RedPacketClaim{}, err
	}
	var internalID int64
	var remaining int64
	var claims int
	err = tx.QueryRow(ctx, `SELECT post.id,packet.remaining_amount,packet.remaining_claims FROM social.posts AS post JOIN social.boards AS board ON board.id=post.board_id JOIN social.post_red_packets AS packet ON packet.post_id=post.id WHERE post.public_id=$1 AND post.state='visible' AND board.enabled FOR UPDATE OF packet`, postID).Scan(&internalID, &remaining, &claims)
	if errors.Is(err, pgx.ErrNoRows) {
		return RedPacketClaim{}, ErrPostNotFound
	}
	if err != nil {
		return RedPacketClaim{}, err
	}
	if claims < 1 || remaining < 1 {
		return RedPacketClaim{}, ErrSocialRedPacketEmpty
	}
	amount := remaining / int64(claims)
	if claims == 1 {
		amount = remaining
	}
	claimIdentity := fmt.Sprintf("%s:%s:%s", postID, claimantID, requestID)
	payload := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", claimIdentity, amount)))
	transactionID := uuid.NewSHA1(socialRedPacketClaimNS, []byte(claimIdentity))
	transaction, err := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{TransactionID: transactionID, TransactionType: economy.TransactionSocialRedPacketClaim, IdempotencyKey: "social-red-packet-claim:" + claimIdentity, SourceReference: "social-red-packet:" + postID.String(), PolicyRevision: "social-red-packet-v1", PayloadSHA256: payload, OccurredAt: now, RecordedAt: now, Postings: []economy.PostingInput{{AccountID: socialRedPacketEscrowID, Amount: -amount}, {AccountID: claimantID, Amount: amount}}})
	if err != nil {
		return RedPacketClaim{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO social.post_red_packet_claims(id,post_id,claimant_id,amount,magic_transaction_id,claimed_at) VALUES($1,$2,$3,$4,$5,$6)`, uuid.NewSHA1(socialRedPacketClaimNS, []byte("claim:"+claimIdentity)), internalID, claimantID, amount, transaction.ID, now)
	if err != nil {
		return RedPacketClaim{}, err
	}
	remaining -= amount
	claims--
	_, err = tx.Exec(ctx, `UPDATE social.post_red_packets SET remaining_amount=$2,remaining_claims=$3 WHERE post_id=$1`, internalID, remaining, claims)
	if err != nil {
		return RedPacketClaim{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RedPacketClaim{}, err
	}
	return RedPacketClaim{Amount: amount, RemainingAmount: remaining, RemainingClaims: claims}, nil
}

func (repository *PostgresPostRepository) attachPostFeatures(ctx context.Context, tx pgx.Tx, postInternalID int64, command createPostCommand) error {
	var allowed bool
	if err := tx.QueryRow(ctx, `SELECT enabled AND (allow_member_posts OR $2) FROM social.boards WHERE id=$1 FOR SHARE`, command.BoardID, command.CanPostRestrictedBoard).Scan(&allowed); errors.Is(err, pgx.ErrNoRows) || err == nil && !allowed {
		return ErrSocialBoardUnavailable
	} else if err != nil {
		return err
	}
	seen := map[uuid.UUID]struct{}{}
	for position, mediaID := range command.MediaIDs {
		if mediaID == uuid.Nil {
			return ErrSocialMediaInvalid
		}
		if _, exists := seen[mediaID]; exists {
			return ErrSocialMediaInvalid
		}
		seen[mediaID] = struct{}{}
		tag, err := tx.Exec(ctx, `UPDATE social.post_media SET post_id=$1,position=$2,attached_at=$3 WHERE id=$4 AND uploader_id=$5 AND post_id IS NULL`, postInternalID, position, command.CreatedAt, mediaID, command.AuthorID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrSocialMediaNotFound
		}
	}
	if command.Poll != nil {
		_, err := tx.Exec(ctx, `INSERT INTO social.post_polls(post_id,question,closes_at,created_at) VALUES($1,$2,$3,$4)`, postInternalID, strings.TrimSpace(command.Poll.Question), command.Poll.ClosesAt, command.CreatedAt)
		if err != nil {
			return err
		}
		for position, label := range command.Poll.Options {
			_, err = tx.Exec(ctx, `INSERT INTO social.post_poll_options(id,post_id,position,label) VALUES($1,$2,$3,$4)`, uuid.New(), postInternalID, position, strings.TrimSpace(label))
			if err != nil {
				return err
			}
		}
	}
	for _, topic := range command.Topics {
		_, err := tx.Exec(ctx, `INSERT INTO social.post_topics(post_id,topic,display_topic) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, postInternalID, strings.ToLower(topic), topic)
		if err != nil {
			return err
		}
	}
	if command.RedPacket != nil {
		fundingIdentity := fmt.Sprintf("%s:%s", command.AuthorID, command.RequestID)
		transactionID := uuid.NewSHA1(socialRedPacketFundingNS, []byte(fundingIdentity))
		payload := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d:%d", fundingIdentity, command.PublicID, command.RedPacket.TotalAmount, command.RedPacket.ClaimCount)))
		transaction, err := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{TransactionID: transactionID, TransactionType: economy.TransactionSocialRedPacketFund, IdempotencyKey: "social-red-packet-fund:" + fundingIdentity, SourceReference: "social-red-packet:" + command.PublicID.String(), PolicyRevision: "social-red-packet-v1", PayloadSHA256: payload, OccurredAt: command.CreatedAt, RecordedAt: command.CreatedAt, Postings: []economy.PostingInput{{AccountID: command.AuthorID, Amount: -command.RedPacket.TotalAmount}, {AccountID: socialRedPacketEscrowID, Amount: command.RedPacket.TotalAmount}}})
		if errors.Is(err, economy.ErrInsufficientBalance) {
			return ErrSocialInsufficientMagic
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO social.post_red_packets(post_id,creator_id,total_amount,claim_count,remaining_amount,remaining_claims,funding_transaction_id,created_at) VALUES($1,$2,$3,$4,$3,$4,$5,$6)`, postInternalID, command.AuthorID, command.RedPacket.TotalAmount, command.RedPacket.ClaimCount, transaction.ID, command.CreatedAt)
		if err != nil {
			return err
		}
	}
	return nil
}

func (repository *PostgresPostRepository) ListBoards(ctx context.Context) ([]Board, error) {
	return repository.listBoards(ctx, repository.pool, false)
}
func (repository *PostgresPostRepository) listBoards(ctx context.Context, db communityDB, enabledOnly bool) ([]Board, error) {
	rows, err := db.Query(ctx, `SELECT board.id,board.name,board.description,board.icon,board.tone,board.display_order,board.enabled,board.allow_member_posts,board.version,board.created_at,board.updated_at,(SELECT count(*)::bigint FROM social.posts WHERE board_id=board.id AND state='visible') FROM social.boards AS board WHERE (NOT $1 OR board.enabled) ORDER BY board.display_order,board.id`, enabledOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Board{}
	for rows.Next() {
		var board Board
		if err := rows.Scan(&board.ID, &board.Name, &board.Description, &board.Icon, &board.Tone, &board.DisplayOrder, &board.Enabled, &board.AllowMemberPosts, &board.Version, &board.CreatedAt, &board.UpdatedAt, &board.PostCount); err != nil {
			return nil, err
		}
		result = append(result, board)
	}
	return result, rows.Err()
}

func (repository *PostgresPostRepository) CreateBoard(ctx context.Context, actor authz.StaffActor, decision authz.Decision, input CreateBoardInput, now time.Time) (Board, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return Board{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `INSERT INTO social.boards(id,name,description,icon,tone,display_order,enabled,allow_member_posts,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9) ON CONFLICT DO NOTHING`, input.ID, strings.TrimSpace(input.Name), strings.TrimSpace(input.Description), input.Icon, input.Tone, input.DisplayOrder, input.Enabled, input.AllowMemberPosts, now)
	if err != nil {
		return Board{}, err
	}
	if tag.RowsAffected() != 1 {
		return Board{}, ErrSocialBoardExists
	}
	board, err := scanBoardRow(tx.QueryRow(ctx, `SELECT id,name,description,icon,tone,display_order,enabled,allow_member_posts,version,created_at,updated_at,0::bigint FROM social.boards WHERE id=$1`, input.ID))
	if err != nil {
		return Board{}, err
	}
	after, _ := json.Marshal(board)
	_, err = tx.Exec(ctx, `INSERT INTO social.board_change_events(id,board_id,actor_id,transition,reason,expected_version,resulting_version,authorization_decision_id,before_state,after_state,occurred_at) VALUES($1,$2,$3,'created',$4,0,1,$5,NULL,$6,$7)`, uuid.New(), input.ID, actor.Subject.ID, strings.TrimSpace(input.Reason), decision.ID, after, now)
	if err != nil {
		return Board{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Board{}, err
	}
	return board, nil
}

func (repository *PostgresPostRepository) UpdateBoard(ctx context.Context, actor authz.StaffActor, decision authz.Decision, input UpdateBoardInput, now time.Time) (Board, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return Board{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := scanBoardRow(tx.QueryRow(ctx, `SELECT id,name,description,icon,tone,display_order,enabled,allow_member_posts,version,created_at,updated_at,(SELECT count(*)::bigint FROM social.posts WHERE board_id=$1 AND state='visible') FROM social.boards WHERE id=$1 FOR UPDATE`, input.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Board{}, ErrSocialBoardNotFound
	}
	if err != nil {
		return Board{}, err
	}
	if before.Version != input.ExpectedVersion {
		return Board{}, ErrSocialCommunityConflict
	}
	tag, err := tx.Exec(ctx, `UPDATE social.boards SET name=$2,description=$3,icon=$4,tone=$5,display_order=$6,enabled=$7,allow_member_posts=$8,version=version+1,updated_at=$9 WHERE id=$1 AND version=$10`, input.ID, strings.TrimSpace(input.Name), strings.TrimSpace(input.Description), input.Icon, input.Tone, input.DisplayOrder, input.Enabled, input.AllowMemberPosts, now, input.ExpectedVersion)
	if err != nil {
		return Board{}, err
	}
	if tag.RowsAffected() != 1 {
		return Board{}, ErrSocialCommunityConflict
	}
	after, err := scanBoardRow(tx.QueryRow(ctx, `SELECT id,name,description,icon,tone,display_order,enabled,allow_member_posts,version,created_at,updated_at,(SELECT count(*)::bigint FROM social.posts WHERE board_id=$1 AND state='visible') FROM social.boards WHERE id=$1`, input.ID))
	if err != nil {
		return Board{}, err
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	_, err = tx.Exec(ctx, `INSERT INTO social.board_change_events(id,board_id,actor_id,transition,reason,expected_version,resulting_version,authorization_decision_id,before_state,after_state,occurred_at) VALUES($1,$2,$3,'updated',$4,$5,$6,$7,$8,$9,$10)`, uuid.New(), input.ID, actor.Subject.ID, strings.TrimSpace(input.Reason), input.ExpectedVersion, after.Version, decision.ID, beforeJSON, afterJSON, now)
	if err != nil {
		return Board{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Board{}, err
	}
	return after, nil
}

func scanBoardRow(row pgx.Row) (Board, error) {
	var board Board
	err := row.Scan(&board.ID, &board.Name, &board.Description, &board.Icon, &board.Tone, &board.DisplayOrder, &board.Enabled, &board.AllowMemberPosts, &board.Version, &board.CreatedAt, &board.UpdatedAt, &board.PostCount)
	return board, err
}

func (repository *PostgresPostRepository) ListManagedPosts(ctx context.Context, viewerID uuid.UUID, query PostListQuery) ([]Post, int64, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var total int64
	err = tx.QueryRow(ctx, `SELECT count(*)::bigint FROM social.posts WHERE state IN ('visible','moderator_hidden') AND ($1='' OR board_id=$1)`, query.BoardID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, `SELECT post.id,post.public_id,post.author_id,author.username,author.display_name,post.body,post.state,post.version,post.created_at,post.updated_at,post.edited_at,COALESCE((SELECT count(*) FROM social.comments AS comment JOIN social.post_comment_threads AS binding ON binding.thread_id=comment.thread_id WHERE binding.post_id=post.id),0)::bigint FROM social.posts AS post JOIN identity.users AS author ON author.id=post.author_id WHERE post.state IN ('visible','moderator_hidden') AND ($1='' OR post.board_id=$1) ORDER BY post.created_at DESC LIMIT $2 OFFSET $3`, query.BoardID, query.Limit, query.Offset)
	if err != nil {
		return nil, 0, err
	}
	items := []Post{}
	for rows.Next() {
		var p Post
		var created, updated, edited pgtype.Timestamptz
		var state string
		if err := rows.Scan(new(int64), &p.ID, &p.Author.ID, &p.Author.Username, &p.Author.DisplayName, &p.Body, &state, &p.Version, &created, &updated, &edited, &p.CommentCount); err != nil {
			rows.Close()
			return nil, 0, err
		}
		p.State = PostState(state)
		p.CreatedAt = created.Time.UTC()
		p.UpdatedAt = updated.Time.UTC()
		if edited.Valid {
			v := edited.Time.UTC()
			p.EditedAt = &v
		}
		items = append(items, p)
	}
	rows.Close()
	items, err = repository.enrichPosts(ctx, tx, viewerID, items, time.Now().UTC())
	if err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (repository *PostgresPostRepository) ModeratePost(ctx context.Context, actor authz.StaffActor, decision authz.Decision, input ModeratePostInput, now time.Time) (Post, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return Post{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var internalID int64
	var state, body, boardID string
	var pinned, featured bool
	var version int64
	err = tx.QueryRow(ctx, `SELECT id,state,body,board_id,is_pinned,is_featured,version FROM social.posts WHERE public_id=$1 AND state IN ('visible','moderator_hidden') FOR UPDATE`, input.PostID).Scan(&internalID, &state, &body, &boardID, &pinned, &featured, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return Post{}, ErrPostNotFound
	}
	if err != nil {
		return Post{}, err
	}
	if version != input.ExpectedVersion {
		return Post{}, ErrPostVersionConflict
	}
	var boardExists bool
	if err := tx.QueryRow(ctx, `SELECT true FROM social.boards WHERE id=$1`, input.BoardID).Scan(&boardExists); errors.Is(err, pgx.ErrNoRows) {
		return Post{}, ErrSocialBoardNotFound
	} else if err != nil {
		return Post{}, err
	}
	newState := "visible"
	if input.Hidden {
		newState = "moderator_hidden"
	}
	if state == newState && boardID == input.BoardID && pinned == input.Pinned && featured == input.Featured {
		post, readErr := repository.readManagedPost(ctx, tx, input.PostID)
		if readErr != nil {
			return Post{}, readErr
		}
		enriched, enrichErr := repository.enrichPosts(ctx, tx, actor.Subject.ID, []Post{post}, now)
		if enrichErr != nil {
			return Post{}, enrichErr
		}
		if err := tx.Commit(ctx); err != nil {
			return Post{}, err
		}
		return enriched[0], nil
	}
	before, _ := json.Marshal(map[string]any{"state": state, "board_id": boardID, "pinned": pinned, "featured": featured})
	tag, err := tx.Exec(ctx, `UPDATE social.posts SET state=$2,board_id=$3,is_pinned=$4,is_featured=$5,moderated_at=$6,moderated_by=$7,updated_at=$6,version=version+1 WHERE id=$1 AND version=$8`, internalID, newState, input.BoardID, input.Pinned, input.Featured, now, actor.Subject.ID, input.ExpectedVersion)
	if err != nil {
		return Post{}, err
	}
	if tag.RowsAffected() != 1 {
		return Post{}, ErrPostVersionConflict
	}
	after, _ := json.Marshal(map[string]any{"state": newState, "board_id": input.BoardID, "pinned": input.Pinned, "featured": input.Featured})
	_, err = tx.Exec(ctx, `INSERT INTO social.post_management_events(id,post_id,actor_id,reason,expected_version,resulting_version,authorization_decision_id,before_state,after_state,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, uuid.New(), internalID, actor.Subject.ID, strings.TrimSpace(input.Reason), input.ExpectedVersion, input.ExpectedVersion+1, decision.ID, before, after, now)
	if err != nil {
		return Post{}, err
	}
	post, err := repository.readManagedPost(ctx, tx, input.PostID)
	if err != nil {
		return Post{}, err
	}
	enriched, err := repository.enrichPosts(ctx, tx, actor.Subject.ID, []Post{post}, now)
	if err != nil {
		return Post{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Post{}, err
	}
	return enriched[0], nil
}

func (repository *PostgresPostRepository) readManagedPost(ctx context.Context, db communityDB, id uuid.UUID) (Post, error) {
	var p Post
	var state string
	var created, updated, edited pgtype.Timestamptz
	err := db.QueryRow(ctx, `SELECT post.public_id,post.author_id,author.username,author.display_name,post.body,post.state,post.version,post.created_at,post.updated_at,post.edited_at,COALESCE((SELECT count(*) FROM social.comments AS comment JOIN social.post_comment_threads AS binding ON binding.thread_id=comment.thread_id WHERE binding.post_id=post.id),0)::bigint FROM social.posts AS post JOIN identity.users AS author ON author.id=post.author_id WHERE post.public_id=$1`, id).Scan(&p.ID, &p.Author.ID, &p.Author.Username, &p.Author.DisplayName, &p.Body, &state, &p.Version, &created, &updated, &edited, &p.CommentCount)
	if err != nil {
		return Post{}, err
	}
	p.State = PostState(state)
	p.CreatedAt = created.Time.UTC()
	p.UpdatedAt = updated.Time.UTC()
	if edited.Valid {
		v := edited.Time.UTC()
		p.EditedAt = &v
	}
	return p, nil
}

func socialDatabaseConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

var _ CommunityRepository = (*PostgresPostRepository)(nil)
