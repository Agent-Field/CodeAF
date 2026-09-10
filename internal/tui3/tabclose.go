package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"

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
//	 ?  Close this tab? the tree walk is working · 2 tasks running
//	      nothing here is deleted
//	      1  keep running  it keeps going here; find it under Chats, and ctrl+shift+t brings the tab back
//	      2  stop work     the reply, tasks and jobs stop; nothing is deleted
//	      3  cancel        nothing changes
//	    [enter] take the pick · [esc] cancel · [←→] pick
//
// The block draws those rows (question.go) and this file owns the words in them.
// EACH ANSWER SAYS WHAT IT DOES ON ITS OWN ROW, which is the one line that used
// to change under the cursor: a sentence that moved was a promise about the
// answer you were passing over rather than about the one you were reading.
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
}

// The three answers, in the order they are drawn, and where the cursor starts.
// The words are the owner's, in the lowercase the rest of this surface's answers
// are spelled in ([stopAnswers] beside them).
//
// THE ORDER IS TWO LAWS AT ONCE on the block that draws them (question.go). The
// FIRST is where the cursor starts — `keep running` wears the SAFE mark, because
// a card whose destructive answer is under the enter key is a card that ends work
// when somebody presses enter to make a question go away. The LAST is what `esc`
// gives — `cancel`, which changes nothing at all, because the dismiss key on this
// surface takes questions away and must not also let a tab go.
var tabCloseAnswers = [...]string{"keep running", "stop work", "cancel"}

const (
	tabCloseKeepAt   = 0
	tabCloseStopAt   = 1
	tabCloseCancelAt = 2
)

// tabCloseQuestionKind is the lane this file's question travels under, and it is
// not one of internal/session's for [stopQuestionKind]'s reason exactly.
const tabCloseQuestionKind session.QuestionKind = "surface-tab-close"

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
	card := &tabCloseCard{
		tab: tab, sig: a.tabSignalFor(tab.key, here), here: here,
		tasks: count.tasks, jobs: count.jobs, clauses: tabCloseClauses(count),
	}
	a.tabClose = card
	// The typed lists follow the draft, and the draft is spoken for while a
	// question is up — [app.raiseStop]'s own line, for its own reason.
	a.closeLists()
	a.raiseQuestion(a.tabCloseShown(card))
	a.touch()
}

// tabCloseShown is the question the block puts up, and the closure that answers
// it.
//
// IT IS A CONFIRMATION for [app.stopShown]'s reason and keeps this card's laws
// in that shape's own grammar: the cursor opens on `keep running`, a key that
// NAMES an answer moves the cursor onto it, `enter` takes what the cursor is on,
// and `esc` is `cancel`. NOTHING IS DECIDED BY ONE KEYSTROKE, and there is still
// no bypass key and no don't-ask-me-again.
//
// THE ANSWER IT CARRIES IS WHAT THE CURSOR WAS ON, and not what the card was
// raised with: every answer is decided against the world as it is now
// ([app.tabCloseTake] says the whole of it).
func (a *app) tabCloseShown(card *tabCloseCard) questionShown {
	return questionShown{
		question: session.Question{
			Kind:    tabCloseQuestionKind,
			Ref:     card.tab.key,
			Ask:     session.AskConfirmation,
			Form:    session.FormCard,
			Asker:   session.Asker{Kind: session.AskerSurface},
			Head:    card.question(),
			Reason:  tabCloseReason,
			Options: a.tabCloseOptions(card),
			Stakes:  session.StakesIrreversible,
			Asked:   a.now(),
		},
		local: func(answer session.Answer) tea.Cmd { return a.tabCloseAnswered(answer.FirstKey()) },
	}
}

// tabCloseReason is the one sentence under the head: what nothing here does to
// the conversation itself. It stands in for the line that used to change under
// the cursor — the block says what each answer does on the answer's OWN row
// (question.go's [session.AnswerOption.Consequence]), where it cannot be read as
// a promise about a different one.
const tabCloseReason = "nothing here is deleted"

