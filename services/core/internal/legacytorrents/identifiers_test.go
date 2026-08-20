package legacytorrents

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestExtractTorrentExternalIDsValidatesAndRecoversKnownProviders(t *testing.T) {
	t.Parallel()

	attributes := map[string]json.RawMessage{
		"imdb_id": json.RawMessage(`"bad-id"`),
		"imdb":    json.RawMessage(`"https://www.imdb.com/title/tt1234567/"`),
		"tmdb_id": json.RawMessage(`"42"`),
		"tmdb":    json.RawMessage(`"https://www.themoviedb.org/movie/42-title"`),
		"douban":  json.RawMessage(`"https://movie.douban.com/subject/1292052/"`),
		"bangumi": json.RawMessage(`"https://bgm.tv/subject/123"`),
	}
	identifiers, warnings, err := extractTorrentExternalIDs(attributes)
	if err != nil {
		t.Fatal(err)
	}
	if identifiers["imdb"] != "tt1234567" || identifiers["tmdb"] != "42" ||
		identifiers["douban"] != "1292052" || identifiers["bangumi"] != "123" {
		t.Fatalf("identifiers = %+v", identifiers)
	}
	if len(warnings) != 1 || warnings[0].Provider != "imdb" || warnings[0].Code != warningExternalIDRecovered {
		t.Fatalf("warnings = %+v", warnings)
	}
}

func TestExtractTorrentExternalIDsRejectsConflictingEvidence(t *testing.T) {
	t.Parallel()

	_, _, err := extractTorrentExternalIDs(map[string]json.RawMessage{
		"imdb_id": json.RawMessage(`"tt1234567"`),
		"imdb":    json.RawMessage(`"https://www.imdb.com/title/tt7654321/"`),
	})
	if !errors.Is(err, errLegacyTaxonomy) {
		t.Fatalf("conflicting identifier error = %v", err)
	}
}

func TestExtractGroupExternalIDsSkipsInvalidOptionalValue(t *testing.T) {
	t.Parallel()

	identifiers, warnings, err := extractGroupExternalIDs(`{
        "imdb":"not-an-imdb-id",
        "tmdb":"42",
        "douban":"1292052"
    }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(identifiers) != 2 || identifiers["tmdb"] != "42" || identifiers["douban"] != "1292052" {
		t.Fatalf("group identifiers = %+v", identifiers)
	}
	if len(warnings) != 1 || warnings[0].Provider != "imdb" || warnings[0].Code != warningExternalIDSkipped {
		t.Fatalf("warnings = %+v", warnings)
	}
}

func TestExtractGroupExternalIDsRecoversOnlyAuditedPtYesIMDbSuffixes(t *testing.T) {
	t.Parallel()

	identifiers, warnings, err := extractGroupExternalIDs(`{
        "imdb":"tt1234567/parentalguide",
        "tmdb":"42"
    }`)
	if err != nil {
		t.Fatal(err)
	}
	if identifiers["imdb"] != "tt1234567" || identifiers["tmdb"] != "42" {
		t.Fatalf("group identifiers = %+v", identifiers)
	}
	if len(warnings) != 1 || warnings[0].Provider != "imdb" || warnings[0].Code != warningExternalIDRecovered {
		t.Fatalf("warnings = %+v", warnings)
	}

	identifiers, warnings, err = extractGroupExternalIDs(`{"imdb":"tt1234567-arbitrary"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(identifiers) != 0 || len(warnings) != 1 || warnings[0].Code != warningExternalIDSkipped {
		t.Fatalf("unexpected broad recovery: identifiers=%+v warnings=%+v", identifiers, warnings)
	}
}
