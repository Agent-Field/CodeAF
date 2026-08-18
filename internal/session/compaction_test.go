package session

// What a compaction pass costs, what it keeps, and where the rest of it went.
//
// The card's own tests are next door in card_test.go and the journal's in
// sessionfile_test.go. These are about the three claims this slice makes that
// nothing else can check: THE PASS CALLS NO MODEL, the text it takes out of the
// window is still readable somewhere, and the line it says about itself is true.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// refusingCompleter is a provider that must never be reached.
//
// It is the whole claim of this slice held as a type: compaction stopped being a
// model call, and the only way to test that a call did not happen is a client
// whose one behaviour is to fail the test when it does.
type refusingCompleter struct{ t *testing.T }

func (c *refusingCompleter) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	c.t.Errorf("the compaction pass called the provider")
	return textResponse("this should never have been asked for"), nil
}

// exchanges appends n user/assistant/tool rounds to a transcript, and the heavy
// set says which of them carry a result big enough to be worth stubbing.
//
// The shape is the one the stub pass counts in: a turn starts at a user message
// (stub.go's [stubCut]), so "six exchanges ago" and "two exchanges ago" are
// positions this builds rather than numbers a test asserts about.
func exchanges(n int, heavy map[int]string) []ai.Message {
	var messages []ai.Message
	for exchange := 1; exchange <= n; exchange++ {
		id := fmt.Sprintf("c%d", exchange)
		result := "small enough to keep whole"
		if text, big := heavy[exchange]; big {
			result = text
		}
		messages = append(messages,
			textMessage("user", fmt.Sprintf("question %d", exchange)),
			ai.Message{
				Role:    "assistant",
				Content: []ai.ContentPart{{Type: "text", Text: fmt.Sprintf("reading for %d", exchange)}},
				ToolCalls: []ai.ToolCall{{
					ID: id, Type: "function",
					Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"a.go"}`},
				}},
			},
			ai.Message{Role: "tool", ToolCallID: id, Content: []ai.ContentPart{{Type: "text", Text: result}}})
	}
	return messages
}

// ── the stub pass ───────────────────────────────────────────────────────────

// THE FIRST PASS IS MECHANICAL AND FREE. A tool result the model has already
// used becomes one line naming the tool, what it said first, and where the whole
// of it can be read back — and the results of the last four turns, which are the
// work in hand, are not touched at all.
func TestCompactionStubsTheOldToolResultsAndCallsNoModel(t *testing.T) {
	heavy := strings.Repeat("package main // the whole of it, again and again.\n", 60)
	agent, workspace := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		// A window nothing in this transcript can exceed, so the pass is the
		// stub pass alone and a fold on top of it is not one more thing to
		// explain.
		config.ContextWindow = 2_000_000
	})

	agent.mu.Lock()
	agent.messages = append(agent.messages, exchanges(6, map[int]string{1: heavy, 5: heavy})...)
	agent.mu.Unlock()

	changed, err := agent.compact(context.Background(), nil)
	if !changed || err != nil {
		t.Fatalf("compact = %v, %v; want a pass that stubbed", changed, err)
	}

	agent.mu.Lock()
	messages := append([]ai.Message(nil), agent.messages...)
	agent.mu.Unlock()

	// Six exchanges back: a stub, and it names all three mechanical facts.
	stub := messageText(messages[3])
	if !strings.HasPrefix(stub, "[tool: read · ") {
		t.Fatalf("the old result was not stubbed: %q", stub)
	}
	if !strings.Contains(stub, "package main") || !strings.Contains(stub, fmt.Sprintf("%d bytes", len(heavy))) {
		t.Fatalf("the stub says nothing about the result it replaced: %q", stub)
	}

	// Two exchanges back: VERBATIM. It is what the model is working on.
	if got := messageText(messages[15]); got != heavy {
		t.Fatalf("a recent result was stubbed:\n%q", shortLine(got))
	}

	// And the person's own words, everywhere, exactly as they were typed.
	for exchange := 1; exchange <= 6; exchange++ {
		want := fmt.Sprintf("question %d", exchange)
		if got := messageText(messages[1+3*(exchange-1)]); got != want {
			t.Fatalf("a user message was rewritten: %q, want %q", got, want)
		}
	}

	// THE POINTER POINTS AT SOMETHING. A stub naming a path with nothing at it
	// is the one failure this pass may not have.
	path := stubPathIn(t, stub)
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	kept, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the stub names bytes that are not there: %v", err)
	}
	if string(kept) != heavy {
		t.Fatalf("the spilled result is %d bytes, want the original %d", len(kept), len(heavy))
	}
}

// A pass with nothing old enough to stub and nothing over threshold to fold did
// nothing, and says so — the error [ErrNothingToCompact] has always meant.
func TestCompactionWithNothingToDoSaysSo(t *testing.T) {
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		config.ContextWindow = 2_000_000
	})
	agent.mu.Lock()
	agent.messages = append(agent.messages, exchanges(2, nil)...)
	agent.mu.Unlock()

	if changed, err := agent.compact(context.Background(), nil); changed || err != ErrNothingToCompact {
		t.Fatalf("compact = %v, %v; want ErrNothingToCompact", changed, err)
	}
}

// ── the fold ────────────────────────────────────────────────────────────────

// Still over threshold after stubbing: the OLDEST ASSISTANT WORK goes into one
// marker line, the person's words stay, the recent tail stays, and the marker
// says how much went and where it can be read.
func TestCompactionFoldsTheOldestAssistantWorkAndKeepsTheWords(t *testing.T) {
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		// Threshold 1000 tokens (4000 bytes), verbatim tail 500.
		config.ContextWindow = 2000
		// A fold marker with no store behind it points at journal lines, so
		// this session needs the journal it would have in life.
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
	long := strings.Repeat("thinking about the parser. ", 100)

	agent.mu.Lock()
	for _, message := range []ai.Message{
		textMessage("user", "the first question"),
		textMessage("assistant", long),
		textMessage("user", "the second question"),
		textMessage("assistant", long),
		textMessage("user", "the third question"),
		textMessage("assistant", "the short last word"),
	} {
		// The journal is the floor a no-store marker points at, so the fixture
		// must write the way the turn does: a message that never reached the
		// file is one the marker can only point at vaguely.
		agent.messages = append(agent.messages, message)
		if agent.file != nil {
			agent.file.append(message, false, nil)
		}
	}
	agent.mu.Unlock()

	changed, err := agent.compact(context.Background(), nil)
	if !changed || err != nil {
		t.Fatalf("compact = %v, %v; want a pass that folded", changed, err)
	}

	agent.mu.Lock()
	messages := append([]ai.Message(nil), agent.messages...)
	agent.mu.Unlock()

	var marker string
	longMessages := 0
	for _, message := range messages {
		text := messageText(message)
		if strings.HasPrefix(text, foldMarkerPrefix) {
			marker = text
		}
		if strings.Contains(text, "thinking about the parser") {
			longMessages++
		}
	}
	if longMessages >= 2 {
		t.Fatalf("the oldest assistant work was not folded: %v", textsOf(messages))
	}
	if marker == "" {
		t.Fatalf("no fold marker in %v", rolesOf(messages))
	}
	if !strings.Contains(marker, "message") || !strings.HasSuffix(marker, "]") {
		t.Fatalf("fold marker = %q", marker)
	}
	// With no store behind this session the marker points at exact lines in the
	// floor it actually has, which is the journal.
	if !strings.Contains(marker, "journal:line-") {
		t.Fatalf("the marker points nowhere: %q", marker)
	}

	// EVERY QUESTION IS STILL THERE. Nothing else in a transcript can be
	// reconstructed from anywhere, so nothing else is protected this way.
	for _, want := range []string{"the first question", "the second question", "the third question"} {
		if !holdsText(messages, want) {
			t.Fatalf("%q was folded away: %v", want, rolesOf(messages))
		}
	}
	// And the tail is verbatim.
	if !holdsText(messages, "the short last word") {
		t.Fatalf("the recent tail went into the fold: %v", rolesOf(messages))
	}
}

// ── the announcement ────────────────────────────────────────────────────────

// The line the turn after a pass shows, and EVERY CLAUSE IN IT IS A REAL COUNT.
func TestTheCompactionLineSaysWhatThePassActuallyDid(t *testing.T) {
	got := compactionHint(compactionPass{stubbed: 14, folded: 31, stored: true}, 0, 0)
	want := "compacted · stubbed 14 tool results · folded 31 messages · nothing lost — full record in the store"
	if got != want {
		t.Fatalf("hint =\n%q\nwant\n%q", got, want)
	}

	// A count of zero is not a clause. The emptiness law: "folded 0 messages"
	// is a sentence about nothing.
	single := compactionHint(compactionPass{stubbed: 1, stored: true}, 9000, 4000)
	if strings.Contains(single, "folded") {
		t.Fatalf("a fold that did not happen was announced: %q", single)
	}
	if !strings.Contains(single, "stubbed 1 tool result ") || strings.Contains(single, "1 tool results") {
		t.Fatalf("the singular is wrong: %q", single)
	}
	if !strings.Contains(single, "~9k → ~4k tokens") {
		t.Fatalf("the pass did not say what it saved: %q", single)
	}
}

// WITHOUT A STORE THE LINE MAKES THE SMALLER PROMISE. "Nothing lost" is a claim
// about a record that outlives this session file, and a session with memory off
// does not have one — it has the journal, which keeps every original line above
// the marker, and that is what it says.
func TestTheCompactionLineIsHonestWithoutAStore(t *testing.T) {
	got := compactionHint(compactionPass{stubbed: 2, folded: 3, stored: false}, 0, 0)
	if strings.Contains(got, "nothing lost") || strings.Contains(got, "in the store") {
		t.Fatalf("a session with no store claimed one: %q", got)
	}
	if !strings.HasSuffix(got, "full record in the session journal") {
		t.Fatalf("hint = %q", got)
	}
}

// ── the lossless floor ──────────────────────────────────────────────────────

// EVERY EVICTED MESSAGE IS STILL READABLE, and this is the test that opens a
// real store to prove it rather than a fake that would agree with whatever the
// code did.
//
// The >16KiB result is the interesting one: it cannot be posted whole
// ([store.MaxMessageBytes]), so the post names a spill file and the stub names
// the post. Following the pointer has to reach the original bytes, or the floor
// is a story about a floor.
func TestCompactionLeavesEveryEvictedMessageReadableInTheStore(t *testing.T) {
	huge := strings.Repeat("the build output, line after line after line.\n", 800)
	if len(huge) <= store.MaxMessageBytes {
		t.Fatalf("the oversized result is only %d bytes", len(huge))
	}
	agent, brain := brainAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		config.ContextWindow = 2000
	})
	workspace := agent.config.Workspace
	if agent.chatlog == nil {
		t.Fatal("a session with a store opened no chat log")
	}

	long := strings.Repeat("working through the parser. ", 60)
	recorded := []ai.Message{
		textMessage("user", "why is the build red"),
		{
			Role:    "assistant",
			Content: []ai.ContentPart{{Type: "text", Text: long}},
			ToolCalls: []ai.ToolCall{{
				ID: "c1", Type: "function",
				Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"go build ./..."}`},
			}},
		},
		{Role: "tool", ToolCallID: "c1", Content: []ai.ContentPart{{Type: "text", Text: huge}}},
		textMessage("user", "and after that"),
		textMessage("assistant", long),
		textMessage("user", "and then"),
		textMessage("assistant", long),
		textMessage("user", "so where are we"),
		textMessage("assistant", "here"),
		textMessage("user", "one more check"),
		textMessage("assistant", long),
		textMessage("user", "last check"),
		textMessage("assistant", "done"),
	}
	for _, message := range recorded {
		agent.record(message)
	}
	// The queue is drained before the pass, because a stub can only name a
	// pointer that has already landed (chatlog.go).
	agent.chatlog.settle()

	changed, err := agent.compact(context.Background(), nil)
	if !changed || err != nil {
		t.Fatalf("compact = %v, %v; want a pass that ran", changed, err)
	}
	agent.chatlog.settle()

	agent.mu.Lock()
	messages := append([]ai.Message(nil), agent.messages...)
	agent.mu.Unlock()

	// The pass did both halves of its job: the heavy result is a stub, and the
	// oldest assistant work is a marker.
	stub := ""
	folded := false
	for _, message := range messages {
		text := messageText(message)
		if strings.HasPrefix(text, "[tool: bash · ") {
			stub = text
		}
		if strings.HasPrefix(text, foldMarkerPrefix) {
			folded = true
		}
	}
	if stub == "" {
		t.Fatalf("the heavy result was never stubbed: %v", rolesOf(messages))
	}
	if !folded {
		t.Fatalf("nothing was folded, so this proves nothing about eviction: %v", rolesOf(messages))
	}

	posted, err := brain.Messages(agent.id, 0, 500)
	if err != nil {
		t.Fatalf("read the thread back: %v", err)
	}
	if len(posted) == 0 {
		t.Fatalf("the session posted nothing under %q", agent.id)
	}

	// Every message this session ever held is findable in the thread — the
	// person's words, the assistant's work, and the tool result by its pointer.
	for _, message := range recorded {
		text := messageContentText(message)
		if text == huge || strings.TrimSpace(text) == "" {
			continue
		}
		if !postedHolds(posted, text) {
			t.Fatalf("an evicted message is in no store record: %q", shortLine(text))
		}
	}

	// THE POINTER IS THE STUB'S OWN. It names a store message; that message
	// names a spill file; the file is the original bytes.
	pointer := stubPathIn(t, stub)
	if !strings.HasPrefix(pointer, chatRefPrefix) {
		t.Fatalf("the stub of a stored result points at %q, want a store id", pointer)
	}
	var landed *store.Message
	for index := range posted {
		if fmt.Sprintf("%s%d", chatRefPrefix, posted[index].Seq) == pointer {
			landed = &posted[index]
		}
	}
	if landed == nil {
		t.Fatalf("the stub points at %q, which is in no thread: %q", pointer, stub)
	}
	if len(landed.Attachments) != 1 {
		t.Fatalf("an oversized post carries %d attachments, want the spill file", len(landed.Attachments))
	}
	spill := landed.Attachments[0]
	if !filepath.IsAbs(spill) {
		spill = filepath.Join(workspace, spill)
	}
	full, err := os.ReadFile(spill)
	if err != nil {
		t.Fatalf("the store names bytes that are not there: %v", err)
	}
	if string(full) != huge {
		t.Fatalf("the spilled result is %d bytes, want the original %d", len(full), len(huge))
	}
}

