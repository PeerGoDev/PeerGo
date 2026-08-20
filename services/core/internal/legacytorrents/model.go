package legacytorrents

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

const (
	torrentFingerprintDomain = "peergo:migration:ptyes-torrent:v1\x00"
	fileManifestDomain       = "peergo:migration:ptyes-torrent-files:v1\x00"
	maxDescriptionBytes      = 4 << 20
	maxMediaInfoBytes        = 16 << 20
)

var errInvalidSourceTorrent = errors.New("PtYes source torrent is invalid")

type sourceTorrent struct {
	LegacyID          int64
	LegacyUUID        string
	InfoHash          string
	Title             string
	Subtitle          string
	Description       string
	SourceCategory    string
	Attributes        string
	SizeBytes         int64
	UploaderLegacyID  int64
	Anonymous         bool
	Status            string
	PromotionType     int
	PromotionTimeType int
	PromotionUntil    *time.Time
	GroupLegacyID     *int64
	GroupExternalIDs  string
	MediaInfo         string
	Poster            string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
}

func (source sourceTorrent) publicID() (uuid.UUID, error) {
	parsed, err := uuid.Parse(source.LegacyUUID)
	if err != nil || parsed == uuid.Nil || parsed.String() != strings.ToLower(source.LegacyUUID) {
		return uuid.Nil, errInvalidSourceTorrent
	}
	return parsed, nil
}

func (source sourceTorrent) parsedInfoHash() (torrents.InfoHashV1, error) {
	return torrents.ParseInfoHashV1Hex(source.InfoHash)
}

func (source sourceTorrent) categoryID() (string, error) {
	category, exists := categoryMap[source.SourceCategory]
	if !exists {
		return "", errInvalidSourceTorrent
	}
	return category, nil
}

func (source sourceTorrent) title() string {
	return strings.TrimSpace(source.Title)
}

func (source sourceTorrent) subtitle() string {
	return strings.TrimSpace(source.Subtitle)
}

func (source sourceTorrent) state() (torrents.State, error) {
	return torrents.MapPtYesState(source.Status, source.DeletedAt != nil)
}

func (source sourceTorrent) publishedAt() *time.Time {
	if strings.EqualFold(strings.TrimSpace(source.Status), "approved") || strings.TrimSpace(source.Status) == "" {
		value := source.CreatedAt
		return &value
	}
	return nil
}

func (source sourceTorrent) stateChangedAt() time.Time {
	if source.DeletedAt != nil && source.DeletedAt.After(source.UpdatedAt) {
		return *source.DeletedAt
	}
	return source.UpdatedAt
}

func (source sourceTorrent) targetUpdatedAt() time.Time {
	changedAt := source.stateChangedAt()
	if changedAt.After(source.UpdatedAt) {
		return changedAt
	}
	return source.UpdatedAt
}

func (source sourceTorrent) validatePromotion() error {
	if source.PromotionType < 1 || source.PromotionType > 7 ||
		source.PromotionTimeType < 0 || source.PromotionTimeType > 2 ||
		(source.PromotionTimeType == 2 && source.PromotionUntil == nil) {
		return errInvalidSourceTorrent
	}
	return nil
}

// catalogPromotion maps only the independently assigned promotion that is
// effective at cutover. It is a public display projection; Tracker settlement
// continues to use its immutable policy timeline and never reads this value.
func (source sourceTorrent) catalogPromotion(cutover time.Time) (catalog.Promotion, *time.Time, error) {
	if cutover.IsZero() || source.validatePromotion() != nil {
		return "", nil, errInvalidSourceTorrent
	}
	if source.PromotionTimeType == 0 || source.PromotionType == 1 {
		return catalog.PromotionNone, nil, nil
	}

	promotions := map[int]catalog.Promotion{
		2: catalog.PromotionFree,
		3: catalog.PromotionDoubleUpload,
		4: catalog.PromotionDoubleUploadFree,
		5: catalog.PromotionHalfDownload,
		6: catalog.PromotionDoubleUploadHalfDownload,
		7: catalog.PromotionThirtyPercentDownload,
	}
	promotion := promotions[source.PromotionType]
	if source.PromotionTimeType == 1 {
		return promotion, nil, nil
	}
	if !source.PromotionUntil.After(cutover) {
		return catalog.PromotionNone, nil, nil
	}
	endsAt := source.PromotionUntil.UTC()
	return promotion, &endsAt, nil
}

func (source sourceTorrent) validate() error {
	publicID, publicIDErr := source.publicID()
	_, hashErr := source.parsedInfoHash()
	_, categoryErr := source.categoryID()
	state, stateErr := source.state()
	titleRunes := utf8.RuneCountInString(source.title())
	subtitleRunes := utf8.RuneCountInString(source.subtitle())
	if source.LegacyID < 1 || publicIDErr != nil || publicID == uuid.Nil || hashErr != nil || categoryErr != nil || stateErr != nil ||
		source.UploaderLegacyID < 1 || source.SizeBytes < 1 ||
		!utf8.ValidString(source.Title) || titleRunes < 1 || titleRunes > 240 ||
		!utf8.ValidString(source.Subtitle) || subtitleRunes > 300 ||
		!utf8.ValidString(source.Description) || len(source.Description) > maxDescriptionBytes ||
		!utf8.ValidString(source.MediaInfo) || len(source.MediaInfo) > maxMediaInfoBytes ||
		!utf8.ValidString(source.Poster) ||
		source.CreatedAt.IsZero() || source.UpdatedAt.Before(source.CreatedAt) ||
		(source.DeletedAt != nil && source.DeletedAt.Before(source.CreatedAt)) ||
		(source.GroupLegacyID != nil && *source.GroupLegacyID < 1) || source.validatePromotion() != nil ||
		!json.Valid([]byte(source.GroupExternalIDs)) || state == "" {
		return errInvalidSourceTorrent
	}
	var groupValues map[string]string
	if err := json.Unmarshal([]byte(source.GroupExternalIDs), &groupValues); err != nil {
		return errInvalidSourceTorrent
	}
	return nil
}

