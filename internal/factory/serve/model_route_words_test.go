package serve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE PERSON READS THE ORGANIZATION'S SENTENCE, NOT A PAYMENT FAILURE. These tests
// run the organization's real model door over a real socket and a real engine turn
// against it, so the line they read is the one a hosted chat's person reads. A
// door whose body the client could not tell from a provider's was found live: a
// chat at its limit read "<service> accepted the key but the account cannot pay —
// This chat has reached its spending limit of $0.00 …", blaming the vendor and the
// person's account for a limit the organization set, and quoting a cap of a tenth
// of a cent as nothing.

// hostedTurnLine runs one real engine turn against the door of the chat and
// answers the sentence the turn ended on, with the service name the engine
// would put in front of a provider's payment refusal.
func hostedTurnLine(t *testing.T, rg *hostedRig, chat string) (line, service string) {
	t.Helper()
	door := httptest.NewServer(rg.h)
	t.Cleanup(door.Close)
	key, base := rg.host.tokenOf(chat), door.URL+"/v1/model"
	agent, err := session.New(session.Config{Workspace: t.TempDir(), Model: testHostedModel, APIKey: key, BaseURL: base})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	events, err := agent.Submit(ctx, "Hi")
	if err != nil {
		t.Fatal(err)
	}
	var failure error
	for event := range events {
		if event.Kind == session.EventError {
			failure = event.Err
		}
	}
	if failure == nil {
		t.Fatal("the turn ended without an error")
	}
	// The service the engine names is the one its own source set resolves for the
	// model, asked the same way (session's failureServiceWord).
	var none modelsource.Set
	resolved, _ := none.OrDefault(key, base).For(testHostedModel)
	return failure.Error(), strings.TrimSpace(resolved.Source.Written)
}

func TestAHostedChatAtItsSpendingLimitReadsTheOrganizationsSentenceAndNothingElse(t *testing.T) {
	// Contract 1 and 3: a limit of a tenth of a cent is spent by the first call.
	var calls int
	rg := newMeteredRig(t, 0.001, func(ctx context.Context, _, model string) (*ai.Response, error) {
		calls++
		return meteredAnswer(ctx, model, "paid", 0.001), nil
	})
	chat := rg.create(t, "santosh")
	if w := callHostedModel(rg, chat, testHostedBody); w.Code != http.StatusOK {
		t.Fatalf("the first call = %d %s", w.Code, w.Body)
	}

	line, service := hostedTurnLine(t, rg, chat)
	const want = "This chat has reached its spending limit of $0.001, so it is paused. Start a new chat to carry on."
	if line != want {
		t.Fatalf("the person read\n  %q\nwant exactly\n  %q", line, want)
	}
	if strings.TrimSpace(service) == "" {
		t.Fatal("the control is void: the engine resolved no service name to leave out")
	}
	for _, blame := range []string{service, "accepted the key", "cannot pay", "402", "API error"} {
		if strings.Contains(strings.ToLower(line), strings.ToLower(blame)) {
			t.Fatalf("the line carries %q, which is not the organization's sentence: %q", blame, line)
		}
	}
	if calls != 1 {
		t.Fatalf("the refused turn reached the upstream: %d calls", calls)
	}
}

func TestAHostedChatAtTheMonthlyLimitReadsTheOrganizationsSentenceAndNothingElse(t *testing.T) {
	// The person's monthly limit is the door's other 402 and had the same defect.
	rg := newMeteredRig(t, 0, func(context.Context, string, string) (*ai.Response, error) {
		t.Fatal("a refused turn reached the upstream")
		return nil, nil
	})
	space := myHostedSpace(t, rg, "santosh")
	if _, err := rg.st.AddSpend(space.Workstream.ID, store.Spend{
		At: rg.h.clock().Format(time.RFC3339Nano), USD: 20, Chat: "agent", By: "santosh",
	}); err != nil {
		t.Fatal(err)
	}
	chat := rg.create(t, "santosh")

	line, service := hostedTurnLine(t, rg, chat)
	const want = "You have reached your monthly limit of $20.00, so this chat is paused. The organization's operator can raise it in Settings."
	if line != want {
		t.Fatalf("the person read\n  %q\nwant exactly\n  %q", line, want)
	}
	if strings.Contains(line, service) || strings.Contains(line, "cannot pay") {
		t.Fatalf("the line blames a service or an account: %q", line)
	}
}

func TestAProvidersOwnPaymentRefusalThroughTheDoorStillReadsAsAnAccountThatCannotPay(t *testing.T) {
	// Contract 2: the door passes a refusal its upstream gave through untouched, and
	// that one IS a service's account failing, so it keeps its wording.
	rg := newMeteredRig(t, 0, func(context.Context, string, string) (*ai.Response, error) {
		return nil, &provider.APIError{Status: http.StatusPaymentRequired, Message: "Insufficient credits. Add more at the provider's settings."}
	})
	chat := rg.create(t, "santosh")

	line, service := hostedTurnLine(t, rg, chat)
	if strings.TrimSpace(service) == "" {
		t.Fatal("the control is void: the engine resolved no service name")
	}
	if !strings.HasPrefix(line, service+" accepted the key but the account cannot pay — ") ||
		!strings.Contains(line, "Insufficient credits. Add more at the provider's settings.") {
		t.Fatalf("a provider's own refusal read %q", line)
	}
	if strings.Contains(line, "spending limit") {
		t.Fatalf("a provider's refusal read as the organization's: %q", line)
	}
}
