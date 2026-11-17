package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeWorkstream answers the workstream verbs the guard calls, and records
// what reached it.
type fakeWorkstream struct {
	decisions string
	calls     []string
	args      []string
}

func (f *fakeWorkstream) call(_ context.Context, tool string, args json.RawMessage) (string, bool, error) {
	f.calls = append(f.calls, tool)
	f.args = append(f.args, string(args))
	switch tool {
	case WSDecisionsTool:
		return f.decisions, false, nil
	case WSTaskCreateTool:
		return `{"said":"Made “Price post”.","task":{"id":"tk_9"}}`, false, nil
	case WSTasksTool:
		return `{"task":{"title":"What's new for 2.4","request":"Draft the text","results":[{"kind":"file","file_id":"f1"}]}}`, false, nil
	case WSFileReadTool:
		return `{"markdown":"Pantry Plus is now $5.99 a month."}`, false, nil
	}
	return `{"said":"ok"}`, false, nil
}

const pantryDecisions = `{"decisions":[{"id":"dc_1","text":"No price change in 2.4","by":"priya","by_name":"Priya"},{"id":"dc_2","text":"Dark mode ships after 2.5","by":"marco","by_name":"Marco"}]}`

func TestNewWorkAgainstADecisionIsMadeThenPausedOnItsMaker(t *testing.T) {
	f := &fakeWorkstream{decisions: pantryDecisions}
	var prompt string
	out, isErr, err := guardDecisionsWith(context.Background(), WSTaskCreateTool, json.RawMessage(`{"title":"Price post","request":"Announce $5.99"}`), f.call,
		func(_ context.Context, p string) (string, error) {
			prompt = p
			return `{"conflicts":[{"decision":1,"why":"it raises the price"}]}`, nil
		})
	if err != nil || isErr {
		t.Fatal(out, err)
	}
	if !strings.Contains(prompt, "1. No price change in 2.4") || !strings.Contains(prompt, "Announce $5.99") {
		t.Fatalf("prompt:\n%s", prompt)
	}
	if strings.Join(f.calls, ",") != "ws_decisions,ws_task_create,ws_task_conflict" {
		t.Fatalf("calls = %v", f.calls)
	}
	if !strings.Contains(f.args[2], `"decision":"dc_1"`) || !strings.Contains(f.args[2], `"task":"tk_9"`) {
		t.Errorf("conflict = %s", f.args[2])
	}
	if !strings.Contains(out, "Priya is asked") {
		t.Errorf("the model is not told who was asked: %s", out)
	}
}

func TestFinishedWorkAgainstADecisionIsNotDoneButKeepsItsResult(t *testing.T) {
	f := &fakeWorkstream{decisions: pantryDecisions}
	var prompt string
	out, _, _ := guardDecisionsWith(context.Background(), WSTaskUpdateTool, json.RawMessage(`{"task":"tk_1","state":"done","check":"under 300 words"}`), f.call,
		func(_ context.Context, p string) (string, error) {
			prompt = p
			return "Sure:\n```json\n{\"conflicts\":[{\"decision\":\"1\",\"why\":\"says $5.99\"}]}\n```", nil
		})
	if !strings.Contains(prompt, "Pantry Plus is now $5.99") {
		t.Fatalf("the check did not read the result file:\n%s", prompt)
	}
	for i, c := range f.calls {
		if c == WSTaskUpdateTool && strings.Contains(f.args[i], `"state"`) {
			t.Fatalf("done reached the door: %s", f.args[i])
		}
	}
	if !strings.Contains(strings.Join(f.calls, ","), "ws_task_update,ws_task_conflict") || !strings.HasPrefix(jsonSaid(out), "Not done") {
		t.Fatalf("calls %v, out %s", f.calls, out)
	}
}

