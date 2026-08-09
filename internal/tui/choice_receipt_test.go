package tui

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/x/ansi"
)

// The promise and the proof are two different things. The head says "I'll use
// kimi for this" in the thread; the card says what the store will actually run,
// and it says it only when there is something to say.
func TestChoiceReceiptSpeaksOnlyForChoices(t *testing.T) {
	for _, probe := range []struct {
		name string
		card jobCard
		want string
	}{
		{"default job", jobCard{}, ""},
		{"pinned model only", jobCard{WorkModel: "moonshotai/kimi-k2"}, "kimi-k2"},
		{"worker only", jobCard{Subharness: "swe"}, "swe"},
		{"worker and model", jobCard{Subharness: "swe", WorkModel: "moonshotai/kimi-k2"},
			"swe · kimi-k2"},
		{"model and planner", jobCard{WorkModel: "moonshotai/kimi-k2", PlanModel: "anthropic/claude-opus-5"},
			"kimi-k2 · planned by claude-opus-5"},
		{"planner only", jobCard{PlanModel: "anthropic/claude-opus-5"},
			"planned by claude-opus-5"},
		{"all three", jobCard{
			Subharness: "swe", WorkModel: "moonshotai/kimi-k2", PlanModel: "anthropic/claude-opus-5"},
			"swe · kimi-k2 · planned by claude-opus-5"},
		{"whitespace is not a choice", jobCard{Subharness: "  ", WorkModel: " ", PlanModel: "\t"}, ""},
	} {
		t.Run(probe.name, func(t *testing.T) {
			if got := cardChoiceReceipt(probe.card); got != probe.want {
				t.Fatalf("cardChoiceReceipt = %q, want %q", got, probe.want)
			}
		})
	}
}

// Zero height when there is nothing to show. A line that reserves a row on every
// card in the thread is a tax the default job pays for a feature it never used.
func TestChoiceReceiptCostsNoHeightOnAnOrdinaryJob(t *testing.T) {
	model := New(&fakeBackend{}, "receipt-height")
	model.setSize(110, 34)
	ordinary := jobCard{ID: "job", Title: "Draft the launch note", State: cardWorking}
	chosen := ordinary
	chosen.Subharness, chosen.WorkModel = "swe", "moonshotai/kimi-k2"

	plain := ansi.Strip(model.renderJobCard(ordinary, 100, false, 0, false, false))
	receipted := ansi.Strip(model.renderJobCard(chosen, 100, false, 0, false, false))
	if strings.Contains(plain, "kimi") || strings.Contains(plain, "planned by") {
		t.Fatalf("an ordinary card spoke about choices:\n%s", plain)
	}
	if got, want := len(strings.Split(receipted, "\n")), len(strings.Split(plain, "\n"))+1; got != want {
		t.Fatalf("chosen card is %d lines, want %d:\n%s", got, want, receipted)
	}
	second := strings.Split(receipted, "\n")[1]
	if !strings.Contains(second, "swe · kimi-k2") {
		t.Fatalf("the receipt is not under the title: %q", second)
	}
	if strings.Contains(second, "moonshotai/") {
		t.Fatalf("the vendor path reached the card: %q", second)
	}
}

// The receipt truncates with the card and never wraps: a narrow window loses the
// end of one line, not the shape of the card.
func TestChoiceReceiptTruncatesInsteadOfWrapping(t *testing.T) {
	model := New(&fakeBackend{}, "receipt-width")
	model.setSize(110, 34)
	card := jobCard{
		ID: "job", Title: "Draft the launch note", State: cardWorking,
		Subharness: "swe", WorkModel: "moonshotai/kimi-k2-instruct",
		PlanModel:  "anthropic/claude-opus-5",
	}
	for _, width := range []int{24, 40, 100} {
		frame := ansi.Strip(model.renderJobCard(card, width, false, 0, false, false))
		for _, line := range strings.Split(frame, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d: line overflows: %q", width, line)
			}
		}
	}
}

