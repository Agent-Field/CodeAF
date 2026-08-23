package jsrun

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// A SCRIPTED ENV AND NOTHING ELSE. Every test in this package runs against this
// fake: no model is called, no network is opened, no file is read. What the
// runtime is being tested for is what it does with the answers, not what the
// answers are, and a test that needed a real model would be a test that could
// not say which of the two had changed.

type scriptedEnv struct {
	// answers is what the model says, by the prompt ref that asked. A ref with
	// no entry answers with the ref itself, so a test that does not care about
	// the text does not have to script one.
	answers map[string]exec.Answer
	// tools is what each belt tool produces.
	tools map[string]exec.ToolResult
	// asks is what the person says, by question.
	asks map[string]exec.AskAnswer
	// notes is this subharness's memory.
	notes []exec.Note

	// failAI, failTool and failLog make a door fail, for the paths that only
	// exist when one does.
	failAI   error
	failTool error
	failLog  error

	// seen is every call this Env was asked to make, in order, as
	// "door:reference". It is what the whitelist and inline-prompt tests read to
	// prove a refused call never arrived.
	seen []string
	// prompts is what a [Prompted] Env was handed.
	prompts map[string]string
}

func (e *scriptedEnv) UsePrompts(prompts map[string]string) { e.prompts = prompts }

func (e *scriptedEnv) AI(_ context.Context, ref string, _ any, _ exec.AIOptions) (exec.Answer, error) {
	e.seen = append(e.seen, "ai:"+ref)
	if e.failAI != nil {
		return exec.Answer{}, e.failAI
	}
	if answer, ok := e.answers[ref]; ok {
		return answer, nil
	}
	return exec.Answer{Text: ref}, nil
}

func (e *scriptedEnv) Tool(_ context.Context, name string, _ map[string]any) (exec.ToolResult, error) {
	e.seen = append(e.seen, "tool:"+name)
	if e.failTool != nil {
		return exec.ToolResult{}, e.failTool
	}
	if result, ok := e.tools[name]; ok {
		return result, nil
	}
	return exec.ToolResult{Text: name + " said nothing"}, nil
}

func (e *scriptedEnv) Ask(_ context.Context, question string, opts exec.AskOptions) (exec.AskAnswer, error) {
	e.seen = append(e.seen, "ask:"+question)
	if answer, ok := e.asks[question]; ok {
		return answer, nil
	}
	if opts.Default != "" {
		return exec.AskAnswer{Text: opts.Default}, nil
	}
	return exec.AskAnswer{Unanswered: true}, nil
}

func (e *scriptedEnv) Remember(_ context.Context, note string) error {
	e.seen = append(e.seen, "remember:"+note)
	e.notes = append(e.notes, exec.Note{Text: note})
	return nil
}

func (e *scriptedEnv) Recall(_ context.Context, query string) ([]exec.Note, error) {
	e.seen = append(e.seen, "recall:"+query)
	if query == "" {
		return e.notes, nil
	}
	var found []exec.Note
	for _, note := range e.notes {
		if strings.Contains(note.Text, query) {
			found = append(found, note)
		}
	}
	return found, nil
}

func (e *scriptedEnv) Log(_ context.Context, status string) error {
	e.seen = append(e.seen, "log:"+status)
	return e.failLog
}

// scriptedMemory is a bundle's own memory door, so the test that proves
// remember() prefers it over the Env can tell the two apart.
type scriptedMemory struct {
	kept []exec.Note
}

func (m *scriptedMemory) Remember(_ context.Context, note string) error {
	m.kept = append(m.kept, exec.Note{Text: note})
	return nil
}

func (m *scriptedMemory) Recall(context.Context, string) ([]exec.Note, error) { return m.kept, nil }

// scriptedLook answers the two free questions a guard asks.
type scriptedLook struct {
	files map[string]bool
	belt  map[string]bool
}

func (l scriptedLook) FileExists(path string) bool { return l.files[path] }
func (l scriptedLook) ToolOnBelt(name string) bool { return l.belt[name] }

// pageJournal keeps every entry a run wrote, which is what the ordering and
// spend tests read.
//
// It locks even though the runtime states host calls are serial, because a test
// helper that would hide a future concurrency bug behind a race detector's
// silence is worth more than the two lines it costs.
type pageJournal struct {
	mu      sync.Mutex
	entries []exec.JournalEntry
}

func (j *pageJournal) Write(entry exec.JournalEntry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entries = append(j.entries, entry)
	return nil
}

func (j *pageJournal) calls() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	var names []string
	for _, entry := range j.entries {
		names = append(names, fmt.Sprintf("%d:%s:%s", entry.Seq, entry.Call, entry.Ref))
	}
	return names
}

// toyManifest is the hand-written subharness the pin runs. It promises a
// summary, may read one tool, and has no guards.
func toyManifest() exec.Manifest {
	return exec.Manifest{
		SubharnessInfo: exec.SubharnessInfo{
			Name:    "toy",
			Purpose: "turn a brief into a summary, for the tests",
		},
		Whitelist: []string{"read"},
		Input:     exec.Schema(`{"type":"object","required":["brief"],"properties":{"brief":{"type":"string"}}}`),
		Output: exec.Schema(`{"type":"object","required":["summary"],` +
			`"properties":{"summary":{"type":"string"},"notes":{"type":"string"}}}`),
	}
}

// runToy builds a bundle around one program and runs it, which is what almost
// every test here wants and none of them should have to spell.
func runToy(t *testing.T, bundle Bundle, env *scriptedEnv, input string) (exec.RunResult, *pageJournal) {
	t.Helper()
	if bundle.Manifest.Name == "" {
		bundle.Manifest = toyManifest()
	}
	runner, err := New(bundle)
	if err != nil {
		t.Fatalf("this bundle would not compile: %v", err)
	}
	journal := &pageJournal{}
	ctx := WithJournal(context.Background(), journal)
	result, err := runner.Run(ctx, json.RawMessage(input), env)
	if err != nil {
		t.Fatalf("the run could not be made to happen: %v", err)
	}
	return result, journal
}
