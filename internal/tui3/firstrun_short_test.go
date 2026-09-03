package tui3

import (
	"strings"
	"testing"
)

// TestAShortWindowKeepsTheSetupsBoxAndItsWayOut is the first-run block giving up
// rows from its MIDDLE.
//
// The block used to be centred and then cut at the window's height, so twelve
// rows drew the wordmark, the question and four lines of prose and stopped: no
// `›` box, no keys line, and no visible way off a screen that read as an install
// that had hung.
func TestAShortWindowKeepsTheSetupsBoxAndItsWayOut(t *testing.T) {
	a, _, _ := setupApp(t, nil)
	for _, height := range []int{12, 16, 10, 8} {
		a.width, a.height = 120, height
		frame, _, _ := a.frame()
		lines := strings.Split(frame, "\n")
		if len(lines) != height {
			t.Fatalf("a %d-row window was given %d rows", height, len(lines))
		}
		text := plain(frame)
		if !strings.Contains(text, strings.TrimSpace(setupLead)) {
			t.Fatalf("a %d-row first run has no box to type in:\n%s", height, text)
		}
		if want := a.setupKeysWord(); !strings.Contains(strings.Join(strings.Fields(text), " "), want) {
			t.Fatalf("a %d-row first run does not say %q, so nothing on it names the way out:\n%s", height, want, text)
		}
	}
}

// TestATallWindowStillDrawsTheWholeSetupBlock is the other half: nothing is
// given up while the frame can hold it.
func TestATallWindowStillDrawsTheWholeSetupBlock(t *testing.T) {
	a, _, _ := setupApp(t, nil)
	a.width, a.height = 120, 40
	frame, _, _ := a.frame()
	said := strings.Join(strings.Fields(plain(frame)), " ")
	for _, want := range []string{product, "your openrouter key", setupKeyURL, a.setupKeysWord()} {
		if !strings.Contains(said, want) {
			t.Fatalf("a 40-row first run dropped %q:\n%s", want, said)
		}
	}
}
