package config

import "testing"

func TestValidateDatabaseURLAllowsOnlyFixedSingleServerDatabase(t *testing.T) {
	t.Setenv("PEERGO_DEPLOYMENT_MODE", "single-server")
	if err := validateDatabaseURL("postgres://peergo_vault:secret@postgresql:5432/peergo_vault?sslmode=disable", "production"); err != nil {
		t.Fatalf("single-server Vault database rejected: %v", err)
	}
	if err := validateDatabaseURL("postgres://peergo_vault:secret@other-db:5432/peergo_vault?sslmode=disable", "production"); err == nil {
		t.Fatal("single-server accepted another clear-text Vault database")
	}
}
