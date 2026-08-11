package chat

import (
	"image"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Per-block disclosure, driven through the real app and read off real frames.
//
// THE DEFECT THESE PIN, in the reporter's own words: "I asked a deep research
// bitcoin and it shows nothing when I click and go there. I can't open anything
// in chat when it has a drop-down-like thing — it should all be click-to-expand
// and click-somewhere-inside-to-collapse for EVERYTHING we show."
//
// Both halves of that sentence are one defect. Reproduced against their own
// journal at 120x36: the room 13.15 built DID draw the record — a charge row and
// a delivery card — and every word of the answer sat behind `▸ 7 lines` and
// `▸ 62 lines`, with the one line above the fold being the worker clearing its
// throat. Clicking either row moved nothing; hovering either row changed no
// byte. "It shows nothing when I click and go there" is a literal, correct
// description of that frame.
//
// THE FIXTURE IS THE SHAPE OF THEIR DATA AND NONE OF ITS CONTENT. What made
// their room read empty is structural and reproduces on any subject: a job
// parented on the SPINE (so its ending is announced as a delivery message rather
// than left in nodes.summary), DONE, ATOMIC (so the room has no part rows to
// carry it), with a brief and a summary both long enough to fold, and a summary
// whose FIRST LINE is a preamble — which is what splitHeadline correctly keeps
// above the fold and what makes the visible row say nothing. Around it, the main
// session's own turns, because the same collapsed card is what the conversation
// draws too.

const (
	// foldGist is the line the fold leaves visible: a worker announcing that it
	// is about to answer. It is not a defect that it is shown — it is the first
	// line the record has — and it is the whole reason the fold behind it has to
	// be reachable by the hand already pointing at it.
	foldGist = "I have what I need. Let me put it together:"
	// foldAnswer is the substance, which only an opened fold ever shows.
	foldAnswer = "The tide crests at 4:12pm and the second crest is smaller."
)

// foldBoard is the reporter's journal shape with their subject removed: one
// root-parented, settled, atomic job carrying a long brief and a long summary,
// exactly one node-anchored message (its own announced ending), and the main
// session's turns interleaved around it by sequence.
func foldBoard() *boardBackend {
	backend := board()
	// A brief long enough that splitGist has something to fold, so the charge
	// row is collapsible exactly as theirs was (1,601 characters on the wire).
	brief := "Report the current state of the harbour tide: the live height, the " +
		"movement over the last day and week, and the concrete drivers behind " +
		"it — weather, moon phase, and anything else notable. " +
		strings.Repeat("Give the full substantive answer inline. ", 12)
	// The answer sits at the END of the summary, so a transcript following its
	// tail shows it the moment the fold opens — the same motion the real one
	// makes, rather than a scroll position a test would have to arrange.
	summary := foldGist + "\n\n" +
		strings.Repeat("Supporting detail that only an opened fold ever shows.\n", 8) +
		foldAnswer

	backend.nodes = append(backend.nodes,
		store.Node{ID: "job-9", Title: "Harbour tide briefing", Status: store.Done,
			CreatedSeq: 40, UpdatedSeq: 52, Brief: brief, Summary: summary})
	backend.usage["job-9"] = store.JobUsage{Cost: 0.0029}
	// ONE message, and it is the job's own ending: role system, node-anchored,
	// no command sequence — which is what isDelivery reads, and what makes the
	// room draw 13.10's card instead of a second work row.
	backend.node["job-9"] = []store.Message{{
		Seq: 52, SessionID: testSession, Role: store.RoleSystem,
		NodeID: "job-9", Body: summary,
	}}
	// The same ending, and the turns around it, in the conversation the job was
	// commissioned from. Their journal interleaves exactly this way: the reader
	// asks, the head says it has put the work in hand, and the settled card
	// lands in the room the asking happened in.
	backend.add(store.Message{Seq: 50, SessionID: testSession, Role: store.RoleUser,
		Body: "can you look at and see what the harbour tide is doing"})
	backend.add(store.Message{Seq: 51, SessionID: testSession, Role: store.RoleAgent,
		Body: "I've put a research task in hand. I'll report back when it lands."})
	backend.add(store.Message{Seq: 52, SessionID: testSession, Role: store.RoleSystem,
		NodeID: "job-9", Body: summary})
	return backend
}

// foldApp is that board, polled, with the conversation on screen.
func foldApp(t *testing.T) *App {
	t.Helper()
	app := newTestApp(foldBoard(), &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	return app
}

// foldRowOf finds the screen row a block drew its disclosure row at, asked of
// the SAME resolver a pointer uses, and then checks the picture agrees.
//
// Both halves matter. Asking the resolver is what makes the click in these tests
// the click a hand makes rather than an offset a test author computed; checking
// the frame is what stops the resolver passing on a row the paint never drew.
func foldRowOf(t *testing.T, app *App, width, height int, id string) int {
	t.Helper()
	frame := strings.Split(ansi.Strip(app.Frame(width, height)), "\n")
	for y := 0; y < len(frame); y++ {
		block, _, ok := app.pane.foldAt(y)
		if !ok || block.ID() != id {
			continue
		}
		if !strings.Contains(frame[y], tokens.GlyphCollapsed) &&
			!strings.Contains(frame[y], tokens.GlyphExpanded) {
			t.Fatalf("row %d resolves to %q but shows no fold affordance: %q",
				y, id, frame[y])
		}
		return y
	}
	t.Fatalf("block %q drew no fold row on a %dx%d frame:\n%s",
		id, width, height, strings.Join(frame, "\n"))
	return -1
}

// foldingBlock is the last collapsible block in a transcript: the settled card
// the reader was looking at when they clicked.
func foldingBlock(t *testing.T, app *App, list *blocks.Transcript) *messageBlock {
	t.Helper()
	for i := list.Len() - 1; i >= 0; i-- {
		if block, ok := list.Block(i).(*messageBlock); ok && block.collapsible {
			return block
		}
	}
	t.Fatal("the fixture has no collapsible block, so nothing here proves anything")
	return nil
}

// THE DEFECT ITSELF, in the conversation: the settled card the reader was
// looking at held the whole answer, said one meaningless line above the fold,
// and did not answer a click.
func TestClickingAFoldRowOpensThatBlock(t *testing.T) {
	app := foldApp(t)

	card := foldingBlock(t, app, app.transcript)
	y := foldRowOf(t, app, 100, 30, card.ID())
	if strings.Contains(ansi.Strip(app.Frame(100, 30)), foldAnswer) {
		t.Fatal("the fixture is not folded: the answer is visible before the click")
	}

	cmd := app.pane.Mouse(clickAt(4, y), image.Point{X: 4, Y: y})
	if cmd != nil {
		t.Fatal("opening a fold posted, read or navigated — it may only repaint")
	}
	frame := ansi.Strip(app.Frame(100, 30))
	if !strings.Contains(frame, foldAnswer) {
		t.Fatalf("clicking the fold row did not open the block:\n%s", frame)
	}
}

// "click-somewhere-inside-to-collapse": the same row is the way back. 4.3's
// grammar flips the glyph with it, so the row says which side of the fold it is
// on rather than only offering one direction.
func TestClickingAnOpenFoldRowClosesThatBlock(t *testing.T) {
	app := foldApp(t)

	card := foldingBlock(t, app, app.transcript)
	y := foldRowOf(t, app, 100, 30, card.ID())
	app.pane.Mouse(clickAt(4, y), image.Point{X: 4, Y: y})
	if !strings.Contains(ansi.Strip(app.Frame(100, 30)), foldAnswer) {
		t.Fatal("the first click did not open the block")
	}
	// 4.3's grammar flips with the state, so the row says which side it is on.
	y = foldRowOf(t, app, 100, 30, card.ID())
	if line := strings.Split(ansi.Strip(app.Frame(100, 30)), "\n")[y]; !strings.Contains(
		line, tokens.GlyphExpanded) {
		t.Fatalf("the opened block's header does not carry ▾: %q", line)
	}
	app.pane.Mouse(clickAt(4, y), image.Point{X: 4, Y: y})
	if strings.Contains(ansi.Strip(app.Frame(100, 30)), foldAnswer) {
		t.Fatalf("clicking the open row did not close it:\n%s", ansi.Strip(app.Frame(100, 30)))
	}
}

// THE ROOM HALF, which is where the reporter met it. An atomic job's room is a
// charge row and a delivery card and nothing else, so if the folds do not open
// there is nothing in the room at all.
func TestClickingAFoldRowInsideATaskRoomOpensIt(t *testing.T) {
	app := newTestApp(foldBoard(), &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	enterRoom(t, app, "5") // Harbour tide briefing, newest first

	frame := ansi.Strip(app.Frame(120, 30))
	if !strings.Contains(frame, foldGist) {
		t.Fatalf("the room did not draw the job's ending at all:\n%s", frame)
	}
	if strings.Contains(frame, foldAnswer) {
		t.Fatalf("the room is not folded, so this test proves nothing:\n%s", frame)
	}

	card := foldingBlock(t, app, app.view.transcript)
	y := foldRowOf(t, app, 120, 30, card.ID())
	app.pane.Mouse(clickAt(4, y), image.Point{X: 4, Y: y})
	if got := ansi.Strip(app.Frame(120, 30)); !strings.Contains(got, foldAnswer) {
		t.Fatalf("the room's fold did not open on a click:\n%s", got)
	}
}

// 7.2's actual requirement, and the only hard half: "state that survives
// re-render". A task room rebuilds its whole block list every time the journal
// moves (13.15's decision 2), so a fold flag living on a block would be thrown
// away several times a second while a job runs.
func TestAnOpenedFoldSurvivesTheRoomBeingRepainted(t *testing.T) {
	backend := foldBoard()
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	enterRoom(t, app, "5")

	card := foldingBlock(t, app, app.view.transcript)
	y := foldRowOf(t, app, 120, 30, card.ID())
	app.pane.Mouse(clickAt(4, y), image.Point{X: 4, Y: y})
	if !strings.Contains(ansi.Strip(app.Frame(120, 30)), foldAnswer) {
		t.Fatal("the click did not open the room's fold")
	}

	// The journal moves in a way that changes a row already on screen — the
	// exact move that forces the rebuild — and the reader's fold must not close.
	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-9" {
			backend.nodes[i].UpdatedSeq = 90
		}
	}
	backend.journal++
	poll(t, app)
	app.paintRoom()

	if got := ansi.Strip(app.Frame(120, 30)); !strings.Contains(got, foldAnswer) {
		t.Fatalf("a repaint slammed the reader's fold shut:\n%s", got)
	}
}

// The two doors cannot disagree. ctrl+r is the room-wide key 13.10 advertises;
// a reader who opened three rows by hand and then asked for ALL of them has
// asked for all of them, and an override surviving that would be rows quietly
// contradicting the key just pressed.
func TestTheRoomWideToggleOutranksEveryPerBlockChoice(t *testing.T) {
	app := foldApp(t)

	card := foldingBlock(t, app, app.transcript)
	y := foldRowOf(t, app, 100, 30, card.ID())
	app.pane.Mouse(clickAt(4, y), image.Point{X: 4, Y: y})
	if len(app.folds) != 1 {
		t.Fatalf("the click did not record a per-block choice: %+v", app.folds)
	}
	// ctrl+r opens everything, so the row the pointer already opened stays open
	// and the overrides are gone.
	app.toggleReceipts()
	if len(app.folds) != 0 {
		t.Fatalf("the room-wide toggle left per-block overrides behind: %+v", app.folds)
	}
	if !strings.Contains(ansi.Strip(app.Frame(100, 30)), foldAnswer) {
		t.Fatal("ctrl+r after a click closed what the click had opened")
	}
	// And again shuts everything, with nothing left holding a row open.
	app.toggleReceipts()
	if strings.Contains(ansi.Strip(app.Frame(100, 30)), foldAnswer) {
		t.Fatal("ctrl+r did not close the row a pointer had opened")
	}
}

// The hover law, 13.14: an interactive control may not live permanently in the
// dimmest tier (5.22's amendment). The fold hint did exactly that until this
// lane — chrome forever, with no focus to rise to — so a reader had no way to
// learn the row was a door.
func TestHoveringAFoldRowPromotesIt(t *testing.T) {
	// The one test here that needs colour: a promotion IS an escape sequence,
	// and NoColor (which keeps every other frame assertable as text) paints
	// nothing, so under it the law under test cannot be observed at all.
	app := New(Options{
		Backend: foldBoard(), Commander: &fakeCommander{model: "anthropic/claude-k3"},
		Session: testSession, Profile: tokens.TrueColor, Now: fixedNow,
		PollEvery: time.Millisecond,
		Root:      "/home/someone/aforge-v2", Home: "/home/someone",
	})
	poll(t, app)

	card := foldingBlock(t, app, app.transcript)
	y := foldRowOf(t, app, 100, 30, card.ID())
	at := image.Point{X: 4, Y: y}
	rest := strings.Split(app.Frame(100, 30), "\n")[y]

	// The bool IS the contract: 13.14's hover door returns "did my bytes move"
	// and the SHELL turns that into a repaint. A test that invalidated first
	// would be asserting about a chain the runtime does not have.
	if !app.pane.Hover(at, true) {
		t.Fatal("hovering the fold row moved nothing")
	}
	app.shell.Invalidate()
	hovered := strings.Split(app.Frame(100, 30), "\n")[y]
	if hovered == rest {
		t.Fatalf("the hovered fold row paints identically to the row at rest:\n%q", rest)
	}
	// The PROMOTION is paint and only paint: the words are the same words.
	if ansi.Strip(hovered) != ansi.Strip(rest) {
		t.Fatalf("hover changed the text of the row:\n rest: %q\nhover: %q",
			ansi.Strip(rest), ansi.Strip(hovered))
	}
	// And leaving puts it back, so a pointer that has moved on leaves no mark.
	if !app.pane.Hover(image.Point{}, false) {
		t.Fatal("leaving the fold row moved nothing")
	}
	app.shell.Invalidate()
	if left := strings.Split(app.Frame(100, 30), "\n")[y]; left != rest {
		t.Fatalf("the row did not go back to rest:\n rest: %q\nleft: %q", rest, left)
	}
}

// A hover may never act. [tui2.PaneHover] returns a bool by type so it cannot,
// and this pins the other half: sweeping the fold rows changes no fold.
func TestHoveringAFoldRowNeverOpensIt(t *testing.T) {
	app := foldApp(t)
	before := ansi.Strip(app.Frame(100, 30))
	for y := 0; y < 30; y++ {
		app.pane.Hover(image.Point{X: 4, Y: y}, true)
	}
	app.pane.Hover(image.Point{}, false)
	if len(app.folds) != 0 {
		t.Fatalf("a hover recorded a fold decision: %+v", app.folds)
	}
	if got := ansi.Strip(app.Frame(100, 30)); got != before {
		t.Fatalf("a pointer sweep over the transcript changed what it says:\n%s", got)
	}
}

// Only a fold row is a fold target. The body of an open card, the prose of a
// turn and the blank between blocks are the record, not the affordance, and a
// click on them opens nothing — the same negative 13.14 pinned for option rows.
func TestClickingAnythingButAFoldRowFoldsNothing(t *testing.T) {
	app := foldApp(t)
	frame := ansi.Strip(app.Frame(100, 30))
	for y, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, tokens.GlyphCollapsed) {
			continue
		}
		app.pane.Mouse(clickAt(4, y), image.Point{X: 4, Y: y})
	}
	if len(app.folds) != 0 {
		t.Fatalf("a click off the fold row folded something: %+v", app.folds)
	}
	if got := ansi.Strip(app.Frame(100, 30)); got != frame {
		t.Fatalf("clicking the prose changed the transcript:\n%s", got)
	}
}

