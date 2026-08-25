package tui3

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE SPLICE, FROM THE KEYBOARD: a sentence sent INTO the answer that is
// already running.
//
// THE GAP THIS FILE CLOSES. Until this wave a person watching an answer go the
// wrong way had two keys and both of them cost something. Plain enter PARKS the
// sentence and waits for the turn to end (park.go), so the whole rest of the
// answer is spent on the wrong thing first. `shift+enter` STOPS the turn and
// sends (bargein.go), so the work already paid for and watched arrive is thrown
// away. The third answer — put the correction into the turn that is running,
// stopping nothing and discarding nothing — existed in the engine
// ([session.Agent.Steer], internal/session's steer.go) and had no key.
//
// A STEER IS NEITHER A NEW QUESTION NOR AN INTERRUPTION. It is more of the same
// question, arriving late: the words are handed to the running turn and reach
// the model at its next step boundary, with the original question and
// everything done about it so far in front of them. Nothing is cancelled and no
// partial reply is discarded.
//
// ── TWO DOORS, ONE ROAD ─────────────────────────────────────────────────────
//
//	cmd+enter   with a sentence in the box: that sentence goes in.
//	→           with an EMPTY box and a message already waiting: that message
//	            is promoted out of the queue and goes in (park.go).
//
// Both are [app.promoteParked]. The chord does not reimplement the send: it
// takes [app.enterLine]'s whole road exactly as [app.bargeIn] does — so a slash
// command still runs at once, a live tag still takes its own door, a picked
// harness still takes the sentence, an empty box still does nothing — and then
// promotes the message that road parked. That is not an economy; it is the
// correctness argument, and it is bargein.go's, said again: there is no second
// copy of the guard list to drift out of step with the first.
//
// ── WHAT CANNOT BE STEERED, AND WHY IT IS LEFT WAITING ──────────────────────
//
// [session.Agent.Steer] is WORDS ONLY — pictures reach a running turn by their
// own door, which assembles parts and journals durable references — and a
// sentence somebody MARKED as something to keep true is a sentence bound for a
// different door entirely (standmark.go). Neither is refused with a note: the
// message simply stays parked and goes as its own turn, which is what it would
// have done had nobody pressed anything. A gesture whose failure mode is the
// default behaviour needs no apology for it.
//
// ── THE REFUSAL IS ANSWERED BY DELIVERING THE WORDS ANYWAY ──────────────────
//
// The engine refuses a steer with [session.ErrNothingToSteer] when no turn is in
// flight, and there is a real window in which that is the honest answer and the
// person did nothing wrong: the turn ended between the frame that offered the
// key and the finger that pressed it. THE PERSON WANTED THE WORDS DELIVERED, so
// they are delivered the ordinary way — back onto the waiting queue, at the head
// they left, where the queue's own machinery sends them ([app.sendParked]). No
// note is drawn, because nothing went wrong: what they see is a message that
// waited, which is what plain enter promises.
//
// ── THIS FILE DRAWS NOTHING ─────────────────────────────────────────────────
//
// The three events a steer produces — [session.EventSteerAccepted],
// [session.EventSteerConsumed], [session.EventSteerFellThrough] — are sent
// through the RUNNING TURN'S hub, so they arrive on the stream this surface is
// already pumping and are drawn by the transcript's own arms. Nothing here
// paints a row.

// steerKey is the chord as a PERSON spells it, and it is the name the hint slot
// and the manual use. The two names the WIRE spells it with are below.
//
// WHY THIS ONE — WHAT THE AUDIT LEFT. The gesture has to read as a SEND rather
// than as a letter, which means a modifier on enter, and the other three are
// spent: `alt+enter` (with `ctrl+j`) opens a line, `ctrl+enter` marks the
// sentence as something to keep true (standmark.go), and `shift+enter` stops the
// answer and sends (bargein.go). cmd+enter is what is left, and it is the right
// one on its own merits — it is the "send it now, properly" chord in every chat
// application a person has ever used, and this is the one send on this surface
// that reaches the model without waiting for anything.
const steerKey = "cmd+enter"