func (source sourceTorrent) fingerprint(files sourceFileManifest) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	if source.validate() != nil || files.TorrentID != source.LegacyID || files.validate() != nil {
		return result, errInvalidSourceTorrent
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(torrentFingerprintDomain))
	writeInt64(digest, source.LegacyID)
	for _, value := range []string{
		source.LegacyUUID,
		source.InfoHash,
		source.Title,
		source.Subtitle,
		source.Description,
		source.SourceCategory,
		source.Attributes,
		source.Status,
		source.GroupExternalIDs,
		source.MediaInfo,
		source.Poster,
		source.CreatedAt.UTC().Format(time.RFC3339Nano),
		source.UpdatedAt.UTC().Format(time.RFC3339Nano),
	} {
		writeString(digest, value)
	}
	writeInt64(digest, source.SizeBytes)
	writeInt64(digest, source.UploaderLegacyID)
	writeBool(digest, source.Anonymous)
	writeInt64(digest, int64(source.PromotionType))
	writeInt64(digest, int64(source.PromotionTimeType))
	writeOptionalTime(digest, source.PromotionUntil)
	writeOptionalInt64(digest, source.GroupLegacyID)
	writeOptionalTime(digest, source.DeletedAt)
	_, _ = digest.Write(files.Digest[:])
	copy(result[:], digest.Sum(nil))
	return result, nil
}

type sourceFile struct {
	LegacyID int64
	Path     string
	Size     int64
}

type sourceFileManifest struct {
	TorrentID int64
	Files     []sourceFile
	TotalSize int64
	Digest    [sha256.Size]byte
}

func newSourceFileManifest(torrentID int64, files []sourceFile) (sourceFileManifest, error) {
	manifest := sourceFileManifest{TorrentID: torrentID, Files: append([]sourceFile(nil), files...)}
	digest := sha256.New()
	_, _ = digest.Write([]byte(fileManifestDomain))
	writeInt64(digest, torrentID)
	var previousID int64
	for _, file := range manifest.Files {
		if file.LegacyID < 1 || file.LegacyID <= previousID || file.Size < 0 || file.Path == "" ||
			!utf8.ValidString(file.Path) || manifest.TotalSize > math.MaxInt64-file.Size {
			return sourceFileManifest{}, errInvalidSourceTorrent
		}
		previousID = file.LegacyID
		manifest.TotalSize += file.Size
		writeInt64(digest, file.LegacyID)
		writeString(digest, file.Path)
		writeInt64(digest, file.Size)
	}
	copy(manifest.Digest[:], digest.Sum(nil))
	return manifest, nil
}

func (manifest sourceFileManifest) validate() error {
	rebuilt, err := newSourceFileManifest(manifest.TorrentID, manifest.Files)
	if err != nil || rebuilt.TotalSize != manifest.TotalSize || rebuilt.Digest != manifest.Digest {
		return errInvalidSourceTorrent
	}
	return nil
}

type sourceTorrentValidationError struct {
	legacyID int64
	code     string
}

func (problem *sourceTorrentValidationError) Error() string {
	return fmt.Sprintf("legacy torrent %d: %s: %s", problem.legacyID, problem.code, errInvalidSourceTorrent)
}

func (problem *sourceTorrentValidationError) Unwrap() error {
	return errInvalidSourceTorrent
}

func sourceTorrentError(legacyID int64, code string) error {
	return &sourceTorrentValidationError{legacyID: legacyID, code: code}
}

func sourceTorrentValidationCode(err error) (string, bool) {
	var problem *sourceTorrentValidationError
	if !errors.As(err, &problem) || problem.code == "" {
		return "", false
	}
	return problem.code, true
}

func writeInt64(writer hash.Hash, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = writer.Write(encoded[:])
}

func writeString(writer hash.Hash, value string) {
	writeInt64(writer, int64(len(value)))
	_, _ = writer.Write([]byte(value))
}

func writeBool(writer hash.Hash, value bool) {
	if value {
		_, _ = writer.Write([]byte{1})
		return
	}
	_, _ = writer.Write([]byte{0})
}

func writeOptionalInt64(writer hash.Hash, value *int64) {
	writeBool(writer, value != nil)
	if value != nil {
		writeInt64(writer, *value)
	}
}

func writeOptionalTime(writer hash.Hash, value *time.Time) {
	writeBool(writer, value != nil)
	if value != nil {
		writeString(writer, value.UTC().Format(time.RFC3339Nano))
	}
}
