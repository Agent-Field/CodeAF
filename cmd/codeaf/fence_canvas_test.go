package main

import (
	"path/filepath"
	"testing"
)

// THE FENCE FINDS THE APP'S CANVAS FOLDER THE WAY THE APP DOES: its variable,
// else the Mac's Application Support, else the XDG data folder.
func TestDeskCanvasRootIsTheAppsOwn(t *testing.T) {
	env := func(pairs map[string]string) func(string) string {
		return func(k string) string { return pairs[k] }
	}
	cases := []struct {
		name string
		env  map[string]string
		goos string
		want string
	}{
		{"set", map[string]string{"WORKAF_CANVAS_ROOT": "/data/canvases"}, "darwin", "/data/canvases"},
		{"mac", nil, "darwin", filepath.Join("/Users/p", "Library", "Application Support", "WorkAF", "canvases")},
		{"xdg", map[string]string{"XDG_DATA_HOME": "/x/data"}, "linux", "/x/data/workaf/canvases"},
		{"xdg relative", map[string]string{"XDG_DATA_HOME": "data"}, "linux", "/Users/p/.local/share/workaf/canvases"},
		{"linux", nil, "linux", "/Users/p/.local/share/workaf/canvases"},
	}
	for _, c := range cases {
		if got := deskCanvasRoot(env(c.env), "/Users/p", c.goos); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if got := deskCanvasRoot(env(nil), "", "linux"); got != "" {
		t.Errorf("no home: %q", got)
	}
}
