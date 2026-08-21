// Package legacypersonalstate imports finite PtYes/Rousi user-owned state that
// is not part of an account balance: torrent bookmarks, invitation ancestry,
// and historical invitation reward totals.
package legacypersonalstate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

const (
	bookmarkFingerprintDomain     = "peergo:migration:ptyes-bookmark:v1\x00"
	relationshipFingerprintDomain = "peergo:migration:ptyes-invitation-relationship:v1\x00"
	rewardFingerprintDomain       = "peergo:migration:ptyes-invitation-reward:v1\x00"
	evidenceFingerprintDomain     = "peergo:migration:ptyes-personal-state:v1\x00"
)

type Config struct {
	RunID          uuid.UUID
	SnapshotSHA256 [sha256.Size]byte
	MappingVersion string
	ImportedAt     time.Time
}

type Progress struct {
	Phase     string
	Processed int64
	Expected  int64
}

type Result struct {
	RunID                      uuid.UUID
	BookmarkSourceRows         int64
	BookmarkDistinctPairs      int64
	BookmarkAppliedRows        int64
	BookmarkUnresolvedRows     int64
	InvitationSourceRows       int64
	InvitationRelationships    int64
	InvitationUnresolvedRows   int64
	HaremRewardSourceRows      int64
	HaremRewardUsers           int64
	InvitationRewardSourceRows int64
	InvitationRewardUsers      int64
	Duplicate                  bool
}

type bookmarkRow struct {
	ID              int64
	LegacyUserID    int64
	LegacyTorrentID int64
	CreatedAt       time.Time
	Fingerprint     [sha256.Size]byte
}

type relationshipRow struct {
	ID              int64
	LegacyInviterID int64
	LegacyInviteeID int64
	ClaimedAt       time.Time
	CreatedAt       time.Time
	Fingerprint     [sha256.Size]byte
}

type rewardRow struct {
	LegacyUserID  int64
	Kind          string
	SourceRows    int64
	ExactAmount   string
	RoundedAmount int64
	FirstAt       time.Time
	LastAt        time.Time
	Fingerprint   [sha256.Size]byte
}

type torrentMapping struct {
	TorrentID int64
	Available bool
}

type sourceState struct {
	Bookmarks     []bookmarkRow
	Relationships []relationshipRow
	Rewards       []rewardRow
	EvidenceSHA   [sha256.Size]byte
}

// Import commits all three projections and their receipt in one Core
// transaction. A replay verifies existing append-only evidence and never
// recreates a bookmark a member deleted after the first successful import.
func Import(
	ctx context.Context,
	source, core *pgxpool.Pool,
	config Config,
	progress func(Progress),
) (Result, error) {
	config = normalizeConfig(config)
	if err := validateConfig(source, core, config); err != nil {
		return Result{}, err
	}
	if progress == nil {
		progress = func(Progress) {}
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, core); err != nil {
		return Result{}, err
	}
	if err := requireRun(ctx, core, config); err != nil {
		return Result{}, err
	}
	state, err := readSource(ctx, source)
	if err != nil {
		return Result{}, err
	}
	users, err := readUserMappings(ctx, core)
	if err != nil {
		return Result{}, err
	}
	torrents, err := readTorrentMappings(ctx, core)
	if err != nil {
		return Result{}, err
	}
	canonicalBookmarks := canonicalBookmarkRows(state.Bookmarks)

	tx, err := core.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("begin legacy personal state import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result := Result{
		RunID:                 config.RunID,
		BookmarkSourceRows:    int64(len(state.Bookmarks)),
		BookmarkDistinctPairs: int64(len(canonicalBookmarks)),
		InvitationSourceRows:  int64(len(state.Relationships)),
	}
	for index, row := range state.Bookmarks {
		disposition, replay, err := importBookmark(
			ctx, tx, config, row, canonicalBookmarks, users, torrents,
		)
		if err != nil {
			return Result{}, err
		}
		if disposition == "imported" || disposition == "already_present" {
			result.BookmarkAppliedRows++
		}
		if strings.HasPrefix(disposition, "unmapped_") || disposition == "unavailable_torrent" {
			result.BookmarkUnresolvedRows++
		}
		_ = replay
		if (index+1)%1000 == 0 || index+1 == len(state.Bookmarks) {
			progress(Progress{Phase: "bookmarks", Processed: int64(index + 1), Expected: int64(len(state.Bookmarks))})
		}
	}

	for index, row := range state.Relationships {
		disposition, err := importRelationship(ctx, tx, config, row, users)
		if err != nil {
			return Result{}, err
		}
		if disposition == "imported" || disposition == "already_present" {
			result.InvitationRelationships++
		} else {
			result.InvitationUnresolvedRows++
		}
		if (index+1)%100 == 0 || index+1 == len(state.Relationships) {
			progress(Progress{Phase: "invitation_relationships", Processed: int64(index + 1), Expected: int64(len(state.Relationships))})
		}
	}

	for index, row := range state.Rewards {
		if err := importReward(ctx, tx, config, row, users); err != nil {
			return Result{}, err
		}
		switch row.Kind {
		case "harem":
			result.HaremRewardSourceRows += row.SourceRows
			result.HaremRewardUsers++
		case "invite_reward":
			result.InvitationRewardSourceRows += row.SourceRows
			result.InvitationRewardUsers++
		}
		if (index+1)%100 == 0 || index+1 == len(state.Rewards) {
			progress(Progress{Phase: "invitation_rewards", Processed: int64(index + 1), Expected: int64(len(state.Rewards))})
		}
	}

	receiptExists, err := persistReceipt(ctx, tx, config, state.EvidenceSHA, result)
	if err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit legacy personal state import: %w", err)
	}
	result.Duplicate = receiptExists
	if _, err := Verify(ctx, source, core, config); err != nil {
		return Result{}, err
	}
	return result, nil
}

