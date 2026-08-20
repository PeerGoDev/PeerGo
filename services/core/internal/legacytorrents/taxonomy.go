package legacytorrents

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	selectionSingle = "single_option"
	selectionMulti  = "multi_option"
)

var errLegacyTaxonomy = errors.New("PtYes taxonomy value is invalid")

type taxonomyError struct {
	code string
}

func (problem taxonomyError) Error() string {
	return problem.code + ": " + errLegacyTaxonomy.Error()
}

func (problem taxonomyError) Unwrap() error {
	return errLegacyTaxonomy
}

type vocabularyKey struct {
	category string
	field    string
	value    string
}

// legacyVocabulary is loaded from PtYes's frozen category option tables. It is
// an allowlist, not a source of PeerGo types: semantic normalization below maps
// every selected value into a shared PeerGo facet.
type legacyVocabulary map[vocabularyKey]string

func (vocabulary legacyVocabulary) add(category, field, value, label string) {
	value = strings.TrimSpace(value)
	label = strings.TrimSpace(label)
	if value == "" {
		return
	}
	if label == "" {
		label = value
	}
	vocabulary[vocabularyKey{category: category, field: field, value: value}] = label
}

func (vocabulary legacyVocabulary) allows(category, field, value string) bool {
	_, declared := vocabulary[vocabularyKey{category: category, field: field, value: value}]
	if declared {
		return true
	}
	_, auditedDrift := auditedPtYesVocabularyDrift[vocabularyKey{
		category: category,
		field:    field,
		value:    value,
	}]
	return auditedDrift
}

// These 19 values account for all 38 snapshot uses outside PtYes's own option
// table. Keeping the finite set explicit prevents an arbitrary source string
// from becoming a live PeerGo taxonomy option.
var auditedPtYesVocabularyDrift = map[vocabularyKey]struct{}{
	{category: "animation", field: "genre", value: "动作"}:        {},
	{category: "animation", field: "genre", value: "喜剧"}:        {},
	{category: "animation", field: "source", value: "Remux"}:    {},
	{category: "animation", field: "source", value: "Encode"}:   {},
	{category: "documentary", field: "source", value: "Encode"}: {},
	{category: "documentary", field: "source", value: "Remux"}:  {},
	{category: "movie", field: "source", value: "Remux"}:        {},
	{category: "movie", field: "source", value: "WEBrip"}:       {},
	{category: "other", field: "genre", value: "其它"}:            {},
	{category: "other", field: "region", value: "大陆"}:           {},
	{category: "other", field: "resolution", value: "1080i"}:    {},
	{category: "other", field: "source", value: "Blu-ray"}:      {},
	{category: "tv", field: "genre", value: "其它"}:               {},
	{category: "tv", field: "genre", value: "战斗"}:               {},
	{category: "tv", field: "genre", value: "搞笑"}:               {},
	{category: "tv", field: "genre", value: "机战"}:               {},
	{category: "tv", field: "genre", value: "热血"}:               {},
	{category: "tv", field: "source", value: "Remux"}:           {},
	{category: "tv", field: "source", value: "WEBrip"}:          {},
}

type facetValue struct {
	CategoryID    string
	FacetID       string
	OptionKey     string
	Label         string
	SelectionMode string
	Position      int
}

type facetTarget struct {
	facetID       string
	optionKey     string
	label         string
	selectionMode string
}

var categoryMap = map[string]string{
	"movie":       "movies",
	"tv":          "tv",
	"documentary": "documentary",
	"animation":   "anime",
	"variety":     "variety",
	"sports":      "sports",
	"music":       "music",
	"software":    "software",
	"ebook":       "ebooks",
	"9kg":         "9kg",
	"other":       "other",
}

var resolutionMap = map[string]facetTarget{
	"4K / 2160p": {facetID: "resolution", optionKey: "2160p", label: "4K / 2160p", selectionMode: selectionSingle},
	"1080p":      {facetID: "resolution", optionKey: "1080p", label: "1080p", selectionMode: selectionSingle},
	"1080i":      {facetID: "resolution", optionKey: "1080i", label: "1080i", selectionMode: selectionSingle},
	"720p":       {facetID: "resolution", optionKey: "720p", label: "720p", selectionMode: selectionSingle},
	"SD":         {facetID: "resolution", optionKey: "sd", label: "SD", selectionMode: selectionSingle},
	"其它":         {facetID: "resolution", optionKey: "other", label: "其它", selectionMode: selectionSingle},
	"其他":         {facetID: "resolution", optionKey: "other", label: "其它", selectionMode: selectionSingle},
}

