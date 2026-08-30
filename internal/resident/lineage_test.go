package resident

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// THE TEXTUAL RUN OF 2026-08-29 (s9), AT THE MOMENT ITS JOB FORGOT ITSELF.
//
// `task-2` ran 350 recorded rows over 80 turns and exhausted on its budget.
// Thirteen minutes later a growth round on `reason: gap` spliced five FRESH IDS
// under it, and `task-2-x1-n2`'s first recorded row is turn 1, "Let me start by
// examining the existing codebase", followed by `find /app`. The record it
// needed was in the same store under an id one character away.
//
// The seed followed the overrun door and the same-id re-claim; a gap round goes
// through neither. What a job has already done is a property of the LINEAGE.
func openLineageStore(t *testing.T) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-2", Brief: "RichLog still snaps back to the newest entry", Stage: 1, Title: "follow state"},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "richlog follow state"}); err != nil {
		t.Fatalf("splice the job: %v", err)
	}
	return graph
}

// recordRun writes one node's worth of turns, in the flushes the recorder
// actually writes, because the batch boundary is what the store's order is on.
func recordRun(t *testing.T, graph *store.Store, nodeID string, turns int) {
	t.Helper()
	entries := make([]store.TranscriptEntry, 0, turns*2)
	for turn := 1; turn <= turns; turn++ {
		entries = append(entries,
			store.TranscriptEntry{Turn: turn, Kind: store.TranscriptAssistant, Text: "adding follow_end to Log"},
			store.TranscriptEntry{Turn: turn, Kind: store.TranscriptToolCall, Tool: "edit",
				CallID: "c", Text: `{"path":"/app/src/textual/widgets/_log.py"}`})
	}
	for start := 0; start < len(entries); start += store.MaxTranscriptBatch {
		end := start + store.MaxTranscriptBatch
		if end > len(entries) {
			end = len(entries)
		}
		if err := graph.RecordTranscript(nodeID, "deepseek/deepseek-v4-flash", entries[start:end]); err != nil {
			t.Fatalf("record the transcript: %v", err)
		}
	}
}

func TestAChildSplicedUnderAnExhaustedLineageCarriesItsOutline(t *testing.T) {
	graph := openLineageStore(t)
	recordRun(t, graph, "task-2", 80)

	// The gap round: five fresh ids under the lineage, none of which has ever
	// recorded anything of its own.
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-2-x1", Brief: "Synthesis", Stage: 1},
		{ID: "task-2-x1-n2", Parent: "task-2-x1", Brief: "Write RichLog snap-back tests", Stage: 1},
	}}, store.Provenance{Origin: store.OriginSelf, SessionID: "s1", Intent: "richlog follow state"}); err != nil {
		t.Fatalf("splice the repair round: %v", err)
	}

	// The node's OWN record is empty, which is the reading that started the run
	// over and the reason a node-shaped seed could never have worked.
	if _, turns := BankedRun(graph, "task-2-x1-n2"); turns != 0 {
		t.Fatalf("the fresh child already had %d turns of its own; the fixture is wrong", turns)
	}

	block, resumed := LineageBank(graph, "task-2", "task-2-x1-n2")
	if resumed == 0 {
		t.Fatal("a child spliced under a lineage with 80 recorded turns was seeded with nothing")
	}
	if resumed != 80 {
		t.Fatalf("the seed carries %d turns, want the 80 the lineage recorded", resumed)
	}
	if !strings.Contains(block, "task-2") {
		t.Fatalf("the seed does not say whose work it is:\n%s", block)
	}
	// The outline, not just the tail: the file the run created is what a
	// successor must not create again, and it is nowhere near the last turns.
	if !strings.Contains(block, "_log.py") {
		t.Fatalf("the seed names no file the lineage touched:\n%s", firstBytes(block, 400))
	}
	// And it is bounded once for the whole composition rather than per node.
	if len(block) > BankedTranscriptBytes+len(lineageRunLead(store.Node{ID: "task-2"}))+2 {
		t.Fatalf("the composed seed is %d bytes, over the %d bound", len(block), BankedTranscriptBytes)
	}

	// The sink is never quoted back to itself.
	if own, _ := LineageBank(graph, "task-2", "task-2"); strings.Contains(own, "task-2)") {
		t.Fatal("the node being seeded was handed its own record")
	}
}

// EVERY REPAIR ROUND IS TOLD WHAT THE JOB IS SHORT OF, VERBATIM. textual s9's
// gate reported the same two unexercised behaviours three times and thirteen of
// its fourteen briefs named neither.
func TestTheOpenFindingsAreReadFromTheRecordAndNotFromProse(t *testing.T) {
	graph := openLineageStore(t)
	const (
		first  = "RichLog exposes is_following_end"
		second = "RichLog.write(expand=True) preserves justified rendering"
	)
	if err := graph.RecordDeliveryGate("task-2", store.DeliveryGate{
		Pass: false, Gap: "the deliverable describes the work rather than carrying it",
		Unexercised: []string{first, second}, Unclosed: true,
	}); err != nil {
		t.Fatalf("record the gate: %v", err)
	}

	// Asked of the node, a repair round finds nothing — it is a fresh id.
	if found := ReadOpenFindings(graph, "task-2-x1-n2"); !found.Empty() {
		t.Fatal("a fresh id answered for findings that belong to its lineage")
	}
	// Asked of the lineage, it finds all of them.
	findings := ReadOpenFindings(graph, "task-2")
	if len(findings.Unexercised) != 2 {
		t.Fatalf("read %d unexercised behaviours, want the 2 the gate measured", len(findings.Unexercised))
	}
	if !findings.Unclosed {
		t.Fatal("the standing gap was not read back as standing")
	}
	words := findings.Words()
	for _, want := range []string{first, second, "describes the work rather than carrying it"} {
		if !strings.Contains(words, want) {
			t.Fatalf("the section does not carry %q verbatim:\n%s", want, words)
		}
	}
	if !strings.HasPrefix(words, OpenFindingsHeader) {
		t.Fatal("the section does not open with its own fixed header")
	}

	// A LATER PASSING GATE ANSWERS THE GAP AND NOT THE MEASUREMENT. A coverage
	// reading is a photograph of the tree, and a judgement about a deliverable
	// does not make an unexercised behaviour exercised.
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-2-x1", Brief: "Synthesis", Stage: 1},
	}}, store.Provenance{Origin: store.OriginSelf, SessionID: "s1", Intent: "richlog follow state"}); err != nil {
		t.Fatalf("splice the repair round: %v", err)
	}
	if err := graph.RecordDeliveryGate("task-2-x1", store.DeliveryGate{Pass: true}); err != nil {
		t.Fatalf("record the passing gate: %v", err)
	}
	after := ReadOpenFindings(graph, "task-2")
	if after.Gap != "" || after.Unclosed {
		t.Fatalf("a passing gate left the old gap standing: %q", after.Gap)
	}
	if len(after.Unexercised) != 2 {
		t.Fatalf("a passing gate erased %d unexercised behaviours nobody re-measured",
			2-len(after.Unexercised))
	}

	// Nothing to say is said with nothing.
	if empty := ReadOpenFindings(graph, "nothing-here").Words(); empty != "" {
		t.Fatalf("a job with no findings composed a section:\n%s", empty)
	}
}

func firstBytes(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}
