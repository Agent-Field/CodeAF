package tui3

import "strings"

// THE TAB IS THE OUTERMOST ROW OF THE RAIL.
//
// A terminal title is read exactly when nobody is looking at the window — in a
// tab bar, the cmd-tab switcher, a tmux status line — so it answers the two
// questions a glance from outside can use and nothing else: which one is this,
// and does it need me. Which one is the project and the conversation's own
// name; does it need me is one glyph, and it is the home rail's glyph, because
// the tab is that rail's outermost row — the one that still shows after the
// window itself is put away.
//
// What it deliberately is not is a status line. No spinner, no model, no cost,
// no elapsed time: each of those would move the one piece of chrome a person
// cannot scroll away from, to say something that is not worth a glance from
// another workspace. The ordinary state — nothing waiting, nothing newly
// landed — is the bare name, which is the emptiness law applied to the space
// outside the frame. And because the title only says names and one glyph, it
// changes when a fact changes, never on a clock: Bubble Tea's renderer
// compares each declared title with the last one and emits OSC 2 only when it
// moved, so an idle surface repainting thirty times a second sends zero bytes
// down this channel.

// windowTitleMax bounds the title in runes. A tab shows twenty-odd characters
// and a window bar under a hundred; past that the bytes are pure cost down a
// channel that exists to say one line. The glyph and the project come first
// because tabs truncate from the right — the signal survives every width the
// conversation's name does not.
const windowTitleMax = 80

// The tab's two glyphs are the rail's own ([homeAskGlyph], [glyphDone]), each
// with the same ASCII stand-in the linear tier uses inside the frame. There is
// no running glyph and no idle glyph on purpose: "moving" and "at rest" are
// what a person assumes of a window they walked away from, and a tab that
// flipped a dot on every turn would be weather where the rail promised news.
const (
	titleAskGlyph = homeAskGlyph
	titleAskASCII = homeAskASCII

	titleDoneGlyph = glyphDone
	titleDoneASCII = glyphDoneASCII
)

// windowTitle is the one line this surface says to the space outside its own
// frame. Shape: `▲ project · conversation name`, each part dropped when it is
// not known — an untitled conversation is just the project, a conversation
// with no workspace is just its name, and a surface that knows neither says
// the product's name rather than nothing, because an empty title makes the
// renderer keep whatever the shell left there, which could be a previous
// program still claiming the tab.
//
// The glyph outranks the tick: a question that is waiting is the next thing
// anyone does here, and something having landed is worth knowing only after
// nothing is blocked. Waiting is asked process-wide — the drawn conversation's
// question ([app.asking]) or any kept one's ([app.waitingCount], the same
// predicate home's rung and the banner read) — because the tab stands for the
// whole terminal, not for the one conversation it happens to be drawing.
// attentionPick is the glyph tier: the drawn shape, or its plain stand-in on a
// surface being read aloud. It lived with the two attention strips until the
// switcher replaced them, and it survives here because the window's own title is
// the one other place a state mark is chosen (styles.go's [glyphRunASCII] holds
// the law).
func attentionPick(ascii bool, glyph, plain string) string {
	if ascii {
		return plain
	}
	return glyph
}

func (a *app) windowTitle() string {
	name := a.sessionName()
	switch {
	case a.place == "":
	case name == "":
		name = a.place
	default:
		name = a.place + " · " + name
	}
	if name == "" {
		name = product
	}
	switch {
	case a.asking() || a.waitingCount() > 0:
		name = attentionPick(a.linear, titleAskGlyph, titleAskASCII) + " " + name
	case a.landedAway:
		name = attentionPick(a.linear, titleDoneGlyph, titleDoneASCII) + " " + name
	}
	// The same payload rule the notification obeys (notify.go's [oscSafe]): a
	// conversation is free to be NAMED after an escape sequence, and the name
	// still may not carry one into an OSC string.
	safe := oscSafe(name)
	if runes := []rune(safe); len(runes) > windowTitleMax {
		safe = strings.TrimSpace(string(runes[:windowTitleMax]))
	}
	return safe
}
