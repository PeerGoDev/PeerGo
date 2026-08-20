package torrents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/generated/torrentdb"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
)

type PostgresTorrentReadRepository struct {
	pool    *pgxpool.Pool
	queries *torrentdb.Queries
}

func NewPostgresTorrentReadRepository(pool *pgxpool.Pool) (*PostgresTorrentReadRepository, error) {
	if pool == nil {
		return nil, errors.New("torrent read database is required")
	}
	return &PostgresTorrentReadRepository{pool: pool, queries: torrentdb.New(pool)}, nil
}

func (repository *PostgresTorrentReadRepository) PublishedDetail(ctx context.Context, torrentID TorrentID) (PublicDetail, error) {
	if torrentID < 1 {
		return PublicDetail{}, ErrTorrentReadInput
	}

	// The publication guard, controlled facets and external identifiers must be
	// observed from one snapshot. Otherwise a concurrent moderation action could
	// expose metadata after the aggregate stopped being public, or return a detail
	// assembled from two different metadata revisions.
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return PublicDetail{}, fmt.Errorf("begin published torrent detail read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := torrentdb.New(tx)
	row, err := queries.GetPublishedTorrentDetail(ctx, int64(torrentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicDetail{}, ErrTorrentReadNotFound
	}
	if err != nil {
		return PublicDetail{}, fmt.Errorf("get published torrent detail: %w", err)
	}
	if row.ID != int64(torrentID) || row.State != string(StatePublished) ||
		row.CategoryID == "" || strings.TrimSpace(row.CategoryName) == "" ||
		strings.TrimSpace(row.Title) == "" || strings.TrimSpace(row.ContentName) == "" ||
		strings.TrimSpace(row.UploaderDisplayName) == "" || len(row.InfoHashV1) != 20 ||
		!catalog.Promotion(row.Promotion).Valid() ||
		row.TotalSizeBytes <= 0 || row.PayloadSizeBytes <= 0 || row.PayloadSizeBytes > row.TotalSizeBytes ||
		row.FileCount < 1 || row.FileCount > 100000 || row.PaddingFileCount < 0 || row.PaddingFileCount > row.FileCount ||
		row.ScreenshotCount < 0 || row.ScreenshotCount > MaxTorrentScreenshots ||
		row.PieceLengthBytes <= 0 || row.PieceCount <= 0 || !row.SubmittedAt.Valid || !row.PublishedAt.Valid ||
		row.PublishedAt.Time.Before(row.SubmittedAt.Time) {
		return PublicDetail{}, ErrTorrentReadInvariant
	}
	if row.Anonymous && row.UploaderDisplayName != "匿名" {
		return PublicDetail{}, ErrTorrentReadInvariant
	}

	facetRows, err := queries.ListPublishedTorrentFacetValues(ctx, row.ID)
	if err != nil {
		return PublicDetail{}, fmt.Errorf("list published torrent facets: %w", err)
	}
	if len(facetRows) > 50 {
		return PublicDetail{}, ErrTorrentReadInvariant
	}
	facets := make([]PublicFacet, 0, len(facetRows))
	seenFacets := make(map[string]struct{}, len(facetRows))
	for _, facet := range facetRows {
		key := facet.FacetID + "\x00" + facet.OptionKey
		if strings.TrimSpace(facet.FacetID) == "" || strings.TrimSpace(facet.FacetName) == "" ||
			strings.TrimSpace(facet.OptionKey) == "" || strings.TrimSpace(facet.OptionLabel) == "" {
			return PublicDetail{}, ErrTorrentReadInvariant
		}
		if _, duplicate := seenFacets[key]; duplicate {
			return PublicDetail{}, ErrTorrentReadInvariant
		}
		seenFacets[key] = struct{}{}
		facets = append(facets, PublicFacet{
			FacetID: facet.FacetID, FacetName: facet.FacetName,
			OptionKey: facet.OptionKey, OptionLabel: facet.OptionLabel,
		})
	}

	identifierRows, err := queries.ListPublishedTorrentExternalIdentifiers(ctx, row.ID)
	if err != nil {
		return PublicDetail{}, fmt.Errorf("list published torrent external identifiers: %w", err)
	}
	if len(identifierRows) > 5 {
		return PublicDetail{}, ErrTorrentReadInvariant
	}
	externalIdentifiers := make([]ExternalIdentifier, 0, len(identifierRows))
	seenProviders := make(map[string]struct{}, len(identifierRows))
	for _, identifier := range identifierRows {
		if !validExternalIdentifierProvider(identifier.Provider) || strings.TrimSpace(identifier.ExternalID) == "" {
			return PublicDetail{}, ErrTorrentReadInvariant
		}
		if _, duplicate := seenProviders[identifier.Provider]; duplicate {
			return PublicDetail{}, ErrTorrentReadInvariant
		}
		seenProviders[identifier.Provider] = struct{}{}
		externalIdentifiers = append(externalIdentifiers, ExternalIdentifier{
			Provider: identifier.Provider, ExternalID: identifier.ExternalID,
		})
	}
	if err := tx.Commit(ctx); err != nil {
		return PublicDetail{}, fmt.Errorf("commit published torrent detail read: %w", err)
	}
	var infoHash InfoHashV1
	copy(infoHash[:], row.InfoHashV1)
	var promotionEndsAt *time.Time
	if row.PromotionEndsAt.Valid {
		value := row.PromotionEndsAt.Time.UTC()
		promotionEndsAt = &value
	}
	var stickyUntil *time.Time
	if row.StickyUntil.Valid {
		value := row.StickyUntil.Time.UTC()
		stickyUntil = &value
	}
	return PublicDetail{
		ID:                  TorrentID(row.ID),
		Category:            catalog.Category{ID: row.CategoryID, Name: row.CategoryName},
		Title:               row.Title,
		Subtitle:            row.Subtitle,
		ContentName:         row.ContentName,
		UploaderDisplayName: row.UploaderDisplayName,
		Anonymous:           row.Anonymous,
		Promotion:           catalog.Promotion(row.Promotion),
		PromotionEndsAt:     promotionEndsAt,
		StickyUntil:         stickyUntil,
		Facets:              facets,
		ExternalIdentifiers: externalIdentifiers,
		InfoHashV1:          infoHash,
		TotalSizeBytes:      row.TotalSizeBytes,
		PayloadSizeBytes:    row.PayloadSizeBytes,
		FileCount:           int(row.FileCount),
		PaddingFileCount:    int(row.PaddingFileCount),
		ScreenshotCount:     int(row.ScreenshotCount),
		PieceLengthBytes:    row.PieceLengthBytes,
		PieceCount:          int(row.PieceCount),
		State:               StatePublished,
		SubmittedAt:         row.SubmittedAt.Time.UTC(),
		PublishedAt:         row.PublishedAt.Time.UTC(),
	}, nil
}

func (repository *PostgresTorrentReadRepository) PublishedScreenshotSource(ctx context.Context, torrentID TorrentID, position int) (PublicScreenshotSource, error) {
	if torrentID < 1 || position < 0 || position >= MaxTorrentScreenshots {
		return PublicScreenshotSource{}, ErrTorrentReadInput
	}
	object, err := repository.queries.GetPublishedTorrentScreenshotObject(ctx, torrentdb.GetPublishedTorrentScreenshotObjectParams{
		ScreenshotPosition: int16(position),
		TorrentID:          int64(torrentID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicScreenshotSource{}, ErrTorrentScreenshotNotFound
	}
	if err != nil {
		return PublicScreenshotSource{}, fmt.Errorf("get published torrent screenshot object: %w", err)
	}
	if object.TorrentID != int64(torrentID) || object.Position != int16(position) || object.ObjectID == uuid.Nil ||
		len(object.ContentSha256) != 32 || object.ByteLength < 1 || object.ByteLength > MaxStoredTorrentScreenshotBytes ||
		object.Width < 1 || object.Height < 1 || int64(object.Width)*int64(object.Height) > maxScreenshotPixels {
		return PublicScreenshotSource{}, ErrTorrentReadInvariant
	}
	if !supportedStoredScreenshotType(object.ContentType) {
		return PublicScreenshotSource{}, ErrTorrentReadInvariant
	}
	var digest ObjectSHA256
	copy(digest[:], object.ContentSha256)
	rows, err := repository.queries.ListPublishedTorrentScreenshotLocations(ctx, object.ObjectID)
	if err != nil {
		return PublicScreenshotSource{}, fmt.Errorf("list published torrent screenshot locations: %w", err)
	}
	locations, err := publicScreenshotLocations(object.ObjectID, rows)
	if err != nil {
		return PublicScreenshotSource{}, err
	}
	return PublicScreenshotSource{
		TorrentID: torrentID, Position: position, ObjectID: object.ObjectID,
		ContentType: object.ContentType, Width: int(object.Width), Height: int(object.Height),
		Descriptor: StoredObjectDescriptor{SHA256: digest, ByteLength: object.ByteLength},
		Locations:  locations,
	}, nil
}

func publicScreenshotLocations(objectID uuid.UUID, rows []torrentdb.ListPublishedTorrentScreenshotLocationsRow) ([]ReadableObjectLocation, error) {
	locations := make([]ReadableObjectLocation, 0, len(rows))
	for _, row := range rows {
		backendID, backendErr := ParseStorageBackendID(row.BackendID)
		objectKey, keyErr := ParseObjectKey(row.ObjectKey)
		if backendErr != nil || keyErr != nil || row.ID == uuid.Nil || row.ObjectID != objectID ||
			row.ObservedByteLength < 1 || len(row.ObservedSha256) != 32 || !row.VerifiedAt.Valid ||
			(row.State != string(StorageLocationVerified) && row.State != string(StorageLocationRetiring)) {
			return nil, ErrTorrentReadInvariant
		}
		var observedDigest ObjectSHA256
		copy(observedDigest[:], row.ObservedSha256)
		locations = append(locations, ReadableObjectLocation{
			ID: row.ID, BackendID: backendID, ObjectKey: objectKey,
			Preferred:  row.IsPreferred,
			VersionID:  nullableStorageString(row.VersionID),
			Descriptor: StoredObjectDescriptor{SHA256: observedDigest, ByteLength: row.ObservedByteLength},
			VerifiedAt: row.VerifiedAt.Time.UTC(),
		})
	}
	return locations, nil
}

func (repository *PostgresTorrentReadRepository) PublishedCoverSource(ctx context.Context, torrentID TorrentID) (PublicCoverSource, error) {
	if torrentID < 1 {
		return PublicCoverSource{}, ErrTorrentReadInput
	}
	object, err := repository.queries.GetPublishedTorrentCoverObject(ctx, int64(torrentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicCoverSource{}, ErrTorrentCoverNotFound
	}
	if err != nil {
		return PublicCoverSource{}, fmt.Errorf("get published torrent cover object: %w", err)
	}
	if object.TorrentID != int64(torrentID) || object.ObjectID == uuid.Nil || len(object.ContentSha256) != 32 ||
		object.ByteLength < 1 || object.ByteLength > MaxStoredTorrentScreenshotBytes ||
		object.Width < 1 || object.Height < 1 ||
		int64(object.Width)*int64(object.Height) > maxScreenshotPixels {
		return PublicCoverSource{}, ErrTorrentReadInvariant
	}
	if !supportedStoredScreenshotType(object.ContentType) {
		return PublicCoverSource{}, ErrTorrentReadInvariant
	}
	var digest ObjectSHA256
	copy(digest[:], object.ContentSha256)
	rows, err := repository.queries.ListPublishedTorrentCoverLocations(ctx, object.ObjectID)
	if err != nil {
		return PublicCoverSource{}, fmt.Errorf("list published torrent cover locations: %w", err)
	}
	locations := make([]ReadableObjectLocation, 0, len(rows))
	for _, row := range rows {
		backendID, backendErr := ParseStorageBackendID(row.BackendID)
		objectKey, keyErr := ParseObjectKey(row.ObjectKey)
		if backendErr != nil || keyErr != nil || row.ID == uuid.Nil || row.ObjectID != object.ObjectID ||
			row.ObservedByteLength < 1 || len(row.ObservedSha256) != 32 || !row.VerifiedAt.Valid ||
			(row.State != string(StorageLocationVerified) && row.State != string(StorageLocationRetiring)) {
			return PublicCoverSource{}, ErrTorrentReadInvariant
		}
		var observedDigest ObjectSHA256
		copy(observedDigest[:], row.ObservedSha256)
		locations = append(locations, ReadableObjectLocation{
			ID: row.ID, BackendID: backendID, ObjectKey: objectKey,
			Preferred:  row.IsPreferred,
			VersionID:  nullableStorageString(row.VersionID),
			Descriptor: StoredObjectDescriptor{SHA256: observedDigest, ByteLength: row.ObservedByteLength},
			VerifiedAt: row.VerifiedAt.Time.UTC(),
		})
	}
	return PublicCoverSource{
		TorrentID:   torrentID,
		ObjectID:    object.ObjectID,
		ContentType: object.ContentType,
		Width:       int(object.Width), Height: int(object.Height),
		Descriptor: StoredObjectDescriptor{SHA256: digest, ByteLength: object.ByteLength},
		Locations:  locations,
	}, nil
}

func (repository *PostgresTorrentReadRepository) PublishedContent(ctx context.Context, torrentID TorrentID) (PublicContent, error) {
	if torrentID < 1 {
		return PublicContent{}, ErrTorrentReadInput
	}
	row, err := repository.queries.GetPublishedTorrentContent(ctx, int64(torrentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicContent{}, ErrTorrentReadNotFound
	}
	if err != nil {
		return PublicContent{}, fmt.Errorf("get published torrent content: %w", err)
	}
	if row.TorrentID != int64(torrentID) ||
		(row.DescriptionFormat != "markdown" && row.DescriptionFormat != "plain_text") ||
		len(row.Description) > 4*1024*1024 || len(row.MediaInfo) > 16*1024*1024 {
		return PublicContent{}, ErrTorrentReadInvariant
	}
	return PublicContent{
		TorrentID: TorrentID(row.TorrentID), Description: row.Description,
		DescriptionFormat: row.DescriptionFormat, MediaInfo: row.MediaInfo,
	}, nil
}

func (repository *PostgresTorrentReadRepository) PublishedRelatedVersions(ctx context.Context, torrentID TorrentID, limit int) ([]catalog.Torrent, error) {
	if torrentID < 1 || limit < 1 || limit > MaxRelatedTorrentVersions {
		return nil, ErrTorrentReadInput
	}
	rows, err := repository.queries.ListPublishedRelatedTorrentVersions(ctx, torrentdb.ListPublishedRelatedTorrentVersionsParams{
		TorrentID:   int64(torrentID),
		ResultLimit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list published related torrent versions: %w", err)
	}
	items := make([]catalog.Torrent, 0, len(rows))
	for _, row := range rows {
		if !row.PublishedAt.Valid || !row.ObservedAt.Valid {
			return nil, ErrTorrentReadInvariant
		}
		item, conversionErr := catalog.TorrentFromProjection(
			row.ID, row.Name, row.Subtitle, row.CategoryID, row.CategoryName,
			row.SizeBytes, row.Promotion, row.StickyUntil.Time, row.PublishedAt.Time,
			row.Seeders, row.Leechers, row.Completed, row.ObservedAt.Time,
		)
		if conversionErr != nil || item.ID == int64(torrentID) {
			return nil, ErrTorrentReadInvariant
		}
		items = append(items, item)
	}
	return items, nil
}

func validExternalIdentifierProvider(provider string) bool {
	switch provider {
	case "imdb", "tmdb", "douban", "bangumi", "steam":
		return true
	default:
		return false
	}
}

func (repository *PostgresTorrentReadRepository) PublishedFiles(ctx context.Context, torrentID TorrentID, limit, offset int) (PublicFilePage, error) {
	if torrentID < 1 || limit < 1 || limit > MaxTorrentFileLimit || offset < 0 || offset > 99999 {
		return PublicFilePage{}, ErrTorrentReadInput
	}

	// Publication state may be revoked while a page is being assembled. A
	// repeatable-read snapshot guarantees that the visibility check and immutable
	// file rows describe one state instead of leaking files after a disable.
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return PublicFilePage{}, fmt.Errorf("begin published torrent file read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := torrentdb.New(tx)
	target, err := queries.GetPublishedTorrentFileTarget(ctx, int64(torrentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicFilePage{}, ErrTorrentReadNotFound
	}
	if err != nil {
		return PublicFilePage{}, fmt.Errorf("get published torrent file target: %w", err)
	}
	if target.ID != int64(torrentID) || target.FileCount < 1 || target.FileCount > 100000 {
		return PublicFilePage{}, ErrTorrentReadInvariant
	}
	rows, err := queries.ListPublishedTorrentFiles(ctx, torrentdb.ListPublishedTorrentFilesParams{
		TorrentID:    target.ID,
		ResultLimit:  int32(limit),
		ResultOffset: int32(offset),
	})
	if err != nil {
		return PublicFilePage{}, fmt.Errorf("list published torrent files: %w", err)
	}
	wantCount := 0
	if offset < int(target.FileCount) {
		wantCount = min(limit, int(target.FileCount)-offset)
	}
	if len(rows) != wantCount {
		return PublicFilePage{}, ErrTorrentReadInvariant
	}
	items := make([]PublicFile, 0, len(rows))
	for index, row := range rows {
		if row.FileIndex != int32(offset+index) || strings.TrimSpace(row.DisplayPath) == "" || row.SizeBytes < 0 {
			return PublicFilePage{}, ErrTorrentReadInvariant
		}
		items = append(items, PublicFile{
			Index:       int(row.FileIndex),
			DisplayPath: row.DisplayPath,
			SizeBytes:   row.SizeBytes,
			IsPadding:   row.IsPadding,
		})
	}
	if err := tx.Commit(ctx); err != nil {
		return PublicFilePage{}, fmt.Errorf("commit published torrent file read: %w", err)
	}
	return PublicFilePage{
		TorrentID: torrentID,
		Items:     items,
		Total:     int(target.FileCount),
		Limit:     limit,
		Offset:    offset,
	}, nil
}

func (repository *PostgresTorrentReadRepository) UserSubmissions(ctx context.Context, uploaderID uuid.UUID, limit int) (MySubmissionPage, error) {
	if uploaderID == uuid.Nil || limit < 1 || limit > MaxMyTorrentSubmissionLimit {
		return MySubmissionPage{}, ErrTorrentReadInput
	}
	rows, err := repository.queries.ListUserTorrentSubmissions(ctx, torrentdb.ListUserTorrentSubmissionsParams{
		UploaderID: uploaderID, ResultLimit: int32(limit),
	})
	if err != nil {
		return MySubmissionPage{}, fmt.Errorf("list user torrent submissions: %w", err)
	}
	page := MySubmissionPage{Items: make([]MySubmission, 0, len(rows)), Limit: limit}
	for _, row := range rows {
		item, conversionErr := mySubmissionFromRow(row)
		if conversionErr != nil {
			return MySubmissionPage{}, conversionErr
		}
		if page.Total != 0 && page.Total != row.TotalCount {
			return MySubmissionPage{}, ErrTorrentReadInvariant
		}
		page.Items = append(page.Items, item)
		page.Total = row.TotalCount
	}
	if page.Total < int64(len(page.Items)) {
		return MySubmissionPage{}, ErrTorrentReadInvariant
	}
	return page, nil
}

func mySubmissionFromRow(row torrentdb.ListUserTorrentSubmissionsRow) (MySubmission, error) {
	state := State(row.State)
	if row.ID < 1 || row.CategoryID == "" || strings.TrimSpace(row.CategoryName) == "" ||
		strings.TrimSpace(row.Title) == "" || strings.TrimSpace(row.ContentName) == "" || len(row.InfoHashV1) != 20 ||
		row.TotalSizeBytes <= 0 || row.FileCount < 1 || row.FileCount > 100000 || !validReadState(state) ||
		row.Version < 1 || !row.SubmittedAt.Valid || !row.StateChangedAt.Valid ||
		row.StateChangedAt.Time.Before(row.SubmittedAt.Time) || row.TotalCount < 1 {
		return MySubmission{}, ErrTorrentReadInvariant
	}
	var publishedAt *time.Time
	if row.PublishedAt.Valid {
		value := row.PublishedAt.Time.UTC()
		if value.Before(row.SubmittedAt.Time) {
			return MySubmission{}, ErrTorrentReadInvariant
		}
		publishedAt = &value
	}
	if (state == StatePublished || state == StateDisabled) && publishedAt == nil {
		return MySubmission{}, ErrTorrentReadInvariant
	}
	if (state == StatePendingReview || state == StateRejected) && publishedAt != nil {
		return MySubmission{}, ErrTorrentReadInvariant
	}

	latestReview, err := uploaderReviewFeedback(row)
	if err != nil {
		return MySubmission{}, err
	}
	contentChange, err := uploaderContentChangeFeedback(row)
	if err != nil {
		return MySubmission{}, err
	}
	screenshotChange, err := uploaderScreenshotChangeFeedback(row)
	if err != nil {
		return MySubmission{}, err
	}
	withdrawal, err := uploaderWithdrawalFeedback(row)
	if err != nil {
		return MySubmission{}, err
	}
	var infoHash InfoHashV1
	copy(infoHash[:], row.InfoHashV1)
	return MySubmission{
		ID:               TorrentID(row.ID),
		Category:         catalog.Category{ID: row.CategoryID, Name: row.CategoryName},
		Title:            row.Title,
		Subtitle:         row.Subtitle,
		ContentName:      row.ContentName,
		InfoHashV1:       infoHash,
		TotalSizeBytes:   row.TotalSizeBytes,
		FileCount:        int(row.FileCount),
		State:            state,
		Version:          row.Version,
		SubmittedAt:      row.SubmittedAt.Time.UTC(),
		PublishedAt:      publishedAt,
		StateChangedAt:   row.StateChangedAt.Time.UTC(),
		LatestReview:     latestReview,
		ContentChange:    contentChange,
		ScreenshotChange: screenshotChange,
		Withdrawal:       withdrawal,
	}, nil
}

func uploaderScreenshotChangeFeedback(row torrentdb.ListUserTorrentSubmissionsRow) (*ScreenshotChangeFeedback, error) {
	if row.ScreenshotChangeStatus == "" {
		if row.ScreenshotChangeCreatedAt.Valid || row.ScreenshotChangeDecidedAt.Valid || row.ScreenshotChangeDecisionReason != "" {
			return nil, ErrTorrentReadInvariant
		}
		return nil, nil
	}
	if !row.ScreenshotChangeCreatedAt.Valid {
		return nil, ErrTorrentReadInvariant
	}
	feedback := &ScreenshotChangeFeedback{
		Status: PublishedScreenshotChangeStatus(row.ScreenshotChangeStatus), SubmittedAt: row.ScreenshotChangeCreatedAt.Time.UTC(),
	}
	switch feedback.Status {
	case PublishedScreenshotChangePending:
		if row.ScreenshotChangeDecidedAt.Valid || row.ScreenshotChangeDecisionReason != "" {
			return nil, ErrTorrentReadInvariant
		}
	case PublishedScreenshotChangeApproved, PublishedScreenshotChangeRejected:
		if !row.ScreenshotChangeDecidedAt.Valid || strings.TrimSpace(row.ScreenshotChangeDecisionReason) == "" ||
			row.ScreenshotChangeDecidedAt.Time.Before(row.ScreenshotChangeCreatedAt.Time) {
			return nil, ErrTorrentReadInvariant
		}
		decidedAt, reason := row.ScreenshotChangeDecidedAt.Time.UTC(), row.ScreenshotChangeDecisionReason
		feedback.DecidedAt, feedback.DecisionReason = &decidedAt, &reason
	default:
		return nil, ErrTorrentReadInvariant
	}
	return feedback, nil
}

func uploaderReviewFeedback(row torrentdb.ListUserTorrentSubmissionsRow) (*ReviewFeedback, error) {
	if !row.ReviewOccurredAt.Valid {
		if row.ReviewOutcome != "" || row.ReviewReasonCode != "" || row.ReviewReason != "" {
			return nil, ErrTorrentReadInvariant
		}
		return nil, nil
	}
	outcome := State(row.ReviewOutcome)
	if (outcome != StatePublished && outcome != StateRejected) || row.ReviewReasonCode == "" ||
		strings.TrimSpace(row.ReviewReason) == "" {
		return nil, ErrTorrentReadInvariant
	}
	return &ReviewFeedback{
		Outcome:    outcome,
		ReasonCode: row.ReviewReasonCode,
		Reason:     row.ReviewReason,
		DecidedAt:  row.ReviewOccurredAt.Time.UTC(),
	}, nil
}

func uploaderContentChangeFeedback(row torrentdb.ListUserTorrentSubmissionsRow) (*ContentChangeFeedback, error) {
	if row.ContentChangeStatus == "" {
		if row.ContentChangeCreatedAt.Valid || row.ContentChangeDecidedAt.Valid || row.ContentChangeDecisionReason != "" {
			return nil, ErrTorrentReadInvariant
		}
		return nil, nil
	}
	if !row.ContentChangeCreatedAt.Valid {
		return nil, ErrTorrentReadInvariant
	}
	feedback := &ContentChangeFeedback{
		Status:      PublishedContentChangeStatus(row.ContentChangeStatus),
		SubmittedAt: row.ContentChangeCreatedAt.Time.UTC(),
	}
	switch feedback.Status {
	case PublishedContentChangePending:
		if row.ContentChangeDecidedAt.Valid || row.ContentChangeDecisionReason != "" {
			return nil, ErrTorrentReadInvariant
		}
	case PublishedContentChangeApproved, PublishedContentChangeRejected:
		if !row.ContentChangeDecidedAt.Valid || strings.TrimSpace(row.ContentChangeDecisionReason) == "" ||
			row.ContentChangeDecidedAt.Time.Before(row.ContentChangeCreatedAt.Time) {
			return nil, ErrTorrentReadInvariant
		}
		decidedAt := row.ContentChangeDecidedAt.Time.UTC()
		decisionReason := row.ContentChangeDecisionReason
		feedback.DecidedAt = &decidedAt
		feedback.DecisionReason = &decisionReason
	default:
		return nil, ErrTorrentReadInvariant
	}
	return feedback, nil
}

func uploaderWithdrawalFeedback(row torrentdb.ListUserTorrentSubmissionsRow) (*WithdrawalFeedback, error) {
	if row.WithdrawalStatus == "" {
		if row.WithdrawalCreatedAt.Valid || row.WithdrawalDecidedAt.Valid || row.WithdrawalDecisionReason != "" {
			return nil, ErrTorrentReadInvariant
		}
		return nil, nil
	}
	if !row.WithdrawalCreatedAt.Valid {
		return nil, ErrTorrentReadInvariant
	}
	feedback := &WithdrawalFeedback{
		Status: TorrentWithdrawalStatus(row.WithdrawalStatus), SubmittedAt: row.WithdrawalCreatedAt.Time.UTC(),
	}
	switch feedback.Status {
	case TorrentWithdrawalPending:
		if row.WithdrawalDecidedAt.Valid || row.WithdrawalDecisionReason != "" {
			return nil, ErrTorrentReadInvariant
		}
	case TorrentWithdrawalApproved, TorrentWithdrawalRejected:
		if !row.WithdrawalDecidedAt.Valid || strings.TrimSpace(row.WithdrawalDecisionReason) == "" ||
			row.WithdrawalDecidedAt.Time.Before(row.WithdrawalCreatedAt.Time) {
			return nil, ErrTorrentReadInvariant
		}
		decidedAt := row.WithdrawalDecidedAt.Time.UTC()
		decisionReason := row.WithdrawalDecisionReason
		feedback.DecidedAt = &decidedAt
		feedback.DecisionReason = &decisionReason
	default:
		return nil, ErrTorrentReadInvariant
	}
	return feedback, nil
}

func validReadState(state State) bool {
	switch state {
	case StatePendingReview, StatePublished, StateRejected, StateDisabled, StateDeleted:
		return true
	default:
		return false
	}
}

var _ TorrentReadRepository = (*PostgresTorrentReadRepository)(nil)
