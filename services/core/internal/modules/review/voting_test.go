package review

import "testing"

func TestResolveRoundUsesAsymmetricPtYesVotingRule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		approve, reject int
		want            RoundOutcome
	}{
		{name: "one vote waits", approve: 1, reject: 0, want: RoundWaiting},
		{name: "three approvals publish", approve: 3, reject: 0, want: RoundPublished},
		{name: "three rejections reject", approve: 0, reject: 3, want: RoundRejected},
		{name: "rejection majority rejects", approve: 1, reject: 2, want: RoundRejected},
		{name: "approval majority asks fourth", approve: 2, reject: 1, want: RoundWaiting},
		{name: "fourth approval publishes", approve: 3, reject: 1, want: RoundPublished},
		{name: "fourth split escalates", approve: 2, reject: 2, want: RoundEscalated},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveRound(test.approve, test.reject); got != test.want {
				t.Fatalf("resolveRound(%d, %d) = %q, want %q", test.approve, test.reject, got, test.want)
			}
		})
	}
}
