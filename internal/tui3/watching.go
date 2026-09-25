package tui3

// ── WATCHING A CONVERSATION SOMEBODY ELSE IS TYPING INTO ────────────────────
//
// More than one window can be attached to one conversation on a far machine: a
// desk and a laptop, two rooms, or a person and the terminal they forgot to
// close. Until now every one of them had a composer, so two windows raced and
// neither screen said the other existed.
//
// THE KEYBOARD FOLLOWS THE NEWEST WINDOW, AND NOTHING IS EVER THROWN AWAY. The
// engine decides who holds it (internal/remote's driver.go) and this file is the
// half a person sees:
//
//   - The window that does not hold it becomes a WATCHER. Its transcript keeps
//     arriving live — that is the whole reason it is not simply closed, because
//     a forgotten window still showing the work is a feature — and where its
//     composer was, one dim line stands instead.
//   - `enter` takes the keyboard back, in one round trip on the connection that
//     was already open. The other window becomes the watcher in the same
//     instant, told by the engine rather than by discovering it on a keystroke.
//   - THE DRAFT IS KEPT. It is not cleared, not sent and not lost: the box is
//     not drawn, and the words are exactly where they were when the keyboard
//     comes back.
//
// WHY `enter` AND NOT THE FIRST KEYSTROKE. Taking the keyboard off another
// machine is a thing to mean, and the first keystroke is a thing a sleeve does.
// It is also a round trip: bound to a character key, the first letter of every
// sentence would race a wire call that can fail, and the letter would be the
// thing that got lost. `enter` is already this surface's "my turn to speak", and
// the line says so in the place a person is looking when they wonder.
//
// AND ALL OF IT IS ABSENT ON A LOCAL SESSION. There is no room to be a watcher
// in — a conversation open in a second window here is refused at the journal,
// with [sessionBusyWord] — so the seam is nil, [app.watching] is false, and not
// one row of this file is ever drawn.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// Driving is who holds the keyboard on this conversation, as the machine
// running it last said.
//
// It is this package's own type and not internal/remote's, for [LinkSeam]'s
// reason: the package that draws a screen does not compile against a protocol.
// The zero value — nobody driving, no machine named — is deliberately NOT what a
// surface with no connection holds; that one has a nil seam, and [app.watching]
// asks about the seam first.
type Driving struct {
	// Yours says this window is the one that can type. It is the ordinary case
	// and it draws nothing at all.
	Yours bool
	// Machine is the name of the machine whose window has the keyboard —
	// `spark`, `macbook` — and the empty string when the far end could not say.
	Machine string
	// Here says the window with the keyboard is another window on THIS machine,
	// which a person reads as one they forgot rather than as a machine they
	// walked away from. It is the case [homeHeldShort] already has a word for.
	Here bool
}

// watching reports that this window is attached to a conversation another window
// is typing into.
//
// IT IS ASKED ON THE DRAW PATH, twice a frame, so it must answer from what is
// already held and never from the wire — the same law [LinkSeam.Note] states,
// and the client obeys it for the same reason: the answer is a field a frame
// already delivered, under the mutex that guards it.
func (a *app) watching() bool {
	if a.link.Driving == nil {
		return false
	}
	return !a.link.Driving().Yours
}

// watchWord is the line that stands where the composer stands, and it is the
// whole of what a watcher is told:
//
//	typing from spark now · enter takes it back
//
// `another window` is what it says about a window on this same machine, because
// that is the word codeaf already uses at home for a conversation open somewhere
// else ([homeHeldWord]) and the two places a person meets this fact should sound
// like one program. A machine that sent no name gets the same word: it is the
// weaker claim of the two and the one that stays true either way.
//
// IT DOES NOT ADVERTISE THE DOOR HOME, and that is not an omission. The foot of
// the frame already says `space space home` in the hint slot ([homeDoorWord]),
// the gesture still works from here, and a second copy of it in this line would
// be the surface teaching one door in two places.
func (a *app) watchWord() string {
	where := "another window"
	if driving := a.link.Driving(); !driving.Here && driving.Machine != "" {
		where = driving.Machine
	}
	return "typing from " + where + " now"
}

// watchKeysWord is the key that ends it, on the right of the bar where every key
// on this surface is written.
const watchKeysWord = "enter takes it back"

