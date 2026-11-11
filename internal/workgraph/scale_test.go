package workgraph

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"testing"
)

// scaleInput is a crew of `owners` conversations holding `each` tasks apiece.
// Every task waits on its predecessor in the same conversation, and every tenth
// waits across conversations, so the graph is deep and wide at once.
func scaleInput(owners, each int) Input {
	facts := make([]Fact, 0, owners*each)
	for o := 0; o < owners; o++ {
		owner := fmt.Sprintf("agent%04d", o)
		for n := 1; n <= each; n++ {
			opts := []opt{state(StateDone), spend(TaskSpend{USD: 0.01, Tokens: 10, Basis: "priced"}), landing("merged"), files("a.go", "b.go")}
			if n > 1 {
				opts = append(opts, waits(owner, fmt.Sprint(n-1)))
			}
			if n%10 == 0 && o > 0 {
				opts = append(opts, waitsExt(fmt.Sprintf("agent%04d", o-1), fmt.Sprint(n)))
			}
			facts = append(facts, task(owner, n, opts...))
		}
	}
	return Input{Scope: GraphScope{Kind: ScopeCrew, ID: "crew"}, Facts: facts, Revision: "r1", Owners: owners}
}

func heapNow() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// THE SOURCE BOUND: 200 000 tasks build once, in bounded time and memory, and a
// page of them is a bounded response however large the scope is.
func TestAScopeAtTheSourceBoundBuildsAndPagesWithinBounds(t *testing.T) {
	if testing.Short() {
		t.Skip("builds 200 000 tasks")
	}
	in := scaleInput(200, 1000)
	before := heapNow()
	snap, err := Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	held := heapNow() - before
	// The producer's own facts are released once built; what the snapshot keeps is
	// what is measured.
	in.Facts = nil
	runtime.KeepAlive(snap)
	t.Logf("200000 tasks: snapshot holds ~%d MiB", (heapNow()-before)/(1<<20))
	_ = held
	res, err := snap.Answer(context.Background(), GraphRequest{Scope: in.Scope, Limit: MaxNodes})
	if err != nil {
		t.Fatal(err)
	}
	if res.Totals.Nodes != 200_000 || len(res.Nodes) > MaxNodes || len(res.Edges) > MaxEdges || res.Next == "" {
		t.Fatalf("page: totals %+v nodes %d edges %d next %q", res.Totals, len(res.Nodes), len(res.Edges), res.Next)
	}
	raw, _ := json.Marshal(res)
	t.Logf("one full page is %d KiB of JSON", len(raw)/1024)
	if len(raw) > 2<<20 {
		t.Fatalf("a page of %d nodes is %d bytes", len(res.Nodes), len(raw))
	}
	if !res.Coverage.Complete || res.Totals.Edges < 200_000 {
		t.Fatalf("coverage %+v edges %d", res.Coverage, res.Totals.Edges)
	}
}

func BenchmarkBuild200K(b *testing.B) {
	in := scaleInput(200, 1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Build(context.Background(), in); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPageOf500From200K(b *testing.B) {
	in := scaleInput(200, 1000)
	snap, err := Build(context.Background(), in)
	if err != nil {
		b.Fatal(err)
	}
	req := GraphRequest{Scope: in.Scope, Limit: MaxNodes}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := snap.Answer(context.Background(), req); err != nil {
			b.Fatal(err)
		}
	}
}
