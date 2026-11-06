package session

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// C1, C6: proposals compare every relative, including finished work, using the shared normalization.
func TestNodeTreeRefusesDuplicateWorkAnywhere(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	for _, relative := range []string{"ancestor", "sibling", "cousin", "finished"} {
		t.Run(relative, func(t *testing.T) {
			n := newNest(t, nil, nil)
			n.handOut(t, "Read: the LAW!")
			first := n.graph.children(n.parent.id)[0]
			worker := n.node
			if relative == "ancestor" {
				worker = n.workerFor(t, first)
				first = n.parent
			}
			if relative == "cousin" {
				n.handOut(t, "Inspect the parser")
				worker = n.workerFor(t, n.graph.children(n.parent.id)[1])
				firstWorker := n.workerFor(t, first)
				n.handOutFrom(t, firstWorker, "Read the rules")
				first = n.graph.children(first.id)[0]
			}
			if relative == "finished" {
				n.graph.complete(first, TaskDone)
			}
			before := len(n.graph.nodes)
			title := "read---THE law"
			if relative == "ancestor" {
				title = "THE---whole JOB"
			}
			if relative == "cousin" {
				title = "read---THE rules"
			}
			answer, refused, err := worker.proposeTask(t.Context(), pieceArgs(title))
			for _, want := range []string{"no: this run already has a task named", fmt.Sprintf("(%d)", first.id), "nothing was added", "do it in your own hands", "name it for what is different"} {
				if err != nil || !refused || !strings.Contains(answer, want) {
					t.Fatalf("duplicate refusal missing %q: %q, %v, %v", want, answer, refused, err)
				}
			}
			if len(n.graph.nodes) != before {
				t.Fatal("duplicate work was admitted")
			}
			if answer := n.handOutFrom(t, worker, "Different work"); !strings.Contains(answer, "started") {
				t.Fatalf("different work refused: %s", answer)
			}
		})
	}
}

// C1, C3: cancelled titles are reusable, and two person-authored roots have independent title scopes.
func TestNodeTreeAllowsCancelledTitlesAndSeparateRoots(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	n := newNest(t, nil, nil)
	n.handOut(t, "Read the law")
	kid := n.graph.children(n.parent.id)[0]
	if _, err := n.graph.stop(kid.id); err != nil {
		t.Fatal(err)
	}
	if answer := n.handOut(t, "read-the LAW"); !strings.Contains(answer, "started") {
		t.Fatalf("cancelled title refused: %s", answer)
	}
	id, _, _, err := n.session.StartTask(t.Context(), n.parent.title(), false)
	if err != nil {
		t.Fatalf("a person's typed task was refused: %v", err)
	}
	other := n.workerFor(t, n.graph.node(id))
	if answer := n.handOutFrom(t, other, "Read the law"); !strings.Contains(answer, "started") {
		t.Fatalf("a different tree inherited the first tree's titles: %s", answer)
	}
}

// C1, C2: division batches are refused before any part exists; quick tasks use the same door.
func TestNodeTreeRefusesAnEntireDuplicateDivisionAndQuickTask(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	n := newNest(t, nil, nil)
	line := &journalDivision{}
	ids, _, refusal := n.node.startTheParts(n.parent, []dividePart{{Title: "Read the law"}, {Title: "read---the LAW"}}, line)
	if len(ids) != 0 || len(n.graph.children(n.parent.id)) != 0 || !strings.Contains(refusal, "this batch names the same work") || !strings.Contains(refusal, "nothing was added") {
		t.Fatalf("division partially admitted or refused for the wrong reason: %v, %s", ids, refusal)
	}
	n.handOut(t, "Read the law")
	ids, _, refusal = n.node.startTheParts(n.parent, []dividePart{{Title: "Different part"}, {Title: "read the LAW"}}, &journalDivision{})
	if len(ids) != 0 || len(n.graph.children(n.parent.id)) != 1 || !strings.Contains(refusal, "already has a task named") {
		t.Fatalf("division containing existing work partly started: %v, %s", ids, refusal)
	}
	id, _, quick := n.node.admitQuick(quickAsk{line: "read---the LAW"})
	if id != 0 || !strings.Contains(quick.said, "already has a task named") || len(n.graph.children(n.parent.id)) != 1 {
		t.Fatalf("quick task bypassed duplicate refusal: %d, %+v", id, quick)
	}
}

