package main

import (
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// soloStub is a solo factory with no bearer held for it. refuse makes it
// answer every request, the mode probe included, with "sign in".
func soloStub(t *testing.T, refuse *atomic.Bool) (*factoryWire, *stubFactory, appwire.FactoryConnection) {
	t.Helper()
	stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
		if refuse.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":"sign in"}`)
			return
		}
		switch r.URL.Path {
		case "/v1/me":
			io.WriteString(w, `{"mode":"solo","handle":"solo"}`)
		case "/v1/queue":
			io.WriteString(w, `{}`)
		default:
			io.WriteString(w, `{"number":1}`)
		}
	})
	w := newFactoryWire(t, testFactoryHub(t))
	var conn appwire.FactoryConnection
	w.must("factory.attach", map[string]any{"url": stub.srv.URL}, &conn)
	w.must("factory.queue", map[string]any{"factory": conn.ID}, nil)
	return w, stub, conn
}

// A solo factory that stops answering is unreachable on its reads, as it is in
// the list, never signed_out: the desktop keeps its rows for unreachable and
// drops them for signed_out.
func TestAppFactorySoloFactoryThatStopsAnsweringReadsAsUnreachable(t *testing.T) {
	var refuse atomic.Bool
	w, stub, conn := soloStub(t, &refuse)
	stub.srv.Close()

	w.mustFail("factory.queue", map[string]any{"factory": conn.ID}, appwire.CodeUnreachable)
	w.mustFail("factory.job", map[string]any{"factory": conn.ID, "job": 1}, appwire.CodeUnreachable)
	var list appwire.FactoryList
	w.must("factory.list", map[string]any{}, &list)
	if len(list.Factories) != 1 || list.Factories[0].State != appwire.FactoryUnreachable {
		t.Fatalf("list: %+v", list.Factories)
	}
}

// A factory that answers and asks for a sign-in is still signed_out.
func TestAppFactoryAnsweringFactoryThatWantsSignInReadsAsSignedOut(t *testing.T) {
	var refuse atomic.Bool
	w, _, conn := soloStub(t, &refuse)
	refuse.Store(true)

	w.mustFail("factory.queue", map[string]any{"factory": conn.ID}, appwire.CodeSignedOut)
	w.mustFail("factory.job", map[string]any{"factory": conn.ID, "job": 1}, appwire.CodeSignedOut)
}
