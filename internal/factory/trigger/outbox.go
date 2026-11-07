package trigger

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/redact"
)

// OutboxDir is where deliveries wait, one file each, in the floor's folder.
const OutboxDir = "trigger-outbox"

// The delivery schedule. A failed delivery waits FirstRetry, then twice as
// long each time, up to MaxRetryWait, and is given up only after GiveUpAfter
// with no 2xx, so a callback that is down for a weekend still hears everything
// when it comes back, and one that is gone for good stops filling the disk.
// A webhook that cannot be read right now (its file is busy or broken) is
// retried the same way rather than dropped.
const (
	FirstRetry   = 2 * time.Second
	MaxRetryWait = time.Hour
	GiveUpAfter  = 72 * time.Hour
	// deliverWait is the most one attempt may take.
	deliverWait = 15 * time.Second
	// idleWait is how often the loop looks again with nothing due.
	idleWait = 30 * time.Second
)

// Body is one delivery's JSON body.
type Body struct {
	ID         string    `json:"id"`
	Event      string    `json:"event"`
	At         time.Time `json:"at"`
	Source     string    `json:"source"`
	ExternalID string    `json:"external_id"`
	Job        int       `json:"job"`
	Text       string    `json:"text"`
	URL        string    `json:"url,omitempty"`
}

// delivery is one webhook waiting to be sent, as its file keeps it. The body
// is fixed when it is queued, so every attempt sends the same bytes under the
// same id; the signature is made fresh per attempt, with that attempt's time.
type delivery struct {
	ID        string          `json:"id"`
	Source    string          `json:"source"`
	Event     string          `json:"event"`
	Body      json.RawMessage `json:"body"`
	Created   time.Time       `json:"created"`
	Attempts  int             `json:"attempts"`
	Next      time.Time       `json:"next"`
	LastError string          `json:"last_error,omitempty"`
}

// Outbox keeps deliveries on disk and sends them. IT IS DURABLE: a delivery
// is a file before [Outbox.Enqueue] returns and is removed only after a 2xx
// (or when its webhook is removed, or it is older than [GiveUpAfter]), so a
// restart loses nothing and sends what was waiting.
type Outbox struct {
	Dir      string
	Webhooks Webhooks
	// HTTP sends deliveries; nil is a client that follows no redirects.
	HTTP *http.Client
	// Now is the clock; nil is time.Now.
	Now  func() time.Time
	Logf func(format string, args ...any)

	mu   sync.Mutex // one pass at a time
	kick chan struct{}
	once sync.Once
}

// OutboxAt is the outbox in the floor folder root.
func OutboxAt(root string) *Outbox {
	return &Outbox{Dir: filepath.Join(root, OutboxDir), Webhooks: WebhooksAt(root)}
}

func (o *Outbox) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o *Outbox) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

func (o *Outbox) kicker() chan struct{} {
	o.once.Do(func() { o.kick = make(chan struct{}, 1) })
	return o.kick
}

// Enqueue keeps w for source and wakes the sender. w.ID and w.At are filled
// here when empty.
func (o *Outbox) Enqueue(source string, w Body) (string, error) {
	if w.ID == "" {
		id, err := deliveryID()
		if err != nil {
			return "", err
		}
		w.ID = id
	}
	now := o.now()
	if w.At.IsZero() {
		w.At = now.UTC()
	}
	w.Source = source
	body, err := json.Marshal(w)
	if err != nil {
		return "", err
	}
	d := delivery{ID: w.ID, Source: source, Event: w.Event, Body: body, Created: now, Next: now}
	if err := o.keep(d); err != nil {
		return "", err
	}
	select {
	case o.kicker() <- struct{}{}:
	default:
	}
	return w.ID, nil
}

// Pending is how many deliveries are waiting.
func (o *Outbox) Pending() int {
	list, _ := o.load()
	return len(list)
}

// Run sends what is due until ctx ends: at once, whenever something is
// queued, and when the earliest retry comes due.
func (o *Outbox) Run(ctx context.Context) {
	kick := o.kicker()
	for {
		wait := o.Pass(ctx)
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-kick:
			t.Stop()
		case <-t.C:
		}
	}
}

// Pass sends every delivery that is due, once, and answers how long until
// the next one is.
func (o *Outbox) Pass(ctx context.Context) time.Duration {
	o.mu.Lock()
	defer o.mu.Unlock()
	list, err := o.load()
	if err != nil {
		o.logf("factory: the webhook outbox cannot be read: %s", redact.Secrets(err.Error()))
		return idleWait
	}
	wait := idleWait
	for _, d := range list {
		if ctx.Err() != nil {
			return wait
		}
		now := o.now()
		if d.Next.After(now) {
			wait = min(wait, d.Next.Sub(now))
			continue
		}
		hook, err := o.Webhooks.Get(d.Source)
		if errors.Is(err, ErrNotFound) {
			// The webhook was removed: nobody to tell.
			o.drop(d)
			continue
		}
		if err == nil {
			err = o.send(ctx, hook, d)
		}
		if err == nil {
			o.drop(d)
			continue
		} else {
			d.LastError = redact.Secrets(err.Error())
		}
		d.Attempts++
		if now.Sub(d.Created) >= GiveUpAfter {
			o.logf("factory: gave up telling %s about %s after %d tries: %s", d.Source, d.Event, d.Attempts, d.LastError)
			o.drop(d)
			continue
		}
		d.Next = now.Add(retryWait(d.Attempts))
		wait = min(wait, d.Next.Sub(now))
		if err := o.keep(d); err != nil {
			o.logf("factory: the webhook outbox cannot keep a retry: %s", redact.Secrets(err.Error()))
		}
	}
	return max(wait, 10*time.Millisecond)
}

// retryWait is the wait after attempts failures.
func retryWait(attempts int) time.Duration {
	d := FirstRetry
	for i := 1; i < attempts && d < MaxRetryWait; i++ {
		d *= 2
	}
	return min(d, MaxRetryWait)
}

// send is one attempt: the body, signed now, to the source's webhook.
func (o *Outbox) send(ctx context.Context, hook Webhook, d delivery) error {
	ctx, cancel := context.WithTimeout(ctx, deliverWait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, bytes.NewReader(d.Body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "codeaf-factory")
	req.Header.Set(SignatureHeader, Sign(hook.Secret, d.Body, o.now()))
	req.Header.Set(DeliveryHeader, d.ID)
	req.Header.Set(EventHeader, d.Event)
	hc := o.HTTP
	if hc == nil {
		hc = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("the webhook answered %s", resp.Status)
	}
	return nil
}

func (o *Outbox) file(id string) string { return filepath.Join(o.Dir, id+".json") }

func (o *Outbox) keep(d delivery) error {
	if err := os.MkdirAll(o.Dir, 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(d)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(o.Dir, ".d-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(body); err != nil {
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
	return os.Rename(f.Name(), o.file(d.ID))
}

func (o *Outbox) drop(d delivery) { _ = os.Remove(o.file(d.ID)) }

// load is every waiting delivery, oldest first. A file that cannot be read
// is left where it is and skipped, so one bad file cannot stop the rest.
func (o *Outbox) load() ([]delivery, error) {
	entries, err := os.ReadDir(o.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []delivery
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(o.Dir, e.Name()))
		if err != nil {
			continue
		}
		var d delivery
		if json.Unmarshal(raw, &d) != nil || d.ID == "" {
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out, nil
}

func deliveryID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("could not name the delivery: %w", err)
	}
	return "dlv_" + hex.EncodeToString(b[:]), nil
}
