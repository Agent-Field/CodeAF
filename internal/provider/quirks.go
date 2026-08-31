package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/home"
)

// Quirks are the request-shape facts a provider will not publish and only a
// call's answer can teach.
//
// The first of them is the reason this file exists:
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
// nonsense file costs one discovery per model per process, which is exactly
// what the state was before it existed.

const quirksFile = "model-quirks.json"

// quirks is the process's memo and where it persists.
type quirksStore struct {
	mutex sync.Mutex
	path  string
	// mandatory is the learned set, keyed by normalized model.
	mandatory map[string]time.Time
	// disableIgnored is the answer-side twin of mandatory: models whose endpoint
	// accepted the disable but still spent the whole output ceiling reasoning.
	// It is separate because a silent ignore and a rejected request are
	// different wire facts even though both mean a caller must leave room.
	disableIgnored map[string]time.Time
	// noCacheControl is the second learned set: models whose endpoint rejected
	// an ephemeral cache breakpoint. It is a separate map rather than a flag on
	// one record because the two facts are independent — a model may reason
	// unconditionally and take breakpoints, or neither, or both.
	noCacheControl map[string]time.Time
	// noReasoningBudget is the third learned set: models whose endpoint rejected
	// the thinking budget the ladder's top two rungs carry (wire.go's
	// refusesReasoningBudget). Independent of both maps above for the same
	// reason they are independent of each other — an endpoint may take the
	// effort word and refuse the budget, or the other way round.
	noReasoningBudget map[string]time.Time
	// noReasoningReplay is learned only from a 400 naming the assistant replay
	// fields. It is per model because one incompatible endpoint must not erase
	// continuity for every reasoning model routed through this process.
	noReasoningReplay map[string]time.Time
	// answerCut is the sixth learned fact and the only one that is a NUMBER
	// rather than a date: how many completion tokens a model spent before its
	// structured answer was cut off, keyed by model and by the lane that asked.
	//
	// It is here rather than beside the seam that reads it because it is the
	// same kind of fact as its neighbours — something a provider will not
	// publish and only a call's answer can teach — and because being here it
	// survives the process, which is the whole difference between a harness
	// that learns and one that rediscovers. The lane is part of the key for the
	// reason the router's call classes are per class: a model that cuts on a
	// fan-out of five parts says nothing about the same model answering a
	// one-object verdict, and a memo that pooled them would raise the ceiling
	// on every call in the system because one of them is wide.
	answerCut map[string]int
	loaded    bool

	// writes counts saves in flight. The save is deliberately off the request
	// path — the call that learned the fact is waiting to be re-sent and must
	// not wait on a disk — which means the process can be holding a file
	// descriptor into a directory its owner believes it has finished with. In
	// production nothing cares; in a test whose profile directory is removed at
	// cleanup, the write and the removal race, and the removal loses. See
	// settle.
	writes sync.WaitGroup
}

var quirks = &quirksStore{
	mandatory:         map[string]time.Time{},
	disableIgnored:    map[string]time.Time{},
	noCacheControl:    map[string]time.Time{},
	noReasoningBudget: map[string]time.Time{},
	noReasoningReplay: map[string]time.Time{},
	answerCut:         map[string]int{},
}

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

	// ReasoningDisableIgnored maps a model to when it accepted a disable but
	// returned an empty, length-capped answer anyway. Unlike the field above,
	// there was no rejected request from which the adapter could learn.
	ReasoningDisableIgnored map[string]time.Time `json:"reasoning_disable_ignored,omitempty"`

	// CacheControlRejected maps a model to when its endpoint refused an
	// ephemeral cache breakpoint. It costs the same as the field above: one
	// rejected call per model per profile, after which the adapter falls back to
	// the automatic prefix cache every provider has anyway.
	CacheControlRejected map[string]time.Time `json:"cache_control_rejected,omitempty"`

	// ReasoningBudgetRejected maps a model to when its endpoint refused a
	// thinking budget. It costs what the two above cost — one rejected call per
	// model per profile — after which the top two ladder rungs are served as the
	// deepest thing that endpoint has a word for.
	ReasoningBudgetRejected map[string]time.Time `json:"reasoning_budget_rejected,omitempty"`

	// ReasoningReplayRejected maps a model to when its endpoint refused
	// assistant reasoning carried back for tool-loop continuity.
	ReasoningReplayRejected map[string]time.Time `json:"reasoning_replay_rejected,omitempty"`

	// AnswerCutAt maps "<model>\n<lane>" to the widest completion, in tokens,
	// that lane has seen this model spend before being cut off mid-answer. It
	// only ever grows, and it is read as evidence rather than as policy: the
	// seam that sizes a structured request asks it what actually happened here
	// and refuses to send a ceiling it has already watched this model overrun.
	AnswerCutAt map[string]int `json:"answer_cut_at,omitempty"`
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
	seed(q.mandatory, wire.ReasoningMandatory)
	seed(q.disableIgnored, wire.ReasoningDisableIgnored)
	seed(q.noCacheControl, wire.CacheControlRejected)
	seed(q.noReasoningBudget, wire.ReasoningBudgetRejected)
	seed(q.noReasoningReplay, wire.ReasoningReplayRejected)
	for key, spent := range wire.AnswerCutAt {
		if spent > q.answerCut[key] {
			q.answerCut[key] = spent
		}
	}
}

