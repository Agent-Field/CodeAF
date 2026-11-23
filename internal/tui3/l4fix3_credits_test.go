package tui3

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/credits"
)

// L4-M6b, fix round 3: A Home message keeps the whole account warning once,
// while its footer remains a separate row of key hints and the project.
func TestL4Fix3HomeMessageCreditWarningOnce(t *testing.T) {
	a := placeApp(t)
	a.width = 200
	a.readCredits = func(context.Context) (credits.Reading, error) { return credits.Reading{}, nil }
	seedLowCredits(t, a)
	a.refreshCreditWarnings()
	a.pageMsg = "open in another window"
	text := placeFrameText(a)
	if strings.Count(text, lowCreditsWarning) != 1 {
		t.Fatalf("warning must appear once: %s", text)
	}
	message, keys := false, false
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, a.pageMsg) {
			message = strings.Contains(line, lowCreditsWarning)
		}
		if strings.Contains(line, "/ commands") {
			keys = true
			if strings.Contains(line, lowCreditsWarning) || strings.Contains(line, a.pageMsg) {
				t.Fatalf("account news joined the key hints: %q", line)
			}
		}
	}
	if !message || !keys {
		t.Fatalf("message or key row missing: %s", text)
	}
}
