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

// Quiet, not invisible. The receipt was faint on top of muted and readers
// reported never seeing it; the frame keeps the faint, the words do not.
func TestChoiceReceiptOnACardIsNotFaint(t *testing.T) {
	model := New(&fakeBackend{}, "receipt-weight")
	model.setSize(110, 34)
	card := jobCard{
		ID: "job", Title: "Draft the launch note", State: cardWorking,
		Subharness: "swe", WorkModel: "moonshotai/kimi-k2",
	}
	if cardReceiptStyle.GetFaint() {
		t.Fatal("the choice receipt still speaks in faint ink")
	}
	if !cardFrameStyle.GetFaint() {
		t.Fatal("the card frame stopped receding — structure is faint, words are not")
	}
	frame := model.renderJobCard(card, 100, false, 0, false, false)
	if !strings.Contains(frame, cardReceiptStyle.Render("swe · kimi-k2")) {
		t.Fatalf("the receipt is not rendered in the receipt's own ink:\n%q", frame)
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
		PlanModel: "anthropic/claude-opus-5",
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
	details := ansi.Strip(model.renderNodeDetailsContent(100))
	if !strings.Contains(details, "swe · moonshotai/kimi-k2 · planned by anthropic/claude-opus-5") {
		t.Fatalf("node details lost the choice:\n%s", details)
	}

	model.inspectedNode = store.Node{ID: "chore", Brief: "Read the file", Status: store.Running}
	if plain := ansi.Strip(model.renderNodeDetailsContent(100)); strings.Contains(plain, "planned by") {
		t.Fatalf("an ordinary node spoke:\n%s", plain)
	}
}

// The short spelling rides the one line that never scrolls away. A receipt a
// reader has to go looking for is a receipt they never see, and this one is the
// answer to "who is actually running this" — the first question the drill-down
// exists to answer.
func TestNodeDrillDownReceiptRidesTheStickyTitle(t *testing.T) {
	model, _ := inspectedWorkerModel(t)
	model.setSize(120, 30)
	model.inspectedNode = store.Node{
		ID: "worker", Parent: store.RootID, Brief: "Land the migration", Status: store.Running,
		Subharness: "swe",
		Provenance: store.Provenance{
			WorkModel: "moonshotai/kimi-k2", PlanModel: "anthropic/glm-5-2",
		},
	}
	model.setSize(120, 30)
	title := ansi.Strip(strings.Split(model.renderNodePane(), "\n")[0])
	if !strings.Contains(title, "swe · kimi-k2 · planned by glm-5-2") {
		t.Fatalf("the sticky title lost the receipt:\n%q", title)
	}
	if strings.Contains(title, "moonshotai/") {
		t.Fatalf("the vendor path reached the title line: %q", title)
	}
	if ansi.StringWidth(title) > 120 {
		t.Fatalf("the title line overflows the frame: %q", title)
	}

	// The name outranks the badge: a frame too narrow for both drops the badge
	// rather than truncating the task away.
	model.setSize(46, 30)
	narrow := ansi.Strip(strings.Split(model.renderNodePane(), "\n")[0])
	if strings.Contains(narrow, "planned by") {
		t.Fatalf("a 46-column title kept the badge and lost the name: %q", narrow)
	}
	if !strings.Contains(narrow, "land") {
		t.Fatalf("a 46-column title lost the task's name entirely: %q", narrow)
	}

	model.setSize(120, 30)
	model.inspectedNode.Provenance = store.Provenance{}
	model.inspectedNode.Subharness = ""
	plain := ansi.Strip(strings.Split(model.renderNodePane(), "\n")[0])
	if strings.Contains(plain, "planned by") {
		t.Fatalf("an ordinary worker's title spoke: %q", plain)
	}
}

// The title is the only thing pinned. The brief, the decisions and the turns are
// one document underneath it, so the first two lines of the pane can hold a name
// and a hairline and nothing else — and the description is reachable only by the
// same scroll that reaches the work.
func TestNodeDrillDownStickyRegionHoldsOnlyTheTitle(t *testing.T) {
	model, _ := inspectedWorkerModel(t)
	model.setSize(100, 30)
	model.inspectedNode = store.Node{
		ID: "worker", Parent: store.RootID, Status: store.Running,
		Title: "Land the migration",
		Brief: "Land the migration. Keep every contract green.",
	}
	model.nodeTraceText = "── turn 1 finish=stop in=10 out=20 ──\ntext: thinking about it\n"
	model.setSize(100, 30)
	model.refreshNodeView(true)

	pane := strings.Split(ansi.Strip(model.renderNodePane()), "\n")
	sticky := strings.Join(pane[:2], "\n")
	if name := nodeLabelInSnapshot(model.inspectedNode, model.snapshot); !strings.Contains(sticky, name) {
		t.Fatalf("the sticky line does not name %q:\n%s", name, sticky)
	}
	if strings.Contains(sticky, "BRIEF") || strings.Contains(sticky, "Keep every contract green.") {
		t.Fatalf("the brief is still pinned above the scroll:\n%s", sticky)
	}

	document := ansi.Strip(model.renderActivityFeed(98))
	for _, want := range []string{"BRIEF", "Keep every contract green.",
		"── execution ", "✳ model · $ shell", "── turn 1 · 20 tok"} {
		if !strings.Contains(document, want) {
			t.Fatalf("the scrolling document is missing %q:\n%s", want, document)
		}
	}
	if strings.Index(document, "── execution ") < strings.Index(document, "Keep every contract green") {
		t.Fatal("the execution seam falls before the description it separates")
	}
	if strings.Index(document, "── turn 1 · 20 tok") < strings.Index(document, "── execution ") {
		t.Fatal("a turn falls above the seam that announces the turns")
	}
}

// Where the document opens says why it was opened: a running worker is being
// watched, a settled one is being read.
func TestNodeDrillDownOpensLiveForWorkAndAtTheTopToRead(t *testing.T) {
	for _, probe := range []struct {
		name   string
		status store.Status
		top    bool
	}{
		{"running worker follows the live end", store.Running, false},
		{"settled worker opens at its brief", store.Done, true},
	} {
		t.Run(probe.name, func(t *testing.T) {
			backend := &fakeBackend{snapshot: store.Snapshot{Nodes: []store.Node{
				{ID: store.RootID},
				{ID: "worker", Parent: store.RootID, Brief: "Inspect this worker", Status: probe.status},
			}}}
			model := NewWithCommander(backend, "open-position", newFakeCommander())
			model.setSize(100, 26)
			model.snapshot = backend.snapshot
			_ = model.openNodeByID("worker")
			model.nodeTraceText = strings.Repeat("text: a line worth reading\n", 80)
			model.refreshNodeView(false)

			if probe.top {
				if !model.nodeTrace.AtTop() {
					t.Fatalf("a settled worker opened at offset %d", model.nodeTrace.YOffset)
				}
				// One scroll of the reader's own releases the pin for good.
				model.scrollNodeFeed(true)
				model.refreshNodeView(false)
				if model.nodePinTop {
					t.Fatal("the pin survived a scroll the reader made")
				}
				return
			}
			if !model.nodeTrace.AtBottom() {
				t.Fatalf("a running worker did not open live: offset %d", model.nodeTrace.YOffset)
			}
		})
	}
}
