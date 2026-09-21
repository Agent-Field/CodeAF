package home

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/env"
)

// H1: root resolution prefers the current directory, falls back to the legacy
// directory, lets the legacy override move it, and lets CODEAF_HOME win.
func TestH1StateRootResolutionOrder(t *testing.T) {
	login := t.TempDir()
	t.Setenv("HOME", login)
	if err := os.Unsetenv(EnvVar); err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv(env.Legacy(EnvVar)); err != nil {
		t.Fatal(err)
	}
	legacy := legacyUnder(login)
	if err := os.Mkdir(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := resolve(); got != legacy {
		t.Fatalf("legacy-only root = %q, want %q", got, legacy)
	}
	current := DefaultUnder(login)
	if err := os.Mkdir(current, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := resolve(); got != current {
		t.Fatalf("current root = %q, want %q", got, current)
	}
	t.Setenv(env.Legacy(EnvVar), filepath.Join(login, "legacy-override"))
	if got := resolve(); got != filepath.Join(login, "legacy-override") {
		t.Fatalf("legacy override = %q", got)
	}
	t.Setenv(EnvVar, filepath.Join(login, "current-override"))
	if got := resolve(); got != filepath.Join(login, "current-override") {
		t.Fatalf("new override did not win: %q", got)
	}
}

// H2: adoption moves the complete legacy tree, leaves a relative compatibility
// link, and a second call is a no-op.
func TestH2AdoptionMovesTheTreeAndIsIdempotent(t *testing.T) {
	login := t.TempDir()
	oldFile := filepath.Join(legacyUnder(login), "v3", "projects", "one", "state.json")
	if err := os.MkdirAll(filepath.Dir(oldFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldFile, []byte("intact"), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, format) }
	ops := realAdoptionOS()
	adopt(login, logf, ops)
	newFile := filepath.Join(DefaultUnder(login), "v3", "projects", "one", "state.json")
	if body, err := os.ReadFile(newFile); err != nil || string(body) != "intact" {
		t.Fatalf("adopted file = %q, %v", body, err)
	}
	info, err := os.Lstat(legacyUnder(login))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("legacy path is not a symlink: %v, %v", info, err)
	}
	if target, err := os.Readlink(legacyUnder(login)); err != nil || target != ".codeaf" {
		t.Fatalf("legacy link = %q, %v", target, err)
	}
	before, _ := os.Lstat(DefaultUnder(login))
	adopt(login, logf, ops)
	after, _ := os.Lstat(DefaultUnder(login))
	if !os.SameFile(before, after) || len(logs) != 0 {
		t.Fatalf("second adoption changed the root or logged: %v", logs)
	}
}

// H2: adoption does nothing when the current root exists or the legacy root is
// a symlink, and the exported door does nothing in a test binary.
func TestH2AdoptionRefusesExistingAndLinkedRootsAndTests(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(string) error
	}{
		{"current exists", func(base string) error { return os.Mkdir(DefaultUnder(base), 0o700) }},
		{"legacy is a link", func(base string) error { return os.Symlink("elsewhere", legacyUnder(base)) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			login := t.TempDir()
			if err := test.setup(login); err != nil {
				t.Fatal(err)
			}
			adopt(login, t.Logf, realAdoptionOS())
			if _, err := os.Lstat(DefaultUnder(login)); test.name == "legacy is a link" && !os.IsNotExist(err) {
				t.Fatalf("linked legacy root was moved: %v", err)
			}
		})
	}
	login := t.TempDir()
	t.Setenv("HOME", login)
	if err := os.Mkdir(legacyUnder(login), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := Dir(); got == DefaultUnder(login) {
		t.Fatalf("test Dir unexpectedly resolved an adopted root: %q", got)
	}
	if _, err := os.Lstat(DefaultUnder(login)); !os.IsNotExist(err) {
		t.Fatalf("Dir adopted state in a test binary: %v", err)
	}
	Adopt(t.Logf)
	if _, err := os.Lstat(DefaultUnder(login)); !os.IsNotExist(err) {
		t.Fatalf("exported Adopt moved state in a test binary: %v", err)
	}
}

// H2: either environment spelling suppresses adoption, including an explicitly
// empty spelling.
func TestH2AdoptionDoesNotRunWithEitherOverrideSet(t *testing.T) {
	for _, name := range []string{EnvVar, env.Legacy(EnvVar)} {
		t.Run(name, func(t *testing.T) {
			login := t.TempDir()
			if err := os.Mkdir(legacyUnder(login), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv(name, "")
			if _, set := env.Lookup(EnvVar); !set {
				t.Fatal("test override is not recorded as set")
			}
			t.Setenv("HOME", login)
			adoptLogin(t.Logf)
			if _, err := os.Lstat(DefaultUnder(login)); !os.IsNotExist(err) {
				t.Fatalf("override %s allowed adoption: %v", name, err)
			}
		})
	}
}

// H2: a failed rename emits exactly one line and leaves resolution on the
// intact legacy root.
func TestH2FailedAdoptionLogsOnceAndPreservesFallback(t *testing.T) {
	login := t.TempDir()
	t.Setenv("HOME", login)
	if err := os.Mkdir(legacyUnder(login), 0o700); err != nil {
		t.Fatal(err)
	}
	ops := realAdoptionOS()
	ops.rename = func(string, string) error { return errors.New("rename refused") }
	var logs []string
	adopt(login, func(format string, args ...any) { logs = append(logs, format) }, ops)
	if len(logs) != 1 {
		t.Fatalf("failure logged %d lines, want one: %v", len(logs), logs)
	}
	if got := resolve(); got != legacyUnder(login) {
		t.Fatalf("root after failed adoption = %q, want legacy root", got)
	}
}

func TestARegularFileAtTheCurrentNameDoesNotHideFormerState(t *testing.T) {
	login := t.TempDir()
	t.Setenv("HOME", login)
	current := DefaultUnder(login)
	if err := os.WriteFile(current, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := legacyUnder(login)
	if err := os.Mkdir(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	var logs []string
	adopt(login, func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}, realAdoptionOS())
	if got := resolve(); got != legacy {
		t.Fatalf("state root = %q, want usable former directory %q", got, legacy)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], current) || !strings.Contains(logs[0], "not a directory") {
		t.Fatalf("unusable current root logged %v", logs)
	}
}

func realAdoptionOS() adoptionOS {
	return adoptionOS{lstat: os.Lstat, stat: os.Stat, rename: os.Rename, symlink: os.Symlink}
}

func TestDirDefaultsUnderTheUserHome(t *testing.T) {
	t.Setenv("HOME", "/tmp/pretend-home")
	t.Setenv(EnvVar, "")
	if got, want := Dir(), filepath.Join("/tmp/pretend-home", ".codeaf"); got != want {
		t.Fatalf("state root: got %q, want %q", got, want)
	}
}

func TestDirFollowsTheOverride(t *testing.T) {
	t.Setenv("HOME", "/tmp/pretend-home")
	t.Setenv(EnvVar, "/tmp/disposable/state")
	if got, want := Dir(), "/tmp/disposable/state"; got != want {
		t.Fatalf("state root: got %q, want %q", got, want)
	}
	if got, want := Join("graph.db"), "/tmp/disposable/state/graph.db"; got != want {
		t.Fatalf("joined: got %q, want %q", got, want)
	}
}

// The probe ran four conversations from four databases in /tmp, and all four
// wrote into one /tmp/workspace: every run's deliverables landed among the
// others' with nothing on disk saying which brain produced which file. Two
// stores in one directory get two workspaces, always.
func TestTwoStoresInOneDirectoryNeverShareAWorkspace(t *testing.T) {
	first, second := StoreDir("/tmp/probe-1.db", "workspace"), StoreDir("/tmp/probe-2.db", "workspace")
	if first == second {
		t.Fatalf("two stores in one directory share %q", first)
	}
	for _, want := range []string{"/tmp/probe-1-workspace", "/tmp/probe-2-workspace"} {
		if first != want && second != want {
			t.Fatalf("workspaces are not named for their stores: %q and %q", first, second)
		}
	}
	// It sits beside the store, so the kinds of a single store stay together and
	// a store's own directory is still one place a person can look.
	if got, want := StoreDir("/tmp/probe-1.db", "scratch"), "/tmp/probe-1-scratch"; got != want {
		t.Fatalf("scratch: got %q, want %q", got, want)
	}
	if got, want := StoreDir(Join("graph.db"), "workspace"), Join("graph-workspace"); got != want {
		t.Fatalf("the default store: got %q, want %q", got, want)
	}
}

// A blank override is not an override: an exported variable set to the empty
// string is how a shell says "unset" more often than it says "use the root".
func TestBlankOverrideIsIgnored(t *testing.T) {
	t.Setenv("HOME", "/tmp/pretend-home")
	t.Setenv(EnvVar, "   ")
	if got, want := Dir(), filepath.Join("/tmp/pretend-home", ".codeaf"); got != want {
		t.Fatalf("state root: got %q, want %q", got, want)
	}
}

// TestContainsComparesPathElementsAndNotPrefixes is the whole reason this is a
// function and not a strings.HasPrefix at each caller: a sibling directory
// named like the root reads as a child of it under a prefix test, and the
// callers use the answer to decide whether a file belongs to somebody else.
func TestContainsComparesPathElementsAndNotPrefixes(t *testing.T) {
	const root = "/state/root"
	for path, want := range map[string]bool{
		root:                       true,
		root + "/v3/lanes.json":    true,
		"/state/root-2/lanes.json": false,
		"/state/rootless":          false,
		"/state":                   false,
		"/elsewhere/v3/lanes.json": false,
	} {
		if got := Contains(root, path); got != want {
			t.Errorf("Contains(%q, %q) = %v, want %v", root, path, got, want)
		}
	}
	// A root nobody named contains nothing, which is what an unset override has
	// to mean to a caller that asks about both roots in turn.
	if Contains("  ", "/state/root/v3/lanes.json") {
		t.Error("an empty root claimed a file")
	}
}

// H10: the login home follows the override, follows a pinned HOME, and a test
// binary that named neither is handed the quarantine rather than the home of
// whoever ran it — the gate Dir() already applies, aimed at the container the
// inherited state root lives in, so a foreign-skill scan can never read a real
// person's dot-folders out of a throwaway store.
func TestH10LoginHomeFollowsOverrideAndQuarantinesTheInherited(t *testing.T) {
	originalHome := os.Getenv("HOME")

	named := t.TempDir()
	t.Setenv(EnvVar, named)
	got, err := Login()
	if err != nil {
		t.Fatal(err)
	}
	if got != named {
		t.Fatalf("Login with the override = %q, want %q", got, named)
	}

	login := t.TempDir()
	t.Setenv("HOME", login)
	if err := os.Unsetenv(EnvVar); err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv(env.Legacy(EnvVar)); err != nil {
		t.Fatal(err)
	}
	if got, err = Login(); err != nil || got != login {
		t.Fatalf("Login with a pinned HOME = %q err = %v, want %q", got, err, login)
	}

	// With nothing named, the inherited root was resolved at process start
	// from the login home this process started with, so putting that home
	// back and asking again has to hit the containment gate: the scan must
	// not read the dot-folders of whoever ran the test.
	if originalHome == "" {
		t.Skip("no HOME was set for this process, so there is no inherited home to quarantine")
	}
	if err := os.Setenv("HOME", originalHome); err != nil {
		t.Fatal(err)
	}
	if got, err = Login(); err != nil {
		t.Fatal(err)
	}
	if got != quarantine {
		t.Fatalf("Login with nothing named = %q, want the quarantine %q", got, quarantine)
	}
}
