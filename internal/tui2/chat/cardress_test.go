package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE CARD DRESS (§4, §18.5): the ground and the edge that mean "a finished
// answer", the quieter rung the commitment stands on, and the anatomy on both.
//
// The lane that produced these is the one the reader asked for in their own
// words — "have proper bg for card, proper lines and headings … more hierarchy
// via background colors, borders or separation lines … it should feel
// professional" — and the law it is held to is §4's last clause: the delivery's
// treatment is the only one of its kind, so a second element wearing it is a
// defect and not a feature.

// colourApp is a window that actually paints: the dress is a ground, and a
// NoColor fixture cannot see one. Everything about SHAPE is asserted at NoColor
// (see the tests below); this is for the tests about the plane itself.
func colourApp(t *testing.T, backend Backend) *App {
	t.Helper()
	app := New(Options{
		Backend: backend, Session: testSession, Profile: tokens.TrueColor,
		Now: fixedNow, PollEvery: time.Millisecond,
		Root: "/home/someone/aforge-v2", Home: "/home/someone",
	})
	poll(t, app)
	return app
}

// bgOf is the background escape one token paints at truecolor — what a row has
// to contain for the ground to be on it.
func bgOf(ground tokens.Token) string {
	return ground.Bg(tokens.TrueColor, tokens.FocusNormal)
}

// deliveredApp is a window whose one task has settled with a result: the card
// under test in most of this file.
func deliveredApp(t *testing.T, profile tokens.Profile) (*App, *boardBackend) {
	t.Helper()
	backend := board()
	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-2" {
			backend.nodes[i].StartedAt = fixedNow().Add(-4 * time.Minute)
			backend.nodes[i].FinishedAt = fixedNow()
		}
	}
	backend.usage["job-2"] = store.JobUsage{Runs: 1, Cost: 0.20}
	var app *App
	if profile == tokens.TrueColor {
		app = colourApp(t, backend)
	} else {
		app = newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
		poll(t, app)
	}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem, NodeID: "job-2",
		Body: "Three hot paths, one fix each.\n\nThe poll loop wakes eight times a second for nothing.\n" +
			"The renderer re-measures every row on every frame.\n" +
			"/tmp/aforge/workspace/job-2/audit.md\nand a tail\nof more\nlines than fit"})
	poll(t, app)
	return app, backend
}

// lastCard is the block standing at the end of the conversation, as a card.
func lastCard(t *testing.T, app *App) *messageBlock {
	t.Helper()
	block, ok := app.transcript.Block(app.transcript.Len() - 1).(*messageBlock)
	if !ok {
		t.Fatal("the last block is not a message block")
	}
	return block
}

// §4: THE DELIVERY CARD IS THE ONLY ELEMENT WITH A DISTINCT GROUND AND A `▎`
// EDGE. Both halves, on the same rows.
func TestTheDeliveryCardWearsTheGroundAndTheEdge(t *testing.T) {
	app, _ := deliveredApp(t, tokens.TrueColor)
	block := lastCard(t, app)
	if block.card != dressDelivery {
		t.Fatalf("the settled card is not wearing the delivery dress: %v", block.card)
	}
	rows := block.Rows(90)
	ground := bgOf(tokens.CardGroundDelivered)
	edged, grounded := 0, 0
	for _, row := range rows {
		if strings.Contains(row, ground) {
			grounded++
		}
		if strings.Contains(ansi.Strip(row), blocks.AccentEdge) {
			edged++
		}
	}
	// Every row but the last: the block's own separating blank is the air
	// BETWEEN blocks and belongs to the room, not to the card (§16's padding
	// rhythm — the card's own leading and trailing blanks are inside its ground).
	if grounded != len(rows)-1 {
		t.Fatalf("the delivery card's ground does not cover it: %d of %d rows\n%s",
			grounded, len(rows), strings.Join(rows, "\n"))
	}
	if edged != len(rows)-1 {
		t.Fatalf("the accent edge does not run down the card: %d of %d rows\n%s",
			edged, len(rows), ansi.Strip(strings.Join(rows, "\n")))
	}
}

