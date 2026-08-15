package session

// The idle consolidation pass (memory_consolidate.go). What is asserted here is
// the pass's contract rather than its wording: the file is swapped only on a
// result that obeys the two laws, the times inside it come through byte for
// byte, the guards refuse the calls nobody should pay for, and the timer stands
// down the moment the person is back.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the fake clock ──────────────────────────────────────────────────────────

// fakeIdle is the injectable half of [idleClock]: a time that only moves when a
// test moves it, and an alarm that only fires when a test fires it. It is what
// makes "a new turn disarms the timer" an assertion rather than a sleep.
type fakeIdle struct {
	mu      sync.Mutex
	at      time.Time
	fire    func()
	armed   int
	stopped int
}

func newFakeIdle() *fakeIdle {
	return &fakeIdle{at: time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)}
}

func (f *fakeIdle) clock() idleClock {
	return idleClock{
		now: func() time.Time {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.at
		},
		afterFunc: func(_ time.Duration, fire func()) idleAlarm {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.armed++
			f.fire = fire
			return fakeAlarm{owner: f}
		},
	}
}

type fakeAlarm struct{ owner *fakeIdle }

func (a fakeAlarm) Stop() bool {
	a.owner.mu.Lock()
	defer a.owner.mu.Unlock()
	a.owner.stopped++
	a.owner.fire = nil
	return true
}

func (f *fakeIdle) advance(d time.Duration) {
	f.mu.Lock()
	f.at = f.at.Add(d)
	f.mu.Unlock()
}

func (f *fakeIdle) counts() (armed, stopped int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.armed, f.stopped
}

func (f *fakeIdle) pending() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fire != nil
}

// trigger fires the armed alarm, synchronously, so the test sees the pass's
// effects without waiting for anything.
func (f *fakeIdle) trigger(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	fire := f.fire
	f.fire = nil
	f.mu.Unlock()
	if fire == nil {
		t.Fatal("no idle timer is armed")
	}
	fire()
}

// ── fixtures ────────────────────────────────────────────────────────────────

// growingMemory is a memory file the way one actually gets: the same preference
// stated twice, a stale fact beside the fact that replaced it, and several
// lines carrying dates, durations and versions — the ones a summarizer drops
// first, which is why they are here.
var growingMemory = []string{
	"- prefers tabs over spaces in Go",
	"- likes tabs, not spaces, in Go files",
	"- the release train ships on 2026-08-15",
	"- deploys are frozen since Tuesday",
	"- the API pin is v2.1.4 and must not move",
	"- runs the full suite before every push",
	"- runs go test ./... before pushing",
	"- reviews land within 2 days",
	"- prefers short commit messages",
	"- the staging cluster is eu-west-1",
	"- the staging cluster moved to eu-central-1 in Q3",
	"- hates being asked before a read-only command",
	"- the postgres version is 16.2",
	"- the migration window is Sunday 03:00",
	"- writes docs in British English",
	"- the team standup is at 09:30",
	"- never force-pushes to master",
	"- the design doc lives in docs/CHAT-V3.md",
	"- prefers make check over ad-hoc builds",
	"- the on-call rotation is 7 days",
	"- dislikes emoji in commit messages",
	"- no emoji anywhere in commits",
	"- the linter config is frozen until 2026-09-01",
	"- the benchmark budget is 30 minutes",
}

// consolidatedMemory is what a well-behaved consolidator returns for it: the
// three duplicate pairs merged, the superseded cluster dropped, and every
// temporal line copied character-for-character.
var consolidatedMemory = []string{
	"- prefers tabs over spaces in Go",
	"- the release train ships on 2026-08-15",
	"- deploys are frozen since Tuesday",
	"- the API pin is v2.1.4 and must not move",
	"- runs go test ./... before pushing",
	"- reviews land within 2 days",
	"- prefers short commit messages",
	"- the staging cluster moved to eu-central-1 in Q3",
	"- hates being asked before a read-only command",
	"- the postgres version is 16.2",
	"- the migration window is Sunday 03:00",
	"- writes docs in British English",
	"- the team standup is at 09:30",
	"- never force-pushes to master",
	"- the design doc lives in docs/CHAT-V3.md",
	"- prefers make check over ad-hoc builds",
	"- the on-call rotation is 7 days",
	"- no emoji anywhere in commits",
	"- the linter config is frozen until 2026-09-01",
	"- the benchmark budget is 30 minutes",
}

