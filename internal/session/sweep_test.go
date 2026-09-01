package session

// THE SWEEP, AND THE ONE THING IT MAY NEVER DO. Droppings expire, litter is
// reaped, and a real conversation's transcript is forever — the last of which is
// the reason the other two are allowed to exist at all.

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/furrow"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// A conversation opened in a temp directory and abandoned for a week is litter:
// the folder goes, and its worktree registration comes out of the repository
// FIRST, so the person is not left with a repository pointing at paths that are
// gone.
func TestTheSweepReapsATempWorkspaceNobodyCameBackTo(t *testing.T) {
	realRoot := t.TempDir()
	root := filepath.Join(t.TempDir(), "projects")
	if err := os.Symlink(realRoot, root); err != nil {
		t.Fatal(err)
	}
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

	held, _, err := openSessionFile(filepath.Join(dir, placeTranscript), dir, "test-model", "")
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
	held, _, err := openSessionFile(filepath.Join(dir, placeTranscript), dir, "test-model", "")
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

// ── rule 4: the ambient side's own litter ───────────────────────────────────

// AN ERRAND THAT CAME TO NOTHING IS THE ONE FOLDER UNDER exchanges/ NOBODY WILL
// OPEN AGAIN. One that became something has already been moved out from under
// this directory, one from this morning is a sentence somebody may still come
// back to, and one somebody is typing into right now is held by a lock.
func TestTheSweepReapsAnErrandThatCameToNothing(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	long := now.Add(-30 * 24 * time.Hour)

	dead := newSweptExchange(t, root, "aaaa0000aaaa0000", long)
	fresh := newSweptExchange(t, root, "bbbb1111bbbb1111", now.Add(-2*time.Hour))
	live := newSweptExchange(t, root, "cccc2222cccc2222", long)

	held, _, err := openSessionFile(filepath.Join(live, placeTranscript), live, "test-model", "")
	if err != nil {
		t.Fatalf("open the transcript: %v", err)
	}
	defer held.Close()

	SweepStanding(root, now, func(line string) { t.Fatalf("the sweep failed: %s", line) })

	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Fatalf("the week-old errand survived (%v)", err)
	}
	for _, kept := range []string{fresh, live} {
		if _, err := os.Stat(filepath.Join(kept, placeTranscript)); err != nil {
			t.Fatalf("%s lost its transcript: %v", kept, err)
		}
	}
}

// A RUN IS REAPED ON ITS OWN WORD AND ON NOTHING ELSE. The marker a firing
// leaves says what it came to; only "nothing" may go, and only after the same
// week. Everything a person could want — a line that was said, work that
// landed, a run waiting for them — keeps its folder like any other session, and
// so does a run that never said what it came to.
func TestTheSweepReapsOnlyRunsThatDeliveredNothing(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	long := now.Add(-30 * 24 * time.Hour)
	item := "0f1e2d3c4b5a6978"

	quiet := newSweptRun(t, root, item, "0001", standing.OutcomeNothing, long)
	landed := newSweptRun(t, root, item, "0002", "landed", long)
	needs := newSweptRun(t, root, item, "0003", "needs-you", long)
	silent := newSweptRun(t, root, item, "0004", "", long)
	recent := newSweptRun(t, root, item, "0005", standing.OutcomeNothing, now.Add(-2*time.Hour))

	// The item's own document and the day's ledger sit beside the runs and are
	// not directories: rule 4 walks folders and never files.
	writeFile(t, filepath.Join(root, item+".json"), `{"schema":1,"id":"`+item+`"}`+"\n")
	writeFile(t, filepath.Join(root, "wake.log"), "woke\n")

	SweepStanding(root, now, func(line string) { t.Fatalf("the sweep failed: %s", line) })

	if _, err := os.Stat(quiet); !os.IsNotExist(err) {
		t.Fatalf("a week-old run that delivered nothing survived (%v)", err)
	}
	for _, kept := range []string{landed, needs, silent, recent} {
		if _, err := os.Stat(filepath.Join(kept, placeTranscript)); err != nil {
			t.Fatalf("%s was reaped: %v", kept, err)
		}
	}
	for _, kept := range []string{item + ".json", "wake.log"} {
		if _, err := os.Stat(filepath.Join(root, kept)); err != nil {
			t.Fatalf("%s was swept: %v", kept, err)
		}
	}
}

