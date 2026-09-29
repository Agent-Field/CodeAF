package cellstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// fillTree writes n small files spread over n/100 directories.
func fillTree(b *testing.B, root string, n int) {
	b.Helper()
	for i := 0; i < n; i++ {
		dir := filepath.Join(root, "src", fmt.Sprintf("d%03d", i/100))
		if i%100 == 0 {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				b.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%03d.txt", i%100)), []byte(fmt.Sprintf("file %d\n", i)), 0o644); err != nil {
			b.Fatal(err)
		}
	}
}

// percentile of sorted durations, in milliseconds.
func percentile(d []time.Duration, p float64) float64 {
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return float64(d[int(float64(len(d)-1)*p)]) / float64(time.Millisecond)
}

// timeSeals runs n seals, each after changing one file, and reports the
// distribution. The first seal (attach + first capture) is outside the loop.
func timeSeals(b *testing.B, e Engine, c cell.Cell, listChanged bool) {
	ctx := context.Background()
	if _, err := e.Seal(ctx, c, TurnInfo{}); err != nil {
		b.Fatal(err)
	}
	victim := filepath.Join(c.Root, "src", "d005", "f005.txt")
	info := TurnInfo{Calls: []Executed{exec1("edit", "")}}
	if listChanged {
		info.Changed = []string{filepath.Join("src", "d005", "f005.txt")}
	}
	took := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := os.WriteFile(victim, []byte(fmt.Sprintf("change %d\n", i)), 0o644); err != nil {
			b.Fatal(err)
		}
		start := time.Now()
		if _, err := e.Seal(ctx, c, info); err != nil {
			b.Fatal(err)
		}
		took = append(took, time.Since(start))
	}
	b.StopTimer()
	b.ReportMetric(percentile(took, 0.5), "p50_ms")
	b.ReportMetric(percentile(took, 0.95), "p95_ms")
	b.ReportMetric(percentile(took, 1), "max_ms")
}

// BenchmarkSealDelta10k is the task 0.7 number: p50 seal latency on a 10,000
// file tree with one changed file, through the real engine, when the caller
// does not say what changed and the engine walks the tree (the stat cache
// spares every unchanged file an open and a read).
func BenchmarkSealDelta10k(b *testing.B) { benchSealDelta10k(b, false) }

// BenchmarkSealDelta10kChanged is the same tree when the caller lists the one
// changed path and the engine visits only it.
func BenchmarkSealDelta10kChanged(b *testing.B) { benchSealDelta10k(b, true) }

func benchSealDelta10k(b *testing.B, listChanged bool) {
	e := realEngineB(b)
	c, err := cell.CreateIn(b.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		b.Fatal(err)
	}
	fillTree(b, c.Root, 10000)
	timeSeals(b, e, c, listChanged)
}

// BenchmarkSealComposeOnly is everything a seal does except the engine spawn,
// on the same tree shape: the harness's share of the latency.
func BenchmarkSealComposeOnly(b *testing.B) {
	fake := &fakeEngine{}
	e := Engine{Binary: "engine", DataRoot: b.TempDir(), Run: fake.run}
	c, err := cell.CreateIn(b.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		b.Fatal(err)
	}
	fillTree(b, c.Root, 10000)
	timeSeals(b, e, c, false)
}

// BenchmarkSpawnBaseline is the cost of starting the engine and doing nothing.
func BenchmarkSpawnBaseline(b *testing.B) {
	e := realEngineB(b)
	took := make([]time.Duration, 0, b.N)
	for i := 0; i < b.N; i++ {
		start := time.Now()
		if _, err := spawn(context.Background(), b.TempDir(), nil, e.Binary, "--version"); err != nil {
			b.Fatal(err)
		}
		took = append(took, time.Since(start))
	}
	b.ReportMetric(percentile(took, 0.5), "p50_ms")
	b.ReportMetric(percentile(took, 0.95), "p95_ms")
}
