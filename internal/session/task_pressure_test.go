package session

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── the reservation: a fan wider than the machine ──────────────────────────
//
// Every machine below is stated rather than borrowed: sixteen gibibytes over
// eight cores, so a node nobody has measured yet is expected to need one
// core's share, two gibibytes (task_pressure.go's footprintLocked), and a floor
// of a gibibyte and a half. The fan these machines can carry at once is worked
// out from those figures in each test, never written down beside them.

const (
	testTotalMB = 16384
	testCores   = 8
	testFloorMB = 1536
	// testRestMB is what this process holds with nothing of the graph running.
	testRestMB = 400
	// fanWidth is the fan #878 measured: twenty parts handed out in one breath.
	// It is stated here rather than read off taskFanLimit so that these tests
	// keep asking what a fan WIDER THAN THE MACHINE does, whatever the cap on
	// one parent's fan-out becomes.
	fanWidth = 20
)

// testFootprintMB is one node's expected need on the stated machine, spelled
// the way the governor derives it.
const testFootprintMB = testTotalMB / testCores

// roomFor is a quiet machine whose available memory is exactly the floor plus
// `extra` footprints, so the fan it admits at once is 1 + extra: the first node
// is judged on the reading alone and every further one against the footprints
// already started.
func roomFor(extra int) machineReading {
	return machineReading{
		loadPerCore: 0.1,
		availableMB: testFloorMB + extra*testFootprintMB,
		totalMB:     testTotalMB,
		cores:       testCores,
		workMB:      testRestMB,
	}
}

// loaded is a machine too busy for anything to start, whatever its memory.
func loaded() machineReading {
	reading := roomFor(20)
	reading.loadPerCore = 3.0
	return reading
}

// fanRunner is a scripted runner that only records a start: every node it is
// handed stays running until the test settles it, ON THE TEST'S OWN GOROUTINE,
// so the frontier pass a settle causes has finished before the next line reads
// the graph.
type fanRunner struct {
	started chan uint64
}

func (r *fanRunner) run(node *TaskNode) { r.started <- node.id }

// settle lands one running node, exactly as a runner would, and turns the
// frontier on the lane it gave back.
func settle(graph *TaskGraph, id uint64) {
	node := graph.node(id)
	node.finish("done", nil, "", "")
	graph.complete(node, TaskDone)
}

// fanGraph is a governed graph over a stated machine with a scripted runner,
// ALONE IN A PROCESS OF ITS OWN, which is what [newTaskGraph] gives a graph
// nobody told about neighbours: the only lanes its governor counts are the ones
// this test starts (task_pressure.go's [TaskLanes]).
//
// ITS POLL NEVER FIRES INSIDE A TEST. The poll is the product's wake-up for a
// machine that got quieter by itself, and here every pass is one the test
// asks for, so a timer turning the frontier underneath it would be a second
// author of the same pass.
func fanGraph(t *testing.T, machine *fakeMachine) (*TaskGraph, *noticeLog) {
	t.Helper()
	return fanGraphIn(t, machine, NewTaskLanes())
}

// fanGraphIn is the same graph in a process a test names, so that two graphs
// can share one — two conversations in one codeaf, which is the whole scene
// issue #907 is about.
func fanGraphIn(t *testing.T, machine *fakeMachine, process *TaskLanes) (*TaskGraph, *noticeLog) {
	t.Helper()
	graph := newTaskGraph()
	graph.lanes = process
	log := &noticeLog{}
	runner := &fanRunner{started: make(chan uint64, 4*fanWidth)}
	graph.report = log.record
	graph.run = runner.run
	graph.governor = &admissionGovernor{
		maxLoad:   1.5,
		minFreeMB: testFloorMB,
		read:      machine.read,
		now:       machine.now,
	}
	graph.pollEvery = time.Hour
	return graph, log
}

// admitChildren hands out `count` parts under one parent, one admission each,
// which is the road a division takes ([Agent.startTheParts]).
func admitChildren(graph *TaskGraph, parent uint64, count int) []uint64 {
	var ids []uint64
	for index := 0; index < count; index++ {
		id := graph.reserve()
		ids = append(ids, id)
		graph.admit(id, taskSpec{
			title: fmt.Sprintf("part %d", index+1), brief: "b", acceptance: "a",
			parent: parent, depth: 1,
		})
	}
	return ids
}

