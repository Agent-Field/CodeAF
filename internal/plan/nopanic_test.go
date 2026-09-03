package plan

import (
	"bytes"
	"context"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The planner's concurrency is all the same bargain: N calls at once, a
// collector that already knows how to read a failed one, and a WaitGroup
// between them. A panicking call must arrive as that failure. The thing being
// held down here is not only "no crash" — it is that the collector still
// completes, because a fault that skipped a Done or left a slot unfilled would
// hang the plan, which is worse than the crash it replaced.

// faultingClient panics on the one call whose final user message carries
// panicOn, and answers every other call with reply.
type faultingClient struct {
	panicOn string
	reply   string
}

func (c *faultingClient) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	var user string
	for _, message := range messages {
		if message.Role != "system" {
			user = textOf(message)
		}
	}
	if c.panicOn != "" && strings.Contains(user, c.panicOn) {
		panic("planner call hit a nil map")
	}
	return response(c.reply), nil
}

// countingFaultClient faults on the call the test picks rather than on the
// prompt, for a pass whose N calls are byte-identical.
type countingFaultClient struct {
	faultOn func() bool
	reply   string
}

func (c *countingFaultClient) CompleteWithMessages(_ context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	if c.faultOn() {
		panic("planner call hit a nil map")
	}
	return response(c.reply), nil
}

func quietPlanLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := &bytes.Buffer{}
	flags, writer := log.Flags(), log.Writer()
	log.SetOutput(buffer)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(writer)
		log.SetFlags(flags)
	})
	return buffer
}

// finishes runs work and fails the test if it has not returned within the
// bound. Every test here would hang rather than fail if a wrapper dropped its
// completion obligation, and a hanging test says nothing until the package
// times out.
func finishes(t *testing.T, work func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		work()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the collector never finished: a fault left the group un-Done or a slot unfilled")
	}
}

// TestFanOutFaultBecomesOneFailedStage is the fan-out shape: results are
// indexed slots, and the faulting worker has to fill its own before Done.
func TestFanOutFaultBecomesOneFailedStage(t *testing.T) {
	logged := quietPlanLog(t)
	client := &faultingClient{
		panicOn: "stage 2",
		reply:   `{"parts":[{"title":"A part","summary":"It does one thing","sources":[]}]}`,
	}
	stages := []Stage{{Title: "One"}, {Title: "Two"}, {Title: "Three"}}

	var nodes []Node
	var usage Usage
	var err error
	finishes(t, func() {
		nodes, usage, err = FanOut(context.Background(), client, "Goal:\nship it", stages)
	})

	if err == nil {
		t.Fatal("a faulting stage produced no error")
	}
	if !strings.Contains(err.Error(), "internal fault in plan/fanout stage 2") {
		t.Fatalf("the error does not name the faulted stage: %v", err)
	}
	// The other two stages are untouched: a fault costs its own stage's nodes
	// and nothing else, exactly as a failed call does.
	if len(nodes) != 2 {
		t.Fatalf("kept %d nodes from the surviving stages, want 2", len(nodes))
	}
	if usage.Calls != 3 {
		t.Fatalf("usage.Calls = %d, want 3 — every stage is still accounted for", usage.Calls)
	}
	if !strings.Contains(logged.String(), `scope="plan/fanout stage 2"`) {
		t.Fatalf("the fault was not recorded to the log: %q", logged.String())
	}
}

// TestFanOutStillAnswersOrdinaryStages proves the guard changed nothing for a
// run that does not fault.
func TestFanOutStillAnswersOrdinaryStages(t *testing.T) {
	quietPlanLog(t)
	client := &faultingClient{reply: `{"parts":[{"title":"A part","summary":"It does one thing","sources":[]}]}`}
	nodes, usage, err := FanOut(context.Background(), client, "Goal:\nship it", []Stage{{Title: "One"}, {Title: "Two"}})
	if err != nil || len(nodes) != 2 || usage.Calls != 2 {
		t.Fatalf("ordinary fan-out = %d nodes, usage %+v, err %v", len(nodes), usage, err)
	}
	if nodes[0].Stage != 1 || nodes[1].Stage != 2 {
		t.Fatalf("stage ordering changed: %d then %d", nodes[0].Stage, nodes[1].Stage)
	}
}