// consolidatingAgent is a session with a memory file holding facts, a journal
// to write the count line into, and a clock the test owns.
func consolidatingAgent(t *testing.T, completer Completer, facts []string) (*Agent, string, string, *fakeIdle) {
	t.Helper()
	dir := t.TempDir()
	memory := filepath.Join(dir, "memory.md")
	journal := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(memory, []byte(strings.Join(facts, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write memory: %v", err)
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.MemoryFile = memory
		config.SessionFile = journal
		// A session with a surface attached — the only kind that dreams.
		config.AskConsent = true
	})
	clock := newFakeIdle()
	agent.memory.idle.clock = clock.clock()
	return agent, memory, journal, clock
}

func factLines(t *testing.T, path string) []string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var lines []string
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// replies with a fixed list, and remembers what it was asked.
func listReply(facts []string) step {
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse(strings.Join(facts, "\n")), nil
	}
}

// ── the pass ────────────────────────────────────────────────────────────────

// The whole contract in one test: the file the person will read next session is
// the consolidated one, and the journal says by how much.
func TestConsolidationSwapsTheFileAndJournalsTheCount(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{listReply(consolidatedMemory)}}
	agent, memory, journal, _ := consolidatingAgent(t, completer, growingMemory)

	if err := agent.ConsolidateMemory(context.Background()); err != nil {
		t.Fatalf("ConsolidateMemory: %v", err)
	}

	got := factLines(t, memory)
	if len(got) != len(consolidatedMemory) {
		t.Fatalf("memory has %d facts, want %d:\n%s", len(got), len(consolidatedMemory), strings.Join(got, "\n"))
	}
	for index, want := range consolidatedMemory {
		if got[index] != want {
			t.Fatalf("fact %d = %q, want %q", index, got[index], want)
		}
	}

	note := "memory consolidated: 24 → 20 facts"
	written, err := os.ReadFile(journal)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if !strings.Contains(string(written), note) {
		t.Fatalf("the journal has no %q line:\n%s", note, written)
	}
}

// THE MEASURED LAW (harness-research-notes.md §4): a time expression is copied,
// never paraphrased. Asserted at both ends — the instruction the consolidator is
// given carries the rule, and a consolidator that obeys it lands its dates in
// the file unchanged.
func TestConsolidationCarriesTheTimeLawAndPreservesTimesVerbatim(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{listReply(consolidatedMemory)}}
	agent, memory, _, _ := consolidatingAgent(t, completer, growingMemory)

	if err := agent.ConsolidateMemory(context.Background()); err != nil {
		t.Fatalf("ConsolidateMemory: %v", err)
	}

	instruction := messageText(completer.request(0)[0])
	for _, law := range []string{
		"CHARACTER-FOR-CHARACTER",
		"time expression",
		"Never paraphrase a time",
		"DO NOT MERGE THEM",
		"SHORTER THAN OR THE SAME LENGTH",
	} {
		if !strings.Contains(instruction, law) {
			t.Fatalf("the consolidator's prompt does not carry %q:\n%s", law, instruction)
		}
	}
	// And the file the person got back spells every time the way they did.
	stored := strings.Join(factLines(t, memory), "\n")
	for _, temporal := range []string{
		"ships on 2026-08-15",
		"frozen since Tuesday",
		"v2.1.4",
		"within 2 days",
		"in Q3",
		"16.2",
		"Sunday 03:00",
		"at 09:30",
		"7 days",
		"until 2026-09-01",
		"30 minutes",
	} {
		if !strings.Contains(stored, temporal) {
			t.Fatalf("the time expression %q did not survive:\n%s", temporal, stored)
		}
	}
}

// A reworded date is a refused pass, whatever else the answer got right. This is
// the mechanical floor under the prompt's law.
func TestConsolidationRefusesARewordedTime(t *testing.T) {
	reworded := append([]string(nil), consolidatedMemory...)
	reworded[1] = "- the release train ships on Aug 15"
	completer := &scriptedCompleter{steps: []step{listReply(reworded)}}
	agent, memory, journal, _ := consolidatingAgent(t, completer, growingMemory)

	before := factLines(t, memory)
	if err := agent.ConsolidateMemory(context.Background()); err == nil {
		t.Fatal("a reworded date was accepted")
	}
	assertUntouched(t, memory, journal, before)
}

