// Package credits reads the default model service's account balance without
// sending a completion or keeping a dollar amount on disk.
package credits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/providerhttp"
)

// LowCreditsUSD covers one worst-case turn on the shipped DeepSeek default:
// roughly $0.04 of input and $0.30 of output at its published limits, rounded up.
const LowCreditsUSD = 0.50

// Reading says whether the balance is known and below the one threshold — or,
// when the service refused the key as expired, that the key itself is the
// answer. An expired reading is Known and never Low: there is no balance to be
// low, and the free defaults would fail on the same key.
type Reading struct{ Known, Low, Expired bool }

// expiredWord is the one word the service's 401 carries when the key has run
// out rather than never been valid: OpenRouter answers `API key expired.` in the
// body and `error_description="API key expired"` in WWW-Authenticate. Any other
// 401 stays a failed read, because a key that was never accepted is already
// answered by the turn's own auth failure.
const expiredWord = "expired"

// Detail is what the service reported, as numbers. A figure the service did
// not send is nil, never zero; a figure that is negative is the account's real
// state and is kept as sent.
type Detail struct {
	// Expired is the service refusing the key as expired; no figure comes with it.
	Expired bool
	// AccountRemaining is total_credits minus total_usage, only when the service
	// reported both.
	AccountRemaining *float64
	// KeyLimitRemaining is the key's own spending cap left, only when the key
	// has one and the service said how much of it remains.
	KeyLimitRemaining *float64
}

// Known reports whether the service said anything about the balance: a figure
// or an expired key.
func (d Detail) Known() bool {
	return d.Expired || d.AccountRemaining != nil || d.KeyLimitRemaining != nil
}

// Remaining is the effective balance: the lesser of the account's and the key's
// when both are reported, whichever is reported when only one is, and false
// when neither is.
func (d Detail) Remaining() (float64, bool) {
	switch {
	case d.AccountRemaining != nil && d.KeyLimitRemaining != nil:
		return math.Min(*d.AccountRemaining, *d.KeyLimitRemaining), true
	case d.AccountRemaining != nil:
		return *d.AccountRemaining, true
	case d.KeyLimitRemaining != nil:
		return *d.KeyLimitRemaining, true
	}
	return 0, false
}

// StatusError is a service answer that was neither a reading nor a refusal the
// reader understands. Its words are the ones Read has always used.
type StatusError struct {
	Status int
	// Kind is what the answer means for a balance, decided where it was read.
	Kind Kind
	what string
}

func (e *StatusError) Error() string { return fmt.Sprintf("%s answered %d", e.what, e.Status) }

// newStatusError names what a status means to a balance read, once, here where
// the status is read: a refused key, a service that is failing or limiting, or
// an answer this reader does not know.
func newStatusError(code int, what string) *StatusError {
	kind := KindUnexpected
	switch {
	case code == http.StatusUnauthorized:
		kind = KindKeyRejected
	case code == http.StatusRequestTimeout, code == http.StatusTooManyRequests, code >= 500:
		kind = KindProvider
	}
	return &StatusError{Status: code, Kind: kind, what: what}
}

// Errors a reader names so a caller can tell a bad answer from a missing one
// without parsing words.
var (
	ErrTooLarge = errors.New("credit response too large")
	ErrInvalid  = errors.New("invalid credit balance")
	errDecode   = errors.New("credit response could not be decoded")
)

// Read asks for the account balance and the key's own cap. An absent number is
// unknown; a failed request is an error and must not change a previous reading.
// A 401 that says the key has expired is not a failed request: it is a reading,
// and the one fact the account can still tell us.
func Read(ctx context.Context, client *http.Client, baseURL, key string) (Reading, error) {
	d, err := ReadDetail(ctx, client, baseURL, key)
	if err != nil {
		return Reading{}, err
	}
	if d.Expired {
		return Reading{Known: true, Expired: true}, nil
	}
	remaining, ok := d.Remaining()
	if !ok {
		return Reading{}, nil
	}
	return Reading{Known: true, Low: remaining <= LowCreditsUSD}, nil
}

