package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The bug this pins: a machine that had never run aforge had no ~/.aforge, and
// SQLite makes database files but never the directories holding them — so the
// very first launch on a new machine could not open a brain at all, and said so
// in the one wording nobody could act on.
func TestOpenMakesItsDirectoryOnAFirstRun(t *testing.T) {
	fresh := filepath.Join(t.TempDir(), ".aforge", "graph.db")
	brain, err := Open(fresh)
	if err != nil {
		t.Fatalf("a first run could not open its own store: %v", err)
	}
	defer brain.Close()
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("the store file is not there after a successful open: %v", err)
	}
	// It is a working store and not merely a file: the spine root is what a new
	// database is created with, so reading it back proves the schema ran.
	if _, found, err := brain.Node(RootID); err != nil || !found {
		t.Fatalf("a store opened on a first run has no spine root: found=%v, err=%v", found, err)
	}
}

// A second launch finds its own directory and does not mind.
func TestOpenIsHappyWhenTheDirectoryIsAlreadyThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".aforge", "graph.db")
	first, err := Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	first.Close()
	second, err := Open(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	second.Close()
}

// The sentence a person reads when the store genuinely will not open has to
// name the file and say what is actually wrong with it. SQLite's own answer for
// every one of these is "unable to open database file: out of memory (14)",
// which sends the reader looking for a memory leak on a machine with plenty.
func TestAnUnwritableDirectorySaysWhichFileAndWhy(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions, so there is nothing here to refuse")
	}
	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(locked, "graph.db")
	brain, err := Open(path)
	if err == nil {
		brain.Close()
		t.Fatal("a store in a directory nobody may write to opened anyway")
	}
	said := err.Error()
	if !strings.Contains(said, path) {
		t.Fatalf("the sentence does not name the file: %q", said)
	}
	if !strings.Contains(said, "permission denied") {
		t.Fatalf("the sentence does not say what is wrong: %q", said)
	}
	if strings.Contains(said, "out of memory") {
		t.Fatalf("the sentence still blames memory: %q", said)
	}
}

// A directory that cannot be made at all — because a FILE is standing where it
// should be — is the same kind of answer, and must not be SQLite's wording
// either.
func TestADirectoryBlockedByAFileSaysSoPlainly(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "state")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	brain, err := Open(filepath.Join(blocker, "graph.db"))
	if err == nil {
		brain.Close()
		t.Fatal("a store under a plain file opened anyway")
	}
	said := err.Error()
	if !strings.Contains(said, "could not open ") || !strings.Contains(said, "graph.db") {
		t.Fatalf("the sentence does not name the file: %q", said)
	}
	if strings.Contains(said, "out of memory") {
		t.Fatalf("the sentence still blames memory: %q", said)
	}
}

// The path in the sentence is spelled the way the person would write it down,
// because `/home/them/.aforge/graph.db` is a path they have to translate and
// `~/.aforge/graph.db` is one they can paste.
func TestThePathIsSpelledTheWayAPersonWouldWriteIt(t *testing.T) {
	house := t.TempDir()
	t.Setenv("HOME", house)
	if got, want := storePathForPerson(filepath.Join(house, ".aforge", "graph.db")), "~/.aforge/graph.db"; got != want {
		t.Fatalf("shortened path: got %q, want %q", got, want)
	}
	if got, want := storePathForPerson(house), "~"; got != want {
		t.Fatalf("the home directory itself: got %q, want %q", got, want)
	}
	// Anything outside the home directory is left exactly as it is — a path
	// somewhere else is not clearer for having a tilde stuck to the front.
	if got, want := storePathForPerson("/srv/brains/graph.db"), "/srv/brains/graph.db"; got != want {
		t.Fatalf("a path elsewhere: got %q, want %q", got, want)
	}
}

// The probe that finds the true reason must never leave a database behind: a
// zero-byte graph.db sitting where the failure happened is a file the NEXT
// launch would open and believe.
func TestAFailedOpenLeavesNothingBehind(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "state")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(blocker, "graph.db")); err == nil {
		t.Fatal("expected the open to fail")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state" {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("the failed open left things behind: %v", names)
	}
}