// watchBar is what stands in the draft box's position while another window has
// the keyboard.
//
// It is the shape [app.rewindBar] already established for a mode that has taken
// the box: FACTS on the left, KEYS on the right, dim, one row, and on a frame
// too narrow for both the keys go and the fact stays — a key is recoverable by
// pressing it, and knowing where your conversation went is not recoverable by
// anything.
func (a *app) watchBar(width int) []string {
	if width < 1 {
		return []string{""}
	}
	left := a.watchWord()
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(watchKeysWord)
	if gap < 1 {
		return []string{a.pal.dim(fit(left, width))}
	}
	return []string{a.pal.dim(left) + strings.Repeat(" ", gap) + a.pal.dim(watchKeysWord)}
}

// ── the keys ────────────────────────────────────────────────────────────────

// watchKey is the narrow thing a watcher's keyboard does differently, and it is
// deliberately narrow: EVERYTHING THAT READS STILL WORKS. Scrolling, copy mode,
// the places, a permission card, ctrl+c — a watcher is a person watching their
// own work, not a guest, and all of those are handled above this in the router
// ([app.key]) or fall through it untouched.
//
// What it takes is the two things that only make sense with a composer: the
// send keys, which become the take-back, and a character typed into a box that
// is not on the frame.
//
// THE DOOR HOME IS COUNTED HERE RATHER THAN LET THROUGH, and that is worth the
// four lines it costs. [app.homeGesture] reads two consecutive spaces out of the
// BOX, and this register has no box on the frame — so letting the space fall
// through while swallowing the letters turned a typed sentence into a run of
// spaces and opened home on the first word with two in it. Driven over a real
// connection, `this should be swallowed` walked straight out of the
// conversation. The gesture is the same gesture; it is simply counted where the
// keys actually are, and any other key ends the run exactly as a letter in the
// box would.
func (a *app) watchKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "enter", standMarkKey:
		a.watchSpaces = 0
		return a.takeKeyboard(), true
	case "alt+enter", "ctrl+j":
		// A newline into a box nobody can see is nothing at all.
		a.watchSpaces = 0
		return nil, true
	}
	text := msg.Key().Text
	if text == "" {
		// Not a character: the arrows, the scroll keys, copy mode, the places.
		// They are not this register's business and they end the run.
		a.watchSpaces = 0
		return nil, false
	}
	if text != " " {
		a.watchSpaces = 0
		return nil, true
	}
	// A SPACE ONLY COUNTS OVER AN EMPTY DRAFT, which is the condition the
	// gesture has always had: a person with words already in the box meant a
	// space in their sentence, and this window is still holding those words.
	if !a.input.empty() {
		return nil, true
	}
	a.watchSpaces++
	if a.watchSpaces >= 2 && a.homeDoorOpen() {
		a.watchSpaces = 0
		return a.openHome(), true
	}
	return nil, true
}

// takeKeyboard asks the far machine for the keyboard back.
//
// IT IS ASKED OFF THE LOOP, because it is a round trip and the update loop must
// not block on one — and it needs nothing back: the engine answers the client,
// the client wakes [app.watchDriving], and the frame that follows draws a
// composer. A REFUSAL IS SHOWN AND NEVER SWALLOWED; the only ways this fails are
// a link that has dropped, which the status line is already saying in its own
// words, and an engine that will not answer, which is news.
func (a *app) takeKeyboard() tea.Cmd {
	take := a.link.Take
	if take == nil {
		return nil
	}
	return func() tea.Msg {
		if err := take(); err != nil {
			return drivingMsg{said: err.Error()}
		}
		return drivingMsg{}
	}
}

// ── the turns this window did not start ─────────────────────────────────────

// Following is a turn some other window on this conversation started, on its
// way to be drawn here.
type Following struct {
	// Said is the message that opened it, and the empty string when the
	// transcript this surface already read has that message in it — which is
	// the case for a turn that was running before this window arrived.
	Said string
	// Events is that turn, arriving, on the same kind of channel a submit here
	// answers with.
	Events <-chan session.Event
}

// followingMsg is one of them reaching the loop.
//
// IT CARRIES A GENERATION for the reason every other lane on this surface does
// (app.go's generation law), and it did not need one until a window could hold
// several conversations on several connections. This wait blocks on ONE
// connection's channel; a switch rebinds [app.link] to another one and leaves
// that wait parked on the old one, so a turn started in the conversation this
// window walked away from would arrive here and be adopted into the conversation
// now on screen — somebody else's reply, drawn under the wrong name.
//
// IT IS THE CONNECTION'S GENERATION AND NOT THE TURN'S ([app.convGen]). This
// wait used to carry [app.gen], which every turn bumps on its way in
// ([app.takeStream], [app.startFollow]) — so the first turn this window ran
// itself made the next wake read as a turn from a connection it had walked away
// from: discarded, and the wait never armed again. On a hosted conversation every
// turn the engine started on its own after that was invisible: the task card
// said done, the model's sentence about it went to the journal, and the screen
// showed nothing until the conversation was reopened.
type followingMsg struct {
	turn Following
	gen  int
}

