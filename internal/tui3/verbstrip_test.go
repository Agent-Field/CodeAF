package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE STRIP BELONGS TO A ROW ──────────────────────────────────────────────
//
// verbstrip.go states the law — "a letter cannot act on a row that has moved out
// from under it" — and for three waves it was enforced by a list of the keys
// that move a cursor. The tasks place binds four more spellings of its own
// cursor (`ctrl+n`, `ctrl+p`, `home`, `end`), none of which were on that list,
// so `→` over a running task and then `ctrl+n` left the strip standing under a
// DIFFERENT row, and `s` there stopped the task the strip was captured on.
//
// These tests are about the law and not about the list: the strip is bound to
// the row's own name, so a key nobody has bound yet cannot get past it.

// stripWalkLab is the tasks place with three rows that can be stopped and the
// cursor on the MIDDLE one, so that every key either end of the list moves it.
func stripWalkLab(t *testing.T) (*app, *stopFake) {
	t.Helper()
	a, agent := tasksFootApp(t)
	drive(t, a, streamEventMsg{gen: a.gen,
		ev: update(8, "Rotate the staging certificate", session.TaskRunning, session.TaskNotice{})})
	drive(t, a, streamEventMsg{gen: a.gen,
		ev: update(9, "Count the tabs", session.TaskRunning, session.TaskNotice{})})
	a.showPage(pageTasks)
	stops := a.taskSheet.stops(a)
	if len(stops) < 3 {
		t.Fatalf("the lab drew %d rows, want three that can be stopped", len(stops))
	}
	a.taskSheet.cursor = stops[1]
	if len(a.taskSheet.verbs(a)) == 0 {
		t.Fatal("the middle row offers no verb, so there is nothing to leave standing")
	}
	return a, agent
}

// EVERY KEY THE PLACES BIND TO A CURSOR TAKES THE STRIP WITH IT, and the verb
// captured on the row left behind fires on nothing.
func TestTheStripCannotOutliveTheRowItWasOpenedOn(t *testing.T) {
	for _, walk := range []string{"up", "down", "ctrl+p", "ctrl+n", "home", "end", "pgup", "pgdown"} {
		t.Run(walk, func(t *testing.T) {
			a, agent := stripWalkLab(t)
			was := (placeTasks{}).rowID(a)
			drive(t, a, key("right"))
			if !a.strip.open {
				t.Fatal("`→` opened no strip on a row that can be stopped")
			}
			drive(t, a, key(walk))
			if now := (placeTasks{}).rowID(a); now == was {
				t.Fatalf("%s did not move the cursor off %q, so this key proves nothing", walk, was)
			}
			// THE FRAME THE PERSON IS LOOKING AT IS WHERE THIS IS SETTLED. Every
			// way a cursor moves ends in a paint, and the paint is where the strip
			// is dropped — so the question asked here is the one they can see:
			// after the move, is the strip still on the screen.
			screen := taskSheetText(a)
			if a.strip.open {
				t.Fatalf("%s left the strip standing under a row it was not about:\n%s", walk, screen)
			}
			if strings.Contains(screen, "s "+stopActWord) {
				t.Fatalf("%s left the strip's letters drawn:\n%s", walk, screen)
			}
			// AND THE LETTER IS A LETTER AGAIN. This is the half that cost a task:
			// the strip drawn under the new row made `s` look like that row's verb.
			drive(t, a, key("s"))
			if len(agent.asked) != 0 {
				t.Fatalf("after %s, `s` asked the engine %v", walk, agent.asked)
			}
		})
	}
}

// AND THE POINTER IS NO DIFFERENT FROM THE KEYBOARD. A wheel tick walks the same
// cursor and was never on any list of keys, because it is not a key at all.
func TestTheStripCannotOutliveTheRowAWheelWalksOffIt(t *testing.T) {
	a, agent := stripWalkLab(t)
	was := (placeTasks{}).rowID(a)
	drive(t, a, key("right"))
	if !a.strip.open {
		t.Fatal("`→` opened no strip on a row that can be stopped")
	}
	drive(t, a, tea.MouseWheelMsg{X: 4, Y: placeHeadRows + 1, Button: tea.MouseWheelDown})
	if now := (placeTasks{}).rowID(a); now == was {
		t.Skip("a wheel tick did not move this place's cursor")
	}
	taskSheetText(a)
	if a.strip.open {
		t.Fatal("a wheel tick left the strip standing under a row it was not about")
	}
	drive(t, a, key("s"))
	if len(agent.asked) != 0 {
		t.Fatalf("after a wheel tick, `s` asked the engine %v", agent.asked)
	}
}

// AND THE ROW STAYING PUT IS NOT A MOVE. A key that cannot go anywhere — `end`
// on the last row — leaves the strip exactly where it is, because the law is
// about the row under the cursor and never about which key was pressed.
func TestAKeyThatMovesNothingLeavesTheStripStanding(t *testing.T) {
	a, agent := stripWalkLab(t)
	drive(t, a, key("end"))
	drive(t, a, key("right"))
	if !a.strip.open {
		t.Fatal("`→` opened no strip on the last row")
	}
	drive(t, a, key("end"))
	taskSheetText(a)
	if !a.strip.open {
		t.Fatal("`end` on the last row closed a strip whose row had not moved")
	}
	drive(t, a, key("s"))
	if len(agent.asked) != 1 {
		t.Fatalf("`s` on the row the strip is still about asked the engine %v", agent.asked)
	}
}
