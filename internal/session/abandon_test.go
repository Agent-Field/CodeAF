package session

// STOP IS STOP.
//
// These tests pin the law issue #265 was filed for: a person's stop must be able
// to end a turn even when the turn is parked on a wait that cannot be cancelled.
// They are deliberately NOT written against a flock — #264 removed that one
// concrete instance, and a test that only proved the flock was gone would prove
// nothing about the general guarantee. The wait here is a channel nobody ever
// closes, spliced onto the belt as a tool: an honest uncancellable wait, of a
// shape nothing in this package has any special knowledge of.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// wedgeTool is A WAIT THAT CANNOT BE CANCELLED, built rather than borrowed.
//
// It takes the turn's context and ignores it completely, which is the whole
// point: everything in this package that ends a turn politely works by cutting
// that context, and this is the shape of tool that made `stopping` unbounded.
// entered is closed once the call is in the wait, so a test never has to sleep
// to know the turn is parked.
func wedgeTool(entered chan<- struct{}) bare.Tool {
	return releasableWedgeTool(entered, make(chan struct{}))
}

// releasableWedgeTool is [wedgeTool] with a way OUT, for the one test that needs
// the left-behind turn to carry on running rather than to stay parked.
func releasableWedgeTool(entered chan<- struct{}, forever <-chan struct{}) bare.Tool {
	opened := false
	return bare.Tool{
		Name:        "wedge",
		Description: "waits forever",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(_ context.Context, _ json.RawMessage) (string, bool, error) {
			if !opened {
				opened = true
				close(entered)
			}
			<-forever
			return "never", false, nil
		},
	}
}

// wedged is an agent whose belt carries [wedgeTool], and the channel that says
// when the turn has actually parked on it.
func wedged(t *testing.T, completer Completer) (*Agent, chan struct{}, string) {
	t.Helper()
	return wedgedReleasable(t, completer, nil)
}

// wedgedReleasable is [wedged] with the wedge's own way out in the caller's hand.
// A nil channel is a wedge nothing ever opens, which is what every other test
// here wants.
func wedgedReleasable(t *testing.T, completer Completer, release <-chan struct{}) (*Agent, chan struct{}, string) {
	t.Helper()
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.SessionFile = journal })
	entered := make(chan struct{})
	if release == nil {
		release = make(chan struct{})
	}
	agent.mu.Lock()
	agent.tools = append(agent.tools, releasableWedgeTool(entered, release))
	definitions, err := toolDefinitions(agent.tools)
	agent.mu.Unlock()
	if err != nil {
		t.Fatalf("toolDefinitions: %v", err)
	}
	agent.mu.Lock()
	agent.definitions = definitions
	agent.mu.Unlock()
	return agent, entered, journal
}

// waitFor fails rather than hangs, which is the only honest way to test a thing
// whose defect was that it hung.
func waitUntilClosed(t *testing.T, what string, done <-chan struct{}, within time.Duration) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(within):
		t.Fatalf("%s did not happen within %s", what, within)
	}
}

// A TURN PARKED ON AN UNCANCELLABLE WAIT CAN STILL BE ENDED.
//
// This is the law, and the test is the shape of the measured failure: the turn
// is inside a tool that will never return, [Agent.Interrupt] therefore changes
// nothing about it, and the stream stays open for as long as the wait lasts —
// which in the measured run was four minutes and could have been forever.
// [Agent.Abandon] ends it, and the proof is that the events channel CLOSES.
func TestAnUncancellableWaitDoesNotKeepATurnFromBeingAbandoned(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "wedge", `{}`), nil
		},
	}}
	agent, entered, _ := wedged(t, completer)

	events, err := agent.Submit(context.Background(), "park on something that cannot be cancelled")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitUntilClosed(t, "the turn parking on the wedge", entered, 10*time.Second)

	// THE FIRST STAGE CHANGES NOTHING HERE, which is the premise of the whole
	// issue rather than an incidental fact: the tool never looks at the context,
	// so cancelling it is cancelling nothing.
	agent.Interrupt()
	select {
	case _, open := <-events:
		if !open {
			t.Fatal("the stream closed on Interrupt alone; this test is no longer testing an uncancellable wait")
		}
	case <-time.After(300 * time.Millisecond):
	}

	if _, letGo := agent.Abandon(AbandonStopTimeout); !letGo {
		t.Fatal("Abandon reported nothing to let go of while a turn was parked")
	}

	closed := make(chan struct{})
	go func() {
		for range events {
		}
		close(closed)
	}()
	waitUntilClosed(t, "the abandoned turn's stream closing", closed, 10*time.Second)
}