// steerKeySuper and steerKeyMeta are the two names the SAME keystroke arrives
// under, and both are bound because which one a terminal sends is a fact about
// the road the bytes took rather than about the hand that made them.
//
// A kitty-protocol terminal sends `CSI 13;9u`, which ultraviolet's CSI-u reader
// decodes against the kitty modifier table, where bit 8 is `super`. A terminal
// speaking xterm's modifyOtherKeys sends `CSI 27;9;13~` instead, which is read
// against the STATIC table, where the ninth column is `meta`. Same key, same
// hand, two names — the same split that left cmd+←/→ bound as `super+left` and
// dead on every terminal there is until commit 7c427797 bound both. The wire
// test in steer_test.go is what keeps this comment honest rather than the
// comment itself.
const (
	steerKeySuper = "super+enter"
	steerKeyMeta  = "meta+enter"
)

// steerSendWord is what this gesture DOES, in the words both lines that name it
// use — ONE SOURCE OF TRUTH for a person-facing phrase that now appears in two
// slots on one screen, which is [bargeSendWord]'s own arrangement. The hint
// under the box says `cmd+enter steers it in`; the waiting message's own dim
// line says `→ steers it in`; they are the same three words because they are
// the same act.
const steerSendWord = "steers it in"

// steerArrowWord is the strip's whole clause: the key, and what it does. The
// arrow is the key a person presses over an empty box, and the word is the
// door they can click instead (park.go's [app.parkedRows] records where it
// landed).
const steerArrowWord = "→ " + steerSendWord

// steerAgent is a session that can take a sentence into the turn it is already
// running. It is ASSERTED rather than added to [Agent], for the reason
// [wakeAgent] is (followup.go): a surface driven by a scripted agent that
// cannot steer must stay representable — and A CAPABILITY THAT CANNOT WORK IS
// ABSENT, NOT BROKEN, so where the assertion fails the chord does nothing and
// neither line names it.
type steerAgent interface {
	Steer(words string) (<-chan session.Event, error)
}

// steerable reports whether this session has the verb at all.
func (a *app) steerable() bool {
	_, ok := a.agent.(steerAgent)
	return ok
}

// steerable reports whether ONE waiting message can go into a running turn.
//
// A message with pictures cannot: [session.Agent.Steer] takes words and the
// tray travels by its own door. A message somebody MARKED cannot either: the
// mark says the sentence is bound for the standing door, and a steer that
// dropped it would be the sentence quietly becoming ordinary work, which is the
// one ending that gesture exists to rule out (standmark.go).
func (p parked) steerable() bool {
	return p.text != "" && len(p.chips) == 0 && !p.standing
}

// nextSteerable is the position of the message a promotion would take: the
// OLDEST that can go, which is the queue's own order and the one esc would send
// first. It answers -1 when there is nothing to promote.
//
// The oldest rather than the newest, which is where this parts company with ↑
// (park.go's [app.recallParked]): ↑ is "the thing I just typed and want to fix",
// and this is "the thing at the front of the queue, now" — the same message the
// queue was always going to send next, arriving a whole turn earlier.
func (a *app) nextSteerable() int {
	for i := range a.parks {
		if a.parks[i].steerable() {
			return i
		}
	}
	return -1
}

// steerAvailable reports whether the KEY would do something if it were pressed
// right now — every condition except the one about the terminal.
//
// THE TERMINAL IS NOT ASKED HERE, and that is the one place this parts company
// with [app.bargeOffered]. `shift+enter` on a terminal that cannot disambiguate
// arrives as a plain `enter` and must be guarded against, because the name it
// answers to is a name that terminal also sends for something else. `cmd+enter`
// has no such twin: a keystroke that arrives spelled `super+enter` or
// `meta+enter` has already PROVED the terminal can spell it, whichever road it
// came by. Gating the key on the kitty reply would kill the modifyOtherKeys
// road, which sends no reply and delivers the chord perfectly.
func (a *app) steerAvailable() bool {
	if !a.steerable() {
		return false
	}
	if a.state != stateWorking {
		// NOTHING TO STEER. At rest plain enter already sends, so a second chord
		// meaning the same thing would be a key that teaches a person a gesture
		// they do not need — and one that meant something ELSE at rest would be a
		// chord with two readings a hand cannot tell apart. So it does nothing at
		// all, which is exactly what `shift+enter` does at rest and for the same
		// reason (bargein.go).
		return false
	}
	// AND IT IS ABSENT WHEREVER THE BOX IS NOT THE CONVERSATION'S, which is
	// [app.bargeOffered]'s list for its reasons: in a room the draft steers a
	// NODE and enter sends it there and then, copy mode and the rewind have taken
	// the keyboard outright, and the rail holds it while the roster is up. Every
	// one of these is read above the plain switch in [app.key], so the guard is
	// here for the HINT's sake as much as the key's.
	return !a.roomOpen() && !a.copy.on && !a.rew.on && !a.railHold
}

