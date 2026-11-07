package claudecode

import (
	"strings"
	"testing"

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
	end := Translate(strings.NewReader(stream), host)
	if !end.seen || end.Status != delegate.StatusPass || end.Deliverable != "done" {
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
	if end := Translate(strings.NewReader(keyed), &fakeHost{}); end.CostUSD != 0.007 {
		t.Fatalf("cost = %v", end.CostUSD)
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
