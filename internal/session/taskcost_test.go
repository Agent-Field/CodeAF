package session

// WHAT A PIECE OF WORK COST, AFTER THE PROCESS THAT PAID FOR IT IS GONE.
//
// Three files carry a bill and each of them is asked one question here: the
// checkpoint (does a resumed node still know what it spent), the project index
// (can a landed row say at what rate, and over how many tokens), and the index
// again for an adaptive run (does the tank ever reach disk). The fourth law —
// that an older file without any of it still opens — is pinned first, because
// every field below is additive and a checkpoint written yesterday is the
// commonest file this build will ever be handed.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── harness ─────────────────────────────────────────────────────────────────

// spentChild is a real agent that has really spent something: one turn, answered
// with a price on it, which is how every test in this package stubs a bill
// (task_repair_test.go's pricedResponse). It is what a node folds in.
func spentChild(t *testing.T, cost float64) *Agent {
	t.Helper()
	child, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return pricedResponse("the work is done", cost), nil
		},
	}}, nil)
	collect(t, mustSubmit(t, child, "do the work"))
	return child
}

// landOneBilledNode admits a node, folds a spender into it exactly where the
// executor does, and lands it — then hands back the checkpoint and the project
// index it left behind.
func landOneBilledNode(t *testing.T, cost float64) (checkpoint, index string, id uint64) {
	t.Helper()
	journal, checkpoint := journalIn(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})
	index = TaskIndexPath(journal)
	child := spentChild(t, cost)

	graph := agent.graph()
	graph.mu.Lock()
	graph.run = func(node *TaskNode) {
		agent.foldTaskUsage(node, child)
		node.finish("the greeting is written", []string{"greet.go"}, "", mergeInPlace)
		node.graph.complete(node, TaskDone)
	}
	graph.mu.Unlock()

	id = graph.reserve()
	graph.admit(id, taskSpec{
		title: "Add the greeting", brief: "write greet.go", acceptance: "the file is there",
		model: "vendor/worker",
	})
	waitDoneNode(t, graph.node(id))
	return checkpoint, index, id
}

// ── the checkpoint ──────────────────────────────────────────────────────────

// ADDITIVE, ALWAYS. A checkpoint written before a node carried a bill has no
// money and no tokens in it, and it must open, reconcile and carry on exactly as
// it always did — with zero standing for "nobody counted", which is what every
// surface here already draws as nothing.
func TestACheckpointWrittenBeforeTasksCarriedABillStillLoadsAndResumes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	old := `{
  "type": "tasks",
  "version": 1,
  "seq": 2,
  "nodes": [
    {"id":1,"title":"Name the bug","brief":"find it","acceptance":"named","state":"done",
     "report":"the nil map is built in reconcile()","noted":true,"elapsed_ms":1200},
    {"id":2,"title":"Fix it","brief":"fix the reconciler","acceptance":"tests pass",
     "depends_on":[1],"state":"queued"}
  ]
}` + "\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	document, ok := loadTaskCheckpoint(path)
	if !ok {
		t.Fatalf("a checkpoint from before the bill existed was refused: %s", path)
	}
	for _, record := range document.Nodes {
		if record.CostUSD != 0 || record.Input != 0 || record.Output != 0 ||
			record.CacheRead != 0 || record.CacheWrite != 0 {
			t.Fatalf("node %d invented a bill out of an old file: %+v", record.ID, record)
		}
	}

	graph := newTaskGraph()
	ran := make(chan uint64, 2)
	graph.run = func(node *TaskNode) {
		ran <- node.id
		node.finish("the reconciler is fixed", nil, "", mergeInPlace)
		node.graph.complete(node, TaskDone)
	}
	recovery := graph.rehydrate(document, t.TempDir(), TaskSettleAsk)
	if recovery.done != 1 || recovery.waiting != 1 {
		t.Fatalf("recovery counted %+v, want one finished node and one still waiting", recovery)
	}
	graph.runFrontier()
	if id := waitStarted(t, ran); id != 2 {
		t.Fatalf("node %d ran, want the one that was waiting", id)
	}
	waitDoneNode(t, graph.node(2))

	// And the node that came back with no bill does not grow one it was never
	// given: an unpriced node is a node nobody could price, not a free one.
	if spent := graph.node(1).spend(); spent != 0 {
		t.Fatalf("the resumed history node reports %.4f spent, want nothing at all", spent)
	}
}

