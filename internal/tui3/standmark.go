package tui3

import (
	"context"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
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
// So there is a second road, and it is deterministic. A sentence handed over
// with `/standing <words>` reaches the model with an instruction saying so — it
// is shaped into a proposal or it is refused in one line, and it is never done
// as one-off work (internal/session's standing_mark.go). The card that comes
// back is the ordinary ratification card; nothing about the yes changes.
//
// THIS ROAD USED TO HAVE A CHORD TOO. `ctrl+enter` marked the draft from
// 2026-09-15 until 2026-09-30, when the queue took the chord over
// (followup.go): queueing is a gesture a person makes many times in a session
// and the mark is one they make a few times in a life, and the chord's other
// claimant — a terminal that cannot tell ctrl+enter from a plain enter — could
// not send the mark at all. The typed form above works on every terminal and
// every reach, and the box still teaches it: while the draft looks
// standing-shaped, the hint slot says the command ([app.standSayOffered]).

// The sentences this gesture says. The hint is quoted in
// internal/manual/chat/standing-orders.md exactly as it is spelled here.
var (
	// standSayHint is the hint slot's line while the draft looks standing-
	// shaped. It names the typed door, which works on every terminal — the
	// chord this slot used to teach is queueing's now (followup.go), and a hint
	// may only name a key that works (render.go's [app.hintWord]).
	standSayHint = "/standing keeps this true"
	// standMarkNowhere is the refusal where this build has no ambient side at
	// all, said as the absence it is.
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

// standSayOffered reports whether the hint slot should name the typed door —
// which is exactly whether the command would DO anything if it were run right
// now.
//
// A HINT MAY ONLY NAME A KEY THAT WORKS (render.go's [app.hintWord] states the
// whole law). Every condition [app.standingSay] refuses on is a condition this
// answers no to, and the two lists are the same list read from both ends.
func (a *app) standSayOffered() bool {
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

// standingSay is `/standing <words>`: the words go through the SAME deliberate
// door the chord used to open (app.go's slash), and the margin's `+ /standing`
// row is what puts the command in the box for somebody who has never typed it
// (margin.go).
//
// THE COMMAND IS THE EXPLICIT MARKED ROAD, which is the whole of why the
// argument form exists: a sentence handed over this way is shaped into a card
// or refused in one line, and it is never carried out as one-off work
// (internal/session's standing_mark.go). Falling back to [app.submit] here would
// be the failure the marked door was built to end, arriving through a door that
// promises the opposite.
//
// AND IT WAITS ITS TURN LIKE ANY OTHER SENTENCE. A command is said to this
// surface at once, but these words are said to the MODEL: typed over a running
// answer they are parked with the mark on them, exactly as the chord's were
// (park.go).
func (a *app) standingSay(text string) tea.Cmd {
	return a.standingSayShown(text, text, nil, nil)
}

// standingSayShown is [app.standingSay] for a sentence whose transcript line
// differs from its words: shownPlain is the demoted tags as offsets into shown,
// and textPlain the same tags as offsets into text. Both are needed because a
// parked message keeps only its words, so it is textPlain that waits with it.
func (a *app) standingSayShown(text, shown string, shownPlain, textPlain []segment) tea.Cmd {
	a.noticeEvent(eventStandingOpened)
	if !a.standingHere() {
		// The same absence the chord answers with, said in the same words: there
		// is nothing here that could hold one.
		a.note(standMarkNowhere)
		return nil
	}
	if a.parking() {
		return a.park(text, true, textPlain)
	}
	return a.submitStandingShown(text, shown, shownPlain)
}

// submitStanding sends one marked message. It is [app.submit] with the other
// door on the seam, and it goes through a command for that function's reason:
// the call talks to a lock and possibly a provider, and the Update loop is not a
// place to wait.
func (a *app) submitStanding(text string) tea.Cmd {
	return a.submitStandingShown(text, text, nil)
}

func (a *app) submitStandingShown(text, shown string, plain []segment) tea.Cmd {
	agent, ctx := a.agent, a.ctx
	return a.submittingShown(text, shown, plain, standingStart(agent, ctx, text))
}

// standingStart is the marked-message call shared by the front and keeper.
// The mark is a property of the message, not of which conversation is drawn.
func standingStart(agent Agent, ctx context.Context, text string) func() (<-chan session.Event, error) {
	return func() (<-chan session.Event, error) { return agent.SubmitStanding(ctx, text) }
}
