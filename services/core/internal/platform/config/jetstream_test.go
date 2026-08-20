package config

import "testing"

func TestProjectionNATSAllowsOnlyFixedSingleServerNode(t *testing.T) {
	t.Setenv("PEERGO_DEPLOYMENT_MODE", "single-server")
	t.Setenv("TEST_NATS_URLS", "nats://peergo-nats:4222")
	if _, err := projectionNATSURLs("TEST_NATS_URLS", "production"); err != nil {
		t.Fatalf("fixed single-server NATS rejected: %v", err)
	}
	t.Setenv("TEST_NATS_URLS", "nats://other-nats:4222")
	if _, err := projectionNATSURLs("TEST_NATS_URLS", "production"); err == nil {
		t.Fatal("single-server accepted another clear-text NATS host")
	}
}