// C1, C2: staged sibling proposals reserve their titles and withdrawal releases exactly its reservation.
func TestNodeTreeReservesPendingProposalTitles(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	n := newNest(t, nil, nil)
	first, ok := n.node.stageTask(t.Context(), pieceArgs("Read the law")).(*stagedProposal)
	if !ok {
		t.Fatal("first proposal did not stage")
	}
	other, ok := n.node.stageTask(t.Context(), pieceArgs("Inspect the parser")).(*stagedProposal)
	if !ok {
		t.Fatal("different proposal did not stage")
	}
	first.Withdraw()
	if answer := n.handOut(t, "READ the law"); !strings.Contains(answer, "started") {
		t.Fatalf("withdrawn title stayed reserved: %s", answer)
	}
	answer, refused, err := n.node.proposeTask(t.Context(), pieceArgs("inspect---the PARSER"))
	if err != nil || !refused || !strings.Contains(answer, "this batch names the same work") {
		t.Fatalf("another pending title lost its reservation: %q, %v, %v", answer, refused, err)
	}
	other.Withdraw()
}

// C4: several nodes exhaust one ceiling, cut an in-flight call, fail queued work and prevent further admission.
func TestNodeTreeSpendStopsEveryNodeAndNamesTheBindingLimit(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	for _, conversation := range []bool{false, true} {
		t.Run(fmt.Sprint(conversation), func(t *testing.T) {
			entered := make(chan struct{})
			completer := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) { return pricedResponse("first", .4), nil },
				func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
					close(entered)
					<-ctx.Done()
					return nil, ctx.Err()
				},
				func(context.Context, []ai.Message) (*ai.Response, error) {
					cost := 1.1
					if conversation {
						cost = .6
					}
					return pricedResponse("last", cost), nil
				},
			}}
			a, _ := newTestAgent(t, completer, func(c *Config) {
				c.ProfileDir = t.TempDir()
				c.Interactive = true
				if conversation {
					c.Budget = Budget{USD: 1.25}
				}
			})
			if err := config.SetCrewTaskCap(a.config.ProfileDir, "1.5"); err != nil {
				t.Fatal(err)
			}
			if conversation {
				a.usage.CostUSD = .25
			}
			g := a.graph()
			g.report = nil
			g.run = func(*TaskNode) {}
			g.limit = 3
			rootID := g.reserve()
			g.admit(rootID, taskSpec{title: "Whole assignment", named: true, depth: 1, model: "test/model"})
			root := g.node(rootID)
			workers := make([]*Agent, 0, 3)
			nodes := []*TaskNode{root}
			for i := 0; i < 3; i++ {
				title := fmt.Sprintf("Part %d", i)
				if refusal := g.claimChild(rootID, title); refusal != "" {
					t.Fatal(refusal)
				}
				id := g.reserve()
				g.admit(id, taskSpec{title: title, named: true, parent: rootID, depth: 2, model: "test/model"})
				nodes = append(nodes, g.node(id))
			}
			for _, node := range nodes[:3] {
				worker, err := a.newTaskAgent(t.Context(), t.TempDir(), node, "")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = worker.Close() })
				workers = append(workers, worker)
			}
			if _, err := workers[0].completeWithModel(t.Context(), purposeTurn, nil, "test/model"); err != nil {
				t.Fatal(err)
			}
			// Keep this node owned until its provider observes cancellation.
			if !nodes[1].claimRun() {
				t.Fatal("in-flight node was not running")
			}
			ended := make(chan error, 1)
			go func() {
				_, err := workers[1].completeWithModel(t.Context(), purposeTurn, nil, "test/model")
				ended <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("scripted call never entered")
			}
			if _, err := workers[2].completeWithModel(t.Context(), purposeTask, nil, "test/model"); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-ended:
				if err == nil {
					t.Fatal("in-flight call was not cancelled")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("tree ceiling left a call in flight")
			}
			g.complete(nodes[1], TaskFailed)
			want := config.CrewTaskCapAction(1.5)
			if conversation {
				want = "conversation"
			}
			for _, node := range nodes {
				row := node.notice()
				if row.State != TaskFailed || row.Ending != TaskEndingCostLimit || !strings.Contains(row.Report, want) || conversation && strings.Contains(row.Report, "raise it in /crew") {
					t.Fatalf("tree limit ending is wrong: %+v", row)
				}
				if conversation && (!strings.Contains(row.Report, "$1.25") || !strings.Contains(row.Report, "/settings")) {
					t.Fatalf("conversation limit lost its amount or raise action: %+v", row)
				}
			}
			if refusal := g.claimChild(rootID, "Another part"); !strings.Contains(refusal, want) {
				t.Fatalf("spent tree admitted another node: %s", refusal)
			}
			completer.mu.Lock()
			calls := len(completer.seen)
			completer.mu.Unlock()
			if calls != 3 || !nodes[3].started.IsZero() {
				t.Fatalf("queued work reached the provider: calls=%d, queued started=%v", calls, nodes[3].started)
			}
		})
	}
}

