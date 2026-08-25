package session

// BPE working state: what is TRUE right now, and what is OPEN.
//
// The session already had experience memory — a handful of standing facts about
// the person, carried across sessions (memory.go) — and nothing else. Everything a
// turn learned about the work in front of it lived in exactly one place: the
// transcript. That is the whole problem this file exists for. A transcript is
// the one structure compaction destroys, so every compaction pass had to
// REDISCOVER the plot from the summary it had just written — which files matter,
// which subgoal is half-done, which command is the one that proves the build is
// green — and a summary is a lossy narration of a trajectory, not a record of
// state.
//
// The research (harness-research-notes.md §4, EvoHarness-RL's BPE abstraction)
// says all harness external state reduces to three roles: BELIEF (what is
// currently true in the environment), PROGRESS (subgoal statuses), EXPERIENCE
// (cross-episode priors). Experience is memory.go and is not duplicated here.
// This file is the other two, and its law is the one the same section states
// twice over:
//
//   - STATE LIVES OUTSIDE THE TRAJECTORY. These records are not messages. They
//     are held on the agent, written beside the journal, and rendered into a
//     block ([Agent.StateBlock]) that a compaction pass re-injects VERBATIM. The
//     trajectory compresses; the state does not.
//   - GROUNDED IN EXECUTION, NOT NARRATION (PMCoder). Every record cites what
//     RAN — the tool call, the command, the file that was read — and the tools
//     refuse a record with no citation. A belief the model merely asserted is a
//     belief that can be wrong forever, and worse, one that a later summary will
//     quote back as if it had been verified.
//
// The whole store is agent-managed: the model decides what to track and when to
// commit, and pays for both out of the same turn budget as its environment
// actions (the BPE paper's point — bookkeeping that is free is bookkeeping that
// is done compulsively). The tool descriptions say so in one line each.
//
// WHAT THIS IS NOT. It is not a todo list (docs/CHAT-V3.md Decision 11 refused
// one, and still does): a todo list is a plan the person is meant to read, and
// work big enough to decompose belongs to the workforce. These records are the
// model's own working state, sized for surviving a compaction, and no surface
// draws them.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

const (
	// stateFileVersion is the schema of state.json. A file with any other
	// version is IGNORED rather than migrated: the records are a working
	// convenience that the session can rebuild by working, and guessing at an
	// older shape risks rehydrating a belief into a field that no longer means
	// what it meant.
	stateFileVersion = 1

	// stateBlockLines caps [Agent.StateBlock] — the block a compaction pass
	// re-injects. Forty lines is around 400 tokens against a 20k verbatim tail:
	// affordable on every compaction forever, and small enough that it cannot
	// become the reason a rebuilt transcript is still too big. Past it the block
	// elides, oldest first, and says how many it dropped.
	stateBlockLines = 40

	// stateRecallLines caps the recall tool's dump. It is larger because recall
	// is paid for ONCE, by the model that asked for it, when it needs the ids —
	// unlike the block, which every request after a compaction carries.
	stateRecallLines = 200

	// stateTextLimit and stateEvidenceLimit bound one record. A record is a
	// short string by contract; these are the backstop that keeps a model
	// pasting a stack trace into track from putting a stack trace in front of
	// every future turn.
	stateTextLimit     = 240
	stateEvidenceLimit = 160

	// stateRecordLimit is the capacity cap (One Recipe, §4: a cap pressures
	// distillation rather than accumulation). Past it the OLDEST closed record —
	// done progress, stale belief — is dropped to make room, and only if there is
	// none does track refuse. A session cannot silently lose an open subgoal to
	// a cap.
	stateRecordLimit = 200
)

// stateKind is which of the two BPE roles a record plays. Experience is
// memory.go's file and is deliberately absent.
type stateKind string

const (
	kindBelief   stateKind = "belief"
	kindProgress stateKind = "progress"
)

// stateStatus is where a record stands. The vocabulary is BPE's
// (attempted/open/blocked/done) narrowed to what this store can honestly
// distinguish: "attempted" and "open" are the same fact from the outside — the
// subgoal is not finished — so a progress item is open, blocked, or done, and a
// belief is live or stale.
type stateStatus string

