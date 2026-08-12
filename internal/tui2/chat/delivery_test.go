package chat

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// The delivery card is tested at the rows, because every claim under test is a
// claim about what a reader sees: that the id is gone, that the money is there,
// that the path is above the fold and the dump below it.

// fakeJobs is a board that answers for one job.
type fakeJobs struct {
	facts map[string]jobFacts
}

func (f fakeJobs) jobFacts(nodeID string) (jobFacts, bool) {
	facts, ok := f.facts[nodeID]
	return facts, ok
}

// settledDelivery is the row the reconciler posts when a job's lifecycle ends:
// a system row, anchored to the node, belonging to no command, carrying the
// worker's own account of itself.
func settledDelivery() store.Message {
	return store.Message{
		Seq: 31, SessionID: testSession, Role: store.RoleSystem, NodeID: "task-16",
		Body: "rivers.txt is written with three original haiku, each about rivers:\n\n" +
			"Silver thread unwinds\nthrough stone and shadowed valleys,\ncarving toward the sea.\n\n" +
			"Files:\n/tmp/aforge/workspace/task-16/rivers.txt\n",
	}
}

func deliveryRows(t *testing.T, message store.Message, board jobSource, width int) (*messageBlock, string) {
	t.Helper()
	block := newMessageBlock(message, nil, board)
	var out strings.Builder
	for _, row := range block.Rows(width) {
		out.WriteString(ansi.Strip(row))
		out.WriteByte('\n')
	}
	return block, out.String()
}

func threeHaikuBoard() fakeJobs {
	return fakeJobs{facts: map[string]jobFacts{
		"task-16": {Name: "three river haiku", Life: rail.LifeSettled, Cost: 0.0012, HasCost: true},
	}}
}

// 4.3 and 5.9: the deliverable is a CARD at its birth position — collapsed, one
// line of brief, its artifact beside it, its money on it — and never the job's
// whole account pasted into the conversation.
func TestDeliveryLandsAsACollapsedCard(t *testing.T) {
	block, out := deliveryRows(t, settledDelivery(), threeHaikuBoard(), 80)

	if !block.collapsible {
		t.Fatalf("the deliverable has no fold:\n%s", out)
	}
	if !strings.Contains(out, "three river haiku") {
		t.Fatalf("the card is not named by the job:\n%s", out)
	}
	if !strings.Contains(out, "rivers.txt is written with three original haiku") {
		t.Fatalf("the card carries no brief:\n%s", out)
	}
	if !strings.Contains(out, "$0.0012") {
		t.Fatalf("5.9's one number the user never forgives us for hiding is missing:\n%s", out)
	}
	// 12.5's artifact law: the path is a reference, and it is ABOVE the fold.
	if !strings.Contains(out, tokens.GlyphCollapsed+" /tmp/aforge/workspace/task-16/rivers.txt") {
		t.Fatalf("the deliverable's path is not a reference row:\n%s", out)
	}
	// THE RESULT IS ORGANIZED, NOT TEASED. This assertion used to be "the fold
	// holds everything below the first line", and the reader overruled it in
	// their own words: a result card should show "the result organized — with
	// expand/view-more on click and ellipsis". §4 asks the same of the card
	// anatomy — "3-5 sentences the assistant absorbed, never 'see the file'" —
	// so what stands is [cardBodyRows] source lines and what folds is the rest,
	// whole ([splitCardBody]).
	if !strings.Contains(out, "Silver thread unwinds") {
		t.Fatalf("the card teases the result instead of organizing it:\n%s", out)
	}
	if strings.Contains(out, "Files:") {
		t.Fatalf("the whole result was dumped into the conversation:\n%s", out)
	}
	if !strings.Contains(out, "lines") {
		t.Fatalf("the fold does not say how much it is holding:\n%s", out)
	}
}

// 5.14's never-shown tier names node ids explicitly, and the heading this card
// replaced WAS one.
func TestDeliveryNeverShowsTheNodeID(t *testing.T) {
	for _, width := range []int{40, 80, 200} {
		_, out := deliveryRows(t, settledDelivery(), threeHaikuBoard(), width)
		// The recorded path legitimately contains the id, so the check is on
		// what the card SAYS: the title, which used to be the bare id.
		for _, row := range strings.Split(out, "\n") {
			if strings.Contains(row, "/") {
				continue
			}
			if strings.Contains(row, "task-16") {
				t.Fatalf("a node id reached the card at %d columns: %q", width, row)
			}
		}
	}
}

// The fold is the record, kept: expanding gives back every word the journal has.
func TestDeliveryExpandsToTheWholeRecord(t *testing.T) {
	block, _ := deliveryRows(t, settledDelivery(), threeHaikuBoard(), 80)
	if !block.SetExpanded(true) {
		t.Fatal("the deliverable's fold would not open")
	}
	var out strings.Builder
	for _, row := range block.Rows(80) {
		out.WriteString(ansi.Strip(row) + "\n")
	}
	if !strings.Contains(out.String(), "carving toward the sea.") {
		t.Fatalf("the opened fold does not hold the record:\n%s", out.String())
	}
	if !strings.Contains(out.String(), tokens.GlyphExpanded) {
		t.Fatalf("an open fold still shows the collapsed affordance:\n%s", out.String())
	}
}

