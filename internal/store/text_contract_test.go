package store

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Text contract C5: dependency products recover native absolute paths from
// summaries, and alternate drive case never carries one file twice.
func TestTextContractSummaryNativePaths(t *testing.T) {
	graph := twoProducers(t)
	path := filepath.Join(t.TempDir(), "report.md")
	if err := os.WriteFile(path, []byte("the predecessor's report\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	other := path
	if runtime.GOOS == "windows" {
		other = strings.ToLower(filepath.VolumeName(path)) + filepath.ToSlash(path[len(filepath.VolumeName(path)):])
	}
	for id, summary := range map[string]string{"wrote": "Written (" + path + ").", "spoke": "Also available: " + other} {
		if err := graph.Complete(mustClaim(t, graph, id, "worker"), summary); err != nil {
			t.Fatal(err)
		}
	}
	inputs, err := graph.DependencyInputs("join", 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	combined := ""
	for _, input := range inputs {
		combined += input.Digest
	}
	if strings.Count(combined, "the predecessor's report") != 1 {
		t.Fatalf("product lost or duplicated: %s", combined)
	}
}
