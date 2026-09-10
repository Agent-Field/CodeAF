package tui3

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE SPLICE, FROM THE KEYBOARD: a sentence sent INTO the answer that is
// already running.
//
// Plain enter is the expected chat gesture and therefore the primary door. It
// cuts the generation that is currently streaming, preserves its arrived text,
// and sends the draft into the same turn. `cmd+enter` keeps the older choice to
// park the draft for the next turn, and `shift+enter` still stops the whole turn
// before sending (bargein.go).
//
// A STEER INTERRUPTS ONE GENERATION, NOT THE TURN. The person's own line is
// drawn as soon as EventSteerAccepted arrives, with a muted clause saying
// whether it cut the reply, preserved a bash job, stopped that command, or is
// waiting for a short tool boundary (steerelbow.go).
//
// ── TWO DOORS, ONE ROAD ─────────────────────────────────────────────────────
//
//	enter       with a sentence in the box: that sentence goes in.
//	cmd+enter   with a sentence in the box: that sentence waits.
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
// ── THE EVENTS DRAW THE PERSON'S LINE ──────────────────────────────────────
//
// The three events a steer produces — [session.EventSteerAccepted],
// [session.EventSteerConsumed], [session.EventSteerFellThrough] — are sent
// through the RUNNING TURN'S hub, so they arrive on the stream this surface is
// already pumping and are drawn by the transcript's own arms (steerelbow.go).
//
// AND THE STEER'S OWN CHANNEL IS NOT THIS FILE'S EITHER. [session.Agent.Steer]
// hands back a stream of its own, and every door here does the same one thing
// with it: gives it to [app.holdSteer] and forgets it. That lane is the single
// reader — a channel with two of them is a channel whose events are split
// between them — and it exists for the one thing the turn's own stream cannot
// carry: a steer that fell through starts a turn that speaks on that channel
// and nowhere else.

// parkKey is the secondary chord as a PERSON spells it. Plain enter is the
// primary send and steers while a turn runs; this chord preserves the older
// choice to hold the sentence for the answer after this one.
//
// WHY THIS ONE — WHAT THE AUDIT LEFT. The gesture has to read as a SEND rather
// than as a letter, which means a modifier on enter, and the other three are
// spent: `alt+enter` (with `ctrl+j`) opens a line, `ctrl+enter` marks the
// sentence as something to keep true (standmark.go), and `shift+enter` stops the
// answer and sends (bargein.go). cmd+enter is what is left, and it is the right
// one on its own merits — it is the "send it now, properly" chord in every chat
// application a person has ever used. Here it is deliberately secondary: plain
// enter reaches the model, while this chord says to wait.
const parkKey = "cmd+enter"

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
// under the box says `enter steers it in`; the waiting message's own dim
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

