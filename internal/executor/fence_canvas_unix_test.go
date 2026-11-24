//go:build linux || darwin

package executor

import (
	"os"
	"path/filepath"
	"testing"
)

// THE APP'S CANVAS FOLDER IS A FENCED RUN'S TO WRITE when it exists, so the
// `canvas` command a crew member types lands where the app shows it; a folder
// that does not exist is not made, and the rest of the home stays refused.
func TestAFencedRunWritesTheAppsCanvasFolder(t *testing.T) {
	requireFence(t)
	w := newFenceWorld(t, FenceAllow{})
	canvases := filepath.Join(w.home, "Library", "Application Support", "WorkAF", "canvases")
	if err := os.MkdirAll(canvases, 0o700); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(w.home, ".local", "share", "workaf", "canvases")
	f, err := NewFence(FenceOptions{Root: w.root, Home: w.home, CodeafHome: w.codeaf, StateDir: filepath.Dir(filepath.Dir(w.fence.Tmp)),
		Canvases: []string{canvases, missing}})
	if err != nil {
		t.Fatal(err)
	}
	w.fence = f
	if res := w.run(t, `mkdir -p "`+filepath.Join(canvases, "c1")+`" && echo ok > "`+filepath.Join(canvases, "c1", "canvas.db")+`"`); res.Exit != 0 {
		t.Fatalf("the app's canvas folder was refused: %s", res.Stderr)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Fatal("the fence made a canvas folder the app never made")
	}
	if res := w.run(t, `echo no > "`+filepath.Join(w.home, "Library", "elsewhere.txt")+`"`); res.Exit == 0 {
		t.Fatal("granting the canvas folder opened the rest of the home")
	}
}
