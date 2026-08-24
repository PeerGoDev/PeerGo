// Package audittext normalizes optional operator evidence before it reaches
// domain validation and durable audit storage.
package audittext

import (
	"strings"
	"unicode/utf8"
)

const MinimumPersistedRunes = 10

var defaults = map[string]string{
	"reason":          "系统自动记录：操作时未填写变更理由。",
	"note":            "系统自动记录：审核时未填写处理意见。",
	"response":        "系统自动记录：处理时未填写回复说明。",
	"correction_note": "系统自动记录：重新提交时未填写修正说明。",
	"review_reason":   "系统自动记录：审核时未填写处理意见。",
}

// NormalizeField preserves meaningful operator text while ensuring optional
// audit fields satisfy the existing storage contract. The boolean reports
// whether the field is an audit-text field known to this package.
func NormalizeField(field, value string) (string, bool) {
	fallback, ok := defaults[field]
	if !ok {
		switch {
		case strings.HasSuffix(field, "_reason"):
			fallback = defaults["reason"]
		case strings.HasSuffix(field, "_note"):
			fallback = defaults["note"]
		case strings.HasSuffix(field, "_response"):
			fallback = defaults["response"]
		default:
			return value, false
		}
	}
	return normalize(value, fallback), true
}

// Reason normalizes the reason part used by multipart commands, which do not
// pass through the JSON request-body middleware.
func Reason(value string) string {
	normalized, _ := NormalizeField("reason", value)
	return normalized
}

func DefaultFor(field string) (string, bool) {
	value, ok := defaults[field]
	return value, ok
}

func normalize(value, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	if utf8.RuneCountInString(trimmed) >= MinimumPersistedRunes {
		return trimmed
	}
	return "人工填写的简短说明：" + trimmed + "（系统自动补全）"
}
