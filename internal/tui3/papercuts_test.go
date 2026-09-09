package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── EVERY REFUSAL NAMES A DOOR ──────────────────────────────────────────────

// THE TABLE IS THE POINT. A refusal that names no destination is a refusal
// somebody wrote in a hurry, and the only way to keep one from being written is
// to have somewhere every refusal has to be listed (roomrefusal.go). This walks
// that list.
func TestEveryRefusalOnTheTaskSurfacesNamesADoor(t *testing.T) {
	if len(taskRefusals) == 0 {
		t.Fatal("the refusal table is empty, so this test proves nothing")
	}
	for _, r := range taskRefusals {
		if strings.TrimSpace(r.what) == "" {
			t.Fatalf("a refusal with no fact in it: %+v", r)
		}
		if strings.TrimSpace(r.door) == "" {
			t.Fatalf("%q refuses and names nowhere for the words to go", r.what)
		}
		// A DOOR IS A PLACE, NOT A KEY. `esc to return` was what the finished
		// room said for a year, and esc is a way off the page rather than a
		// destination for a sentence — so a door has to name somewhere the words
		// can actually go: this window's own conversation, or — on a page read
		// through somebody else's — the conversation that owns the work, which is
		// the one place main is NOT (roomrefusal.go's [roomGuestFinishedRefusal]).
		if !strings.Contains(r.door, roomCrumbRoot) && !strings.Contains(r.door, refusalOwnerPlace) {
			t.Fatalf("%q names %q, which is not a place words can go", r.what, r.door)
		}
		for _, spelling := range []string{r.line(), r.short()} {
			if !strings.Contains(spelling, r.door) {
				t.Fatalf("the spelling %q dropped its door %q", spelling, r.door)
			}
		}
		// AND THE DOOR SURVIVES A NARROW FRAME. The fact is what degrades; a
		// refusal cut back to its fact is the defect all over again.
		narrow := r.fit(len(r.short()))
		if !strings.Contains(narrow, r.door) {
			t.Fatalf("at %d cells %q lost its door: %q", len(r.short()), r.what, narrow)
		}
		// The vocabulary law: none of this may read as machinery.
		for _, never := range []string{"auditor", "verdict", "verified", "refuted", "unavailable"} {
			if strings.Contains(strings.ToLower(r.line()), never) {
				t.Fatalf("the refusal %q says %q, which is machinery", r.line(), never)
			}
		}
	}
}

// AND THE ROOM OF A FINISHED NODE SAYS IT, with the parent named where there is
// one. This is the incident that found the defect: a correction typed at a task
// that had landed was refused with `task finished — esc to return`, and the words
// had two destinations the surface knew about and mentioned neither.
func TestAFinishedRoomNamesTheChatAndItsParent(t *testing.T) {
	a, _, _ := roomApp(t)
	// The parent lands first, then the child that names it.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(1, "Ship the port", session.TaskRunning,
		session.TaskNotice{})})
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(4, "Port the parser", session.TaskDone,
		session.TaskNotice{Parent: 1, Merge: mergeWordMerged})})
	a.openRoom(4, "Port the parser")
	a.room.done = true
	a.room.dirty = true

	page := roomText(a)
	for _, want := range []string{roomFinishedRefusal.what, refusalMainDoor, "Ship the port"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the finished room does not say %q:\n%s", want, page)
		}
	}
	// AND IT DOES NOT SPEND THE ROW ON A KEY THE LEGEND IS ALREADY CARRYING.
	if strings.Contains(page, "esc to return") {
		t.Fatalf("the foot names esc, which the legend and the header both already do:\n%s", page)
	}
}

// A NODE WITH NO PARENT IS OFFERED THE ONE DOOR IT HAS, and no clause about a
// parent nobody named. The emptiness law, said about a sentence.
func TestAFinishedRoomWithNoParentOffersOnlyTheChat(t *testing.T) {
	a, _, _ := roomApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(4, "Port the parser", session.TaskDone,
		session.TaskNotice{Merge: mergeWordMerged})})
	a.openRoom(4, "Port the parser")
	a.room.done = true
	a.room.dirty = true

	page := roomText(a)
	if !strings.Contains(page, roomFinishedRefusal.line()) {
		t.Fatalf("the finished room does not say %q:\n%s", roomFinishedRefusal.line(), page)
	}
	if strings.Contains(page, refusalParentDoor) {
		t.Fatalf("the room offers a parent that does not exist:\n%s", page)
	}
}