// C5: one node below the ceiling runs through the real quick-task runner and finishes normally.
func TestNodeTreeUnderTheCeilingFinishesNormally(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	a, _ := newTestAgent(t, &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return pricedResponse("finished", .25), nil
	}}}, func(c *Config) { c.ProfileDir = t.TempDir() })
	g := a.graph()
	g.report = nil
	id, _, refusal := a.admitQuick(quickAsk{line: "Read the law"})
	if refusal.said != "" {
		t.Fatal(refusal.said)
	}
	node := g.node(id)
	waitDoneNode(t, node)
	if row := node.notice(); row.State != TaskDone || row.Report != "finished" || row.CostUSD != .25 || row.Ending == TaskEndingCostLimit {
		t.Fatalf("under-ceiling node changed: %+v", row)
	}
}

// C4: a restored root keeps exactly the shared spend, including child work already folded into its bill.
func TestNodeTreeCheckpointKeepsTheUnspentAllowance(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	profile := t.TempDir()
	if err := config.SetCrewTaskCap(profile, "1"); err != nil {
		t.Fatal(err)
	}
	a, _ := newTestAgent(t, &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return pricedResponse("partly done", .75), nil
	}}}, func(c *Config) { c.ProfileDir = profile })
	g := a.graph()
	g.run, g.report = func(*TaskNode) {}, nil
	checkpoint := filepath.Join(t.TempDir(), "tasks.json")
	g.store = newTaskStore(checkpoint)
	id := g.reserve()
	g.admit(id, taskSpec{title: "Read the law", named: true, brief: "Read the law", acceptance: "The law is read", depth: 1, model: "test/model"})
	worker, err := a.newTaskAgent(t.Context(), t.TempDir(), g.node(id), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	if _, err := worker.completeWithModel(t.Context(), purposeTurn, nil, "test/model"); err != nil {
		t.Fatal(err)
	}
	document := readCheckpoint(t, checkpoint)
	if saved := recordOf(t, document, id).TreeSpend; saved == nil || saved.Spent != .75 {
		t.Fatalf("checkpoint lost the tree's live spend: %+v", saved)
	}
	// A parent's historic bill can already contain its finished child's bill.
	document.Nodes[0].State, document.Nodes[0].CostUSD = TaskQueued, .75
	document.Nodes = append(document.Nodes, taskRecord{ID: id + 1, Parent: id, Depth: 2, Title: "Finished part", Brief: "Read the rules", Acceptance: "The rules are read", State: TaskDone, CostUSD: .25})
	document.Seq = id + 1
	b, _ := newTestAgent(t, &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return pricedResponse("the remaining work", .25), nil
	}}}, func(c *Config) { c.ProfileDir = profile })
	restored := b.graph()
	restored.run, restored.report = func(*TaskNode) {}, nil
	restored.rehydrate(document, b.config.Workspace, TaskSettleAsk)
	restored.runFrontier()
	resumed, err := b.newTaskAgent(t.Context(), t.TempDir(), restored.node(id), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resumed.Close() })
	if _, err := resumed.completeWithModel(t.Context(), purposeTurn, nil, "test/model"); err != nil {
		t.Fatalf("the actual remaining allowance was lost: %v", err)
	}
	if row := restored.node(id).notice(); row.Ending != TaskEndingCostLimit || !strings.Contains(row.Report, config.CrewTaskCapAction(1)) {
		t.Fatalf("resumed work received a fresh tree allowance: %+v", row)
	}
}

// C6: both admission roads make the same decision for Unicode, digits and punctuation.
func TestNodeTreeUsesThePlanStoresWorkIdentity(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	for _, title := range []string{"READ---café 42", "read_café...42", "Read / CAFÉ / 42"} {
		t.Run(title, func(t *testing.T) {
			store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.db"), "work", "root", "Read: Café 42!", "the work")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if _, err := store.AddManyBounded([]plandb.TaskSpec{{Title: title}}); err == nil || !strings.Contains(err.Error(), "already has a task named") {
				t.Fatalf("plan admission changed its work identity: %v", err)
			}
			n := newNest(t, nil, nil)
			n.handOut(t, "Read: Café 42!")
			if answer := n.handOut(t, title); !strings.Contains(answer, "already has a task named") {
				t.Fatalf("node admission disagrees with plan admission: %s", answer)
			}
		})
	}
}