// Verify binds the immutable source snapshot to the receipt and checks that
// every imported relationship still has the expected edge. Bookmark presence
// is deliberately not required because bookmarks remain mutable after import.
func Verify(ctx context.Context, source, core *pgxpool.Pool, config Config) (Result, error) {
	config = normalizeConfig(config)
	if err := validateConfig(source, core, config); err != nil {
		return Result{}, err
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, core); err != nil {
		return Result{}, err
	}
	if err := requireRun(ctx, core, config); err != nil {
		return Result{}, err
	}
	state, err := readSource(ctx, source)
	if err != nil {
		return Result{}, err
	}
	want := sourceResult(config.RunID, state)
	var snapshot, evidence []byte
	var got Result
	got.RunID = config.RunID
	err = core.QueryRow(ctx, `
SELECT source_snapshot_sha256, source_evidence_sha256,
       bookmark_source_rows, bookmark_distinct_pairs,
       bookmark_applied_rows, bookmark_unresolved_rows,
       invitation_source_rows, invitation_relationships,
       invitation_unresolved_rows, harem_reward_source_rows,
       harem_reward_users, invite_reward_source_rows, invite_reward_users
FROM migration.legacy_personal_state_imports
WHERE run_id = $1`, config.RunID).Scan(
		&snapshot, &evidence,
		&got.BookmarkSourceRows, &got.BookmarkDistinctPairs,
		&got.BookmarkAppliedRows, &got.BookmarkUnresolvedRows,
		&got.InvitationSourceRows, &got.InvitationRelationships,
		&got.InvitationUnresolvedRows, &got.HaremRewardSourceRows,
		&got.HaremRewardUsers, &got.InvitationRewardSourceRows,
		&got.InvitationRewardUsers,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{}, errors.New("legacy personal state receipt is missing")
	}
	if err != nil {
		return Result{}, fmt.Errorf("read legacy personal state receipt: %w", err)
	}
	if !bytes.Equal(snapshot, config.SnapshotSHA256[:]) || !bytes.Equal(evidence, state.EvidenceSHA[:]) ||
		got.BookmarkSourceRows != want.BookmarkSourceRows ||
		got.BookmarkDistinctPairs != want.BookmarkDistinctPairs ||
		got.InvitationSourceRows != want.InvitationSourceRows ||
		got.HaremRewardSourceRows != want.HaremRewardSourceRows ||
		got.HaremRewardUsers != want.HaremRewardUsers ||
		got.InvitationRewardSourceRows != want.InvitationRewardSourceRows ||
		got.InvitationRewardUsers != want.InvitationRewardUsers {
		return Result{}, errors.New("legacy personal state receipt conflicts with the immutable source")
	}

	var bookmarkEvidence, relationshipEvidence, rewardEvidence, brokenRelationships int64
	err = core.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM migration.legacy_torrent_bookmark_openings WHERE first_run_id = $1),
    (SELECT count(*) FROM migration.legacy_invitation_relationship_openings WHERE first_run_id = $1),
    (SELECT count(*) FROM migration.legacy_invitation_reward_openings WHERE first_run_id = $1),
    (SELECT count(*)
       FROM migration.legacy_invitation_relationship_openings AS opening
       LEFT JOIN identity.invitation_relationships AS relationship
         ON relationship.invitee_user_id = opening.invitee_user_id
        AND relationship.inviter_user_id = opening.inviter_user_id
      WHERE opening.first_run_id = $1
        AND opening.disposition IN ('imported', 'already_present')
        AND relationship.invitee_user_id IS NULL)
