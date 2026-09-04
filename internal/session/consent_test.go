package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the gate ────────────────────────────────────────────────────────────────

// promptAll is the policy a fresh install has: ask about everything.
func promptAll() *approval.Policy {
	return &approval.Policy{Default: approval.ActionPrompt}
}

// consentAgent builds an agent behind a policy, with one belt tool that
// reports every execution on the returned channel.
func consentAgent(t *testing.T, completer Completer, policy *approval.Policy, ask bool) (*Agent, chan string) {
	t.Helper()
	runs := make(chan string, 4)
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ApprovalPolicy = policy
		config.AskConsent = ask
	})
	agent.tools = append(agent.tools, countingTool("touch", runs, nil))
	return agent, runs
}

func toolCallTurn(names ...string) []step {
	steps := make([]step, 0, len(names)+1)
	for index, name := range names {
		id := "call-" + name + string(rune('0'+index))
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(id, name, "{}"), nil
		})
	}
	return append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	})
}

// drainAnswering drains one turn's stream to close, handing every consent
// request to answer as it arrives. It fails rather than hanging: a gate that
// blocks forever is exactly the fault worth catching here.
func drainAnswering(t *testing.T, events <-chan Event, answer func(Event)) []Event {
	t.Helper()
	var collected []Event
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event, open := <-events:
			if !open {
				return collected
			}
			collected = append(collected, event)
			if event.Kind == EventConsentRequest && answer != nil {
				answer(event)
			}
		case <-deadline:
			t.Fatalf("the turn never finished; events so far: %v", kinds(collected))
			return nil
		}
	}
}

func firstOfKind(events []Event, kind EventKind) (Event, bool) {
	for _, event := range events {
		if event.Kind == kind {
			return event, true
		}
	}
	return Event{}, false
}

func countKind(events []Event, kind EventKind) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}

// A deny rule refuses the call BEFORE it runs, and says which rule did it. The
// model is handed an ordinary error result: a refusal it can act on beats a
// turn that ends.
func TestDenyRefusesTheCallWithoutRunningIt(t *testing.T) {
	completer := &scriptedCompleter{steps: toolCallTurn("touch")}
	policy := &approval.Policy{
		Default: approval.ActionAllow,
		Tools:   map[string]approval.Action{"touch": approval.ActionDeny},
	}
	agent, runs := consentAgent(t, completer, policy, true)

	events := collect(t, mustSubmit(t, agent, "touch the file"))

	if len(runs) != 0 {
		t.Fatal("a denied tool ran")
	}
	failed, ok := firstOfKind(events, EventToolFailed)
	if !ok {
		t.Fatalf("no failure event; got %v", kinds(events))
	}
	if !strings.Contains(failed.Output, `denied by approval rule: tool "touch"`) {
		t.Fatalf("refusal = %q, want it to name the rule", failed.Output)
	}
	if countKind(events, EventConsentRequest) != 0 {
		t.Fatal("a deny rule asked the person a question it had already answered")
	}
	// The model reads the refusal off the transcript, in the tool's own slot.
	if got := messageText(lastToolMessage(t, agent)); !strings.Contains(got, "denied by approval rule") {
		t.Fatalf("tool result = %q", got)
	}
}

// prompt → allow runs the call, once, after the answer.
func TestPromptThenAllowRunsTheCall(t *testing.T) {
	completer := &scriptedCompleter{steps: toolCallTurn("touch")}
	agent, runs := consentAgent(t, completer, promptAll(), true)

	events := drainAnswering(t, mustSubmit(t, agent, "touch the file"), func(request Event) {
		agent.ResolveConsent(request.ID, true)
	})

	if len(runs) != 1 {
		t.Fatalf("the tool ran %d times, want 1", len(runs))
	}
	request, ok := firstOfKind(events, EventConsentRequest)
	if !ok {
		t.Fatalf("no consent request; got %v", kinds(events))
	}
	if request.ID == 0 || request.Tool != "touch" || request.Rule == "" {
		t.Fatalf("request = %+v, want an id, the tool and the rule", request)
	}
	if _, failed := firstOfKind(events, EventToolFailed); failed {
		t.Fatal("an approved call was recorded as a failure")
	}
}

