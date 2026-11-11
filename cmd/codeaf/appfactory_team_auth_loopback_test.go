package main

import (
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// THE REAL SERVER ANSWERS A NO-BEARER LOOPBACK REQUEST AS ITS HOST, with no
// forwarded header to hide that. A team connection with no held credential must
// therefore be refused here, before anything private is sent.

func loopbackList(t *testing.T, w *factoryWire) appwire.FactoryConnection {
	t.Helper()
	var list appwire.FactoryList
	w.must("factory.list", struct{}{}, &list)
	if len(list.Factories) != 1 {
		t.Fatalf("list = %+v", list)
	}
	return list.Factories[0]
}

func privateRequests(f *adminFactory) []string {
	var out []string
	for _, r := range f.since() {
		if r != "GET /v1/about" {
			out = append(out, r)
		}
	}
	return out
}

func TestAppFactoryTeamAddressOnlyAttachNeedsSignInOnLoopback(t *testing.T) {
	f := startAdminFactoryAt(t, false)
	w := newFactoryWire(t, testFactoryHub(t))
	var conn appwire.FactoryConnection
	w.must("factory.attach", map[string]any{"url": f.URL}, &conn)
	if conn.State != appwire.FactorySignedOut || conn.Operator {
		t.Fatalf("a bare address was given the host's authority: %+v", conn)
	}
	if c := loopbackList(t, w); c.State != appwire.FactorySignedOut || c.Operator {
		t.Fatalf("listed: %+v", c)
	}
	f.since()
	for _, verb := range []string{"factory.queue", "factory.me", "factory.people", "factory.runners"} {
		w.mustFail(verb, map[string]any{"factory": conn.ID}, appwire.CodeSignedOut)
	}
	// Only the mode probe may have been sent; no private read reached it.
	for _, r := range privateRequests(f) {
		if r != "GET /v1/me" {
			t.Fatalf("a private request was sent with no credential: %s", r)
		}
	}
}

func TestAppFactoryTeamSignOutAndRestartStaySignedOutOnLoopback(t *testing.T) {
	f := startAdminFactoryAt(t, false)
	bob := f.signIn(t, 1, "alice")
	if c := bob.listed(t); c.State != appwire.FactoryOK || c.Mode != "team" {
		t.Fatalf("signed in: %+v", c)
	}
	var done appwire.FactoryAdminDone
	bob.w.must("factory.sign_out", map[string]any{"factory": bob.conn.ID}, &done)
	if c := bob.listed(t); c.State != appwire.FactorySignedOut || c.Operator {
		t.Fatalf("after sign-out: %+v", c)
	}
	f.since()
	bob.w.mustFail("factory.queue", map[string]any{"factory": bob.conn.ID}, appwire.CodeSignedOut)
	if got := f.since(); len(got) != 0 {
		t.Fatalf("a signed-out read sent %v", got)
	}

	// A restart keeps the saved row and mode but no credential.
	hub := testFactoryHub(t)
	hub.dir = bob.w.backend.factoryHub().dir
	w := newFactoryWire(t, hub)
	if c := loopbackList(t, w); c.ID != bob.conn.ID || c.State != appwire.FactorySignedOut || c.Operator {
		t.Fatalf("after restart: %+v", c)
	}
	f.since()
	w.mustFail("factory.queue", map[string]any{"factory": bob.conn.ID}, appwire.CodeSignedOut)
	if got := f.since(); len(got) != 0 {
		t.Fatalf("a restarted read sent %v", got)
	}
}

func TestAppFactoryTeamReadIgnoresReplyAfterCredentialChanges(t *testing.T) {
	f := startAdminFactoryAt(t, false)
	bob := f.signIn(t, 1, "alice")
	var once sync.Once
	reached, release := make(chan struct{}), make(chan struct{})
	f.gate = func(path string) {
		if path == "/v1/queue" {
			once.Do(func() { close(reached); <-release })
		}
	}
	errc := make(chan error, 1)
	go func() {
		_, err := bob.w.backend.FactoryQueue(t.Context(), bob.conn.ID)
		errc <- err
	}()
	<-reached
	// The credential is dropped while the factory is still answering.
	h := bob.w.backend.factoryHub()
	h.mu.Lock()
	delete(h.sessions, bob.conn.ID)
	h.mu.Unlock()
	close(release)
	if err := <-errc; err == nil || wireCodeOf(t, err) != appwire.CodeSignedOut {
		t.Fatalf("a reply for a removed credential was used: %v", err)
	}

	// Swapped for another caller's: the old reply is not promoted either.
	other := f.signIn(t, 2, "bob")
	reached2, release2 := make(chan struct{}), make(chan struct{})
	var once2 sync.Once
	f.gate = func(path string) {
		if path == "/v1/queue" {
			once2.Do(func() { close(reached2); <-release2 })
		}
	}
	go func() {
		_, err := other.w.backend.FactoryQueue(t.Context(), other.conn.ID)
		errc <- err
	}()
	<-reached2
	h2 := other.w.backend.factoryHub()
	h2.hold(other.conn.ID, "tok_swapped_credential", 0)
	close(release2)
	if err := <-errc; err == nil || wireCodeOf(t, err) != appwire.CodeConflict {
		t.Fatalf("a reply for a replaced credential was used: %v", err)
	}
}
