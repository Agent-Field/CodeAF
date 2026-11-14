package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// Exercise the terminal's own construction, including its liveDrive, watch,
// adaptive session and memory store. A hand-bound Drive misses this boundary.
func TestIdleRelease_R6_RealTerminalReleasesAfterModelAndShell(t *testing.T) {
	idleRealTerminal(t, false)
}
func TestIdleRelease_R5_1_RealTerminalSettingsAndMemoryReachReceiver(t *testing.T) {
	idleRealTerminal(t, true)
}
func idleRealTerminal(t *testing.T, settings bool) {
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	service := relayserve.New(relayserve.Config{Store: t.TempDir()})
	t.Cleanup(func() { _ = service.Close() })
	h := newHostedMoveRig(t, func(http.Handler) http.Handler { return service.Handler })
	t.Setenv("CODEAF_NO_UPDATE_CHECK", "1")
	t.Setenv("CODEAF_ENGINE_DAEMON", "1")
	t.Setenv(syncsetup.IntervalVar, "5000")
	t.Setenv("CODEAF_PROFILE_DIR", h.a.Home)
	if err := h.b.Rename(t.Context(), "other computer"); err != nil {
		t.Fatal(err)
	}
	place, err := v3MintSession(filepath.Join(h.a.Home, "v3", "projects", "terminal"), h.work, h.work, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(place.Workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if raw, err := exec.Command("git", "init", "--quiet", place.Workspace).CombinedOutput(); err != nil {
		t.Fatalf("fixture git: %s %v", raw, err)
	}
	brain := v3Memory(h.a.Home)
	if brain == nil {
		t.Fatal("no terminal memory store")
	}
	defer brain.Close()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		answer := "terminal answer"
		if strings.Contains(string(raw), "D1 second question") {
			answer = "terminal second answer"
		}
		content, _ := json.Marshal(answer)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", content)
	}))
	defer model.Close()
	cfg := session.Config{Workspace: place.Workspace, Place: place, SessionFile: place.Transcript(), ProfileDir: h.a.Home,
		Model: "stub/chat", APIKey: "stub", BaseURL: model.URL, System: "SYSTEM", Memory: brain, MemoryIndex: brain}
	agent, cfg, _, err := openV3Agent(cfg, place.Workspace, v3OpenSession)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { agent.Close(); syncDrives.closeAll() }()
	noRestore := &walkNoRestore{attempted: make(chan struct{}, 1)}
	input, typed := io.Pipe()
	screen := &surfaceFrameWriter{ready: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	previousRun := runSurfaceProgram
	runSurfaceProgram = func(ctx context.Context, o tui3.Options) error {
		o.Input, o.Output, o.Width, o.Height = input, screen, 120, 35
		return tui3.Run(ctx, o)
	}
	defer func() { runSurfaceProgram = previousRun }()
	go func() {
		done <- runSurface(ctx, tui3.Options{Agent: agent, Workspace: place.Workspace, SessionFile: place.Transcript(), ProfileDir: h.a.Home, Machines: surfaceMachines{h.a}, Takeover: noRestore, Memory: v3MemorySeam(brain), Input: input, Output: screen, Width: 120, Height: 35})
	}()
	defer func() { cancel(); input.Close(); typed.Close(); <-done }()
	select {
	case <-screen.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("no terminal frame")
	}
	send := func(text, saved string) {
		t.Helper()
		if _, err := io.WriteString(typed, text+"\r"); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			raw, _ := os.ReadFile(place.Transcript())
			if strings.Contains(string(raw), saved) {
				time.Sleep(200 * time.Millisecond)
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("terminal did not save %s: %s", saved, screen.String())
	}
	send("terminal model question", "terminal answer")
	send("! printf terminal-file > terminal.txt; printf 'node_modules/\\n' > .gitignore; mkdir -p node_modules/x; printf local-install > node_modules/x/index.js", "terminal-file")

	id := place.ID()
	released := func(after uint64) directory.CellView {
		t.Helper()
		c, err := cell.OpenAt(place.Dir, id)
		if err != nil {
			t.Fatal(err)
		}
		// A command's words enter the journal before its tool has finished.
		// Measure idle release from settled work and its published local head,
		// as in the hand walk, rather than charging execution to release latency.
		settling := time.Now()
		readyBy := settling.Add(30 * time.Second)
		published := false
		for time.Now().Before(readyBy) {
			head, headErr := cellstore.Head(c)
			view, viewErr := h.a.Dir.Cell(t.Context(), id)
			if headErr == nil && head != nil && viewErr == nil && view.Cell.Head == head.Turn.ID && view.Cell.Lease.Fence > after && cfg.Seals.(interface{ IdleReady() bool }).IdleReady() {
				published = true
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !published {
			head, _ := cellstore.Head(c)
			view, viewErr := h.a.Dir.Cell(t.Context(), id)
			t.Fatalf("terminal work did not settle and publish: ready=%v local=%+v relay=%+v error=%v title=%q", cfg.Seals.(interface{ IdleReady() bool }).IdleReady(), head, view, viewErr, agent.Title())
		}
		t.Logf("terminal work settled and published in %s", time.Since(settling))
		quiet := time.Now()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			view, e := h.a.Dir.Cell(t.Context(), id)
			if e == nil && view.Cell.Head != "" && view.Cell.Lease.Expires == 0 && view.Cell.Lease.Fence > after {
				t.Logf("fence %d released %s after settled publication", view.Cell.Lease.Fence, time.Since(quiet))
				return view
			}
			time.Sleep(50 * time.Millisecond)
		}
		stack := make([]byte, 256<<10)
		stack = stack[:runtime.Stack(stack, true)]
		for _, block := range strings.Split(string(stack), "\n\n") {
			if strings.Contains(block, "syncsetup") || strings.Contains(block, "idleForHandoff") || strings.Contains(block, "liveDrive") {
				t.Log(block)
			}
		}
		proof, proofErr := cellstore.EngineFor(place.Workspace).SavedState(t.Context(), c)
		var changes []string
		if proof != nil {
			changes = proof.Changes
		}
		t.Fatalf("terminal did not release: watchReady=%v dirty=%v err=%v screen=%s", cfg.Seals.(interface{ IdleReady() bool }).IdleReady(), changes, proofErr, screen.String())
		return directory.CellView{}
	}
	first := released(0)
	if strings.TrimSpace(agent.Title()) == "" {
		t.Fatal("terminal chat has no title after the model turn")
	}
	t.Logf("model and ! turns released fence %d", first.Cell.Lease.Fence)
	send("D1 second question", "terminal second answer")
	second := released(first.Cell.Lease.Fence)
	if noRestore.calls.Load() != 0 {
		t.Fatal("D1 send used restore")
	}
	ignored := filepath.Join(place.Workspace, "node_modules", "x", "index.js")
	if raw, e := os.ReadFile(ignored); e != nil || string(raw) != "local-install" {
		t.Fatalf("D1 install=%q err=%v", raw, e)
	}
	for _, line := range []string{"Moved here", "Set this machine up like", "Continue this chat here?", "submit failed: This chat is available to continue here."} {
		if strings.Contains(screen.String(), line) {
			t.Fatalf("D1 showed %q", line)
		}
	}
	if !settings {
		return
	}
	if _, err := io.WriteString(typed, "/effort high\r/remember terminal kept memory\r"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		m, _ := session.LoadMeta(place.Dir)
		if m.Effort == "high" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	third := released(second.Cell.Lease.Fence)
	got, err := h.b.Continuer(h.eb, syncsetup.TakeOptions{DeviceName: "receiver", RootFor: func(id string) string { return filepath.Join(h.bRoots, id) }}).OpenAvailable(t.Context(), id, &directory.LeaseExpectation{Device: third.Cell.Lease.Device, Fence: third.Cell.Lease.Fence})
	if err != nil {
		t.Fatal(err)
	}
	m, err := session.LoadMeta(got.Taken.Cell.Root)
	if err != nil || m.Effort != "high" {
		t.Fatalf("C5 received effort=%q err=%v", m.Effort, err)
	}
	raw, err := os.ReadFile(filepath.Join(got.Taken.Cell.Root, cell.StateDir, "memories.jsonl"))
	if err != nil || !strings.Contains(string(raw), "terminal kept memory") {
		t.Fatalf("C5 received memory=%q err=%v", raw, err)
	}
}

func TestIdleRelease_R6_RealTerminalOpeningSavedChatReleases(t *testing.T) {
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	service := relayserve.New(relayserve.Config{Store: t.TempDir()})
	t.Cleanup(func() { _ = service.Close() })
	h := newHostedMoveRig(t, func(http.Handler) http.Handler { return service.Handler })
	t.Setenv("CODEAF_ENGINE_DAEMON", "1")
	t.Setenv("CODEAF_PROFILE_DIR", h.a.Home)
	if err := h.b.Rename(t.Context(), "peer"); err != nil {
		t.Fatal(err)
	}
	// Seed the terminal's real store, rather than the rig's private native store.
	// Otherwise reopening cannot find the already-published snapshot locally.
	engine := cellstore.EngineFor(h.work)
	source := h.publish(h.a, engine, h.c, "source")
	if err := source.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := h.a.Dir.Cell(t.Context(), h.c.ID)
	if err != nil {
		t.Fatal(err)
	}
	place := session.Place{Dir: h.c.Root, Workspace: h.work}
	agent, cfg, _, err := openV3Agent(session.Config{Workspace: h.work, Place: place, SessionFile: place.Transcript(), ProfileDir: h.a.Home, Model: "stub/chat", APIKey: "stub", BaseURL: "http://127.0.0.1:1", System: "SYSTEM"}, h.work, v3OpenSession)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { agent.Close(); syncDrives.closeAll() }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		v, err := h.a.Dir.Cell(t.Context(), h.c.ID)
		if err == nil && v.Cell.Lease.Fence > before.Cell.Lease.Fence && v.Cell.Lease.Expires == 0 {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	proof, proofErr := engine.SavedState(t.Context(), h.c)
	t.Fatalf("opening a saved terminal chat never released: ready=%v proof=%+v error=%v", cfg.Seals.(interface{ IdleReady() bool }).IdleReady(), proof, proofErr)
}
