package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// THE HOST DOOR HANDS THE WINDOW THE FAR MACHINE'S AUTOMATIONS, AND NOTHING WHEN
// THE FAR ENGINE HAS NONE. The engine below keeps a store of the test's own in a
// zone that is nobody's local one: the seam hostOptions builds reads that store
// over the wire, carries that zone, and claims a run's notification once; an
// engine whose welcome does not say it has the doors hands the zero seam, which
// the surface reads as automations absent — never this laptop's store.
func TestTheHostDoorReadsTheFarMachinesAutomations(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, err := automation.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version, Workspace: "/srv/app"}, remote.Options{
		Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: &quietAgent{}, Workspace: "/srv/app", SessionFile: "/srv/app/j.jsonl",
				Automations: &remote.EngineAutomations{Store: store, Zone: "Pacific/Chatham"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("dial the loopback: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	options, _ := hostOptions(onePipeFleet("devbox", loop.Client), loop.Client.Welcome(), false)
	seam := options.Automations
	if seam.List == nil || seam.Changes == nil || seam.Claim == nil || seam.Windows == nil {
		t.Fatal("the door hands a window over --host no automations seam, so it can draw none of that machine's automations")
	}
	if seam.Zone != "Pacific/Chatham" {
		t.Fatalf("a rhythm typed over --host is read in %q, want the far machine's own zone", seam.Zone)
	}
	made, err := store.Create(automation.Automation{
		Title: "leave", Workspace: "/srv/app",
		Schedule: automation.Schedule{At: time.Now().Add(time.Hour)},
		Action:   automation.Action{Say: "time to leave"},
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := seam.List()
	if err != nil || len(list) != 1 || list[0].ID != made.ID {
		t.Fatalf("the window's list of the far machine answered %+v, %v", list, err)
	}
	run, err := store.QueueNow(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !seam.Claim(run.ID) {
		t.Fatal("the first window to claim a run's notification was refused it")
	}
	if seam.Claim(run.ID) {
		t.Fatal("a run's notification was claimed twice")
	}

	bare, _ := hostOptions(onePipeFleet("devbox", hostedClient(t)), remote.Welcome{Version: remote.Version, Workspace: "/srv/app"}, false)
	if !reflect.ValueOf(bare.Automations).IsZero() {
		t.Fatalf("an engine without the automations doors was handed a seam: %+v", bare.Automations)
	}
}

// THE ENGINE HANDS THE WIRE THIS MACHINE'S AUTOMATIONS, AND A WINDOW HELD
// THROUGH IT IS A WINDOW THIS MACHINE'S CLOCK COUNTS. The hook carries the store
// and the zone this process opened once, and its HoldWindow is the very door a
// local window holds presence through — so a window attached from another
// machine is counted in the folder the clock reads, and stops being counted the
// moment it is let go.
func TestTheEngineHandsTheWireThisMachinesAutomations(t *testing.T) {
	// THE STORE IS OPENED ONCE PER PROCESS, under the home this test binary
	// already moved to a throwaway folder (testenv_test.go) — the same place
	// every test that builds a conversation's config opens it. Only the window
	// held below goes to a folder this test owns, so the count starts at zero.
	autos, hook := v3Automations(), engineAutomations()
	if autos == nil || hook == nil {
		t.Fatalf("this process opened automations %v and handed the wire %v; both must be there under a writable home", autos, hook)
	}
	if hook.Store != autos.Store || hook.Zone != autos.Zone || hook.HoldWindow == nil {
		t.Fatalf("the wire was handed %+v, not this process's own store %p in %q", hook, autos.Store, autos.Zone)
	}
	t.Setenv(home.EnvVar, t.TempDir())
	presence := automation.NewPresence(automationsRoot())
	release := hook.HoldWindow("attached from laptop")
	if open, err := presence.Count(); err != nil || open != 1 {
		release()
		t.Fatalf("a window held through the engine counts %d, %v", open, err)
	}
	release()
	if open, err := presence.Count(); err != nil || open != 0 {
		t.Fatalf("a window the engine let go is still counted: %d, %v", open, err)
	}
}
