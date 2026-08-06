package router

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// Event is one routed attempt, written as it happened.
//
// This file is not telemetry for a dashboard; it is the substrate for the
// offline policy work the two labs could only simulate. Both of them had to
// reconstruct what a router *would* have done from an offline matrix, and the
// thing neither could recover was the counterfactual: which models were in the
// running when a choice was made. So the candidates are recorded alongside the
// choice, which is the one field that makes a logged decision analysable after
// the fact rather than merely auditable.
type Event struct {
	At    time.Time `json:"at"`
	Call  string    `json:"call"` // ties every row about one unit of work together
	Run   string    `json:"run"`  // the run cache key, which is a run identity
	Class string    `json:"class"`
	// Shape is the sub-population within the class the rating was keyed on —
	// which leaves this leaf was ordered against. Empty for an undivided class.
	Shape string `json:"shape,omitempty"`

	// Candidates is the ordered rung list the choice was made from, and Rung is
	// where in it Model sits. Escalation is what had already been tried and
	// failed before this attempt.
	Candidates []string `json:"candidates,omitempty"`
	Rung       int      `json:"rung"`
	Escalation []string `json:"escalation,omitempty"`

	Model string `json:"model"`
	// Resolved is the response's own model field. For a floating alias it is a
	// dated snapshot and it is what the ledger keys on, because a rating pooled
	// across two sets of weights measures neither.
	Resolved string `json:"resolved,omitempty"`

	// Explore marks an attempt the router took to buy evidence rather than
	// because it expected the best answer, and Propensity is the probability it
	// would have. The pair is what makes the log usable for offline policy work:
	// evidence gathered by a rule that chose it for its own reasons is biased in
	// the direction of the rule, and only a recorded propensity lets a later
	// estimator weight it back out. It is one number and it is written down at
	// the moment it applied, which is the only moment it can be known.
	Explore    bool    `json:"explore,omitempty"`
	Propensity float64 `json:"propensity,omitempty"`

	Verdict provider.Verdict `json:"verdict"`
	// Final marks the row that carries the settled verdict. An attempt is
	// written when it returns, before the call site has had a chance to check
	// the answer; when the check lands it is appended as a second, short row
	// against the same call id. The log is append-only, so a correction is an
	// append — a reader takes the last row for a call id as the truth.
	Final bool `json:"final,omitempty"`

	PromptTokens     int     `json:"prompt_tokens,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"`
	CachedTokens     int     `json:"cached_tokens,omitempty"`
	Cost             float64 `json:"cost,omitempty"`
	LatencyMS        int64   `json:"latency_ms,omitempty"`
}

// Events is the append-only record of every routing decision.
type Events struct {
	mutex sync.Mutex
	file  *os.File
}

// OpenEvents opens the log. A log that cannot be opened is not a reason to
// refuse a run: the router still routes, it just stops keeping a diary, so the
// error is returned for reporting and a nil Events is usable.
func OpenEvents(dir string) (*Events, error) {
	path, err := statePath(dir, "router-events.jsonl")
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &Events{file: file}, nil
}

// Append writes one row. Rows are written whole and O_APPEND is atomic for a
// write of this size on every platform this runs on, so two concurrent aforge
// processes interleave rows without ever interleaving bytes.
//
// Every read of the file handle happens under the mutex, including the one that
// asks whether there is a file at all. A leaf's settled verdict is appended from
// whichever goroutine reported it, which may be after the run has begun shutting
// down, so Close and Append genuinely do race — the check outside the lock was a
// data race on the handle that only the race detector would ever have shown,
// since a closed *os.File returns an error rather than panicking.
func (e *Events) Append(event Event) {
	if e == nil {
		return
	}
	event.At = time.Now().UTC()
	encoded, err := json.Marshal(event)
	if err != nil {
		return
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.file == nil {
		return
	}
	_, _ = e.file.Write(append(encoded, '\n'))
}

// Close releases the log.
func (e *Events) Close() error {
	if e == nil || e.file == nil {
		return nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	err := e.file.Close()
	e.file = nil
	return err
}

// callID names one unit of work so that the attempt rows and the verdict row
// that corrects them can be joined. It is random rather than derived: two
// identical calls in one run are two units of work and must not collapse into
// one row in the analysis.
func callID() string {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(bytes[:])
}