const (
	statusLive    stateStatus = "live"
	statusStale   stateStatus = "stale"
	statusOpen    stateStatus = "open"
	statusBlocked stateStatus = "blocked"
	statusDone    stateStatus = "done"
)

// closed reports whether a record has left the working set: a finished subgoal
// or a belief that stopped being true. Closed records are what the capacity cap
// evicts and what the block elides first.
func (s stateStatus) closed() bool { return s == statusDone || s == statusStale }

// validFor reports whether a status is one this kind can hold. It is the load
// path's schema check as much as the tools': a state.json claiming a belief is
// "done" is a file written by something that is not this code.
func (s stateStatus) validFor(kind stateKind) bool {
	switch kind {
	case kindBelief:
		return s == statusLive || s == statusStale
	case kindProgress:
		return s == statusOpen || s == statusBlocked || s == statusDone
	}
	return false
}

// stateRecord is one belief or one progress item.
//
// Evidence is the CREATED-FROM POINTER and it is required at every entrance —
// the tool refuses an empty one, and the loader drops a file containing one.
// It names what ran ("bash: go test ./internal/session", "read: go.mod"), which
// is the difference between a record and a rumour: a future turn reading the
// state block can go re-run the thing that established it, and a summarizer
// cannot launder narration into fact by way of this store.
type stateRecord struct {
	ID       string      `json:"id"`
	Kind     stateKind   `json:"kind"`
	Text     string      `json:"text"`
	Status   stateStatus `json:"status"`
	Evidence string      `json:"evidence"`

	// Seq orders records against each other. It is a counter rather than a
	// timestamp because "newest first" must be exact and two records tracked in
	// one parallel tool batch share a millisecond.
	Seq int `json:"seq"`

	// Created and Updated are for the person reading state.json, not for the
	// ordering. RFC3339, and Updated is empty until something changes.
	Created string `json:"created"`
	Updated string `json:"updated,omitempty"`
}

// stateDocument is the on-disk file: a type tag, a version, and the records.
type stateDocument struct {
	Type    string        `json:"type"`
	Version int           `json:"version"`
	Records []stateRecord `json:"records"`
}

// stateStore is the records and the lock over them.
//
// Like memoryStore it sits OUTSIDE Agent.mu and holds its own lock: its writers
// are tool calls running in parallel inside one batch, and its reader is a
// compaction pass that must not have to take the session lock to render a
// block.
//
// path is "" for a session with no journal. That is a working store with no
// disk behind it, NOT an absent one — the tools are on the belt either way,
// because compaction happens to an in-memory session exactly as it happens to a
// journaled one, and state that survives compaction is the point. Only the
// resume half is missing, and only a session with nothing to resume from loses
// it.
type stateStore struct {
	mu      sync.Mutex
	path    string
	records []stateRecord
	seq     int
	// minted counts ids handed out per kind, so an id is never reused inside one
	// session and never collides with one rehydrated from disk.
	minted map[stateKind]int
	// loaded marks the one rehydration attempt. The load is lazy — the store is
	// built on first use — and it must not re-read the file after the session
	// has started writing it.
	loaded bool
}

// statePath derives the state file from the journal: the same name with a
// different extension, beside it in the same directory.
//
// It is per-journal rather than one state.json per session DIRECTORY, which the
// FLAT layout's shape demanded: ~/.aforge/v3/sessions/<workspace>/ held every
// session this workspace ever had, so a single state.json there would have been
// every window and every resumed conversation writing over each other's
// beliefs. State belongs to ONE conversation, and the journal is what names one
// conversation.
//
// A SESSION FOLDER ANSWERS ITS OWN NAME. Under the new layout (place.go) the
// directory holds exactly one conversation, so the file is the folder's
// state.json — and this derivation says so from the path alone, because the one
// journal that can be called transcript.jsonl is a folder's. [Config.stateFile]
// is the door callers holding a [Place] come through; this is what answers a
// caller holding only the path.
func statePath(sessionFile string) string {
	sessionFile = strings.TrimSpace(sessionFile)
	if sessionFile == "" {
		return ""
	}
	if filepath.Base(sessionFile) == placeTranscript {
		return filepath.Join(filepath.Dir(sessionFile), placeState)
	}
	return strings.TrimSuffix(sessionFile, filepath.Ext(sessionFile)) + ".state.json"
}