func TestTheCheckStandsAsideWhenItCannotJudge(t *testing.T) {
	for name, tc := range map[string]struct {
		decisions, reply string
	}{
		"no decisions":    {`{"decisions":[]}`, ""},
		"no verb":         {"", ""},
		"unreadable":      {pantryDecisions, "I think it's fine"},
		"a number not on": {pantryDecisions, `{"conflicts":[{"decision":9,"why":"x"}]}`},
		"nothing":         {pantryDecisions, `{"conflicts":[]}`},
	} {
		f := &fakeWorkstream{decisions: tc.decisions}
		if tc.decisions == "" {
			f.decisions = "not json"
		}
		_, _, _ = guardDecisionsWith(context.Background(), WSTaskCreateTool, json.RawMessage(`{"title":"Roadmap entry","request":"Dark mode after 2.5"}`), f.call,
			func(context.Context, string) (string, error) { return tc.reply, nil })
		if strings.Contains(strings.Join(f.calls, ","), WSTaskConflictVerb) {
			t.Errorf("%s: raised a conflict: %v", name, f.calls)
		}
		if !strings.Contains(strings.Join(f.calls, ","), WSTaskCreateTool) {
			t.Errorf("%s: the work was not made", name)
		}
	}
}

func jsonSaid(s string) string {
	var o struct{ Said string }
	_ = json.Unmarshal([]byte(s), &o)
	return o.Said
}

func TestADocumentAgainstADecisionIsNotWritten(t *testing.T) {
	f := &fakeWorkstream{decisions: pantryDecisions}
	out, isErr, _ := guardDecisionsWith(context.Background(), WSFileCreateTool, json.RawMessage(`{"name":"What's new","markdown":"Pantry Plus is now $5.99"}`), f.call,
		func(context.Context, string) (string, error) {
			return `{"conflicts":[{"decision":1,"why":"raises the price"}]}`, nil
		})
	if !isErr || !strings.HasPrefix(out, "Nothing written") || strings.Contains(strings.Join(f.calls, ","), WSFileCreateTool) {
		t.Fatalf("calls %v, out %q", f.calls, out)
	}
}

// A CONFLICT THAT COULD NOT BE ASKED IS NOT SAID TO BE (live2/E6b): the work
// made is answered as made, the tool is not called again, and the reply
// claims no pause.
func TestAConflictTheDoorCannotAskClaimsNoPause(t *testing.T) {
	f := &fakeWorkstream{decisions: pantryDecisions}
	failing := func(ctx context.Context, tool string, args json.RawMessage) (string, bool, error) {
		if tool == WSTaskConflictVerb {
			f.calls = append(f.calls, tool)
			return "client_unavailable", true, nil
		}
		return f.call(ctx, tool, args)
	}
	out, isErr, _ := guardDecisionsWith(context.Background(), WSTaskCreateTool, json.RawMessage(`{"title":"Price post","request":"Announce $5.99"}`), failing,
		func(context.Context, string) (string, error) {
			return `{"conflicts":[{"decision":1,"why":"raises the price"}]}`, nil
		})
	creates := strings.Count(strings.Join(f.calls, ","), WSTaskCreateTool)
	if isErr || creates != 1 || strings.Contains(out, "Paused") || strings.Contains(out, "asked") {
		t.Fatalf("calls %v, out %q", f.calls, out)
	}
}

// The reply names whoever the door asked: on Personal that is the person in
// the conversation, never a name from the decision's byline.
func TestTheReplyNamesWhoTheDoorAsked(t *testing.T) {
	if got := conflictAsked(`{"asked":"you"}`, checkDecision{ByName: "Priya"}); got != "the person you are talking to" {
		t.Fatal(got)
	}
	if got := conflictAsked(`{"asked":"Santosh"}`, checkDecision{ByName: "Priya"}); got != "Santosh" {
		t.Fatal(got)
	}
	if got := conflictAsked(`{}`, checkDecision{ByName: "Priya"}); got != "Priya" {
		t.Fatal(got)
	}
}
