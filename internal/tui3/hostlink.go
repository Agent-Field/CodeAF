package tui3

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE LINK TO THE MACHINE THE CONVERSATION IS ON ──────────────────────────
//
// host.go says the connection is shown as the PLACE and nowhere else, and that
// is still true of a connection that is working. This file is the other half:
// what the surface says when the link between here and there is not working, or
// has just come back and brought news with it.
//
// Do not confuse this with link.go, which is about the terminal READING this
// surface being on the far side of an ssh — a different link, a different fact,
// and one that only ever changes how often a frame is drawn. This one is the
// pipe `--host` opens to the engine, and internal/remote redials it for up to
// five minutes without telling anybody (its redial.go).
//
// THREE THINGS CROSS AND EACH IS A DIFFERENT KIND OF SENTENCE:
//
//	the note      a CONDITION, true right now and false a second later, so it
//	              is a status-line segment and nothing else. It is asked on the
//	              frame, so it must answer from what is already held.
//	the notice    NEWS, once — the engine did not keep the turn, or it came back
//	              with a different conversation open. It is an ordinary note in
//	              the transcript, which is where every one-off sentence this
//	              surface says already lands ([Options.Notice] takes the same
//	              road on the first frame).
//	a held card   a QUESTION that was raised while nobody was here. It is not a
//	              new kind of card: it is the card this surface would have drawn
//	              live, replayed through the same door.
//
// AND ALL THREE ARE ABSENT ON A LOCAL SESSION, because there is no link to have
// anything to say about — a capability that cannot work is absent, not broken.

// LinkSeam is what a door that opened this conversation over a connection can
// tell the surface about that connection.
//
// It is functions rather than a handle for [StandingSeam]'s reason: the door
// owns what the link IS and this package owns what a person reads, and a test
// of "what does the status line say while it is reconnecting" should be a
// string handed over rather than a pipe torn in half.
//
// The zero value is a surface with no connection to report — every local
// session, every test, and every headless frame. Nothing half-works there: no
// segment is drawn, no notice is looked for, and no question is asked about.
type LinkSeam struct {
	// Note is the quiet true sentence about the link right now, and the empty
	// string whenever there is nothing to say — which is almost always. It
	// reads `reconnecting to devbox — trying for up to 5 minutes` while a
	// dropped link is being redialled (internal/remote's [Client.LinkNote]).
	//
	// IT IS ASKED ON THE DRAW PATH AND MUST ANSWER IMMEDIATELY. The status row
	// is laid out twice per frame and the frame turns thirty times a second, so
	// a Note that reached for the wire would be a terminal that stopped
	// repainting for as long as the far machine took to answer — on a link that
	// had just died, ten seconds per frame ([hostStanding] in cmd/aforge states
	// the whole of that law about its own seam, and keeps a cache to obey it).
	//
	// This one needs no cache, and that is a fact about the client rather than
	// a hope: the sentence is built from a field the redial loop has already
	// set, under the mutex that guards it, with nothing on the wire behind it.
	// A door that wired something slower here would be wiring a different law.
	Note func() string

	// Notice is one sentence to show once and then forget, and the empty string
	// when there is none.
	//
	// IT DRAINS, so it is read from exactly one place ([app.takeLinkNotice],
	// called at the top of [app.Update] and nowhere else). A second reader
	// would not show the sentence twice — it would swallow it, because whichever
	// of the two asked first is the only one that gets it.
	Notice func() string

	// Held is the questions this conversation raised while nobody was attached,
	// each with the event that raised it and how long it has been waiting. It
	// travels on the wire and may take a moment, so it is asked off the loop
	// ([app.askHeld]) and never on the frame.
	//
	// AN ERROR IS AN ERROR AND NOT AN EMPTY LIST, which is the shape
	// internal/remote hands over: "nothing is waiting" and "the far end did not
	// answer" are different facts, and a surface that drew the second as the
	// first would be quietly telling a person there is nothing to answer.
	Held func() ([]HeldQuestion, error)

	// Driving is who holds the keyboard on this conversation, as the machine
	// running it last said. Nil is a surface with no room to share.
	//
	// IT IS ASKED ON THE DRAW PATH and must answer from what is already held,
	// under [LinkSeam.Note]'s law and for the same reason: the answer is a field
	// a frame already delivered, with nothing on the wire behind it.
	Driving func() Driving

	// DrivingChanged is closed the next time that answer moves.
	//
	// IT IS THE ONE ASYNC FACT THIS SURFACE HAS NO OTHER ROAD FOR. A hand-over
	// happens because somebody attached on ANOTHER MACHINE — no keystroke, no
	// stream event — and a surface waiting for something to happen here would
	// keep a composer on screen until the person next touched it. So it is a
	// channel a command can block on (watching.go's [app.watchDriving]), the
	// way a turn's events are.
	DrivingChanged func() <-chan struct{}

	// Take asks for the keyboard back. One round trip, never a reconnect, and
	// the far end does not refuse it — a person pressing enter on their own work
	// has said the only thing that needs saying. Its error is a link that is
	// down or an engine that did not answer, and it is SHOWN.
	Take func() error
}

