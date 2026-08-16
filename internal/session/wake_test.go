package session

// THE WAKE, AS TESTS: a task that lands must produce a SENTENCE, not a card and
// silence.
//
// Every test here drives the real settle path — a node admitted to the graph,
// finished, completed — because "a task landed" is a fact the graph states and
// nothing else may state on its behalf (task_run.go's reportTaskNode). What is
// asserted is the conversation's half: a turn started, the outcome reached the
// model, the note reached the journal, and no arrangement of settles starts more
// turns than there are things to say.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── harness ─────────────────────────────────────────────────────────────────

// settleTask runs one node to a final state through the graph the agent owns,
// so the note, the index row and the wake all happen exactly as they do in a
// real session. It returns once the node's state has settled; the note it
// triggers lands a moment later, which is what the waits below are for.
func settleTask(t *testing.T, agent *Agent, title, report string) *TaskNode {
	t.Helper()
	graph := agent.graph()
	// The runner is replaced before the first admission, so nothing is reading
	// it yet: no worktree, no child agent, no provider — the node's whole life
	// is the report it finishes with.
	graph.run = func(node *TaskNode) {
		node.finish(report, nil, "", "")
		node.graph.complete(node, TaskDone)
	}
	id := graph.reserve()
	graph.admit(id, taskSpec{title: title, brief: "b", acceptance: "a"})
	node := graph.node(id)
	waitDoneNode(t, node)
	return node
}

// conversationRequests counts the requests that are THIS CONVERSATION's turns.
// A session with a file names itself after its first completed turn (title.go),
// and that auxiliary call rides the same completer; it is told apart by its
// system message, which is the namer's prompt and never the session's.
func conversationRequests(completer *scriptedCompleter) int {
	completer.mu.Lock()
	defer completer.mu.Unlock()
	count := 0
	for _, messages := range completer.seen {
		if len(messages) > 0 && messages[0].Role == "system" && messageText(messages[0]) == "SYSTEM" {
			count++
		}
	}
	return count
}

// userTextIn is every user-role message of one request, joined — what the model
// was told by anybody other than itself.
func userTextIn(messages []ai.Message) string {
	var text strings.Builder
	for _, message := range messages {
		if message.Role == "user" {
			text.WriteString(messageText(message))
			text.WriteString("\n")
		}
	}
	return text.String()
}

func transcriptText(a *Agent) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var text strings.Builder
	for _, message := range a.messages {
		text.WriteString(message.Role)
		text.WriteString(": ")
		text.WriteString(messageText(message))
		text.WriteString("\n")
	}
	return text.String()
}

// ── the wake ────────────────────────────────────────────────────────────────

