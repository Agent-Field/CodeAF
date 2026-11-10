package run

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A message sent from outside (AgentField's `message`, [inbox.Message]) is in
// the next step's brief exactly as a comment on an issue is.
func TestAMessageFromOutsideIsInTheNextStepsBrief(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := inbox.Entry{Source: "inbox:ops", ExternalID: "A1", Title: "Fix the login timeout", Repo: "acme/web"}
	if _, err := inbox.Add(context.Background(), st, e, now); err != nil {
		t.Fatal(err)
	}
	if _, err := inbox.Message(context.Background(), st, inbox.Said{Source: "inbox:ops", ID: "A1", Author: "dana", Text: "the timeout is 3s on mobile only"}, now); err != nil {
		t.Fatal(err)
	}
	it, err := inbox.Find(st, "inbox:ops", "A1")
	if err != nil {
		t.Fatal(err)
	}
	var brief []string
	for _, p := range itemContext(Job{Item: it}) {
		brief = append(brief, p...)
	}
	if got := strings.Join(brief, "\n"); !strings.Contains(got, "- dana: the timeout is 3s on mobile only") {
		t.Fatalf("the brief does not carry the message:\n%s", got)
	}
}