// 5.16: green is money and success, coral is broken — and neither is claimed
// unless the board said so (8.2.20: missing data is drawn missing, never
// guessed).
func TestDeliveryHueFollowsTheBoardAndNeverTheProse(t *testing.T) {
	settled := newMessageBlock(settledDelivery(), nil, threeHaikuBoard())
	if settled.head.GlyphHue != blocks.HueMoney {
		t.Fatalf("a delivered job is not drawn as success: %v", settled.head.GlyphHue)
	}

	failed := newMessageBlock(settledDelivery(), nil, fakeJobs{facts: map[string]jobFacts{
		"task-16": {Name: "three river haiku", Life: rail.LifeFailed},
	}})
	if failed.head.GlyphHue != blocks.HueBroken {
		t.Fatalf("a failed job is not drawn as broken: %v", failed.head.GlyphHue)
	}

	// No board at all: an honest card with no name and no colour, never a green
	// claim about something nobody confirmed.
	unknown, out := deliveryRows(t, settledDelivery(), nil, 80)
	if unknown.head.GlyphHue != blocks.HueNone {
		t.Fatalf("an unconfirmed ending was painted anyway: %v", unknown.head.GlyphHue)
	}
	if !strings.Contains(out, "delivered") {
		t.Fatalf("an unnamed job lost its heading entirely:\n%s", out)
	}
	if strings.Contains(out, "task-16\n") {
		t.Fatalf("an unnamed job fell back to its id:\n%s", out)
	}
}

// A narrator's progress line and a command's receipt are anchored to nodes too,
// and neither is a deliverable. The reading is columns, never prose (13.3.1).
func TestOnlyTheLifecycleRowIsADelivery(t *testing.T) {
	delivery := settledDelivery()
	if !isDelivery(delivery) {
		t.Fatal("the reconciler's own delivery row is not recognised")
	}
	progress := delivery
	progress.Role = store.RoleAgent
	if isDelivery(progress) {
		t.Fatal("a narrator's progress line was dressed as a deliverable")
	}
	receipt := delivery
	receipt.CommandSeq = 12
	if isDelivery(receipt) {
		t.Fatal("a command receipt was dressed as a deliverable")
	}
	speech := delivery
	speech.NodeID = ""
	if isDelivery(speech) {
		t.Fatal("a system row with no node was dressed as a deliverable")
	}
}

// The typed part is the truth when a producer attaches one; the summary is the
// fallback, read exactly as internal/head/depth.go reads the same field.
func TestDeliveryArtifactSources(t *testing.T) {
	typed := settledDelivery()
	typed.Parts = []store.MessagePart{
		store.ArtifactRef(store.ArtifactPart{Path: "/tmp/aforge/workspace/task-16/rivers.txt"}),
	}
	if files := deliveryFiles(typed); len(files) != 1 ||
		files[0] != "/tmp/aforge/workspace/task-16/rivers.txt" {
		t.Fatalf("the typed artifact part was not preferred: %v", files)
	}
	// And the typed part is drawn once, by the card, not twice.
	_, out := deliveryRows(t, typed, threeHaikuBoard(), 100)
	if strings.Count(out, "/tmp/aforge/workspace/task-16/rivers.txt") != 1 {
		t.Fatalf("the artifact reference was drawn twice:\n%s", out)
	}

	// A relative word in the prose is not a path and must not become a row.
	prose := settledDelivery()
	prose.Body = "wrote rivers.txt and nothing else"
	if files := deliveryFiles(prose); len(files) != 0 {
		t.Fatalf("a bare filename was mistaken for a recorded path: %v", files)
	}
}

// The board answer comes off the same walk the rail card came off, so the card
// in the thread and the card in the rail cannot disagree about one job.
func TestJobFactsReadTheRailsOwnWalk(t *testing.T) {
	source := &scopeSource{
		ready: true,
		label: map[string]string{"task-16": "three river haiku"},
		tasks: map[string]rail.Scope{
			rowTaskPrefix + "task-16": {Rows: []rail.Row{{
				ID: rowTaskPrefix + "task-16", Name: "three river haiku",
				Life: rail.LifeSettled,
				Meta: rail.Telemetry{Cost: 0.0012, HasCost: true},
			}}},
		},
	}
	facts, ok := source.jobFacts("task-16")
	if !ok || facts.Name != "three river haiku" || !facts.HasCost || facts.Life != rail.LifeSettled {
		t.Fatalf("the board's own row did not reach the card: %+v (%v)", facts, ok)
	}
	if _, ok := source.jobFacts("task-99"); ok {
		t.Fatal("a job the board never saw answered anyway")
	}

	var unbuilt *scopeSource
	if _, ok := unbuilt.jobFacts("task-16"); ok {
		t.Fatal("a source that has never been built answered")
	}
}
