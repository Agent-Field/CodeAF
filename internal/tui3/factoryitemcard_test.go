package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE LIVE ITEM CARD, AS A PERSON MEETS IT ────────────────────────────────
//
// Every test below asserts what is on the screen and what the floor is asked
// (factoryitemcard.go).

// liveItem is #1 on the demo repository, new, with a $5 cap.
func liveItem() factory.Item {
	return factory.Item{ID: 1, Repo: "factory-demo", Kind: factory.KindIssue, Title: "Total double-counts an entry added twice",
		State: factory.StateNew, Cap: 5, Triage: factory.Triage{Type: "bug", Size: "S"},
		Stages: factory.CopyStages(factory.DefaultRecipe().For(factory.KindIssue)), Changed: factoryTestNow}
}

// liveFloor is a seam over one snapshot holding items, counting its loads.
func liveFloor(a *app, items ...factory.Item) *int {
	loads := 0
	snap := factory.Snapshot{Now: factoryTestNow, Items: items,
		Repos: []factory.Repo{{Name: "factory-demo", Recipe: factory.DefaultRecipe()}}}
	a.factory = factory.Seam{Load: func() (factory.Snapshot, error) {
		loads++
		return snap, nil
	}}
	a.factoryFold(snap)
	return &loads
}

// liveCardRows is a live card for item id, drawn plain at width.
func liveCardRows(a *app, id, width int) []string {
	card := &factoryCard{live: &factoryItemLive{id: id, ref: "#" + itoa(id)}, liveOnly: true}
	return questionPlainRows(FactoryCardRows(a, card, width, false))
}

