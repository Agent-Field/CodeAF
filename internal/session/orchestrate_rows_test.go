package session

// A RUN'S ROWS OUTLIVE THE LANE THAT DREW THEM, and these are the two halves of
// that: a lane opening late is handed the whole of a run that is happening now,
// and a conversation reopened tomorrow is handed the runs it started as history.
//
// The two halves share one row-space and one door — [TaskGraph.runs] and the
// roster replay — which is what these tests are really pinning: a replayed row
// and a live row are the same notice, and a restored row is that notice settled.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
)

// mustEncode is one document as its bytes, for the tests that hand a file
// straight to the validator.
func mustEncode(t *testing.T, document taskDocument) []byte {
	t.Helper()
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode the checkpoint: %v", err)
	}
	return encoded
}

// runRowsOf is the family's rows out of a drained lane, keyed by id in the order
// they arrived.
func runRowsOf(notices []TaskNotice, run string) []TaskNotice {
	var out []TaskNotice
	for _, notice := range notices {
		if notice.Run == run {
			out = append(out, notice)
		}
	}
	return out
}

// ── a lane that opens late ──────────────────────────────────────────────────

// A RUN STILL FORMING REPLAYS AS A RUN STILL FORMING. This is the minute before
// there is anything to draw: the row exists, the opening planner call is out,
// and a second surface attaching in that minute must see the same line the first
// one did — not a bare row, and not nothing at all.
func TestALaneOpeningWhileARunIsFormingIsHandedTheFormingLine(t *testing.T) {
	agent, updates := familyAgent(t)
	agent.newOrchestrateFamily("audit the pricing code", "cheap/planner", "run-pricing")
	familyNotices(t, updates)

	replayed := runRowsOf(familyNotices(t, agent.TaskUpdates()), "run-pricing")
	if len(replayed) != 1 {
		t.Fatalf("%d rows replayed for a run nobody has planned yet: %+v", len(replayed), replayed)
	}
	root := replayed[0]
	if root.Doing != orchestrateForming {
		t.Fatalf("the replayed run's row says %q, want %q", root.Doing, orchestrateForming)
	}
	if root.State != TaskRunning || root.Node != "" || root.Parent != 0 || root.Model != "cheap/planner" {
		t.Fatalf("the replayed run's row is %+v", root)
	}
}

// AND A RUN WITH WORKERS REPLAYS WHOLE: the run's own row, then every worker
// under it, each carrying the identity a tree hangs it by.
func TestALaneOpeningLateIsHandedALiveRunsWholeFamily(t *testing.T) {
	agent, updates := familyAgent(t)
	family := agent.newOrchestrateFamily("audit the pricing code", "cheap/planner", "run-pricing")
	family.worker = "cheap/worker"
	family.upsert([]orchestrate.NodeStatus{
		node("n1", "tariff table", "You are reading the tariff table.", orchestrate.Running),
		node("n2", "invoice writer", "You are reading the invoice writer.", orchestrate.Queued),
	})
	familyNotices(t, updates)

	replayed := runRowsOf(familyNotices(t, agent.TaskUpdates()), "run-pricing")
	if len(replayed) != 3 {
		t.Fatalf("%d rows replayed, want the run and its two workers: %+v", len(replayed), replayed)
	}

	// THE RUN'S OWN ROW COMES FIRST, because that is the order it was published
	// in and a tree wants its root before the children that name it.
	root := replayed[0]
	if root.Node != "" || root.Parent != 0 {
		t.Fatalf("the first replayed row is not the run's own: %+v", root)
	}
	// And the forming line is off it, exactly as it came off live once the
	// workers landed.
	if root.Doing != "" {
		t.Fatalf("the replayed run's row still says %q with its workers on the board", root.Doing)
	}

	states := map[string]TaskState{}
	for _, row := range replayed[1:] {
		if row.Parent != root.ID {
			t.Fatalf("worker %q hangs off %d, want the run's row %d", row.Node, row.Parent, root.ID)
		}
		if row.Model != "cheap/worker" {
			t.Fatalf("worker %q runs on %q, want the run's worker model", row.Node, row.Model)
		}
		states[row.Node] = row.State
	}
	if states["n1"] != TaskRunning || states["n2"] != TaskQueued {
		t.Fatalf("the replayed workers are %+v", states)
	}
	if replayed[1].Title != "tariff table" || replayed[2].Title != "invoice writer" {
		t.Fatalf("the replayed workers are named %q and %q", replayed[1].Title, replayed[2].Title)
	}
}

