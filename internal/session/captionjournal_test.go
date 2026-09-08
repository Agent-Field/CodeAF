package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE STEP'S TITLE OUTLIVES THE WINDOW ────────────────────────────────────

// captionedTurn runs ONE turn whose model calls `bash` on a command slow enough
// for the narrator's dwell to fire, and answers the narrator with `line`. It
// returns the agent and the journal path.
//
// The sleep is the point rather than an inconvenience: the narrator exists for
// the batch that takes long enough to be worth naming (caption.go's
// [captionDwell]), so a fixture that finished instantly would be testing a road
// the caption never travels.
func captionedTurn(t *testing.T, line string) (*Agent, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-batch", "bash", `{"command":"sleep 1"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the suite is green"), nil
		},
	}}
	answerTheNarrator(completer, line)
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = path })
	collect(t, mustSubmit(t, agent, "run the loader suite"))
	return agent, path
}

// reopen is the conversation read back off disk, exactly as a second window or
// tomorrow morning reads it.
func reopen(t *testing.T, path string) *Agent {
	t.Helper()
	resumed, err := newAgent(Config{
		Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM", SessionFile: path,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = resumed.Close() })
	return resumed
}

func toolEntryFor(t *testing.T, entries []DisplayEntry, callID string) DisplayEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.Role == "tool" && entry.CallID == callID {
			return entry
		}
	}
	t.Fatalf("no tool entry for %q in %#v", callID, entries)
	return DisplayEntry{}
}

// A NARRATED STEP READS THE SAME TOMORROW AS IT DID WHILE IT RAN — the sentence
// and the family both.
//
// This is the whole point of journaling a caption. The deterministic composite
// underneath is a real floor, so a conversation without it is not blank — it is
// WORSE in a way nobody can see: "running 1 command" replaces "running the
// loader suite", and a step the narrator called a `test` comes back a `run`,
// because `bash` is all the tool names can say. A record that changes what it
// says about the same work is a record that cannot be cited.
func TestANarratedStepKeepsItsSentenceAndFamilyAcrossAReopen(t *testing.T) {
	agent, path := captionedTurn(t, "test | running the loader suite")

	live := toolEntryFor(t, agent.Transcript(), "call-batch")
	if live.Caption != "running the loader suite" {
		t.Fatalf("the live entry carries %q, want the narrated sentence", live.Caption)
	}
	if live.CaptionCategory != ActionTest {
		t.Fatalf("the live entry carries the %q family, want %q", live.CaptionCategory, ActionTest)
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	back := toolEntryFor(t, reopen(t, path).Transcript(), "call-batch")
	if back.Caption != live.Caption {
		t.Fatalf("the reopened step says %q, want %q", back.Caption, live.Caption)
	}
	if back.CaptionCategory != live.CaptionCategory {
		t.Fatalf("the reopened step is a %q, want the %q the narrator named — "+
			"the tool name alone would have demoted it to %q",
			back.CaptionCategory, live.CaptionCategory, ActionCategoryForTool("bash"))
	}
	// AND IT IS THE REFINEMENT THAT SURVIVED, not a coincidence. `bash` derives
	// `run`; only the record can say this batch was a `test`.
	if back.CaptionCategory == ActionCategoryForTool("bash") {
		t.Fatal("the assertion above passes for the wrong reason: pick a family bash does not derive")
	}
}

// THE CAPTION IS ANCHORED TO THE BATCH AND TO NOTHING ELSE. It rides the call
// the batch opened with, so a surface keys the step exactly where the live
// stream keyed it — and no other call in the conversation claims a title it was
// never given.
func TestACaptionIsCarriedOnlyByItsOwnBatchAnchor(t *testing.T) {
	agent, path := captionedTurn(t, "test | running the loader suite")
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	entries := reopen(t, path).Transcript()

	carried := 0
	for _, entry := range entries {
		if entry.Caption == "" {
			continue
		}
		carried++
		if entry.Role != "tool" || entry.CallID != "call-batch" {
			t.Fatalf("a %q entry (call %q) carries the caption %q",
				entry.Role, entry.CallID, entry.Caption)
		}
	}
	if carried != 1 {
		t.Fatalf("%d entries carry the caption, want exactly the batch anchor", carried)
	}
}

// THE FILE SAYS IT PLAINLY. A `caption` line names its batch, its words and its
// family, and it is one line of its own rather than a field on a message —
// which is why it can be written at all, since the message it is about was
// journaled before the batch ran.
func TestTheJournalWritesOneCaptionLinePerNarration(t *testing.T) {
	agent, path := captionedTurn(t, "test | running the loader suite")
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	found := 0
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var entry sessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.Type != "caption" {
			continue
		}
		found++
		if entry.Caption == nil {
			t.Fatal("a caption line carries no caption")
		}
		if entry.Caption.CallID != "call-batch" {
			t.Fatalf("the caption line is anchored to %q, want the batch's first call",
				entry.Caption.CallID)
		}
		if entry.Caption.Text != "running the loader suite" {
			t.Fatalf("the caption line says %q", entry.Caption.Text)
		}
		if entry.Caption.Category != ActionTest {
			t.Fatalf("the caption line's family is %q, want %q", entry.Caption.Category, ActionTest)
		}
	}
	if found != 1 {
		t.Fatalf("%d caption lines in the journal, want 1", found)
	}
}

