package exec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func tracePath(t *testing.T, space *Workspace, nodeID int) string {
	t.Helper()
	full, _, err := space.ScratchPath(filepath.Join(traceDir, fmt.Sprintf("%d.trace.log", nodeID)))
	if err != nil {
		t.Fatal(err)
	}
	return full
}

// The recorder is buffered now, so the two things worth pinning are that the
// buffering is invisible in the file it leaves behind, and that a run which is
// still going has landed everything up to its last record boundary.
func TestTraceWritesEveryLineAndLosesNothingOnClose(t *testing.T) {
	space, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	trace := newTracer(space, 7)

	var want strings.Builder
	for i := range 5000 {
		line := fmt.Sprintf(`{"type":"message.updated","seq":%d}`, i)
		trace.note(line)
		want.WriteString(line + "\n")
	}
	trace.close()

	data, err := os.ReadFile(tracePath(t, space, 7))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want.String() {
		t.Fatalf("the trace holds %d bytes, want %d", len(data), want.Len())
	}
}

// A flush is what a batch boundary buys: everything written before it is on
// disk while the run is still going.
func TestTraceFlushLandsWhatWasWritten(t *testing.T) {
	space, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	trace := newTracer(space, 3)
	defer trace.close()

	trace.note("engine: started")
	trace.flush()

	data, err := os.ReadFile(tracePath(t, space, 3))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "engine: started\n" {
		t.Fatalf("after a flush the trace holds %q", data)
	}
}

// A turn is a whole record and lands on its own, without waiting for a batch to
// fill or for the run to end.
func TestTraceLandsATurnWhenItIsWritten(t *testing.T) {
	space, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	trace := newTracer(space, 4)
	defer trace.close()

	trace.note("contract: do the thing")
	trace.turn(1, nil, nil, nil, "final")

	data, err := os.ReadFile(tracePath(t, space, 4))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "contract: do the thing") || !strings.Contains(string(data), "turn 1") {
		t.Fatalf("a written turn is not on disk: %q", data)
	}
}

// The turn line carries the cached share of the prompt, and the recorder does
// not live where the leaf was sent to read.
//
// Both are the same defect seen from two sides. Every cache discipline in this
// codebase is unfalsifiable while the trace writes only in= and out=: a run
// whose affinity key was never set and a run whose prefix was perfect leave
// identical records. And the recorder itself used to sit in .obs, which is the
// one machinery directory a leaf is deliberately sent into by every spill
// pointer — so leaves read their own flight recorders, and their siblings', out
// of the directory they had been told to read.
func TestTheTurnLineCarriesTheCachedShareAndSitsOutOfTheLeafsWay(t *testing.T) {
	space, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	trace := newTracer(space, 11)
	trace.turn(3, &ai.Response{
		Choices: []ai.Choice{{FinishReason: "tool_calls"}},
		Usage: &ai.Usage{PromptTokens: 12_000, CompletionTokens: 300,
			PromptTokensDetails: &ai.PromptTokensDetails{CachedTokens: 11_400}},
	}, nil, nil, "")
	trace.close()

	data, err := os.ReadFile(tracePath(t, space, 11))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "in=12000 out=300 cached=11400") {
		t.Fatalf("the turn line does not say what was billed warm: %q", data)
	}

	// The recorder is not in the directory the spill stubs send the leaf to.
	if entries, err := os.ReadDir(filepath.Join(space.Root(), obsDir)); err == nil {
		for _, entry := range entries {
			if strings.Contains(entry.Name(), "trace") {
				t.Fatalf("the flight recorder is back in %s, where leaves read: %s", obsDir, entry.Name())
			}
		}
	}
}

// The child's stdout and its stderr are traced from two goroutines at once, so
// the recorder is written to concurrently. Run under -race.
func TestTraceTakesConcurrentWriters(t *testing.T) {
	space, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	trace := newTracer(space, 9)

	var writers sync.WaitGroup
	for writer := range 4 {
		writers.Add(1)
		go func(writer int) {
			defer writers.Done()
			for i := range 500 {
				trace.note(fmt.Sprintf("writer %d line %d", writer, i))
				if i%100 == 0 {
					trace.flush()
				}
			}
		}(writer)
	}
	writers.Wait()
	trace.close()

	data, err := os.ReadFile(tracePath(t, space, 9))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(data), "\n"); lines != 2000 {
		t.Fatalf("the trace holds %d lines, want 2000", lines)
	}
	// Whole lines, not fragments of two writers spliced together.
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if !strings.HasPrefix(line, "writer ") || strings.Count(line, "writer ") != 1 {
			t.Fatalf("a line came out spliced: %q", line)
		}
	}
}

// The zero tracer is what a node with nowhere to write gets, and every entry
// point has to survive it.
func TestTheZeroTracerIsUsable(t *testing.T) {
	var trace tracer
	trace.note("nothing to write to")
	trace.turn(1, nil, nil, nil, "")
	trace.flush()
	trace.close()
}
