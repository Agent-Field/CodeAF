package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// memoryAgent is a session with a memory file of its own, in a directory the
// test owns. The path is inside t.TempDir() and never ~/.aforge: a test that
// wrote to the person's real memory would be a test that changes their next
// session.
func memoryAgent(t *testing.T, completer Completer) (*Agent, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.md")
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.MemoryFile = path
	})
	return agent, path
}

func systemPromptOf(t *testing.T, completer *scriptedCompleter, request int) string {
	t.Helper()
	messages := completer.request(request)
	if len(messages) == 0 {
		t.Fatalf("request %d was never made", request)
	}
	if messages[0].Role != "system" {
		t.Fatalf("request %d starts with %q, want the system message", request, messages[0].Role)
	}
	return messageText(messages[0])
}

// A note is for the NEXT turn, and that is the whole contract: the tool appends
// while this turn's prompt is already on the wire, and the turn after it opens
// with the fact in front of the model. Anything weaker — a note that needs a
// restart to be seen — makes remembering something a person has to remember to
// do twice.
func TestNoteReachesTheNextTurnsPrompt(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("n1", "note", `{"fact":"prefers tabs over spaces in Go"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("noted"), nil
		},
	}}
	agent, path := memoryAgent(t, completer)

	collect(t, mustSubmit(t, agent, "remember that I prefer tabs"))

	// The turn that wrote the note did not read it: its prompt was rendered
	// before the tool ran.
	if opening := systemPromptOf(t, completer, 0); strings.Contains(opening, "prefers tabs") {
		t.Fatalf("the note appeared in the turn that wrote it:\n%s", opening)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("memory file: %v", err)
	}
	if got, want := strings.TrimSpace(string(stored)), "- prefers tabs over spaces in Go"; got != want {
		t.Fatalf("memory file = %q, want %q", got, want)
	}

	collect(t, mustSubmit(t, agent, "carry on"))

	next := systemPromptOf(t, completer, 2)
	if !strings.Contains(next, "<memory>") || !strings.Contains(next, "</memory>") {
		t.Fatalf("the next turn's prompt has no <memory> block:\n%s", next)
	}
	if !strings.Contains(next, "- prefers tabs over spaces in Go") {
		t.Fatalf("the note is not in the next turn's prompt:\n%s", next)
	}
	if !strings.HasPrefix(next, "SYSTEM") {
		t.Fatalf("the memory block replaced the prompt instead of following it:\n%s", next)
	}
}

// forget removes every line that contains the text and says how many went. The
// count is the point: "forget the tabs thing" that silently matched nothing
// reads exactly like one that worked.
func TestForgetRemovesMatchingLinesAndReportsTheCount(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("f1", "forget", `{"pattern":"TABS"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("dropped"), nil
		},
	}}
	agent, path := memoryAgent(t, completer)
	if err := os.WriteFile(path, []byte("- prefers tabs over spaces\n- runs make check before yielding\n- tabs in Makefiles are mandatory\n"), 0o644); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	events := collect(t, mustSubmit(t, agent, "forget the tabs thing"))

	result := toolOutput(t, events, "forget")
	if !strings.Contains(result, "Forgot 2 line(s)") {
		t.Fatalf("forget said %q, want the count of what it removed", result)
	}
	if !strings.Contains(result, "prefers tabs over spaces") {
		t.Fatalf("forget did not report what it removed: %q", result)
	}

	left, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("memory file: %v", err)
	}
	if got, want := string(left), "- runs make check before yielding\n"; got != want {
		t.Fatalf("memory file = %q, want only the unmatched line (%q)", got, want)
	}
}

// Nothing matched is an ANSWER, not a failure: a model that reads it as an
// error retries the same call with a wilder pattern, which is how a memory file
// gets emptied by accident.
func TestForgetWithNoMatchIsNotAnError(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("f1", "forget", `{"pattern":"emacs"}`), nil
		},
	}}
	agent, path := memoryAgent(t, completer)
	if err := os.WriteFile(path, []byte("- prefers tabs\n"), 0o644); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	events := collect(t, mustSubmit(t, agent, "forget emacs"))

	for _, event := range events {
		if event.Kind == EventToolFailed {
			t.Fatalf("a no-match forget was reported as a failure: %q", event.Output)
		}
	}
	if result := toolOutput(t, events, "forget"); !strings.Contains(result, "nothing was removed") {
		t.Fatalf("forget said %q, want it to say nothing matched", result)
	}
	if left, _ := os.ReadFile(path); string(left) != "- prefers tabs\n" {
		t.Fatalf("a no-match forget rewrote the file: %q", string(left))
	}
}

// No file, no memory: a session with nowhere to keep a note must not be told it
// can take one. A belt that carries the verb and refuses every use of it is
// worse than not having it.
func TestMemoryIsOffWithoutAFile(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, nil)

	if agent.hasTool("note") || agent.hasTool("forget") {
		t.Fatal("note/forget are on the belt of a session with no memory file")
	}
	for _, definition := range agent.definitions {
		if definition.Function.Name == "note" || definition.Function.Name == "forget" {
			t.Fatalf("%s reached the wire without a memory file", definition.Function.Name)
		}
	}

	collect(t, mustSubmit(t, agent, "hello"))

	if prompt := systemPromptOf(t, completer, 0); prompt != "SYSTEM" {
		t.Fatalf("system prompt = %q, want the configured prompt untouched", prompt)
	}
}

// An empty file is the same as no file for the prompt: a <memory> block with
// nothing in it is a header the person pays for on every request.
func TestEmptyMemoryFileRendersNoBlock(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, path := memoryAgent(t, completer)
	if err := os.WriteFile(path, []byte("\n\n  \n"), 0o644); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	collect(t, mustSubmit(t, agent, "hello"))

	if prompt := systemPromptOf(t, completer, 0); prompt != "SYSTEM" {
		t.Fatalf("system prompt = %q, want no block for an empty file", prompt)
	}
}

// A memory file over the block limit rides as its TAIL, cut on a line boundary
// and marked. Half a remembered fact is a fact nobody stated, and a silent cut
// makes the model act on the half.
func TestOversizeMemoryKeepsWholeLinesFromTheEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.md")
	var seed strings.Builder
	seed.WriteString("- the oldest fact, which will not fit\n")
	for seed.Len() < memoryBlockLimit {
		seed.WriteString("- " + strings.Repeat("filler ", 20) + "\n")
	}
	seed.WriteString("- the newest fact, which must survive\n")
	if err := os.WriteFile(path, []byte(seed.String()), 0o644); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	block := newMemoryStore(path).block()
	if !strings.Contains(block, "- the newest fact, which must survive") {
		t.Fatal("the newest line was cut away; the tail is what must survive")
	}
	if strings.Contains(block, "- the oldest fact, which will not fit") {
		t.Fatal("the block is over the limit: the oldest line should have been cut")
	}
	if !strings.Contains(block, "Only the most recent 4KiB is shown") {
		t.Fatalf("a truncated block did not say so:\n%s", block)
	}
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, "filler") {
			t.Fatalf("the cut landed inside a line: %q", line)
		}
	}
}

// toolOutput is one named tool's result as the surface saw it. It reads the
// event rather than the transcript because a surface's copy is the one a person
// is shown, and the two must agree.
func toolOutput(t *testing.T, events []Event, tool string) string {
	t.Helper()
	for _, event := range events {
		if event.Tool != tool {
			continue
		}
		if event.Kind == EventToolEnd || event.Kind == EventToolFailed {
			return event.Output
		}
	}
	t.Fatalf("no result event for %s; events = %v", tool, kinds(events))
	return ""
}
