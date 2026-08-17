package session

// THE SWEEP, AND THE ONE THING IT MAY NEVER DO. Droppings expire, litter is
// reaped, and a real conversation's transcript is forever — the last of which is
// the reason the other two are allowed to exist at all.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A conversation opened in a temp directory and abandoned for a week is litter:
// the folder goes, and its worktree registration comes out of the repository
// FIRST, so the person is not left with a repository pointing at paths that are
// gone.
func TestTheSweepReapsATempWorkspaceNobodyCameBackTo(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	repo := newTestRepo(t)

	litter := newSweptSession(t, root, "-tmp-scratch", "aaaa1111aaaa1111", Meta{
		ID:         "aaaa1111aaaa1111",
		Workspace:  repo,
		LaunchDir:  filepath.Join(os.TempDir(), "scratch"),
		LastUserAt: now.Add(-30 * 24 * time.Hour),
	})
	// One node's worktree, registered in the repository exactly as a run would
	// have left it.
	tree, err := prepareTaskTree(Place{Dir: litter}, repo, "aaaa1111aaaa1111", 1, "do the thing")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}

	SweepPlaces(root, now, func(line string) { t.Logf("sweep said: %s", line) })

	if _, err := os.Stat(litter); !os.IsNotExist(err) {
		t.Fatalf("the litter session is still there (%v)", err)
	}
	if list := gitOut(t, repo, "worktree", "list"); strings.Contains(list, tree.dir) {
		t.Fatalf("the repository still points at a worktree that is gone:\n%s", list)
	}
}

// AND THE ONE BESIDE IT, IN A REAL PROJECT, IS UNTOUCHED — however old it is.
// This is the law the sweep is only allowed to exist under: a transcript a
// person could go back to is forever, and no amount of idleness makes it
// otherwise.
func TestTheSweepCannotReachARealSessionsTranscript(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	real := newSweptSession(t, root, "-home-someone-project", "bbbb2222bbbb2222", Meta{
		ID:         "bbbb2222bbbb2222",
		Workspace:  "/home/someone/project",
		LaunchDir:  "/home/someone/project/cmd",
		LastUserAt: now.Add(-400 * 24 * time.Hour),
	})
	// A session with no identity at all is the other half of the law: a folder
	// that cannot say what it is is a folder the sweep may not judge.
	nameless := filepath.Join(root, "-home-someone-project", "cccc3333cccc3333")
	writeFile(t, filepath.Join(nameless, placeTranscript), "{}\n")
	old := now.Add(-400 * 24 * time.Hour)
	if err := os.Chtimes(nameless, old, old); err != nil {
		t.Fatal(err)
	}

	SweepPlaces(root, now, func(line string) { t.Logf("sweep said: %s", line) })

	for _, dir := range []string{real, nameless} {
		if _, err := os.Stat(filepath.Join(dir, placeTranscript)); err != nil {
			t.Fatalf("a transcript that must be forever is gone from %s: %v", dir, err)
		}
	}
	// The person's own content goes with the transcript, not with the droppings.
	if _, err := os.Stat(filepath.Join(real, placeWork, "notes.md")); err != nil {
		t.Fatalf("the session's work was swept: %v", err)
	}
}

// Droppings carry a seven-day TTL and NOTHING ELSE DOES. The old log goes, the
// recent one stays, and every file the sweep is forbidden to touch is still
// there afterwards — including in a session old enough to be reaped, had it been
// litter.
func TestTheSweepExpiresLogsAndNothingElse(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	dir := newSweptSession(t, root, "-home-someone-project", "dddd4444dddd4444", Meta{
		ID:         "dddd4444dddd4444",
		Workspace:  "/home/someone/project",
		LastUserAt: now.Add(-400 * 24 * time.Hour),
	})
	stale := filepath.Join(dir, placeLogs, "job-1.log")
	fresh := filepath.Join(dir, placeLogs, "job-2.log")
	writeFile(t, stale, "the build said things\n")
	writeFile(t, fresh, "the build is saying things\n")
	long := now.Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(stale, long, long); err != nil {
		t.Fatal(err)
	}
	// A transcript as old as the stale log, to prove the TTL is scoped to logs/
	// and not to age.
	if err := os.Chtimes(filepath.Join(dir, placeTranscript), long, long); err != nil {
		t.Fatal(err)
	}

	SweepPlaces(root, now, func(line string) { t.Fatalf("the sweep failed: %s", line) })

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("the week-old dropping is still there (%v)", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("a log from this morning was expired: %v", err)
	}
	for _, kept := range []string{placeTranscript, filepath.Join(placeWork, "notes.md"), placeMeta} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Fatalf("%s was swept: %v", kept, err)
		}
	}
	// The directory itself stays: the next job opens a log in it.
	if info, err := os.Stat(filepath.Join(dir, placeLogs)); err != nil || !info.IsDir() {
		t.Fatalf("logs/ was removed with its contents: %v", err)
	}
}

