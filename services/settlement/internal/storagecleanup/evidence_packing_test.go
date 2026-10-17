package storagecleanup

import "testing"

func TestEvidencePackingKeepsMaintenanceCatchingUp(t *testing.T) {
	result := Result{EvidencePacked: 1}
	if result.Total() != 1 || !result.Saturated(5000) {
		t.Fatal("packing progress must keep the worker on its bounded backlog cadence")
	}
	if (Result{}).Saturated(5000) || result.Saturated(0) {
		t.Fatal("empty or invalid batches must not trigger catch-up")
	}
}
