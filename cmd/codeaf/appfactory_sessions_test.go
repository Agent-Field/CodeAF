package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// take drains the sign-ins to keep or forget, as the app's core does.
func (p *person) take(t *testing.T) []appwire.FactorySessionChange {
	t.Helper()
	var out appwire.FactorySessionsTaken
	p.w.must("factory.sessions_take", struct{}{}, &out)
	return out.Changes
}

// A SIGN-IN SURVIVES A RESTART THROUGH THE APP'S KEEPER: the sign-in is taken
// once, a new engine is handed it back and is signed in as the same person,
// and signing out queues the keeper's item for removal.
func TestAppFactorySignInSurvivesARestartThroughTheKeeper(t *testing.T) {
	f := startAdminFactory(t)
	bob := f.signIn(t, 1, "alice")
	if !strings.Contains(bob.w.hello, appwire.FeatureFactorySessions) {
		t.Fatalf("hello does not announce %s", appwire.FeatureFactorySessions)
	}
	changes := bob.take(t)
	if len(changes) != 1 || changes[0].Factory != bob.conn.ID || changes[0].URL != f.URL || changes[0].Token != bob.held() || changes[0].Removed {
		t.Fatalf("taken = %+v", changes)
	}
	if again := bob.take(t); len(again) != 0 {
		t.Fatalf("a change was answered twice: %+v", again)
	}
	kept := changes[0]

	// The restart: a new engine over the same saved connections.
	second := testFactoryHub(t)
	second.dir = bob.w.backend.factoryHub().dir
	second.now = f.clock.now
	after := &person{w: newFactoryWire(t, second), handle: "alice", conn: bob.conn}
	if c := after.listed(t); c.State != appwire.FactorySignedOut {
		t.Fatalf("before the hand-back: %+v", c)
	}
	var restored appwire.FactorySessionRestored
	after.w.must("factory.session_restore", map[string]any{"factory": kept.Factory, "url": kept.URL, "token": kept.Token, "expires": kept.Expires}, &restored)
	if !restored.Kept {
		t.Fatal("the kept sign-in was not taken back")
	}
	if c := after.listed(t); c.State != appwire.FactoryOK {
		t.Fatalf("after a restart the sign-in is lost: %+v", c)
	}
	var me appwire.FactoryMe
	after.w.must("factory.me", map[string]any{"factory": bob.conn.ID}, &me)
	if me.Handle != "alice" {
		t.Fatalf("restored as %q", me.Handle)
	}
	if changes := after.take(t); len(changes) != 0 {
		t.Fatalf("a hand-back was queued to be kept again: %+v", changes)
	}

	var done appwire.FactoryAdminDone
	after.w.must("factory.sign_out", map[string]any{"factory": bob.conn.ID}, &done)
	gone := after.take(t)
	if len(gone) != 1 || gone[0].Factory != bob.conn.ID || !gone[0].Removed || gone[0].Token != "" {
		t.Fatalf("sign-out did not remove the kept sign-in: %+v", gone)
	}
}

// A RESTORED SIGN-IN THE FACTORY REFUSES IS FORGOTTEN ONCE: the connection
// reads signed out, the keeper is told to remove it, and the dead token is
// never sent again.
func TestAppFactoryRefusedRestoreIsSignedOutOnceAndRemoved(t *testing.T) {
	f := startAdminFactory(t)
	bob := f.signIn(t, 1, "alice")
	bob.take(t)
	second := testFactoryHub(t)
	second.dir = bob.w.backend.factoryHub().dir
	second.now = f.clock.now
	after := &person{w: newFactoryWire(t, second), handle: "alice", conn: bob.conn}
	var restored appwire.FactorySessionRestored
	after.w.must("factory.session_restore", map[string]any{"factory": bob.conn.ID, "url": f.URL, "token": "dead-session-token-0123456789"}, &restored)
	if !restored.Kept {
		t.Fatal("a hand-back for a saved connection was refused before it was tried")
	}
	f.since()
	if c := after.listed(t); c.State != appwire.FactorySignedOut {
		t.Fatalf("a refused sign-in reads %+v", c)
	}
	gone := after.take(t)
	if len(gone) != 1 || !gone[0].Removed || gone[0].Factory != bob.conn.ID {
		t.Fatalf("the refused sign-in was not removed: %+v", gone)
	}
	f.since()
	if c := after.listed(t); c.State != appwire.FactorySignedOut {
		t.Fatalf("second read: %+v", c)
	}
	for _, sent := range f.since() {
		if sent != "GET /v1/me" {
			t.Fatalf("after the refusal the engine sent %q", sent)
		}
	}
	if after.held() != "" || len(after.take(t)) != 0 {
		t.Fatal("the refused sign-in is retried or removed twice")
	}
}

// A HAND-BACK IS TAKEN ONLY FOR A SAVED CONNECTION AT THE SAME ADDRESS, and
// never for the machine's own factory.
func TestAppFactoryRestoreRefusesAnotherAddressOrTheLocalFactory(t *testing.T) {
	f := startAdminFactory(t)
	bob := f.signIn(t, 1, "alice")
	for _, c := range []map[string]any{
		{"factory": bob.conn.ID, "url": "https://elsewhere.example", "token": "t-0123456789"},
		{"factory": "factory_0000000000000000", "url": f.URL, "token": "t-0123456789"},
		{"factory": appLocalID, "url": f.URL, "token": "t-0123456789"},
	} {
		var restored appwire.FactorySessionRestored
		bob.w.must("factory.session_restore", c, &restored)
		if restored.Kept {
			t.Fatalf("restore %v was kept", c)
		}
	}
}

// AN UNTAKEN CHANGE EXPIRES: nothing waits past its life for a caller.
func TestAppFactorySessionChangesExpireUntaken(t *testing.T) {
	f := startAdminFactory(t)
	bob := f.signIn(t, 1, "alice")
	f.clock.advance(appwire.FactorySessionsTakeLife + time.Second)
	if changes := bob.take(t); len(changes) != 0 {
		t.Fatalf("an expired change was answered: %+v", changes)
	}
}

// A SIGN-IN TO KEEP IS ANNOUNCED, AND THE ANNOUNCEMENT CARRIES NOTHING: the
// core hears factory.sessions_changed and must pull the change itself.
func TestAppFactorySessionChangeIsAnnouncedWithoutTheToken(t *testing.T) {
	f := startRealFactory(t)
	rec := newAppRecorder()
	b := newFakeBackend(rec, nil)
	defer b.Close()
	dir := filepath.Join(t.TempDir(), "app")
	b.factoryHub().dir = func() string { return dir }
	if _, err := b.FactoryAttach(context.Background(), f.URL, testHostToken); err != nil {
		t.Fatal(err)
	}
	i := rec.waitFor(t, 5*time.Second, func(event string, _ any) bool { return event == appwire.EventFactorySessionsChanged })
	rec.mu.Lock()
	data := rec.data[i]
	rec.mu.Unlock()
	if data != (appwire.Empty{}) {
		t.Fatalf("the announcement carried %+v", data)
	}
	taken, _ := b.FactorySessionsTake(context.Background())
	if len(taken.Changes) != 1 || taken.Changes[0].Token != testHostToken {
		t.Fatalf("taken = %+v", taken)
	}
}
