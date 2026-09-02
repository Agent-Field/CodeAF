package main

import (
	"path/filepath"
	"testing"
)

// testWindow opens one store the way an errand opens it, in a directory the
// test owns and closes behind itself.
func testWindow(t *testing.T, root string) *chatWindow {
	t.Helper()
	window, err := openChatWindow(filepath.Join(root, "graph.db"), filepath.Join(root, "graph.db"), "new")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(window.close)
	return window
}