// A SECOND NARRATION OF ONE STEP IS THE ONE A PERSON WAS LEFT LOOKING AT. The
// lines accumulate and the replay takes the last, the way the title's do —
// nothing rewrites the file.
func TestTheLastCaptionForABatchIsTheOneThatReplays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"session","version":1,"id":"old","cwd":"/tmp","model":"test/model","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"run the suite","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"","toolCalls":[{"id":"call-batch","type":"function","function":{"name":"bash","arguments":"{\"command\":\"go test ./...\"}"}}],"timestamp":"t"}`,
		`{"type":"message","role":"tool","toolCallId":"call-batch","content":"ok","timestamp":"t"}`,
		`{"type":"caption","caption":{"callId":"call-batch","text":"starting the suite","category":"run"},"timestamp":"t"}`,
		`{"type":"caption","caption":{"callId":"call-batch","text":"running the loader suite","category":"test"},"timestamp":"t"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("writing the journal: %v", err)
	}
	entry := toolEntryFor(t, reopen(t, path).Transcript(), "call-batch")
	if entry.Caption != "running the loader suite" || entry.CaptionCategory != ActionTest {
		t.Fatalf("the replay took %q/%q, want the last line's", entry.Caption, entry.CaptionCategory)
	}
}

// A FILE WRITTEN BEFORE THE LINE EXISTED REPLAYS EXACTLY AS IT ALWAYS DID: no
// caption, no family, and a surface that recomposes both from the tool names —
// which is what every conversation on disk today will do.
func TestAJournalWithNoCaptionLinesReplaysWithNoTitle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"session","version":1,"id":"old","cwd":"/tmp","model":"test/model","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"run the suite","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"","toolCalls":[{"id":"call-batch","type":"function","function":{"name":"bash","arguments":"{\"command\":\"go test ./...\"}"}}],"timestamp":"t"}`,
		`{"type":"message","role":"tool","toolCallId":"call-batch","content":"ok","timestamp":"t"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("writing the older journal: %v", err)
	}
	entry := toolEntryFor(t, reopen(t, path).Transcript(), "call-batch")
	if entry.Caption != "" || entry.CaptionCategory != "" {
		t.Fatalf("an older journal grew a caption out of nothing: %q/%q",
			entry.Caption, entry.CaptionCategory)
	}
	if entry.Tool != "bash" {
		t.Fatalf("the old line stopped replaying as itself: %#v", entry)
	}
}

// A CAPTION LINE THAT NAMES NO STEP, OR SAYS NOTHING, IS NOT A CAPTION. Neither
// could ever be looked up, and a line nothing can find is a line that only grows
// the file. A malformed one must not take the good one's place either.
func TestAMalformedCaptionLineIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"session","version":1,"id":"old","cwd":"/tmp","model":"test/model","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"","toolCalls":[{"id":"call-batch","type":"function","function":{"name":"bash","arguments":"{}"}}],"timestamp":"t"}`,
		`{"type":"message","role":"tool","toolCallId":"call-batch","content":"ok","timestamp":"t"}`,
		`{"type":"caption","caption":{"callId":"call-batch","text":"running the loader suite","category":"test"},"timestamp":"t"}`,
		`{"type":"caption","timestamp":"t"}`,
		`{"type":"caption","caption":{"callId":"","text":"anchored to nothing"},"timestamp":"t"}`,
		`{"type":"caption","caption":{"callId":"call-batch","text":"   "},"timestamp":"t"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("writing the journal: %v", err)
	}
	entry := toolEntryFor(t, reopen(t, path).Transcript(), "call-batch")
	if entry.Caption != "running the loader suite" || entry.CaptionCategory != ActionTest {
		t.Fatalf("a malformed line displaced the good one: %q/%q",
			entry.Caption, entry.CaptionCategory)
	}
}

// A JOURNALED FAMILY THIS BUILD DOES NOT KNOW IS STILL JUST A STRING, and the
// surface's own table answers the bucket for it. It must not stop the SENTENCE
// from replaying: the words are what a person reads, and they are correct
// whatever the fourteenth family turns out to be called.
func TestAnUnknownJournaledFamilyStillReplaysItsSentence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"session","version":1,"id":"old","cwd":"/tmp","model":"test/model","timestamp":"t"}`,
		`{"type":"message","role":"assistant","content":"","toolCalls":[{"id":"call-batch","type":"function","function":{"name":"bash","arguments":"{}"}}],"timestamp":"t"}`,
		`{"type":"message","role":"tool","toolCallId":"call-batch","content":"ok","timestamp":"t"}`,
		`{"type":"caption","caption":{"callId":"call-batch","text":"reticulating the splines","category":"reticulate"},"timestamp":"t"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("writing the journal: %v", err)
	}
	entry := toolEntryFor(t, reopen(t, path).Transcript(), "call-batch")
	if entry.Caption != "reticulating the splines" {
		t.Fatalf("an unknown family cost the sentence: %q", entry.Caption)
	}
	if _, known := ParseActionCategory(string(entry.CaptionCategory)); known {
		t.Fatal("the fixture no longer uses a family this build cannot parse")
	}
}
