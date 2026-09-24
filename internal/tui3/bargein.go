package tui3

import (
	tea "charm.land/bubbletea/v2"
)

// BARGE-IN: the way a person actually interrupts is by SPEAKING.
//
// THE GAP THIS FILE CLOSES. Two gestures already exist for a sentence typed
// over a running answer and neither says "stop, then send these words". Plain
// enter PARKS the message and waits for the turn to end (park.go); ctrl+c STOPS
// the turn and drops everything waiting. So the person who is watching an
// answer go the wrong way and types "no — the OTHER file" needs one deliberate
// gesture that preserves the correction while ending the answer.
//
// So this is those two presses as ONE act. The draft is in the box, the answer
// is streaming, and one chord means: stop this, and here is what I want instead.
//
// ── IT IS ENTER PLUS A STOP, LITERALLY ──────────────────────────────────────
//
// [app.bargeIn] does not reimplement the send. It calls [app.enterLine] — the
// same road plain enter takes, with every guard on it — and then, ONLY IF that
// road parked something, interrupts. That is not an economy, it is the whole
// correctness argument: a slash command still runs at once instead of being
// stopped for, a live `/task` tag still goes through its own door, two tags
// still refuse, a picked harness still takes the sentence, `@task` mentions
// still grow their footnotes, the tray of pictures still travels with the
// words, and an empty box still does nothing. There is no second copy of that
// list to drift out of step with the first.
//
// ── AND THE ORDERING IS THE PARK QUEUE'S, UNCHANGED ─────────────────────────
//
// THE SEND DOES NOT RACE THE INTERRUPT, because it is not this gesture that
// sends. The message goes onto [app.parks], the turn is stopped, the stream
// closes, and app.go's streamClosedMsg drains the park queue exactly as it
// drains it after any other interrupt. So the send cannot reach the session
// before the turn it interrupted has truly ended — session.ErrTurnInFlight is
// unreachable from here, not by a check but because [app.sendParked] stands
// down while `a.stream != nil` and the close is the only thing that calls it.
//
// What the transcript then reads is the truth with NO NEW CLAIMS ON IT: the
// partial answer, kept; the `stopped` note; then the person's message
// opening the next turn. Every one of those rows is drawn by machinery that was
// already there.
//
// ── PLAIN ENTER IS UNTOUCHED ────────────────────────────────────────────────
//
// THE SAFE DEFAULT IS SACRED. enter over a running turn still parks and still
// only parks. A person who has learned that their sentence waits has learned
// something that is still true, and nothing in this file can be reached by the
// key they already press.

// bargeKey is the chord, and it is named ONCE: the hint under the box, the key
// router and the manual's own sentence are all this constant or a quotation of
// it (spellout.go states the same rule about its own).
//
// Shift+enter opens a line in every message box. Adding ctrl preserves a
// distinct stop-and-send gesture without taking the ordinary newline chord.
// Ctrl+enter keeps its standing-order meaning, and alt+enter remains the
// conversation's alternate newline key and home's task composer key.
//
// ── THE CAPABILITY LAW, AND HOW THIS BUILD CAN ACTUALLY ANSWER IT ───────────
//
// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN, and absent has to
// include the advertisement: a dim line naming a chord this terminal will never
// deliver is worse than silence, because the person presses it, nothing
// happens, and they learn the surface lies.
//
// ctrl+shift+enter reaches a program only where the terminal disambiguates it from a
// bare enter — the kitty keyboard protocol, or xterm's modifyOtherKeys, or
// win32-input. Bubble Tea v2 asks for the first of those on every frame
// (its cursed_renderer.go writes ansi.KittyKeyboard with the disambiguation
// flag whether or not a View requests enhancements) and hands the terminal's
// answer back as a tea.KeyboardEnhancementsMsg. So this surface does not have
// to GUESS: [app.keysDisambiguated] is that answer, and it gates the hint and
// the key together ([app.bargeOffered]).
//
// The one direction it errs in is the safe one. A terminal that speaks
// modifyOtherKeys but not the kitty protocol never sends that reply, so the
// chord WORKS there and is never advertised — a feature quietly present rather
// than a hint that lies. The reverse cannot happen: a terminal that answered
// the query can spell the chord.
const bargeKey = "ctrl+shift+enter"

