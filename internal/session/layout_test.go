package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLayoutFollowsTheFolderShape(t *testing.T) {
	root := t.TempDir()
	flat, cellDir := filepath.Join(root, "a"), filepath.Join(root, "b")
	if err := os.MkdirAll(filepath.Join(cellDir, ".cell"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(flat, 0o700); err != nil {
		t.Fatal(err)
	}
	for dir, want := range map[string]string{
		flat:    filepath.Join(flat, "transcript.jsonl"),
		cellDir: filepath.Join(cellDir, ".cell", "transcript.jsonl"),
	} {
		got := Place{Dir: dir}.Transcript()
		if got != want {
			t.Fatalf("Transcript = %s, want %s", got, want)
		}
		if folder, ok := FolderOf(got); !ok || folder != dir {
			t.Fatalf("FolderOf(%s) = %q %v", got, folder, ok)
		}
		if BucketOf(got) != root || DirOf(got) != dir {
			t.Fatalf("bucket/dir of %s wrong", got)
		}
	}
	if _, ok := FolderOf(filepath.Join(root, "flat.jsonl")); ok {
		t.Fatal("a flat file has no folder")
	}
	if (Place{}).Transcript() != "" {
		t.Fatal("zero Place must stay legacy and answer empty")
	}
}