// tabCloseOptions is the three answers as the block takes them, each with what
// taking it does. The `keep running` answer wears the SAFE mark, which is the
// one place the cursor's home is decided.
func (a *app) tabCloseOptions(card *tabCloseCard) []session.AnswerOption {
	return []session.AnswerOption{
		{Key: "1", Label: tabCloseAnswers[tabCloseKeepAt], Safe: true, Consequence: tabCloseKeepSays},
		{Key: "2", Label: tabCloseAnswers[tabCloseStopAt], Consequence: a.tabCloseStopSays(card)},
		{Key: "3", Label: tabCloseAnswers[tabCloseCancelAt], Consequence: tabCloseCancelSays},
	}
}

// tabCloseAnswered turns one answer key back into the card's own index and
// takes it. A key this card does not know is `cancel`, which is the reading that
// cannot close a tab nobody meant to close.
func (a *app) tabCloseAnswered(key string) tea.Cmd {
	at := tabCloseCancelAt
	switch key {
	case "1":
		at = tabCloseKeepAt
	case "2":
		at = tabCloseStopAt
	}
	return a.tabCloseTake(at)
}

// dropTabClose takes the question down and changes nothing else. It is `esc`,
// and it is also what a switch does on its way somewhere else: a card about a tab
// is a card about a screen that is being replaced.
func (a *app) dropTabClose() {
	if a.tabClose == nil {
		return
	}
	// AND THE QUESTION GOES WITH THE CARD, for [app.dropStop]'s reason.
	for _, open := range a.questions {
		if open.question.Kind == tabCloseQuestionKind {
			a.closeQuestion(open, session.Answer{})
			break
		}
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
		held.conv.Agent.InterruptFor(session.StopByLeaving)
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

// tabCloseStopSays is what `stop work` does, and it is the one consequence on
// this card that is BUILT rather than written down: what stops depends on what
// this conversation has running, and a sentence that named tasks and jobs on a
// conversation with neither would be promising to end work that is not there.
func (a *app) tabCloseStopSays(card *tabCloseCard) string {
	if card.tasks > 0 || card.jobs > 0 {
		return "the reply, tasks and jobs stop; nothing is deleted"
	}
	return "the reply stops where it is; nothing is deleted"
}

const (
	// tabCloseKeepSays names both ways back to a conversation whose tab has gone,
	// because "where did it go" is the one question this answer raises.
	tabCloseKeepSays   = "it keeps going here; find it under Chats, and " + reopenTabChord + " brings the tab back"
	tabCloseCancelSays = "nothing changes"
	// tabCloseStopSaysFloor is what `stop work` says on a conversation with
	// nothing but a reply in flight. It is the floor [app.tabCloseStopSays]
	// falls back to and the words the question is built with.
	tabCloseStopSaysFloor = "the reply stops where it is; nothing is deleted"
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
	if msg.String() == "ctrl+c" {
		// Leaving is never modal (quitarm.go), and the card goes on its way past.
		a.dropTabClose()
		return nil, false
	}
	// EVERY KEY THAT ANSWERS THIS CARD IS THE BLOCK'S (question.go): ←/→ walk
	// the three answers, a digit moves the cursor onto the answer it names,
	// `enter` takes what the cursor is on and `esc` is `cancel`. It is offered
	// the key HERE rather than at its own rung because this rung is read first,
	// and what the block hands back is SWALLOWED rather than passed on — this
	// card is raised over a tab row whose own keys would otherwise act
	// underneath a question about closing it.
	//
	// `k` AND `s` ARE NOT KEYS ANY MORE. They were shortcuts that answered
	// outright, which is the bypass this card was built to not have; the answers
	// carry their own keys on their own rows now, and a key that names one moves
	// the cursor onto it so `enter` is still what decides.
	if cmd, took := a.questionKey(msg); took {
		return cmd, true
	}
	return nil, true
}

// ── the card, drawn ─────────────────────────────────────────────────────────

// ── the pointer ─────────────────────────────────────────────────────────────

// tabClosePress reports whether a press belongs to the raised card.
//
// THE CARD'S OWN ANSWERS ARE THE BLOCK'S TARGETS NOW (question.go's
// [app.questionPress], which the frame offers a press to before this). What is
// left here is the refusal: while this card is up, a press that reached this far
// was aimed at the tab row underneath it, and letting it through would close a
// second tab while a question about the first is on screen.
func (a *app) tabClosePress(x, y int, took *tea.Cmd) bool {
	return a.closingTab()
}
