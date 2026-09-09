package remote

// driver.go is THE ROOM'S ONE KEYBOARD.
//
// Several surfaces have been able to attach to one conversation since version 2
// — that is what makes a desk and a phone one room — and until version 4 every
// one of them could type. There was no lock and no notion of who was driving:
// two windows both submitted, and the only arbiter was the session's own "a turn
// is already running" refusal, which is a sentence about the turn and says
// nothing at all about the other person.
//
// THE RULE IS THAT THE KEYBOARD FOLLOWS THE NEWEST WINDOW, AND NOTHING IS EVER
// THROWN AWAY. A person who walks to another machine and opens the conversation
// there is not an intruder to be refused, and the window they left behind is not
// rubbish to be closed — it is still showing the work, which is a feature. So:
//
//   - A NEW surface takes the keyboard on arrival. The older ones stay attached
//     and keep receiving every event live; they simply stop being the one that
//     can type, and their screens say so and say how to take it back.
//   - A RETURNING surface ([Hello.Back]) takes it only if it is going spare.
//     A redial is an attach the person did not make — a lid closed in one city
//     reconnecting half an hour later — and it must not pull the keyboard off
//     the machine they are actually sitting at.
//   - [MethodTake] moves it, in one round trip, to whoever asked.
//   - A DETACH hands it to the newest surface still in the room, so the last
//     window standing can always type.
//
// AND THE ENGINE IS THE ONLY THING THAT DECIDES. Every surface learns who drives
// from a frame the engine sent ([Driver]), a Submit from a surface that is not
// the driver is REFUSED WITH A SENTENCE rather than dropped, and there is
// exactly one driver per conversation at any instant because there is exactly
// one field. A surface that decided locally would be a second authority on a
// fact that can only have one, and the failure mode is two windows both
// believing they hold the keyboard and both being half right.

import (
	"errors"
	"os"
	"strings"
)

// MachineName is what this machine calls itself on the wire: its host name with
// any domain trimmed off, so a person reads `spark` rather than
// `spark.local.example.com` in a sentence about which window is typing.
//
// IT IS A LABEL AND NOT AN IDENTITY (see [Hello.Surface]). The empty string is a
// machine that could not answer, and the empty string is what every screen
// downstream draws as nothing at all rather than as "unknown" — the emptiness
// law, applied to a name.
//
// internal/pair's ThisMachineLabel reads the same fact for the pairing lane and
// is deliberately NOT called here: the two packages are siblings glued together
// by the door in cmd/aforge, neither imports the other, and pairing's label is a
// device's durable name where this is one connection's passing one.
func MachineName() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	if dot := strings.IndexByte(name, '.'); dot > 0 {
		name = name[:dot]
	}
	return machineLabel(name)
}

// machineNameMost is the most of a machine name that is worth carrying. A name
// longer than this is not a machine somebody types at; it is a mistake or a
// prank, and either way it is about to be drawn on somebody else's screen.
const machineNameMost = 32

// machineLabel makes one machine name safe to draw.
//
// A BOUNDARY THAT TRUSTS ITS INPUT IS NOT A BOUNDARY, which is the same law
// [WireFile.Name] states about a file name from the other side. This string
// arrives from another machine and ends up in a line on a person's screen, so a
// control character in it would be an escape sequence in somebody's terminal.
// Control characters go, whitespace collapses, and the length is capped.
func machineLabel(said string) string {
	kept := make([]rune, 0, machineNameMost)
	for _, r := range strings.TrimSpace(said) {
		if r < ' ' || r == 0x7f {
			continue
		}
		kept = append(kept, r)
		if len(kept) == machineNameMost {
			break
		}
	}
	return strings.TrimSpace(string(kept))
}

// ── the engine's side ───────────────────────────────────────────────────────

// takeLocked hands the keyboard to one surface. The caller holds the session's
// lock and tells the room afterwards ([Session.tellDriver]), because telling the
// room means writing to other connections and no session lock may be held across
// a write to a pipe that might be slow.
func (sess *Session) takeLocked(s *server) bool {
	if sess.driver == s {
		return false
	}
	sess.driver = s
	return true
}

