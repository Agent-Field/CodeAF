package serve

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"strings"
)

// SMTPMailer is the Config.SendMail of a server told how to send mail:
// raw is "smtp://user:password@host:587" (STARTTLS when the server offers
// it, as net/smtp does) and from the address the invites come from. Empty raw
// is no mailer, which is nil and no error: invites are then links to copy.
func SMTPMailer(raw, from string) (func(ctx context.Context, to, subject, body string) error, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "smtp" || u.Hostname() == "" {
		return nil, errors.New(`the mail server must be written smtp://user:password@host:port`)
	}
	sender, err := mail.ParseAddress(from)
	if err != nil {
		return nil, fmt.Errorf("the address mail comes from is not an email address: %w", err)
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "587")
	}
	var auth smtp.Auth
	if u.User != nil {
		password, _ := u.User.Password()
		auth = smtp.PlainAuth("", u.User.Username(), password, u.Hostname())
	}
	return func(_ context.Context, to, subject, body string) error {
		if strings.ContainsAny(to+subject, "\r\n") {
			return errors.New("a mail header cannot hold a line break")
		}
		msg := "From: " + sender.String() + "\r\nTo: " + to + "\r\nSubject: " + subject +
			"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" +
			strings.ReplaceAll(body, "\n", "\r\n")
		return smtp.SendMail(host, auth, sender.Address, []string{to}, []byte(msg))
	}, nil
}
