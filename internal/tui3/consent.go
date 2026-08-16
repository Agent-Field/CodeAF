package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE APPROVAL QUESTION.
//
// internal/session's consent.go decides that one tool call needs a person, and
// then BLOCKS that call until somebody answers. This file is the person: the
// question is drawn where every other question on this surface is drawn — a
// bottom-anchored block in the palette idiom — and answered with one key.
//
//	╰─▶ bash rm -rf build
//	allow? [y] yes · [n] no · [a] always, this tool · [esc] cancel · 7s
//	bash pattern "rm -rf *"
//	2 more
//
// Four decisions, and each of them is the reason the block looks like this:
//
//   - IT SHOWS THE ROW THAT IS ALREADY THERE. The question is about a call the
//     transcript has already drawn (session sends the consent request AFTER the
//     batch's EventToolBegin rows), so the block re-uses that row's own
//     rendering rather than describing the call a second time in different
//     words. Two renderings of one call is how a person ends up approving
//     something other than what they read.
//   - IT NAMES THE RULE, DIM. "Why am I being asked" is the policy's own
//     sentence (internal/approval phrases it), and it is the difference between
//     a prompt somebody reads and a prompt somebody dismisses.
//   - IT SUSPENDS THE KEYBOARD. While a question is up the draft below is
//     untouched and every key that is not an answer does nothing. A blocked
//     tool call is the one moment on this surface where typing something else
//     would be typing into a conversation that cannot move.
//   - IT QUEUES. A batch can raise several questions at once; they are answered
//     oldest first, and the count of the ones behind it is on screen, because a
//     person who answers one question and gets another one must have been told
//     it was coming.
//
// After an answer the ROW STAYS, annotated dim with what was decided. The
// transcript is what happened, and "you were asked about this and said yes" is
// part of what happened — one of the few things this surface records that the
// session file never will (consent is events, never journal).

// ask is one unanswered question.
type ask struct {
	id   uint64
	tool string
	hint string
	rule string
	// memo says a session-scoped yes would actually stand for something
	// (session.Event's Memo). It is what decides whether the always key is on
	// the offer at all: the same lane carries the stuck question, whose
	// tool-session scope the engine drops, and an offer that does nothing is
	// worse than a missing one — a person who presses it believes they have
	// stopped being asked.
	memo bool
	// entry is the tool row the question is about. It is always a real index:
	// a request whose row is missing gets one (see [app.askConsent]), because a
	// question about a call nobody can see is a question nobody can answer.
	entry int
}

// askConsent takes one session.EventConsentRequest.
func (a *app) askConsent(ev session.Event) {
	at := a.callAwaiting(ev.Tool)
	if at < 0 {
		// The row should already exist. When it does not — a surface that
		// attached mid-batch, a tool whose begin was dropped — the call is drawn
		// now rather than asked about invisibly.
		a.closeLive()
		a.entries = append(a.entries, entry{
			kind: entryTool, tool: ev.Tool, text: ev.Hint, turn: a.turn,
			status: toolConsent, detail: toolDetail{Args: ev.Args},
		})
		at = len(a.entries) - 1
	}
	// The row stops claiming to be working. Its spinner was the second half of
	// the defect this wave fixes: a call parked on a question that nobody could
	// see was a question turned exactly like a call doing work.
	a.entries[at].status = toolConsent
	a.entries[at].stale = true
	// The typed lists follow the draft, and the draft is suspended for as long
	// as the question is up: a list left open under a modal is a list answering
	// keys nobody is pressing.
	a.closeLists()
	// AND THE MODEL OVERLAY GOES, for the harder version of the same reason.
	// Every option on this block has a LETTER, and the picker's filter box
	// answers to letters too — two readers for one keystroke, and the one that
	// wins decides whether "a" narrowed a list or approved a call. So a question
	// that arrives takes the search box off the screen rather than competing
	// with it: where there are hotkeys there is no fuzzy filter, anywhere on
	// this surface.
	if a.pick.open {
		a.pick.close()
	}
	was := a.asking()
	a.asks = append(a.asks, ask{
		id: ev.ID, tool: ev.Tool, hint: ev.Hint, rule: ev.Rule, memo: ev.Memo, entry: at,
	})
	if !was {
		a.startAskClock()
	}
	a.follow()
	a.touch()
}

// startAskClock stamps the countdown for whichever question is now at the head
// of the queue, and takes the pause off.
//
// The clock is per QUESTION and not per queue: three questions raised together
// are three separate decisions, and a person who spent nine seconds on the first
// must not find the second already expired. It is also why the pause is cleared
// here — the keystroke that answered the last one is not an answer to this one.
func (a *app) startAskClock() {
	a.askAt, a.askPaused = a.now(), false
}

