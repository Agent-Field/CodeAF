// Package trigger is the factory's Trigger API for any caller: the message and
// status doors ([Message], [Status]) that POST /api/intake/{source}/{id}/...
// and AgentField's `message` and `status` reasoners both call, and the signed
// webhook — one sink on the inbox's event bus ([inbox.RegisterSink]) that
// posts each moment of a job to the address its source registered, through a
// durable [Outbox] that retries until a 2xx.
//
// Filing stays inbox.Add, THE ONE DOOR. docs/design/factory-general/DESIGN.md
// ("Wave 2 — the Trigger API", "AgentField — the factory is a node on the
// control plane") is the contract this package fills.
package trigger

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

// WebhooksFile is where a factory keeps its webhooks, in the floor's folder.
// It is not a number, so the store never reads it as a job.
//
// THE SIGNING SECRET IS KEPT AS IT IS, because signing a delivery needs it:
// in this private (0600) file only, never in config.yaml, and shown once,
// when the webhook is added.
const WebhooksFile = "trigger-webhooks.json"

// The headers every delivery carries.
const (
	// SignatureHeader is `t=<unix seconds>,sha256=<hex>`: the HMAC-SHA256,
	// keyed by the webhook's secret, of `<t>.<body>`. The timestamp is inside
	// the signed bytes, so a delivery cannot be replayed under a fresh one.
	SignatureHeader = "X-Codeaf-Signature"
	// DeliveryHeader is the delivery's id, the body's `id`; a retry of one
	// delivery carries the same id.
	DeliveryHeader = "X-Codeaf-Delivery"
	// EventHeader is the body's `event`.
	EventHeader = "X-Codeaf-Event"
)

// Tolerance is how far a delivery's timestamp may be from the receiver's
// clock before [Verify] refuses it.
const Tolerance = 5 * time.Minute

// The webhook events, one per moment ([inbox.EventKind]).
const (
	EventFiled    = "job.filed"
	EventStarted  = "job.started"
	EventQuestion = "job.question"
	EventPROpened = "job.pr_opened"
	EventMerged   = "job.merged"
	EventStuck    = "job.stuck"
	EventDone     = "job.done"
	EventMessage  = "job.message"
)

// MaxURL is the longest webhook address kept, in bytes.
const MaxURL = 2048

// ErrNotFound is a source with no webhook.
var ErrNotFound = errors.New("no webhook is set for that source")

// sourceName is a source as inbox.Entry.Source spells one.
var sourceName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}(:[a-z0-9][a-z0-9._/-]{0,63})?$`)

// Webhook is one source's webhook as the file keeps it.
type Webhook struct {
	Source  string    `json:"source"`
	URL     string    `json:"url"`
	Secret  string    `json:"secret"`
	Created time.Time `json:"created"`
}

// Public is a webhook as a person reads it, never the secret.
type Public struct {
	Source  string    `json:"source"`
	URL     string    `json:"url"`
	Created time.Time `json:"created"`
}

// Public is w without its secret.
func (w Webhook) Public() Public { return Public{Source: w.Source, URL: w.URL, Created: w.Created} }

// Added is an add's answer: the webhook and its secret, shown this once.
type Added struct {
	Public
	Secret string `json:"secret"`
}

// CheckSource refuses a name no inbox entry can carry, and the factory's own
// origins, whose moments never reach a sink.
func CheckSource(name string) error {
	switch {
	case name == "":
		return errors.New("name the source, such as jira or inbox:ops")
	case name == string(factory.OriginForge) || name == string(factory.OriginTerminal) || name == string(factory.OriginChat):
		return fmt.Errorf("%q is the factory's own; its jobs are answered on GitHub or here", name)
	case !sourceName.MatchString(name):
		return errors.New("a source is a lowercase name, such as jira or inbox:ops")
	}
	return nil
}

// CheckURL is an http or https address with no password in it.
func CheckURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if raw == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(raw) > MaxURL || u.User != nil {
		return "", fmt.Errorf("the webhook address must be an http or https address of at most %d characters, with no password in it", MaxURL)
	}
	return raw, nil
}

// Webhooks is the webhooks file of one floor.
type Webhooks struct {
	Path string
}

// WebhooksAt is the file in the floor folder root.
func WebhooksAt(root string) Webhooks { return Webhooks{Path: filepath.Join(root, WebhooksFile)} }

// Load is every webhook. A missing file is none.
func (r Webhooks) Load() ([]Webhook, error) {
	raw, err := os.ReadFile(r.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct {
		Webhooks []Webhook `json:"webhooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("the webhooks file %s cannot be read: %w", r.Path, err)
	}
	return doc.Webhooks, nil
}

