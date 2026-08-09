package resident

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// span is when one leaf's execution actually held the machine. Overlap is the
// only evidence that separates a graph from a queue, and it cannot be read off
// counts — three jobs that each ran for ten seconds look identical whether they
// ran together or in single file. So the fake client records the clock.
type span struct {
	id    string
	start time.Time
	end   time.Time
}

// peakConcurrency is the largest number of spans alive at one instant. It is
// computed by sweeping the edges rather than by sampling, so a short overlap is
// not missed by a slow poll.
func peakConcurrency(spans []span) int {
	type edge struct {
		at    time.Time
		delta int
	}
	edges := make([]edge, 0, 2*len(spans))
	for _, s := range spans {
		edges = append(edges, edge{s.start, 1}, edge{s.end, -1})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].at.Equal(edges[j].at) {
			// Ends settle before starts at an identical instant, so two leaves
			// that merely touched are never counted as having overlapped.
			return edges[i].delta < edges[j].delta
		}
		return edges[i].at.Before(edges[j].at)
	})
	live, peak := 0, 0
	for _, e := range edges {
		live += e.delta
		if live > peak {
			peak = live
		}
	}
	return peak
}

// recordingRunner is the scripted client the flow tests drive: every execution
// takes a known amount of wall time and says exactly when it held it.
func recordingRunner(t *testing.T, graph *store.Store, hold time.Duration, workers int) (*Runner, func() []span) {
	t.Helper()
	var mu sync.Mutex
	var spans []span
	runner := NewRunner(graph, func(ctx context.Context, node store.Node) (ExecResult, error) {
		start := time.Now()
		select {
		case <-ctx.Done():
		case <-time.After(hold):
		}
		mu.Lock()
		spans = append(spans, span{id: node.ID, start: start, end: time.Now()})
		mu.Unlock()
		return ExecResult{Summary: "did " + node.Brief}, nil
	}, "flow", workers)
	return runner, func() []span {
		mu.Lock()
		defer mu.Unlock()
		return append([]span(nil), spans...)
	}
}

// Three things said in one breath are three jobs, and three jobs are supposed to
// be the parallelism the product sells instead of terminal tabs. Measured in the
// field they were a queue: a concurrency factor — summed job seconds over the
// seconds the person waited — of 0.32 to 0.76, which is worse than serial,
// because the jobs ran one after another AND left gaps between them.
//
// The assertion is overlap in time rather than a count of anything, because a
// count cannot tell the two apart.
func TestIndependentJobsRunSideBySide(t *testing.T) {
	graph := openRunnerStore(t)
	const jobs = 3
	const hold = 300 * time.Millisecond
	for index := range jobs {
		id := fmt.Sprintf("job-%d", index)
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
			{ID: id, Brief: "independent errand " + id, Stage: 1},
		}}, store.Provenance{
			Origin: store.OriginUser, SessionID: "flow", Intent: "independent errand " + id,
		}); err != nil {
			t.Fatal(err)
		}
	}

	runner, spans := recordingRunner(t, graph, hold, 8)
	// The host is reported ten times over its ceiling on purpose. Back-pressure
	// is a reason to stop growing a fan-out; it was silently a reason to abolish
	// one, and that is the shape the field measurement had.
	runner.WithGovernor(executor.NewGovernorFrom(func() (float64, bool) {
		return executor.GovernorLoadCeiling * 10, true
	}))

	wall := time.Now()
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	runner.Wait()
	elapsed := time.Since(wall)

	recorded := spans()
	if len(recorded) != jobs {
		t.Fatalf("ran %d leaves, want %d", len(recorded), jobs)
	}
	if peak := peakConcurrency(recorded); peak < 2 {
		t.Fatalf("peak concurrent leaves = %d, want at least 2 — the jobs ran as a queue", peak)
	}
	// And the wall clock agrees with the spans. Serial would be jobs*hold; the
	// bar is deliberately loose enough to survive a slow machine and tight
	// enough that single file cannot pass it.
	if serial := jobs * hold; elapsed >= serial-hold/2 {
		t.Fatalf("three independent jobs took %s, want meaningfully under the serial %s", elapsed, serial)
	}
}

