package delivery

import (
	"errors"
	"fmt"
	"html/template"
	"net/mail"
	"net/url"
	"strings"
	texttemplate "text/template"
	"time"
)

type Template string

const (
	TemplateVerification     Template = "peergo-email-verification-v1"
	TemplatePasswordRecovery Template = "peergo-password-recovery-v1"
	TemplateDeliveryTest     Template = "peergo-delivery-test-v1"
)

type Request struct {
	Template  Template  `json:"template"`
	Recipient string    `json:"recipient"`
	ActionURL string    `json:"action_url,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

type Message struct {
	Recipient string
	Subject   string
	TextBody  string
	HTMLBody  string
}

type Renderer struct {
	siteName string
}

func NewRenderer(siteName string) (*Renderer, error) {
	siteName = strings.TrimSpace(siteName)
	if siteName == "" || strings.ContainsAny(siteName, "\r\n") {
		return nil, errors.New("email site name is invalid")
	}
	return &Renderer{siteName: siteName}, nil
}

func (renderer *Renderer) Render(request Request) (Message, error) {
	parsedRecipient, err := mail.ParseAddress(request.Recipient)
	if err != nil || parsedRecipient.Address != request.Recipient {
		return Message{}, errors.New("recipient must be one bare email address")
	}
	data := struct {
		SiteName  string
		ActionURL string
		ExpiresAt string
	}{SiteName: renderer.siteName, ActionURL: request.ActionURL, ExpiresAt: request.ExpiresAt.UTC().Format("2006-01-02 15:04 UTC")}

	var subject, textSource, htmlSource string
	switch request.Template {
	case TemplateVerification:
		if err := validateAction(request); err != nil {
			return Message{}, err
		}
		subject = fmt.Sprintf("验证你的 %s 邮箱", renderer.siteName)
		textSource = "你正在验证 {{.SiteName}} 账户邮箱。\n\n打开链接：{{.ActionURL}}\n\n链接有效至 {{.ExpiresAt}}。如果不是你本人操作，请忽略此邮件。"
		htmlSource = `<h1>验证邮箱</h1><p>你正在验证 <strong>{{.SiteName}}</strong> 账户邮箱。</p><p><a href="{{.ActionURL}}">验证邮箱</a></p><p>链接有效至 {{.ExpiresAt}}。如果不是你本人操作，请忽略此邮件。</p>`
	case TemplatePasswordRecovery:
		if err := validateAction(request); err != nil {
			return Message{}, err
		}
		subject = fmt.Sprintf("重置你的 %s 密码", renderer.siteName)
		textSource = "有人请求重置你的 {{.SiteName}} 账户密码。\n\n打开链接：{{.ActionURL}}\n\n链接有效至 {{.ExpiresAt}}。如果不是你本人操作，请忽略此邮件。"
		htmlSource = `<h1>重置密码</h1><p>有人请求重置你的 <strong>{{.SiteName}}</strong> 账户密码。</p><p><a href="{{.ActionURL}}">重置密码</a></p><p>链接有效至 {{.ExpiresAt}}。如果不是你本人操作，请忽略此邮件。</p>`
	case TemplateDeliveryTest:
		if request.ActionURL != "" || !request.ExpiresAt.IsZero() {
			return Message{}, errors.New("delivery test must not carry an action link")
		}
		subject = fmt.Sprintf("%s 邮件投递测试", renderer.siteName)
		textSource = "这是一封 {{.SiteName}} 邮件投递测试。收到此邮件表示 Vault、HTTPS Relay 和 SMTP 链路可以正常投递。"
		htmlSource = `<h1>邮件投递测试</h1><p>这是一封 <strong>{{.SiteName}}</strong> 邮件投递测试。</p><p>收到此邮件表示 Vault、HTTPS Relay 和 SMTP 链路可以正常投递。</p>`
	default:
		return Message{}, errors.New("unsupported email template")
	}
	textBody, err := executeText(textSource, data)
	if err != nil {
		return Message{}, err
	}
	htmlBody, err := executeHTML(htmlSource, data)
	if err != nil {
		return Message{}, err
	}
	return Message{Recipient: request.Recipient, Subject: subject, TextBody: textBody, HTMLBody: htmlBody}, nil
}

func validateAction(request Request) error {
	parsed, err := url.Parse(request.ActionURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment == "" || request.ExpiresAt.IsZero() {
		return errors.New("transactional action URL must be absolute HTTPS with a fragment and expiry")
	}
	return nil
}

func executeText(source string, data any) (string, error) {
	parsed, err := texttemplate.New("mail").Parse(source)
	if err != nil {
		return "", err
	}
	var output strings.Builder
	if err := parsed.Execute(&output, data); err != nil {
		return "", err
	}
	return output.String(), nil
}

func executeHTML(source string, data any) (string, error) {
	parsed, err := template.New("mail").Parse(source)
	if err != nil {
		return "", err
	}
	var output strings.Builder
	if err := parsed.Execute(&output, data); err != nil {
		return "", err
	}
	return output.String(), nil
}