var regionMap = map[string]facetTarget{
	"大陆":   {facetID: "region", optionKey: "mainland-china", label: "大陆", selectionMode: selectionSingle},
	"香港":   {facetID: "region", optionKey: "hong-kong", label: "香港", selectionMode: selectionSingle},
	"台湾":   {facetID: "region", optionKey: "taiwan", label: "台湾", selectionMode: selectionSingle},
	"日本":   {facetID: "region", optionKey: "japan", label: "日本", selectionMode: selectionSingle},
	"韩国":   {facetID: "region", optionKey: "south-korea", label: "韩国", selectionMode: selectionSingle},
	"美国":   {facetID: "region", optionKey: "usa", label: "美国", selectionMode: selectionSingle},
	"英国":   {facetID: "region", optionKey: "uk", label: "英国", selectionMode: selectionSingle},
	"法国":   {facetID: "region", optionKey: "france", label: "法国", selectionMode: selectionSingle},
	"德国":   {facetID: "region", optionKey: "germany", label: "德国", selectionMode: selectionSingle},
	"意大利":  {facetID: "region", optionKey: "italy", label: "意大利", selectionMode: selectionSingle},
	"西班牙":  {facetID: "region", optionKey: "spain", label: "西班牙", selectionMode: selectionSingle},
	"俄罗斯":  {facetID: "region", optionKey: "russia", label: "俄罗斯", selectionMode: selectionSingle},
	"印度":   {facetID: "region", optionKey: "india", label: "印度", selectionMode: selectionSingle},
	"泰国":   {facetID: "region", optionKey: "thailand", label: "泰国", selectionMode: selectionSingle},
	"澳大利亚": {facetID: "region", optionKey: "australia", label: "澳大利亚", selectionMode: selectionSingle},
	"加拿大":  {facetID: "region", optionKey: "canada", label: "加拿大", selectionMode: selectionSingle},
	"新西兰":  {facetID: "region", optionKey: "new-zealand", label: "新西兰", selectionMode: selectionSingle},
	"其它":   {facetID: "region", optionKey: "other", label: "其它", selectionMode: selectionSingle},
}

var ordinarySourceMap = map[string]facetTarget{
	"UHD Blu-ray": {facetID: "source-medium", optionKey: "uhd-blu-ray", label: "UHD Blu-ray", selectionMode: selectionSingle},
	"UHD Blu-Ray": {facetID: "source-medium", optionKey: "uhd-blu-ray", label: "UHD Blu-ray", selectionMode: selectionSingle},
	"Blu-ray":     {facetID: "source-medium", optionKey: "blu-ray", label: "Blu-ray", selectionMode: selectionSingle},
	"Blu-Ray":     {facetID: "source-medium", optionKey: "blu-ray", label: "Blu-ray", selectionMode: selectionSingle},
	"WEB-DL":      {facetID: "release-type", optionKey: "web-dl", label: "WEB-DL", selectionMode: selectionSingle},
	"Web-DL":      {facetID: "release-type", optionKey: "web-dl", label: "WEB-DL", selectionMode: selectionSingle},
	"WEBrip":      {facetID: "release-type", optionKey: "webrip", label: "WEBRip", selectionMode: selectionSingle},
	"Webrip":      {facetID: "release-type", optionKey: "webrip", label: "WEBRip", selectionMode: selectionSingle},
	"HDTV":        {facetID: "release-type", optionKey: "hdtv", label: "HDTV", selectionMode: selectionSingle},
	"DVDRip":      {facetID: "release-type", optionKey: "dvdrip", label: "DVDRip", selectionMode: selectionSingle},
	"Remux":       {facetID: "release-type", optionKey: "remux", label: "Remux", selectionMode: selectionSingle},
	"Encode":      {facetID: "release-type", optionKey: "encode", label: "Encode", selectionMode: selectionSingle},
	"其它":          {facetID: "source-medium", optionKey: "other", label: "其它", selectionMode: selectionSingle},
}

