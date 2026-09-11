package tui3

import (
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/charmbracelet/x/ansi"
)

// Waiting belongs to the conversation, not to the completed step beside it.
// The same feather used by the live caption softly brightens one still dot;
// no icon changes identity and no second animation clock is introduced.
func (a *app) compactWaitMark() string {
	return a.shimmer(a.linearMark("·", "."))
}

// The most specific known wait earns the label: a lost connection immediately,
// a response wait after ten seconds, then the turn's generic elapsed work. A
// task room has its own worker, so it cannot borrow the parent conversation's
// request clock, turn clock or phase.
func (a *app) compactWaitWords(d deck) string {
	if !d.lens.clock {
		return ""
	}
	news, hasPhase := a.livePhase()
	if hasPhase {
		// A known lost connection is actionable context immediately, not a
		// slow response that waits for the quiet ten-second label threshold.
		if news.Phase == provider.PhaseConnectionLost {
			return string(provider.PhaseConnectionLost)
		}
	}
	if a.awaitingReply() && (!hasPhase || phaseWaiting(news.Phase)) {
		began := a.awaited
		if hasPhase && !news.Since.IsZero() && news.Since.Before(began) {
			began = news.Since
		}
		if age := compactStepAge(began, a.now()); age != "" {
			return "awaiting response · " + age
		}
		return ""
	}
	// A completed caption names the last step, not the work continuing after
	// it. The fallback therefore times the conversation's turn and says only
	// that it is still working; it never relights or retimes the finished step.
	if age := compactStepAge(a.turnBegan, a.now()); age != "" {
		return "still working · " + age
	}
	return ""
}

// The suffix spends only spare cells on the last wrapped line. When even a dot
// cannot fit, the caller puts that dot in the existing icon gutter instead.
// The caption keeps every word and the clock never creates another row.
func (a *app) compactWaitSuffix(line string, room int, d deck) (string, bool) {
	space := room - ansi.StringWidth(line)
	if space < 3 {
		return "", false
	}
	tail := "  " + a.compactWaitMark()
	if words := a.compactWaitWords(d); words != "" && space >= 4+ansi.StringWidth(words) {
		tail += a.pal.dim(" " + words)
	}
	return tail, true
}