// TestBriefFaultLandsAsAMissingBrief covers the other collector shape — a
// mutex, a map and an error slice written from the goroutine — which the
// ensemble's paired brief writer shares. A faulted brief leaves the leaf to the
// generic loop, which is what a failed one already does.
func TestBriefFaultLandsAsAMissingBrief(t *testing.T) {
	logged := quietPlanLog(t)
	graph := &Graph{Goal: "write it up", NextID: 1}
	graph.Add(Node{Stage: 1, Title: "Berlin", Summary: "the Berlin leg"})
	graph.Add(Node{Stage: 1, Title: "Lisbon", Summary: "the Lisbon leg"})
	client := &faultingClient{panicOn: "Lisbon", reply: `{"instruction":"Do the work and leave the result behind."}`}

	var usage Usage
	var err error
	finishes(t, func() {
		usage, err = Briefs(context.Background(), client, graph)
	})

	if err == nil || !strings.Contains(err.Error(), "internal fault in plan/brief node 2") {
		t.Fatalf("the faulted brief did not reach the caller as an error: %v", err)
	}
	if graph.Nodes[0].Brief == "" {
		t.Fatal("the healthy leaf lost its brief to the other one's fault")
	}
	if graph.Nodes[1].Brief != "" {
		t.Fatalf("the faulted leaf got a brief anyway: %q", graph.Nodes[1].Brief)
	}
	if usage.Calls != 1 {
		t.Fatalf("usage.Calls = %d, want the one call that answered", usage.Calls)
	}
	if !strings.Contains(logged.String(), `scope="plan/brief node 2"`) {
		t.Fatalf("the fault was not recorded to the log: %q", logged.String())
	}
}

// TestContractFaultDegradesToTheGenericLoop covers the appending collector: the
// faulting worker owns adding its own failed entry, because a result that is
// never appended is a node the pass forgets it ever asked about.
func TestContractFaultDegradesToTheGenericLoop(t *testing.T) {
	logged := quietPlanLog(t)
	graph := &Graph{Goal: "Repair the parser", NextID: 1}
	graph.Add(Node{Stage: 1, Title: "Parser checks", Summary: "Repair parser validation"})
	graph.Add(Node{Stage: 1, Title: "Lexer checks", Summary: "Repair lexer validation"})
	client := &faultingClient{panicOn: "Lexer checks", reply: `{"contract":"Run the focused checks first."}`}

	var err error
	finishes(t, func() {
		_, err = Contracts(context.Background(), client, graph, nil)
	})

	if err == nil || !strings.Contains(err.Error(), "internal fault in plan/contract node 2") {
		t.Fatalf("the faulted contract did not reach the caller as an error: %v", err)
	}
	if graph.Nodes[0].Contract == "" {
		t.Fatal("the healthy node lost its contract to the other one's fault")
	}
	if graph.Nodes[1].Contract != "" {
		t.Fatalf("the faulted node got a contract anyway: %q", graph.Nodes[1].Contract)
	}
	if !strings.Contains(logged.String(), `scope="plan/contract node 2"`) {
		t.Fatalf("the fault was not recorded to the log: %q", logged.String())
	}
}

// TestSpineProgressFaultDoesNotWedgeTheSamples is the deadlock this sweep could
// have introduced. The progress callback belongs to the caller; it runs under
// the mutex that counts completed samples, and with the panic absorbed rather
// than fatal, a mutex left locked would park every other sample forever. The
// samples themselves landed before the tick, so the spine still comes back.
func TestSpineProgressFaultDoesNotWedgeTheSamples(t *testing.T) {
	logged := quietPlanLog(t)
	client := &faultingClient{reply: `{"stages":[{"title":"One","summary":"first"},{"title":"Two","summary":"second"}]}`}
	progress := func(ProgressUpdate) { panic("the surface went away mid-plan") }

	var choice *SpineChoice
	var err error
	finishes(t, func() {
		choice, _, err = spineWithProgress(context.Background(), client, "ship it", "", nil, Measurement{}, 3, progress)
	})

	if err != nil {
		t.Fatalf("a faulting progress tick cost the plan its spine: %v", err)
	}
	if choice == nil || len(choice.Stages) != 2 || choice.Samples != 3 {
		t.Fatalf("spine choice = %+v, want all three samples kept", choice)
	}
	if !strings.Contains(logged.String(), `scope="plan/spine sample`) {
		t.Fatalf("the fault was not recorded to the log: %q", logged.String())
	}
}

// TestSpineSampleFaultLeavesTheOthersToChooseFrom is the same slot from the
// other side: a call that faults before it lands is one fewer candidate, and
// the medoid is taken over what did come back.
func TestSpineSampleFaultLeavesTheOthersToChooseFrom(t *testing.T) {
	logged := quietPlanLog(t)
	calls := 0
	var mutex sync.Mutex
	client := &countingFaultClient{reply: `{"stages":[{"title":"One","summary":"first"}]}`, faultOn: func() bool {
		mutex.Lock()
		defer mutex.Unlock()
		calls++
		return calls == 1
	}}

	var choice *SpineChoice
	var err error
	finishes(t, func() {
		choice, _, err = spineWithProgress(context.Background(), client, "ship it", "", nil, Measurement{}, 3, nil)
	})

	if err != nil {
		t.Fatalf("two good samples should still choose a spine: %v", err)
	}
	if choice == nil || choice.Samples != 2 {
		t.Fatalf("spine choice = %+v, want the two samples that survived", choice)
	}
	if !strings.Contains(logged.String(), `scope="plan/spine sample`) {
		t.Fatalf("the fault was not recorded to the log: %q", logged.String())
	}
}