// partition sorts nodes into the ones running and the ones still queued.
func partition(graph *TaskGraph, ids []uint64) (running, queued []uint64) {
	for _, id := range ids {
		switch graph.node(id).stateNow() {
		case TaskRunning:
			running = append(running, id)
		case TaskQueued:
			queued = append(queued, id)
		}
	}
	return running, queued
}

// A PARENT HANDS OUT A FAN AND THE MACHINE CARRIES WHAT IT CAN. Twenty parts are
// ready in one pass on a machine with room for exactly a few: that few start,
// every other one is held saying the machine is busy, and the held ones start
// as earlier ones settle or as a later reading shows room. This is issue #878's
// replication, and before the reservation every one of the twenty started in
// that pass against one reading taken before any of them existed.
func TestAFanWiderThanTheMachineStartsWhatItCanCarryAndHoldsTheRest(t *testing.T) {
	const extra = 3
	carried := 1 + extra
	machine := &fakeMachine{}
	machine.set(roomFor(20), true)
	graph, log := fanGraph(t, machine)

	// The parent starts, divides, and hands its lane back while it waits on its
	// parts, which is what a dividing worker does ([TaskGraph.park]).
	parent := admitNodes(graph, 1)[0]
	graph.park(graph.node(parent))

	// The parts are handed out while the machine is too busy for any of them,
	// so all twenty are ready and queued when the machine makes room.
	machine.set(loaded(), true)
	parts := admitChildren(graph, parent, fanWidth)
	if running, _ := partition(graph, parts); len(running) != 0 {
		t.Fatalf("%d parts started on a loaded machine", len(running))
	}

	machine.set(roomFor(extra), true)
	graph.runFrontier()

	running, queued := partition(graph, parts)
	if len(running) != carried {
		t.Fatalf("one pass over twenty ready parts started %d, want the %d this machine can carry", len(running), carried)
	}
	for _, id := range queued {
		if notice, _ := log.last(id); notice.Waiting != waitingMachineBusy {
			t.Fatalf("held part %d says %q, want %q", id, notice.Waiting, waitingMachineBusy)
		}
	}
	// AND THEY START IN THE ORDER THEY WERE HANDED OUT, because the frontier
	// walks its order and a reservation only ever says "not yet" to the tail.
	for index, id := range running {
		if id != parts[index] {
			t.Fatalf("the parts that started are %v, want the first %d of %v", running, carried, parts)
		}
	}

	// A PART SETTLING FREES ITS FOOTPRINT, and exactly one held part takes it.
	settle(graph, running[0])
	after, stillQueued := partition(graph, parts)
	if len(after) != carried || len(stillQueued) != len(queued)-1 {
		t.Fatalf("after one part settled %d run and %d wait, want %d and %d", len(after), len(stillQueued), carried, len(queued)-1)
	}

	// AND A LATER READING THAT SHOWS ROOM starts one more: somebody else's work
	// gave a footprint of memory back, which is the poll's reason to exist.
	machine.set(roomFor(extra+1), true)
	graph.runFrontier()
	if widened, _ := partition(graph, parts); len(widened) != carried+1 {
		t.Fatalf("a reading with one more footprint of room left %d running, want %d", len(widened), carried+1)
	}
}

// ONE READING, JUDGED ONCE PER ADMISSION. The host is read at most once a
// second, and a fan handed out inside that second is judged twenty times
// against one reading — each time with the nodes already started counted. This
// is the shape the defect had: one reading, one yes, twenty starts.
func TestOneReadingIsJudgedOncePerAdmissionWithTheStartedNodesCounted(t *testing.T) {
	const extra = 2
	machine := &fakeMachine{}
	machine.set(roomFor(extra), true)
	graph, log := fanGraph(t, machine)
	// A clock that stands still, so every pass inside the fan is served from the
	// one reading the first pass took.
	instant := time.Unix(0, 0)
	graph.governor.now = func() time.Time { return instant }

	parts := admitChildren(graph, 0, fanWidth)

	if reads := machine.readCount(); reads != 1 {
		t.Fatalf("twenty admissions inside one second read the host %d times, want once", reads)
	}
	running, queued := partition(graph, parts)
	if len(running) != 1+extra {
		t.Fatalf("one reading admitted %d of twenty, want %d", len(running), 1+extra)
	}
	if len(queued) != len(parts)-len(running) {
		t.Fatalf("%d parts are neither running nor queued", len(parts)-len(running)-len(queued))
	}
	for _, id := range queued {
		if words := log.waitings(id); len(words) != 1 || words[0] != waitingMachineBusy {
			t.Fatalf("held part %d was announced %v, want one %q", id, words, waitingMachineBusy)
		}
	}
}