// consentWait is the configured countdown as a duration. Zero — the setting's
// own off — is a question that waits forever.
//
// It is read at boot and re-read at every turn end ([app.settle]), on the terms
// the gate's posture and the mouse row are read on: a row that only ever changes
// by hand does not need to be resolved off disk once per question.
func (a *app) consentWait() time.Duration {
	seconds := config.ConsentTimeoutAt(a.profileDir)
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// pauseAsk stops the countdown, and it is called from EVERY key the question
// reads — the answers included, which cost nothing because they resolve it in
// the same breath.
//
// There is no way back. "Paused" here means a person is at the keyboard, and
// that fact does not expire: a clock that resumed after a few idle seconds
// would be a clock that fires exactly when somebody has looked away from the
// screen mid-decision, which is the one moment it must not.
func (a *app) pauseAsk() {
	if !a.asking() || a.askPaused {
		return
	}
	a.askPaused = true
	a.touch()
}

// tickAsk is the countdown running down, on the frame clock that is already
// turning (app.go's [app.paint]) — no ticker of its own, exactly as the task
// proposal's countdown has none.
//
// AT EXPIRY IT DENIES, and this is the one clock on this surface that answers a
// question rather than stopping asking it. The two are the same act here: the
// engine is BLOCKED on this answer, so a surface that merely stopped drawing the
// question would leave a tool call parked forever on a prompt nobody can see.
// Denying is the only expiry that is safe in both directions — the call does not
// run, and the model is handed a refusal it can act on and try something else.
func (a *app) tickAsk() {
	if !a.asking() || a.askPaused || a.askWait <= 0 {
		return
	}
	if a.now().Before(a.askAt.Add(a.askWait)) {
		return
	}
	a.answerWith(false, session.ConsentOnce, consentExpiredWord)
}

// consentExpiredWord is what the row keeps when the clock answered. It says
// "denied" first, because that is what happened to the call, and then says who
// said so — which is nobody.
const consentExpiredWord = "denied · no answer"

// askLeft is how much of the countdown is left, and whether there is a clock at
// all. It is recomputed from [app.askAt] every frame rather than stepped, for
// the reason the proposal's meter is (task.go): a number that advanced itself
// would drift from the deadline the answer is actually measured against.
func (a *app) askLeft() (time.Duration, bool) {
	if !a.asking() || a.askWait <= 0 {
		return 0, false
	}
	left := a.askAt.Add(a.askWait).Sub(a.now())
	if left < 0 {
		left = 0
	}
	return left, true
}

// callAwaiting finds the oldest still-running row for a tool — the same rule
// [app.closeTool] uses, and for the same reason: calls run in parallel and the
// first one begun is the one a person watching the column expects to be asked
// about first.
//
// A row another question is ALREADY about is skipped. One batch can raise three
// bash questions at once, and every one of them would otherwise attach to the
// first bash row on screen — three questions annotating one line and two calls
// the person never saw asked about.
func (a *app) callAwaiting(tool string) int {
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryTool || !e.status.live() || e.tool != tool || e.decision != "" {
			continue
		}
		if a.claimed(i) {
			continue
		}
		return i
	}
	return -1
}

// claimed reports whether a queued question is already about this row.
func (a *app) claimed(i int) bool {
	for _, queued := range a.asks {
		if queued.entry == i {
			return true
		}
	}
	return false
}

// answer resolves the question at the head of the queue.
//
// The scope goes to the session verbatim: [session.ConsentOnce] answers this
// call, [session.ConsentToolSession] answers every later prompt for the same
// tool for the rest of the agent's life — and no longer, which is why the key
// says "(session)" out loud. Nothing here writes a setting.
func (a *app) answer(allow bool, scope session.ConsentScope) {
	a.answerWith(allow, scope, decisionWord(allow))
}

// answerWith is [app.answer] with the word the ROW keeps spelled out, and the
// clock is the only caller that spells it differently.
//
// The distinction is the transcript's honesty and nothing else: the engine gets
// the same deny either way, but "denied" and "denied · no answer" are different
// things to read six screens later. One is a decision somebody made about a
// call; the other is a call that went past somebody who was not there.
func (a *app) answerWith(allow bool, scope session.ConsentScope, word string) {
	if len(a.asks) == 0 {
		return
	}
	head := a.asks[0]
	a.asks = a.asks[1:]
	// The question behind it gets a clock of its own, starting now — the queue
	// is a queue of decisions, not one decision with several parts.
	if len(a.asks) > 0 {
		a.startAskClock()
	}
	if a.agent != nil {
		// The narrow answer goes through the narrow method. They do the same
		// thing — session's ResolveConsent is ResolveConsentRemember with
		// ConsentOnce — and saying which one this is at the call site is how the
		// scope stays a decision rather than a defaulted argument.
		if scope == session.ConsentToolSession {
			a.agent.ResolveConsentRemember(head.id, allow, scope)
		} else {
			a.agent.ResolveConsent(head.id, allow)
		}
	}
	if head.entry >= 0 && head.entry < len(a.entries) {
		e := &a.entries[head.entry]
		e.decision = word
		// The question is over, and the row goes back to being a call: allowed,
		// it runs and spins; denied, session hands the model a refusal and the
		// close event that follows lands on the same row either way. Leaving it
		// in the question state would leave a violet row on screen for a question
		// nobody is being asked.
		if e.status == toolConsent {
			e.status = toolRunning
			e.began = time.Now()
		}
		e.stale = true
	}
	a.touch()
}

