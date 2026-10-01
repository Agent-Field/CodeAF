package session

import (
	"strings"
	"testing"
)

// The branch has no intermediate commits to protect the program's edits,
// so the brief must also steer it away from hiding or overwriting its work.
func TestTheBriefSteersAwayFromHidingOrOverwritingUncommittedWork(t *testing.T) {
	repo := newTestRepo(t)
	folder := prepareIn(t, notedProgram(), repo, "Commit, stash and push the fix")
	_, note, _ := strings.Cut(folder.BriefNote(), "Leave your work uncommitted")
	note = "Leave your work uncommitted" + strings.SplitN(note, ".", 2)[0]
	for _, want := range []string{"Leave your work uncommitted", "stash", "reset", "clean", "check out", "restore files over your work"} {
		if !strings.Contains(note, want) {
			t.Errorf("brief leaves uncommitted work exposed: missing %q in %q", want, note)
		}
	}
}
