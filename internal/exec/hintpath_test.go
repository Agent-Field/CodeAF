package exec

import (
	"os"
	"path/filepath"
	"testing"
)

// An output hint is an invitation to write a FILE at a name. A directory
// already standing at that name makes it unfulfillable, and the caller that
// offers hints has to be able to ask — a leaf handed one anyway wrote its
// deliverable inside the directory, reported the file written, and left the
// asked-for file nowhere on disk.
func TestADirectoryAtAHintPathIsVisibleToWhoeverOffersIt(t *testing.T) {
	root := t.TempDir()
	space, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	hint := SuggestPathFor("job-n2", "write docs/decision-memo.md, a rigorous")
	if space.DirectoryAt(hint) {
		t.Fatalf("nothing is at %q and it reads as a directory", hint)
	}
	// Nothing may have brought it into existence in the asking, either: an
	// offered address the offer itself creates is one the next leaf has to
	// write around.
	if _, err := os.Stat(filepath.Join(root, hint)); !os.IsNotExist(err) {
		t.Fatalf("the hint path exists after being asked about: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, hint), 0o755); err != nil {
		t.Fatal(err)
	}
	if !space.DirectoryAt(hint) {
		t.Fatalf("a directory at %q is invisible to the caller that offers it", hint)
	}
	// A file at the name is exactly what the hint invites, and is not this.
	file := "answer.md"
	if err := os.WriteFile(filepath.Join(root, file), []byte("the answer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if space.DirectoryAt(file) {
		t.Fatal("a written file reads as a directory")
	}
}
