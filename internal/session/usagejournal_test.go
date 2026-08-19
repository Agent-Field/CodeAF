package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// journalUsageLines is every usage line in one journal, in file order. It reads
// the file rather than the replay because these tests are about what was
// WRITTEN: the replay's own sums are asserted separately, and a test that only
// ever read them back could not tell one honest line from two halves of it.
func journalUsageLines(t *testing.T, path string) []journalUsage {
	t.Helper()
	var used []journalUsage
	for _, line := range readLines(t, path) {
		var entry sessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type != "usage" || entry.Usage == nil {
			continue
		}
		used = append(used, *entry.Usage)
	}
	return used
}

// turnUsageLines is the subset a turn of the person's wrote — the auxiliary
// lines beside them are the session's own errands (see [journalUsage]).
func turnUsageLines(t *testing.T, path string) []journalUsage {
	t.Helper()
	var turns []journalUsage
	for _, used := range journalUsageLines(t, path) {
		if !used.Aux {
			turns = append(turns, used)
		}
	}
	return turns
}

// quietCompleter answers every call with words and NO accounting at all, which
// is what a provider that reports no usage looks like from here.
type quietCompleter struct{}

func (quietCompleter) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{
		Role:    "assistant",
		Content: []ai.ContentPart{{Type: "text", Text: "nothing to bill"}},
	}}}}, nil
}

// A line type this build does not know is READ AND SKIPPED, never turned into
// something a person said. This is the property the whole format rests on: a
// usage line was added without bumping sessionFileVersion, so an older aforge
// has to resume a newer file as exactly the conversation it always was.
func TestAnUnknownEntryTypeIsSkippedWithoutBecomingAMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path,
		`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"what did that cost?","timestamp":"t"}`,
		`{"type":"usage","usage":{"model":"m","input":10,"output":5,"costUsd":0.25,"calls":1},"timestamp":"t"}`,
		`{"type":"something-else","content":"a line from a build that does not exist yet","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"a quarter","timestamp":"t"}`,
	)

	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !replayed.existed {
		t.Fatal("a file holding lines this build skips was reported as new")
	}
	if got := rolesOf(replayed.messages); !equalStrings(got, []string{"user", "assistant"}) {
		t.Fatalf("restored roles = %v, want the two messages and nothing else", got)
	}
	if got := replayed.usage.CostUSD; got != 0.25 {
		t.Fatalf("restored cost = %v, want the usage line's 0.25", got)
	}
}

// One turn, one line: the model it rode, what it spent and how long it took.
func TestOneCompletedTurnJournalsExactlyOneUsageLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			// A turn with no duration at all would make the assertion below
			// about a field the emptiness law drops, so the step takes long
			// enough to be a millisecond.
			time.Sleep(20 * time.Millisecond)
			return textResponse("a quarter"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = path })
	collect(t, mustSubmit(t, agent, "what did that cost?"))
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	turns := turnUsageLines(t, path)
	if len(turns) != 1 {
		t.Fatalf("journal holds %d turn usage lines, want exactly 1", len(turns))
	}
	line := turns[0]
	if line.Model != "test/model" {
		t.Fatalf("usage line model = %q, want the model the turn rode", line.Model)
	}
	if line.Input != 10 || line.Output != 5 || line.Calls != 1 {
		t.Fatalf("usage line = %+v, want the turn's own tokens and one call", line)
	}
	if line.DurationMS <= 0 {
		t.Fatalf("usage line duration = %dms, want the turn's wall time", line.DurationMS)
	}
}

// A TURN THAT SPENT NOTHING WRITES NOTHING. The emptiness law reaches the file
// too: a provider that reported no usage leaves no row rather than a row of
// zeroes that reads as a turn which cost nothing to run.
func TestATurnThatSpentNothingJournalsNoUsageLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, quietCompleter{}, func(config *Config) { config.SessionFile = path })
	collect(t, mustSubmit(t, agent, "say something free"))
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if used := journalUsageLines(t, path); len(used) != 0 {
		t.Fatalf("journal holds %d usage lines for a turn that spent nothing: %+v", len(used), used)
	}
}