// AND A REPLAYED ROW IS THE ROW THAT WAS SENT, field for field. This is the
// guarantee that makes there be no second builder: whatever a live watcher was
// handed is what the next lane is handed.
func TestAReplayedRunRowIsTheNoticeThatWasSent(t *testing.T) {
	agent, updates := familyAgent(t)
	family := agent.newOrchestrateFamily("audit the pricing code", "cheap/planner", "run-pricing")
	family.worker = "cheap/worker"
	family.upsert([]orchestrate.NodeStatus{
		{Node: orchestrate.Node{ID: "n1", Title: "tariff table", Goal: "You are reading the tariff table."},
			State: orchestrate.Done, Digest: "it charges per seat", Cost: 0.25},
	})
	live := runRowsOf(familyNotices(t, updates), "run-pricing")
	replayed := runRowsOf(familyNotices(t, agent.TaskUpdates()), "run-pricing")

	last := make(map[uint64]TaskNotice, len(live))
	for _, row := range live {
		last[row.ID] = row
	}
	if len(replayed) != len(last) {
		t.Fatalf("%d rows replayed for %d live rows", len(replayed), len(last))
	}
	for _, row := range replayed {
		if was, drawn := last[row.ID]; !drawn || !reflect.DeepEqual(was, row) {
			t.Fatalf("row %d replayed as %+v, was sent as %+v", row.ID, row, was)
		}
	}
}

// ── a conversation reopened ─────────────────────────────────────────────────

// resumedRunRows runs a family against one conversation, closes it, and answers
// with the rows the next conversation on the same journal redraws.
func resumedRunRows(t *testing.T, script func(*orchestrateFamily)) (*Agent, []TaskNotice) {
	t.Helper()
	journal, _ := journalIn(t)
	yesterday, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})
	family := yesterday.newOrchestrateFamily("audit the pricing code", "cheap/planner", "7")
	family.worker = "cheap/worker"
	script(family)
	if err := yesterday.Close(); err != nil {
		t.Fatalf("close the first conversation: %v", err)
	}

	today, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})
	return today, runRowsOf(familyNotices(t, today.TaskUpdates()), "7")
}

// THE OWNER'S SCENARIO. A run was mid-flight when aforge closed; the next
// morning the conversation is reopened and its rows are on the column again —
// settled, saying what happened to them, and not pretending to be alive.
func TestAResumedConversationRedrawsAMidFlightRunSettled(t *testing.T) {
	today, rows := resumedRunRows(t, func(family *orchestrateFamily) {
		family.upsert([]orchestrate.NodeStatus{
			node("n1", "tariff table", "You are reading the tariff table.", orchestrate.Running),
			node("n2", "invoice writer", "You are reading the invoice writer.", orchestrate.Queued),
		})
	})
	if len(rows) != 3 {
		t.Fatalf("%d rows redrawn, want the run and its two workers: %+v", len(rows), rows)
	}
	for _, row := range rows {
		if !row.State.settled() {
			t.Fatalf("row %d came back %s — a restored run is history, not work: %+v", row.ID, row.State, row)
		}
		if !row.Stopped {
			t.Fatalf("row %d came back as a failure rather than as work that was cut short: %+v", row.ID, row)
		}
		if row.Report != orchestrateEndedReport {
			t.Fatalf("row %d says %q, want %q", row.ID, row.Report, orchestrateEndedReport)
		}
		if row.Doing != "" {
			t.Fatalf("row %d came back mid-phase, saying %q", row.ID, row.Doing)
		}
	}
	// The tree is still a tree: the run's own row, and the workers under it.
	if rows[0].Parent != 0 || rows[1].Parent != rows[0].ID || rows[2].Parent != rows[0].ID {
		t.Fatalf("the restored family is not a family: %+v", rows)
	}
	// And the names survived, because a row nobody can read is a row nobody can
	// use.
	if rows[1].Title != "tariff table" || rows[2].Title != "invoice writer" {
		t.Fatalf("the restored workers are named %q and %q", rows[1].Title, rows[2].Title)
	}

	// NONE OF IT IS WORK. Not on the frontier, not in the graph, not counted as
	// running, and not a run this session can be asked to stop.
	graph := today.graph()
	graph.mu.Lock()
	nodes, order := len(graph.nodes), len(graph.order)
	graph.mu.Unlock()
	if nodes != 0 || order != 0 {
		t.Fatalf("a restored run put %d nodes into the graph", nodes)
	}
	if working := CountWorking(today.WorkingNow()); working != 0 {
		t.Fatalf("%d pieces of work are moving in a session that has started none", working)
	}
	if _, err := today.Cancel(CancelRun + ":7"); err == nil {
		t.Fatal("a run that ended with the last process answered a stop")
	}
}

