package session

// The hand-over press, at every road that can end it: the turn that reads it,
// the turn that never will, and a turn the session has let go of.
//
// The press is a ticket on the node and on the note that carries it
// (task_audit.go's [handOverTicket]). These tests are the ones that fail when a
// road stops honouring the ticket — tools_tasks_test.go has the press itself and
// the turn that reads it.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A TURN THE SESSION HAS LET GO OF HANDS BACK NOTHING.
//
// An abandoned turn's goroutine can outlive the abandon by as long as the wait it
// is stuck in, and when it finally unwinds its clean-up used to run the
// end-of-turn floor before it looked at the turn number. By then the person may
// have pressed "let aforge decide" and the turn that replaced it may be reading
// the press — and the let-go turn handed that decision back to the person, so the
// card redrew its answers while aforge was deciding and a second press was taken
// as a fresh one.
func TestADisownedTurnLeavesTheNextTurnsDecisionAlone(t *testing.T) {
	completer, stuck, reading, release := stuckThenReading()
	defer release()
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.AskConsent = true })
	id := landUnverified(t, agent)

	letGo := awaitStuck(t, stuck)
	if _, abandoned := agent.Abandon(AbandonStopTimeout); !abandoned {
		t.Fatal("there was no turn to abandon")
	}
	if err := agent.HandUnverifiedToModel(id); err != nil {
		t.Fatalf("handing the decision over: %v", err)
	}
	awaitParked(t, reading)
	release()
	waitGoroutineGone(t, letGo)

	if owner := agent.taskNode(id).decidedBy(); owner != TaskAskOwnerModel {
		t.Fatalf("the let-go turn's end left %q deciding while the next turn reads the press, want the model", owner)
	}
	if err := agent.HandUnverifiedToModel(id); !errors.Is(err, ErrTaskHandedOver) {
		t.Fatalf("a second press while aforge reads the first answers %v, want it already handed over", err)
	}
}

// AND A QUESTION THE POLICY HANDED OVER IS THE LIVE TURN'S TOO. With nobody
// watching, a landing is the model's by policy ([Agent.handToModelOnAuto]), and
// what keeps a let-go turn's floor off it is the clean-up asking the turn number
// first — before it reaches the ticket's own check.
func TestADisownedTurnLeavesAPolicyHeldLandingAlone(t *testing.T) {
	completer, stuck, reading, release := stuckThenReading()
	defer release()
	agent, _ := newTestAgent(t, completer, nil)
	landUnverified(t, agent)
	letGo := awaitStuck(t, stuck)
	if _, abandoned := agent.Abandon(AbandonStopTimeout); !abandoned {
		t.Fatal("there was no turn to abandon")
	}
	id := landUnverified(t, agent)
	awaitParked(t, reading)
	release()
	waitGoroutineGone(t, letGo)

	if owner := agent.taskNode(id).decidedBy(); owner != TaskAskOwnerModel {
		t.Fatalf("the let-go turn's end took the landing the next turn is settling: %q is deciding", owner)
	}
}

// A DRAIN BELONGS TO THE LIVE TURN.
//
// [Agent.Abandon] frees the session before it cuts the let-go turn's context, so a
// goroutine that comes unstuck in between passes the loop's cancel check and
// reaches the drain before its next request (loop.go). That drain took what was
// queued for whatever came next — a press among it, marked read by a turn that
// was never going to answer, whose clean-up then hands nothing back. The press
// was left with aforge and nobody deciding. The test stands where that goroutine
// stands: after the abandon, at the drain, with its own turn's hub.
func TestALetGoTurnsDrainTakesNothingFromTheQueue(t *testing.T) {
	entered := make(chan struct{}, 1)
	agent, _ := newTestAgent(t, parkedModel(entered), func(config *Config) { config.AskConsent = true })
	id := landUnverified(t, agent)
	awaitParked(t, entered)
	agent.mu.Lock()
	letGo := agent.hub
	agent.mu.Unlock()
	if _, abandoned := agent.Abandon(AbandonStopTimeout); !abandoned {
		t.Fatal("there was no turn to abandon")
	}
	// With work stopped the press wakes no turn, so its note waits on the queue
	// for whoever comes next.
	if err := agent.StopWork(); err != nil {
		t.Fatalf("stopping work: %v", err)
	}
	if err := agent.HandUnverifiedToModel(id); err != nil {
		t.Fatalf("handing the decision over: %v", err)
	}

	agent.drainSteering(letGo)
	if queued := strings.Join(steeringQueue(agent), "\n"); !strings.Contains(queued, handOverLead) {
		t.Fatal("a let-go turn's drain took the press off the queue of the turn that comes next")
	}
}

