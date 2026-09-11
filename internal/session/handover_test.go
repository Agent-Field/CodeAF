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
	"runtime"
	"strconv"
	"strings"
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
	stuck, reading := make(chan uint64, 1), make(chan struct{}, 1)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	completer := &scriptedCompleter{steps: []step{
		// The landing's turn, stuck in a wait no context reaches — the shape an
		// abandon exists for (abandon.go).
		func(context.Context, []ai.Message) (*ai.Response, error) {
			stuck <- goroutineID()
			<-release
			return textResponse("I was somewhere else."), nil
		},
		// The turn the press wakes, with the press in front of it.
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			reading <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.AskConsent = true })
	id := landUnverified(t, agent)

	var letGo uint64
	select {
	case letGo = <-stuck:
	case <-time.After(10 * time.Second):
		t.Fatal("the landing never woke a turn")
	}
	if _, abandoned := agent.Abandon(AbandonStopTimeout); !abandoned {
		t.Fatal("there was no turn to abandon")
	}
	if err := agent.HandUnverifiedToModel(id); err != nil {
		t.Fatalf("handing the decision over: %v", err)
	}
	select {
	case <-reading:
	case <-time.After(10 * time.Second):
		t.Fatal("no turn read the hand-over")
	}
	close(release)
	waitGoroutineGone(t, letGo)

	if owner := agent.taskNode(id).decidedBy(); owner != TaskAskOwnerModel {
		t.Fatalf("the let-go turn's end left %q deciding while the next turn reads the press, want the model", owner)
	}
	if err := agent.HandUnverifiedToModel(id); !errors.Is(err, ErrTaskHandedOver) {
		t.Fatalf("a second press while aforge reads the first answers %v, want it already handed over", err)
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

	markHandOversRead([]handOverTicket{first})
	agent.handBackUnsettled()
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
