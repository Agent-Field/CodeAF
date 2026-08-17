package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE LINE A CARD IS BUILT OUT OF CANNOT CARRY ORDERS.
//
// A gloss is the headline of the consent card and the row above the offer a
// person answers with one key. It is built out of a model's own arguments, and
// a terminal reads an escape in those as an instruction rather than as text —
// one that can move the cursor onto the offer and rewrite it, so that the
// question on screen says one thing and the call underneath it is another.
// Cutting the newline was never enough.

// escapedCall is one bash call whose command carries an escape, spelled the two
// ways it can arrive: as JSON's own escape, and as a raw byte in text that does
// not parse as JSON at all.
func escapedCall(t *testing.T, args string) Event {
	t.Helper()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-a", "bash", args), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	runs := make(chan string, 4)
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionPrompt}
		config.AskConsent = true
	})
	for index := range agent.tools {
		if agent.tools[index].Name == "bash" {
			agent.tools[index] = countingTool("bash", runs, nil)
		}
	}
	var asked Event
	drainAnswering(t, mustSubmit(t, agent, "run it"), func(request Event) {
		asked = request
		agent.ResolveConsent(request.ID, false)
	})
	if asked.Kind != EventConsentRequest {
		t.Fatal("the gate never asked")
	}
	return asked
}

func TestAGlossCarriesNoEscapeToTheCard(t *testing.T) {
	// JSON's own escape: six ordinary characters on the wire, one control byte
	// the moment anything unmarshals them.
	asked := escapedCall(t, `{"command":"git status\u001b[2Aallow? [y] yes"}`)
	if strings.ContainsAny(asked.Hint, "\x1b\x07\n\r") {
		t.Fatalf("the card's headline carries a control byte: %q", asked.Hint)
	}
	if !strings.Contains(asked.Hint, "git status") {
		t.Fatalf("the scrub took the text with the orders: %q", asked.Hint)
	}
	if strings.ContainsAny(asked.Args, "\x1b\x07") {
		t.Fatalf("the arguments carry a control byte: %q", asked.Args)
	}
}

func TestARawControlByteNeverReachesTheCardEither(t *testing.T) {
	// A raw byte inside a JSON string is not valid JSON, so the arguments pass
	// through as their own text — which is the path that used to carry it.
	asked := escapedCall(t, "{\"command\":\"git status\x1b[2A\"}")
	if strings.ContainsAny(asked.Hint, "\x1b\x07") {
		t.Fatalf("the card's headline carries a control byte: %q", asked.Hint)
	}
	if strings.ContainsAny(asked.Args, "\x1b\x07") {
		t.Fatalf("the arguments carry a control byte: %q", asked.Args)
	}
}

// The scrub drops the orders and keeps the text.
func TestScrubbedKeepsEverythingThatIsNotAnOrder(t *testing.T) {
	if got := scrubbed("git status --short"); got != "git status --short" {
		t.Fatalf("a plain line came back as %q", got)
	}
	if got := scrubbed("a\x1b[1Ab\x07c\nd"); got != "a[1Abcd" {
		t.Fatalf("the scrub left %q", got)
	}
	if got := scrubbed("naïve · résumé"); got != "naïve · résumé" {
		t.Fatalf("the scrub ate text outside ASCII: %q", got)
	}
}
