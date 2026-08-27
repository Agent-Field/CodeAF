package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/reflex"
)

// ── the merge ───────────────────────────────────────────────────────────────

// A goal REPLACES and every list APPENDS, because a goal is one fact a turn can
// revise and a list is an accumulation. An empty goal says nothing rather than
// erasing what is there.
func TestStateCardMergeReplacesTheGoalAndAppendsTheLists(t *testing.T) {
	card := newCardStore(filepath.Join(t.TempDir(), "card.json"))

	if !card.merge(reflex.StateDelta{
		Goal: "ship the parser",
		Done: []string{"read the spec"},
		Next: []string{"write the replay"},
	}) {
		t.Fatal("the first delta changed nothing")
	}
	if !card.merge(reflex.StateDelta{
		Goal: "ship the parser and its replay",
		Done: []string{"wrote the cut"},
	}) {
		t.Fatal("the second delta changed nothing")
	}

	got := card.snapshot()
	if got.Goal != "ship the parser and its replay" {
		t.Fatalf("goal = %q, want the newer one", got.Goal)
	}
	if !equalStrings(got.Done, []string{"read the spec", "wrote the cut"}) {
		t.Fatalf("done = %v, want both, in order", got.Done)
	}
	if !equalStrings(got.Next, []string{"write the replay"}) {
		t.Fatalf("next = %v, want the earlier list untouched", got.Next)
	}

	// An empty delta is not an erasure, and it is not a change either.
	if card.merge(reflex.StateDelta{}) {
		t.Fatal("an empty delta reported a change")
	}
	if after := card.snapshot(); after.Goal != got.Goal || !equalStrings(after.Done, got.Done) {
		t.Fatalf("an empty delta rewrote the card: %+v", after)
	}
}

// The same line said twice is one line. Exact-string dedupe, because a fuzzy
// match would quietly merge "read config.go" with "wrote config.go".
func TestStateCardMergeDeduplicatesExactly(t *testing.T) {
	card := newCardStore(filepath.Join(t.TempDir(), "card.json"))
	card.merge(reflex.StateDelta{Done: []string{"ran the tests"}})
	if card.merge(reflex.StateDelta{Done: []string{"ran   the tests"}}) {
		t.Fatal("the same line, differently spaced, was added twice")
	}
	if got := card.snapshot().Done; len(got) != 1 {
		t.Fatalf("done = %v, want one entry", got)
	}
	if !card.merge(reflex.StateDelta{Done: []string{"ran the tests again"}}) {
		t.Fatal("a genuinely new line was dropped")
	}
}

// A card that grew would be the memory.md failure again: paid for on every
// request forever. The caps drop the OLDEST, because what landed an hour ago is
// history and the card is a statement about now.
func TestStateCardCapsDropTheOldest(t *testing.T) {
	card := newCardStore(filepath.Join(t.TempDir(), "card.json"))
	for index := 0; index < cardMaxDone+4; index++ {
		card.merge(reflex.StateDelta{Done: []string{"landed step " + string(rune('a'+index))}})
	}
	done := card.snapshot().Done
	if len(done) != cardMaxDone {
		t.Fatalf("done holds %d entries, want the cap of %d", len(done), cardMaxDone)
	}
	if done[0] != "landed step e" {
		t.Fatalf("done starts at %q, want the four oldest gone", done[0])
	}
	if done[len(done)-1] != "landed step p" {
		t.Fatalf("done ends at %q, want the newest kept", done[len(done)-1])
	}

	// The other lists carry the tighter cap.
	for index := 0; index < cardMaxItems+3; index++ {
		card.merge(reflex.StateDelta{Open: []string{"question " + string(rune('a'+index))}})
	}
	if open := card.snapshot().Open; len(open) != cardMaxItems {
		t.Fatalf("open holds %d entries, want the cap of %d", len(open), cardMaxItems)
	}
}

// ── the file ────────────────────────────────────────────────────────────────

// The card is the thing a resumed conversation knows about itself before its
// first turn, so it has to come back off disk exactly as it went.
func TestStateCardSurvivesAReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "card.json")
	first := newCardStore(path)
	first.merge(reflex.StateDelta{
		Goal:     "ship the parser",
		Done:     []string{"read the spec"},
		Inflight: []string{"the replay"},
		Refs:     []string{"internal/session/sessionfile.go"},
	})
	want := first.snapshot()

	second := newCardStore(path)
	second.load()
	got := second.snapshot()
	if got.Goal != want.Goal || !equalStrings(got.Done, want.Done) ||
		!equalStrings(got.Inflight, want.Inflight) || !equalStrings(got.Refs, want.Refs) {
		t.Fatalf("reloaded card = %+v, want %+v", got, want)
	}
	if got.UpdatedSeq != want.UpdatedSeq {
		t.Fatalf("reloaded revision = %d, want %d", got.UpdatedSeq, want.UpdatedSeq)
	}

	// The write is atomic and leaves nothing behind.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("the temporary file survived the rename: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read card: %v", err)
	}
	var document stateCard
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatalf("card.json does not parse: %v", err)
	}
	if document.Goal != "ship the parser" || document.UpdatedSeq == 0 {
		t.Fatalf("card document = %+v", document)
	}
}

