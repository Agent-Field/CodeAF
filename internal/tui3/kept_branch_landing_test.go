package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestKeptBranchLandingNamesTheBranchAndMergedWorkStaysQuiet(t *testing.T) {
	for _, merge := range []string{mergeWordKept, mergeWordMerged} {
		t.Run(merge, func(t *testing.T) {
			a, _, _ := taskApp(t)
			drive(t, a, streamEventMsg{gen: a.gen, ev: update(91, "Write the guide", session.TaskDone, session.TaskNotice{Branch: "task/exact-kept-guide", Merge: merge, Report: "Guide written"})})
			got := taskText(a)
			if merge == mergeWordKept && !strings.Contains(got, "branch kept · task/exact-kept-guide") {
				t.Fatalf("landing hid kept branch:\n%s", got)
			}
			if merge == mergeWordMerged && strings.Contains(got, "Write the guide") {
				t.Fatalf("routine landing became loud:\n%s", got)
			}
		})
	}
}
