package session

// THE CONVERSATION ABOVE A COMPACTION, AND WHY IT IS A SPLICE.
//
// A compacted session's own record of what it had said stopped at the marker.
// The journal held every original line above it and nothing could reach them, so
// a surface scrolling back read the pass's shortened copy — stubs where the tool
// output was, one line where a long run of work had been — and had no way to ask
// for the words themselves.
//
// The correction is NOT "there is more conversation above the marker". The pass
// re-journals its whole rebuilt window BELOW the marker, so the same
// conversation is in the file twice: once as it happened, above, and once
// rewritten, below. [EarlierHistory] is therefore a region AND a floor, and the
// conversation told once is `Entries` followed by `Transcript()[Floor:]`.
//
// What is asserted here: the region is taken with the reducer's own state (a
// rewind before the marker stays taken back), the floor comes from the file
// rather than from a guess, and a file that does not carry the floor is offered
// no region at all rather than a doubled conversation.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the region the journal keeps ────────────────────────────────────────────

// THE CONVERSATION ABOVE THE MARKER COMES BACK WHOLE, in its original lines,
// while the live transcript is untouched and the floor says how much of it the
// region replaces.
func TestReplayKeepsTheConversationAboveTheLatestCompaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path,
		`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the very first question","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"the very first answer","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the newest question","timestamp":"t"}`,
		// The pass rewrote those three and journaled all three again.
		`{"type":"compaction","stubbed":3,"window":3,"tokensBefore":84000,"timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the very first question","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"[folded]","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the newest question","timestamp":"t"}`,
		// And this turn happened after the pass.
		`{"type":"message","role":"assistant","content":"the answer after","timestamp":"t"}`,
	)

	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if got := messageTexts(replayed.earlier); !equalStrings(got,
		[]string{"the very first question", "the very first answer", "the newest question"}) {
		t.Fatalf("the region above the marker = %v", got)
	}
	// The live transcript is exactly what it always was — the region is beside
	// it, never instead of it.
	if got := messageTexts(replayed.messages); !equalStrings(got, []string{
		"the very first question", "[folded]", "the newest question", "the answer after",
	}) {
		t.Fatalf("the live transcript changed: %v", got)
	}
	if replayed.overlap != 3 {
		t.Fatalf("overlap = %d, want the 3 messages the pass wrote back", replayed.overlap)
	}
	// And the splice tells the conversation ONCE.
	told := append(messageTexts(replayed.earlier), messageTexts(replayed.messages[replayed.overlap:])...)
	if !equalStrings(told, []string{
		"the very first question", "the very first answer", "the newest question", "the answer after",
	}) {
		t.Fatalf("the spliced conversation = %v", told)
	}
}

// A MARKER THAT DOES NOT SAY HOW MUCH IT WROTE BACK IS OFFERED NO REGION. Every
// file written before the length existed is in this shape, and so is every
// legacy marker. The transcript below it is the whole of what can honestly be
// drawn — showing the region as well would draw the conversation twice.
func TestAMarkerWithNoWindowLengthOffersNoRegion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path,
		`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the very first question","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"the very first answer","timestamp":"t"}`,
		`{"type":"compaction","stubbed":3,"tokensBefore":84000,"timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the very first question","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"[output stubbed]","timestamp":"t"}`,
	)

	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(replayed.earlier) != 0 || replayed.overlap != 0 {
		t.Fatalf("a marker with no window length offered %d messages at overlap %d",
			len(replayed.earlier), replayed.overlap)
	}
	if got := messageTexts(replayed.messages); !equalStrings(got,
		[]string{"the very first question", "[output stubbed]"}) {
		t.Fatalf("the transcript = %v", got)
	}
}

