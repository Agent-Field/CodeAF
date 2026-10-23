package session

// A factory stage's turn that must report, the report the factory makes for a
// stage, and the hand-offs a stage never has (tools_stage.go, beltfacts.go,
// checkpoint.go's MoveTurnToTask), held to what they promise.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// choiceCompleter answers a turn's requests in order and keeps the
// `tool_choice` and the tool names each one carried. The errands no fixture
// is about (the narrator, the namer) are answered with silence off the queue.
type choiceCompleter struct {
	mu      sync.Mutex
	answers []*ai.Response
	choices []any
	belts   [][]string
}

func (c *choiceCompleter) CompleteWithMessages(_ context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	if isCaptionCall(messages) || isTitleCall(messages) {
		return textResponse(""), nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var belt []string
	for _, tool := range request.Tools {
		belt = append(belt, tool.Function.Name)
	}
	c.choices = append(c.choices, request.ToolChoice)
	c.belts = append(c.belts, belt)
	if len(c.answers) == 0 {
		return textResponse("done"), nil
	}
	next := c.answers[0]
	c.answers = c.answers[1:]
	return next, nil
}

func (c *choiceCompleter) seen() ([]any, [][]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]any(nil), c.choices...), append([][]string(nil), c.belts...)
}

// requiredName is the function a `tool_choice` names, "" for a word or none.
func requiredName(choice any) string {
	named, _ := choice.(map[string]any)
	function, _ := named["function"].(map[string]any)
	name, _ := function["name"].(string)
	return name
}

func stageTestAgent(t *testing.T, completer Completer, door StageDoor) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, completer, func(c *Config) {
		c.Stage = door
		c.Unattended = true
	})
	return agent
}

// THE TURN THAT MUST REPORT names `stage_result` in `tool_choice` on its first
// request and on no other: the call is answered, the next request is free, and
// an ordinary turn after it owes nothing.
func TestATurnThatMustReportRequiresTheVerbOnItsFirstRequestOnly(t *testing.T) {
	completer := &choiceCompleter{answers: []*ai.Response{
		toolResponse("c1", "stage_result", `{"done":true,"output":"planned two edits"}`),
		textResponse("reported"),
	}}
	door := &fakeStageDoor{}
	agent := stageTestAgent(t, completer, door)
	events, err := agent.FollowUpRequiring("Call stage_result now.", "stage_result")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	events, err = agent.FollowUp("and one more thing")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	choices, _ := completer.seen()
	if len(choices) != 3 {
		t.Fatalf("%d requests, want 3: %#v", len(choices), choices)
	}
	if requiredName(choices[0]) != "stage_result" {
		t.Fatalf("the first request's tool_choice = %#v, want stage_result named", choices[0])
	}
	for i, choice := range choices[1:] {
		if choice != "auto" {
			t.Errorf("request %d still required a tool: %#v", i+2, choice)
		}
	}
	if len(door.reports) != 1 || door.reports[0].Output != "planned two edits" {
		t.Fatalf("door got %+v", door.reports)
	}
}

// A TOOL THE BELT DOES NOT CARRY IS NEVER NAMED: a choice of a function the
// request does not define is refused by every provider that reads it.
func TestATurnNeverRequiresAToolItsBeltDoesNotCarry(t *testing.T) {
	completer := &choiceCompleter{}
	agent, _ := newTestAgent(t, completer, nil)
	events, err := agent.FollowUpRequiring("report", "stage_result")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if choices, _ := completer.seen(); len(choices) == 0 || choices[0] != "auto" {
		t.Fatalf("a conversation with no stage_result was made to call it: %#v", choices)
	}
}