// handOnLocked gives the keyboard to the newest surface still in the room, and
// to nobody when the room is empty.
//
// NEWEST, because that is the same rule an arrival follows and a person should
// not have to learn two. When the machine they walked away from closes its
// window, the keyboard should land where they went most recently — which is the
// most recent arrival that is still here.
// A WATCHER IS NOT IN THE RUNNING. It said on arrival that it is here to read
// ([Hello.Watch]), and a room whose only remaining surfaces are readers is left
// with NO driver — which is the honest state and the one every door already
// handles, rather than handing the keyboard to a window that will not use it.
func (sess *Session) handOnLocked() {
	var newest *server
	for surface := range sess.surfaces {
		if surface.watching {
			continue
		}
		if newest == nil || surface.arrived > newest.arrived {
			newest = surface
		}
	}
	sess.driver = newest
}

// driverForLocked is the driver as ONE surface should read it. See [Driver] for
// why the answer depends on who is asking.
func (sess *Session) driverForLocked(s *server) Driver {
	driving := sess.driver
	// A WATCHER IS NEVER TOLD THE KEYBOARD IS ITS OWN, and the empty room is why
	// this is here rather than folded into the line below: with no driver at all
	// `Yours` would be true for everybody, which is the right answer for a window
	// that may type and a lie to one that may not ([Hello.Watch]).
	if s.watching {
		if driving == nil {
			return Driver{}
		}
		return Driver{Machine: driving.name, Here: driving.name == "" || driving.name == s.name}
	}
	if driving == nil || driving == s {
		return Driver{Yours: true}
	}
	return Driver{
		Machine: driving.name,
		// Two windows on one machine send one name, so an equal name IS the
		// same machine — and an ABSENT name is read as the same machine too,
		// because `another window` is the weaker claim of the two and the one
		// that stays true either way.
		Here: driving.name == "" || driving.name == s.name,
	}
}

// tellDriver says who drives to everybody attached except one — the surface
// that has just been told some other way, which is the arriving one holding its
// own welcome (and its own writer, which [server.send] would deadlock on).
//
// EVERY SURFACE IS TOLD EVERY TIME, including the ones whose answer did not
// change. The frame is one line, the surface assigns a value it may already
// hold, and a fan-out that tried to work out who needed telling would be a
// second copy of the state on the engine — which is the one thing this file
// exists to avoid.
func (sess *Session) tellDriver(except *server) {
	sess.mu.Lock()
	notes := make(map[*server]Driver, len(sess.surfaces))
	for surface := range sess.surfaces {
		if surface == except {
			continue
		}
		notes[surface] = sess.driverForLocked(surface)
	}
	sess.mu.Unlock()

	for surface, note := range notes {
		_ = surface.send(Frame{Kind: "driver", Payload: mustJSON(note)})
	}
}

// take is [MethodTake]: this surface asks for the keyboard and gets it.
//
// IT NEVER REFUSES. A person pressing enter on a window that is showing them the
// work has said the only thing that needs saying, and there is nothing for the
// engine to weigh — the other window keeps the transcript, keeps its draft, and
// is told in the same instant.
//
// It ANSWERS WITH THE FACT rather than with nothing, so the surface that asked
// learns the outcome from the call it made instead of racing a frame it also
// receives. Everybody else is told by the frame.
func (s *server) take() Driver {
	sess := s.session
	sess.mu.Lock()
	// EXCEPT FOR A WATCHER, which is the one surface that asked not to be given
	// this. It is told who does drive, and nothing moves ([Hello.Watch]).
	if s.watching {
		mine := sess.driverForLocked(s)
		sess.mu.Unlock()
		return mine
	}
	moved := sess.takeLocked(s)
	mine := sess.driverForLocked(s)
	sess.mu.Unlock()
	if moved {
		sess.tellDriver(s)
	}
	return mine
}