// AND IT CANNOT REACH ANYTHING ELSE ON THE MACHINE. Rule 4 is arithmetic on the
// root it was handed: no root is no pass at all, and a projects tree sitting
// beside the standing one — the very thing rule 3 protects — is not read, let
// alone written.
func TestTheStandingSweepTouchesNothingOutsideItsRoot(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "standing")
	projects := filepath.Join(home, "projects")
	now := time.Now()
	long := now.Add(-400 * 24 * time.Hour)

	session := newSweptSession(t, projects, "-home-someone-project", "3333cccc3333cccc", Meta{
		ID:         "3333cccc3333cccc",
		Workspace:  "/home/someone/project",
		LastUserAt: long,
	})
	if err := os.Chtimes(session, long, long); err != nil {
		t.Fatal(err)
	}
	// An errand old enough to reap, so the pass has something to do and the
	// proof is not "it did nothing anywhere".
	dead := newSweptExchange(t, root, "dddd3333dddd3333", long)

	// No root is no pass: nothing is walked and nothing is said.
	SweepStanding("", now, func(line string) { t.Fatalf("an empty root swept something: %s", line) })
	SweepStanding("   ", now, func(line string) { t.Fatalf("a blank root swept something: %s", line) })
	if _, err := os.Stat(dead); err != nil {
		t.Fatalf("an empty root reached the standing side anyway: %v", err)
	}

	SweepStanding(root, now, func(line string) { t.Fatalf("the sweep failed: %s", line) })

	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Fatalf("the week-old errand survived (%v)", err)
	}
	if _, err := os.Stat(filepath.Join(session, placeTranscript)); err != nil {
		t.Fatalf("rule 4 reached a conversation's transcript: %v", err)
	}
}

// A standing root that is not there is a machine with nothing standing, and the
// sweep says nothing about it.
func TestTheStandingSweepIsQuietAboutAMachineWithNothingStanding(t *testing.T) {
	SweepStanding(filepath.Join(t.TempDir(), "never-created"), time.Now(), func(line string) {
		t.Fatalf("the sweep complained about an empty machine: %s", line)
	})
}

// newSweptExchange writes one `ask here` folder the way home would have left
// it, with everything in it stamped at the given moment.
func newSweptExchange(t *testing.T, root, id string, at time.Time) string {
	t.Helper()
	dir := filepath.Join(standing.ExchangesRoot(root), id)
	writeFile(t, filepath.Join(dir, placeTranscript), `{"kind":"header","id":"`+id+`"}`+"\n")
	stampTree(t, dir, at)
	return dir
}

// newSweptRun writes one firing's run folder, with the marker the ticker leaves
// saying what it came to. An empty word writes no marker at all, which is every
// run this build wrote before the marker existed.
func newSweptRun(t *testing.T, root, item, number, cameTo string, at time.Time) string {
	t.Helper()
	dir := filepath.Join(root, item, "runs", number)
	writeFile(t, filepath.Join(dir, placeTranscript), `{"kind":"header","id":"`+number+`"}`+"\n")
	if cameTo != "" {
		writeFile(t, filepath.Join(dir, standing.CameTo), cameTo+"\n")
	}
	stampTree(t, dir, at)
	return dir
}

// stampTree puts one moment on a folder and everything in it, so a test can say
// "nobody has touched this since" without waiting a week.
func stampTree(t *testing.T, dir string, at time.Time) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, at, at)
	})
	if err != nil {
		t.Fatalf("stamp %s: %v", dir, err)
	}
}

// ── the forks of a session nobody landed (issue #195) ───────────────────────

// A SWEPT SESSION'S FORKS ARE FORGOTTEN, NOT JUST DELETED. A task that came home
// dropped its own record on the way past; a session killed mid-run never got the
// chance, so what was left in `furrow forks` was a line describing a directory
// the sweep had already removed — a dangling record in the person's own project,
// which is exactly the litter [reapSession] exists to prevent.
//
// The claim is asserted through furrow's own listing rather than through
// anything the sweep says, because "the record is gone" is the whole promise and
// a drop that was merely ASKED FOR is not it.
func TestTheSweepTellsFurrowToForgetASweptSessionsForks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projects")
	now := time.Now()
	repo := dirtyRepo(t)
	installFakeFurrow(t)

	litter, tree := newSweptUniverseSession(t, root, "dddd1111dddd1111", repo, now)
	if names := forkNames(t, repo); len(names) != 1 || names[0] != tree.universe {
		t.Fatalf("before the sweep furrow holds %v, want the one fork %q", names, tree.universe)
	}

	var said []string
	SweepPlaces(root, now, func(line string) { said = append(said, line) })

	if _, err := os.Stat(litter); !os.IsNotExist(err) {
		t.Fatalf("the litter session is still there (%v)", err)
	}
	if names := forkNames(t, repo); len(names) != 0 {
		t.Fatalf("furrow still holds a record of %v, pointing at a directory the sweep removed", names)
	}
	// AND THE LOG NAMES WHAT WENT. After this pass nothing on the machine knows
	// the fork's name, so a line that did not carry it would leave an operator
	// with no way to tell a drop from a miss.
	if !saidSomethingAbout(said, tree.universe) {
		t.Fatalf("the sweep said %q and never named the fork it dropped", said)
	}
}

