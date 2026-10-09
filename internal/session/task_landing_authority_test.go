package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

type landingAuthorityCompleter struct {
	scriptedCompleter
	toolsMu    sync.Mutex
	toolCounts []int
}

func (c *landingAuthorityCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var req ai.Request
	for _, o := range options {
		_ = o(&req)
	}
	c.toolsMu.Lock()
	c.toolCounts = append(c.toolCounts, len(req.Tools))
	c.toolsMu.Unlock()
	return c.scriptedCompleter.CompleteWithMessages(ctx, messages, options...)
}
func TestCompletionCannotRecommissionEvenIfProviderReturnsTools(t *testing.T) {
	question := "Start a background task: sleep 30 and write the marker. Return immediately."
	c := &landingAuthorityCompleter{scriptedCompleter: scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("repeat-task", "propose_task", `{"title":"Repeat marker","summary":"Repeat the already finished marker command.","brief":"Run sleep 30 and write the marker again.","acceptance":"Marker contains NATIVE_REATTACH_DONE.","deliverable":"native-reattach-marker.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("repeat-command", "landing_probe", `{}`), nil
		},
		finalText("The original task finished and wrote the marker."),
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("fresh-command", "landing_probe", `{}`), nil
		},
		finalText("The new requested action ran."),
	}}}
	a, _ := newTestAgent(t, c, func(cfg *Config) { cfg.AskConsent = false })
	var executed atomic.Int32
	a.tools = append(a.tools, bare.Tool{Name: "landing_probe", Description: "records actual execution", Schema: json.RawMessage(`{"type":"object","properties":{}}`), Execute: func(context.Context, json.RawMessage) (string, bool, error) {
		executed.Add(1)
		return "ran", false, nil
	}})
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "run", "1", "Marker", question)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.Revise(store.RootID(), plandb.TaskPatch{Question: &question}); err != nil {
		t.Fatal(err)
	}
	wakes, stop := a.WatchWakes()
	defer stop()
	a.deliverBeltRunLanding(&beltRun{store: store, root: store.RootID()}, RunSummary{Outcome: beltRunOutcomeDone, Result: "The marker was written."}, RunLanding{})
	stream := <-wakes
	for range stream {
	}
	if rows := a.PlanTasks(); len(rows) != 0 {
		t.Fatalf("completion created new tasks: %#v", rows)
	}
	if executed.Load() != 0 {
		t.Fatal("completion executed a new action")
	}
	a.mu.Lock()
	owed := append([]owedAsk(nil), a.owedAsks...)
	a.mu.Unlock()
	for _, ask := range owed {
		if ask.from == owedByPerson {
			t.Fatalf("completion claimed fresh human authority: %#v", ask)
		}
	}
	request := c.request(0)
	last := messageText(request[len(request)-1])
	if strings.HasPrefix(last, question) || !strings.Contains(last, "not a new request") || !strings.Contains(last, "The marker was written.") {
		t.Fatalf("completion lost framing/result: %q", last)
	}
	c.toolsMu.Lock()
	counts := append([]int(nil), c.toolCounts...)
	c.toolsMu.Unlock()
	if len(counts) < 3 {
		t.Fatalf("missing retry requests: %v", counts)
	}
	for _, n := range counts[:3] {
		if n != 0 {
			t.Fatalf("completion exposed tools: %v", counts)
		}
	}
	events, err := a.Submit(context.Background(), "Now run landing_probe once.")
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	if executed.Load() != 1 {
		t.Fatalf("new person request executed %d actions, want1", executed.Load())
	}
}
func TestCompletionGuardPreservesDecisionWakesAndFreshSteering(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	ctx := withSettleWake(context.Background(), settleWake{ceiling: 3, answerOnly: true})
	if !a.answerOnlyLanding(ctx) {
		t.Fatal("completion boundary missing")
	}
	a.mu.Lock()
	a.owedAsks = []owedAsk{{text: "new instruction", from: owedByPerson}}
	a.mu.Unlock()
	if a.answerOnlyLanding(ctx) {
		t.Fatal("fresh human steering did not restore tools")
	}
	if a.answerOnlyLanding(withSettleWake(context.Background(), settleWake{ceiling: 3})) {
		t.Fatal("ordinary decision wake was restricted")
	}
}

func TestCompletionRestrictionDoesNotSwallowAnotherQueuedDecision(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.steering = []userMessage{{settle: true, settleCeiling: 3, settleAnswerOnly: true, settlePrompt: landingAnswerPrompt}, {settle: true, settleCeiling: 3}}
	wake, ok := a.settleWakeLocked()
	if !ok || wake.answerOnly {
		t.Fatal("completion restricted another landing decision")
	}
}

func TestProgramOutcomeKeepsItsActionableVerificationAuthority(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	question := "Repair the parser and verify it."
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "run", "1", "Repair", question)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.Revise(store.RootID(), plandb.TaskPatch{Question: &question}); err != nil {
		t.Fatal(err)
	}
	note := a.programLandingNote(&beltRun{store: store, root: store.RootID()}, RunSummary{Outcome: "unverified"}, "Run the verification checks before accepting the result.")
	if note.settleAnswerOnly || note.settlePrompt != programOutcomePrompt || !strings.Contains(note.text(), "Run the verification checks") {
		t.Fatalf("program action authority lost: %#v", note)
	}
	if strings.Contains(note.text(), "do not commission or execute it again") {
		t.Fatal("completion restriction leaked into actionable program outcome")
	}
	a.mu.Lock()
	a.steering = []userMessage{note}
	wake, _ := a.settleWakeLocked()
	a.mu.Unlock()
	if a.answerOnlyLanding(withSettleWake(context.Background(), wake)) {
		t.Fatal("program verification tools were disabled")
	}
}