// mayDrive is the guard in front of every door that puts words into the
// conversation, and the sentence it refuses with is the ENGINE'S.
//
// IT IS A REFUSAL AND NEVER A SILENT DROP. A message a person typed and pressed
// enter on either lands or is answered; a third outcome, where it disappears
// because a window on another machine had the keyboard, is the one behaviour
// nobody could debug from the screen. The sentence names the machine that has it
// and the key that takes it back, which are the only two facts that help.
//
// A surface that is drawing the watcher line will not reach this in the ordinary
// way — its composer does not submit. It is reached in the seconds a race is
// open (the keyboard moved while a message was on the wire), and by any surface
// that does not draw the line at all.
func (s *server) mayDrive() error {
	sess := s.session
	sess.mu.Lock()
	defer sess.mu.Unlock()
	// A WATCHER IS REFUSED EVEN WITH THE KEYBOARD GOING SPARE, because it never
	// held it and an empty seat is not an invitation ([Hello.Watch]). The sentence
	// is its own: `press enter to take it back` would be advice about a key this
	// surface deliberately does not have.
	if s.watching {
		return errors.New(watchingWord)
	}
	if sess.driver == nil || sess.driver == s {
		return nil
	}
	return errors.New(notDrivingWord(sess.driverForLocked(s)))
}

// watchingWord is what a reading surface is told if it tries to change anything.
// It states the surface's own posture rather than blaming the conversation,
// because nothing about the conversation is in the way.
const watchingWord = "this window is reading this conversation, not typing into it"

// watcherReads is EVERYTHING a [Hello.Watch] connection may ask for, and the
// list is short because the one thing such a connection exists to do is read one
// task's journal (internal/tui3's taskowner.go).
//
// IT IS AN ALLOW-LIST AND NOT A DENY-LIST, which is the whole of its safety. The
// methods that mutate a conversation without typing into it are numerous and
// keep arriving — a model, an effort rung, a permission, a resolve, a revive, a
// start, a stop, opening another session in this one, closing it — and a
// deny-list is a list somebody forgets to add to. This one refuses anything it
// has not heard of, so a method landing next year is read-only here by default
// and its author decides deliberately to add it.
//
// MethodDetach is on it because leaving on purpose is not a change to the
// conversation: it is the difference between a window closing and a pipe
// breaking, and a reader that could not say it would look like a crash.
//
// MethodPing is on it because it is the one call on this wire that is DEFINED as
// changing nothing: an empty frame out and an empty frame back, timed by the
// surface's own clock (wire.go says why the engine contributes no timestamp). A
// link that could not ask it would be a link that cannot measure itself, and the
// refusal would arrive as a fault on a connection that is working perfectly.
//
// AND THE TASK ROSTER SUBSCRIPTION IS ON IT BECAUSE THE OWNER'S GRAPH IS THE
// ONLY TRUTHFUL SOURCE FOR WHAT THE WORK IS DOING. [MethodTaskWatch] opens a
// standing read — the engine replays its roster and then pushes one frame per
// change (tasklane.go) — and it is what a reading page asks INSTEAD of guessing
// from files on the machine it happens to be running on
// (internal/tui3's taskowner.go).
var watcherReads = map[string]bool{
	MethodTaskRoom:  true,
	MethodTaskWatch: true,
	MethodDetach:    true,
	MethodPing:      true,
}

func watcherMay(method string) bool { return watcherReads[method] }

// notDrivingWord is what a surface without the keyboard is told when it tries to
// type anyway. `another window` is the word aforge already uses at home for a
// conversation open somewhere else (internal/tui3's home.go), and it is kept
// here so the two places a person meets this fact sound like one program.
func notDrivingWord(driver Driver) string {
	where := "in another window"
	if !driver.Here && strings.TrimSpace(driver.Machine) != "" {
		where = "on " + driver.Machine
	}
	return "the keyboard is " + where + " right now — press enter here to take it back"
}

// ── the turns a surface did not start ───────────────────────────────────────

// tellTurn says "a turn has started here" to everybody in the room except the
// surface that started it.
//
// THE ROOM MUST BE ABLE TO WATCH ITSELF. A turn's events have fanned out to
// every attached surface since version 2, but a surface only DRAWS a stream it
// has been told about, so a turn started on another machine went past a watching
// window in silence — the whole promise of staying attached, unkept. See [Turn]
// for why the engine sends this rather than the surface inferring it.
func (sess *Session) tellTurn(turn Turn, except *server) {
	sess.mu.Lock()
	watching := make([]*server, 0, len(sess.surfaces))
	for surface := range sess.surfaces {
		if surface != except {
			watching = append(watching, surface)
		}
	}
	sess.mu.Unlock()

	frame := Frame{Kind: "turn", Payload: mustJSON(turn)}
	for _, surface := range watching {
		_ = surface.send(frame)
	}
}
