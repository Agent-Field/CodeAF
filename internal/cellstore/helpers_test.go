package cellstore

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/shell"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/furrow"
)

func newCell(t *testing.T) cell.Cell {
	t.Helper()
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// realEngine is the Engine over the actual engine binary; the test skips when
// none resolves (set CODEAF_FURROW, or build with the binary embedded).
func realEngine(t *testing.T) Engine { return engineFor(t, t.TempDir()) }

func realEngineB(b *testing.B) Engine { return engineFor(b, b.TempDir()) }

func engineFor(t testing.TB, dataRoot string) Engine {
	t.Helper()
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Skipf("no engine binary: %v", err)
	}
	engine := Engine{Binary: bin, DataRoot: dataRoot, Transport: current.make(t, bin)}
	// Retire this fixture before stopping its daemon, or a background group could
	// restart that writer while Windows removes its now-closed catalog (U4/C28).
	t.Cleanup(func() {
		// Join the background group before retiring its routes. Otherwise an
		// already-copied debt could reopen a fixture after its daemon stopped.
		commits.flushing <- struct{}{}
		defer func() { <-commits.flushing }()
		socket := transportSocket(engine.Transport)
		engineCommits.Lock()
		defer engineCommits.Unlock()
		for root, debt := range engineCommits.cells {
			if debt.target.DataDir == dataRoot || strings.HasPrefix(debt.target.DataDir, dataRoot+string(filepath.Separator)) || (socket != "" && transportSocket(debt.transport) == socket) {
				delete(engineCommits.cells, root)
			}
		}
	})
	return engine
}

// A transportCase is one way the real engine is reached; every real-engine
// test runs once per case, so the Store implementations answer to one suite.
type transportCase struct {
	name string
	make func(t testing.TB, bin string) Transport
}

var transportCases = []transportCase{
	{"spawn", func(t testing.TB, bin string) Transport {
		daemon := daemonFor(t, bin).(Daemon)
		return Spawn{Binary: bin, Socket: daemon.Socket}
	}},
	{"daemon", daemonFor},
}

// current is the case the running subtest is in; realEngine reads it.
var current = transportCases[0]

// forEachTransport runs body against every transport. Tests that use it do
// not run in parallel: they share current.
func forEachTransport(t *testing.T, body func(t *testing.T)) {
	for _, tc := range transportCases {
		t.Run(tc.name, func(t *testing.T) {
			previous := current
			current = tc
			t.Cleanup(func() { current = previous })
			body(t)
		})
	}
}

// daemonFor is a daemon of the test's own, on a socket of the test's own,
// stopped when the test ends.
func daemonFor(t testing.TB, bin string) Transport {
	dir, err := os.MkdirTemp("", "cs")
	if err != nil {
		t.Fatal(err)
	}
	d := Daemon{Socket: filepath.Join(dir, "e.sock"), Binary: bin}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), SealWithin)
		defer cancel()
		if err := d.Stop(ctx); err != nil {
			t.Errorf("stop fixture engine: %v", err)
		}
		os.RemoveAll(dir)
	})
	return d
}

// fakeEngine seals without spawning anything: it invents snapshot ids.
type fakeEngine struct {
	mu    sync.Mutex
	calls [][]string
}

func (f *fakeEngine) engine(t *testing.T) Engine {
	return Engine{Binary: "engine", DataRoot: t.TempDir(), Run: f.run}
}

func (f *fakeEngine) run(_ context.Context, _ string, _ []string, argv ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, argv)
	if strings.Contains(strings.Join(argv, " "), "capture-plan") {
		return []byte(`{"token":"test-plan","full":true,"paths":[]}`), nil
	}
	if strings.Contains(strings.Join(argv, " "), "--json sync") {
		return []byte(`{"durable":true}`), nil
	}
	return json.Marshal(map[string]string{"snapshot": fmt.Sprintf("%064x", len(f.calls))})
}

func (f *fakeEngine) count(verb string) (n int) {
	for _, c := range f.calls {
		if strings.Contains(strings.Join(c, " "), verb) {
			n++
		}
	}
	return n
}

// stubExec is an executor whose result and side effects the test controls.
type stubExec struct {
	mu   sync.Mutex
	runs int
	fn   func(executor.ExecRequest) (executor.ExecResult, error)
}

func (s *stubExec) Exec(_ context.Context, req executor.ExecRequest, _ func(executor.Chunk)) (executor.ExecResult, error) {
	s.mu.Lock()
	s.runs++
	s.mu.Unlock()
	if s.fn == nil {
		return executor.ExecResult{SideEffect: executor.Classify(req.Net)}, nil
	}
	return s.fn(req)
}

// testSocket keeps even long test names below AF_UNIX's address ceiling (K2).
func testSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "cs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "e.sock")
}

// testShell retains Unix argv and uses the native Git Bash resolver on Windows
// so K3's secret propagation runs through an actual shell on both platforms.
func testShell(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		return "sh"
	}
	bash, err := shell.Bash()
	if err != nil {
		t.Fatal(err)
	}
	return bash
}

// transportSocket identifies a fixture's writer even when a test replaces its
// data root with a simulated home after constructing the engine.
func transportSocket(tr Transport) string {
	switch t := tr.(type) {
	case Daemon:
		return t.Socket
	case Spawn:
		return t.Socket
	}
	return ""
}