// HeldQuestion is one card raised while no window was attached to the
// conversation, as the far machine kept it.
//
// It is this package's own type and not internal/remote's, for [StandingSeam]'s
// reason again — this package does not import the wire — and it is three of the
// wire's four fields with the event already unwrapped.
//
// THE FOURTH IS THE STREAM IT BELONGS TO, AND IT IS DELIBERATELY NOT HERE. The
// wire carries it so a surface can put a card back where it was in a room with
// several turns in it; this surface has one conversation and one place a card
// goes — over the input, in the order the questions were raised — so a field
// for it would be a field nobody reads.
type HeldQuestion struct {
	// Kind is which door answers this: "consent", "standing", "harness",
	// "connect". A KIND THIS BUILD DOES NOT KNOW IS SKIPPED, which is the
	// wire's stated contract: a newer engine holding a question this surface
	// cannot draw must leave that question waiting for a build that can, rather
	// than be drawn as something it is not or refuse the conversation.
	Kind string
	// Event is the event that raised it, so the card drawn is the card that
	// would have been drawn live.
	Event session.Event
	// Since is when it was raised, which is the one thing a live card never has
	// to say: a permission question from four hours ago is a different thing to
	// answer than one from four seconds ago.
	Since time.Time
}

// The four kinds this build draws. They are spelled here rather than imported
// because this package cannot see internal/remote, and they are a closed list
// on purpose — see [app.replayHeld] for what an unrecognised one does.
const (
	heldConsent  = "consent"
	heldStanding = "standing"
	heldHarness  = "harness"
	heldConnect  = "connect"
)

// ── 1. the note, on the status line ─────────────────────────────────────────

// linkSegment is the status line's segment for the link:
//
//	reconnecting to devbox — trying for up to 5 minutes
//
// NOTHING AT ALL WHEN THERE IS NOTHING, which is the emptiness law and also
// exactly what host.go's header has always promised about a remote session:
// there is no badge, no icon and no "connected" word. A working link says
// nothing, and this segment exists only in the seconds where that stops being
// true — which is the test a good indicator passes.
//
// THE SENTENCE IS THE CLIENT'S OWN AND IS NOT REBUILT HERE. It names the
// machine and the span it will keep trying for, both of which are derived over
// there from the window the redialling actually uses (internal/remote's
// roamSpan): a second spelling on this side would be a number that drifts the
// first time somebody changes that window.
func (a *app) linkSegment() string {
	if a.link.Note == nil {
		return ""
	}
	return a.link.Note()
}

// linkNoting reports whether the link has something to say at this instant. It
// is the paint clock's question — a frame drawn while a redial is running has
// to be followed by another one, or the segment would stay on the screen after
// the link came back and vanish only when something else happened to repaint.
func (a *app) linkNoting() bool { return a.linkSegment() != "" }

// ── 2. the notice, once ─────────────────────────────────────────────────────

// takeLinkNotice drains the one-shot sentence and puts it in the transcript.
//
// THIS IS THE ONLY CALLER OF [LinkSeam.Notice] AND IT MUST STAY THE ONLY ONE.
// The seam forgets what it hands over, so a second reader is not a second
// showing of the sentence — it is the sentence going to whichever reader asked
// first and never reaching the screen at all. It is called at the top of
// [app.Update], which is the one place this surface sees every message: a
// keystroke, a stream event, a frame tick. A drain on the frame clock alone
// would hold the news until the next turn on a window sitting idle, and idle is
// exactly when a link is most likely to have dropped.
//
// It is an ordinary note because it is an ordinary one-off sentence, and the
// surface has one way of saying those (app.go's [app.note]) — the same road the
// door's own entry notice takes on the first frame.
func (a *app) takeLinkNotice() {
	if a.link.Notice == nil {
		return
	}
	if said := a.link.Notice(); said != "" {
		a.note(said)
	}
}

