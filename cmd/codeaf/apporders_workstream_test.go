package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/automation"
)

// A STANDING ORDER MADE FROM A WORKSTREAM IS PROPOSED: nothing runs before its
// switch, it belongs to the workstream, says its results nowhere but there, and
// shows the runs it would make before it is switched on.
func TestAnOrderMadeFromAWorkstreamIsProposedAndPreviewed(t *testing.T) {
	backend, _, store := ordersBackend(t)
	o, err := backend.CreateOrder(appwire.OrderCreate{When: "every 0 9 * * 1", Does: "Draft the release notes from last week's done tasks.", Workstream: "ws_pantry"})
	if err != nil {
		t.Fatal(err)
	}
	if o.State != appwire.OrderProposed || o.Workstream != "ws_pantry" || o.AnswersIn != appwire.AnswersNotHere || o.Trust != appwire.TrustAsksFirst || o.Kind != appwire.OrderKindOrder {
		t.Fatalf("the new order = %+v", o)
	}
	if o.Title != "Draft the release notes from last week's done tasks" || !strings.Contains(o.When, "Monday at 09:00") {
		t.Fatalf("title %q, when %q", o.Title, o.When)
	}
	if len(o.NextRuns) != appwire.OrderNextRunsMost || o.FiresAt != nil {
		t.Fatalf("a proposed order's preview = %v, fires at %v", o.NextRuns, o.FiresAt)
	}
	for i, at := range o.NextRuns {
		if at.Weekday() != time.Monday && at.In(time.Local).Weekday() != time.Monday {
			t.Fatalf("run %d is not a Monday: %v", i, at)
		}
		if i > 0 && at.Sub(o.NextRuns[i-1]) < 6*24*time.Hour {
			t.Fatalf("runs a week apart: %v", o.NextRuns)
		}
	}
	if due, _ := store.Due(time.Now().Add(30 * 24 * time.Hour)); len(due) != 0 {
		t.Fatalf("a proposed order is due: %+v", due)
	}
	// Switching it on is ratify, and it then has a time it fires.
	trusted := appwire.TrustTrusted
	on, err := backend.RatifyOrder(o.ID, &appwire.OrderEdits{Trust: &trusted})
	if err != nil || on.State != appwire.OrderActive || on.FiresAt == nil || on.Trust != appwire.TrustTrusted || len(on.NextRuns) == 0 {
		t.Fatalf("switched on = %+v, %v", on, err)
	}
}

func TestAnOrderWithAWhenNobodyCanReadIsNotMade(t *testing.T) {
	backend, _, store := ordersBackend(t)
	for _, when := range []string{"every Monday", "sometimes", "at yesterday 09:00"} {
		if _, err := backend.CreateOrder(appwire.OrderCreate{When: when, Does: "x", Workstream: "w"}); err == nil {
			t.Fatalf("an order was made with when %q", when)
		}
	}
	if all, _ := store.List(); len(all) != 0 {
		t.Fatalf("a refused order was kept: %+v", all)
	}
}

func TestOrdersPreviewIsTheClocksArithmetic(t *testing.T) {
	backend, _, _ := ordersBackend(t)
	p, err := backend.PreviewOrder("every 0 9 1 * *")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.When, "1st of every month") || len(p.NextRuns) != 3 || p.NextRuns[0].Day() != 1 {
		t.Fatalf("preview = %+v", p)
	}
	once, err := backend.PreviewOrder("in 2h")
	if err != nil || len(once.NextRuns) != 1 {
		t.Fatalf("a one-time preview = %+v, %v", once, err)
	}
	if _, err := backend.PreviewOrder("whenever"); err == nil {
		t.Fatal("a when nobody can read was previewed")
	}
}

// A RUN'S OWN OUTCOME RIDES BESIDE ITS RESULT, so "your call" and "stopped"
// are not read as a run that did not finish.
func TestAnOrderRunSaysItsOwnOutcome(t *testing.T) {
	for outcome, result := range map[automation.Outcome]string{
		automation.OutcomeYourCall: appwire.ResultIncomplete, automation.OutcomeStopped: appwire.ResultIncomplete,
		automation.OutcomeDone: appwire.ResultOK,
	} {
		v := orderRunView(automation.Run{ID: 7, Phase: automation.PhaseOver, Outcome: outcome, Finished: time.Now()})
		if v.Outcome != string(outcome) || v.Result != result {
			t.Fatalf("%s: %+v", outcome, v)
		}
	}
	if v := orderRunView(automation.Run{ID: 8, Phase: automation.PhaseRunning}); v.Outcome != "" {
		t.Fatalf("a running run has an outcome: %+v", v)
	}
}

// AN ORGANIZATION'S STANDING ORDER IS NEVER MADE ON A PERSON'S COMPUTER (E4
// F0): the clock here would run it as a general agent with this person's shell
// over their whole home. orders.create refuses it, saying where such orders
// run, and keeps nothing.
func TestAnOrganizationsOrderIsNotMadeOnThisComputer(t *testing.T) {
	backend, _, store := ordersBackend(t)
	_, err := backend.CreateOrder(appwire.OrderCreate{When: "every 1h", Does: "Check Pantry app for tasks due today.", Workstream: "org:conn-1/ws_pantry"})
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != appwire.CodeBadScope || wire.Message != automation.LineOrgWorkstream {
		t.Fatalf("an organization's order was made here: %v", err)
	}
	if all, _ := store.List(); len(all) != 0 {
		t.Fatalf("a refused order was kept: %+v", all)
	}
}
