package session

import (
	"os"
	"strings"
	"testing"
)

// Spending every automatic retry calls for a report to the person, and the
// role page and manual must not invite an extra round of branch edits.
func TestRetriesSpentTellThePersonWhatStillDoesNotWork(t *testing.T) {
	manual, err := os.ReadFile("../manual/chat/senior-dev.md")
	if err != nil {
		t.Fatal(err)
	}
	for name, words := range map[string]string{"prompt": programOutcomePrompt, "manual": string(manual)} {
		if strings.Contains(words, "only for a trivial gap, or once") {
			t.Errorf("%s invites branch edits after retries are spent", name)
		}
		if !strings.Contains(words, "can be sent back no more") || !strings.Contains(words, "tells the person what still does not work") {
			t.Errorf("%s omits the report after retries are spent", name)
		}
	}
	next := programNextStep(programOutcome{verdict: programFailed, programAttempt: programAttempt{auto: programAutoRetries}})
	if !strings.Contains(next, "do not hand it back") || !strings.Contains(next, "what still does not work") {
		t.Fatalf("retry footer lost its reporting policy: %q", next)
	}
}
