// Package codeaftrigger is the Go client for a codeaf factory's Trigger API:
// the four calls another tool needs to hand work to a factory and hear back.
//
//   - [Client.File] files a piece of work (POST /api/intake).
//   - [Client.Message] says something more about it (POST /api/intake/{source}/{id}/messages).
//   - [Client.Status] reads how far it is (GET /api/intake/{source}/{id}).
//   - [VerifyWebhook] checks that a webhook the factory sent is really its.
//
// IT WRAPS ONLY THOSE CALLS. It imports nothing from codeaf's internal
// packages, so a program outside this repository can depend on it; the
// factory's own webhook sender signs with this package's [Sign], so the two
// sides cannot drift.
//
// A source's token is minted on the factory with
// `codeaf factory source add NAME --callback URL`, which also prints the
// webhook signing secret. That token can file, read and message only work
// whose source is NAME.
package codeaftrigger

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The headers every webhook delivery carries.
const (
	// SignatureHeader is `t=<unix seconds>,sha256=<hex>`: the HMAC-SHA256,
	// keyed by the source's signing secret, of `<t>.<body>`. The timestamp is
	// inside the signed bytes, so a delivery cannot be replayed later under a
	// fresh one.
	SignatureHeader = "X-Codeaf-Signature"
	// DeliveryHeader is the delivery's id, the same as the body's `id`. A
	// retry of one delivery carries the same id, so a receiver that keeps the
	// ids it has handled can drop a repeat.
	DeliveryHeader = "X-Codeaf-Delivery"
	// EventHeader is the body's `event`.
	EventHeader = "X-Codeaf-Event"
)

// Tolerance is how far a delivery's timestamp may be from the receiver's
// clock before [VerifyWebhook] refuses it.
const Tolerance = 5 * time.Minute

// The events a factory sends. Each is one moment in the life of a job that
// the source filed.
const (
	EventFiled    = "job.filed"     // the work became a job
	EventStarted  = "job.started"   // its first step began
	EventQuestion = "job.question"  // it stopped on a question for a person
	EventPROpened = "job.pr_opened" // it opened its pull request
	EventMerged   = "job.merged"    // its pull request merged
	EventStuck    = "job.stuck"     // it stopped and needs a person to look
	EventDone     = "job.done"      // it is finished
	EventMessage  = "job.message"   // the job's manager answered a message
)

// Entry is one piece of outside work. Source and ExternalID together are its
// identity: filing the same pair again updates the job it made.
type Entry struct {
	Source     string   `json:"source"`
	ExternalID string   `json:"external_id"`
	Title      string   `json:"title"`
	Body       string   `json:"body,omitempty"`
	URL        string   `json:"url,omitempty"`
	Author     string   `json:"author,omitempty"`
	Labels     []string `json:"labels,omitempty"`
	Priority   string   `json:"priority,omitempty"` // p0..p3
	Repo       string   `json:"repo,omitempty"`     // owner/name
	// Trust is collaborator or stranger. A source's token can never file
	// above the trust its source was registered with; empty takes that trust.
	Trust string `json:"trust,omitempty"`
}

// Filed is what filing did: the job's number, and whether it is new.
type Filed struct {
	ID      int  `json:"id"`
	Created bool `json:"created"`
}

// Accepted is what a message did: the job it reached.
type Accepted struct {
	Job      int  `json:"job"`
	Accepted bool `json:"accepted"`
}

// Status is how far a job is. State is one of the factory's job states
// (`new`, `queued`, `running`, `needs_you`, `ready`, `done`, …); Route is the
// routing rule that decided it, PRURL its pull request, Question what it is
// waiting to be told. Each is absent when there is none.
type Status struct {
	Job        int       `json:"job"`
	State      string    `json:"state"`
	StepsDone  int       `json:"steps_done"`
	StepsTotal int       `json:"steps_total"`
	Route      string    `json:"route,omitempty"`
	PRURL      string    `json:"pr_url,omitempty"`
	Question   string    `json:"question,omitempty"`
	Updated    time.Time `json:"updated"`
}

// Webhook is the body of one delivery.
type Webhook struct {
	ID         string    `json:"id"`
	Event      string    `json:"event"`
	At         time.Time `json:"at"`
	Source     string    `json:"source"`
	ExternalID string    `json:"external_id"`
	Job        int       `json:"job"`
	Text       string    `json:"text"`
	URL        string    `json:"url,omitempty"`
}

// Client calls one factory with one token.
type Client struct {
	// BaseURL is the factory's address, such as https://factory.example.com.
	BaseURL string
	// Token is a source's token, or the factory's own access token.
	Token string
	// HTTP is the transport; nil is a client with a 30-second timeout.
	HTTP *http.Client
}

// New is a client for the factory at baseURL holding token.
func New(baseURL, token string) *Client {
	return &Client{BaseURL: baseURL, Token: token}
}

// Error is a refusal in the factory's own words, with the HTTP status.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("the factory refused the call (HTTP %d)", e.Status)
	}
	return fmt.Sprintf("the factory refused the call (HTTP %d): %s", e.Status, e.Message)
}

// File files e, or updates the job the same source and id made before.
func (c *Client) File(ctx context.Context, e Entry) (Filed, error) {
	var out Filed
	return out, c.call(ctx, http.MethodPost, "/api/intake", e, &out)
}

// Message says text about the job filed as (source, id). It reaches that
// job's manager the way a comment on an issue does, held to the trust the
// work was filed with.
func (c *Client) Message(ctx context.Context, source, id, text string) (Accepted, error) {
	return c.MessageAs(ctx, source, id, "", text)
}

// MessageAs is [Client.Message] naming who said it on the source's side.
func (c *Client) MessageAs(ctx context.Context, source, id, author, text string) (Accepted, error) {
	var out Accepted
	body := struct {
		Author string `json:"author,omitempty"`
		Text   string `json:"text"`
	}{author, text}
	return out, c.call(ctx, http.MethodPost, jobPath(source, id)+"/messages", body, &out)
}

// Status reads how far the job filed as (source, id) is.
func (c *Client) Status(ctx context.Context, source, id string) (Status, error) {
	var out Status
	return out, c.call(ctx, http.MethodGet, jobPath(source, id), nil, &out)
}

func jobPath(source, id string) string {
	return "/api/intake/" + url.PathEscape(source) + "/" + url.PathEscape(id)
}

func (c *Client) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var refusal struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &refusal)
		return &Error{Status: resp.StatusCode, Message: refusal.Error}
	}
	return json.Unmarshal(data, out)
}

// Sign is the SignatureHeader value for body sent at at, keyed by secret.
func Sign(secret string, body []byte, at time.Time) string {
	t := strconv.FormatInt(at.Unix(), 10)
	return "t=" + t + ",sha256=" + mac(secret, t, body)
}

func mac(secret, t string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(t))
	m.Write([]byte("."))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

// ErrSignature is every way a delivery can fail [VerifyWebhook].
var ErrSignature = errors.New("the webhook signature does not match")

// VerifyWebhook checks header (the SignatureHeader value) against body with
// secret, and that its timestamp is within [Tolerance] of now.
func VerifyWebhook(secret string, body []byte, header string) error {
	return VerifyWebhookAt(secret, body, header, time.Now())
}

// VerifyWebhookAt is [VerifyWebhook] against the clock reading now.
func VerifyWebhookAt(secret string, body []byte, header string, now time.Time) error {
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

// ParseWebhook reads a delivery's body. Call it after [VerifyWebhook].
func ParseWebhook(body []byte) (Webhook, error) {
	var w Webhook
	err := json.Unmarshal(body, &w)
	return w, err
}