// THE CONSERVATIVE LAW: merging and dropping can only shorten a list. Anything
// longer is a consolidator that invented something, and the file it would have
// replaced is left alone.
func TestConsolidationRefusesAListThatGrew(t *testing.T) {
	grown := append(append([]string(nil), growingMemory...), "- prefers vim keybindings")
	completer := &scriptedCompleter{steps: []step{listReply(grown)}}
	agent, memory, journal, _ := consolidatingAgent(t, completer, growingMemory)

	before := factLines(t, memory)
	if err := agent.ConsolidateMemory(context.Background()); err == nil {
		t.Fatal("a longer list was accepted")
	}
	assertUntouched(t, memory, journal, before)
}

// A provider that failed is a pass that did not happen. Nothing is swapped,
// nothing is journaled, and the error is the caller's to ignore.
func TestFailedConsolidationLeavesTheFileUntouched(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return nil, errors.New("provider is having a bad minute")
		},
	}}
	agent, memory, journal, _ := consolidatingAgent(t, completer, growingMemory)

	before := factLines(t, memory)
	if err := agent.ConsolidateMemory(context.Background()); err == nil {
		t.Fatal("a failed provider call reported success")
	}
	assertUntouched(t, memory, journal, before)
}

// Commentary is not a fact. A model that opens with a sentence has that sentence
// dropped rather than remembered — and a reply that is ONLY commentary is a
// refused pass.
func TestConsolidationRefusesAReplyWithNoFacts(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		listReply([]string{"I could not find anything to merge in this list."}),
	}}
	agent, memory, journal, _ := consolidatingAgent(t, completer, growingMemory)

	before := factLines(t, memory)
	if err := agent.ConsolidateMemory(context.Background()); err == nil {
		t.Fatal("a reply with no facts was accepted")
	}
	assertUntouched(t, memory, journal, before)
}

func assertUntouched(t *testing.T, memory, journal string, before []string) {
	t.Helper()
	after := factLines(t, memory)
	if strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatalf("the memory file changed:\n%s", strings.Join(after, "\n"))
	}
	if written, err := os.ReadFile(journal); err == nil && strings.Contains(string(written), "memory consolidated") {
		t.Fatalf("a refused pass journaled a count line:\n%s", written)
	}
}

// ── the guards ──────────────────────────────────────────────────────────────

// A file nobody has touched since the last pass cannot consolidate any further,
// so the second pass is not made. The clock is moved well past the interval so
// the ONLY thing refusing the call is the hash.
func TestUnchangedMemoryIsNotConsolidatedTwice(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{listReply(consolidatedMemory)}}
	agent, _, _, clock := consolidatingAgent(t, completer, growingMemory)

	if err := agent.ConsolidateMemory(context.Background()); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	clock.advance(20 * time.Minute)
	if err := agent.ConsolidateMemory(context.Background()); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if got := completer.requests(); got != 1 {
		t.Fatalf("the consolidator was called %d times for one unchanged file, want 1", got)
	}
}

// Twice inside ten minutes is once, even when the file did change.
func TestConsolidationHoldsToItsInterval(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{listReply(consolidatedMemory)}}
	agent, _, _, clock := consolidatingAgent(t, completer, growingMemory)

	if err := agent.ConsolidateMemory(context.Background()); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	// The person notes something new a minute later: the file has changed, so
	// the hash guard would let a pass through, and the interval is what does not.
	if _, err := agent.memory.note("prefers ripgrep over grep"); err != nil {
		t.Fatalf("note: %v", err)
	}
	clock.advance(time.Minute)
	if err := agent.ConsolidateMemory(context.Background()); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if got := completer.requests(); got != 1 {
		t.Fatalf("the consolidator was called %d times inside one interval, want 1", got)
	}
}