// THE LAST RESORT is one call that carries `stage_result` alone and requires
// it; what it reports comes back to the runner and never reaches the door.
func TestAReportMadeForAStageCallsTheVerbAndReachesNoDoor(t *testing.T) {
	completer := &choiceCompleter{answers: []*ai.Response{
		toolResponse("c1", "stage_result", `{"done":true,"findings":0,"output":"changed ledger.go"}`),
	}}
	door := &fakeStageDoor{}
	agent := stageTestAgent(t, completer, door)
	res, err := agent.InferStageResult(context.Background(), "The step's brief: plan it.")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Done || res.Output != "changed ledger.go" || len(door.reports) != 0 {
		t.Fatalf("result %+v, door %+v", res, door.reports)
	}
	choices, belts := completer.seen()
	if len(choices) != 1 || requiredName(choices[0]) != "stage_result" || strings.Join(belts[0], ",") != "stage_result" {
		t.Fatalf("the call carried %v and chose %#v", belts, choices)
	}

	words := &choiceCompleter{answers: []*ai.Response{textResponse("I think it is done")}}
	if _, err := stageTestAgent(t, words, door).InferStageResult(context.Background(), "x"); !errors.Is(err, errNoStageReport) {
		t.Fatalf("an answer in words = %v", err)
	}
	plain, _ := newTestAgent(t, &choiceCompleter{}, nil)
	if _, err := plain.InferStageResult(context.Background(), "x"); !errors.Is(err, errNotAStage) {
		t.Fatalf("a conversation that is not a stage = %v", err)
	}
}

func TestLastWordsIsWhatTheModelLastSaid(t *testing.T) {
	completer := &choiceCompleter{answers: []*ai.Response{textResponse("finishing here · the plan is ready")}}
	agent := stageTestAgent(t, completer, &fakeStageDoor{})
	if got := agent.LastWords(); got != "" {
		t.Fatalf("a fresh conversation said %q", got)
	}
	events, err := agent.FollowUp("plan it")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if got := agent.LastWords(); got != "finishing here · the plan is ready" {
		t.Fatalf("last words = %q", got)
	}
}

// NO STAGE CONVERSATION CAN HAND WORK TO A QUICK TASK: the verb is off its
// belt, its page names no quick task, and a full task is still its to start.
func TestAStageBeltCarriesNoQuickTask(t *testing.T) {
	stage := stageTestAgent(t, &choiceCompleter{}, &fakeStageDoor{})
	plain, _ := newTestAgent(t, &choiceCompleter{}, nil)
	if !plain.offers(quickTaskToolName) {
		t.Fatal("the plain conversation this test compares against has no quick_task")
	}
	if stage.offers(quickTaskToolName) || stage.offers(quickItemsToolName) {
		t.Fatal("a stage conversation carries quick_task")
	}
	if !stage.offers("propose_task") || !stage.offers("stage_result") {
		t.Fatal("a stage conversation lost propose_task or stage_result")
	}
	page := promptWithBeltFacts(stage.config)
	if strings.Contains(page, "`"+quickTaskToolName+"`") {
		t.Fatal("a stage's page names quick_task, which its belt does not carry")
	}
	if !strings.Contains(page, "`propose_task`") || !strings.Contains(page, "it has no quick tasks") {
		t.Fatal("a stage's page does not say the one road it has")
	}
	if stage.config.maySweepReads() {
		t.Fatal("a stage's reads may be swept to a quick task")
	}
}

// AND ITS REPLY IS NEVER MOVED TO A TASK, whoever asks.
func TestAStagesReplyIsNeverMovedToATask(t *testing.T) {
	stage := stageTestAgent(t, &choiceCompleter{}, &fakeStageDoor{})
	stage.config.HandOverOnAsk = true
	if err := stage.MoveTurnToTask(); !errors.Is(err, errStageStays) {
		t.Fatalf("a stage's reply could be moved: %v", err)
	}
	if stage.moveAsked.Load() {
		t.Fatal("the move was taken for the next boundary")
	}
}

func TestADailyLimitIsToldApartFromAConversationsOwn(t *testing.T) {
	if !IsDailyLimit(spendRailReached{daily: true, said: "daily limit reached"}) {
		t.Fatal("today's limit is not the daily limit")
	}
	if IsDailyLimit(spendRailReached{said: "conversation limit reached"}) || IsDailyLimit(errors.New("daily limit reached")) {
		t.Fatal("the daily limit was read from words or from another limit")
	}
	if !errors.Is(spendRailReached{daily: true}, ErrSpendRail) {
		t.Fatal("the daily limit no longer unwraps to the spend rail")
	}
}
