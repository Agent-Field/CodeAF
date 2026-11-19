package main

import (
	"net/http"
	"os"
	"path/filepath"
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
	t.Setenv(syncsetup.URLVar, "")
	offering := t.TempDir()
	if err := syncsetup.SaveRelayURL(offering, relay.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Ensure(offering); err != nil {
		t.Fatal(err)
	}
	a := e2StartAppWithSetup(t, offering, nil)
	dir := t.TempDir()
	// The native desktop installs this skill before launching the engine.
	const bundledSkill = "---\nname: workaf-canvas\ndescription: Create canvases\n---\n<!-- workaf-canvas v17 -->\n"
	skillPath := filepath.Join(dir, ".codeaf", "skills", "workaf-canvas", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte(bundledSkill), 0o600); err != nil {
		t.Fatal(err)
	}
	opened := func() *e2App {
		return e2StartAppWithSetup(t, dir, func(b *e2App) {
			machineCall[any](t, b.w, "setup.get", struct{}{})
			machineCall[any](t, b.w, "setup.set", appwire.Setup{
				Step: appwire.SetupStep{Done: true}, Start: appwire.SetupAgents,
				Provider: "openrouter", DefaultModel: "vendor/model",
			})
			machineCall[appwire.DevicesResult](t, b.w, "devices.list", struct{}{})
		})
	}
	b := opened()
	e2Quit(t, b)
	// WorkAF writes this file when picking a relay, even if no chat was sent.
	if err := syncsetup.SaveRelayURL(dir, relay.URL); err != nil {
		t.Fatal(err)
	}
	old, err := identity.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := syncsetup.SaveStorage(dir, relay.URL, old.ID(), "on"); err != nil {
		t.Fatal(err)
	}
	if ok, err := identity.Joinable(dir); err != nil || !ok {
		t.Fatalf("an opened but unused home refused pairing: %v", err)
	}
	b = opened()
	var approveID, joinID string
	if method == "code" {
		offer := machineCall[appwire.PairResult](t, a.w, "pair.start", appwire.PairStartParams{Mode: "offer_code", Name: "Living Room Mac mini"}).Pairing
		code := e2PairWait(t, a.w, offer.ID, "waiting").Code
		join := machineCall[appwire.PairResult](t, b.w, "pair.start", appwire.PairStartParams{Mode: "use_code", Code: code, Name: "Travel MacBook"}).Pairing
		approveID, joinID = offer.ID, join.ID
	} else {
		request := machineCall[appwire.PairResult](t, b.w, "pair.start", appwire.PairStartParams{Mode: "request_link", Name: "Travel MacBook"}).Pairing
		link := e2PairWait(t, b.w, request.ID, "waiting").Link
		approve := machineCall[appwire.PairResult](t, a.w, "pair.start", appwire.PairStartParams{Mode: "approve_link", Link: link, Name: "Living Room Mac mini"}).Pairing
		approveID, joinID = approve.ID, request.ID
	}
	question := e2PairWait(t, a.w, approveID, "question").Question
	machineCall[appwire.PairResult](t, a.w, "pair.answer", appwire.PairAnswerParams{ID: approveID, Question: question.ID, Option: "y"})
	e2PairWait(t, a.w, approveID, "done")
	if done := e2PairWait(t, b.w, joinID, "done"); !done.Restart {
		t.Fatal("joining an already opened home did not request an engine restart")
	}
	if current, err := os.ReadFile(skillPath); err != nil || string(current) != bundledSkill {
		t.Fatalf("ordinary pairing did not preserve the bundled skill: %v", err)
	}
	savedSetup := machineCall[struct {
		Setup appwire.Setup `json:"setup"`
	}](t, b.w, "setup.get", struct{}{}).Setup
	if !savedSetup.Step.Done {
		t.Fatal("ordinary pairing did not preserve completed onboarding")
	}
	id, err := identity.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	e2Quit(t, b)
	b = e2StartAppWithSetup(t, dir, nil)
	for _, check := range []struct {
		app  *e2App
		peer string
	}{{a, "Travel MacBook"}, {b, "Living Room Mac mini"}} {
		appSyncWait(t, "the other computer appears with its chosen name", func() bool {
			for _, device := range appDeviceList(t, check.app.w).Devices {
				if !device.Self && !device.Removed && device.Name == check.peer {
					return true
				}
			}
			return false
		})
	}
	if got := syncsetup.ReportedStorage(dir, relay.URL, id.ID()); got != "off" {
		t.Fatalf("the completed pairing's storage answer was %q", got)
	}
}
