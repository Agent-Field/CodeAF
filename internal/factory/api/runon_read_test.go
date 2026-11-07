package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A job an AgentField reasoner runs is filed with the factory's default
// pipeline but never runs it: until its run reports a tree it reads as plain
// Backlog with no steps, and once the run's steps are on the job they are what
// the rail draws and counts.
func TestARunOnJobReadsAsBacklogWithNoStepsUntilItsRunBegins(t *testing.T) {
	it := factory.Item{Num: 7, Kind: factory.KindIssue, Title: "ledger total wrong", State: factory.StateNew,
		RunOn:  "support.handle_ticket",
		Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}, {Name: "approve", Kind: factory.StageGate, On: true}}}
	j := FromItem(it)
	PresentJob(&j)
	if j.StatePresentation.Title != "Backlog" || j.StatePresentation.Detail != "" {
		t.Fatalf("a run_on job waiting to start reads %q / %q, want plain Backlog", j.StatePresentation.Title, j.StatePresentation.Detail)
	}
	if len(j.Steps) != 0 || j.StepsTotal != 0 {
		t.Fatalf("a run_on job not yet run shows %d steps (%d counted); its steps are the run's", len(j.Steps), j.StepsTotal)
	}

	it.AgentField = &factory.AgentFieldRun{ExecutionID: "exec_1", Status: "running"}
	it.Stages = []factory.Stage{{Name: "triage", Kind: factory.StageChat, On: true}, {Name: "investigate", Kind: factory.StageChat, On: true},
		{Name: "searchdocs", Kind: factory.StageChat, On: true, Parent: "investigate"}}
	j = FromItem(it)
	// The nested call is on the job, naming its parent, so the rail can fold
	// it; it is not counted as a step of its own.
	if len(j.Steps) != 3 || j.Steps[0].Name != "triage" || j.Steps[2].Parent != "investigate" || j.StepsTotal != 2 {
		t.Fatalf("once the run began the job reads %d steps (%d counted), want the run's three with two counted", len(j.Steps), j.StepsTotal)
	}

	// Its model is the node's, which this factory does not know.
	ModelsIn(&j, it, factory.ModelDefaults{Factory: "vendor/strong"})
	if j.RunsOn != nil || j.Steps[1].RunsOn != nil {
		t.Fatalf("a run_on job names the factory's model: job %+v, step %+v", j.RunsOn, j.Steps[1].RunsOn)
	}

	plain := factory.Item{Num: 8, Kind: factory.KindIssue, State: factory.StateNew}
	j = FromItem(plain)
	PresentJob(&j)
	if j.StatePresentation.Title != factory.NoRepoTitle {
		t.Fatalf("outside work with no repository reads %q, want %q", j.StatePresentation.Title, factory.NoRepoTitle)
	}
}

// The ordinary job read says where an AgentField job came from: source,
// origin and labels, on the wire under those names, and none of them on a job
// that has nothing to say.
func TestAnAgentFieldJobsReadCarriesItsSourceOriginAndLabels(t *testing.T) {
	it := factory.Item{Num: 9, Kind: factory.KindIssue, Title: "refund failed", State: factory.StateNew,
		RunOn: "support.triage", Origin: "agentfield:generic-bearer", SourceRef: "agentfield:generic-bearer:T-9",
		AgentField: &factory.AgentFieldRun{RunID: "run_9", URL: "https://cp/ui/runs/run_9"}, Labels: []string{"Billing"}}
	raw, err := json.Marshal(FromItem(it))
	if err != nil {
		t.Fatal(err)
	}
	var read struct {
		Source *factory.JobSourceFacts `json:"source"`
		Origin *factory.JobOrigin      `json:"origin"`
		Labels []string                `json:"labels"`
	}
	_ = json.Unmarshal(raw, &read)
	if read.Source == nil || *read.Source != (factory.JobSourceFacts{Name: "agentfield", Ref: "support.triage", URL: "https://cp/ui/runs/run_9"}) {
		t.Fatalf("source %+v", read.Source)
	}
	if read.Origin == nil || *read.Origin != (factory.JobOrigin{Node: "support", Reasoner: "triage", Trigger: "generic-bearer"}) {
		t.Fatalf("origin %+v", read.Origin)
	}
	if strings.Join(read.Labels, ",") != "node:support,billing" {
		t.Fatalf("labels %v", read.Labels)
	}

	raw, _ = json.Marshal(FromItem(factory.Item{Num: 10, Kind: factory.KindIssue, State: factory.StateNew}))
	var top map[string]json.RawMessage
	_ = json.Unmarshal(raw, &top)
	if _, ok := top["origin"]; ok {
		t.Fatalf("a plain job reads an origin: %s", top["origin"])
	}
	if _, ok := top["labels"]; ok {
		t.Fatalf("a job with no labels reads labels: %s", top["labels"])
	}
}

