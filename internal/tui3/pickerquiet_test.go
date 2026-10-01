package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/config"
)

// ── ONE PAIR OF KEYS, TWO GESTURES, AND TIME BREAKS THE TIE ─────────────────
//
// `→` and `←` are what a hand reaches for at a tree and also how a caret walks
// text. The tie used to be broken by position alone, which made coming back out
// of an open fold cost one press per character typed: with `deep` in the box, `←`
// stepped through `p`, `e`, `e`, `d` while the fold stayed open. It is broken by
// TIME as well now ([pickerQuiet]) — still typing means still editing.

// WHILE THE BOX IS BEING EDITED THE ARROWS ARE THE CARET'S, which is the half of
// the rule that was already true and must stay true: a person mid-word has not
// finished typing and their `←` is a correction.
func TestWhileTheFilterIsBeingEditedTheArrowsWalkTheCaret(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 130, 40
	typeLine(t, a, "/model")
	typeInto(t, a, "deep")

	if !a.pick.editing() {
		t.Fatal("the box does not read as being edited on the keystroke itself")
	}
	// `→` at the END of what is typed has no character to step over and was
	// always the tree's, so the fold opens.
	drive(t, a, key("right"))
	if a.pick.unfold == "" {
		t.Fatal("→ at the end of the text did not open the fold")
	}
	// AND `←` IS STILL THE CARET'S while the box is warm: the caret steps back
	// and the fold stays exactly where it was.
	drive(t, a, key("left"))
	if a.pick.unfold == "" {
		t.Fatal("← closed the fold while the box was still being edited")
	}
	if got := a.pick.filter.cursor; got != 3 {
		t.Fatalf("the caret is at %d, want 3 — ← has to step over the p", got)
	}
}

// AND ONCE THE BOX HAS GONE QUIET THE SAME `←` IS THE TREE'S. The stamp is moved
// rather than the clock, because what is under test is the window and not
// [time.Now]: a test that slept 600ms would be a test that costs 600ms and still
// proves nothing about the boundary.
func TestOnceTheFilterGoesQuietTheArrowsWalkTheTree(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 130, 40
	typeLine(t, a, "/model")
	typeInto(t, a, "deep")
	drive(t, a, key("right"))
	if a.pick.unfold == "" {
		t.Fatal("the fold did not open")
	}

	a.pick.typed = time.Now().Add(-pickerQuiet)
	if a.pick.editing() {
		t.Fatal("the box still reads as being edited a whole window later")
	}
	// `←` closes the fold FROM THE END OF THE TEXT, which is the press that used
	// to cost four.
	at := a.pick.filter.cursor
	drive(t, a, key("left"))
	if a.pick.unfold != "" {
		t.Fatalf("← did not close the fold after the quiet window: %q", a.pick.unfold)
	}
	if got := a.pick.filter.cursor; got != at {
		t.Fatalf("← moved the caret from %d to %d as well as closing the fold", at, got)
	}
	// AND THE QUERY IS UNTOUCHED. Handing the arrows to the tree may not cost a
	// letter of what was typed.
	if got := string(a.pick.filter.value); got != "deep" {
		t.Fatalf("the filter reads %q after walking the tree", got)
	}
	// AND `→` OPENS IT AGAIN from the same caret, so the pair is symmetric.
	drive(t, a, key("right"))
	if a.pick.unfold == "" {
		t.Fatal("→ did not reopen the fold after the quiet window")
	}
}

// TYPING COMES BACK TO THE BOX, and so does every other edit. This is the rule
// that keeps the mode from being a trap: the way back is the thing a person is
// about to do anyway.
func TestAnyEditHandsTheArrowsBackToTheCaret(t *testing.T) {
	for _, probe := range []struct {
		name  string
		press string
		want  string
	}{
		// A letter, which is what "go back to filtering" means most of the time.
		{"a character", "s", "deeps"},
		// AND THE EDITS THAT ARE NOT CHARACTERS. A backspace is plainly editing,
		// and a mode that ignored it would take the arrows away from somebody in
		// the middle of fixing a typo.
		{"backspace", "backspace", "dee"},
		{"a word kill", "ctrl+w", ""},
		// AND `ctrl+b`, WHICH IS THE WAY BACK THAT COSTS NO LETTER. `←`'s
		// understudy is never the tree's, so it always reaches the caret — which
		// is what makes the mode escapable without editing the query.
		{"ctrl+b", "ctrl+b", "deep"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			laneLab(t, threeLanes())
			a := laneApp(t)
			a.width, a.height = 130, 40
			typeLine(t, a, "/model")
			typeInto(t, a, "deep")
			a.pick.typed = time.Now().Add(-pickerQuiet)

			drive(t, a, key(probe.press))
			if !a.pick.editing() {
				t.Fatal("the edit did not hand the arrows back to the caret")
			}
			if got := string(a.pick.filter.value); got != probe.want {
				t.Fatalf("the filter reads %q, want %q", got, probe.want)
			}
		})
	}
}

