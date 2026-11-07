package factory

import (
	"fmt"
	"slices"
	"testing"
)

// A job an AgentField reasoner runs says where it came from with no work by
// the reasoner's developer: AgentField and the reasoner as its source, the run's
// page as its link, the node, reasoner and trigger as its origin, and the
// source's labels plus node:<node>, folded, each once, at most twenty.
func TestAnAgentFieldJobSaysWhereItCameFrom(t *testing.T) {
	labels := []string{"Billing", "billing", " Refund ", ""}
	for i := range 30 {
		labels = append(labels, fmt.Sprintf("extra%02d", i))
	}
	it := Item{RunOn: "support.triage", Origin: "agentfield:generic-bearer", SourceRef: "agentfield:generic-bearer:T-1",
		AgentField: &AgentFieldRun{RunID: "run_1", URL: "http://cp:8080/ui/runs/run_1"}, Labels: labels}

	src, ok := JobSource(it)
	if !ok || src != (JobSourceFacts{Name: "agentfield", Ref: "support.triage", URL: "http://cp:8080/ui/runs/run_1"}) {
		t.Fatalf("source is %+v", src)
	}
	o, ok := JobOriginOf(it)
	if !ok || o != (JobOrigin{Node: "support", Reasoner: "triage", Trigger: "generic-bearer"}) {
		t.Fatalf("origin is %+v", o)
	}
	got := JobLabels(it)
	if len(got) != JobLabelsMost || got[0] != "node:support" || got[1] != "billing" || got[2] != "refund" || slices.Contains(got, "Billing") {
		t.Fatalf("labels are %v", got)
	}

	// Filed by a direct execute: no trigger fired.
	it.Origin = "agentfield:execute"
	if o, _ := JobOriginOf(it); o.Trigger != "" {
		t.Fatalf("a direct execute names trigger %q", o.Trigger)
	}

	// A job this factory runs itself has no node to name, whatever filed it,
	// and keeps the source it always had.
	own := Item{Origin: "agentfield:generic-bearer", SourceRef: "agentfield:generic-bearer:T-2", Labels: []string{"Ops"}}
	if _, ok := JobOriginOf(own); ok {
		t.Fatal("a job the factory runs names an origin node")
	}
	if src, _ := JobSource(own); src.Name != "agentfield:generic-bearer" || src.Ref != "T-2" {
		t.Fatalf("a trigger-filed factory job's source is %+v", src)
	}
	if got := JobLabels(own); !slices.Equal(got, []string{"ops"}) {
		t.Fatalf("its labels are %v", got)
	}

	// A GitHub issue's labels, folded the same way.
	gh := Item{Origin: OriginForge, Repo: "api", Product: "acme", Num: 7, Labels: []string{"Bug", "good first issue", "bug"}}
	if got := JobLabels(gh); !slices.Equal(got, []string{"bug", "good first issue"}) {
		t.Fatalf("a GitHub job's labels are %v", got)
	}
	if JobLabels(Item{}) != nil {
		t.Fatal("a job with no labels reads labels")
	}
}