// AND THE NEXT THING THE PERSON TYPES OPENS AN ORDINARY TURN.
//
// A stop that freed the screen and left the session claiming to be busy would be
// half a stop: the surface would be usable and the box would not. The abandoned
// goroutine is still parked on the wedge for the life of this test, which is
// exactly the condition the assertion is worth making under.
func TestAnAbandonedTurnLeavesTheSessionFreeForTheNextPrompt(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "wedge", `{}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the next turn ran"), nil
		},
	}}
	agent, entered, _ := wedged(t, completer)

	if _, err := agent.Submit(context.Background(), "park"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitUntilClosed(t, "the turn parking on the wedge", entered, 10*time.Second)
	agent.Interrupt()
	agent.Abandon(AbandonStopTimeout)

	next, err := agent.Submit(context.Background(), "and now something else")
	if err != nil {
		t.Fatalf("the session refused the next prompt after an abandoned turn: %v", err)
	}
	// THE PROOF IS THAT IT ANSWERED AND ENDED. What it said is the scripted
	// completer's business and not this law's — the turn's own auxiliary calls
	// (the title, the memory reflex) take steps off the same script, so pinning
	// a sentence here would pin the number of errands a turn happens to make.
	before := completer.requests()
	var answered bool
	for event := range next {
		if event.Kind == EventTurnDone {
			answered = true
		}
	}
	if !answered {
		t.Fatal("the turn after an abandoned one never reached its end")
	}
	if completer.requests() <= before {
		t.Fatal("the turn after an abandoned one never reached the model")
	}
}

// AND THE TURN THAT WAS LEFT BEHIND NEVER WRITES INTO THE CONVERSATION AGAIN.
//
// THIS IS THE COST OF GIVING THE BATCH AN ESCAPE, and it has to be paid or the
// escape is worse than the wait it replaced. [waitBatch] returns while the tools
// are still running, so the loop under an abandoned turn reaches its step
// boundary — where it records one tool message per call — with a.messages that
// by now belongs to whatever turn the person started after they stopped this
// one. The wedge is released here on purpose, so the left-behind turn actually
// runs on and gets its chance to write.
func TestATurnThatWasLeftBehindNeverWritesIntoTheConversationAgain(t *testing.T) {
	released := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "wedge", `{}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the turn after it"), nil
		},
	}}
	agent, entered, _ := wedgedReleasable(t, completer, released)

	if _, err := agent.Submit(context.Background(), "park"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitUntilClosed(t, "the turn parking on the wedge", entered, 10*time.Second)
	agent.Interrupt()
	agent.Abandon(AbandonStopTimeout)

	// The abandoned turn is let out of its wait now, with every reason to write:
	// its batch has an answer, and the step below the batch records one tool
	// message per call.
	close(released)

	next, err := agent.Submit(context.Background(), "and now something else")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	for range next {
	}
	// Long enough for the left-behind goroutine to have reached the boundary it
	// would have written at.
	time.Sleep(time.Second)

	agent.mu.Lock()
	defer agent.mu.Unlock()
	for _, message := range agent.messages {
		if message.ToolCallID == "call-1" {
			t.Fatalf("the abandoned turn's tool result reached the conversation: %+v", message)
		}
	}
}

// EXACTLY ONE JOURNAL LINE, WITH THE TURN'S LAST KNOWN SPEND.
//
// The line is the acceptance and not the decoration: a turn nobody waits for
// never writes the seal that carries its cost, so without it a turn let go of at
// the bound is money spent with no record that anything was let go of at all.
// The second Abandon proves the ONE: a duplicated deadline writes no second line.
func TestAnAbandonedTurnWritesOneJournalLineWithItsLastKnownSpend(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "wedge", `{}`), nil
		},
	}}
	agent, entered, journal := wedged(t, completer)

	if _, err := agent.Submit(context.Background(), "park"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitUntilClosed(t, "the turn parking on the wedge", entered, 10*time.Second)
	agent.Interrupt()

	spend, letGo := agent.Abandon(AbandonStopTimeout)
	if !letGo {
		t.Fatal("Abandon reported nothing to let go of")
	}
	// The scripted response reports usage, so the turn HAS spent something by
	// the time it parks — which is what makes "last known spend" a measurement
	// here rather than an absence.
	if spend.Input == 0 && spend.Output == 0 {
		t.Fatalf("the abandoned turn reported no spend at all: %+v", spend)
	}
	// A SECOND DEADLINE WRITES NOTHING. Idempotence is what makes "exactly one
	// line" a property of the door rather than of its callers.
	if _, again := agent.Abandon(AbandonStopTimeout); again {
		t.Fatal("Abandon let go of a turn twice")
	}

	lines := abandonedLines(t, journal)
	if len(lines) != 1 {
		t.Fatalf("want exactly one abandoned line, got %d", len(lines))
	}
	line := lines[0]
	if line.Reason != string(AbandonStopTimeout) {
		t.Fatalf("the abandoned line says reason %q, want %q", line.Reason, AbandonStopTimeout)
	}
	if line.Input != spend.Input || line.Output != spend.Output {
		t.Fatalf("the line carries %d/%d tokens, the door reported %d/%d",
			line.Input, line.Output, spend.Input, spend.Output)
	}
}

