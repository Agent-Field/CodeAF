package session

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// noticeLog collects what a graph told the world, so the tests can assert on
// the wire a surface reads rather than on the fields behind it.
type noticeLog struct {
	mu      sync.Mutex
	notices []TaskNotice
}

// record is the graph's report hook. The notice is taken OUTSIDE the log's own
// lock for the reason the graph announces outside its own: node.notice() takes
// the graph's, and a test that inverted that order would deadlock the thing it
// is testing.
func (l *noticeLog) record(node *TaskNode) {
	notice := node.notice()
	l.mu.Lock()
	l.notices = append(l.notices, notice)
	l.mu.Unlock()
}

// last is the most recent notice about one node.
func (l *noticeLog) last(id uint64) (TaskNotice, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for index := len(l.notices) - 1; index >= 0; index-- {
		if l.notices[index].ID == id {
			return l.notices[index], true
		}
	}
	return TaskNotice{}, false
}

// waitings is every Waiting word this node was ever announced with, in order,
// so a test can assert that a hold was said once rather than on every pass.
func (l *noticeLog) waitings(id uint64) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var words []string
	for _, notice := range l.notices {
		if notice.ID == id {
			words = append(words, notice.Waiting)
		}
	}
	return words
}

// heldGraph is the shape every test below starts from: a scripted runner that
// parks until it is released, and a log of everything announced.
func heldGraph(t *testing.T) (*TaskGraph, *noticeLog, chan uint64, chan struct{}) {
	t.Helper()
	graph := newTaskGraph()
	log := &noticeLog{}
	started := make(chan uint64, 16)
	release := make(chan struct{})
	graph.report = log.record
	graph.run = func(node *TaskNode) {
		started <- node.id
		<-release
		node.finish("done", nil, "", "")
		node.graph.complete(node, TaskDone)
	}
	return graph, log, started, release
}

func admitNodes(graph *TaskGraph, count int) []uint64 {
	var ids []uint64
	for index := 0; index < count; index++ {
		id := graph.reserve()
		ids = append(ids, id)
		graph.admit(id, taskSpec{title: fmt.Sprintf("node %d", id), brief: "b", acceptance: "a"})
	}
	return ids
}

// NO CAP IS THE DEFAULT, and it means what it says: five independent nodes all
// work at once rather than two of them working and three watching.
func TestNoCapRunsTheWholeFrontierAtOnce(t *testing.T) {
	graph, _, started, release := heldGraph(t)
	defer close(release)

	const width = 5
	admitNodes(graph, width)
	for index := 0; index < width; index++ {
		waitStarted(t, started)
	}
	for _, id := range graph.order {
		if state := graph.node(id).stateNow(); state != TaskRunning {
			t.Fatalf("node %d is %q with no cap set, want every node running", id, state)
		}
	}
}

// A cap of one is the cap doing its job at its smallest: one node works and the
// rest are a queue, which is what the frontier held unconditionally before the
// number was anybody's to choose.
func TestTheCapIsTheSettingAndNotAConstant(t *testing.T) {
	graph, log, started, release := heldGraph(t)
	graph.limit = 1

	ids := admitNodes(graph, 3)
	waitStarted(t, started)
	for _, id := range ids[1:] {
		if state := graph.node(id).stateNow(); state != TaskQueued {
			t.Fatalf("node %d is %q behind a cap of 1, want queued", id, state)
		}
		notice, said := log.last(id)
		if !said || notice.Waiting != waitingSlot {
			t.Fatalf("node %d was announced waiting on %q, want %q", id, notice.Waiting, waitingSlot)
		}
	}

	close(release)
	for index := 0; index < 2; index++ {
		waitStarted(t, started)
	}
	for _, id := range ids {
		waitDoneNode(t, graph.node(id))
	}
	// AND THE HOLD LIFTS ON THE WIRE, not just in the scheduler: the update that
	// says the node is running says nothing about waiting.
	for _, id := range ids[1:] {
		if notice, _ := log.last(id); notice.Waiting != "" {
			t.Fatalf("node %d landed still saying it waits on %q", id, notice.Waiting)
		}
	}
}

// A node waiting on its own edges says NOTHING about waiting: DependsOn is
// already on the notice, and a second word for the same fact would be the wire
// saying it twice.
func TestADependentSaysNothingAboutWaiting(t *testing.T) {
	graph, log, started, release := heldGraph(t)
	defer close(release)

	first := graph.reserve()
	second := graph.reserve()
	graph.admit(first, taskSpec{title: "find it", brief: "b", acceptance: "a"})
	graph.admit(second, taskSpec{title: "fix it", brief: "b", acceptance: "a", dependsOn: []uint64{first}})

	waitStarted(t, started)
	notice, said := log.last(second)
	if !said {
		t.Fatal("the dependent was never announced queued")
	}
	if notice.Waiting != "" {
		t.Fatalf("a node waiting on its prerequisite said %q, want the edges to speak for it", notice.Waiting)
	}
}

