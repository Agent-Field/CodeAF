//go:build !windows

package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// The plain-folder recorder knows the changed paths but deliberately supplies
// no patch text. The ending must not turn that missing measurement into zero.
func TestPlainFolderCandidateDescriptionOmitsAbsentPatchBytes(t *testing.T) {
	workspace, recorder := snapshotWorkspace(t, map[string]string{"first.txt": "before\n"})
	base, err := recorder.Base(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.Record(base, "starting tree"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first.txt", "second.txt", "third.txt"} {
		if err := writeFile(filepath.Join(workspace, name), "after\n"); err != nil {
			t.Fatal(err)
		}
	}
	change, err := recorder.Change(base)
	if err != nil {
		t.Fatal(err)
	}
	if !change.changed || change.files != 3 || change.patch != "" {
		t.Fatalf("the real snapshot recorder supplied an unexpected change: %+v", change)
	}
	candidate := &frozenCandidate{TreeSHA: change.treeSHA, PatchFiles: change.files, PatchBytes: len(change.patch)}
	description := candidate.describe()
	if strings.Contains(description, "bytes") || !strings.Contains(description, "across 3 file(s), tree "+shortSHA(change.treeSHA)) {
		t.Fatalf("the plain-folder description invents a patch size: %q", description)
	}
}

func TestCandidateDescriptionKeepsPositivePatchBytes(t *testing.T) {
	candidate := &frozenCandidate{TreeSHA: "123456789abcdef", PatchFiles: 3, PatchBytes: 128}
	if got, want := candidate.describe(), "128 bytes across 3 file(s), tree 123456789abc"; got != want {
		t.Fatalf("the measured patch description is %q, want %q", got, want)
	}
	var absent *frozenCandidate
	if got := absent.describe(); got != "nothing frozen" {
		t.Fatalf("an absent candidate reads %q", got)
	}
}
