package tui3

import (
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestLiveNodeResultShowsItsReportAndFiles(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		name := "before index refresh"
		if indexed {
			name = "with older index row"
		}
		t.Run(name, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			a.homeRoot = t.TempDir()
			a.file = a.homeRoot + "/review-chat/transcript.jsonl"
			a.width, a.height = 160, 80
			a.taskUpdate(update(7, "Review the pull request diff", session.TaskRunning, session.TaskNotice{}))
			if indexed {
				a.comp.tasks = []session.TaskIndexEntry{{ID: "7", SessionID: "review-chat", Title: "Review the pull request diff", Status: string(session.TaskRunning)}}
			}
			const report = "incomplete — you said it is not finished\n\nThe original reason: nobody could check it.\nKeep the parser changes for another look."
			files := []string{"parser.go", "parser_test.go"}
			a.taskUpdate(update(7, "Review the pull request diff", session.TaskFailed, session.TaskNotice{
				Ending: session.TaskEndingRefused, Report: report, Changed: files, Merge: "inplace",
			}))
			// Open the same result door as a settled conversation row, before an index reload.
			drain(t, a, a.openWorkCompletion(&workCompletion{task: 7}))
			if !a.taskSheet.detailOn {
				t.Fatal("open result did not open the viewer")
			}
			lines, _, _, _ := a.taskCardFrame(160, 80)
			screen := plain(strings.Join(lines, "\n"))
			for _, want := range []string{"you said it is not finished", "nobody could check it", "Keep the parser changes for another look.", "2 files changed"} {
				if !strings.Contains(screen, want) {
					t.Errorf("live result lacks %q:\n%s", want, screen)
				}
			}
			entry := a.currentTaskEntry(a.taskSheet.detail)
			if !slices.Equal(entry.Files, files) {
				t.Errorf("result files = %v, want %v", entry.Files, files)
			}
		})
	}
}
