package main

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/automation"
)

// These tests are written from the validation contract of "Go, and stop
// asking", "trust releases the run waiting" and "trust at setup", at the
// app's own door: what a person's desktop sees and what the engine then does.

// ratifiedOrderAsking is an order the person agreed to that has a run waiting for
// their go, with the transcript an Inbox row needs to name its agent.
func ratifiedOrderAsking(t *testing.T, store *automation.Store) (automation.Automation, automation.Run, string) {
	t.Helper()
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
	return a, run, appAgentID(transcript)
}

// Contract C3: the run's ask offers Go, "Go, and stop asking", Skip this one
// and Not right, in that order.
func TestAppOrderRunAskOffersGoAndStopAsking(t *testing.T) {
	ask := appOrderRunAsk(appwire.Order{ID: "o", Title: "weekly update", Trust: appwire.TrustAsksFirst}, automation.Run{ID: 7})
	var got []string
	for _, option := range ask.Options {
		got = append(got, option.ID+"="+option.Label)
	}
	want := "go=Go|always=Go, and stop asking|skip=Skip this one|not_right=Not right"
	if strings.Join(got, "|") != want {
		t.Fatalf("the ask offers %q, want %q", strings.Join(got, "|"), want)
	}
	if err := appValidateOrderAnswer(ask.ID, appwire.VerdictAlways); err != nil {
		t.Fatalf("always is refused: %v", err)
	}
	err := appValidateOrderAnswer(ask.ID, "whenever")
	if err == nil || !strings.Contains(err.Error(), "go, always, skip or not_right") {
		t.Fatalf("an answer that is no option: %v", err)
	}
}

// Contract C3: answering the ask "Go, and stop asking" runs that run, trusts
// the order, and leaves the Inbox with a receipt that says so, not "Always
// allowed".
func TestAppOrderAnsweredGoAndStopAskingTrustsTheOrder(t *testing.T) {
	backend, rec, store := ordersBackend(t)
	a, run, agent := ratifiedOrderAsking(t, store)
	id := appOrderAskID(run.ID)
	open := map[int64]appwire.Ask{}
	backend.sayOrderAsks(store, open)
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		opened, ok := data.(appwire.AskOpened)
		return ok && opened.Ask.ID == id
	})

	if err := backend.AnswerAsk(agent, id, appwire.VerdictAlways, ""); err != nil {
		t.Fatal(err)
	}
	orders, _ := backend.Orders("")
	if len(orders) != 1 || orders[0].Trust != appwire.TrustTrusted || orders[0].Asking != nil || orders[0].ApprovedRuns != 1 {
		t.Fatalf("after go and stop asking = %+v", orders)
	}
	if queued, _ := store.Queued(); len(queued) != 1 || queued[0].ID != run.ID {
		t.Fatalf("the answered run is not queued to run: %+v", queued)
	}
	if got, _ := store.Get(a.ID); got.Granted != run.ID {
		t.Fatalf("the run is not the granted one: %d vs %d", got.Granted, run.ID)
	}

	backend.sayOrderAsks(store, open)
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		closed, ok := data.(appwire.AskClosed)
		return ok && closed.ID == id && closed.By == appwire.ClosedByApp && closed.Option == appwire.VerdictAlways
	})
	i := rec.waitFor(t, time.Second, func(event string, data any) bool {
		resolved, ok := data.(appwire.InboxResolvedEvent)
		return ok && resolved.ID == appInboxID(agent, id)
	})
	rec.mu.Lock()
	resolved := rec.data[i].(appwire.InboxResolvedEvent)
	rec.mu.Unlock()
	if !strings.HasPrefix(resolved.Receipt, "Went ahead, and stopped asking · you · ") {
		t.Fatalf("the receipt reads %q", resolved.Receipt)
	}
}

// Contract C3: it is the same answer through orders.run's own door.
func TestAppOrderVerdictAlwaysTrustsTheOrder(t *testing.T) {
	backend, _, store := ordersBackend(t)
	a, run, _ := ratifiedOrderAsking(t, store)
	got, err := backend.OrderVerdict(a.ID, 0, appwire.VerdictAlways)
	if err != nil || got.Trust != appwire.TrustTrusted || got.Asking != nil {
		t.Fatalf("always = %+v, %v", got, err)
	}
	if r, _ := store.Run(run.ID); r.Phase != automation.PhaseQueued {
		t.Fatalf("the answered run is %q, want queued", r.Phase)
	}
}

// Contract C3: an answer given at another surface that went ahead on an order
// now trusted reads as "Go, and stop asking" in the Inbox; one that only
// earned a go reads as Go, as before.
func TestAppOrderAnsweredAlwaysElsewhereReadsAsAnswered(t *testing.T) {
	for reply, want := range map[automation.Reply]string{automation.ReplyAlways: appwire.VerdictAlways, automation.ReplyGo: appwire.VerdictGo} {
		t.Run(string(reply), func(t *testing.T) {
			backend, rec, store := ordersBackend(t)
			_, run, agent := ratifiedOrderAsking(t, store)
			id := appOrderAskID(run.ID)
			open := map[int64]appwire.Ask{}
			backend.sayOrderAsks(store, open)
			if _, err := store.Answer(run.ID, reply); err != nil {
				t.Fatal(err)
			}
			backend.sayOrderAsks(store, open)
			rec.waitFor(t, time.Second, func(event string, data any) bool {
				closed, ok := data.(appwire.AskClosed)
				return ok && closed.ID == id && closed.By == appwire.ClosedByElsewhere && closed.Option == want
			})
			i := rec.waitFor(t, time.Second, func(event string, data any) bool {
				resolved, ok := data.(appwire.InboxResolvedEvent)
				return ok && resolved.ID == appInboxID(agent, id)
			})
			rec.mu.Lock()
			resolved := rec.data[i].(appwire.InboxResolvedEvent)
			rec.mu.Unlock()
			prefix := "Answered · you · "
			if want == appwire.VerdictAlways {
				prefix = "Went ahead, and stopped asking · you · "
			}
			if !strings.HasPrefix(resolved.Receipt, prefix) {
				t.Fatalf("a run answered %s elsewhere resolved as %+v", want, resolved)
			}
		})
	}
}

