package router

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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

	// Outcome is the settle's own plain word for how a whole piece of work
	// ended — `landed`, `not accepted`, `stopped` — and it is empty on every
	// row about a single model call. A verdict says what may be LEARNED from an
	// outcome; this says what the outcome WAS, and the two stopped being the
	// same question when the chat engine started grading settled task nodes:
	// a node somebody stopped and a node nobody could check are both
	// unlearnable and are not the same news (internal/session's taskgrade.go).
	Outcome string `json:"outcome,omitempty"`
	// Retries is how many times the work was handed back before it settled —
	// the repair rounds a task node spent. Zero on a routed call, which retries
	// by escalating rather than by repeating, and where Escalation already says
	// what was tried.
	Retries int `json:"retries,omitempty"`

	PromptTokens     int     `json:"prompt_tokens,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"`
	CachedTokens     int     `json:"cached_tokens,omitempty"`
	Cost             float64 `json:"cost,omitempty"`
	LatencyMS        int64   `json:"latency_ms,omitempty"`
}

// Events is the append-only record of every routing decision.
//
// One row per turn is the design — a leaf may take two hundred of them — so the
// rows are buffered and land in batches. What is never batched is a *row*: see
// Append for why half a row on disk would be worse than no row at all.
type Events struct {
	mutex   sync.Mutex
	file    *os.File
	writer  *bufio.Writer
	row     bytes.Buffer
	encoder *json.Encoder
}

// eventBuffer holds a batch of rows. Rows are a few hundred bytes, so this is
// on the order of a leaf's worth of turns — enough that the per-row write is
// gone, small enough that an interrupted run has lost a paragraph of its diary
// rather than a chapter.
const eventBuffer = 32 << 10

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
	events := &Events{file: file, writer: bufio.NewWriterSize(file, eventBuffer)}
	// One encoder over one reused buffer, made here rather than per row: it
	// writes exactly what json.Marshal wrote, plus the newline the format
	// already ended every row with.
	events.encoder = json.NewEncoder(&events.row)
	return events, nil
}

// Append writes one row. Rows are written whole and O_APPEND is atomic for a
// write of this size on every platform this runs on, so two concurrent aforge
// processes interleave rows without ever interleaving bytes.
//
// That is the one thing the buffering must not break, and it is why the buffer
// is landed before a row that would not fit rather than after: a row split
// across two writes is a row another process is free to land inside, and half a
// JSON object is worse in the log than no object. Every write the buffer makes
// therefore carries whole rows only — including a row larger than the buffer,
// which goes straight to the file in one call.
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
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.writer == nil {
		return
	}
	e.row.Reset()
	if err := e.encoder.Encode(event); err != nil {
		return
	}
	if e.row.Len() > e.writer.Available() {
		_ = e.writer.Flush()
	}
	_, _ = e.writer.Write(e.row.Bytes())
	// The settled verdict closes the account for one unit of work, which is the
	// batch boundary the reader of this file cares about: an attempt row and
	// the row that corrects it belong to the same instant, and a run that dies
	// between them should not have left only the first behind.
	if event.Final {
		_ = e.writer.Flush()
	}
}

// Close releases the log.
func (e *Events) Close() error {
	if e == nil {
		return nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.file == nil {
		return nil
	}
	flushErr := e.writer.Flush()
	err := e.file.Close()
	e.file, e.writer = nil, nil
	return errors.Join(flushErr, err)
}

// idEntropy is randomness bought in bulk. The ids stay exactly as random as
// they were — the same generator, the same eight bytes each — but a turn no
// longer asks the generator for them one at a time, and a chat leaf takes two
// hundred turns.
type idEntropy struct {
	mutex sync.Mutex
	pool  [idPoolBytes]byte
	next  int
}

const (
	idBytes     = 8
	idPoolBytes = 512
)

// The pool starts spent, so the first id fills it.
var idPool = idEntropy{next: idPoolBytes}

// callID names one unit of work so that the attempt rows and the verdict row
// that corrects them can be joined. It is random rather than derived: two
// identical calls in one run are two units of work and must not collapse into
// one row in the analysis.
func callID() string {
	var id [idBytes]byte
	idPool.mutex.Lock()
	if idPool.next+idBytes > idPoolBytes {
		if _, err := rand.Read(idPool.pool[:]); err != nil {
			idPool.mutex.Unlock()
			return time.Now().UTC().Format("20060102T150405.000000000")
		}
		idPool.next = 0
	}
	copy(id[:], idPool.pool[idPool.next:])
	// Spent bytes are cleared as they are handed out: what is left in the pool
	// is randomness nobody has seen, and it should not outlive its use.
	clear(idPool.pool[idPool.next : idPool.next+idBytes])
	idPool.next += idBytes
	idPool.mutex.Unlock()
	return hex.EncodeToString(id[:])
}