// A PASTE IS AN EDIT, arriving through a different door than the keyboard. It
// reaches the box without going through [picker.navigate] at all, so it stamps for
// itself ([picker.pasteFilter]) — and a paste that left the arrows on the tree
// would be the one way to put text in this box that did not count as typing it.
func TestAPasteIntoTheFilterCountsAsEditing(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 130, 40
	typeLine(t, a, "/model")
	typeInto(t, a, "deep")
	a.pick.typed = time.Now().Add(-pickerQuiet)
	if a.pick.editing() {
		t.Fatal("the box did not go quiet")
	}

	drive(t, a, tea.PasteMsg{Content: "seek"})
	if !a.pick.editing() {
		t.Fatal("a paste did not hand the arrows back to the caret")
	}
	if got := string(a.pick.filter.value); got != "deepseek" {
		t.Fatalf("the filter reads %q after the paste", got)
	}
	// AND IT FILTERED, which is the other half of what [picker.pasteFilter] owes:
	// text in the box that the list has not been re-ranked against is a list
	// showing rows the query excludes.
	if got := pickerIDs(a); len(got) != 1 || got[0] != flash {
		t.Fatalf("the paste left the list at %v", got)
	}
}

// AND OPENING THE LIST WITH A QUERY ALREADY IN IT IS NOT EDITING. `/model deep
// flash` finished its query before the list existed, so the arrows are the tree's
// on the first frame — which is the whole reason that door exists.
func TestOpeningThePickerPreFilledLeavesTheArrowsOnTheTree(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 130, 40
	typeLine(t, a, "/model deepseek flash")

	if a.pick.editing() {
		t.Fatal("a pre-filled list opened with the arrows on the caret")
	}
	if got := string(a.pick.filter.value); got != "deepseek flash" {
		t.Fatalf("the box reads %q", got)
	}
	// So `→` opens the providers on the very first press, with no wait.
	drive(t, a, key("right"))
	if a.pick.unfold == "" {
		t.Fatal("→ did not open the fold on a pre-filled list")
	}
}

// AND WALKING THE LIST DOES NOT. `↑`/`↓` move the cursor and leave the box alone,
// so reading a filtered list never quietly hands the arrows back — which is the
// whole point of paying attention to the box rather than to any keypress.
func TestWalkingTheListDoesNotHandTheArrowsBack(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 130, 40
	typeLine(t, a, "/model")
	typeInto(t, a, "e")
	a.pick.typed = time.Now().Add(-pickerQuiet)

	drive(t, a, key("down"), key("up"))
	if a.pick.editing() {
		t.Fatal("walking the list put the arrows back on the caret")
	}
}

// THE FOOT NAMES WHICHEVER KEY ACTUALLY WORKS. A foot that said `←` while `←` was
// stepping through a word is what made the fold feel broken in the first place,
// and a foot that says `tab` after the arrows have come back is the same lie the
// other way round.
func TestTheFootNamesTheKeyThatWorksRightNow(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 130, 40
	typeLine(t, a, "/model")
	typeInto(t, a, "deep")
	drive(t, a, key("right"), key("left")) // the caret steps back; the fold stays

	if got := a.hintWord(); !strings.Contains(got, "tab back") {
		t.Fatalf("mid-word the foot reads %q, want it to name tab", got)
	}
	a.pick.typed = time.Now().Add(-pickerQuiet)
	if got := a.hintWord(); !strings.Contains(got, "← back") {
		t.Fatalf("after the quiet window the foot reads %q, want it to name ←", got)
	}
}

// ── ENTER ON `openrouter` OPENS IT ──────────────────────────────────────────

