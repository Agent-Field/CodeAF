package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A FAN WIDER THAN THE MACHINE DRAWS BOTH HALVES OF ITSELF. A parent hands out
// more parts than this box can carry at once, the engine starts the ones it can
// and holds the rest (internal/session's task_pressure.go), and the column has
// to say which is which — a held part wears the machine's own word, and NEVER
// the cap's, because "slot" sends a person to a setting they did not set.
func TestAHeldChildSaysTheMachineHeldItAndNotTheCap(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(1, "Port the parser", session.TaskRunning, session.TaskNotice{})},
		streamEventMsg{gen: a.gen, ev: update(2, "Port the lexer", session.TaskRunning, session.TaskNotice{
			Parent: 1,
		})},
		streamEventMsg{gen: a.gen, ev: update(3, "Port the printer", session.TaskQueued, session.TaskNotice{
			Parent: 1, Waiting: waitWordMachine,
		})},
	)

	held := a.tasks[3]
	if held == nil {
		t.Fatal("the held part never reached the surface")
	}
	if held.waiting != waitWordMachine {
		t.Fatalf("the held part carries %q, want %q", held.waiting, waitWordMachine)
	}
	if rows := a.railUnder(held, underWidth(underCols)); len(rows) != 1 || !strings.Contains(plain(rows[0]), waitWordMachine) {
		t.Fatalf("the held part's row is %q, want it to say %q", rows, waitWordMachine)
	}
	if status := a.taskStatus(held); status.Reason != waitWordMachine {
		t.Fatalf("the held part's reason is %q, want %q", status.Reason, waitWordMachine)
	}

	// AND THE COLUMN DRAWS IT where a person is looking: the held part under the
	// cursor wears its reason under its own row.
	drive(t, a, altT())
	railFocusOn(t, a, 3)
	roster := rosterText(a, 12) + "\n" + a.sideHoverWords() + "\n" + railHint(a, 3)
	if !strings.Contains(roster, waitWordMachine) {
		t.Fatalf("the roster does not say the machine is holding a part:\n%s", roster)
	}
	// AND THE PART THAT IS WORKING SAYS NOTHING, because a fan that is half held
	// is half running and a column that said "machine busy" over the whole
	// family would be describing work that is under way.
	if working := a.tasks[2]; working == nil || working.waiting != "" {
		t.Fatalf("the running part carries a hold: %q", working.waiting)
	}
	if strings.Contains(roster, waitWordSlot) {
		t.Fatalf("a part held by the machine was drawn as held by the cap:\n%s", roster)
	}
}
