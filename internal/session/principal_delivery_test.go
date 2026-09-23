package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
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

func TestARetainedResultReadsAndRecordsTheOriginalDeliveryRequest(t *testing.T) {
	const ask = "make the fix and leave the result on a branch without merging"
	path := filepath.Join(t.TempDir(), "session.jsonl")
	payload, _ := json.Marshal(map[string]any{"work": true, "goal": ask, "acceptance": "the retained branch holds the tested fix", "checks": []string{"true"}, "delivery": deliveryContract{Kind: "branch", Quote: "leave the result on a branch without merging"}})
	completer := &scriptedCompleter{steps: []step{finalText(string(payload))}}
	a, _ := newTestAgent(t, completer, func(c *Config) { c.Unattended = true; c.Budget = Budget{Wall: time.Hour}; c.SessionFile = path })
	a.steward().hear(ask)
	a.openAcceptance(context.Background(), nil)
	if completer.requests() != 0 {
		t.Fatalf("opening the request made %d model calls", completer.requests())
	}
	remains := Remains{Landed: true, ReaderSaysDone: true, Landings: []Landing{{ID: 1, Title: "fix", State: TaskDone,
		Files: []string{"fix.go"}, Retained: "task/fix"}}}
	remains = a.completeRetainedContract(context.Background(), remains)
	d := remains.Delivery
	if d.Kind != "branch" || d.Quote == "" {
		t.Fatalf("acceptance wire lost delivery: %+v", d)
	}
	if got := a.steward().declaredChecks(); len(got) != 0 {
		t.Fatalf("the delivery-only reading granted late command authority: %v", got)
	}
	if decision := a.steward().Decide(remains); decision.Verb != DecideDone {
		t.Fatalf("the requested retained branch did not complete: %+v", decision)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range journaledEntries(t, path, "principal") {
		if e.Principal != nil && e.Principal.Event == "delivery" && e.Principal.Delivery != nil {
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

func TestALazyDeliveryReadingNeverInstallsOrRunsLateChecks(t *testing.T) {
	const ask = "make the fix and leave it on a branch"
	dir := t.TempDir()
	marker := filepath.Join(dir, "late-check-ran")
	payload, _ := json.Marshal(map[string]any{"work": true, "goal": ask,
		"acceptance": "done", "checks": []string{"touch " + marker},
		"delivery": deliveryContract{Kind: "branch", Quote: "leave it on a branch"}})
	a, _ := newTestAgent(t, &scriptedCompleter{steps: []step{finalText(string(payload))}}, func(c *Config) {
		c.Workspace = dir
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	a.steward().hear(ask)
	if !a.steward().setAcceptanceDelivery(ask, sessionAcceptance(ask), []string{"true"}, deliveryContract{}) {
		t.Fatal("could not install the opening structured check")
	}
	a.openBaseline(context.Background())
	remains := Remains{Landed: true, Landings: []Landing{{ID: 1, State: TaskDone,
		Files: []string{"fix.go"}, Retained: "task/fix"}}}
	remains = a.completeRetainedContract(context.Background(), remains)
	if got := a.steward().declaredChecks(); !slices.Equal(got, []string{"true"}) {
		t.Fatalf("late destination reading changed opening command authority: %v", got)
	}
	_, _, _ = a.terminalAudit(context.Background())
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("late check ran at the ending: %v", err)
	}
}

func TestALazyDeliveryReadingSeesTheTailOfTheOriginalAsk(t *testing.T) {
	const tail = "leave the final result on a branch without merging"
	ask := strings.Repeat("background ", routeAskBytes) + tail
	payload, _ := json.Marshal(map[string]any{"work": true, "goal": ask,
		"acceptance": "done", "delivery": deliveryContract{Kind: "branch", Quote: tail}})
	completer := &scriptedCompleter{steps: []step{finalText(string(payload))}}
	a, _ := newTestAgent(t, completer, func(c *Config) { c.Unattended = true; c.Budget = Budget{Wall: time.Hour} })
	a.steward().hear(ask)
	a.openAcceptance(context.Background(), nil)
	remains := Remains{Landed: true, Landings: []Landing{{ID: 1, State: TaskDone,
		Files: []string{"fix.go"}, Retained: "task/fix"}}}
	remains = a.completeRetainedContract(context.Background(), remains)
	if !remains.Delivery.acceptsRetained() {
		t.Fatalf("delivery permission at the request tail was lost: %+v", remains.Delivery)
	}
	sent := completer.request(0)
	if len(sent) == 0 || !strings.Contains(messageText(sent[len(sent)-1]), tail) {
		t.Fatal("the lazy destination reader did not receive the full original ask")
	}
}

func TestConcurrentEndingsShareOneLazyDeliveryReading(t *testing.T) {
	const ask = "make the fix and leave it on a branch"
	payload, _ := json.Marshal(map[string]any{"work": true, "goal": ask,
		"acceptance": "done", "delivery": deliveryContract{Kind: "branch", Quote: "leave it on a branch"}})
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	completer := &scriptedCompleter{steps: []step{func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
		once.Do(func() { close(entered) })
		select {
		case <-release:
			return textResponse(string(payload)), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}}
	a, _ := newTestAgent(t, completer, func(c *Config) { c.Unattended = true; c.Budget = Budget{Wall: time.Hour} })
	a.steward().hear(ask)
	a.openAcceptance(context.Background(), nil)
	input := Remains{Landed: true, Landings: []Landing{{ID: 1, State: TaskDone,
		Files: []string{"fix.go"}, Retained: "task/fix"}}}
	results := make(chan Remains, 2)
	for range 2 {
		go func() { results <- a.completeRetainedContract(context.Background(), input) }()
	}
	<-entered
	close(release)
	first, second := <-results, <-results
	if completer.requests() != 1 {
		t.Fatalf("concurrent endings bought %d destination readings", completer.requests())
	}
	if first.Delivery != second.Delivery || !first.Delivery.acceptsRetained() {
		t.Fatalf("concurrent endings disagreed: first=%+v second=%+v", first.Delivery, second.Delivery)
	}
}

func TestAnUnreadableRetainedDestinationDefaultsToWorkspaceOnce(t *testing.T) {
	const ask = "make the requested change"
	completer := &scriptedCompleter{steps: []step{finalText("not a structured answer")}}
	a, _ := newTestAgent(t, completer, func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	a.steward().hear(ask)
	a.openAcceptance(context.Background(), nil)
	remains := Remains{Landed: true, Landings: []Landing{{ID: 1, Title: "change", State: TaskDone,
		Files: []string{"change.txt"}, Retained: "task/change"}}}
	first := a.completeRetainedContract(context.Background(), remains)
	second := a.completeRetainedContract(context.Background(), first)
	if first.Delivery.Kind != "workspace" || second.Delivery.Kind != "workspace" {
		t.Fatalf("unreadable destination was not conservative: first=%+v second=%+v", first.Delivery, second.Delivery)
	}
	if completer.requests() != 1 {
		t.Fatalf("unreadable destination was asked %d times", completer.requests())
	}
}

func TestARequestedRetainedReportDeliversItsProducedAnswer(t *testing.T) {
	const ask = "give me a written report and leave its source on a branch"
	payload, _ := json.Marshal(map[string]any{"work": true, "goal": ask,
		"acceptance": "the written report answers the request",
		"delivery":   deliveryContract{Kind: "report", Quote: "give me a written report"}})
	a, _ := newTestAgent(t, &scriptedCompleter{steps: []step{finalText(string(payload))}}, func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	a.steward().hear(ask)
	a.openAcceptance(context.Background(), nil)
	remains := Remains{Landed: true, Landings: []Landing{{ID: 1, Title: "report", State: TaskDone,
		Files: []string{"report.md"}, Retained: "task/report", Produced: true}}}
	remains = a.completeRetainedContract(context.Background(), remains)
	if decision := a.steward().Decide(remains); decision.Verb != DecideDone {
		t.Fatalf("the requested produced report did not complete: %+v", decision)
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

func TestAnUncomparableChangedListIsNotEvidenceOfDelivery(t *testing.T) {
	repo := newTestRepo(t)
	mustGit(t, repo, "checkout", "-b", "main")
	mustGit(t, repo, "checkout", "-b", "task/x")
	writeFile(t, filepath.Join(repo, "subproject", "actual.txt"), "the change\n")
	mustGit(t, repo, "add", "subproject/actual.txt")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "task work")
	mustGit(t, repo, "checkout", "main")

	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Workspace = repo })
	node := &TaskNode{parent: a.config.taskID}
	for _, changed := range [][]string{
		{"nosuchfile.txt"},
		{"actual.txt"},
		{"subproject/actual.txt"},
	} {
		if got := a.retainedDelivery(node, changed, "task/x", mergeKept); got != "task/x" {
			t.Fatalf("retained delivery = %q, want task/x when none of %v can be compared", got, changed)
		}
	}

	mustGit(t, repo, "merge", "--ff-only", "task/x")
	if got := a.retainedDelivery(node, []string{"subproject/actual.txt"}, "task/x", mergeKept); got != "" {
		t.Fatalf("retained delivery = %q after the branch content reached the workspace, want empty", got)
	}
}

func TestAKeptBranchNobodyCanCompareIsNotFinished(t *testing.T) {
	repo := newTestRepo(t)
	mustGit(t, repo, "checkout", "-b", "main")
	tree, err := prepareTaskTree(Place{Dir: t.TempDir(), Workspace: repo}, repo, "delivery", 1, "write the fix")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(tree.dir, "fix.txt"), "the fix\n")
	merge, detail, _, _ := tree.comeHome("write the fix", []string{"fix.txt"}, gitSignature{})
	if merge != mergeKept {
		t.Fatalf("protected landing=%s: %s", merge, detail)
	}
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = repo
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	s := a.steward()
	s.hear("implement the fix in this workspace")
	s.setAcceptanceDelivery(s.Ask(), "fix.txt is in this workspace", nil, deliveryContract{Kind: "workspace"})
	g := a.graph()
	g.mu.Lock()
	g.nodes[1] = &TaskNode{graph: g, id: 1, owner: a, state: TaskDone, spec: taskSpec{title: "fix"}, changed: []string{"nosuchfile.txt"}, branch: tree.branch, merge: merge, report: detail}
	g.order = []uint64{1}
	g.mu.Unlock()

	beforeHead := gitOut(t, repo, "rev-parse", "HEAD")
	beforeStatus := gitOut(t, repo, "status", "--porcelain")
	got := a.decideRemains(context.Background(), readerLine{answered: true, nothingLeft: true}, "everything is done")
	if got.Verb != DecideCarryOn || !strings.Contains(got.Brief, "retained branch") {
		t.Fatalf("checkpoint falsely finished: %+v", got)
	}
	if afterHead := gitOut(t, repo, "rev-parse", "HEAD"); afterHead != beforeHead {
		t.Fatalf("completion check moved HEAD from %s to %s", beforeHead, afterHead)
	}
	if afterStatus := gitOut(t, repo, "status", "--porcelain"); afterStatus != beforeStatus {
		t.Fatalf("completion check changed checkout status from %q to %q", beforeStatus, afterStatus)
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
	merge, detail, _, _ := tree.comeHome("write the fix", []string{"fix.txt"}, gitSignature{})
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
