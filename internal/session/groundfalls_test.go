package session

// THE FORK THAT COULD NOT BE MADE, PAID FOR ONCE (groundfalls.go).
//
// Every test here is written from the laptop's census: ninety-three nodes over
// three days, each of which attached, sealed and forked its workspace, threw the
// fork away and took the snapshot rung — fifteen seconds in front of every
// task's first request, for a rung that never once succeeded there, and not a
// word about it anywhere.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/furrow"
)

// installRefusingFurrow is a furrow that attaches like the real one and REFUSES
// EVERY FORK in its own words, writing each subcommand it is asked for into a
// file the test reads back. The file is the proof the tests below turn on: not
// that the ladder ended on the snapshot rung — it always did — but whether
// furrow was asked anything at all on the way.
func installRefusingFurrow(t *testing.T) (asked string) {
	t.Helper()
	dir := t.TempDir()
	asked = filepath.Join(dir, "asked")
	script := filepath.Join(dir, "furrow")
	writeFile(t, script, `#!/bin/sh
if [ "$1" = "--version" ]; then echo "furrow 0.1.0"; exit 0; fi
repo="$2"
shift 3
echo "$1" >> "`+asked+`"
case "$1" in
status)
  if [ ! -d "$repo/.furrow" ]; then echo "this workspace is not watched" >&2; exit 1; fi
  echo '{"workspace":"'"$repo"'","head":"aaaabbbbcccc0001","watcher_running":true}'
  ;;
watch)
  mkdir -p "$repo/.furrow"
  echo '{"snapshot":"aaaabbbbcccc0001","workspace":"'"$repo"'"}'
  ;;
fork)
  echo "Error: the store is over its budget" >&2
  exit 1
  ;;
*)
  echo "the fake furrow was asked for $1" >&2
  exit 2
  ;;
esac
`)
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatalf("chmod the fake furrow: %v", err)
	}
	t.Setenv(furrow.BinaryEnvVar, script)
	furrow.Forget()
	t.Cleanup(furrow.Forget)
	return asked
}

// askedOf reads back what the refusing furrow was asked, one subcommand a line.
func askedOf(t *testing.T, asked string) []string {
	t.Helper()
	raw, err := os.ReadFile(asked)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(raw))
}

// THE WHOLE LAW IN ONE RUN: the first node on a ground pays for the fork and
// says why it fell; the second node on the same ground asks furrow NOTHING, gets
// the same world, and says it did not try and why.
func TestAForkThatCouldNotBeMadeIsNotTriedAgainOnThatGround(t *testing.T) {
	repo := dirtyRepo(t)
	asked := installRefusingFurrow(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	first, err := prepareTaskTree(place, repo, "abab1111abab1111", 21, "the first node")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if first.rung != GroundRungSnapshot {
		t.Fatalf("rung = %q, want the snapshot rung under a furrow that will not fork", first.rung)
	}
	if forks := strings.Count(strings.Join(askedOf(t, asked), " "), "fork"); forks != 1 {
		t.Fatalf("furrow was asked to fork %d times for one node, want once", forks)
	}
	climb := strings.Join(first.climb, "\n")
	if !strings.Contains(climb, "was tried and could not be made") || !strings.Contains(climb, "the store is over its budget") {
		t.Fatalf("the first node's log does not say the fork fell or why:\n%s", climb)
	}
	if !strings.Contains(climb, "its world was made in") {
		t.Fatalf("the first node's log does not say how long its world took:\n%s", climb)
	}

	before := len(askedOf(t, asked))
	second, err := prepareTaskTree(place, repo, "abab1111abab1111", 22, "the second node")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if second.rung != GroundRungSnapshot {
		t.Fatalf("rung = %q, want the snapshot rung", second.rung)
	}
	// NOT A FORK, NOT AN ATTACH, NOT A STATUS: the rung stood down before it
	// touched furrow, which is the fifteen seconds this file exists to stop
	// paying twice.
	if now := askedOf(t, asked); len(now) != before {
		t.Fatalf("furrow was asked %v for a ground whose fork is known to fall", now[before:])
	}
	climb = strings.Join(second.climb, "\n")
	if !strings.Contains(climb, "was not tried") || !strings.Contains(climb, "the store is over its budget") {
		t.Fatalf("the second node's log does not say the fork was not tried, or why:\n%s", climb)
	}
	// And the world it got is the same world: the parent's uncommitted work is in
	// it, exactly as it was for the node that paid.
	if got := readFile(t, filepath.Join(second.dir, "shared.txt")); !strings.Contains(got, "the parent's own") {
		t.Fatalf("the second child's shared.txt is %q", got)
	}
}

// UNTIL SOMETHING THAT COULD CHANGE THE ANSWER HAS CHANGED. A different furrow
// is a new question, so the rung is climbed again — and a fork that is made
// forgets the fall outright, so the memory never outlives the failure it was
// about.
func TestAFallIsForgottenWhenTheFurrowChangesAndTheForkIsMade(t *testing.T) {
	repo := worldGitCannotSee(t)
	installRefusingFurrow(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	if _, err := prepareTaskTree(place, repo, "cdcd2222cdcd2222", 23, "falls"); err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if _, fallen := universeFallAt(repo); !fallen {
		t.Fatal("the fork fell and nothing remembers it")
	}

	installFakeFurrow(t)
	if _, fallen := universeFallAt(repo); fallen {
		t.Fatal("a remembered fall still holds under a different furrow")
	}
	tree, err := prepareTaskTree(place, repo, "cdcd2222cdcd2222", 24, "is made")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if tree.rung != GroundRungUniverse {
		t.Fatalf("rung = %q, want the fork a working furrow makes", tree.rung)
	}
	if _, ok := readUniverseFalls()[canonicalPath(repo)]; ok {
		t.Fatal("a fork that was made left its ground's old fall behind")
	}
}

// A STOP IS NOT A FALL. A task cancelled while its fork was being made fails the
// fork with its own cancellation, which says nothing about this ground — so the
// next task on it is still offered the fork.
func TestAStoppedTaskIsNotRememberedAsAFall(t *testing.T) {
	repo := dirtyRepo(t)
	installRefusingFurrow(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	stopped, stop := context.WithCancel(context.Background())
	stop()
	tree, err := prepareTaskTreeAt(stopped, place, repo, "efef3333efef3333", 25, "stopped", "", "")
	if err != nil {
		t.Fatalf("prepareTaskTreeAt: %v", err)
	}
	if tree.rung != GroundRungSnapshot {
		t.Fatalf("rung = %q, want the snapshot rung", tree.rung)
	}
	if _, fallen := universeFallAt(repo); fallen {
		t.Fatal("a stopped task's cancelled fork was remembered as a fork this ground cannot have")
	}
}

// A MACHINE WITH NO FURROW SAYS NOTHING. The rung does not apply there, which is
// not a fall and never a line: the climb holds only how long the world took.
func TestNoFurrowIsNotAFallAndNotALine(t *testing.T) {
	repo := dirtyRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	tree, err := prepareTaskTree(place, repo, "a0a04444a0a04444", 26, "no furrow")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if len(tree.climb) != 1 || !strings.HasPrefix(tree.climb[0], "its world was made in") {
		t.Fatalf("climb = %q; a rung that does not apply is not a line", tree.climb)
	}
	if _, fallen := universeFallAt(repo); fallen {
		t.Fatal("a machine with no furrow wrote a fall down")
	}
}
