package main

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/command"
	"github.com/Agent-Field/aforge-v2/internal/lease"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui"
)

// testBrains builds brains with no provider behind them. What promotion has to
// prove is which process runs a brain and that the brain's loops actually
// start — not what a head would have said, which needs a key and a network and
// says nothing about residency at all.
type testBrains struct {
	mu      sync.Mutex
	started int
}

func (b *testBrains) build(w *chatWindow, session string, hand resident.HandoverFunc) (*chatBrain, error) {
	reconciler := resident.New(w.graph, nil, nil).
		WithHandover(hand).
		WithResidentSince(time.Now())
	if err := reconciler.AttachSession(session); err != nil {
		return nil, err
	}
	runner := resident.NewRunner(w.graph, func(context.Context, store.Node) (resident.ExecResult, error) {
		return resident.ExecResult{}, nil
	}, "test-runner", 1)
	events := make(chan tui.StreamEvent, 1)
	brain := &chatBrain{
		window: w, session: session,
		commander: command.New(command.Options{
			Database: w.path, PrefsDir: w.dir, Store: w.graph,
			SessionID: session, StreamEvents: events,
		}),
		reconciler:   reconciler,
		runner:       runner,
		consent:      newConsentDesk(w.graph),
		streamEvents: events,
	}
	brain.serveHead = func(ctx context.Context) {
		b.mu.Lock()
		b.started++
		b.mu.Unlock()
		<-ctx.Done()
	}
	return brain, nil
}

func (b *testBrains) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.started
}

func testWindow(t *testing.T, root string) *chatWindow {
	t.Helper()
	window, err := openChatWindow(filepath.Join(root, "graph.db"), filepath.Join(root, "graph.db"), "new")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(window.close)
	return window
}

// testResidency is a window wired for a test's patience rather than a person's.
func testResidency(t *testing.T, window *chatWindow, brains *testBrains) *chatResidency {
	t.Helper()
	role := newChatResidency(window)
	role.build = brains.build
	role.probe = 0
	t.Cleanup(func() {
		// stop closes the window to any promotion in flight and waits for it,
		// which is exactly what the process exit path does.
		role.stop()
		if role.promotingNow() {
			t.Error("a promotion was still in flight when the window closed")
		}
	})
	return role
}

// until spins a poll until the window says what the test is waiting for. Every
// promotion in the product is driven by the surface's own poll, so a test that
// drove it any other way would be proving a path nobody takes.
func until(t *testing.T, role *chatResidency, want func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		role.Poll()
		if want() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the window never reached the state under test")
}

// The failure this whole mechanism exists for: a resident exits and the window
// still open beside it has to become the brain, without a restart.
func TestVisitorPromotesWhenTheResidentLetsGo(t *testing.T) {
	root := t.TempDir()
	window := testWindow(t, root)
	holder, heldBy, err := lease.AcquireResident(window.dir, "chat")
	if err != nil || holder == nil {
		t.Fatalf("could not stand in for the first window: %v %+v", err, heldBy)
	}

	brains := &testBrains{}
	role := testResidency(t, window, brains)
	if err := role.claim(); err != nil {
		t.Fatal(err)
	}
	if role.serving() {
		t.Fatal("a second window took the role while the first still held it")
	}
	state, _ := role.Poll()
	if !state.Visitor || state.PID == 0 {
		t.Fatalf("the visitor does not know who is resident: %+v", state)
	}
	if brains.count() != 0 {
		t.Fatal("a visitor started a brain")
	}

	if err := holder(); err != nil {
		t.Fatal(err)
	}
	until(t, role, func() bool { return role.serving() && brains.count() == 1 })
	state, _ = role.Poll()
	if state.Visitor {
		t.Fatalf("a promoted window still calls itself a visitor: %+v", state)
	}
	if !saidEventually(t, window, "Taking over as resident") {
		t.Fatal("the promotion happened without one line saying so")
	}
}

