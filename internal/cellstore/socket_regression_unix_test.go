//go:build !windows

package cellstore

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// F5/C7: A preclaimed short directory must not let an impersonator receive work.
// The lane-local temp base models a shared /tmp without involving another user.
func TestR9UnixShortSocketRejectsPreclaimedDirectory(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	for _, kind := range []string{"writable-directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			bin := "/engine/furrow-review-" + kind
			t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), strings.Repeat("home", 30)))
			socket := socketFor(bin)
			dir := filepath.Dir(socket)
			// Selection now creates the private base; replace it to model preclaiming.
			if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if !strings.HasPrefix(dir, os.TempDir()+string(filepath.Separator)) || len(socket) >= maxSocketBytes {
				t.Fatalf("C7: fixture did not select its lane-local short fallback: %q", socket)
			}
			if _, err := os.Lstat(dir); !os.IsNotExist(err) {
				t.Fatalf("C7: fixture directory already exists: %v", err)
			}
			if kind == "symlink" {
				if err := os.Symlink(t.TempDir(), dir); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(dir, 0777); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(dir, 0777); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			requests := make(chan wireRequest, 2)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				reader := bufio.NewReader(conn)
				for range 2 {
					line, err := reader.ReadBytes('\n')
					var req wireRequest
					if err != nil || json.Unmarshal(line, &req) != nil {
						return
					}
					requests <- req
					ok, _ := json.Marshal(map[string]any{"engine": filepath.Base(bin), "impersonated": true})
					reply, _ := json.Marshal(wireResponse{ID: req.ID, Ok: ok})
					_, _ = conn.Write(append(reply, '\n'))
				}
			}()
			out, err := (Daemon{Socket: socketFor(bin), Binary: bin}).Do(t.Context(), Target{Tree: t.TempDir()}, snapOp{})
			if err == nil && strings.Contains(string(out), `"impersonated":true`) {
				t.Errorf("C7: daemon accepted an impersonated engine in %s and forwarded the operation: %s", kind, out)
			}
			if _, err := (Daemon{Socket: socket, Binary: bin}).Do(t.Context(), Target{Tree: t.TempDir()}, snapOp{}); err == nil {
				t.Error("F5: a previously selected endpoint bypassed directory validation")
			}
			select {
			case req := <-requests:
				t.Errorf("C7: unsafe endpoint received %q before directory ownership/permission validation", req.Verb)
			default:
			}
		})
	}
}

// TestR9UnixShortSocketRejectsSocketSymlink proves F5 before any dial or prune.
func TestR9UnixShortSocketRejectsSocketSymlink(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), strings.Repeat("home", 30)))
	bin := "/engine/furrow-review-socket-link"
	socket := socketFor(bin)
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(socket)) })
	target := testSocket(t)
	listener, err := net.Listen("unix", target)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan bool, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			conn.Close()
		}
		accepted <- err == nil
	}()
	if err := os.Symlink(target, socket); err != nil {
		t.Fatal(err)
	}
	other := socketFor(bin)
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(other)) })
	if other == "" || other == socket || len(other) >= maxSocketBytes || other != socketFor(bin) {
		t.Fatalf("F5: socket symlink did not select another stable bounded address: %q", other)
	}
	stale := filepath.Join(filepath.Dir(socket), "e-stale.sock")
	if err := os.WriteFile(stale, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := (Daemon{Socket: socket, Binary: bin}).Do(ctx, Target{}, snapOp{}); err == nil {
		t.Error("F5: socket symlink was accepted")
	}
	listener.Close()
	if <-accepted {
		t.Error("F5: socket symlink received a connection")
	}
	if _, err := os.Lstat(stale); err != nil {
		t.Errorf("F5: an unsafe endpoint triggered stale pruning: %v", err)
	}
	if _, err := os.Lstat(stale + ".lock"); !os.IsNotExist(err) {
		t.Errorf("F5: an unsafe endpoint opened a pruning lock: %v", err)
	}
}

// TestR9UnixRuntimeSocketPreference proves F5 without starting an engine.
func TestR9UnixRuntimeSocketPreference(t *testing.T) {
	runtime, err := os.MkdirTemp("/tmp", "r9-runtime-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	login, err := os.MkdirTemp("/tmp", "r9-home-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(login) })
	t.Setenv("CODEAF_HOME", login)
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	bin := "/engine/furrow-review-runtime"
	socket := socketFor(bin)
	if !strings.HasPrefix(socket, runtime+string(filepath.Separator)) || socket != socketFor(bin) || len(socket) >= maxSocketBytes {
		t.Fatalf("F5: private runtime directory was not preferred: %q", socket)
	}
	for _, path := range []string{runtime, filepath.Dir(socket)} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0700 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
			t.Fatalf("F5: selected base is not private and owned: %s", info.Mode())
		}
	}
	alias := filepath.Join(t.TempDir(), "runtime-link")
	if err := os.Symlink(runtime, alias); err != nil {
		t.Fatal(err)
	}
	for _, unusable := range []string{"relative", alias, filepath.Join(runtime, "missing"), filepath.Join(runtime, strings.Repeat("long", 30))} {
		t.Setenv("XDG_RUNTIME_DIR", unusable)
		if got := socketFor(bin); got != filepath.Join(login, "v3", "engine-review-runtime.sock") {
			t.Fatalf("F5: unusable runtime directory displaced the fitting home: %q", got)
		}
	}
	if err := os.Chmod(runtime, 0777); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	if got := socketFor(bin); got != filepath.Join(login, "v3", "engine-review-runtime.sock") {
		t.Fatalf("F5: writable runtime directory was accepted: %q", got)
	}
}

// F5/C1/C4/C7: A project below home still rewinds through a real, private fallback.
func TestR9ProjectChildRewindsThroughPrivateSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	login := t.TempDir()
	project := filepath.Join(login, "project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", login)
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), strings.Repeat("state", 25)))
	bin, err := sealingEngine()
	if err != nil {
		t.Fatal(err)
	}
	d := Daemon{Socket: socketFor(bin), Binary: bin}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := d.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	e := realEngine(t)
	e.Workspace, e.Transport = project, d
	c := newCell(t)
	file := filepath.Join(project, "example.txt")
	var first Sealed
	for i, value := range []string{"before\n", "after\n"} {
		if err := os.WriteFile(file, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		sealed, err := e.Seal(ctx, c, TurnInfo{})
		if err != nil {
			t.Fatal(err)
		}
		if err := durable(ctx, c.Root); err != nil {
			t.Fatalf("C1: project seal did not reach durable storage: %v", err)
		}
		if i == 0 {
			first = sealed
		}
	}
	if _, err := e.Rewind(ctx, c, first.Turn.ID); err != nil {
		t.Fatalf("C1: project below home lost rewind: %v", err)
	}
	if err := durable(ctx, c.Root); err != nil {
		t.Fatalf("C1: project rewind did not reach durable storage: %v", err)
	}
	if raw, err := os.ReadFile(file); err != nil || string(raw) != "before\n" {
		t.Fatalf("C1: rewind did not restore project file: %q %v", raw, err)
	}
	for path, mode := range map[string]os.FileMode{filepath.Dir(d.Socket): 0700, d.Socket: 0600} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != mode || stat.Uid != uint32(os.Getuid()) {
			t.Errorf("C7: managed endpoint is not owner-only: %s mode=%s uid=%d", path, info.Mode(), stat.Uid)
		}
	}
}
