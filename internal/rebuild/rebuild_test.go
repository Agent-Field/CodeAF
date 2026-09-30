package rebuild

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func tree(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x==1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func nothingSkipped(string) bool { return false }

func TestScanDoesNotEnterAnInstallFolder(t *testing.T) {
	root := tree(t, "package-lock.json", "node_modules/a/package-lock.json", "node_modules/a/node_modules/b/x", "web/node_modules/c/x", "src/main.go")
	got := Scan(root, nil, Found{}, nothingSkipped)
	if !reflect.DeepEqual(got.Folders, []string{"node_modules", "web/node_modules"}) || !reflect.DeepEqual(got.Locks, []string{"package-lock.json"}) {
		t.Fatalf("found %+v: a folder is one entry, and what lies inside it is not looked at", got)
	}
}

func TestScanOfChangedPathsKeepsWhatWasKnown(t *testing.T) {
	root := tree(t, "package-lock.json", "web/package-lock.json")
	before := Found{Folders: []string{"node_modules"}, Locks: []string{"package-lock.json", "gone/yarn.lock"}}
	got := Scan(root, []string{"web/node_modules/x/i.js", "web/package-lock.json", "a.go"}, before, nothingSkipped)
	if !reflect.DeepEqual(got.Folders, []string{"node_modules", "web/node_modules"}) || !reflect.DeepEqual(got.Locks, []string{"package-lock.json", "web/package-lock.json"}) {
		t.Fatalf("found %+v", got)
	}
}

func TestScanSkipsWhatIsNotInTheSeal(t *testing.T) {
	root := tree(t, "node_modules/x", "vendor/y", "ignored/node_modules/z")
	got := Scan(root, nil, Found{}, func(rel string) bool { return rel == "ignored" })
	if !reflect.DeepEqual(got.Folders, []string{"node_modules", "vendor"}) {
		t.Fatalf("found %+v", got)
	}
}

func TestLockForLooksInTheParentThenEachAncestor(t *testing.T) {
	root := tree(t, "yarn.lock", "desktop/pnpm-lock.yaml", "a/b/c/x")
	for folder, want := range map[string]string{
		"desktop/node_modules": "desktop/pnpm-lock.yaml",
		"a/b/node_modules":     "yarn.lock",
		"node_modules":         "yarn.lock",
	} {
		if got, ok := LockFor(root, folder, nil); !ok || got != want {
			t.Errorf("LockFor(%q) = %q, %v; want %q", folder, got, ok, want)
		}
	}
	if _, ok := LockFor(root, "desktop/target", nil); ok {
		t.Error("a Cargo lock was found where there is none")
	}
	if _, ok := LockFor(root, "src", nil); ok {
		t.Error("a folder that is not an install folder has a lock")
	}
}

func TestALockThatDoesNotTravelIsNoLock(t *testing.T) {
	root := tree(t, "package-lock.json")
	if _, ok := LockFor(root, "node_modules", func(string) bool { return false }); ok {
		t.Fatal("a lock the seal does not carry licensed a folder")
	}
}

func TestACargoLockCountsOnlyBesideItsManifest(t *testing.T) {
	root := tree(t, "Cargo.lock")
	if _, ok := LockFor(root, "target", nil); ok {
		t.Fatal("a Cargo.lock with no Cargo.toml licensed target/")
	}
	root = tree(t, "Cargo.lock", "Cargo.toml")
	if lock, ok := LockFor(root, "target", nil); !ok || lock != "Cargo.lock" {
		t.Fatalf("LockFor = %q, %v", lock, ok)
	}
}

func TestAllPinned(t *testing.T) {
	for text, want := range map[string]bool{
		"":                              false,
		"# nothing\n":                   false,
		"requests==2.0\nflask == 3.1\n": true,
		"requests>=2\n":                 false,
		"requests==2.0\nflask\n":        false,
		"-r base.txt\n":                 false,
		"pkg[extra]==1.0 # why\n":       true,
	} {
		if got := allPinned(text); got != want {
			t.Errorf("allPinned(%q) = %v", text, got)
		}
	}
}

func TestHintNamesTheUsualCommand(t *testing.T) {
	for lock, want := range map[string]string{"web/package-lock.json": "npm ci", "uv.lock": "uv sync", "Cargo.lock": "cargo build", "nothing.lock": ""} {
		if got := Hint(lock); got != want {
			t.Errorf("Hint(%q) = %q, want %q", lock, got, want)
		}
	}
}

func TestTrackedSeesFilesGitHoldsAndTreatsNoRepositoryAsNone(t *testing.T) {
	root := tree(t, "node_modules/x/i.js", "other/y")
	if Tracked(root, "node_modules") {
		t.Fatal("a folder outside any repository is tracked")
	}
}
