package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The task-scope tree (5.15), the focused card (5.9) and the live join
// (10.5 idea 15), driven through the real app.
//
// The fixture below is 5.15's own wireframe as a graph: a job with a settled
// part, a plan step whose worker is running, and a part sitting behind that step
// on a waits-on edge. Every law in this file is asserted against what the rail
// SAYS about it — the row values a reader sees, and the frame those rows draw.

// planBoard is a job with real depth, plus the permanent spine every store has.
//
//	wisp-parity            the job
//	  XhrSyn               settled part
//	  H2                   a plan step, pending itself…
//	    H2Probe            …with a worker running under it
//	  KeyCutter            queued behind H2
func planBoard() *boardBackend {
	started := fixedNow().Add(-28 * time.Minute)
	return &boardBackend{
		nodes: []store.Node{
			// The spine: `running` forever, and never a job (13.8 finding 4).
			{ID: store.RootID, Brief: "Permanent Aforge spine", Status: store.Running,
				StartedAt: fixedNow().Add(-72 * time.Hour), CreatedSeq: 1},
			{ID: "job-1", Parent: store.RootID, Title: "wisp-parity", Status: store.Running,
				CreatedSeq: 10, Summary: "reworking NavCtx after the worker died"},
			{ID: "job-1/xhr", Parent: "job-1", Title: "XhrSyn", Status: store.Done,
				CreatedSeq: 11, StartedAt: fixedNow().Add(-time.Hour)},
			{ID: "job-1/h2", Parent: "job-1", Title: "H2", Status: store.Pending, CreatedSeq: 12},
			{ID: "job-1/h2/probe", Parent: "job-1/h2", Title: "H2Probe", Status: store.Running,
				CreatedSeq: 13, StartedAt: started},
			{ID: "job-1/keycutter", Parent: "job-1", Title: "KeyCutter", Status: store.Pending,
				CreatedSeq: 14},
		},
		edges: []store.Edge{{From: "job-1/h2", To: "job-1/keycutter", Kind: store.Blocks}},
		usage: map[string]store.JobUsage{"job-1": {Cost: 8.65}},
		sessions: []store.Session{
			{ID: testSession, Title: "the wisp parity push"},
		},
	}
}

