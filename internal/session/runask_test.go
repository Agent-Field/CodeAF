package session

import (
	"context"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

type runAskRecorder struct {
	inner    *scriptedCompleter
	mu       sync.Mutex
	requests []ai.Request
}

func (r *runAskRecorder) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var req ai.Request
	for _, option := range options {
		_ = option(&req)
	}
	r.mu.Lock()
	r.requests = append(r.requests, req)
	r.mu.Unlock()
	return r.inner.CompleteWithMessages(ctx, messages, options...)
}

func runAskFixture(t *testing.T, script ...step) (*Agent, *runAskRecorder, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "ask-chat", plandb.TaskSpec{ID: "child", Title: "Write handler", Description: "replace the lookup"})
	store, err := plandb.Open(path, "the run", planRootID, "The run", "drive the plan", "ask-chat")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.AddNote("child", "worker", "use token checks"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Claim("child", "worker"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Done("child", "worker", "Changed lookup and refresh.", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for i := 1; i <= 14; i++ {
		lines = append(lines, `{"kind":"step","step":`+runAskItoa(i)+`,"command":"cmd `+runAskItoa(i)+`","observation":"observation `+runAskItoa(i)+`"}`)
	}
	writePlanTrajectory(t, dir, "child", lines...)
	rec := &runAskRecorder{inner: &scriptedCompleter{steps: script}}
	agent, _ := newTestAgent(t, rec, nil)
	armPlanStore(t, agent, path, "ask-chat")
	return agent, rec, path
}
func runAskItoa(n int) string {
	const digits = "0123456789"
	if n < 10 {
		return string(digits[n])
	}
	return string(digits[n/10]) + string(digits[n%10])
}

func TestAskRunExactReadTaskBeltAndBoundedPage(t *testing.T) {
	agent, rec, _ := runAskFixture(t,
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "read_task", `{"id":"t-child"}`), nil
		},
		func(_ context.Context, m []ai.Message) (*ai.Response, error) {
			transcript := runAskMessagesText(m)
			for _, want := range []string{"replace the lookup", "Changed lookup and refresh.", "use token checks", "cmd 3", "cmd 14"} {
				if !strings.Contains(transcript, want) {
					t.Errorf("tool page missing %q", want)
				}
			}
			if strings.Contains(transcript, "cmd 2") {
				t.Error("read_task exposed more than the last twelve steps")
			}
			return textResponse(`{"text":"It changed the lookup.","from":[{"id":"t-child","title":"Write handler","step_start":3,"step_end":14}]}`), nil
		})
	got, err := agent.AskRun(context.Background(), "t-root", "what changed?", []RunAskExchange{{Question: "old 1", Answer: "a"}, {Question: "old 2", Answer: "b"}, {Question: "old 3", Answer: "c"}, {Question: "old 4", Answer: "d"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "It changed the lookup." || len(got.From) != 1 || got.From[0].StepStart != 3 || got.From[0].StepEnd != 14 {
		t.Fatalf("AskRun = %#v", got)
	}
	if len(rec.requests) != 2 {
		t.Fatalf("requests=%d want 2", len(rec.requests))
	}
	for _, req := range rec.requests {
		if len(req.Tools) != 1 || req.Tools[0].Function.Name != "read_task" {
			t.Fatalf("belt=%#v, want exactly read_task", req.Tools)
		}
	}
	first := runAskMessagesText(rec.inner.request(0))
	if strings.Contains(first, "old 1") || !strings.Contains(first, "old 4") {
		t.Fatalf("earlier exchange bound not applied: %s", first)
	}
}

func TestAskRunRefusesMissingSourceOnce(t *testing.T) {
	agent, rec, _ := runAskFixture(t,
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(`{"text":"guess","from":[]}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(`{"text":"still unknown","from":[]}`), nil
		})
	got, err := agent.AskRun(context.Background(), "root", "why?", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "still unknown" || len(got.From) != 0 || len(rec.requests) != 2 {
		t.Fatalf("got=%#v requests=%d", got, len(rec.requests))
	}
	if !strings.Contains(runAskMessagesText(rec.inner.request(1)), "source") {
		t.Fatal("retry did not demand a source")
	}
}

func TestAskRunSteerNeverWritesAndSendRunNoteWritesOneRow(t *testing.T) {
	agent, rec, path := runAskFixture(t)
	before := runAskNotes(t, path)
	got, err := agent.AskRun(context.Background(), "root", "tell it to skip the fixtures", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsNote || got.Note != "skip the fixtures" {
		t.Fatalf("steer=%#v", got)
	}
	if rec.inner.requests() != 0 || len(runAskNotes(t, path)) != len(before) {
		t.Fatal("AskRun sent or wrote the steer")
	}
	if err := agent.SendRunNote("root", got.Note); err != nil {
		t.Fatal(err)
	}
	after := runAskNotes(t, path)
	if len(after) != len(before)+1 {
		t.Fatalf("notes %d -> %d", len(before), len(after))
	}
	note := after[len(after)-1]
	if note.From != plandb.NoteFromPerson || note.Body != "skip the fixtures" || note.Agent != "" {
		t.Fatalf("note=%#v", note)
	}
}
func runAskNotes(t *testing.T, path string) []plandb.Note {
	t.Helper()
	s, e := plandb.Open(path, "the run", planRootID, "The run", "drive the plan", "ask-chat")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	return s.Notes("root", 0)
}
func runAskMessagesText(ms []ai.Message) string {
	var b strings.Builder
	for _, m := range ms {
		for _, p := range m.Content {
			b.WriteString(p.Text)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// A MODEL WRAPS ITS OBJECT IN A FENCE OR A SENTENCE MORE OFTEN THAN NOT. The
// answer and its sources are still read; text with no object is the answer with
// no source, which the caller refuses once.
func TestRunAskAnswerIsReadThroughAFenceAndANoteIsRecognised(t *testing.T) {
	fenced := "Here you go:\n```json\n{\"text\":\"It checks the token.\",\"from\":[{\"id\":\"t-leaf\",\"title\":\"write the handler\",\"step_start\":7,\"step_end\":11}]}\n```"
	got, err := decodeRunAsk(fenced)
	if err != nil || got.Text != "It checks the token." || len(got.From) != 1 || got.From[0].ID != "t-leaf" || got.From[0].StepEnd != 11 {
		t.Fatalf("fenced answer = %#v, %v", got, err)
	}
	bare, _ := decodeRunAsk("The record does not say.")
	if bare.Text != "The record does not say." || len(bare.From) != 0 || bare.IsNote {
		t.Fatalf("bare answer = %#v", bare)
	}
	note, _ := decodeRunAsk(`{"note":"skip the fixtures"}`)
	if !note.IsNote || note.Note != "skip the fixtures" || note.Text != "" {
		t.Fatalf("a recognised steer = %#v, want a note and no answer", note)
	}
}

// A QUESTION ASKED OF A RUN IS PAID FOR, SO IT IS IN THE BOOKS, every round of
// it: the read_task round and the answer alike, which reached the journal and
// nothing else until this test.
func TestAQuestionAskedOfARunIsInTheConversationsBooks(t *testing.T) {
	agent, _, _ := runAskFixture(t,
		func(context.Context, []ai.Message) (*ai.Response, error) {
			response := toolResponse("c1", "read_task", `{"id":"t-child"}`)
			cost := 0.01
			response.Usage = &ai.Usage{PromptTokens: 10, CompletionTokens: 5, Cost: &cost}
			return response, nil
		},
		pricedText(`{"text":"It changed the lookup.","from":[{"id":"t-child","title":"Write handler","step_start":3,"step_end":14}]}`, 0.02))
	if _, err := agent.AskRun(context.Background(), "t-root", "what changed?", nil); err != nil {
		t.Fatal(err)
	}
	if got := agent.Usage().CostUSD; math.Abs(got-0.03) > 1e-9 {
		t.Fatalf("the conversation's books hold $%.4f, want both rounds' $0.03", got)
	}
}
