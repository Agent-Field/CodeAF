package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeliveryContractRejectsMissingInvalidAndStaleAuthority(t *testing.T) {
	const ask = "leave the fix on a branch without merging"
	for _, d := range []deliveryContract{
		{}, {Kind: "branch"}, {Kind: "report", Quote: "write me a report"}, {Kind: "anything", Quote: ask},
	} {
		if got := validDelivery(ask, d); got.acceptsRetained() {
			t.Fatalf("invalid declaration relaxed delivery: %+v", got)
		}
	}
	v := routeVerdict{checksRequest: "worker says this is done", Delivery: deliveryContract{Kind: "branch", Quote: ask}}
	if routeDelivery(v, ask).acceptsRetained() {
		t.Fatal("worker or stale request supplied delivery authority")
	}
	s := NewSteward(ask, Budget{Wall: time.Hour}, nil)
	if !s.setAcceptanceDelivery(ask, "the branch holds the fix", nil, deliveryContract{Kind: "branch", Quote: ask}) {
		t.Fatal("initial contract refused")
	}
	if s.setAcceptanceDelivery("integrate it now", "different", nil, deliveryContract{Kind: "report", Quote: "integrate it now"}) {
		t.Fatal("follow-up changed frozen delivery")
	}
	if s.setAcceptanceDelivery(ask, "different", nil, deliveryContract{Kind: "report", Quote: ask}) {
		t.Fatal("worker could rewrite delivery")
	}
	if s.declaredDelivery().Kind != "branch" {
		t.Fatal("frozen delivery changed")
	}
}

func TestTheAcceptanceWireFreezesAndRecordsDelivery(t *testing.T) {
	const ask = "make the fix and leave the result on a branch without merging"
	path := filepath.Join(t.TempDir(), "session.jsonl")
	payload, _ := json.Marshal(map[string]any{"work": true, "goal": ask, "acceptance": "the retained branch holds the tested fix", "delivery": deliveryContract{Kind: "branch", Quote: "leave the result on a branch without merging"}})
	a, _ := newTestAgent(t, &scriptedCompleter{steps: []step{finalText(string(payload))}}, func(c *Config) { c.Unattended = true; c.Budget = Budget{Wall: time.Hour}; c.SessionFile = path })
	a.steward().hear(ask)
	a.openAcceptance(context.Background(), nil)
	d := a.remainsFor("", readerLine{}).Delivery
	if d.Kind != "branch" || d.Quote == "" {
		t.Fatalf("acceptance wire lost delivery: %+v", d)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range journaledEntries(t, path, "principal") {
		if e.Principal != nil && e.Principal.Event == "acceptance" && e.Principal.Delivery != nil {
			found = *e.Principal.Delivery == d
		}
	}
	if !found {
		t.Fatal("principal receipt lost delivery")
	}
	resumed, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Unattended = true; c.Budget = Budget{Wall: time.Hour}; c.SessionFile = path })
	resumed.steward().hear("integrate the fix into this workspace")
	if resumed.steward().declaredDelivery().acceptsRetained() {
		t.Fatal("old branch acceptance became a new ask's authority")
	}
}

func TestRetainedDeliveryDecisionMatrix(t *testing.T) {
	for _, tc := range []struct {
		name     string
		delivery deliveryContract
		retained string
		want     DecisionVerb
	}{
		{"workspace kept", deliveryContract{Kind: "workspace"}, "task/fix", DecideCarryOn},
		{"unknown kept", deliveryContract{}, "task/fix", DecideCarryOn},
		{"branch requested", validDelivery("leave a branch", deliveryContract{Kind: "branch", Quote: "leave a branch"}), "task/fix", DecideDone},
		{"report requested", validDelivery("give me a report", deliveryContract{Kind: "report", Quote: "give me a report"}), "task/report", DecideDone},
		{"merged", deliveryContract{Kind: "workspace"}, "", DecideDone},
		{"shared", deliveryContract{Kind: "workspace"}, "", DecideDone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Remains{Landed: true, Delivery: tc.delivery, ReaderSaysDone: true, Landings: []Landing{{ID: 1, Title: "fix", State: TaskDone, Retained: tc.retained, Files: []string{"fix.txt"}, Merged: tc.name == "merged", InPlace: tc.name == "shared", Produced: tc.name == "report requested"}}}
			got := budgetLeft(t).Decide(r)
			if got.Verb != tc.want {
				t.Fatalf("decision=%+v want %v", got, tc.want)
			}
		})
	}
}

