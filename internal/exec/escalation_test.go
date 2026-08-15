package exec

import (
	"strings"
	"testing"
)

// The menu a retry is chosen from is the ordinary menu with the worker that
// just failed struck off it, and the whole point is what happens when that
// leaves nothing: a specialist may not be handed back its own failure, and with
// one specialist registered that means no menu, no call, and the baseline
// escalation the process had before any of this existed.
func TestRetryMenuNeverOffersTheWorkerThatJustFailed(t *testing.T) {
	if got := MenuTextExcept(""); got != "" {
		t.Fatalf("a baseline process has a retry menu:\n%s", got)
	}
	if got := MenuTextExcept("swe"); got != "" {
		t.Fatalf("excluding an unregistered worker invented a menu:\n%s", got)
	}
	defer ForgetSubharnesses()
	RegisterSubharness(SubharnessInfo{Name: "swe", Purpose: "software engineering taken whole"})

	// A generalist leaf that failed may be offered the specialist.
	offered := MenuTextExcept(LinearSubharness)
	if !strings.Contains(offered, "swe — software engineering taken whole") {
		t.Fatalf("the specialist was not offered to a failed generalist leaf:\n%s", offered)
	}
	if offered != MenuText() {
		t.Fatalf("excluding the baseline changed the menu:\n%s", offered)
	}

	// The specialist's own failed leaf has nowhere to go. No self-rung.
	if got := MenuTextExcept("swe"); got != "" {
		t.Fatalf("a failed swe leaf was offered swe again:\n%s", got)
	}

	// A second specialist makes the exclusion visible rather than total.
	RegisterSubharness(SubharnessInfo{Name: "reviewer", Purpose: "reading one change whole"})
	menu := MenuTextExcept("swe")
	if strings.Contains(menu, "swe —") {
		t.Fatalf("the failed worker is still on its own retry menu:\n%s", menu)
	}
	if !strings.Contains(menu, "reviewer — reading one change whole") {
		t.Fatalf("the other specialist was struck off too:\n%s", menu)
	}
}
