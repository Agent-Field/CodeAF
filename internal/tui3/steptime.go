package tui3

import (
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Short steps need no clock. A longer step gains elapsed time, because its
// completion time is unknown and a countdown would promise an invented deadline.
const stepElapsedAfter = 10 * time.Second

func compactStepAge(began, now time.Time) string {
	if began.IsZero() || now.Sub(began) < stepElapsedAfter {
		return ""
	}
	return countUpWord(now.Sub(began))
}

// The clock uses spare space on the caption's last line. It never rewraps the
// sentence or adds a fourth row, and its digits remain outside the shimmer.
func (a *app) stepTimeSuffix(line, painted, age string, room int) string {
	if age == "" {
		return painted
	}
	tail := "  " + age
	if ansi.StringWidth(line)+ansi.StringWidth(tail) > room {
		return painted
	}
	return painted + a.pal.dim(tail)
}
