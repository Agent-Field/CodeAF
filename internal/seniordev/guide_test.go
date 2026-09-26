//go:build !windows

package seniordev

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// SENIOR-DEV'S GUIDE CLAIMS THE HARD CODING WORK, IN FEWER BYTES THAN ITS CAP.
// The guide is what the chat's model reads before it reaches for senior-dev,
// and the paragraph it is printed under tells that model to prefer a program
// for whatever its guide claims (internal/session/delegate_door.go). So the
// claim is pinned here: an issue in a mature codebase, the work senior-dev
// was going unused on while the guide priced it at "an hour", and the brief
// carrying the issue in full. And it rides every request of every turn, so it
// is held under delegate.GuideMax.
func TestTheGuideClaimsComplexCodingWorkWithinItsBytes(t *testing.T) {
	guide := strings.TrimSpace(Program.Guide)
	if len(guide) > delegate.GuideMax {
		t.Fatalf("the guide is %d bytes, over delegate.GuideMax's %d: %q", len(guide), delegate.GuideMax, guide)
	}
	for _, want := range []string{
		"complex, multi-part coding work",
		"fixing an issue in a mature codebase whose cause spans files",
		"the issue or ask in full",
		"what done means and how to check it",
		"what must not change",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("the guide does not say %q: %q", want, guide)
		}
	}
	// AND IT SETS NO PRICE THAT KEEPS IT ON THE SHELF. "worth an hour" read as
	// a bar the chat's model almost never judged a piece of work to clear.
	if strings.Contains(guide, "worth an hour") {
		t.Errorf("the guide still prices senior-dev at an hour: %q", guide)
	}
}
