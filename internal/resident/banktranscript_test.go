package resident

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A RESTART RESUMES; IT DOES NOT START OVER.
//
// The measured failure: happy-dom, 2026-08-28. Node `task-2-x1-n2` started at
// 03:02:55, again at 03:23:10 and again at 03:43:20 — a provider call hung, the
// twenty-minute claim reaper took the node back, and the leaf that picked it up
// began with an empty context beside a workspace holding its own edits. It ran
// the project's test file seventy-one times across those three starts and
// produced nothing. Every one of its turns was in the store the whole time.

func transcriptGraph(t *testing.T) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "leaf", Brief: "make the observer deterministic", Title: "Leaf"},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "make the observer deterministic"}); err != nil {
		t.Fatal(err)
	}
	return graph
}

// TestARestartedLeafKeepsItsEarlierToolResults is defect 2. The attempt that was
// taken away had read a file, edited it and run the tests; the attempt that
// takes over must be handed all three rather than rediscovering them.
func TestARestartedLeafKeepsItsEarlierToolResults(t *testing.T) {
	graph := transcriptGraph(t)
	if err := graph.RecordTranscript("leaf", "deepseek/deepseek-v4-flash", []store.TranscriptEntry{
		{Turn: 1, Kind: store.TranscriptAssistant, Text: "I'll read the observer first."},
		{Turn: 1, Kind: store.TranscriptToolCall, Tool: "read", CallID: "c1",
			Text: `{"filePath":"src/IntersectionObserver.ts"}`},
		{Turn: 1, Kind: store.TranscriptToolResult, Tool: "read", CallID: "c1",
			Text: "export class IntersectionObserver {", Millis: 12},
		{Turn: 2, Kind: store.TranscriptToolCall, Tool: "bash", CallID: "c2",
			Text: `{"command":"npx vitest run test/intersection-observer"}`},
		{Turn: 2, Kind: store.TranscriptToolResult, Tool: "bash", CallID: "c2",
			Text: "Tests  3 failed | 17 passed", Millis: 41000, Failed: true},
	}); err != nil {
		t.Fatal(err)
	}

	block := BankedTranscript(graph, "leaf")
	for _, kept := range []string{
		"I'll read the observer first.",
		"npx vitest run test/intersection-observer",
		"Tests  3 failed | 17 passed",
		"export class IntersectionObserver {",
	} {
		if !strings.Contains(block, kept) {
			t.Fatalf("the restart lost %q from its own record:\n%s", kept, block)
		}
	}
	// And it arrives at the next attempt, under a header that says the work is
	// already paid for.
	body := (Bank{Transcript: block}).Continuation()
	if !strings.Contains(body, ContinuationTranscriptHeader) {
		t.Fatalf("the turns arrived without the header that says they are done:\n%s", body)
	}
	if (Bank{Transcript: block}).Empty() {
		t.Fatal("a bank holding an attempt's own turns is not empty")
	}
}

// TestAPartialTranscriptSaysWhereItWasInterrupted is the case the design has to
// survive: the recorder flushes on a fault, so the last thing in the record is
// routinely a tool call whose answer never arrived. Saying it returned nothing
// would be a lie a resuming leaf acts on — it would skip the command — so the
// record says the call was interrupted and leaves the decision to the leaf.
func TestAPartialTranscriptSaysWhereItWasInterrupted(t *testing.T) {
	graph := transcriptGraph(t)
	if err := graph.RecordTranscript("leaf", "deepseek/deepseek-v4-flash", []store.TranscriptEntry{
		{Turn: 1, Kind: store.TranscriptToolCall, Tool: "bash", CallID: "c1", Text: `{"command":"npm test"}`},
		{Turn: 1, Kind: store.TranscriptToolResult, Tool: "bash", CallID: "c1", Text: "all green"},
		{Turn: 2, Kind: store.TranscriptToolCall, Tool: "edit", CallID: "c2", Text: `{"filePath":"a.ts"}`},
		{Turn: 2, Kind: store.TranscriptFault, Text: "nothing came back from the model in 1m30s", Failed: true},
	}); err != nil {
		t.Fatal(err)
	}

	block := BankedTranscript(graph, "leaf")
	if !strings.Contains(block, "interrupted before it answered") {
		t.Fatalf("a call with no result must say so:\n%s", block)
	}
	if strings.Count(block, "interrupted before it answered") != 1 {
		t.Fatalf("only the unanswered call is interrupted:\n%s", block)
	}
	if !strings.Contains(block, "nothing came back from the model in 1m30s") {
		t.Fatalf("how the attempt ended is part of its record:\n%s", block)
	}
}

