package tui3

import (
	"strings"
	"testing"
)

// The journal is a worker inspecting cancellation, with optional earlier user
// context or a later step. The tool id survives replays and output growth.
func compactReadingJournal(t *testing.T, prefix bool, frontier string) string {
	t.Helper()
	lines := []string{`{"type":"message","role":"user","content":"Fix cancellation without losing pending parser requests."}`}
	if prefix {
		lines = append(lines, `{"type":"message","role":"user","content":"Preserve the public API while changing the worker."}`)
	}
	lines = append(lines,
		`{"type":"message","role":"assistant","content":"Checking cancellation in the worker.","toolCalls":[{"id":"worker-read","function":{"name":"read","arguments":"{\"path\":\"worker.go\"}"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"worker-read","content":"pending requests belong to the worker"}`)
	if frontier != "same" {
		lines = append(lines, `{"type":"message","role":"assistant","content":"Cancellation now resolves pending parser requests."}`)
	}
	if frontier == "next" {
		lines = append(lines,
			`{"type":"message","role":"assistant","content":"Checking retry after cancellation.","toolCalls":[{"id":"retry-read","function":{"name":"read","arguments":"{\"path\":\"retry.go\"}"}}]}`,
			`{"type":"message","role":"tool","toolCallId":"retry-read","content":"retry starts a new worker"}`)
	}
	return roomJournal(t, lines...)
}

func TestTaskReadingRestoresOnlyTheSameCompactLiveWork(t *testing.T) {
	for _, frontier := range []string{"same", "next", "done"} {
		t.Run(frontier, func(t *testing.T) {
			a, fake, _ := roomApp(t)
			fake.journal = compactReadingJournal(t, false, "same")
			a.openRoom(7, "Repair cancellation")
			_ = roomText(a)
			if strings.Contains(roomText(a), "read worker.go") {
				t.Fatal("the task's work did not start compact")
			}
			if !a.toggleLatestWorkfold() || !a.room.workOpen[0] {
				t.Fatal("the newest disclosure did not open the live task work")
			}
			if !strings.Contains(roomText(a), "worker.go") {
				t.Fatal("opening the work did not expose its call")
			}
			a.closeRoom()
			fake.journal = compactReadingJournal(t, true, frontier)
			a.openRoom(7, "Repair cancellation")
			if frontier == "done" {
				a.room.setDone(true)
			}
			_ = roomText(a)
			if got, want := a.room.workOpen[0], frontier == "same"; got != want {
				t.Fatalf("restored live expansion = %v, want %v for %s frontier", got, want, frontier)
			}
			for key, open := range a.room.workOpen {
				if key != 0 && open {
					t.Fatalf("live bookmark expanded settled phase %d", key)
				}
			}
		})
	}
}

func TestHostedTaskReadingUsesReportedActivityWithoutLocalLane(t *testing.T) {
	for _, state := range []string{"running", "done", "failed read", "lost guest"} {
		t.Run(state, func(t *testing.T) {
			a, fake, _ := roomApp(t)
			fake.journal = compactReadingJournal(t, false, "same")
			a.openRoom(7, "Repair cancellation")
			r := a.room
			if r.stop != nil {
				r.stop()
				r.stop = nil
			}
			r.lane = nil
			switch state {
			case "done":
				r.setDone(true)
			case "failed read":
				r.readFailed = true
			case "lost guest":
				r.guest = &taskGuest{session: a.file, lost: true}
			}
			if got, want := r.deck().runningTurn != 0, state == "running"; got != want {
				t.Fatalf("reported activity = %v, want %v for %s", got, want, state)
			}
			if state == "running" {
				if !a.toggleLatestWorkfold() || !r.workOpen[0] {
					t.Fatal("hosted transcript has no compact live disclosure")
				}
			}
		})
	}
}
