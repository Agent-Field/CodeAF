package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/store"
)

// THE SHELF BEFORE THE FIRST MESSAGE. A person's skills for other harnesses —
// Claude Code, Codex, any agentskills.io reader — are on the shelf when the
// first message is built, not after some later tick finds the time. The v3
// chat door claims no residency (runChatV3's header), so the launch itself
// runs the import pass against the process's own store; these tests hold that
// law.

// aForeignSkill installs one real-shaped SKILL.md folder the way the person's
// harness keeps it: a directory under the isolated login home's .claude/skills
// holding a frontmatter SKILL.md. It must be called AFTER v3TestProcess, which
// is what pins CODEAF_HOME to a directory of the test's own.
func aForeignSkill(t *testing.T) string {
	t.Helper()
	stateRoot := os.Getenv(home.EnvVar)
	if stateRoot == "" {
		t.Fatal("the suite's isolated CODEAF_HOME is not set")
	}
	dir := filepath.Join(stateRoot, ".claude", "skills", "pdf")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: pdf\ndescription: Extract text and tables from PDF files.\n---\n\n# pdf\nRead the folder with the read tool.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAForeignSkillIsOnTheShelfBeforeTheFirstMessage(t *testing.T) {
	proc := v3TestProcess(t)
	dir := aForeignSkill(t)

	workspace := t.TempDir()
	launch, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: workspace})
	if err != nil {
		t.Fatalf("the launch did not open: %v", err)
	}

	facts, err := launch.Config.Memory.SkillFacts(store.FactActive, 50)
	if err != nil {
		t.Fatalf("the shelf did not read: %v", err)
	}
	var found *store.Fact
	for i := range facts {
		if facts[i].Artifact == dir {
			found = &facts[i]
		}
	}
	if found == nil {
		t.Fatalf("no active skill fact for %q after the launch; facts: %+v", dir, facts)
	}
	if found.Trust != "imported-provisional" {
		t.Fatalf("imported skill trust = %q, want imported-provisional", found.Trust)
	}
	if found.Body != "Extract text and tables from PDF files." {
		t.Fatalf("imported skill body = %q, want the frontmatter description", found.Body)
	}

	// AND THE OPEN IS IDEMPOTENT: a second launch over an unchanged disk
	// journals no new fact — the shelf keeps its one-active-fact-per-folder
	// shape, so a person who opens and closes conversations all day leaves
	// exactly one row behind.
	if _, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: workspace}); err != nil {
		t.Fatalf("the second launch did not open: %v", err)
	}
	again, err := launch.Config.Memory.SkillFacts(store.FactActive, 50)
	if err != nil {
		t.Fatalf("the shelf did not read the second time: %v", err)
	}
	count := 0
	for _, fact := range again {
		if fact.Artifact == dir {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("a second launch left %d active facts for %q, want exactly 1", count, dir)
	}
}

// A LAUNCH WITH NO STORE OPENS ANYWAY. The nil store is the same answer the
// catalog already gives when memory is off — no shelf, no pass, and certainly
// no refusal.
func TestALaunchWithNoStoreSkipsTheShelfPassWithoutPanic(t *testing.T) {
	importForeignSkillsBeforeFirstMessage(nil, t.TempDir())
}

// MEMORY OFF IS NOT SKILLS OFF. A launch whose memory row is off opens no
// memory store at all, and still reaches the skills a person installed for
// another harness: the process builds a shelf of the folders alone, the
// launch imports into it before the first message, and the process removes it
// when it closes, so nothing about the folders outlives the process that read
// them.
func TestAMemoryOffLaunchStillHasTheSkillShelf(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())
	profile := t.TempDir()
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	if err := os.WriteFile(filepath.Join(profile, "config.json"), []byte(`{"memory.enabled": "off"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	proc, err := openV3Process("chat")
	if err != nil {
		t.Fatalf("the process did not open: %v", err)
	}
	t.Cleanup(proc.closeAll)
	dir := aForeignSkill(t)

	launch, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir()})
	if err != nil {
		t.Fatalf("the launch did not open: %v", err)
	}
	if launch.Config.Memory != nil {
		t.Fatal("a launch with memory off was handed a memory store")
	}
	if launch.Config.Skills == nil {
		t.Fatal("a launch with memory off was handed no skill shelf")
	}
	facts, err := launch.Config.Skills.SkillFacts(store.FactActive, 50)
	if err != nil {
		t.Fatalf("the memory-off shelf did not read: %v", err)
	}
	found := false
	for _, fact := range facts {
		found = found || fact.Artifact == dir
	}
	if !found {
		t.Fatalf("the skill folder is not on the memory-off shelf: %+v", facts)
	}

	shelfDir := proc.skillsDir
	if shelfDir == "" {
		t.Fatal("the memory-off shelf has no folder of its own to remove")
	}
	proc.closeAll()
	if _, err := os.Stat(shelfDir); !os.IsNotExist(err) {
		t.Fatalf("the memory-off shelf outlived its process at %s (%v)", shelfDir, err)
	}
}

// AND WITH MEMORY ON THERE IS ONE SHELF, the memory store itself: no second
// database is opened beside the one the conversation remembers into.
func TestAMemoryOnLaunchReadsSkillsFromTheMemoryStore(t *testing.T) {
	proc := v3TestProcess(t)
	launch, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir()})
	if err != nil {
		t.Fatalf("the launch did not open: %v", err)
	}
	if launch.Config.Memory == nil || launch.Config.Skills != launch.Config.Memory {
		t.Fatalf("with memory on the skill shelf is %p and memory is %p, want the same store", launch.Config.Skills, launch.Config.Memory)
	}
	if proc.skillsDir != "" {
		t.Fatalf("a memory-on process made a second shelf at %s", proc.skillsDir)
	}
}
