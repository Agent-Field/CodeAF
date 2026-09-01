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
//	allow? [y] yes · [n] no · [a] always, this command · [esc] cancel · 7s
//	bash pattern "rm -rf *"
//	2 more
//
// Five decisions, and each of them is the reason the block looks like this:
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
//   - THE WIDENING YES IS WRITTEN DOWN, where the door wired somewhere to write
//     it ([app.rememberAlways]) — the tool's allow, or, for the one tool judged
//     by its arguments, the SHAPE the person picked out of a second beat (see
//     [app.always]). It is the only thing this surface does that outlives the
//     process, so it is the only thing it prints a receipt for, and the receipt
//     says where to undo it. The no is never written: a standing never is a
//     settings edit somebody makes on purpose.
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
	// shapes is the second beat, and it is non-empty only while that beat is on
	// screen: the shapes this always could be banked as, offered by number, with
	// the command line itself last (internal/config's [config.BashShapes]).
	//
	// It lives on the QUESTION rather than beside the queue because it is one
	// question's unfinished answer. A person part-way through choosing a shape
	// who is handed the next question in the batch must not find the previous
	// one's offer still on screen.
	shapes []string
	// chosen is the shape they picked, once they have. It is what gets written
	// down, and it is kept here so the write happens where every other write
	// happens ([app.rememberAlways]) rather than in the keystroke that picked it.
	chosen string
}