// AN AGENTFIELD JOB RUNS ON ITS NODE'S MACHINE. The job's "runs on" fact is
// the node, with the machine the node named when the run began — never this
// factory's own machine, which the desk would otherwise fall back to.
func TestAnAgentFieldJobRunsOnItsNodeNotThisMachine(t *testing.T) {
	runner := func(it factory.Item) string {
		raw, _ := json.Marshal(FromItem(it))
		var read struct {
			Runner string `json:"runner"`
		}
		_ = json.Unmarshal(raw, &read)
		return read.Runner
	}
	base := factory.Item{Num: 9, Kind: factory.KindIssue, State: factory.StateNew, RunOn: "support.handle_ticket", WorktreeMachine: "mac-studio"}
	if got := runner(base); got != "support" {
		t.Fatalf("before the run, runner %q", got)
	}
	base.AgentField = &factory.AgentFieldRun{RunID: "r", Host: "dumb"}
	if got := runner(base); got != "support · dumb" {
		t.Fatalf("with the node's host, runner %q", got)
	}
	if got := runner(factory.Item{Num: 10, Kind: factory.KindIssue, State: factory.StateNew, WorktreeMachine: "mac-studio"}); got != "mac-studio" {
		t.Fatalf("a native job's runner moved: %q", got)
	}
}

// A SOURCE LINK THE APP CANNOT OPEN IS NOT SENT. The app opens a source's
// link over https only; a run an older build recorded with an http page
// reads with no link rather than a chip nobody can click.
func TestAnAgentFieldJobSendsNoLinkItCannotOpen(t *testing.T) {
	it := factory.Item{Num: 9, Kind: factory.KindIssue, State: factory.StateNew, RunOn: "support.triage",
		AgentField: &factory.AgentFieldRun{RunID: "run_9", URL: "http://100.64.233.75:18741/ui/runs/run_9"}}
	raw, _ := json.Marshal(FromItem(it))
	var read struct {
		Source *factory.JobSourceFacts `json:"source"`
	}
	_ = json.Unmarshal(raw, &read)
	if read.Source == nil || read.Source.URL != "" || read.Source.Name != "agentfield" {
		t.Fatalf("source %+v", read.Source)
	}
}

// A STARTED AGENTFIELD JOB WEARS NONE OF THE STOCK PIPELINE. Between Start and
// the run's first report its stream still holds the default plan · write · …
// it was queued with; the job reads no step and no steps, never "plan".
func TestAStartedAgentFieldJobShowsNoStockStep(t *testing.T) {
	it := factory.Item{Num: 9, Kind: factory.KindIssue, State: factory.StateQueued, RunOn: "support.handle_ticket",
		Stages: factory.DefaultRecipe().ByKind[factory.KindIssue],
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhasePending}, {Name: "write", State: factory.PhasePending}}}}
	j := FromItem(it)
	if j.Step != nil || len(j.Steps) != 0 {
		t.Fatalf("step %+v steps %d", j.Step, len(j.Steps))
	}
	it.AgentField = &factory.AgentFieldRun{RunID: "r"}
	it.Stages, it.Stream.Phases = []factory.Stage{{Name: "triage", Label: "Triage", Kind: factory.StageCheck, On: true}}, []factory.Phase{{Name: "triage", State: factory.PhaseRunning}}
	it.State = factory.StateRunning
	j = FromItem(it)
	if j.Step == nil || j.Step.Name != "triage" || j.Step.Label != "Triage" {
		t.Fatalf("once the run reports, step %+v", j.Step)
	}
}
