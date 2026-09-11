package session

// standing_grant_test.go holds the wave-4 law for unattended work's belt: a tool
// the approval policy does not grant is ABSENT from a firing's belt, not
// present and refused ([Config.grants]). Three of ten live runs on 2026-09-10
// (live8 run05, run07, run08) and validator S09c held a finished report back
// as "waiting on you" because the run called `commit` or `bash`, was refused
// "nobody to ask", and the refusal read as a question for the person.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// shippedFloorForTest is the approval floor cmd/aforge's v3BuiltinApprovals
// seeds under the shipped prompt default, spelled here because this package
// cannot import the door. A change to that floor is a change to what unattended
// work carries, and belongs in both places.
func shippedFloorForTest() *approval.Policy {
	return &approval.Policy{Default: approval.ActionPrompt, Tools: map[string]approval.Action{
		"read": approval.ActionAllow, "grep": approval.ActionAllow, "find": approval.ActionAllow, "ls": approval.ActionAllow,
		"jobs": approval.ActionAllow, "remember": approval.ActionAllow, "track": approval.ActionAllow, "recall": approval.ActionAllow,
		"manual": approval.ActionAllow, "settings": approval.ActionAllow,
	}}
}

// callingTwo is one response that calls two tools at once, as the live runs'
// resumed pass did before it replied with its report.
func callingTwo(first, firstArgs, second, secondArgs string) step {
	return func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		response := toolResponse("call_a", first, firstArgs)
		response.Choices[0].Message.ToolCalls = append(response.Choices[0].Message.ToolCalls, ai.ToolCall{
			ID: "call_b", Type: "function", Function: ai.ToolCallFunction{Name: second, Arguments: secondArgs},
		})
		return response, nil
	}
}

// grantedRunner is a firing's runner under a policy, keeping the child agent
// it built so the test can read the belt the run actually carried.
func grantedRunner(t *testing.T, root string, model Completer, policy *approval.Policy) (*standingRunner, **Agent) {
	t.Helper()
	runner := standingChildRunner(t, root, model)
	runner.parent.ApprovalPolicy = policy
	var built *Agent
	child := runner.child
	runner.child = func(cfg Config) (*Agent, error) {
		agent, err := child(cfg)
		built = agent
		return agent, err
	}
	return runner, &built
}

// THE LIVE DEFECT, SCRIPTED. The resumed pass read, then called `commit` on its
// own working state and `bash` to look around, then replied with a complete
// report. Neither tool is granted by the shipped floor, so neither is on the
// belt; the calls are answered as tools the run does not have, and the report
// publishes. On the old logic both were refused "nobody to ask" and the run
// came to needs-you with its report withheld.
func TestAnUnattendedRunCarriesOnlyWhatItIsGrantedAndItsReportPublishes(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	body := "# Inbox\n- Decision: ship Friday"
	model := &scriptedCompleter{steps: []step{
		callingTwo("commit", `{"id":"b1"}`, "bash", `{"command":"ls inbox"}`),
		saying("<report>\n" + body + "\n</report>"),
	}}
	runner, built := grantedRunner(t, root, model, shippedFloorForTest())
	outcome, err := runner.Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != "landed" || outcome.NeedsPerson != "" || outcome.Withheld != "" || outcome.Published == nil {
		t.Fatalf("a run whose ungranted calls failed did not publish: %+v", outcome)
	}
	raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if string(raw) != body+"\n" {
		t.Fatalf("published %q", raw)
	}
	carried := beltNameSet((*built).beltTools())
	for _, absent := range []string{"commit", "bash", "write", "edit", "ask"} {
		if carried[absent] {
			t.Errorf("the unattended belt carries %s, which the floor does not grant", absent)
		}
	}
	for _, present := range []string{"read", "grep", "find", "ls", "track", "recall", "manual"} {
		if !carried[present] {
			t.Errorf("the unattended belt lost %s, which the floor grants", present)
		}
	}
	if calls := model.seen; len(calls) < 2 || strings.Contains(messageText(calls[1][len(calls[1])-1]), "nobody to ask") {
		t.Fatalf("an ungranted call was refused as a question rather than absent")
	}
}

// A GRANTED TOOL THAT MEETS A REAL BOUNDARY IS STILL A QUESTION. The person
// allowed `cat *` in the shell and nothing else, so `bash` is carried; a
// command outside that pattern is refused with nobody to ask, and the run waits
// on the person, truthfully, with its report held.
func TestAGrantedToolMeetingItsBoundaryStillWaitsOnThePerson(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	policy := shippedFloorForTest()
	policy.BashPatterns = []approval.Rule{{Match: "cat *", Action: approval.ActionAllow}}
	model := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			return toolResponse("call_rm", "bash", `{"command":"rm -f inbox/a.md"}`), nil
		},
		saying("<report>\n# Inbox\n</report>"),
	}}
	runner, built := grantedRunner(t, root, model, policy)
	outcome, err := runner.Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	if !beltNameSet((*built).beltTools())["bash"] {
		t.Fatal("an allow pattern did not put the shell on the belt")
	}
	if outcome.Kind != standing.OutcomeNeedsYou || !strings.Contains(outcome.NeedsPerson, "nobody to ask") || outcome.Withheld != "waiting-on-person" {
		t.Fatalf("a granted tool refused at its boundary did not wait on the person: %+v", outcome)
	}
}

// A WRITE OF THE RUN'S OWN REPORT IS STILL THE RUN TRYING TO PUBLISH ITSELF when
// `write` is not on its belt at all: answered as a tool it does not have, and
// an apology after it is not a report (review 425's first blocker, which an
// absent writer must not reopen).
func TestAnAbsentWriteOfItsOwnReportWithNoReportPublishesNothing(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	args, _ := json.Marshal(map[string]string{"path": "reports/r.md", "content": "my own copy"})
	model := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "Writing it now.")
			return toolResponseWithText("call_w", "write", string(args), "Writing it now."), nil
		},
		saying("I could not write the file."),
	}}
	runner, _ := grantedRunner(t, root, model, shippedFloorForTest())
	outcome, err := runner.Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "reports", "r.md")); !os.IsNotExist(err) {
		t.Fatalf("something was published: %v", err)
	}
	if outcome.Kind != standing.OutcomeFailed || outcome.Withheld != "self-write" {
		t.Fatalf("an absent write of the report published or asked: %+v", outcome)
	}
}
