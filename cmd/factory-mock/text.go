package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func width(s string) int { return ansi.StringWidth(s) }

// fit truncates a painted string to w cells with an ellipsis.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if width(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return ansi.Truncate(s, w-1, "") + "…"
}

func pad(s string, w int) string {
	d := w - width(s)
	if d <= 0 {
		return s
	}
	return s + strings.Repeat(" ", d)
}

func padLeft(s string, w int) string {
	d := w - width(s)
	if d <= 0 {
		return s
	}
	return strings.Repeat(" ", d) + s
}

// twoSides lays left and right on one row of w cells; left yields.
func twoSides(left, right string, w int) string {
	rw := width(right)
	lw := w - rw - 1
	if lw < 8 {
		return fit(left, w)
	}
	return pad(fit(left, lw), lw) + " " + right
}

func ago(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func dur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}

var sparks = []rune("▁▂▃▄▅▆▇█")

func spark(xs []int) string {
	var b strings.Builder
	for _, x := range xs {
		if x < 0 {
			x = 0
		}
		if x > 7 {
			x = 7
		}
		b.WriteRune(sparks[x])
	}
	return b.String()
}

func money(f float64) string {
	if f == 0 {
		return ""
	}
	if f < 10 {
		return fmt.Sprintf("$%.2f", f)
	}
	return fmt.Sprintf("$%.0f", f)
}