// The charge and the ending are separate rows and separate decisions. An atomic
// job's room is exactly these two, so if one carried the other's state the room
// would still be all-or-nothing — which is what ctrl+r already was.
func TestEachBlockHoldsItsOwnFoldState(t *testing.T) {
	app := newTestApp(foldBoard(), &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	enterRoom(t, app, "5")

	if got := app.view.transcript.Block(0).ID(); got != chargeBlockID {
		t.Fatalf("the room does not open on the charge: first block is %q", got)
	}
	charge, _ := app.view.transcript.Block(0).(*messageBlock)
	if charge == nil || !charge.collapsible {
		t.Fatalf("the charge is not collapsible, so this test proves nothing")
	}
	app.toggleFold(charge)
	if !charge.Expanded() {
		t.Fatal("the charge did not open")
	}
	// The ending is the other block, and it must still be shut.
	for i := 1; i < app.view.transcript.Len(); i++ {
		block, ok := app.view.transcript.Block(i).(*messageBlock)
		if ok && block.collapsible && block.Expanded() {
			t.Fatalf("opening the charge opened %q too", block.ID())
		}
	}
	if strings.Contains(ansi.Strip(app.Frame(120, 40)), foldAnswer) {
		t.Fatal("opening the charge revealed the ending's folded body")
	}
}

// A block born after the reader opened everything is born open. The room's
// blocks were always told (13.15), the conversation's never were, so a reader
// who pressed ctrl+r watched every new turn arrive shut.
func TestANewTurnIsBornOnTheSideOfTheFoldTheRoomIsOn(t *testing.T) {
	backend := foldBoard()
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	app.toggleReceipts()

	late := foldGist + "\n\n" + "A second job's answer, arriving after ctrl+r."
	backend.add(store.Message{Seq: 60, SessionID: testSession, Role: store.RoleSystem,
		NodeID: "job-9", Body: late})
	poll(t, app)

	if got := ansi.Strip(app.Frame(100, 40)); !strings.Contains(got,
		"A second job's answer, arriving after ctrl+r.") {
		t.Fatalf("a turn that arrived after ctrl+r was born folded:\n%s", got)
	}
}

// A block with nothing folded is never a fold target, so the pointer cannot
// claim a row that has no door behind it (5.20 rule 3).
func TestARowWithNothingFoldedIsNotAFoldRow(t *testing.T) {
	block := &messageBlock{id: "msg-1"}
	if block.isFoldRow(0) {
		t.Fatal("a block with no fold offered its header as a fold row")
	}
	block.collapsible, block.hidden = true, 3
	block.head.Title = "something"
	if !block.isFoldRow(0) {
		t.Fatal("a collapsible block did not offer its header as a fold row")
	}
	if block.isFoldRow(1) {
		t.Fatal("a body row was offered as a fold row")
	}
}

// press has no ctrl+r, and the ladder is where the key becomes the act — so the
// key itself is driven here rather than the method it reaches.
func TestTheFoldKeyStillReachesTheWholeRoom(t *testing.T) {
	app := foldApp(t)
	app.drain(app.key(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}))
	if !strings.Contains(ansi.Strip(app.Frame(100, 40)), foldAnswer) {
		t.Fatal("ctrl+r no longer opens the room's folds")
	}
}
