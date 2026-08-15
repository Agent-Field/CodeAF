package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// (f) a session written to disk reopens as the same transcript.
func TestSessionFileRoundTrip(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "nested", "session.jsonl")

	writer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "ls", `{"path":"."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("nothing in there yet"), nil
		},
	}}
	first, _ := newTestAgent(t, writer, func(config *Config) { config.SessionFile = path })
	collect(t, mustSubmit(t, first, "what is in the workspace?"))
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	first.mu.Lock()
	want := append([]ai.Message(nil), first.messages...)
	first.mu.Unlock()

	second, err := newAgent(Config{
		Workspace:   first.config.Workspace,
		Model:       "test/model",
		System:      "SYSTEM",
		SessionFile: path,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	second.mu.Lock()
	got := second.messages
	second.mu.Unlock()

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("restored transcript differs\n got: %#v\nwant: %#v", got, want)
	}

	// The header is line 1 and is written once, not once per open.
	lines := readLines(t, path)
	var header sessionHeader
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("header: %v", err)
	}
	if header.Type != "session" || header.Version != sessionFileVersion ||
		header.Cwd != first.config.Workspace || header.Model != "test/model" ||
		header.ID == "" || header.Timestamp == "" {
		t.Fatalf("header = %+v", header)
	}
	for _, line := range lines[1:] {
		if strings.Contains(line, `"type":"session"`) {
			t.Fatal("reopening the session rewrote the header")
		}
	}
	// The session named itself after its first completed turn, so one line is
	// the title (title.go) and is not part of the transcript. Counting it here
	// rather than filtering it out silently is the point: the journal holds
	// exactly the messages plus the facts about the session itself.
	titles := 0
	for _, line := range lines {
		if strings.Contains(line, `"type":"title"`) {
			titles++
		}
	}
	if titles != 1 {
		t.Fatalf("journal holds %d title lines, want exactly 1", titles)
	}
	if second.Title() == "" {
		t.Fatal("the resumed session came back without the name in its file")
	}
	// system is never journaled: it is rendered fresh on every open.
	if got, want := len(lines)-titles, 1+len(want)-1; got != want {
		t.Fatalf("journal has %d message lines, want %d (header + every message but system)", got, want)
	}
}

// A resumed session starts after the latest compaction marker, with its
// summary as the context prefix and the tail the pass kept verbatim — which
// the marker re-journals behind itself, so replay reads it on the near side.
func TestSessionFileResumesAfterCompaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path,
		`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the old question","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"the old answer","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"the kept question","timestamp":"t"}`,
		`{"type":"compaction","summary":"## Goal\nship the parser","tokensBefore":84000,"timestamp":"t"}`,
		// The kept tail, re-journaled by the pass on the far side of its marker.
		`{"type":"message","role":"user","content":"the kept question","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"and now?","timestamp":"t"}`,
		// A half-written tail line: the replay skips it rather than refusing
		// to open the session.
		`{"type":"message","role":"assist`,
	)

	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !replayed.existed {
		t.Fatal("existing file reported as new")
	}
	messages := replayed.messages
	if got := rolesOf(messages); !equalStrings(got, []string{"user", "user", "user"}) {
		t.Fatalf("restored roles = %v, want the summary note + the kept tail", got)
	}
	if !strings.Contains(messageText(messages[0]), "ship the parser") {
		t.Fatalf("summary note = %q", messageText(messages[0]))
	}
	if strings.Contains(messageText(messages[0]), "the old question") {
		t.Fatal("messages from before the compaction survived")
	}
	if messageText(messages[1]) != "the kept question" || messageText(messages[2]) != "and now?" {
		t.Fatalf("tail = %q / %q, want the kept tail verbatim",
			messageText(messages[1]), messageText(messages[2]))
	}
}

// The resumed transcript must EQUAL the live one after a compaction pass. The
// marker means "discard everything above me", so a kept tail journaled only
// above it is a tail the resume throws away.
func TestSessionFileResumeEqualsLiveAfterCompaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	long := strings.Repeat("context that will not fit. ", 40)
	writer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("short reply"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("## Goal\nfit the window"), nil
		},
	}}
	live, workspace := newTestAgent(t, writer, func(config *Config) {
		config.ContextWindow = 200
		config.CompactEnabled = true
		config.SessionFile = path
	})
	collect(t, mustSubmit(t, live, long))

	live.mu.Lock()
	want := append([]ai.Message(nil), live.messages...)
	live.mu.Unlock()
	if len(want) != 3 || !strings.Contains(messageText(want[1]), "[context compacted]") {
		t.Fatalf("the live transcript did not compact: %v", rolesOf(want))
	}
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

	resumed.mu.Lock()
	got := resumed.messages
	resumed.mu.Unlock()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resumed transcript differs from the live one\n got: %#v\nwant: %#v", got, want)
	}
}