// A LANDED NODE'S BILL IS ON ITS CHECKPOINT, in money and in tokens. The money
// is what somebody published at the time; the tokens are what actually
// happened, and they are kept apart so a node run on a model nobody priced can
// still be priced later.
func TestALandedNodesCheckpointCarriesItsCostAndItsTokens(t *testing.T) {
	checkpoint, _, id := landOneBilledNode(t, 0.02)

	record := awaitRecord(t, checkpoint, id, func(record taskRecord) bool {
		return record.State == TaskDone && record.CostUSD > 0
	}, "landed with its bill written down")

	if record.CostUSD < 0.02 {
		t.Fatalf("the checkpoint says %.4f, want at least the 0.02 the node's agent spent: %+v",
			record.CostUSD, record)
	}
	// The stub answers every turn with 10 prompt tokens and 5 completion tokens
	// (agent_test.go's textResponse), so one turn is the floor and a session that
	// also named itself is above it.
	if record.Input < 10 || record.Output < 5 {
		t.Fatalf("the tokens did not survive the transition: %+v", record)
	}

	// And they come back as themselves: a resumed node reports the same bill,
	// which is the whole point of writing it down.
	document := readCheckpoint(t, checkpoint)
	graph := newTaskGraph()
	graph.run = func(*TaskNode) {}
	graph.rehydrate(document, t.TempDir(), TaskSettleAsk)
	node := graph.node(id)
	if node == nil {
		t.Fatalf("node %d did not come back", id)
	}
	if spend := node.spend(); spend != record.CostUSD {
		t.Fatalf("the resumed node reports %.4f, want the %.4f on its record", spend, record.CostUSD)
	}
	graph.mu.Lock()
	in, out := node.input, node.output
	graph.mu.Unlock()
	if in != record.Input || out != record.Output {
		t.Fatalf("the resumed node holds %d/%d tokens, want the %d/%d on its record",
			in, out, record.Input, record.Output)
	}
}

// ── the project index ───────────────────────────────────────────────────────

// THE FILE THAT CARRIES THE COST CARRIES THE RATE. A row that could say what a
// node spent but not what it spent it on made re-pricing a landed task a
// three-file join; the tokens ride beside it as ONE sum, because this file is a
// citation and the split lives in the journal the row names.
func TestALandedNodesIndexRowCarriesItsModelAndItsTokens(t *testing.T) {
	_, index, id := landOneBilledNode(t, 0.02)

	row := awaitIndexRow(t, index, id, func(row TaskIndexEntry) bool { return row.Cost > 0 })
	if row.Model != "vendor/worker" {
		t.Fatalf("the row says the work ran on %q, want the model it was admitted on: %+v", row.Model, row)
	}
	if row.Tokens < 15 {
		t.Fatalf("the row counted %d tokens, want at least the one turn's 15: %+v", row.Tokens, row)
	}
}

