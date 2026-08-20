package legacytorrents

import (
	"errors"
	"testing"
)

func TestMapLegacyAttributesUsesSharedCanonicalFacets(t *testing.T) {
	t.Parallel()

	vocabulary := legacyVocabulary{}
	for _, item := range [][3]string{
		{"movie", "genre", "剧情"},
		{"movie", "region", "澳大利亚"},
		{"movie", "resolution", "4K / 2160p"},
		{"software", "genre", "游戏"},
		{"software", "platform", "Windows"},
		{"9kg", "source", "Web-DL"},
	} {
		vocabulary.add(item[0], item[1], item[2], item[2])
	}

	values, _, err := mapLegacyAttributes("movie", `{
        "genre":["剧情"],
        "region":"澳大利亚",
        "resolution":"4K / 2160p",
        "source":"Remux",
        "imdb_id":"tt1234567"
    }`, vocabulary)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"genre":        "剧情",
		"region":       "australia",
		"resolution":   "2160p",
		"release-type": "remux",
	}
	if len(values) != len(want) {
		t.Fatalf("facet values = %+v", values)
	}
	for _, value := range values {
		if want[value.FacetID] != value.OptionKey || value.CategoryID != "movies" {
			t.Fatalf("unexpected facet value = %+v", value)
		}
	}

	software, _, err := mapLegacyAttributes(
		"software",
		`{"genre":["游戏"],"platform":"Windows"}`,
		vocabulary,
	)
	if err != nil || len(software) != 2 {
		t.Fatalf("software facets = %+v, %v", software, err)
	}
	for _, value := range software {
		if value.CategoryID != "software" {
			t.Fatalf("software category was guessed into games: %+v", value)
		}
	}

	adult, _, err := mapLegacyAttributes("9kg", `{"source":"Web-DL"}`, vocabulary)
	if err != nil || len(adult) != 1 || adult[0].FacetID != "distribution-channel" || adult[0].OptionKey != "web-dl" {
		t.Fatalf("adult source facet = %+v, %v", adult, err)
	}
}

func TestMapLegacyAttributesFailsClosedOutsideFrozenVocabulary(t *testing.T) {
	t.Parallel()

	_, _, err := mapLegacyAttributes(
		"movie",
		`{"genre":["未审核的新类型"]}`,
		legacyVocabulary{},
	)
	if !errors.Is(err, errLegacyTaxonomy) {
		t.Fatalf("map unknown value error = %v", err)
	}

	vocabulary := legacyVocabulary{}
	vocabulary.add("movie", "new-field", "value", "value")
	_, _, err = mapLegacyAttributes(
		"movie",
		`{"new-field":"value"}`,
		vocabulary,
	)
	if !errors.Is(err, errLegacyTaxonomy) {
		t.Fatalf("map unknown field error = %v", err)
	}
}
