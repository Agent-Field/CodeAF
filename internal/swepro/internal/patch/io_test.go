package patch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyPatchAndAffectedPaths(t *testing.T) {
	root := t.TempDir()
	add := filepath.Join(root, "nested", "add.txt")
	update := filepath.Join(root, "update.txt")
	deletePath := filepath.Join(root, "delete.txt")
	if err := os.WriteFile(update, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deletePath, []byte("gone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patchText := "*** Begin Patch\n" +
		"*** Add File: " + add + "\n+x\n" +
		"*** Update File: " + update + "\n@@\n-old\n+new\n" +
		"*** Delete File: " + deletePath + "\n" +
		"*** End Patch"
	affected, err := ApplyPatch(patchText)
	if err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}
	if len(affected.Added) != 1 || affected.Added[0] != add ||
		len(affected.Modified) != 1 || affected.Modified[0] != update ||
		len(affected.Deleted) != 1 || affected.Deleted[0] != deletePath {
		t.Fatalf("affected = %#v", affected)
	}
	data, err := os.ReadFile(add)
	if err != nil || string(data) != "x" {
		t.Fatalf("add content=%q err=%v", data, err)
	}
	data, err = os.ReadFile(update)
	if err != nil || string(data) != "new\n" {
		t.Fatalf("update content=%q err=%v", data, err)
	}
}

func TestMaybeParseApplyPatchVerified(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patchText := "*** Begin Patch\n*** Update File: file.txt\n@@\n-old\n+new\n*** End Patch"
	result := MaybeParseApplyPatchVerified([]string{"apply_patch", patchText}, root)
	if result.Type != VerifiedBody || result.Action == nil {
		t.Fatalf("result = %#v", result)
	}
	change, ok := result.Action.Changes.Get(path)
	if !ok || change.Type != "update" || change.NewContent != "new\n" {
		t.Fatalf("change = %#v, ok=%v", change, ok)
	}
	implicit := MaybeParseApplyPatchVerified([]string{patchText}, root)
	if implicit.Type != VerifiedCorrectnessError || implicit.Err == nil || implicit.Err.Error() != ErrorImplicitInvocation {
		t.Fatalf("implicit = %#v", implicit)
	}
}

func TestApplyHunksRejectsEmpty(t *testing.T) {
	_, err := ApplyHunksToFiles(nil)
	if err == nil || err.Error() != "No files were modified." {
		t.Fatalf("error = %v", err)
	}
}