// The other half of the same promise: parallelism is the graph's to give, and
// a job's own edges are the graph saying no. Order inside one job survives
// everything done to make different jobs overlap.
func TestAJobsOwnDependenciesStillRunInOrder(t *testing.T) {
	graph := openRunnerStore(t)
	// Two independent jobs beside an ordered one, so the ordered job is proved
	// under exactly the conditions that made the others concurrent.
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "write", Brief: "write it up", Stage: 2,
			Needs: []store.Need{{NodeID: "gather", Kind: store.FeedsInto}}},
		{ID: "gather", Parent: "write", Brief: "gather the facts", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "flow", Intent: "ordered job"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"loose-a", "loose-b"} {
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
			{ID: id, Brief: "unrelated " + id, Stage: 1},
		}}, store.Provenance{Origin: store.OriginUser, SessionID: "flow", Intent: "unrelated " + id}); err != nil {
			t.Fatal(err)
		}
	}

	runner, spans := recordingRunner(t, graph, 100*time.Millisecond, 8)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := runner.Tick(context.Background()); err != nil {
			t.Fatalf("tick: %v", err)
		}
		runner.Wait()
		node, found, err := graph.Node("write")
		if err != nil {
			t.Fatal(err)
		}
		if found && node.Status == store.Done {
			break
		}
	}

	var gather, write span
	for _, s := range spans() {
		switch s.id {
		case "gather":
			gather = s
		case "write":
			write = s
		}
	}
	if gather.end.IsZero() || write.start.IsZero() {
		t.Fatalf("the ordered job did not run both of its leaves: %+v", spans())
	}
	if write.start.Before(gather.end) {
		t.Fatalf("the consumer started at %s, before its producer finished at %s",
			write.start, gather.end)
	}
}

