package session

// A factory stage's two verbs (tools_stage.go), held to their laws: absent
// without the door, the arguments read into the result and the edit the door
// is handed, the sentences the model reads back, the door's refusal said back,
// and a belt that costs every other conversation nothing.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/manual"
)

// THE TWO DOORS ARE ONE SHAPE. The runner builds a run.StageDoor and the
// wiring hands it to Config.Stage; this line stops compiling the day either
// spelling drifts.
var _ StageDoor = run.StageDoor(nil)

// fakeStageDoor keeps what it is handed, or refuses with refuse.
type fakeStageDoor struct {
	mu      sync.Mutex
	reports []factory.StageResult
	edits   []factory.PlanEdit
	refuse  error
}

func (d *fakeStageDoor) Report(_ context.Context, r factory.StageResult) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.refuse != nil {
		return d.refuse
	}
	d.reports = append(d.reports, r)
	return nil
}

func (d *fakeStageDoor) Edit(_ context.Context, e factory.PlanEdit) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.refuse != nil {
		return d.refuse
	}
	d.edits = append(d.edits, e)
	return nil
}

func stageAgent(t *testing.T, door StageDoor) *Agent {
	t.Helper()
	a := &Agent{config: Config{Workspace: t.TempDir()}}
	if door != nil {
		a.config.Stage = door
	}
	a.tools = a.belt()
	return a
}

func stageToolNamed(t *testing.T, a *Agent, name string) bare.Tool {
	t.Helper()
	for _, tool := range a.offeredTools() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("%s is not on the belt", name)
	return bare.Tool{}
}

