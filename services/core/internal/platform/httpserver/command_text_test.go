package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/peergo/peergo/services/core/internal/platform/audittext"
)

func TestFillAutomaticCommandTextNormalizesBlankAndShortEvidence(t *testing.T) {
	t.Parallel()

	var received map[string]any
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode normalized body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/example", strings.NewReader(`{
		"reason":"   ",
		"nested":{"note":"已核对","response":"这是用户主动填写且足够长的处理结果说明。"}
	}`))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response := httptest.NewRecorder()

	fillAutomaticCommandText(next).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	reason, _ := received["reason"].(string)
	nested, _ := received["nested"].(map[string]any)
	note, _ := nested["note"].(string)
	responseText, _ := nested["response"].(string)
	defaultReason, _ := audittext.DefaultFor("reason")
	if reason != defaultReason {
		t.Fatalf("reason = %q", reason)
	}
	if utf8.RuneCountInString(note) < audittext.MinimumPersistedRunes || !strings.Contains(note, "已核对") {
		t.Fatalf("note = %q", note)
	}
	if responseText != "这是用户主动填写且足够长的处理结果说明。" {
		t.Fatalf("response = %q", responseText)
	}
}

func TestFillAutomaticCommandTextLeavesNonJSONBodyUntouched(t *testing.T) {
	t.Parallel()

	const body = "reason="
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(value) != body {
			t.Fatalf("body = %q", value)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/example", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	fillAutomaticCommandText(next).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestFillAutomaticCommandTextDoesNotRepairInvalidJSON(t *testing.T) {
	t.Parallel()

	const body = `{"reason":""} trailing`
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(value) != body {
			t.Fatalf("body = %q", value)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/example", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	fillAutomaticCommandText(next).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}
