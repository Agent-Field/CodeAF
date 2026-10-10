package placegraph

import (
	"os"
	"path/filepath"
	"testing"
)

// folderSource builds a source the way the door that adds one does (NewSource), with no deny list.
func folderSource(t *testing.T, kind SourceKind, path string) Source {
	t.Helper()
	src, err := NewSource(kind, path, AddedByYou, noDeny)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func TestANewChatWorksInThePlacesFirstFolderInTheOrderItWasListed(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	os.MkdirAll(first, 0o755)
	os.MkdirAll(second, 0o755)
	s, _ := newStore(t)
	p := placeWith(t, s, "Lexer", Context{Sources: []Source{
		{ID: "page", Kind: SourceURL, Ref: "https://example.com/spec", AddedBy: AddedByYou},
		folderSource(t, SourceFolder, first), folderSource(t, SourceFolder, second)}}, Policy{})
	want, _ := filepath.EvalSymlinks(first)
	got := snap(t, s).WorkingFolder(p.ID, noDeny)
	if got.Path != want || len(got.Skipped) != 0 || !got.Listed() {
		t.Fatalf("working folder = %+v, want %s", got, want)
	}
}

func TestAMissingFirstFolderIsSkippedWithItsReasonAndTheNextIsUsed(t *testing.T) {
	root := t.TempDir()
	gone, there := filepath.Join(root, "gone"), filepath.Join(root, "there")
	os.MkdirAll(gone, 0o755)
	os.MkdirAll(filepath.Join(there, ".git"), 0o755)
	os.WriteFile(filepath.Join(there, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o600)
	s, _ := newStore(t)
	p := placeWith(t, s, "Lexer", Context{Sources: []Source{folderSource(t, SourceFolder, gone), folderSource(t, SourceRepo, there)}}, Policy{})
	os.Remove(gone)
	got := snap(t, s).WorkingFolder(p.ID, noDeny)
	if want, _ := filepath.EvalSymlinks(there); got.Path != want || len(got.Skipped) != 1 || got.Skipped[0].Reason != "not on this disk right now" {
		t.Fatalf("working folder = %+v", got)
	}
}

func TestASymlinkedSourceResolvesToTheRealDirectoryAndADeniedTargetIsRefusedEvenThroughALink(t *testing.T) {
	root := t.TempDir()
	real, secrets := filepath.Join(root, "real"), filepath.Join(root, "secrets")
	os.MkdirAll(real, 0o755)
	os.MkdirAll(secrets, 0o755)
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks here")
	}
	s, _ := newStore(t)
	// A source stored as a link spelling (older record) still resolves to the real folder.
	p := placeWith(t, s, "A", Context{Sources: []Source{{ID: "s1", Kind: SourceFolder, Ref: link, AddedBy: AddedByYou}}}, Policy{})
	wantReal, _ := filepath.EvalSymlinks(real)
	if got := snap(t, s).WorkingFolder(p.ID, noDeny); got.Path != wantReal {
		t.Fatalf("a link did not resolve to the real folder: %+v", got)
	}
	// A link that was re-pointed into a denied folder after it was added is refused, and the fallback is
	// reported with the reason, not a silent next choice.
	denied := SourcePolicy{Deny: []string{secrets}}
	os.Remove(link)
	os.Symlink(secrets, link)
	got := snap(t, s).WorkingFolder(p.ID, denied)
	if got.Path != "" || len(got.Skipped) != 1 || got.Skipped[0].Reason == "" {
		t.Fatalf("a link into a denied folder was accepted: %+v", got)
	}
}

func TestAFolderTheAccountCannotEnterIsSkippedAndAFilesystemRootNeverQualifies(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root enters every folder")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	os.MkdirAll(locked, 0o755)
	s, _ := newStore(t)
	p := placeWith(t, s, "A", Context{Sources: []Source{folderSource(t, SourceFolder, locked), {ID: "top", Kind: SourceFolder, Ref: string(os.PathSeparator), AddedBy: AddedByYou}}}, Policy{})
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	got := snap(t, s).WorkingFolder(p.ID, noDeny)
	if got.Path != "" || len(got.Skipped) != 2 {
		t.Fatalf("working folder = %+v", got)
	}
	if got.Skipped[0].Reason != "this account cannot enter that folder" && got.Skipped[0].Reason != "this account cannot read that folder" {
		t.Fatalf("permission reason = %q", got.Skipped[0].Reason)
	}
	if got.Skipped[1].Reason != "the top of a disk is not a working folder" {
		t.Fatalf("root reason = %q", got.Skipped[1].Reason)
	}
}

func TestAFolderInsideARepositoryStaysThatFolder(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o600)
	sub := filepath.Join(root, "pkg")
	os.MkdirAll(sub, 0o755)
	s, _ := newStore(t)
	p := placeWith(t, s, "Lexer", Context{Sources: []Source{folderSource(t, SourceFolder, sub)}}, Policy{})
	want, _ := filepath.EvalSymlinks(sub)
	got := snap(t, s).WorkingFolder(p.ID, noDeny)
	if got.Path != want || len(got.Skipped) != 0 {
		t.Fatalf("working folder climbed out of the listed folder: %+v, want %s", got, want)
	}
}

func TestUsableDirectoryRefusesARelativePathAFileAndTheTopOfADisk(t *testing.T) {
	file := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(file, []byte("x"), 0o600)
	for _, ref := range []string{"pkg", file, string(os.PathSeparator)} {
		if path, reason := UsableDirectory(ref, noDeny); path != "" || reason == "" {
			t.Fatalf("%q usable as %q (%s)", ref, path, reason)
		}
	}
}

func TestAPlaceWithNoFolderSourceHasNothingToSayAndAncestorsDoNotLendOne(t *testing.T) {
	dir := t.TempDir()
	s, _ := newStore(t)
	parent := placeWith(t, s, "Parent", Context{Sources: []Source{folderSource(t, SourceFolder, dir)}}, Policy{})
	child := placeWith(t, s, "Child", Context{Sources: []Source{{ID: "page", Kind: SourceURL, Ref: "https://example.com", AddedBy: AddedByYou}}}, Policy{}, parent.ID)
	got := snap(t, s).WorkingFolder(child.ID, noDeny)
	if got.Path != "" || got.Listed() {
		t.Fatalf("a child borrowed its parent's folder or spoke without a folder: %+v", got)
	}
	if unknown := snap(t, s).WorkingFolder("nope", noDeny); unknown.Path != "" || unknown.Listed() {
		t.Fatal("an unknown place answered something")
	}
}