// AND THE ABANDONED TURN'S COST STILL REACHES THE SESSION'S BOOKS.
//
// Money is counted per call rather than per turn (usage_ledger.go, issue #269),
// so a turn that never seals is not a turn whose money is lost — and this pins
// that the second stage did not quietly reintroduce the old hole.
func TestAnAbandonedTurnsCostIsStillOnTheSessionsMeter(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			response := toolResponse("call-1", "wedge", `{}`)
			cost := 0.25
			response.Usage.Cost = &cost
			return response, nil
		},
	}}
	agent, entered, _ := wedged(t, completer)

	if _, err := agent.Submit(context.Background(), "park"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitUntilClosed(t, "the turn parking on the wedge", entered, 10*time.Second)
	agent.Interrupt()
	spend, _ := agent.Abandon(AbandonStopTimeout)

	if spend.CostUSD <= 0 {
		t.Fatalf("the abandoned turn reported no cost: %+v", spend)
	}
	if got := agent.Usage().CostUSD; got < spend.CostUSD {
		t.Fatalf("the session's meter reads %v, less than the abandoned turn's own %v", got, spend.CostUSD)
	}
}

// THE IN-FLIGHT REQUEST IS ABORTED BY THE DOOR.
//
// Every provider request is built on the turn's context (internal/provider's
// client.go), so the abort is the cancellation reaching the transport. What this
// pins is that [Agent.Abandon] performs it — a door that ended the local waits
// and left a socket open would be a turn that goes on being billed for after
// nobody is listening.
func TestAbandonAbortsTheRequestThatIsStillInFlight(t *testing.T) {
	reached := make(chan struct{})
	aborted := make(chan error, 1)
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(reached)
			<-ctx.Done()
			aborted <- ctx.Err()
			return nil, ctx.Err()
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	if _, err := agent.Submit(context.Background(), "ask something that never answers"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitUntilClosed(t, "the request going out", reached, 10*time.Second)

	if _, letGo := agent.Abandon(AbandonStopTimeout); !letGo {
		t.Fatal("Abandon reported nothing to let go of while a request was in flight")
	}
	select {
	case err := <-aborted:
		if err == nil {
			t.Fatal("the request's context was not cancelled")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the in-flight request was never aborted")
	}
}

// A TOOL BATCH LEFT BEHIND REPORTS THE CALLS THAT NEVER CAME BACK.
//
// The batch's wait now has a second stage ([waitBatch]), and the slot of a call
// that was still running when the turn was let go of must say so rather than
// keep the seeded panic sentence — nothing panicked, and the transcript is the
// one place that difference is readable afterwards.
func TestAToolStillRunningWhenTheTurnIsLetGoIsRecordedAsUnfinished(t *testing.T) {
	slots := newBatchSlots([]ai.ToolCall{
		{ID: "a", Function: ai.ToolCallFunction{Name: "wedge"}},
		{ID: "b", Function: ai.ToolCallFunction{Name: "read"}},
	})
	slots.put(1, toolResult{text: "this one came back"})

	taken := slots.taken(false)
	if !strings.Contains(taken[0].text, "did not finish") || !taken[0].isError {
		t.Fatalf("the unfinished call reads %q, want the unfinished sentence", taken[0].text)
	}
	if taken[1].text != "this one came back" {
		t.Fatalf("a call that DID come back was overwritten: %q", taken[1].text)
	}
	// And a batch that finished normally keeps every seeded rule it always had.
	if got := slots.taken(true)[0].text; !strings.Contains(got, "panicked") {
		t.Fatalf("a finished batch's unwritten slot reads %q, want the panic seed", got)
	}
}

// abandonedLines reads back every `abandoned` line in one session's journal.
func abandonedLines(t *testing.T, path string) []journalAbandoned {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	var found []journalAbandoned
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry sessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type == "abandoned" && entry.Abandoned != nil {
			found = append(found, *entry.Abandoned)
		}
	}
	return found
}