// THE CARD'S ROWS SAY WHERE THE ITEM STANDS, in each state, boxed like a task
// element: the ref, the title and the facts; then the state and the stages.
func TestFactoryItemCardRowsPerState(t *testing.T) {
	lab := newQuestionLab(t)
	a := lab.a
	started := factoryTestNow.Add(-24 * time.Minute)
	running := liveItem()
	running.ID, running.State = 2, factory.StateRunning
	running.Stream = &factory.Stream{Started: started, Ended: started.Add(24 * time.Minute), Spent: 1.42,
		Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "write", State: factory.PhaseRunning}}}
	needs := liveItem()
	needs.ID, needs.State, needs.Question = 3, factory.StateNeedsYou, "plan is ready"
	landed := liveItem()
	landed.ID, landed.State = 4, factory.StateLanded
	landed.Proof = []factory.Claim{{OK: true}, {OK: true}, {OK: false}}
	landed.Policy = []factory.Claim{{OK: true}}
	shipped := liveItem()
	shipped.ID, shipped.State = 5, factory.StateShipped
	shipped.Stream = &factory.Stream{Spent: 1.9}
	liveFloor(a, liveItem(), running, needs, landed, shipped)

	rows := liveCardRows(a, 1, 110)
	if len(rows) != 3 {
		t.Fatalf("a live card is its head row, its state row and its foot; it drew:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[0], a.icon(tokens.GFileDocument)+" #1 Total double-counts an entry added twice") ||
		!strings.HasSuffix(strings.TrimRight(rows[0], " "), "factory-demo · bug · S") {
		t.Fatalf("the head row is %q", rows[0])
	}
	if !strings.Contains(rows[1], "new · budget $5") {
		t.Fatalf("a new item's row is %q", rows[1])
	}
	pending := a.icon(tokens.GStepPending)
	for _, cell := range []string{pending + " plan", pending + " write", pending + " test", pending + " proof"} {
		if !strings.Contains(rows[1], cell) {
			t.Fatalf("the strip lacks %q: %q", cell, rows[1])
		}
	}
	if strings.Contains(rows[1], "security") {
		t.Fatalf("a stage that is off is on the strip: %q", rows[1])
	}
	for _, c := range []struct {
		id   int
		want []string
	}{
		{2, []string{"running 24m · $1.42 / $5", a.icon(tokens.GStepDone) + " plan", a.icon(tokens.GStepRunning) + " write"}},
		{3, []string{a.icon(tokens.GNeedsHuman) + " plan is ready"}},
		{4, []string{"landed · 3" + a.icon(tokens.GSettled) + " 1" + a.icon(tokens.GFailed)}},
		{5, []string{"shipped · $1.90"}},
	} {
		rows := liveCardRows(a, c.id, 110)
		for _, want := range c.want {
			if len(rows) < 2 || !strings.Contains(rows[1], want) {
				t.Errorf("item %d: the state row lacks %q:\n%s", c.id, want, strings.Join(rows, "\n"))
			}
		}
	}
}

// AN ITEM'S OWN CONVERSATION OPENS ON ITS CARD: the brief's marker is drawn as
// the item, from the brief's own lines before the floor says anything, and the
// brief the model reads is not drawn.
func TestFactoryItemCardReplacesTheBriefsMarker(t *testing.T) {
	lab := newQuestionLab(t)
	a := lab.a
	brief := "[factory item #1]\n#1 · Total double-counts an entry added twice\nrepo factory-demo · tier owner\n\nCall this item #1 in everything you say."
	// THE BRIEF IS A LINE THE SESSION WROTE, which the transcript calls an
	// aside (session's SeedConversation).
	blocks, turns := a.replayBlocks([]session.DisplayEntry{{Role: "aside", Text: brief}, {Role: "user", Text: "skip review"}, {Role: "assistant", Text: "Here is the plan."}}, replayShape{})
	if turns != 1 || len(blocks) != 3 || blocks[0].kind != entryFactory || blocks[0].fac == nil || !blocks[0].fac.liveOnly {
		t.Fatalf("the brief replayed as %d turns and %+v", turns, blocks)
	}
	a.entries = blocks
	rows := questionPlainRows(FactoryCardRows(a, blocks[0].fac, 110, false))
	if len(rows) == 0 || !strings.Contains(rows[0], "#1 Total double-counts an entry added twice") || !strings.Contains(rows[0], "factory-demo") {
		t.Fatalf("the card before any read:\n%s", strings.Join(rows, "\n"))
	}
	if strings.Contains(strings.Join(rows, "\n"), "Call this item") {
		t.Fatal("the brief the model reads was drawn")
	}
	// AND ONCE THE FLOOR IS READ IT IS THE FLOOR'S ROW.
	liveFloor(a, liveItem())
	rows = questionPlainRows(FactoryCardRows(a, blocks[0].fac, 110, false))
	if !strings.Contains(strings.Join(rows, "\n"), "new · budget $5") {
		t.Fatalf("the card does not follow the floor:\n%s", strings.Join(rows, "\n"))
	}
	if _, ok := factoryMarkerCard("an ordinary first message"); ok {
		t.Fatal("an ordinary message was read as a brief")
	}
	if card, ok := factoryMarkerCard("[factory item #1540 · 7]\n#1540 · fix it\nrepo api"); !ok || card.live.id != 7 || card.live.ref != "#1540" || card.live.repo != "api" {
		t.Fatalf("a forge item's marker = %+v, %v", card, ok)
	}
}

// A REPLY THAT NAMES AN ITEM THE FLOOR KNOWS GETS ITS CARD, ONCE PER TURN; a
// ref the floor does not know gets none.
func TestFactoryItemCardUnderAReplyOncePerTurn(t *testing.T) {
	lab := newQuestionLab(t)
	a := lab.a
	a.workspace = "/work/factory-demo"
	liveFloor(a, liveItem())
	a.turn = 3
	a.entries = append(a.entries, entry{kind: entryAssistant, turn: 3, settled: true,
		text: "I looked at #1 and #1 again; #99 is not on the floor, and colour#1 is not a ref."})
	a.factoryRefCards()
	count := func() int {
		n := 0
		for _, e := range a.entries {
			if e.kind == entryFactory && e.fac != nil && e.fac.liveOnly {
				n++
			}
		}
		return n
	}
	if got := count(); got != 1 {
		t.Fatalf("the reply drew %d live cards, want 1", got)
	}
	a.factoryRefCards()
	if got := count(); got != 1 {
		t.Fatalf("asking again in the same turn drew %d cards", got)
	}
	a.turn = 4
	a.entries = append(a.entries, entry{kind: entryAssistant, turn: 4, settled: true, text: "#1 is ready to launch."})
	a.factoryRefCards()
	if got := count(); got != 2 {
		t.Fatalf("the next turn's mention drew %d cards in all, want 2", got)
	}
}

// `enter` ON THE SELECTED CARD OPENS THE ITEM PAGE, the floor's own road; a
// press is the same door, and the card's rows carry the hit for it.
func TestFactoryItemCardOpensTheItemPage(t *testing.T) {
	lab := newQuestionLab(t)
	a := lab.a
	a.width, a.height = 120, 40
	liveFloor(a, liveItem())
	a.entries = append(a.entries, entry{kind: entryFactory, turn: a.turn,
		fac: &factoryCard{live: &factoryItemLive{id: 1, ref: "#1"}, liveOnly: true}})
	at := len(a.entries) - 1
	hit := false
	for _, r := range a.visible(a.bodyWidth()) {
		if r.entry == at && r.hit == hitFactoryItem {
			hit = true
		}
	}
	if !hit {
		t.Fatal("the live card's rows are not a press target")
	}
	a.sel = at
	cmd := a.enter()
	spend(t, a, cmd)
	if !a.at(pageFactory) || !a.fp.open {
		t.Fatalf("enter on the card did not open the item page: page %v, open %v", a.page, a.fp.open)
	}
	if it, ok := a.factoryCursorItem(); !ok || it.ID != 1 {
		t.Fatalf("the item page is on %+v", it)
	}

	// AND A WINDOW WITH NO FLOOR DRAWS THE CARD AND OPENS NOTHING.
	bare := newQuestionLab(t).a
	bare.entries = append(bare.entries, entry{kind: entryFactory, fac: &factoryCard{live: &factoryItemLive{id: 1, ref: "#1", title: "t"}, liveOnly: true}})
	if _, ok := bare.openFactoryItemCard(len(bare.entries) - 1); ok {
		t.Fatal("a window with no floor opened an item page")
	}
}

// IT STAYS LIVE: the clock reads the floor while a card is in front, one Load
// a beat, and stops when no card is; and over a window with no floor the card
// takes the item the news carried.
func TestFactoryItemCardStaysLive(t *testing.T) {
	lab := newQuestionLab(t)
	a := lab.a
	loads := liveFloor(a, liveItem())
	if cmd := a.factoryCardPollArm(); cmd != nil {
		t.Fatal("the clock started with no card on screen")
	}
	a.entries = append(a.entries, entry{kind: entryFactory, fac: &factoryCard{live: &factoryItemLive{id: 1, ref: "#1"}, liveOnly: true}})
	if cmd := a.factoryCardPollArm(); cmd == nil {
		t.Fatal("the clock did not start for a card")
	}
	if cmd := a.factoryCardPollArm(); cmd != nil {
		t.Fatal("a second road started a second clock")
	}
	// THE CACHED ROWS GO STALE WHEN THE ITEM MOVES, and only then.
	e := &a.entries[len(a.entries)-1]
	if !a.factoryLiveStale(e) || a.factoryLiveStale(e) {
		t.Fatal("the first look is not stale, or the second is")
	}
	moved := liveItem()
	moved.Cap, moved.Changed = 8, factoryTestNow.Add(time.Minute)
	a.factoryFold(factory.Snapshot{Now: factoryTestNow, Items: []factory.Item{moved}})
	if !a.factoryLiveStale(e) {
		t.Fatal("a moved item left the card's rows cached")
	}
	before := *loads
	spend(t, a, a.factoryCardPoll())
	if *loads != before+1 {
		t.Fatalf("one beat read the floor %d times", *loads-before)
	}
	a.entries = nil
	if cmd := a.factoryCardPoll(); cmd != nil {
		t.Fatal("the clock went on with no card on screen")
	}

	// THE NEWS IS DRAWN WHERE THERE IS NO FLOOR.
	far := newQuestionLab(t).a
	far.entries = append(far.entries, entry{kind: entryFactory, fac: &factoryCard{live: &factoryItemLive{id: 1, ref: "#1", title: "Total double-counts an entry added twice"}, liveOnly: true}})
	changed := liveItem()
	changed.Cap = 8
	far.itemChanged(session.Event{Kind: session.EventItemChanged, FactoryItem: &session.ItemNotice{ID: "g9", Item: 1, Now: &changed}})
	rows := questionPlainRows(FactoryCardRows(far, far.entries[0].fac, 110, false))
	if len(rows) < 2 || !strings.Contains(rows[1], "new · budget $8") {
		t.Fatalf("the card did not take the news:\n%s", strings.Join(rows, "\n"))
	}
}

// THE CARD STOPS SHORT OF THE TRANSCRIPT'S EDGE by the margin a task element's
// head keeps, so it never runs into the side divider.
func TestFactoryItemCardKeepsTheTaskMargin(t *testing.T) {
	lab := newQuestionLab(t)
	a := lab.a
	liveFloor(a, liveItem())
	for _, w := range []int{160, 100} {
		for i, row := range liveCardRows(a, 1, w) {
			if got := ansi.StringWidth(row); got > w-factoryCardMargin {
				t.Fatalf("width %d row %d is %d cells, over %d: %q", w, i, got, w-factoryCardMargin, row)
			}
		}
	}
}

// A MARKDOWN HEADING IN THE PEEK IS BOLD, not a body line: the fixture's
// `## Claims` is drawn with the bold attribute on.
func TestFactoryPeekHeadingIsBold(t *testing.T) {
	a := factoryPlaceLab(t)
	factoryNoForge(t, a, 4)
	for at, i := range factoryWalk(a.fp.snap) {
		if a.fp.snap.Items[i].ID != 4 {
			continue
		}
		a.fp.cursor = at
		for _, row := range a.factoryPane(factoryPaneW(150), 30) {
			if strings.Contains(ansi.Strip(row), "Claims") && strings.Contains(row, "\x1b[1m") {
				return
			}
		}
		t.Fatal("the heading `Claims` is not drawn bold in the peek")
	}
	t.Fatal("item 4 is not on the floor")
}
