package tui3

// THE ONE LINE THE CARD GAINS, AND THE ROW IT DOES NOT DRAW.
//
// The preflight's whole surface here is a dim row between the brief and the
// answers. Both halves of it are worth pinning: that the fact reaches the person
// while the countdown is still running, and that a proposal with nothing to say
// about other windows draws no row at all — an extra line on every proposal is
// how a warning becomes furniture and then becomes something somebody turns off.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// elsewhereProposal is [proposal] carrying the preflight's line.
func elsewhereProposal(a *app, id uint64, countdown time.Duration, line string) session.Event {
	ev := proposal(a, id, countdown)
	ev.Task.Elsewhere = line
	return ev
}

func TestAProposalSaysWhichWindowIsAlreadyInTheseFiles(t *testing.T) {
	a, _, _ := taskApp(t)
	line := "another window is already in internal/parse/keys.go · Port the picker"
	drive(t, a, streamEventMsg{gen: a.gen, ev: elsewhereProposal(a, 7, 4*time.Second, line)})

	text := taskText(a)
	if !strings.Contains(text, line) {
		t.Fatalf("the card never said who else is in these files:\n%s", text)
	}
	// IT IS A FACT AND NOT A GATE: the same answers and the same clock, on the
	// question the block is drawing above the box (question.go).
	ask := plain(strings.Join(a.questionRows(a.width), "\n"))
	for _, want := range []string{"1  start it", "2  no", "start it in 4s"} {
		if !strings.Contains(ask, want) {
			t.Fatalf("the warning changed the question: %q is gone:\n%s", want, ask)
		}
	}
	// The words this surface must never use about it. Nothing is being prevented,
	// so nothing may read as though it were.
	for _, banned := range []string{"conflict", "collision", "blocked", "locked"} {
		if strings.Contains(strings.ToLower(text), banned) {
			t.Fatalf("the card spoke machinery — %q:\n%s", banned, text)
		}
	}
}

func TestAProposalWithNothingToSayAboutOtherWindowsDrawsNoRowForIt(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: elsewhereProposal(a, 7, 4*time.Second, "")})

	text := taskText(a)
	if strings.Contains(text, "another window") {
		t.Fatalf("a proposal with no overlap drew a row about other windows:\n%s", text)
	}
	// AND IT CERTAINLY DOES NOT SAY THE OPPOSITE. Silence means nothing was found,
	// never that the files are clear.
	for _, banned := range []string{"alone", "no other window", "nobody else"} {
		if strings.Contains(text, banned) {
			t.Fatalf("a silence was drawn as clearance — %q:\n%s", banned, text)
		}
	}
}