// steerOffered reports whether the CHORD may be named — which is
// [app.steerAvailable] plus the two things an advertisement needs and a key does
// not: a terminal that can deliver it, and a sentence for it to send.
//
// A HINT MAY ONLY NAME A KEY THAT WORKS (render.go's [app.hintWord] states the
// whole law). The terminal question errs in the safe direction here exactly as
// it does for the barge: a terminal that speaks modifyOtherKeys and not the
// kitty protocol never answers the query, so the chord WORKS there and is never
// advertised — a feature quietly present rather than a hint that lies.
func (a *app) steerOffered() bool {
	if !a.keysDisambiguated || !a.steerAvailable() {
		return false
	}
	// A TRAY WITH NO WORDS IS NOT A STEER. An empty box with a picture on the
	// tray is a message [app.enterLine] will happily park, and the parked message
	// then cannot be promoted — so the chord is real but this is not the state to
	// name it in.
	return !a.input.empty()
}

// steerParkOffered reports whether the STRIP's arrow may be named: a turn to
// steer, a session that can, and a waiting message that is words alone.
//
// It does not ask about the box. The arrow means the caret the moment there is
// a sentence to move it through, and the clause stays on the line anyway,
// because the WORD IS A DOOR — a click on it promotes the message at any width
// and with anything typed ([app.steerDoorPress]). That is `↑ or click to edit`'s
// own arrangement one piece to the left: the key half is conditional, the
// pointer half never is, and the line names the pair.
func (a *app) steerParkOffered() bool {
	return a.steerAvailable() && a.nextSteerable() >= 0
}

// ── the chord ───────────────────────────────────────────────────────────────

// steerIn is cmd+enter: the draft goes into the answer that is running.
//
// THE ORDER IS PARK-THEN-PROMOTE AND IT IS THE WHOLE CORRECTNESS ARGUMENT.
// [app.enterLine] is called while the turn is still open, so [app.parking] is
// true and the sentence is PARKED rather than sent — which means every other
// thing that road can do instead of parking has already happened by the time
// this asks. A chord that spliced a slash command into a model's transcript, or
// sent a picked harness's request to the wrong door, would be the worst kind of
// surprise.
//
// AND IT ONLY PROMOTES SOMETHING IF IT SAID SOMETHING. The park queue growing is
// the one honest signal that the road took a message rather than doing one of
// the dozen other things enter does — a command, a tag's own door, a refusal, a
// tool row opened, nothing at all ([app.bargeIn] reads the same signal for the
// same reason).
func (a *app) steerIn() tea.Cmd {
	if !a.steerAvailable() {
		// THE KEY IS ABSENT WHEREVER IT CANNOT WORK, and absent means it does
		// nothing at all rather than saying it cannot (spellout.go's [app.spellKey]
		// states it). Swallowing is safe because this chord carries no text —
		// ultraviolet gives KeyEnter the CR rune, which is not printable, so the
		// bottom of input.go's router would have done nothing with it anyway.
		return nil
	}
	waiting := len(a.parks)
	// The mark is deliberately not passed, for [app.bargeIn]'s reason: ctrl+enter
	// is the gesture that means "keep this true" and this one means "and also
	// this" — a chord that did both would be one keystroke making two decisions.
	cmd := a.enterLine(false)
	if len(a.parks) == waiting {
		// The road did something other than park. The turn is left alone and the
		// words went wherever that road sends them, which is the guard above said
		// again.
		return cmd
	}
	// THE SENTENCE JUST PARKED IS THE ONE THAT GOES, and it is found by position
	// rather than by [app.nextSteerable]: the person pressed the key on THESE
	// words, and an older message waiting in front of them is one they already
	// chose to let wait.
	return tea.Batch(cmd, a.promoteParked(len(a.parks)-1))
}

