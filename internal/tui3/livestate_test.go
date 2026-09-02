package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// revealLump is a paragraph a stalled wire dumps in one event: past
// [revealAtOnce], so it opens a walk, and long enough that one slot of the
// clock cannot finish it.
var revealLump = strings.Repeat("the loader never makes the map. ", 13)

// midWalk puts a half-walked lump in a live conversation block and hands back
// the surface and the block's INDEX — never a pointer, because a door that
// appends may move the slice out from under one.
func midWalk(t *testing.T) (*app, int) {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 80, 24
	slot, _ := revealClock(a)
	a.applyEvent(text(session.EventTextDelta, revealLump), true)
	if a.live < 0 {
		t.Fatal("the lump did not open a live block")
	}
	slot()
	at := a.live
	if !a.entries[at].revealing() {
		t.Fatalf("the block is not mid-walk: %d of %d", a.entries[at].edge, len(a.entries[at].text))
	}
	return a, at
}

// A BLOCK STOPS PACING THE MOMENT IT STOPS BEING LIVE (livestate.go).
//
// Every door out of the live state is walked here with an unread remainder
// still on the block, because the failure this pins is silent: the frame clock
// only walks the block a live pointer names ([app.liveRevealing]), so one that
// leaves without snapping freezes mid-word for the rest of the session and no
// later frame ever puts it right.
func TestEveryDoorOutOfTheLiveStateSnapsTheEdge(t *testing.T) {
	for _, door := range []struct {
		name string
		shut func(t *testing.T, a *app, at int)
	}{
		{"the turn settles", func(_ *testing.T, a *app, _ int) { a.settleTurn() }},
		{"the block is closed", func(_ *testing.T, a *app, _ int) { a.closeLive() }},
		{"the stream is cut", func(_ *testing.T, a *app, _ int) { a.dropLive() }},
		{"the transcript is rebuilt", func(_ *testing.T, a *app, _ int) { a.rebuildTranscript() }},
		{"the conversation is left", func(_ *testing.T, a *app, _ int) { a.clearConversation() }},
	} {
		t.Run(door.name, func(t *testing.T) {
			a, at := midWalk(t)
			whole := a.entries[at].text
			door.shut(t, a, at)
			// A door that THREW THE BLOCK AWAY has nothing left to be wrong
			// about; one that kept it must have the whole of it.
			if at >= len(a.entries) || a.entries[at].text != whole {
				return
			}
			if got := a.entries[at].revealed(); got != whole {
				t.Fatalf("the block was left %d of %d bytes drawn, cut at %q",
					len(got), len(whole), got[max(0, len(got)-24):])
			}
			if a.entries[at].revealing() {
				t.Fatal("the block is still asking the clock to walk it")
			}
		})
	}
}

// AND THE THOUGHT BLOCK, whose two doors fold it rather than end the turn.
func TestTheThoughtBlockSnapsWhenItFolds(t *testing.T) {
	for _, door := range []struct {
		name string
		shut func(a *app)
	}{
		{"it settles under the answer", func(a *app) { a.settleThought() }},
		{"it collapses", func(a *app) { a.collapseThought() }},
	} {
		t.Run(door.name, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			a.width, a.height = 80, 24
			slot, _ := revealClock(a)
			a.applyEvent(text(session.EventReasoning, revealLump), true)
			if a.think < 0 {
				t.Fatal("the lump did not open a thinking block")
			}
			slot()
			at := a.think
			if !a.entries[at].revealing() {
				t.Fatal("the thinking block is not mid-walk")
			}
			door.shut(a)
			if got := a.entries[at].revealed(); got != a.entries[at].text {
				t.Fatalf("a folded thought was left %d of %d bytes drawn",
					len(got), len(a.entries[at].text))
			}
		})
	}
}

// AND A SETTLED BLOCK IS FORMATTED WHOLE. The markdown a reader ends up with is
// the whole document and not the prefix the edge had reached, which is the
// reading a mid-walk settle would have got wrong.
func TestASettleFormatsTheWholeBlockAndNotTheDrawnPrefix(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 80, 24
	slot, _ := revealClock(a)
	body := "# Findings\n\n" + revealLump + "\n\nand **that** is the whole of it."
	a.applyEvent(text(session.EventTextDelta, body), true)
	slot()
	at := a.live
	a.settleTurn()

	rows := plain(strings.Join(a.entryRows(a.conversation(), at, a.width), "\n"))
	for _, want := range []string{"Findings", "the whole of it"} {
		if !strings.Contains(rows, want) {
			t.Fatalf("the settled block lost %q — it was formatted at the edge:\n%s", want, rows)
		}
	}
}

// AND THE NODE'S PAGE, which has the same two pointers over its own list. The
// append door is the one that was actually broken on dev: it let the pointer go
// without snapping, so a note or a tool row landing under a streaming answer
// froze that answer mid-word for as long as the page was open.
func TestARoomsBlockSnapsWhenItStopsBeingLive(t *testing.T) {
	for _, door := range []struct {
		name string
		shut func(a *app)
	}{
		{"the page's block is closed", func(a *app) { a.roomCloseLive() }},
		{"something lands under it", func(a *app) {
			a.roomAppend(entry{kind: entryNote, text: "a note", turn: a.room.turn})
		}},
	} {
		t.Run(door.name, func(t *testing.T) {
			a, _, _ := roomApp(t)
			clickRail(t, a, 0)
			slot, _ := revealClock(a)
			a.roomSay(revealLump, true)
			if a.room.live < 0 {
				t.Fatal("the lump did not open a live block on the page")
			}
			slot()
			at := a.room.live
			if !a.room.entries[at].revealing() {
				t.Fatal("the page's block is not mid-walk")
			}
			door.shut(a)
			if got := a.room.entries[at].revealed(); got != revealLump {
				t.Fatalf("the page left the block %d of %d bytes drawn",
					len(got), len(revealLump))
			}
		})
	}
}

