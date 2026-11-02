package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/preflight"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

type copiedPingPong struct {
	*hostedMoveRig
	copyRoot    string
	initialRoot string
	door        string
	replies     []string
}

type copiedTakeResult struct {
	taken tui3.Taken
	err   error
}

type copiedSurfaceTaker struct {
	inner  takeover
	result chan copiedTakeResult
}

func (c copiedSurfaceTaker) Take(ctx context.Context, id string) (tui3.Taken, error) {
	return c.TakeSeen(ctx, id, nil)
}

func (c copiedSurfaceTaker) TakeSeen(ctx context.Context, id string, seen *directory.LeaseExpectation) (tui3.Taken, error) {
	got, err := c.inner.TakeSeen(ctx, id, seen)
	c.result <- copiedTakeResult{got, err}
	return got, err
}

func (c copiedSurfaceTaker) SafeToContinue(ctx context.Context, id, current string, automatic bool) (bool, error) {
	if automatic {
		return false, nil
	}
	return c.inner.SafeToContinue(ctx, id, current, false)
}

func (h *copiedPingPong) surfaceTake(s *syncsetup.Sync, taker takeover, root, door, from string) (cell.Cell, error) {
	h.t.Helper()
	t := h.t
	input, typed := io.Pipe()
	screen := &surfaceFrameWriter{ready: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	result := make(chan copiedTakeResult, 1)
	file := session.Place{Dir: root}.Transcript()
	go func() {
		done <- tui3.Run(ctx, tui3.Options{Agent: &quietAgent{}, Workspace: filepath.Join(root, "work"), SessionFile: file,
			Landing: door == "home", Machines: surfaceMachines{s}, DeviceID: s.Device.ID(),
			Takeover: copiedSurfaceTaker{taker, result}, Input: input, Output: screen, Width: 120, Height: 35,
			Open: func(where, file string) (tui3.Conversation, error) {
				return tui3.Conversation{Agent: &quietAgent{}, Workspace: where, SessionFile: file}, nil
			}})
	}()
	defer func() { cancel(); input.Close(); typed.Close(); <-done }()
	<-screen.ready
	if door == "home" {
		h.wait("Home continuation offer", func() bool {
			return strings.Contains(screen.String(), "Continue "+from+"'s latest chat here?")
		})
		_, _ = io.WriteString(typed, "\x1bc")
	} else {
		_, _ = io.WriteString(typed, "continue this copied chat\r")
		h.wait("copied chat card", func() bool { return strings.Contains(screen.String(), "Continue this chat here?") })
		_, _ = io.WriteString(typed, "1\r")
	}
	var got copiedTakeResult
	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	confirmed := door != "home"
waiting:
	for {
		select {
		case got = <-result:
			break waiting
		case <-tick.C:
			if !confirmed && strings.Contains(screen.String(), "Continue this chat here?") {
				confirmed = true
				_, _ = io.WriteString(typed, "1\r")
			}
		case <-deadline.C:
			t.Fatalf("take did not finish: %s", screen.String())
		}
	}
	if got.err != nil {
		return cell.Cell{}, got.err
	}
	h.wait("Moved from "+from, func() bool { return strings.Contains(screen.String(), "Moved from "+from) })
	return cell.OpenAt(session.DirOf(got.taken.Transcript), h.c.ID)
}

func newCopiedPingPong(t *testing.T) *copiedPingPong {
	t.Helper()
	h := newHostedMoveRig(t)
	h.ea.DataRoot = filepath.Join(h.a.Home, "v3", "stores")
	h.eb.DataRoot = filepath.Join(h.b.Home, "v3", "stores")
	machine, err := preflight.OpenMachine(h.c.Root, h.work)
	if err != nil {
		t.Fatal(err)
	}
	machine.Observer().Refresh()
	return &copiedPingPong{hostedMoveRig: h, initialRoot: filepath.Join(h.b.Home, "v3", "projects", "copies", h.c.ID)}
}

func (h *copiedPingPong) trace(p handoff.Phase) {
	if !p.Ended.IsZero() {
		h.t.Logf("%s: %s", p.Name, p.Ended.Sub(p.Started))
	}
}

func (h *copiedPingPong) publishTurn(s *syncsetup.Sync, e cellstore.Engine, c cell.Cell, work, name, text string) {
	h.t.Helper()
	t := h.t
	t.Setenv(home.EnvVar, s.Home)
	e.Workspace = work
	if err := os.WriteFile(filepath.Join(work, "latest.txt"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	journal, err := os.OpenFile(session.Place{Dir: c.Root}.Transcript(), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := fmt.Fprintf(journal, "{\"type\":\"message\",\"role\":\"assistant\",\"content\":%q}\n", text)
	closeErr := journal.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("append reply: %v %v", writeErr, closeErr)
	}
	d := h.publish(s, e, c, name)
	if err := d.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.replies = append(h.replies, text)
}

func (h *copiedPingPong) takeB() (cell.Cell, error) {
	h.t.Helper()
	h.t.Setenv(home.EnvVar, h.b.Home)
	if err := os.Rename(h.work, h.work+".away"); err != nil {
		h.t.Fatal(err)
	}
	defer func() {
		if err := os.Rename(h.work+".away", h.work); err != nil {
			h.t.Fatal(err)
		}
	}()
	rootFor := func(string) string {
		if h.copyRoot != "" {
			return h.copyRoot
		}
		return h.initialRoot
	}
	c := h.b.Continuer(h.eb, syncsetup.TakeOptions{DeviceName: "computer B", RootFor: rootFor, OnPhase: h.trace,
		BucketFor: func(string) string { return filepath.Join(h.b.Home, "v3", "projects", "copy--project") }})
	door := h.door
	if h.copyRoot == "" {
		door = "home"
	}
	got, err := h.surfaceTake(h.b, takeover{c}, rootFor(h.c.ID), door, "computer A")
	if err != nil {
		return cell.Cell{}, err
	}
	if got.Root == h.initialRoot {
		h.t.Fatal("the first arrival was not filed into its copy bucket")
	}
	h.copyRoot = got.Root
	return got, nil
}

func (h *copiedPingPong) takeA() cell.Cell {
	h.t.Helper()
	h.t.Setenv(home.EnvVar, h.a.Home)
	c := h.a.Continuer(h.ea, syncsetup.TakeOptions{DeviceName: "computer A", RootFor: func(string) string { return h.c.Root }, OnPhase: h.trace})
	got, err := h.surfaceTake(h.a, takeover{c}, h.c.Root, "home", "computer B")
	if err != nil {
		h.t.Fatal(err)
	}
	return got
}

func (h *copiedPingPong) content(c cell.Cell, work, text string) {
	h.t.Helper()
	if body, err := os.ReadFile(filepath.Join(work, "latest.txt")); err != nil || string(body) != text {
		h.t.Fatalf("latest file=%q: %v; want %q", body, err, text)
	}
	body, err := os.ReadFile(session.Place{Dir: c.Root}.Transcript())
	if err != nil {
		h.t.Fatal(err)
	}
	for _, reply := range h.replies {
		if !strings.Contains(string(body), reply) {
			h.t.Fatalf("copied chat lost reply %q", reply)
		}
	}
	h.liveBinding()
}

func (h *copiedPingPong) liveBinding() {
	h.t.Helper()
	for data, work := range map[string]string{h.ea.DataRoot: h.work, h.eb.DataRoot: filepath.Join(h.copyRoot, "work")} {
		want, err := filepath.EvalSymlinks(work)
		if err != nil {
			h.t.Fatal(err)
		}
		bindings, err := filepath.Glob(filepath.Join(data, "shared", "store-v1", "workspaces", "*", "root.path"))
		if err != nil {
			h.t.Fatal(err)
		}
		live := 0
		for _, binding := range bindings {
			if _, err := os.Stat(filepath.Join(filepath.Dir(binding), "detached")); err == nil {
				continue
			} else if !os.IsNotExist(err) {
				h.t.Fatal(err)
			}
			root, err := os.ReadFile(binding)
			if err != nil || string(root) != want {
				h.t.Fatalf("live binding=%q: %v; want %s", root, err, want)
			}
			live++
		}
		if live != 1 {
			h.t.Fatalf("live bindings for %s=%d; want 1", want, live)
		}
	}
}

// Contract H1, H2 and H6 keep the latest files and replies through three real copied takes.
func TestCopiedChatPingPongsThroughThreeTakes(t *testing.T) {
	for _, door := range []string{"home", "card"} {
		t.Run(door, func(t *testing.T) {
			h := newCopiedPingPong(t)
			h.door = door
			for round := 1; round <= 3; round++ {
				fromA := fmt.Sprintf("reply A %d", round)
				h.publishTurn(h.a, h.ea, h.c, h.work, "computer A", fromA)
				b, err := h.takeB()
				if err != nil {
					t.Fatalf("B take %d: %v", round, err)
				}
				h.content(b, filepath.Join(b.Root, "work"), fromA)
				if round == 3 {
					break
				}
				fromB := fmt.Sprintf("reply B %d", round)
				h.publishTurn(h.b, h.eb, b, filepath.Join(b.Root, "work"), "computer B", fromB)
				a := h.takeA()
				h.content(a, h.work, fromB)
			}
		})
	}
}

// Contract H5 and H6 repair a filed copy beside a detached admission timeline from its first take.
func TestCopiedChatTakeRepairsDetachedAdmissionBinding(t *testing.T) {
	h := newCopiedPingPong(t)
	h.door = "home"
	h.publishTurn(h.a, h.ea, h.c, h.work, "computer A", "first arrival")
	b, err := h.takeB()
	if err != nil {
		t.Fatal(err)
	}
	stage := h.initialRoot + ".taking"
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stage, 0700); err != nil {
		t.Fatal(err)
	}
	stage, err = filepath.EvalSymlinks(stage)
	if err != nil {
		t.Fatal(err)
	}
	for _, verb := range [][]string{{"watch", "--no-daemon"}, {"forget"}} {
		args := append([]string{"--json"}, verb...)
		args = append(args, "--timeline", "incoming-admission-"+h.c.ID)
		cmd := exec.CommandContext(t.Context(), bin, args...)
		cmd.Dir = stage
		cmd.Env = append(os.Environ(), "FURROW_DATA_DIR="+filepath.Join(h.eb.DataRoot, "shared"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("seed detached binding: %s: %v", out, err)
		}
	}
	if err := os.RemoveAll(stage); err != nil {
		t.Fatal(err)
	}
	bindings, err := filepath.Glob(filepath.Join(h.eb.DataRoot, "shared", "store-v1", "workspaces", "*", "root.path"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, binding := range bindings {
		root, err := os.ReadFile(binding)
		if err != nil {
			t.Fatal(err)
		}
		if string(root) == stage {
			if _, err := os.Stat(filepath.Join(filepath.Dir(binding), "detached")); err != nil {
				t.Fatalf("old admission binding is not detached: %v", err)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("broken home lacks its detached admission binding")
	}
	h.liveBinding()
	h.publishTurn(h.b, h.eb, b, filepath.Join(b.Root, "work"), "computer B", "return from B")
	h.takeA()
	h.publishTurn(h.a, h.ea, h.c, h.work, "computer A", "latest after repair")
	b, err = h.takeB()
	if err != nil {
		t.Fatalf("take from broken home: %v", err)
	}
	h.content(b, filepath.Join(b.Root, "work"), "latest after repair")
}