// prompt → deny refuses it, and names the rule the question came from.
func TestPromptThenDenyRefusesTheCall(t *testing.T) {
	completer := &scriptedCompleter{steps: toolCallTurn("touch")}
	agent, runs := consentAgent(t, completer, promptAll(), true)

	events := drainAnswering(t, mustSubmit(t, agent, "touch the file"), func(request Event) {
		agent.ResolveConsent(request.ID, false)
	})

	if len(runs) != 0 {
		t.Fatal("a refused tool ran")
	}
	failed, ok := firstOfKind(events, EventToolFailed)
	if !ok {
		t.Fatalf("no failure event; got %v", kinds(events))
	}
	if !strings.Contains(failed.Output, "denied by the person") {
		t.Fatalf("refusal = %q, want it to say the person declined", failed.Output)
	}
}

// A prompt with nobody watching denies. Blocking would hang a headless run on
// a question with no reader; allowing would make "prompt" mean "allow"
// wherever the surface is not a terminal.
func TestPromptWithNoResolverDenies(t *testing.T) {
	completer := &scriptedCompleter{steps: toolCallTurn("touch")}
	agent, runs := consentAgent(t, completer, promptAll(), false)

	events := collect(t, mustSubmit(t, agent, "touch the file"))

	if len(runs) != 0 {
		t.Fatal("a call nobody approved ran")
	}
	failed, ok := firstOfKind(events, EventToolFailed)
	if !ok {
		t.Fatalf("no failure event; got %v", kinds(events))
	}
	if !strings.Contains(failed.Output, "needs approval but no resolver is attached") {
		t.Fatalf("refusal = %q", failed.Output)
	}
	if countKind(events, EventConsentRequest) != 0 {
		t.Fatal("a question was asked with nobody there to answer it")
	}
}

// An interrupt while a question is pending ends the turn. The deadline is the
// assertion: a gate that held the agent lock, or that waited on an answer that
// can no longer come, would deadlock here rather than fail.
func TestInterruptDuringPendingConsentEndsTheTurn(t *testing.T) {
	completer := &scriptedCompleter{steps: toolCallTurn("touch")}
	agent, runs := consentAgent(t, completer, promptAll(), true)

	events := drainAnswering(t, mustSubmit(t, agent, "touch the file"), func(Event) {
		// The answer never comes; the person hits escape instead.
		agent.Interrupt()
	})

	if len(runs) != 0 {
		t.Fatal("the tool ran after the turn was interrupted")
	}
	if last := events[len(events)-1]; last.Kind != EventTurnDone {
		t.Fatalf("interrupted turn ended with %v, want EventTurnDone", last.Kind)
	}
	if _, pending := firstOfKind(events, EventConsentRequest); !pending {
		t.Fatal("the turn never asked, so the interrupt proved nothing")
	}
	if left := agent.PendingConsent(); len(left) != 0 {
		t.Fatalf("%d abandoned questions are still registered", len(left))
	}
	// The agent is free, not wedged.
	if _, err := agent.Submit(context.Background(), "never mind"); err != nil {
		t.Fatalf("Submit after the interrupt: %v", err)
	}
}

// "Don't ask me again" is answered once and holds for the rest of the session.
func TestRememberedConsentIsNotAskedTwice(t *testing.T) {
	completer := &scriptedCompleter{steps: toolCallTurn("touch", "touch")}
	agent, runs := consentAgent(t, completer, promptAll(), true)

	events := drainAnswering(t, mustSubmit(t, agent, "touch it twice"), func(request Event) {
		agent.ResolveConsentRemember(request.ID, true, ConsentToolSession)
	})

	if asked := countKind(events, EventConsentRequest); asked != 1 {
		t.Fatalf("the person was asked %d times, want 1", asked)
	}
	if len(runs) != 2 {
		t.Fatalf("the tool ran %d times, want 2 — the second call rode the remembered answer", len(runs))
	}
	if _, failed := firstOfKind(events, EventToolFailed); failed {
		t.Fatal("a remembered approval was recorded as a failure")
	}
}