`, config.RunID).Scan(&bookmarkEvidence, &relationshipEvidence, &rewardEvidence, &brokenRelationships)
	if err != nil {
		return Result{}, fmt.Errorf("verify legacy personal state evidence: %w", err)
	}
	if bookmarkEvidence != int64(len(state.Bookmarks)) ||
		relationshipEvidence != int64(len(state.Relationships)) ||
		rewardEvidence != int64(len(state.Rewards)) || brokenRelationships != 0 {
		return Result{}, fmt.Errorf(
			"legacy personal state evidence is incomplete: bookmarks=%d/%d relationships=%d/%d rewards=%d/%d broken_relationships=%d",
			bookmarkEvidence, len(state.Bookmarks), relationshipEvidence, len(state.Relationships),
			rewardEvidence, len(state.Rewards), brokenRelationships,
		)
	}
	got.Duplicate = true
	return got, nil
}

func normalizeConfig(config Config) Config {
	config.MappingVersion = strings.TrimSpace(config.MappingVersion)
	config.ImportedAt = config.ImportedAt.UTC().Truncate(time.Microsecond)
	return config
}

func validateConfig(source, core *pgxpool.Pool, config Config) error {
	if source == nil || core == nil || config.RunID == uuid.Nil ||
		config.SnapshotSHA256 == ([sha256.Size]byte{}) ||
		config.MappingVersion == "" || config.ImportedAt.IsZero() {
		return errors.New("legacy personal state import configuration is invalid")
	}
	return nil
}

func requireRun(ctx context.Context, core *pgxpool.Pool, config Config) error {
	var snapshot []byte
	var mappingVersion, state string
	err := core.QueryRow(ctx, `
SELECT source_snapshot_sha256, mapping_version, state
FROM migration.runs WHERE id = $1`, config.RunID).Scan(&snapshot, &mappingVersion, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("legacy personal state migration run was not found")
	}
	if err != nil {
		return fmt.Errorf("read legacy personal state migration run: %w", err)
	}
	if !bytes.Equal(snapshot, config.SnapshotSHA256[:]) || mappingVersion != config.MappingVersion {
		return errors.New("legacy personal state run identity does not match the snapshot")
	}
	if state != "importing" && state != "imported" && state != "reconciled" {
		return fmt.Errorf("legacy personal state run state %q cannot accept an import", state)
	}
	return nil
}

func readSource(ctx context.Context, source *pgxpool.Pool) (sourceState, error) {
	bookmarks, err := readBookmarks(ctx, source)
	if err != nil {
		return sourceState{}, err
	}
	relationships, err := readRelationships(ctx, source)
	if err != nil {
		return sourceState{}, err
	}
	if err := validateRelationshipGraph(relationships); err != nil {
		return sourceState{}, err
	}
	rewards, err := readRewards(ctx, source)
	if err != nil {
		return sourceState{}, err
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(evidenceFingerprintDomain))
	for _, row := range bookmarks {
		_, _ = hash.Write(row.Fingerprint[:])
	}
	for _, row := range relationships {
		_, _ = hash.Write(row.Fingerprint[:])
	}
	for _, row := range rewards {
		_, _ = hash.Write(row.Fingerprint[:])
	}
	var evidence [sha256.Size]byte
	copy(evidence[:], hash.Sum(nil))
	return sourceState{Bookmarks: bookmarks, Relationships: relationships, Rewards: rewards, EvidenceSHA: evidence}, nil
}

func readBookmarks(ctx context.Context, source *pgxpool.Pool) ([]bookmarkRow, error) {
	rows, err := source.Query(ctx, `