// The defect this whole file exists for: a task lands on an idle session and
// the person gets a card and nothing said. The note must start a turn, and the
// model must have the outcome in front of it when it does.
func TestSettledTaskWakesAnIdleSession(t *testing.T) {
	asked := make(chan []ai.Message, 4)
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			asked <- messages
			return textResponse("the comparison is at ~/oauth.md"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	settleTask(t, agent, "research oauth", "wrote the comparison to ~/oauth.md")

	select {
	case messages := <-asked:
		if !strings.Contains(userTextIn(messages), "wrote the comparison to ~/oauth.md") {
			t.Fatalf("the woken turn did not carry the outcome:\n%s", userTextIn(messages))
		}
		if !strings.Contains(userTextIn(messages), "task 1 finished") {
			t.Fatalf("the woken turn did not carry the settle itself:\n%s", userTextIn(messages))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a settled task never started a turn: the person gets a card and silence")
	}

	waitFor(t, "the answer to reach the transcript", func() bool {
		return strings.Contains(transcriptText(agent), "the comparison is at ~/oauth.md")
	})
}

// The note is the MODEL's copy of the card and it is journaled like any message
// the person sends, so the outcome survives a resume.
func TestWakeNoteIsJournaledAndSurvivesReplay(t *testing.T) {
	file := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = file
	})

	settleTask(t, agent, "research oauth", "wrote the comparison to ~/oauth.md")
	waitFor(t, "the note to reach the transcript", func() bool {
		return strings.Contains(transcriptText(agent), "wrote the comparison to ~/oauth.md")
	})
	if err := agent.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	journal, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if !strings.Contains(string(journal), "wrote the comparison to ~/oauth.md") {
		t.Fatalf("the settle never reached the journal:\n%s", journal)
	}

	// AND THE RESUME READS IT BACK. A conversation that forgot what its own
	// tasks produced would ask for the work again.
	resumed, err := newAgent(Config{
		Workspace:   workspace,
		Model:       "test/model",
		System:      "SYSTEM",
		SessionFile: file,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	t.Cleanup(func() { _ = resumed.Close() })
	if !strings.Contains(transcriptText(resumed), "wrote the comparison to ~/oauth.md") {
		t.Fatalf("the resumed session lost the outcome:\n%s", transcriptText(resumed))
	}
}

// A settle that lands MID-TURN is steering: it joins the turn already running
// at its next step boundary, and starts nothing of its own.
func TestSettleMidTurnQueuesIntoTheRunningTurn(t *testing.T) {
	var (
		inFlight = make(chan struct{})
		release  = make(chan struct{})
		second   = make(chan []ai.Message, 1)
	)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(inFlight)
			<-release
			return toolResponse("call-1", "ls", `{"path":"."}`), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			second <- messages
			return textResponse("and the task landed too"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	events, err := agent.Submit(context.Background(), "what is in this directory?")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	<-inFlight
	settleTask(t, agent, "research oauth", "wrote the comparison to ~/oauth.md")
	close(release)
	collect(t, events)

	select {
	case messages := <-second:
		if !strings.Contains(userTextIn(messages), "wrote the comparison to ~/oauth.md") {
			t.Fatalf("the settle never reached the running turn:\n%s", userTextIn(messages))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the turn never reached its second step")
	}

	// TWO REQUESTS, and the second is the step the note rode into. A settle that
	// had started a turn of its own would show up here as a third.
	time.Sleep(50 * time.Millisecond)
	if count := conversationRequests(completer); count != 2 {
		t.Fatalf("the mid-turn settle produced %d requests, want the running turn's 2", count)
	}
}

// Two tasks landing in one window are ONE turn, not two: they queue together
// and the turn that answers them has both in front of it.
func TestTwoSettlesInOneWindowAreAnsweredByOneTurn(t *testing.T) {
	var (
		inFlight = make(chan struct{})
		release  = make(chan struct{})
		woken    = make(chan []ai.Message, 4)
	)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(inFlight)
			<-release
			return textResponse("looking into it"), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			woken <- messages
			return textResponse("both landed: the comparison and the migration"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	// Both settles land while the person's turn is in flight, so both are queued
	// behind its last request and neither has been answered when it ends.
	events, err := agent.Submit(context.Background(), "start both of those")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	<-inFlight
	settleTask(t, agent, "research oauth", "wrote the comparison to ~/oauth.md")
	settleTask(t, agent, "migrate the store", "moved the store to sqlite")
	close(release)
	collect(t, events)

	select {
	case messages := <-woken:
		text := userTextIn(messages)
		if !strings.Contains(text, "wrote the comparison to ~/oauth.md") ||
			!strings.Contains(text, "moved the store to sqlite") {
			t.Fatalf("one wake did not carry both outcomes:\n%s", text)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("two settles at the end of a turn never produced an answer")
	}

	// ONE wake for the pair. A turn per settle is the storm this coalescing
	// exists to prevent.
	time.Sleep(50 * time.Millisecond)
	if count := conversationRequests(completer); count != 2 {
		t.Fatalf("two settles produced %d requests, want the person's turn plus ONE wake", count)
	}
}

// A settle whose note the turn drained but never sent — it arrived after the
// last request — still gets answered: that is the one silence the queue alone
// used to leave.
func TestSettleAfterTheLastRequestGetsItsOwnTurn(t *testing.T) {
	var (
		inFlight = make(chan struct{})
		release  = make(chan struct{})
		woken    = make(chan []ai.Message, 2)
	)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(inFlight)
			<-release
			return textResponse("on it"), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			woken <- messages
			return textResponse("the comparison is at ~/oauth.md"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	events, err := agent.Submit(context.Background(), "kick that off")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	<-inFlight
	settleTask(t, agent, "research oauth", "wrote the comparison to ~/oauth.md")
	close(release)
	collect(t, events)

	select {
	case messages := <-woken:
		if !strings.Contains(userTextIn(messages), "wrote the comparison to ~/oauth.md") {
			t.Fatalf("the continuation turn did not carry the outcome:\n%s", userTextIn(messages))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a note drained at the end of a turn was never answered")
	}
}

// An INTERRUPTED turn is not resurrected by what it drained. A person who
// stopped the session gets the note in their transcript and silence.
func TestInterruptedTurnDoesNotWakeOnTheNoteItDrained(t *testing.T) {
	inFlight := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(inFlight)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	events, err := agent.Submit(context.Background(), "work on this")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	<-inFlight
	settleTask(t, agent, "research oauth", "wrote the comparison to ~/oauth.md")
	agent.Interrupt()
	collect(t, events)

	// The note is in the transcript — the person typed nothing, but the work did
	// land — and nothing spoke about it.
	waitFor(t, "the note to be drained into the transcript", func() bool {
		return strings.Contains(transcriptText(agent), "wrote the comparison to ~/oauth.md")
	})
	time.Sleep(100 * time.Millisecond)
	if count := conversationRequests(completer); count != 1 {
		t.Fatalf("an interrupted turn produced %d requests, want only the one that was stopped", count)
	}
}

// The AMBIENT lane — a background job's exit, a watch's news, a resume's
// account of an interrupt — queues and starts nothing.
func TestAmbientNoteDoesNotWakeAnIdleSession(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, nil)

	agent.enqueueAmbientNote("job 2 exited with status 1")
	time.Sleep(100 * time.Millisecond)

	if count := conversationRequests(completer); count != 0 {
		t.Fatalf("an ambient note started %d turns, want none", count)
	}
	if queued := steeringQueue(agent); len(queued) != 1 {
		t.Fatalf("the ambient note is not waiting on the queue: %v", queued)
	}
}

// A woken turn has no caller, so the session hands its stream to whoever
// subscribed for exactly this: the surface that has to draw the answer.
func TestWakesCarriesTheWokenTurnsStream(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the comparison is at ~/oauth.md"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	wakes := agent.Wakes()

	settleTask(t, agent, "research oauth", "wrote the comparison to ~/oauth.md")

	select {
	case stream := <-wakes:
		if stream == nil {
			t.Fatal("the wake lane carried a nil stream")
		}
		done := false
		for event := range stream {
			if event.Kind == EventTurnDone {
				done = true
			}
		}
		if !done {
			t.Fatal("the woken turn's stream never reported the turn ending")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("nothing was published on the wake lane")
	}
}

// The rail is a rail. A turn nobody asked for is still a turn that has to be
// paid for, so a session at its ceiling queues the note instead of spending.
func TestSpendRailRefusesToWake(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.SpendRailUSD = 1
	})
	agent.mu.Lock()
	agent.usage.CostUSD = 2
	agent.mu.Unlock()

	settleTask(t, agent, "research oauth", "wrote the comparison to ~/oauth.md")
	time.Sleep(100 * time.Millisecond)

	if count := conversationRequests(completer); count != 0 {
		t.Fatalf("a railed session spent %d requests on a wake", count)
	}
	if queued := steeringQueue(agent); len(queued) != 1 {
		t.Fatalf("the note was lost rather than queued: %v", queued)
	}
}

// A TASK NODE'S OWN AGENT never wakes itself: its turns belong to the runner
// driving it, and a second conversation inside a worktree is nobody's.
func TestTaskNodeAgentDoesNotWake(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.InTask = true
	})

	agent.enqueueSteering("task 1 finished: something landed")
	time.Sleep(100 * time.Millisecond)

	if count := conversationRequests(completer); count != 0 {
		t.Fatalf("a task node started %d turns of its own", count)
	}
}

// Work that settles INSIDE construction — recovery's cascade over the
// dependents of an interrupted node — queues rather than speaking to a room
// that does not exist yet.
func TestASettleBeforeTheSessionIsOpenDoesNotWake(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, nil)
	agent.mu.Lock()
	agent.opened = false
	agent.mu.Unlock()

	settleTask(t, agent, "research oauth", "wrote the comparison to ~/oauth.md")
	time.Sleep(100 * time.Millisecond)

	if count := conversationRequests(completer); count != 0 {
		t.Fatalf("a settle during construction started %d turns", count)
	}
	if !steeringContains(agent, "wrote the comparison to ~/oauth.md") {
		t.Fatalf("the note was lost rather than queued: %v", steeringQueue(agent))
	}
}

// Close ends the standing subscription rather than leaving a surface waiting
// for a wake that is never coming.
func TestWakesClosesWithTheSession(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	wakes := agent.Wakes()
	if err := agent.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case _, open := <-wakes:
		if open {
			t.Fatal("the wake lane delivered a turn after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the wake lane stayed open after Close")
	}
}

// A wake and a person speaking at the same instant must not produce two turns:
// the lock that starts one is the lock the other waits on.
func TestWakeAndSubmitDoNotBothStartATurn(t *testing.T) {
	var mu sync.Mutex
	concurrent, peak := 0, 0
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			mu.Lock()
			concurrent++
			if concurrent > peak {
				peak = concurrent
			}
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			mu.Lock()
			concurrent--
			mu.Unlock()
			return textResponse("ok"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		settleTask(t, agent, "research oauth", "wrote the comparison to ~/oauth.md")
	}()
	go func() {
		defer group.Done()
		events, err := agent.Submit(context.Background(), "and what about the other one?")
		if err != nil {
			return
		}
		collect(t, events)
	}()
	group.Wait()
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if peak > 1 {
		t.Fatalf("%d turns ran at once: a wake must never be a second conversation", peak)
	}
}
