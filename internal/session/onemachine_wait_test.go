package session

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/taxonomy"
)

// THE ONE MACHINE THAT KEEPS CUTTING IS WAITED ON, NOT HAMMERED (#1358).
//
// A person on their own base url has one server and no chain behind it, and the
// policy answers a cut from it with the one retry that has no bound
// (internal/taxonomy's waitsForEver) and a wait that climbs to
// [taxonomy.OneMachineCutCeiling]. The turn loop used to answer every cut before
// it reached the wait: forty cuts were forty-one requests in about a
// millisecond, with no wait between any two of them and no line telling the
// person what was happening. The verdict's wait is now honoured whatever shape
// produced it, and the status line says how long it has been asking.
func TestAOneMachineThatKeepsCuttingIsWaitedOnAndSaysSo(t *testing.T) {
	log := watchPhases(t)

	// THE TURN'S CLOCK IS A FICTION THAT RECORDS EVERY WAIT, so the schedule is
	// asserted exactly and costs no real time.
	var (
		mu    sync.Mutex
		waits []time.Duration
		moved atomic.Int64
	)
	base := time.Now()
	previousNow, previousWait := turnNow, turnBackoff
	turnNow = func() time.Time { return base.Add(time.Duration(moved.Load())) }
	turnBackoff = func(ctx context.Context, delay time.Duration) error {
		mu.Lock()
		waits = append(waits, delay)
		mu.Unlock()
		moved.Add(int64(delay))
		return ctx.Err()
	}
	t.Cleanup(func() { turnNow, turnBackoff = previousNow, previousWait })

	const cuts = 40
	steps := make([]step, 0, cuts+1)
	for i := 0; i < cuts; i++ {
		reason := provider.CutSilent
		if i%2 == 1 {
			reason = provider.CutBabble
		}
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return nil, &provider.StreamCut{Reason: reason, OneMachine: true}
		})
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("the machine came back"), nil
	})
	completer := &scriptedCompleter{steps: steps}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Interactive = true })
	collected := collect(t, mustSubmit(t, agent, "go on"))

	if failure, failed := firstOfKind(collected, EventError); failed {
		t.Fatalf("the turn ended while the person was still watching it wait: %v", failure.Err)
	}
	if got := completer.requests(); got != cuts+1 {
		t.Fatalf("requests = %d, want every cut asked again and the answer", got)
	}

	mu.Lock()
	got := append([]time.Duration(nil), waits...)
	mu.Unlock()
	if len(got) != cuts {
		t.Fatalf("waits = %d (%v), want one in front of every ask after a cut", len(got), got)
	}
	for i, wait := range got {
		if wait <= 0 {
			t.Fatalf("wait %d was %v: a cut from the one machine was asked again at once (%v)", i+1, wait, got)
		}
		if wait > taxonomy.OneMachineCutCeiling {
			t.Fatalf("wait %d was %v, past the ceiling of %v", i+1, wait, taxonomy.OneMachineCutCeiling)
		}
		if i > 0 && wait < got[i-1] {
			t.Fatalf("wait %d (%v) is shorter than the one before it (%v): the schedule climbs and holds", i+1, wait, got[i-1])
		}
	}
	if last := got[len(got)-1]; last != taxonomy.OneMachineCutCeiling {
		t.Fatalf("the last wait was %v, want the schedule held at the ceiling %v", last, taxonomy.OneMachineCutCeiling)
	}

	// AND THE LINE SAYS HOW LONG IT HAS BEEN ASKING, while it asks.
	var said []string
	for _, news := range log.all() {
		if news.Phase == provider.PhaseRetrying {
			said = append(said, news.Detail)
		}
	}
	if len(said) == 0 {
		t.Fatalf("no retrying phase was told while the one machine was waited on")
	}
	want := waitingOnOneMachine(cuts, 0)
	want = want[:strings.Index(want, " in ")]
	found := false
	for _, detail := range said {
		if strings.HasPrefix(detail, want) && strings.HasSuffix(detail, "· still asking · esc stops") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no phase said %q…; the details were %q", want, said)
	}
}