// AND ITS FLOOR TAKES BACK ONLY WHAT IT READ. A let-go turn's floor can run after
// the turn that replaced it has carried a press — at the instant [Agent.Abandon]
// frees the session, or in a clean-up that asked the turn number a moment
// before the abandon moved it. The press belongs to the turn that read it.
func TestALetGoTurnsFloorLeavesAPressTheNextTurnRead(t *testing.T) {
	entered := make(chan struct{}, 1)
	agent, _ := newTestAgent(t, parkedModel(entered), func(config *Config) { config.AskConsent = true })
	id := landUnverified(t, agent)
	awaitParked(t, entered)
	agent.mu.Lock()
	letGo := agent.turnSeq
	agent.mu.Unlock()
	if _, abandoned := agent.Abandon(AbandonStopTimeout); !abandoned {
		t.Fatal("there was no turn to abandon")
	}
	if err := agent.HandUnverifiedToModel(id); err != nil {
		t.Fatalf("handing the decision over: %v", err)
	}
	awaitParked(t, entered)

	agent.handBackUnsettled(letGo)
	if owner := agent.taskNode(id).decidedBy(); owner != TaskAskOwnerModel {
		t.Fatalf("the let-go turn's floor took the press the next turn is reading: %q is deciding", owner)
	}
}

// A PICTURE ANSWER THAT IS LET GO OF IS LET GO OF ONCE. The picture turn's
// clean-up had no turn number: when an abandoned one came unstuck it closed the
// `done` the abandon had already closed — a panic nothing recovers — and cleared
// the running flag, hub and cancel of whatever turn had started since.
func TestAPictureAnswerLetGoOfLeavesTheNextTurnAlone(t *testing.T) {
	completer, stuck, reading, release := stuckThenReading()
	defer release()
	agent, workspace := newTestAgent(t, completer, blindWithVision)
	agent.SetModel("vendor/blind")
	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	if _, err := agent.SubmitImage(ctx, "what is in this?", []Image{{Path: writeImage(t, workspace, "shot.png", "PHOTOBYTES")}}); err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	letGo := awaitStuck(t, stuck)
	if _, abandoned := agent.Abandon(AbandonStopTimeout); !abandoned {
		t.Fatal("there was no turn to abandon")
	}
	if _, err := agent.Submit(ctx, "and the boats?"); err != nil {
		t.Fatalf("the next message: %v", err)
	}
	awaitParked(t, reading)
	release()
	waitGoroutineGone(t, letGo)

	agent.mu.Lock()
	running, hub := agent.running, agent.hub
	agent.mu.Unlock()
	if !running || hub == nil {
		t.Fatalf("the let-go picture turn's clean-up ended the turn after it (running %v, hub %v)", running, hub != nil)
	}
}

