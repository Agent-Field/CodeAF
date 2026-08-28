package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE DESIGN'S QUESTION, ASKED WHERE THE PERSON IS STANDING.
//
// A harness design ends in one question — keep this page or do not — and until
// this file there was exactly one place to answer it: the card in the
// conversation (harnesscard.go). That was fine while the design was something
// that happened out of sight and arrived finished. It stopped being fine the
// moment a design became a ROOM you could walk into and watch, because the room
// is where a person actually is when the page lands: they opened it to see the
// thing being written, they read the card in the thread, and then there was
// nothing there to answer with. What people did instead was type "ok go build
// it" at the thread — a sentence nothing in this program acts on — and the
// thread, correctly and uselessly, told them the card was the only door.
//
// SO THE ROOM HAS THE SAME DOOR. Not a second question and not a copy of the
// card: the same question, with the same id, answered through the same method
// (session's ResolveHarness) and recorded in the same place, so the card out in
// the conversation shows the answer given in here and the row in here shows the
// answer given out there. Whichever is answered first wins and the other finds
// the question already gone — the engine's answers channel holds one and
// forgetHarness takes the rest.
//
// ── WHY IT IS A PINNED ROW AND NOT A BLOCK IN THE PAGE ──
//
// The card is already in the room, in the thread, drawn as prose the design
// itself wrote (session's harnessPageThread). It scrolls, which is right — it is
// history the moment the next thing is said. A question does not scroll. So what
// is pinned above the box is the ANSWER and not the page: two rows, in the hue
// this surface paints everything it is waiting on you for, sitting exactly where
// every other question on this surface sits.
//
// ── THE KEYS ──
//
// esc leaves the room and enter sends the message box, and neither may be taken:
// a person mid-sentence who reaches for enter must send their sentence. Nor may
// a bare letter — `x` is already the surface's stop, taken only over an empty
// box, and a second letter with a second rule about when it is a letter is how a
// message box stops being one. So the two answers are chords, which carry no
// text and can never be typed by accident: ctrl+k keeps it and ctrl+x drops it.
// Both were documented as unbound and both are bound now, in one place and only
// while this row is up.
//
// AND THE THIRD ANSWER IS THE BOX ITSELF, which is the whole reason the row says
// so out loud. Saying what is wrong with the page is a real answer to this
// question — the thread turns it into a rewrite (session's revise_design) — and
// it is the answer people reach for most. It needs no key because the box is
// already pointed at the thread.

// The row's words, spelled once.
const (
	// roomApprovalWord is the first row: what the surface is waiting for, and the
	// reassurance that comes with it. The second half is there because it is the
	// question people actually have while looking at a card they have not
	// answered — nothing has been written anywhere, and nothing will be.
	roomApprovalWord = "waiting on your approval — this design saves only if you say so"
	// The two chords, and the words beside them. Each key and its word are one
	// pressable target, which is the offer row's rule (harness.go): `[ctrl+k]` is
	// eight cells and `[ctrl+k] save it` is sixteen, and that is the difference
	// between a target a person hits and one they aim at.
	roomApprovalSaveKey  = "[ctrl+k]"
	roomApprovalSaveWord = " save it · "
	roomApprovalDropKey  = "[ctrl+x]"
	roomApprovalDropWord = " drop it · "
	// roomApprovalSayWord is the third answer, which has no key because it is the
	// box under this row.
	roomApprovalSayWord = "or say below what to change"
)

// roomApprovalCard is the design this room is standing in front of, or nil.
//
// IT IS THE CONVERSATION'S OWN CARD AND NOT A SECOND RECORD OF ANYTHING. One
// design is one [harnessCard], keyed by the id the answer goes back through, and
// a design node's ask id IS its task id — session mints one number and uses it
// for both (harness_task.go's reserveHarnessDesign). So the room looks its own
// node up in the card list and finds the same object the feed is drawing, which
// is what lets the two doors share one state without either telling the other
// anything.
func (a *app) roomApprovalCard() *harnessCard {
	if a.room == nil || a.room.orch != nil {
		return nil
	}
	node := a.tasks[a.room.id]
	if node == nil || node.kind != session.TaskKindHarness {
		return nil
	}
	card, _ := a.harnessCardOf(a.room.id)
	if card == nil || card.page == nil {
		return nil
	}
	return card
}

