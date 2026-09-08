package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── CLOSING A TAB THAT IS STILL DOING SOMETHING ─────────────────────────────
//
// Taking a tab off the row has never ended work and does not now: the ✕ closes a
// VIEW, the agent behind it goes on running, and the conversation is still on the
// switcher `ctrl+k` opens (chattabs.go states that law and is the only place it is
// decided). What was missing was that a person could not SEE that this was what
// happened, and could not ask for the other thing without leaving the tab row.
//
// So a tab with something in flight is asked about once, in the shape this
// surface already asks about ending work (stop.go, whose card this one is a
// sibling of and whose slot it shares):
//
//	 ? Close this tab? the tree walk is working · 2 tasks running
//	   ▌[keep running]   [stop work]   [cancel]
//	   it keeps going here; find it under Chats, ctrl+shift+t brings the tab back
//
// ── THE THREE ANSWERS ARE THREE DIFFERENT ACTS AND THE CARD SAYS SO ─────────
//
//   - KEEP RUNNING is what the ✕ has always done. The tab leaves the row, the
//     conversation stays in the keeper with its watcher draining and counting
//     (keeper.go), its draft and its reading position are exactly where they
//     were, and it is still on the switcher with `working` or `needs you`
//     against it.
//   - STOP WORK ends the turn and every running node IN THIS CONVERSATION and
//     nothing else, and then the tab leaves the row exactly as above. The
//     transcript is untouched: closing a tab never deletes anything, and this
//     card must not be the place a person learns otherwise.
//   - CANCEL leaves everything — the work, the tab, the draft, the caret —
//     exactly as it was. It is `esc`, which on this surface is always the key
//     that takes a question away without acting.
//
// ── AND THE CURSOR STARTS ON KEEP RUNNING ───────────────────────────────────
//
// stop.go's rule, applied to a different question: the answer under `enter` is
// the one a person gets by pressing the key they press to make a question go
// away, so it has to be the answer that loses nothing. Here that is not the
// refusal — the person asked for the tab to close — it is the one that closes it
// and keeps the work.
//
// ── A TAB WITH NOTHING IN FLIGHT IS NOT ASKED ABOUT AT ALL ──────────────────
//
// [app.tabCloseAsks] is the whole gate, and it reads the strip's own signal
// (tabsignal.go), which opens no file and crosses no wire. A conversation at rest
// closes on the first press with no card, because there is nothing to decide.

// tabCloseCard is one raised question: which tab it is about, what that
// conversation was doing when it was raised, and which answer the cursor is on.
type tabCloseCard struct {
	tab chatTab
	// sig is what the conversation was doing at the moment the card went up. It
	// is kept rather than re-read so the first line does not change under
	// somebody's eyes while they are reading it; what the answers DO is decided
	// against the world as it is when they are taken, which is why the card can
	// safely be answered by a conversation that has finished meanwhile.
	sig tabSignal
	// clauses is what was running, in the person's own words, taken with sig.
	clauses string
	// here says this card is about the conversation ON SCREEN, and tasks and jobs
	// are what it had running when the card went up. All three are read once,
	// with sig, because the line under the answers must not change its promise
	// while somebody is reading it.
	here  bool
	tasks int
	jobs  int
	// pick indexes [tabCloseAnswers], and spans are where they landed, written by
	// the layout and read by the press — stop.go's own bargain.
	pick  int
	spans []hudSpan
}

// The three answers, in the order they are drawn, and where the cursor starts.
// The words are the owner's, in the lowercase the rest of this surface's answers
// are spelled in ([stopAnswers] beside them).
var tabCloseAnswers = [...]string{"keep running", "stop work", "cancel"}

const (
	tabCloseKeepAt   = 0
	tabCloseStopAt   = 1
	tabCloseCancelAt = 2
)

// closingTab reports whether the card owns the keyboard.
func (a *app) closingTab() bool { return a.tabClose != nil }

// tabCloseAsks says whether taking this tab off the row is worth a question.
//
// IT IS THE STRIP'S OWN READING and not a second one (tabsignal.go): the mark on
// the tab and the question raised by its ✕ have to be the same fact, or a person
// would be asked about a tab wearing no mark and let go of one wearing a `?`.
func (a *app) tabCloseAsks(tab chatTab) bool {
	if tab.start || tab.key == "" {
		// The new-chat page's synthetic tab has no conversation behind it, and a
		// row this window only remembers has nothing this process is holding.
		return false
	}
	return a.tabSignalFor(tab.key, tab.key == a.frontTabKey()) != tabIdle
}

