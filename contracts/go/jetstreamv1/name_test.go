package jetstreamv1

import "testing"

func TestValidLiteralNames(t *testing.T) {
	t.Parallel()
	if !ValidStreamName("PEERGO_SETTLEMENT_TRAFFIC_V1") || !ValidLiteralSubject("peergo.settlement.traffic.v1") {
		t.Fatal("valid literal names were rejected")
	}
	for _, invalid := range []string{"", "a.b", "a*", "a b"} {
		if ValidStreamName(invalid) {
			t.Fatalf("ValidStreamName(%q) = true", invalid)
		}
	}
	for _, invalid := range []string{"", ".a", "a.", "a..b", "a.*"} {
		if ValidLiteralSubject(invalid) {
			t.Fatalf("ValidLiteralSubject(%q) = true", invalid)
		}
	}
}
