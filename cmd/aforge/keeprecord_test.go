package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	homepkg "github.com/Agent-Field/aforge-v2/internal/home"
)

// A FAILURE KEEPS ITS OWN EVIDENCE WITHOUT BEING ASKED.
//
// The table is the whole decision, which is why it is one function and not
// four: the only run whose private store is thrown away is the one that worked
// and was not asked to keep it. Everything else — asked on purpose, the debug
// switch, a failure, a partial, a run that never reached an outcome at all —
// leaves the record on disk, because the person who wants it only finds out
// they wanted it after the thing went wrong.
func TestAFailedHeadlessRunKeepsItsRecord(t *testing.T) {
	for _, row := range []struct {
		name      string
		asked     bool
		debugging bool
		outcome   headlessOutcome
		err       error
		keep      bool
	}{
		{name: "a clean run is deleted", keep: false},
		{name: "a clean run asked to be kept", asked: true, keep: true},
		{name: "a clean run under the debug switch", debugging: true, keep: true},
		{name: "a run that failed", outcome: headlessOutcome{status: exitFailed}, keep: true},
		{name: "a run that landed partial", outcome: headlessOutcome{status: exitPartial}, keep: true},
		{name: "a run that never reached an outcome", err: errors.New("the store would not open"), keep: true},
		{name: "a failure asked to be kept", asked: true, outcome: headlessOutcome{status: exitFailed}, keep: true},
	} {
		t.Run(row.name, func(t *testing.T) {
			got := keepPrivateStore(row.asked, row.debugging, errandSucceeded(row.outcome, row.err))
			if got != row.keep {
				t.Fatalf("keep = %v, want %v", got, row.keep)
			}
		})
	}
}

// `aforge exec` runs one leaf, and every model call it makes belongs to that
// leaf. The key is what puts the node on the call-log row: without it the whole
// run's rows name no work at all, and a person reading the record afterwards
// cannot tell an exec call from a call with nothing behind it.
func TestTheExecTaskCarriesANodeKey(t *testing.T) {
	task := execTask("count the lines in notes.txt", "be brief", "/tmp/work")
	if task.NodeKey == "" {
		t.Fatal("the exec task names no node, so every row it writes is anonymous")
	}
	if task.NodeKey != execNodeKey {
		t.Fatalf("node key = %q, want %q", task.NodeKey, execNodeKey)
	}
}

// A KEPT RECORD LIVES UNDER THE STATE ROOT, never in the operating system's
// temporary directory. The two were indistinguishable while every run's home
// died with it; they stop being indistinguishable the moment a failure keeps
// its own, because /tmp is swept by the machine and the state root is not.
func TestThePrivateStoreLivesUnderTheStateRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv(homepkg.EnvVar, root)

	path, home, ephemeral, err := headlessStore("")
	if err != nil {
		t.Fatal(err)
	}
	if !ephemeral {
		t.Fatal("a run with no --db has a private store, which is the one that is ever deleted")
	}
	runs := filepath.Join(root, "runs")
	if parent := filepath.Dir(home); parent != runs {
		t.Fatalf("the private store was made in %s, want it under %s", parent, runs)
	}
	if path != filepath.Join(home, "graph.db") {
		t.Fatalf("the journal is at %s, want it inside %s", path, home)
	}
	info, err := os.Stat(runs)
	if err != nil {
		t.Fatal(err)
	}
	// What a run keeps is the person's own prompts, replies and deliverables.
	if mode := info.Mode().Perm(); mode != 0o700 {
		t.Fatalf("runs/ is %04o, want 0700 — a kept record is nobody else's reading", mode)
	}

	// A run pointed at its own store never had a private one, and nothing about
	// where the state root is may move it.
	named := filepath.Join(t.TempDir(), "mine.db")
	got, _, ephemeral, err := headlessStore(named)
	if err != nil {
		t.Fatal(err)
	}
	if ephemeral || got != named {
		t.Fatalf("--db %s landed at %s (ephemeral=%v)", named, got, ephemeral)
	}
}

// A run's own account of itself may not contradict what ended it. Both endings
// arrive at the same branch of the watcher — the context is done either way —
// so the sentence is the only place the difference can be told, and telling
// somebody who pressed Ctrl+C that they ran out of time is a sentence they
// know to be false.
func TestARunSaysWhichOfTheTwoEndingsItGot(t *testing.T) {
	for _, row := range []struct {
		name      string
		artifacts []string
		stopped   bool
		want      string
	}{
		{name: "the wall, with nothing to show", want: "The time limit was reached before anything finished."},
		{name: "stopped, with nothing to show", stopped: true, want: "The run was stopped before anything finished."},
		{name: "the wall, over files", artifacts: []string{"/w/a.txt"},
			want: "The time limit was reached before the work was summarised. "},
		{name: "stopped, over files", artifacts: []string{"/w/a.txt"}, stopped: true,
			want: "The run was stopped before the work was summarised. "},
	} {
		t.Run(row.name, func(t *testing.T) {
			got := wallWords(row.artifacts, row.stopped)
			if !strings.HasPrefix(got, row.want) {
				t.Fatalf("the run said %q, want it to open %q", got, row.want)
			}
		})
	}

	// A watcher nobody told how to answer says the wall, which is the reading
	// every caller had before signals were routed through the context at all.
	if (&settlementWatch{}).stoppedByHand() {
		t.Fatal("a watcher with no signal to read called an ordinary wall a stop")
	}
}
