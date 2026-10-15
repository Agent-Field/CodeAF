package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/store"
)

// ── NO MACHINERY VOCABULARY IN THE TERMINAL READER ─────────────────────────
//
// `codeaf why <id>` and `codeaf rebuild` are the two commands whose whole job is
// to hand a person a record, and both handed them a word out of the code
// instead. The record's own notes were signed `the harness` — which names
// neither who wrote the line nor what happened, and spends on the machinery a
// word this product already uses for the saved shapes of work a person builds
// by name. The rebuild receipt counted `nodes`, which are `steps` in the `--json`
// envelope, on the task page, and everywhere else a person is shown a count of
// the same things.

// A NOTE IN THE RECORD IS SIGNED `codeaf`, THROUGH THE REAL DOOR.
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
		t.Fatalf("codeaf why task-1: %v", err)
	}
	record := printed.String()
	if strings.Contains(record, "harness") {
		t.Fatalf("`codeaf why` signs a note with machinery vocabulary:\n%s\n"+
			"  a note is codeaf writing about the run; it is signed %q", record, "turn 4 · codeaf")
	}
	if !strings.Contains(record, "turn 4 · codeaf") {
		t.Fatalf("`codeaf why` does not say who wrote the note:\n%s\n  want a headline reading %q",
			record, "turn 4 · codeaf")
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
		t.Fatalf("codeaf rebuild: %v", err)
	}
	receipt := strings.TrimSpace(printed.String())
	if strings.Contains(receipt, "nodes") {
		t.Fatalf("`codeaf rebuild` counts its work in machinery vocabulary: %q\n"+
			"  the same pieces of work are `steps` in the --json envelope and on the task page", receipt)
	}
	if !strings.Contains(receipt, "steps") {
		t.Fatalf("`codeaf rebuild` says nothing about what it rebuilt: %q\n"+
			"  want a receipt reading `rebuilt N steps from M journaled events`", receipt)
	}
}