// A READING THAT CATCHES UP COUNTS NOTHING TWICE. Once the running nodes'
// memory shows in the reading, MemAvailable has fallen by exactly what the
// visible half rose by, so the projection a new node is judged against is the
// same one it was before the reading caught up — and a node settling then
// frees exactly one footprint of room, not less.
func TestAReadingThatShowsTheRunningNodesReleasesTheirReservation(t *testing.T) {
	machine := &fakeMachine{}
	governor := &admissionGovernor{minFreeMB: testFloorMB, read: machine.read, now: machine.now}
	carried := func(running int) int {
		// How many more the governor would start beside `running` already started.
		more := 0
		for governor.admits(running + more) {
			more++
		}
		return more
	}

	const extra = 5
	machine.set(roomFor(extra), true)
	governor.observe(0)
	if got := carried(0); got != 1+extra {
		t.Fatalf("a quiet machine carries %d, want %d", got, 1+extra)
	}

	// Three nodes started, and the reading has not seen them yet: their three
	// footprints are reserved and three fewer start.
	const started = 3
	if got := carried(started); got != 1+extra-started {
		t.Fatalf("with %d started and unseen the machine carries %d more, want %d", started, got, 1+extra-started)
	}

	// The reading catches up: the three nodes hold their footprints in this
	// process's tree, and MemAvailable fell by the same amount.
	seen := roomFor(extra)
	seen.availableMB -= started * testFootprintMB
	seen.workMB += started * testFootprintMB
	machine.set(seen, true)
	governor.observe(started)
	if got := carried(started); got != 1+extra-started {
		t.Fatalf("once the reading shows the %d nodes the machine carries %d more, want the same %d — a reservation still held beside the memory it stood for", started, got, 1+extra-started)
	}

	// And one of them settles: its memory goes back to the machine and one more
	// footprint of room is there.
	settled := roomFor(extra)
	settled.availableMB -= (started - 1) * testFootprintMB
	settled.workMB += (started - 1) * testFootprintMB
	machine.set(settled, true)
	governor.observe(started - 1)
	if got := carried(started - 1); got != 1+extra-started+1 {
		t.Fatalf("after one settled the machine carries %d more, want %d", got, 1+extra-started+1)
	}
}

// THE FOOTPRINT IS MEASURED, and it grows when this session's nodes turn out to
// be heavier than a core's share: the next fan is judged at what the last one
// was seen to need.
func TestTheFootprintGrowsToWhatThisSessionsNodesWereSeenToHold(t *testing.T) {
	machine := &fakeMachine{}
	governor := &admissionGovernor{minFreeMB: testFloorMB, read: machine.read, now: machine.now}
	machine.set(roomFor(12), true)
	governor.observe(0)
	governor.mu.Lock()
	prior := governor.footprintLocked()
	governor.mu.Unlock()
	if prior != testFootprintMB {
		t.Fatalf("an unmeasured node is expected to need %d MiB, want one core's share, %d", prior, testFootprintMB)
	}

	// Two nodes running and holding three core-shares each, visibly.
	const heavy = 3 * testFootprintMB
	heavyReading := roomFor(12)
	heavyReading.workMB += 2 * heavy
	machine.set(heavyReading, true)
	governor.observe(2)
	governor.mu.Lock()
	measured := governor.footprintLocked()
	governor.mu.Unlock()
	if measured != heavy {
		t.Fatalf("two nodes holding %d MiB each left a footprint of %d, want %d", heavy, measured, heavy)
	}

	// AND IT DOES NOT FALL BACK when the nodes are between builds: the room is
	// kept for the next build, which is when it will be needed.
	machine.set(roomFor(12), true)
	governor.observe(2)
	governor.mu.Lock()
	kept := governor.footprintLocked()
	governor.mu.Unlock()
	if kept != heavy {
		t.Fatalf("a quiet reading lowered the footprint to %d, want the %d already seen", kept, heavy)
	}
}

// The memory half OFF is the reservation off: the person turned off the floor
// the reservation protects, so twenty ready nodes on a quiet machine all start.
func TestNoMemoryFloorMeansNoReservation(t *testing.T) {
	machine := &fakeMachine{}
	machine.set(roomFor(0), true)
	graph, _ := fanGraph(t, machine)
	graph.governor.minFreeMB = 0

	parts := admitChildren(graph, 0, fanWidth)
	if running, _ := partition(graph, parts); len(running) != len(parts) {
		t.Fatalf("with no memory floor %d of %d started", len(running), len(parts))
	}
}