// roomApprovalAsking reports whether the row is asking rather than reporting:
// the design is at the phase where the only remaining step is the person's, and
// nobody has taken it yet.
//
// THE PHASE IS THE TEST AND THE CARD IS NOT. A card with a page on it stays in
// the feed forever — that is what a transcript is — so a row drawn off the page
// alone would still be asking a week later. The node's phase is the live fact
// about whether anything is waiting, and it is the same fact the roster's tally
// now reads (task.go's [taskAwaitsPerson]): while a rewrite is being written the
// phase goes back to "designing" and this row goes away with it, which is right,
// because at that moment there is nothing to approve.
func (a *app) roomApprovalAsking() bool {
	node := a.tasks[a.room.id]
	return taskAwaitsPerson(node)
}

// roomApprovalHeight is how many rows the block takes: two while it is asking,
// one while it is reporting the answer, none the rest of the time.
func (a *app) roomApprovalHeight() int {
	card := a.roomApprovalCard()
	if card == nil {
		return 0
	}
	if card.state != "" {
		// ANSWERED, AND THE ROW SAYS WHAT THE ANSWER WAS. It is one row and it is
		// brief: between the keypress and the node landing there are a few frames
		// in which the question is gone and the settle card has not arrived, and a
		// block that simply vanished in that gap would read as the keypress having
		// done nothing.
		return 1
	}
	if !a.roomApprovalAsking() {
		return 0
	}
	return 2
}

// roomApprovalRows draws it, in the question hue every block this surface is
// blocked on wears.
func (a *app) roomApprovalRows(width int) []string {
	// The targets are rewritten by every layout and by nothing else: a stale span
	// is a press that answers about a design that is no longer on screen.
	a.roomApprovalTaps = nil
	card := a.roomApprovalCard()
	if card == nil {
		return nil
	}
	if card.state != "" {
		return []string{a.pal.dim(fit(card.state, width))}
	}
	if !a.roomApprovalAsking() {
		return nil
	}
	parts := []string{
		roomApprovalSaveKey, roomApprovalSaveWord,
		roomApprovalDropKey, roomApprovalDropWord,
		roomApprovalSayWord,
	}
	// THE ANSWERS ARE NEVER WHAT GETS CUT, which is the offer row's law one lane
	// over (harness.go's harnessOffer): the third answer's sentence goes first,
	// because it is a reminder about a box that is already on screen, and the two
	// keys are the last thing standing — a question that fits by losing them is a
	// question with no visible way to answer it.
	if ansi.StringWidth(strings.Join(parts, "")) > width {
		parts = parts[:4]
		parts[3] = strings.TrimSuffix(roomApprovalDropWord, " · ")
	}
	head := a.pal.ask(fit(roomApprovalWord, width))
	if ansi.StringWidth(strings.Join(parts, "")) > width {
		// Narrower than the two chords themselves. It is cut rather than
		// re-spelled, and it records no targets: a target under an ellipsis is a
		// press that answers something a person cannot read.
		return []string{head, a.pal.ask(fit(strings.Join(parts, ""), width))}
	}
	a.recordRoomApprovalTaps(parts)
	var out string
	for i, part := range parts {
		if i%2 == 0 && i < 4 {
			out += a.pal.askBold(part)
			continue
		}
		if part == roomApprovalSayWord {
			// The third answer is dim beside the two keys, because it is a
			// reminder and not an instruction: the box below is already pointed at
			// the thread, and this row is only saying that talking to it counts.
			out += a.pal.dim(part)
			continue
		}
		out += a.pal.ask(part)
	}
	if a.hoveringRoomApproval() {
		out = a.pal.cursor(out, width)
	}
	return []string{head, out}
}