// AND THE ONE DOOR THAT IS NOT ONE. A person's line landing mid-stream leaves
// the block LIVE on purpose — [app.said] appends below it and keeps the pointer,
// so the next delta grows the answer rather than opening a second one under
// somebody else's sentence — and a block that is still live is a block the clock
// is still walking. The law is about leaving, not about every event.
func TestAPersonsLineLeavesTheWalkRunning(t *testing.T) {
	a, at := midWalk(t)
	a.said(entry{kind: entryUser, text: "do something else", turn: a.turn})
	if a.live != at {
		t.Fatalf("the answer stopped being live at %d, want %d", a.live, at)
	}
	if !a.entries[at].revealing() {
		t.Fatal("the walk was ended by a line that did not end the block")
	}
	catchUpReveal(a)
	if got := a.entries[at].revealed(); got != revealLump {
		t.Fatalf("the clock left the block %d of %d bytes drawn", len(got), len(revealLump))
	}
}

// AND AN ERRAND'S PANE, whose reply rows carry the same two facts.
func TestAnErrandsReplySnapsWhenItCloses(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	ex := askedHere(t, lab, a, "remind me at 6 to leave")
	slot, _ := revealClock(a)

	a.errandEvent(ex, text(session.EventTextDelta, revealLump))
	if ex.live < 0 {
		t.Fatal("the lump did not open a reply row")
	}
	slot()
	at := ex.live
	if !revealing(ex.rows[at].edge, ex.rows[at].text, ex.rows[at].settled) {
		t.Fatal("the reply row is not mid-walk")
	}
	ex.closeReply()
	row := ex.rows[at]
	if got := revealedText(row.text, row.edge, row.settled); got != revealLump {
		t.Fatalf("the pane left the reply %d of %d bytes drawn", len(got), len(revealLump))
	}
}

// ── AND NOBODY WRITES THOSE FIELDS BY HAND ──────────────────────────────────

// liveStateSeam is the one file allowed to take a block out of the live state.
const liveStateSeam = "livestate.go"

// liveStateOwn is the one write to a field of these names that is about
// something else entirely: connectcaps.go's tab has a `settled` bool meaning
// "this tab has been built at least once", which is not a block and has no edge.
//
// The cursor is spelled `edge` and not `shown` for this test's sake: half the
// page structs in this package carry a `shown` of their own (how many rows a
// list is drawing), and a law that has to keep a list of exceptions is a law
// that will be edited rather than obeyed.
var liveStateOwn = map[string]bool{"s.conn.settled": true}

// A SEVENTH DOOR MAY NOT BE WRITTEN BY HAND.
//
// livestate.go states the law and owns the act; this is what makes "owns" true.
// The six doors were six hand-written pairs of field writes in the six files
// that happened to own the events, which is why the third field the blocks
// gained was missing from all of them — and why the same defect (#178, a reply
// left permanently unsettled down a lane somebody forgot to teach) came back
// wearing the live edge instead of the settle.
//
// LETTING GO OF A POINTER AND SETTLING A BLOCK ARE ASSIGNMENTS, NEVER LITERALS.
// A block may be BORN settled — history replayed from a transcript, a note
// folded in whole — and those are composite literals, which say the block never
// streamed at all. Only a statement that turns a live block into a finished one
// is this law's business.
func TestNothingLeavesTheLiveStateOutsideTheSeam(t *testing.T) {
	fset := token.NewFileSet()
	for _, name := range placeSourceFiles(t) {
		if name == liveStateSeam {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("could not parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, lhs := range assign.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				spelling := types.ExprString(lhs)
				if liveStateOwn[spelling] {
					continue
				}
				switch sel.Sel.Name {
				case "settled", "edge":
					t.Errorf("%s: %s is written by hand — settle through %s",
						fset.Position(lhs.Pos()), spelling, liveStateSeam)
				case "live", "think":
					if !droppedPointer(assign, lhs) {
						continue
					}
					t.Errorf("%s: %s is let go by hand — leave the live state through %s",
						fset.Position(lhs.Pos()), spelling, liveStateSeam)
				}
			}
			return true
		})
	}
}

// droppedPointer reports whether this assignment sets that pointer to -1, which
// is the only way a live pointer is ever LET GO. Setting it to a real index is
// a block becoming live, which is the door in and not the door out.
func droppedPointer(assign *ast.AssignStmt, lhs ast.Expr) bool {
	for i, at := range assign.Lhs {
		if at != lhs || i >= len(assign.Rhs) {
			continue
		}
		unary, ok := assign.Rhs[i].(*ast.UnaryExpr)
		if !ok || unary.Op != token.SUB {
			return false
		}
		lit, ok := unary.X.(*ast.BasicLit)
		return ok && lit.Value == "1"
	}
	return false
}
