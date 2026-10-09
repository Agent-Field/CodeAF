package workspacestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func open(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(Options{Dir: dir, WatchInterval: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// doc builds a valid shared document holding the given tab ids, in order.
func doc(ids ...string) json.RawMessage {
	tabs := make([]map[string]any, len(ids))
	for i, id := range ids {
		tabs[i] = map[string]any{"id": id, "title": "Tab " + id, "draft": "", "pinned": false, "kind": "conversation"}
	}
	data, _ := json.Marshal(map[string]any{"schema": 1, "tabs": tabs, "groups": []any{}, "closed": []any{}, "nextNumber": len(ids) + 1})
	return data
}

func tabIDs(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var d struct {
		Tabs []struct {
			ID string `json:"id"`
		} `json:"tabs"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, tab := range d.Tabs {
		ids = append(ids, tab.ID)
	}
	return ids
}

func TestARoundTripKeepsTheDocumentAndMovesTheRevision(t *testing.T) {
	s := open(t, t.TempDir())
	empty, err := s.Get("now")
	if err != nil || empty.Revision != 0 || empty.Workspace != nil {
		t.Fatalf("nothing saved should read as revision 0 and no workspace: %+v %v", empty, err)
	}
	rec, err := s.Put("now", 0, "w1", doc("a", "b"))
	if err != nil || rec.Revision != 1 || rec.Writer != "w1" {
		t.Fatalf("first write: %+v %v", rec, err)
	}
	got, err := s.Get("now")
	if err != nil || got.Revision != 1 || strings.Join(tabIDs(t, got.Workspace), ",") != "a,b" || got.UpdatedAt == nil {
		t.Fatalf("read back: %+v %v", got, err)
	}
	rec, err = s.Put("now", 1, "w1", doc("a", "b", "c"))
	if err != nil || rec.Revision != 2 {
		t.Fatalf("second write: %+v %v", rec, err)
	}
}

func TestAWriteFromAStaleRevisionIsRefusedWithTheCurrentDocument(t *testing.T) {
	s := open(t, t.TempDir())
	if _, err := s.Put("now", 0, "w1", doc("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put("now", 1, "w1", doc("a", "b")); err != nil {
		t.Fatal(err)
	}
	_, err := s.Put("now", 1, "w2", doc("a", "x"))
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("a stale write must be refused, got %v", err)
	}
	if conflict.Current.Revision != 2 || strings.Join(tabIDs(t, conflict.Current.Workspace), ",") != "a,b" {
		t.Fatalf("the refusal must carry the current document: %+v", conflict.Current)
	}
	// A write that assumes nothing exists is refused once something does.
	if _, err := s.Put("now", 0, "w3", doc("z")); !errors.As(err, &conflict) {
		t.Fatalf("revision 0 over a saved document must conflict, got %v", err)
	}
}

func TestAnIdenticalWriteDoesNotMoveTheRevision(t *testing.T) {
	s := open(t, t.TempDir())
	if _, err := s.Put("now", 0, "w1", doc("a")); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Put("now", 1, "w2", doc("a"))
	if err != nil || rec.Revision != 1 || rec.Writer != "w1" {
		t.Fatalf("an echo must leave the record as it was: %+v %v", rec, err)
	}
}

func TestKeysOutsideThePatternAreRefused(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	for _, key := range []string{"", "NOW", "../now", "now/../x", "pl_", "pl_0123", "pl_0123456789ABCDEF", "pl_0123456789abcdef0", "/etc/passwd", "now.json", "root", "_windows"} {
		if _, err := s.Get(key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Get(%q) = %v, want ErrInvalidKey", key, err)
		}
		if _, err := s.Put(key, 0, "w", doc("a")); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Put(%q) = %v, want ErrInvalidKey", key, err)
		}
	}
	if _, err := s.Put("pl_0123456789abcdef", 0, "w", doc("a")); err != nil {
		t.Fatalf("a place-graph id is a key: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		// The fixed pair lock is the one file no client input can name.
		if !strings.HasPrefix(e.Name(), "pl_0123456789abcdef.json") && e.Name() != pairLockName {
			t.Errorf("only the valid key may touch the disk, found %s", e.Name())
		}
	}
}

func TestOversizeNonObjectAndMalformedDocumentsAreRefused(t *testing.T) {
	s := open(t, t.TempDir())
	big := strings.Repeat("x", MaxDocumentBytes)
	cases := map[string]string{
		"not an object":    `[1,2]`,
		"null":             `null`,
		"no tabs":          `{"schema":1,"tabs":[],"groups":[],"closed":[],"nextNumber":1}`,
		"wrong schema":     `{"schema":2,"tabs":[{"id":"a","title":"","draft":"","pinned":false}],"groups":[],"closed":[],"nextNumber":1}`,
		"unknown field":    `{"schema":1,"tabs":[{"id":"a","title":"","draft":"","pinned":false}],"groups":[],"closed":[],"nextNumber":1,"path":"/etc"}`,
		"active is local":  `{"schema":1,"tabs":[{"id":"a","title":"","draft":"","pinned":false}],"groups":[],"closed":[],"nextNumber":1,"activeId":"a"}`,
		"recent is local":  `{"schema":1,"tabs":[{"id":"a","title":"","draft":"","pinned":false}],"groups":[],"closed":[],"nextNumber":1,"recentIds":["a"]}`,
		"split focus":      `{"schema":1,"tabs":[{"id":"s","title":"","draft":"","pinned":false,"split":{"layout":"1x2","focus":1,"panes":[{"id":"p","title":"","draft":""},{"id":"q","title":"","draft":""}]}}],"groups":[],"closed":[],"nextNumber":1}`,
		"duplicate id":     `{"schema":1,"tabs":[{"id":"a","title":"","draft":"","pinned":false},{"id":"a","title":"","draft":"","pinned":false}],"groups":[],"closed":[],"nextNumber":1}`,
		"pane id reused":   `{"schema":1,"tabs":[{"id":"a","title":"","draft":"","pinned":false},{"id":"s","title":"","draft":"","pinned":false,"split":{"layout":"1x2","panes":[{"id":"a","title":"","draft":""},{"id":"q","title":"","draft":""}]}}],"groups":[],"closed":[],"nextNumber":1}`,
		"open and closed":  `{"schema":1,"tabs":[{"id":"a","title":"","draft":"","pinned":false}],"groups":[],"closed":[{"id":"a","title":"","draft":"","pinned":false}],"nextNumber":1}`,
		"no pinned":        `{"schema":1,"tabs":[{"id":"a","title":"","draft":""}],"groups":[],"closed":[],"nextNumber":1}`,
		"control in id":    `{"schema":1,"tabs":[{"id":"a\u0000","title":"","draft":"","pinned":false}],"groups":[],"closed":[],"nextNumber":1}`,
		"five panes":       `{"schema":1,"tabs":[{"id":"s","title":"","draft":"","pinned":false,"split":{"panes":[{"id":"1","title":"","draft":""},{"id":"2","title":"","draft":""},{"id":"3","title":"","draft":""},{"id":"4","title":"","draft":""},{"id":"5","title":"","draft":""}]}}],"groups":[],"closed":[],"nextNumber":1}`,
		"duplicate group":  `{"schema":1,"tabs":[{"id":"a","title":"","draft":"","pinned":false}],"groups":[{"id":"g","title":"G","collapsed":false},{"id":"g","title":"H","collapsed":false}],"closed":[],"nextNumber":1}`,
		"bad next number":  `{"schema":1,"tabs":[{"id":"a","title":"","draft":"","pinned":false}],"groups":[],"closed":[],"nextNumber":0}`,
		"trailing garbage": `{"schema":1,"tabs":[{"id":"a","title":"","draft":"","pinned":false}],"groups":[],"closed":[],"nextNumber":1} {}`,
		"oversize":         `{"schema":1,"tabs":[{"id":"a","title":"","draft":"` + big + `","pinned":false}],"groups":[],"closed":[],"nextNumber":1}`,
	}
	for name, body := range cases {
		_, err := s.Put("now", 0, "w", json.RawMessage(body))
		if !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrTooLarge) {
			t.Errorf("%s: %v, want a refusal", name, err)
		}
	}
	if rec, _ := s.Get("now"); rec.Revision != 0 {
		t.Fatalf("a refused write must not reach the disk: %+v", rec)
	}
	if _, err := s.Put("now", 0, "bad writer!", doc("a")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a writer tag outside its pattern is refused: %v", err)
	}
}

func TestTooManyTabsAreRefused(t *testing.T) {
	s := open(t, t.TempDir())
	ids := make([]string, MaxTabs+1)
	for i := range ids {
		ids[i] = fmt.Sprint(i)
	}
	if _, err := s.Put("now", 0, "w", doc(ids...)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%d tabs must be refused: %v", len(ids), err)
	}
	if _, err := s.Put("now", 0, "w", doc(ids[:MaxTabs]...)); err != nil {
		t.Fatalf("%d tabs fit: %v", MaxTabs, err)
	}
}

// Two writers, each in its own Store as two processes would be, append their
// own tab through the compare-and-swap loop the renderer runs. Every append must
// land: a refused write is retried over the document it was refused with.
func TestTwoWritersNeverLoseAnUpdate(t *testing.T) {
	dir := t.TempDir()
	stores := []*Store{open(t, dir), open(t, dir)}
	if _, err := stores[0].Put("now", 0, "seed", doc("seed")); err != nil {
		t.Fatal(err)
	}
	const each = 25
	var wg sync.WaitGroup
	conflicts := make([]int, 2)
	for w := range stores {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			s := stores[w]
			for i := 0; i < each; i++ {
				id := fmt.Sprintf("w%d-%d", w, i)
				for {
					cur, err := s.Get("now")
					if err != nil {
						t.Error(err)
						return
					}
					ids := append(tabIDs(t, cur.Workspace), id)
					if _, err := s.Put("now", cur.Revision, fmt.Sprintf("w%d", w), doc(ids...)); err == nil {
						break
					} else if !errors.As(err, new(*ConflictError)) {
						t.Error(err)
						return
					}
					conflicts[w]++
				}
			}
		}(w)
	}
	wg.Wait()
	final, err := stores[0].Get("now")
	if err != nil {
		t.Fatal(err)
	}
	ids := tabIDs(t, final.Workspace)
	if len(ids) != 1+2*each {
		t.Fatalf("lost updates: %d tabs, want %d (conflicts seen %v)", len(ids), 1+2*each, conflicts)
	}
	if final.Revision != uint64(1+2*each) {
		t.Fatalf("revision %d, want one per write", final.Revision)
	}
}

func TestAReadOfADamagedFileChangesNothingAndTheNextWriteSetsItAside(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	path := filepath.Join(dir, "now.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Get("now")
	if err != nil || !rec.Damaged || rec.Revision != 0 {
		t.Fatalf("a damaged file reads as damaged, revision 0: %+v %v", rec, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("a read must not touch the disk, found %d entries", len(entries))
	}
	saved, err := s.Put("now", 0, "w", doc("a"))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision < 1_000_000 {
		t.Fatalf("a write over a damaged file starts its revision from the clock, got %d", saved.Revision)
	}
	matches, _ := filepath.Glob(path + ".damaged-*")
	if len(matches) != 1 {
		t.Fatalf("the damaged file is kept beside, found %v", matches)
	}
	if kept, _ := os.ReadFile(matches[0]); string(kept) != "{not json" {
		t.Fatalf("the damaged file is kept verbatim: %q", kept)
	}
}

func TestAFileFromANewerBuildIsRefusedAndLeftAlone(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	path := filepath.Join(dir, "now.json")
	newer := `{"schema":9,"key":"now","revision":4,"workspace":{}}`
	if err := os.WriteFile(path, []byte(newer), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("now"); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("read: %v", err)
	}
	if _, err := s.Put("now", 4, "w", doc("a")); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("write: %v", err)
	}
	if kept, _ := os.ReadFile(path); string(kept) != newer {
		t.Fatalf("a newer file must be left alone, got %q", kept)
	}
}

func TestAFailedRenameKeepsTheOldDocument(t *testing.T) {
	s := open(t, t.TempDir())
	if _, err := s.Put("now", 0, "w", doc("a")); err != nil {
		t.Fatal(err)
	}
	s.beforeRename = func() error { return errors.New("disk full") }
	if _, err := s.Put("now", 1, "w", doc("a", "b")); err == nil {
		t.Fatal("the write must report the failure")
	}
	s.beforeRename = nil
	rec, err := s.Get("now")
	if err != nil || rec.Revision != 1 || len(tabIDs(t, rec.Workspace)) != 1 {
		t.Fatalf("the old document must survive: %+v %v", rec, err)
	}
	tmps, _ := filepath.Glob(filepath.Join(s.opts.Dir, ".workspace-*.tmp"))
	if len(tmps) != 0 {
		t.Fatalf("no temp file may be left behind: %v", tmps)
	}
}

func TestWaitWakesOnAWriteInThisProcessAndInAnother(t *testing.T) {
	dir := t.TempDir()
	here, there := open(t, dir), open(t, dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got := make(chan Record, 1)
	go func() { rec, _ := here.Wait(ctx, "now", 0); got <- rec }()
	time.Sleep(30 * time.Millisecond)
	if _, err := here.Put("now", 0, "w", doc("a")); err != nil {
		t.Fatal(err)
	}
	if rec := <-got; rec.Revision != 1 {
		t.Fatalf("an in-process write wakes the waiter: %+v", rec)
	}
	go func() { rec, _ := here.Wait(ctx, "now", 1); got <- rec }()
	if _, err := there.Put("now", 1, "w2", doc("a", "b")); err != nil {
		t.Fatal(err)
	}
	if rec := <-got; rec.Revision != 2 || rec.Writer != "w2" {
		t.Fatalf("another process's write is seen by the re-read: %+v", rec)
	}
	short, stop := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer stop()
	rec, err := here.Wait(short, "now", 2)
	if err != nil || rec.Revision != 2 {
		t.Fatalf("a quiet wait ends with the current record: %+v %v", rec, err)
	}
}

func TestOpenRefusesARelativeDirectory(t *testing.T) {
	if _, err := Open(Options{Dir: "workspaces"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a relative directory must be refused: %v", err)
	}
}
