package desktopbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// BE-FILE-07: Shell 3e draws a viewer and a handoff to an editor. A write,
// rename, delete, or move from the file tab is not a route, so the request
// stays an unknown action and the bytes on disk are the witness that nothing
// was changed through this door.
func TestTheDesktopFileTabDoesNotWriteRenameOrDelete(t *testing.T) {
	for _, route := range bridgeRoutes {
		for _, tail := range []string{"/files/write", "/files/rename", "/files/delete", "/files/move"} {
			if strings.Contains(route, tail) {
				t.Errorf("file mutation route %s is registered; BE-FILE-07 ships none", route)
			}
		}
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{
		"path": target,
		"text": "changed",
		"to":   filepath.Join(dir, "other.txt"),
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _, id := fixture(t)
	for _, route := range []string{"/files/write", "/files/rename", "/files/delete", "/files/move"} {
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
			w := request(b, method, "/api/engine/sessions/"+id+route, string(body))
			if w.Code != 404 || !strings.Contains(w.Body.String(), "unknown engine action") {
				t.Errorf("%s %s status=%d body=%s", method, route, w.Code, w.Body.String())
			}
		}
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep" {
		t.Fatalf("file changed to %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "other.txt")); !os.IsNotExist(err) {
		t.Fatal("a rename target appeared")
	}
}