// A LATE ACT ABOUT A PRESS THAT WAS TAKEN BACK LEAVES THE NEXT PRESS ALONE.
//
// The drain that carries a note and the turn end that finds nobody carried it
// both act after the lock that saw the press is let go of, and in between the
// person can take the question back and press again. The ticket is what tells
// the two presses apart: the first one's act must not mark the second read —
// which would let the floor hand it back unread — nor give it back.
func TestALateActOnATakenBackPressLeavesTheNextPressAlone(t *testing.T) {
	entered := make(chan struct{}, 1)
	agent, _ := newTestAgent(t, parkedModel(entered), func(config *Config) { config.AskConsent = true })
	id := landUnverified(t, agent)
	awaitParked(t, entered)
	node := agent.taskNode(id)

	if err := agent.HandUnverifiedToModel(id); err != nil {
		t.Fatalf("the first press: %v", err)
	}
	first := queuedPress(t, agent)
	if err := agent.TakeBackDecision(id); err != nil {
		t.Fatalf("taking it back: %v", err)
	}
	if err := agent.HandUnverifiedToModel(id); err != nil {
		t.Fatalf("the second press: %v", err)
	}

	agent.mu.Lock()
	turn := agent.turnSeq
	agent.mu.Unlock()
	markHandOversRead([]handOverTicket{first}, turn)
	agent.handBackUnsettled(turn)
	if owner := node.decidedBy(); owner != TaskAskOwnerModel {
		t.Fatalf("carrying the first press's note let the floor take the unread second one: %q is deciding", owner)
	}
	agent.giveBackHandOvers([]handOverTicket{first})
	if owner := node.decidedBy(); owner != TaskAskOwnerModel {
		t.Fatalf("giving back the first press gave back the second: %q is deciding", owner)
	}
}

// A PRESS NO TURN IS GOING TO READ COMES BACK TO THE PERSON — for each way a turn
// can end without starting the one that would read it.

// The person stops the turn their press arrived in.
func TestAPressComesBackWhenItsTurnIsStopped(t *testing.T) {
	entered := make(chan struct{}, 1)
	agent, _ := newTestAgent(t, parkedModel(entered), func(config *Config) { config.AskConsent = true })
	wakes, stop := agent.WatchWakes()
	defer stop()
	id := landUnverified(t, agent)
	turn := awaitWake(t, wakes)
	awaitParked(t, entered)

	if err := agent.HandUnverifiedToModel(id); err != nil {
		t.Fatalf("handing the decision over: %v", err)
	}
	agent.Interrupt()
	collect(t, turn)
	if owner := agent.taskNode(id).decidedBy(); owner != TaskAskOwnerPerson {
		t.Fatalf("the press outlived the stopped turn it arrived in: %q is deciding, want the person", owner)
	}
}

// The turn is abandoned: nothing follows an abandon.
func TestAPressComesBackWhenItsTurnIsAbandoned(t *testing.T) {
	entered := make(chan struct{}, 1)
	agent, _ := newTestAgent(t, parkedModel(entered), func(config *Config) { config.AskConsent = true })
	id := landUnverified(t, agent)
	awaitParked(t, entered)

	if err := agent.HandUnverifiedToModel(id); err != nil {
		t.Fatalf("handing the decision over: %v", err)
	}
	if _, abandoned := agent.Abandon(AbandonStopTimeout); !abandoned {
		t.Fatal("there was no turn to abandon")
	}
	if owner := agent.taskNode(id).decidedBy(); owner != TaskAskOwnerPerson {
		t.Fatalf("the press outlived the abandoned turn: %q is deciding, want the person", owner)
	}
}