// ── THE COMPOSER OWNS THE LETTERS ───────────────────────────────────────────

// chordSentence carries every bare letter this surface has ever bound — the
// roster's widen `w`, the stop card's `x`, the settle answers `a l n d`, the
// proposal's `y r n`, the design card's `e`, and the home sheet's `k j g m p s`
// — so that a surface which eats any one of them cannot pass.
const chordSentence = "we all know exactly why any dry run needs a plan, so just go and mend the parser"

// A SENTENCE TYPED OVER THE ROSTER ARRIVES INTACT. This is the incident: the
// roster had been handed the keyboard and never given it back, its widen binding
// was the bare letter `w` read before the box, and sentences came out as
// `riting the port` and `orktree`.
//
// IT STARTS FROM AN EMPTY BOX ON PURPOSE. A guard that only bit once there were
// already words in the box would still have eaten the FIRST letter of every
// sentence beginning with `w`, which is exactly what `writing` and `worktree`
// were.
func TestASentenceTypedOverTheRosterArrivesIntact(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskDone,
		session.TaskNotice{Merge: mergeWordMerged})})
	drive(t, a, key(railHoldChord))
	if !a.railHold {
		t.Fatal("the roster was not handed the keyboard")
	}
	wide := a.railWide

	typeInto(t, a, chordSentence)

	if got := a.input.String(); got != chordSentence {
		t.Fatalf("the roster ate the sentence:\n want %q\n  got %q", chordSentence, got)
	}
	if a.railWide != wide {
		t.Fatal("a letter of somebody's sentence widened the column")
	}
	if a.stopping() {
		t.Fatal("a letter of somebody's sentence raised the stop card")
	}
}

// AND WITH WORDS IN THE BOX, NOTHING FIRES AT ALL — over a node that is still
// running, where `x` and the widen chord are both live. This is the half of the
// rule chordfocus.go states: the box wins every time.
func TestNoBareChordFiresWhileTheBoxHoldsWords(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskRunning,
		session.TaskNotice{})})
	drive(t, a, key(railHoldChord))
	typeInto(t, a, "note: ")
	wide := a.railWide

	typeInto(t, a, chordSentence)

	if got := a.input.String(); got != "note: "+chordSentence {
		t.Fatalf("a chord ate part of the draft:\n want %q\n  got %q", "note: "+chordSentence, got)
	}
	if a.railWide != wide {
		t.Fatal("the column widened under a draft")
	}
	if a.stopping() {
		t.Fatal("the stop card was raised under a draft")
	}
}

// THE SETTLE LETTERS ARE HELD TO THE SAME RULE, on the card and in the room
// alike: a landing does not get decided by the first word of a paragraph.
func TestTheSettleLettersStandDownUnderADraft(t *testing.T) {
	a, agent := settleApp(t)
	landUnverified(t, a)
	typeInto(t, a, "actually let me look at this first")

	for _, letter := range []string{"a", "l", "n", "d"} {
		drive(t, a, key(letter))
	}
	if len(agent.resolved) > 0 || len(agent.handed) > 0 {
		t.Fatalf("a letter of somebody's sentence answered the landing: %+v %+v",
			agent.resolved, agent.handed)
	}
}

// AND THE WIDEN CHORD IS NOT A LETTER ANY MORE. It is the one bare letter this
// surface ever bound to a view toggle rather than to an answer, so it is the one
// that moved (chordfocus.go says why).
func TestWidenIsAChordAndNotALetterBesideAColumn(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskDone,
		session.TaskNotice{Merge: mergeWordMerged})})
	drive(t, a, key(railHoldChord))
	wide := a.railWide

	drive(t, a, key(railWidenChord))
	if a.railWide == wide {
		t.Fatalf("%s did not widen the column", railWidenChord)
	}
	// AND THE HINT NAMES THE SPELLING THAT WORKS. A hint may only name a key
	// that works, which is why the bare letter cannot stay on this line.
	hint := a.railHoldHintWord()
	if !strings.Contains(hint, railWidenChord) {
		t.Fatalf("the hold hint does not name %q: %q", railWidenChord, hint)
	}
	if strings.Contains(hint, "· "+railWidenKey+" wide") {
		t.Fatalf("the hold hint still offers the bare letter: %q", hint)
	}
}

