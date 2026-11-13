package session

import (
	"context"
	"errors"
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

// C3, C4: real runners refuse each tree member's next request at the binding limit; other trees still finish.
func TestNodeTreeSpendStopsEveryNodeAndNamesTheBindingLimit(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	useTaskUsageHome(t)
	for _, conversation := range []bool{false, true} {
		t.Run(fmt.Sprint(conversation), func(t *testing.T) {
			rootEntered, childEntered := make(chan struct{}), make(chan struct{})
			rootHold, releaseRoot := context.WithCancel(t.Context())
			defer releaseRoot()
			childHold, releaseChild := context.WithCancel(t.Context())
			defer releaseChild()
			completer := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return nodeTreeReadResponse("root-read", .5), nil
				},
				func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
					close(rootEntered)
					select {
					case <-rootHold.Done():
						return nodeTreeReadResponse("root-read-again", 0), nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				},
				func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
					close(childEntered)
					select {
					case <-childHold.Done():
						cost := 1.0
						if conversation {
							cost = .5
						}
						return nodeTreeReadResponse("part-read", cost), nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				},
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return pricedResponse("another tree finished", .125), nil
				},
			}}
			a, _ := newTestAgent(t, completer, func(c *Config) {
				c.ProfileDir = t.TempDir()
				writeFile(t, filepath.Join(c.Workspace, "rules.txt"), "Read the rules.\n")
				c.TaskParallel = 2
				c.TaskAudit = false
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
			// Deliver child reports so the root can finish folding its parts.
			// Root reports stay quiet to keep conversation wakes off the script.
			g.report = func(node *TaskNode) {
				if node.parent != 0 {
					a.reportTaskNode(node)
				}
			}
			g.store = newTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
			root := nodeTreeQuick(t, a, 0, "Whole assignment")
			nodeTreeAwait(t, rootEntered)
			part := nodeTreeQuick(t, a, root.id, "Read the rules")
			nodeTreeAwait(t, childEntered)
			queued := nodeTreeQuick(t, a, root.id, "Read the examples")
			if queued.stateNow() != TaskQueued {
				t.Fatal("third node did not queue behind the two live workers")
			}
			releaseChild()
			waitDoneNode(t, part)
			waitDoneNode(t, queued)
			if queued.notice().StartedAt.IsZero() {
				t.Fatal("queued node did not reach its first request boundary")
			}
			// Reaching the limit leaves the already admitted provider call alone.
			select {
			case <-root.done:
				t.Fatal("the tree limit cancelled a request in flight")
			default:
			}
			if queued.spend() != 0 || completer.requests() != 3 {
				t.Fatalf("queued node bought a request: spend=%v, calls=%d", queued.spend(), completer.requests())
			}
			otherID := g.reserve()
			g.admit(otherID, taskSpec{title: "Whole assignment", named: true, brief: "Read the rules", acceptance: "The rules are read", depth: 1, model: "test/model"})
			other := g.node(otherID)
			waitDoneNode(t, other)
			if row := other.notice(); row.State != TaskDone || !strings.Contains(row.Report, "another tree finished") || row.CostUSD != .125 {
				t.Fatalf("another tree inherited the exhausted allowance: %+v", row)
			}
			releaseRoot()
			waitDoneNode(t, root)
			want := config.CrewTaskCapAction(1.5)
			if conversation {
				want = "the conversation reached its $1.25 limit · raise the per conversation limit in /settings"
			}
			for _, node := range []*TaskNode{root, part, queued} {
				nodeTreeCostEnding(t, node, want)
				if conversation && strings.Contains(node.notice().Report, "/crew") {
					t.Fatalf("conversation limit was described as the task cap: %+v", node.notice())
				}
			}
			if calls := completer.requests(); calls != 4 {
				t.Fatalf("exhausted tree reached the provider again: calls=%d, want 4 including the other tree", calls)
			}
			g.checkpoint()
			document := readCheckpoint(t, g.store.path)
			recovered := newTaskGraph()
			recovered.rehydrate(document, a.config.Workspace, TaskSettleAsk)
			for _, node := range []*TaskNode{root, part, queued} {
				record := recordOf(t, document, node.id)
				if record.Ending != TaskEndingCostLimit || recovered.node(node.id).stoppedByPerson() {
					t.Fatalf("cost ending became a person's stop after restore: %+v", record)
				}
			}
		})
	}
}