// ── the arrow ───────────────────────────────────────────────────────────────

// steerWaiting is → over an empty box: the message at the front of the waiting
// queue is promoted into the running answer. It reports whether it took the key.
//
// It exists ONLY WHERE → HAD NO MEANING. With a sentence in the box the arrow is
// the caret's, as it is everywhere else on this surface — input.go's own law
// about the empty box, and the one thing that keeps a navigation key from eating
// an edit.
func (a *app) steerWaiting() (tea.Cmd, bool) {
	at := a.nextSteerable()
	if !a.steerAvailable() || at < 0 {
		return nil, false
	}
	return a.promoteParked(at), true
}

// steerDoorPress is a click on the strip's steer word.
//
// THE LINE PRINTS A GESTURE, SO THE LINE ANSWERS TO IT. A dim line naming a door
// that only opens for the keyboard would be the one dead phrase on the screen —
// which is the argument the block above it already makes for `click to edit`
// (park.go's [app.parkPress]).
//
// It answers by ROW AND COLUMN, unlike the block above it: this row is a
// sentence of four clauses and only one of them is a door, so a press anywhere
// along it would be three phrases quietly acting as a fourth.
func (a *app) steerDoorPress(x, y int) (tea.Cmd, bool) {
	if !a.steerDoor.holds(x) {
		return nil, false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeParkedHint {
		return nil, false
	}
	cmd, took := a.steerWaiting()
	if !took {
		return nil, false
	}
	return cmd, true
}

// ── the road both doors take ────────────────────────────────────────────────

// promoteParked lifts one waiting message off the queue and hands it to the
// running turn.
//
// The message LEAVES THE QUEUE HERE, before the session has answered, and that
// is deliberate: from the person's side the words have gone, and a strip that
// went on saying `waits for this answer` for the round trip would be the surface
// showing them a message they had just sent. The one outcome that puts it back
// is the refusal below, which puts it back where it was.
func (a *app) promoteParked(at int) tea.Cmd {
	if at < 0 || at >= len(a.parks) {
		return nil
	}
	one := a.parks[at]
	if !one.steerable() {
		// LEFT EXACTLY WHERE IT WAS. A message with pictures on it, or one marked
		// as something to keep true, waits for the turn to end and goes through its
		// own door — which is what it was always going to do.
		return nil
	}
	agent, ok := a.agent.(steerAgent)
	if !ok {
		return nil
	}
	a.parks = append(a.parks[:at], a.parks[at+1:]...)
	a.follow()
	a.touch()
	words := one.text
	return func() tea.Msg {
		ch, err := agent.Steer(words)
		return steeredMsg{words: words, ch: ch, err: err}
	}
}

// steeredMsg is the session's answer to one steer. It is a message rather than a
// call because [session.Agent.Steer] takes the agent's lock, and the Update loop
// is not a place to wait — followup.go's [followMsg] makes the same bargain for
// the same reason.
type steeredMsg struct {
	words string
	ch    <-chan session.Event
	err   error
}

// steerEventMsg is one event off a steer's own stream. See [app.steerEvent] for
// why that stream is read at all and why nothing is drawn from it.
type steerEventMsg struct {
	words string
	ch    <-chan session.Event
	ev    session.Event
	ok    bool
}

// tookSteer takes the session's answer.
//
// THE REFUSAL IS NOT SHOWN, IT IS ANSWERED. [session.ErrNothingToSteer] means
// the turn ended between the frame that offered the key and the press — the one
// failure here that is nobody's mistake — so the words go back onto the waiting
// queue at the head, where the ordinary machinery sends them: at once if nothing
// is being pumped, and at the next stream close if a stopped turn is still
// winding down. What the person asked for was that these words be delivered, and
// they are.
//
// Every OTHER error is a note, because every other one means something a person
// needs to know: a closed session, a message that was empty by the time it
// arrived.
func (a *app) tookSteer(msg steeredMsg) tea.Cmd {
	if errors.Is(msg.err, session.ErrNothingToSteer) {
		a.parks = append([]parked{{text: msg.words}}, a.parks...)
		a.follow()
		a.touch()
		if a.stream != nil {
			return nil
		}
		return a.sendParked()
	}
	if msg.err != nil {
		a.note("steer failed: " + msg.err.Error())
		return nil
	}
	if msg.ch == nil {
		return nil
	}
	return waitSteer(msg.words, msg.ch)
}

// waitSteer takes one event off a steer's own stream.
func waitSteer(words string, ch <-chan session.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		return steerEventMsg{words: words, ch: ch, ev: ev, ok: ok}
	}
}

