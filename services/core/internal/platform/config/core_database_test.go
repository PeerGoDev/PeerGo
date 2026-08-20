package config

import (
	"strings"
	"testing"
)

func TestLoadCoreDatabaseProcessRejectsInvalidAndInsecureProductionURLs(t *testing.T) {
	t.Setenv("PEERGO_ENV", "development")
	t.Setenv("PEERGO_CORE_DATABASE_URL", "not-a-postgres-url")
	if _, err := LoadCoreDatabaseProcess(); err == nil || !strings.Contains(err.Error(), "PostgreSQL connection URL") {
		t.Fatalf("invalid Core database URL error = %v", err)
	}
	t.Setenv("PEERGO_ENV", "production")
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://core.example/peergo?sslmode=disable")
	if _, err := LoadCoreDatabaseProcess(); err == nil || !strings.Contains(err.Error(), "PostgreSQL connection URL") {
		t.Fatalf("insecure production Core database URL error = %v", err)
	}
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://core.example/peergo?sslmode=require")
	if _, err := LoadCoreDatabaseProcess(); err != nil {
		t.Fatalf("secure Core database URL error = %v", err)
	}
}

func TestValidateCutoverDatabaseURLRequiresHostnameVerificationInProduction(t *testing.T) {
	if err := ValidateCutoverDatabaseURL("postgres://core.example/peergo?sslmode=require", "production"); err == nil {
		t.Fatal("production cutover accepted TLS without hostname verification")
	}
	if err := ValidateCutoverDatabaseURL("postgres://core.example/peergo?sslmode=verify-full", "production"); err != nil {
		t.Fatalf("production cutover rejected verify-full: %v", err)
	}
	if err := ValidateCutoverDatabaseURL("postgres://127.0.0.1/peergo?sslmode=disable", "development"); err != nil {
		t.Fatalf("development cutover URL rejected: %v", err)
	}
}

func TestSingleServerDatabaseExceptionIsExact(t *testing.T) {
	t.Setenv("PEERGO_ENV", "production")
	t.Setenv("PEERGO_DEPLOYMENT_MODE", "single-server")
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://peergo_core:secret@postgresql:5432/peergo_core?sslmode=disable")
	if _, err := LoadCoreDatabaseProcess(); err != nil {
		t.Fatalf("single-server database rejected: %v", err)
	}
	if err := ValidateCutoverDatabaseURL("postgres://peergo_core:secret@postgresql:5432/peergo_core?sslmode=disable", "production"); err != nil {
		t.Fatalf("single-server cutover database rejected: %v", err)
	}
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://peergo_core:secret@other-db:5432/peergo_core?sslmode=disable")
	if _, err := LoadCoreDatabaseProcess(); err == nil {
		t.Fatal("single-server accepted a different clear-text database host")
	}
}
