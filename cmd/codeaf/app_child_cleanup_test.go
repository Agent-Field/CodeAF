package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
)

type appChildDaemon struct {
	pid    int
	socket string
}

// appChildDaemons discovers the child's native helpers even when a long state
// path puts their sockets outside the state folder. Match the exact state bin
// directory, not just the engine name: other tests can run beside this one.
func appChildDaemons(t *testing.T, state string) []appChildDaemon {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, "ps", "-ww", "-u", strconv.Itoa(os.Getuid()), "-o", "pid=,stat=,args=").Output()
	if err != nil {
		t.Fatalf("list app-child daemons: %v", err)
	}
	var found []appChildDaemon
	for line := range strings.SplitSeq(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || strings.HasPrefix(fields[1], "Z") {
			continue
		}
		command := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), fields[0]))
		command = strings.TrimSpace(strings.TrimPrefix(command, fields[1]))
		binary, args, ok := strings.Cut(command, " serve --socket ")
		if !ok || filepath.Dir(binary) != filepath.Join(state, "bin") || !strings.HasPrefix(filepath.Base(binary), "furrow-") {
			continue
		}
		socket, _, ok := strings.Cut(args, " --idle-minutes ")
		pid, err := strconv.Atoi(fields[0])
		if !ok || socket == "" || err != nil {
			t.Fatalf("invalid app-child daemon command for state %s", state)
		}
		found = append(found, appChildDaemon{pid: pid, socket: socket})
	}
	return found
}

// cleanupAppChildDaemons must be registered after TempDir creation and before
// the app's own cleanup, so LIFO cleanup joins the app, then its detached
// writers, then removes their state. Stop cannot join a child-owned process
// through the parent's process-local registry, so wait for its exit as well.
func cleanupAppChildDaemons(t *testing.T, state string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, daemon := range appChildDaemons(t, state) {
			if err := (cellstore.Daemon{Socket: daemon.socket}).Stop(ctx); err != nil {
				t.Errorf("stop app-child daemon %d for %s: %v", daemon.pid, state, err)
			}
		}
		for {
			daemons := appChildDaemons(t, state)
			if len(daemons) == 0 {
				return
			}
			select {
			case <-ctx.Done():
				t.Errorf("app-child daemons still writing %s after shutdown: %+v", state, daemons)
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	})
}

func TestAppChildCleanupJoinsDetachedEngine(t *testing.T) {
	celltest.RequireEngine(t)
	root := e2TempDir(t)
	state := filepath.Join(root, "state")
	t.Run("app exits before state removal", func(t *testing.T) {
		_, workspace := wakeEnvironment(t, root)
		model := appTwoMessageSource(t, "echo cleanup-proof")
		t.Cleanup(model.Close)
		t.Setenv("CODEAF_BASE_URL", model.URL)
		app := e2StartApp(t, state)
		created := machineCall[struct{ Agent appwire.Agent }](t, app.w, "agents.create", map[string]any{"workspace": workspace}).Agent
		e2Turn(t, app, created.ID, "save before closing")
		outageWait(t, "app starts a native engine daemon", func() bool {
			return len(appChildDaemons(t, state)) > 0
		})
	})
	if daemons := appChildDaemons(t, state); len(daemons) != 0 {
		// Release the red probe's daemon too; its survival is the assertion,
		// not a reason to leave a writer behind after this test fails.
		for _, daemon := range daemons {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = (cellstore.Daemon{Socket: daemon.socket}).Stop(ctx)
			cancel()
		}
		t.Fatalf("app-child cleanup left native engine daemons serving %s: %+v", state, daemons)
	}
	stateFiles := func() []string {
		t.Helper()
		var files []string
		err := filepath.WalkDir(filepath.Join(state, "v3"), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			files = append(files, fmt.Sprintf("%s %d %v", path, info.Size(), info.ModTime()))
			return nil
		})
		if err != nil {
			t.Fatalf("inspect state before TempDir removal: %v", err)
		}
		return files
	}
	before := stateFiles()
	time.Sleep(100 * time.Millisecond)
	if !slices.Equal(before, stateFiles()) {
		t.Fatal("state/v3 changed after app-child cleanup returned")
	}
}
