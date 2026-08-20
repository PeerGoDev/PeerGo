package delivery

import (
	"strings"
	"testing"
	"time"
)

func TestRendererProducesVersionedSafeMessages(t *testing.T) {
	t.Parallel()
	renderer, err := NewRenderer("PeerGo")
	if err != nil {
		t.Fatal(err)
	}
	message, err := renderer.Render(Request{
		Template: TemplateVerification, Recipient: "member@example.test",
		ActionURL: "https://peergo.example/verify-email#token=secret", ExpiresAt: time.Date(2026, 8, 19, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message.Subject, "PeerGo") || !strings.Contains(message.TextBody, "https://peergo.example") || !strings.Contains(message.HTMLBody, "验证邮箱") {
		t.Fatalf("message = %+v", message)
	}
	if _, err := renderer.Render(Request{Template: TemplateDeliveryTest, Recipient: "member@example.test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := renderer.Render(Request{Template: TemplateVerification, Recipient: "member@example.test", ActionURL: "http://peergo.example/#token=x", ExpiresAt: time.Now()}); err == nil {
		t.Fatal("renderer accepted a plaintext action URL")
	}
}
