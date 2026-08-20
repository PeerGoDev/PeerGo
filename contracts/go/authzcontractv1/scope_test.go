package authzcontractv1

import "testing"

func TestCanonicalSiteScope(t *testing.T) {
	if SiteScopeType != "site" || SiteScopeID != "peergo" {
		t.Fatalf("canonical site scope = %q:%q", SiteScopeType, SiteScopeID)
	}
}