// The memo answers a question; it does not overrule a rule. A tool the policy
// DENIES is refused even after the person said "always allow" for it, because
// nobody was ever asked about it.
func TestARememberedAnswerDoesNotOverruleADenyRule(t *testing.T) {
	completer := &scriptedCompleter{steps: toolCallTurn("touch")}
	policy := &approval.Policy{
		Default: approval.ActionPrompt,
		Tools:   map[string]approval.Action{"touch": approval.ActionDeny},
	}
	agent, runs := consentAgent(t, completer, policy, true)
	agent.rememberConsent("touch", true)

	collect(t, mustSubmit(t, agent, "touch the file"))

	if len(runs) != 0 {
		t.Fatal("a denied tool ran on a remembered answer")
	}
}

// Consent is ephemera: the journal holds what was DONE, and a call that was
// asked about — approved or not — leaves the record any other call leaves.
func TestConsentIsNeverJournaled(t *testing.T) {
	path := t.TempDir() + "/session.jsonl"
	completer := &scriptedCompleter{steps: toolCallTurn("touch")}
	runs := make(chan string, 2)
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ApprovalPolicy = promptAll()
		config.AskConsent = true
		config.SessionFile = path
	})
	agent.tools = append(agent.tools, countingTool("touch", runs, nil))

	drainAnswering(t, mustSubmit(t, agent, "touch the file"), func(request Event) {
		agent.ResolveConsent(request.ID, false)
	})
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	for _, line := range readLines(t, path) {
		if strings.Contains(line, "consent") || strings.Contains(line, `"type":"approval"`) {
			t.Fatalf("the journal recorded a question: %s", line)
		}
	}
}

// An unanswered approval is not a no. The surface used to arm a hidden ~10s
// clock that recorded "denied" and cancelled the call (F41). The engine never
// does that: silence keeps the question up, the work stays blocked — not
// refused — and a late yes still runs it.
func TestAnUnansweredApprovalPastTheTimeoutDoesNotDeny(t *testing.T) {
	completer := &scriptedCompleter{steps: toolCallTurn("touch")}
	agent, runs := consentAgent(t, completer, promptAll(), true)

	stream := mustSubmit(t, agent, "touch the file")
	var seen []Event
	var request Event
	deadline := time.After(2 * time.Second)
	for request.Kind != EventConsentRequest {
		select {
		case event, open := <-stream:
			if !open {
				t.Fatalf("the turn ended unanswered; events: %v", kinds(seen))
			}
			seen = append(seen, event)
			if event.Kind == EventToolFailed {
				t.Fatalf("silence recorded a denial: %q", event.Output)
			}
			if event.Kind == EventConsentRequest {
				request = event
			}
		case <-deadline:
			t.Fatalf("no consent request; events: %v", kinds(seen))
		}
	}
	if request.Wait != ConsentWaiting {
		t.Fatalf("Wait = %q, want %q — the question must say silence is not a no", request.Wait, ConsentWaiting)
	}
	if len(runs) != 0 {
		t.Fatal("the tool ran before anyone answered")
	}

	// Past any instant deny, and past a slice of the old hidden timer, with
	// nobody answering: the engine has no such clock.
	time.Sleep(50 * time.Millisecond)
	select {
	case event, open := <-stream:
		if !open {
			t.Fatal("silence ended the turn")
		}
		seen = append(seen, event)
		if event.Kind == EventToolFailed || event.Kind == EventTurnDone {
			t.Fatalf("silence cancelled the work: %v %q", event.Kind, event.Output)
		}
	default:
	}
	pending := agent.PendingConsent()
	if len(pending) != 1 || pending[0] != request.ID {
		t.Fatalf("pending = %v, want the unanswered question still registered", pending)
	}
	if len(runs) != 0 {
		t.Fatal("the unanswered call ran")
	}

	// The work is still there to approve.
	agent.ResolveConsent(request.ID, true)
	rest := drainAnswering(t, stream, nil)
	seen = append(seen, rest...)
	if len(runs) != 1 {
		t.Fatalf("the tool ran %d times after the late yes, want 1", len(runs))
	}
	if failed, ok := firstOfKind(seen, EventToolFailed); ok {
		t.Fatalf("the late yes still recorded a denial: %q", failed.Output)
	}
}