// askConsent takes one session.EventConsentRequest.
func (a *app) askConsent(ev session.Event) {
	at := a.callAwaiting(ev)
	if at < 0 {
		// The row should already exist. When it does not — a surface that
		// attached mid-batch, a tool whose begin was dropped — the call is drawn
		// now rather than asked about invisibly.
		a.closeLive()
		a.entries = append(a.entries, entry{
			kind: entryTool, tool: ev.Tool, text: ev.Hint, turn: a.turn,
			status: toolConsent, callID: ev.CallID, detail: toolDetail{Args: ev.Args},
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
	// A QUESTION COMING BACK FROM ANOTHER CONVERSATION KEEPS THE READING TIME IT
	// HAD. A switch stops drawing the card and the clock stops with it — there
	// is nobody reading a conversation that is not on screen, which is the same
	// argument [app.tickAsk] makes about an unfocused window — so what the
	// sidecar carried is the REMAINDER, and this rebases it: an askAt that far
	// in the past leaves exactly that much of askWait to run (switcher.go).
	if a.askResume > 0 {
		a.askAt = a.now().Add(a.askResume - a.askWait)
		a.askPaused = a.askResumePaused
		a.askResume, a.askResumePaused = 0, false
	}
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
//
// AND IT DOES NOT RUN ON A WINDOW NOBODY IS LOOKING AT. Ten seconds is "long
// enough to read a command and a rule" (config's DefaultConsentTimeout says so
// in those words), which is a claim about a person READING — and there is
// nobody reading a terminal that does not have the keyboard. A person who
// starts a turn in one window and steps over to another is the ordinary way
// this surface is used, and until this line existed every call that turn made
// through the gate was refused ten seconds later by a clock they could not have
// beaten. From where they were sitting the unfocused session simply stopped
// working, and the reason was on a screen behind them.
//
// So the countdown is HELD while the window is blurred and starts again whole
// when the keyboard comes back ([app.refocusAsk]) — the same bargain
// [app.pauseAsk] already makes for a person whose hands are on the keys, told
// about the other half of the same fact. What keeps the held question from
// being a session parked in silence is that it says so out loud: the presence
// file's "waiting on you" reaches every other window and home
// (session's taskpresence.go), and a banner reaches the desktop the moment it
// goes up ([app.notifyAsk]).
func (a *app) tickAsk() {
	if !a.asking() || a.askPaused || a.askWait <= 0 || !a.focused {
		return
	}
	if a.now().Before(a.askAt.Add(a.askWait)) {
		return
	}
	a.answerWith(false, session.ConsentOnce, consentExpiredWord)
}

// refocusAsk hands a waiting question its whole countdown back, because the
// window it is drawn on has just got the keyboard.
//
// THE CLOCK MEASURES READING TIME AND NOT WALL TIME. A person returning to a
// window that has been blurred for an hour has read nothing yet, so restamping
// is what gives them the ten seconds the setting promises rather than an expiry
// on their first frame back.
//
// A PAUSED QUESTION STAYS PAUSED. [app.pauseAsk] is a one-way door — somebody
// has touched the keys and is deciding — and alt-tabbing away and back is not
// them changing their mind.
func (a *app) refocusAsk() {
	if !a.asking() || a.askPaused {
		return
	}
	a.askAt = a.now()
	a.touch()
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

// callAwaiting finds the row a question is about.
//
// THE CALL'S ID IS THE ANSWER WHEREVER THERE IS ONE. The gate sends it with the
// question (session's consent.go), the row has been carrying it since its first
// fragment (app.go's [entry.callID]), and pairing on it is exact. It matters
// more here than anywhere else on this surface: [app.askCommand] reads the
// command the always will be REMEMBERED as off the row this walk returns, so a
// question that landed on the wrong row is a person reading one command and
// banking a standing rule for another.
//
// The walk by NAME below is what is left for a provider that streams no ids —
// oldest still-running row of that tool, the same rule [feed.closeTool] uses,
// because the first call begun is the one a person watching the column expects
// to be asked about first. It is a convention rather than a fact, which is why
// it is second (session's loop.go makes the same argument for the announcement).
//
// A row another question is ALREADY about is skipped either way. One batch can
// raise three bash questions at once, and every one of them would otherwise
// attach to the first bash row on screen — three questions annotating one line
// and two calls the person never saw asked about.
func (a *app) callAwaiting(ev session.Event) int {
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryTool || !e.status.live() || e.decision != "" {
			continue
		}
		if a.claimed(i) {
			continue
		}
		if ev.CallID != "" && e.callID != "" {
			if e.callID == ev.CallID {
				return i
			}
			continue
		}
		if e.tool != ev.Tool {
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
// tool for the rest of the agent's life.
//
// AND, WHERE THE DOOR WIRED A WAY TO, the widening answer is also written down
// ([app.rememberAlways]). That is the one thing on this block that outlives the
// process, so it is the one thing this block says a receipt about.
func (a *app) answer(allow bool, scope session.ConsentScope) {
	word := decisionWord(allow)
	if allow && scope == session.ConsentToolSession && a.rememberAlways() {
		word = consentSavedWord
		if a.asks[0].tool == consentBash {
			// AND THE SESSION IS TOLD A RULE EXISTS, which is what stops it
			// writing a memo of its own. The engine's memo is keyed by tool name
			// alone, so on bash it means "every command", and a card that said
			// "always, this command" and left that behind would have widened the
			// session by more than the sentence a person read
			// (session.ConsentRule). The rule just written is what answers the
			// next call, and it answers only the calls it matches.
			scope = session.ConsentRule
		}
	}
	a.answerWith(allow, scope, word)
}

// consentSavedWord is what the row keeps when the always was persisted: what
// happened, and where to undo it. It is the block's memo voice — one line, dim,
// in the [entry.decision] slot every other answer lands in — because a person
// who has just changed a setting by pressing a letter has to be told BOTH that
// it changed and that the change has an address.
const consentSavedWord = "always · saved — /permissions to change"

// consentBash is the one tool whose answer is about its ARGUMENT and not its
// name. Everywhere else on this block a question is about a tool; here it is
// about the command line, which is why the offer says "this command" and why
// what gets written is a rule and not a name.
const consentBash = "bash"

// rememberAlways writes the head question's widening answer to the person's
// settings, and reports whether it landed.
//
// THE RUNNING SESSION NEEDS NOTHING FROM THIS. The scope this press sends is
// [session.ConsentToolSession], and the engine writes its own memo for the tool
// the moment it lands (internal/session's askAnswer) — which is what actually
// stops the asking for the rest of this conversation, for bash as much as for
// anything else. So this seam is only ever about the NEXT session, and nothing
// here reaches into a policy the gate is using: two places deciding the same
// question is how a card and a gate come to disagree about what was answered.
//
// NEVER A DENY. It is only ever reached from an allow ([app.answer]), and that
// is deliberate: n is a one-time no, and a standing never is a line somebody
// types into the settings sheet on purpose. A surface that turned a keystroke
// under a countdown into a permanent refusal would be writing policy out of
// impatience.
//
// A FAILED WRITE IS DROPPED, exactly as the rail's is (cmd/aforge's
// chatv2_rail.go states the reasoning): the answer has already been given, the
// session already stops asking, and an unwritable profile directory must not put
// a config error on a line in the middle of somebody's work. What it costs is
// the receipt — the row says "allowed" instead of "saved", which is the truth.
func (a *app) rememberAlways() bool {
	if len(a.asks) == 0 {
		return false
	}
	head := a.asks[0]
	if head.tool == consentBash {
		if a.saveBashApproval == nil {
			return false
		}
		// The shape the person picked in the second beat, and the line itself
		// where there was no beat to pick in — a command that never arrived
		// whole, a compound line nothing can be derived from ([app.always]).
		command := head.chosen
		if command == "" {
			command = a.askCommand(head)
		}
		if command == "" {
			return false
		}
		return a.saveBashApproval(command) == nil
	}
	if a.saveApproval == nil {
		return false
	}
	return a.saveApproval(head.tool) == nil
}

// askCommand is the exact command line a bash question is about.
//
// IT READS THE ARGUMENTS AND NEVER THE LINE ON SCREEN. The row's text is a
// gloss — clipped for a column, sometimes the tool's own name and nothing else
// — and a rule written from a gloss would be a standing approval for a command
// that was never run. The arguments are what arrived; when they did not arrive
// whole (session caps them) they do not parse, this answers empty, and nothing
// is written at all.
func (a *app) askCommand(head ask) string {
	if head.entry < 0 || head.entry >= len(a.entries) {
		return ""
	}
	e := &a.entries[head.entry]
	if e.kind != entryTool {
		return ""
	}
	return strings.TrimSpace(argString(argsOf(e.detail.Args), "command"))
}

// ── the second beat: what shape is this always ──────────────────────────────
//
// PRESSING ALWAYS ON A SHELL COMMAND ASKS ONE MORE THING.
//
// The card used to write the line down exactly as it ran, which meant an always
// pressed on `git status --short` bought silence for that string and nothing
// else: the same work with one more flag asked again, and the person pressed
// always forever. What they meant was a SHAPE, and only they know which one. So
// the offer line becomes, for one keystroke:
//
//	always? [1] git status*  ·  [2] git *  ·  [3] just this line  ·  [esc] never mind
//
// Three decisions, and each is why this is a beat rather than a guess:
//
//   - NOTHING IS WIDENED WITHOUT BEING READ. The shapes are derived from the
//     line (internal/config's bashshapes.go) and printed in full before any of
//     them is written. A card that widened an approval on its own would be this
//     surface deciding a permission on somebody's behalf.
//   - IT REPLACES THE OFFER, in place, on the row the offer was on. It is the
//     same question one step further in, not a second block appearing under the
//     first; the call above it does not move and the keyboard does not change
//     hands.
//   - ESC LEAVES THE BEAT AND ANSWERS NOTHING. Everywhere else on this block esc
//     denies, because the safe reading of "get this off my screen" is no. Here
//     the thing on screen is a step inside an answer, and backing out of a step
//     puts the question back — the call is still parked, and the person still
//     has every answer they had a moment ago.
//
// Only bash has a beat. Every other tool's always is about the tool's NAME,
// which has exactly one shape.

// always is the widening yes: either the answer, or the question that has to be
// answered before it.
func (a *app) always() {
	if len(a.asks) == 0 || a.shaping() {
		return
	}
	if shapes := a.askShapes(a.asks[0]); len(shapes) > 1 {
		a.asks[0].shapes = shapes
		a.touch()
		return
	}
	// One shape is not a choice, and none at all is a command nothing can be
	// derived from. Both answer the way this card always did: the session is
	// told, and [app.rememberAlways] writes the line if it can.
	a.answer(true, session.ConsentToolSession)
}

// askShapes is what a bash always could be banked as, or nothing at all.
//
// It reads the arguments through [app.askCommand] and derives from those, so a
// call whose payload never arrived whole has no shapes and gets no beat — the
// same silence that seam has always kept about a rule it cannot write honestly.
func (a *app) askShapes(head ask) []string {
	if head.tool != consentBash || a.saveBashApproval == nil {
		return nil
	}
	return config.BashShapes(a.askCommand(head))
}

// shaping reports whether the second beat is on screen. It is what decides
// which line the block draws where the offer goes, and which keys mean
// something (render.go's hint reads it too).
func (a *app) shaping() bool {
	return len(a.asks) > 0 && len(a.asks[0].shapes) > 0
}

// pickShape banks the shape at this index and answers the question with it.
func (a *app) pickShape(index int) {
	if !a.shaping() {
		return
	}
	head := &a.asks[0]
	if index < 0 || index >= len(head.shapes) {
		return
	}
	head.chosen = head.shapes[index]
	head.shapes = nil
	a.answer(true, session.ConsentToolSession)
}

// dropShapes takes the beat off and leaves the question exactly as it was.
func (a *app) dropShapes() {
	if !a.shaping() {
		return
	}
	a.asks[0].shapes = nil
	a.touch()
}

// shapeWord is how one shape reads on the offer. The last one is the line
// itself ([config.BashShapes] promises that), and it is named for what it does
// rather than repeated: the command is already on the row above, and printing
// it twice on two lines is the two-renderings defect this block exists to avoid.
func shapeWord(shapes []string, index int) string {
	if index == len(shapes)-1 {
		return "just this line"
	}
	return shapes[index]
}

// remembering reports whether pressing always would actually write something
// down. It is what decides the offer's WORDS — a card that said "always, this
// command" on a surface that cannot remember one would be promising a file it
// is not going to write.
func (a *app) remembering() bool {
	if len(a.asks) == 0 {
		return false
	}
	if a.asks[0].tool == consentBash {
		return a.saveBashApproval != nil
	}
	return a.saveApproval != nil
}

// alwaysWord is the always option's name: what it reaches, in the fewest words
// that are true.
//
// Three spellings and each says exactly what will happen. With nothing wired the
// answer lasts for this agent's life and the parenthetical says so — that is the
// card this file drew for a year. With a write seam behind it the answer is
// PERSISTED, the parenthetical would be a lie, and the object it is persisted
// against is named instead: the tool, or — for the one tool judged by its
// arguments — this command.
func (a *app) alwaysWord() string {
	if !a.remembering() {
		return "always, this tool (session)"
	}
	if a.asks[0].tool == consentBash {
		return "always, this command"
	}
	return "always, this tool"
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
		// scope stays a decision rather than a defaulted argument. Both of the
		// answers that reach past this call carry their scope through the wide
		// one: the memo and the banked rule are two different ways of not being
		// asked again, and the engine reads which from the word it is sent.
		if scope == session.ConsentToolSession || scope == session.ConsentRule {
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
	// BUT NOT BEHIND HOME. A QUESTION NOBODY CAN SEE IS A QUESTION NOBODY CAN
	// ANSWER, and home is the whole frame — the card, its command, its rule and
	// its three letters are all off screen while it is up. Answering it blind
	// from there would be this surface approving a call on the strength of a
	// keystroke aimed at something else, which is the one thing an approval
	// question exists to prevent.
	//
	// It costs nothing, because a question that ARRIVES takes home down on its
	// way in (app.go's EventConsentRequest) — so this is only ever reached by a
	// home somebody opened over a question already up, and there the letters are
	// theirs to type. Home has its own way to answer from where it stands, by
	// number, on the row of the window that is asking (homeband_answer.go), and
	// this rung is what lets those digits through.
	//
	// The three questions below all live under it: the proposal and the standing
	// card are read from this hook, and both take bare letters too.
	if a.at(pageHome) {
		return nil, false
	}
	// A TASK PROPOSAL IS THE OTHER QUESTION on this surface, and it is read from
	// the same hook because it is the same rung: a question the session is
	// blocked on outranks every overlay below it (input.go's key order). It is
	// deliberately NOT modal — the box under it is the redirect lane — so it
	// takes three keys and hands everything else back (task.go).
	if cmd, taken := a.taskKey(msg); taken {
		return cmd, true
	}
	// AND THE STANDING CARD IS THE THIRD QUESTION, read from the same hook and
	// on the same terms: the session is blocked on it, so it outranks every
	// overlay below, and it is not modal either — the box under it is the
	// correction lane (standing.go).
	if cmd, taken := a.standingKey(msg); taken {
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
	if a.shaping() {
		// THE BEAT OWNS THE KEYBOARD WHILE IT IS UP, and it owns y and n with
		// everything else: a person part-way through choosing a shape who pressed
		// y would be answering a question that is no longer the one on screen.
		// The numbers pick, esc goes back, and nothing else does anything.
		switch key := msg.String(); key {
		case "esc":
			a.dropShapes()
		default:
			if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
				a.pickShape(int(key[0] - '1'))
			}
		}
		return nil, true
	}
	switch msg.String() {
	case consentYes:
		a.answer(true, session.ConsentOnce)
	case consentAlways, "t":
		// The widening answer, and the ONE key on this block that is refused
		// when it would do nothing (see [ask.memo]). On bash it opens the second
		// beat rather than answering ([app.always]).
		if a.asks[0].memo {
			a.always()
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
//
// The phone sheet's height is COUNTED rather than derived, because its command
// region wraps: two questions about the same call are two different heights on
// the same frame, and a block whose height and whose rows disagreed would put
// the caret a row off the box. It is laid out at the frame's own width — the
// width [app.chrome] is drawn at — so the count and the drawing are the same
// arithmetic on the same number.
func (a *app) consentHeight() int {
	if !a.asking() {
		return 0
	}
	if width, _ := a.size(); a.consentSheeted(width) {
		return len(a.consentSheet(width))
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
	// The targets are rewritten by every layout and by nothing else: a stale
	// span is a tap that answers about the previous question (see [app.askTaps]).
	a.askTaps = nil
	if !a.asking() {
		return nil
	}
	if a.consentSheeted(width) {
		return a.consentSheet(width)
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

// consentMark is what the pointer is over on row i of the block, which is the
// frame's half of the same geometry ([app.chrome]).
//
// ON A PHONE THE WHOLE SHEET ANSWERS TO THE POINTER, not just the row with the
// answers on it. It is a sheet over the bottom of the screen, and the gap
// between two bands falling through to a transcript row underneath is how a
// thumb aimed at "deny" expands a tool call instead. Everywhere else the block
// is a block, and the offer line is the one row of it that was ever pressable.
func (a *app) consentMark(i, width int) chromeRow {
	if a.consentSheeted(width) {
		return chromeRow{kind: chromeChoices, index: i}
	}
	if i == consentOfferRow {
		// The index is the row within the block at EVERY tier, which is the
		// number [app.consentPress] resolves a target against. Here it is
		// [consentOfferRow] by definition, and saying so is what keeps one
		// meaning for one field.
		return chromeRow{kind: chromeChoices, index: consentOfferRow}
	}
	return chromeRow{}
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
	if a.shaping() {
		return a.consentShapes(width)
	}
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
	parts := offer(" " + a.alwaysWord())
	if ansi.StringWidth(strings.Join(parts, "")+a.consentClock()) > width {
		parts = offer(" always")
	}
	line := strings.Join(parts, "")
	if ansi.StringWidth(line) > width {
		return a.pal.ask(fit(line, width))
	}
	a.recordOfferTaps(parts)
	// The clock takes what is left over, and takes nothing when there is not
	// room for the whole of it.
	clock := a.consentClock()
	if clock != "" && ansi.StringWidth(line+clock) > width {
		clock = ""
	}
	return a.paintOffer(parts, clock, width)
}

// paintOffer inks one offer line: the words in the question hue, the keys — the
// only bold cells on it — inside them, and the clock's tail dim on the end.
//
// It is shared by the offer and by the second beat below because the two are
// one line in two states, and a beat that inked itself differently would read as
// a different object arriving rather than as the same question going on.
func (a *app) paintOffer(parts []string, clock string, width int) string {
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
		return a.pal.cursor(out, width)
	}
	return out
}

// consentShapes is the second beat's line, drawn where the offer was: the
// shapes by number, the line itself last, and the way back out.
//
// It degrades the offer's way and for the offer's reason. The escape hatch is
// the first thing dropped when the shapes will not fit beside it — a person who
// can see the shapes can still press esc, and a shape they cannot see is a
// shape they cannot choose.
func (a *app) consentShapes(width int) string {
	shapes := a.asks[0].shapes
	parts := make([]string, 0, len(shapes)*2+3)
	parts = append(parts, "always? ")
	for i := range shapes {
		parts = append(parts, "["+itoa(i+1)+"]", " "+shapeWord(shapes, i)+" · ")
	}
	full := append(append([]string{}, parts...), "[esc]", " never mind")
	if ansi.StringWidth(strings.Join(full, "")) <= width {
		parts = full
	} else {
		// No room for the way out: the last shape's separator would be a middot
		// with nothing after it.
		parts[len(parts)-1] = strings.TrimSuffix(parts[len(parts)-1], " · ")
	}
	line := strings.Join(parts, "")
	if ansi.StringWidth(line) > width {
		return a.pal.ask(fit(line, width))
	}
	a.recordShapeTaps(parts)
	return a.paintOffer(parts, "", width)
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
	if word := a.consentClockWord(); word != "" {
		return " · " + word
	}
	return ""
}

// consentClockWord is that countdown without the separator that joins it to a
// line of words — "7s", or "paused", or nothing. The sheet spells it alone in a
// corner, where a leading middot would be a middot with nothing on its left.
func (a *app) consentClockWord() string {
	left, running := a.askLeft()
	if !running {
		return ""
	}
	if a.askPaused {
		return "paused"
	}
	return countdownWord(left)
}

// ── the phone sheet ─────────────────────────────────────────────────────────
//
// UNDER SIXTY COLUMNS THE BLOCK BECOMES A BOTTOM SHEET.
//
// The block above is a line of words with three keys in it, and on a phone-sized
// frame it is the wrong object twice over. It is too wide — the offer's own
// narrow spelling exists because at that width the sentence stops fitting — and
// it is untappable, because there is no keyboard on a phone and `[y]` is three
// cells for a thumb that covers ten. So at [tierPhone] the same four facts are
// laid out as a sheet over the bottom of the screen:
//
//	───── ? bash ─────────────
//	 git commit -m "wave"
//	 bash pattern "git *"
//	──────────────────────────
//	 [y] allow
//	 [n] deny
//	 [a] always, this tool
//	 2 more                 8s
//
// Four decisions, and each is the phone's own:
//
//   - THE ANSWERS ARE BANDS, NOT COLUMNS. Three answers across forty-four
//     columns is fourteen cells each before the gaps, and a phone tier reaches
//     down to twenty columns where three columns is four cells each — a target
//     that misses. A band is the WHOLE ROW, at every width this tier has, and it
//     is the only shape that holds the floor everywhere.
//   - THE CLOCK IS ON ITS OWN CORNER. It stays bottom-right where the sketch put
//     it, but off the bands: a countdown drawn inside a full-width target is a
//     word you cannot touch without answering, and the one answer nobody means
//     to give is the one they were reaching past the clock for.
//   - THE COMMAND GETS ROOM. The block above re-uses the call's transcript row,
//     one line, cut to fit — and one line cut to fit at forty-four columns is an
//     approval prompt with the interesting half of the command missing. Here it
//     wraps, up to [consentSheetLines], and says so with an ellipsis when even
//     that was not enough.
//   - NOTHING NEW IS ASKED. Same three answers, same keys, same clock, same
//     queue count, same policy sentence — the sheet is a LAYOUT and not a second
//     question. esc still denies from the keyboard; its tap target is the deny
//     band, because on this surface cancelling and denying are one act.

const (
	// consentSheetLines caps the command region. Six lines is about two hundred
	// and fifty characters at this tier — longer than any command a person reads
	// before deciding — and the cap is what keeps one pathological argument from
	// pushing the answers off a short screen.
	consentSheetLines = 6
	// consentSheetFloor is the narrowest frame the sheet is drawn on. Under it a
	// band cannot hold its own label, and the line of words the block has always
	// drawn — which degrades by truncating rather than by breaking — is the
	// better shape.
	consentSheetFloor = 16
	// consentBandPad is the one cell of margin every row of the sheet opens
	// with. The bands are pressable edge to edge regardless: the margin is
	// breathing room for the eye, not a gap for the thumb.
	consentBandPad = " "
)

// consentSheeted reports whether the question is drawn as the phone sheet.
func (a *app) consentSheeted(width int) bool {
	return a.asking() && width >= consentSheetFloor && layoutTier(width) == tierPhone
}

// consentTap is one answer's columns on one row of the block. A press inside
// [span.from, span.to) on that row is that answer, and nothing outside any span
// answers anything (see [app.consentPress]).
type consentTap struct {
	span  hudSpan
	row   int
	allow bool
	scope session.ConsentScope
	// shape is which of the head question's [ask.shapes] this target banks, and
	// it is -1 on every ordinary answer. The two kinds share one list because
	// they share one row: the beat is drawn where the offer was, and a press
	// there means whichever of the two is on screen.
	shape int
}

const (
	// noShape is the shape of a target that ANSWERS rather than banks — every
	// target on the offer line, and the bands on the sheet.
	noShape = -1
	// backShape is the beat's way out. It is not an answer and must not be read
	// as one: pressing it puts the question back exactly as it was.
	backShape = -2
)

// consentSheet draws the phone form and records where its answers landed.
func (a *app) consentSheet(width int) []string {
	head := a.asks[0]
	name, command := a.consentWords(head)
	out := make([]string, 0, consentSheetLines+6)
	out = append(out, a.consentTitle(name, width))
	out = append(out, a.consentCommand(command, width)...)
	if head.rule != "" {
		out = append(out, a.pal.dim(fit(consentBandPad+head.rule, width)))
	}
	out = append(out, a.pal.dim(strings.Repeat("─", width)))

	taps := make([]consentTap, 0, 4)
	band := func(key, word string, allow bool, scope session.ConsentScope, shape int) {
		row := len(out)
		out = append(out, a.consentBand(row, key, word, width))
		taps = append(taps, consentTap{
			span: hudSpan{from: 0, to: width}, row: row, allow: allow, scope: scope, shape: shape,
		})
	}
	if a.shaping() {
		// THE BEAT IS BANDS HERE TOO. The line-of-words form replaces the offer
		// with the shapes; the sheet replaces the answers with them, because on
		// this tier an answer is a row a thumb lands on and a shape is an answer.
		for i := range head.shapes {
			word := shapeWord(head.shapes, i)
			if room := width - len(consentBandPad) - len("[1] "); ansi.StringWidth(word) > room {
				word = fit(word, room)
			}
			band(itoa(i+1), word, true, session.ConsentToolSession, i)
		}
		band("esc", "never mind", false, session.ConsentOnce, backShape)
		a.askTaps = taps
		if foot := a.consentFoot(width); foot != "" {
			out = append(out, foot)
		}
		return out
	}
	band(consentYes, "allow", true, session.ConsentOnce, noShape)
	band(consentNo, "deny", false, session.ConsentOnce, noShape)
	if head.memo {
		// The same spellings the offer line keeps ([app.alwaysWord]), and for the
		// same reason: the widening yes is the one answer whose name has to say
		// how far it reaches, and a name cut off mid-reach says less than the
		// short one. The sheet drops the parenthetical the line above carries —
		// a band is a target and reads at a glance — so an unwired surface says
		// the plain "always, this tool" it always said here.
		word := strings.TrimSuffix(a.alwaysWord(), " (session)")
		if ansi.StringWidth(consentBandPad+"["+consentAlways+"] "+word) > width {
			word = "always"
		}
		band(consentAlways, word, true, session.ConsentToolSession, noShape)
	}
	a.askTaps = taps
	if foot := a.consentFoot(width); foot != "" {
		out = append(out, foot)
	}
	return out
}

// consentWords is the call split in two: what the tool is, and what it was
// asked to do. The block above draws them joined, by the renderer that drew the
// transcript row (see [app.consentCall]); the sheet needs them apart, because
// the tool names the sheet and the command fills it.
//
// It reads the ROW where there is one, on the same terms [app.toolLine] does —
// the argument the row shows, falling back to the hint the gate sent — so the
// sheet and the line above it cannot be describing different calls.
func (a *app) consentWords(head ask) (string, string) {
	name, command := toolWords(head.tool, head.hint)
	if head.entry >= 0 && head.entry < len(a.entries) {
		if e := &a.entries[head.entry]; e.kind == entryTool {
			name, command = toolWords(e.tool, e.text)
			if target := toolTarget(e.tool, e.detail.Args, e.text); target != "" {
				command = target
			}
		}
	}
	if name == "" {
		name = head.tool
	}
	return name, command
}

// consentTitle is the sheet's head: a rule with the tool's name written into
// it, in the question hue. It is the same move the seam above the draft makes
// (render.go's legend) — a line that was already there, carrying the one word
// that says what this is.
func (a *app) consentTitle(name string, width int) string {
	label := " " + glyphAsk + " " + name + " "
	lead := 3
	if room := width - lead - 1; ansi.StringWidth(label) > room {
		label = fit(label, room)
	}
	rest := width - lead - ansi.StringWidth(label)
	if rest < 0 {
		rest = 0
	}
	return a.pal.dim(strings.Repeat("─", lead)) +
		a.pal.askBold(label) +
		a.pal.dim(strings.Repeat("─", rest))
}

// consentCommand is the command region: what is actually about to run, wrapped
// rather than cut, because the tail of a command is where the reason to say no
// usually is.
func (a *app) consentCommand(command string, width int) []string {
	// SCRUBBED BEFORE IT IS DRAWN. The region is built from the call's arguments
	// (see [app.consentWords]), which are the model's own text, and a terminal
	// reads an escape in them as an instruction rather than as a character — one
	// that can move the cursor onto the answers below and rewrite them. The gate
	// scrubs the gloss and the arguments at their source (session's loop.go);
	// this is the byte a surface makes again when it unmarshals one.
	command = strings.TrimSpace(plainText(command))
	if command == "" {
		// A call with no argument — the tool's own name is the whole of it, and
		// the title above already says it. A blank region would be a row spent
		// saying nothing.
		return nil
	}
	lines := wrap(command, width-len(consentBandPad))
	if len(lines) > consentSheetLines {
		lines = lines[:consentSheetLines]
		last := lines[consentSheetLines-1]
		lines[consentSheetLines-1] = fit(last+glyphMore, width-len(consentBandPad))
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, a.pal.ink(fit(consentBandPad+line, width)))
	}
	return out
}

// plainText drops the bytes a terminal takes as orders rather than as text. It
// DROPS rather than escapes, on notify.go's reasoning: there is no escape form
// that reads better here, and a row with a missing byte says more than a row
// with a stray backslash in it.
func plainText(text string) string {
	return strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, text)
}

// consentBand is one answer, drawn as a row: the key it also answers to, then
// the word. Under the pointer it takes the background every pressable row on
// this surface takes (hover.go) — which is an affordance for a mouse and a
// no-op for a thumb, so the row says what it is in WORDS as well.
func (a *app) consentBand(row int, key, word string, width int) string {
	text := a.pal.ask(consentBandPad) + a.pal.askBold("["+key+"]") + a.pal.ask(" "+word)
	if a.hoveringChoice(row) {
		return a.pal.cursor(text, width)
	}
	return text
}

// consentFoot is the sheet's bottom line: what is still queued behind this
// question on the left, and the countdown on the right. Both are dim and
// neither is a target — it is the row that reports, under the rows that act.
func (a *app) consentFoot(width int) string {
	var left string
	if more := len(a.asks) - 1; more > 0 {
		left = consentBandPad + itoa(more) + " more"
	}
	right := a.consentClockWord()
	if left == "" && right == "" {
		return ""
	}
	if right != "" {
		right += consentBandPad
	}
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		// No room for both: the clock goes, on the offer line's own rule — a
		// countdown a person cannot see is still a countdown, and the queue
		// count is the one of the two that says something about their next
		// decision rather than about this one.
		return a.pal.dim(fit(left, width))
	}
	return a.pal.dim(left + strings.Repeat(" ", gap) + right)
}

// hoveringChoice reports whether the pointer is on this row of the block. It is
// [app.hoveringChoices] with the row asked as well, which is what the sheet
// needs and the one-line offer never did.
func (a *app) hoveringChoice(row int) bool {
	return a.hot.kind == hoverChoices && a.hot.index == row
}

// ── the pointer ─────────────────────────────────────────────────────────────

// recordOfferTaps writes the offer line's columns: every key chip on it, and
// the word beside it, are one target.
//
// THE WORD IS PART OF THE TARGET and not decoration beside it. `[y]` is three
// cells; `[y] yes` is seven, which is the difference between a target a person
// hits and one they aim at. The separator that leads to the next answer is left
// out of both — a press in the gap between two answers must not be able to
// resolve as either.
func (a *app) recordOfferTaps(parts []string) {
	answer := func(chip string) (bool, session.ConsentScope, bool) {
		switch chip {
		case "[" + consentYes + "]":
			return true, session.ConsentOnce, true
		case "[" + consentAlways + "]":
			return true, session.ConsentToolSession, true
		case "[" + consentNo + "]", "[esc]":
			return false, session.ConsentOnce, true
		}
		return false, session.ConsentOnce, false
	}
	taps := make([]consentTap, 0, 4)
	at := 0
	for i, part := range parts {
		width := ansi.StringWidth(part)
		allow, scope, ok := answer(part)
		if !ok {
			at += width
			continue
		}
		to := at + width
		if i+1 < len(parts) {
			to += ansi.StringWidth(strings.TrimSuffix(parts[i+1], " · "))
		}
		taps = append(taps, consentTap{
			span: hudSpan{from: at, to: to}, row: consentOfferRow,
			allow: allow, scope: scope, shape: noShape,
		})
		at += width
	}
	a.askTaps = taps
}

// recordShapeTaps is [app.recordOfferTaps] for the second beat: the same
// row, the same "the key and its word are one target" bargain, and the numbers
// banking a shape instead of the letters answering.
func (a *app) recordShapeTaps(parts []string) {
	taps := make([]consentTap, 0, len(parts)/2+1)
	at, shape := 0, 0
	for i, part := range parts {
		width := ansi.StringWidth(part)
		to := at + width
		if i+1 < len(parts) {
			to += ansi.StringWidth(strings.TrimSuffix(parts[i+1], " · "))
		}
		span := hudSpan{from: at, to: to}
		at += width
		switch {
		case part == "[esc]":
			taps = append(taps, consentTap{
				span: span, row: consentOfferRow, scope: session.ConsentOnce, shape: backShape,
			})
		case i%2 == 1:
			taps = append(taps, consentTap{
				span: span, row: consentOfferRow, allow: true,
				scope: session.ConsentToolSession, shape: shape,
			})
			shape++
		}
	}
	a.askTaps = taps
}

// consentPress resolves a click on the question, and reports whether it took
// it.
//
// THE BLOCK SWALLOWS EVERY PRESS IN IT, answer or no answer. It is the one
// thing on screen the session is blocked on; a press that missed a band and
// fell through to the transcript underneath would expand a tool call while the
// person was trying to deny one — and on the sheet, which covers the bottom of
// the screen, "underneath" is most of what they can see.
//
// AND A PRESS STOPS THE CLOCK, exactly as every key does ([app.consentKey] says
// why): the countdown exists so an unattended session cannot park a call
// forever, and a pointer inside the question is a person at the machine.
func (a *app) consentPress(x, y int) bool {
	if !a.asking() || a.copy.on || a.at(pageSettings) {
		return false
	}
	// THE ROW IS RESOLVED BEFORE THE COLUMN, and that order is load-bearing for
	// the reason [app.statusPress] states: laying the chrome out is what writes
	// the spans, and reading them first would be reading where the answers were
	// drawn on the frame before this one.
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeChoices {
		return false
	}
	a.pauseAsk()
	for _, tap := range a.askTaps {
		if tap.row != mark.index || !tap.span.holds(x) {
			continue
		}
		switch {
		case tap.shape == backShape:
			a.dropShapes()
			return true
		case tap.shape >= 0:
			a.pickShape(tap.shape)
			return true
		}
		if tap.scope == session.ConsentToolSession && !a.asks[0].memo {
			// The one answer that is refused when it would do nothing, refused
			// here for the same reason the key is (see [ask.memo]).
			return true
		}
		a.answer(tap.allow, tap.scope)
		return true
	}
	return true
}