// C3, C4: workers, repairs, checks and owner helpers share one tree's money; another tree remains live.
func TestNodeTreeCountsEveryCallAndKeepsOtherTreesIndependent(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	completer := &scriptedCompleter{}
	for i := 0; i < 7; i++ {
		completer.steps = append(completer.steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return pricedResponse("a paid answer", .125), nil
		})
	}
	a, _ := newTestAgent(t, completer, func(c *Config) { c.ProfileDir = t.TempDir() })
	if err := config.SetCrewTaskCap(a.config.ProfileDir, "0.75"); err != nil {
		t.Fatal(err)
	}
	g := a.graph()
	g.run, g.report = func(*TaskNode) {}, nil
	rootID := g.reserve()
	g.admit(rootID, taskSpec{title: "Read the law", named: true, depth: 1, model: "test/model"})
	otherID := g.reserve()
	g.admit(otherID, taskSpec{title: "Read the law", named: true, depth: 1, model: "test/model"})
	childID := g.reserve()
	g.admit(childID, taskSpec{title: "Read the rules", named: true, parent: rootID, depth: 2, model: "test/model"})
	root := g.node(rootID)
	builds := []func() (*Agent, error){
		func() (*Agent, error) { return a.newTaskAgent(t.Context(), t.TempDir(), root, "") },
		func() (*Agent, error) {
			return a.newTaskAgentOn(t.Context(), t.TempDir(), root, "-repair", "test/model", true)
		},
		func() (*Agent, error) {
			return a.newAuditAgent(t.TempDir(), root, plainDoor(auditReadCommands), "test/model")
		},
		func() (*Agent, error) { return a.newTaskAgent(t.Context(), t.TempDir(), g.node(childID), "") },
	}
	var rootWorker *Agent
	for _, build := range builds {
		worker, err := build()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = worker.Close() })
		if rootWorker == nil {
			rootWorker = worker
		}
		if _, err := worker.completeWithModel(t.Context(), purposeTurn, nil, "test/model"); err != nil {
			t.Fatal(err)
		}
	}
	helper, err := rootWorker.newChildAgent(Config{Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM", InTask: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = helper.Close() })
	if _, err := helper.completeWithModel(t.Context(), purposeTurn, nil, "test/model"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.completeWithModel(withNodeTree(t.Context(), root), purposeTask, nil, "test/model"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint64{rootID, childID} {
		if row := g.node(id).notice(); row.State != TaskFailed || row.Ending != TaskEndingCostLimit {
			t.Fatalf("a paid role escaped the tree tally: %+v", row)
		}
	}
	other := g.node(otherID)
	if other.stateNow() != TaskRunning {
		t.Fatal("one tree's limit stopped a separate assignment")
	}
	worker, err := a.newTaskAgent(t.Context(), t.TempDir(), other, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	if _, err := worker.completeWithModel(t.Context(), purposeTurn, nil, "test/model"); err != nil {
		t.Fatalf("another tree lost its own allowance: %v", err)
	}
	other.finish("finished", nil, "", "")
	g.complete(other, TaskDone)
	if other.stateNow() != TaskDone {
		t.Fatal("another tree could not finish")
	}
	if got := completer.requests(); got != 7 {
		t.Fatalf("paid calls = %d, want 7", got)
	}
}

// C4: an already spent conversation allowance starts no node, even with an unpriced completer.
func TestNodeTreeWithNoConversationAllowanceNeverStarts(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	completer := &scriptedCompleter{}
	a, _ := newTestAgent(t, completer, func(c *Config) {
		c.ProfileDir = t.TempDir()
		c.Interactive = true
		c.Budget = Budget{USD: 1}
	})
	a.usage.CostUSD = 1
	g := a.graph()
	g.run, g.report = func(*TaskNode) { t.Error("a node without any allowance started") }, nil
	id := g.reserve()
	g.admit(id, taskSpec{title: "Read the law", named: true, brief: "Read the law", acceptance: "The law is read", depth: 1})
	row := g.node(id).notice()
	if row.State != TaskFailed || row.Ending != TaskEndingCostLimit || !row.StartedAt.IsZero() || !strings.Contains(row.Report, "conversation") || !strings.Contains(row.Report, "$1") || completer.requests() != 0 {
		t.Fatalf("an exhausted conversation allowance reached a worker: %+v, calls=%d", row, completer.requests())
	}
}