// AND WHAT THE ABANDONED TURN ITSELF WAS DECIDING COMES BACK AT THE ABANDON. Its
// goroutine may never unwind, and when it does its clean-up hands back nothing,
// so the abandon is that turn's end.
func TestWhatAnAbandonedTurnWasDecidingComesBackAtTheAbandon(t *testing.T) {
	pressed, reading := make(chan struct{}), make(chan struct{}, 1)
	completer := &scriptedCompleter{steps: []step{
		// The landing's turn calls a tool once the press is made, so a step
		// boundary follows the press and the turn carries it.
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			select {
			case <-pressed:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return toolResponse("call-1", "no_such_tool", "{}"), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			reading <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.AskConsent = true })
	id := landUnverified(t, agent)

	if err := agent.HandUnverifiedToModel(id); err != nil {
		t.Fatalf("handing the decision over: %v", err)
	}
	close(pressed)
	select {
	case <-reading:
	case <-time.After(10 * time.Second):
		t.Fatal("the turn never came back with the press in front of it")
	}
	if _, abandoned := agent.Abandon(AbandonStopTimeout); !abandoned {
		t.Fatal("there was no turn to abandon")
	}
	if owner := agent.taskNode(id).decidedBy(); owner != TaskAskOwnerPerson {
		t.Fatalf("what the abandoned turn was deciding stayed with %q, want the person", owner)
	}
}

// The session is closed: its queue is never drained again.
func TestAPressOnAClosedSessionComesStraightBack(t *testing.T) {
	agent, _ := newTestAgent(t, parkedModel(nil), func(config *Config) { config.AskConsent = true })
	id := landUnverified(t, agent)
	if err := agent.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}
	if err := agent.HandUnverifiedToModel(id); err == nil {
		t.Fatal("a press on a closed session was taken as a hand-over")
	}
	if owner := agent.taskNode(id).decidedBy(); owner != TaskAskOwnerPerson {
		t.Fatalf("a press nobody can read stayed with %q, want the person", owner)
	}
}

// The press lands while another model is answering a picture the chat model
// cannot see (image.go): that turn is one call with no step to read it in, and
// its end starts no turn of its own.
func TestAPressDuringAPictureAnswerComesBackWhenItEnds(t *testing.T) {
	var (
		agent *Agent
		id    uint64
	)
	pressed := make(chan error, 1)
	completer := &scriptedCompleter{}
	completer.aside = func(messages []ai.Message) (*ai.Response, bool) {
		if len(messages) == 0 || len(imagePartURLs(messages[len(messages)-1])) == 0 {
			return nil, false
		}
		pressed <- agent.HandUnverifiedToModel(id)
		return textResponse("a red harbour at dusk"), true
	}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		blindWithVision(config)
		config.AskConsent = true
	})
	agent.SetModel("vendor/blind")
	wakes, stop := agent.WatchWakes()
	defer stop()
	id = landUnverified(t, agent)
	collect(t, awaitWake(t, wakes))

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.SubmitImage(ctx, "what is in this?", []Image{{Path: writeImage(t, workspace, "shot.png", "PHOTOBYTES")}})
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	collect(t, events)
	if err := <-pressed; err != nil {
		t.Fatalf("handing the decision over: %v", err)
	}
	if owner := agent.taskNode(id).decidedBy(); owner != TaskAskOwnerPerson {
		t.Fatalf("the press outlived the picture's answer: %q is deciding, want the person", owner)
	}
}

// ── the settle-policy road ──────────────────────────────────────────────────
//
// With nobody watching, and under `task.settle = auto`, a landing is handed to
// the model by POLICY ([Agent.handToModelOnAuto]), and its note carries a ticket
// exactly as a press's does. These are the press's roads walked by a landing.