// stateFile is where THIS session keeps its working state: the folder's
// state.json when the session has a [Place], and the stem-derived sidecar for
// the legacy flat layout that the zero Place stands for.
func (c Config) stateFile() string {
	if path := c.Place.State(); path != "" {
		return path
	}
	return statePath(c.SessionFile)
}

func newStateStore(path string) *stateStore {
	return &stateStore{path: path, minted: map[stateKind]int{}}
}

// state is the agent's store, built on first use.
//
// It is lazy rather than constructed in newAgent for one reason worth the
// sync.Once: the belt closes over the agent, so the tools can reach a store that
// does not exist yet, and building it here keeps the whole feature — schema,
// tools, file, block — inside this file. Every reader goes through this method,
// so the rehydration happens before the first record is read, whichever caller
// gets there first.
func (a *Agent) state() *stateStore {
	a.stateOnce.Do(func() {
		a.stateStore = newStateStore(a.config.stateFile())
		a.stateStore.load()
	})
	return a.stateStore
}

// ── the file ────────────────────────────────────────────────────────────────

// load rehydrates the records, and ignores anything it cannot fully validate.
//
// A CORRUPT STATE FILE IS NEVER FATAL, and the reason is the asymmetry between
// the two failure modes. Refusing to start a session because a bookkeeping file
// has a bad byte in it costs the person their whole conversation; starting with
// no records costs them a block the model can rebuild by working. So a file that
// does not parse, carries another version, or holds a record with no evidence is
// dropped WHOLE — not partially salvaged, because a half-loaded state is a state
// nobody wrote — with one log line naming the path.
func (s *stateStore) load() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return
	}
	s.loaded = true
	if s.path == "" {
		return
	}
	content, err := os.ReadFile(s.path)
	if err != nil {
		// A missing file is the ordinary case — a fresh session — and says
		// nothing worth logging.
		if !os.IsNotExist(err) {
			log.Printf("session: ignoring unreadable state file %s: %v", s.path, err)
		}
		return
	}
	records, err := decodeState(content)
	if err != nil {
		log.Printf("session: ignoring corrupt state file %s: %v", s.path, err)
		return
	}
	s.records = records
	for _, record := range records {
		if record.Seq > s.seq {
			s.seq = record.Seq
		}
		if number := idNumber(record.ID); number > s.minted[record.Kind] {
			s.minted[record.Kind] = number
		}
	}
}

// decodeState parses and VALIDATES one state file. Every rule it enforces is a
// rule the tools enforce on the way in, so a file that breaks one was not
// written by this store.
func decodeState(content []byte) ([]stateRecord, error) {
	var document stateDocument
	if err := json.Unmarshal(content, &document); err != nil {
		return nil, err
	}
	if document.Type != "state" {
		return nil, fmt.Errorf("type is %q, want \"state\"", document.Type)
	}
	if document.Version != stateFileVersion {
		return nil, fmt.Errorf("version %d, want %d", document.Version, stateFileVersion)
	}
	seen := make(map[string]bool, len(document.Records))
	for _, record := range document.Records {
		switch {
		case strings.TrimSpace(record.ID) == "":
			return nil, fmt.Errorf("a record has no id")
		case seen[record.ID]:
			return nil, fmt.Errorf("id %q appears twice", record.ID)
		case strings.TrimSpace(record.Text) == "":
			return nil, fmt.Errorf("record %s has no text", record.ID)
		case strings.TrimSpace(record.Evidence) == "":
			return nil, fmt.Errorf("record %s has no evidence", record.ID)
		case record.Kind != kindBelief && record.Kind != kindProgress:
			return nil, fmt.Errorf("record %s has kind %q", record.ID, record.Kind)
		case !record.Status.validFor(record.Kind):
			return nil, fmt.Errorf("record %s is a %s with status %q", record.ID, record.Kind, record.Status)
		}
		seen[record.ID] = true
	}
	return document.Records, nil
}