// A session killed mid-batch journals the assistant's tool_calls and none of
// their results (the loop records results only after the whole batch). Replayed
// as-is that transcript is rejected with a 400 by every provider, on this
// request and every request after it — the session could never be resumed.
func TestSessionFileRepairsAnInterruptedToolBatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path,
		`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"read the file","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"on it","toolCalls":[{"id":"c1","type":"function","function":{"name":"read","arguments":"{\"path\":\"a.go\"}"}},{"id":"c2","type":"function","function":{"name":"read","arguments":"{\"path\":\"b.go\"}"}}],"timestamp":"t"}`,
		// The kill landed here: c1 answered, c2 never was.
		`{"type":"message","role":"tool","toolCallId":"c1","content":"package a","timestamp":"t"}`,
	)

	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("resumed cleanly"), nil
		},
	}}
	agent, err := newAgent(Config{
		Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM", SessionFile: path,
	}, completer)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })

	if got, want := transcriptRoles(agent), []string{"system", "user"}; !equalStrings(got, want) {
		t.Fatalf("resumed roles = %v, want %v — the unanswered batch must be trimmed", got, want)
	}

	collect(t, mustSubmit(t, agent, "carry on"))
	assertWellFormed(t, completer.request(0))
}

// The mirror shape: a tool result whose call is not in the transcript.
func TestSessionFileDropsOrphanedToolResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path,
		`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
		// A compaction summarized the call away; the result was journaled after.
		`{"type":"compaction","summary":"## Goal\nship it","tokensBefore":84000,"timestamp":"t"}`,
		`{"type":"message","role":"tool","toolCallId":"c1","content":"package a","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"and now?","timestamp":"t"}`,
	)

	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	messages := replayed.messages
	if got, want := rolesOf(messages), []string{"user", "user"}; !equalStrings(got, want) {
		t.Fatalf("restored roles = %v, want %v — the orphaned result must be dropped", got, want)
	}
	assertWellFormed(t, append([]ai.Message{textMessage("system", "SYSTEM")}, messages...))
}

// A file from a newer aforge can hold entry types this build drops in silence.
// A resume that looks complete and is not is worse than a refusal to open.
func TestSessionFileRejectsANewerFormatVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path,
		`{"type":"session","version":99,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"hello","timestamp":"t"}`,
	)

	if _, err := replaySessionFile(path); err == nil {
		t.Fatal("a version-99 session file was replayed as if this build understood it")
	} else if !strings.Contains(err.Error(), "newer aforge") {
		t.Fatalf("error = %v, want it to name the version mismatch", err)
	}
	if _, err := newAgent(Config{
		Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM", SessionFile: path,
	}, &scriptedCompleter{}); err == nil {
		t.Fatal("New opened a session file it cannot read")
	}
}

// Close cancels the in-flight turn and waits for it: the messages a cancelled
// turn still owes the journal — the partial reply it keeps — must be on disk
// before the file closes.
func TestCloseWaitsForTheInFlightTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	streaming := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "half an answer")
			close(streaming)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = path })

	events := mustSubmit(t, agent, "explain it")
	select {
	case <-streaming:
	case <-time.After(10 * time.Second):
		t.Fatal("the step never streamed")
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	collect(t, events)

	joined := strings.Join(readLines(t, path), "\n")
	if !strings.Contains(joined, "half an answer") {
		t.Fatalf("the cancelled turn's partial never reached the journal:\n%s", joined)
	}
}

// assertWellFormed fails if a request carries a shape a provider rejects with a
// 400: an assistant tool_calls nobody answered, or a result with no call.
func assertWellFormed(t *testing.T, messages []ai.Message) {
	t.Helper()
	if len(messages) == 0 {
		t.Fatal("the request carries no messages")
	}
	open := map[string]bool{}
	for index, message := range messages {
		if message.Role == "tool" {
			if !open[message.ToolCallID] {
				t.Fatalf("message %d is a tool result for %q with no call above it: %v",
					index, message.ToolCallID, rolesOf(messages))
			}
			delete(open, message.ToolCallID)
			continue
		}
		for _, call := range message.ToolCalls {
			open[call.ID] = true
		}
	}
	if len(open) != 0 {
		t.Fatalf("request ends with %d unanswered tool call(s): %v", len(open), rolesOf(messages))
	}
}

