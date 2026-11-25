package claudeagent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
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

// Under `codeaf app --home <dir>` the skills the app installs for codeaf live in
// that folder and the person's other programs' stay in the real login home.
// Claude Code is handed both, and not the command line's own folder.
func TestSkillPluginUnderAnAppHomeFollowsTheLoginHome(t *testing.T) {
	login, state := t.TempDir(), t.TempDir()
	t.Setenv(home.EnvVar, state)
	t.Setenv(home.LoginEnvVar, login)
	write := func(base, root, name string) {
		dir := filepath.Join(base, root, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + name + "\ndescription: does " + name + "\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(state, ".codeaf/skills", "workaf-canvas")
	write(login, ".agents/skills", "shared-one")
	write(login, ".codeaf/skills", "cli-only")
	root := SkillPlugin(t.TempDir())
	if root == "" {
		t.Fatal("no plugin was built for skills Claude Code does not have")
	}
	for name, want := range map[string]bool{"workaf-canvas": true, "shared-one": true, "cli-only": false} {
		_, err := os.Stat(filepath.Join(root, "skills", name, "SKILL.md"))
		if (err == nil) != want {
			t.Errorf("%s linked = %v, want %v (%v)", name, err == nil, want, err)
		}
	}
}
