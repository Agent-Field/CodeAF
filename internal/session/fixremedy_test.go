package session

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── the two measured lines (fixremedy.go's header) ──────────────────────────
//
// Both of these came off one real session on 2026-08-31, whose store read
// `{asked: 17, found: 3, worked: 0}`. Every row below is that file's own
// material, and the assertion is silence.

// A REGEX IS NOT A REMEDY, however many times it was watched. The measured file
// had `/\/+$` — the pattern the next grep searched with — filed as the answer to
// a "Path not found", and offered back as something to run.
func TestARegexFragmentIsNeverOfferedAsARemedy(t *testing.T) {
	store := testStore(t, time.Now())
	signature, keyed := fixSignature("grep", "Path not found: /Users/x/code/tests/test_execution_logger.py")
	if !keyed {
		t.Fatal("the measured error has to key on something for this test to be about anything")
	}
	// Planted well past every count in the store, so nothing but the reading of
	// the patch itself can be what silences it.
	for i := 0; i < 9; i++ {
		store.confirmAdvised(signature, `/\/+$`)
	}
	if found := store.consult(signature); len(found) != 0 {
		t.Fatalf("a regex may never be offered as a command; it offered %q", found[0].Fix)
	}
}

// AND NEITHER IS A COMMAND THIS MACHINE DOES NOT HAVE. It is fixblame.go's law
// (c) read the other way round: routing around an absence is not advice, and
// advice that names the absent program is the same nothing.
func TestAPatchNamingAProgramThisMachineLacksIsNeverOffered(t *testing.T) {
	store := testStore(t, time.Now())
	signature, _ := fixSignature("bash", "ld: symbol(s) not found for architecture arm64")
	for i := 0; i < 9; i++ {
		store.confirmAdvised(signature, "aforge-no-such-program-9f3c rebuild")
	}
	if found := store.consult(signature); len(found) != 0 {
		t.Fatalf("nothing this machine could run; it offered %q", found[0].Fix)
	}
}

// ONE COMMAND AFTER ONE FAILURE IS AN ADJACENCY. The measured file answered
// `npm error Missing script: build` with `git log --oneline -5`, which is a
// perfectly good command and had nothing to do with npm — the model looked at
// the log next, and the store wrote down a cause.
//
// The store cannot tell npm from git and is never asked to. What it can tell is
// whether the world produced the pair twice, and until it has, it says nothing.
func TestACommandWatchedOnceIsNotYetOfferedAndTwiceIs(t *testing.T) {
	store := testStore(t, time.Now())
	signature, keyed := fixSignature("bash", "npm error Missing script: build")
	if !keyed {
		t.Fatal("the measured error has to key on something")
	}

	store.confirm(signature, "git log --oneline -5")
	if found := store.consult(signature); len(found) != 0 {
		t.Fatalf("one sighting is an adjacency and earns no line; it offered %q", found[0].Fix)
	}

	store.confirm(signature, "git log --oneline -5")
	found := store.consult(signature)
	if len(found) != 1 {
		t.Fatal("a pairing the world produced twice is worth the weaker line")
	}
	line := fixAnnotate("npm error Missing script: build", []fixAdvice{{
		patch:  found[0].Fix,
		ok:     found[0].ok(),
		worked: found[0].worked(),
		failed: found[0].failed(),
	}})
	want := "this exact error came up here before · what ran next and it went away: git log --oneline -5"
	if !strings.HasSuffix(line, want) {
		t.Fatalf("the wording is unchanged and says only what was watched:\n%q", line)
	}
}

// A patch that was handed back, taken, and made the error go away needs no
// second sighting: that observation is a cure and not an adjacency.
func TestAPatchThatHasWorkedIsOfferedOnItsFirstSighting(t *testing.T) {
	store := testStore(t, time.Now())
	signature, _ := fixSignature("bash", "ld: symbol(s) not found for architecture arm64")
	store.confirmAdvised(signature, "make clean && make build")
	if found := store.consult(signature); len(found) != 1 {
		t.Fatal("one taken offer that worked is evidence a hundred adjacencies are not")
	}
}

// ── through the live path ───────────────────────────────────────────────────

// The gate is on the way OUT as well as on the way in, because every laptop
// already has a store full of what the old one let through. This plants the
// measured entry by the back door and asks the belt.
func TestAStoreAlreadyFullOfJunkStopsSayingIt(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AFORGE_HOME", root)
	bucket := filepath.Join(root, "v3", "projects", "ws")
	broken := "npm error Missing script: build"
	signature, _ := fixSignature("bash", broken)

	store := newFixStore(filepath.Join(bucket, fixesFileName))
	for i := 0; i < 5; i++ {
		store.confirm(signature, `/\/+$`)
	}

	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.fixesDir = bucket
	})
	annotated := agent.newEpisode().noteToolOutcome(fixBash("npm run build"), fixFailed(broken))
	if annotated.text != broken {
		t.Fatalf("an entry nothing could run must leave the result as the tool wrote it:\n%q", annotated.text)
	}
}

// A pattern the model searched with is never written down as a fix, whichever
// hand produced it. The recording side is tightened with the same reading, so
// junk stops entering the two files at all rather than being filtered on the way
// back out forever.
func TestNothingThatCouldNotBeRunIsEverWrittenDown(t *testing.T) {
	episode, bucket := fixTestLane(t)
	broken := "ld: symbol(s) not found for architecture arm64"

	episode.noteToolOutcome(fixBash("make build"), fixFailed(broken))
	episode.noteToolOutcome(fixBash(`^(func|type) [A-Z]`), fixWorked("found it"))

	document := readFixDocument(filepath.Join(bucket, fixesFileName))
	if len(document.Entries) != 0 {
		t.Fatalf("nothing runnable happened, so nothing may be recorded; got %+v", document.Entries)
	}
}

// The reading itself, on the shapes it exists to separate.
func TestARemedyIsACommandThisMachineHas(t *testing.T) {
	for _, runnable := range []string{
		"go build ./...",
		"make clean && make build",
		"grep -F '(sub)' .",
		"true",
	} {
		if !fixRunnableRemedy(runnable) {
			t.Errorf("%q is a command anybody could type", runnable)
		}
	}
	for _, junk := range []string{
		`/\/+$`,
		"^(func|type) [A-Z]",
		"internal/session/loop.go",
		"the project answer",
		"--stdio",
		"",
		"   ",
		"aforge-no-such-program-9f3c",
	} {
		if fixRunnableRemedy(junk) {
			t.Errorf("%q is not a command this machine can run", junk)
		}
	}
}