// seed folds a loaded set into a live one without ever dropping a fact learned
// in this process: a read only adds.
func seed(into, from map[string]time.Time) {
	for model, learnedAt := range from {
		if key := normalizeModel(model); key != "" {
			if _, known := into[key]; !known {
				into[key] = learnedAt
			}
		}
	}
}

// note records a refusal and reports whether it was new. Only a new fact is
// worth a write, so a model that refuses on every call still costs one.
func (q *quirksStore) note(model string, at time.Time) bool {
	return q.record(func(s *quirksStore) map[string]time.Time { return s.mandatory }, model, at)
}

func (q *quirksStore) knows(model string) bool {
	return q.recorded(func(s *quirksStore) map[string]time.Time { return s.mandatory }, model)
}

// noteDisableIgnored records that a nominal reasoning disable did not preserve
// any room for the answer.
func (q *quirksStore) noteDisableIgnored(model string, at time.Time) bool {
	return q.record(func(s *quirksStore) map[string]time.Time { return s.disableIgnored }, model, at)
}

func (q *quirksStore) knowsDisableIgnored(model string) bool {
	return q.recorded(func(s *quirksStore) map[string]time.Time { return s.disableIgnored }, model)
}

// noteNoCacheControl records that this model's endpoint rejected a breakpoint.
func (q *quirksStore) noteNoCacheControl(model string, at time.Time) bool {
	return q.record(func(s *quirksStore) map[string]time.Time { return s.noCacheControl }, model, at)
}

func (q *quirksStore) knowsNoCacheControl(model string) bool {
	return q.recorded(func(s *quirksStore) map[string]time.Time { return s.noCacheControl }, model)
}

// noteNoReasoningBudget records that this model's endpoint rejected a thinking
// budget.
func (q *quirksStore) noteNoReasoningBudget(model string, at time.Time) bool {
	return q.record(func(s *quirksStore) map[string]time.Time { return s.noReasoningBudget }, model, at)
}

func (q *quirksStore) knowsNoReasoningBudget(model string) bool {
	return q.recorded(func(s *quirksStore) map[string]time.Time { return s.noReasoningBudget }, model)
}

func (q *quirksStore) noteNoReasoningReplay(model string, at time.Time) bool {
	return q.record(func(s *quirksStore) map[string]time.Time { return s.noReasoningReplay }, model, at)
}

func (q *quirksStore) knowsNoReasoningReplay(model string) bool {
	return q.recorded(func(s *quirksStore) map[string]time.Time { return s.noReasoningReplay }, model)
}

// NoteAnswerCut records that a model ran out of room mid-answer on one lane,
// and reports whether that is wider than anything already known. Only a wider
// cut is worth a write: a model that cuts on every call of a lane costs the
// profile one save, not one per call.
//
// The figure recorded is what the model actually SPENT, never what it was
// allowed — a provider that stops short of the ceiling has told us where its
// own wall is, and that is the more useful of the two numbers.
func NoteAnswerCut(model, lane string, spent int) bool {
	key := answerCutKey(model, lane)
	if key == "" || spent <= 0 {
		return false
	}
	quirks.mutex.Lock()
	if spent <= quirks.answerCut[key] {
		quirks.mutex.Unlock()
		return false
	}
	quirks.answerCut[key] = spent
	quirks.mutex.Unlock()
	quirks.persist()
	return true
}