// The tree the visible half reads is this process's own, and it is never
// nothing: the process reading it is resident.
func TestTheTreeReadingCountsThisProcess(t *testing.T) {
	if _, err := os.Stat("/proc/self/statm"); err != nil {
		t.Skip("this platform has no /proc; the governor never holds here")
	}
	if mb := treeResidentMB(os.Getpid()); mb <= 0 {
		t.Fatalf("this process's tree reads %d MiB resident", mb)
	}
}

// ── the law: no node starts without the governor being asked ──────────────
//
// THE LAW: EVERY START IS JUDGED BY THE GOVERNOR, ONE ADMISSION AT A TIME.
//
// The defect this guards against is a door that marks a node running on a
// reading of its own — or on none — which is exactly what #878 was: one
// reading, one yes, a whole fan started under it. So the check is on the
// SOURCE:
//
//   - a node is moved to TaskRunning in exactly one function, runFrontier;
//   - runFrontier asks holdOnStartingLocked about each node before that move;
//   - holdOnStartingLocked asks for the node road, and the run engine's
//     MayStart gate asks for the worker road. No other path asks the governor.
//
// A node BORN running is not a start and is named below with its reason.
//
// What the walk cannot see, said so nobody reads it as more than it is: it
// matches the constant TaskRunning on the right of a `.state` assignment and a
// method called on a `.governor` field, so a state set through a variable or a
// helper (a `node.setState(s)` that does not exist today) and a governor
// reached through an alias (`gov := g.governor; gov.admits(n)`) pass it
// unexamined, and those are for review to refuse.
var bornRunning = map[string]string{
	"standingWideWork": "the firing itself, already running in this process when its graph is built around it",
}

func TestEveryStartIsJudgedByTheGovernor(t *testing.T) {
	files := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package: %v", err)
	}
	starts := map[string]int{}
	callers := map[string]map[string]bool{"admits": {}, "observe": {}}
	asksHold := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(files, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			where := fn.Name.Name
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.AssignStmt:
					for index, lhs := range node.Lhs {
						if selector, ok := lhs.(*ast.SelectorExpr); ok && selector.Sel.Name == "state" &&
							index < len(node.Rhs) && isIdent(node.Rhs[index], "TaskRunning") {
							starts[where]++
						}
					}
				case *ast.KeyValueExpr:
					if isIdent(node.Key, "state") && isIdent(node.Value, "TaskRunning") {
						if _, named := bornRunning[where]; !named {
							t.Errorf("%s: %s builds a node already running, which no governor was asked about. "+
								"Admit it queued and let the frontier start it, or name it in bornRunning with the reason.",
								files.Position(node.Pos()), where)
						}
					}
				case *ast.CallExpr:
					selector, ok := node.Fun.(*ast.SelectorExpr)
					if !ok {
						break
					}
					if inner, ok := selector.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "governor" {
						if seen, watched := callers[selector.Sel.Name]; watched {
							seen[where] = true
						}
					}
					if where == "runFrontier" && selector.Sel.Name == "holdOnStartingLocked" {
						asksHold = true
					}
				}
				return true
			})
		}
	}
	if len(starts) != 1 || starts["runFrontier"] != 1 {
		t.Errorf("nodes are moved to TaskRunning in %v, want exactly once, in runFrontier: every start is the frontier's", starts)
	}
	if !asksHold {
		t.Error("runFrontier no longer asks holdOnStartingLocked before it starts a node")
	}
	for method, allowed := range map[string]map[string]bool{
		"admits":  {"holdOnStartingLocked": true, "MayStart": true},
		"observe": {"runFrontier": true, "MayStart": true},
	} {
		got := callers[method]
		if len(got) != len(allowed) {
			t.Errorf("the governor's %s is called from %v, want only %v", method, keys(got), keys(allowed))
			continue
		}
		for caller := range got {
			if !allowed[caller] {
				t.Errorf("the governor's %s is called from %v, want only %v", method, keys(got), keys(allowed))
			}
		}
	}
}

func isIdent(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

func keys(set map[string]bool) []string {
	var out []string
	for key := range set {
		out = append(out, key)
	}
	return out
}
