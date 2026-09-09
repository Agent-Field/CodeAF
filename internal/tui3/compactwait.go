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

// Only a known response wait earns a label. A task room has its own worker,
// so it cannot borrow the parent conversation's request clock or phase.
func (a *app) compactWaitWords(d deck) string {
	if !d.lens.clock || !a.awaitingReply() {
		return ""
	}
	began := a.awaited
	if news, ok := a.livePhase(); ok {
		// A known lost connection is actionable context immediately, not a
		// slow response that waits for the quiet ten-second label threshold.
		if news.Phase == provider.PhaseConnectionLost {
			return string(provider.PhaseConnectionLost)
		}
		if !phaseWaiting(news.Phase) {
			return ""
		}
		if !news.Since.IsZero() && news.Since.Before(began) {
			began = news.Since
		}
	}
	if age := compactStepAge(began, a.now()); age != "" {
		return "awaiting response · " + age
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