// A JOURNAL THAT WAS NEVER COMPACTED HAS NO REGION AT ALL — the case nearly
// every session is in, and the one that must behave exactly as it did before
// this existed.
func TestReplayOfAnUncompactedJournalHasNoEarlierRegion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path,
		`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"hello","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"hi","timestamp":"t"}`,
	)

	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(replayed.earlier) != 0 || replayed.overlap != 0 {
		t.Fatalf("an uncompacted journal answered a region of %d messages at overlap %d",
			len(replayed.earlier), replayed.overlap)
	}
	if got := messageTexts(replayed.messages); !equalStrings(got, []string{"hello", "hi"}) {
		t.Fatalf("the transcript = %v", got)
	}
}

// A REWOUND TURN DOES NOT COME BACK FROM THE DEAD. The lines a rewind took back
// are still in the file — the journal is append-only and records the cut as a
// count — so a reader that walked the raw lines above the marker would put a
// turn the person deliberately unsaid back on their screen. The region is taken
// with the reducer's own state, after the cut has been applied.
func TestTheEarlierRegionRespectsARewindTakenBeforeTheCompaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path,
		`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the question that stands","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"the answer that stands","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the question taken back","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"the answer taken back","timestamp":"t"}`,
		`{"type":"rewind","dropped":2,"timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the question said better","timestamp":"t"}`,
		`{"type":"compaction","stubbed":1,"window":3,"timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the question that stands","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"[folded]","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the question said better","timestamp":"t"}`,
	)

	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	got := messageTexts(replayed.earlier)
	for _, gone := range []string{"the question taken back", "the answer taken back"} {
		if holdsString(got, gone) {
			t.Fatalf("a rewound turn came back in the region above the marker: %v", got)
		}
	}
	if !equalStrings(got, []string{
		"the question that stands", "the answer that stands", "the question said better",
	}) {
		t.Fatalf("the region above the marker = %v", got)
	}
}

// A REWIND AFTER THE MARKER THAT CUTS INTO THE REWRITTEN WINDOW pulls the floor
// down with it. The overlap names messages the transcript still holds, and one
// that ran off the end of a shortened transcript would hide rows that are on
// nobody's screen.
func TestTheFloorIsClampedByARewindTakenAfterTheCompaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path,
		`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"one","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"two","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"three","timestamp":"t"}`,
		`{"type":"compaction","stubbed":1,"window":3,"timestamp":"t"}`,
		`{"type":"message","role":"user","content":"one","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"[folded]","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"three","timestamp":"t"}`,
		`{"type":"rewind","dropped":2,"timestamp":"t"}`,
	)

	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replayed.overlap > len(replayed.messages) {
		t.Fatalf("overlap %d runs past the %d messages left after the cut",
			replayed.overlap, len(replayed.messages))
	}
}

// TWO PASSES, ONE REGION, AND IT STILL OPENS ON THE FIRST THING ANYBODY SAID.
// The region is everything before the LATEST marker with every older marker
// applied, which is the reading [replayedSession.earlier] defends: the second
// pass's window was re-journaled behind the first marker, so walking through
// that marker would draw the whole conversation twice. Nothing is lost by
// applying it — a pass never folds a person's own words.
func TestTheEarlierRegionOfATwiceCompactedJournalStillReachesTheFirstWords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path,
		`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the first thing anybody said","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"a long answer, in full","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the second thing","timestamp":"t"}`,
		`{"type":"compaction","stubbed":1,"window":3,"timestamp":"t"}`,
		// The first pass re-journals its whole rebuilt window: the person's words
		// verbatim, its own work shortened.
		`{"type":"message","role":"user","content":"the first thing anybody said","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"[folded: a long answer]","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the second thing","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the third thing","timestamp":"t"}`,
		`{"type":"compaction","stubbed":2,"window":4,"timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the first thing anybody said","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"[folded: a long answer]","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the second thing","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the third thing","timestamp":"t"}`,
	)

	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	got := messageTexts(replayed.earlier)
	if len(got) == 0 || got[0] != "the first thing anybody said" {
		t.Fatalf("the region does not open on the conversation's first words: %v", got)
	}
	// And it holds each of them ONCE. Walking through the older marker would put
	// the opening exchange in twice, once raw and once as the pass rewrote it.
	if n := countString(got, "the first thing anybody said"); n != 1 {
		t.Fatalf("the conversation's first message is drawn %d times: %v", n, got)
	}
	if n := countString(got, "the second thing"); n != 1 {
		t.Fatalf("a message from before the older marker is drawn %d times: %v", n, got)
	}
	if !holdsString(got, "the third thing") {
		t.Fatalf("the newest message above the latest marker is missing: %v", got)
	}
	// The whole transcript is inside the floor: the latest pass wrote its window
	// back and nothing has happened since.
	if replayed.overlap != len(replayed.messages) {
		t.Fatalf("overlap = %d of %d live messages", replayed.overlap, len(replayed.messages))
	}
}