// AND THE ROWS THAT HAD ALREADY SAID THEIR LAST WORD KEEP IT. A restored run is
// not a run rewritten: done stays done with its digest, and a failure keeps the
// finding that was made about it.
func TestARestoredRunKeepsItsDoneAndFailedRowsVerbatim(t *testing.T) {
	_, rows := resumedRunRows(t, func(family *orchestrateFamily) {
		family.upsert([]orchestrate.NodeStatus{
			{Node: orchestrate.Node{ID: "n1", Title: "tariff table", Goal: "You are reading the tariff table."},
				State: orchestrate.Done, Digest: "it charges per seat", Cost: 0.25},
			{Node: orchestrate.Node{ID: "n2", Title: "invoice writer", Goal: "You are reading the invoice writer."},
				State: orchestrate.Failed, Err: "the file was not there"},
		})
	})
	by := make(map[string]TaskNotice, len(rows))
	for _, row := range rows {
		by[row.Node] = row
	}
	landed, broke := by["n1"], by["n2"]
	if landed.State != TaskDone || landed.Report != "it charges per seat" || landed.CostUSD != 0.25 {
		t.Fatalf("the landed worker came back as %+v", landed)
	}
	if landed.Stopped {
		t.Fatalf("work that finished came back stopped: %+v", landed)
	}
	if broke.State != TaskFailed || broke.Report != "the file was not there" {
		t.Fatalf("the broken worker came back as %+v", broke)
	}
	if broke.Stopped {
		t.Fatalf("a finding came back as a stop: %+v", broke)
	}
}

// AND THE NEXT RUN DOES NOT WEAR YESTERDAY'S NAME. The counter is a session's,
// so without a claim at recovery the first run of today would sit beside run 7
// as a second run 7 — and would write its workers' journals into run 7's folder.
func TestARunOfAResumedConversationDoesNotReuseARestoredRunsName(t *testing.T) {
	today, rows := resumedRunRows(t, func(family *orchestrateFamily) {
		family.upsert([]orchestrate.NodeStatus{
			node("n1", "tariff table", "You are reading the tariff table.", orchestrate.Running),
		})
	})
	if len(rows) == 0 {
		t.Fatal("nothing was restored to name a run past")
	}
	today.mu.Lock()
	seq := today.orchestrateSeq
	today.mu.Unlock()
	if seq < 7 {
		t.Fatalf("the run counter is at %d, so the next run would be called %d — a name already on the column", seq, seq+1)
	}
}

// ── the file ────────────────────────────────────────────────────────────────

// A CHECKPOINT WRITTEN BEFORE RUNS HAD ROWS LOADS EXACTLY AS IT ALWAYS DID. The
// version does not move for an added field, because a version that moved would
// throw away every graph written before this change.
func TestACheckpointWithNoRunRowsLoadsWholeAndUnchanged(t *testing.T) {
	journal, checkpoint := journalIn(t)
	writeCheckpoint(t, checkpoint, taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 2,
		Nodes: []taskRecord{{
			ID: 1, Title: "Fix the reconciler", Brief: "the whole brief",
			Acceptance: "it builds", State: TaskDone, Report: "done",
		}},
	})
	document := readCheckpoint(t, checkpoint)
	if len(document.Nodes) != 1 || len(document.Runs) != 0 {
		t.Fatalf("an older checkpoint decoded as %+v", document)
	}

	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})
	graph := agent.graph()
	graph.mu.Lock()
	nodes, runs := len(graph.nodes), len(graph.runRuns)
	graph.mu.Unlock()
	if nodes != 1 || runs != 0 {
		t.Fatalf("an older checkpoint resumed with %d nodes and %d runs", nodes, runs)
	}
}

// AND A FILE WHERE ONE NUMBER NAMES TWO PIECES OF WORK IS NOT THIS STORE'S FILE.
// The ids are one space on purpose (a run's rows come from the graph's own
// counter), so a row wearing a node's id is a corruption a roster could not draw
// its way out of.
func TestACheckpointWhoseRunRowShadowsANodeIsRefused(t *testing.T) {
	_, err := decodeTasks(mustEncode(t, taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 2,
		Nodes: []taskRecord{{
			ID: 1, Title: "Fix the reconciler", Brief: "the whole brief",
			Acceptance: "it builds", State: TaskDone,
		}},
		Runs: []runRecord{{ID: 1, Run: "3", Title: "audit the pricing code", State: TaskDone}},
	}))
	if err == nil || !strings.Contains(err.Error(), "is also a node") {
		t.Fatalf("the store accepted a row wearing a node's id: %v", err)
	}
}

// AND THE ID COUNTER HAS TO COVER THE ROWS TOO, or the next run's first row
// would be minted with an id already on the column.
func TestACheckpointWhoseCounterIsBehindARunRowIsRefused(t *testing.T) {
	_, err := decodeTasks(mustEncode(t, taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 1,
		Runs: []runRecord{{ID: 9, Run: "3", Title: "audit the pricing code", State: TaskDone}},
	}))
	if err == nil || !strings.Contains(err.Error(), "id counter") {
		t.Fatalf("the store accepted a counter behind its rows: %v", err)
	}
}
