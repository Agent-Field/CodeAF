package diskspace

import (
	"path/filepath"
	"testing"
)

// A folder's filesystem answers its size and what is free on it; a path that
// does not exist yet answers for the nearest folder above it that does.
func TestOfAnswersTheFilesystemAPathIsOn(t *testing.T) {
	dir := t.TempDir()
	got, err := Of(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalBytes == 0 || got.FreeBytes > got.TotalBytes {
		t.Fatalf("the filesystem reads %+v", got)
	}
	below, err := Of(filepath.Join(dir, "not", "made", "yet"))
	if err != nil || below.TotalBytes != got.TotalBytes {
		t.Fatalf("a path not made yet reads %+v, %v", below, err)
	}
	if _, err := Of(""); err == nil {
		t.Fatal("an empty path was answered")
	}
}