// The dispatch loop must survive a store that answers badly once. It used to
// return on the first error, which killed claiming for the life of the process
// while everything else — the reconciler, the board, the lease — carried on
// looking healthy. The field signature was a compiled leaf sitting pending with
// an empty started_at for fifteen minutes.
func TestATransientClaimFailureDoesNotEndDispatchForever(t *testing.T) {
	graph := openRunnerStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "survivor", Brief: "the work nobody claimed", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "flow", Intent: "the work nobody claimed"}); err != nil {
		t.Fatal(err)
	}
	runner, spans := recordingRunner(t, graph, time.Millisecond, 2)

	// One pass faults before it can claim anything. tickGuarded records it and
	// the loop must keep its nerve; the next pass reads the same durable graph.
	faulted := false
	runner.WithGovernor(executor.NewGovernorFrom(func() (float64, bool) {
		if !faulted {
			faulted = true
			panic("the host reading blew up")
		}
		return governorCalmLoad, true
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- runner.Serve(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(spans()) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(spans()) == 0 {
		t.Fatal("a single faulting pass ended claiming for good")
	}
	cancel()
	<-served
}

// The other half of the gaps, and the one no worker pool could have closed: the
// second job did not exist yet.
//
// Commands were applied strictly one at a time, and a splice is three model
// round-trips — compile, plan, title — before its work is a single node in the
// graph. One sentence naming three things therefore started its jobs staggered
// by the full planning latency of every job ahead of them, and that stagger is
// pure dead wall time in which the graph has nothing to run. It is the same
// single-file queue that made a redirect arrive after the leaf it was aimed at.
func TestIndependentAsksArePlannedSideBySide(t *testing.T) {
	graph := openStore(t)
	const asks = 3
	const planning = 300 * time.Millisecond

	var mu sync.Mutex
	var spans []span
	compile := func(_ context.Context, instruction, _ string) (Compiled, error) {
		return Compiled{Goal: instruction}, nil
	}
	plan := func(ctx context.Context, compiled Compiled) (store.Subtree, error) {
		start := time.Now()
		select {
		case <-ctx.Done():
		case <-time.After(planning):
		}
		mu.Lock()
		spans = append(spans, span{id: compiled.Goal, start: start, end: time.Now()})
		mu.Unlock()
		return store.Subtree{Nodes: []store.NodeSpec{
			{ID: "task-" + compiled.Goal, Brief: compiled.Goal, Stage: 1},
		}}, nil
	}
	reconciler := New(graph, compile, plan)

	for index := range asks {
		if _, err := graph.RequestCommand(store.Command{
			SessionID: "flow", Kind: store.CommandSplice,
			Instruction: fmt.Sprintf("thing%d", index),
		}); err != nil {
			t.Fatal(err)
		}
	}

	wall := time.Now()
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	elapsed := time.Since(wall)

	mu.Lock()
	planned := append([]span(nil), spans...)
	mu.Unlock()
	if len(planned) != asks {
		t.Fatalf("planned %d asks, want %d", len(planned), asks)
	}
	if peak := peakConcurrency(planned); peak < 2 {
		t.Fatalf("peak concurrent plans = %d, want at least 2 — the asks were planned in single file", peak)
	}
	if serial := asks * planning; elapsed >= serial-planning/2 {
		t.Fatalf("planning three asks took %s, want meaningfully under the serial %s", elapsed, serial)
	}

	// Concurrent application, ordered settlement: every ask is resolved, and the
	// nodes all exist. Receipts stay in the order the person said them.
	for index := range asks {
		id := fmt.Sprintf("task-thing%d", index)
		node, found, err := graph.Node(id)
		if err != nil || !found {
			t.Fatalf("node %s: found=%v err=%v", id, found, err)
		}
		if node.Status != store.Pending {
			t.Fatalf("node %s = %s, want pending and claimable", id, node.Status)
		}
	}
	pending, err := graph.PendingCommands(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("%d commands were left unresolved by a concurrent pass", len(pending))
	}
}

// The whole Tier B stall, end to end, from the one input that caused it.
//
// A compiler answered builds_on:["root"]. Continuity accepted it because a node
// by that name existed, and wired a feeds_into edge from the permanent spine —
// which is Running by construction and settles never. The job compiled cleanly,
// spliced cleanly, and was deadlocked from birth: pending, empty started_at, for
// the entire 900-second ceiling, while the resident ticked beside it once a
// second with nothing it was allowed to claim.
//
// The assertion is the only one that matters: the work runs. A test that merely
// checked the edge was absent would pass on a graph that still never moved.
func TestAJobWhoseCompilerNamedTheSpineStillRuns(t *testing.T) {
	graph := openStore(t)
	compile := func(_ context.Context, instruction, _ string) (Compiled, error) {
		// Verbatim the shape that wedged it.
		return Compiled{Goal: instruction, BuildsOn: []string{store.RootID}}, nil
	}
	reconciler := New(graph, compile, func(context.Context, Compiled) (store.Subtree, error) {
		return store.Subtree{Nodes: []store.NodeSpec{
			{ID: "task-2", Brief: "the work that could never start", Stage: 1},
		}}, nil
	})
	if _, err := graph.RequestCommand(store.Command{
		SessionID: "flow", Kind: store.CommandSplice,
		Instruction: "fix the failing test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	// Ready is the whole diagnosis: it is what the dispatcher asks, and for 900
	// seconds it answered nothing.
	ready, err := graph.Ready(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 || ready[0].ID != "task-2" {
		t.Fatalf("ready = %+v, want the compiled leaf claimable", ready)
	}
	runner, spans := recordingRunner(t, graph, time.Millisecond, 2)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()
	if len(spans()) != 1 {
		t.Fatal("the leaf was spliced and never claimed — the job was deadlocked from birth")
	}
}

// Opening a chat must not wait out somebody else's planner.
//
// The reconciler's lock was taken for the whole of Tick, model calls included,
// and AttachSession takes that same lock on the chat-startup path. Arriving at
// the keyboard while any job was being compiled and planned therefore meant
// sitting through three model round-trips that had nothing to do with you — and
// on a slow provider that is the whole of the first impression.
func TestOpeningAChatDoesNotWaitOutAPlan(t *testing.T) {
	graph := openStore(t)
	planning := make(chan struct{})
	released := make(chan struct{})
	reconciler := New(graph,
		func(_ context.Context, instruction, _ string) (Compiled, error) {
			return Compiled{Goal: instruction}, nil
		},
		func(_ context.Context, compiled Compiled) (store.Subtree, error) {
			close(planning)
			<-released
			return store.Subtree{Nodes: []store.NodeSpec{{
				ID: "planned", Brief: compiled.Goal, Stage: 1,
			}}}, nil
		})
	if _, err := graph.RequestCommand(store.Command{
		SessionID: "chat", Kind: store.CommandSplice, Instruction: "do the slow thing",
	}); err != nil {
		t.Fatal(err)
	}

	ticked := make(chan error, 1)
	go func() { ticked <- reconciler.Tick(context.Background()) }()
	<-planning

	attached := make(chan error, 1)
	go func() { attached <- reconciler.AttachSession("chat") }()
	select {
	case err := <-attached:
		if err != nil {
			close(released)
			<-ticked
			t.Fatalf("attach session: %v", err)
		}
	case <-time.After(5 * time.Second):
		close(released)
		<-ticked
		t.Fatal("opening a chat waited out the planner")
	}

	close(released)
	if err := <-ticked; err != nil {
		t.Fatalf("tick: %v", err)
	}
	if _, found, err := graph.Node("planned"); err != nil || !found {
		t.Fatalf("planned node found=%t err=%v", found, err)
	}
}
