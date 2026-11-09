package cellstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// TestAFailedCaptureOfAFolderThatIsGoneIsNotAFailedSave: a job closed
// locally takes its checkout away while its chat is open, the chat's next
// capture fails, and the quit's last flush must not report that chat as one
// that could not be saved (the ninth live WorkAF round's idle quit, exit 1).
// A failure of a folder that is still there is reported as before.
func TestAFailedCaptureOfAFolderThatIsGoneIsNotAFailedSave(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cell")
	gone := false
	state := &captureState{done: make(chan struct{}), gone: func() bool { return gone }}
	registerCapture(root, state)
	t.Cleanup(func() {
		captures.Lock()
		delete(captures.cells, root)
		captures.Unlock()
	})
	state.err = errors.New("plan capture: engine capture-plan: No such file or directory (os error 2)")
	completeCapture(root, state)
	if err := WaitCapture(context.Background(), cell.Cell{Root: root}); err == nil {
		t.Fatal("a failed capture of a folder that is still there was not reported")
	}
	gone = true
	if err := WaitCapture(context.Background(), cell.Cell{Root: root}); err != nil {
		t.Fatalf("a failed capture of a folder that is gone was reported: %v", err)
	}
}

// TestASealOfAFolderThatIsGoneSaysSo: the seal refuses with
// [ErrWorkspaceGone] before it reaches the engine, and the recorder does not
// report it.
func TestASealOfAFolderThatIsGoneSaysSo(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "checkout")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	c := cell.Cell{ID: "01TESTGONE", Root: filepath.Join(t.TempDir(), "01TESTGONE")}
	engine := Engine{Workspace: workspace}
	if engine.workspace(c) == "" {
		t.Skip("this folder is not one the engine seals")
	}
	if err := engine.workspaceThere(c); err != nil {
		t.Fatalf("a folder that is there reads %v", err)
	}
	if err := os.RemoveAll(workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Seal(context.Background(), c, TurnInfo{}); !errors.Is(err, ErrWorkspaceGone) {
		t.Fatalf("a seal of a folder that is gone = %v, want ErrWorkspaceGone", err)
	}
	reported := 0
	r := &Recorder{cell: c, opts: Options{Report: func(error) { reported++ }}}
	r.report(errors.Join(errors.New("seal"), ErrWorkspaceGone))
	if reported != 0 || r.lastError != nil {
		t.Fatalf("a gone folder was reported (%d reports, last %v)", reported, r.lastError)
	}
}