// AND A DROP IT CANNOT MAKE IS A LINE IN THE LOG AND NEVER A HELD-UP REMOVAL.
// furrow gone from the machine is the ordinary way this happens — the ground
// deleted or detached is the same shape — and the session folder is litter
// either way, so the reap goes through and the miss is written down.
func TestASweptSessionIsReapedEvenWhenFurrowCannotForgetItsFork(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projects")
	now := time.Now()
	repo := dirtyRepo(t)
	installFakeFurrow(t)

	litter, tree := newSweptUniverseSession(t, root, "dddd2222dddd2222", repo, now)
	// furrow leaves the machine between the run and the sweep: the record it is
	// keeping is now out of reach from here, whatever it says.
	t.Setenv(furrow.BinaryEnvVar, filepath.Join(t.TempDir(), "no-furrow-here"))
	furrow.Forget()

	var said []string
	SweepPlaces(root, now, func(line string) { said = append(said, line) })

	if _, err := os.Stat(litter); !os.IsNotExist(err) {
		t.Fatalf("a fork that could not be forgotten held up the reap (%v)", err)
	}
	if !saidSomethingAbout(said, tree.universe) {
		t.Fatalf("the sweep said %q and swallowed the miss", said)
	}
}

// THE SAME PROMISE AGAINST THE PROGRAM ITSELF, gated exactly as the ground
// ladder's own real-furrow test is (groundladder_test.go's [realFurrowEnvVar]):
// this package's TestMain forbids answers that depend on what a machine has
// installed, so the fake carries the claim everywhere and this carries the one
// thing a fake cannot — that furrow really does forget the fork.
//
//	AFORGE_FURROW_REAL=$(which furrow) go test ./internal/session/ -run RealFurrow
func TestARealFurrowForgetsASweptSessionsFork(t *testing.T) {
	binary := strings.TrimSpace(os.Getenv(realFurrowEnvVar))
	if binary == "" {
		t.Skip("set " + realFurrowEnvVar + " to a furrow binary to run this against the real program")
	}
	root := filepath.Join(t.TempDir(), "projects")
	now := time.Now()
	repo := dirtyRepo(t)
	t.Setenv(furrow.BinaryEnvVar, binary)
	furrow.Forget()
	t.Cleanup(furrow.Forget)

	litter, tree := newSweptUniverseSession(t, root, "dddd3333dddd3333", repo, now)
	if names := forkNames(t, repo); len(names) == 0 {
		t.Fatalf("the real furrow reports no fork of %s at all, so there is nothing to forget", repo)
	}

	SweepPlaces(root, now, func(line string) { t.Logf("sweep said: %s", line) })

	if _, err := os.Stat(litter); !os.IsNotExist(err) {
		t.Fatalf("the litter session is still there (%v)", err)
	}
	for _, name := range forkNames(t, repo) {
		if name == tree.universe {
			t.Fatalf("the real furrow still holds a record of %q", name)
		}
	}
}

// newSweptUniverseSession is a session with one universe-grounded node, KILLED
// MID-RUN: the ladder really carves the world, and the checkpoint left on disk
// is the one a process that died would have left — a node still running, and the
// four ground fields [TaskNode.setTree] writes onto it.
//
// It is seeded rather than run because the defect is about what SURVIVES a run
// that nobody finished, and a landing would have dropped the record itself.
func newSweptUniverseSession(t *testing.T, root, id, repo string, now time.Time) (string, taskTree) {
	t.Helper()
	dir := newSweptSession(t, root, "-tmp-scratch", id, Meta{
		ID:         id,
		Workspace:  repo,
		LaunchDir:  filepath.Join(os.TempDir(), "scratch"),
		LastUserAt: now.Add(-30 * 24 * time.Hour),
	})
	tree, err := prepareTaskTree(Place{Dir: dir}, repo, id, 1, "work in a world of its own")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if tree.rung != GroundRungUniverse || strings.TrimSpace(tree.universe) == "" {
		t.Fatalf("rung = %q with universe %q, want a fork to reap", tree.rung, tree.universe)
	}
	writeCheckpoint(t, (Place{Dir: dir}).Tasks(), taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 1,
		Nodes: []taskRecord{{
			ID:         1,
			Title:      "work in a world of its own",
			Brief:      "do the thing in the fork",
			Acceptance: "the thing is done",
			State:      TaskRunning,
			Ground:     tree.ground,
			Mode:       tree.mode,
			Rung:       tree.rung,
			Seal:       tree.seal,
			Base:       tree.base,
			Universe:   tree.universe,
			Worktree:   tree.dir,
			Branch:     tree.branch,
		}},
	})
	return dir, tree
}

// saidSomethingAbout reports that one of the sweep's lines named a thing. The
// wording of a log line is nobody's contract; that it carries the name is.
func saidSomethingAbout(said []string, name string) bool {
	for _, line := range said {
		if strings.Contains(line, name) {
			return true
		}
	}
	return false
}
