package tui3

// THE PULSE'S OWN BEAT: THE COUNTS, KEPT WHILE NO HOME IS OPEN.
//
// Home reads the world on its three-second beat and the pulse's two counts come
// off that reading (homemachine.go's [app.readMachine]). A conversation has no
// such beat — nothing inside a chat walks the world — and a conversation is where
// the pulse now says `2 want you · 1 moving` (DESIGN.md's law 11), so without
// this file the counts would be whatever the last home left behind, frozen, for
// as long as a person sat in a chat.
//
// ── THE LAWS ──
//
//   - THE WALK IS A COMMAND, OFF THE LOOP. The world is every project's index on
//     the machine, and a person typing into a chat must never wait for it: the
//     three inputs of the seam are taken on the loop and the walk is taken off it
//     (home.go's [worldSeam], the device hop.go's [app.countConversations]
//     already uses). What comes back is counted on the loop, where the standing
//     bands and the errands live.
//
//   - A DRAW NEVER TAKES IT. The beat writes [app.machine]; the pulse reads it.
//
//   - IT STANDS ASIDE FOR HOME. While home is open its own beat reads a fresher
//     world three times as often, so this one re-arms and does nothing — and a
//     walk that comes back after home opened is dropped rather than laid over
//     home's reading.

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// pulseEvery is how long between the pulse's own readings of the world.
//
// TEN SECONDS AND NOT HOME'S THREE. A conversation is a page a person is
// reading and typing into, and the counts on its top line are a glance taken
// between sentences; a figure ten seconds old is as true as a glance needs, and
// the walk is paid a third as often as home pays it.
const pulseEvery = 10 * time.Second

// pulseTickMsg is the beat, arriving.
//
// THERE IS NO GENERATION. Home and the places need one because they are opened
// and closed and each opening arms a clock; this one is armed exactly once, by
// [app.Init], and re-arms itself for the life of the window.
type pulseTickMsg struct{}

// pulseWorldMsg is the walk, coming back.
type pulseWorldMsg struct {
	world session.World
	known bool
}

// pulseTick schedules the next beat.
func pulseTick() tea.Cmd {
	return tea.Tick(pulseEvery, func(time.Time) tea.Msg { return pulseTickMsg{} })
}

// pulseNow is the FIRST reading, asked the moment the window starts rather than
// ten seconds into it: a conversation opened over two questions should say so
// on its first frames, not after a person has started typing.
func pulseNow() tea.Msg { return pulseTickMsg{} }

// pulseBeat answers one beat: it asks for the walk when no home is open, and
// always asks for the next beat.
func (a *app) pulseBeat() tea.Cmd {
	// AND IT IS THE TICK THE DISK MEMOS RIDE. Nothing on this machine tells a
	// terminal that a picture was overwritten or that another window rewrote the
	// model cache, so the only way to know is to ask again — and this is the beat
	// this surface already pays for, on the loop, where the fourth law says a
	// reading belongs (learned.go's [app.refreshLearning]).
	a.refreshLearning()
	if a.at(pageHome) {
		return pulseTick()
	}
	return tea.Batch(a.pulseWalk(), pulseTick())
}

// pulseWalk is the walk as a command. The seam's three inputs are taken HERE,
// on the loop, for [app.countConversations]' reason: a command that reached
// back into the app would be reading fields the update loop is writing.
func (a *app) pulseWalk() tea.Cmd {
	door, root, hosted := a.world, a.placesRoot(), a.hosted()
	return func() tea.Msg {
		world, known := worldSeam(door, root, hosted)
		return pulseWorldMsg{world: world, known: known}
	}
}

// pulseCounted lays the walk's answer into the memo.
//
// A WORLD NOBODY COULD READ LEAVES THE COUNTS AS THEY WERE. Over a connection
// the far machine may not have answered yet, and "not known" is not "nothing
// wants you" — the emptiness law draws an unknown as nothing, but a known figure
// already on the line is not unknown because one reading failed.
func (a *app) pulseCounted(msg pulseWorldMsg) {
	if a.at(pageHome) || !msg.known {
		return
	}
	bands, _, _ := a.standBandsOf(msg.world)
	a.readMachine(a.now(), msg.world.Sessions(), bands)
	a.touch()
}
