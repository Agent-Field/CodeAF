package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
)

// The command's real signal and wire doors are exercised in separate processes.
// Only final disk saving is replaced, by a disk that never answers its caller.
func TestAppOneEngine(t *testing.T) {
	if mode := os.Getenv("CODEAF_TEST_ONE_ENGINE"); mode != "" {
		switch mode {
		case "stuck":
			flushFinalSaves = func(context.Context) error {
				_ = os.WriteFile(home.Join("saving"), nil, 0600)
				select {}
			}
		case "impatient":
			appHomeWait = 300 * time.Millisecond
		}
		root := os.Getenv("CODEAF_TEST_ONE_HOME")
		t.Setenv(home.EnvVar, root)
		os.Args = []string{os.Args[0], "app", "--home", root}
		if mode == "schema" {
			os.Args = append(os.Args, "--schema")
		}
		if mode == "version" {
			os.Args = []string{os.Args[0], "version"}
		}
		main()
		os.Exit(0)
	}
	closing := func(t *testing.T, root string) *oneEngineProcess {
		t.Helper()
		first := startOneEngine(t, root, "stuck")
		first.hello(t)
		_ = first.cmd.Process.Signal(syscall.SIGTERM)
		waitOneEngineFile(t, filepath.Join(root, "saving"))
		return first
	}
	t.Run("the second engine waits and starts when the first ends", func(t *testing.T) {
		root := t.TempDir()
		first := closing(t, root)
		second := startOneEngine(t, root, "ordinary")
		select {
		case line := <-second.lines:
			t.Fatalf("second engine admitted while the first lives: %s", line)
		case <-second.done:
			t.Fatalf("second engine exited instead of waiting: %s", second.stderr.String())
		case <-time.After(2 * time.Second):
		}
		// A crash releases ownership without a stale-pid recovery or a deleted lock.
		first.kill(t)
		second.hello(t)
		second.leave(t)
	})
	t.Run("a crash leaves nothing to wait for", func(t *testing.T) {
		root := t.TempDir()
		first := startOneEngine(t, root, "ordinary")
		first.hello(t)
		first.kill(t)
		began := time.Now()
		next := startOneEngine(t, root, "ordinary")
		next.hello(t)
		next.leave(t)
		if waited := time.Since(began); waited > 8*time.Second {
			t.Fatalf("relaunch after a crash took %s", waited)
		}
	})
	t.Run("the wait ends with a clear message and starts nothing", func(t *testing.T) {
		root := t.TempDir()
		closing(t, root)
		second := startOneEngine(t, root, "impatient")
		select {
		case line := <-second.lines:
			t.Fatalf("engine spoke after giving up: %s", line)
		case <-second.done:
			if !strings.Contains(second.stderr.String(), "another codeaf app is still closing this home") {
				t.Fatalf("missing message: %s", second.stderr.String())
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the wait had no bound")
		}
	})
	t.Run("only one home's app is held", func(t *testing.T) {
		root := t.TempDir()
		closing(t, root)
		other := startOneEngine(t, t.TempDir(), "ordinary")
		other.hello(t)
		other.leave(t)
		for _, mode := range []string{"schema", "version"} {
			p := startOneEngine(t, root, mode)
			select {
			case <-p.done:
			case <-time.After(10 * time.Second):
				t.Fatalf("%s waited on the app's lock", mode)
			}
		}
	})
}

type oneEngineProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan string
	done   chan struct{}
	stderr bytes.Buffer
}

func startOneEngine(t *testing.T, root, mode string) *oneEngineProcess {
	t.Helper()
	p := &oneEngineProcess{lines: make(chan string, 4096), done: make(chan struct{})}
	p.cmd = exec.Command(os.Args[0], "-test.run=^TestAppOneEngine$")
	p.cmd.Env = append(os.Environ(), "CODEAF_TEST_ONE_ENGINE="+mode, home.EnvVar+"="+filepath.Join(root, "inherited"), "CODEAF_TEST_ONE_HOME="+root)
	var err error
	p.stdin, err = p.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := p.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	p.cmd.Stderr = &p.stderr
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	scanned := make(chan struct{})
	go func() {
		defer close(scanned)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 16<<20)
		for scanner.Scan() {
			select {
			case p.lines <- scanner.Text():
			default:
			}
		}
	}()
	go func() { <-scanned; _ = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() { p.kill(t); _ = p.stdin.Close() })
	return p
}

func (p *oneEngineProcess) hello(t *testing.T) {
	t.Helper()
	select {
	case line := <-p.lines:
		if !strings.Contains(line, `"event":"hello"`) {
			t.Fatalf("not hello: %s", line)
		}
	case <-p.done:
		t.Fatalf("engine ended before hello: %s", p.stderr.String())
	case <-time.After(10 * time.Second):
		t.Fatal("engine did not say hello")
	}
}

func (p *oneEngineProcess) leave(t *testing.T) {
	t.Helper()
	_ = p.stdin.Close()
	select {
	case <-p.done:
	case <-time.After(10 * time.Second):
		t.Fatal("engine did not close")
	}
}

func (p *oneEngineProcess) kill(t *testing.T) {
	t.Helper()
	select {
	case <-p.done:
		return
	default:
	}
	// Check the exact child before killing it; never select processes by name.
	if raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", p.cmd.Process.Pid)); err == nil && !bytes.Contains(raw, []byte("TestAppOneEngine")) {
		t.Fatalf("unexpected child command: %q", raw)
	}
	_ = p.cmd.Process.Kill()
	<-p.done
}

func waitOneEngineFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("signal did not reach final saving")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Stands in for the lock until it exists, so the first run shows the behaviour.
var appHomeWait time.Duration
