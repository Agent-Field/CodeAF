package cellstore

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"testing"
)

// C5: Sealing protects both spellings of a symlinked home without planning it.
func TestW3SymlinkHomeAncestorsSealOnlyTheChat(t *testing.T) {
	base := t.TempDir()
	physical := filepath.Join(base, "physical", "person")
	profiles := filepath.Join(base, "profiles")
	for _, dir := range []string{physical, profiles} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	login := filepath.Join(profiles, "person")
	if err := os.Symlink(physical, login); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("home symlink unavailable: %v", err)
		}
		t.Fatal(err)
	}
	useHome(t, login)
	for _, tc := range []struct {
		name, workspace string
		ownFolder       bool
	}{
		{"original home", login, true},
		{"original ancestor", profiles, true},
		{"resolved home", physical, true},
		{"resolved ancestor", filepath.Dir(physical), true},
		{"project child", filepath.Join(login, "project"), false},
		{"sibling prefix", filepath.Join(profiles, "person-other"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.MkdirAll(tc.workspace, 0700); err != nil {
				t.Fatal(err)
			}
			c := newCell(t)
			ledger := &planLedger{}
			e := EngineFor(tc.workspace)
			e.DataRoot, e.Transport = t.TempDir(), ledger.over(fakePlans())
			if got := e.workspace(c) == ""; got != tc.ownFolder {
				t.Errorf("conversation-only workspace = %v, want %v", got, tc.ownFolder)
			}
			if _, err := e.Seal(t.Context(), c, TurnInfo{}); err != nil {
				t.Fatal(err)
			}
			want := tc.workspace
			if tc.ownFolder {
				want = c.Root
				if ledger.asked(tc.workspace) != 0 {
					t.Errorf("home tree was planned: %v", ledger.trees)
				}
			}
			if ledger.asked(want) == 0 {
				t.Fatalf("expected tree was not planned: %v", ledger.trees)
			}
		})
	}
}

// C5: Missing HOME still protects the passwd home without reading its contents.
func TestW3MissingHomeProtectsLoginHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix HOME/passwd fallback")
	}
	login, err := user.Current()
	if err != nil || login.HomeDir == "" {
		t.Fatalf("no OS login home: %v", err)
	}
	for _, mode := range []string{"empty", "unset"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", "")
			if mode == "unset" {
				if err := os.Unsetenv("HOME"); err != nil {
					t.Fatal(err)
				}
			}
			c := newCell(t)
			for workspace := login.HomeDir; ; workspace = filepath.Dir(workspace) {
				if EngineFor(workspace).workspace(c) != "" {
					t.Errorf("login home or ancestor was allowed: %s", workspace)
				}
				if filepath.Dir(workspace) == workspace {
					break
				}
			}
			if got := EngineFor(filepath.Join(login.HomeDir, "project")).workspace(c); got == "" {
				t.Error("project child lost its file history")
			}
		})
	}
}

// C5: Home aliases still select the chat's own folder; project children do not.
func TestR8HomeAliasesAndProjectChildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink fixture")
	}
	base := t.TempDir()
	login := filepath.Join(base, "person")
	project := filepath.Join(login, "project")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(login, alias); err != nil {
		t.Fatal(err)
	}
	useHome(t, alias)
	c := newCell(t)
	for _, p := range []string{login, alias, login + "/", filepath.Dir(login), "/"} {
		if EngineFor(p).workspace(c) != "" {
			t.Errorf("home alias was allowed: %s", p)
		}
	}
	for _, p := range []string{project, filepath.Join(alias, "project"), filepath.Join(base, "person-2")} {
		if EngineFor(p).workspace(c) == "" {
			t.Errorf("ordinary project lost saved file history: %s", p)
		}
	}
}
