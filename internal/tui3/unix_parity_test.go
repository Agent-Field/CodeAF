package tui3

import (
	"runtime"
	"testing"
)

// Unix parity contract 2: Unix folder browsing retains the typed parent spelling.
func TestUnixParityFolderParent(t *testing.T) {
	got, leaf := pathHead("./folder//child")
	want := "./folder/"
	if runtime.GOOS == "windows" {
		want = "folder"
	}
	if got != want || leaf != "child" {
		t.Fatalf("parent = %q, %q; want %q, child", got, leaf, want)
	}
	if got := sameFolderPath("./folder", "folder"); got != (runtime.GOOS == "windows") {
		t.Fatalf("folder spelling equality = %v", got)
	}
}
