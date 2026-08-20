package newcomer

import "testing"

func TestValidPolicyRequiresBoundedDurationAndAtLeastOneEnabledTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		policy PolicyInput
		valid  bool
	}{
		{
			name: "enabled upload and seeding targets",
			policy: PolicyInput{
				Enabled: true, DurationSeconds: 30 * 86400,
				MinimumCreditedUploadBytes: 50 << 30, MinimumSeedingActiveSeconds: 72 * 3600,
			},
			valid: true,
		},
		{
			name: "disabled policy normalizes targets to zero",
			policy: PolicyInput{
				Enabled: false, DurationSeconds: 30 * 86400,
			},
			valid: true,
		},
		{
			name: "enabled without a target",
			policy: PolicyInput{
				Enabled: true, DurationSeconds: 30 * 86400,
			},
			valid: false,
		},
		{
			name: "disabled with hidden target",
			policy: PolicyInput{
				Enabled: false, DurationSeconds: 30 * 86400,
				MinimumCreditedUploadBytes: 1,
			},
			valid: false,
		},
		{
			name: "duration shorter than seven days",
			policy: PolicyInput{
				Enabled: true, DurationSeconds: 7*86400 - 1,
				MinimumSeedingActiveSeconds: 1,
			},
			valid: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := validPolicy(test.policy); got != test.valid {
				t.Fatalf("validPolicy() = %v, want %v", got, test.valid)
			}
		})
	}
}