// ── 3. the questions that waited ────────────────────────────────────────────

// heldMsg is one reading of the far machine's waiting room, on its way back to
// the loop.
type heldMsg struct {
	questions []HeldQuestion
	err       error
}

// askHeld asks what was raised while nobody was here, off the loop.
//
// IT IS ASKED WHEN A CONVERSATION IS TAKEN UP AND NOT ON A BEAT. A question
// raised while this window IS attached arrives on the turn's own stream like
// every other event, and a redial replays the gap onto that same stream
// (internal/remote's redial.go), so the only moment this list holds anything
// the surface has not already seen is the moment it arrives somewhere: the
// first frame, and every /new or /resume after it. Polling for it would be a
// wire call a second asking a question whose answer changes when a person opens
// a conversation.
func (a *app) askHeld() tea.Cmd {
	ask := a.link.Held
	if ask == nil {
		return nil
	}
	return func() tea.Msg {
		held, err := ask()
		return heldMsg{questions: held, err: err}
	}
}

// replayHeld draws the questions that were waiting, oldest first.
//
// EACH ONE GOES THROUGH THE DOOR ITS LIVE TWIN GOES THROUGH ([app.event]), and
// that is the whole design: this surface already knows how to draw a permission
// question, a reminder asking to stand, a harness offer and an account request,
// and every one of those answers back through a call keyed by the card's own id
// rather than by the stream it came down. So a held card is not a second kind
// of card — it is the same card, arriving by a different road, and the key that
// answers it is the key that always answered it.
//
// A KIND THIS BUILD DOES NOT KNOW IS SKIPPED AND NOT GUESSED AT. The far
// machine may be a newer build holding a question this one has never drawn; a
// fallback rendering would be a card a person can see and cannot answer, which
// is worse than one they were never shown. Skipping leaves it waiting, exactly
// as it was, for a build that knows it.
//
// A FAILED ASK SAYS NOTHING. The two ways it fails are a link that is down —
// which the status line is already saying in its own words — and an engine with
// no waiting room at all, and neither of those is the news "there is nothing to
// answer". Inventing a sentence about a question about questions would be
// machinery talking.
func (a *app) replayHeld(msg heldMsg) tea.Cmd {
	if msg.err != nil {
		return nil
	}
	var cmds []tea.Cmd
	for _, q := range msg.questions {
		switch q.Kind {
		case heldConsent, heldStanding, heldHarness, heldConnect:
		default:
			continue
		}
		// HOW LONG IT WAITED IS SAID BEFORE THE CARD AND NOT ON IT. The card is
		// the same card it would have been live, and a line of its own is where
		// this surface puts everything that is true ABOUT a thing rather than
		// part of it. A question raised moments ago says nothing at all, by the
		// emptiness law — "waiting 0 minutes" is a fact nobody asked for.
		if word := waitedWord(a.now().Sub(q.Since)); word != "" {
			a.note("this question has been waiting " + word)
		}
		cmds = append(cmds, a.event(q.Event))
	}
	return tea.Batch(cmds...)
}

// heldFloor is how long a question has to have been waiting before the wait is
// worth a line. Below it the answer is "you were only just away", which is not
// news and is what the emptiness law asks a surface to draw as nothing.
const heldFloor = time.Minute

// waitedWord is a wait as a person would say it out loud: "3 minutes",
// "4 hours", "2 days". It is prose in the transcript rather than a status-line
// segment, so it is spelled in words and not in the `4h` this surface uses
// where a column is scarce (home.go's [sinceAt]).
func waitedWord(d time.Duration) string {
	switch {
	case d < heldFloor:
		return ""
	case d < time.Hour:
		n := int(d / time.Minute)
		return itoa(n) + plural(" minute", n)
	case d < 24*time.Hour:
		n := int(d / time.Hour)
		return itoa(n) + plural(" hour", n)
	default:
		n := int(d / (24 * time.Hour))
		return itoa(n) + plural(" day", n)
	}
}