// spoken is this message as the model reads it: its own chips unfolded into
// its own text (pastechip.go's [unfoldPastes]). The chips stay on the message,
// because a refusal puts it back whole and ↑ pulls it back into the box.
func (p parked) spoken() string {
	return unfoldPastes(p.text, p.pastes)
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

// steerOffered reports whether plain enter may be named as a steer — which is
// [app.steerAvailable] plus the sentence it needs to send.
//
// PLAIN ENTER NEEDS NO TERMINAL CAPABILITY. This predicate deliberately does
// not share [app.bargeOffered]'s keyboard gate: the primary gesture reaches
// every terminal, while the two secondary chords are named only when the
// terminal says it can distinguish them.
func (a *app) steerOffered() bool {
	if !a.steerAvailable() {
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

// steerIn is plain enter during a running turn: the draft goes into the answer
// that is running.
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
	// THE MODEL READS THE PASTE, THE SCREEN KEEPS THE TAG: what goes down the
	// wire is the message unfolded, and what the row and a refusal's put-back
	// carry is the message as it was parked (pastechip.go).
	spoken := one.spoken()
	return func() tea.Msg {
		ch, err := agent.Steer(spoken)
		return steeredMsg{words: one.text, park: one, ch: ch, err: err}
	}
}

// steeredMsg is the session's answer to one steer. It is a message rather than a
// call because [session.Agent.Steer] takes the agent's lock, and the Update loop
// is not a place to wait — followup.go's [followMsg] makes the same bargain for
// the same reason.
type steeredMsg struct {
	words string
	// park is the message as it was parked — its chips with it — so a refusal
	// puts back exactly what it took.
	park parked
	ch   <-chan session.Event
	err  error
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
		back := msg.park
		if back.text == "" {
			back = parked{text: msg.words}
		}
		a.parks = append([]parked{back}, a.parks...)
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
	// AND THE CHANNEL IS HANDED STRAIGHT TO THE ONE LANE THAT READS IT
	// (steerelbow.go's [app.holdSteer]). This door's whole business with the
	// stream is over on this line: it does not read a single event off it, and
	// it must not — TWO READERS ON ONE CHANNEL SPLIT ITS EVENTS BETWEEN THEM,
	// and the two events the seam turns on would then each land wherever the
	// scheduler happened to put them. There is exactly one pump.
	return a.holdSteer(msg.ch)
}

// ── the line that teaches the chord ─────────────────────────────────────────

// typingHint is the send half of the running-turn hint while there is something
// in the box or on the tray.
//
// It teaches plain enter first, then the stop-and-send chord only when the
// terminal says it can distinguish it. `cmd+enter waits` remains on the keys
// page, but this live slot spends its cells on the actions that move now.
//
//	enter steers it in · shift+enter stops and sends
var steerShortHint = "enter " + steerSendWord

// enterWaitHint is the plain-enter half of the running-turn hint. The tray and
// the box share this exact clause because either makes plain enter wait.
const enterWaitHint = "enter waits"

func (a *app) typingHint() string {
	if !a.runSendOffered() {
		return ""
	}
	first := enterWaitHint
	if a.steerOffered() {
		first = steerShortHint
	}
	if !a.bargeOffered() {
		return first
	}
	return first + hintSegment + bargeKey + " " + bargeSendWord
}

// runSendOffered is [app.bargeOffered] without the terminal's chord gate. It is
// the common truth under both send clauses: there is a message and the
// conversation's box still owns its keys.
func (a *app) runSendOffered() bool {
	if a.state != stateWorking || a.input.empty() && len(a.chips) == 0 {
		return false
	}
	return !a.roomOpen() && !a.copy.on && !a.rew.on && !a.railHold
}

// runHint is the one line while a turn runs. Its order follows the hand across
// the box: send, stop-and-send, background, stop. Every conditional clause asks
// the same predicate as its key, so a word in this line is a working gesture on
// the frame that drew it.
func (a *app) runHint() string {
	parts := make([]string, 0, 4)
	if send := a.typingHint(); send != "" {
		parts = append(parts, strings.Split(send, hintSegment)...)
	}
	if a.promotableRow() >= 0 {
		parts = append(parts, "ctrl+g backgrounds")
	}
	stop := "esc interrupt"
	if len(a.parks) > 0 && a.parking() {
		stop = parkedHint[1]
	}
	parts = append(parts, stop)
	return strings.Join(parts, hintSegment)
}

// hintShorter is the running slot said in fewer cells, or "" where there is no
// shorter true form — which is every other state and its one-clause floor.
//
// THE LADDER DROPS FROM THE RIGHT. That preserves the fixed priority of the
// full sentence and guarantees forward progress: one clause is the floor and
// returns nothing, so [app.legend] can never loop on an unshortenable rung.
func (a *app) hintShorter(slot string) string {
	full := a.runHint()
	if slot == "" || slot != full && !strings.HasPrefix(full, slot+hintSegment) {
		return ""
	}
	parts := strings.Split(slot, hintSegment)
	if len(parts) <= 1 {
		return ""
	}
	return strings.Join(parts[:len(parts)-1], hintSegment)
}