// A LANDING THAT MISSES A TURN IS HELD FOR THE TURN THAT READS IT.
//
// The work lands while a turn's last request is already out, so its note waits
// on the queue; the turn ends, and the end of it drains the note and wakes the
// next turn to settle the work. The floor at that first end used to hand the
// question straight back — the policy's hand-over had no ticket to say its note
// was still unread — so the card drew its answers again while the next turn was
// being told to settle the work.
func TestALandingThatMissesATurnIsHeldForTheTurnThatReadsIt(t *testing.T) {
	type reading struct {
		decider TaskAskOwner
		carried bool
	}
	var (
		graph *TaskGraph
		id    uint64
	)
	read := make(chan reading, 1)
	completer := &scriptedCompleter{steps: []step{
		// THE WORK LANDS WITH THIS TURN'S LAST REQUEST ALREADY OUT. The graph
		// announces a landing before it closes `done`, so the note is on the queue
		// before this request answers.
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			graph.admit(id, taskSpec{title: "Hidden rental digs", named: true, brief: "b", acceptance: "a"})
			select {
			case <-graph.node(id).done:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return textResponse("Nothing else from me."), nil
		},
		// AND THIS IS THE TURN THAT READS IT: the node must still be the model's.
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			carried := false
			for _, message := range messages {
				carried = carried || strings.Contains(messageText(message), "Hidden rental digs")
			}
			read <- reading{graph.node(id).decidedBy(), carried}
			return textResponse("I have looked at it."), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	graph = stubbedGraph(agent, func(node *TaskNode) {
		node.finish("UNVERIFIED — the auditor answered neither VERIFIED nor REFUTED", nil, "", "")
		node.graph.complete(node, TaskUnverified)
	})
	id = graph.reserve()

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.Submit(ctx, "tidy the notes")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	var got reading
	select {
	case got = <-read:
	case <-time.After(10 * time.Second):
		t.Fatal("no turn ever read the landing")
	}
	if !got.carried {
		t.Fatal("the turn after the landing did not carry its note")
	}
	if got.decider != TaskAskOwnerModel {
		t.Fatalf("the turn reading the landing finds %q deciding, want the model", got.decider)
	}
	// AND THE FLOOR STILL HOLDS: the turn that read it and did not settle it hands
	// it back when it ends, so the question is never held by nobody.
	waitFor(t, "the reading turn's end to hand the landing back", func() bool {
		return graph.node(id).decidedBy() == TaskAskOwnerPerson
	})
}

// A PIECE'S LANDING IS READ BY THE WORKER THAT ASKED FOR IT. Its note goes to the
// parent node's own agent ([Agent.taskNoteReaders]), so it is that agent's drain
// that marks the ticket read and that agent's floor that hands it back. A ticket
// the worker's drain never marked would be one no floor anywhere takes back.
func TestAPiecesLandingIsHeldByTheWorkerThatReadsIt(t *testing.T) {
	land := make(chan struct{})
	var piece *TaskNode
	read := make(chan TaskAskOwner, 1)
	worker := &scriptedCompleter{steps: []step{
		// The piece lands while the worker's first request is out, and the tool
		// call makes a step boundary for its note to be carried at.
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(land)
			select {
			case <-piece.done:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return toolResponse("call-1", "no_such_tool", "{}"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			read <- piece.decidedBy()
			return textResponse("I have looked at the piece."), nil
		},
	}}
	nest := newNest(t, worker, func(node *TaskNode) {
		if node.parent == 0 {
			return
		}
		<-land
		node.finish("UNVERIFIED — the auditor answered neither VERIFIED nor REFUTED", nil, "", "")
		node.graph.complete(node, TaskUnverified)
	})
	piece = pieceUnder(t, nest.graph, nest.parent.id, "currency")

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := nest.node.Submit(ctx, "fold the pieces in")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	select {
	case owner := <-read:
		if owner != TaskAskOwnerModel {
			t.Fatalf("the worker reading the piece finds %q deciding, want the model", owner)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the worker never read the piece")
	}
	if owner := piece.decidedBy(); owner != TaskAskOwnerPerson {
		t.Fatalf("the worker's turn read the piece and ended, and %q is still deciding", owner)
	}
}

// A LANDING NOBODY TAKES COMES STRAIGHT BACK. Its note is refused by every reader
// ([TaskNode.releaseNote]), so no turn is ever going to read the ticket, and a
// question handed to nobody stays the person's.
func TestALandingNobodyTakesStaysWithThePerson(t *testing.T) {
	agent, _ := newTestAgent(t, parkedModel(nil), nil)
	graph := stubbedGraph(agent, func(*TaskNode) {})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "Hidden rental digs", named: true, brief: "b", acceptance: "a"})
	node := graph.node(id)
	if err := agent.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	node.finish("UNVERIFIED — the auditor answered neither VERIFIED nor REFUTED", nil, "", "")
	graph.complete(node, TaskUnverified)
	if node.reported() {
		t.Fatal("the landing's note was taken by a closed session")
	}
	if owner := node.decidedBy(); owner != TaskAskOwnerPerson {
		t.Fatalf("a landing nobody can read stayed with %q, want the person", owner)
	}
}

// ONE ENDING ANNOUNCED TWICE IS ONE HAND-OVER. The second announcement is refused
// as a duplicate ([TaskNode.claimNote]), and the first note is still on the queue
// with its ticket. A second ticket would make the first one stale — nothing would
// ever mark it read — and then be given back with its refused note, handing the
// question back to the person while the first note asks the model to settle it.
func TestALandingAnnouncedTwiceIsHandedOverOnce(t *testing.T) {
	entered := make(chan struct{}, 1)
	agent, _ := newTestAgent(t, parkedModel(entered), nil)
	// A turn parked in its request drains nothing, so the landing's note waits on
	// the queue for its next step.
	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	if _, err := agent.Submit(ctx, "tidy the notes"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	awaitParked(t, entered)
	id := landUnverified(t, agent)
	node := agent.taskNode(id)
	queued := queuedPress(t, agent)

	agent.reportTaskNode(node)
	if owner := node.decidedBy(); owner != TaskAskOwnerModel {
		t.Fatalf("announcing the landing again handed it to %q, want the model", owner)
	}
	agent.mu.Lock()
	turn := agent.turnSeq
	agent.mu.Unlock()
	markHandOversRead([]handOverTicket{queued}, turn)
	agent.handBackUnsettled(turn)
	if owner := node.decidedBy(); owner != TaskAskOwnerPerson {
		t.Fatalf("the turn that read the first note could not hand it back: %q is deciding", owner)
	}
}

// AND A LET-GO TURN'S FLOOR LEAVES A LANDING THE NEXT TURN READ. The policy's
// hand-over is stamped with the turn that read it, so the round-3 law holds
// here too: a floor takes back only what its own turn read.
func TestALetGoTurnsFloorLeavesALandingTheNextTurnRead(t *testing.T) {
	entered := make(chan struct{}, 1)
	agent, _ := newTestAgent(t, parkedModel(entered), nil)
	landUnverified(t, agent)
	awaitParked(t, entered)
	agent.mu.Lock()
	letGo := agent.turnSeq
	agent.mu.Unlock()
	if _, abandoned := agent.Abandon(AbandonStopTimeout); !abandoned {
		t.Fatal("there was no turn to abandon")
	}
	id := landUnverified(t, agent)
	awaitParked(t, entered)

	agent.handBackUnsettled(letGo)
	if owner := agent.taskNode(id).decidedBy(); owner != TaskAskOwnerModel {
		t.Fatalf("the let-go turn's floor took the landing the next turn is reading: %q is deciding", owner)
	}
}

// A PIECE WAITING ON A STOPPED PARENT COMES BACK TO THE PERSON. It lands `your
// call` while a sibling still runs, so its note waits on the parked worker's
// queue: the runner parks again rather than start a turn about half the news.
// Then the parent is stopped — its runner's context is cut and the retire closes
// the worker — and no turn will ever read that note. The close dropped the queue
// with the ticket on it, and the conversation's floor never takes back a ticket
// nobody read, so the card said `aforge is deciding` until somebody took it back
// or aforge restarted.
func TestAPieceWaitingOnAStoppedParentComesBackToThePerson(t *testing.T) {
	land := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "propose_task", string(pieceArgs("read the law"))), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c2", "propose_task", string(pieceArgs("read the index"))), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("handed both out"), nil
		},
	}}
	// The index is still being read when the parent is stopped; the law lands.
	nest := newNest(t, completer, func(node *TaskNode) {
		if node.spec.title != "read the law" {
			return
		}
		go func() {
			<-land
			node.finish("UNVERIFIED — the auditor answered neither VERIFIED nor REFUTED", nil, "", "")
			node.graph.complete(node, TaskUnverified)
		}()
	})
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = runTaskChild(ctx, nest.node, nest.parent, "do the whole job", nest.node.config.Workspace,
			taskLimits{maxSteps: taskMaxSteps, noProgress: taskNoProgress}, nest.parent.openRoom(), io.Discard)
	}()
	waitRequests(t, completer, 3)
	waitQuiet(t, nest.node)
	law := nest.graph.children(nest.parent.id)[0]
	close(land)
	waitDoneNode(t, law)
	// Its note, with its ticket, is waiting on the parked worker's queue.
	queuedPress(t, nest.node)

	stop()
	<-done
	if err := nest.node.Close(); err != nil {
		t.Fatalf("closing the worker: %v", err)
	}
	if owner := law.decidedBy(); owner != TaskAskOwnerPerson {
		t.Fatalf("the stopped parent's worker never read the piece, and %q is still deciding it", owner)
	}
}