// watchFollowing waits for the next turn started somewhere else.
//
// It is the pair of [app.watchDriving] and it exists for the same reason: the
// thing that happened happened on ANOTHER MACHINE. Without it a window that is
// not holding the keyboard would sit on a still frame while the work went on in
// front of somebody else — a window, but not one onto anything.
func (a *app) watchFollowing() tea.Cmd {
	follow := a.link.Follow
	if follow == nil {
		return nil
	}
	gen := a.convGen
	return func() tea.Msg {
		turn, ok := <-follow()
		if !ok {
			return nil
		}
		return followingMsg{turn: turn, gen: gen}
	}
}

// followTurn queues a hosted turn through the same admission path as a local
// wake. The previous stream may still be draining on this window even though
// the engine has started its next turn; dropping that arrival loses the answer.
func (a *app) followTurn(msg followingMsg) tea.Cmd {
	// A TURN FROM THE CONNECTION THIS WINDOW HAS SINCE WALKED AWAY FROM IS
	// DISCARDED, AND THE WAIT IS NOT RE-ARMED. The conversation it belongs to has
	// its own wait, armed when it came forward; re-arming here would leave two of
	// them on one channel. It is the CONNECTION's generation that decides, never
	// the turn's ([followingMsg] states the drop that reading the turn's caused).
	if msg.gen != a.convGen {
		return nil
	}
	next := a.watchFollowing()
	if msg.turn.Events == nil || msg.turn.Events == a.stream {
		return next
	}
	for _, pending := range a.follows {
		if pending.ch == msg.turn.Events {
			return next
		}
	}
	a.dismissWelcome()
	said := strings.TrimSpace(msg.turn.Said)
	a.follows = append(a.follows, queued{text: said, ch: msg.turn.Events, woken: said == ""})
	a.touch()
	return tea.Batch(a.startFollow(), next)
}

// ── learning that it moved, with nobody touching this keyboard ──────────────

// drivingMsg is the keyboard having moved, on its way back to the loop. said is
// a take-back that failed and has something to report, and gen is the
// conversation it is about ([followingMsg] holds the whole of why it is here).
//
// A TAKE-BACK CARRIES NO GENERATION and is always heard: it is the answer to a
// keystroke the person made a moment ago, and a refusal worth saying is worth
// saying wherever they now are.
type drivingMsg struct {
	said string
	gen  int
	lane bool
}

// watchDriving waits for the next hand-over and wakes the surface for it.
//
// THIS IS THE ONE THING ABOUT A WATCHER THAT NOTHING LOCAL NEEDS: the fact
// changes because something happened on ANOTHER MACHINE, with no keystroke and
// no stream event behind it, and a surface drawing on its own clock would keep a
// composer on screen until the person next touched it. So the wait is a command
// that blocks on the client's own channel, exactly as a turn's events do
// (app.go's [waitEvent]), and it re-arms itself on every arrival.
//
// A SEAM THAT IS NOT WIRED ARMS NOTHING, which is every local session.
func (a *app) watchDriving() tea.Cmd {
	changed := a.link.DrivingChanged
	if changed == nil {
		return nil
	}
	gen := a.convGen
	return func() tea.Msg {
		<-changed()
		return drivingMsg{gen: gen, lane: true}
	}
}

// drivingMoved is what the loop does with one: say anything that failed, redraw,
// and go back to waiting.
func (a *app) drivingMoved(msg drivingMsg) tea.Cmd {
	var said tea.Cmd
	if msg.said != "" {
		a.note(msg.said)
	}
	// A HAND-OVER ON A CONNECTION THIS WINDOW HAS WALKED AWAY FROM MOVES NOTHING
	// AND RE-ARMS NOTHING ([followingMsg] states why, and why it is the
	// connection's generation and not the turn's). The conversation it is about
	// armed its own wait when it came forward.
	if msg.lane && msg.gen != a.convGen {
		return said
	}
	return tea.Batch(said, a.wake(), a.watchDriving())
}
