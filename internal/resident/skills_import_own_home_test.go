package resident

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
)

// Under `codeaf app --home <dir>` the app's folder is codeaf's state and the
// person's login home is still where Claude Code, Codex and the rest keep their
// skills. The import pass reads the second for other programs and the first for
// codeaf's own, and never the command line's .codeaf/skills in the login home.
func TestImportUnderAppHomeReadsOtherProgramsFromTheLoginHome(t *testing.T) {
	login, state, project := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv(home.EnvVar, state)
	t.Setenv(home.LoginEnvVar, login)
	claudeDir := filepath.Join(login, ".claude", "skills", "from-claude")
	codexDir := filepath.Join(login, ".codex", "skills", "from-codex")
	cliDir := filepath.Join(login, ".codeaf", "skills", "cli-only")
	appDir := filepath.Join(state, ".codeaf", "skills", "app-canvas")
	writeImportedSkill(t, claudeDir, "from-claude", "Kept for Claude Code")
	writeImportedSkill(t, codexDir, "from-codex", "Kept for Codex")
	writeImportedSkill(t, cliDir, "cli-only", "Kept for the command line")
	writeImportedSkill(t, appDir, "app-canvas", "Installed by the app")

	st := openStore(t)
	ReconcileImportedSkills(st, project, login)
	for dir, want := range map[string]int{claudeDir: 1, codexDir: 1, appDir: 1, cliDir: 0} {
		if got := countImportedActiveFolder(t, st, dir); got != want {
			t.Errorf("active facts for %s = %d, want %d", dir, got, want)
		}
	}
}

// A process that only moved CODEAF_HOME keeps the quarantine: its login home is
// that folder, so a skill in the real person's home is never imported.
func TestImportWithOnlyAMovedStateRootStaysQuarantined(t *testing.T) {
	login, state, project := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv(home.EnvVar, state)
	t.Setenv(home.LoginEnvVar, "")
	realDir := filepath.Join(login, ".claude", "skills", "from-claude")
	ownDir := filepath.Join(state, ".claude", "skills", "in-the-quarantine")
	writeImportedSkill(t, realDir, "from-claude", "Kept for Claude Code")
	writeImportedSkill(t, ownDir, "in-the-quarantine", "Kept where the run said")

	loginNamed, err := home.Login()
	if err != nil || loginNamed != state {
		t.Fatalf("Login with only CODEAF_HOME = %q err = %v, want %q", loginNamed, err, state)
	}
	st := openStore(t)
	ReconcileImportedSkills(st, project, loginNamed)
	if got := countImportedActiveFolder(t, st, realDir); got != 0 {
		t.Errorf("a skill in the real login home was imported by a run that only moved CODEAF_HOME: %d", got)
	}
	if got := countImportedActiveFolder(t, st, ownDir); got != 1 {
		t.Errorf("a skill in the quarantine home was not imported: %d", got)
	}
}

// With neither set the pass is what it was: codeaf's own folder is read under
// the login home beside the others.
func TestImportWithNothingMovedReadsCodeafsFolderUnderTheLoginHome(t *testing.T) {
	login, project := t.TempDir(), t.TempDir()
	t.Setenv(home.LoginEnvVar, "")
	cliDir := filepath.Join(login, ".codeaf", "skills", "cli-only")
	claudeDir := filepath.Join(login, ".claude", "skills", "from-claude")
	writeImportedSkill(t, cliDir, "cli-only", "Kept for the command line")
	writeImportedSkill(t, claudeDir, "from-claude", "Kept for Claude Code")

	st := openStore(t)
	ReconcileImportedSkills(st, project, login)
	for _, dir := range []string{cliDir, claudeDir} {
		if got := countImportedActiveFolder(t, st, dir); got != 1 {
			t.Errorf("active facts for %s = %d, want 1", dir, got)
		}
	}
}
