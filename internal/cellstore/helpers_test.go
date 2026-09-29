package cellstore

import (
	"context"
	"encoding/json"
	"fmt"
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
	return Engine{Binary: bin, DataRoot: dataRoot}
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
