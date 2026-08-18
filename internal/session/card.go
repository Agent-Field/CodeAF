package session

// The state card is what this conversation is FOR and where it stands, kept
// outside the transcript and rebuilt in front of the model on every turn.
//
// It exists because compaction stopped being a model call. The old pass paid a
// summarizer, at the worst possible moment, to answer a question nobody had
// asked until the window filled: what was this conversation about? The card
// answers it CONTINUOUSLY instead — the post-turn extractor already reads every
// exchange (memory.go), and it was already returning a state delta that nothing
// read ([reflex.ExtractResult.State]). Folding that delta into a small file
// amortizes the whole cost across the turns that produced it, and leaves
// compaction with nothing to do but rearrange.
//
// ── WHY THIS IS NOT state.json ──
//
// state.go's working state is the MODEL'S OWN notebook: it writes to it with
// `track` and `commit`, it names records by id, and nothing writes there that
// the model did not decide to write. This card is written by nobody's hand — it
// is what a cheap reader noticed about an exchange after the fact. Two files,
// two authors, and a corruption in either costs only its own author's record.
//
// ── THE CAPS ARE THE WHOLE DESIGN ──
//
// A card that grew would be the memory.md failure again: paid for on every
// request, forever, whether or not the turn had anything to do with it. So every
// list is bounded ([cardMaxDone], [cardMaxRefs], [cardMaxItems]) and the OLDEST
// entries go first — what landed an hour ago is history, and the card is a
// statement about now.

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/reflex"
)

const (
	// The caps, in the order the card renders. Done is the longest because it
	// is the section a resumed conversation is most often asked about — "what
	// have we already finished" — and refs longer still because a path costs a
	// dozen characters and is the cheapest thing here to be right about.
	cardMaxDone  = 12
	cardMaxRefs  = 16
	cardMaxItems = 8

	// cardLineRunes bounds ONE entry. The extractor is asked for short lines and
	// almost always gives them; this is the stop against the turn where it hands
	// back a paragraph and quietly triples what every future request carries.
	cardLineRunes = 240
)

// stateCard is the card as it sits on disk and as it renders.
//
// UpdatedSeq counts merges rather than turns: it is the card's own revision
// number, and its only job is to make a card that changed distinguishable from
// one that was merely re-saved.
type stateCard struct {
	Goal       string   `json:"goal,omitempty"`
	Done       []string `json:"done,omitempty"`
	Inflight   []string `json:"inflight,omitempty"`
	Next       []string `json:"next,omitempty"`
	Open       []string `json:"open,omitempty"`
	Refs       []string `json:"refs,omitempty"`
	UpdatedSeq int      `json:"updated_seq"`
}

// cardStore is the card plus the file it lives in. It holds its own lock for
// [memoryBrain]'s reason: its writer is the post-turn goroutine, which outlives
// the turn that started it and must never contend for the lock Interrupt takes.
type cardStore struct {
	path string

	mu     sync.Mutex
	loaded bool
	card   stateCard
}

func newCardStore(path string) *cardStore { return &cardStore{path: path} }

// cardFile is where THIS session keeps its card: the folder's card.json under
// the session layout, and a stem-derived sidecar for the legacy flat one — the
// same two answers [Config.stateFile] gives, derived the same way.
func (c Config) cardFile() string {
	if path := c.Place.Card(); path != "" {
		return path
	}
	sessionFile := strings.TrimSpace(c.SessionFile)
	if sessionFile == "" {
		return ""
	}
	if filepath.Base(sessionFile) == placeTranscript {
		return filepath.Join(filepath.Dir(sessionFile), placeCard)
	}
	return strings.TrimSuffix(sessionFile, filepath.Ext(sessionFile)) + ".card.json"
}

// card is the agent's card, built and rehydrated on first use — [Agent.state]'s
// shape, for [Agent.state]'s reason.
func (a *Agent) card() *cardStore {
	a.cardOnce.Do(func() {
		a.cardStore = newCardStore(a.config.cardFile())
		a.cardStore.load()
	})
	return a.cardStore
}

// load rehydrates the card, and a file it cannot fully read is dropped WHOLE
// with one log line. Same asymmetry [stateStore.load] states: a bad byte in a
// bookkeeping file may not cost somebody their conversation.
func (s *cardStore) load() {
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
		if !os.IsNotExist(err) {
			log.Printf("session: ignoring unreadable state card %s: %v", s.path, err)
		}
		return
	}
	var card stateCard
	if err := json.Unmarshal(content, &card); err != nil {
		log.Printf("session: ignoring corrupt state card %s", s.path)
		return
	}
	s.card = card
	s.card.clamp()
}

