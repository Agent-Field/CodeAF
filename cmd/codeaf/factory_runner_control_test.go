//go:build !windows

package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestFactoryRunnerRuntimeOverrideSharesPrivateSocket(t *testing.T) {
	// The socket must fit macOS's path limit regardless of the inherited TMPDIR.
	base, err := os.MkdirTemp("/tmp", "rt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	t.Setenv("CODEAF_FACTORY_RUNTIME_DIR", base)
	floor, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner := &appFactoryHost{ctx: context.Background(), floor: floor, credentials: newFactorySessions()}
	closeControl := owner.runnerControl()
	defer closeControl()
	path, err := factoryRunnerSocket(floor.Root())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, base+string(filepath.Separator)) {
		t.Fatalf("control socket %q escaped configured runtime base %q", path, base)
	}
	for name, mode := range map[string]os.FileMode{filepath.Dir(path): 0700, path: 0600} {
		info, err := os.Lstat(name)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Errorf("mode=%o, want %o", info.Mode().Perm(), mode)
		}
	}
	source := newFactorySessions()
	source.SetFactorySession("https://team.example", "fixture-memory-session")
	peer := &appFactoryHost{ctx: context.Background(), floor: floor, credentials: source}
	peer.shareRunnerSessions()
	if owner.credentials.FactorySession("https://team.example") != "fixture-memory-session" {
		t.Fatal("same-profile peer could not reach the isolated runner")
	}
	same, err := factoryRunnerSocket(filepath.Join(floor.Root(), "."))
	if err != nil || same != path {
		t.Fatalf("same profile changed socket: %q %v", same, err)
	}
	other, err := factoryRunnerSocket(floor.Root() + "-other")
	if err != nil || other == path {
		t.Fatalf("different profiles collided: %q %v", other, err)
	}
}

func TestFactoryRunnerRuntimeLongPathFallsBackWithReason(t *testing.T) {
	t.Setenv("CODEAF_FACTORY_RUNTIME_DIR", "")
	root := t.TempDir()
	legacy, err := factoryRunnerSocket(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(legacy, factoryRunnerRuntimeDefault+string(filepath.Separator)) {
		t.Fatalf("default socket %q escaped injected runtime base %q", legacy, factoryRunnerRuntimeDefault)
	}
	long := filepath.Join(t.TempDir(), strings.Repeat("long", 30))
	t.Setenv("TMPDIR", long)
	same, err := factoryRunnerSocket(root)
	if err != nil || same != legacy {
		t.Fatalf("long TMPDIR broke stable default: %q %v", same, err)
	}
	t.Setenv("CODEAF_FACTORY_RUNTIME_DIR", long)
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stderr
	os.Stderr = write
	fallback, err := factoryRunnerSocket(root)
	os.Stderr = previous
	_ = write.Close()
	reason, readErr := io.ReadAll(read)
	_ = read.Close()
	if err != nil || readErr != nil || fallback != legacy || len(fallback) >= 100 {
		t.Fatalf("long runtime fallback: %q %v %v", fallback, err, readErr)
	}
	if !strings.Contains(string(reason), "Unix socket") || !strings.Contains(string(reason), "using the default") {
		t.Fatalf("fallback did not explain the path limit: %q", reason)
	}
}

func TestFactoryRunnerRuntimeRejectsPrivateDirectorySymlink(t *testing.T) {
	// A long base would select fallback before exercising the symlink refusal.
	base, err := os.MkdirTemp("/tmp", "rt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	t.Setenv("CODEAF_FACTORY_RUNTIME_DIR", base)
	dir := filepath.Join(base, "codeaf-runners-"+strconv.Itoa(os.Getuid()))
	if err := os.Symlink(t.TempDir(), dir); err != nil {
		t.Fatal(err)
	}
	if path, err := factoryRunnerSocket(t.TempDir()); err == nil {
		t.Fatalf("symlink runtime directory accepted: %s", path)
	}
}
