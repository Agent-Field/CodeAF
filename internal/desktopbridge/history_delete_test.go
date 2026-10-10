package desktopbridge

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/remote"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func useTrash(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "trash")
	old := trashRoot
	trashRoot = func() string { return dir }
	t.Cleanup(func() { trashRoot = old })
	return dir
}

func postDelete(b *Bridge, path, body string) (int, map[string]any) {
	w := request(b, "POST", "/api/engine"+path, body)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestDeleteRefusesUnarchived(t *testing.T) {
	useTrash(t)
	b, root := historyFixture(t)
	code, _ := postDelete(b, "/history/delete", `{"ids":["cccc000000000003","aaaa000000000001"]}`)
	if code != 409 {
		t.Fatalf("code = %d, want 409", code)
	}
	// The whole request is refused, so the archived one stays too.
	for _, id := range []string{"aaaa000000000001", "cccc000000000003"} {
		if _, err := os.Stat(filepath.Join(root, "-work-lexer", id)); err != nil {
			t.Fatalf("%s moved: %v", id, err)
		}
	}
}

func TestDeleteThenRestoreRoundTrips(t *testing.T) {
	trash := useTrash(t)
	b, root := historyFixture(t)
	folder := filepath.Join(root, "-work-lexer", "cccc000000000003")
	code, out := postDelete(b, "/history/delete", `{"ids":["cccc000000000003"]}`)
	token, _ := out["undoToken"].(string)
	if code != 200 || out["deleted"] != float64(1) || token == "" {
		t.Fatalf("delete = %d %v", code, out)
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatal("the folder is still in place")
	}
	if _, err := os.Stat(filepath.Join(trash, token, "cccc000000000003", "meta.json")); err != nil {
		t.Fatalf("not in the trash: %v", err)
	}
	var list HistoryList
	getJSON(t, b, "/history", &list)
	if list.Total != 3 {
		t.Fatalf("total after delete = %d", list.Total)
	}
	code, out = postDelete(b, "/history/restore", `{"undoToken":"`+token+`"}`)
	if code != 200 || out["restored"] != float64(1) {
		t.Fatalf("restore = %d %v", code, out)
	}
	getJSON(t, b, "/history", &list)
	if list.Total != 4 || !list.Items[2].Archived {
		t.Fatalf("after restore: total %d", list.Total)
	}
	if code, _ = postDelete(b, "/history/restore", `{"undoToken":"../../etc"}`); code != 400 {
		t.Fatalf("a path-shaped token = %d", code)
	}
}

func TestTrashPurgedOnStart(t *testing.T) {
	trash := useTrash(t)
	old := time.Now().UTC().Add(-11*time.Minute).Format(trashStampLayout) + "-aaaaaaaa"
	fresh := time.Now().UTC().Add(-time.Minute).Format(trashStampLayout) + "-bbbbbbbb"
	for _, name := range []string{old, fresh} {
		if err := os.MkdirAll(filepath.Join(trash, name, "x"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	historyFixture(t) // attaching a history is the start
	if _, err := os.Stat(filepath.Join(trash, old)); !os.IsNotExist(err) {
		t.Fatal("an old entry survived the start")
	}
	if _, err := os.Stat(filepath.Join(trash, fresh)); err != nil {
		t.Fatal("a fresh entry was purged")
	}
}

func TestDeleteRefusesAttached(t *testing.T) {
	useTrash(t)
	b, root := historyFixture(t)
	dir := filepath.Join(root, "-work-lexer", "cccc000000000003")
	b.sessions = map[string]*conversation{"x": attachedTo(filepath.Join(dir, "transcript.jsonl"))}
	code, _ := postDelete(b, "/history/delete", `{"ids":["cccc000000000003"]}`)
	if code != 409 {
		t.Fatalf("code = %d, want 409", code)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("the attached conversation moved")
	}
}

func attachedTo(file string) *conversation {
	return &conversation{done: make(chan struct{}), conn: Connection{Close: func() {}, Welcome: remote.Welcome{SessionFile: file}}}
}