// bargeSendWord is what this gesture does in the running-turn hint. It belongs
// to ctrl+shift+enter alone: esc stops and clears waiting queues, while this chord
// deliberately preserves the draft it just parked so the stream close sends it.
const bargeSendWord = "stops and sends"

// bargeOffered reports whether the chord should be named — which is exactly
// whether it would DO anything if it were pressed right now.
//
// A HINT MAY ONLY NAME A KEY THAT WORKS (render.go's [app.hintWord] states the
// whole law), so this one predicate is both the advertisement's condition and
// the key's guard, and the two cannot come apart. It is spellout.go's shape for
// spellout.go's reason.
func (a *app) bargeOffered() bool {
	// THE TERMINAL HAS TO BE ABLE TO SAY IT. This is the capability law's whole
	// weight and it is asked first, because it is the one condition under which
	// this feature does not exist at all.
	if !a.keysDisambiguated {
		return false
	}
	if a.state != stateWorking {
		// Nothing to stop. Plain enter already sends at rest, so a second chord
		// meaning the same thing would be a key that teaches a person a gesture
		// they do not need.
		return false
	}
	// A FULL TRAY IS A MESSAGE, on [app.enterLine]'s own law: an empty box with
	// a picture attached is not an empty message. The two tests match the ones
	// the send road makes, so the hint cannot offer a chord the road refuses.
	if a.input.empty() && len(a.chips) == 0 {
		return false
	}
	// AND IT IS ABSENT WHEREVER THE BOX IS NOT THE CONVERSATION'S. In a room the
	// draft steers a NODE and enter sends it there and then — there is no queue
	// to jump and no turn of this conversation's that a person in that page
	// meant to stop (room.go). Copy mode and the rewind have taken the keyboard
	// outright, and the rail holds it while the roster is up. Every one of these
	// is read above the plain switch in [app.key], so the guard is here for the
	// HINT's sake as much as the key's.
	return !a.roomOpen() && !a.copy.on && !a.rew.on && !a.railHold
}

// bargeIn is the chord: the draft goes, and the turn stops.
//
// THE ORDER IS PARK-THEN-STOP AND IT MATTERS. [app.enterLine] is called while
// the turn is still open, so [app.parking] is true and the sentence is PARKED
// rather than sent — which is what puts it on the queue the stream's close
// drains. Interrupting first would leave a draft being sent into a turn that is
// already ending, which is the race this gesture must not have.
//
// AND IT ONLY STOPS SOMETHING IF IT SAID SOMETHING. The park queue growing is
// the one honest signal that the road took a message rather than doing one of
// the dozen other things enter does — a slash command, a tag's own door, a
// refusal, a tool row opened, nothing at all. A chord that stopped a running
// turn on its way to opening a fold would be the worst kind of surprise: an
// irreversible act as a side effect of a navigational one.
func (a *app) bargeIn() tea.Cmd {
	if !a.bargeOffered() {
		// THE KEY IS ABSENT WHEREVER IT CANNOT WORK, and absent means it does
		// nothing at all rather than saying it cannot (spellout.go's [app.spellKey]
		// states it). It is safe to swallow rather than fall through because
		// `ctrl+shift+enter` carries no text — ultraviolet's key table gives KeyEnter
		// the CR rune, which is not printable, so its Text is empty and the
		// bottom of input.go's router would have done nothing with it anyway.
		return nil
	}
	waiting := len(a.parks)
	// The mark is deliberately not passed. ctrl+enter is the gesture that means
	// "keep this true" and this one means "instead of that" — a chord that did
	// both would be one keystroke making two decisions (standmark.go).
	cmd := a.enterLine(false)
	if len(a.parks) == waiting {
		// The road did something other than park: a command ran, a door took the
		// words, a refusal was noted, or there was nothing to say. The turn is
		// left alone, which is the whole of the guard above said again.
		return cmd
	}
	a.interruptForBarge()
	return cmd
}
