package tui

import (
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/lipgloss"
)

// The gap between typing and being answered was the one stretch of the product
// with nothing on screen at all. A routing call is a provider round trip; on a
// reasoning model it is several seconds, and for those seconds the thread
// showed the user's own words and then nothing — no cursor, no line, no
// evidence the machine had heard. People retyped, and then had two jobs.
//
// This is the smallest honest fix: one faint line standing exactly where the
// reply will appear, wearing the speaker label the reply will wear, so the
// arrival replaces it in place rather than pushing it aside. It is not a status
// report and it never guesses — it says only that this window is waiting.
const (
	// awaitingStaleAfter follows the shimmer's rule for the same reason: a
	// reply that has not come in two minutes is not coming on this frame
	// either, and a line breathing forever pins the repaint loop. The line
	// stays; only the breath stops.
	awaitingStaleAfter = 2 * time.Minute
	// awaitingPulseTicks is how many animation ticks each phase of the pulse
	// holds. At the 120ms tick that is a whole cycle a little over a second —
	// slow enough to read as breathing rather than as a spinner.
	awaitingPulseTicks = 2
)

// noteAwaitingReply starts the wait. It is called where the store accepts this
// window's post, which is what keeps a visitor window silent about turns
// somebody else typed: a message that arrives through the poll never passes
// through here.
func (m *Model) noteAwaitingReply(posted store.Message) {
	if posted.Role != store.RoleUser || posted.NodeID != "" || posted.Seq == 0 {
		return
	}
	// A turn typed while an earlier one is still unanswered adds to the count
	// rather than replacing it. The line keeps standing under the oldest turn
	// nobody has answered, which is the one the reader is still waiting on.
	if m.awaitingSeq == 0 {
		m.awaitingSeq = posted.Seq
		m.awaitingSince = m.standingTime()
	}
	m.awaitingPending++
}

// noteAwaitingAnswered ends it. Anything unanchored the thread accepts after
// the awaited turn is the answer to it — an agent reply, a spoken receipt, a
// refusal — because the law downstream is that a user's words end in exactly
// one visible thread line.
func (m *Model) noteAwaitingAnswered(message store.Message) {
	if m.awaitingSeq == 0 || message.Role == store.RoleUser || message.NodeID != "" {
		return
	}
	if message.Seq <= m.awaitingSeq {
		return
	}
	// One answer settles one turn. With another still owed, the wait moves to
	// it — past this answer's own sequence, so the next line the thread accepts
	// is the one that can end it — instead of ending here for everybody.
	if m.awaitingPending > 1 {
		m.awaitingPending--
		m.awaitingSeq = message.Seq
		m.awaitingSince = m.standingTime()
		return
	}
	m.clearAwaitingReply()
}

func (m *Model) clearAwaitingReply() {
	m.awaitingSeq = 0
	m.awaitingPending = 0
	m.awaitingSince = time.Time{}
}

// awaitingReply reports whether the indicator belongs on screen. A live stream
// is already the reply arriving, so the line yields to it rather than sitting
// above it saying the same thing twice — but only to a stream with something in
// it. A stream that has opened and produced nothing yet, which is every control
// call and every reasoning phase, draws no line of its own; yielding to it put
// the thread back where this whole file started, blank for the longest part of
// the wait.
func (m *Model) awaitingReply() bool {
	return m.awaitingSeq != 0 && !m.streamVisible()
}

// awaitingAnimating is what keeps the animation ticking for it, and what stops.
func (m *Model) awaitingAnimating() bool {
	if !m.awaitingReply() {
		return false
	}
	return m.standingTime().Sub(m.awaitingSince) < awaitingStaleAfter
}

// awaitingPulse is the whole motion: one dot moving between the three inks the
// shimmer already uses. No frames, no glyph cycle, nothing with a personality.
var awaitingPulse = []lipgloss.Style{sweepBaseStyle, sweepSoftStyle, sweepCoreStyle, sweepSoftStyle}

// renderAwaitingReply draws the line, or nothing. It is deliberately the same
// shape as a speaker header — the label, then something faint after it — so
// when the reply lands the eye sees one line finish rather than two lines swap.
func (m *Model) renderAwaitingReply(width int) string {
	if !m.awaitingReply() || width <= 0 {
		return ""
	}
	dot := sweepBaseStyle
	if m.awaitingAnimating() {
		dot = awaitingPulse[(m.shimmerFrame/awaitingPulseTicks)%len(awaitingPulse)]
	}
	line := aforgeLabelStyle.Render(truncate("aforge", width))
	if width > lipgloss.Width("aforge")+2 {
		line += mutedStyle.Faint(true).Render("  ") + dot.Render("·")
	}
	return line
}