// askAnimating reports whether a countdown is running down, which is what keeps
// the paint clock turning while a question waits (app.go's [app.paint]). A
// paused clock is not animating: the line it draws says one word and stops
// changing.
func (a *app) askAnimating() bool {
	_, running := a.askLeft()
	return running && !a.askPaused
}

// dropAsks forgets every unanswered question. It runs when the turn that raised
// them ends: the session released those calls when its context died, so the
// answers are late and the questions are about work that is over.
func (a *app) dropAsks() {
	if len(a.asks) == 0 {
		return
	}
	a.asks = nil
	a.touch()
}

func decisionWord(allow bool) string {
	if allow {
		return "allowed"
	}
	return "denied"
}

// asking reports whether a question owns the keyboard.
func (a *app) asking() bool { return len(a.asks) > 0 }

// The four answers, as the keys that give them.
//
// THEY ARE THE ANSWER'S OWN FIRST LETTERS, which is the change this wave makes
// and the reason it is worth making. The old set was [a] once, [t] this tool
// always, [d] deny — three letters standing for three phrasings, none of which
// is the word a person says in their head when they decide. What they say is
// yes, or no. So yes is y, no is n, and the third answer — the one that changes
// something beyond this call — keeps a letter of its own rather than being a
// modifier on a key that already means something.
//
// [t] and [d] are still read, silently. They were the keys for a year of this
// surface's life, they cannot collide with anything (t is "always" as it always
// was, d is "deny" as it always was), and a person whose hand remembers them is
// answering the same question with the same meaning. They are not on the offer:
// a line that named five keys for three answers would be teaching the history
// instead of the choice.
const (
	consentYes    = "y"
	consentNo     = "n"
	consentAlways = "a"
)

// consentKey routes one keypress while a question is up, and reports whether it
// took it — which, apart from ctrl+c, is ALWAYS: the draft is suspended, so a
// key that is not an answer is a key that does nothing rather than a key that
// types into a conversation the model cannot read.
//
// EVERY KEY STOPS THE CLOCK, whether or not it answers anything. That is the
// whole of the pause: the countdown exists so an unattended session cannot park
// a tool call forever, and the moment there is evidence of somebody at the
// keyboard the reason for it is gone.
//
// esc cancels, and cancelling is denying. A modal that cannot be left by the
// dismiss key would be a trap; the safe reading of "get this off my screen" is
// no, and it is the same answer the clock gives when nobody says anything at
// all.
func (a *app) consentKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	// A TASK PROPOSAL IS THE OTHER QUESTION on this surface, and it is read from
	// the same hook because it is the same rung: a question the session is
	// blocked on outranks every overlay below it (input.go's key order). It is
	// deliberately NOT modal — the box under it is the redirect lane — so it
	// takes three keys and hands everything else back (task.go).
	if cmd, taken := a.taskKey(msg); taken {
		return cmd, true
	}
	if !a.asking() {
		return nil, false
	}
	if msg.String() == "ctrl+c" {
		// Leaving is never modal — and mid-turn ctrl+c is the interrupt, which
		// releases the blocked call the honest way. It does not pause: the
		// question is about to stop existing.
		return nil, false
	}
	a.pauseAsk()
	switch msg.String() {
	case consentYes:
		a.answer(true, session.ConsentOnce)
	case consentAlways, "t":
		// The widening answer, and the ONE key on this block that is refused
		// when it would do nothing (see [ask.memo]).
		if a.asks[0].memo {
			a.answer(true, session.ConsentToolSession)
		}
	case consentNo, "d", "esc":
		a.answer(false, session.ConsentOnce)
	}
	return nil, true
}

// ── the block ───────────────────────────────────────────────────────────────
//
// THE QUESTION IS THE ONE THING ON THIS SURFACE THAT SHOUTS.
//
// It used to be drawn in exactly the ink everything else is drawn in — a dim
// sentence above the box, under a row whose spinner was still turning — and the
// result was the defect this wave exists to fix: a person could not tell that
// the agent had stopped and was waiting for them. Quiet is the right default
// for a surface that reports; it is the wrong default for a surface that is
// blocked on you.
//
// So the question takes the fifth colour (styles.go), and it takes it
// everywhere at once: the call's row, its marker, the offer line, and the word
// in the status line. Violet appears nowhere else on this surface, which is
// what makes seeing it mean one thing.