// roomApprovalTap is one pressable answer on the row.
type roomApprovalTap struct {
	span hudSpan
	save bool
}

// recordRoomApprovalTaps writes the row's pressable columns: each chord and the
// word beside it, and never the separator between them — a press in the gap must
// not resolve as either answer.
func (a *app) recordRoomApprovalTaps(parts []string) {
	taps := make([]roomApprovalTap, 0, 2)
	at := 0
	for i, part := range parts {
		width := ansi.StringWidth(part)
		save, ok := false, false
		switch part {
		case roomApprovalSaveKey:
			save, ok = true, true
		case roomApprovalDropKey:
			save, ok = false, true
		}
		if !ok {
			at += width
			continue
		}
		to := at + width
		if i+1 < len(parts) {
			to += ansi.StringWidth(strings.TrimSuffix(parts[i+1], " · "))
		}
		taps = append(taps, roomApprovalTap{span: hudSpan{from: at, to: to}, save: save})
		at += width
	}
	a.roomApprovalTaps = taps
}

// roomApprovalMark says what one row of the block is for the pointer. Only the
// second row answers: the first is a statement, and the reporting row is a
// statement too.
func (a *app) roomApprovalMark(i int) chromeRow {
	if a.roomApprovalHeight() != 2 || i != 1 {
		return chromeRow{}
	}
	return chromeRow{kind: chromeRoomApproval, index: i}
}

// roomApprovalPress resolves a click on the row and reports whether it took it.
//
// IT SWALLOWS EVERY PRESS ON ITS OWN ROW, answer or not, for the reason the
// offer blocks above it do: this is a thing the design is waiting on, and a press
// that missed the keys and fell through would scroll the room out from under
// somebody who was reaching for an answer.
func (a *app) roomApprovalPress(x, y int) bool {
	if a.roomApprovalHeight() != 2 || a.copy.on || a.at(pageSettings) {
		return false
	}
	// THE ROW IS RESOLVED BEFORE THE COLUMN: laying the chrome out is what writes
	// the spans, and reading them first would be reading where the answers were
	// drawn on the frame before this one.
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeRoomApproval {
		return false
	}
	for _, tap := range a.roomApprovalTaps {
		if !tap.span.holds(x) {
			continue
		}
		a.answerRoomApproval(tap.save)
		return true
	}
	return true
}

// hoveringRoomApproval reports whether the pointer is on the answers row.
func (a *app) hoveringRoomApproval() bool { return a.hot.kind == hoverRoomApproval }

// roomApprovalKey routes the two chords, and reports whether it took one.
//
// EVERY OTHER KEY FALLS THROUGH UNTOUCHED, which is the difference between this
// and every modal question on this surface. Those own the keyboard because the
// session is blocked on them; this one is a standing question in a room somebody
// is also having a conversation in — the third answer to it is a sentence typed
// into the box below — so a row that swallowed keys would be a row that made the
// box it points at unusable.
func (a *app) roomApprovalKey(msg tea.KeyPressMsg) bool {
	if a.roomApprovalHeight() != 2 {
		return false
	}
	switch msg.String() {
	case "ctrl+k":
		a.answerRoomApproval(true)
	case "ctrl+x":
		a.answerRoomApproval(false)
	default:
		return false
	}
	return true
}

// answerRoomApproval spends the answer through the card's own resolver, which is
// the whole point: one design, one id, one method, one recorded state — so the
// card out in the conversation reads `saved as <name> v1` because of a chord
// pressed in here (harnesscard.go's [app.resolveHarnessCard]).
func (a *app) answerRoomApproval(save bool) {
	card := a.roomApprovalCard()
	if card == nil || card.state != "" {
		return
	}
	action := "drop"
	if save {
		action = "save"
	}
	a.resolveHarnessCard(card, action)
	a.roomTouched()
}