// Two windows watching the same free lock both try. The flock is the arbiter
// and nothing else is: one becomes the brain, the other stays a surface and
// re-points at the winner.
func TestOnlyOneOfTwoVisitorsWinsTheRole(t *testing.T) {
	root := t.TempDir()
	holder, _, err := lease.AcquireResident(filepath.Join(root), "chat")
	if err != nil || holder == nil {
		t.Fatalf("could not stand in for the first window: %v", err)
	}
	brains := &testBrains{}
	first := testResidency(t, testWindow(t, root), brains)
	second := testResidency(t, testWindow(t, root), brains)
	if err := first.claim(); err != nil {
		t.Fatal(err)
	}
	if err := second.claim(); err != nil {
		t.Fatal(err)
	}
	if err := holder(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		first.Poll()
		second.Poll()
		if first.serving() != second.serving() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if first.serving() == second.serving() {
		t.Fatalf("both windows are resident=%v", first.serving())
	}
	loser, winner := first, second
	if first.serving() {
		loser, winner = second, first
	}
	// The loser must not sit on a stale reading. Its next poll names the winner.
	until(t, loser, func() bool {
		state, _ := loser.Poll()
		return state.Visitor && state.PID > 0
	})
	if !winner.serving() {
		t.Fatal("the winner did not keep the role")
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && brains.count() < 1 {
		time.Sleep(5 * time.Millisecond)
	}
	if brains.count() != 1 {
		t.Fatalf("%d brains started for one role", brains.count())
	}
}

// A resident whose loop died holds the flock forever — it is a live process, so
// the lock cannot be taken from it. The heartbeat is what tells the difference,
// and it is the only path back from a binary too old to know about handovers.
func TestVisitorPromotesBesideAWedgedResident(t *testing.T) {
	root := t.TempDir()
	window := testWindow(t, root)
	holder, _, err := lease.AcquireResident(window.dir, "chat")
	if err != nil || holder == nil {
		t.Fatalf("could not stand in for the first window: %v", err)
	}
	defer holder()
	if err := lease.NoteResidentTick(window.dir, time.Now().Add(-2*lease.StuckAfter)); err != nil {
		t.Fatal(err)
	}

	brains := &testBrains{}
	role := testResidency(t, window, brains)
	if err := role.claim(); err != nil {
		t.Fatal(err)
	}
	until(t, role, func() bool { return role.serving() && brains.count() == 1 })
	// It is serving without the lease, because the wedged process still holds
	// it, and the thread says so rather than pretending the lock moved.
	if release := role.leaseHeld(); release {
		t.Fatal("the window claims a lease the wedged holder still owns")
	}
	if !saidEventually(t, window, "has not completed a pass since") {
		t.Fatal("promoting beside a wedged resident happened silently")
	}
}

// The version-aware path end to end: a resident that knows the verb hands the
// role over on request, and does it without taking the store down.
func TestResidentStandsDownWhenAskedToHandOver(t *testing.T) {
	root := t.TempDir()
	window := testWindow(t, root)
	brains := &testBrains{}
	role := testResidency(t, window, brains)
	if err := role.claim(); err != nil {
		t.Fatal(err)
	}
	if !role.serving() {
		t.Fatal("the first window on a store did not take the role")
	}

	command, err := window.graph.RequestCommand(store.Command{
		SessionID:   window.session,
		Kind:        store.CommandHandover,
		Instruction: "pid 999 opened this store with a newer build",
	})
	if err != nil {
		t.Fatal(err)
	}
	// The pass that read the request settles it and the standing down runs
	// beside that pass, so the two land in either order; both have to land.
	var settled store.Command
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		current, ok, err := window.graph.CommandBySeq(command.Seq)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("the handover command vanished")
		}
		settled = current
		if settled.Status != store.CommandPending && !role.serving() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if role.serving() {
		t.Fatal("the resident ignored a handover request")
	}
	if settled.Status != store.CommandApplied {
		t.Fatalf("handover settled as %s: %s", settled.Status, settled.Result)
	}
	// The lock is free the moment the role is given up, so the asking window
	// can promote on its very next poll.
	next, _, err := lease.AcquireResident(window.dir, "chat")
	if err != nil || next == nil {
		t.Fatalf("the demoted window did not let go of the lease: %v", err)
	}
	_ = next()
	if !saidEventually(t, window, "Handing the resident role over") {
		t.Fatal("the demotion happened without one line saying so")
	}
	state, _ := role.Poll()
	if !state.Visitor {
		t.Fatalf("a demoted window still calls itself the resident: %+v", state)
	}
}

// A request journaled before this process took the role was addressed to
// whoever was serving then. Answering it would make a freshly promoted window
// stand straight back down, and the role would circle the open windows forever.
func TestAHandoverAskedOfSomebodyElseIsRefused(t *testing.T) {
	root := t.TempDir()
	window := testWindow(t, root)
	command, err := window.graph.RequestCommand(store.Command{
		SessionID:   window.session,
		Kind:        store.CommandHandover,
		Instruction: "an ask for the process that used to be here",
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)

	brains := &testBrains{}
	role := testResidency(t, window, brains)
	if err := role.claim(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		settled, ok, err := window.graph.CommandBySeq(command.Seq)
		if err != nil {
			t.Fatal(err)
		}
		if ok && settled.Status != store.CommandPending {
			if settled.Status != store.CommandRejected {
				t.Fatalf("a stale handover was honoured: %s", settled.Status)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !role.serving() {
		t.Fatal("a freshly promoted window stood down for somebody else's ask")
	}
}

// leaseHeld says whether this window's residency is backed by the flock.
func (r *chatResidency) leaseHeld() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.release != nil
}

// saidEventually waits for a line, because the state change and the sentence
// about it are two writes and the test may look between them.
func saidEventually(t *testing.T, window *chatWindow, fragment string) bool {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if saidSomethingLike(t, window, fragment) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func saidSomethingLike(t *testing.T, window *chatWindow, fragment string) bool {
	t.Helper()
	messages, err := window.graph.Messages(window.session, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if strings.Contains(message.Body, fragment) {
			return true
		}
	}
	return false
}

// The whole journey the user asked for: rebuild, open a new window, and the new
// window is the brain a poll or two later while the old one quietly says it is
// second now. Both halves run here, over one store, exactly as two processes
// would — the flock does not care that they share an address space.
func TestANewerWindowTakesTheRoleFromAnOlderOne(t *testing.T) {
	root := t.TempDir()
	brains := &testBrains{}
	older := testResidency(t, testWindow(t, root), brains)
	if err := older.claim(); err != nil {
		t.Fatal(err)
	}
	if !older.serving() {
		t.Fatal("the first window on a store did not take the role")
	}

	newer := testResidency(t, testWindow(t, root), brains)
	// The rebuild, expressed the way the lock expresses it. Two windows in one
	// process are otherwise the same binary by construction, which is the one
	// thing a handover is never asked for.
	newer.stamp = lease.Build{ModTime: time.Now().Add(time.Hour), Size: 1, Revision: "rebuilt"}
	if err := newer.claim(); err != nil {
		t.Fatal(err)
	}
	if newer.serving() {
		t.Fatal("the second window took the role without asking")
	}

	until(t, newer, func() bool {
		return newer.serving() && !older.serving() && brains.count() == 2
	})
	state, _ := older.Poll()
	if !state.Visitor {
		t.Fatalf("the window that stood down still calls itself resident: %+v", state)
	}
	if !saidEventually(t, older.window, "Handing the resident role over") {
		t.Fatal("the old window stood down without saying so")
	}
	if !saidEventually(t, newer.window, "Taking over as resident") {
		t.Fatal("the new window took the role without saying so")
	}
}

// A resident that refuses says so, and the window that asked stops promising a
// handover is coming — the header is the only place a person finds out that the
// remedy is now closing the other window.
func TestARefusedHandoverIsSaidOutLoudInTheHeader(t *testing.T) {
	root := t.TempDir()
	window := testWindow(t, root)
	graph := window.graph
	// A resident that knows the verb and declines it.
	stubborn := resident.New(graph, nil, nil).
		WithHandover(func(resident.Handover) (bool, string) { return false, "this window is staying" }).
		WithResidentSince(time.Now().Add(-time.Minute))
	holder, _, err := lease.AcquireResident(window.dir, "chat")
	if err != nil || holder == nil {
		t.Fatalf("could not stand in for the first window: %v", err)
	}
	defer holder()

	role := testResidency(t, window, &testBrains{})
	role.stamp = lease.Build{ModTime: time.Now().Add(time.Hour), Size: 1, Revision: "rebuilt"}
	if err := role.claim(); err != nil {
		t.Fatal(err)
	}
	if state, _ := role.Poll(); !strings.Contains(state.Note, "asking pid") {
		t.Fatalf("the newer window did not ask: %+v", state)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if err := stubborn.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		if state, _ := role.Poll(); strings.Contains(state.Note, "would not hand over") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	state, _ := role.Poll()
	t.Fatalf("the refusal never reached the header: %+v", state)
}