// AND A PIECE THAT LANDS AFTER THE WORKER'S LAST REQUEST IS HELD FOR ITS NEXT
// TURN. A worker's turn wakes no turn of its own; its runner starts the next one
// for the news the end of this one drained (task_child_run.go's
// [childRun.foldParts]). The end of the turn gave the question back first, so
// the runner's turn was told to settle a piece whose card had its answers back.
func TestAPieceThatMissesTheWorkersTurnIsHeldForTheNextOne(t *testing.T) {
	land := make(chan struct{})
	var family *nest
	// The piece is looked up where the worker's steps run, because it is handed
	// out inside the worker's own first turn.
	law := func() *TaskNode { return family.graph.children(family.parent.id)[0] }
	read := make(chan TaskAskOwner, 1)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "propose_task", string(pieceArgs("read the law"))), nil
		},
		// THE PIECE LANDS WITH THE WORKER'S LAST REQUEST ALREADY OUT.
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(land)
			select {
			case <-law().done:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return textResponse("handed the reading out"), nil
		},
		// AND THIS IS THE RUNNER'S TURN, THE ONE THAT READS IT.
		func(context.Context, []ai.Message) (*ai.Response, error) {
			read <- law().decidedBy()
			return textResponse("the reading landed and I have looked at it"), nil
		},
	}}
	family = newNest(t, completer, func(node *TaskNode) {
		if node.parent == 0 {
			return
		}
		go func() {
			<-land
			node.finish("UNVERIFIED — the auditor answered neither VERIFIED nor REFUTED", nil, "", "")
			node.graph.complete(node, TaskUnverified)
		}()
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = runTaskChild(context.Background(), family.node, family.parent, "do the whole job",
			family.node.config.Workspace, taskLimits{maxSteps: taskMaxSteps, noProgress: taskNoProgress},
			family.parent.openRoom(), io.Discard)
	}()

	select {
	case owner := <-read:
		if owner != TaskAskOwnerModel {
			t.Fatalf("the runner's turn reading the piece finds %q deciding, want the model", owner)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the runner never started the turn that reads the piece")
	}
	<-done
	if owner := law().decidedBy(); owner != TaskAskOwnerPerson {
		t.Fatalf("the turn that read the piece ended and %q is still deciding it", owner)
	}
}

// A LANDING THE PERSON HOLDS IS NOT HANDED OVER BY ITS OWN ECHO. A second report
// of an ending already announced is refused its note, so it may not take a
// ticket either. Taken before the claim, it drew `aforge is deciding` over a card
// the person had just taken back — a `d` pressed then was swallowed as a repeat —
// and gave it back a moment later.
func TestAReannouncedLandingThePersonHoldsStaysWithThem(t *testing.T) {
	entered := make(chan struct{}, 1)
	agent, _ := newTestAgent(t, parkedModel(entered), nil)
	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.Submit(ctx, "tidy the notes")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	awaitParked(t, entered)
	id := landUnverified(t, agent)
	if err := agent.TakeBackDecision(id); err != nil {
		t.Fatalf("taking it back: %v", err)
	}

	agent.reportTaskNode(agent.taskNode(id))
	agent.Interrupt()
	var said []TaskAskOwner
	for _, event := range collect(t, events) {
		if event.Kind == EventTaskUpdate && event.Task != nil && event.Task.ID == id {
			said = append(said, event.Task.Decider)
		}
	}
	// The landing said aforge, the take-back said the person, and nothing after
	// the take-back may say aforge again.
	for i := 1; i < len(said); i++ {
		if said[i-1] == TaskAskOwnerPerson && said[i] == TaskAskOwnerModel {
			t.Fatalf("the card was told %v: the echo handed the person's card to aforge", said)
		}
	}
}

// landUnverified lands one task as `your call` on a stubbed graph and answers its
// id once the landing is fully settled.
func landUnverified(t *testing.T, agent *Agent) uint64 {
	t.Helper()
	graph := stubbedGraph(agent, func(node *TaskNode) {
		node.finish("UNVERIFIED — the auditor answered neither VERIFIED nor REFUTED", nil, "", "")
		node.graph.complete(node, TaskUnverified)
	})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "Hidden rental digs", named: true, brief: "b", acceptance: "a"})
	waitDoneNode(t, graph.node(id))
	return id
}