// THE RUN'S OWN ROW IS CLOSED BY A SECOND ROW. The launch wrote one saying
// "running" with no price on it, because there was neither an ending nor a bill
// yet; this is the row that says how it ended and what the whole tank came to.
// Nothing is edited — the newest row for an id is the one that counts.
//
// The pair is read off the FILE and not out of [ReadTaskIndex], because the
// reader is where "the last row counts" is actually enforced: it collapses a
// node to one row on the way out ([lastPerNode]), so a run that started and
// ended answers with its ending and never with both. The two halves are tested
// together here — the file keeps the pair, the reader hands back the closing
// row — because a reader that collapsed the wrong way would still pass either
// half on its own.
func TestTheRunsClosingRowLandsInTheIndexWithTheWholeTank(t *testing.T) {
	var index string
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
		index = TaskIndexPath(config.SessionFile)
	})
	family := agent.newOrchestrateFamily("audit the pricing code", "cheap/planner", "run-7")
	family.upsert([]orchestrate.NodeStatus{
		{Node: orchestrate.Node{ID: "tariff", Goal: "read the tariff table"},
			State: orchestrate.Done, Digest: "found regional prices", Cost: 0.12},
	})
	family.settle(orchestrate.Snapshot{
		Done: true, Answer: "the tariff table is keyed by region",
		Fuel: orchestrate.Fuel{Cap: 5, Spent: 0.31},
	}, nil)

	rows := readTaskIndexFile(t, index)
	rootID := taskIndexParent(family.root)
	var opening, closing *TaskIndexEntry
	for i, row := range rows {
		if row.ID != rootID || row.Parent != "" {
			continue
		}
		if row.Status == string(TaskRunning) {
			opening = &rows[i]
			continue
		}
		closing = &rows[i]
	}
	if opening == nil || closing == nil {
		t.Fatalf("the run left %d rows and not the opening and closing pair: %+v", len(rows), rows)
	}
	if opening.Cost != 0 {
		t.Fatalf("the launch row priced a run that had not spent anything yet: %+v", opening)
	}
	// An ending field on a row that has not ended is a landing time nobody can
	// read as anything else.
	if !opening.EndedAt.IsZero() {
		t.Fatalf("a running row is dated %s, which is when it began", opening.EndedAt)
	}
	if closing.Status != string(TaskDone) || closing.Outcome != "the tariff table is keyed by region" {
		t.Fatalf("the closing row is %+v", closing)
	}
	// THE ROOT'S FIGURE IS THE TANK, NOT THE SUM OF ITS CHILDREN: the planner
	// and the closing write-up bill against the run and belong to no node, so
	// the gap between the two is what the orchestration itself cost.
	if closing.Cost != 0.31 {
		t.Fatalf("the closing row says %.4f, want the whole tank's 0.31: %+v", closing.Cost, closing)
	}
	if closing.Model != "cheap/planner" {
		t.Fatalf("the closing row says the run thought with %q, want the planner's model", closing.Model)
	}
	// The two rows name the same piece of work, so a reader taking the last row
	// per id is taking the last row about the SAME thing.
	if closing.Name != opening.Name || closing.Title != opening.Title {
		t.Fatalf("the closing row renamed the run: %q/%q against %q/%q",
			closing.Name, closing.Title, opening.Name, opening.Title)
	}
	// And what a reader is handed is the ending, once: the index answers what
	// the work CAME TO, never a "running" row about a run that has ended.
	var answered int
	for _, row := range ReadTaskIndex(index) {
		if row.ID != rootID || row.Parent != "" {
			continue
		}
		answered++
		if row.Status != string(TaskDone) || row.Cost != 0.31 {
			t.Fatalf("the index answered with %+v, want the closing row", row)
		}
	}
	if answered != 1 {
		t.Fatalf("the index answered with %d rows for the run, want the closing one alone", answered)
	}
}

// readTaskIndexFile is every row the index file HOLDS, oldest first, with no
// collapsing — the raw record behind [ReadTaskIndex], for the tests that are
// about what was written rather than about what a reader is handed.
func readTaskIndexFile(t *testing.T, path string) []TaskIndexEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the index was never written: %v", err)
	}
	var rows []TaskIndexEntry
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row TaskIndexEntry
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("the index holds a row that does not parse: %v", err)
		}
		rows = append(rows, row)
	}
	return rows
}

// awaitIndexRow polls the project index until a row for one node satisfies a
// condition. The index is appended on the far side of a node's done channel, so
// it is waited for rather than assumed — exactly as awaitRecord waits for the
// checkpoint (task_store_test.go).
func awaitIndexRow(t *testing.T, path string, id uint64, want func(TaskIndexEntry) bool) TaskIndexEntry {
	t.Helper()
	wanted := taskIndexParent(id)
	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, row := range ReadTaskIndex(path) {
			if row.ID == wanted && want(row) {
				return row
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the index never showed a settled row for node %d", id)
			return TaskIndexEntry{}
		}
		time.Sleep(5 * time.Millisecond)
	}
}