// askTabClose puts the question up. The tab stays exactly where it is until it
// is answered.
func (a *app) askTabClose(tab chatTab) {
	here := tab.key == a.frontTabKey()
	count := a.tabCloseWork(tab, here)
	a.tabClose = &tabCloseCard{
		tab: tab, sig: a.tabSignalFor(tab.key, here), here: here,
		tasks: count.tasks, jobs: count.jobs, clauses: tabCloseClauses(count),
		pick: tabCloseKeepAt,
	}
	// The typed lists follow the draft, and the draft is spoken for while a
	// question is up — [app.raiseStop]'s own line, for its own reason.
	a.closeLists()
	a.touch()
}

// dropTabClose takes the question down and changes nothing else. It is `esc`,
// and it is also what a switch does on its way somewhere else: a card about a tab
// is a card about a screen that is being replaced.
func (a *app) dropTabClose() {
	if a.tabClose == nil {
		return
	}
	a.tabClose = nil
	a.touch()
}

// tabCloseTake answers the card.
//
// EVERY ANSWER IS DECIDED AGAINST THE WORLD AS IT IS NOW, never against the
// reading the card was raised with, which is the whole of how a conversation
// finishing under the card is handled. Keep running on a conversation that has
// since landed is an ordinary tab dismiss; stop work on one is a stop that finds
// nothing to stop, which is what pressing `esc` on a finished turn already does.
// The card does NOT take itself down when the work ends, because a card that
// vanished under a hand about to press a key would move that keystroke onto
// whatever was behind it.
func (a *app) tabCloseTake(at int) tea.Cmd {
	card := a.tabClose
	if card == nil {
		return nil
	}
	tab := card.tab
	a.dropTabClose()
	switch at {
	case tabCloseCancelAt:
		return nil
	case tabCloseStopAt:
		if err := a.stopConversation(tab); err != nil {
			a.note("could not stop work: " + err.Error())
			return nil
		}
	}
	return a.tabDismissNow(tab)
}

// stopConversation ends the work in ONE conversation and touches nothing else in
// the window.
//
// The engine operation closes admission as well as cancelling work. The older
// in-process adapter fallback uses only this conversation's replayed roster;
// project-index IDs are never cancellation authority because they repeat.
func (a *app) stopConversation(tab chatTab) error {
	// The engine owns admission as well as cancellation. A surface roster can
	// miss a job created while this card is open, so prefer the complete door.
	var agent any = a.agent
	if tab.key != a.frontTabKey() {
		if held := a.behind[tab.key]; held != nil {
			agent = held.conv.Agent
		} else {
			agent = nil
		}
	}
	if door, ok := agent.(interface{ StopWork() error }); ok {
		if err := door.StopWork(); err != nil {
			return err
		}
		if tab.key == a.frontTabKey() {
			a.interrupt()
		}
		return nil
	}

	if tab.key == a.frontTabKey() {
		a.interrupt()
		a.stopFrontNodes()
		if doors, ok := a.stopDoors(); ok {
			for _, job := range a.jobs {
				if !job.Over() {
					_, _ = doors.Cancel(session.CancelJob + ":" + itoa(job.ID))
				}
			}
		}
		return nil
	}
	if held := a.behind[tab.key]; held != nil && held.conv.Agent != nil {
		held.conv.Agent.Interrupt()
		if doors, ok := held.conv.Agent.(stopAgent); ok {
			tasks, jobs := held.watch.workIDs()
			for _, id := range append(tasks, jobs...) {
				_, _ = doors.Cancel(id)
			}
		}
	}
	return nil
}

// stopFrontNodes asks this conversation to end each node in its own roster that
// has not settled.
//
// THE REFUSALS ARE DROPPED RATHER THAN NOTED: this is a bulk act on a
// conversation the person is walking away from, and a row of engine sentences
// about work that had already settled between the reading and the call would be
// noise on a screen they are leaving.
func (a *app) stopFrontNodes() {
	doors, ok := a.stopDoors()
	if !ok {
		return
	}
	for _, id := range a.taskOrder {
		node := a.tasks[id]
		if node == nil {
			continue
		}
		switch node.state {
		case session.TaskQueued, session.TaskRunning:
			_, _ = doors.Cancel(session.CancelTask + ":" + itoa(int(node.id)))
		}
	}
}

