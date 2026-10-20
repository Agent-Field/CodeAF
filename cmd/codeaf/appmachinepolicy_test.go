package main

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cachedir"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/subharness"
)

type machineHarness struct {
	b     *appBackend
	w     *actionWire
	agent string
}

// Machine methods need no model endpoint or listening socket. The wire uses
// pipes so the confirmation and policy checks also run in restricted sandboxes.
func newMachineHarness(t *testing.T) *machineHarness {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv(config.ProfileDirEnv, "")
	b, _, agent := workBackend(t, &fakeConversation{})
	b.profileDir = t.TempDir()
	return &machineHarness{b: b, w: newActionWire(t, b), agent: agent.ID}
}

func machineCall[T any](t *testing.T, w *actionWire, method string, params any) T {
	t.Helper()
	raw, err := w.call(method, params)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	var got T
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

// This drives deletion through the real wire, proving absent, false and
// misspelled confirmation cannot reach the shared producer.
func TestAppMachineCacheConfirmationAndRadius(t *testing.T) {
	h := newMachineHarness(t)
	for _, feature := range []string{"cache.status", "cache.clean", "autonomy.get", "autonomy.set", "harness.list", "settings.crew.get", "settings.crew.set"} {
		if !slices.Contains(h.w.hello, feature) {
			t.Errorf("hello missing %s", feature)
		}
	}
	root := cachedir.Root()
	dir := filepath.Join(root, "toolchain", "go-mod")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cache"), []byte("cached"), 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(filepath.Dir(root), "keep-settings")
	if err := os.WriteFile(sibling, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	status := machineCall[appwire.CacheStatus](t, h.w, "cache.status", map[string]any{})
	if status.Path != root || status.Bytes != 6 || status.Human != "6 B" {
		t.Fatalf("status: %+v", status)
	}
	for _, params := range []map[string]any{{}, {"confirm": false}, {"confirmed": true}, {"confirm": "true"}, {"confirm": true, "host": "other"}} {
		if _, err := h.w.call("cache.clean", params); err == nil || err.Code != appwire.CodeBadParams {
			t.Fatalf("unguarded clean %v: %v", params, err)
		}
		if cachedir.Size() != 6 {
			t.Fatal("refused call changed cache")
		}
	}
	clean := machineCall[appwire.CacheCleanResult](t, h.w, "cache.clean", map[string]any{"confirm": true})
	if clean.FreedBytes != 6 || cachedir.Size() != 0 {
		t.Fatalf("clean: %+v", clean)
	}
	if raw, err := os.ReadFile(sibling); err != nil || string(raw) != "keep" {
		t.Fatalf("sibling changed: %q %v", raw, err)
	}
	clean = machineCall[appwire.CacheCleanResult](t, h.w, "cache.clean", map[string]any{"confirm": true})
	if clean.FreedBytes != 0 {
		t.Fatalf("second clean: %+v", clean)
	}
}

// The conversation uses remote.Loopback over the real session. No model turn
// runs: writes and the always-ask refusals are the terminal engine's own.
func TestAppMachineAutonomyUsesConversationDoor(t *testing.T) {
	h := newMachineHarness(t)
	agent, err := session.New(session.Config{Workspace: t.TempDir(), Model: "test/model", APIKey: "test-key", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
		return &remote.Engine{Agent: agent}, nil
	}})
	if err != nil {
		agent.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	h.b.locked(func() { h.b.agents[h.agent].live.conn.Conv = loop.Client.Agent() })
	get := func() appwire.AutonomyResult {
		return machineCall[appwire.AutonomyResult](t, h.w, "autonomy.get", map[string]any{"agent": h.agent})
	}
	before := get()
	if len(before.Kinds) != 8 {
		t.Fatalf("kinds: %+v", before)
	}
	changed := machineCall[appwire.AutonomyResult](t, h.w, "autonomy.set", appwire.AutonomySetParams{Agent: h.agent, Kind: "choice", Rule: "recommend 30s"})
	if changed.Kinds[1].Rule != "recommend" || changed.Kinds[1].After != "30s" {
		t.Fatalf("rule: %+v", changed)
	}
	if get().Kinds[1] != changed.Kinds[1] {
		t.Fatal("rule was not persisted")
	}
	for _, kind := range []string{"confirmation", "clarification", "made-up"} {
		_, err := h.w.call("autonomy.set", appwire.AutonomySetParams{Agent: h.agent, Kind: kind, Rule: "decide"})
		if err == nil || err.Code != appwire.CodeBadParams {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	for _, rule := range []string{"recommend 0s", "recommend nonsense", "never", "ask ignored"} {
		_, err := h.w.call("autonomy.set", appwire.AutonomySetParams{Agent: h.agent, Kind: "choice", Rule: rule})
		if err == nil || err.Code != appwire.CodeBadParams {
			t.Fatalf("%s: %v", rule, err)
		}
	}
}

func TestAppMachineCrewRulesAndCapsKeepTheirMeaning(t *testing.T) {
	h := newMachineHarness(t)
	for _, rule := range []string{"all", "open", "≤1/5", "+vendor/model", "-vendor/model"} {
		got := machineCall[appwire.CrewPolicy](t, h.w, "settings.crew.set", map[string]any{"models": rule})
		if got.Models != config.CrewAllowedAt(h.b.profileDir).String() {
			t.Fatalf("rule %q: %+v", rule, got)
		}
	}
	got := machineCall[appwire.CrewPolicy](t, h.w, "settings.crew.set", map[string]any{"cap": "3"})
	if got.CapUSD != 3 {
		t.Fatalf("cap: %+v", got)
	}
	for _, params := range []map[string]any{{}, {"models": "all", "cap": "4"}, {"cap": "-1"}, {"models": ""}, {"models": "   "}, {"cap": ""}} {
		if _, err := h.w.call("settings.crew.set", params); err == nil {
			t.Fatalf("accepted %v", params)
		}
	}
	if config.CrewCapAt(h.b.profileDir) != 3 {
		t.Fatal("refusal changed cap")
	}
	got = machineCall[appwire.CrewPolicy](t, h.w, "settings.crew.set", map[string]any{"cap": "off"})
	if got.CapUSD != 0 {
		t.Fatalf("off: %+v", got)
	}
}

func TestAppMachineHarnessListReadsSavedHeads(t *testing.T) {
	h := newMachineHarness(t)
	empty := machineCall[appwire.HarnessListResult](t, h.w, "harness.list", map[string]any{})
	if empty.Harnesses == nil || len(empty.Harnesses) != 0 {
		t.Fatalf("empty: %+v", empty)
	}
	store := subharness.Default()
	page := machineSavedPage("wire-shape")
	if _, err := store.Save(page); err != nil {
		t.Fatal(err)
	}
	page.Id.Desc = "latest"
	if _, err := store.Save(page); err != nil {
		t.Fatal(err)
	}
	got := machineCall[appwire.HarnessListResult](t, h.w, "harness.list", map[string]any{})
	if len(got.Harnesses) != 1 || got.Harnesses[0].Description != "latest" || got.Harnesses[0].Version != 2 || got.Harnesses[0].LastRun != nil {
		t.Fatalf("list: %+v", got)
	}
	saved, err := store.Load("wire-shape", 0)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	if _, err := store.SaveRun(subharness.Trace{Id: saved.Id, Started: at}); err != nil {
		t.Fatal(err)
	}
	got = machineCall[appwire.HarnessListResult](t, h.w, "harness.list", map[string]any{})
	if got.Harnesses[0].Runs != 1 || got.Harnesses[0].LastRun == nil || !got.Harnesses[0].LastRun.At.Equal(at) {
		t.Fatalf("history: %+v", got)
	}
	if _, err := h.w.call("harness.use", map[string]any{"agent": "x", "name": "wire-shape"}); err == nil || err.Code != appwire.CodeUnknownMethod {
		t.Fatalf("use: %v", err)
	}
}

func TestAppMachineHarnessListRefusesOversizeRegistry(t *testing.T) {
	h := newMachineHarness(t)
	store := subharness.Default()
	for i := 0; i < 129; i++ {
		if _, err := store.Save(machineSavedPage("shape-" + actionItoa(i))); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := h.w.call("harness.list", map[string]any{}); err == nil || err.Code != appwire.CodeTooLarge {
		t.Fatalf("oversize: %v", err)
	}
}

func machineSavedPage(name string) subharness.Harness {
	return subharness.Harness{
		Id: subharness.Id{Name: name, Desc: "first"},
		Program: subharness.Program{Nodes: []subharness.Node{
			{Id: "look", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "look", "tools": "read"}},
			{Id: "land", Kind: subharness.KindHumanGate, Fields: subharness.Fields{"ask": "land it?"}},
		}, Edges: []subharness.Edge{{"look", "land"}}}, Whitelist: []string{"read"},
	}
}