// consentOfferRow is where the offer sits inside the block. The frame needs it
// to know which row the pointer can be over (view.go).
const consentOfferRow = 1

// consentHeight is how many rows the question takes: the call, the offer, the
// rule, and the count of the questions behind it when there are any.
func (a *app) consentHeight() int {
	if !a.asking() {
		return 0
	}
	rows := 3
	if a.asks[0].rule == "" {
		rows--
	}
	if len(a.asks) > 1 {
		rows++
	}
	return rows
}

// consentRows draws the block. It is laid out by [app.frame], directly above the
// input, because that is where this surface puts everything it wants answered.
func (a *app) consentRows(width int) []string {
	if !a.asking() {
		return nil
	}
	head := a.asks[0]
	out := make([]string, 0, 4)
	out = append(out, a.consentCall(head, width))
	out = append(out, a.consentOffer(width))
	if head.rule != "" {
		out = append(out, a.pal.dim(fit("  "+head.rule, width)))
	}
	if more := len(a.asks) - 1; more > 0 {
		out = append(out, a.pal.dim(fit("  "+itoa(more)+" more", width)))
	}
	return out
}

// consentCall is the tool row itself, drawn by the renderer that drew it in the
// transcript. A call whose row has gone missing falls back to the one plain
// sentence this tree says about a tool anywhere (ToolGloss).
func (a *app) consentCall(head ask, width int) string {
	if head.entry >= 0 && head.entry < len(a.entries) {
		e := &a.entries[head.entry]
		if e.kind == entryTool {
			return a.toolLine(e, head.entry, true, width)
		}
	}
	return a.pal.ink(fit("  "+ToolGloss(head.tool, head.hint), width))
}

// consentOffer is the answers, and the whole line is the question hue — the
// keys bold within it, because the person is looking for which letter to press
// and the sentence around it is there to be recognized rather than read twice.
// It was dim until this wave, which made the one line on screen that needs an
// answer look like the lines that do not.
//
// IT IS THE BLOCK'S TITLE as well as its offer, which is why the countdown is on
// it. There is no separate title row here on purpose — the row above is the
// call's own line, re-used rather than re-worded — so the one line this block
// writes for itself carries both of the things it has to say: what the keys are,
// and how long they are yours.
//
// A narrow terminal gets the short spelling rather than a truncated long one —
// an offer with its last option cut off is an offer that hides an answer — and
// the clock is the first thing dropped, because a countdown a person cannot see
// is still a countdown and an answer they cannot see is not an answer.
func (a *app) consentOffer(width int) string {
	// Pairs: the words at even indices, the keys — the only bold cells on the
	// line — at odd ones. The two spellings differ in ONE cell, the always
	// option's, because it is the only answer whose name has to say how far it
	// reaches; every other option is already one word.
	offer := func(always string) []string {
		parts := []string{
			"allow? ", "[" + consentYes + "]", " yes · ", "[" + consentNo + "]", " no",
		}
		if a.asks[0].memo {
			parts = append(parts, " · ", "["+consentAlways+"]", always)
		}
		return append(parts, " · ", "[esc]", " cancel")
	}
	parts := offer(" always, this tool (session)")
	if ansi.StringWidth(strings.Join(parts, "")+a.consentClock()) > width {
		parts = offer(" always")
	}
	line := strings.Join(parts, "")
	if ansi.StringWidth(line) > width {
		return a.pal.ask(fit(line, width))
	}
	// The clock takes what is left over, and takes nothing when there is not
	// room for the whole of it.
	clock := a.consentClock()
	if clock != "" && ansi.StringWidth(line+clock) > width {
		clock = ""
	}
	var out string
	for i, part := range parts {
		if i%2 == 1 {
			out += a.pal.askBold(part)
			continue
		}
		out += a.pal.ask(part)
	}
	out += a.pal.dim(clock)
	if a.hoveringChoices() {
		return a.pal.hover(out, width)
	}
	return out
}

// consentClock is the countdown's tail on the offer line — " · 7s", or
// " · paused" once a key has been pressed, or nothing at all when the setting
// turned the clock off.
//
// It is spelled in WHOLE SECONDS where the proposal's meter spells tenths, and
// the difference is what the two clocks are for. The proposal's is a bar the eye
// reads as a proportion, and the tenth is what proves the bar is moving. This is
// a word on a line of words: a digit changing ten times a second beside three
// answers would be the loudest thing in a block whose whole job is to be read
// once and answered.
func (a *app) consentClock() string {
	left, running := a.askLeft()
	if !running {
		return ""
	}
	if a.askPaused {
		return " · paused"
	}
	return " · " + countdownWord(left)
}
