package tui3

import (
	"strconv"
	"strings"
	"testing"
)

// A human correction and a completed response are conversation, even when
// asynchronous work resumes on either side of them. Exercise the same deck
// renderer used by chat, task pages and nested transcripts at narrow widths.
func TestCleanChatSteeringAndInterimReplySurviveResumedWork(t *testing.T) {
	for _, width := range []int{32, 100} {
		for _, surface := range []struct {
			name string
			lens lens
		}{
			{"chat", participantLens}, {"task", overseerLens}, {"nested", transcriptLens},
		} {
			t.Run(surface.name+strconv.Itoa(width), func(t *testing.T) {
				a := liveStepsApp(t)
				es := liveStepsFixture()
				es[9].status = toolOK
				es = append(es,
					entry{kind: entryAssistant, text: "**First result is ready.**", turn: 1, settled: true, confirmed: &responseConfirmation{done: true}},
					entry{kind: entrySteer, turn: 1, steer: &steerElbow{id: 1, words: "Use the second option", consumed: true}},
					entry{kind: entryNote, turn: 1, text: "INTERNAL HOUSEKEEPING", settled: true},
					entry{kind: entryAssistant, turn: 1, text: "Checking the corrected option.", settled: true},
					entry{kind: entryTool, turn: 1, tool: "bash", status: toolRunning, detail: toolDetail{Output: "PRIVATE OUTPUT"}},
				)
				d := deck{entries: es, lens: surface.lens, runningTurn: 1}
				rendered, _ := a.deckRows(d, width)
				var lines []string
				activity := 0
				for _, r := range rendered {
					lines = append(lines, plain(r.text))
					if r.hit == hitWorkFold {
						activity++
					}
				}
				page := strings.Join(lines, "\n")
				for _, want := range []string{"First result is ready.", "Use the second option"} {
					if !strings.Contains(page, want) {
						t.Fatalf("lost human conversation %q:\n%s", want, page)
					}
				}
				for _, hidden := range []string{"INTERNAL HOUSEKEEPING", "PRIVATE OUTPUT", "**First", "Searching the tree for the caller"} {
					if strings.Contains(page, hidden) {
						t.Fatalf("leaked old work or markup %q:\n%s", hidden, page)
					}
				}
				if activity > liveStepRows {
					t.Fatalf("%d activity rows exceed shared budget:\n%s", activity, page)
				}
			})
		}
	}
}

func TestCleanChatSettledToolOnlyTailDoesNotLeakAcrossViews(t *testing.T) {
	for _, surface := range []struct {
		name string
		lens lens
	}{
		{"chat", participantLens}, {"task", overseerLens}, {"nested", transcriptLens},
	} {
		t.Run(surface.name, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			es := []entry{
				{kind: entryUser, text: "Check the service", turn: 1},
				{kind: entryAssistant, text: "Inspecting the service.", turn: 1, settled: true},
				{kind: entryTool, tool: "bash", turn: 1, status: toolFailed, detail: toolDetail{Output: "PRIVATE FAILURE TRACEBACK"}, open: true},
			}
			rs, _ := a.deckRows(deck{entries: es, lens: surface.lens}, 100)
			var lines []string
			for _, r := range rs {
				lines = append(lines, plain(r.text))
			}
			if page := strings.Join(lines, "\n"); strings.Contains(page, "PRIVATE FAILURE TRACEBACK") {
				t.Fatalf("settled failure leaked without disclosure:\n%s", page)
			}
		})
	}
}
