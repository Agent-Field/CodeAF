package remote

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/automation"
)

// openAutomations is a store of the test's own, under a folder the test owns —
// never the developer's home, whose automations are somebody's real work.
func openAutomations(t *testing.T) *automation.Store {
	t.Helper()
	store, err := automation.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// automationsLoop is an in-process engine whose machine keeps the store given,
// in a zone that is nobody's local one, so a zone that arrives was carried.
//
// THE LOOP IS CLOSED BEFORE THE STORE. Cleanups run last-registered first, and
// the loop's is registered after the store's, so no engine is still answering
// out of a database that has been shut.
func automationsLoop(t *testing.T, store *automation.Store, hello Hello, hold func(string) func()) *Loop {
	t.Helper()
	autos := &EngineAutomations{Store: store, Zone: "Pacific/Chatham", HoldWindow: hold}
	loop, err := Loopback(hello, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{model: "m"}, Workspace: "/srv/app", SessionFile: "/srv/app/j.jsonl", Automations: autos}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	return loop
}

// heldWindows is an engine door's HoldWindow over the store's REAL presence
// folder — the one the far clock counts — remembering what each window called
// itself.
type heldWindows struct {
	presence *automation.Presence
	mu       sync.Mutex
	labels   []string
}

func (h *heldWindows) hold(label string) func() {
	h.mu.Lock()
	h.labels = append(h.labels, label)
	h.mu.Unlock()
	release, err := h.presence.Hold(label)
	if err != nil {
		return func() {}
	}
	return release
}

func (h *heldWindows) said() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.labels...)
}