func planApp(t *testing.T) *App {
	t.Helper()
	app := newTestApp(planBoard(), &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	return app
}

// enterTask puts the keyboard on the map and descends into the one job.
// The digit is the home rail's own 1–9 jump, and the row it lands on is
// asserted rather than assumed.
func enterTask(t *testing.T, app *App) {
	t.Helper()
	press(app, "ctrl+o")
	press(app, "4") // aforge · the wisp parity push · + new room · wisp-parity
	if name := app.railModel.Selected().Name; name != "wisp-parity" {
		t.Fatalf("the jump landed on %q, not the task card", name)
	}
	press(app, "enter")
	if app.railModel.Depth() != 1 {
		t.Fatalf("enter did not re-scope: depth = %d", app.railModel.Depth())
	}
}

func taskRow(t *testing.T, app *App, name string) rail.Row {
	t.Helper()
	for _, row := range app.railModel.Rows() {
		if row.Name == name {
			return row
		}
	}
	t.Fatalf("no row named %q in %q", name, rowNames(app))
	return rail.Row{}
}

// 5.15: entering a task re-scopes the rail to that task's DAG — row 0 is the
// orchestrator, then the plan steps and workers AS AN INDENTED TREE. The indent
// is the structure: a worker under a step is one level in, and the job's own
// parts sit flush under the surface row, exactly as the wireframe draws them.
func TestTheTaskScopeIsAnIndentedTree(t *testing.T) {
	app := planApp(t)
	enterTask(t, app)

	want := []struct {
		name  string
		kind  rail.RowKind
		depth int
	}{
		{"wisp-parity", rail.RowSurface, 0},
		{"XhrSyn", rail.RowWorker, 0},
		{"H2", rail.RowStep, 0},
		{"H2Probe", rail.RowWorker, 1},
		{"KeyCutter", rail.RowWorker, 0},
	}
	rows := app.railModel.Rows()
	if len(rows) != len(want) {
		t.Fatalf("the task scope is %q", rowNames(app))
	}
	for i, expected := range want {
		if rows[i].Name != expected.name || rows[i].Kind != expected.kind ||
			rows[i].Depth != expected.depth {
			t.Fatalf("row %d is %q (%v, depth %d), want %q (%v, depth %d)",
				i, rows[i].Name, rows[i].Kind, rows[i].Depth,
				expected.name, expected.kind, expected.depth)
		}
	}
}

// 10.5 idea 15: "plan steps lit by live execution — our DAG knows the join
// exactly, so light the plan-step row accent while its worker runs". H2 is
// `pending` in the store and has a worker running under it; the row is lit.
//
// The join is the parent edge and only lifts a row out of QUEUED: KeyCutter is
// pending too, has nothing running under it, and stays a flag.
func TestARunningWorkerLightsItsPlanStep(t *testing.T) {
	app := planApp(t)
	enterTask(t, app)

	step := taskRow(t, app, "H2")
	if step.Life != rail.LifeWorking || step.Attention() != rail.AttnWorking {
		t.Fatalf("the step with a running worker reads %v/%v", step.Life, step.Attention())
	}
	if step.State() != tokens.StateLive {
		t.Fatal("a lit step does not carry the live accent (8.1.6)")
	}
	blocked := taskRow(t, app, "KeyCutter")
	if blocked.Life != rail.LifeQueued || blocked.Attention() != rail.AttnWaitsOn {
		t.Fatalf("a step with nothing running under it reads %v/%v",
			blocked.Life, blocked.Attention())
	}
	settled := taskRow(t, app, "XhrSyn")
	if settled.Life != rail.LifeSettled {
		t.Fatalf("the settled part was re-lit: %v", settled.Life)
	}
}

// 5.15's wireframe spends the right of a tree row on the clock (`◐ H2 28m`) and
// leaves the settled row bare. A step with no clock of its own borrows the
// longest one under it, because that is the answer the reader was asking for.
func TestATreeRowCarriesItsClockAndNeverAnInventedNumber(t *testing.T) {
	app := planApp(t)
	enterTask(t, app)

	worker := taskRow(t, app, "H2Probe")
	if !worker.Meta.HasElapsed || worker.Meta.Elapsed != 28*time.Minute {
		t.Fatalf("the running worker's clock is %v (has=%v)",
			worker.Meta.Elapsed, worker.Meta.HasElapsed)
	}
	step := taskRow(t, app, "H2")
	if !step.Meta.HasElapsed || step.Meta.Elapsed != 28*time.Minute {
		t.Fatalf("the step did not borrow its worker's clock: %v", step.Meta)
	}
	if settled := taskRow(t, app, "XhrSyn"); settled.Meta.HasElapsed {
		t.Fatalf("a settled row still carries a running clock: %v", settled.Meta.Elapsed)
	}
	// The reads this seam cannot make are absent cells, never invented ones.
	if worker.Meta.HasCost || worker.Meta.ContextWindow > 0 || worker.Meta.Model != "" {
		t.Fatalf("a worker row invented telemetry the store cannot answer: %+v", worker.Meta)
	}
}

// The waits-on structure is what the task scope exists to make visible (5.15),
// and the room says it in words: `⚑ KeyCutter` / `waits on H2`.
func TestTheEnteredRoomDrawsTheWaitsOnEdgeInWords(t *testing.T) {
	app := planApp(t)
	enterTask(t, app)

	blocked := taskRow(t, app, "KeyCutter")
	if len(blocked.WaitsOn) != 1 || blocked.WaitsOn[0] != "H2" {
		t.Fatalf("the blocked row does not name what it waits on: %+v", blocked.WaitsOn)
	}
	frame := ansi.Strip(app.Frame(120, 30))
	if !strings.Contains(frame, "waits on H2") {
		t.Fatalf("the room drew no waits-on line:\n%s", frame)
	}
}

// 5.9's progressive disclosure, on the HOME card: collapsed is three lines, and
// the FOCUSED card expands IN PLACE into plan progress and per-worker rows.
// Full depth is still one room away — the card never becomes the room.
func TestAFocusedCardExpandsInPlaceAndCollapsesWhenTheCursorLeaves(t *testing.T) {
	app := planApp(t)
	press(app, "ctrl+o")
	press(app, "4")
	if name := app.railModel.Selected().Name; name != "wisp-parity" {
		t.Fatalf("the cursor is on %q", name)
	}

	card := app.railModel.Selected()
	if done, total := rail.StepProgress(card.Steps); done != 1 || total != 3 {
		t.Fatalf("the card's plan progress is %d/%d, want 1/3", done, total)
	}
	// The leaves, live work first: the two that are still moving, then history.
	if got := rowNamesOf(card.Workers); len(got) != 3 ||
		got[0] != "H2Probe" || got[1] != "KeyCutter" || got[2] != "XhrSyn" {
		t.Fatalf("the card expands into %q", got)
	}

	focused := ansi.Strip(app.Frame(120, 30))
	if !strings.Contains(focused, "1/3") {
		t.Fatalf("the focused card drew no plan progress:\n%s", focused)
	}
	if !strings.Contains(focused, "H2Probe") {
		t.Fatalf("the focused card drew no worker rows:\n%s", focused)
	}

	// Move off it: the card goes back to being three lines.
	press(app, "up")
	collapsed := ansi.Strip(app.Frame(120, 30))
	if strings.Contains(collapsed, "H2Probe") {
		t.Fatalf("an unfocused card kept its worker rows:\n%s", collapsed)
	}
	if !strings.Contains(collapsed, "wisp-parity") {
		t.Fatalf("the card itself went missing:\n%s", collapsed)
	}
}

// 13.8 finding 4: the rail and the head disagreed about what is running on one
// screen — rail "1 running · Permanent Aforge spine", head "nothing is running
// right now". The spine is a node with `status='running'` that never finishes.
// It is plumbing (5.14): not a card, and not a count.
func TestThePermanentSpineIsNeitherACardNorACount(t *testing.T) {
	backend := planBoard()
	// Only the spine and one settled job: a window where nothing is happening.
	backend.nodes = []store.Node{
		backend.nodes[0],
		{ID: "job-2", Parent: store.RootID, Title: "perf-audit", Status: store.Done, CreatedSeq: 5},
	}
	backend.edges = nil
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)

	if status := app.railModel.Rows()[0].Status; status != "nothing running" {
		t.Fatalf("the spine was counted as work: row 0 says %q", status)
	}
	frame := ansi.Strip(app.Frame(120, 30))
	for _, forbidden := range []string{"Permanent Aforge spine", store.RootID} {
		if strings.Contains(frame, forbidden) {
			t.Fatalf("the rail drew the spine (%q):\n%s", forbidden, frame)
		}
	}
	for _, row := range rowNames(app) {
		if row == "Permanent Aforge spine" {
			t.Fatalf("the spine has a card: %q", rowNames(app))
		}
	}
}