// A card file this build cannot read costs the session a block, never the
// session itself.
func TestStateCardIgnoresACorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "card.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	card := newCardStore(path)
	card.load()
	if got := card.snapshot(); got.Goal != "" || len(got.Done) != 0 {
		t.Fatalf("a corrupt file was half-loaded: %+v", got)
	}
}

// ── the note the card rides in ──────────────────────────────────────────────

// volatileNote lands the volatile note the way a request does — the drain at a
// step boundary (agent.go) — and answers with what the model would read. It is
// the honest reading of "what the card puts in front of the model": the card
// stopped being part of message[0] when the cache autopsy showed that a block
// which moves every time a delta lands re-prices the entire conversation behind
// it (memory.go's refreshSystemLocked).
func volatileNote(agent *Agent) string {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.landVolatileLocked()
	return agent.lastVolatileNoteLocked()
}

// An empty card renders NOTHING — not an empty <state> block, not a heading
// over nothing, and no note at all to carry them. The emptiness law.
func TestStateCardRidesTheTailNoteOnlyWhenItHasSomethingToSay(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "transcript.jsonl")
	})

	agent.mu.Lock()
	system := messageText(agent.messages[0])
	agent.mu.Unlock()
	if system != "SYSTEM" {
		t.Fatalf("a fresh session's prompt = %q, want the base and nothing else", system)
	}
	if note := volatileNote(agent); note != "" {
		t.Fatalf("a session with nothing to say landed a note:\n%s", note)
	}

	agent.mergeStateCard(reflex.StateDelta{
		Goal: "ship the parser",
		Next: []string{"write the replay"},
	})

	note := volatileNote(agent)
	for _, want := range []string{"<state>", "goal: ship the parser", "next:", "- write the replay", "</state>"} {
		if !strings.Contains(note, want) {
			t.Fatalf("the card block is missing %q:\n%s", want, note)
		}
	}
	// Sections with nothing in them are absent entirely.
	for _, unwanted := range []string{"done:", "in flight:", "open:", "refs:"} {
		if strings.Contains(note, unwanted) {
			t.Fatalf("an empty section was printed (%q):\n%s", unwanted, note)
		}
	}
	// And the base prompt is untouched by any of it: the card moved out of
	// message[0] and nothing about a merge may put it back.
	agent.mu.Lock()
	system = messageText(agent.messages[0])
	agent.mu.Unlock()
	if system != "SYSTEM" {
		t.Fatalf("a card merge rewrote the system message: %q", system)
	}

	// And a REPLACEMENT, never a stack: the newest note carries the second
	// merge's block and only that.
	agent.mergeStateCard(reflex.StateDelta{Goal: "ship the parser and its replay"})
	note = volatileNote(agent)
	if strings.Count(note, "<state>") != 1 {
		t.Fatalf("the card block stacked:\n%s", note)
	}
	if strings.Contains(note, "goal: ship the parser\n") {
		t.Fatalf("the old goal survived the replacement:\n%s", note)
	}
}

// The card is read AFTER the memory block, and now it is a whole transcript
// that separates them rather than two lines of one string. Memory is what is
// true across conversations and holds for the life of this one, so it stays in
// message[0]; the card is what is true this minute, so it rides at the tail
// where a change costs the note and nothing behind it.
func TestStateCardRendersAfterTheMemoryBlock(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mergeStateCard(reflex.StateDelta{Goal: "ship the parser"})

	agent.mu.Lock()
	agent.memoryText = "\n<memory>\n- a remembered line\n</memory>\n"
	agent.refreshSystemLocked()
	system := messageText(agent.messages[0])
	agent.mu.Unlock()

	if !strings.Contains(system, "<memory>") {
		t.Fatalf("the memory block left message[0]:\n%s", system)
	}
	if strings.Contains(system, "<state>") {
		t.Fatalf("the card is back in message[0], in front of the whole transcript:\n%s", system)
	}
	if note := volatileNote(agent); !strings.Contains(note, "<state>") {
		t.Fatalf("the card reached nothing the model reads:\n%s", note)
	}
}

// A session with a card.json beside its transcript is holding the card before
// its first turn runs — a resumed conversation must not have to wait for a
// post-turn pass to be told what it is doing.
func TestStateCardIsInTheFirstRequestOnResume(t *testing.T) {
	directory := t.TempDir()
	transcript := filepath.Join(directory, placeTranscript)
	card := newCardStore(filepath.Join(directory, placeCard))
	card.merge(reflex.StateDelta{Goal: "ship the parser"})

	agent, err := newAgent(Config{
		Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM", SessionFile: transcript,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })

	if note := volatileNote(agent); !strings.Contains(note, "goal: ship the parser") {
		t.Fatalf("the resumed session did not carry its card:\n%s", note)
	}
}
