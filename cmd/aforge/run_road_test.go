package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── WHICH OF `aforge run`'s TWO ROADS AN ARGUMENT TAKES ─────────────────────
//
// `aforge run` answers to one new spelling and one old one: `aforge run
// <program>` runs a saved program out of the registry, and `aforge run
// <plan.json>` is the retired spelling of `aforge plan run` and executes a
// static plan file. Something has to tell them apart, and what it used to be
// was os.Stat — a first positional that EXISTED took the plan road.
//
// That made the meaning of a command a property of the caller's shell. A saved
// program called `formatter` ran `./formatter` as a static plan in any
// directory where a file of that name happened to be sitting, and ran the saved
// program one directory up. The reading is the shape of the word now, and this
// file is that law: the same spellings, read from two directories, mean the
// same two things.

// runRoad is which door an `aforge run` invocation went through.
type runRoad string

const (
	roadProgram runRoad = "the saved-program runner"
	roadPlan    runRoad = "the retired plan runner"
)

// roadTaken runs `aforge run` and says which road it took, read off TWO signals
// that no single mistake can forge: the rename notice, which only the plan road
// prints, and the sentence each door says at its own first wall — the program
// runner asks for the input it was not given, and the plan runner tries to read
// the file. Both walls are reached before a provider key is wanted, so nothing
// here spends anything.
//
// An invocation that shows one signal and not the other is not classified as
// either road; it fails, because a road told only by its notice is the shape of
// test this file exists to not be.
func roadTaken(t *testing.T, args ...string) runRoad {
	t.Helper()
	var failed error
	said, _ := captureNotice(t, func() error {
		failed = runExecute(args)
		return failed
	})
	words := wordsOf(failed)
	saidPlan := strings.Contains(said, "`aforge run <plan.json>` is now `aforge plan run <plan.json>`")
	readAFile := strings.HasPrefix(words, "open ") || strings.HasPrefix(words, "load graph:")
	// THE PROGRAM RUNNER HAS TWO FIRST WALLS AND EITHER ONE IDENTIFIES IT.
	//
	// It used to have one — the input it was not given — and it now checks the
	// NAME before it reads the bytes, because `aforge run nosuchprogram --input
	// -` answered `the input is empty` and never mentioned the name, so the
	// person went away and fixed the thing they had got right (audit-cli row
	// 17). Both sentences are the program runner's own and neither can be
	// forged by the plan road, which never resolves a program name at all.
	askedForInput := strings.Contains(words, "this run needs its input")
	refusedTheName := strings.Contains(words, "there is no subharness called")
	switch {
	case saidPlan && readAFile:
		return roadPlan
	case said == "" && (askedForInput || refusedTheName):
		return roadProgram
	}
	t.Fatalf("`aforge run %s` went down neither road:\n  notice: %q\n  ending: %q",
		strings.Join(args, " "), strings.TrimSpace(said), words)
	return ""
}

// WHAT `aforge run <arg>` MEANS DOES NOT DEPEND ON WHERE YOU ARE STANDING.
//
// The test runs the SAME six spellings from two directories — one with files
// called `formatter` and `plan.json` sitting in it, one with neither — and
// demands the same six answers from both. A test that ran from one directory
// could not fail for the reason this defect has: the old reading was correct in
// every directory but the one where the file was.
//
// The law it pins: a bare word is a saved program's name, whatever is on disk
// beside it, and only an argument a person SPELLED as a path — a separator in
// it, a `./`, `../` or `~` in front, or an extension on the end — is a file.
// Where both readings are available the path form wins, because that is the one
// the caller typed on purpose.
func TestWhatAforgeRunMeansIsTheSameFromEveryDirectory(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())

	beside := t.TempDir()
	for _, name := range []string{"formatter", "plan.json"} {
		// Deliberately NOT a plan. The old reading took this road for a file
		// that existed; a reading that took it for a file that parses would be
		// the same defect wearing a better disguise, since whether `./formatter`
		// happens to be JSON is exactly as accidental as whether it is there.
		if err := os.WriteFile(filepath.Join(beside, name), []byte("not a plan at all\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bare := t.TempDir()

	for _, standing := range []struct {
		where string
		dir   string
	}{
		{"standing in the directory those files are in", beside},
		{"standing in a directory that has neither", bare},
	} {
		for _, spelling := range []struct {
			what string
			arg  string
			want runRoad
		}{
			{"a bare word is a saved program", "formatter", roadProgram},
			{"a bare word with no file anywhere is still a saved program", "tidy-up", roadProgram},
			{"a dot-slash is a file", "./formatter", roadPlan},
			{"a separator anywhere is a file", "plans/formatter", roadPlan},
			{"an extension is a file", "plan.json", roadPlan},
			{"a home path is a file", "~/formatter", roadPlan},
		} {
			t.Run(standing.where+", "+spelling.what, func(t *testing.T) {
				t.Chdir(standing.dir)
				if took := roadTaken(t, spelling.arg); took != spelling.want {
					t.Fatalf("`aforge run %s`, %s, ran %s\n  ran:  %s\n  want: %s\n"+
						"  the directory a command is typed in must never change what it means",
						spelling.arg, standing.where, took, took, spelling.want)
				}
			})
		}
	}
}

// AND THE OLD SPELLING STILL WORKS AND STILL SAYS SO. The point of reading the
// shape of the argument is that `aforge run plan.json` keeps executing the plan
// and keeps being told what it is called now; a rename that narrowed itself into
// silence would have broken the scripts it exists to carry.
func TestARealPlanFileStillRunsAndStillSaysItIsPlanRunNow(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	planFile := writeTestPlan(t)
	// From a directory that is not the plan's own, so the path — not the
	// neighbourhood — is what routes it.
	t.Chdir(t.TempDir())

	var failed error
	said, _ := captureNotice(t, func() error {
		failed = runExecute([]string{planFile})
		return failed
	})
	if !strings.Contains(said, "`aforge plan run <plan.json>`") {
		t.Fatalf("`aforge run <plan.json>` said %q, which never names `aforge plan run <plan.json>`",
			strings.TrimSpace(said))
	}
	// It reached the plan reader, which is the whole claim: this fixture is a
	// graph with no nodes, and that is the plan runner's own refusal.
	if got := wordsOf(failed); !strings.HasPrefix(got, "load graph:") {
		t.Fatalf("`aforge run <plan.json>` ended with %q\n  want: the plan reader's own refusal, `load graph: …`", got)
	}
}