// ── replay ──────────────────────────────────────────────────────────────────

// A RESUME REBUILDS THE IDENTICAL WINDOW. The marker means "discard everything
// above me", and the pass now EDITS above it — a result becomes a stub, a run of
// work becomes one line — so the whole rebuilt window is re-journaled below it
// and comes back as ordinary message lines rather than from counts.
func TestResumeRebuildsTheStubbedAndFoldedWindowVerbatim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	workspace := t.TempDir()
	heavy := strings.Repeat("package main // the whole of it, again and again.\n", 60)

	live, err := newAgent(Config{
		Workspace: workspace, Model: "test/model", System: "SYSTEM",
		SessionFile: path, ContextWindow: 2000,
	}, &refusingCompleter{t: t})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	long := strings.Repeat("working through the parser. ", 100)
	prefix := []ai.Message{
		textMessage("user", "start with the architecture"),
		textMessage("assistant", long),
	}
	for _, message := range append(prefix, append(exchanges(6, map[int]string{1: heavy}),
		textMessage("user", "keep going"), textMessage("assistant", long))...) {
		live.record(message)
	}

	changed, err := live.compact(context.Background(), nil)
	if !changed || err != nil {
		t.Fatalf("compact = %v, %v; want a pass that ran", changed, err)
	}

	live.mu.Lock()
	want := append([]ai.Message(nil), live.messages...)
	live.mu.Unlock()
	if !holdsPrefix(want, "[tool: read · ") {
		t.Fatalf("nothing was stubbed, so replay proves nothing: %v", rolesOf(want))
	}
	if !holdsPrefix(want, foldMarkerPrefix) {
		t.Fatalf("nothing was folded, so replay proves nothing: %v", rolesOf(want))
	}
	if err := live.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	resumed, err := newAgent(Config{
		Workspace: workspace, Model: "test/model", System: "SYSTEM", SessionFile: path,
	}, &refusingCompleter{t: t})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = resumed.Close() })

	resumed.mu.Lock()
	got := append([]ai.Message(nil), resumed.messages...)
	resumed.mu.Unlock()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the resumed window differs from the live one\n got: %v\nwant: %v",
			textsOf(got), textsOf(want))
	}
}

