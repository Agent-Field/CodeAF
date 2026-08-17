package tui3

import (
	"strings"
	"time"
)

// THE FRAME RATE IS A CLAIM ABOUT THE LINK.
//
// Every frame this surface builds is a screenful of styled text handed to a
// terminal, and the whole of [frameInterval]'s reasoning — thirty frames a
// second is what a person can see, so building more is building for nobody —
// holds only while the terminal is on this machine. Over a connection it is a
// claim about the wire as well: a spinner drawn thirty times a second across a
// 200ms round trip is thirty frames queueing behind each other, and a queue of
// frames is what jank IS. The cursor lands late, the scroll arrives in steps,
// and a keystroke waits behind animation nobody asked for.
//
// So the clock reads the link once, at construction, and turns slower on the
// far side of one. THERE IS NOTHING TO SET: a person who has just typed `ssh`
// has already said everything this needs to know, and a setting for it would be
// a question about round-trip time asked of somebody who came here to work.
//
// WHAT IS DRAWN DOES NOT CHANGE — only how often. The animations are counted in
// frame SLOTS rather than in frames drawn ([app.frameStride]), so a spinner
// takes the same second and a half to go round whether it was drawn forty-five
// times or fifteen, and the count-ups, the countdowns and the fades were
// already measured against the wall clock and never noticed. The remote surface
// is the local one with fewer intermediate pictures of the same motion.

// remoteFrameInterval is the repaint ceiling on the far side of a link: three
// frame slots, about ten frames a second.
//
// Ten is chosen where two curves cross. Below it, motion stops reading as
// motion — a braille spinner at 6fps is a symbol jumping between shapes — and
// above it, a link with a round trip worth naming spends the whole gain on
// frames that were already stale when they were written. It is an exact
// multiple of [frameInterval] so that one drawn frame is a whole number of
// slots, which is what lets every animation on this surface keep its wall-clock
// pace without knowing anything about the link.
const remoteFrameInterval = 3 * frameInterval

// The two words a shell sets when the session it opened came in over the
// network. Either one is enough, and both are read because they are set by
// different halves of the same act: SSH_CONNECTION is the connection's own
// four figures, and SSH_TTY the terminal it was given.
const (
	envSSHConnection = "SSH_CONNECTION"
	envSSHTTY        = "SSH_TTY"
)

// remoteLink reports whether the terminal reading this surface is on the far
// side of a connection, from the environment alone — NO PROBE. A round-trip
// measurement would be a packet sent to answer a question the shell has already
// answered, and it would have to be taken again to stay true.
//
// It is read once, at construction, for the reason [tmuxTerm] is (copymode.go):
// a session does not stop being remote halfway through, and a fact re-read on
// the render path is a fact that can change between two halves of one frame.
func remoteLink(env func(string) string) bool {
	if env == nil {
		return false
	}
	for _, key := range []string{envSSHConnection, envSSHTTY} {
		if strings.TrimSpace(env(key)) != "" {
			return true
		}
	}
	return false
}

// frameEvery is how long the next frame waits, and it is the ONE thing the rest
// of this surface asks about the link. [frameInterval] stays what it always
// was: the local cadence, and the unit everything else is counted in.
func (a *app) frameEvery() time.Duration {
	if a.remote {
		return remoteFrameInterval
	}
	return frameInterval
}

// frameStride is how many frame slots one drawn frame covers — 1 locally, 3 on
// the far side of a link.
//
// It is what keeps the two surfaces the same surface. Every animation here
// steps on a multiple of [app.paints] (spinnerStep, pulseStep, welcomeFrames),
// so paints counts SLOTS ELAPSED rather than frames drawn: a spinner at 10fps
// then advances by three quarters of a step per frame instead of a quarter, and
// goes round in the same second and a half it always did. The alternative —
// counting frames drawn — is an animation that runs a third as fast over a
// link, which is a surface that behaves differently depending on where it is
// being read.
func (a *app) frameStride() int {
	return max(1, int(a.frameEvery()/frameInterval))
}

// dueEvery reports whether the frame being painted crossed the next multiple of
// every slots — "is this the frame that does the once-a-third-of-a-second
// thing".
//
// It asks about a CROSSING rather than about a remainder because a stride
// greater than one steps over most multiples without landing on them: a modulo
// test would fire on a third of the frames it was written for over a link, and
// the period it names is a period in wall time, not a count of pictures.
func (a *app) dueEvery(every int) bool {
	if every <= 0 {
		return false
	}
	return a.paints/every != (a.paints-a.frameStride())/every
}
