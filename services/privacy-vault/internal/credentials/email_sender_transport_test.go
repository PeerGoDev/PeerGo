package credentials

import (
	"strings"
	"testing"
	"time"
)

func TestRelaySenderPrivateHTTPExceptionIsExactAndExplicit(t *testing.T) {
	token := strings.Repeat("t", 32)
	endpoint := "http://email-relay:8086/internal/v1/deliveries/transactional"
	if _, err := NewRelayTransactionalEmailSender(endpoint, token, time.Second, false); err == nil {
		t.Fatal("default Relay constructor accepted clear-text transport")
	}
	if _, err := NewRelayTransactionalEmailSender(endpoint, token, time.Second, true); err != nil {
		t.Fatalf("fixed single-server Relay rejected: %v", err)
	}
	if _, err := NewRelayTransactionalEmailSender("http://other-relay:8086", token, time.Second, true); err == nil {
		t.Fatal("single-server Relay accepted another clear-text host")
	}
	if _, err := NewRelayTransactionalEmailSender("http://email-relay:8086/readyz", token, time.Second, true); err == nil {
		t.Fatal("single-server Relay accepted another clear-text path")
	}
}