// ── the machinery that is gone ──────────────────────────────────────────────

// The frames rung and the summarizer are DELETED rather than disabled, and the
// dropping directory they wrote into is gone with them. A capability that cannot
// work is absent, not broken — and a session folder that still made a frames/
// directory would be a session still promising one.
func TestTheFramesDroppingIsGone(t *testing.T) {
	place := newPlace(t, false)
	if _, err := os.Stat(filepath.Join(place.Logs(), "frames")); !os.IsNotExist(err) {
		t.Fatalf("a frames directory is still being made: %v", err)
	}
}

// ── small readers ───────────────────────────────────────────────────────────

func holdsText(messages []ai.Message, want string) bool {
	for _, message := range messages {
		if strings.Contains(messageText(message), want) {
			return true
		}
	}
	return false
}

func holdsPrefix(messages []ai.Message, prefix string) bool {
	for _, message := range messages {
		if strings.HasPrefix(messageText(message), prefix) {
			return true
		}
	}
	return false
}

func postedHolds(posted []store.Message, want string) bool {
	for _, message := range posted {
		if strings.Contains(message.Body, want) {
			return true
		}
	}
	return false
}

func textsOf(messages []ai.Message) []string {
	out := make([]string, 0, len(messages))
	for _, message := range messages {
		out = append(out, message.Role+": "+shortLine(messageText(message)))
	}
	return out
}

func shortLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		text = text[:index]
	}
	if len(text) > 80 {
		text = text[:80] + "…"
	}
	return text
}
