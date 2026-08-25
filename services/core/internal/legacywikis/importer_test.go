package legacywikis

import (
	"slices"
	"testing"
)

func TestParseEditorIDsAcceptsLegacyShapes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		raw  string
		want []int64
	}{
		{raw: "[]", want: []int64{}},
		{raw: "[163, 1, 163]", want: []int64{1, 163}},
		{raw: `["163", "1"]`, want: []int64{1, 163}},
		{raw: "null", want: []int64{}},
	} {
		got, err := parseEditorIDs(test.raw)
		if err != nil {
			t.Fatalf("parseEditorIDs(%q) error = %v", test.raw, err)
		}
		if !slices.Equal(got, test.want) {
			t.Fatalf("parseEditorIDs(%q) = %v, want %v", test.raw, got, test.want)
		}
	}
}

func TestParseEditorIDsRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{`[0]`, `[-1]`, `["member"]`, `{}`} {
		if _, err := parseEditorIDs(raw); err == nil {
			t.Fatalf("parseEditorIDs(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestMarkdownSummarySkipsHeadingsAndCode(t *testing.T) {
	t.Parallel()

	body := "# 用户规范\n\n```\nnot a summary\n```\n\n> 欢迎阅读站点规则。"
	if got := markdownSummary(body, "fallback"); got != "欢迎阅读站点规则。" {
		t.Fatalf("markdownSummary() = %q", got)
	}
}