// ── the agent's own answer ──────────────────────────────────────────────────

// A SESSION THAT WAS NEVER COMPACTED HAS NOTHING ABOVE IT, and says so.
func TestEarlierHistoryIsEmptyWithoutACompaction(t *testing.T) {
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, nil)
	if got := agent.EarlierHistory(); len(got.Entries) != 0 || got.Floor != 0 {
		t.Fatalf("a session that never compacted answered %d entries at floor %d",
			len(got.Entries), got.Floor)
	}
}

// A PASS HANDS ITS HISTORY OVER AS IT RUNS. What [Agent.Transcript] said one
// instant before the pass is exactly what the region says after it, and the
// floor is the WHOLE transcript — because at that instant every row of the
// transcript is the pass's rewritten copy of a row in the region.
func TestAPassMovesTheConversationItEditedIntoTheEarlierHistory(t *testing.T) {
	heavy := strings.Repeat("package main // the whole of it, again and again.\n", 60)
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		config.ContextWindow = 2_000_000
	})

	agent.mu.Lock()
	// Only the OLDEST result is heavy, so it is the one the pass stubs: the last
	// four turns are the work in hand and are never touched.
	agent.messages = append(agent.messages, exchanges(6, map[int]string{1: heavy})...)
	agent.mu.Unlock()

	before := agent.Transcript()
	if len(before) == 0 {
		t.Fatal("the transcript is empty before the pass")
	}
	if got := agent.EarlierHistory(); len(got.Entries) != 0 {
		t.Fatalf("there was history above the conversation before any pass ran: %d entries",
			len(got.Entries))
	}

	if _, err := agent.compact(context.Background(), nil); err != nil {
		t.Fatalf("compact: %v", err)
	}

	history := agent.EarlierHistory()
	if len(history.Entries) != len(before) {
		t.Fatalf("the region holds %d entries, want the %d the transcript held",
			len(history.Entries), len(before))
	}
	for i := range history.Entries {
		if history.Entries[i].Text != before[i].Text || history.Entries[i].Role != before[i].Role {
			t.Fatalf("entry %d = %s/%q, want %s/%q", i,
				history.Entries[i].Role, history.Entries[i].Text, before[i].Role, before[i].Text)
		}
	}
	if history.Floor != len(agent.Transcript()) {
		t.Fatalf("floor = %d, want the whole %d-entry transcript the pass rewrote",
			history.Floor, len(agent.Transcript()))
	}
	// And the whole of the original result is in it, which is the point: the
	// live transcript is now holding a stub of that same result.
	if !containsWhole(history.Entries, heavy) {
		t.Fatal("the tool result the pass stubbed is not readable in the region above it")
	}
	if containsWhole(agent.Transcript(), heavy) {
		t.Fatal("the pass did not actually stub the result it kept for the scrollback")
	}
}