// C5: an ordinary node below the ceiling runs through the real runner and finishes normally.
func TestNodeTreeUnderTheCeilingFinishesNormally(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	useTaskUsageHome(t)
	a, _ := newTestAgent(t, &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return pricedResponse("finished", .25), nil
	}}}, func(c *Config) {
		c.ProfileDir = t.TempDir()
		c.TaskAudit = false
	})
	g := a.graph()
	g.report = nil
	id := g.reserve()
	g.admit(id, taskSpec{title: "Read the law", named: true, brief: "Read the law", acceptance: "The law is read", depth: 1, model: "test/model"})
	node := g.node(id)
	waitDoneNode(t, node)
	if row := node.notice(); row.State != TaskDone || !strings.Contains(row.Report, "finished") || row.CostUSD != .25 || row.Ending == TaskEndingCostLimit {
		t.Fatalf("under-ceiling node changed: %+v", row)
	}
}

// C4: recovery retains a conversation-bound ceiling and a paid live worker's bill before any owner fold.
func TestNodeTreeCheckpointKeepsTheUnspentAllowance(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	useTaskUsageHome(t)
	profile := t.TempDir()
	if err := config.SetCrewTaskCap(profile, "5"); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	hold, release := context.WithCancel(t.Context())
	defer release()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return nodeTreeReadResponse("before-recovery", .75), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(entered)
			select {
			case <-hold.Done():
				return pricedResponse("finished after the snapshot", 0), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}}
	options := func(c *Config) {
		c.ProfileDir = profile
		writeFile(t, filepath.Join(c.Workspace, "rules.txt"), "Read the rules.\n")
		c.Interactive = true
		c.Budget = Budget{USD: 1.25}
		c.TaskAudit = false
	}
	a, _ := newTestAgent(t, completer, options)
	a.usage.CostUSD = .25
	g := a.graph()
	g.report = nil
	checkpoint := filepath.Join(t.TempDir(), "tasks.json")
	g.store = newTaskStore(checkpoint)
	id := g.reserve()
	g.admit(id, taskSpec{title: "Read the law", named: true, brief: "Read the law", acceptance: "The law is read", depth: 1, model: "test/model"})
	root := g.node(id)
	nodeTreeAwait(t, entered)
	g.checkpoint()
	// Atomic control writes must retain the same unfolded bill as ordinary saves.
	for _, action := range []string{"pause", "resume"} {
		if err := a.AppTaskAction(AppTaskIntent{ID: fmt.Sprint(id), Action: action}); err != nil {
			t.Fatal(err)
		}
	}
	document := readCheckpoint(t, checkpoint)
	record := recordOf(t, document, id)
	if record.TreeLimit == nil || record.TreeLimit.CeilingUSD != 1 || !record.TreeLimit.Conversation || record.TreeLimit.ConversationCapUSD != 1.25 || record.CostUSD != .75 {
		t.Fatalf("checkpoint lost the admitted ceiling or live bill: %+v", record)
	}
	g.mu.Lock()
	folded := root.cost
	g.mu.Unlock()
	if folded != 0 || a.Usage().CostUSD != .25 {
		t.Fatalf("snapshot was taken after folding: node=%v, conversation=%v", folded, a.Usage().CostUSD)
	}
	// Changed settings and the owner's still-unfolded books cannot grant a new ceiling.
	if err := config.SetCrewTaskCap(profile, "10"); err != nil {
		t.Fatal(err)
	}
	resumedCalls := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return nodeTreeReadResponse("after-recovery", .25), nil
		},
	}}
	b, _ := newTestAgent(t, resumedCalls, options)
	b.usage.CostUSD = .25
	restored := b.graph()
	restored.report = nil
	restored.rehydrate(document, b.config.Workspace, TaskSettleAsk)
	restored.runFrontier()
	resumed := restored.node(id)
	waitDoneNode(t, resumed)
	nodeTreeCostEnding(t, resumed, "the conversation reached its $1.25 limit · raise the per conversation limit in /settings")
	if resumed.spend() != 1 || resumedCalls.requests() != 1 {
		t.Fatalf("restored tree replenished its allowance: spend=%v, requests=%d", resumed.spend(), resumedCalls.requests())
	}
	release()
	waitDoneNode(t, root)
}