var formatMap = map[string]facetTarget{
	"FLAC": {facetID: "format", optionKey: "flac", label: "FLAC", selectionMode: selectionSingle},
	"APE":  {facetID: "format", optionKey: "ape", label: "APE", selectionMode: selectionSingle},
	"WAV":  {facetID: "format", optionKey: "wav", label: "WAV", selectionMode: selectionSingle},
	"DSD":  {facetID: "format", optionKey: "dsd", label: "DSD", selectionMode: selectionSingle},
	"MP3":  {facetID: "format", optionKey: "mp3", label: "MP3", selectionMode: selectionSingle},
	"AAC":  {facetID: "format", optionKey: "aac", label: "AAC", selectionMode: selectionSingle},
	"EPUB": {facetID: "format", optionKey: "epub", label: "EPUB", selectionMode: selectionSingle},
	"MOBI": {facetID: "format", optionKey: "mobi", label: "MOBI", selectionMode: selectionSingle},
	"PDF":  {facetID: "format", optionKey: "pdf", label: "PDF", selectionMode: selectionSingle},
	"AZW3": {facetID: "format", optionKey: "azw3", label: "AZW3", selectionMode: selectionSingle},
	"TXT":  {facetID: "format", optionKey: "txt", label: "TXT", selectionMode: selectionSingle},
	"其它":   {facetID: "format", optionKey: "other", label: "其它", selectionMode: selectionSingle},
}

var platformMap = map[string]facetTarget{
	"Windows":     {facetID: "platform", optionKey: "windows", label: "Windows", selectionMode: selectionMulti},
	"macOS":       {facetID: "platform", optionKey: "macos", label: "macOS", selectionMode: selectionMulti},
	"Linux":       {facetID: "platform", optionKey: "linux", label: "Linux", selectionMode: selectionMulti},
	"Android":     {facetID: "platform", optionKey: "android", label: "Android", selectionMode: selectionMulti},
	"iOS":         {facetID: "platform", optionKey: "ios", label: "iOS", selectionMode: selectionMulti},
	"PlayStation": {facetID: "platform", optionKey: "playstation", label: "PlayStation", selectionMode: selectionMulti},
	"Xbox":        {facetID: "platform", optionKey: "xbox", label: "Xbox", selectionMode: selectionMulti},
	"Nintendo":    {facetID: "platform", optionKey: "nintendo", label: "Nintendo", selectionMode: selectionMulti},
	"其它":          {facetID: "platform", optionKey: "other", label: "其它", selectionMode: selectionMulti},
}

var adultSourceMap = map[string]facetTarget{
	"转载":          {facetID: "distribution-channel", optionKey: "repost", label: "转载", selectionMode: selectionSingle},
	"其他":          {facetID: "distribution-channel", optionKey: "other", label: "其它", selectionMode: selectionSingle},
	"其它":          {facetID: "distribution-channel", optionKey: "other", label: "其它", selectionMode: selectionSingle},
	"Web-DL":      {facetID: "distribution-channel", optionKey: "web-dl", label: "WEB-DL", selectionMode: selectionSingle},
	"推特/X":        {facetID: "distribution-channel", optionKey: "twitter-x", label: "推特 / X", selectionMode: selectionSingle},
	"AI破解":        {facetID: "distribution-channel", optionKey: "ai-cracked", label: "AI 破解", selectionMode: selectionSingle},
	"Blu-Ray":     {facetID: "distribution-channel", optionKey: "blu-ray", label: "Blu-ray", selectionMode: selectionSingle},
	"Onlyfans":    {facetID: "distribution-channel", optionKey: "onlyfans", label: "OnlyFans", selectionMode: selectionSingle},
	"母带流出":        {facetID: "distribution-channel", optionKey: "master-leak", label: "母带流出", selectionMode: selectionSingle},
	"电报":          {facetID: "distribution-channel", optionKey: "telegram", label: "Telegram", selectionMode: selectionSingle},
	"Patreon":     {facetID: "distribution-channel", optionKey: "patreon", label: "Patreon", selectionMode: selectionSingle},
	"Webrip":      {facetID: "distribution-channel", optionKey: "webrip", label: "WEBRip", selectionMode: selectionSingle},
	"Fansly":      {facetID: "distribution-channel", optionKey: "fansly", label: "Fansly", selectionMode: selectionSingle},
	"ManyVids":    {facetID: "distribution-channel", optionKey: "manyvids", label: "ManyVids", selectionMode: selectionSingle},
	"Pornhub":     {facetID: "distribution-channel", optionKey: "pornhub", label: "Pornhub", selectionMode: selectionSingle},
	"UHD Blu-Ray": {facetID: "distribution-channel", optionKey: "uhd-blu-ray", label: "UHD Blu-ray", selectionMode: selectionSingle},
}