// steerEvent reads a steer's own stream, and it DRAWS NOTHING OFF IT.
//
// WHY THE STREAM IS READ AT ALL, GIVEN THAT. It is a second subscriber to the
// hub of the turn being steered, so every event on it is an event the stream
// this surface is already pumping delivered a moment ago — the three steer
// events included, which is how the transcript's own arms see them. Drawing
// from here would draw the running turn twice. So the events are spent, and
// exactly one of them is acted on.
//
// [session.EventSteerFellThrough] IS THE SEAM. A steer whose turn ended before a
// step boundary came is not dropped and is not pretended about: the session
// lifts the words onto its own follow-up queue and CARRIES THIS STREAM ACROSS
// WITH THEM, so the turn those words then start of their own accord speaks here
// and nowhere else (internal/session's steer.go). Left unread it would be a
// whole turn running with nothing on the screen about it — so at that event the
// channel stops being a duplicate and becomes a queued message's stream, which
// is followup.go's own shape for exactly this ([app.queueFollow]): queued behind
// whatever is being pumped, adopted at the next close, and adopted at once when
// nothing is.
//
// The queue is drained ahead of it rather than jumped, which is followup.go's
// ordering law and is right here too: a ctrl+q message handed over before this
// steer was typed is a sentence the person said first.
func (a *app) steerEvent(msg steerEventMsg) tea.Cmd {
	if !msg.ok {
		// The turn ended with the steer consumed, so the hub closed this stream
		// with the rest of them. There is nothing left to carry.
		return nil
	}
	if msg.ev.Kind != session.EventSteerFellThrough {
		return waitSteer(msg.words, msg.ch)
	}
	a.follows = append(a.follows, queued{text: msg.words, ch: msg.ch})
	a.touch()
	if a.stream != nil {
		return nil
	}
	return a.startFollow()
}

// ── the line that teaches the chord ─────────────────────────────────────────

// typingHint is the hint slot while a turn is running with something in the box.
//
// It teaches every meaning enter's neighbourhood has in that one state, in the
// order a person meets them: the key they are about to press, then the two they
// do not know about — and those two in the order of what they COST, because the
// gentler one being second would be this line recommending the interrupt.
//
//	enter waits · cmd+enter steers it in · shift+enter stops and sends
//
// The steer clause is dropped on a terminal that cannot deliver the chord, which
// is [app.steerOffered]'s whole job, and what is left is the line bargein.go
// already drew. The slot is the legend's right end, so a frame too narrow for
// the longer sentence drops the whole hint rather than wrapping it — the
// legend's own ladder, unchanged (render.go's [app.legend]).
func (a *app) typingHint() string {
	if !a.steerOffered() {
		return bargeHint
	}
	return "enter waits · " + steerKey + " " + steerSendWord +
		" · " + bargeKey + " " + bargeSendWord
}

// hintShorter is the same slot said in fewer cells, or "" where there is no
// shorter true form — which is every state but this one.
//
// THE SLOT IS ALL OR NOTHING WITHOUT IT. The legend's ladder drops the whole
// hint when the sentence will not fit beside the rule, so the third clause
// arriving in this line would have taken the other two off narrow frames with
// it: a person on a seventy-column terminal would have stopped being told about
// `shift+enter` because `cmd+enter` had been added. The shorter form is the line
// as it read before the splice existed — still exactly true, still naming a key
// that works — and render.go's [app.legend] measures it with the same
// arithmetic it measures everything else with.
func (a *app) hintShorter(slot string) string {
	if slot == "" || slot != a.typingHint() || slot == bargeHint {
		return ""
	}
	return bargeHint
}