// A REFUSED PASS CHANGES NOTHING. [ErrNothingToCompact] means the transcript was
// not edited, so the history above it is whatever it already was — and a region
// replaced by a pass that did not happen would be the surface showing the same
// conversation twice, once above the seam and once below.
func TestARefusedPassLeavesTheEarlierHistoryAlone(t *testing.T) {
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		config.ContextWindow = 2_000_000
	})
	agent.mu.Lock()
	agent.messages = append(agent.messages, textMessage("user", "one short question"))
	agent.mu.Unlock()

	if _, err := agent.compact(context.Background(), nil); err != ErrNothingToCompact {
		t.Fatalf("compact = %v, want ErrNothingToCompact", err)
	}
	if got := agent.EarlierHistory(); len(got.Entries) != 0 || got.Floor != 0 {
		t.Fatalf("a refused pass wrote %d entries at floor %d above the conversation",
			len(got.Entries), got.Floor)
	}
}

// THE RESUMED HISTORY IS THE LIVE ONE. A pass runs, the session is put down and
// picked up again, and what a surface can scroll back into is what it could have
// scrolled back into a moment before the session closed — which is the whole
// round trip: the region, the floor, and the window length that places it.
func TestAResumedSessionRecoversTheHistoryTheLiveOneHad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	long := strings.Repeat("thinking about the parser. ", 40)
	writer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(long), nil
		},
	}}
	live, workspace := newTestAgent(t, writer, func(config *Config) {
		config.ContextWindow = 200
		config.CompactEnabled = true
		config.SessionFile = path
	})
	collect(t, mustSubmit(t, live, "go"))

	want := live.EarlierHistory()
	if len(want.Entries) == 0 {
		t.Fatal("the turn did not compact, so there is no history to recover")
	}
	liveTold := len(want.Entries) + len(live.Transcript()) - want.Floor
	if err := live.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	resumed, err := newAgent(Config{
		Workspace: workspace, Model: "test/model", System: "SYSTEM", SessionFile: path,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = resumed.Close() })

	got := resumed.EarlierHistory()
	if len(got.Entries) != len(want.Entries) || got.Floor != want.Floor {
		t.Fatalf("resumed history = %d entries at floor %d, want %d at %d",
			len(got.Entries), got.Floor, len(want.Entries), want.Floor)
	}
	for i := range got.Entries {
		if got.Entries[i].Text != want.Entries[i].Text {
			t.Fatalf("entry %d = %q, want %q", i, got.Entries[i].Text, want.Entries[i].Text)
		}
	}
	// AND THE SPLICE TELLS THE CONVERSATION ONCE, at the same length it did live.
	if told := len(got.Entries) + len(resumed.Transcript()) - got.Floor; told != liveTold {
		t.Fatalf("the resumed conversation is %d entries long, want %d", told, liveTold)
	}
}

// ── the floor is counted, and the count is the shaping's own ────────────────

// THE FLOOR IS A COUNT OF ENTRIES, and a pass takes it with [countEntries]
// rather than by shaping the rewritten transcript and measuring the list. That
// is only allowed while the two answer identically, so this asks them both about
// every shape a transcript is made of: the system message the shaping drops, a
// person's words, an assistant message carrying no calls, one carrying several,
// the results that answer them, and a fold marker.
func TestTheEntryCountAgreesWithTheShaping(t *testing.T) {
	call := func(ids ...string) ai.Message {
		message := ai.Message{
			Role:    "assistant",
			Content: []ai.ContentPart{{Type: "text", Text: "reading"}},
		}
		for _, id := range ids {
			message.ToolCalls = append(message.ToolCalls, ai.ToolCall{
				ID: id, Type: "function",
				Function: ai.ToolCallFunction{
					Name: "read",
					// Long enough to go through [capArgsValues], which is the
					// expensive half of the shaping this count exists to skip.
					Arguments: `{"path":"` + strings.Repeat("a", argsLimit) + `.go"}`,
				},
			})
		}
		return message
	}
	result := func(id string) ai.Message {
		return ai.Message{Role: "tool", ToolCallID: id,
			Content: []ai.ContentPart{{Type: "text", Text: strings.Repeat("output. ", 400)}}}
	}

	for name, messages := range map[string][]ai.Message{
		"nothing at all":   nil,
		"the system alone": {textMessage("system", "you are aforge")},
		"one exchange": {
			textMessage("system", "you are aforge"),
			textMessage("user", "a question"),
			textMessage("assistant", "an answer"),
		},
		"a batch of three calls": {
			textMessage("system", "you are aforge"),
			textMessage("user", "a question"),
			call("c1", "c2", "c3"),
			result("c1"), result("c2"), result("c3"),
			textMessage("assistant", "an answer"),
		},
		"a folded transcript": {
			textMessage("system", "you are aforge"),
			textMessage("user", "the first question"),
			textMessage("user", foldMarker(9, "", 0, 0, false)),
			call("c1"), result("c1"),
			textMessage("user", "the newest question"),
		},
		"six exchanges": append(
			[]ai.Message{textMessage("system", "you are aforge")},
			exchanges(6, nil)...),
	} {
		if got, want := countEntries(messages), len(shapeEntries(messages, nil)); got != want {
			t.Fatalf("%s: countEntries = %d, the shaping made %d rows", name, got, want)
		}
	}
}

