package delivery

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

type SMTPSettings struct {
	Host        string
	Port        int
	Username    string
	Password    string
	FromAddress string
	FromName    string
	TLSMode     string
	Timeout     time.Duration
}

type SMTPSender struct {
	settings SMTPSettings
}

func NewSMTPSender(settings SMTPSettings) (*SMTPSender, error) {
	if settings.Host == "" || settings.Port < 1 || settings.Username == "" || settings.Password == "" ||
		settings.FromAddress == "" || (settings.TLSMode != "starttls" && settings.TLSMode != "implicit") || settings.Timeout <= 0 {
		return nil, errors.New("SMTP settings are incomplete")
	}
	return &SMTPSender{settings: settings}, nil
}

// Send establishes a fresh authenticated TLS session for each bounded
// transaction. This deliberately avoids an unbounded shared SMTP connection:
// providers may close idle sessions, and ambiguous reuse makes retry behavior
// harder to reason about.
func (sender *SMTPSender) Send(ctx context.Context, message Message) error {
	payload, err := sender.encode(message)
	if err != nil {
		return err
	}
	address := net.JoinHostPort(sender.settings.Host, fmt.Sprintf("%d", sender.settings.Port))
	dialer := &net.Dialer{Timeout: sender.settings.Timeout}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("connect SMTP provider: %w", err)
	}
	defer connection.Close()
	deadline := time.Now().Add(sender.settings.Timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return fmt.Errorf("bound SMTP connection: %w", err)
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: sender.settings.Host}
	if sender.settings.TLSMode == "implicit" {
		tlsConnection := tls.Client(connection, tlsConfig)
		if err := tlsConnection.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("establish implicit SMTP TLS: %w", err)
		}
		connection = tlsConnection
	}
	client, err := smtp.NewClient(connection, sender.settings.Host)
	if err != nil {
		return fmt.Errorf("open SMTP client: %w", err)
	}
	defer client.Close()
	if sender.settings.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP provider does not advertise STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("establish SMTP STARTTLS: %w", err)
		}
	}
	if err := client.Auth(smtp.PlainAuth("", sender.settings.Username, sender.settings.Password, sender.settings.Host)); err != nil {
		return fmt.Errorf("authenticate SMTP provider: %w", err)
	}
	if err := client.Mail(sender.settings.FromAddress); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(message.Recipient); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	data, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP message body: %w", err)
	}
	if _, err := data.Write(payload); err != nil {
		_ = data.Close()
		return fmt.Errorf("write SMTP message body: %w", err)
	}
	if err := data.Close(); err != nil {
		return fmt.Errorf("commit SMTP message body: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("complete SMTP transaction: %w", err)
	}
	return nil
}

func (sender *SMTPSender) encode(message Message) ([]byte, error) {
	if message.Recipient == "" || message.Subject == "" || message.TextBody == "" || message.HTMLBody == "" || strings.ContainsAny(message.Subject, "\r\n") {
		return nil, errors.New("SMTP message is incomplete")
	}
	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	writePart := func(contentType, content string) error {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Type", contentType+`; charset="UTF-8"`)
		header.Set("Content-Transfer-Encoding", "quoted-printable")
		part, err := multipartWriter.CreatePart(header)
		if err != nil {
			return err
		}
		encoded := quotedprintable.NewWriter(part)
		if _, err := encoded.Write([]byte(content)); err != nil {
			return err
		}
		return encoded.Close()
	}
	if err := writePart("text/plain", message.TextBody); err != nil {
		return nil, err
	}
	if err := writePart("text/html", message.HTMLBody); err != nil {
		return nil, err
	}
	if err := multipartWriter.Close(); err != nil {
		return nil, err
	}
	from := (&mail.Address{Name: sender.settings.FromName, Address: sender.settings.FromAddress}).String()
	to := (&mail.Address{Address: message.Recipient}).String()
	var output bytes.Buffer
	fmt.Fprintf(&output, "From: %s\r\n", from)
	fmt.Fprintf(&output, "To: %s\r\n", to)
	fmt.Fprintf(&output, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", message.Subject))
	output.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&output, "Content-Type: multipart/alternative; boundary=%q\r\n", multipartWriter.Boundary())
	output.WriteString("\r\n")
	output.Write(body.Bytes())
	return output.Bytes(), nil
}
