package tui3

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE DELIBERATE GESTURE: a send that means "keep this true".
//
// Everything else about standing orders leans on the model NOTICING that an
// ordinary sentence was a standing one (internal/session's tools_standing.go).
// When that works it is the best thing on this surface, and when it fails it
// fails silently: the sentence is carried out once as ordinary work, the person
// watches something happen and believes a rule was made, and nothing is
// holding. There is no error, no card and nothing on the screen that reads any
// differently from a rule that WAS made.
//
// So there is a second road, and it is deterministic. A modifier-send on the
// draft marks the sentence, and a marked sentence reaches the model with an
// instruction saying so — it is shaped into a proposal or it is refused in one
// line, and it is never done as one-off work (internal/session's
// standing_mark.go). The card that comes back is the ordinary ratification card;
// nothing about the yes changes.
//
// AND THE BOX TEACHES THE GESTURE, because a chord nobody knows about is a
// capability nobody has. While the draft looks standing-shaped, the hint slot
// under the box says the chord ([app.standMarkOffered]).

// standMarkKey is the chord, and it is named ONCE: the hint under the box, the
// key router and the manual's own sentence are all this constant or a quotation
// of it.
//
// WHY THIS ONE AND NOT alt+enter. In a conversation alt+enter is already
// spoken for — it and ctrl+j are the two spellings of "open a line" in the draft
// (input.go), which is the gesture a paste and a paragraph both need — so
// binding it here would take a key people use to write the very sentences this
// gesture is about. ctrl+enter is the only free modifier-send left, it is
// unmistakably a SEND rather than a letter, and it is already this build's chord
// for "a send that means something other than the ordinary one": home's box
// binds it to `ask here` (home.go). One hand shape, one kind of meaning, one
// name per surface.
//
// Its limit is the same one home's carries and the manual states: a terminal
// that cannot tell ctrl+enter from a plain enter never sends it. Saying the
// sentence in words is the road that works everywhere, and it is the road the
// recognition half was built for.
const standMarkKey = "ctrl+enter"

// The two sentences this gesture says, and both are quoted in
// internal/manual/chat/standing-orders.md exactly as they are spelled here.
var (
	// standMarkHint is the hint slot's line while the draft looks standing-
	// shaped. It is the chord and what the chord promises, in the slot's own
	// grammar — a key, then what it does (render.go's [app.hintWord]).
	standMarkHint = standMarkKey + " keeps this true"
	// standMarkWordsOnly is the refusal when the box is holding something the
	// gesture is not about. Nothing is sent and the draft is untouched, which
	// is the point of refusing rather than falling back to an ordinary send:
	// silently doing the sentence as work is the failure this whole file exists
	// to end.
	standMarkWordsOnly = standMarkKey + " keeps a sentence true — take the pictures or the shape of work off first"
	// standMarkNowhere is the same refusal where this build has no ambient side
	// at all, said as the absence it is.
	standMarkNowhere = "nothing here can hold a standing order"
)

// standMarkShapes is the LOCAL, CHEAP look at a draft that decides whether the
// hint is worth drawing. It is a courtesy and never the doctrine: the real
// recognition is the model's, over the whole sentence, in `stand`'s own
// description — this list only has to catch enough of what people type to teach
// them the chord exists.
//
// SO FALSE NEGATIVES ARE FREE AND THE LIST STAYS SHORT. Every entry here is a
// phrase that is almost never anything but a condition; a wider list would put a
// dim line under half the drafts on this surface, and a hint a person learns to
// ignore is worse than one they never saw. It is matched at a word boundary for
// that reason too — "however" is not "ever", and "delivery" is not "every".
var standMarkShapes = []string{
	"always ",
	"never ",
	"every ",
	"whenever ",
	"each time",
	"from now on",
	"remind me",
	"keep an eye",
}

// standMarkPaired is the one shape that needs a companion, because "make sure"
// on its own is how people ask for anything at all — "make sure it compiles" is
// a thing to do now. Beside a never or an always it is a condition.
const standMarkPaired = "make sure"

// standMarkScan is how far into a draft the shapes are looked for. A standing
// sentence says what it is in its first few words; past this the draft is a
// paste, and building a four-thousand-line string twice a frame to look for
// "never " in it is a hundred kilobytes of garbage per frame spent on a line
// nobody wanted (input.go's [editor.empty] states the same law about the same
// box).
const standMarkScan = 160

// looksStanding reports whether a draft is worth offering the chord for.
func looksStanding(draft string) bool {
	head := strings.ToLower(draft)
	for _, shape := range standMarkShapes {
		if standMarkAt(head, shape) {
			return true
		}
	}
	if !standMarkAt(head, standMarkPaired) {
		return false
	}
	return standMarkAt(head, "never") || standMarkAt(head, "always")
}

