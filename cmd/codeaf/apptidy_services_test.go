package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/service"
	"github.com/Agent-Field/codeaf/internal/session"
)

// tidyVerbs are the two public doors a person's Close and Archive come through.
var tidyVerbs = map[string]func(b *appBackend, id string) error{
	"close":   func(b *appBackend, id string) error { return b.MoveToClosed([]string{id}) },
	"archive": func(b *appBackend, id string) error { return b.Archive([]string{id}) },
}

// tidyUntil runs verb in the background and moves the test's clock through the
// stop's grace until it answers; no real sleeping beyond a yield.
func tidyUntil(t *testing.T, w *svcWorld, verb func() error, during func()) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- verb() }()
	if during != nil {
		during()
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		select {
		case err := <-done:
			return err
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the tidy verb never answered")
		}
		w.clock.Advance(time.Second)
		time.Sleep(time.Millisecond)
	}
}

// realTranscript points the agent at a conversation folder that exists, so
// Archive's mark (session.SetArchived) has somewhere to be written.
func (w *svcWorld) realTranscript(t *testing.T) {
	t.Helper()
	a, err := w.backend.find(w.agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := session.SaveMeta(dir, session.Meta{ID: w.agent.ID}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "transcript.jsonl")
	w.backend.locked(func() { a.transcript = path })
}

func (w *svcWorld) tidyState(t *testing.T) (state string, closed, archived bool) {
	t.Helper()
	a, err := w.backend.find(w.agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	w.backend.locked(func() { state = a.view.State })
	_ = w.backend.state.read(func(f *appStateFile) {
		_, closed = f.Closed[w.agent.ID]
		_, archived = f.Archived[w.agent.ID]
	})
	return
}

// TestClosingOrArchivingStopsTheAgentsOwnProcessesFirst: both verbs stop what
// the agent kept, only that, then hide it, and leave no fence behind.
func TestClosingOrArchivingStopsTheAgentsOwnProcessesFirst(t *testing.T) {
	for name, verb := range tidyVerbs {
		t.Run(name, func(t *testing.T) {
			w := newSvcWorld(t)
			w.realTranscript(t)
			row := w.keep(t, w.agent.ID, service.PolicyAlways)
			other, err := w.sup.Keep("another-agent", service.Spec{Name: "api", Command: "node api.js", Dir: w.work, Health: service.Health{Port: 4011}}, service.PolicyNever, service.Provenance{})
			if err != nil {
				t.Fatal(err)
			}
			if err := tidyUntil(t, w, func() error { return verb(w.backend, w.agent.ID) }, nil); err != nil {
				t.Fatal(err)
			}
			if got, _ := w.sup.Get(w.agent.ID, row.ID); got.Running || got.StoppedBy != appServiceClosedBy {
				t.Fatalf("the process after %s: %+v", name, got)
			}
			if got, _ := w.sup.Get("another-agent", other.ID); !got.Running {
				t.Fatal("another agent's process was stopped")
			}
			state, closed, archived := w.tidyState(t)
			want := appwire.StateClosed
			if name == "archive" {
				want = appwire.StateArchived
			}
			if state != want || (name == "close") != closed || (name == "archive") != archived {
				t.Fatalf("state %s closed=%v archived=%v", state, closed, archived)
			}
			// No fence is left: the agent may start things once reopened.
			if _, err := w.backend.StartService(w.agent.ID + ":" + row.ID); err != nil {
				t.Fatalf("a start after %s: %v", name, err)
			}
		})
	}
}

// TestARefusedStopLeavesTheAgentOpenAndUnfenced: a process that will not stop
// keeps the agent open and unarchived, says which, and a retry then succeeds.
func TestARefusedStopLeavesTheAgentOpenAndUnfenced(t *testing.T) {
	for name, verb := range tidyVerbs {
		t.Run(name, func(t *testing.T) {
			w := newSvcWorld(t)
			w.realTranscript(t)
			row := w.keep(t, w.agent.ID, service.PolicyAlways)
			id := w.agent.ID + ":" + row.ID
			w.rt.Refusing(true)
			err := tidyUntil(t, w, func() error { return verb(w.backend, w.agent.ID) }, nil)
			if err == nil || !strings.Contains(err.Error(), row.ID) {
				t.Fatalf("a %s that could not stop its process said %v", name, err)
			}
			state, closed, archived := w.tidyState(t)
			if state == appwire.StateClosed || state == appwire.StateArchived || closed || archived {
				t.Fatalf("hidden over a running process: %s closed=%v archived=%v", state, closed, archived)
			}
			if got, _ := w.sup.Get(w.agent.ID, row.ID); !got.Running {
				t.Fatalf("the process is hidden: %+v", got)
			}
			// The fence is gone: a keep for the owner is not refused as closing.
			w.rt.Refusing(false)
			if _, err := w.sup.Keep(w.agent.ID, service.Spec{Name: "api", Command: "node api.js", Dir: w.work, Health: service.Health{Port: 4013}}, service.PolicyNever, service.Provenance{}); errors.Is(err, service.ErrClosing) {
				t.Fatalf("a fence leaked past the refusal: %v", err)
			}
			if err := tidyUntil(t, w, func() error { return verb(w.backend, w.agent.ID) }, nil); err != nil {
				t.Fatalf("the retry: %v", err)
			}
			if got, _ := w.sup.Get(w.agent.ID, row.ID); got.Running {
				t.Fatalf("after the retry: %+v", got)
			}
			_ = id
		})
	}
}

// TestNothingStartsWhileAnAgentIsClosedOrArchived: a start or keep asked for
// during the stop's grace is refused, and the fence is gone afterwards.
func TestNothingStartsWhileAnAgentIsClosedOrArchived(t *testing.T) {
	for name, verb := range tidyVerbs {
		t.Run(name, func(t *testing.T) {
			w := newSvcWorld(t)
			w.realTranscript(t)
			row := w.keep(t, w.agent.ID, service.PolicyNever)
			id := w.agent.ID + ":" + row.ID
			w.rt.Refusing(true)
			err := tidyUntil(t, w, func() error { return verb(w.backend, w.agent.ID) }, func() {
				w.changed(t, id, func(p appwire.Process) bool { return p.Service.Stopping })
				if _, err := w.backend.StartService(id); err == nil || !strings.Contains(err.Error(), "closing") {
					t.Errorf("a start during the %s said %v", name, err)
				}
				if _, err := w.sup.Keep(w.agent.ID, service.Spec{Name: "api", Command: "node api.js", Dir: w.work, Health: service.Health{Port: 4014}}, service.PolicyNever, service.Provenance{}); !errors.Is(err, service.ErrClosing) {
					t.Errorf("a keep during the %s said %v", name, err)
				}
				w.rt.Refusing(false)
			})
			if err != nil {
				// The refusal outlasted the grace; the retry is the person's.
				err = tidyUntil(t, w, func() error { return verb(w.backend, w.agent.ID) }, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.backend.StartService(id); err != nil {
				t.Fatalf("after the %s: %v", name, err)
			}
		})
	}
}
