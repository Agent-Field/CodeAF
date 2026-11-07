package pipeline

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

const sweAF = "pipeline:\n  issue: [build]\nsteps:\n  build:\n    label: Build with SWE-AF\n    kind: agentfield\n    target: swe-planner.build_issue\n" +
	"    input:\n      title: \"{title}\"\n      body: \"{body}\"\n      repo_url: \"{repo_url}\"\n      issue_number: \"{issue_number}\"\n"

// An agentfield step names its reasoner and maps the job into its input; the
// recipe carries both, and the file writes them back as it read them.
func TestAnAgentFieldStepParsesAndRoundTrips(t *testing.T) {
	f := parseClean(t, sweAF)
	st := f.Recipe().Stages[0]
	if st.Kind != factory.StageAgentField || st.Target != "swe-planner.build_issue" || st.Input["repo_url"] != "{repo_url}" || st.Input["issue_number"] != "{issue_number}" || st.Label != "Build with SWE-AF" {
		t.Fatalf("stage = %+v", st)
	}
	data, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"kind: agentfield", "swe-planner.build_issue", "input:", "{repo_url}"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("encoded file lost %q:\n%s", want, data)
		}
	}
	back := parseClean(t, string(data))
	if !sameStep(back.Steps["build"], f.Steps["build"]) {
		t.Fatalf("round trip changed the step: %+v / %+v", back.Steps["build"], f.Steps["build"])
	}
}

// An input value that names a fact the job does not have is a problem the
// author reads, and input on any other kind of step is one too.
func TestAnAgentFieldInputNamesOnlyTheJobsFacts(t *testing.T) {
	for name, src := range map[string]string{
		"unknown fact":         strings.Replace(sweAF, "{body}", "{branch}", 1),
		"input on a chat step": "steps:\n  write:\n    prompt: do it\n    input:\n      x: \"{title}\"\n",
	} {
		f, err := Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		if len(f.Problems) == 0 {
			t.Errorf("%s: no problem", name)
		}
	}
}

// The desk's "Run on AgentField" and Change with AI add one step the same way:
// an add_step of kind agentfield, its prompt the reasoner, its input the job's
// facts. The file then runs it; a bad reasoner, a fact the job does not have,
// a model's word, or input on another kind is refused.
func TestAnAgentFieldStepIsAddedByOneEdit(t *testing.T) {
	kind := "agentfield"
	op := add("issue", "swe", "", "swe-planner.build_issue")
	op.Kind = &kind
	op.Input = map[string]string{"title": "{title}", "issue_number": "{issue_number}"}
	got := mustApply(t, "pipeline:\n  issue: [plan]\n", op)
	f := reread(t, got.Text)
	var st factory.Stage
	for _, s := range f.Recipe().Stages {
		if s.Name == "swe" {
			st = s
		}
	}
	if st.Kind != factory.StageAgentField || st.Target != "swe-planner.build_issue" || st.Input["issue_number"] != "{issue_number}" {
		t.Fatalf("added %+v from\n%s", st, got.Text)
	}
	until := "done"
	for name, bad := range map[string]func(*EditOp){
		"no reasoner":       func(o *EditOp) { o.Prompt = nil },
		"not node.reasoner": func(o *EditOp) { p := "build issue"; o.Prompt = &p },
		"an unknown fact":   func(o *EditOp) { o.Input = map[string]string{"x": "{branch}"} },
		"a model's word":    func(o *EditOp) { o.Until = &until },
		"input on a chat":   func(o *EditOp) { o.Kind = nil },
	} {
		o := op
		bad(&o)
		if _, err := ApplyEdits("pipeline:\n  issue: [plan]\n", []EditOp{o}); err == nil {
			t.Errorf("%s was taken", name)
		}
	}
}
