package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// Quirks are the request-shape facts a provider will not publish and only a
// rejected call can teach.
//
// There is exactly one of them today, and it is the reason this file exists:
// an endpoint that refuses to have its reasoning turned off. OpenRouter's
// listing says which knobs a model ACCEPTS — that is the catalog's
// supported_parameters, and it is enough to keep the harness from sending a
// reasoning field to a model that takes none. It does not say whether the
// disable inside that field is honoured, and on MiniMax M2.7 it is not: the
// model always thinks, and answers {"reasoning":{"enabled":false}} with a 400.
//
// Nothing here is a list of model names. A name in the source would be a
// second, private catalog that goes stale the week a provider changes its mind,
// and the whole point of the memo is that the endpoint itself is the authority.
// The adapter learns the fact the one way it can be learned — by being told no
// — repairs the call it was making, and writes it down so that the next process
// does not have to be told again.
//
// The file is a cache and never a source of truth. A missing, unreadable or
// nonsense file costs one rejected request per model per process, which is
// exactly what the state was before it existed.

const quirksFile = "model-quirks.json"

// quirks is the process's memo and where it persists.
type quirksStore struct {
	mutex sync.Mutex
	path  string
	// mandatory is the learned set, keyed by normalized model.
	mandatory map[string]time.Time
	loaded    bool
}

var quirks = &quirksStore{mandatory: map[string]time.Time{}}

// LoadQuirks seeds the process from a profile directory and names the file
// later discoveries are written to. Empty dir means ~/.aforge, which is where
// every other durable Aforge fact lives.
//
// It is called once at startup, before any request is shaped. Calling it twice
// re-reads the file, which is harmless: the memo only ever grows, and a fact
// learned in memory is never dropped by a read.
func LoadQuirks(dir string) {
	quirks.load(quirksPath(dir))
}

func quirksPath(dir string) string {
	if dir = strings.TrimSpace(dir); dir != "" {
		return filepath.Join(dir, quirksFile)
	}
	return home.Join(quirksFile)
}

type quirksWire struct {
	// ReasoningMandatory maps a model to when it refused a disable. The date is
	// for a person reading the file, not for a rule: nothing here expires,
	// because a provider that starts honouring the disable costs the harness one
	// economy it can live without, while re-testing a refusal on a schedule
	// would cost a failed call on a cadence nobody asked for.
	ReasoningMandatory map[string]time.Time `json:"reasoning_mandatory,omitempty"`
}

func (q *quirksStore) load(path string) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.path = path
	q.loaded = true
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var wire quirksWire
	if json.Unmarshal(raw, &wire) != nil {
		return
	}
	for model, learnedAt := range wire.ReasoningMandatory {
		if key := normalizeModel(model); key != "" {
			if _, known := q.mandatory[key]; !known {
				q.mandatory[key] = learnedAt
			}
		}
	}
}

// note records a refusal and reports whether it was new. Only a new fact is
// worth a write, so a model that refuses on every call still costs one.
func (q *quirksStore) note(model string, at time.Time) bool {
	key := normalizeModel(model)
	if key == "" {
		return false
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if _, known := q.mandatory[key]; known {
		return false
	}
	q.mandatory[key] = at
	return true
}

func (q *quirksStore) knows(model string) bool {
	key := normalizeModel(model)
	if key == "" {
		return false
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	_, known := q.mandatory[key]
	return known
}

// snapshot copies the memo out from under the lock, so the file write below
// happens with nothing held: encoding and two syscalls are not a critical
// section, and a lock spanning them would put a disk on every reader's path.
func (q *quirksStore) snapshot() (string, quirksWire) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	wire := quirksWire{ReasoningMandatory: make(map[string]time.Time, len(q.mandatory))}
	for model, learnedAt := range q.mandatory {
		wire.ReasoningMandatory[model] = learnedAt
	}
	return q.path, wire
}

// save writes the whole memo. It is called off the request path, after a new
// fact, and a failure is silent: a cache that could not be written is a cache
// that will be rebuilt, not an error the run should carry.
func (q *quirksStore) save() {
	path, wire := q.snapshot()
	if path == "" {
		return
	}
	encoded, err := json.MarshalIndent(wire, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	// Written beside and renamed on top, so a reader never sees half a file and
	// a crash mid-write leaves the previous memo intact.
	temporary := path + ".tmp"
	if os.WriteFile(temporary, encoded, 0o644) != nil {
		return
	}
	if os.Rename(temporary, path) != nil {
		_ = os.Remove(temporary)
	}
}
