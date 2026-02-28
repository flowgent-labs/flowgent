package notifier

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
)

// EmailSender sends notifications via SMTP.
type EmailSender struct {
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	UseTLS   bool   `json:"use_tls"`
}

func (s *EmailSender) Type() string { return "email" }

func (s *EmailSender) Validate() error {
	if s.SMTPHost == "" {
		return fmt.Errorf("email: smtp_host is required")
	}
	if s.From == "" {
		return fmt.Errorf("email: from address is required")
	}
	return nil
}

func (s *EmailSender) Send(ctx context.Context, recipient, title, body string) error {
	to := recipient
	port := s.SMTPPort
	if port == 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", s.SMTPHost, port)

	msg := buildEmail(s.From, to, title, body)
	var auth smtp.Auth
	if s.Username != "" {
		auth = smtp.PlainAuth("", s.Username, s.Password, s.SMTPHost)
	}

	// Check context before dialing
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if s.UseTLS {
		return fmt.Errorf("email: TLS not yet implemented; set use_tls=false for STARTTLS")
	}
	return smtp.SendMail(addr, auth, s.From, []string{to}, []byte(msg))
}

func buildEmail(from, to, subject, body string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("From: %s\r\n", from))
	b.WriteString(fmt.Sprintf("To: %s\r\n", to))
	b.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.String()
}