// parkedModel is a model whose every request waits for the session to cut it,
// saying so on entered (when there is one) as it starts to wait — so a test knows
// the turn has made its request and will drain nothing more until it is cut.
func parkedModel(entered chan<- struct{}) *scriptedCompleter {
	park := func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		if entered != nil {
			select {
			case entered <- struct{}{}:
			default:
			}
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &scriptedCompleter{steps: []step{park, park, park, park}}
}

// stuckThenReading is a model whose first request is stuck in a wait no context
// reaches until release is called — the shape [Agent.Abandon] exists for — and
// whose second parks until the session cuts it, saying so on reading. stuck
// carries the stuck request's goroutine, so a test can wait for that turn's
// clean-up ([waitGoroutineGone]). A test defers release, which runs before the
// session's close: a close waits for a turn that is still stuck.
func stuckThenReading() (*scriptedCompleter, <-chan uint64, <-chan struct{}, func()) {
	stuck, reading := make(chan uint64, 1), make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	let := func() { once.Do(func() { close(release) }) }
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			stuck <- goroutineID()
			<-release
			return textResponse("I was somewhere else."), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			reading <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}
	return completer, stuck, reading, let
}

func awaitStuck(t *testing.T, stuck <-chan uint64) uint64 {
	t.Helper()
	select {
	case id := <-stuck:
		return id
	case <-time.After(10 * time.Second):
		t.Fatal("the first request was never made")
		return 0
	}
}

