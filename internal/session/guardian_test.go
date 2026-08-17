package session

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the harness ─────────────────────────────────────────────────────────────

// guardianCompleter answers the guardian's question itself and hands every other
// request to the scripted completer underneath. Routing on the system prompt is
// what keeps the turn's script aligned: a guardian call that consumed a step
// would shift every later assertion by one.
type guardianCompleter struct {
	inner *scriptedCompleter

	mu       sync.Mutex
	asked    []string
	verdict  string
	failWith error
}

func (g *guardianCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	if len(messages) == 2 && messageContentText(messages[0]) == guardianPrompt {
		g.mu.Lock()
		g.asked = append(g.asked, messageContentText(messages[1]))
		verdict, failure := g.verdict, g.failWith
		g.mu.Unlock()
		if failure != nil {
			return nil, failure
		}
		return textResponse(verdict), nil
	}
	return g.inner.CompleteWithMessages(ctx, messages, options...)
}

func (g *guardianCompleter) questions() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.asked...)
}

// guardianAgent is one agent behind a prompting policy, with a belt tool that
// reports every execution.
func guardianAgent(t *testing.T, completer Completer, guardian bool) (*Agent, chan string) {
	t.Helper()
	runs := make(chan string, 4)
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ApprovalPolicy = promptAll()
		config.AskConsent = true
		config.Guardian = guardian
	})
	agent.tools = append(agent.tools, countingTool("touch", runs, nil))
	return agent, runs
}

