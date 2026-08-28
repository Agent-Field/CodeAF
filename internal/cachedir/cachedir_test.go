package cachedir

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// The whole reason this package exists is a blast radius that cannot drift:
// Clean takes down the cache directory and nothing beside it, read-only
// corners included, and says how much left.
func TestCleanTakesTheCacheWholeAndNothingBesideIt(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)

	// The shape Go's module cache actually leaves behind: a read-only file
	// inside a read-only directory. This is what made a bare rm -rf fail on a
	// real machine, so it is the shape the test pins.
	locked := filepath.Join(Root(), "toolchain", "go-mod", "module@v1")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "LICENSE"), make([]byte, 2048), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	// A neighbour under the state root that is NOT cache — the stand-in for the
	// journal, the sessions, the credentials. Clean must not know it exists.
	neighbour := filepath.Join(root, "graph.db")
	if err := os.WriteFile(neighbour, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := Size(); got != 2048 {
		t.Fatalf("Size() = %d, want the 2048 bytes that were written", got)
	}
	freed, err := Clean()
	if err != nil {
		t.Fatalf("Clean() failed over a read-only module directory: %v", err)
	}
	if freed != 2048 {
		t.Fatalf("Clean() reported %d bytes freed, want 2048", freed)
	}
	if _, err := os.Stat(Root()); !os.IsNotExist(err) {
		t.Fatal("the cache directory is still there after Clean()")
	}
	if _, err := os.Stat(neighbour); err != nil {
		t.Fatalf("Clean() reached outside the cache: %v", err)
	}
}

// An absent cache is the day-one state and the state right after a clean, so
// both verbs answer it calmly: zero, and no error.
func TestAnAbsentCacheIsZeroAndNoError(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	if got := Size(); got != 0 {
		t.Fatalf("Size() over nothing = %d, want 0", got)
	}
	freed, err := Clean()
	if err != nil || freed != 0 {
		t.Fatalf("Clean() over nothing = (%d, %v), want (0, nil)", freed, err)
	}
}

// One formatter, spot-checked at its rungs — both surfaces read this string to
// a person deciding whether to delete something.
func TestHumanReadsAtEveryRung(t *testing.T) {
	cases := map[int64]string{
		0:                "0 B",
		512:              "512 B",
		1536:             "1.5 KB",
		539 * 1 << 20:    "539.0 MB",
		2761 * 1 << 20:   "2.7 GB",
		3 * 1 << 30 / 2:  "1.5 GB",
	}
	for size, want := range cases {
		if got := Human(size); got != want {
			t.Errorf("Human(%d) = %q, want %q", size, got, want)
		}
	}
}
