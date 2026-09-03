package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// ── NO MACHINERY VOCABULARY IN THE TERMINAL READER ─────────────────────────
//
// `aforge why <id>` and `aforge rebuild` are the two commands whose whole job is
// to hand a person a record, and both handed them a word out of the code
// instead. The record's own notes were signed `the harness` — which names
// neither who wrote the line nor what happened, and spends on the machinery a
// word this product already uses for the saved shapes of work a person builds
// by name. The rebuild receipt counted `nodes`, which are `steps` in the `--json`
// envelope, on the task page, and everywhere else a person is shown a count of
// the same things.

// A NOTE IN THE RECORD IS SIGNED `aforge`, THROUGH THE REAL DOOR.
//
// The store is written and then read back by runWhyTo, rather than the headline
// function being called directly, because the defect is what a person sees after
// typing the command and every layer between here and there is part of that.
func TestTheTurnRecordSignsItsOwnNotesWithTheProductsName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "why-note.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Brief: "read the file", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "read the file"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordTranscript("task-1", "a/model", []store.TranscriptEntry{
		{Turn: 4, Kind: store.TranscriptAssistant, Text: "I will read the file."},
		{Turn: 4, Kind: store.TranscriptNote, Text: "compacted 18 turns to stay inside the window"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	var printed bytes.Buffer
	if err := runWhyTo([]string{"task-1", "--db", path}, &printed, time.Now()); err != nil {
		t.Fatalf("aforge why task-1: %v", err)
	}
	record := printed.String()
	if strings.Contains(record, "harness") {
		t.Fatalf("`aforge why` signs a note with machinery vocabulary:\n%s\n"+
			"  a note is aforge writing about the run; it is signed %q", record, "turn 4 · aforge")
	}
	if !strings.Contains(record, "turn 4 · aforge") {
		t.Fatalf("`aforge why` does not say who wrote the note:\n%s\n  want a headline reading %q",
			record, "turn 4 · aforge")
	}
	// AND THE NOTE'S OWN WORDS ARE STILL UNDER IT. A headline nobody can read a
	// body under is a signature on an empty page.
	if !strings.Contains(record, "compacted 18 turns") {
		t.Fatalf("the note's body went missing from the record:\n%s", record)
	}
}

// THE REBUILD RECEIPT COUNTS STEPS.
func TestTheRebuildReceiptCountsStepsAndNotNodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rebuild-words.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "do the thing", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "do the thing"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	var printed bytes.Buffer
	if err := runRebuildWith([]string{"--db", path, "--yes"}, strings.NewReader(""), &printed); err != nil {
		t.Fatalf("aforge rebuild: %v", err)
	}
	receipt := strings.TrimSpace(printed.String())
	if strings.Contains(receipt, "nodes") {
		t.Fatalf("`aforge rebuild` counts its work in machinery vocabulary: %q\n"+
			"  the same pieces of work are `steps` in the --json envelope and on the task page", receipt)
	}
	if !strings.Contains(receipt, "steps") {
		t.Fatalf("`aforge rebuild` says nothing about what it rebuilt: %q\n"+
			"  want a receipt reading `rebuilt N steps from M journaled events`", receipt)
	}
}

// ── THE EMPTINESS LAW REACHES THE WAKE RECEIPT ─────────────────────────────
//
// `aforge wake` printed all eight of its figures every time, so an ordinary
// quiet pass read `examined 3, checked 2, fired 1, no 0, errors 0, rail waits 0,
// practice 0, learning 2` — four numbers asserting a measurement where nothing
// had happened. Unknown or zero draws NOTHING; the one sanctioned exception is
// the live status line's `$0.00`, which exists so a status segment does not jump
// sideways as it redraws, and a receipt printed once is not that.

func TestTheWakeReceiptSaysOnlyWhatHappened(t *testing.T) {
	said := wakePassWords(resident.WatchPass{
		Examined: 3, Checked: 2, Fired: 1,
	}, 0, 2)
	if said != "examined 3, checked 2, fired 1, learning 2" {
		t.Fatalf("a quiet pass reported %q\n  want %q\n"+
			"  every clause it dropped was a zero, and a zero is a measurement nobody took",
			said, "examined 3, checked 2, fired 1, learning 2")
	}

	// EVERY CLAUSE STILL APPEARS WHEN IT HAS SOMETHING TO SAY. A receipt that
	// dropped a count it should have printed would pass the row above, so each of
	// the eight is driven on its own with one thing in it.
	for _, only := range []struct {
		what string
		pass resident.WatchPass
		// practice and learning are counted off the journal rather than the
		// pass, so they are their own two arguments and their own two rows.
		practice, learning int
		want               string
	}{
		{what: "examined", pass: resident.WatchPass{Examined: 7}, want: "examined 7"},
		{what: "checked", pass: resident.WatchPass{Checked: 7}, want: "checked 7"},
		{what: "fired", pass: resident.WatchPass{Fired: 7}, want: "fired 7"},
		{what: "no", pass: resident.WatchPass{No: 7}, want: "no 7"},
		{what: "errors", pass: resident.WatchPass{Errors: 7}, want: "errors 7"},
		{what: "rail waits", pass: resident.WatchPass{RailWaits: 7}, want: "rail waits 7"},
		{what: "practice", practice: 7, want: "practice 7"},
		{what: "learning", learning: 7, want: "learning 7"},
	} {
		if got := wakePassWords(only.pass, only.practice, only.learning); got != only.want {
			t.Fatalf("a pass whose only count was %s reported %q, want %q", only.what, got, only.want)
		}
	}
}

// AND A PASS ON WHICH NOTHING HAPPENED SAYS SO IN A SENTENCE.
//
// Eight zeroes and a reader that failed look identical, which is why the
// emptiness law asks for the sentence rather than for the line to disappear.
func TestAWakePassThatFoundNothingSaysSoRatherThanPrintingZeroes(t *testing.T) {
	said := wakePassWords(resident.WatchPass{}, 0, 0)
	if strings.ContainsRune(said, '0') {
		t.Fatalf("a pass on which nothing happened printed a figure: %q", said)
	}
	if said != "nothing was waiting to be looked at." {
		t.Fatalf("a pass on which nothing happened said %q\n  want %q",
			said, "nothing was waiting to be looked at.")
	}
}