// THE AUTOMATIONS DOORS CROSS, AND THEY ANSWER FROM THE ENGINE MACHINE'S STORE.
// The welcome says the engine has them and in which zone a typed rhythm is
// read; an automation made over the wire lands in the engine's store; a change,
// a pause and a refused status all travel; a run asked for now, stopped, and
// recorded by the clock's half reaches the window through Changes exactly once;
// the desktop notification's claim is won once and only once; and a delete the
// store refuses comes back as the store's own sentinel.
func TestTheAutomationsDoorsCrossFromTheEnginesStore(t *testing.T) {
	store := openAutomations(t)
	client := automationsLoop(t, store, Hello{Version: Version}, nil).Client

	welcome := client.Welcome()
	if !welcome.Automations || welcome.AutomationsZone != "Pacific/Chatham" {
		t.Fatalf("the welcome says automations=%v in %q, want the engine's store in its own zone", welcome.Automations, welcome.AutomationsZone)
	}
	cursor, err := client.AutomationsCursor()
	if err != nil {
		t.Fatalf("the cursor: %v", err)
	}

	at := time.Now().Add(time.Hour).Truncate(time.Millisecond)
	saved, err := client.AutomationsCreate(automation.Automation{
		Title: "leave", Workspace: "/srv/app",
		Schedule: automation.Schedule{At: at},
		Action:   automation.Action{Say: "time to leave"},
	})
	if err != nil || saved.ID == "" || saved.Status != automation.StatusActive || !saved.Next.Equal(at) {
		t.Fatalf("the create answered %+v, %v", saved, err)
	}
	if got, err := store.Get(saved.ID); err != nil || got.Title != "leave" {
		t.Fatalf("the engine's store holds %+v, %v", got, err)
	}
	list, err := client.AutomationsList()
	if err != nil || len(list) != 1 || list[0].ID != saved.ID {
		t.Fatalf("the list answered %+v, %v", list, err)
	}

	saved.Title = "leave for the train"
	updated, err := client.AutomationsUpdate(saved)
	if err != nil || updated.Title != "leave for the train" || updated.Revision != 2 {
		t.Fatalf("the update answered %+v, %v", updated, err)
	}
	paused, err := client.AutomationsSetStatus(saved.ID, automation.StatusPaused)
	if err != nil || paused.Status != automation.StatusPaused {
		t.Fatalf("the pause answered %+v, %v", paused, err)
	}
	// A STATUS ONLY THE CLOCK MAY SET IS REFUSED IN THE STORE'S OWN WORDS, and
	// the refusal crosses rather than turning into an answer.
	if _, err := client.AutomationsSetStatus(saved.ID, automation.StatusFinished); err == nil || !strings.Contains(err.Error(), "cannot set the status") {
		t.Fatalf("a person finishing an automation was answered %v", err)
	}

	if err := client.AutomationsRunNow(saved.ID); err != nil {
		t.Fatalf("run now: %v", err)
	}
	active, err := client.AutomationsActive()
	if err != nil || len(active) != 1 || active[0].Phase != automation.PhaseQueued || active[0].Why != automation.WhyNow {
		t.Fatalf("the active runs answered %+v, %v", active, err)
	}
	run := active[0]
	if err := client.AutomationsStopRun(run.ID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if stops, err := store.StopRequested(); err != nil || !stops[run.ID] {
		t.Fatalf("the engine's clock was not asked to stop run %d: %v, %v", run.ID, stops, err)
	}

	// THE CLOCK'S HALF, played by the store the clock writes through: the run
	// is taken in hand and recorded as it ended.
	if err := store.Start(run.ID); err != nil {
		t.Fatal(err)
	}
	run.Outcome, run.Line = automation.OutcomeStopped, "stopped by you"
	if err := store.Finish(run); err != nil {
		t.Fatal(err)
	}
	runs, next, err := client.AutomationsChanges(cursor)
	if err != nil || next <= cursor || len(runs) != 1 || runs[0].ID != run.ID || runs[0].Phase != automation.PhaseOver || runs[0].Outcome != automation.OutcomeStopped {
		t.Fatalf("the changes after %d answered %+v up to %d, %v", cursor, runs, next, err)
	}
	if quiet, again, err := client.AutomationsChanges(next); err != nil || len(quiet) != 0 || again != next {
		t.Fatalf("a quiet store after %d answered %+v up to %d, %v", next, quiet, again, err)
	}
	history, err := client.AutomationsRuns(saved.ID, 10)
	if err != nil || len(history) != 1 || history[0].Line != "stopped by you" {
		t.Fatalf("the history answered %+v, %v", history, err)
	}

	// ONE WINDOW RAISES THE BANNER, whichever machine it is on.
	if first, err := client.AutomationsClaim(run.ID); err != nil || !first {
		t.Fatalf("the first claim answered %v, %v", first, err)
	}
	if again, err := client.AutomationsClaim(run.ID); err != nil || again {
		t.Fatalf("a second claim of the same run answered %v, %v", again, err)
	}

	// A hello that did not say it is a window holds nothing, so nothing is open.
	if open, err := client.AutomationsWindows(); err != nil || open != 0 {
		t.Fatalf("the windows answered %d, %v", open, err)
	}

	if err := client.AutomationsDelete(saved.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if list, err := client.AutomationsList(); err != nil || len(list) != 0 {
		t.Fatalf("after the delete the list answered %+v, %v", list, err)
	}
	if err := client.AutomationsDelete(saved.ID); !errors.Is(err, automation.ErrNotFound) {
		t.Fatalf("deleting what is gone answered %v, want the store's own ErrNotFound", err)
	}
}

// A WINDOW ATTACHED FROM ANOTHER MACHINE IS A WINDOW OPEN ON THIS ONE, FOR
// EXACTLY AS LONG AS IT IS ATTACHED. Its hello says so, and the engine holds the
// machine's presence for it — counted where the clock counts, and over the wire
// with this window included — and lets it go when the connection closes. A
// hello that did not say so holds nothing, so a probe or a script never keeps a
// clock running on its own.
func TestAWindowHelloHoldsTheMachinesPresenceWhileItIsAttached(t *testing.T) {
	store := openAutomations(t)
	windows := &heldWindows{presence: automation.NewPresence(store.Root())}

	plain := automationsLoop(t, store, Hello{Version: Version, Surface: "laptop"}, windows.hold)
	if open, err := windows.presence.Count(); err != nil || open != 0 {
		t.Fatalf("a hello that is not a window counts %d, %v", open, err)
	}
	if err := plain.Close(); err != nil {
		t.Fatal(err)
	}
	if said := windows.said(); len(said) != 0 {
		t.Fatalf("a hello that is not a window was held as %q", said)
	}

	window := automationsLoop(t, store, Hello{Version: Version, Surface: "laptop", Window: true}, windows.hold)
	if open, err := windows.presence.Count(); err != nil || open != 1 {
		t.Fatalf("a window attached from another machine counts %d, %v", open, err)
	}
	if open, err := window.Client.AutomationsWindows(); err != nil || open != 1 {
		t.Fatalf("over the wire the window counts %d, %v — it must count itself", open, err)
	}
	if said := windows.said(); len(said) != 1 || said[0] != "attached from laptop" {
		t.Fatalf("the window was held as %q", said)
	}
	// Close returns once the engine has finished leaving (loopback.go), so the
	// count below is the engine's last word and not a race against it.
	if err := window.Close(); err != nil {
		t.Fatal(err)
	}
	if open, err := windows.presence.Count(); err != nil || open != 0 {
		t.Fatalf("a window that closed is still counted: %d, %v", open, err)
	}
}

// AN ENGINE THAT KEEPS NO AUTOMATIONS SAYS NOTHING AND REFUSES. The welcome
// carries no flag and no zone, so a window builds no seam; a door asked anyway
// is a refusal and never an empty list; and a window hello holds nothing,
// because there is no clock here for it to keep running.
func TestAnEngineWithNoAutomationsSaysNothingAndRefuses(t *testing.T) {
	for _, autos := range []*EngineAutomations{nil, {Zone: "Pacific/Chatham", HoldWindow: func(string) func() {
		t.Error("an engine with no store held a window")
		return func() {}
	}}} {
		loop, err := Loopback(Hello{Version: Version, Window: true}, Options{Boot: func(Hello) (*Engine, error) {
			return &Engine{Agent: &fakeAgent{model: "m"}, Automations: autos}, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		welcome := loop.Client.Welcome()
		if welcome.Automations || welcome.AutomationsZone != "" {
			t.Errorf("an engine with no store said automations=%v in %q", welcome.Automations, welcome.AutomationsZone)
		}
		if list, err := loop.Client.AutomationsList(); err == nil || !strings.Contains(err.Error(), "keeps no automations") {
			t.Errorf("an engine with no store answered the list %+v, %v", list, err)
		}
		if first, err := loop.Client.AutomationsClaim(7); err == nil || first {
			t.Errorf("an engine with no store answered a claim %v, %v", first, err)
		}
		if err := loop.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// THE READINGS ARE GETTERS AND THE CHANGES ARE ACTS, so none of them waits
// behind a turn on the ordered lane: the watcher reads on a beat while a reply
// streams, and a pause pressed in the middle of one owes that reply nothing.
func TestTheAutomationsDoorsNeverQueueBehindATurn(t *testing.T) {
	for _, method := range []string{
		MethodAutomationsList, MethodAutomationsRuns, MethodAutomationsChanges,
		MethodAutomationsCursor, MethodAutomationsActive, MethodAutomationsWindows,
	} {
		if classify(method) != classGetter {
			t.Errorf("%s is a reading and is not a getter", method)
		}
		if !isAutomationsMethod(method) {
			t.Errorf("%s is not answered by the automations doors", method)
		}
	}
	for _, method := range []string{
		MethodAutomationsCreate, MethodAutomationsUpdate, MethodAutomationsSetStatus,
		MethodAutomationsDelete, MethodAutomationsRunNow, MethodAutomationsStopRun,
		MethodAutomationsClaim,
	} {
		if classify(method) != classAct {
			t.Errorf("%s is a person's small change and is not an act", method)
		}
		if !isAutomationsMethod(method) {
			t.Errorf("%s is not answered by the automations doors", method)
		}
	}
}
