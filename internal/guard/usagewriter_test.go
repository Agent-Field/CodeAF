package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryUsageWriterStartHasARegistryOwner is the standing ownership law for
// the spending ledger's background writer. There is exactly one start door. It
// registers the writer before starting it, and the registry's terminal close
// closes that writer's queue and joins its completion before returning.
func TestEveryUsageWriterStartHasARegistryOwner(t *testing.T) {
	path := filepath.Join(repositoryRoot(t), "internal", "session", "usage_ledger.go")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	spawns, err := scanSpawns(raw)
	if err != nil {
		t.Fatal(err)
	}
	var writerStarts []string
	for _, spawn := range spawns {
		if strings.Contains(spawn.statement, ".run(") {
			writerStarts = append(writerStarts, spawn.statement)
		}
	}
	if len(writerStarts) != 1 || writerStarts[0] != "go writer.run(path)" {
		t.Fatalf("usage writer starts must use the one registry-owned door, got %v", writerStarts)
	}

	source := string(raw)
	for _, owned := range []string{
		"usageWriters[path] = writer\n\tgo writer.run(path)",
		"close(writer.queue)",
		"<-writer.stopped",
		"defer close(w.stopped)",
	} {
		if !strings.Contains(source, owned) {
			t.Fatalf("usage writer owner law is missing %q", owned)
		}
	}
}