SELECT id, user_id, torrent_id, created_at
FROM public.bookmarks
ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query PtYes bookmarks: %w", err)
	}
	defer rows.Close()
	result := make([]bookmarkRow, 0, 10_000)
	for rows.Next() {
		var row bookmarkRow
		if err := rows.Scan(&row.ID, &row.LegacyUserID, &row.LegacyTorrentID, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan PtYes bookmark: %w", err)
		}
		row.CreatedAt = row.CreatedAt.UTC().Truncate(time.Microsecond)
		if row.ID < 1 || row.LegacyUserID < 1 || row.LegacyTorrentID < 1 || row.CreatedAt.IsZero() {
			return nil, fmt.Errorf("PtYes bookmark %d is invalid", row.ID)
		}
		row.Fingerprint = fingerprint(bookmarkFingerprintDomain,
			strconv.FormatInt(row.ID, 10), strconv.FormatInt(row.LegacyUserID, 10),
			strconv.FormatInt(row.LegacyTorrentID, 10), canonicalTime(row.CreatedAt))
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish PtYes bookmark query: %w", err)
	}
	return result, nil
}

func readRelationships(ctx context.Context, source *pgxpool.Pool) ([]relationshipRow, error) {
	rows, err := source.Query(ctx, `
SELECT id, inviting_user, claimed_by_uid, claimed_at, created_at
FROM public.invites
WHERE claimed = true AND claimed_by_uid IS NOT NULL
ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query PtYes claimed invitations: %w", err)
	}
	defer rows.Close()
	result := make([]relationshipRow, 0, 512)
	for rows.Next() {
		var row relationshipRow
		if err := rows.Scan(&row.ID, &row.LegacyInviterID, &row.LegacyInviteeID, &row.ClaimedAt, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan PtYes claimed invitation: %w", err)
		}
		row.ClaimedAt = row.ClaimedAt.UTC().Truncate(time.Microsecond)
		row.CreatedAt = row.CreatedAt.UTC().Truncate(time.Microsecond)
		// PtYes uses inviter 0 for system-issued invitations. Preserve those
		// source rows as unresolved ancestry evidence instead of inventing an
		// inviter or rejecting an otherwise valid claimed invitation.
		if row.ID < 1 || row.LegacyInviterID < 0 || row.LegacyInviteeID < 1 ||
			row.LegacyInviterID == row.LegacyInviteeID || row.ClaimedAt.IsZero() ||
			row.CreatedAt.IsZero() || row.ClaimedAt.Before(row.CreatedAt) {
			return nil, fmt.Errorf("PtYes claimed invitation %d is invalid", row.ID)
		}
		row.Fingerprint = fingerprint(relationshipFingerprintDomain,
			strconv.FormatInt(row.ID, 10), strconv.FormatInt(row.LegacyInviterID, 10),
			strconv.FormatInt(row.LegacyInviteeID, 10), canonicalTime(row.ClaimedAt),
			canonicalTime(row.CreatedAt))
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish PtYes invitation query: %w", err)
	}
	return result, nil
}

func readRewards(ctx context.Context, source *pgxpool.Pool) ([]rewardRow, error) {
	rows, err := source.Query(ctx, `
SELECT user_id, action, count(*)::bigint, sum(amount)::text,
       round(sum(amount))::bigint, min(created_at), max(created_at)
FROM public.points_logs
WHERE point_type = 'karma'
  AND action IN ('harem', 'invite_reward')
  AND amount > 0
GROUP BY user_id, action
ORDER BY action, user_id`)
	if err != nil {
		return nil, fmt.Errorf("query PtYes invitation rewards: %w", err)
	}
	defer rows.Close()
	result := make([]rewardRow, 0, 256)
	for rows.Next() {
		var row rewardRow
		if err := rows.Scan(
			&row.LegacyUserID, &row.Kind, &row.SourceRows, &row.ExactAmount,
			&row.RoundedAmount, &row.FirstAt, &row.LastAt,
		); err != nil {
			return nil, fmt.Errorf("scan PtYes invitation reward: %w", err)
		}
		row.FirstAt = row.FirstAt.UTC().Truncate(time.Microsecond)
		row.LastAt = row.LastAt.UTC().Truncate(time.Microsecond)
		if row.LegacyUserID < 1 || (row.Kind != "harem" && row.Kind != "invite_reward") ||
			row.SourceRows < 1 || row.RoundedAmount < 0 || row.FirstAt.IsZero() ||
			row.LastAt.Before(row.FirstAt) {
			return nil, fmt.Errorf("PtYes invitation reward %d/%s is invalid", row.LegacyUserID, row.Kind)
		}
		row.Fingerprint = fingerprint(rewardFingerprintDomain,
			strconv.FormatInt(row.LegacyUserID, 10), row.Kind,
			strconv.FormatInt(row.SourceRows, 10), row.ExactAmount,
			strconv.FormatInt(row.RoundedAmount, 10), canonicalTime(row.FirstAt),
			canonicalTime(row.LastAt))
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish PtYes invitation reward query: %w", err)
	}
	return result, nil
}

func readUserMappings(ctx context.Context, core *pgxpool.Pool) (map[int64]uuid.UUID, error) {
	rows, err := core.Query(ctx, `
SELECT mapping.legacy_user_id, mapping.user_id
FROM migration.user_id_map AS mapping
JOIN identity.users AS target ON target.id = mapping.user_id
WHERE mapping.source_system = 'ptyes'`)
	if err != nil {
		return nil, fmt.Errorf("list personal state user mappings: %w", err)
	}
	defer rows.Close()
	result := make(map[int64]uuid.UUID)
	for rows.Next() {
		var legacyID int64
		var targetID uuid.UUID
		if err := rows.Scan(&legacyID, &targetID); err != nil {
			return nil, fmt.Errorf("scan personal state user mapping: %w", err)
		}
		result[legacyID] = targetID
	}
	return result, rows.Err()
}

func readTorrentMappings(ctx context.Context, core *pgxpool.Pool) (map[int64]torrentMapping, error) {
	rows, err := core.Query(ctx, `
SELECT mapping.legacy_torrent_id, mapping.torrent_id,
       (catalog.id IS NOT NULL) AS available
FROM migration.torrent_id_map AS mapping
JOIN torrents.torrents AS target ON target.id = mapping.torrent_id
LEFT JOIN catalog.torrents AS catalog ON catalog.id = mapping.torrent_id
WHERE mapping.source_system = 'ptyes'`)
	if err != nil {
		return nil, fmt.Errorf("list personal state torrent mappings: %w", err)
	}
	defer rows.Close()
	result := make(map[int64]torrentMapping)
	for rows.Next() {
		var legacyID int64
		var mapping torrentMapping
		if err := rows.Scan(&legacyID, &mapping.TorrentID, &mapping.Available); err != nil {
			return nil, fmt.Errorf("scan personal state torrent mapping: %w", err)
		}
		result[legacyID] = mapping
	}
	return result, rows.Err()
}

func canonicalBookmarkRows(rows []bookmarkRow) map[[2]int64]int64 {
	result := make(map[[2]int64]int64, len(rows))
	for _, row := range rows {
		key := [2]int64{row.LegacyUserID, row.LegacyTorrentID}
		if current, exists := result[key]; !exists || row.ID < current {
			result[key] = row.ID
		}
	}
	return result
}

func validateRelationshipGraph(rows []relationshipRow) error {
	parents := make(map[int64]int64, len(rows))
	for _, row := range rows {
		if existing, exists := parents[row.LegacyInviteeID]; exists && existing != row.LegacyInviterID {
			return fmt.Errorf("PtYes invitee %d has conflicting inviters", row.LegacyInviteeID)
		}
		parents[row.LegacyInviteeID] = row.LegacyInviterID
	}
	for invitee := range parents {
		seen := map[int64]bool{invitee: true}
		for current := parents[invitee]; current != 0; current = parents[current] {
			if seen[current] {
				return fmt.Errorf("PtYes invitation graph contains a cycle at user %d", current)
			}
			seen[current] = true
		}
	}
	return nil
}

func importBookmark(
	ctx context.Context,
	tx pgx.Tx,
	config Config,
	row bookmarkRow,
	canonical map[[2]int64]int64,
	users map[int64]uuid.UUID,
	torrents map[int64]torrentMapping,
) (string, bool, error) {
	if disposition, exists, err := verifyBookmarkReplay(ctx, tx, config, row); err != nil || exists {
		return disposition, exists, err
	}
	userID, userMapped := users[row.LegacyUserID]
	torrent, torrentMapped := torrents[row.LegacyTorrentID]
	disposition := ""
	switch {
	case !userMapped:
		disposition = "unmapped_user"
	case !torrentMapped:
		disposition = "unmapped_torrent"
	case !torrent.Available:
		disposition = "unavailable_torrent"
	case canonical[[2]int64{row.LegacyUserID, row.LegacyTorrentID}] != row.ID:
		disposition = "duplicate"
	default:
		command, err := tx.Exec(ctx, `
INSERT INTO catalog.torrent_bookmarks (user_id, torrent_id, created_at)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, torrent_id) DO NOTHING`, userID, torrent.TorrentID, row.CreatedAt)
		if err != nil {
			return "", false, fmt.Errorf("insert PtYes bookmark %d: %w", row.ID, err)
		}
		if command.RowsAffected() == 1 {
			disposition = "imported"
		} else {
			disposition = "already_present"
		}
	}
	var userValue any
	if userMapped {
		userValue = userID
	}
	var torrentValue any
	if torrentMapped {
		torrentValue = torrent.TorrentID
	}
	_, err := tx.Exec(ctx, `
INSERT INTO migration.legacy_torrent_bookmark_openings (
    legacy_bookmark_id, first_run_id, legacy_user_id, user_id,
    legacy_torrent_id, torrent_id, disposition, source_fingerprint,
    bookmarked_at, imported_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		row.ID, config.RunID, row.LegacyUserID, userValue,
		row.LegacyTorrentID, torrentValue, disposition, row.Fingerprint[:],
		row.CreatedAt, config.ImportedAt,
	)
	if err != nil {
		return "", false, fmt.Errorf("record PtYes bookmark %d opening: %w", row.ID, err)
	}
	return disposition, false, nil
}

