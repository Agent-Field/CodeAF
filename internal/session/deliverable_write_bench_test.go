package session

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// BenchmarkDeliverableOverwrite measures the mutation and index append together,
// with both an empty history and a history beyond the legacy reader's window.
func BenchmarkDeliverableOverwrite(b *testing.B) { benchmarkDeliverableWrites(b, false) }

// BenchmarkDeliverableFirstWrites measures distinct new paths after one warm-up.
func BenchmarkDeliverableFirstWrites(b *testing.B) { benchmarkDeliverableWrites(b, true) }

func benchmarkDeliverableWrites(b *testing.B, first bool) {
	for _, history := range []int{0, artifactRows + 1} {
		b.Run(strconv.Itoa(history), func(b *testing.B) {
			dir := b.TempDir()
			index := filepath.Join(dir, ArtifactsIndexName)
			agent, err := newAgent(Config{
				Workspace: dir, Model: "test/model", System: "SYSTEM",
				SessionFile: filepath.Join(dir, "transcript.jsonl"), ArtifactsIndex: index,
			}, &scriptedCompleter{})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = agent.Close() })
			for i := 0; i < history; i++ {
				if !RecordArtifact(index, Artifact{Path: "/work/other.md", Title: "other.md", Session: "other", Kind: DeliverableMarkdown, Created: time.Unix(1, 0)}) {
					b.Fatal("could not seed history")
				}
			}
			var write bare.Tool
			for _, tool := range agent.belt() {
				if tool.Name == "write" {
					write = tool
				}
			}
			args := []byte(`{"path":"notes.md","content":"# report\n"}`)
			mutate := func() {
				if out, bad, err := write.Execute(b.Context(), args); err != nil || bad {
					b.Fatalf("write: %v %s", err, out)
				}
			}
			mutate()
			before, err := os.Stat(index)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if first {
					args = []byte(`{"path":"file` + strconv.Itoa(i) + `.md","content":"# report\n"}`)
				}
				mutate()
			}
			b.StopTimer()
			after, err := os.Stat(index)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(after.Size()-before.Size())/float64(b.N), "index_bytes/op")
		})
	}
}