// AND A PASS THAT FOLDS PUTS THE FLOOR AT THE WHOLE REWRITTEN TRANSCRIPT, which
// is the case the counted floor could get wrong that the stubbed one cannot: a
// fold takes messages OUT and puts a marker in, so the number the pass records
// is not the number it shaped a moment earlier.
func TestAFoldingPassFloorsTheWholeRewrittenTranscript(t *testing.T) {
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		// Threshold 1000 tokens (4000 bytes), verbatim tail 500 — the fold
		// fixture next door in compaction_test.go.
		config.ContextWindow = 2000
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
	long := strings.Repeat("thinking about the parser. ", 100)

	var fixture []ai.Message
	for round := 1; round <= 6; round++ {
		fixture = append(fixture,
			textMessage("user", fmt.Sprintf("question %d", round)),
			textMessage("assistant", long))
	}
	fixture = append(fixture, textMessage("assistant", "the short last word"))

	agent.mu.Lock()
	for _, message := range fixture {
		// The journal is the floor a no-store marker points at, so the fixture
		// writes the way the turn does.
		agent.messages = append(agent.messages, message)
		if agent.file != nil {
			agent.file.append(message, false, nil)
		}
	}
	agent.mu.Unlock()

	before := agent.Transcript()
	if _, err := agent.compact(context.Background(), nil); err != nil {
		t.Fatalf("compact: %v", err)
	}
	after := agent.Transcript()
	if len(after) >= len(before) {
		t.Fatalf("nothing was folded — %d entries before, %d after", len(before), len(after))
	}
	folded := false
	for _, entry := range after {
		if strings.HasPrefix(entry.Text, foldMarkerPrefix) {
			folded = true
		}
	}
	if !folded {
		t.Fatal("no fold marker in the rewritten transcript, so this proves nothing about a fold")
	}
	history := agent.EarlierHistory()
	if history.Floor != len(after) {
		t.Fatalf("floor = %d, want the whole %d-entry transcript the pass rewrote",
			history.Floor, len(after))
	}
	if len(history.Entries) != len(before) {
		t.Fatalf("the region holds %d entries, want the %d the transcript held",
			len(history.Entries), len(before))
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func messageTexts(messages []ai.Message) []string {
	out := make([]string, 0, len(messages))
	for _, message := range messages {
		out = append(out, messageText(message))
	}
	return out
}

func holdsString(all []string, want string) bool { return countString(all, want) > 0 }

func countString(all []string, want string) int {
	n := 0
	for _, got := range all {
		if got == want {
			n++
		}
	}
	return n
}

// containsWhole asks whether the text is readable in full anywhere in the run —
// the question a stub makes interesting, since a stub carries the opening words
// of what it replaced.
func containsWhole(entries []DisplayEntry, want string) bool {
	for _, e := range entries {
		if strings.Contains(e.Text, want) || strings.Contains(e.Output, want) {
			return true
		}
	}
	return false
}
