//go:build e2e && !windows

package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/furrowbin"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The app and storage daemon are real. A transparent socket proxy holds only
// capture-plan requests until the test releases them. Readiness requires saved
// conversation text and a composer, not terminal mode bytes or a process spawn.
func TestStartupWhileStorageCaptureIsPending(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux required")
	}
	engine, err := furrowbin.Ensure()
	if err != nil {
		t.Fatal(err)
	}
	for _, hosted := range []bool{false, true} {
		t.Run(fmt.Sprintf("hosted_%t", hosted), func(t *testing.T) {
			home := recoveryShortHome(t, newHome(t, map[string]any{"memory.enabled": false, "model_pool": "off"}))
			ws := newWorkspace(t, "capture-startup", false)
			if err := config.MarkSetupSeen(home, time.Now()); err != nil {
				t.Fatal(err)
			}
			bucket := filepath.Join(home, "v3", "projects", strings.ReplaceAll(ws, string(filepath.Separator), "-"))
			c, err := cell.CreateIn(bucket, cell.Options{Class: cell.HostBound})
			if err != nil {
				t.Fatal(err)
			}
			dir := c.Root
			file := filepath.Join(dir, cell.TranscriptPath)
			journal := fmt.Sprintf("{\"type\":\"session\",\"version\":1,\"id\":%q,\"cwd\":%q,\"model\":\"deepseek/deepseek-v4.1-flash\"}\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"CAPTURE_STARTUP_HISTORY\"}\n", c.ID, ws)
			if err := os.WriteFile(file, []byte(journal), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := session.SaveMeta(dir, session.Meta{ID: filepath.Base(dir), Title: "Capture startup", Workspace: ws}); err != nil {
				t.Fatal(err)
			}
			entered, release := delayedCaptureProxy(t, home, engine)
			var released sync.Once
			unblock := func() { released.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			url, calls := efficiencyWire(t)
			env := []string{config.APIKeyEnv + "=mock-efficiency-key", "CODEAF_BASE_URL=" + url, furrowbin.BinaryEnvVar + "=" + engine}
			stopRecoveryEngineOnCleanup(t, env, home, ws)
			name := fmt.Sprintf("capture_startup_%t", hosted)
			target := fmt.Sprintf("p%d-%s", os.Getpid(), name)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			ready := make(chan time.Time, 1)
			go func() {
				for ctx.Err() == nil {
					out, _ := exec.CommandContext(ctx, "tmux", "capture-pane", "-p", "-t", target).Output()
					s := string(out)
					if strings.Contains(s, "CAPTURE_STARTUP_HISTORY") && strings.Contains(s, "/ commands") {
						ready <- time.Now()
						return
					}
					time.Sleep(5 * time.Millisecond)
				}
			}()
			args := []string{"chat", "--session", file, "--model", "deepseek/deepseek-v4.1-flash", "--one-model"}
			if !hosted {
				args = append(args, "--no-host")
			}
			began := time.Now()
			launched := make(chan *rig, 1)
			go func() { launched <- startWithEnv(t, env, name, home, ws, 140, 42, args...) }()
			var launchedRig *rig
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("fixture never reached an actual capture-plan request")
			}
			select {
			case shown := <-ready:
				t.Logf("launch_to_history_and_composer_ms=%.3f capture_still_held=true", float64(shown.Sub(began))/float64(time.Millisecond))
				var r *rig
				select {
				case r = <-launched:
					launchedRig = r
				case <-ctx.Done():
					t.Fatal("launcher did not observe the visible composer")
				}
				typed := time.Now()
				if out, err := exec.Command("tmux", "send-keys", "-t", r.name, "-l", "CAPTURE_STARTUP_DRAFT").CombinedOutput(); err != nil {
					t.Fatalf("typing: %v %s", err, out)
				}
				elapsed := efficiencyWait(t, r, typed, "CAPTURE_STARTUP_DRAFT")
				t.Logf("input_to_visible_draft_ms=%.3f capture_still_held=true", float64(elapsed)/float64(time.Millisecond))
			case <-ctx.Done():
				// This is the fixture's deadlock watchdog, not a latency budget.
				// The causal assertion is that the UI progresses before release.
				t.Errorf("fixture watchdog expired with capture still held; no usable frame after %s", time.Since(began))
			}
			unblock()
			if launchedRig == nil {
				select {
				case launchedRig = <-launched:
				case <-time.After(10 * time.Second):
					t.Fatal("launcher did not finish after capture was released")
				}
			}
			for deadline := time.Now().Add(15 * time.Second); ; {
				if head, err := cellstore.Head(c); err == nil && head != nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("storage did not finish saving after its request was released")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if calls.Load() != 0 {
				t.Fatalf("startup made %d model calls", calls.Load())
			}
		})
	}
}

func delayedCaptureProxy(t *testing.T, home, engine string) (<-chan struct{}, chan struct{}) {
	t.Helper()
	base := filepath.Join(home, "v3")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	backend := filepath.Join(home, "backend.sock")
	cmd := guardedCommand(t, context.Background(), home, append(os.Environ(), "CODEAF_HOME="+home), engine, "serve", "--socket", backend, "--idle-minutes", "10")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	for deadline := time.Now().Add(5 * time.Second); ; {
		if c, err := net.Dial("unix", backend); err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native storage daemon did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	front := filepath.Join(base, "engine-"+strings.TrimPrefix(filepath.Base(engine), "furrow-")+".sock")
	ln, err := net.Listen("unix", front)
	if err != nil {
		t.Fatal(err)
	}
	entered, release, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { close(stopped); ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				far, err := net.Dial("unix", backend)
				if err != nil {
					return
				}
				defer far.Close()
				go func() { _, _ = io.Copy(conn, far) }()
				reader := bufio.NewReader(conn)
				for {
					line, err := reader.ReadBytes('\n')
					if err != nil {
						return
					}
					var req struct {
						Verb string `json:"verb"`
					}
					if json.Unmarshal(line, &req) == nil && req.Verb == "capture-plan" {
						once.Do(func() { close(entered) })
						select {
						case <-release:
						case <-stopped:
							return
						}
					}
					if _, err := far.Write(line); err != nil {
						return
					}
				}
			}()
		}
	}()
	return entered, release
}