// A short memory has no clusters in it. Consolidating one is a provider call to
// save nothing, so it is never made.
func TestSmallMemoryIsNeverConsolidated(t *testing.T) {
	small := growingMemory[:minConsolidateFacts-1]
	completer := &scriptedCompleter{steps: []step{listReply(small[:5])}}
	agent, memory, _, _ := consolidatingAgent(t, completer, small)

	before := factLines(t, memory)
	if err := agent.ConsolidateMemory(context.Background()); err != nil {
		t.Fatalf("ConsolidateMemory: %v", err)
	}
	if got := completer.requests(); got != 0 {
		t.Fatalf("a %d-fact memory was sent to the consolidator (%d calls)", len(small), got)
	}
	if after := factLines(t, memory); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatal("a memory under the floor was rewritten")
	}
}

// ── the idle timer ──────────────────────────────────────────────────────────

// idleAgent is [consolidatingAgent] for the tests that run TURNS: the same
// memory file and the same test-owned clock, with NO session file.
//
// The journal is left off deliberately. A session with one names itself after
// its first completed turn (title.go), and that auxiliary call would sit in the
// middle of the scripted requests these tests count — an assertion about the
// idle timer would then be an assertion about the namer's position in a script.
// The count line has its own test, which calls the pass directly.
func idleAgent(t *testing.T, completer Completer, askConsent bool) (*Agent, string, *fakeIdle) {
	t.Helper()
	memory := filepath.Join(t.TempDir(), "memory.md")
	if err := os.WriteFile(memory, []byte(strings.Join(growingMemory, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write memory: %v", err)
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.MemoryFile = memory
		config.AskConsent = askConsent
	})
	clock := newFakeIdle()
	agent.memory.idle.clock = clock.clock()
	return agent, memory, clock
}

// The countdown is armed when the turn settles, and firing it consolidates.
func TestIdleTimerArmsAfterATurnAndFiresThePass(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
		listReply(consolidatedMemory),
	}}
	agent, memory, clock := idleAgent(t, completer, true)

	collect(t, mustSubmit(t, agent, "hello"))
	if armed, _ := clock.counts(); armed != 1 {
		t.Fatalf("the idle timer was armed %d times after one turn, want 1", armed)
	}

	clock.trigger(t)
	if got := len(factLines(t, memory)); got != len(consolidatedMemory) {
		t.Fatalf("memory has %d facts after the idle pass, want %d", got, len(consolidatedMemory))
	}
}

// The person is back: the countdown stands down before the turn does anything.
func TestNewTurnDisarmsTheIdleTimer(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("one"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("two"), nil },
	}}
	agent, _, clock := idleAgent(t, completer, true)

	collect(t, mustSubmit(t, agent, "hello"))
	if !clock.pending() {
		t.Fatal("no idle timer was armed by the first turn")
	}

	collect(t, mustSubmit(t, agent, "and another thing"))
	if _, stopped := clock.counts(); stopped == 0 {
		t.Fatal("a new turn did not disarm the idle timer")
	}
	// Two turns, two requests: the second turn re-arms at its own end, and
	// nothing consolidated in between.
	if got := completer.requests(); got != 2 {
		t.Fatalf("%d provider requests for two turns, want 2 — a pass ran through a turn", got)
	}
}

// A steering note is the session in use, even when the session wrote it itself
// (a background job's completion line).
func TestSteeringNoteDisarmsTheIdleTimer(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	agent, _, clock := idleAgent(t, completer, true)

	collect(t, mustSubmit(t, agent, "hello"))
	agent.enqueueSteering("the dev server exited with status 1")
	if _, stopped := clock.counts(); stopped == 0 {
		t.Fatal("a steering note did not disarm the idle timer")
	}
	if clock.pending() {
		t.Fatal("the idle timer is still armed after a steering note")
	}
}

// Close is the lights going out.
func TestCloseDisarmsTheIdleTimer(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	agent, _, clock := idleAgent(t, completer, true)

	collect(t, mustSubmit(t, agent, "hello"))
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if clock.pending() {
		t.Fatal("the idle timer survived Close")
	}
}

// A headless --once run is a process that is about to exit. It has no surface
// answering questions (AskConsent false), and it arms nothing.
func TestHeadlessOnceArmsNoIdleTimer(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	agent, _, clock := idleAgent(t, completer, false)

	collect(t, mustSubmit(t, agent, "one question, then exit"))
	if armed, _ := clock.counts(); armed != 0 {
		t.Fatalf("a --once run armed the idle timer %d times, want 0", armed)
	}
}
