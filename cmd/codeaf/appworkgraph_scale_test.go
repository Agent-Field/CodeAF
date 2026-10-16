package main

// THE MEMORY LAW OF work.graph, on the real backend and the real wire: a
// board-sized scope is answered inside the budget and its second page costs
// nothing to build or read; a scope past the budget is refused, typed, before
// the memory is spent.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/workgraph"
)

// scaleCheckpoint is a conversation of n queued tasks, each waiting on the one
// before it, with a brief a person might have written.
func scaleCheckpoint(n int) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, `{"type":"tasks","version":1,"seq":%d,"nodes":[`, n)
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteByte(',')
		}
		dep := ""
		if i > 1 {
			dep = fmt.Sprintf(`,"depends_on":[%d]`, i-1)
		}
		fmt.Fprintf(&b, `{"id":%d,"title":"Task %d of the migration","brief":"Move the tables for part %d and keep the old reads working until the cutover.","acceptance":"The checks for part %d pass.","state":"queued","admittedAt":"2026-10-01T09:00:00Z","stateAt":"2026-10-01T09:00:00Z"%s}`, i, i, i, i, dep)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

// scaleFixture is a crew of `owners` conversations holding `each` tasks apiece,
// behind a real wire.
func scaleFixture(t *testing.T, owners, each int) (*graphWire, string) {
	t.Helper()
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	bucket, err := v3ProjectDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := scaleCheckpoint(each)
	var transcripts []string
	for i := 0; i < owners; i++ {
		id := fmt.Sprintf("%016x", 0x1000+i)
		dir := writeV3Session(t, bucket, id, "w", time.Now())
		if err := os.WriteFile(session.Place{Dir: dir}.Tasks(), body, 0o600); err != nil {
			t.Fatal(err)
		}
		transcripts = append(transcripts, session.Place{Dir: dir}.Transcript())
	}
	backend := newFakeBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	t.Cleanup(func() { backend.Close() })
	backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	backend.profileDir = t.TempDir()
	crew := teamstore.NewID()
	err = teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: crew, Name: "harbor", Made: time.Now()})
		for i, tr := range transcripts {
			if err := f.AddMember(crew, teamstore.Member{Key: tr, File: tr, Where: "/w", Word: fmt.Sprint(i), Handle: fmt.Sprint(i)}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	server := appwire.NewServer(outW)
	go func() {
		_ = server.Serve(context.Background(), inR, appwire.Hello{Version: "test"}, backend)
		outW.Close()
	}()
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 8<<20), 8<<20)
	t.Cleanup(func() { inW.Close() })
	if !scanner.Scan() {
		t.Fatal("no hello")
	}
	return &graphWire{t: t, in: inW, lines: scanner}, crew
}

func allocated() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.TotalAlloc
}

