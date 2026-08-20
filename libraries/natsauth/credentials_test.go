package natsauth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOptionFromCredentialsFileAcceptsSingleServerSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nats.creds")
	if err := os.WriteFile(path, []byte(SingleServerMarker+"\nusername=peergo\npassword=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OptionFromCredentialsFile(path); err != nil {
		t.Fatalf("OptionFromCredentialsFile() error = %v", err)
	}
}

func TestOptionFromCredentialsFileRejectsMalformedSingleServerSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nats.creds")
	if err := os.WriteFile(path, []byte(SingleServerMarker+"\nusername=peergo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OptionFromCredentialsFile(path); err == nil {
		t.Fatal("OptionFromCredentialsFile() error = nil")
	}
}

func TestOptionFromCredentialsFileKeepsStandardNATSCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nats.creds")
	if err := os.WriteFile(path, []byte("-----BEGIN NATS USER JWT-----\nvalue\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OptionFromCredentialsFile(path); err != nil {
		t.Fatalf("OptionFromCredentialsFile() error = %v", err)
	}
}
