package tui3

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestWorkspacePickerAnchorsThroughWorkspaceDoor(t *testing.T) {
	a, root := folderLab(t)
	target := filepath.Join(root, "sibling")
	var anchored string
	a.anchorWorkspace = func(path string) (string, error) { anchored = path; return path, nil }
	a.slash("/workspace")
	if !a.folder.open || !a.folder.forWorkspace {
		t.Fatal("workspace picker did not open")
	}
	a.tookFolderStore(folderStoreMsg{})
	if !a.folder.forWorkspace {
		t.Fatal("late store changed picker purpose")
	}
	a.folder.browsing = false
	for i, hit := range a.folder.hits {
		if a.folder.all[hit].path == target {
			a.folder.cursor = i
			break
		}
	}
	if got := a.folder.actionWord(target, true); got != "set workspace · " {
		t.Fatalf("wrong action: %s", got)
	}
	a.folderConfirm()
	if anchored != target || a.workspace != target || a.anchorWorkspace != nil || a.folder.open {
		t.Fatalf("anchor=%q workspace=%q open=%v", anchored, a.workspace, a.folder.open)
	}
	a.slash("/workspace")
	if a.folder.open {
		t.Fatal("already anchored conversation offered another anchor")
	}
}

func TestWorkspacePickerRefusalKeepsChoice(t *testing.T) {
	a, _ := folderLab(t)
	a.anchorWorkspace = func(string) (string, error) { return "", errors.New("cannot anchor") }
	original := a.workspace
	a.slash("/workspace")
	a.folderConfirm()
	if !a.folder.open || a.workspace != original || a.anchorWorkspace == nil {
		t.Fatal("failed anchor changed workspace or discarded choice")
	}
	a.closeFolderSheet()
	if a.workspace != original {
		t.Fatal("cancel changed workspace")
	}
}
