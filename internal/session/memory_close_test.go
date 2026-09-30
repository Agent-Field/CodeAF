package session

import (
	"context"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── NO MEMORY PASS OUTLIVES THE SESSION ─────────────────────────────────────
//
// The door that owns the brain closes it the moment [Agent.Close] returns, and
// a test's TempDir cleanup removes the folder the moment after that. So a pass
// still reading or writing the store when Close returns is a writer inside a
// folder that is being removed, which is how
// TestANeighbourTurnsTheWriteIntoADecision failed once on PR #1658's gate
// (`TempDir RemoveAll cleanup: ... directory not empty`) with its own assertion
// green.
//
// THESE HOLD THE PASS OPEN RATHER THAN RACE IT. A gate the fixture owns stands
// between the pass and its write, the bubble's clock only moves when every
// goroutine in it is blocked, and [synctest.Wait] is what says "Close has had
// every chance to return". Neither test depends on which goroutine the
// scheduler happens to run first, which is the only way a failure seen once in
// hundreds of loaded runs can be made to fail every time.

// heldReflex answers a turn and its reflexes, and holds exactly one of them —
// the router or the extractor — until the test opens its gate.
//
// THE HELD CALL IGNORES ITS CONTEXT ON PURPOSE. It stands for a reading already
// on its way back when Close begins, past anything a cancel can reach, which is
// the window the store write sits in.
type heldReflex struct {
	holdRoute bool
	gate      chan struct{}
	entered   chan struct{}
	once      sync.Once
}

func newHeldReflex(holdRoute bool) *heldReflex {
	return &heldReflex{holdRoute: holdRoute, gate: make(chan struct{}), entered: make(chan struct{})}
}

func (h *heldReflex) hold() {
	h.once.Do(func() { close(h.entered) })
	<-h.gate
}

func (h *heldReflex) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	system := ""
	if len(messages) > 0 {
		system = messageText(messages[0])
	}
	switch {
	case strings.Contains(system, "memory router"):
		if h.holdRoute {
			h.hold()
			return textResponse(`{"inject":[],"cmd":{"name":"remember","arg":"the release train leaves on Thursdays"}}`), nil
		}
		return textResponse(`{"inject":[],"cmd":null}`), nil
	case strings.Contains(system, "worth remembering after this session ends"):
		if !h.holdRoute {
			h.hold()
			return textResponse(`{"mem":1,"type":"fact","scope":"project","title":"release train","text":"the release train leaves on Thursdays","tags":[]}`), nil
		}
		return textResponse(`{"mem":0}`), nil
	}
	return textResponse("done"), nil
}

func TestClosedSessionHasNoMemoryPassStillWritingTheStore(t *testing.T) {
	for _, probe := range []struct {
		name string
		// holdRoute holds the pre-turn recall; otherwise the post-turn pass.
		holdRoute bool
		// pastTheGrace lets Close's first wait run out, and its cancel land,
		// before the held call is let go.
		pastTheGrace bool
	}{
		// The recall rides beside the turn and may still be reading the store
		// after the turn it was started for has ended. Nothing joined it.
		{name: "a recall left behind its own turn", holdRoute: true},
		// The post-turn pass WAS joined, but only for the grace: the cancel
		// that followed was never waited for.
		{name: "a post-turn pass the grace ran out on", pastTheGrace: true},
	} {
		t.Run(probe.name, func(t *testing.T) {
			// The spending ledger's writer lives as long as the process
			// ([usageWriterFor]), so it is started out here: one started inside
			// the bubble would be a goroutine the bubble is never rid of.
			usageWriterFor(UsageLedgerPath())
			synctest.Test(t, func(t *testing.T) {
				reflexes := newHeldReflex(probe.holdRoute)
				agent, brain := brainAgent(t, reflexes, nil)
				if probe.holdRoute {
					// Something to route against: an empty brain is never a
					// call. The post-turn case keeps the brain empty, so its
					// write is an add with no decision in front of it.
					remember(t, brain, "standup time", "standup is at 9:15")
				}

				collect(t, mustSubmit(t, agent, "the release train leaves on Thursdays"))
				<-reflexes.entered

				closed := make(chan struct{})
				go func() {
					_ = agent.Close()
					close(closed)
				}()
				if probe.pastTheGrace {
					// Every goroutine blocks, the bubble's clock jumps past the
					// grace, and Close cancels the pass it was waiting for.
					time.Sleep(closeGrace + time.Millisecond)
				}
				synctest.Wait()
				returnedEarly := false
				select {
				case <-closed:
					returnedEarly = true
				default:
				}
				// The gate opens either way, so a failure below is reported as
				// itself rather than as a bubble left holding the pass.
				close(reflexes.gate)
				<-closed
				if returnedEarly {
					t.Fatal("Close returned while a memory pass was still on its way to the store")
				}
				// AND WHAT IT WROTE IS THERE, read after Close and before the
				// store is: the pass reached the store inside the session's life.
				kept, err := brain.ListMemories("", 10)
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				found := false
				for _, memory := range kept {
					found = found || strings.Contains(memory.Text, "Thursdays")
				}
				if !found {
					t.Fatalf("the held pass never wrote; the store holds %v", titles(kept))
				}
			})
		})
	}
}