// The round trip this whole change exists for: a conversation reopened tomorrow
// knows what it cost today. Before the usage lines, /cost on a resumed session
// reported nothing for a session that had spent real money.
func TestAResumedSessionComesBackHoldingItsSpend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return pricedResponse("that will be forty cents", 0.40), nil
		},
	}}
	first, workspace := newTestAgent(t, completer, func(config *Config) { config.SessionFile = path })
	collect(t, mustSubmit(t, first, "what does this cost?"))
	spent := first.Usage()
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if spent.CostUSD < 0.40 {
		t.Fatalf("the live session reported %v, want at least the turn's 0.40", spent.CostUSD)
	}

	second, err := newAgent(Config{
		Workspace: workspace, Model: "test/model", System: "SYSTEM", SessionFile: path,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	restored := second.Usage()
	if restored.CostUSD != spent.CostUSD {
		t.Fatalf("the resumed session reports %v, want the %v it spent", restored.CostUSD, spent.CostUSD)
	}
	if restored.Input != spent.Input || restored.Output != spent.Output {
		t.Fatalf("restored tokens = %d/%d, want the %d/%d it spent",
			restored.Input, restored.Output, spent.Input, spent.Output)
	}
	if restored.Calls != spent.Calls || restored.Turns != spent.Turns {
		t.Fatalf("restored calls/turns = %d/%d, want %d/%d",
			restored.Calls, restored.Turns, spent.Calls, spent.Turns)
	}
}

// A COMPACTION DOES NOT UN-SPEND ANYTHING. The marker replaces the message
// window, and the replay's usage accumulator must survive it: the money was
// spent on a conversation, not on the part of it that is still in context.
func TestACompactionDoesNotResetTheRestoredTotal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path,
		`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the old question","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"the old answer","timestamp":"t"}`,
		`{"type":"usage","usage":{"model":"m","input":900,"output":100,"costUsd":1.5,"calls":1},"timestamp":"t"}`,
		`{"type":"compaction","tokensBefore":84000,"stubbed":1,"folded":2,"timestamp":"t"}`,
		`{"type":"message","role":"user","content":"and now?","timestamp":"t"}`,
		`{"type":"usage","usage":{"model":"m","input":100,"output":20,"costUsd":0.5,"calls":1},"timestamp":"t"}`,
	)

	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if got := rolesOf(replayed.messages); !equalStrings(got, []string{"user"}) {
		t.Fatalf("restored roles = %v, want only the window after the marker", got)
	}
	if got := replayed.usage.CostUSD; got != 2.0 {
		t.Fatalf("restored cost = %v, want both lines' 2.0 — a compaction does not un-spend", got)
	}
	if got := replayed.usage.Input; got != 1000 {
		t.Fatalf("restored input = %d, want both lines' 1000", got)
	}
}

// An auxiliary call is the person's money and NOT a step of the conversation,
// and the line says which it was. Calls counts every request; Turns counts only
// the ones a turn of theirs made.
func TestAnAuxiliaryCallJournalsAnAuxLineAndReplaysAsACallNotATurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.SessionFile = path })
	agent.addAuxiliaryUsage(pricedResponse("eight words", 0.02), "test/namer", 1)
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	lines := journalUsageLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("journal holds %d usage lines, want exactly the auxiliary one", len(lines))
	}
	if !lines[0].Aux {
		t.Fatal("the auxiliary call was journaled as a turn of the conversation")
	}
	if lines[0].Model != "test/namer" {
		t.Fatalf("aux line model = %q, want the model the call actually rode", lines[0].Model)
	}

	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replayed.usage.Calls != 1 {
		t.Fatalf("restored calls = %d, want the one request that was made", replayed.usage.Calls)
	}
	if replayed.usage.Turns != 0 {
		t.Fatalf("restored turns = %d, want 0 — no turn asked for that call", replayed.usage.Turns)
	}
	if replayed.usage.CostUSD != 0.02 {
		t.Fatalf("restored cost = %v, want the call's 0.02", replayed.usage.CostUSD)
	}
}

// A journal written before usage lines existed replays to a session that spent
// nothing — which is what it always did, and what an older aforge still writes.
func TestAJournalWithNoUsageLinesReplaysToNoSpend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path,
		`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"hello","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"hello yourself","timestamp":"t"}`,
		`{"type":"title","title":"Greetings","timestamp":"t"}`,
	)

	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replayed.usage != (Usage{}) {
		t.Fatalf("restored usage = %+v, want nothing at all", replayed.usage)
	}
}

// THE LIVE FIGURES, not the replayed ones: a session reports every request it
// made under Calls, and only the conversation's own steps under Turns. It is
// what makes Calls the honest denominator for the bill beside it — /cost prints
// this one (internal/tui3's statusnote.go), because the money above it was spent
// over every request and not only over the turns that asked for some of them.
func TestASessionsCallCountHoldsTheAuxiliaryCallsItsTurnCountDoesNot(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	collect(t, mustSubmit(t, agent, "hello"))

	turns, calls := agent.Usage().Turns, agent.Usage().Calls
	agent.addAuxiliaryUsage(pricedResponse("eight words", 0.02), "test/namer", 1)

	used := agent.Usage()
	if used.Calls != calls+1 {
		t.Fatalf("calls went %d → %d over one auxiliary request", calls, used.Calls)
	}
	if used.Turns != turns {
		t.Fatalf("turns went %d → %d: nobody took a turn to make that call", turns, used.Turns)
	}
	if used.CostUSD < 0.02 {
		t.Fatalf("the auxiliary call's 0.02 is missing from the session's %.4f", used.CostUSD)
	}
}
