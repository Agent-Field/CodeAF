package tui3

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A HOSTED CHAT AT THE ORGANIZATION'S LIMIT READS THE ORGANIZATION'S SENTENCE.
// The organization's model door refuses a chat that reached its spending limit
// with an HTTP 402 in its own words; the turn used to put "<service> accepted the
// key but the account cannot pay" in front of it, blaming the vendor and the
// person's account for a limit the organization chose (provider/orgdoor.go).
// These tests run a REAL engine turn against a server answering with the door's
// exact bytes, so the typed refusal they hand the page is the one a local engine
// raises, not one built by hand.

const orgDoorLimitSentence = "This chat has reached its spending limit of $0.001, so it is paused. Start a new chat to carry on."

// doorBody is the door's error exactly as internal/factory/serve writes it, keys
// in the order encoding/json emits them.
func doorBody() string {
	return fmt.Sprintf(`{"error":{"code":402,%q:%q,"message":%q,"type":"insufficient_quota"}}`,
		provider.DoorKey, provider.OrganizationDoor, orgDoorLimitSentence)
}

// refusedTurn runs one engine turn against a server that refuses it with status
// and body, and answers the error the turn ended on and the service the page
// would name for the model.
func refusedTurn(t *testing.T, status int, body string) (error, string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	const key, model = "test-key", "vendor/model"
	agent, err := session.New(session.Config{Workspace: t.TempDir(), Model: model, APIKey: key, BaseURL: server.URL})
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
	var none modelsource.Set
	resolved, _ := none.OrDefault(key, server.URL).For(model)
	return failure, strings.TrimSpace(resolved.Source.Written)
}

func TestAChatAtTheOrganizationsLimitDrawsOnlyTheOrganizationsSentence(t *testing.T) {
	failure, service := refusedTurn(t, http.StatusPaymentRequired, doorBody())
	if service == "" {
		t.Fatal("the control is void: the page resolved no service name to leave out")
	}
	// The page named a service for this model, as it does when it composes the
	// vendor line, and still said nothing of it.
	note := (&feed{}).failureNote(failure, service)
	if note != orgDoorLimitSentence {
		t.Fatalf("the page drew\n  %q\nwant exactly\n  %q", note, orgDoorLimitSentence)
	}
	forbidden(t, note)
	for _, blame := range []string{service, "accepted the key", "cannot pay"} {
		if strings.Contains(strings.ToLower(note), strings.ToLower(blame)) {
			t.Fatalf("the line carries %q: %q", blame, note)
		}
	}
	// A turn that had been asked again still draws the sentence and not a ladder.
	f := &feed{}
	f.asked, f.askedTurn = 3, f.turn
	if counted := f.failureNote(failure, service); counted != orgDoorLimitSentence {
		t.Fatalf("a counted turn drew %q", counted)
	}
}

func TestAProvidersOwnPaymentRefusalStillNamesTheServiceAndTheAccount(t *testing.T) {
	failure, service := refusedTurn(t, http.StatusPaymentRequired,
		`{"error":{"code":402,"message":"Insufficient credits. Add more at the provider's settings."}}`)
	note := (&feed{}).failureNote(failure, service)
	want := service + " accepted the key but the account cannot pay — Insufficient credits. Add more at the provider's settings."
	if note != want {
		t.Fatalf("a provider's refusal drew\n  %q\nwant\n  %q", note, want)
	}
}

func TestTheOrganizationsLimitDoesNotSendAReadOfThePersonsOwnCredits(t *testing.T) {
	a := placeApp(t)
	a.readCredits = func(context.Context) (credits.Reading, error) { return credits.Reading{}, nil }

	own, _ := refusedTurn(t, http.StatusPaymentRequired, doorBody())
	if a.creditRefusalEnded(own) {
		t.Fatal("the organization's limit asked for a read of the person's OpenRouter balance")
	}
	theirs, _ := refusedTurn(t, http.StatusPaymentRequired, `{"error":{"code":402,"message":"Insufficient credits."}}`)
	if !a.creditRefusalEnded(theirs) {
		t.Fatal("a provider's payment refusal stopped asking for a credit read")
	}
}