// TestATranscriptBlockKeepsTheEndOfTheWork pins which end survives the VERBATIM
// bound. A continuation needs where the attempt GOT TO; the head of a long run
// is what an autopsy wants, and the store already keeps that
// (store.MaxTranscriptEntries seals from the front).
//
// The outline above it is the other half of the same law and is asserted here
// too, because the two are only correct together: a tail with no outline is
// twelve turns of one file's contents, which is what the ink run of 2026-08-29
// paid eleven million prompt tokens a go to rediscover around.
func TestATranscriptBlockKeepsTheEndOfTheWork(t *testing.T) {
	graph := transcriptGraph(t)
	const turns = BankedTranscriptTurns + 8
	for turn := 1; turn <= turns; turn++ {
		if err := graph.RecordTranscript("leaf", "m", []store.TranscriptEntry{
			{Turn: turn, Kind: store.TranscriptAssistant, Text: "step number " + strconv.Itoa(turn) + " of the work"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	block, reached := BankedRun(graph, "leaf")
	if reached != turns {
		t.Fatalf("the run reached turn %d, want %d", reached, turns)
	}
	outline, tail, split := strings.Cut(block, BankedRunTailLead)
	if !split {
		t.Fatalf("the block has no verbatim tail:\n%s", block)
	}
	if !strings.Contains(tail, "step number "+strconv.Itoa(turns)+" of") {
		t.Fatalf("the last turn is missing from the tail:\n%s", tail)
	}
	if strings.Contains(tail, "step number 1 of") {
		t.Fatalf("the oldest turns must be dropped from the tail, not the newest:\n%s", tail)
	}
	if lines := strings.Count(strings.TrimSpace(tail), "\n"); lines > BankedTranscriptTurns {
		t.Fatalf("banked %d verbatim turns, want at most %d:\n%s", lines, BankedTranscriptTurns, tail)
	}
	// And the outline carries every turn, including the ones the tail dropped:
	// one line each, which is what makes the whole run affordable.
	if !strings.Contains(outline, "step number 1 of") {
		t.Fatalf("the outline lost the start of the run:\n%s", outline)
	}
	for turn := 1; turn <= turns; turn++ {
		if !strings.Contains(outline, "turn "+strconv.Itoa(turn)+" ·") {
			t.Fatalf("turn %d is missing from the outline:\n%s", turn, outline)
		}
	}
}

// TestABankedRunIsOneAttemptAndNotTheWholeTable is the memory defect of the ink
// run of 2026-08-29, in the shape the store actually held it.
//
// A node's record is every attempt any worker ever made under it, appended, and
// each attempt numbers its own turns from one. The seed used to be "every entry
// whose turn is above the last turn minus twelve", read across the whole table —
// so a leaf on its second claim was handed an interleaving of two runs, and the
// turns of its own most recent attempt that fell below the cutoff were dropped
// while the previous attempt's higher-numbered turns were kept.
func TestABankedRunIsOneAttemptAndNotTheWholeTable(t *testing.T) {
	graph := transcriptGraph(t)
	// The first attempt got a long way and died.
	for turn := 1; turn <= 72; turn++ {
		if err := graph.RecordTranscript("leaf", "m", []store.TranscriptEntry{
			{Turn: turn, Kind: store.TranscriptAssistant, Text: "first attempt, step " + strconv.Itoa(turn)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	// The second began again from turn one and is the one being resumed.
	for turn := 1; turn <= 20; turn++ {
		if err := graph.RecordTranscript("leaf", "m", []store.TranscriptEntry{
			{Turn: turn, Kind: store.TranscriptAssistant, Text: "second attempt, step " + strconv.Itoa(turn)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	block, reached := BankedRun(graph, "leaf")
	if reached != 20 {
		t.Fatalf("the run being resumed reached turn %d, want 20 — the count is the second attempt's", reached)
	}
	if strings.Contains(block, "first attempt") {
		t.Fatalf("the seed mixed a previous attempt into the one being resumed:\n%s", block)
	}
	if !strings.Contains(block, "second attempt, step 1") {
		t.Fatalf("the resumed attempt's own first turn was dropped by a cutoff read across both runs:\n%s", block)
	}
	if !strings.Contains(block, "second attempt, step 20") {
		t.Fatalf("the resumed attempt's last turn is missing:\n%s", block)
	}
}

// TestTheClaimReaperStaysAboveALeafsOwnDeadline is the other half of defect 2:
// the window that took the node away is a BACKSTOP, so it must sit above every
// deadline actually granted rather than above the one the constant assumed.
func TestTheClaimReaperStaysAboveALeafsOwnDeadline(t *testing.T) {
	runner := NewRunner(transcriptGraph(t), nil, "owner", 1)
	if got := runner.staleWindow(); got != staleClaimAge {
		t.Fatalf("a fresh runner opens at %s, want %s", got, staleClaimAge)
	}
	// A well-fed leaf: its budget bought it a deadline past the default window.
	runner.RaiseStaleAge(40 * time.Minute)
	if got := runner.staleWindow(); got != 40*time.Minute+claimReaperPad {
		t.Fatalf("window = %s, want the leaf's own deadline plus the landing pad", got)
	}
	// And a smaller leaf afterwards must not pull it back under the big one
	// still running: the sweep is one query over every claim in the store.
	runner.RaiseStaleAge(2 * time.Minute)
	if got := runner.staleWindow(); got != 40*time.Minute+claimReaperPad {
		t.Fatalf("window fell to %s under a live leaf", got)
	}
}

// TestANodeWithNoRecordBanksNothing keeps the honest empty answer: not every
// worker records a transcript, and a bank with one fewer block is what that
// means — never an announcement that an attempt produced nothing.
func TestANodeWithNoRecordBanksNothing(t *testing.T) {
	graph := transcriptGraph(t)
	if block := BankedTranscript(graph, "leaf"); block != "" {
		t.Fatalf("a node nobody recorded must bank nothing, got:\n%s", block)
	}
	if block := BankedTranscript(nil, "leaf"); block != "" {
		t.Fatalf("no journal, nothing to read: %q", block)
	}
}
