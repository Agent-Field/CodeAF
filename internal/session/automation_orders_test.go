package session

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/automation"
)

// A STANDING ORDER IS SAVED AS PROPOSED BEFORE ITS CARD IS UP, named on the
// card, and runs nothing until the yes: the yes ratifies it, and a no takes
// it away again.
func TestAStandingOrderIsProposedBeforeTheCardAndRatifiedByTheYes(t *testing.T) {
	for _, yes := range []bool{true, false} {
		store := automationStoreFor(t)
		completer := &scriptedCompleter{steps: []step{
			automationCall("a1", map[string]any{
				"op": "propose", "title": "weekly update",
				"when": map[string]any{"every": "0 9 * * 1"}, "do": "draft the weekly update from the git log",
			}),
			finalText("done"),
		}}
		agent := automationAgent(t, completer, store, func(c *Config) { c.StandingOrders = true })
		events, err := agent.Submit(context.Background(), "every Monday at 9, draft the weekly update")
		if err != nil {
			t.Fatal(err)
		}
		drainAnsweringAutomation(t, events, func(event Event) {
			held, err := store.Get(event.Automation.Automation.ID)
			if err != nil || held.Status != automation.StatusProposed || held.Trust != automation.TrustAsksFirst {
				t.Errorf("while the card is up the order is %+v, %v", held, err)
			}
			if q := AutomationQuestion(*event.Automation); q.Subject.Ref != held.ID {
				t.Errorf("the card names %q, not the order %q", q.Subject.Ref, held.ID)
			}
			agent.ResolveAutomation(event.Automation.ID, AutomationAnswer{Save: yes})
		})
		all, _ := store.List()
		switch {
		case yes && (len(all) != 1 || all[0].Status != automation.StatusActive):
			t.Fatalf("after the yes: %+v", all)
		case !yes && len(all) != 0:
			t.Fatalf("after the no: %+v", all)
		}
	}
}

// AN ORDER AGREED TO FROM THE APP'S OWN LIST, while its card is still up, is
// not ratified twice when the card's yes arrives.
func TestAnOrderRatifiedElsewhereIsKeptByTheCardsYes(t *testing.T) {
	store := automationStoreFor(t)
	completer := &scriptedCompleter{steps: []step{
		automationCall("a1", map[string]any{
			"op": "propose", "title": "leave", "when": map[string]any{"in": "2h"}, "say": "time to leave",
		}),
		finalText("done"),
	}}
	agent := automationAgent(t, completer, store, func(c *Config) { c.StandingOrders = true })
	events, err := agent.Submit(context.Background(), "remind me to leave in two hours")
	if err != nil {
		t.Fatal(err)
	}
	drainAnsweringAutomation(t, events, func(event Event) {
		if _, err := store.Ratify(event.Automation.Automation.ID); err != nil {
			t.Error(err)
		}
		agent.ResolveAutomation(event.Automation.ID, AutomationAnswer{Save: true})
	})
	all, _ := store.List()
	if len(all) != 1 || all[0].Status != automation.StatusActive || all[0].Trust != "" {
		t.Fatalf("after the yes: %+v", all)
	}
}