func callStageTool(t *testing.T, tool bare.Tool, args string) (string, bool) {
	t.Helper()
	out, isErr, err := tool.Execute(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return out, isErr
}

func TestTheStageVerbsAreOnTheBeltOnlyWithTheDoor(t *testing.T) {
	for _, tool := range stageAgent(t, nil).offeredTools() {
		if tool.Name == "stage_result" || tool.Name == "plan_edit" {
			t.Fatalf("a conversation that is not a stage carries %s", tool.Name)
		}
	}
	a := stageAgent(t, &fakeStageDoor{})
	stageToolNamed(t, a, "stage_result")
	stageToolNamed(t, a, "plan_edit")
	// AND THE SHIPPED CONVERSATION IS NOT A STAGE: no door sets Stage there.
	if shipped := shippedShapeAgent(t); shipped.offers("stage_result") || shipped.offers("plan_edit") {
		t.Fatal("the shipped conversation carries a stage verb")
	}
}

func TestStageResultHandsTheDoorWhatTheModelReported(t *testing.T) {
	door := &fakeStageDoor{}
	tool := stageToolNamed(t, stageAgent(t, door), "stage_result")
	out, isErr := callStageTool(t, tool, `{"done":true,"findings":1,
		"claims":[{"text":"  refunds   count once ","ok":true,"evidence":"TestRefund passes","medium":" Test "}],
		"notes":["the export changed",""],"output":"  one finding left  "}`)
	if isErr || out != "reported · the stage ends when you stop" {
		t.Fatalf("result = %q (error %v)", out, isErr)
	}
	want := factory.StageResult{Done: true, Findings: 1, Notes: []string{"the export changed"}, Output: "one finding left",
		Claims: []factory.Claim{{Text: "refunds count once", OK: true, Evidence: "TestRefund passes", Medium: "test"}}}
	got := door.reports[0]
	if got.Done != want.Done || got.Findings != want.Findings || got.Output != want.Output ||
		len(got.Notes) != 1 || got.Notes[0] != want.Notes[0] || len(got.Claims) != 1 || got.Claims[0] != want.Claims[0] {
		t.Fatalf("door got %+v, want %+v", got, want)
	}
}

func TestStageResultSaysWhatIsMissing(t *testing.T) {
	door := &fakeStageDoor{}
	tool := stageToolNamed(t, stageAgent(t, door), "stage_result")
	for args, want := range map[string]string{
		`{"findings":0}`:                                  stageNeedsDone,
		`{"done":true,"findings":-1}`:                     stageNeedsFindings,
		`{"done":true,"claims":[{"text":" ","ok":true}]}`: stageNeedsClaim,
	} {
		if out, isErr := callStageTool(t, tool, args); !isErr || out != want {
			t.Errorf("%s answered %q, want %q", args, out, want)
		}
	}
	if len(door.reports) != 0 {
		t.Fatal("an invalid call reached the door")
	}
	door.refuse = errors.New("this stage already reported, and its first report stands")
	if out, isErr := callStageTool(t, tool, `{"done":false}`); !isErr || out != "not reported: this stage already reported, and its first report stands" {
		t.Fatalf("a refusal answered %q", out)
	}
}

func TestPlanEditHandsTheDoorAProposalAndChangesNothing(t *testing.T) {
	door := &fakeStageDoor{}
	tool := stageToolNamed(t, stageAgent(t, door), "plan_edit")
	out, isErr := callStageTool(t, tool, `{"add":["after review, read it for auth holes"],"skip":[" neaten "],"on":["security"],"why":"touches  billing"}`)
	if isErr || out != "proposed · the runner applies it within the recipe's bounds" {
		t.Fatalf("result = %q", out)
	}
	e := door.edits[0]
	if len(e.Add) != 1 || e.Add[0].Ask != "after review, read it for auth holes" || len(e.Skip) != 1 || e.Skip[0] != "neaten" ||
		len(e.On) != 1 || e.On[0] != "security" || e.Why != "touches billing" {
		t.Fatalf("edit = %+v", e)
	}
	if out, isErr := callStageTool(t, tool, `{"why":"nothing"}`); !isErr || out != planNeedsChange {
		t.Fatalf("an empty proposal answered %q", out)
	}
	if out, isErr := callStageTool(t, tool, `{"add":["after test,"]}`); !isErr || !strings.Contains(out, "needs to say what it does") {
		t.Fatalf("a stage with no ask answered %q", out)
	}
	door.refuse = errors.New("the proposal changes nothing")
	if out, isErr := callStageTool(t, tool, `{"skip":["neaten"]}`); !isErr || out != "not proposed: the proposal changes nothing" {
		t.Fatalf("a refusal answered %q", out)
	}
}

// THE DESCRIPTIONS SAY WHAT A STAGE IS, because the model has nothing else to
// learn it from: no sentence on the page names these verbs.
func TestTheStageDescriptionsSayWhatAStageIsAndHowItEnds(t *testing.T) {
	for _, want := range []string{"IS one stage of a factory item", "its brief", "tasks", "ONCE", "until clean needs zero", "Nothing is posted anywhere", "ends when you stop"} {
		if !strings.Contains(stageResultDescription, want) {
			t.Errorf("stage_result's description lacks %q", want)
		}
	}
	for _, want := range []string{"Nothing changes by calling this", "recipe's bounds", "never posts"} {
		if !strings.Contains(planEditDescription, want) {
			t.Errorf("plan_edit's description lacks %q", want)
		}
	}
	for _, d := range []string{stageResultDescription, planEditDescription} {
		if strings.Contains(d, "—") {
			t.Error("a description carries an em dash")
		}
	}
}

// stageBeltBudget is what the two verbs cost a stage conversation on every
// request, in bytes of name, description and schema. It is measured, with a
// little room, and only ever comes down.
const stageBeltBudget = 2_500

// THE PROMPT FRAGMENT IS THE TWO TOOLS AND NOTHING ON THE PAGE. The page had
// four bytes of headroom against its budget (prefixbudget_test.go) when this
// lane arrived, so a stage is told what it is by its brief and by these
// descriptions, and the page every other conversation reads is unchanged.
func TestTheStageVerbsCostOnlyTheStageAndStayUnderTheirBudget(t *testing.T) {
	a := stageAgent(t, &fakeStageDoor{})
	total := 0
	for _, name := range []string{"stage_result", "plan_edit"} {
		tool := stageToolNamed(t, a, name)
		total += len(tool.Name) + len(tool.Description) + len(tool.Schema)
		if !json.Valid(tool.Schema) {
			t.Errorf("%s's schema is not JSON", name)
		}
	}
	t.Logf("the stage verbs are %d bytes", total)
	if total > stageBeltBudget {
		t.Fatalf("the stage verbs are %d bytes, over their %d", total, stageBeltBudget)
	}
	plain := Config{Workspace: "/w"}
	stage := plain
	stage.Stage = &fakeStageDoor{}
	if promptWithBeltFacts(plain) != promptWithBeltFacts(stage) {
		t.Fatal("being a stage changed the page: the page is paid for by every conversation")
	}
}

func TestTheStageVerbsHaveAFamilyAGlossAndAPageInTheManual(t *testing.T) {
	for _, name := range []string{"stage_result", "plan_edit"} {
		if got := ActionCategoryForTool(name); got != ActionPlan {
			t.Errorf("%s is in the %s family", name, got)
		}
		if !manual.Chat().Mentions(name) {
			t.Errorf("the manual never names %s", name)
		}
	}
	if glossField["stage_result"] != "output" || glossField["plan_edit"] != "why" {
		t.Fatal("the stage verbs are glossed by the wrong field")
	}
}
