package skills

import (
	"path/filepath"
	"slices"
	"testing"
)

// The desktop app keeps codeaf's state, and the skills it installs for codeaf,
// in a folder of its own, while the person's other programs keep theirs in the
// real login home. OwnHomeDir is how a scan says so: codeaf's own folder is
// read from the first directory, every other program's from the second.

// ownHomeFixture lays down a login home that holds another program's skills, a
// plugin and Codex's own, plus a .codeaf/skills the command line owns, and a
// separate folder that holds the app's own .codeaf/skills.
func ownHomeFixture(t *testing.T) (project, login, own string) {
	t.Helper()
	root := pluginFixture(t)
	project, login = filepath.Join(root, "elsewhere"), filepath.Join(root, "home")
	own = t.TempDir()
	writeSkill(t, filepath.Join(login, ".claude", "skills", "claude-kept"), "claude-kept")
	writeSkill(t, filepath.Join(login, ".codex", "skills", "codex-kept"), "codex-kept")
	writeSkill(t, filepath.Join(login, ".config", "goose", "skills", "goose-kept"), "goose-kept")
	writeSkill(t, filepath.Join(login, ".codeaf", "skills", "cli-only"), "cli-only")
	writeSkill(t, filepath.Join(own, ".codeaf", "skills", "app-canvas"), "app-canvas")
	return project, login, own
}

func TestOwnHomeDirReadsOtherProgramsFromTheLoginHomeAndCodeafsFromItsOwn(t *testing.T) {
	project, login, own := ownHomeFixture(t)
	found, err := Discover(Options{ProjectDir: project, HomeDir: login, OwnHomeDir: own})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ suffix, root string }{
		{filepath.Join(login, ".claude", "skills", "claude-kept"), ".claude/skills"},
		{filepath.Join(login, ".codex", "skills", "codex-kept"), ".codex/skills"},
		{filepath.Join(login, ".config", "goose", "skills", "goose-kept"), ".config/goose/skills"},
		{filepath.Join("tidy", "1.0.0", "skills", "tidy-commits"), RootClaudePlugins},
		{filepath.Join(login, ".codex", "skills", ".system", "image-lite"), RootCodexSystem},
		{filepath.Join(own, ".codeaf", "skills", "app-canvas"), RootCodeaf},
	} {
		skill, ok := byDir(found, want.suffix)
		if !ok || skill.Scope != ScopeUser || skill.Shadowed || skill.Root != want.root {
			t.Errorf("the user skill at %s is missing or wrong (%+v) in %+v", want.suffix, skill, found)
		}
	}
	if skill, ok := byDir(found, filepath.Join(login, ".codeaf", "skills", "cli-only")); ok {
		t.Errorf("the command line's own skills folder was read for an app that keeps its own: %+v", skill)
	}
}

func TestWithoutOwnHomeDirCodeafsFolderIsReadUnderTheLoginHome(t *testing.T) {
	project, login, _ := ownHomeFixture(t)
	found := discover(t, project, login)
	if skill, ok := byDir(found, filepath.Join(login, ".codeaf", "skills", "cli-only")); !ok || skill.Scope != ScopeUser || skill.Root != RootCodeaf {
		t.Fatalf("codeaf's own folder under the login home was not read: %+v", found)
	}
	if _, ok := byDir(found, filepath.Join(login, ".claude", "skills", "claude-kept")); !ok {
		t.Fatalf("another program's skill was not read: %+v", found)
	}
	// Naming the login home as its own folder is the same as naming none.
	same, err := Discover(Options{ProjectDir: project, HomeDir: login, OwnHomeDir: login})
	if err != nil {
		t.Fatal(err)
	}
	if len(same) != len(found) {
		t.Fatalf("OwnHomeDir equal to HomeDir changed the answer: %d skills, want %d", len(same), len(found))
	}
}