func TestSessionFileJournalsToolCallsAndCompaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	journal, _, err := openSessionFile(path, "/w", "m")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	journal.appendMessage(ai.Message{
		Role:    "assistant",
		Content: []ai.ContentPart{{Type: "text", Text: "looking"}},
		ToolCalls: []ai.ToolCall{{
			ID: "c1", Type: "function",
			Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"a.go"}`},
		}},
	})
	journal.appendCompaction("## Goal\nsomething", 42000, []ai.Message{
		textMessage("user", "the kept tail"),
	})
	if err := journal.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	lines := readLines(t, path)
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want header + message + compaction + the re-journaled tail", len(lines))
	}
	var entry sessionEntry
	if err := json.Unmarshal([]byte(lines[1]), &entry); err != nil {
		t.Fatalf("message line: %v", err)
	}
	if entry.Type != "message" || entry.Role != "assistant" || entry.Content != "looking" ||
		len(entry.ToolCalls) != 1 || entry.ToolCalls[0].Function.Name != "read" {
		t.Fatalf("message entry = %+v", entry)
	}
	var marker sessionEntry
	if err := json.Unmarshal([]byte(lines[2]), &marker); err != nil {
		t.Fatalf("compaction line: %v", err)
	}
	if marker.Type != "compaction" || marker.TokensBefore != 42000 || !strings.Contains(marker.Summary, "something") {
		t.Fatalf("compaction entry = %+v", marker)
	}
	// The kept tail is re-journaled AFTER the marker: replay discards
	// everything above it, so a tail written only above it is a tail lost.
	var tail sessionEntry
	if err := json.Unmarshal([]byte(lines[3]), &tail); err != nil {
		t.Fatalf("tail line: %v", err)
	}
	if tail.Type != "message" || tail.Content != "the kept tail" {
		t.Fatalf("tail entry = %+v, want the kept message re-journaled", tail)
	}

	// Writing after Close is a no-op, not a panic on a closed descriptor.
	journal.appendMessage(textMessage("user", "too late"))
	if got := len(readLines(t, path)); got != 4 {
		t.Fatalf("lines after Close = %d, want 4", got)
	}
}

// Two aforge processes resuming the same file both replay it and both append,
// and the journal that comes out replays as neither conversation. A resume
// picks the newest transcript by mtime, so the second window lands on the live
// file by default — the second open has to be refused, by name, and it has to
// stop being refused the moment the first one closes.
func TestSessionFileIsLockedWhileOpen(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "session.jsonl")
	open := func() (*Agent, error) {
		return newAgent(Config{
			Workspace: directory, Model: "test/model", System: "SYSTEM", SessionFile: path,
		}, &scriptedCompleter{})
	}

	first, err := open()
	if err != nil {
		t.Fatalf("first open: %v", err)
	}

	second, err := open()
	if err == nil {
		_ = second.Close()
		t.Fatal("a second agent opened a session file the first one holds")
	}
	if !errors.Is(err, ErrSessionLocked) {
		t.Fatalf("second open = %v, want ErrSessionLocked", err)
	}
	// The surface offers "open it where it is / start a new one", so the error
	// has to say WHICH file — not just that some session is busy.
	var locked *SessionLockedError
	if !errors.As(err, &locked) {
		t.Fatalf("second open = %v, want a *SessionLockedError", err)
	}
	if locked.Path != path {
		t.Fatalf("locked path = %q, want %q", locked.Path, path)
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("error text = %q, want it to name %q", err.Error(), path)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// Close releases the claim, so the handover is immediate: the second window
	// opens as soon as the first quits, with no sweep and nothing to retry past.
	third, err := open()
	if err != nil {
		t.Fatalf("open after Close: %v, want the released file to open", err)
	}
	t.Cleanup(func() { _ = third.Close() })

	// The claim is the descriptor, not a file on the side: nothing may be left
	// in the directory for a crashed process to strand.
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "session.jsonl" {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("directory holds %v, want only the session file", names)
	}
}

// Close runs from the surface's quit and from a t.Cleanup behind it, so the
// second call must not double-unlock or close a closed descriptor.
func TestSessionFileCloseIsIdempotent(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "session.jsonl")
	agent, err := newAgent(Config{
		Workspace: directory, Model: "test/model", System: "SYSTEM", SessionFile: path,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		if err := agent.Close(); err != nil {
			t.Fatalf("Close #%d: %v", attempt, err)
		}
	}

	// And a repeated Close still leaves the file claimable, rather than having
	// released a lock the reopen below then takes from itself.
	reopened, err := newAgent(Config{
		Workspace: directory, Model: "test/model", System: "SYSTEM", SessionFile: path,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen after repeated Close: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("reopen Close: %v", err)
	}
}

// An in-memory session has no file to claim, so two of them coexist.
func TestInMemorySessionsAreNotLocked(t *testing.T) {
	workspace := t.TempDir()
	for index := 0; index < 2; index++ {
		agent, err := newAgent(Config{
			Workspace: workspace, Model: "test/model", System: "SYSTEM",
		}, &scriptedCompleter{})
		if err != nil {
			t.Fatalf("in-memory agent %d: %v", index, err)
		}
		t.Cleanup(func() { _ = agent.Close() })
	}
}

// A refused open must not leave the descriptor it opened holding the file:
// the error path runs before the journal exists, so the release is its own.
func TestSessionFileReleasesTheLockOnAFailedOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path,
		`{"type":"session","version":99,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
	)
	if _, _, err := openSessionFile(path, "/w", "m"); err == nil {
		t.Fatal("a newer-format file opened")
	}

	// Rewrite it as something this build reads; if the failed open had kept its
	// claim, this open would be refused by its own process.
	write(t, path,
		`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
	)
	journal, _, err := openSessionFile(path, "/w", "m")
	if err != nil {
		t.Fatalf("open after a failed open: %v", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func write(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}
