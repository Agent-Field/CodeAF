package resident

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

// ONE SCAN AT A TIME (#1659). The resident reconciler's tick and a chat
// door's launch pass run the same scan; the pass serializes them, so two
// concurrent runners leave the shelf with exactly one active fact per folder
// — the shape the idempotent pass promises — and never contend the store's
// writes mid-scan.
func TestTwoConcurrentImportPassesLeaveOneFactPerFolder(t *testing.T) {
	project, home := t.TempDir(), t.TempDir()
	folder := filepath.Join(project, ".agents", "skills", "pdf")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: pdf\ndescription: Extract text and tables from PDF files.\n---\n\n# pdf\nRead the folder.\n"
	if err := os.WriteFile(filepath.Join(folder, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	graph := openStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ReconcileImportedSkills(graph, project, home)
		}()
	}
	wg.Wait()
	facts, err := graph.SkillFacts(store.FactActive, 10)
	if err != nil {
		t.Fatalf("the shelf did not read: %v", err)
	}
	count := 0
	for _, fact := range facts {
		if fact.SkillName() == "pdf" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("two concurrent passes left %d active facts for pdf, want exactly 1", count)
	}
}
