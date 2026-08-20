package clientpolicy

import (
	"testing"

	"github.com/peergo/peergo/contracts/go/trackerruntimepolicyv1"
)

func TestAllowedUsesCuratedFingerprintAndMinimum(t *testing.T) {
	policy := trackerruntimepolicyv1.Policy{
		ClientMode: trackerruntimepolicyv1.ClientModeAllowList,
		AllowedClients: []trackerruntimepolicyv1.ClientRule{{
			Family: trackerruntimepolicyv1.ClientFamilyQBittorrent, MinVersion: "4.6.4",
		}},
	}
	var accepted, old, unknown [20]byte
	copy(accepted[:], "-qB4640-abcdefghijkl")
	copy(old[:], "-qB4530-abcdefghijkl")
	copy(unknown[:], "-XX9999-abcdefghijkl")
	if !Allowed(policy, accepted) || Allowed(policy, old) || Allowed(policy, unknown) {
		t.Fatal("client policy did not enforce the curated family and minimum version")
	}
}

func TestAllowAllKeepsMigratedClientsCompatible(t *testing.T) {
	var peerID [20]byte
	copy(peerID[:], "legacy-client-id-0000")
	if !Allowed(trackerruntimepolicyv1.Policy{ClientMode: trackerruntimepolicyv1.ClientModeAllowAll}, peerID) {
		t.Fatal("allow-all policy rejected an unrecognised migrated client")
	}
}
