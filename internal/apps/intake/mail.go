package intake

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"golang.org/x/net/html/charset"
)

func parseEmail(raw []byte, attach bool) (map[string]string, []data.IntakeFile, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, BadInput("this file is not a readable email")
	}
	decoder := &mime.WordDecoder{CharsetReader: charset.NewReaderLabel}
	subject, err := decoder.DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || subject == "" {
		subject = msg.Header.Get("Subject")
	}
	from := msg.Header.Get("From")
	if senders, err := msg.Header.AddressList("From"); err == nil && len(senders) > 0 {
		from = senders[0].Address
	}
	parts := emailParts{attach: attach, remaining: data.IntakeBytesMax}
	if err := parts.read(textproto.MIMEHeader(msg.Header), msg.Body, 0); err != nil {
		return nil, nil, BadInput("this email has an unreadable or oversized MIME part")
	}
	body := strings.Join(parts.plain, "\n")
	if len(parts.plain) == 0 {
		body = strings.Join(parts.html, "\n")
	}
	return map[string]string{"subject": subject, "from": from, "body": body}, parts.files, nil
}

type emailParts struct {
	attach      bool
	remaining   int64
	count       int
	plain, html []string
	files       []data.IntakeFile
}

// read walks MIME parts with one shared decoded byte budget. A plain part
// takes precedence over HTML, whose source is kept as text.
func (p *emailParts) read(header textproto.MIMEHeader, r io.Reader, depth int) error {
	p.count++
	if depth > 12 || p.count > 128 {
		return fmt.Errorf("too many MIME parts")
	}
	kind, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if header.Get("Content-Type") == "" {
		kind, err = "text/plain", nil
	}
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(header.Get("Content-Transfer-Encoding"))) {
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, r)
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	}
	if strings.HasPrefix(kind, "multipart/") {
		m := multipart.NewReader(r, params["boundary"])
		for {
			part, err := m.NextRawPart()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			err = p.read(part.Header, part, depth+1)
			part.Close()
			if err != nil {
				return err
			}
		}
	}
	disposition, details, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
	name := details["filename"]
	if name == "" {
		name = params["name"]
	}
	if name != "" || disposition == "attachment" {
		if !p.attach {
			return nil
		}
		name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
		if !fileName(name) {
			name = fmt.Sprintf("attachment-%d", p.count)
		}
		content, err := p.take(r)
		if err != nil {
			return err
		}
		p.files = append(p.files, data.IntakeFile{Name: name, MIME: kind, Bytes: content})
		return nil
	}
	if kind != "text/plain" && kind != "text/html" {
		return nil
	}
	if encoding := params["charset"]; encoding != "" {
		r, err = charset.NewReaderLabel(encoding, r)
		if err != nil {
			return err
		}
	}
	content, err := p.take(r)
	if err != nil {
		return err
	}
	value := strings.ToValidUTF8(string(content), "\uFFFD")
	if kind == "text/plain" {
		p.plain = append(p.plain, value)
	} else {
		p.html = append(p.html, value)
	}
	return nil
}

func (p *emailParts) take(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, p.remaining+1))
	if err == nil && int64(len(b)) > p.remaining {
		err = fmt.Errorf("MIME parts are too large")
	}
	p.remaining -= int64(len(b))
	return b, err
}
