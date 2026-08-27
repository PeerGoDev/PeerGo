package torrents

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewPendingTorrentDerivesImmutableIdentityFromParser(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	metainfo := mustParseV1(t, validSingleFixture("release.bin", 42, 16*1024), ValidationProfileStrictUpload)
	torrent, err := NewPendingTorrent(NewPendingTorrentInput{
		UploaderID: uuid.MustParse("0198fc78-d000-7b21-a222-222222222222"),
		CategoryID: " software ",
		Title:      " Release 2026 ",
		Subtitle:   " First edition ",
		ObjectID:   uuid.MustParse("0198fc78-d000-7b21-a333-333333333333"),
		Metainfo:   metainfo,
		OccurredAt: now,
	})
	if err != nil {
		t.Fatalf("NewPendingTorrent() error = %v", err)
	}
	if torrent.State != StatePendingReview || torrent.Version != 1 || torrent.TrackerEligible() {
		t.Fatalf("new torrent state = %+v", torrent)
	}
	if torrent.CategoryID != "software" || torrent.Title != "Release 2026" || torrent.Subtitle != "First edition" {
		t.Fatalf("normalized metadata = %+v", torrent)
	}
	if torrent.InfoHashV1 != metainfo.InfoHashV1 || torrent.Object.ContentSHA256 != metainfo.ObjectSHA256 ||
		torrent.Object.InfoOffset != metainfo.InfoOffset || torrent.Object.InfoLength != metainfo.InfoLength {
		t.Fatalf("derived immutable identity = %+v", torrent.Object)
	}
	if torrent.TotalSizeBytes != 42 || torrent.PayloadSizeBytes != 42 || torrent.FileCount != 1 {
		t.Fatalf("content summary = %+v", torrent)
	}
}

func TestNewPendingTorrentRejectsUntrustedObjectMetadata(t *testing.T) {
	t.Parallel()

	metainfo := mustParseV1(t, validSingleFixture("release.bin", 1, 16*1024), ValidationProfileStrictUpload)
	metainfo.InfoLength = metainfo.ObjectByteLength + 1
	_, err := NewPendingTorrent(NewPendingTorrentInput{
		UploaderID: uuid.New(),
		CategoryID: "software",
		Title:      "Release",
		ObjectID:   uuid.New(),
		Metainfo:   metainfo,
		OccurredAt: time.Now(),
	})
	if !errors.Is(err, ErrTorrentInputInvalid) {
		t.Fatalf("NewPendingTorrent() error = %v, want ErrTorrentInputInvalid", err)
	}
}

