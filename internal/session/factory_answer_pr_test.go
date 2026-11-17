package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type prDecisionDoor struct {
	savedManagerDoor
	action, words string
}

func (d *prDecisionDoor) DecidePR(_ context.Context, _, action, words string) (string, error) {
	d.action, d.words = action, words
	return "Merge pressed for pull request #161", nil
}

// "MERGE IT" IN THE MANAGER'S CHAT PRESSES THE JOB PAGE'S MERGE. On the live
// audit the manager had no verb for the post-PR card and said nothing was
// waiting; factory_answer's merge and mark_ready reach the door the page's
// buttons take, with the person's words, and nothing else answers for them.
func TestFactoryAnswerMergeAndMarkReadyReachThePRDecisionDoor(t *testing.T) {
	for _, action := range []string{ActionMerge, ActionMarkReady, ActionApprove} {
		d := &prDecisionDoor{}
		a := &Agent{config: Config{FactoryRun: d}}
		out, bad, err := a.factoryAnswerTool().Execute(context.Background(), json.RawMessage(`{"action":"`+action+`","answer":"merge it"}`))
		if err != nil || bad || !strings.Contains(out, "#161") || d.action != action || d.words != "merge it" || d.answers != 0 {
			t.Fatalf("%s: %q bad=%v err=%v door=%+v", action, out, bad, err, d)
		}
	}
	// A door that cannot press is told so in words, and nothing is answered.
	d := &savedManagerDoor{}
	a := &Agent{config: Config{FactoryRun: d}}
	out, bad, _ := a.factoryAnswerTool().Execute(context.Background(), json.RawMessage(`{"action":"merge","answer":"merge it"}`))
	if !bad || !strings.Contains(out, "Nothing changed") || d.answers != 0 {
		t.Fatalf("a door with no press: %q bad=%v answers=%d", out, bad, d.answers)
	}
	if !strings.Contains(factoryAnswerSchemaJSON, `"merge","mark_ready","approve"`) {
		t.Fatal("the schema does not offer merge and mark_ready")
	}
}
