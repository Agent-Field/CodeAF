package main

import (
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/automation"
)

// ordersBackend is a backend over an automations store of its own.
func ordersBackend(t *testing.T) (*appBackend, *appRecorder, *automation.Store) {
	t.Helper()
	store, err := automation.Open(filepath.Join(t.TempDir(), "automations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rec := newAppRecorder()
	backend := newFakeBackend(rec, nil)
	t.Cleanup(func() { backend.Close() })
	backend.orderStore = func() *automation.Store { return store }
	return backend, rec, store
}

// proposedOrder is work an agent proposed, waiting for the person.
func proposedOrder(t *testing.T, store *automation.Store) automation.Automation {
	t.Helper()
	a, err := store.Create(automation.Automation{
		Title: "weekly update", Schedule: automation.Schedule{Every: "1h", Zone: "UTC"},
		Action: automation.Action{Do: "draft the weekly update"}, Workspace: t.TempDir(),
		Status: automation.StatusProposed, Trust: automation.TrustAsksFirst,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// A PROPOSED ORDER IS LISTED AS PROPOSED AND ASKING FIRST, and ratifying it
// from the list makes it active with a time it fires.
func TestAppOrdersListAndRatify(t *testing.T) {
	backend, _, store := ordersBackend(t)
	a := proposedOrder(t, store)
	orders, err := backend.Orders("")
	if err != nil || len(orders) != 1 {
		t.Fatalf("orders = %+v, %v", orders, err)
	}
	o := orders[0]
	if o.State != appwire.OrderProposed || o.Trust != appwire.TrustAsksFirst || o.Kind != appwire.OrderKindOrder || o.FiresAt != nil || o.AnswersIn != appwire.AnswersChat {
		t.Fatalf("the proposed order = %+v", o)
	}
	every, usd := "every 30m", 2.5
	o, err = backend.RatifyOrder(a.ID, &appwire.OrderEdits{When: &every, BudgetPerRunUSD: &usd})
	if err != nil || o.State != appwire.OrderActive || o.FiresAt == nil || o.BudgetPerRunUSD != 2.5 || o.WhenExact == "" {
		t.Fatalf("ratified = %+v, %v", o, err)
	}
	if _, err := backend.RatifyOrder(a.ID, nil); err == nil {
		t.Fatal("an active order was ratified twice")
	}
}

// NOT NOW TAKES A PROPOSED ORDER AWAY, and is refused for one agreed to.
func TestAppOrderNotNow(t *testing.T) {
	backend, rec, store := ordersBackend(t)
	a := proposedOrder(t, store)
	if err := backend.NotNowOrder(a.ID); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		r, ok := data.(appwire.OrderRemoved)
		return ok && r.ID == a.ID
	})
	if _, err := store.Get(a.ID); !errors.Is(err, automation.ErrNotFound) {
		t.Fatalf("the order outlived not now: %v", err)
	}
	b := proposedOrder(t, store)
	if _, err := store.Ratify(b.ID); err != nil {
		t.Fatal(err)
	}
	if err := backend.NotNowOrder(b.ID); err == nil {
		t.Fatal("not now took away an order already agreed to")
	}
}

// A RUN THAT ASKS FIRST IS SAID AS order.asking AND ANSWERED THROUGH ITS ASK:
// not right puts the ladder back to the bottom.
func TestAppOrderRunAsksAndIsAnswered(t *testing.T) {
	backend, rec, store := ordersBackend(t)
	a := proposedOrder(t, store)
	if _, err := store.Ratify(a.ID); err != nil {
		t.Fatal(err)
	}
	cursor, _ := store.Cursor()
	run, err := store.QueueAsking(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	backend.ordersOnce(store, cursor, map[string]string{})
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		f, ok := data.(appwire.OrderFired)
		return ok && event == appwire.EventOrderAsking && f.Run.AskID == "order:"+strconv.FormatInt(run.ID, 10)
	})
	orders, _ := backend.Orders("")
	if len(orders) != 1 || orders[0].Asking == nil || orders[0].Asking.ID != run.ID {
		t.Fatalf("the asking order = %+v", orders)
	}
	if err := backend.AnswerAsk("", "order:"+strconv.FormatInt(run.ID, 10), appwire.VerdictNotRight, ""); err != nil {
		t.Fatal(err)
	}
	orders, _ = backend.Orders("")
	if orders[0].Asking != nil || orders[0].LastRun == nil || orders[0].LastRun.Result != appwire.ResultNotRight || orders[0].Trust != appwire.TrustAsksFirst {
		t.Fatalf("after not right = %+v", orders[0])
	}
}

// RUN NOW IS THE PERSON'S GO: work that asks first is queued already granted,
// and counts one step up the ladder.
func TestAppOrderRunNowIsItsOwnGo(t *testing.T) {
	backend, _, store := ordersBackend(t)
	a := proposedOrder(t, store)
	if _, err := backend.RunOrderNow(a.ID); err == nil {
		t.Fatal("a proposed order ran")
	}
	if _, err := store.Ratify(a.ID); err != nil {
		t.Fatal(err)
	}
	o, err := backend.RunOrderNow(a.ID)
	if err != nil || o.ApprovedRuns != 1 || o.Trust != appwire.TrustEarning {
		t.Fatalf("after run now = %+v, %v", o, err)
	}
	queued, _ := store.Queued()
	if len(queued) != 1 {
		t.Fatalf("queued = %+v", queued)
	}
	if got, _ := store.Get(a.ID); got.Granted != queued[0].ID {
		t.Fatalf("the run is not the granted one: %d vs %d", got.Granted, queued[0].ID)
	}
}

// PAUSE KEEPS THE TRUST, AND STOP IS FOR GOOD.
func TestAppOrderPauseResumeStop(t *testing.T) {
	backend, _, store := ordersBackend(t)
	a := proposedOrder(t, store)
	if _, err := store.Ratify(a.ID); err != nil {
		t.Fatal(err)
	}
	trusted := appwire.TrustTrusted
	if _, err := backend.ChangeOrder(a.ID, appwire.OrderEdits{Trust: &trusted}); err != nil {
		t.Fatal(err)
	}
	o, err := backend.SetOrderState(a.ID, appwire.OrderPaused)
	if err != nil || o.State != appwire.OrderPaused || o.Trust != appwire.TrustTrusted {
		t.Fatalf("paused = %+v, %v", o, err)
	}
	if o, err = backend.SetOrderState(a.ID, appwire.OrderActive); err != nil || o.State != appwire.OrderActive {
		t.Fatalf("resumed = %+v, %v", o, err)
	}
	if o, err = backend.SetOrderState(a.ID, appwire.OrderStopped); err != nil || o.State != appwire.OrderStopped {
		t.Fatalf("stopped = %+v, %v", o, err)
	}
	if _, err := backend.SetOrderState(a.ID, appwire.OrderActive); err == nil {
		t.Fatal("a stopped order was resumed")
	}
}

// A RUN WAITING ON THE PERSON IS AN ASK LIKE ANY OTHER: opened for the
// Inbox, listed for its agent under the run's id, and closed as answered here
// once the person answers it.
func TestAppOrderRunIsAnAskEverywhere(t *testing.T) {
	backend, rec, store := ordersBackend(t)
	transcript := filepath.Join(t.TempDir(), "places", "proj", "abcd1234", "transcript.jsonl")
	a, err := store.Create(automation.Automation{
		Title: "weekly update", Schedule: automation.Schedule{Every: "1h", Zone: "UTC"},
		Action: automation.Action{Do: "draft the weekly update"}, Workspace: t.TempDir(),
		Status: automation.StatusProposed, Trust: automation.TrustAsksFirst,
		Origin: automation.Origin{Transcript: transcript},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Ratify(a.ID); err != nil {
		t.Fatal(err)
	}
	run, err := store.QueueAsking(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	agent, id := appAgentID(transcript), "order:"+strconv.FormatInt(run.ID, 10)
	open := map[int64]appwire.Ask{}
	backend.sayOrderAsks(store, open)
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		opened, ok := data.(appwire.AskOpened)
		return ok && opened.Ask.ID == id && opened.Ask.Agent == agent && opened.Ask.Subkind == appwire.SubkindOrderRun
	})
	if asks := backend.orderAsksOf(agent); len(asks) != 1 || asks[0].ID != id || asks[0].Subject == nil || asks[0].Subject.Ref != a.ID {
		t.Fatalf("the agent's asks = %+v", asks)
	}
	if err := backend.AnswerAsk(agent, id, appwire.VerdictGo, ""); err != nil {
		t.Fatal(err)
	}
	backend.sayOrderAsks(store, open)
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		closed, ok := data.(appwire.AskClosed)
		return ok && closed.ID == id && closed.By == appwire.ClosedByApp && closed.Option == appwire.VerdictGo
	})
	if asks := backend.orderAsksOf(agent); len(asks) != 0 {
		t.Fatalf("an answered run still asks: %+v", asks)
	}
}
