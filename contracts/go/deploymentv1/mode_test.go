package deploymentv1

import "testing"

func TestParseDefaultsToCluster(t *testing.T) {
	for _, value := range []string{"", "  ", "cluster"} {
		mode, err := Parse(value)
		if err != nil || mode != Cluster {
			t.Fatalf("Parse(%q) = %q, %v", value, mode, err)
		}
	}
}

func TestParseRequiresExactSingleServerMode(t *testing.T) {
	mode, err := Parse(" single-server ")
	if err != nil || mode != SingleServer {
		t.Fatalf("Parse(single-server) = %q, %v", mode, err)
	}
	if _, err := Parse("single_server"); err == nil {
		t.Fatal("Parse(single_server) error = nil")
	}
}