// C4: a checkpoint without the additive ceiling fact retains the existing task-cap remainder.
func TestNodeTreeCheckpointWithoutLimitComputesItAtRestore(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	useTaskUsageHome(t)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return nodeTreeReadResponse("remaining-work", .25), nil
		},
	}}
	a, _ := newTestAgent(t, completer, func(c *Config) {
		c.ProfileDir = t.TempDir()
		writeFile(t, filepath.Join(c.Workspace, "rules.txt"), "Read the rules.\n")
		c.TaskAudit = false
	})
	if err := config.SetCrewTaskCap(a.config.ProfileDir, "1"); err != nil {
		t.Fatal(err)
	}
	g := a.graph()
	g.report = nil
	g.rehydrate(taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: 1, Nodes: []taskRecord{{
		ID: 1, Title: "Read the law", Brief: "Read the law", Acceptance: "The law is read",
		State: TaskQueued, Depth: 1, Model: "test/model", CostUSD: .75,
	}}}, a.config.Workspace, TaskSettleAsk)
	g.runFrontier()
	root := g.node(1)
	waitDoneNode(t, root)
	nodeTreeCostEnding(t, root, config.CrewTaskCapAction(1))
	if root.spend() != 1 || completer.requests() != 1 {
		t.Fatalf("old checkpoint lost its remaining allowance: spend=%v, requests=%d", root.spend(), completer.requests())
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
			if _, err := store.AddManyBounded([]plandb.TaskSpec{{ID: "variant", Title: title}}); err == nil || !strings.Contains(err.Error(), "already has a task named") {
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

// C4: a queued worker in an already exhausted conversation is refused at its first request without spending.
func TestNodeTreeWithNoConversationAllowanceSpendsNothing(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	useTaskUsageHome(t)
	completer := &scriptedCompleter{}
	a, _ := newTestAgent(t, completer, func(c *Config) {
		c.ProfileDir = t.TempDir()
		c.Interactive = true
		c.Budget = Budget{USD: 1}
	})
	a.usage.CostUSD = 1
	g := a.graph()
	g.report = nil
	root := nodeTreeQuick(t, a, 0, "Read the law")
	waitDoneNode(t, root)
	nodeTreeCostEnding(t, root, "the conversation reached its $1 limit · raise the per conversation limit in /settings")
	if root.spend() != 0 || completer.requests() != 0 {
		t.Fatalf("an exhausted conversation reached the provider: spend=%v, calls=%d", root.spend(), completer.requests())
	}
}

// C4: a reached ceiling refuses the next request even while checkpoint writes are blocked.
func TestNodeTreeLimitRefusesBeforeCheckpointWrites(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	useTaskUsageHome(t)
	entered := make(chan struct{})
	hold, release := context.WithCancel(t.Context())
	defer release()
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(entered)
			select {
			case <-hold.Done():
				return nodeTreeReadResponse("last-read", .5), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}}
	a, _ := newTestAgent(t, completer, func(c *Config) {
		c.ProfileDir = t.TempDir()
		writeFile(t, filepath.Join(c.Workspace, "rules.txt"), "Read the rules.\n")
		c.Interactive = true
		c.Budget = Budget{USD: .5}
	})
	if err := config.SetCrewTaskCap(a.config.ProfileDir, "1.5"); err != nil {
		t.Fatal(err)
	}
	g := a.graph()
	g.report = nil
	g.store = newTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	root := nodeTreeQuick(t, a, 0, "Read the law")
	nodeTreeAwait(t, entered)
	events, leave, err := a.WatchTaskRoom(root.id)
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	g.store.mu.Lock()
	locked := true
	defer func() {
		if locked {
			g.store.mu.Unlock()
		}
	}()
	release()
	action := "the conversation reached its $0.50 limit · raise the per conversation limit in /settings"
	deadline := time.After(5 * time.Second)
	refused := false
	for !refused {
		select {
		case event, open := <-events:
			if !open {
				t.Fatal("worker ended before publishing its refusal")
			}
			if event.Kind == EventError {
				var stopped ErrSpendStopped
				if !errors.As(event.Err, &stopped) || stopped.Action != action {
					t.Fatalf("wrong request refusal: %v", event.Err)
				}
				refused = true
			}
		case <-deadline:
			t.Fatal("the spend boundary waited for checkpoint I/O")
		}
	}
	if root.endingSet() != TaskEndingCostLimit || completer.requests() != 1 {
		t.Fatalf("blocked checkpoint postponed the limit: ending=%q, calls=%d", root.endingSet(), completer.requests())
	}
	g.store.mu.Unlock()
	locked = false
	waitDoneNode(t, root)
	nodeTreeCostEnding(t, root, action)
}

// C4: a part a worker proposed runs on that worker and folds its bill into it
// when it lands, so the parent's own spend already holds the part. The tree
// total counts every dollar once: the part, and a grandchild two levels down,
// are not counted again in each worker they folded into.
func TestNodeTreeSpendCountsAPartFoldedIntoItsOwnerOnce(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	useTaskUsageHome(t)
	n := newNest(t, nil, nil)
	root := n.parent
	n.node.usage.CostUSD = 1
	n.handOut(t, "Read the law")
	part := nodeTreeChild(t, n.graph, root.id, "Read the law")
	if part.owner != n.node || n.graph.runner(part) != n.node {
		t.Fatal("the part is not owned by the worker that proposed it")
	}
	partWorker := n.workerFor(t, part)
	n.handOutFrom(t, partWorker, "Read the examples")
	grandchild := nodeTreeChild(t, n.graph, part.id, "Read the examples")
	if grandchild.owner != partWorker {
		t.Fatal("the grandchild is not owned by the part's worker")
	}
	grandWorker := n.workerFor(t, grandchild)
	partWorker.usage.CostUSD = 2
	grandWorker.usage.CostUSD = .5
	// Real spend is $1 + $2 + $0.50 = $3.50 at every point below.
	limit := func(ceiling float64) string {
		n.graph.mu.Lock()
		root.treeLimit = &nodeTreeLimit{CeilingUSD: ceiling}
		n.graph.mu.Unlock()
		return root.treeSpendLimit()
	}
	land := func(node *TaskNode, worker *Agent) {
		node.openRoom().speaking(nil)
		n.graph.runner(node).foldTaskUsage(node, worker)
	}
	for _, step := range []struct {
		name string
		land func()
	}{
		{"before any fold", func() {}},
		{"grandchild folded into the part's worker", func() { land(grandchild, grandWorker) }},
		{"part folded into the root's worker", func() { land(part, partWorker) }},
	} {
		step.land()
		if action := limit(4); action != "" {
			t.Fatalf("%s: $3.50 of real spend was refused under a $4 tree limit: %s", step.name, action)
		}
		if action := limit(3.5); action != config.CrewTaskCapAction(3.5) {
			t.Fatalf("%s: $3.50 of real spend passed a $3.50 tree limit: %q", step.name, action)
		}
	}
}

// nodeTreeChild is the one node under parent with title.
func nodeTreeChild(t *testing.T, g *TaskGraph, parent uint64, title string) *TaskNode {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, id := range g.order {
		if node := g.nodes[id]; node != nil && node.parent == parent && node.spec.title == title {
			return node
		}
	}
	t.Fatalf("no task %q under %d", title, parent)
	return nil
}

func nodeTreeReadResponse(id string, cost float64) *ai.Response {
	response := toolResponse(id, "read", `{"path":"rules.txt"}`)
	response.Usage.Cost = &cost
	return response
}

func nodeTreeAwait(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("scripted worker did not reach its request")
	}
}

func nodeTreeQuick(t *testing.T, a *Agent, parent uint64, title string) *TaskNode {
	t.Helper()
	spec, refusal := a.newQuickSpec(quickAsk{line: title})
	if refusal != "" {
		t.Fatal(refusal)
	}
	g := a.graph()
	spec.parent = parent
	if parent != 0 {
		spec.depth = g.node(parent).depth + 1
	}
	if refusal := g.claimChild(parent, title); refusal != "" {
		t.Fatal(refusal)
	}
	id := g.reserve()
	g.admit(id, spec)
	return g.node(id)
}

func nodeTreeCostEnding(t *testing.T, node *TaskNode, action string) {
	t.Helper()
	row := node.notice()
	if row.State != TaskFailed || row.Ending != TaskEndingCostLimit || row.Stopped || !strings.Contains(row.Report, action) {
		t.Fatalf("wrong cost-limit ending, want %q: %+v", action, row)
	}
}