// This exercises real protected-branch landing and the Agent's checkpoint door.
// The main checkout is deliberately unchanged and every task result says done.
func TestCheckpointKeepsProtectedBranchDeliveryUnfinished(t *testing.T) {
	repo := newTestRepo(t)
	mustGit(t, repo, "checkout", "-b", "main")
	before := gitOut(t, repo, "rev-parse", "HEAD")
	tree, err := prepareTaskTree(Place{Dir: t.TempDir(), Workspace: repo}, repo, "delivery", 1, "write the fix")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(tree.dir, "fix.txt"), "the fix\n")
	merge, detail, _ := tree.comeHome("write the fix", []string{"fix.txt"})
	if merge != mergeKept {
		t.Fatalf("protected landing=%s: %s", merge, detail)
	}
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Workspace = repo; c.Unattended = true; c.Budget = Budget{Wall: time.Hour} })
	s := a.steward()
	s.hear("implement the fix in this workspace")
	s.setAcceptanceDelivery(s.Ask(), "fix.txt is in this workspace", nil, deliveryContract{Kind: "workspace"})
	g := a.graph()
	g.mu.Lock()
	g.nodes[1] = &TaskNode{graph: g, id: 1, owner: a, state: TaskDone, spec: taskSpec{title: "fix"}, changed: []string{"fix.txt"}, branch: tree.branch, merge: merge, report: detail}
	// A child merged only into its parent must not waive the retained parent.
	g.nodes[2] = &TaskNode{graph: g, id: 2, parent: 1, state: TaskDone, spec: taskSpec{title: "part"}, merge: mergeMerged}
	g.order = []uint64{1, 2}
	g.mu.Unlock()
	got := a.decideRemains(context.Background(), readerLine{answered: true, nothingLeft: true}, "everything is done")
	if got.Verb != DecideCarryOn || !strings.Contains(got.Brief, "retained branch") {
		t.Fatalf("checkpoint falsely finished: %+v", got)
	}
	if gitOut(t, repo, "rev-parse", "HEAD") != before || gitOut(t, repo, "status", "--porcelain") != "" {
		t.Fatal("completion check mutated protected checkout")
	}
	// Explicit integration by the person resolves the retained-delivery fact.
	mustGit(t, repo, "merge", "--ff-only", tree.branch)
	if r := a.remainsFor("", readerLine{}); r.Landings[0].Retained != "" {
		t.Fatalf("actual integrated content still blocked: %+v", r.Landings)
	}
}

func TestMergedChildOnlyDeliversToItsOwnParentSession(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Unattended = true; c.Budget = Budget{Wall: time.Hour} })
	g := a.graph()
	g.mu.Lock()
	g.nodes[2] = &TaskNode{graph: g, id: 2, parent: 1, state: TaskDone, spec: taskSpec{title: "child implementation"}, merge: mergeMerged}
	g.order = []uint64{2}
	g.mu.Unlock()
	_, landed, _ := a.landings()
	if landed {
		t.Fatal("child merged into absent parent counted as delivered to root")
	}
	// Restored nodes have no owner pointer. The persisted parent id is enough.
	a.config.taskID = 1
	_, landed, _ = a.landings()
	if !landed {
		t.Fatal("child's real parent lost its own delivered work")
	}
}

func TestAReportDeclarationNeedsAnActualAnswer(t *testing.T) {
	r := Remains{Landed: true, Delivery: validDelivery("give me a report", deliveryContract{Kind: "report", Quote: "give me a report"}), Landings: []Landing{{ID: 1, Title: "report", State: TaskDone, Files: []string{"report.md"}, Retained: "task/report"}}}
	if got := budgetLeft(t).Decide(r); got.Verb == DecideDone {
		t.Fatal("empty report waived retained work")
	}
	r.Landings[0].Produced = true
	if got := budgetLeft(t).Decide(r); got.Verb != DecideDone {
		t.Fatalf("requested report was not accepted: %+v", got)
	}
}

func TestAReportAlreadyPrintedInItsLandingStillCountsAsProduced(t *testing.T) {
	const ask = "give me a report of the findings"
	const answer = "The audit found three unused ports."
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Workspace = t.TempDir(); c.Unattended = true; c.Budget = Budget{Wall: time.Hour} })
	s := a.steward()
	s.hear(ask)
	s.setAcceptanceDelivery(ask, "the report contains the findings", nil, deliveryContract{Kind: "report", Quote: ask})
	g := a.graph()
	n := &TaskNode{graph: g, id: 1, state: TaskDone, spec: taskSpec{title: "findings"}, changed: []string{"report.md"}, branch: "task/report", merge: mergeKept, report: answer}
	n.keepResult(answer)
	g.mu.Lock()
	g.nodes[1] = n
	g.order = []uint64{1}
	g.mu.Unlock()
	r := a.remainsFor(answer, readerLine{answered: true, nothingLeft: true})
	if !r.Landings[0].Produced {
		t.Fatal("presentation dedupe erased evidence of the actual answer")
	}
	if got := s.Decide(r); got.Verb != DecideDone {
		t.Fatalf("report wrongly unfinished: %+v", got)
	}
}
