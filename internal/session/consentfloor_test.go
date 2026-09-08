package session

import (
	"context"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// WHAT A MEMO DOES NOT ANSWER, AND WHAT A BANKED RULE MEANS.
//
// The memo is one person saying they are done being asked about a tool. Two
// things it was quietly answering that nobody ever put to them: the critical
// shapes, and the calls that leave this machine in their name. Both of those
// answer PROMPT rather than deny (internal/approval), so a memo consulted ahead
// of them swallowed them whole — one approved `git status` bought silence for
// `rm -rf /` for the rest of the session.

// bashCallTurn is one turn that asks for these shell commands in order, one per
// step, so the answers to the earlier ones are in hand before the later ones
// are judged.
func bashCallTurn(commands ...string) []step {
	steps := make([]step, 0, len(commands)+1)
	for index, command := range commands {
		id := "call-" + string(rune('a'+index))
		args := `{"command":` + quoteJSON(command) + `}`
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(id, "bash", args), nil
		})
	}
	return append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	})
}

func quoteJSON(text string) string {
	out := make([]byte, 0, len(text)+2)
	out = append(out, '"')
	for index := 0; index < len(text); index++ {
		if text[index] == '"' || text[index] == '\\' {
			out = append(out, '\\')
		}
		out = append(out, text[index])
	}
	return string(append(out, '"'))
}

func bashAgent(t *testing.T, policy *approval.Policy, commands ...string) (*Agent, chan string) {
	t.Helper()
	completer := &scriptedCompleter{steps: bashCallTurn(commands...)}
	runs := make(chan string, 8)
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ApprovalPolicy = policy
		config.AskConsent = true
	})
	// THE BELT'S OWN BASH IS TAKEN OFF. The floor is a table of commands that
	// destroy a disk or drop the machine, so the one thing these tests must never
	// do is let one through to a shell — the assertion is that the gate ASKED, and
	// a test that proved it by running `rm -rf /` would be proving it once.
	for index := range agent.tools {
		if agent.tools[index].Name == "bash" {
			agent.tools[index] = countingTool("bash", runs, nil)
		}
	}
	return agent, runs
}

// THE CRITICAL FLOOR IS ASKED WHATEVER THE MEMO SAYS. An ordinary command still
// rides the memo — that is what the memo is for — and the shape that destroys a
// disk still stops and asks.
func TestTheMemoDoesNotAnswerForACriticalCommand(t *testing.T) {
	policy := &approval.Policy{
		Default:      approval.ActionAllow,
		BashPatterns: []approval.Rule{{Match: "ls*", Action: approval.ActionPrompt}},
	}
	agent, runs := bashAgent(t, policy, "ls -la", "ls /tmp", "rm -rf /")

	var asked []Event
	events := drainAnswering(t, mustSubmit(t, agent, "clean up"), func(request Event) {
		asked = append(asked, request)
		agent.ResolveConsentRemember(request.ID, true, ConsentToolSession)
	})

	if len(asked) != 2 {
		t.Fatalf("the person was asked %d times, want 2 — the second ls rode the memo and the deletion did not", len(asked))
	}
	if asked[1].Rule == "" || asked[1].Hint == "" {
		t.Fatalf("the second question says nothing about itself: %+v", asked[1])
	}
	if got := asked[1].Rule; got != `critical command "rm -rf /"` {
		t.Fatalf("the second question is about %q, want the critical shape", got)
	}
	if len(runs) != 3 {
		t.Fatalf("%d calls ran, want 3", len(runs))
	}
	if countKind(events, EventToolFailed) != 0 {
		t.Fatal("a call was refused")
	}
}