// `enter` ON THE CONTAINER CHOOSES `default` AND SHOWS THAT IT DID. Shut, the
// gesture reads as "you have chosen openrouter", which sounds like a destination
// and hides that there was a list under it; open, with the band on `default`, it
// reads as the true sentence.
func TestEnterOnOpenrouterOpensItAndMarksDefault(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 130, 40
	typeLine(t, a, "/model")
	drive(t, a, key("right"), key("down")) // into the fold, onto openrouter

	row, on := a.pick.laneUnder()
	if !on || row.lane != laneRoutAt {
		t.Fatalf("the walk did not land on openrouter: %+v (on=%v)", row, on)
	}
	if a.pick.machines {
		t.Fatal("the machines were open before enter")
	}

	drive(t, a, key("enter"))

	// THE WRITE IS UNCHANGED — the container and the row inside it say the same
	// thing, which is what makes this a shortcut rather than a fourth answer.
	if got := config.LaneAt(a.profileDir, talkSlot); got != config.LaneOpenRouter {
		t.Fatalf("enter on openrouter wrote %q", got)
	}
	// AND THE FOLD IS OPEN, with the cursor on the row that now carries the
	// answer.
	if !a.pick.machines {
		t.Fatalf("enter on openrouter left its machines shut:\n%s", plain(frame(a)))
	}
	after, on := a.pick.laneUnder()
	if !on || after.lane != laneDefaultAt {
		t.Fatalf("the cursor sits on %+v (on=%v), want the default row", after, on)
	}
	if !a.pick.marked(a.pick.cursor) {
		t.Fatalf("the default row does not wear the mark:\n%s", plain(frame(a)))
	}
	// AND THE CONTAINER HAS GIVEN THE MARK UP, because one answer drawn twice is
	// two answers ([picker.marked]).
	for at, row := range a.pick.list {
		if row.lane == laneRoutAt && a.pick.marked(at) {
			t.Fatal("openrouter still wears the mark with its machines open")
		}
	}

	// AND THE LIST IS STILL UP, which is enter's own rule here.
	if !a.pick.open {
		t.Fatal("enter closed the picker")
	}
}

// AND `←` FROM THE ROW ENTER LANDED ON CLOSES ONE LEVEL, not two.
//
// This is the press a person makes next, and it was skipping a level: `default`'s
// lane number is negative like the two containers' are, so the test for "a row
// inside the machines" — which was `row.lane >= 0`, every real machine — did not
// cover it, and `←` there shut the model's whole fold. The gesture that opens the
// machines and the gesture that leaves them have to be the same length, or the
// tree is not a tree.
func TestLeftFromTheDefaultRowClosesOneLevelOnly(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 130, 40
	typeLine(t, a, "/model")
	drive(t, a, key("right"), key("down"), key("enter")) // enter on openrouter lands on default

	row, on := a.pick.laneUnder()
	if !on || row.lane != laneDefaultAt {
		t.Fatalf("enter did not land the cursor on default: %+v (on=%v)", row, on)
	}

	// ONE press: the machines shut and the model's own fold is still open, with
	// the cursor back on the row that holds them.
	drive(t, a, key("left"))
	if a.pick.machines {
		t.Fatal("← did not shut the machines")
	}
	if a.pick.unfold == "" {
		t.Fatal("← shut the model's fold as well — two levels on one press")
	}
	back, on := a.pick.laneUnder()
	if !on || back.lane != laneRoutAt {
		t.Fatalf("← left the cursor on %+v (on=%v), want openrouter", back, on)
	}

	// AND THE SECOND press shuts the model's fold, which is the other level.
	drive(t, a, key("left"))
	if a.pick.unfold != "" {
		t.Fatalf("the second ← left the fold open at %q", a.pick.unfold)
	}
}

// AND THE SAME IS TRUE FROM A MACHINE ROW, which is the half that already worked
// and must keep working: the two kinds of row inside the machines behave alike.
func TestLeftFromAMachineClosesOneLevelOnly(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 130, 40
	typeLine(t, a, "/model")
	drive(t, a, key("right"), key("down"), key("right")) // into the machines

	row, on := a.pick.laneUnder()
	if !on || row.lane < 0 {
		t.Fatalf("the walk did not land on a machine: %+v (on=%v)", row, on)
	}
	drive(t, a, key("left"))
	if a.pick.machines || a.pick.unfold == "" {
		t.Fatalf("← from a machine closed %v levels (machines=%v unfold=%q)",
			2, a.pick.machines, a.pick.unfold)
	}
}

// AND A SECOND `enter` ON THE SAME ROW IS NOT A TOGGLE. The cursor has moved into
// the fold, so there is no second press on the container to make — but a person
// who walks back up to it and presses enter again must not have it snap shut,
// because enter has never meant "close" anywhere in this list.
func TestEnterOnOpenrouterAgainLeavesItOpen(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width, a.height = 130, 40
	typeLine(t, a, "/model")
	drive(t, a, key("right"), key("down"), key("enter"))
	if !a.pick.machines {
		t.Fatal("the first enter did not open the machines")
	}

	// Back up onto the container and choose again.
	for a.pick.cursor > 0 {
		row, on := a.pick.laneUnder()
		if on && row.lane == laneRoutAt {
			break
		}
		drive(t, a, key("up"))
	}
	row, on := a.pick.laneUnder()
	if !on || row.lane != laneRoutAt {
		t.Fatalf("the walk back did not reach openrouter: %+v (on=%v)", row, on)
	}
	drive(t, a, key("enter"))
	if !a.pick.machines {
		t.Fatal("a second enter on openrouter shut its machines")
	}
}