// AND NOTHING ELSE IN THE TRANSCRIPT WEARS IT. §4's "nothing else may wear it"
// is the whole point of the treatment: the moment a second kind of row is drawn
// like a delivery, the dress stops meaning "a finished answer".
func TestTheDeliveryDressIsUniqueInTheTranscript(t *testing.T) {
	app, _ := deliveredApp(t, tokens.TrueColor)
	delivery := lastCard(t, app)
	ground := bgOf(tokens.CardGroundDelivered)

	for i := 0; i < app.transcript.Len(); i++ {
		block := app.transcript.Block(i)
		if block.ID() == delivery.ID() {
			continue
		}
		for _, row := range block.Rows(90) {
			if strings.Contains(row, ground) {
				t.Fatalf("block %q wears the delivery ground: %q", block.ID(), row)
			}
			if strings.Contains(ansi.Strip(row), blocks.AccentEdge) {
				t.Fatalf("block %q wears the delivery edge: %q", block.ID(), ansi.Strip(row))
			}
		}
	}
}

// THE COMMITMENT STANDS ONE RUNG UNDER IT, and that step is what says "this
// finished" without a word being spent on it.
func TestTheCommitmentStandsOnTheRungUnderTheDelivery(t *testing.T) {
	backend := board()
	backend.nodes = append(backend.nodes,
		store.Node{ID: "task-" + commissionCommand, Title: "wisp-parity", Status: store.Running,
			CreatedSeq: 40, UpdatedSeq: 40, StartedAt: fixedNow().Add(-20 * time.Second)})
	app := colourApp(t, backend)
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem,
		CommandSeq: 4242, Body: "Here's my reading: bring the browser to parity."})
	poll(t, app)

	block := lastCard(t, app)
	if block.card != dressCommitment {
		t.Fatalf("the commitment is not wearing the commitment dress: %v", block.card)
	}
	rows := strings.Join(block.Rows(90), "\n")
	if !strings.Contains(rows, bgOf(tokens.CardGroundWorking)) {
		t.Fatalf("the commitment card has no ground:\n%s", rows)
	}
	// The delivery's own dress is denied it, in both halves.
	if strings.Contains(rows, bgOf(tokens.CardGroundDelivered)) {
		t.Fatalf("the commitment card is wearing the delivery ground:\n%s", rows)
	}
	if strings.Contains(ansi.Strip(rows), blocks.AccentEdge) {
		t.Fatalf("the commitment card is wearing the delivery edge:\n%s", ansi.Strip(rows))
	}
	// And the ladder is a ladder: the delivered rung is above the working one.
	working := tokens.CardGroundWorking.Color(tokens.FocusNormal)
	delivered := tokens.CardGroundDelivered.Color(tokens.FocusNormal)
	if !(working.Luminance() < delivered.Luminance()) {
		t.Fatalf("the card rungs are out of order: working %s, delivered %s",
			working.Hex(), delivered.Hex())
	}
}

// AT NoColor THE TWO ARE STILL TOLD APART, by a printable character and a state
// glyph rather than by a hue. §12's degradation law and §18.5's gate agree: at
// 16 colours and none there is no honest raised ground, so what carries the
// difference has to be something that is not colour at all.
func TestCommitmentAndDeliveryDifferWithoutColour(t *testing.T) {
	app, backend := boardApp(t)
	jobStatus(backend, "job-1", "preparing the repository")
	poll(t, app)
	commitment := ansi.Strip(blockRows(t, app, app.transcript.Len()-1, 80))

	delivered, _ := deliveredApp(t, tokens.NoColor)
	delivery := ansi.Strip(blockRows(t, delivered, delivered.transcript.Len()-1, 80))

	if strings.Contains(commitment, blocks.AccentEdge) {
		t.Fatalf("the commitment card wears the edge at NoColor:\n%s", commitment)
	}
	if !strings.Contains(delivery, blocks.AccentEdge) {
		t.Fatalf("the delivery card lost its edge at NoColor — nothing tells them apart:\n%s",
			delivery)
	}
	if !strings.Contains(commitment, tokens.GlyphWorking) {
		t.Fatalf("the commitment card has no working glyph:\n%s", commitment)
	}
	if !strings.Contains(delivery, tokens.GlyphSettled) {
		t.Fatalf("the delivery card has no settled glyph:\n%s", delivery)
	}
	// The two dresses keep ONE geometry, so the card does not jump sideways at
	// the moment it settles (§3: the block becomes the delivery card in place).
	if got := columnOf(commitment, "wisp-parity"); got != cardLane+2 {
		t.Fatalf("the commitment's title is at column %d, want %d", got, cardLane+2)
	}
	if got := columnOf(delivery, "perf-audit"); got != cardLane+2 {
		t.Fatalf("the delivery's title is at column %d, want %d", got, cardLane+2)
	}
}

