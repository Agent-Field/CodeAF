//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestDisplacedWindowsContinueWithoutBranchesOrLostDrafts(t *testing.T) {
	root, err := os.MkdirTemp("", "l2lfix-displaced-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	const first = "Remember the mango code word."
	const second = "Recall the mango code word."
	const draftA = "Add 2 mangoes to the saved count."
	const draftB = "Add 3 mangoes to the saved count."
	var requests atomic.Int32
	brain := &automationBrain{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/models", brain.serveCatalog)
	mux.HandleFunc("/api/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		reply := "MANGO_REMEMBERED"
		if strings.Contains(string(raw), second) {
			reply = "MANGO_RECALLED"
		}
		if strings.Contains(string(raw), draftA) {
			reply = "MANGO_ADDED"
		}
		if strings.Contains(string(raw), draftA) || strings.Contains(string(raw), draftB) {
			requests.Add(1)
		}
		send, ok := automationStream(w)
		if !ok {
			return
		}
		send(fmt.Sprintf(`{"content":%q}`, reply), "")
		send(`{}`, "stop")
		send(`{}`, "[DONE]")
	})
	model := httptest.NewServer(mux)
	t.Cleanup(model.Close)
	relay := quitContinuationRelay(t, root)
	t.Setenv(syncsetup.URLVar, relay)
	homes := []string{filepath.Join(root, "first"), filepath.Join(root, "second")}
	names := []string{"first", "second"}
	work := []string{filepath.Join(root, "work-first"), filepath.Join(root, "work-second")}
	for i, home := range homes {
		if err := os.MkdirAll(home, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(work[i], 0700); err != nil {
			t.Fatal(err)
		}
		rows := map[string]any{"api_key": "stub-key", "model.talk": automationStubModel, "memory.enabled": false, "tools.approvalMode": "allow", "setup_seen_at": time.Now().UTC().Format(time.RFC3339), config.KeyIcons: config.IconsPlain}
		for _, key := range []string{config.KeyTaskModel, config.KeyTierWorkerModel, config.KeyTierLowModel, config.KeyTierHighModel, config.KeyTierReflexModel, config.KeyTierMastermindModel} {
			rows[key] = automationStubModel
		}
		raw, _ := json.Marshal(rows)
		if err := os.WriteFile(filepath.Join(home, "config.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	id, err := identity.Ensure(homes[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(homes[1], id, false); err != nil {
		t.Fatal(err)
	}
	var syncs []*syncsetup.Sync
	for i, home := range homes {
		if _, err := devname.Set(home, names[i]); err != nil {
			t.Fatal(err)
		}
		s, ok, err := syncsetup.Open(home)
		if err != nil || !ok {
			t.Fatalf("paired home: %v %v", ok, err)
		}
		syncs = append(syncs, s)
		if err := syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, names[i]); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{config.APIKeyEnv + "=stub-key", "CODEAF_BASE_URL=" + model.URL + "/api/v1", "CODEAF_PROFILE_DIR=", "CODEAF_SYNC_URL=" + relay, "CODEAF_SYNC_INTERVAL_MS=100", "CODEAF_TELEMETRY=off", "DO_NOT_TRACK=1", "CODEAF_NO_UPDATE_CHECK=1", "CODEAF_NO_AUTOMATIONS=1"}
	t.Cleanup(func() {
		pids := engineDaemonsUnder(root)
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
		deadline := time.Now().Add(10 * time.Second)
		for _, pid := range pids {
			for !terminalProcessExited(pid) && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			if !terminalProcessExited(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	// Both launch chats remain unused while a separate conversation travels.
	for i, home := range homes {
		startWithEnv(t, env, fmt.Sprintf("unused-%d", i), home, work[i], 180, 55)
		work[i] += "-chat"
		if err := os.Mkdir(work[i], 0700); err != nil {
			t.Fatal(err)
		}
	}
	a := startWithEnv(t, env, "displaced-first", homes[0], work[0], 180, 55)
	b := startWithEnv(t, env, "displaced-second", homes[1], work[1], 180, 55)
	a.lit(first)
	a.keys("Enter")
	a.waitFor(35*time.Second, "MANGO_REMEMBERED", "idle")
	waitDisplacedChatSaved(t, syncs[0], first)
	b.lit("/home")
	b.keys("Enter")
	b.waitFor(35*time.Second, "running on "+names[0], "type to search or start")
	selected := false
	for y, line := range strings.Split(b.capture(), "\n") {
		if strings.Contains(line, "running on "+names[0]) && !strings.Contains(line, "untitled") {
			x := ansi.StringWidth(line[:strings.Index(line, "running on")])
			t.Logf("continuing the used chat: %s", strings.TrimSpace(line))
			b.mouseClick(x+2, y+1)
			selected = true
			break
		}
	}
	if !selected {
		t.Fatalf("Home did not list the used chat:\n%s", b.capture())
	}
	if screen := b.waitFor(10*time.Second, "Continue this chat here?"); !strings.Contains(screen, "Continue this chat here?") {
		t.FailNow()
	}
	b.keys("1", "Enter")
	b.waitFor(35*time.Second, "Moved from "+names[0])
	b.lit(second)
	b.keys("Enter")
	b.waitFor(35*time.Second, "MANGO_RECALLED", "idle")
	waitDisplacedChatSaved(t, syncs[1], second)
	for i, turn := range []struct {
		window *rig
		draft  string
		from   string
	}{{a, draftA, names[1]}, {b, draftB, names[0]}} {
		turn.window.waitFor(15*time.Second, "this window now only shows it")
		turn.window.lit(turn.draft)
		before := requests.Load()
		turn.window.keys("Enter")
		turn.window.waitFor(10*time.Second, "Continue this chat here?")
		turn.window.keys("1", "Enter")
		screen := turn.window.waitFor(35*time.Second, "Moved from "+turn.from)
		if strings.Contains(screen, "were kept as") {
			t.Errorf("shared work was reported as kept edits:\n%s", screen)
		}
		if !strings.Contains(screen, "› "+turn.draft) {
			t.Errorf("arrived composer lost its unsent words:\n%s", screen)
		}
		if requests.Load() != before {
			t.Error("continuation sent without another enter")
		}
		if i == 0 {
			// Keep the sequence moving even on a failing draft assertion.
			turn.window.keys("C-u")
			turn.window.lit(turn.draft)
			turn.window.keys("Enter")
			turn.window.waitFor(35*time.Second, "MANGO_ADDED", "idle")
			waitDisplacedChatSaved(t, syncs[0], draftA)
		}
	}
	for _, window := range []*rig{a, b} {
		window.keys("C-u")
		window.lit("/home")
		window.keys("Enter")
		screen := window.waitFor(10*time.Second, "type to search or start")
		if strings.Contains(screen, "turn from") || strings.Contains(screen, "turns from") {
			t.Errorf("Home lists a branch for shared work:\n%s", screen)
		}
		for _, line := range strings.Split(screen, "\n") {
			if strings.Contains(line, "untitled") && strings.Contains(line, "running on") {
				t.Errorf("Home lists an unused chat from another computer: %s", line)
			}
		}
	}
}

// A reply can reach the screen before its asynchronous drive has acquired its
// final fence and published the seal. The next move waits for that boundary.
func waitDisplacedChatSaved(t *testing.T, client *syncsetup.Sync, words string) {
	t.Helper()
	var at cell.Cell
	err := filepath.WalkDir(filepath.Join(client.Home, "v3", "projects"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != "transcript.jsonl" {
			return err
		}
		raw, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(raw), words) {
			root := session.DirOf(path)
			at = cell.Cell{ID: filepath.Base(root), Root: root}
		}
		return err
	})
	if err != nil || at.ID == "" {
		t.Fatalf("saved conversation was not found: %v", err)
	}
	var fence uint64
	if !waitUntil(25*time.Second, func() bool {
		head, err := cellstore.Head(at)
		if err != nil || head == nil {
			return false
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		view, err := client.Dir.Cell(ctx, at.ID)
		if err != nil || view.Cell.Head != head.Turn.ID || view.Cell.Lease.Device != client.Device.ID() {
			return false
		}
		stable := fence != 0 && fence == view.Cell.Lease.Fence
		fence = view.Cell.Lease.Fence
		return stable
	}) {
		t.Fatal("reply did not become durable under its driving session")
	}
}