// ── the admission governor ──────────────────────────────────────────────────

// fakeMachine is a host under the test's control: a reading it can change
// between passes, and a clock that always moves past the sample TTL so a
// changed machine is believed at once.
type fakeMachine struct {
	mu      sync.Mutex
	reading machineReading
	known   bool
	ticks   int
}

func (m *fakeMachine) read() (machineReading, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reading, m.known
}

func (m *fakeMachine) set(reading machineReading, known bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reading, m.known = reading, known
}

// now walks forward two TTLs per call, so nothing a test asks is answered out
// of the cache.
func (m *fakeMachine) now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ticks++
	return time.Unix(0, 0).Add(time.Duration(m.ticks) * 2 * taskPressureTTL)
}

func governedGraph(t *testing.T, machine *fakeMachine) (*TaskGraph, *noticeLog, chan uint64, chan struct{}) {
	t.Helper()
	graph, log, started, release := heldGraph(t)
	graph.governor = &admissionGovernor{
		maxLoad:   1.5,
		minFreeMB: 1536,
		read:      machine.read,
		now:       machine.now,
	}
	// The poll is the machine's only wake-up, and five real seconds is the right
	// cadence for a machine and the wrong one for a test suite.
	graph.pollEvery = 5 * time.Millisecond
	return graph, log, started, release
}

// A LOADED MACHINE HOLDS ADMISSION, and the node says so in the words a person
// reads rather than in the reading behind them.
func TestTheGovernorHoldsAdmissionAndSaysTheMachineIsBusy(t *testing.T) {
	machine := &fakeMachine{}
	machine.set(machineReading{loadPerCore: 3.0, availableMB: 8192}, true)
	graph, log, started, release := governedGraph(t, machine)
	defer close(release)

	ids := admitNodes(graph, 1)
	if state := graph.node(ids[0]).stateNow(); state != TaskQueued {
		t.Fatalf("a node admitted onto a loaded machine is %q, want queued", state)
	}
	notice, said := log.last(ids[0])
	if !said || notice.Waiting != waitingMachineBusy {
		t.Fatalf("the held node was announced waiting on %q, want %q", notice.Waiting, waitingMachineBusy)
	}

	// AND IT RELEASES WHEN THE PRESSURE DROPS. Nothing in this process caused
	// that — somebody else's build finished — so the poll is what notices.
	machine.set(machineReading{loadPerCore: 0.2, availableMB: 8192}, true)
	if id := waitStarted(t, started); id != ids[0] {
		t.Fatalf("the quiet machine started node %d, want %d", id, ids[0])
	}
	waitFor(t, "the started node to take back its machine-busy claim", func() bool {
		notice, _ := log.last(ids[0])
		return notice.State == TaskRunning && notice.Waiting == ""
	})
}

// The memory floor is the other half, and it holds on its own.
func TestTheGovernorHoldsOnTheMemoryFloorAlone(t *testing.T) {
	machine := &fakeMachine{}
	machine.set(machineReading{loadPerCore: 0.1, availableMB: 200}, true)
	graph, log, started, release := governedGraph(t, machine)
	defer close(release)

	ids := admitNodes(graph, 1)
	if state := graph.node(ids[0]).stateNow(); state != TaskQueued {
		t.Fatalf("a node admitted under the memory floor is %q, want queued", state)
	}
	if notice, _ := log.last(ids[0]); notice.Waiting != waitingMachineBusy {
		t.Fatalf("the held node said %q, want %q", notice.Waiting, waitingMachineBusy)
	}

	machine.set(machineReading{loadPerCore: 0.1, availableMB: 8192}, true)
	if id := waitStarted(t, started); id != ids[0] {
		t.Fatalf("the freed machine started node %d, want %d", id, ids[0])
	}
}

// SILENCE IS NEVER A HOLD. A platform this package cannot measure gets the
// scheduler it had before the governor existed.
func TestAHostThatCannotSayNeverHolds(t *testing.T) {
	machine := &fakeMachine{}
	machine.set(machineReading{}, false)
	graph, _, started, release := governedGraph(t, machine)
	defer close(release)

	admitNodes(graph, 2)
	waitStarted(t, started)
	waitStarted(t, started)
}