// ReadDetail is Read's one implementation, answering the amounts instead of the
// verdict: at most the key's own row and the account's credits, each read with
// a 64 KiB cap, under one ten-second deadline. A reported figure that is not
// finite makes the whole answer invalid rather than a guess.
func ReadDetail(ctx context.Context, client *http.Client, baseURL, key string) (Detail, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if client == nil {
		client = http.DefaultClient
	}
	baseURL = strings.TrimRight(baseURL, "/")
	get := func(path string) ([]byte, int, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Accept", "application/json")
		resp, err := providerhttp.Do(client, req)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
		if err != nil {
			return nil, 0, err
		}
		if len(body) > 64<<10 {
			return nil, 0, ErrTooLarge
		}
		return body, resp.StatusCode, nil
	}
	keyBody, keyStatus, err := get("/key")
	if err != nil {
		return Detail{}, err
	}
	if keyStatus == http.StatusNotFound {
		keyBody, keyStatus, err = get("/auth/key")
		if err != nil {
			return Detail{}, err
		}
	}
	if keyExpired(keyStatus, keyBody) {
		return Detail{Expired: true}, nil
	}
	var detail Detail
	switch keyStatus {
	case http.StatusOK:
		var row struct {
			Data struct {
				Remaining *float64 `json:"limit_remaining"`
			} `json:"data"`
		}
		if err := json.Unmarshal(keyBody, &row); err != nil {
			return Detail{}, fmt.Errorf("decode key credits: %w", errors.Join(errDecode, err))
		}
		detail.KeyLimitRemaining = row.Data.Remaining
	case http.StatusForbidden, http.StatusNotFound:
	default:
		return Detail{}, newStatusError(keyStatus, "key credits")
	}
	creditsBody, creditsStatus, err := get("/credits")
	if err != nil {
		return Detail{}, err
	}
	if keyExpired(creditsStatus, creditsBody) {
		return Detail{Expired: true}, nil
	}
	switch creditsStatus {
	case http.StatusOK:
		var row struct {
			Data struct {
				Total *float64 `json:"total_credits"`
				Usage *float64 `json:"total_usage"`
			} `json:"data"`
		}
		if err := json.Unmarshal(creditsBody, &row); err != nil {
			return Detail{}, fmt.Errorf("decode account credits: %w", errors.Join(errDecode, err))
		}
		if row.Data.Total != nil && row.Data.Usage != nil {
			remaining := *row.Data.Total - *row.Data.Usage
			detail.AccountRemaining = &remaining
		}
	case http.StatusForbidden, http.StatusNotFound:
	default:
		return Detail{}, newStatusError(creditsStatus, "account credits")
	}
	for _, f := range []*float64{detail.AccountRemaining, detail.KeyLimitRemaining} {
		if f != nil && (math.IsNaN(*f) || math.IsInf(*f, 0)) {
			return Detail{}, ErrInvalid
		}
	}
	return detail, nil
}

// keyExpired reports whether an answer is the service refusing the key as
// expired: a 401 whose body says so.
func keyExpired(status int, body []byte) bool {
	switch status {
	case http.StatusUnauthorized:
		return strings.Contains(strings.ToLower(string(body)), expiredWord)
	}
	return false
}

// Reason names the event that asked for a balance. A launch or changed key is
// never debounced; repeated refusals and paid-model switches share a quiet span.
type Reason uint8

const (
	Launch Reason = iota
	KeyChanged
	Refusal
	PaidSwitch
)

// Trigger permits one read at a time and coalesces bursts after it finishes.
type Trigger struct {
	mu        sync.Mutex
	now       func() time.Time
	busy      bool
	completed time.Time
}

func NewTrigger(now func() time.Time) *Trigger { return &Trigger{now: now} }

func (t *Trigger) Begin(reason Reason) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.busy {
		return false
	}
	if reason == Refusal || reason == PaidSwitch {
		if !t.completed.IsZero() && t.now().Sub(t.completed) < 30*time.Second {
			return false
		}
	}
	t.busy = true
	return true
}

func (t *Trigger) End() { t.mu.Lock(); t.busy = false; t.completed = t.now(); t.mu.Unlock() }