// Get is source's webhook.
func (r Webhooks) Get(source string) (Webhook, error) {
	all, err := r.Load()
	if err != nil {
		return Webhook{}, err
	}
	for _, w := range all {
		if w.Source == source {
			return w, nil
		}
	}
	return Webhook{}, ErrNotFound
}

// Add sets source's webhook to address with a fresh secret. A source that
// already had one has it replaced, and the old secret stops signing at once.
func (r Webhooks) Add(source, address string, now time.Time) (Added, error) {
	source = strings.TrimSpace(source)
	if err := CheckSource(source); err != nil {
		return Added{}, err
	}
	address, err := CheckURL(address)
	if err != nil {
		return Added{}, err
	}
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return Added{}, fmt.Errorf("could not mint a secret: %w", err)
	}
	row := Webhook{Source: source, URL: address, Secret: "whsec_" + hex.EncodeToString(b[:]), Created: now.UTC()}
	err = r.change(func(all []Webhook) ([]Webhook, error) {
		all = slices.DeleteFunc(all, func(w Webhook) bool { return w.Source == source })
		return append(all, row), nil
	})
	if err != nil {
		return Added{}, err
	}
	return Added{Public: row.Public(), Secret: row.Secret}, nil
}

// Remove forgets source's webhook; deliveries still waiting for it are dropped.
func (r Webhooks) Remove(source string) error {
	return r.change(func(all []Webhook) ([]Webhook, error) {
		i := slices.IndexFunc(all, func(w Webhook) bool { return w.Source == source })
		if i < 0 {
			return nil, ErrNotFound
		}
		return slices.Delete(all, i, i+1), nil
	})
}

// change rewrites the file under its lock, atomically and privately.
func (r Webhooks) change(edit func([]Webhook) ([]Webhook, error)) error {
	if err := os.MkdirAll(filepath.Dir(r.Path), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(r.Path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return fmt.Errorf("the webhooks file is busy: %w", err)
	}
	defer func() { _ = filelock.Unlock(lock) }()
	all, err := r.Load()
	if err != nil {
		return err
	}
	if all, err = edit(all); err != nil {
		return err
	}
	body, err := json.MarshalIndent(struct {
		Webhooks []Webhook `json:"webhooks"`
	}{all}, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(r.Path, body)
}

// writePrivate writes body to path through a temporary file in the same
// folder, so a reader sees the old file or the new one and never half.
func writePrivate(path string, body []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".trigger-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(body); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Sign is the SignatureHeader value for body sent at at, keyed by secret.
func Sign(secret string, body []byte, at time.Time) string {
	t := strconv.FormatInt(at.Unix(), 10)
	return "t=" + t + ",sha256=" + mac(secret, t, body)
}

func mac(secret, t string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(t + "."))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

// ErrSignature is every way a delivery can fail [Verify].
var ErrSignature = errors.New("the webhook signature does not match")

// Verify checks header against body with secret, and that its timestamp is
// within [Tolerance] of now. It is what a receiver written in Go does; the
// manual states the same steps for any other language.
func Verify(secret string, body []byte, header string, now time.Time) error {
	if secret == "" {
		return ErrSignature
	}
	var t, sum string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			t = v
		case "sha256":
			sum = v
		}
	}
	unix, err := strconv.ParseInt(t, 10, 64)
	if err != nil || sum == "" {
		return ErrSignature
	}
	if d := now.Sub(time.Unix(unix, 0)); d > Tolerance || d < -Tolerance {
		return fmt.Errorf("%w: its timestamp is more than %s from now", ErrSignature, Tolerance)
	}
	if !hmac.Equal([]byte(mac(secret, t, body)), []byte(sum)) {
		return ErrSignature
	}
	return nil
}