func TestOwnHomeDirNeverMovesAProjectsFolders(t *testing.T) {
	project, login, own := ownHomeFixture(t)
	writeSkill(t, filepath.Join(project, ".codeaf", "skills", "repo-skill"), "repo-skill")
	writeSkill(t, filepath.Join(project, ".claude", "skills", "claude-kept"), "claude-kept")
	found, err := Discover(Options{ProjectDir: project, HomeDir: login, OwnHomeDir: own})
	if err != nil {
		t.Fatal(err)
	}
	if skill, ok := byDir(found, filepath.Join(project, ".codeaf", "skills", "repo-skill")); !ok || skill.Scope != ScopeProject || skill.Shadowed {
		t.Fatalf("a repository's own .codeaf/skills is missing or wrong: %+v", found)
	}
	// The project's copy still outranks the login home's, as it always did.
	if loser, ok := byDir(found, filepath.Join(login, ".claude", "skills", "claude-kept")); !ok || !loser.Shadowed {
		t.Fatalf("the login home's copy of a project skill's name is not shadowed: %+v", found)
	}
}

// Opened in the login home itself, the home folds into the project as it
// always did, and codeaf's own folder, which is somewhere else, is still read.
func TestOwnHomeDirWhenOpenedInTheLoginHome(t *testing.T) {
	_, login, own := ownHomeFixture(t)
	found, err := Discover(Options{ProjectDir: login, HomeDir: login, OwnHomeDir: own})
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{
		filepath.Join(login, ".claude", "skills", "claude-kept"),
		filepath.Join(login, ".config", "goose", "skills", "goose-kept"),
		filepath.Join(own, ".codeaf", "skills", "app-canvas"),
	} {
		count := 0
		for _, skill := range found {
			if skill.Dir == suffix {
				count++
				if skill.Shadowed {
					t.Errorf("the only copy of %s is shadowed", suffix)
				}
			}
		}
		if count != 1 {
			t.Errorf("%s was reported %d times with codeaf opened in the login home: %+v", suffix, count, found)
		}
	}
	if skill, ok := byDir(found, filepath.Join(own, ".codeaf", "skills", "app-canvas")); ok && skill.Scope != ScopeUser {
		t.Errorf("codeaf's own folder is scope %q, want %q", skill.Scope, ScopeUser)
	}
}

// What a remembered scan depends on follows what the scan reads, so an edit to
// the app's own folder re-reads and an edit to the command line's does not.
func TestInputPathsNameTheFoldersOwnHomeDirMoves(t *testing.T) {
	project, login, own := ownHomeFixture(t)
	moved := InputPaths(Options{ProjectDir: project, HomeDir: login, OwnHomeDir: own})
	if !slices.Contains(moved, filepath.Join(own, ".codeaf", "skills")) {
		t.Errorf("the app's own skills folder is not an input: %v", moved)
	}
	if slices.Contains(moved, filepath.Join(login, ".codeaf", "skills")) {
		t.Errorf("the command line's skills folder is an input of a scan that does not read it: %v", moved)
	}
	if !slices.Contains(moved, filepath.Join(login, ".claude", "skills")) {
		t.Errorf("another program's folder in the login home is not an input: %v", moved)
	}
	plain := InputPaths(Options{ProjectDir: project, HomeDir: login})
	if !slices.Contains(plain, filepath.Join(login, ".codeaf", "skills")) {
		t.Errorf("without OwnHomeDir the login home's .codeaf/skills is not an input: %v", plain)
	}
}

// THE APP'S OWN SHELF IS <state>/.codeaf/skills. The desktop app writes its
// canvas skill to <state>/.codeaf/skills/workaf-canvas/SKILL.md, and a scan
// that moved codeaf's folder anywhere else, <state>/skills among them, which is
// the shelf of promoted commands, would stop seeing it.
func TestOwnHomeDirKeepsTheAppsCanvasSkillWhereTheAppWritesIt(t *testing.T) {
	project, login, own := ownHomeFixture(t)
	canvas := filepath.Join(own, ".codeaf", "skills", "workaf-canvas")
	writeSkill(t, canvas, "workaf-canvas")
	writeSkill(t, filepath.Join(own, "skills", "promoted-shelf"), "promoted-shelf")
	found, err := Discover(Options{ProjectDir: project, HomeDir: login, OwnHomeDir: own})
	if err != nil {
		t.Fatal(err)
	}
	skill, ok := byDir(found, canvas)
	if !ok || skill.Scope != ScopeUser || skill.Root != RootCodeaf || skill.Shadowed || skill.Name != "workaf-canvas" {
		t.Fatalf("the canvas skill at %s is missing or wrong (%+v) in %+v", canvas, skill, found)
	}
	if stray, ok := byDir(found, filepath.Join(own, "skills", "promoted-shelf")); ok {
		t.Fatalf("the promoted shelf was read as a skills folder: %+v", stray)
	}
}