// A wait that ends without an answer is not a person's no. A timeout and a
// cancelled turn must say so in those words; "denied by the person" is a lie
// about a click that never happened (R2).
func TestAConsentTimeoutIsNotWordedAsDeniedByThePerson(t *testing.T) {
	agent, _ := consentAgent(t, &scriptedCompleter{}, promptAll(), true)
	hub := newEventHub()
	call := ai.ToolCall{ID: "c1"}
	call.Function.Name = "touch"
	call.Function.Arguments = "{}"

	timedOut, cancelTimeout := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelTimeout()
	result, allowed := agent.approve(timedOut, hub, call)
	if allowed {
		t.Fatal("a timed-out call ran")
	}
	if strings.Contains(result.text, "denied by the person") {
		t.Fatalf("timeout refusal = %q, which blames a person who was not asked", result.text)
	}
	if !strings.Contains(result.text, "not approved: the question timed out") {
		t.Fatalf("timeout refusal = %q, want it to say the question timed out", result.text)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	result, allowed = agent.approve(cancelled, hub, call)
	if allowed {
		t.Fatal("a cancelled call ran")
	}
	if strings.Contains(result.text, "denied by the person") {
		t.Fatalf("cancel refusal = %q, which blames a person who was not asked", result.text)
	}
	if !strings.Contains(result.text, "not approved: ended before an answer") {
		t.Fatalf("cancel refusal = %q, want it to say the wait ended before an answer", result.text)
	}
}

// In default (prompt) mode a look is not a question. read / ls / grep / find,
// a tasks look, and `git status` run without a card; a write still asks.
func TestAReadOnlyCallIsNotGated(t *testing.T) {
	agent, _ := consentAgent(t, &scriptedCompleter{}, promptAll(), true)
	ctx := context.Background()
	// No hub: a prompt would refuse "no resolver"; an allow runs. That is
	// the whole assertion — a look must never reach ask().
	looks := []ai.ToolCall{
		gateCall("read", `{"path":"x"}`),
		gateCall("ls", `{"path":"."}`),
		gateCall("grep", `{"pattern":"x"}`),
		gateCall("find", `{"pattern":"*.go"}`),
		gateCall("tasks", `{"id":1}`),
		gateCall(approval.ToolBash, `{"command":"git status"}`),
	}
	for _, call := range looks {
		result, allowed := agent.approve(ctx, nil, call)
		if !allowed {
			t.Fatalf("%s was gated: %q", call.Function.Name, result.text)
		}
	}

	result, allowed := agent.approve(ctx, nil, gateCall("write", `{"path":"x"}`))
	if allowed {
		t.Fatal("write ran without a person in default mode")
	}
	if !strings.Contains(result.text, "needs approval but no resolver is attached") {
		t.Fatalf("write refusal = %q, want the no-resolver sentence", result.text)
	}
}

func lastToolMessage(t *testing.T, a *Agent) ai.Message {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	for index := len(a.messages) - 1; index >= 0; index-- {
		if a.messages[index].Role == "tool" {
			return a.messages[index]
		}
	}
	t.Fatal("no tool message in the transcript")
	return ai.Message{}
}