// ── THE SETTLE VERBS SHOW THEMSELVES ────────────────────────────────────────

// THE `!` SUMMONS SOMEBODY TO THE COLUMN, SO THE COLUMN HAS TO ANSWER. A node
// lands `needs your look`; until this the three words that answer it lived on a
// card in the conversation and at the foot of the node's own room, and nothing
// where the person was standing said which key to press.
func TestARowThatNeedsYourLookShowsItsVerbsBeforeTheRoomIsEntered(t *testing.T) {
	a, _ := settleApp(t)
	landUnverified(t, a)
	drive(t, a, key(railHoldChord))
	if a.railFocusNode() == nil {
		t.Fatal("the roster has no focused row to ask about")
	}

	hint := a.railHoldHintWord()
	// THE SLOT NAMES THE CARD'S OWN CHIPS. The two answers are the ask's
	// ([session.TaskAsk]), so a slot spelling `accept` over a card whose first
	// chip reads `resolve it` would be naming a key that is not there.
	for _, want := range []string{"a accept", "n not right"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("the hint is missing %q: %q", want, hint)
		}
	}
	// AND THE SLOT IS WHAT THE FRAME ACTUALLY DRAWS, not a string only this test
	// can see.
	if got := a.hintWord(); !strings.Contains(got, "a accept") {
		t.Fatalf("the frame's hint slot says something else: %q", got)
	}
}

// AND THE LETTERS IT NAMES ANSWER FROM THERE. A hint may only name a key that
// works, and a key that only acts once you have found out what it is for is a
// key that is not there.
func TestTheSettleVerbsAnswerFromTheRosterRow(t *testing.T) {
	a, agent := settleApp(t)
	landUnverified(t, a)
	drive(t, a, key(railHoldChord))

	drive(t, a, key("a"))
	if len(agent.resolved) != 1 || agent.resolved[0].answer != session.TaskAccept {
		t.Fatalf("the row did not take the answer: %+v", agent.resolved)
	}
	if agent.resolved[0].id != 7 {
		t.Fatalf("the answer went to node %d and not to the focused row", agent.resolved[0].id)
	}
	// ONCE ANSWERED THE SLOT GOES BACK TO THE MOVE KEYS, because the question is
	// gone and a hint may not name a key that no longer does anything.
	if hint := a.railHoldHintWord(); strings.Contains(hint, "a accept") {
		t.Fatalf("the hint still asks a question that was answered: %q", hint)
	}
}

// AND AN ORDINARY ROW IS OFFERED NOTHING. The verbs belong to the one state that
// is asking; a column that named them over merged work would be teaching a key
// that does nothing.
func TestAnOrdinaryRowIsOfferedNoSettleVerbs(t *testing.T) {
	a, agent := settleApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskDone,
		session.TaskNotice{Elapsed: time.Second, Merge: mergeWordMerged})})
	drive(t, a, key(railHoldChord))

	if hint := a.railHoldHintWord(); strings.Contains(hint, "a accept") {
		t.Fatalf("a merged row offers the answers: %q", hint)
	}
	drive(t, a, key("a"))
	if len(agent.resolved) > 0 {
		t.Fatalf("a merged row answered a question nobody asked: %+v", agent.resolved)
	}
}

// AND THE BARE LETTER SURVIVES WHERE THERE IS NO BOX TO STEAL FROM. Under the
// slim floor the roster is drawn over the whole frame and the composer is not on
// it, so `w` there cannot be the first letter of anybody's sentence — the home
// sheet's own rule, said about the roster.
func TestTheBareWidenLetterKeepsTheFullFrameRoster(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width, a.height = 60, 24
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskRunning,
		session.TaskNotice{})})
	drive(t, a, key(railHoldChord))
	if !a.railFull() {
		t.Fatalf("the roster is not over the body at %d columns", a.width)
	}
	wide := a.railWide

	drive(t, a, key(railWidenKey))
	if a.railWide == wide {
		t.Fatal("the bare letter did not widen the full-frame roster")
	}
}
