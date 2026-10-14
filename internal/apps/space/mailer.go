package space

import (
	"context"
	"log"
	"net"
	"net/smtp"
	"os"
)

// Mailer delivers a one-time sign-in code. The workspace never talks to a
// mailbox except through this seam.
type Mailer interface {
	SendCode(ctx context.Context, email, code string) error
}

// LogMailer prints the code on the server log. A nil Log uses the process logger.
type LogMailer struct{ Log *log.Logger }

func (m LogMailer) SendCode(_ context.Context, email, code string) error {
	logger := m.Log
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf("space sign-in code for %s: %s", email, code)
	return nil
}

// SMTPMailer sends the code when CODEAF_SPACE_SMTP_ADDR is set.
type SMTPMailer struct {
	Addr, User, Password, From string
}

func (m SMTPMailer) SendCode(_ context.Context, email, code string) error {
	from := m.From
	if from == "" {
		from = m.User
	}
	body := []byte("From: " + from + "\r\nTo: " + email + "\r\nSubject: Your codeaf sign-in code\r\n\r\n" + code + "\r\n")
	var auth smtp.Auth
	if m.User != "" {
		host, _, err := net.SplitHostPort(m.Addr)
		if err != nil {
			return err
		}
		auth = smtp.PlainAuth("", m.User, m.Password, host)
	}
	return smtp.SendMail(m.Addr, auth, from, []string{email}, body)
}

// MailerFromEnv is the log mailer unless SMTP is configured.
func MailerFromEnv() Mailer {
	addr := os.Getenv("CODEAF_SPACE_SMTP_ADDR")
	if addr == "" {
		return LogMailer{}
	}
	return SMTPMailer{
		Addr:     addr,
		User:     os.Getenv("CODEAF_SPACE_SMTP_USER"),
		Password: os.Getenv("CODEAF_SPACE_SMTP_PASSWORD"),
		From:     os.Getenv("CODEAF_SPACE_SMTP_FROM"),
	}
}
