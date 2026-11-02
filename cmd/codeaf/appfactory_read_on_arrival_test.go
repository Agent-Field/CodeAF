package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// READ NEW ISSUES AS THEY ARRIVE, AGAINST THE REAL SERVER AND STORE: the app
// reads the factory's setting (off, on a factory that never saved it); a
// teammate who is not the operator cannot change it; the operator turns it
// on, and the floor's own settings say on.
func TestAppFactoryReadOnArrivalAgainstTheRealServer(t *testing.T) {
	f := startAdminFactory(t)
	alice := f.signIn(t, 1, "alice")
	bob := f.signIn(t, 2, "bob")
	alice.w.mu.Lock()
	hello := alice.w.lines[0]
	alice.w.mu.Unlock()
	if !strings.Contains(hello, appwire.FeatureFactoryReadOnArrival) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryReadOnArrival, hello)
	}
	var got appwire.FactoryReadOnArrival
	alice.w.must("factory.read_on_arrival", map[string]any{"factory": alice.conn.ID}, &got)
	if got.On {
		t.Fatal("a new factory reads as on")
	}
	bob.w.mustFail("factory.read_on_arrival", map[string]any{"factory": bob.conn.ID, "on": true}, appwire.CodeForbidden)
	if saved, _ := f.st.RunnerSettings(); saved.ReadOnArrival {
		t.Fatal("a teammate turned it on")
	}
	alice.w.must("factory.read_on_arrival", map[string]any{"factory": alice.conn.ID, "on": true}, &got)
	saved, err := f.st.RunnerSettings()
	if !got.On || err != nil || !saved.ReadOnArrival {
		t.Fatalf("after turning it on: answer %+v, saved %+v, %v", got, saved, err)
	}
}
