package main

import (
	"path/filepath"
	"testing"
)

func TestValidateOutputPath(t *testing.T) {
	wanted := filepath.Join(t.TempDir(), "preview.jsonl")
	got, err := validateOutputPath(wanted)
	if err != nil || got != wanted {
		t.Fatalf("validateOutputPath() = %q, %v", got, err)
	}
	for _, invalid := range []string{"preview.jsonl", wanted + " ", filepath.Join(filepath.Dir(wanted), "preview.json")} {
		if _, err := validateOutputPath(invalid); err == nil {
			t.Fatalf("validateOutputPath(%q) succeeded", invalid)
		}
	}
}