var ignoredAttributeKeys = map[string]struct{}{
	"imdb": {}, "imdb_id": {}, "tmdb": {}, "tmdb_id": {},
	"douban": {}, "douban_id": {}, "bangumi": {}, "bangumi_id": {},
	"bgm": {}, "bgm_id": {}, "steam": {}, "steam_id": {},
}

func mapLegacyAttributes(
	sourceCategory string,
	raw string,
	vocabulary legacyVocabulary,
) ([]facetValue, map[string]json.RawMessage, error) {
	targetCategory, exists := categoryMap[sourceCategory]
	if !exists {
		return nil, nil, taxonomyError{code: "unknown_category"}
	}
	attributes := make(map[string]json.RawMessage)
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &attributes); err != nil {
			return nil, nil, taxonomyError{code: "invalid_attribute_json"}
		}
	}
	result := make([]facetValue, 0, len(attributes))
	seenSingle := make(map[string]struct{})
	seenOption := make(map[string]struct{})
	for field, encoded := range attributes {
		if _, ignored := ignoredAttributeKeys[field]; ignored {
			continue
		}
		values, err := legacyAttributeValues(field, encoded)
		if err != nil {
			return nil, nil, err
		}
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if !vocabulary.allows(sourceCategory, field, value) {
				return nil, nil, taxonomyError{code: "unmapped_attribute_value"}
			}
			target, err := mapLegacyFacetTarget(sourceCategory, field, value)
			if err != nil {
				return nil, nil, err
			}
			if !validFacetText(target.optionKey) || !validFacetText(target.label) {
				return nil, nil, taxonomyError{code: "invalid_attribute_value"}
			}
			identity := target.facetID + "\x00" + target.optionKey
			if _, duplicate := seenOption[identity]; duplicate {
				continue
			}
			if target.selectionMode == selectionSingle {
				if _, duplicate := seenSingle[target.facetID]; duplicate {
					return nil, nil, taxonomyError{code: "duplicate_single_facet"}
				}
				seenSingle[target.facetID] = struct{}{}
			}
			position := 0
			for _, existing := range result {
				if existing.FacetID == target.facetID {
					position++
				}
			}
			if position > 31 {
				return nil, nil, taxonomyError{code: "too_many_facet_values"}
			}
			seenOption[identity] = struct{}{}
			result = append(result, facetValue{
				CategoryID: targetCategory, FacetID: target.facetID,
				OptionKey: target.optionKey, Label: target.label,
				SelectionMode: target.selectionMode, Position: position,
			})
		}
	}
	return result, attributes, nil
}

func legacyAttributeValues(field string, raw json.RawMessage) ([]string, error) {
	var scalar string
	if err := json.Unmarshal(raw, &scalar); err == nil {
		return []string{scalar}, nil
	}
	if field != "genre" && field != "themes" && field != "behaviors" && field != "platform" {
		return nil, taxonomyError{code: "invalid_attribute_shape"}
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, taxonomyError{code: "invalid_attribute_shape"}
	}
	return values, nil
}

func mapLegacyFacetTarget(sourceCategory, field, value string) (facetTarget, error) {
	switch field {
	case "genre", "themes", "behaviors":
		return facetTarget{facetID: field, optionKey: value, label: value, selectionMode: selectionMulti}, nil
	case "resolution":
		return mappedFacetTarget(resolutionMap, value)
	case "region":
		return mappedFacetTarget(regionMap, value)
	case "source":
		if sourceCategory == "9kg" {
			return mappedFacetTarget(adultSourceMap, value)
		}
		return mappedFacetTarget(ordinarySourceMap, value)
	case "format":
		return mappedFacetTarget(formatMap, value)
	case "platform":
		return mappedFacetTarget(platformMap, value)
	default:
		return facetTarget{}, taxonomyError{code: "unknown_attribute"}
	}
}

func mappedFacetTarget(values map[string]facetTarget, value string) (facetTarget, error) {
	target, exists := values[value]
	if !exists {
		return facetTarget{}, taxonomyError{code: "unmapped_attribute_value"}
	}
	return target, nil
}

func validFacetText(value string) bool {
	length := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && strings.TrimSpace(value) == value && length >= 1 && length <= 80
}