// columnOf is the CELL a word starts at on the first row that carries it. Cells
// and not bytes: the state glyphs in the gutter are multi-byte, and a byte
// offset would make every column assertion in this file wrong by two.
func columnOf(rows, word string) int {
	for _, row := range strings.Split(rows, "\n") {
		if at := strings.Index(row, word); at >= 0 {
			return blocks.Width(row[:at])
		}
	}
	return -1
}

// THE ANATOMY, TOP TO BOTTOM (§4): a title row with the state glyph in the
// card's gutter, the name, and the receipt; the prompt line under it; the one
// internal seam; the result's lead and body; the artifact path as a reference;
// and the door.
func TestTheDeliveryCardsAnatomy(t *testing.T) {
	app, backend := deliveredApp(t, tokens.NoColor)
	_ = backend
	block := lastCard(t, app)
	block.adoptPrompt("Here's my reading: audit the poll loop for wasted wakeups.")
	rows := strings.Split(ansi.Strip(strings.Join(block.Rows(90), "\n")), "\n")

	find := func(want string) int {
		for i, row := range rows {
			if strings.Contains(row, want) {
				return i
			}
		}
		t.Fatalf("the card is missing %q:\n%s", want, strings.Join(rows, "\n"))
		return -1
	}
	title := find("perf-audit")
	prompt := find("audit the poll loop")
	lead := find("Three hot paths")
	path := find("audit.md")
	door := find(blocks.Disclose(false, 2, "line", "lines"))

	if !(title < prompt && prompt < lead && lead <= path && path < door) {
		t.Fatalf("the card's anatomy is out of order — title %d, prompt %d, lead %d, path %d, door %d:\n%s",
			title, prompt, lead, path, door, strings.Join(rows, "\n"))
	}
	// The receipt rides with its title (§20's placement 1 or 3, never a gulf).
	if !strings.Contains(rows[title], "$0.20") {
		t.Fatalf("the title row carries no receipt: %q", rows[title])
	}
	// THE SEAM, and exactly one of it (user-amended 2026-08-11): a short dim
	// hairline between what was asked and what came of it, INSET from both of
	// the card's edges, on the card's own ground. It replaced a full-measure row
	// in the other card rung, which read as a bar of shadow across the card —
	// "not a big fan of this thick black line separating". §16's ruled-lines law
	// names the delivery/execution seam as one of its two allowances, and this
	// is that seam drawn inside one card instead of between two blocks.
	if block.seamLine < 0 {
		t.Fatal("the card drew no internal seam between its prompt and its result")
	}
	seams := 0
	for _, row := range rows {
		if strings.Contains(row, tokens.GlyphTreeDash+tokens.GlyphTreeDash) {
			seams++
		}
	}
	if seams != 1 {
		t.Fatalf("the card drew %d ruled lines, want exactly the one seam:\n%s",
			seams, strings.Join(rows, "\n"))
	}
	// INSET, not across: the rule starts where the card's words start and stops
	// short of its right edge, so it reads as a boundary between two halves
	// rather than as a line through the whole thing.
	seam := ansi.Strip(rows[block.seamLine])
	from := strings.Index(seam, tokens.GlyphTreeDash)
	to := strings.LastIndex(seam, tokens.GlyphTreeDash) + len(tokens.GlyphTreeDash)
	if from < 0 {
		t.Fatalf("the seam row holds no rule: %q", seam)
	}
	if blocks.Width(seam[:from]) < cardLane+bodyIndent {
		t.Fatalf("the seam is not inset from the card's left edge: %q", seam)
	}
	if end := blocks.Width(seam[:to]); end > 90-bodyIndent {
		t.Fatalf("the seam runs to the card's right edge (ends at %d of %d): %q",
			end, 90, seam)
	}
	// The result's first line is the answer's LEAD and stands one tier above
	// the body under it (§16's dim ramp, three tiers on the surface).
	coloured, _ := deliveredApp(t, tokens.TrueColor)
	lit := strings.Join(lastCard(t, coloured).Rows(90), "\n")
	primary := tokens.TextPrimary.Fg(tokens.TrueColor, tokens.FocusNormal)
	for _, row := range strings.Split(lit, "\n") {
		if strings.Contains(ansi.Strip(row), "Three hot paths") && !strings.Contains(row, primary) {
			t.Fatalf("the answer's lead is not at the primary tier: %q", ansi.Strip(row))
		}
	}
}

