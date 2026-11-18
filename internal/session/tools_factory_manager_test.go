package session

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/manual"
)

// offeredOnBelt reports whether a name is among the tools an agent offers the model.
func offeredOnBelt(a *Agent, name string) bool {
	a.tools = a.belt()
	for _, tool := range a.offeredTools() {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// `factory_act` AND `factory_task` RIDE ON THE MANAGER'S DOOR WHERE IT CAN
// PRESS, and are absent everywhere else; the manual names them, each has a
// family, and Auto does not stop to ask before them.
func TestTheManagersActAndTaskAreOnTheBeltOnlyWhereTheDoorPresses(t *testing.T) {
	for _, name := range []string{"factory_act", "factory_task"} {
		if !offeredOnBelt(&Agent{config: Config{Workspace: t.TempDir(), FactoryRun: &prDecisionDoor{}}}, name) {
			t.Errorf("a door that presses did not put %s on the belt", name)
		}
		if offeredOnBelt(&Agent{config: Config{Workspace: t.TempDir(), FactoryRun: &fakeRunDoor{}}}, name) {
			t.Errorf("a door that cannot press put %s on the belt", name)
		}
		if offeredOnBelt(&Agent{config: Config{Workspace: t.TempDir()}}, name) {
			t.Errorf("a belt with no run door carries %s", name)
		}
		if !manual.Chat().Mentions(name) {
			t.Errorf("no chat manual page mentions %s", name)
		}
		if glossField[name] == "" {
			t.Errorf("%s has no gloss", name)
		}
	}
	if got := ActionCategoryForTool("factory_act"); got != ActionTransfer {
		t.Errorf("factory_act's family is %q, want %q", got, ActionTransfer)
	}
	if got := ActionCategoryForTool("factory_task"); got != ActionCoordinate {
		t.Errorf("factory_task's family is %q, want %q", got, ActionCoordinate)
	}
	// AUTO IS THE PERSON'S STANDING YES TO THE JOB'S OWN ACTS: neither tool
	// waits for a tool approval there, and an explicit denial still holds.
	p := automaticJobPolicy(&approval.Policy{Default: approval.ActionPrompt, Tools: map[string]approval.Action{"factory_task": approval.ActionDeny}})
	if d := p.Check("factory_act", json.RawMessage(`{}`)); d.Action != approval.ActionAllow {
		t.Errorf("Auto asks before factory_act: %s", d.Action)
	}
	if d := p.Check("factory_task", json.RawMessage(`{}`)); d.Action != approval.ActionDeny {
		t.Errorf("Auto overrode a denial of factory_task: %s", d.Action)
	}
	if d := automaticJobPolicy(&approval.Policy{Default: approval.ActionPrompt}).Check("factory_task", json.RawMessage(`{}`)); d.Action != approval.ActionAllow {
		t.Errorf("Auto asks before factory_task: %s", d.Action)
	}
	for _, want := range []string{"factory_task", "factory_act", "Never merge"} {
		if !strings.Contains(jobAutoInstruction, want) {
			t.Errorf("the Auto instruction does not say %q", want)
		}
	}
}

// `factory_act` HANDS THE ROUND'S ACTS TO THE DOOR AS ONE PRESS, with its one
// line of why and the person's own words of the turn. What is posted is kept
// as written; names are trimmed.
func TestFactoryActPressesTheActsAsWritten(t *testing.T) {
	d := &prDecisionDoor{}
	a := &Agent{config: Config{FactoryRun: d, SessionFile: "/tmp/manager/transcript.jsonl"}}
	a.personWords = "answer that comment and push"
	args := `{"acts":[{"kind":" reply ","thread":" inline:77 ","text":"Fixed in the next push.\n\nThanks."},{"kind":"push"},{"kind":"rerequest_review","reviewers":[" @sam ",""]}],"why":"  the reviewer's  note is addressed "}`
	out, bad, err := a.factoryActTool().Execute(context.Background(), json.RawMessage(args))
	if err != nil || bad || out == "" || len(d.pressed) != 1 {
		t.Fatalf("act = %q bad=%v err=%v pressed=%d", out, bad, err, len(d.pressed))
	}
	got := d.pressed[0]
	want := ManagerPress{Action: ActionAct, Words: "the reviewer's note is addressed", Said: "answer that comment and push", Acts: []PublicAct{
		{Kind: ActReply, Thread: "inline:77", Text: "Fixed in the next push.\n\nThanks."},
		{Kind: ActPush},
		{Kind: ActRerequestReview, Reviewers: []string{"sam"}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pressed %+v\nwant    %+v", got, want)
	}
	// AN ACT THAT CANNOT BE CARRIED OUT AS WRITTEN IS SENT BACK BEFORE THE
	// DOOR: nothing is pressed for a set with one bad act in it.
	for args, want := range map[string]string{
		`{"acts":[],"why":"x"}`:                                                                          "at least one act",
		`{"acts":[{"kind":"push"}]}`:                                                                     "one line of why",
		`{"acts":[{"kind":"force_push"}],"why":"x"}`:                                                     "act 1: unknown kind force_push",
		`{"acts":[{"kind":"push"},{"kind":"reply","text":"hi"}],"why":"x"}`:                              "act 2: reply needs thread and text",
		`{"acts":[{"kind":"comment"}],"why":"x"}`:                                                        "comment needs text",
		`{"acts":[{"kind":"change_base"}],"why":"x"}`:                                                    "change_base needs base",
		`{"acts":[{"kind":"edit_pr"}],"why":"x"}`:                                                        "edit_pr needs a title, a body or both",
		`{"acts":[{"kind":"resolve_thread"}],"why":"x"}`:                                                 "resolve_thread needs thread",
		`{"acts":[` + strings.Repeat(`{"kind":"push"},`, PublicActsMost) + `{"kind":"push"}],"why":"x"}`: "at most 20 acts",
	} {
		out, bad, err := a.factoryActTool().Execute(context.Background(), json.RawMessage(args))
		if err != nil || !bad || !strings.Contains(out, want) || len(d.pressed) != 1 {
			t.Errorf("%s answered %q bad=%v err=%v pressed=%d, want %q", args, out, bad, err, len(d.pressed), want)
		}
	}
	// THE DESCRIPTION SAYS THE RULES A JOB MADE BEFORE THEM NEVER READ IN ITS
	// BRIEF: every kind, both modes, whose name a reply is posted under, that a
	// merge is never carried out, and the untrusted commenter.
	for _, want := range append([]string{
		"In Manual the acts wait for the person's approval and nothing is sent until then",
		"show them the text that will be posted",
		"In Auto the factory carries them out and logs each one",
		"under the person's own GitHub name", "a factory agent posted it",
		"merge never merges", "marked untrusted", "only with the person's word",
		"what was accepted or refused and why",
	}, publicActKinds...) {
		if !strings.Contains(factoryActDescription, want) {
			t.Errorf("factory_act's description does not say %q", want)
		}
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(factoryActSchemaJSON()), &schema); err != nil {
		t.Fatalf("factory_act's schema is not JSON: %v", err)
	}
	for _, kind := range publicActKinds {
		if !strings.Contains(factoryActSchemaJSON(), `"`+kind+`"`) {
			t.Errorf("the schema does not offer %s", kind)
		}
	}
}

// `factory_task` HANDS THE INSTRUCTIONS TO THE DOOR, waiting for the round in
// progress unless the call says now.
func TestFactoryTaskPressesTheInstructions(t *testing.T) {
	d := &prDecisionDoor{}
	a := &Agent{config: Config{FactoryRun: d}}
	out, bad, err := a.factoryTaskTool().Execute(context.Background(), json.RawMessage(`{"instructions":"  Rebase on main and fix the conflict in ledger.go.\nRun the ledger tests. "}`))
	if err != nil || bad || out == "" || len(d.pressed) != 1 {
		t.Fatalf("task = %q bad=%v err=%v", out, bad, err)
	}
	if got, want := d.pressed[0], (ManagerPress{Action: ActionTask, Words: "Rebase on main and fix the conflict in ledger.go.\nRun the ledger tests."}); !reflect.DeepEqual(got, want) {
		t.Fatalf("pressed %+v, want %+v", got, want)
	}
	if _, bad, _ = a.factoryTaskTool().Execute(context.Background(), json.RawMessage(`{"instructions":"do it","now":true}`)); bad || !d.pressed[1].Now {
		t.Fatalf("now was not carried: %+v", d.pressed[1])
	}
	if out, bad, _ = a.factoryTaskTool().Execute(context.Background(), json.RawMessage(`{"instructions":"   "}`)); !bad || !strings.Contains(out, "needs instructions") || len(d.pressed) != 2 {
		t.Fatalf("a task with no instructions: %q bad=%v pressed=%d", out, bad, len(d.pressed))
	}
	for _, want := range []string{"work no step covers", "rebase or merge the base branch in", "the checks that had passed run again", "waiting for the person's yes in Manual", "now=true interrupts"} {
		if !strings.Contains(factoryTaskDescription, want) {
			t.Errorf("factory_task's description does not say %q", want)
		}
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(factoryTaskSchemaJSON), &schema); err != nil {
		t.Fatalf("factory_task's schema is not JSON: %v", err)
	}
}

// A REFUSAL IS THE FACTORY'S OWN SENTENCE, handed back for the person to be
// told plainly; an outcome nobody confirmed is said as unconfirmed.
func TestAPressRefusedIsReadBackInTheFactorysWords(t *testing.T) {
	d := &prDecisionDoor{refuse: errors.New("Nothing changed: the job's budget is reached")}
	a := &Agent{config: Config{FactoryRun: d}}
	out, bad, _ := a.factoryTaskTool().Execute(context.Background(), json.RawMessage(`{"instructions":"do it"}`))
	if !bad || !strings.HasPrefix(out, "Nothing changed. Tell the person plainly") || !strings.HasSuffix(out, "The factory said: Nothing changed: the job's budget is reached") {
		t.Fatalf("a refusal read back as %q", out)
	}
	d.refuse = errors.New("the factory did not answer")
	out, bad, _ = a.factoryActTool().Execute(context.Background(), json.RawMessage(`{"acts":[{"kind":"push"}],"why":"x"}`))
	if !bad || !strings.HasPrefix(out, "Not confirmed.") || !strings.Contains(out, "read factory_status before trying again") {
		t.Fatalf("an unconfirmed press read back as %q", out)
	}
}

// THE DECISIONS ON A STOP ARE `factory_answer`'S ACTIONS: each reaches the
// door as itself with the manager's words, the step when it names one, and
// the person's own words of the turn. The ones that record a reason or carry
// instructions are refused without words, and none is also sent to the person.
func TestFactoryAnswerPressesTheDecisionsOnAStop(t *testing.T) {
	d := &prDecisionDoor{}
	a := &Agent{config: Config{FactoryRun: d}}
	for action, needs := range stopDecisions {
		before := len(d.pressed)
		out, bad, err := a.factoryAnswerTool().Execute(context.Background(), json.RawMessage(`{"action":"`+action+`","step":" test ","answer":"  run the ledger tests first  "}`))
		if err != nil || bad || len(d.pressed) != before+1 {
			t.Fatalf("%s: %q bad=%v err=%v", action, out, bad, err)
		}
		if got, want := d.pressed[before], (ManagerPress{Action: action, Step: "test", Words: "run the ledger tests first"}); !reflect.DeepEqual(got, want) {
			t.Errorf("%s pressed %+v, want %+v", action, got, want)
		}
		out, bad, _ = a.factoryAnswerTool().Execute(context.Background(), json.RawMessage(`{"action":"`+action+`"}`))
		if needs && (!bad || !strings.Contains(out, action+" needs your instructions or reason") || len(d.pressed) != before+1) {
			t.Errorf("%s with no words: %q bad=%v", action, out, bad)
		}
		if !needs && (bad || len(d.pressed) != before+2) {
			t.Errorf("%s needs no words and was refused: %q", action, out)
		}
		pressed := len(d.pressed)
		if out, bad, _ = a.factoryAnswerTool().Execute(context.Background(), json.RawMessage(`{"action":"`+action+`","answer":"a","ask_person":"b"}`)); !bad || len(d.pressed) != pressed {
			t.Errorf("%s with ask_person was pressed: %q", action, out)
		}
		if !strings.Contains(factoryAnswerSchemaJSON, `"`+action+`"`) || !strings.Contains(factoryAnswerDescription, action) {
			t.Errorf("the schema or the description does not offer %s", action)
		}
	}
	a.personWords = "yes, post them"
	if _, bad, _ := a.factoryAnswerTool().Execute(context.Background(), json.RawMessage(`{"action":"approve_acts","answer":"the person said yes"}`)); bad || d.said != "yes, post them" {
		t.Fatalf("approve_acts did not carry the person's own words: %+v", d)
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(factoryAnswerSchemaJSON), &schema); err != nil {
		t.Fatalf("factory_answer's schema is not JSON: %v", err)
	}
	// THE DESCRIPTION SAYS WHAT IS HELD IN CODE, for a job whose brief was
	// written before it.
	for _, want := range []string{
		"merge is pressed only on a turn where the person told you to merge, in Manual and in Auto",
		"In Manual, approve, mark_ready and approve_acts are pressed only on a turn where the person said so in their own words; in Auto you may press them yourself",
		"the stop goes to the person with one plain sentence of why, and it is always allowed",
		"when a step ran out of rounds", "when a step failed or its results were not saved", "when Follow PR stopped (its limit, a scope question, a conflict, a reviewer asking for changes)",
		"when a publish the person already approved failed",
	} {
		if !strings.Contains(factoryAnswerDescription, want) {
			t.Errorf("factory_answer's description does not say %q", want)
		}
	}
}

// AN INBOX TURN ANSWERS ONE QUESTION AND PRESSES NOTHING. The door the runner
// lends that turn cannot press, so every pressing tool refuses on it in a
// plain sentence while the conversation's own door stays untouched.
func TestThePressingToolsRefuseOnATurnWhoseLentDoorCannotPress(t *testing.T) {
	own := &prDecisionDoor{}
	a := &Agent{config: Config{Workspace: t.TempDir(), FactoryRun: own}}
	a.running, a.teamTurnSerial = true, 5
	turnRunDoors.Store(a, turnRunDoor{door: &fakeInboxDoor{}, serial: 5})
	t.Cleanup(func() { turnRunDoors.Delete(a) })
	calls := map[string]func() (string, bool, error){
		"factory_act": func() (string, bool, error) {
			return a.factoryActTool().Execute(context.Background(), json.RawMessage(`{"acts":[{"kind":"push"}],"why":"x"}`))
		},
		"factory_task": func() (string, bool, error) {
			return a.factoryTaskTool().Execute(context.Background(), json.RawMessage(`{"instructions":"do it"}`))
		},
		"another_round": func() (string, bool, error) {
			return a.factoryAnswerTool().Execute(context.Background(), json.RawMessage(`{"action":"another_round","answer":"again"}`))
		},
	}
	for name, call := range calls {
		out, bad, err := call()
		if err != nil || !bad || out != managerTurnOnly || len(own.pressed) != 0 {
			t.Errorf("%s on an inbox turn: %q bad=%v err=%v pressed=%d", name, out, bad, err, len(own.pressed))
		}
	}
}