// Contract C4: trusting an order from its settings, with a run waiting on it,
// starts that run and closes its ask: no trusted order is left asking.
func TestAppOrderTrustedFromItsSettingsReleasesTheWaitingRun(t *testing.T) {
	backend, rec, store := ordersBackend(t)
	a, run, agent := ratifiedOrderAsking(t, store)
	open := map[int64]appwire.Ask{}
	backend.sayOrderAsks(store, open)

	trusted := appwire.TrustTrusted
	o, err := backend.ChangeOrder(a.ID, appwire.OrderEdits{Trust: &trusted})
	if err != nil || o.Trust != appwire.TrustTrusted || o.Asking != nil || o.ApprovedRuns != 0 {
		t.Fatalf("after trusting = %+v, %v", o, err)
	}
	if r, _ := store.Run(run.ID); r.Phase != automation.PhaseQueued {
		t.Fatalf("the waiting run is %q, want queued", r.Phase)
	}
	backend.sayOrderAsks(store, open)
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		closed, ok := data.(appwire.AskClosed)
		return ok && closed.ID == appOrderAskID(run.ID) && closed.By == appwire.ClosedByElsewhere
	})
	if asks := backend.orderAsksOf(agent); len(asks) != 0 {
		t.Fatalf("a trusted order still asks: %+v", asks)
	}
}

// fakeOrderRunner counts the pieces of work the real clock starts.
type fakeOrderRunner struct{ worked atomic.Int32 }

func (*fakeOrderRunner) Look(context.Context, automation.Automation) (automation.Sight, error) {
	return automation.Sight{}, nil
}

func (*fakeOrderRunner) Judge(context.Context, automation.Automation, automation.Sight) (automation.Judgment, error) {
	return automation.Judgment{Sure: true}, nil
}

func (r *fakeOrderRunner) Work(context.Context, automation.Automation, automation.Run, string) (automation.Report, error) {
	r.worked.Add(1)
	return automation.Report{Outcome: automation.OutcomeDone, Line: "drafted"}, nil
}

// afterItsFirstSlot runs the real clock, held open by a window, at a moment
// just after the order's first slot, until it has either asked or finished a
// run, and says what it came to.
func afterItsFirstSlot(t *testing.T, store *automation.Store, id string) (asking []automation.Run, over []automation.Run, worked int32) {
	t.Helper()
	a, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeOrderRunner{}
	presence := automation.NewPresence(store.Root())
	window, err := presence.Hold("test window")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	clock := &automation.Clock{Store: store, Presence: presence, Runner: runner,
		Now: func() time.Time { return a.Next.Add(time.Minute) }, Log: func(line string) { t.Log("clock: " + line) }}
	go func() { done <- clock.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		window()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("the clock did not stop")
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		asking, _ = store.Asking()
		runs, _ := store.Runs(id, 10)
		over = over[:0]
		for _, run := range runs {
			if run.Phase == automation.PhaseOver {
				over = append(over, run)
			}
		}
		if len(asking) > 0 || len(over) > 0 {
			return asking, over, runner.worked.Load()
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the clock neither asked nor ran the first slot")
	return nil, nil, 0
}

// Contract C5 and C7: ratifying with trust "trusted" saves the order active and
// trusted, and its first run starts with no ask; "asks_first" and no trust at
// all keep today's default, and the first run waits for the person.
func TestAppOrderTrustAtSetupOnlyWhenAsked(t *testing.T) {
	trusted, asksFirst := appwire.TrustTrusted, appwire.TrustAsksFirst
	cases := []struct {
		name  string
		edits *appwire.OrderEdits
		trust string
		asks  bool
	}{
		{"trusted", &appwire.OrderEdits{Trust: &trusted}, appwire.TrustTrusted, false},
		{"asks_first", &appwire.OrderEdits{Trust: &asksFirst}, appwire.TrustAsksFirst, true},
		{"edits without trust", &appwire.OrderEdits{}, appwire.TrustAsksFirst, true},
		{"no edits", nil, appwire.TrustAsksFirst, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend, _, store := ordersBackend(t)
			a := proposedOrder(t, store)
			o, err := backend.RatifyOrder(a.ID, tc.edits)
			if err != nil || o.State != appwire.OrderActive || o.Trust != tc.trust || o.FiresAt == nil {
				t.Fatalf("ratified = %+v, %v", o, err)
			}
			asking, over, worked := afterItsFirstSlot(t, store, a.ID)
			if tc.asks {
				if len(asking) != 1 || worked != 0 {
					t.Fatalf("its first run: %d waiting for the person, %d worked", len(asking), worked)
				}
				return
			}
			if len(asking) != 0 || worked != 1 || len(over) != 1 || over[0].Outcome != automation.OutcomeDone {
				t.Fatalf("its first run: %d waiting, %d worked, over %+v", len(asking), worked, over)
			}
		})
	}
}

// Contract C6: the engine says it takes the verdict, so a desktop draws the
// button only against one that does; and the engine that does not say so
// refuses the word rather than guess.
func TestAppBackendTakesGoAndStopAsking(t *testing.T) {
	var engine appwire.Engine = newFakeBackend(newAppRecorder(), nil)
	if _, ok := engine.(appwire.OrderAlwaysTaker); !ok {
		t.Fatal("the app's engine does not say it takes always")
	}
}
