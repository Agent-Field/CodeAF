package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/manual"
)

// The manual quotes the stale guest footer from the surface's one string, so a
// changed footer cannot silently leave another window's task page misdescribed.
func TestTheManualQuotesTheStaleGuestFooter(t *testing.T) {
	page, ok := manual.Chat().Page("tasks")
	if !ok {
		t.Fatal("the chat manual has no tasks page")
	}
	if !strings.Contains(page, "footer says\n`"+roomGuestStaleWord+"`") {
		t.Fatalf("the manual does not quote the stale footer %q", roomGuestStaleWord)
	}
}