// idNumber is the counter inside an id ("p12" → 12), or 0 for anything else.
func idNumber(id string) int {
	if len(id) < 2 {
		return 0
	}
	number, err := strconv.Atoi(id[1:])
	if err != nil {
		return 0
	}
	return number
}

// saveLocked writes the file, atomically, and reports what went wrong.
//
// Through a temporary file and a rename for memoryStore.forget's reason: a
// state file truncated by a crash mid-write is a file the next session logs as
// corrupt and drops, and the rename is the one write that cannot half-happen.
// The error is RETURNED rather than logged because its caller is a tool call,
// and a model that could not persist a record should be told in the same breath
// it is told the record was made.
func (s *stateStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	document := stateDocument{Type: "state", Version: stateFileVersion, Records: s.records}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if directory := filepath.Dir(s.path); directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, append(encoded, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(temporary, s.path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

// ── the three operations ────────────────────────────────────────────────────

// track records a belief or opens a progress item and returns the record.
//
// Re-tracking something already tracked UPDATES rather than duplicates: the same
// kind and the same text, compared case-insensitively, refreshes the evidence
// and moves the record to the front. Two rows saying the same thing with
// different ids is the failure mode that makes a state block unreadable, and a
// model re-asserting a belief it already holds is confirming it, which is what
// fresher evidence means.
func (s *stateStore) track(text string, kind stateKind, status stateStatus, evidence string) (stateRecord, error) {
	text = stateLine(text, stateTextLimit)
	evidence = stateLine(evidence, stateEvidenceLimit)
	switch {
	case text == "":
		return stateRecord{}, fmt.Errorf("text is required")
	case evidence == "":
		return stateRecord{}, fmt.Errorf("evidence is required: name the tool call or command that established this")
	case kind != kindBelief && kind != kindProgress:
		return stateRecord{}, fmt.Errorf("kind must be %q or %q", kindBelief, kindProgress)
	case !status.validFor(kind):
		return stateRecord{}, fmt.Errorf("a %s cannot have status %q", kind, status)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++
	now := stamp()
	for index, record := range s.records {
		if record.Kind != kind || !strings.EqualFold(record.Text, text) {
			continue
		}
		s.records[index].Evidence = evidence
		s.records[index].Status = status
		s.records[index].Seq = s.seq
		s.records[index].Updated = now
		updated := s.records[index]
		return updated, s.saveLocked()
	}

	s.evictLocked()
	s.minted[kind]++
	record := stateRecord{
		ID:       idPrefix(kind) + strconv.Itoa(s.minted[kind]),
		Kind:     kind,
		Text:     text,
		Status:   status,
		Evidence: evidence,
		Seq:      s.seq,
		Created:  now,
	}
	s.records = append(s.records, record)
	return record, s.saveLocked()
}

// evictLocked makes room for one new record when the store is at capacity, by
// dropping the oldest CLOSED one. An open subgoal and a live belief are never
// evicted: the cap is there to stop history accumulating, and losing the thing
// the session is currently doing would be the cap eating the point of the
// store. A store full of open work simply grows past the cap, which is a
// conversation with a real problem, not a bookkeeping one.
func (s *stateStore) evictLocked() {
	if len(s.records) < stateRecordLimit {
		return
	}
	oldest, found := -1, false
	for index, record := range s.records {
		if !record.Status.closed() {
			continue
		}
		if !found || record.Seq < s.records[oldest].Seq {
			oldest, found = index, true
		}
	}
	if !found {
		return
	}
	s.records = append(s.records[:oldest], s.records[oldest+1:]...)
}

// commit closes one record by id: a progress item to done, a belief to stale.
// It reports the record as it now stands and whether the call changed anything —
// committing something already closed is a fact, not an error.
func (s *stateStore) commit(id string) (stateRecord, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return stateRecord{}, false, fmt.Errorf("id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, record := range s.records {
		if !strings.EqualFold(record.ID, id) {
			continue
		}
		if record.Status.closed() {
			return record, false, nil
		}
		if record.Kind == kindBelief {
			s.records[index].Status = statusStale
		} else {
			s.records[index].Status = statusDone
		}
		s.records[index].Updated = stamp()
		return s.records[index], true, s.saveLocked()
	}
	return stateRecord{}, false, fmt.Errorf("no record has id %q", id)
}

// snapshot is the records, newest first, as a copy the caller may hold.
func (s *stateStore) snapshot() []stateRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]stateRecord, len(s.records))
	copy(out, s.records)
	// Insertion order is by seq already, but a record refreshed by a re-track
	// carries a new seq in an old slot, so the sort is what makes "newest first"
	// true rather than usually true.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Seq > out[j-1].Seq; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// stateLine flattens one field to the single short line the store keeps.
// Multi-line in, one line out: the block's unit is the line, and a record
// containing a newline would be a row nothing can render or cap.
func stateLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > limit {
		text = strings.TrimSpace(text[:limit]) + "…"
	}
	return text
}

func idPrefix(kind stateKind) string {
	if kind == kindBelief {
		return "b"
	}
	return "p"
}

// ── the compaction seam ─────────────────────────────────────────────────────

// StateBlock renders the working state as one compact bracketed block, or "" when
// there is nothing to say.
//
// THIS IS THE SEAM COMPACTION IS BUILT FOR, and it is deliberately a pure
// function of the store: it takes no lock of the agent's, makes no request, and
// can be called from inside a compaction pass that is holding whatever it is
// holding. What the parent does with it is inject it into the REBUILT
// transcript, right after the summary note, so that the pass hands the model
// back two different things about the same conversation: a lossy narration of
// what happened (the summary) and a verbatim record of what is true and what is
// open (this). The second one is not derived from the first and never passes
// through the summarizer — that is the entire mechanism (§4: retrieval must not
// re-ingest its own output).
//
//	[state] 2 beliefs · 1 open · 1 done — working state, kept outside the transcript
//	beliefs:
//	- b2 the module path is github.com/Agent-Field/aforge-v2  ← read: go.mod
//	open:
//	- p2 wire StateBlock into the compaction rebuild  ← grep: compact loop.go
//	done:
//	- p1 state.json rehydrates on resume  ← bash: go test ./internal/session
//
// Beliefs first because they are what the next turn will act on, OPEN BEFORE
// DONE because the open ones are work and the done ones are history, and newest
// first inside each because the recent end of a conversation is the live one.
// Stale beliefs do not appear at all: a belief that stopped being true has no
// business riding in front of every future turn, and it stays in state.json only
// so the person can see what was retracted.
func (a *Agent) StateBlock() string {
	return a.state().render(stateBlockLines)
}

// render is the block at a given line cap, shared by [Agent.StateBlock] and the
// recall tool so the model reads exactly the shape a compaction will preserve —
// a recall that showed a different rendering would teach the model to expect
// something the block does not deliver.
func (s *stateStore) render(limit int) string {
	records := s.snapshot()
	var beliefs, open, done []stateRecord
	for _, record := range records {
		switch {
		case record.Kind == kindBelief && record.Status == statusLive:
			beliefs = append(beliefs, record)
		case record.Kind == kindProgress && !record.Status.closed():
			open = append(open, record)
		case record.Status == statusDone:
			done = append(done, record)
		}
	}
	if len(beliefs)+len(open)+len(done) == 0 {
		return ""
	}

	keepBeliefs, keepOpen, keepDone := stateQuotas(len(beliefs), len(open), len(done), limit)
	dropped := (len(beliefs) - keepBeliefs) + (len(open) - keepOpen) + (len(done) - keepDone)

	var out strings.Builder
	fmt.Fprintf(&out, "[state] %s — working state, kept outside the transcript\n",
		stateCounts(len(beliefs), len(open), len(done)))
	section := func(title string, records []stateRecord, keep int) {
		if keep <= 0 {
			return
		}
		out.WriteString(title + ":\n")
		for _, record := range records[:keep] {
			out.WriteString("- " + stateRow(record) + "\n")
		}
	}
	section("beliefs", beliefs, keepBeliefs)
	section("open", open, keepOpen)
	section("done", done, keepDone)
	if dropped > 0 {
		fmt.Fprintf(&out, "(%d older record(s) not shown; recall for all)\n", dropped)
	}
	return strings.TrimRight(out.String(), "\n")
}

// stateRow is one record's line: the id the model commits by, what it says, and
// the evidence it stands on. A blocked item says so — it is not done, but it is
// also not something the next turn should simply pick up.
func stateRow(record stateRecord) string {
	row := record.ID + " " + record.Text
	if record.Status == statusBlocked {
		row += " (blocked)"
	}
	return row + "  ← " + record.Evidence
}

func stateCounts(beliefs, open, done int) string {
	parts := make([]string, 0, 3)
	if beliefs > 0 {
		parts = append(parts, strconv.Itoa(beliefs)+" belief(s)")
	}
	if open > 0 {
		parts = append(parts, strconv.Itoa(open)+" open")
	}
	if done > 0 {
		parts = append(parts, strconv.Itoa(done)+" done")
	}
	return strings.Join(parts, " · ")
}

// stateQuotas divides a line budget between the three sections.
//
// The budget is LINES, not records, so the header, each section's own title and
// the elision line are all paid for out of it — a block that promised forty
// lines and rendered forty-five would be a cap that does not cap.
//
// Done is squeezed FIRST and hardest (four lines when space is tight): finished
// work is the one section a future turn can lose without being confused about
// what it is doing. What is left is split evenly between beliefs and open, with
// whatever one of them does not need flowing to the other, because those two are
// exactly the things the compaction exists to preserve and neither may starve
// the other.
func stateQuotas(beliefs, open, done, limit int) (int, int, int) {
	const (
		headerLines   = 1
		elisionLines  = 1
		doneWhenTight = 4
	)
	sections := 0
	for _, count := range []int{beliefs, open, done} {
		if count > 0 {
			sections++
		}
	}
	if headerLines+sections+beliefs+open+done <= limit {
		return beliefs, open, done
	}
	available := limit - headerLines - elisionLines - sections
	if available < 0 {
		available = 0
	}
	keepDone := done
	if keepDone > doneWhenTight {
		keepDone = doneWhenTight
	}
	if keepDone > available {
		keepDone = available
	}
	rest := available - keepDone
	half := rest / 2
	keepBeliefs := beliefs
	if keepBeliefs > half {
		keepBeliefs = half
	}
	keepOpen := open
	if keepOpen > rest-keepBeliefs {
		keepOpen = rest - keepBeliefs
	}
	// The spill-back: whatever open did not use is beliefs' to take.
	if beliefs > keepBeliefs && rest-keepOpen > keepBeliefs {
		keepBeliefs = rest - keepOpen
		if keepBeliefs > beliefs {
			keepBeliefs = beliefs
		}
	}
	return keepBeliefs, keepOpen, keepDone
}

// ── the three tools ─────────────────────────────────────────────────────────

// THESE THREE STRINGS ARE BILLED ON EVERY REQUEST OF EVERY TURN — the whole
// tool-schema block rides in front of each one, some seventy times in a single
// task — so they are written for density rather than for prose. Every rule that
// was here is still here; what left was the second and third telling of it.
//
// The budget sentence is the BPE paper's own finding rather than a style choice:
// harness actions share the interaction budget with environment actions, and an
// agent that is not told so keeps books it never reads. IT IS NOW SAID ONCE, on
// `track`, which is the only one of the three that spends anything worth
// deciding about — commit and recall carried the identical sentence and paid for
// it on every request to repeat a rule the model had already read.
const stateBudgetLaw = " Bookkeeping shares this turn's budget."

const trackDescription = "Record one piece of working state a compaction must not lose, OUTSIDE the transcript; the same text twice updates it." + stateBudgetLaw

// The two laws that used to sit in the preamble — what a belief is against what
// a progress item is, and that evidence must name something that RAN — now sit
// on the fields they govern. A field description is read at the moment the model
// is filling that field in, which is where a rule about the field belongs.
const trackSchemaJSON = `{"type":"object","properties":{"text":{"type":"string","description":"The record, one short line"},"kind":{"type":"string","enum":["belief","progress"],"description":"belief = true now; progress = a subgoal opened, unfinished"},"evidence":{"type":"string","description":"What actually RAN, e.g. 'bash: go test ./...'; never what was only said or planned"},"status":{"type":"string","enum":["open","blocked"],"description":"Progress only: open (default), or blocked on something else"}},"required":["text","kind","evidence"],"additionalProperties":false}`

const commitDescription = "Close one record by id: a progress item becomes done, a belief no longer true goes stale and leaves."

const commitSchemaJSON = `{"type":"object","properties":{"id":{"type":"string","description":"The id track returned, e.g. 'p2'"}},"required":["id"],"additionalProperties":false}`

const recallDescription = "Your working state: beliefs, open subgoals, recently finished ones, and the ids commit takes. Survives a compaction verbatim, so call it for an id or after one for the state you kept."

const recallSchemaJSON = `{"type":"object","properties":{},"additionalProperties":false}`

// A track call reads as what it recorded and a commit as what it closed — the
// tool name alone says bookkeeping happened and not what was booked. recall has
// no argument and needs no gloss: its name is the whole of what it did.
func init() {
	glossField["track"] = "text"
	glossField["commit"] = "id"
}

// stateTools is the three hands, and unlike memoryTools they are UNCONDITIONAL.
// A session with no journal still compacts, and state that outlives compaction
// is what these are for; only the resume half needs a file, and a session with
// no file has nothing to resume anyway.
func (a *Agent) stateTools() []bare.Tool {
	return []bare.Tool{
		{
			Name:        "track",
			Description: trackDescription,
			Schema:      json.RawMessage(trackSchemaJSON),
			Execute: func(_ context.Context, args json.RawMessage) (string, bool, error) {
				var parsed struct {
					Text     string `json:"text"`
					Kind     string `json:"kind"`
					Evidence string `json:"evidence"`
					Status   string `json:"status"`
				}
				if err := json.Unmarshal(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
				kind := stateKind(strings.ToLower(strings.TrimSpace(parsed.Kind)))
				status := stateStatus(strings.ToLower(strings.TrimSpace(parsed.Status)))
				if status == "" {
					// The default is the kind's live state: a belief is asserted as
					// true, a subgoal is opened as unfinished.
					status = statusOpen
					if kind == kindBelief {
						status = statusLive
					}
				}
				record, err := a.state().track(parsed.Text, kind, status, parsed.Evidence)
				if err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
				return fmt.Sprintf("Tracked %s (%s, %s):\n%s", record.ID, record.Kind, record.Status, stateRow(record)), false, nil
			},
		},
		{
			Name:        "commit",
			Description: commitDescription,
			Schema:      json.RawMessage(commitSchemaJSON),
			Execute: func(_ context.Context, args json.RawMessage) (string, bool, error) {
				var parsed struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
				record, changed, err := a.state().commit(parsed.ID)
				if err != nil {
					// An unknown id is a mistake the model can fix with one call,
					// so it is told which call.
					return "Invalid arguments: " + err.Error() + "; call recall to see the current ids.", true, nil
				}
				if !changed {
					return fmt.Sprintf("%s was already %s; nothing changed.", record.ID, record.Status), false, nil
				}
				return fmt.Sprintf("Committed %s (%s):\n%s", record.ID, record.Status, stateRow(record)), false, nil
			},
		},
		{
			Name:        "recall",
			Description: recallDescription,
			Schema:      json.RawMessage(recallSchemaJSON),
			Execute: func(_ context.Context, _ json.RawMessage) (string, bool, error) {
				block := a.state().render(stateRecallLines)
				if block == "" {
					// Not an error: an empty store is a fact, and a model that read
					// a failure here would try again in different words.
					return "No working state yet. Track a belief or a subgoal when there is something a future compaction must not lose.", false, nil
				}
				return block, false, nil
			},
		},
	}
}
