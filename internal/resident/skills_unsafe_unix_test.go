//go:build unix

package resident

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/store"
)

func TestReconcileImportedSkillsSkipsPipeWithoutBlocking(t *testing.T) {
	project, home := t.TempDir(), t.TempDir()
	folder := filepath.Join(project, ".agents", "skills", "pipe")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(folder, "SKILL.md"), 0o600); err != nil {
		t.Skip(err)
	}
	graph := openStore(t)
	done := make(chan struct{})
	go func() { ReconcileImportedSkills(graph, project, home); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("resident import blocked on a FIFO")
	}
	facts, err := graph.SkillFacts(store.FactActive, 10)
	if err != nil || len(facts) != 0 {
		t.Fatalf("pipe was imported: %+v, %v", facts, err)
	}
}
