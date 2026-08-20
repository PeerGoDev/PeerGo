package legacytorrents

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

var (
	imdbIDPattern    = regexp.MustCompile(`^tt[0-9]{7,10}$`)
	numericIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)
	imdbPathPattern  = regexp.MustCompile(`/title/(tt[0-9]{7,10})(/|$)`)
	// Twelve audited PtYes resource-group rows contain a valid IMDb ID followed
	// by one of these exact UI/link fragments. Keep this recovery rule local to
	// the finite legacy importer; accepting an arbitrary prefix would turn
	// malformed identifiers into live catalog data without review.
	legacyGroupIMDbPattern = regexp.MustCompile(`^(tt[0-9]{7,10})(?:/(?:parentalguide|ratings|\[|\]https://www\.imdb\.com)|\]?https://www\.imdb\.com)$`)
	tmdbPathPattern        = regexp.MustCompile(`/(movie|tv)/([0-9]{1,20})([-/]|$)`)
	subjectPathPattern     = regexp.MustCompile(`/subject/([0-9]{1,20})(/|$)`)
	steamPathPattern       = regexp.MustCompile(`/app/([0-9]{1,20})(/|$)`)
)

type identifierWarning struct {
	Provider string
	Code     string
}

const (
	warningExternalIDRecovered = "invalid_external_id_recovered"
	warningExternalIDSkipped   = "invalid_external_id_skipped"
)

func extractTorrentExternalIDs(attributes map[string]json.RawMessage) (map[string]string, []identifierWarning, error) {
	result := make(map[string]string)
	warnings := make([]identifierWarning, 0)
	for _, provider := range []string{"imdb", "tmdb", "douban", "bangumi", "steam"} {
		direct := stringAttribute(attributes, provider+"_id")
		if provider == "bangumi" && direct == "" {
			direct = stringAttribute(attributes, "bgm_id")
		}
		link := stringAttribute(attributes, provider)
		if provider == "bangumi" && link == "" {
			link = stringAttribute(attributes, "bgm")
		}
		directValue, directValid := validateExternalID(provider, direct)
		linkValue := externalIDFromLink(provider, link)
		if directValid && linkValue != "" && directValue != linkValue {
			return nil, nil, taxonomyError{code: "external_id_conflict"}
		}
		switch {
		case directValid:
			result[provider] = directValue
		case linkValue != "":
			result[provider] = linkValue
			if strings.TrimSpace(direct) != "" {
				warnings = append(warnings, identifierWarning{Provider: provider, Code: warningExternalIDRecovered})
			}
		case strings.TrimSpace(direct) != "" || strings.TrimSpace(link) != "":
			warnings = append(warnings, identifierWarning{Provider: provider, Code: warningExternalIDSkipped})
		}
	}
	return result, warnings, nil
}

func extractGroupExternalIDs(raw string) (map[string]string, []identifierWarning, error) {
	values := make(map[string]string)
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, nil, taxonomyError{code: "invalid_group_external_ids"}
	}
	result := make(map[string]string)
	warnings := make([]identifierWarning, 0)
	for provider, value := range values {
		if provider != "imdb" && provider != "tmdb" && provider != "douban" && provider != "bangumi" && provider != "steam" {
			return nil, nil, taxonomyError{code: "unknown_group_external_id"}
		}
		parsed, valid := validateExternalID(provider, value)
		if !valid {
			if recovered, ok := recoverLegacyGroupExternalID(provider, value); ok {
				result[provider] = recovered
				warnings = append(warnings, identifierWarning{Provider: provider, Code: warningExternalIDRecovered})
				continue
			}
			warnings = append(warnings, identifierWarning{Provider: provider, Code: warningExternalIDSkipped})
			continue
		}
		result[provider] = parsed
	}
	return result, warnings, nil
}

func recoverLegacyGroupExternalID(provider, value string) (string, bool) {
	if provider != "imdb" {
		return "", false
	}
	matches := legacyGroupIMDbPattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(value)))
	if len(matches) != 2 {
		return "", false
	}
	return matches[1], true
}

func stringAttribute(attributes map[string]json.RawMessage, key string) string {
	raw, exists := attributes[key]
	if !exists {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func validateExternalID(provider, value string) (string, bool) {
	value = strings.TrimSpace(value)
	if provider == "imdb" {
		value = strings.ToLower(value)
		return value, imdbIDPattern.MatchString(value)
	}
	return value, numericIDPattern.MatchString(value)
}

func externalIDFromLink(provider, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if direct, valid := validateExternalID(provider, value); valid {
		return direct
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" && provider == "steam" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	path := parsed.EscapedPath()
	var matches []string
	switch provider {
	case "imdb":
		if !hostMatches(host, "imdb.com") {
			return ""
		}
		matches = imdbPathPattern.FindStringSubmatch(path)
		if len(matches) == 3 {
			return strings.ToLower(matches[1])
		}
	case "tmdb":
		if !hostMatches(host, "themoviedb.org") {
			return ""
		}
		matches = tmdbPathPattern.FindStringSubmatch(path)
		if len(matches) == 4 {
			return matches[2]
		}
	case "douban":
		if !hostMatches(host, "douban.com") {
			return ""
		}
		matches = subjectPathPattern.FindStringSubmatch(path)
		if len(matches) == 3 {
			return matches[1]
		}
	case "bangumi":
		if host != "bgm.tv" && !hostMatches(host, "bangumi.tv") && !hostMatches(host, "chii.in") {
			return ""
		}
		matches = subjectPathPattern.FindStringSubmatch(path)
		if len(matches) == 3 {
			return matches[1]
		}
	case "steam":
		if !hostMatches(host, "steampowered.com") {
			return ""
		}
		matches = steamPathPattern.FindStringSubmatch(path)
		if len(matches) == 3 {
			return matches[1]
		}
	}
	return ""
}

func hostMatches(host, domain string) bool {
	return host == domain || strings.HasSuffix(host, "."+domain)
}
