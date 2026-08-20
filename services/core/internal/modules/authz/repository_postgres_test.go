package authz

import "testing"

func TestDecodeConstraintsRejectsUnknownOrUnboundedValues(t *testing.T) {
	t.Parallel()

	for _, encoded := range []string{
		`{"unknown":true}`,
		`{"mfa_max_age_seconds":86401}`,
		`{"mfa_max_age_seconds":-1}`,
		`{} {}`,
	} {
		if _, err := decodeConstraints(encoded); err == nil {
			t.Fatalf("decodeConstraints(%q) error = nil", encoded)
		}
	}

	constraints, err := decodeConstraints(`{"purpose_required":true,"case_required":true,"mfa_max_age_seconds":300}`)
	if err != nil {
		t.Fatalf("decodeConstraints() error = %v", err)
	}
	if !constraints.PurposeRequired || !constraints.CaseRequired || constraints.MFAMaxAgeSeconds != 300 {
		t.Fatalf("constraints = %+v", constraints)
	}
}