// peakHeap runs f and reports the most live heap it added over what was there
// before, sampled every millisecond with the collector run eagerly, so garbage
// that was already dead does not read as memory held. It is a measurement of
// this process's Go heap and no claim about its resident size.
func peakHeap(f func()) uint64 {
	old := debug.SetGCPercent(10)
	defer debug.SetGCPercent(old)
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	var peak atomic.Uint64
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		var m runtime.MemStats
		for {
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > peak.Load() {
				peak.Store(m.HeapAlloc)
			}
			select {
			case <-stop:
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()
	f()
	close(stop)
	<-done
	if p := peak.Load(); p > base.HeapAlloc {
		return p - base.HeapAlloc
	}
	return 0
}

// TEN THOUSAND ORDINARY TASKS, the size a board is tested at: the cold ask
// allocates less than the whole 128 MiB budget, builds once, and every later
// page — and an unchanged revision — builds nothing and reads no file.
func TestATenThousandTaskScopeIsAnsweredInsideTheBudgetAndPagesCostNothing(t *testing.T) {
	w, crew := scaleFixture(t, 10, 1000)
	workGraphs.init()
	scope := `{"kind":"crew","id":"` + crew + `"}`
	var first workgraph.GraphResult
	cold := peakHeap(func() { first = w.graph(`{"scope":` + scope + `,"limit":500}`) })
	if first.Totals.Nodes != 10_000 || len(first.Nodes) != 500 || first.Next == "" {
		t.Fatalf("first page: totals %+v, %d nodes, next %q", first.Totals, len(first.Nodes), first.Next)
	}
	t.Logf("cold ask of 10 000 tasks peaked %d MiB of live heap", cold>>20)
	if cold > 128<<20 {
		t.Fatalf("the cold ask peaked at %d MiB against a 128 MiB budget", cold>>20)
	}
	builds, _ := workGraphs.cache.Stats()
	parses := workGraphs.source.Parses()
	cursor, _ := json.Marshal(first.Next)
	start := time.Now()
	pages := peakHeap(func() {
		for i := 0; i < 5; i++ {
			page := w.graph(`{"scope":` + scope + `,"limit":500,"cursor":` + string(cursor) + `}`)
			if len(page.Nodes) != 500 {
				t.Errorf("page %d has %d nodes", i, len(page.Nodes))
			}
		}
	})
	if b, _ := workGraphs.cache.Stats(); b != builds {
		t.Fatalf("paging rebuilt the snapshot: %d -> %d", builds, b)
	}
	if workGraphs.source.Parses() != parses {
		t.Fatal("paging read the conversations' files again")
	}
	t.Logf("5 more pages: %v, peak %d MiB", time.Since(start), pages>>20)
	if pages > 24<<20 {
		t.Fatalf("five cached pages peaked at %d MiB", pages>>20)
	}
	if got := w.graph(`{"scope":` + scope + `,"if_revision":"` + first.Revision + `"}`); !got.Unchanged {
		t.Fatal("an unmoved revision was not answered unchanged")
	}
	if b, _ := workGraphs.cache.Stats(); b != builds {
		t.Fatal("an unchanged answer built a snapshot")
	}
}

// A SCOPE PAST THE BUDGET is refused with a typed error, and the refusal costs
// little: reading stopped at the conversation that crossed the quota, nothing
// was built, and the wire says which smaller scope to ask for.
func TestAScopePastTheBudgetIsRefusedTypedBeforeTheMemoryIsSpent(t *testing.T) {
	w, crew := scaleFixture(t, 40, 1000)
	workGraphs.init()
	parses := workGraphs.source.Parses()
	builds, _ := workGraphs.cache.Stats()
	var bad map[string]any
	spent := peakHeap(func() { _, bad = w.call(`{"scope":{"kind":"crew","id":"` + crew + `"},"limit":500}`) })
	if bad == nil || bad["code"] != appwire.CodeTooLarge {
		t.Fatalf("40 000 tasks answered %v", bad)
	}
	detail, _ := bad["detail"].(map[string]any)
	if detail["reason"] != workgraph.NarrowScope || detail["scope"] != "agent" || detail["need"].(float64) <= detail["limit"].(float64) {
		t.Fatalf("detail = %v", detail)
	}
	if read := workGraphs.source.Parses() - parses; read >= 40 {
		t.Fatalf("the refusal read all %d conversations", read)
	}
	if b, _ := workGraphs.cache.Stats(); b != builds {
		t.Fatal("a refused scope was built")
	}
	t.Logf("refused after reading %d of 40 conversations, peak %d MiB", workGraphs.source.Parses()-parses, spent>>20)
	if spent > 64<<20 {
		t.Fatalf("the refusal peaked at %d MiB", spent>>20)
	}
	// An agent of the same crew is within the budget and is answered.
	if got := w.graph(`{"scope":{"kind":"agent","id":"` + fmt.Sprintf("%016x", 0x1000) + `"}}`); got.Totals.Nodes != 1000 {
		t.Fatalf("one agent of the crew: %+v", got.Totals)
	}
}