// A conversation somebody is having RIGHT NOW is untouchable, litter or not: the
// flock on its transcript is a live writer, and a sweep that read a temp
// workspace and an old stamp would otherwise delete a session out from under the
// window it is open in.
func TestTheSweepLeavesALiveSessionAlone(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	dir := newSweptSession(t, root, "-tmp-scratch", "eeee5555eeee5555", Meta{
		ID:         "eeee5555eeee5555",
		Workspace:  filepath.Join(os.TempDir(), "scratch"),
		LastUserAt: now.Add(-30 * 24 * time.Hour),
	})
	stale := filepath.Join(dir, placeLogs, "job-1.log")
	writeFile(t, stale, "the build said things\n")
	long := now.Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(stale, long, long); err != nil {
		t.Fatal(err)
	}

	held, _, err := openSessionFile(filepath.Join(dir, placeTranscript), dir, "test-model")
	if err != nil {
		t.Fatalf("open the transcript: %v", err)
	}
	defer held.Close()

	SweepPlaces(root, now, func(line string) { t.Logf("sweep said: %s", line) })

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("a session somebody is talking to was reaped: %v", err)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("a live session's logs were expired under it: %v", err)
	}
}

// And the same session, once the window has closed, is litter after all — which
// is what makes the check above a check on the LOCK and not on the rules.
func TestTheSweepReapsThatSessionOnceItIsClosed(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	dir := newSweptSession(t, root, "-tmp-scratch", "ffff6666ffff6666", Meta{
		ID:         "ffff6666ffff6666",
		Workspace:  filepath.Join(os.TempDir(), "scratch"),
		LastUserAt: now.Add(-30 * 24 * time.Hour),
	})
	held, _, err := openSessionFile(filepath.Join(dir, placeTranscript), dir, "test-model")
	if err != nil {
		t.Fatalf("open the transcript: %v", err)
	}
	if err := held.Close(); err != nil {
		t.Fatalf("close the transcript: %v", err)
	}

	SweepPlaces(root, now, func(line string) { t.Logf("sweep said: %s", line) })

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the closed litter session survived (%v)", err)
	}
}

// A session that is only HALF the proof stays: a temp workspace somebody used
// this morning, and an ancient conversation in a real project. Both halves are
// needed and the test says so in both directions.
func TestTheSweepNeedsBothHalvesOfTheProof(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	busy := newSweptSession(t, root, "-tmp-scratch", "1111aaaa1111aaaa", Meta{
		ID:         "1111aaaa1111aaaa",
		Workspace:  filepath.Join(os.TempDir(), "scratch"),
		LastUserAt: now.Add(-2 * time.Hour),
	})
	kept := newSweptSession(t, root, "-home-someone-project", "2222bbbb2222bbbb", Meta{
		ID:         "2222bbbb2222bbbb",
		Workspace:  "/home/someone/project",
		LastUserAt: now.Add(-400 * 24 * time.Hour),
	})

	SweepPlaces(root, now, func(line string) { t.Fatalf("the sweep failed: %s", line) })

	for _, dir := range []string{busy, kept} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("%s was reaped on half a proof: %v", dir, err)
		}
	}
}

// A projects root that is not there is a machine that has not held a
// conversation, and the sweep says nothing about it.
func TestTheSweepIsQuietAboutAMachineWithNoSessions(t *testing.T) {
	SweepPlaces(filepath.Join(t.TempDir(), "never-created"), time.Now(), func(line string) {
		t.Fatalf("the sweep complained about an empty machine: %s", line)
	})
}

// newSweptSession writes one session folder the way a launch would have left it:
// a transcript, an identity, a person's work and a droppings directory.
func newSweptSession(t *testing.T, root, bucket, id string, meta Meta) string {
	t.Helper()
	dir := filepath.Join(root, bucket, id)
	writeFile(t, filepath.Join(dir, placeTranscript), `{"kind":"header","id":"`+id+`"}`+"\n")
	writeFile(t, filepath.Join(dir, placeWork, "notes.md"), "what I was reading\n")
	if err := os.MkdirAll(filepath.Join(dir, placeLogs), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SaveMeta(dir, meta); err != nil {
		t.Fatalf("save meta: %v", err)
	}
	return dir
}
