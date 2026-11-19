package main

import (
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestQAOpenedDesktopHomeCanJoinAndCacheStorage(t *testing.T) {
	for _, method := range []string{"code", "link"} {
		t.Run(method, func(t *testing.T) { testOpenedDesktopPair(t, method) })
	}
}

func testOpenedDesktopPair(t *testing.T, method string) {
	t.Helper()
	celltest.RequireEngine(t)
	relay := e2Relay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/store/stats" {
				_, _ = w.Write([]byte(`{"storage":"off"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	t.Setenv(syncsetup.URLVar, relay.URL)
	offering := t.TempDir()
	if _, err := identity.Ensure(offering); err != nil {
		t.Fatal(err)
	}
	a := e2StartAppWithSetup(t, offering, nil)
	dir := t.TempDir()
	opened := func() *e2App {
		return e2StartAppWithSetup(t, dir, func(b *e2App) {
			machineCall[any](t, b.w, "setup.get", struct{}{})
			machineCall[appwire.DevicesResult](t, b.w, "devices.list", struct{}{})
		})
	}
	b := opened()
	e2Quit(t, b)
	if ok, err := identity.Joinable(dir); err != nil || !ok {
		t.Fatalf("an opened but unused home refused pairing: %v", err)
	}
	b = opened()
	var approveID, joinID string
	if method == "code" {
		offer := machineCall[appwire.PairResult](t, a.w, "pair.start", appwire.PairStartParams{Mode: "offer_code"}).Pairing
		code := e2PairWait(t, a.w, offer.ID, "waiting").Code
		join := machineCall[appwire.PairResult](t, b.w, "pair.start", appwire.PairStartParams{Mode: "use_code", Code: code}).Pairing
		approveID, joinID = offer.ID, join.ID
	} else {
		request := machineCall[appwire.PairResult](t, b.w, "pair.start", appwire.PairStartParams{Mode: "request_link"}).Pairing
		link := e2PairWait(t, b.w, request.ID, "waiting").Link
		approve := machineCall[appwire.PairResult](t, a.w, "pair.start", appwire.PairStartParams{Mode: "approve_link", Link: link}).Pairing
		approveID, joinID = approve.ID, request.ID
	}
	question := e2PairWait(t, a.w, approveID, "question").Question
	machineCall[appwire.PairResult](t, a.w, "pair.answer", appwire.PairAnswerParams{ID: approveID, Question: question.ID, Option: "y"})
	e2PairWait(t, a.w, approveID, "done")
	if done := e2PairWait(t, b.w, joinID, "done"); !done.Restart {
		t.Fatal("joining an already opened home did not request an engine restart")
	}
	id, err := identity.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := syncsetup.ReportedStorage(dir, relay.URL, id.ID()); got != "off" {
		t.Fatalf("the completed pairing's storage answer was %q", got)
	}
}
