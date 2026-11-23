package claudecode

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/claudeagent"
	"github.com/Agent-Field/codeaf/internal/delegate"
)

type fakeHost struct {
	stages []delegate.StageRecord
	steps  []delegate.StepRecord
}

func (h *fakeHost) Workspace() string            { return "." }
func (h *fakeHost) Ceilings() delegate.Ceilings  { return delegate.Ceilings{} }
func (h *fakeHost) Hello([]string)               {}
func (h *fakeHost) Stage(s delegate.StageRecord) { h.stages = append(h.stages, s) }
func (h *fakeHost) Step(s delegate.StepRecord)   { h.steps = append(h.steps, s) }
func (h *fakeHost) Terminal(delegate.Ending)     {}
func (h *fakeHost) Models() delegate.ModelAPI    { return delegate.ModelAPI{} }

// The stream below is the shape Claude Code 2.1 printed for a real run, cut
// to the lines this package reads.
const stream = `{"type":"system","subtype":"hook_started"}
{"type":"system","subtype":"init","model":"claude-haiku-4-5","apiKeySource":"none"}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"echo hi > hi.txt"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Read","input":{"file_path":"hi.txt"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":[{"type":"text","text":"1\thi"}]}]}}
{"type":"result","subtype":"success","is_error":false,"result":"done","total_cost_usd":0.007,"num_turns":3}
`

func TestAPlanRunBecomesStepsAndAnEndingThatBooksNoSpend(t *testing.T) {
	host := &fakeHost{}
	end := translate(stream, host)
	if end.Status != delegate.StatusPass || end.Deliverable != "done" {
		t.Fatalf("ending = %+v", end)
	}
	if end.CostUSD != 0 {
		t.Fatalf("a Claude plan run booked $%v of spend", end.CostUSD)
	}
	if len(host.stages) != 1 || len(host.steps) != 2 {
		t.Fatalf("stages %d steps %d", len(host.stages), len(host.steps))
	}
	if host.steps[0].Command != "echo hi > hi.txt" || *host.steps[0].Exit != 0 || host.steps[1].Observation != "1\thi" {
		t.Fatalf("steps = %+v", host.steps)
	}
}

func TestAnAPIKeyRunBooksClaudeCodesOwnFigure(t *testing.T) {
	keyed := strings.Replace(stream, `"apiKeySource":"none"`, `"apiKeySource":"ANTHROPIC_API_KEY"`, 1)
	if end := translate(keyed, &fakeHost{}); end.CostUSD != 0.007 {
		t.Fatalf("cost = %v", end.CostUSD)
	}
}

func translate(stream string, host *fakeHost) delegate.Ending {
	tr := newTranslator(host)
	for _, line := range strings.Split(strings.TrimSpace(stream), "\n") {
		tr.handle(claudeagent.Event{Raw: []byte(line)})
	}
	return tr.ending("s1")
}

func TestAPushIsRefusedAndWorkOnTheBranchIsNot(t *testing.T) {
	for input, allow := range map[string]bool{
		`{"command":"git push origin HEAD"}`: false, `{"command":"gh pr create --fill"}`: false,
		`{"command":"npm publish"}`: false, `{"command":"go test ./..."}`: true, `{"command":"git commit -m x"}`: true,
	} {
		if got := policy(claudeagent.PermissionRequest{ToolName: "Bash", Input: []byte(input)}); got.Allow != allow {
			t.Errorf("%s: allow = %v", input, got.Allow)
		}
	}
}

func TestACrewSeatOnAVersionStartsThatVersion(t *testing.T) {
	// A worker pinned to a version row hands its tasks to Claude Code, and the
	// task is owed the same version the conversation would have run.
	for seat, want := range map[string]string{
		"claude-code/claude-haiku-5-5": "claude-haiku-5-5",
		"claude-code/haiku":            "haiku",
		"anthropic/claude-opus-4.5":    "opus",
	} {
		got := crewFlags(delegate.Crew{Hands: seat})
		if len(got) != 2 || got[0] != "--claude-model" || got[1] != want {
			t.Errorf("crewFlags(%q) = %v, want --claude-model %s", seat, got, want)
		}
	}
	if got := crewFlags(delegate.Crew{Hands: "openai/gpt-5"}); got != nil {
		t.Errorf("a seat that is not Claude gave %v", got)
	}
}

func TestTheCrewSeatIsHeardOnlyWhenItIsClaude(t *testing.T) {
	for model, want := range map[string]string{
		"anthropic/claude-opus-4.5": "opus", "claude-sonnet-4-5": "sonnet", "openai/gpt-5": "", "vendor/hands": "",
	} {
		if got := Family(model); got != want {
			t.Errorf("Family(%q) = %q, want %q", model, got, want)
		}
	}
	if err := Program.Validate(); err != nil {
		t.Fatal(err)
	}
}
