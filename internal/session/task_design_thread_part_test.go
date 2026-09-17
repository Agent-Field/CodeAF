package session

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A DESIGN THREAD CAN HAND WORK OUT, AND THE DESIGN BODY CLOSES IT WITHOUT
// STOPPING WHAT IT OWNS.
//
// workTaskNode carries the nursery law: it stops the parts of the worker it is
// about to close (task_run.go's `defer node.graph.stopChildren(node.id)`,
// registered before that worker's own retire), because a part's job row lives in
// its OWNER'S registry and [jobRegistry.shutdown] cuts that row's context through
// [job.signal], which takes the `stop` handle and never the explicit one that
// only [jobRegistry.kill] calls. A part cut that way reads its own cancel as A
// PROCESS QUITTING: it lands on the "paused — it resumes" road, whose whole
// meaning is that a NEXT process will pick it up, and returns "" so its state
// stays running with an empty Ending for a recovery that is never coming.
//
// The design body builds the same kind of child — its thread — and closes it
// with a bare `defer child.Close()` and NO stopChildren (harness_task.go's
// [Agent.designHarnessNode]). The thread is not a leaf: it is built by
// [Agent.newTaskAgent] with the family's graph and its depth, so it carries
// propose_task and tasks on its belt and can own parts of its own. Every part it
// hands out is therefore cut down with the thread, unmarked, exactly as the
// nursery law describes — the route the guard in workTaskNode does not cover.
func TestAPartAThreadHandedOutIsNotLeftPausedWhenTheDesignCloses(t *testing.T) {
	completer := &designThreadPartCompleter{partBlocked: make(chan struct{})}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		buildConfig(config, t.TempDir())
		// A repository, so the part a thread hands out can make a working copy of
		// its own and reach its first request rather than failing to open a world.
		config.Workspace = newGoModuleRepo(t)
	})
	submitBuild(t, agent)
	design := designNode(t, agent)

	// The page is written and the design is waiting on its card, which is the
	// only moment its thread stands in the room with no turn of its own running.
	waitForPhase(t, design, HarnessPhaseAsking)
	thread := design.openRoom().speaker()
	if thread == nil {
		t.Fatal("the design has no thread standing in its room")
	}
	// Every request from here on belongs to the part the thread is about to hand
	// out — the design makes none of its own while its card is up.
	completer.hold()

	// The thread hands out a part through its own belt, the way the model would.
	if _, _, err := thread.proposeTask(context.Background(), pieceArgs("a part of the design")); err != nil {
		t.Fatalf("the design's thread could not hand out a part: %v", err)
	}
	part := waitForChild(t, agent, design.id)
	// The part has to be RUNNING — mid-request, held open — when the design
	// closes, or there is nothing for the thread's Close to cut.
	select {
	case <-completer.partBlocked:
	case <-time.After(harnessTestPatience):
		t.Fatalf("the part never reached its first request; state %q", part.stateNow())
	}

	// The design goes away. Its body returns, and the thread it built is closed
	// with it — the moment the part is cut down.
	if _, err := agent.Cancel("task:" + itoa64(design.id)); err != nil {
		t.Fatalf("stopping the design: %v", err)
	}
	select {
	case <-design.done:
	case <-time.After(harnessTestPatience):
		t.Fatal("the design never landed")
	}

	// THE RECORD. The thread's Close cut the part down before anything marked it
	// stopped — the design's own stopChildren runs after its body has already
	// closed the thread — so it must not be left reading like a run a next
	// process will resume: state running, no ending, and a report promising it
	// resumes, all at once. Any one of those alone is fine; the three together
	// are the shape the nursery law exists to forbid.
	notice := waitForPartToStopMoving(t, part)
	if notice.State == TaskRunning && notice.Ending == "" && strings.Contains(notice.Report, "paused — it resumes") {
		t.Fatalf("a part the design's thread handed out was cut down unmarked and left reading as resumable work:\n"+
			"  state   = %q\n  ending  = %q\n  stopped = %v\n  report  = %q",
			notice.State, notice.Ending, notice.Stopped, notice.Report)
	}
}

// designThreadPartCompleter answers the design's own two turns, and once the
// design is waiting on its card holds every further request open until its
// context is cut — the shape of a part that is running when the thing that owns
// it goes away.
type designThreadPartCompleter struct {
	mu          sync.Mutex
	designTurns int
	holding     bool
	partBlocked chan struct{}
	once        sync.Once
}

// hold makes every request after it a part's: the design has no turn left to
// run, so nothing else this session asks is the design talking.
func (c *designThreadPartCompleter) hold() {
	c.mu.Lock()
	c.holding = true
	c.mu.Unlock()
}

func (c *designThreadPartCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	if isNameCall(messages) || isCaptionCall(messages) || isTitleCall(messages) {
		return textResponse(""), nil
	}
	c.mu.Lock()
	holding := c.holding
	if !holding {
		c.designTurns++
	}
	turn := c.designTurns
	c.mu.Unlock()
	if !holding {
		if turn == 1 {
			return textResponse(designReply), nil
		}
		return textResponse(reviewReply), nil
	}
	c.once.Do(func() { close(c.partBlocked) })
	<-ctx.Done()
	return nil, ctx.Err()
}

// waitForChild waits for the one part handed out under parent to exist.
func waitForChild(t *testing.T, agent *Agent, parent uint64) *TaskNode {
	t.Helper()
	for until := time.Now().Add(harnessTestPatience); time.Now().Before(until); {
		if kids := agent.graph().children(parent); len(kids) == 1 {
			return kids[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the design never handed out its part")
	return nil
}

// waitForPartToStopMoving waits for the part to take its ending road — the
// paused road keeps a node running, so settling is not the test.
func waitForPartToStopMoving(t *testing.T, part *TaskNode) TaskNotice {
	t.Helper()
	for until := time.Now().Add(harnessTestPatience); time.Now().Before(until); {
		notice := part.notice()
		if notice.State != TaskRunning || notice.Report != "" {
			return notice
		}
		time.Sleep(5 * time.Millisecond)
	}
	return part.notice()
}
