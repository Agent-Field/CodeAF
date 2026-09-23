package resident

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

// activeSkill records one skill fact and activates it at dir, under trust.
func activeSkill(t *testing.T, graph *store.Store, dir, trust string) store.Fact {
	t.Helper()
	candidate, err := graph.RecordSkillCandidateFrom(store.FactWriterOther, store.RootID,
		"repo:"+filepath.Dir(dir), "a skill at "+filepath.Base(dir), dir, trust)
	if err != nil {
		t.Fatalf("record skill: %v", err)
	}
	if err := graph.ActivateSkill(candidate.Seq, dir, "digest-"+filepath.Base(dir)); err != nil {
		t.Fatalf("activate skill: %v", err)
	}
	return candidate
}

// The foreign-skill import is gone, and a store that ran it still holds its
// facts on the active shelf. The retirement takes exactly those off — a skill
// this program forged is never touched — and a second run journals nothing.
func TestRetireTakesOnlyTheOldImportsOffTheShelf(t *testing.T) {
	root := t.TempDir()
	graph := openStore(t)
	imported := filepath.Join(root, "home", ".claude", "skills", "pdf")
	forged := filepath.Join(root, "shelf", "lint-fix")
	activeSkill(t, graph, imported, importedSkillTrust)
	activeSkill(t, graph, forged, "forged")

	RetireImportedSkills(graph)
	after, err := graph.LatestEventSeq()
	if err != nil {
		t.Fatal(err)
	}
	RetireImportedSkills(graph)
	again, err := graph.LatestEventSeq()
	if err != nil {
		t.Fatal(err)
	}
	if again != after {
		t.Fatalf("a second retirement over a clean shelf journaled %d events", again-after)
	}

	active, err := graph.SkillFacts(store.FactActive, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || filepath.Clean(active[0].Artifact) != forged {
		t.Fatalf("active shelf after retirement = %+v, want only the forged skill", active)
	}
}

// A door with memory off hands no store, and the retirement is a no-op there
// rather than a panic.
func TestRetireWithNoStoreDoesNothing(t *testing.T) {
	RetireImportedSkills(nil)
}
