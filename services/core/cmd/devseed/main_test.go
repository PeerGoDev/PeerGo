package main

import (
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

func TestFixtureTorrentUploadInputMatchesTheCurrentMovieVocabulary(t *testing.T) {
	t.Parallel()

	raw := fixtureMetainfo()
	cover, err := fixtureCoverPNG()
	if err != nil {
		t.Fatalf("fixtureCoverPNG() error = %v", err)
	}
	input := fixtureTorrentUploadInput(raw, cover)
	if input.ID != fixtureUploadID || input.CategoryID != "movies" || !reflect.DeepEqual(input.RawMetainfo, raw) {
		t.Fatalf("fixture upload identity = %+v", input)
	}
	if input.Title != fixtureTitle || input.Subtitle != fixtureSubtitle {
		t.Fatalf("fixture public copy = %+v", input)
	}
	if input.Description != fixtureDescription || input.MediaInfo != fixtureMediaInfo || len(input.Screenshots) != 1 || !reflect.DeepEqual(input.Screenshots[0].Raw, cover) {
		t.Fatalf("fixture presentation metadata = %+v", input)
	}
	wantFacets := []torrents.FacetSelection{
		{FacetID: "genre", OptionKeys: []string{"剧情"}},
		{FacetID: "region", OptionKeys: []string{"mainland-china"}},
		{FacetID: "resolution", OptionKeys: []string{"1080p"}},
		{FacetID: "release-type", OptionKeys: []string{"web-dl"}},
	}
	if !reflect.DeepEqual(input.FacetSelections, wantFacets) {
		t.Fatalf("fixture facets = %+v, want %+v", input.FacetSelections, wantFacets)
	}
}

func TestFixtureMetainfoUsesTheStrictRuntimeParser(t *testing.T) {
	t.Parallel()

	raw := fixtureMetainfo()
	parsed, err := torrents.ParseV1(raw, torrents.ValidationProfileStrictUpload)
	if err != nil {
		t.Fatalf("ParseV1() error = %v", err)
	}
	if parsed.Name != "PeerGo.Comment.Demo.2026.1080p.mkv" || !parsed.Private || parsed.MultiFile {
		t.Fatalf("parsed fixture identity = %+v", parsed)
	}
	if parsed.TotalSizeBytes != 1_879_048_192 || parsed.PayloadSizeBytes != parsed.TotalSizeBytes || len(parsed.Files) != 1 {
		t.Fatalf("parsed fixture layout = %+v", parsed)
	}
	if parsed.ObjectByteLength != int64(len(raw)) || torrents.TorrentObjectKey(parsed.ObjectSHA256) == "" {
		t.Fatalf("parsed fixture object = %+v", parsed)
	}
}

func TestOrderedUUIDsFailsClosedAfterConfiguredValues(t *testing.T) {
	t.Parallel()

	next := orderedUUIDs(fixtureCoverObjectID, fixtureObjectID)
	if got := next(); got != fixtureCoverObjectID {
		t.Fatalf("first UUID = %s", got)
	}
	if got := next(); got != fixtureObjectID {
		t.Fatalf("second UUID = %s", got)
	}
	if got := next(); got != uuid.Nil {
		t.Fatalf("exhausted UUID = %s", got)
	}
}