// WidestAnswerCut answers what this model has been seen to spend before being
// cut off on this lane, zero when it has never been cut off here. Zero means
// "nothing was learned", which is the honest reading and the one that leaves the
// derived ceiling standing alone.
func WidestAnswerCut(model, lane string) int {
	key := answerCutKey(model, lane)
	if key == "" {
		return 0
	}
	quirks.mutex.Lock()
	defer quirks.mutex.Unlock()
	return quirks.answerCut[key]
}

// answerCutKey joins the two halves of the memo's key. A call with no model
// named — every path that runs without a router — has nothing to learn about and
// nothing to remember, so it keys to nothing and both sides above no-op.
func answerCutKey(model, lane string) string {
	name := normalizeModel(model)
	lane = strings.TrimSpace(lane)
	if name == "" || lane == "" {
		return ""
	}
	return name + "\n" + lane
}

func (q *quirksStore) record(set func(*quirksStore) map[string]time.Time, model string, at time.Time) bool {
	key := normalizeModel(model)
	if key == "" {
		return false
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	learned := set(q)
	if _, known := learned[key]; known {
		return false
	}
	learned[key] = at
	return true
}

func (q *quirksStore) recorded(set func(*quirksStore) map[string]time.Time, model string) bool {
	key := normalizeModel(model)
	if key == "" {
		return false
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	_, known := set(q)[key]
	return known
}

// snapshot copies the memo out from under the lock, so the file write below
// happens with nothing held: encoding and two syscalls are not a critical
// section, and a lock spanning them would put a disk on every reader's path.
func (q *quirksStore) snapshot() (string, quirksWire) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	wire := quirksWire{
		ReasoningMandatory:      make(map[string]time.Time, len(q.mandatory)),
		ReasoningDisableIgnored: make(map[string]time.Time, len(q.disableIgnored)),
		CacheControlRejected:    make(map[string]time.Time, len(q.noCacheControl)),
		ReasoningBudgetRejected: make(map[string]time.Time, len(q.noReasoningBudget)),
		ReasoningReplayRejected: make(map[string]time.Time, len(q.noReasoningReplay)),
		AnswerCutAt:             make(map[string]int, len(q.answerCut)),
	}
	for key, spent := range q.answerCut {
		wire.AnswerCutAt[key] = spent
	}
	for model, learnedAt := range q.mandatory {
		wire.ReasoningMandatory[model] = learnedAt
	}
	for model, learnedAt := range q.disableIgnored {
		wire.ReasoningDisableIgnored[model] = learnedAt
	}
	for model, learnedAt := range q.noCacheControl {
		wire.CacheControlRejected[model] = learnedAt
	}
	for model, learnedAt := range q.noReasoningBudget {
		wire.ReasoningBudgetRejected[model] = learnedAt
	}
	for model, learnedAt := range q.noReasoningReplay {
		wire.ReasoningReplayRejected[model] = learnedAt
	}
	return q.path, wire
}

// persist schedules the memo's write and counts it while it is in flight. Every
// new fact goes through here rather than spawning its own goroutine, so there is
// exactly one place that knows a write is outstanding.
func (q *quirksStore) persist() {
	q.writes.Add(1)
	guard.Go("provider/quirks", func() {
		defer q.writes.Done()
		q.save()
	})
}

// settle waits for every scheduled write to land. Nothing on a request path may
// call it — the whole point of the write being scheduled is that no request
// waits for it — and nothing does: its one caller is the test helper that owns
// the profile directory being written into, which cannot remove that directory
// while a writer still has business in it.
func (q *quirksStore) settle() { q.writes.Wait() }

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
	//
	// THE TEMPORARY NAME IS UNIQUE, which a fixed `.tmp` beside the memo was
	// not. Two savers running at once — two clients in one process, or two
	// processes sharing a home — then opened the SAME scratch file: the second
	// truncated what the first had written, and the first renamed the truncation
	// on top of the memo. What a reader saw was not half a write but a whole
	// file that was half a document, which is how it reached a decoder as
	// "unexpected end of JSON input". [writeAtomic] in internal/lane already
	// does it this way; this is the same fix.
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return
	}
	name := temporary.Name()
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		_ = os.Remove(name)
		return
	}
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		_ = os.Remove(name)
		return
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(name)
		return
	}
	if os.Rename(name, path) != nil {
		_ = os.Remove(name)
	}
}
