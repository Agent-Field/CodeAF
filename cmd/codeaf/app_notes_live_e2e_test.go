//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	cellstoretest "github.com/Agent-Field/codeaf/internal/cellstore/celltest"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// THE LIVE JOURNEY OF A MESSAGE TO ANOTHER COMPUTER. Two real `codeaf app`
// processes, A and B, paired through a loopback relay, every model call the
// pinned [liveFactoryModel] through [liveRecorder]. It walks what a person at
// B does with a chat A holds, and asserts the facts the desktop draws from:
//
//  1. A message sent while A is open is delivered, and A's real model runs it.
//  2. A quits: the row says A is offline, and a message sent then waits.
//  3. B continues the chat here: the waiting message runs on B, with B's model,
//     right after the move (THE CHAT, NOT THE COMPUTER, IS THE ADDRESS) — the
//     fact the desktop's "Continuing here runs your waiting messages" states.
//  4. The relay goes away: a send says so in the person's words, unreachable.
//
// Opt-in (CODEAF_LIVE_FACTORY=1); a few model calls, well under a cent.
func TestLiveAMessageToAnotherComputerFollowsTheChat(t *testing.T) {
	if os.Getenv(liveFactoryOptIn) != "1" {
		t.Skipf("opt-in live lane: set %s=1 (spends under a cent on OpenRouter)", liveFactoryOptIn)
	}
	if liveFactoryKey == "" {
		t.Fatal("BLOCKED: no OpenRouter key by any road the product reads; not faking a pass")
	}
	celltest.RequireEngine(t)
	root := e2TempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("OPENROUTER_API_KEY", liveFactoryKey)
	recorder := newLiveRecorder(t, config.DefaultBaseURL)
	t.Setenv("CODEAF_BASE_URL", recorder.server.URL)
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workspace)
	freshEngineProcess(t)
	t.Cleanup(func() {
		recorder.mu.Lock()
		defer recorder.mu.Unlock()
		t.Logf("live model: %d calls %v, replies %v, statuses %v, cost $%.4f", recorder.calls, recorder.requests, recorder.replies, recorder.statuses, recorder.cost)
		for _, p := range recorder.problems {
			t.Errorf("live model: %s", p)
		}
	})

	relay := e2WireRelay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	t.Setenv(syncsetup.IntervalVar, "50")
	dirA, dirB := home.Dir(), e2TempDir(t)
	cellstoretest.EngineDir(t)
	id, err := identity.Ensure(dirA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(dirB, id, false); err != nil {
		t.Fatal(err)
	}
	rows, _ := json.Marshal(map[string]any{
		"tools.approvalMode": "allow", config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: liveFactoryModel,
		"models.roles": "", "models.tiers.low": liveFactoryModel, "models.tiers.high": liveFactoryModel,
		"models.tiers.reflex": liveFactoryModel, "models.tiers.mastermind": liveFactoryModel,
		"models.tiers.worker": liveFactoryModel, "task.model": liveFactoryModel, "models.fallbacks": "",
		config.KeyChatModel: liveFactoryModel,
	})
	for dir, name := range map[string]string{dirA: "A", dirB: "B"} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.json"), rows, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := devname.Set(dir, name); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{dirA, dirB} {
		s := appDeviceSync(t, dir)
		if err := syncsetup.PutOwnDevice(context.Background(), s.Dir, s.Identity, s.Device, devname.Name(dir)); err != nil {
			t.Fatal(err)
		}
	}
	start := func(dir string) *e2App {
		return e2StartAppWithSetup(t, dir, func(app *e2App) {
			machineCall[any](t, app.w, "setup.get", struct{}{})
			machineCall[appwire.Empty](t, app.w, "keys.set", map[string]any{"provider": "openrouter", "key": liveFactoryKey})
			machineCall[appwire.ChatsResult](t, app.w, "chats.list", struct{}{})
		})
	}
	a, b := start(dirA), start(dirB)

	created := machineCall[struct {
		Agent appwire.Agent `json:"agent"`
	}](t, a.w, "agents.create", map[string]any{"workspace": workspace}).Agent
	e2Turn(t, a, created.ID, "Say hello in one word.")

	rowAtB := func() (appwire.ChatRow, bool) {
		for _, r := range machineCall[appwire.ChatsResult](t, b.w, "chats.list", struct{}{}).Chats {
			if r.Cell == created.ID {
				return r, true
			}
		}
		return appwire.ChatRow{}, false
	}
	entries := func(app *e2App, agent string) string {
		raw, failed := app.w.call("agents.entries", appwire.IDParams{ID: agent})
		if failed != nil {
			return ""
		}
		return string(raw)
	}
	until := func(what string, within time.Duration, check func() bool) {
		t.Helper()
		deadline := time.Now().Add(within)
		for !check() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out after %s waiting for %s", within, what)
			}
			time.Sleep(250 * time.Millisecond)
		}
	}

	// 1. Open on A: a message from B is delivered and A's model answers it.
	until("B to list A's chat as open on A", time.Minute, func() bool {
		r, ok := rowAtB()
		return ok && !r.Here && r.Status == "running" && r.Online != nil && *r.Online
	})
	first := machineCall[appwire.ChatSendResult](t, b.w, "chats.send", appwire.ChatSendParams{Cell: created.ID, Text: "Reply with only the word PAPAYA written backwards in capitals.", ClientID: "n1"})
	if first.Note.State != appwire.ChatNoteWaiting {
		t.Fatalf("first send = %+v; want waiting", first.Note)
	}
	until("A's model to answer B's message", 3*time.Minute, func() bool { return strings.Contains(entries(a, created.ID), "AYAPAP") })
	until("B's row to say delivered", time.Minute, func() bool {
		r, _ := rowAtB()
		return len(r.Notes) == 1 && r.Notes[0].ID == first.Note.ID && r.Notes[0].State == appwire.ChatNoteDelivered
	})

	// 2. A quits: B's row says it is offline, and a message sent now waits.
	e2Quit(t, a)
	until("B's row to say A is offline", time.Minute, func() bool {
		r, _ := rowAtB()
		return r.Online != nil && !*r.Online
	})
	second := machineCall[appwire.ChatSendResult](t, b.w, "chats.send", appwire.ChatSendParams{Cell: created.ID, Text: "Reply with only the word MANGO written backwards in capitals.", ClientID: "n2"})
	var row appwire.ChatRow
	until("the second message to wait while A is offline, and the chat to be takeable", time.Minute, func() bool {
		row, _ = rowAtB()
		waiting := false
		for _, n := range row.Notes {
			waiting = waiting || n.ID == second.Note.ID && n.State == appwire.ChatNoteWaiting
		}
		return waiting && row.CanTake
	})

	// 3. Continue here: the waiting message runs on B, with B's model.
	taken := machineCall[appwire.TakeResult](t, b.w, "agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen}).Agent
	if taken.Location == nil || !taken.Location.Here {
		t.Fatalf("taken: %+v", taken)
	}
	until("B's model to answer the message that waited for A", 3*time.Minute, func() bool { return strings.Contains(entries(b, taken.ID), "OGNAM") })
	if got := entries(b, taken.ID); !strings.Contains(got, "AYAPAP") {
		t.Fatalf("the moved chat lost A's answer to the first message")
	}

	// 4. The relay goes away: a send is refused in the person's words.
	relay.CloseClientConnections()
	relay.Close()
	_, down := b.w.call("chats.send", appwire.ChatSendParams{Cell: created.ID, Text: "while the relay is down", ClientID: "n3"})
	if down == nil || down.Code != appwire.CodeUnreachable || down.Message != appNoteUnreachable {
		t.Fatalf("relay down = %+v; want %q", down, appNoteUnreachable)
	}
}
