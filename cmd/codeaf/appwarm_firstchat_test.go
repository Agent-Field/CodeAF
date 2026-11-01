package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

// THE FIRST CHAT DOES NOT WAIT FOR THE SKILL SHELF. Every conversation opens
// on the skills the person installed for other tools, and the first open in a
// process reads them all from disk, which on a machine with many skills is
// seconds the new chat spent saying "starting". The hello's warm reads them for
// the work folder a new chat starts in, so the first message finds the shelf
// already filled.
func TestAppWarmFillsTheSkillShelfBeforeTheFirstChat(t *testing.T) {
	root := resolvedTempDir(t)
	wakeEnvironment(t, root)
	skill := filepath.Join(root, "home", ".claude", "skills", "pdf")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: pdf\ndescription: Fill forms\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	backend := newAppBackend(newAppRecorder(), appOpenInProcess)
	defer backend.Close()

	backend.Warm()

	proc := openedEngineProcess()
	if proc == nil {
		t.Fatal("the warm did not open the process half")
	}
	facts, err := proc.skillShelf().SkillFacts(store.FactActive, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range facts {
		if filepath.Clean(fact.Artifact) == skill {
			return
		}
	}
	t.Fatalf("after the warm the shelf holds %d skills and not %s", len(facts), skill)
}