// merge folds one exchange's delta into the card and reports whether anything
// actually moved.
//
// THE GOAL REPLACES AND EVERY LIST APPENDS. A goal is a single fact that a turn
// can revise — the person changed their mind, or said what they meant more
// precisely — so a non-empty one wins outright and an empty one says nothing at
// all. The lists are accumulations, deduplicated by EXACT string because that is
// the only comparison that cannot be wrong: two lines that differ by a word are
// two things the extractor chose to say separately, and a fuzzy match here would
// quietly merge "read config.go" with "wrote config.go".
func (s *cardStore) merge(delta reflex.StateDelta) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	before := s.card
	if goal := cardLine(delta.Goal); goal != "" {
		s.card.Goal = goal
	}
	s.card.Done = cardAppend(s.card.Done, delta.Done, cardMaxDone)
	s.card.Inflight = cardAppend(s.card.Inflight, delta.Inflight, cardMaxItems)
	s.card.Next = cardAppend(s.card.Next, delta.Next, cardMaxItems)
	s.card.Open = cardAppend(s.card.Open, delta.Open, cardMaxItems)
	s.card.Refs = cardAppend(s.card.Refs, delta.Refs, cardMaxRefs)
	if s.card.same(before) {
		return false
	}
	s.card.UpdatedSeq++
	if err := s.saveLocked(); err != nil {
		// A card that did not reach disk is still the card this session is
		// running on. The loss is one resume's worth of context, which is not
		// worth telling anybody about mid-conversation.
		log.Printf("session: could not save the state card %s: %v", s.path, err)
	}
	return true
}

// snapshot is the card as it stands, safe to read outside the lock.
func (s *cardStore) snapshot() stateCard {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.card.clone()
}

// saveLocked writes the file through a temporary and a rename, for
// [stateStore.saveLocked]'s reason: a rename is the one write that cannot
// half-happen, and a truncated card is a card the next session logs and drops.
func (s *cardStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	encoded, err := json.MarshalIndent(s.card, "", "  ")
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

// ── rendering ───────────────────────────────────────────────────────────────

// stateCardText is the block the system prompt carries, or "" when the card has
// nothing to say. An empty section is not printed at all — the emptiness law:
// "next:" over nothing is a heading that says a conversation has no next steps,
// which is not the same claim as saying nothing.
//
//	<state>
//	goal: ship the JSONL replay
//	done:
//	- replay drops the pre-cut transcript
//	in flight:
//	- the fold marker's id range
//	refs:
//	- internal/session/sessionfile.go
//	</state>
func (a *Agent) stateCardText() string { return a.card().text() }

func (s *cardStore) text() string {
	return s.snapshot().render()
}

func (c stateCard) render() string {
	var body strings.Builder
	if c.Goal != "" {
		body.WriteString("goal: " + c.Goal + "\n")
	}
	section := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		body.WriteString(title + ":\n")
		for _, item := range items {
			body.WriteString("- " + item + "\n")
		}
	}
	section("done", c.Done)
	section("in flight", c.Inflight)
	section("next", c.Next)
	section("open", c.Open)
	section("refs", c.Refs)
	if body.Len() == 0 {
		return ""
	}
	return "\n<state>\n" + body.String() + "</state>\n"
}

// ── the small rules ─────────────────────────────────────────────────────────

// cardAppend adds what is new to the end of a list and drops from the FRONT
// when it overflows. Oldest first, because a cap that dropped the newest would
// freeze the card at whatever the conversation happened to be about first.
func cardAppend(existing, incoming []string, limit int) []string {
	if len(incoming) == 0 {
		return existing
	}
	seen := make(map[string]bool, len(existing))
	for _, item := range existing {
		seen[item] = true
	}
	next := existing
	for _, raw := range incoming {
		item := cardLine(raw)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		next = append(next, item)
	}
	if len(next) > limit {
		next = append([]string(nil), next[len(next)-limit:]...)
	}
	return next
}

// cardLine is one entry, flattened to a single line and bounded. Newlines are
// what turn one entry into three when the block is read back.
func cardLine(text string) string {
	line := strings.Join(strings.Fields(text), " ")
	if runes := []rune(line); len(runes) > cardLineRunes {
		line = strings.TrimSpace(string(runes[:cardLineRunes])) + "…"
	}
	return line
}

// clamp applies the caps to a card that arrived from disk, so a file written by
// a build with looser limits — or edited by hand — cannot put an unbounded block
// in front of every request.
func (c *stateCard) clamp() {
	c.Goal = cardLine(c.Goal)
	c.Done = cardAppend(nil, c.Done, cardMaxDone)
	c.Inflight = cardAppend(nil, c.Inflight, cardMaxItems)
	c.Next = cardAppend(nil, c.Next, cardMaxItems)
	c.Open = cardAppend(nil, c.Open, cardMaxItems)
	c.Refs = cardAppend(nil, c.Refs, cardMaxRefs)
}

func (c stateCard) clone() stateCard {
	c.Done = append([]string(nil), c.Done...)
	c.Inflight = append([]string(nil), c.Inflight...)
	c.Next = append([]string(nil), c.Next...)
	c.Open = append([]string(nil), c.Open...)
	c.Refs = append([]string(nil), c.Refs...)
	return c
}

func (c stateCard) same(other stateCard) bool {
	return c.Goal == other.Goal &&
		equalCardList(c.Done, other.Done) &&
		equalCardList(c.Inflight, other.Inflight) &&
		equalCardList(c.Next, other.Next) &&
		equalCardList(c.Open, other.Open) &&
		equalCardList(c.Refs, other.Refs)
}

func equalCardList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}