// A governor with both halves at zero is not a governor that always says yes —
// it is one that was never built.
func TestBothRowsAtZeroBuildsNoGovernorAtAll(t *testing.T) {
	if governor := newAdmissionGovernor(0, 0); governor != nil {
		t.Fatal("both checks off should leave no governor to consult")
	}
	if newAdmissionGovernor(0, 0).holds() {
		t.Fatal("a nil governor held admission")
	}
}

// The cap is asked BEFORE the machine, so a person who set a cap of one and is
// sitting on a loaded machine reads the reason they can do something about.
func TestTheCapOutranksTheMachineOnTheWire(t *testing.T) {
	machine := &fakeMachine{}
	machine.set(machineReading{loadPerCore: 3.0, availableMB: 8192}, true)
	graph, log, started, release := governedGraph(t, machine)
	defer close(release)
	graph.limit = 1

	// The first node starts before the machine is loaded, so the second is held
	// by the cap rather than by the load.
	machine.set(machineReading{loadPerCore: 0.1, availableMB: 8192}, true)
	ids := admitNodes(graph, 1)
	waitStarted(t, started)
	machine.set(machineReading{loadPerCore: 3.0, availableMB: 8192}, true)
	ids = append(ids, admitNodes(graph, 1)...)

	if notice, _ := log.last(ids[1]); notice.Waiting != waitingSlot {
		t.Fatalf("the second node said %q, want the cap's own word %q", notice.Waiting, waitingSlot)
	}
}

// ANNOUNCE ON CHANGE, which is Mending's discipline: a hold that has not moved
// is not news, and the frontier turns on every landing.
func TestAHoldThatHasNotMovedIsNotAnnouncedAgain(t *testing.T) {
	machine := &fakeMachine{}
	machine.set(machineReading{loadPerCore: 3.0, availableMB: 8192}, true)
	graph, log, _, release := governedGraph(t, machine)
	defer close(release)

	ids := admitNodes(graph, 1)
	// Several passes over a machine that has not changed its mind.
	for index := 0; index < 5; index++ {
		graph.runFrontier()
	}
	words := log.waitings(ids[0])
	if len(words) != 1 || words[0] != waitingMachineBusy {
		t.Fatalf("the held node was announced %v, want one %q and nothing after it", words, waitingMachineBusy)
	}
}

// ── the running half of Waiting ─────────────────────────────────────────────

// A RUNNING node parked on the provider says so, and takes it back when the
// call gets through. This is the signal [TaskNode.pacing] carries up from the
// retry loop (internal/provider's patience.go).
func TestARunningNodeSaysWhenTheProviderIsPacingIt(t *testing.T) {
	graph, log, started, release := heldGraph(t)
	defer close(release)

	ids := admitNodes(graph, 1)
	waitStarted(t, started)
	node := graph.node(ids[0])

	node.pacing(true)
	notice, _ := log.last(ids[0])
	if notice.State != TaskRunning || notice.Waiting != waitingRateLimited {
		t.Fatalf("a parked node announced %q/%q, want running and %q", notice.State, notice.Waiting, waitingRateLimited)
	}

	node.pacing(false)
	if notice, _ = log.last(ids[0]); notice.Waiting != "" {
		t.Fatalf("a node whose call got through still says %q", notice.Waiting)
	}
}

// A node is an agent and an agent can have more than one call out — a repair
// round beside an audit — so the node stops being paced when the LAST of them
// gets through, not when the first does.
func TestPacingIsCountedAndNotFlagged(t *testing.T) {
	graph, log, started, release := heldGraph(t)
	defer close(release)

	ids := admitNodes(graph, 1)
	waitStarted(t, started)
	node := graph.node(ids[0])

	node.pacing(true)
	node.pacing(true)
	node.pacing(false)
	if notice, _ := log.last(ids[0]); notice.Waiting != waitingRateLimited {
		t.Fatalf("one of two parked calls got through and the node said %q", notice.Waiting)
	}
	node.pacing(false)
	if notice, _ := log.last(ids[0]); notice.Waiting != "" {
		t.Fatalf("every call got through and the node still says %q", notice.Waiting)
	}
}

// A call that parked and was then cancelled must not announce anything about a
// node that has already landed.
func TestPacingSaysNothingAboutALandedNode(t *testing.T) {
	graph, log, started, release := heldGraph(t)

	ids := admitNodes(graph, 1)
	waitStarted(t, started)
	close(release)
	node := graph.node(ids[0])
	waitDoneNode(t, node)

	before := len(log.waitings(ids[0]))
	node.pacing(true)
	if after := len(log.waitings(ids[0])); after != before {
		t.Fatal("a landed node announced that one of its calls is waiting")
	}
	if notice, _ := log.last(ids[0]); notice.Waiting != "" {
		t.Fatalf("a landed node's notice says %q", notice.Waiting)
	}
}
