package delivery

import (
	"strings"
	"testing"
	"time"
)

func TestSMTPEncodingUsesMultipartBodiesWithoutHeaderInjection(t *testing.T) {
	t.Parallel()
	sender, err := NewSMTPSender(SMTPSettings{
		Host: "smtp.example.test", Port: 587, Username: "mailer", Password: "secret",
		FromAddress: "noreply@example.test", FromName: "PeerGo", TLSMode: "starttls", Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := sender.encode(Message{Recipient: "member@example.test", Subject: "验证邮箱", TextBody: "text", HTMLBody: "<p>html</p>"})
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(payload)
	if !strings.Contains(encoded, "multipart/alternative") || !strings.Contains(encoded, "text/plain") || !strings.Contains(encoded, "text/html") {
		t.Fatalf("payload = %s", encoded)
	}
	if _, err := sender.encode(Message{Recipient: "member@example.test", Subject: "safe\r\nBcc: stolen@example.test", TextBody: "text", HTMLBody: "html"}); err == nil {
		t.Fatal("encode accepted a header break")
	}
}