// A FAILED TASK KEEPS THE CARD AND TURNS THE EDGE CORAL (§4's failed variant).
func TestAFailedDeliveryTurnsTheEdgeCoral(t *testing.T) {
	backend := board()
	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-1" {
			backend.nodes[i].Status = store.Failed
		}
	}
	app := colourApp(t, backend)
	backend.add(store.Message{SessionID: testSession, Role: store.RoleSystem, NodeID: "job-1",
		Body: "did not finish — the provider dropped the stream"})
	poll(t, app)

	block := lastCard(t, app)
	rows := block.Rows(90)
	coral := tokens.Coral.Fg(tokens.TrueColor, tokens.FocusNormal)
	found := false
	for _, row := range rows {
		at := strings.Index(ansi.Strip(row), blocks.AccentEdge)
		if at != 0 {
			continue
		}
		if strings.Contains(row, coral) {
			found = true
		}
	}
	if !found {
		t.Fatalf("a failed delivery's edge is not coral:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.Contains(ansi.Strip(strings.Join(rows, "\n")), "dropped the stream") {
		t.Fatalf("the failed card does not say why:\n%s", ansi.Strip(strings.Join(rows, "\n")))
	}
}

// THE DOOR OPENS AND CLOSES, and says how much it is holding on the way (§20's
// summary tails: never a silent cut).
//
// It says it in the ONE disclosure grammar every other door in the product
// wears ([blocks.Disclose]) — one mark, one count, one unit, and no verb. It
// used to say `▸ view more (2 lines)`, which spelled "more" once in the chevron
// and again in the words.
func TestTheDeliveryDoorOpensAndSaysWhatItHolds(t *testing.T) {
	app, _ := deliveredApp(t, tokens.NoColor)
	block := lastCard(t, app)
	if !block.collapsible || !block.tailFold {
		t.Fatal("the delivery card has no door of its own")
	}
	shut := ansi.Strip(strings.Join(block.Rows(90), "\n"))
	if !strings.Contains(shut, blocks.Disclose(false, 2, "line", "lines")) {
		t.Fatalf("the door does not say how much it holds:\n%s", shut)
	}
	if strings.Contains(shut, "view more") {
		t.Fatalf("the door still spells a verb the chevron already says:\n%s", shut)
	}
	if strings.Contains(shut, "lines than fit") {
		t.Fatalf("the fold is holding nothing — the tail is already on screen:\n%s", shut)
	}
	// The whole row is the door, and it is the row the pointer resolves to.
	if !block.isFoldRow(block.foldLine, 90) {
		t.Fatalf("the disclosure row at %d does not answer a click", block.foldLine)
	}
	block.SetExpanded(true)
	open := ansi.Strip(strings.Join(block.Rows(90), "\n"))
	if !strings.Contains(open, "lines than fit") {
		t.Fatalf("opening the card did not show what it was holding:\n%s", open)
	}
	if !strings.Contains(open, tokens.GlyphExpanded) {
		t.Fatalf("the opened card carries no ▾ witness:\n%s", open)
	}
}