func verifyBookmarkReplay(ctx context.Context, tx pgx.Tx, config Config, row bookmarkRow) (string, bool, error) {
	var runID uuid.UUID
	var legacyUserID, legacyTorrentID int64
	var fingerprint []byte
	var disposition string
	var bookmarkedAt time.Time
	err := tx.QueryRow(ctx, `
SELECT first_run_id, legacy_user_id, legacy_torrent_id,
       source_fingerprint, disposition, bookmarked_at
FROM migration.legacy_torrent_bookmark_openings
WHERE legacy_bookmark_id = $1`, row.ID).Scan(
		&runID, &legacyUserID, &legacyTorrentID, &fingerprint, &disposition, &bookmarkedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read PtYes bookmark %d opening: %w", row.ID, err)
	}
	if runID != config.RunID || legacyUserID != row.LegacyUserID ||
		legacyTorrentID != row.LegacyTorrentID || !bytes.Equal(fingerprint, row.Fingerprint[:]) ||
		!bookmarkedAt.UTC().Equal(row.CreatedAt) {
		return "", true, fmt.Errorf("PtYes bookmark %d opening conflicts with this snapshot", row.ID)
	}
	return disposition, true, nil
}

func importRelationship(ctx context.Context, tx pgx.Tx, config Config, row relationshipRow, users map[int64]uuid.UUID) (string, error) {
	if disposition, exists, err := verifyRelationshipReplay(ctx, tx, config, row); err != nil || exists {
		return disposition, err
	}
	inviterID, inviterMapped := users[row.LegacyInviterID]
	inviteeID, inviteeMapped := users[row.LegacyInviteeID]
	disposition := ""
	switch {
	case !inviterMapped:
		disposition = "unmapped_inviter"
	case !inviteeMapped:
		disposition = "unmapped_invitee"
	default:
		var existingInviter uuid.UUID
		err := tx.QueryRow(ctx, `
SELECT inviter_user_id
FROM identity.invitation_relationships
WHERE invitee_user_id = $1`, inviteeID).Scan(&existingInviter)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			recordedAt := config.ImportedAt
			if recordedAt.Before(row.ClaimedAt) {
				recordedAt = row.ClaimedAt
			}
			_, err = tx.Exec(ctx, `
INSERT INTO identity.invitation_relationships (
    invitee_user_id, inviter_user_id, invitation_id, source_kind,
    source_reference, source_run_id, source_fingerprint,
    established_at, recorded_at
) VALUES ($1,$2,NULL,'legacy_import',$3,$4,$5,$6,$7)`,
				inviteeID, inviterID, "ptyes-invite:"+strconv.FormatInt(row.ID, 10),
				config.RunID, row.Fingerprint[:], row.ClaimedAt, recordedAt,
			)
			if err != nil {
				return "", fmt.Errorf("insert PtYes invitation relationship %d: %w", row.ID, err)
			}
			disposition = "imported"
		case err != nil:
			return "", fmt.Errorf("read PtYes invitation relationship %d target: %w", row.ID, err)
		case existingInviter == inviterID:
			disposition = "already_present"
		default:
			return "", fmt.Errorf("PtYes invitee %d already belongs to another inviter", row.LegacyInviteeID)
		}
	}
	var inviterValue, inviteeValue any
	if inviterMapped {
		inviterValue = inviterID
	}
	if inviteeMapped {
		inviteeValue = inviteeID
	}
	_, err := tx.Exec(ctx, `
INSERT INTO migration.legacy_invitation_relationship_openings (
    legacy_invitation_id, first_run_id, legacy_inviter_id, inviter_user_id,
    legacy_invitee_id, invitee_user_id, disposition, source_fingerprint,
    established_at, source_created_at, imported_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		row.ID, config.RunID, row.LegacyInviterID, inviterValue,
		row.LegacyInviteeID, inviteeValue, disposition, row.Fingerprint[:],
		row.ClaimedAt, row.CreatedAt, config.ImportedAt,
	)
	if err != nil {
		return "", fmt.Errorf("record PtYes invitation relationship %d opening: %w", row.ID, err)
	}
	return disposition, nil
}

func verifyRelationshipReplay(ctx context.Context, tx pgx.Tx, config Config, row relationshipRow) (string, bool, error) {
	var runID uuid.UUID
	var inviterID, inviteeID int64
	var fingerprint []byte
	var disposition string
	err := tx.QueryRow(ctx, `
SELECT first_run_id, legacy_inviter_id, legacy_invitee_id,
       source_fingerprint, disposition
FROM migration.legacy_invitation_relationship_openings
WHERE legacy_invitation_id = $1`, row.ID).Scan(
		&runID, &inviterID, &inviteeID, &fingerprint, &disposition,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read PtYes invitation relationship %d opening: %w", row.ID, err)
	}
	if runID != config.RunID || inviterID != row.LegacyInviterID || inviteeID != row.LegacyInviteeID ||
		!bytes.Equal(fingerprint, row.Fingerprint[:]) {
		return "", true, fmt.Errorf("PtYes invitation relationship %d conflicts with this snapshot", row.ID)
	}
	return disposition, true, nil
}

func importReward(ctx context.Context, tx pgx.Tx, config Config, row rewardRow, users map[int64]uuid.UUID) error {
	var existingRun uuid.UUID
	var sourceRows, roundedAmount int64
	var exactAmount, disposition string
	var fingerprint []byte
	err := tx.QueryRow(ctx, `
SELECT first_run_id, source_row_count, exact_amount::text, rounded_amount,
       disposition, source_fingerprint
FROM migration.legacy_invitation_reward_openings
WHERE legacy_user_id = $1 AND reward_kind = $2`, row.LegacyUserID, row.Kind).Scan(
		&existingRun, &sourceRows, &exactAmount, &roundedAmount, &disposition, &fingerprint,
	)
	if err == nil {
		if existingRun != config.RunID || sourceRows != row.SourceRows || exactAmount != row.ExactAmount ||
			roundedAmount != row.RoundedAmount || !bytes.Equal(fingerprint, row.Fingerprint[:]) {
			return fmt.Errorf("PtYes invitation reward %d/%s conflicts with this snapshot", row.LegacyUserID, row.Kind)
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read PtYes invitation reward %d/%s opening: %w", row.LegacyUserID, row.Kind, err)
	}
	userID, mapped := users[row.LegacyUserID]
	var userValue any
	disposition = "unmapped_user"
	if mapped {
		userValue = userID
		disposition = "preserved"
	}
	_, err = tx.Exec(ctx, `
INSERT INTO migration.legacy_invitation_reward_openings (
    legacy_user_id, reward_kind, first_run_id, user_id, source_row_count,
    exact_amount, rounded_amount, source_fingerprint, first_rewarded_at,
    last_rewarded_at, disposition, imported_at
) VALUES ($1,$2,$3,$4,$5,$6::numeric,$7,$8,$9,$10,$11,$12)`,
		row.LegacyUserID, row.Kind, config.RunID, userValue, row.SourceRows,
		row.ExactAmount, row.RoundedAmount, row.Fingerprint[:], row.FirstAt,
		row.LastAt, disposition, config.ImportedAt,
	)
	if err != nil {
		return fmt.Errorf("record PtYes invitation reward %d/%s opening: %w", row.LegacyUserID, row.Kind, err)
	}
	return nil
}

func persistReceipt(ctx context.Context, tx pgx.Tx, config Config, evidence [sha256.Size]byte, result Result) (bool, error) {
	command, err := tx.Exec(ctx, `
INSERT INTO migration.legacy_personal_state_imports (
    run_id, source_snapshot_sha256, source_evidence_sha256,
    bookmark_source_rows, bookmark_distinct_pairs, bookmark_applied_rows,
    bookmark_unresolved_rows, invitation_source_rows, invitation_relationships,
    invitation_unresolved_rows, harem_reward_source_rows, harem_reward_users,
    invite_reward_source_rows, invite_reward_users, imported_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
ON CONFLICT (run_id) DO NOTHING`,
		config.RunID, config.SnapshotSHA256[:], evidence[:],
		result.BookmarkSourceRows, result.BookmarkDistinctPairs, result.BookmarkAppliedRows,
		result.BookmarkUnresolvedRows, result.InvitationSourceRows, result.InvitationRelationships,
		result.InvitationUnresolvedRows, result.HaremRewardSourceRows, result.HaremRewardUsers,
		result.InvitationRewardSourceRows, result.InvitationRewardUsers, config.ImportedAt,
	)
	if err != nil {
		return false, fmt.Errorf("insert legacy personal state receipt: %w", err)
	}
	if command.RowsAffected() == 1 {
		return false, nil
	}
	var snapshot, existingEvidence []byte
	var existing Result
	err = tx.QueryRow(ctx, `
SELECT source_snapshot_sha256, source_evidence_sha256,
       bookmark_source_rows, bookmark_distinct_pairs, bookmark_applied_rows,
       bookmark_unresolved_rows, invitation_source_rows, invitation_relationships,
       invitation_unresolved_rows, harem_reward_source_rows, harem_reward_users,
       invite_reward_source_rows, invite_reward_users
FROM migration.legacy_personal_state_imports WHERE run_id = $1`, config.RunID).Scan(
		&snapshot, &existingEvidence,
		&existing.BookmarkSourceRows, &existing.BookmarkDistinctPairs, &existing.BookmarkAppliedRows,
		&existing.BookmarkUnresolvedRows, &existing.InvitationSourceRows, &existing.InvitationRelationships,
		&existing.InvitationUnresolvedRows, &existing.HaremRewardSourceRows, &existing.HaremRewardUsers,
		&existing.InvitationRewardSourceRows, &existing.InvitationRewardUsers,
	)
	if err != nil {
		return true, fmt.Errorf("read legacy personal state receipt replay: %w", err)
	}
	if !bytes.Equal(snapshot, config.SnapshotSHA256[:]) || !bytes.Equal(existingEvidence, evidence[:]) ||
		existing.BookmarkSourceRows != result.BookmarkSourceRows ||
		existing.BookmarkDistinctPairs != result.BookmarkDistinctPairs ||
		existing.BookmarkAppliedRows != result.BookmarkAppliedRows ||
		existing.BookmarkUnresolvedRows != result.BookmarkUnresolvedRows ||
		existing.InvitationSourceRows != result.InvitationSourceRows ||
		existing.InvitationRelationships != result.InvitationRelationships ||
		existing.InvitationUnresolvedRows != result.InvitationUnresolvedRows ||
		existing.HaremRewardSourceRows != result.HaremRewardSourceRows ||
		existing.HaremRewardUsers != result.HaremRewardUsers ||
		existing.InvitationRewardSourceRows != result.InvitationRewardSourceRows ||
		existing.InvitationRewardUsers != result.InvitationRewardUsers {
		return true, errors.New("legacy personal state receipt replay conflicts with existing evidence")
	}
	return true, nil
}

func sourceResult(runID uuid.UUID, state sourceState) Result {
	result := Result{
		RunID:                 runID,
		BookmarkSourceRows:    int64(len(state.Bookmarks)),
		BookmarkDistinctPairs: int64(len(canonicalBookmarkRows(state.Bookmarks))),
		InvitationSourceRows:  int64(len(state.Relationships)),
	}
	for _, row := range state.Rewards {
		switch row.Kind {
		case "harem":
			result.HaremRewardSourceRows += row.SourceRows
			result.HaremRewardUsers++
		case "invite_reward":
			result.InvitationRewardSourceRows += row.SourceRows
			result.InvitationRewardUsers++
		}
	}
	return result
}

func fingerprint(domain string, fields ...string) [sha256.Size]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte(domain))
	for _, field := range fields {
		_, _ = hash.Write([]byte(strconv.Itoa(len(field))))
		_, _ = hash.Write([]byte{':'})
		_, _ = hash.Write([]byte(field))
		_, _ = hash.Write([]byte{0})
	}
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func canonicalTime(value time.Time) string {
	return value.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
}
