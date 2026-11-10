package manual

import (
	"strings"
	"testing"
)

func TestPoolQuitManualExplainsCancelledScoring(t *testing.T) {
	page, ok := Chat().Page("running-from-the-terminal")
	if !ok {
		t.Fatal("terminal manual is missing")
	}
	for _, words := range []string{"Quitting cancels unfinished scoring", "at most one second", "next ordinary launch retries", "Scores saved before you quit stay saved"} {
		if !strings.Contains(page, words) {
			t.Errorf("pool quit answer omits %q", words)
		}
	}
}
