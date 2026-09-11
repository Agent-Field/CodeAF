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

// The wire can split narration around reasoning while the journal stores one
// assistant message. Reopening must follow the stable call, not its prose head.
func TestTaskReadingKeepsLiveAndCaptionChoicesWhenReplayJoinsNarration(t *testing.T) {
	for _, captionOpen := range []bool{false, true} {
		t.Run(map[bool]string{false: "caption closed", true: "caption open"}[captionOpen], func(t *testing.T) {
			a, fake, _ := roomApp(t)
			fake.journal = roomJournal(t,
				`{"type":"message","role":"user","content":"Compare the parser contracts."}`,
				`{"type":"message","role":"assistant","content":"Comparing parser contracts.The contracts agree. Reading compatibility notes.","toolCalls":[{"id":"compatibility-read","function":{"name":"read","arguments":"{\"path\":\"compatibility-notes.md\"}"}}]}`,
				`{"type":"message","role":"tool","toolCallId":"compatibility-read","content":"the public contracts match"}`)
			a.openRoom(7, "Compare parser contracts")
			a.room.entries = []entry{
				{kind: entryUser, text: "Compare the parser contracts.", turn: 1},
				{kind: entryAssistant, text: "Comparing parser contracts.", settled: true, turn: 1},
				{kind: entryThinking, text: "Check ownership transfer before compatibility.", settled: true, turn: 1},
				{kind: entryAssistant, text: "The contracts agree. Reading compatibility notes.", settled: true, turn: 1},
				{kind: entryTool, tool: "read", callID: "compatibility-read", text: "read compatibility-notes.md", status: toolRunning, turn: 1},
			}
			a.room.turn, a.room.dirty = 1, true
			if !a.toggleLatestWorkfold() || !a.room.workOpen[0] {
				t.Fatal("no live work to expand before leaving")
			}
			a.setCapOpen(a.room.deck(), 3, captionOpen)
			a.closeRoom()
			a.openRoom(7, "Compare parser contracts")
			_ = roomText(a)
			if !a.room.workOpen[0] {
				t.Fatal("coalesced narration lost expansion of the same live call")
			}
			captions := deriveCaptions(a.room.entries, a.room.turn)
			if len(captions) != 1 || captions[0].start == 3 {
				t.Fatalf("fixture did not coalesce the caption head: %#v", captions)
			}
			if got, set := a.room.capOpen[captions[0].start]; !set || got != captionOpen {
				t.Fatalf("caption choice after coalescence = %v (set %v), want %v", got, set, captionOpen)
			}
		})
	}
}