// AND THE CALLS THAT ACT IN THE PERSON'S NAME. A memo about the tool cannot
// vouch for a message that had not been written when it was made.
func TestTheMemoDoesNotAnswerForACallThatActsInThePersonsName(t *testing.T) {
	completer := &scriptedCompleter{steps: toolCallTurn("gmail_send")}
	policy := &approval.Policy{Default: approval.ActionAllow}
	agent, runs := consentAgent(t, completer, policy, true)
	agent.tools = append(agent.tools, countingTool("gmail_send", runs, nil))
	agent.rememberConsent("gmail_send", true)

	var asked int
	drainAnswering(t, mustSubmit(t, agent, "send it"), func(request Event) {
		asked++
		agent.ResolveConsent(request.ID, true)
	})
	if asked != 1 {
		t.Fatalf("the person was asked %d times, want 1", asked)
	}
}

// A memo still answers everything else, which is the half of it that is not
// being taken away.
func TestTheMemoStillAnswersAnOrdinaryCommand(t *testing.T) {
	policy := &approval.Policy{Default: approval.ActionPrompt}
	agent, runs := bashAgent(t, policy, "make build", "make test")

	var asked int
	drainAnswering(t, mustSubmit(t, agent, "check the tree"), func(request Event) {
		asked++
		agent.ResolveConsentRemember(request.ID, true, ConsentToolSession)
	})
	if asked != 1 {
		t.Fatalf("the person was asked %d times, want 1", asked)
	}
	if len(runs) != 2 {
		t.Fatalf("%d calls ran, want 2", len(runs))
	}
}

// ── the banked rule ─────────────────────────────────────────────────────────

// A [ConsentRule] ANSWER WRITES NO MEMO. The surface banked a shape into the
// person's own settings, and the memo beside it would be the coarse tool-wide
// yes the card never offered: the card says "always, this command", and the memo
// is keyed by tool name alone.
func TestARuleScopedAnswerLeavesNoToolWideMemo(t *testing.T) {
	policy := &approval.Policy{Default: approval.ActionPrompt}
	agent, runs := bashAgent(t, policy, "make build", "curl example.com")

	var asked []Event
	drainAnswering(t, mustSubmit(t, agent, "look around"), func(request Event) {
		asked = append(asked, request)
		agent.ResolveConsentRemember(request.ID, true, ConsentRule)
	})

	if len(asked) != 2 {
		t.Fatalf("the person was asked %d times, want 2 — a banked rule is not a tool-wide yes", len(asked))
	}
	if _, known := agent.rememberedConsent("bash"); known {
		t.Fatal("a rule-scoped answer left a memo for the whole tool")
	}
	if len(runs) != 2 {
		t.Fatalf("%d calls ran, want 2 — both were allowed", len(runs))
	}
}

// An unknown scope is still read as the narrow one, and [ConsentRule] is not
// one: a typo must not widen anything, and it must not silence anything either.
func TestAnUnknownScopeIsStillReadAsOnce(t *testing.T) {
	policy := &approval.Policy{Default: approval.ActionPrompt}
	agent, _ := bashAgent(t, policy, "make build", "make build")

	var asked int
	drainAnswering(t, mustSubmit(t, agent, "twice"), func(request Event) {
		asked++
		agent.ResolveConsentRemember(request.ID, true, ConsentScope("forever"))
	})
	if asked != 2 {
		t.Fatalf("the person was asked %d times, want 2", asked)
	}
}

// ── which call the question is about ────────────────────────────────────────

// THE QUESTION CARRIES THE CALL'S ID. A surface pairs it to the row it draws it
// under, and the card reads the command it is about to remember off that row.
func TestAConsentRequestNamesTheCallItIsAbout(t *testing.T) {
	policy := &approval.Policy{Default: approval.ActionPrompt}
	agent, _ := bashAgent(t, policy, "make build")

	var asked []Event
	drainAnswering(t, mustSubmit(t, agent, "check it"), func(request Event) {
		asked = append(asked, request)
		agent.ResolveConsent(request.ID, false)
	})
	if len(asked) != 1 {
		t.Fatalf("the person was asked %d times, want 1", len(asked))
	}
	if asked[0].CallID != "call-a" {
		t.Fatalf("the question names call %q, want the one it is about", asked[0].CallID)
	}
}
