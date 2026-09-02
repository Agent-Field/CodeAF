package calllog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// TestATestBinaryNeverWritesIntoTheHomeItInherited is the law of undertest.go
// at the one seam that can enforce it: a test that was handed no path of its
// own gets no log, however the path would have been arrived at — the fallback
// nobody opened, an Open with no directory, and an Open on a profile directory
// that was itself derived from the inherited home, which is how a package that
// loads a config reaches the person's ledger without ever naming it.
func TestATestBinaryNeverWritesIntoTheHomeItInherited(t *testing.T) {
	inherited := t.TempDir()
	t.Setenv(home.EnvVar, inherited)
	t.Setenv(EnvVar, "")

	if got := PathFor(""); got != "" {
		t.Errorf("the inherited state root should resolve to no log; got %q", got)
	}
	if got := PathFor(filepath.Join(inherited, "profiles", "default")); got != "" {
		t.Errorf("a profile inside the inherited root should resolve to no log; got %q", got)
	}

	// And the write path agrees, which is the half that matters: 356 rows of
	// invented traffic reached a real ledger through exactly this fallback.
	fresh(t, "")
	Append(Record{Time: "2026-09-01T10:00:00.000Z", Tag: "turn", Model: "vendor/vision-model", Cost: 4.25})
	if path := Path(); path != "" {
		t.Errorf("an unopened log in a test binary should be off; got %q", path)
	}
	if _, err := os.Stat(filepath.Join(inherited, DirName, FileName)); !os.IsNotExist(err) {
		t.Fatalf("a test wrote into the ledger of whoever started it: %v", err)
	}
}

// TestATestThatSaysWhereItsLogGoesStillGetsOne is the other half, and it is why
// the gate can be this blunt: the refusal is of an INHERITED path, never of a
// path a test chose. A temporary directory of its own and the AFORGE_CALL_LOG
// pin both still work, so a test with something to assert about the log has two
// ways to say so.
func TestATestThatSaysWhereItsLogGoesStillGetsOne(t *testing.T) {
	inherited := t.TempDir()
	t.Setenv(home.EnvVar, inherited)
	t.Setenv(EnvVar, "")

	own := t.TempDir()
	fresh(t, "")
	Open(own)
	Append(Record{Time: "2026-09-01T10:00:00.000Z", Tag: "turn", Model: "sim/model"})
	if records := readLines(t, filepath.Join(own, DirName, FileName)); len(records) != 1 {
		t.Fatalf("a log the test pointed at should hold its one call; got %d", len(records))
	}

	pinned := filepath.Join(t.TempDir(), "calls.jsonl")
	t.Setenv(EnvVar, pinned)
	fresh(t, "")
	Open("")
	Append(Record{Time: "2026-09-01T10:00:01.000Z", Tag: "turn", Model: "sim/model"})
	if records := readLines(t, pinned); len(records) != 1 {
		t.Fatalf("the pin should still be where the log goes; got %d rows", len(records))
	}
}

// TestOnlyTheInheritedRootIsRefusedAndNotItsNeighbours pins the comparison to
// path elements. A prefix test would read /state/root-2 as living inside
// /state/root and switch off a log that has nothing to do with anybody's home.
func TestOnlyTheInheritedRootIsRefusedAndNotItsNeighbours(t *testing.T) {
	parent := t.TempDir()
	inherited := filepath.Join(parent, "root")
	t.Setenv(home.EnvVar, inherited)
	t.Setenv(EnvVar, "")

	neighbour := filepath.Join(parent, "root-2")
	if got, want := PathFor(neighbour), filepath.Join(neighbour, DirName, FileName); got != want {
		t.Fatalf("a directory beside the state root is not inside it: got %q, want %q", got, want)
	}
	if got := PathFor(filepath.Join(inherited, "logs-elsewhere")); got != "" {
		t.Fatalf("a directory inside the state root should resolve to no log; got %q", got)
	}
}

// TestOutsideATestTheProductResolvesExactlyAsItAlwaysHas holds the gate to test
// binaries. The product's log is always on, and a change that quietly switched
// it off for the person running aforge would trade one silent problem for a
// worse one.
func TestOutsideATestTheProductResolvesExactlyAsItAlwaysHas(t *testing.T) {
	inherited := t.TempDir()
	t.Setenv(home.EnvVar, inherited)
	t.Setenv(EnvVar, "")

	previous := underTest
	underTest = false
	defer func() { underTest = previous }()

	if got, want := PathFor(""), filepath.Join(inherited, DirName, FileName); got != want {
		t.Fatalf("the state root's log is at %q, want %q", got, want)
	}
	profile := filepath.Join(inherited, "profiles", "default")
	if got, want := PathFor(profile), filepath.Join(profile, DirName, FileName); got != want {
		t.Fatalf("the profile's log is at %q, want %q", got, want)
	}
}
