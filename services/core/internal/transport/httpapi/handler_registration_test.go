package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func TestRegistrationPolicySettingsDTOEncodesEmptyCollectionsAsArrays(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(registrationPolicySettingsDTO(identity.RegistrationPolicy{}))
	if err != nil {
		t.Fatalf("marshal registration settings: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("decode registration settings: %v", err)
	}

	for _, field := range []string{"reserved_usernames", "email_domains"} {
		value, ok := payload[field].([]any)
		if !ok {
			t.Fatalf("%s = %#v, want JSON array", field, payload[field])
		}
		if len(value) != 0 {
			t.Fatalf("%s = %#v, want empty JSON array", field, value)
		}
	}
}