func awaitParked(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the landing's turn never made its request")
	}
}

func awaitWake(t *testing.T, wakes <-chan (<-chan Event)) <-chan Event {
	t.Helper()
	select {
	case turn := <-wakes:
		return turn
	case <-time.After(10 * time.Second):
		t.Fatal("the landing never woke a turn")
		return nil
	}
}

// queuedPress is the ticket the note on the steering queue carries.
func queuedPress(t *testing.T, agent *Agent) handOverTicket {
	t.Helper()
	agent.mu.Lock()
	defer agent.mu.Unlock()
	for _, message := range agent.steering {
		if len(message.handsOver) > 0 {
			return message.handsOver[0]
		}
	}
	t.Fatal("no hand-over note is waiting on the queue")
	return handOverTicket{}
}

// goroutineID is the runtime's number for the calling goroutine, read off the
// first line of its own stack ("goroutine 123 [running]:").
func goroutineID() uint64 {
	buffer := make([]byte, 64)
	fields := strings.Fields(string(buffer[:runtime.Stack(buffer, false)]))
	id, _ := strconv.ParseUint(fields[1], 10, 64)
	return id
}

// waitGoroutineGone waits for one goroutine to have returned, every deferred
// call included. It is how a test knows a let-go turn's clean-up has run: that
// turn has no channel left for anybody to read.
func waitGoroutineGone(t *testing.T, id uint64) {
	t.Helper()
	marker := fmt.Sprintf("goroutine %d [", id)
	waitFor(t, "the let-go turn to finish", func() bool {
		buffer := make([]byte, 1<<16)
		for {
			n := runtime.Stack(buffer, true)
			if n < len(buffer) {
				return !strings.Contains(string(buffer[:n]), marker)
			}
			buffer = make([]byte, 2*len(buffer))
		}
	})
}