// ── what the card says ──────────────────────────────────────────────────────

// tabCloseQuestion is the card's first line: what is being closed and what that
// conversation is doing.
func (c *tabCloseCard) question() string {
	name := strings.TrimSpace(c.tab.word)
	if name == "" {
		name = "this chat"
	}
	said := "Close this tab? " + name + " is " + tabSignalWord(c.sig)
	if c.clauses != "" {
		said += " · " + c.clauses
	}
	return said
}

// tabCloseWork is what is running in this conversation, counted the way the quit
// warning counts it so the two cannot disagree (quitarm.go).
func (a *app) tabCloseWork(tab chatTab, here bool) quitWorkCount {
	count := quitWorkCount{}
	if here {
		for _, id := range a.taskOrder {
			if node := a.tasks[id]; node != nil && node.state == session.TaskRunning {
				count.tasks++
			}
		}
		count.jobs = a.hudStats().jobs
		return count
	}
	if held := a.behind[tab.key]; held != nil && held.conv.Agent != nil {
		tasks, jobs := held.watch.workIDs()
		count.tasks, count.jobs = len(tasks), len(jobs)
	}
	return count
}

// tabCloseClauses spells that count for the card's first line, or "" when there
// is nothing to spell — the emptiness law, said about a clause.
func tabCloseClauses(count quitWorkCount) string {
	if word := quitWorkWord(count); word != "" {
		return word + " running"
	}
	return ""
}

// tabCloseSays is the line under the answers: what the one the cursor is on will
// ACTUALLY DO. It is pickrow.go's convention — walking the row is a way of
// reading the question — and every sentence in it is built from what this
// conversation has rather than written once and left to go stale.
func (a *app) tabCloseSays(card *tabCloseCard) string {
	switch card.pick {
	case tabCloseKeepAt:
		return tabCloseKeepSays
	case tabCloseCancelAt:
		return tabCloseCancelSays
	}
	said := "the reply stops where it is; nothing is deleted"
	if card.tasks > 0 || card.jobs > 0 {
		said = "the reply, tasks and jobs stop; nothing is deleted"
	}
	return said
}

const (
	// tabCloseKeepSays names both ways back to a conversation whose tab has gone,
	// because "where did it go" is the one question this answer raises.
	tabCloseKeepSays   = "it keeps going here; find it under Chats, and " + reopenTabChord + " brings the tab back"
	tabCloseCancelSays = "nothing changes"
)

// ── the keyboard ────────────────────────────────────────────────────────────

// tabCloseKey is the card's claim on the keyboard, read where [app.stopKey] is
// read and on the same terms: while it is up it takes everything but the door,
// because two of its three answers act and a key that reached the page underneath
// would be a key aimed at the conversation being decided about.
func (a *app) tabCloseKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.closingTab() {
		return nil, false
	}
	switch key := msg.String(); key {
	case "ctrl+c":
		// Leaving is never modal (quitarm.go), and the card goes on its way past.
		a.dropTabClose()
		return nil, false
	case "left":
		a.moveTabClose(-1)
	case "right":
		a.moveTabClose(1)
	case "k":
		return a.tabCloseTake(tabCloseKeepAt), true
	case "enter":
		return a.tabCloseTake(a.tabClose.pick), true
	case "esc":
		a.dropTabClose()
	case tabCloseStopKey:
		// The one answer with a letter of its own, for the hand that already
		// knows it. The smallest frame shows k, s and esc as its complete
		// answers; those same shortcuts work at every width.
		return a.tabCloseTake(tabCloseStopAt), true
	}
	return nil, true
}

// tabCloseStopKey is the letter on the one answer that ends work. It is `s`, and
// it is safe to bind because this card owns the keyboard while it is up — there
// is no box on the frame for it to be a letter of.
const tabCloseStopKey = "s"

