package tui3

// WHICH LIFE ANOTHER WINDOW'S TASK IS IN.
//
// A node's state is `running` from its worker's first call to its landing, and
// for whole minutes in the middle of that the worker is not the one working: a
// check is reading what it left, or a repair round is closing what the check
// found. The live window learned to say so (taskphase.go); every OTHER window
// went on drawing a bare `running`, because the phase was on the node and the
// node is in another process.
//
// It rides the presence file now (session's [session.PresenceTask.Phase]), and
// these are the two halves of what that has to mean: a fresh row says the phase
// in the same words the live window draws, and a row nothing is behind any more
// says exactly what it said before this existed.

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestAHomeRowSaysWhichLifeAnotherWindowsTaskIsIn(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", "/tmp/alpha", now)
	other := lab.session("-tmp-alpha", "aaaa000000000002", "port the picker", "/tmp/alpha", now.Add(-time.Hour))
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "7", Name: "port", Label: "Port the picker", Title: "Port the picker",
		Status: string(session.TaskRunning), SessionID: "aaaa000000000002",
	})
	// The other window says what it is doing right now, and the node has been
	// out of its worker's hands for four minutes.
	lab.presence("-tmp-alpha", "aaaa000000000002", session.PresenceWorking, "", now,
		session.PresenceTask{
			ID: "7", Title: "Port the picker", State: "running",
			StartedAt: now.Add(-9 * time.Minute), Phase: session.TaskPhaseChecking,
		})

	a := lab.app(mine)
	a.openHome()
	card := homeCardFor(t, a, other)
	// THE WORDS ARE THE ONES THE LIVE WINDOW DRAWS, from the one place they are
	// spelled (taskphase.go) — a second wording here would be this surface
	// telling two windows two different things about one node.
	want := homeLiveGlyph + " working" + railSep + taskCheckingWord
	if cardLine(card, want) < 0 {
		t.Fatalf("the card does not say which life the task is in (want %q):\n%s", want, strings.Join(card, "\n"))
	}
}

// AND THE SWITCHER SAYS IT ON THE ROW ITSELF, which is where somebody who is not
// pointing at anything reads what a conversation is doing.
func TestTheSwitcherRowSaysWhichLifeAnotherWindowsTaskIsIn(t *testing.T) {
	now := time.Date(2026, time.August, 31, 13, 0, 0, 0, time.UTC)
	entry := session.TaskIndexEntry{ID: "1", Status: string(session.TaskRunning)}
	row := session.SessionRow{
		ID: "run", Title: "Running chat", At: now.Add(-2 * time.Hour), Live: true,
		Presence: session.SessionPresence{
			State: session.PresenceWorking,
			RunningTasks: []session.PresenceTask{{
				ID: "1", State: "running", StartedAt: now.Add(-2 * time.Hour),
				Phase: session.TaskPhaseRepairing,
			}},
		},
		Tasks: session.TaskRollup{Running: 1, Rows: []session.TaskIndexEntry{entry}},
	}
	note := switcherConversationNote(row, now.Add(-3*time.Hour))
	want := "1 task running" + railSep + taskClosingWord
	if note != want {
		t.Fatalf("the switcher row says %q, want %q", note, want)
	}

	// THE ROUNDS ARE NOT ON THIS WIRE AND ARE NOT INVENTED HERE. The presence
	// file carries the life and nothing else, so the row says what is happening
	// and says no numbers at all rather than "round 0 of 0" (the emptiness law).
	if strings.Contains(note, "round") {
		t.Fatalf("the switcher row made up rounds nothing told it: %q", note)
	}

	// AND A NODE GETTING ON WITH THE WORK IS THE ROW IT ALWAYS WAS. Working is
	// written as nothing at all, so what is left is the activity the index row
	// carries, exactly as before.
	row.Presence.RunningTasks[0].Phase = ""
	row.Tasks.Rows[0].Activity = "reading filings"
	if note := switcherConversationNote(row, now.Add(-3*time.Hour)); note != "1 task running"+railSep+"reading filings" {
		t.Fatalf("a working node's row is %q, want the activity it always drew", note)
	}
}

// A ROW NOTHING IS BEHIND ANY MORE KEEPS THE ROW IT ALWAYS DREW.
//
// A presence file older than the freshness window is a claim nobody is left to
// correct — the window it describes may have been killed an hour ago — and a
// phase read off it would be this surface narrating minutes that ended when the
// process did. Everything about such a row is what it was before the phase
// existed: the work is `incomplete`, and no life of it is drawn at all.
func TestAStalePresenceRowKeepsTheRowItAlwaysDrew(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", "/tmp/alpha", now)
	other := lab.session("-tmp-alpha", "aaaa000000000002", "port the picker", "/tmp/alpha", now.Add(-time.Hour))
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "7", Name: "port", Label: "Port the picker", Title: "Port the picker",
		Status: string(session.TaskRunning), SessionID: "aaaa000000000002",
	})
	lab.presence("-tmp-alpha", "aaaa000000000002", session.PresenceWorking, "", now.Add(-time.Hour),
		session.PresenceTask{
			ID: "7", Title: "Port the picker", State: "running",
			StartedAt: now.Add(-70 * time.Minute), Phase: session.TaskPhaseChecking,
		})

	a := lab.app(mine)
	a.openHome()
	card := homeCardFor(t, a, other)
	drawn := strings.Join(card, "\n")
	if strings.Contains(ansi.Strip(drawn), taskCheckingWord) {
		t.Fatalf("a stale claim was drawn as a life the work is in right now:\n%s", drawn)
	}
	if cardLine(card, taskRecordStoppedWord) < 0 {
		t.Fatalf("the stale row does not say %q, which is the word it always said:\n%s", taskRecordStoppedWord, drawn)
	}
}
