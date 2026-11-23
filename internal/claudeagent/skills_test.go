package claudeagent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodeafsOwnSkillsReachClaudeCodeAndItsOwnAreNotHandedTwice(t *testing.T) {
	login := t.TempDir()
	t.Setenv("CODEAF_HOME", login)
	write := func(root, name string) {
		dir := filepath.Join(login, root, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + name + "\ndescription: does " + name + "\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".codeaf/skills", "workaf-canvas")
	write(".agents/skills", "shared-one")
	write(".claude/skills", "claudes-own")
	root := SkillPlugin(t.TempDir())
	if root == "" {
		t.Fatal("no plugin was built for skills codeaf has and Claude Code does not")
	}
	if _, err := os.Stat(filepath.Join(root, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("the plugin has no manifest: %v", err)
	}
	for _, name := range []string{"workaf-canvas", "shared-one"} {
		if _, err := os.Stat(filepath.Join(root, "skills", name, "SKILL.md")); err != nil {
			t.Fatalf("%s is not linked: %v", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "skills", "claudes-own")); err == nil {
		t.Fatal("a skill Claude Code already reads was handed to it a second time")
	}
}