func TestNewPendingTorrentNormalizesUserContentAndExternalIdentifiers(t *testing.T) {
	t.Parallel()

	metainfo := mustParseV1(t, validSingleFixture("release.bin", 1, 16*1024), ValidationProfileStrictUpload)
	torrent, err := NewPendingTorrent(NewPendingTorrentInput{
		UploaderID: uuid.New(), CategoryID: "movies", Title: "Release",
		Description: "# Description\n", MediaInfo: "General\nComplete name: release.mkv", Anonymous: true, PurchasePrice: 88,
		ExternalIdentifiers: []ExternalIdentifier{
			{Provider: " TMDB ", ExternalID: " 12345 "},
			{Provider: "IMDB", ExternalID: "tt1234567"},
		},
		FacetSelections: []FacetSelection{
			{FacetID: "resolution", OptionKeys: []string{"1080p"}},
			{FacetID: "genre", OptionKeys: []string{"drama", "action"}},
		},
		ObjectID: uuid.New(), Metainfo: metainfo, OccurredAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("NewPendingTorrent() error = %v", err)
	}
	if torrent.DescriptionFormat != DescriptionFormatMarkdown || !torrent.Anonymous || torrent.PurchasePrice != 88 ||
		torrent.Description != "# Description\n" || torrent.MediaInfo == "" {
		t.Fatalf("user content = %+v", torrent)
	}
	want := []ExternalIdentifier{{Provider: "imdb", ExternalID: "tt1234567"}, {Provider: "tmdb", ExternalID: "12345"}}
	if !reflect.DeepEqual(torrent.ExternalIdentifiers, want) {
		t.Fatalf("external identifiers = %+v, want %+v", torrent.ExternalIdentifiers, want)
	}
	wantFacets := []FacetSelection{
		{FacetID: "genre", OptionKeys: []string{"action", "drama"}},
		{FacetID: "resolution", OptionKeys: []string{"1080p"}},
	}
	if !reflect.DeepEqual(torrent.FacetSelections, wantFacets) {
		t.Fatalf("facet selections = %+v, want %+v", torrent.FacetSelections, wantFacets)
	}

	_, err = NewPendingTorrent(NewPendingTorrentInput{
		UploaderID: uuid.New(), CategoryID: "movies", Title: "Release",
		Description: strings.Repeat("x", maxTorrentDescriptionBytes+1),
		ObjectID:    uuid.New(), Metainfo: metainfo, OccurredAt: time.Now(),
	})
	if !errors.Is(err, ErrTorrentInputInvalid) {
		t.Fatalf("oversized description error = %v, want ErrTorrentInputInvalid", err)
	}
	invalidPrice := NewPendingTorrentInput{
		UploaderID: uuid.New(), CategoryID: "movies", Title: "Release", PurchasePrice: 1_000_001,
		ObjectID: uuid.New(), Metainfo: metainfo, OccurredAt: time.Now(),
	}
	if _, err := NewPendingTorrent(invalidPrice); !errors.Is(err, ErrTorrentInputInvalid) {
		t.Fatalf("invalid purchase price error = %v, want ErrTorrentInputInvalid", err)
	}
}

func TestNewPendingTorrentRejectsInvalidFacetSelections(t *testing.T) {
	t.Parallel()
	metainfo := mustParseV1(t, validSingleFixture("release.bin", 1, 16*1024), ValidationProfileStrictUpload)
	base := NewPendingTorrentInput{
		UploaderID: uuid.New(), CategoryID: "movies", Title: "Release",
		ObjectID: uuid.New(), Metainfo: metainfo, OccurredAt: time.Now(),
	}
	for name, selections := range map[string][]FacetSelection{
		"duplicate facet": {
			{FacetID: "genre", OptionKeys: []string{"drama"}},
			{FacetID: "genre", OptionKeys: []string{"action"}},
		},
		"duplicate option": {{FacetID: "genre", OptionKeys: []string{"drama", "drama"}}},
		"missing option":   {{FacetID: "genre"}},
		"invalid facet":    {{FacetID: "Genre", OptionKeys: []string{"drama"}}},
	} {
		t.Run(name, func(t *testing.T) {
			input := base
			input.FacetSelections = selections
			if _, err := NewPendingTorrent(input); !errors.Is(err, ErrTorrentInputInvalid) {
				t.Fatalf("NewPendingTorrent() error = %v, want ErrTorrentInputInvalid", err)
			}
		})
	}
}

func TestNewPendingTorrentRejectsDuplicateOrInvalidExternalIdentifiers(t *testing.T) {
	t.Parallel()

	metainfo := mustParseV1(t, validSingleFixture("release.bin", 1, 16*1024), ValidationProfileStrictUpload)
	base := NewPendingTorrentInput{
		UploaderID: uuid.New(), CategoryID: "movies", Title: "Release",
		ObjectID: uuid.New(), Metainfo: metainfo, OccurredAt: time.Now(),
	}
	for name, identifiers := range map[string][]ExternalIdentifier{
		"duplicate provider": {{Provider: "imdb", ExternalID: "tt1234567"}, {Provider: "imdb", ExternalID: "tt7654321"}},
		"invalid imdb":       {{Provider: "imdb", ExternalID: "12345"}},
		"unknown provider":   {{Provider: "other", ExternalID: "12345"}},
	} {
		t.Run(name, func(t *testing.T) {
			input := base
			input.ExternalIdentifiers = identifiers
			if _, err := NewPendingTorrent(input); !errors.Is(err, ErrTorrentInputInvalid) {
				t.Fatalf("NewPendingTorrent() error = %v, want ErrTorrentInputInvalid", err)
			}
		})
	}
}

func TestNewPendingTorrentRejectsQuarantinedPublicLegacyObject(t *testing.T) {
	t.Parallel()

	raw := testDictionary(map[string][]byte{
		"info": testDictionary(map[string][]byte{
			"length":       testInteger(1),
			"name":         testBytes([]byte("legacy-public.bin")),
			"piece length": testInteger(16 * 1024),
			"pieces":       testBytes(make([]byte, 20)),
		}),
	})
	metainfo, err := InspectLegacyV1(raw)
	if err != nil {
		t.Fatalf("InspectLegacyV1() error = %v", err)
	}
	_, err = NewPendingTorrent(NewPendingTorrentInput{
		UploaderID: uuid.New(), CategoryID: "software", Title: "Legacy",
		ObjectID: uuid.New(), Metainfo: metainfo, OccurredAt: time.Now(),
	})
	if !errors.Is(err, ErrTorrentInputInvalid) {
		t.Fatalf("NewPendingTorrent() error = %v, want ErrTorrentInputInvalid", err)
	}
}

func TestTorrentStateMachineControlsTrackerEligibility(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	torrent := testPendingTorrent(t, base)
	if err := torrent.Publish(base.Add(time.Minute)); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if !torrent.TrackerEligible() || torrent.PublishedAt == nil || torrent.Version != 2 {
		t.Fatalf("published torrent = %+v", torrent)
	}
	firstPublishedAt := *torrent.PublishedAt

	if err := torrent.Disable(base.Add(2 * time.Minute)); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	if torrent.TrackerEligible() || torrent.State != StateDisabled {
		t.Fatalf("disabled torrent = %+v", torrent)
	}
	if err := torrent.Restore(base.Add(3 * time.Minute)); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if !torrent.TrackerEligible() || torrent.PublishedAt == nil || !torrent.PublishedAt.Equal(firstPublishedAt) {
		t.Fatalf("restored torrent = %+v", torrent)
	}
	if err := torrent.Delete(base.Add(4 * time.Minute)); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if torrent.TrackerEligible() || torrent.State != StateDeleted || torrent.Version != 5 {
		t.Fatalf("deleted torrent = %+v", torrent)
	}
	if err := torrent.Restore(base.Add(5 * time.Minute)); !errors.Is(err, ErrTorrentStateConflict) {
		t.Fatalf("Restore() error = %v, want ErrTorrentStateConflict", err)
	}
}

func TestTorrentRejectedSubmissionCanBeResubmittedButNotDisabled(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	torrent := testPendingTorrent(t, base)
	if err := torrent.Reject(base.Add(time.Minute)); err != nil {
		t.Fatalf("Reject() error = %v", err)
	}
	if err := torrent.Disable(base.Add(2 * time.Minute)); !errors.Is(err, ErrTorrentStateConflict) {
		t.Fatalf("Disable() error = %v, want ErrTorrentStateConflict", err)
	}
	if err := torrent.Resubmit(base.Add(2 * time.Minute)); err != nil {
		t.Fatalf("Resubmit() error = %v", err)
	}
	if torrent.State != StatePendingReview || torrent.Version != 3 || torrent.PublishedAt != nil {
		t.Fatalf("resubmitted torrent = %+v", torrent)
	}
}

func TestTorrentResubmissionChangesOnlyEditableMetadata(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	torrent := testPendingTorrent(t, base)
	if err := torrent.Reject(base.Add(time.Minute)); err != nil {
		t.Fatalf("Reject() error = %v", err)
	}
	originalInfoHash := torrent.InfoHashV1
	originalObject := torrent.Object
	originalSubmittedAt := torrent.SubmittedAt
	metadata, err := NewEditableMetadata(" movies ", " Corrected release ", " New subtitle ")
	if err != nil {
		t.Fatalf("NewEditableMetadata() error = %v", err)
	}
	if err := torrent.ResubmitWithMetadata(metadata, base.Add(2*time.Minute)); err != nil {
		t.Fatalf("ResubmitWithMetadata() error = %v", err)
	}
	if torrent.State != StatePendingReview || torrent.Version != 3 || torrent.CategoryID != "movies" ||
		torrent.Title != "Corrected release" || torrent.Subtitle != "New subtitle" {
		t.Fatalf("resubmitted torrent = %+v", torrent)
	}
	if torrent.InfoHashV1 != originalInfoHash || torrent.Object.ID != originalObject.ID ||
		torrent.Object.ContentSHA256 != originalObject.ContentSHA256 || torrent.Object.ByteLength != originalObject.ByteLength ||
		!torrent.SubmittedAt.Equal(originalSubmittedAt) {
		t.Fatalf("resubmission changed immutable identity = %+v", torrent)
	}
}

func TestTorrentResubmissionRejectsUnchangedMetadataWithoutMutation(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	torrent := testPendingTorrent(t, base)
	if err := torrent.Reject(base.Add(time.Minute)); err != nil {
		t.Fatalf("Reject() error = %v", err)
	}
	before := torrent
	metadata, err := NewEditableMetadata(torrent.CategoryID, torrent.Title, torrent.Subtitle)
	if err != nil {
		t.Fatalf("NewEditableMetadata() error = %v", err)
	}
	if err := torrent.ResubmitWithMetadata(metadata, base.Add(2*time.Minute)); !errors.Is(err, ErrTorrentMetadataUnchanged) {
		t.Fatalf("ResubmitWithMetadata() error = %v, want ErrTorrentMetadataUnchanged", err)
	}
	if !reflect.DeepEqual(torrent, before) {
		t.Fatalf("unchanged resubmission mutated torrent = %+v", torrent)
	}
}

func TestTorrentStateMachineRejectsTimeRegression(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	torrent := testPendingTorrent(t, base)
	if err := torrent.Publish(base.Add(-time.Second)); !errors.Is(err, ErrTorrentTimeRegression) {
		t.Fatalf("Publish() error = %v, want ErrTorrentTimeRegression", err)
	}
	if torrent.State != StatePendingReview || torrent.Version != 1 {
		t.Fatalf("torrent mutated after rejected transition = %+v", torrent)
	}
}

func TestParseInfoHashV1HexUsesFixedBinaryIdentity(t *testing.T) {
	t.Parallel()

	hash, err := ParseInfoHashV1Hex("00112233445566778899AABBCCDDEEFF00112233")
	if err != nil {
		t.Fatalf("ParseInfoHashV1Hex() error = %v", err)
	}
	if got, want := hash.Hex(), "00112233445566778899aabbccddeeff00112233"; got != want {
		t.Fatalf("Hex() = %q, want %q", got, want)
	}
	if _, err := ParseInfoHashV1Hex("0011"); !errors.Is(err, ErrTorrentInputInvalid) {
		t.Fatalf("ParseInfoHashV1Hex(short) error = %v, want ErrTorrentInputInvalid", err)
	}
}

func testPendingTorrent(t *testing.T, occurredAt time.Time) Torrent {
	t.Helper()
	metainfo := mustParseV1(t, validSingleFixture("release.bin", 1, 16*1024), ValidationProfileStrictUpload)
	torrent, err := NewPendingTorrent(NewPendingTorrentInput{
		UploaderID: uuid.MustParse("0198fc78-d000-7b21-a222-222222222222"),
		CategoryID: "software",
		Title:      "Release",
		ObjectID:   uuid.MustParse("0198fc78-d000-7b21-a333-333333333333"),
		Metainfo:   metainfo,
		OccurredAt: occurredAt,
	})
	if err != nil {
		t.Fatalf("NewPendingTorrent() error = %v", err)
	}
	return torrent
}
