// Package intake maps declared, external deliveries onto the app data writer.
package intake

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/cell"
)

// Post is the shared wire request. Values are form fields or a connector's
// JSON object; a file delivery carries a basename and the original bytes.
type Post struct {
	App     string         `json:"app"`
	Channel string         `json:"channel"`
	Key     string         `json:"dedupe_key,omitempty"`
	Values  map[string]any `json:"values,omitempty"`
	Name    string         `json:"name,omitempty"`
	Content []byte         `json:"content,omitempty"`
}

type Channel struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Record string `json:"record"`
	Run    string `json:"run,omitempty"`
	Form   string `json:"form,omitempty"`
	URL    string `json:"url,omitempty"`
	Folder string `json:"folder,omitempty"`
	Active bool   `json:"active"`
}

type Definition struct {
	Manifest schema.App
	Version  int
}

func FromStored(saved store.Definition) (Definition, error) {
	var def Definition
	if saved.Meta.Archived || saved.Meta.Deleted {
		return def, Missing()
	}
	def.Version = saved.Meta.Version
	return def, json.Unmarshal(saved.App, &def.Manifest)
}

func BadInput(message string) *data.Error {
	return &data.Error{Code: data.CodeInvalid, Message: message}
}

func Missing() *data.Error {
	return &data.Error{Code: data.CodeNotFound, Message: "intake channel not found"}
}

func enabled(c schema.Channel) bool {
	switch c.Kind {
	case "form-link", "file-drop":
		return true
	case "email-in":
		return len(c.Mapping) > 0
	case "webhook-in", "connector-event":
		return c.Secret != "" && len(c.Mapping) > 0
	}
	return false
}

// Service loads the published declaration at each intake door. The data
// writer checks the version again when it commits the delivery.
type Service struct {
	Root      string
	Load      func(app string) (Definition, error)
	Store     func() (*data.Store, error)
	StartRuns func() error
}

func (s Service) channel(app, id string) (Definition, schema.Channel, error) {
	if !safeApp(app) || !safeName(id) {
		return Definition{}, schema.Channel{}, Missing()
	}
	def, err := s.Load(app)
	if err != nil {
		return def, schema.Channel{}, err
	}
	channel, ok := def.Manifest.Channels[id]
	if !ok || !enabled(channel) {
		return def, schema.Channel{}, Missing()
	}
	return def, channel, nil
}

func safeApp(id string) bool { return cell.ValidID(id) }

func safeName(name string) bool {
	return name != "" && len(name) <= 48 && strings.IndexFunc(name, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-')
	}) == -1
}

func (s Service) Post(p Post) (data.IntakeReceipt, error) {
	def, channel, err := s.channel(p.App, p.Channel)
	if err != nil {
		return data.IntakeReceipt{}, err
	}
	return s.deliver(def, channel, p)
}

func (s Service) deliver(def Definition, ch schema.Channel, p Post) (data.IntakeReceipt, error) {
	var empty data.IntakeReceipt
	if len(p.Content) > data.IntakeBytesMax {
		return empty, BadInput(fmt.Sprintf("the delivery exceeds %d bytes", data.IntakeBytesMax))
	}
	in := data.IntakeDelivery{App: p.App, Channel: p.Channel, Version: def.Version,
		Key: strings.TrimSpace(p.Key), Type: ch.Record, Run: ch.Run, Set: map[string]any{}}
	if err := prepareDelivery(&in, ch, p); err != nil {
		return empty, err
	}
	db, err := s.Store()
	if err != nil {
		return empty, err
	}
	if prior, found, err := db.IntakeReplay(in); found || err != nil {
		return prior, err
	}
	if err := mapDelivery(&in, def, ch, p); err != nil {
		return empty, err
	}
	if in.Run != "" && s.StartRuns != nil {
		if err := s.StartRuns(); err != nil {
			return empty, err
		}
	}
	return db.Deliver(in)
}

