package audittext

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeFieldCompletesKnownAndSuffixedAuditFields(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"reason", "note", "response", "review_reason", "moderator_note", "appeal_response"} {
		normalized, known := NormalizeField(field, "")
		if !known || utf8.RuneCountInString(normalized) < MinimumPersistedRunes {
			t.Fatalf("field %q normalized to %q, known=%t", field, normalized, known)
		}
	}
}

func TestNormalizeFieldPreservesAndCompletesOperatorText(t *testing.T) {
	t.Parallel()

	short, known := NormalizeField("reason", "  已核对  ")
	if !known || !strings.Contains(short, "已核对") || utf8.RuneCountInString(short) < MinimumPersistedRunes {
		t.Fatalf("short reason = %q, known=%t", short, known)
	}

	const complete = "已核对当前设置并确认可以安全保存。"
	if normalized, _ := NormalizeField("reason", complete); normalized != complete {
		t.Fatalf("complete reason = %q", normalized)
	}

	if normalized, known := NormalizeField("statement", ""); known || normalized != "" {
		t.Fatalf("business statement was treated as audit text: %q, known=%t", normalized, known)
	}
}
