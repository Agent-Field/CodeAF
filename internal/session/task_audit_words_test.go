package session

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// A DURATION A PERSON READS IS SPELLED IN THE UNIT THEY ARE IN.
//
// [time.Duration.String] is the engine talking to itself. It put `one call ran
// 29.24078975s without answering and was abandoned` on a landing a person read
// — eight decimal places of a figure nobody can act on, and the nanoseconds were
// never a fact about the run, only the resolution of the clock that measured it.
// [taskSpanWord] is this package's one spelling of how long something took.
func TestAnAbandonedCallIsSaidInWholeSeconds(t *testing.T) {
	raw := regexp.MustCompile(`\d+\.\d+s`)
	for _, bound := range []time.Duration{
		29240789750 * time.Nanosecond,
		2*time.Minute + 500*time.Millisecond,
		90 * time.Minute,
	} {
		said := checkerStalled(bound)
		if raw.MatchString(said) {
			t.Fatalf("the landing reads\n  %s\nand a person cannot act on the decimals", said)
		}
		if want := taskSpanWord(bound); !strings.Contains(said, want) {
			t.Fatalf("the landing reads\n  %s\nwant the one spelling of how long it took, %q", said, want)
		}
	}
}