// standMarkAt is "this phrase appears, at the start of a word". The draft is
// already lower-cased.
func standMarkAt(head, shape string) bool {
	for at := 0; ; {
		found := strings.Index(head[at:], shape)
		if found < 0 {
			return false
		}
		found += at
		if found == 0 || !unicode.IsLetter(rune(head[found-1])) {
			return true
		}
		at = found + 1
	}
}

// standMarkOffered reports whether the hint slot should name the chord — which
// is exactly whether the chord would DO anything if it were pressed right now.
//
// A HINT MAY ONLY NAME A KEY THAT WORKS (render.go's [app.hintWord] states the
// whole law). So every condition [app.enterStanding] refuses on is a condition
// this answers no to, and the two lists are the same list read from both ends.
func (a *app) standMarkOffered() bool {
	if a.input.empty() || !a.standingHere() {
		return false
	}
	if a.harnChip != "" || len(a.chips) > 0 {
		return false
	}
	head := a.standMarkHead()
	if strings.HasPrefix(strings.TrimSpace(head), "/") {
		return false
	}
	return looksStanding(head)
}

// standMarkHead is as much of the draft as [standMarkScan] looks at, and it is
// taken off the editor's runes rather than through [editor.String] for that
// constant's own reason: this question is asked on every frame the box is not
// empty, and a paste is what people put in this box.
func (a *app) standMarkHead() string {
	value := a.input.value
	if len(value) > standMarkScan {
		value = value[:standMarkScan]
	}
	return string(value)
}

// enterStanding is the chord: enter, with the sentence MARKED.
//
// It is [app.enter]'s own road with one bit set on it, rather than a second
// send of its own, because everything a message does to this surface — the
// transcript line, the turn number, the recall history, the draft file — is the
// same whichever way it was sent. What differs is the door at the end of it
// (input.go's [app.enterLine]).
//
// THE THREE REFUSALS DO NOT FALL BACK TO AN ORDINARY SEND. A gesture whose
// failure mode is "the thing you were trying to avoid" is not a gesture, so a
// draft this cannot mark stays in the box with one line saying why.
func (a *app) enterStanding() tea.Cmd {
	line := strings.TrimSpace(a.input.String())
	if line == "" {
		// The emptiness law: there is no sentence to keep true, and a note about
		// a key somebody pressed over an empty box would be noise.
		return nil
	}
	if !a.standingHere() {
		a.note(standMarkNowhere)
		return nil
	}
	if strings.HasPrefix(line, "/") {
		// A COMMAND IS SAID TO THIS SURFACE AND NOT TO THE MODEL, so there is no
		// sentence here to keep true and nothing to refuse: /standing marked
		// standing is /standing (input.go's own reading of a slash).
		return a.enterLine(false)
	}
	if a.harnChip != "" || len(a.chips) > 0 {
		a.note(standMarkWordsOnly)
		return nil
	}
	return a.enterLine(true)
}

// standingSay is `/standing <words>`: the words go through the SAME deliberate
// door the chord opens (app.go's slash), and the margin's `+ /standing` row is
// what puts the command in the box for somebody who has never typed it
// (margin.go).
//
// IT IS THE CHORD'S ROAD AND NOT THE ORDINARY SEND, which is the whole of why
// the argument form exists: a sentence handed over this way is shaped into a
// card or refused in one line, and it is never carried out as one-off work
// (internal/session's standing_mark.go). Falling back to [app.submit] here would
// be the failure the marked door was built to end, arriving through a door that
// promises the opposite.
//
// AND IT WAITS ITS TURN LIKE ANY OTHER SENTENCE. A command is said to this
// surface at once, but these words are said to the MODEL: typed over a running
// answer they are parked with the mark on them, exactly as the chord's are
// (park.go).
func (a *app) standingSay(text string) tea.Cmd {
	return a.standingSayShown(text, text)
}

func (a *app) standingSayShown(text, shown string) tea.Cmd {
	if !a.standingHere() {
		// The same absence the chord answers with, said in the same words: there
		// is nothing here that could hold one.
		a.note(standMarkNowhere)
		return nil
	}
	if a.parking() {
		return a.park(text, true)
	}
	return a.submitStandingShown(text, shown)
}

// submitStanding sends one marked message. It is [app.submit] with the other
// door on the seam, and it goes through a command for that function's reason:
// the call talks to a lock and possibly a provider, and the Update loop is not a
// place to wait.
func (a *app) submitStanding(text string) tea.Cmd {
	return a.submitStandingShown(text, text)
}

func (a *app) submitStandingShown(text, shown string) tea.Cmd {
	agent, ctx := a.agent, a.ctx
	return a.submittingShown(text, shown, func() (<-chan session.Event, error) { return agent.SubmitStanding(ctx, text) })
}
