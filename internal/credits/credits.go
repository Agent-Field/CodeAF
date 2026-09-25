// Package credits reads the default model service's account balance without
// sending a completion or keeping a dollar amount on disk.
package credits

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

// LowCreditsUSD covers one worst-case turn on the shipped DeepSeek default:
// roughly $0.04 of input and $0.30 of output at its published limits, rounded up.
const LowCreditsUSD = 0.50

// Reading says only whether the balance is known and below the one threshold.
type Reading struct{ Known, Low bool }

// Read asks for the account balance and the key's own cap. An absent number is
// unknown; a failed request is an error and must not change a previous reading.
func Read(ctx context.Context, client *http.Client, baseURL, key string) (Reading, error) {
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
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
		if err != nil {
			return nil, 0, err
		}
		if len(body) > 64<<10 {
			return nil, 0, fmt.Errorf("credit response too large")
		}
		return body, resp.StatusCode, nil
	}
	keyBody, keyStatus, err := get("/key")
	if err != nil {
		return Reading{}, err
	}
	if keyStatus == http.StatusNotFound {
		keyBody, keyStatus, err = get("/auth/key")
		if err != nil {
			return Reading{}, err
		}
	}
	var capValue *float64
	switch keyStatus {
	case http.StatusOK:
		var row struct {
			Data struct {
				Remaining *float64 `json:"limit_remaining"`
			} `json:"data"`
		}
		if err := json.Unmarshal(keyBody, &row); err != nil {
			return Reading{}, fmt.Errorf("decode key credits: %w", err)
		}
		capValue = row.Data.Remaining
	case http.StatusForbidden, http.StatusNotFound:
	default:
		return Reading{}, fmt.Errorf("key credits answered %d", keyStatus)
	}
	creditsBody, creditsStatus, err := get("/credits")
	if err != nil {
		return Reading{}, err
	}
	var account *float64
	switch creditsStatus {
	case http.StatusOK:
		var row struct {
			Data struct {
				Total *float64 `json:"total_credits"`
				Usage *float64 `json:"total_usage"`
			} `json:"data"`
		}
		if err := json.Unmarshal(creditsBody, &row); err != nil {
			return Reading{}, fmt.Errorf("decode account credits: %w", err)
		}
		if row.Data.Total != nil && row.Data.Usage != nil {
			remaining := *row.Data.Total - *row.Data.Usage
			account = &remaining
		}
	case http.StatusForbidden, http.StatusNotFound:
	default:
		return Reading{}, fmt.Errorf("account credits answered %d", creditsStatus)
	}
	if account == nil && capValue == nil {
		return Reading{}, nil
	}
	remaining := math.Inf(1)
	if account != nil {
		remaining = *account
	}
	if capValue != nil {
		remaining = math.Min(remaining, *capValue)
	}
	if math.IsNaN(remaining) || math.IsInf(remaining, 0) {
		return Reading{}, fmt.Errorf("invalid credit balance")
	}
	return Reading{Known: true, Low: remaining <= LowCreditsUSD}, nil
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
