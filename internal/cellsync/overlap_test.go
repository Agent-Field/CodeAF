package cellsync

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// gatedEngine counts partial imports and holds each one until released, so a
// test can land frames while an import is in flight.
type gatedEngine struct {
	*FakeEngine
	mu      sync.Mutex
	partial int
	gate    chan struct{}
}

func (g *gatedEngine) ImportPartial(ctx context.Context, c cell.Cell, head, inbox string) (int, error) {
	g.mu.Lock()
	g.partial++
	g.mu.Unlock()
	<-g.gate
	return g.FakeEngine.ImportPartial(ctx, c, head, inbox)
}

func (g *gatedEngine) imports() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.partial
}

// Frames keep landing while an import runs: the landing path never waits on
// the importer, the requests made meanwhile share one more import, and finish
// waits for the one in flight.
func TestOverlapNeverBlocksLandingFrames(t *testing.T) {
	eng := &gatedEngine{FakeEngine: NewFakeEngine(t.TempDir()), gate: make(chan struct{})}
	lap := startOverlap(context.Background(), eng, cell.Cell{ID: cellID, Root: t.TempDir()}, "head", t.TempDir())
	for i := 0; i < 5*ImportEvery; i++ {
		lap.landed() // would deadlock here if landing waited for the gated import
	}
	close(eng.gate)
	lap.finish()
	if n := eng.imports(); n < 1 || n > 5 {
		t.Fatalf("partial imports = %d, want 1 to 5 for %d landed frames", n, 5*ImportEvery)
	}
}

// A partial import stores what the head reaches and deletes nothing else: a
// file whose parent has not landed stays for the next call.
func TestPartialImportKeepsWhatItCannotYetReach(t *testing.T) {
	r := newRig(t)
	head := r.publishFirst(map[string]string{"a": "one", "b": "two"})
	f, c := primedFetcherFor(t, r.mem, r.dir.For(devB))
	inbox := t.TempDir()
	stray := filepath.Join(inbox, "ffff")
	if err := os.WriteFile(stray, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Engine.ImportPartial(context.Background(), c, head, inbox); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Fatalf("a partial import removed a file it did not take: %v", err)
	}
}

// The take completes whole when frames land in several windows, and the
// materialized tree is the head's.
func TestPrimedTakeWithOverlapCompletes(t *testing.T) {
	r := newRig(t)
	files := map[string]string{}
	for _, n := range []string{"a", "b", "c", "d", "e", "f"} {
		files[n] = n + n
	}
	head := r.publishFirst(files)
	f, c := primedFetcherFor(t, r.mem, r.dir.For(devB))
	if err := f.Fetch(context.Background(), c, head); err != nil {
		t.Fatal(err)
	}
	if got := readFiles(t, c.Root); !reflect.DeepEqual(got, files) {
		t.Fatalf("materialized %v, want %v", got, files)
	}
}