// moveTabClose walks the answers and STOPS at the ends rather than wrapping, for
// [app.moveStop]'s reason: a cursor that reappeared at the far end would put
// `stop work` under a key pressed to reach `cancel`.
func (a *app) moveTabClose(delta int) {
	card := a.tabClose
	if card == nil {
		return
	}
	at := card.pick + delta
	switch {
	case at < 0:
		at = 0
	case at >= len(tabCloseAnswers):
		at = len(tabCloseAnswers) - 1
	}
	card.pick = at
	a.touch()
}

// ── the card, drawn ─────────────────────────────────────────────────────────

// tabCloseHeight is what the card costs the frame: the question, the answers,
// and the line saying what the answer under the cursor does.
func (a *app) tabCloseHeight() int {
	if !a.closingTab() {
		return 0
	}
	return 3
}

// tabCloseRows draws it, in the question hue everything on this surface that is
// blocked on a keystroke wears. It shares [app.guardRows]' slot with the stop
// card, and the two can never be up together: this one is raised from the tab
// row and that one from a key taken over an empty box, and each owns the keyboard
// while it stands.
func (a *app) tabCloseRows(width int) []string {
	card := a.tabClose
	if card == nil || width < 4 {
		return nil
	}
	head := card.question()
	rows := []string{a.pal.askBold(glyphAsk) + a.pal.ask(fit(" "+head, width-ansi.StringWidth(glyphAsk)))}

	card.spans = card.spans[:0]
	words, pad, gap, cursor := a.tabCloseLayout(width)
	line, at := pad, len(pad)
	for i, word := range words {
		if i > 0 {
			line += gap
			at += len(gap)
		}
		lead := ""
		if cursor {
			lead = a.orchLead(i == card.pick)
		}
		text := lead + "[" + word + "]"
		cols := ansi.StringWidth(text)
		painted := a.pal.ask(text)
		if i == card.pick {
			painted = a.pal.askBold(text)
		}
		// THE POINTER LIGHTS THE ANSWER AND NOT THE ROW, [app.stopRows]' own law:
		// three presses share this line and they are three ends of one decision.
		if a.hoveringTabClose(i) {
			painted = a.pal.cursor(painted, 0)
		}
		line += painted
		card.spans = append(card.spans, hudSpan{from: at, to: at + cols})
		at += cols
	}
	rows = append(rows, fit(line, width))
	return append(rows, a.pal.dim(fit(stopAnswerPad+a.tabCloseSays(card), width)))
}

// tabCloseLayout shortens complete answers before giving up their spacing.
// Every answer remains visible and clickable on a narrow terminal; truncating
// the painted row would leave invisible hit targets for destructive actions.
func (a *app) tabCloseLayout(width int) ([]string, string, string, bool) {
	words := tabCloseAnswers[:]
	pad, gap, cursor := stopAnswerPad, stopAnswerGap, true
	cells := func() int {
		n := len(pad) + (len(words)-1)*len(gap)
		for _, word := range words {
			n += ansi.StringWidth(word) + 2
		}
		if cursor {
			n += len(words) * ansi.StringWidth(a.orchLead(false))
		}
		return n
	}
	if cells() > width {
		words = []string{"keep", "stop", "cancel"}
	}
	if cells() > width {
		pad, gap = "", ""
	}
	if cells() > width {
		words = []string{"k", "s", "esc"}
	}
	if cells() > width {
		cursor = false
	}
	return words, pad, gap, cursor
}

// ── the pointer ─────────────────────────────────────────────────────────────

// tabClosePress answers a press on the raised card.
//
// A PRESS ANYWHERE ON THE ANSWERS ROW IS THE ROW'S, whether or not it landed on
// an answer, for [app.stopCardPress]'s reason: the gaps between three answers are
// places people miss, and a miss falling through to the box under them would put
// a caret in a sentence instead of answering a question about somebody's work.
func (a *app) tabClosePress(x, y int, took *tea.Cmd) bool {
	card := a.tabClose
	if card == nil {
		return false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeTabClose {
		return false
	}
	if mark.index != 1 {
		// The question's own row and the line under the answers. Neither has
		// anything on it to press, and both are the card's rather than the box's.
		return true
	}
	for at, span := range card.spans {
		if span.holds(x) {
			*took = a.tabCloseTake(at)
			return true
		}
	}
	return true
}