// Never on any row: node ids, seq numbers, journal internals (5.14) — including
// inside the entered room, where every row is a node.
func TestTheTaskTreeDrawsNoIdsAtAnyWidth(t *testing.T) {
	app := planApp(t)
	enterTask(t, app)
	for width := 1; width <= 160; width++ {
		frame := app.Frame(width, 24)
		for _, row := range strings.Split(frame, "\n") {
			if got := ansi.StringWidth(row); got > width {
				t.Fatalf("width %d produced a %d-cell row: %q", width, got, row)
			}
		}
		if width < 40 {
			continue
		}
		plain := ansi.Strip(frame)
		for _, id := range []string{"job-1", "job-1/h2", "job-1/keycutter", store.RootID,
			testSession} {
			if strings.Contains(plain, id) {
				t.Fatalf("width %d drew the id %q:\n%s", width, id, plain)
			}
		}
	}
}

// A deep scope must not cost a walk per row: the rollups are computed once per
// journal move, with the rest of the scope map.
func TestTheTreeCostsNoExtraReads(t *testing.T) {
	app := planApp(t)
	backend := app.backend.(*boardBackend)
	before := backend.reads
	enterTask(t, app)
	for i := 0; i < 20; i++ {
		app.Frame(120, 30)
		press(app, "j")
		press(app, "k")
	}
	if backend.reads > before+2 {
		t.Fatalf("walking the tree cost %d store reads", backend.reads-before)
	}
}