// Fingerprint the original delivery so an edited mapping still replays its receipt.
func prepareDelivery(in *data.IntakeDelivery, ch schema.Channel, p Post) error {
	digest := sha256.New()
	fmt.Fprintln(digest, ch.Kind)
	switch ch.Kind {
	case "email-in", "file-drop":
		if len(p.Values) != 0 || len(p.Content) == 0 || !fileName(p.Name) {
			return BadInput("send a file name and its contents")
		}
		if ch.Kind == "file-drop" {
			fmt.Fprintln(digest, p.Name)
		}
		digest.Write(p.Content)
		if in.Key == "" {
			in.Key = fmt.Sprintf("file:%x", digest.Sum(nil))
		}
	case "form-link", "webhook-in", "connector-event":
		if len(p.Content) != 0 || p.Name != "" || in.Key == "" || p.Values == nil {
			return BadInput("send values with a dedupe key")
		}
		if err := json.NewEncoder(digest).Encode(p.Values); err != nil {
			return BadInput("send valid JSON values")
		}
	}
	if len(in.Key) > 256 {
		return BadInput("the dedupe key must be at most 256 bytes")
	}
	in.Fingerprint = fmt.Sprintf("%x", digest.Sum(nil))
	return nil
}

func mapDelivery(in *data.IntakeDelivery, def Definition, ch schema.Channel, p Post) error {
	switch ch.Kind {
	case "form-link":
		form := def.Manifest.Process.Forms[ch.Form]
		in.Set, in.Form = p.Values, &form
		if ch.WorkItem != "" {
			work := def.Manifest.Process.WorkItems[ch.WorkItem]
			in.Work = &data.WorkItem{Title: work.Title, Assignee: work.Assignee, Form: work.Form, Due: work.Due, SLAAt: work.SLA}
		}
	case "email-in":
		if !strings.EqualFold(filepath.Ext(p.Name), ".eml") {
			return BadInput("drop an .eml email file")
		}
		message, files, err := parseEmail(p.Content, ch.Mapping["attachments"] != "")
		if err != nil {
			return err
		}
		for from, to := range ch.Mapping {
			if from != "attachments" {
				in.Set[to] = message[from]
			}
		}
		in.FileField, in.Files = ch.Mapping["attachments"], files
	case "file-drop":
		return mapFile(in, ch, p)
	case "webhook-in", "connector-event":
		for from, to := range ch.Mapping {
			if value, ok := p.Values[from]; ok {
				in.Set[to] = value
			}
		}
		if len(in.Set) == 0 {
			return BadInput("the event has no mapped fields")
		}
	}
	return nil
}

func fileName(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 255 && !strings.ContainsAny(name, "/\\\x00\r\n")
}

const csvMIME = "text/csv"

func mapFile(in *data.IntakeDelivery, ch schema.Channel, post Post) error {
	mime := http.DetectContentType(post.Content)
	if strings.EqualFold(filepath.Ext(post.Name), ".csv") {
		mime = csvMIME
	}
	if len(ch.Accept) > 0 && !slices.ContainsFunc(ch.Accept, func(allowed string) bool {
		return allowed == mime || strings.HasSuffix(allowed, "/*") && strings.HasPrefix(mime, strings.TrimSuffix(allowed, "*"))
	}) {
		return BadInput("this channel does not accept this file type")
	}
	switch {
	case mime == csvMIME:
		in.CSV, in.Mapping = post.Content, ch.Mapping
	case mime == "application/pdf" || strings.HasPrefix(mime, "image/"):
		if ch.FileField == "" {
			return BadInput("this channel needs an attachment field for PDFs and images")
		}
		if ch.NameField != "" {
			in.Set[ch.NameField] = post.Name
		}
		in.FileField = ch.FileField
		in.Files = []data.IntakeFile{{Name: post.Name, MIME: mime, Bytes: post.Content}}
	default:
		return BadInput("drop a CSV, PDF or image")
	}
	return nil
}

func publicMessage(err error) string {
	var e *data.Error
	if errors.As(err, &e) {
		return e.Message
	}
	return "could not accept this delivery"
}