// oneToolTurn is the script for a turn that calls touch once and then answers.
func oneToolTurn() *scriptedCompleter {
	return &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "touch", `{"path":"notes.md"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
}

func ranTool(runs chan string) bool {
	select {
	case <-runs:
		return true
	default:
		return false
	}
}

// ── the gate ────────────────────────────────────────────────────────────────

// ALLOW runs the call, nobody is asked, and the row carries the annotation
// saying who answered.
func TestGuardianAllowRunsTheCallAndSaysSo(t *testing.T) {
	completer := &guardianCompleter{inner: oneToolTurn(), verdict: "ALLOW"}
	agent, runs := guardianAgent(t, completer, true)

	events, err := agent.Submit(context.Background(), "touch it")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := drainAnswering(t, events, func(Event) {
		t.Errorf("the person was asked about a call the guardian allowed")
	})

	if !ranTool(runs) {
		t.Fatal("the guardian allowed the call and it did not run")
	}
	if count := countKind(collected, EventConsentRequest); count != 0 {
		t.Fatalf("consent requests: got %d, want 0", count)
	}
	note, ok := firstOfKind(collected, EventGuardianAllowed)
	if !ok {
		t.Fatal("a call ran on the guardian's word and no annotation said so")
	}
	if note.Tool != "touch" || note.Rule != "default" {
		t.Fatalf("annotation: tool %q rule %q, want touch/default", note.Tool, note.Rule)
	}
	if !strings.Contains(note.Args, "notes.md") {
		t.Fatalf("annotation args %q do not carry the call", note.Args)
	}

	// The question is about THIS call and carries what the call would do.
	asked := completer.questions()
	if len(asked) != 1 {
		t.Fatalf("guardian asked %d times, want 1", len(asked))
	}
	if !strings.Contains(asked[0], "tool: touch") ||
		!strings.Contains(asked[0], "notes.md") ||
		!strings.Contains(asked[0], "matched rule: default") {
		t.Fatalf("the guardian was asked a question missing the call: %q", asked[0])
	}
}

// The guardian is paid for out of the session's pocket, like every other
// auxiliary call, and charged to no turn.
func TestGuardianUsageFoldsIntoTheSessionAndNotTheTurn(t *testing.T) {
	completer := &guardianCompleter{inner: oneToolTurn(), verdict: "ALLOW"}
	agent, _ := guardianAgent(t, completer, true)

	events, err := agent.Submit(context.Background(), "touch it")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := drainAnswering(t, events, nil)
	done, ok := firstOfKind(collected, EventTurnDone)
	if !ok {
		t.Fatal("the turn never reported itself done")
	}

	session := agent.Usage()
	// The turn is the two scripted responses: 20 + 10 prompt tokens.
	if done.Usage.Input != 30 {
		t.Fatalf("turn input: got %d, want 30", done.Usage.Input)
	}
	// The session is those plus the guardian's own 10.
	if session.Input != 40 {
		t.Fatalf("session input: got %d, want 40 (the turn's 30 plus the guardian's 10)", session.Input)
	}
	if session.Turns != done.Usage.Turns {
		t.Fatalf("the guardian counted as a turn: session %d, turn %d", session.Turns, done.Usage.Turns)
	}
}

// ASK falls through to the person, exactly as if there were no guardian.
func TestGuardianAskFallsThroughToThePerson(t *testing.T) {
	completer := &guardianCompleter{inner: oneToolTurn(), verdict: "ASK"}
	agent, runs := guardianAgent(t, completer, true)

	events, err := agent.Submit(context.Background(), "touch it")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	asked := 0
	collected := drainAnswering(t, events, func(event Event) {
		asked++
		agent.ResolveConsent(event.ID, true)
	})

	if asked != 1 {
		t.Fatalf("the person was asked %d times, want 1", asked)
	}
	if !ranTool(runs) {
		t.Fatal("the person allowed the call and it did not run")
	}
	if count := countKind(collected, EventGuardianAllowed); count != 0 {
		t.Fatalf("a guardian that said ASK annotated %d rows", count)
	}
}

// A guardian that cannot answer — a dead provider, a model nobody can resolve, a
// reply that is not the word — is a guardian that changes nothing.
func TestGuardianFailuresFallThroughToThePerson(t *testing.T) {
	cases := []struct {
		name    string
		verdict string
		failure error
	}{
		{name: "provider error", failure: errors.New("upstream is on fire")},
		{name: "an explanation instead of a word", verdict: "ALLOW, since this only reads a file."},
		{name: "the other word", verdict: "ASK"},
		{name: "nothing at all", verdict: ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			completer := &guardianCompleter{inner: oneToolTurn(), verdict: test.verdict, failWith: test.failure}
			agent, runs := guardianAgent(t, completer, true)

			events, err := agent.Submit(context.Background(), "touch it")
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			asked := 0
			drainAnswering(t, events, func(event Event) {
				asked++
				agent.ResolveConsent(event.ID, true)
			})
			if asked != 1 {
				t.Fatalf("the person was asked %d times, want 1", asked)
			}
			if !ranTool(runs) {
				t.Fatal("the person allowed the call and it did not run")
			}
		})
	}
}

// Off is the default, and off means no call was made at all — not a call whose
// answer was ignored.
func TestGuardianIsOffUntilItIsTurnedOn(t *testing.T) {
	completer := &guardianCompleter{inner: oneToolTurn(), verdict: "ALLOW"}
	agent, _ := guardianAgent(t, completer, false)

	if agent.config.Guardian {
		t.Fatal("Config.Guardian is on without anybody saying so")
	}
	events, err := agent.Submit(context.Background(), "touch it")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	asked := 0
	drainAnswering(t, events, func(event Event) {
		asked++
		agent.ResolveConsent(event.ID, true)
	})

	if asked != 1 {
		t.Fatalf("the person was asked %d times, want 1", asked)
	}
	if questions := completer.questions(); len(questions) != 0 {
		t.Fatalf("the guardian was asked %d questions while switched off", len(questions))
	}
}

// The guardian never sees a call the policy refused outright: it can turn a
// prompt into an allow and it can do nothing else.
func TestGuardianNeverSeesADeniedCall(t *testing.T) {
	completer := &guardianCompleter{inner: oneToolTurn(), verdict: "ALLOW"}
	runs := make(chan string, 4)
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionDeny}
		config.AskConsent = true
		config.Guardian = true
	})
	agent.tools = append(agent.tools, countingTool("touch", runs, nil))

	events, err := agent.Submit(context.Background(), "touch it")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	drainAnswering(t, events, func(Event) {
		t.Error("a denied call became a question")
	})

	if ranTool(runs) {
		t.Fatal("a denied call ran")
	}
	if questions := completer.questions(); len(questions) != 0 {
		t.Fatalf("the guardian was asked about a call the rules already refused: %v", questions)
	}
}

// The verdict reader takes the word and the decorations a model wraps it in, and
// nothing else.
func TestGuardianVerdictIsTheWordAlone(t *testing.T) {
	allowed := []string{"ALLOW", "allow", " ALLOW ", "`ALLOW`", "**ALLOW**", "ALLOW.", "ALLOW\nbecause it reads"}
	for _, reply := range allowed {
		if !guardianSaysAllow(reply) {
			t.Errorf("guardianSaysAllow(%q) = false, want true", reply)
		}
	}
	refused := []string{"", "ASK", "I would ALLOW this", "yes", "safe", "ALLOWED", "ALLOW ASK"}
	for _, reply := range refused {
		if guardianSaysAllow(reply) {
			t.Errorf("guardianSaysAllow(%q) = true, want false", reply)
		}
	}
}

// NOBODY STANDS IN FOR THE PERSON ON WHAT LEAVES IN THEIR NAME. The guardian is
// on and would say ALLOW to anything, and a send is still put to the person.
func TestGuardianNeverAnswersForASend(t *testing.T) {
	service := &stubTransport{answer: `{"id":"sent-1"}`}
	hub := &fakeHub{connected: true, account: "you@example.test", transport: service}
	completer := &guardianCompleter{inner: &scriptedCompleter{steps: sendTurn()}, verdict: "ALLOW"}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.connectHub = hub
		config.ApprovalPolicy = promptAll()
		config.AskConsent = true
		config.Guardian = true
	})
	armGoogle(t, agent)

	collected := drainAnswering(t, mustSubmit(t, agent, "tell alice noon works"), func(event Event) {
		agent.ResolveConsent(event.ID, false)
	})

	if asked := completer.questions(); len(asked) != 0 {
		t.Fatalf("the guardian was consulted about a send: %v", asked)
	}
	if count := countKind(collected, EventConsentRequest); count != 1 {
		t.Fatalf("the person was asked %d times, want once", count)
	}
	if service.calls() != 0 {
		t.Fatal("a refused message left anyway")
	}
}