// The card reads the durable row, and it reads it from the job's own root. A
// worker settled on one leaf is that leaf's business — the root card speaks for
// the job it is the face of.
func TestChoiceReceiptRidesTheRootRowOnly(t *testing.T) {
	root := store.Node{
		ID: "job", Parent: store.RootID, Title: "Land the migration", Status: store.Running,
		Subharness: "swe",
		Provenance: store.Provenance{
			Origin: store.OriginUser, SessionID: "cards",
			WorkModel: "moonshotai/kimi-k2", PlanModel: "anthropic/claude-opus-5",
			Subharness: "swe",
		},
	}
	leaf := store.Node{
		ID: "job-leaf", Parent: "job", Title: "Write the code", Status: store.Running,
		Subharness: "swe",
		Provenance: store.Provenance{Origin: store.OriginUser, SessionID: "cards"},
	}
	plain := store.Node{
		ID: "chore", Parent: store.RootID, Title: "Read the file", Status: store.Running,
		Provenance: store.Provenance{Origin: store.OriginUser, SessionID: "cards"},
	}
	snapshot := store.Snapshot{Nodes: []store.Node{{ID: store.RootID}, root, leaf, plain}}
	cards := deriveJobCards("cards", snapshot, nil, nil, nil, nil)
	if len(cards) != 2 {
		t.Fatalf("derived %d cards, want 2", len(cards))
	}
	byID := map[string]jobCard{}
	for _, card := range cards {
		byID[card.ID] = card
	}
	if got := cardChoiceReceipt(byID["job"]); got != "swe · kimi-k2 · planned by claude-opus-5" {
		t.Fatalf("chosen job receipt = %q", got)
	}
	if got := cardChoiceReceipt(byID["chore"]); got != "" {
		t.Fatalf("ordinary job spoke: %q", got)
	}

	model := New(&fakeBackend{}, "cards")
	model.setSize(110, 34)
	frame := ansi.Strip(model.renderJobCard(byID["job"], 100, true, 0, false, false))
	receipts := 0
	for _, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, "planned by") {
			receipts++
		}
	}
	if receipts != 1 {
		t.Fatalf("the expanded card said it %d times:\n%s", receipts, frame)
	}
	if strings.Contains(frame, "Write the code — running\nswe") {
		t.Fatalf("a part carried a receipt:\n%s", frame)
	}
}

// The splice's choice reaches the card even when the row's own worker column is
// empty, which is how a subtree-wide choice was always meant to be read.
func TestSettledWorkerFallsBackToTheSplicesChoice(t *testing.T) {
	node := store.Node{Provenance: store.Provenance{Subharness: "swe"}}
	if got := settledWorker(node); got != "swe" {
		t.Fatalf("settledWorker = %q", got)
	}
	node.Subharness = "review"
	if got := settledWorker(node); got != "review" {
		t.Fatalf("the row's own worker lost to the splice: %q", got)
	}
	if got := settledWorker(store.Node{}); got != "" {
		t.Fatalf("an ordinary node named a worker: %q", got)
	}
}

// The short name is the whole point of the line fitting: the vendor path and the
// dated build are noise at card width, and the id keeps its full spelling where
// there is room for it.
func TestModelShortNamesTheModelNotThePath(t *testing.T) {
	for _, probe := range []struct{ full, want string }{
		{"moonshotai/kimi-k2", "kimi-k2"},
		{"anthropic/claude-opus-5", "claude-opus-5"},
		{"anthropic/claude-3-5-sonnet-2024-10-22", "claude-3-5-sonnet"},
		{"kimi-k2", "kimi-k2"},
	} {
		if got := modelShort(probe.full); got != probe.want {
			t.Fatalf("modelShort(%q) = %q, want %q", probe.full, got, probe.want)
		}
	}
}

// The drill-down is where the full spelling lives. A card trades the vendor path
// for width; the flight recorder must not, or there is nowhere left to check
// which build ran the work.
func TestNodeDrillDownCarriesTheUntruncatedChoice(t *testing.T) {
	model := New(&fakeBackend{}, "node-receipt")
	model.setSize(110, 34)
	model.inspectedNode = store.Node{
		ID: "job-leaf", Parent: "job", Brief: "Land the migration", Status: store.Running,
		Subharness: "swe",
		Provenance: store.Provenance{
			WorkModel: "moonshotai/kimi-k2", PlanModel: "anthropic/claude-opus-5",
		},
	}
	details := ansi.Strip(model.renderNodeDetailsContent(100, 12))
	if !strings.Contains(details, "swe · moonshotai/kimi-k2 · planned by anthropic/claude-opus-5") {
		t.Fatalf("node details lost the choice:\n%s", details)
	}

	model.inspectedNode = store.Node{ID: "chore", Brief: "Read the file", Status: store.Running}
	if plain := ansi.Strip(model.renderNodeDetailsContent(100, 12)); strings.Contains(plain, "planned by") {
		t.Fatalf("an ordinary node spoke:\n%s", plain)
	}
}
