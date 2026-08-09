package storage

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestStoreReadWriteUpdateListRemove(t *testing.T) {
	root := filepath.Join(t.TempDir(), "storage")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "migration"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := New(root)
	value := NewObject()
	value.Set("z", float64(1))
	value.Set("a", "before")
	if err := store.Write([]string{"session", "s1"}, value); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "session", "s1.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"z\": 1,\n  \"a\": \"before\"\n}"
	if string(raw) != want {
		t.Fatalf("write bytes:\n got %q\nwant %q", raw, want)
	}

	updated, err := store.Update([]string{"session", "s1"}, func(value any) {
		value.(*Object).Set("a", "after")
		value.(*Object).Set("new", true)
	})
	if err != nil {
		t.Fatal(err)
	}
	if keys := updated.(*Object).Keys(); !reflect.DeepEqual(keys, []string{"z", "a", "new"}) {
		t.Fatalf("updated keys: %v", keys)
	}

	keys, err := store.List([]string{"session"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(keys, [][]string{{"session", "s1"}}) {
		t.Fatalf("list: %#v", keys)
	}

	if err := store.Remove([]string{"session", "s1"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove([]string{"session", "s1"}); err != nil {
		t.Fatal(err)
	}
	_, err = store.Read([]string{"session", "s1"})
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
	if notFound.Message != "Resource not found: "+filepath.Join(root, "session", "s1.json") {
		t.Fatalf("message: %q", notFound.Message)
	}
}

func TestStoreUpdateSerializesConcurrentMutations(t *testing.T) {
	root := filepath.Join(t.TempDir(), "storage")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "migration"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := New(root)
	type counter struct {
		N int `json:"n"`
	}
	if err := store.Write([]string{"counter"}, counter{}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := UpdateAs(store, []string{"counter"}, func(value *counter) {
				value.N++
			}); err != nil {
				t.Errorf("update: %v", err)
			}
		}()
	}
	wg.Wait()
	got, err := ReadAs[counter](store, []string{"counter"})
	if err != nil {
		t.Fatal(err)
	}
	if got.N != 30 {
		t.Fatalf("counter = %d, want 30", got.N)
	}
}

func TestStoreUpdatesDifferentResourcesConcurrentlyContract(t *testing.T) {
	// Parity audit contract 6: unrelated streaming resources do not queue
	// behind a store-wide exclusive lock.
	root := filepath.Join(t.TempDir(), "storage")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "migration"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := New(root)
	for _, key := range []string{"first", "second"} {
		if err := store.Write([]string{key}, map[string]any{"value": 0}); err != nil {
			t.Fatal(err)
		}
	}
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := store.Update([]string{"first"}, func(any) {
			close(firstEntered)
			<-releaseFirst
		})
		firstDone <- err
	}()
	<-firstEntered
	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		_, err := store.Update([]string{"second"}, func(any) { close(secondEntered) })
		secondDone <- err
	}()
	select {
	case <-secondEntered:
	case <-time.After(2 * time.Second):
		close(releaseFirst)
		t.Fatal("different-resource update blocked behind the first resource")
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}

func TestStoreUpdateCallbackOwnsResourceLockContract(t *testing.T) {
	// Parity audit contract 10: Update's documented callback boundary holds the
	// resource lock, so same-key callers serialize around the non-reentrant callback.
	root := filepath.Join(t.TempDir(), "storage")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "migration"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := New(root)
	if err := store.Write([]string{"shared"}, map[string]any{"value": 0}); err != nil {
		t.Fatal(err)
	}
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := store.Update([]string{"shared"}, func(any) {
			close(firstEntered)
			<-releaseFirst
		})
		firstDone <- err
	}()
	<-firstEntered
	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		_, err := store.Update([]string{"shared"}, func(any) { close(secondEntered) })
		secondDone <- err
	}()
	select {
	case <-secondEntered:
		close(releaseFirst)
		t.Fatal("same-resource callback ran without owning the resource lock")
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-secondEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("same-resource waiter did not resume after callback returned")
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}

func TestStoreCrossProcessLockAndAtomicReplacementContract(t *testing.T) {
	// Parity audit contract: independent store instances cannot lose an RMW,
	// and each durable rewrite is an atomic inode replacement.
	root := filepath.Join(t.TempDir(), "storage")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "migration"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	type counter struct {
		N int `json:"n"`
	}
	first, second := New(root), New(root)
	if err := first.Write([]string{"counter"}, counter{}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "counter.json")
	commands := make([]*exec.Cmd, 2)
	for index := range commands {
		commands[index] = exec.Command(os.Args[0], "-test.run=^TestStoreProcessUpdateHelper$", "-test.count=1")
		commands[index].Env = append(os.Environ(), "CODEAF_STORAGE_HELPER_ROOT="+root)
		if err := commands[index].Start(); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatalf("storage helper: %v", err)
		}
	}
	got, err := ReadAs[counter](second, []string{"counter"})
	if err != nil || got.N != 40 {
		t.Fatalf("cross-store counter = %+v, %v; want 40", got, err)
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Write([]string{"counter"}, got); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("durable rewrite mutated the JSON inode in place")
	}
	matches, err := filepath.Glob(filepath.Join(root, ".tmp-*.json"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("atomic rewrite leftovers = %v, %v", matches, err)
	}
}

func TestStoreProcessUpdateHelper(t *testing.T) {
	root := os.Getenv("CODEAF_STORAGE_HELPER_ROOT")
	if root == "" {
		t.Skip("subprocess helper")
	}
	type counter struct {
		N int `json:"n"`
	}
	store := New(root)
	for index := 0; index < 20; index++ {
		if _, err := UpdateAs(store, []string{"counter"}, func(value *counter) { value.N++ }); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigration2MovesDiffsAndPreservesObjectOrder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "storage")
	source := filepath.Join(root, "session", "old-project", "s1.json")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	input := `{"title":"t","id":"s1","projectID":"new-project","summary":{"diffs":[{"additions":2,"deletions":3},{"additions":5,"deletions":7}]},"tail":true}`
	if err := os.WriteFile(source, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "migration"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := New(root)
	if _, err := store.List(nil); err != nil {
		t.Fatal(err)
	}

	diff, err := os.ReadFile(filepath.Join(root, "session_diff", "s1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(diff), "[\n  {\n    \"additions\": 2,\n    \"deletions\": 3\n  },\n  {\n    \"additions\": 5,\n    \"deletions\": 7\n  }\n]"; got != want {
		t.Fatalf("diff:\n%s\nwant:\n%s", got, want)
	}
	session, err := os.ReadFile(filepath.Join(root, "session", "new-project", "s1.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"title\": \"t\",\n  \"id\": \"s1\",\n  \"projectID\": \"new-project\",\n  \"summary\": {\n    \"additions\": 7,\n    \"deletions\": 10\n  },\n  \"tail\": true\n}"
	if string(session) != want {
		t.Fatalf("session:\n%s\nwant:\n%s", session, want)
	}
}

type mockDB struct {
	runs    []string
	inserts map[string][]*Object
	fail    map[string]error
}

func (m *mockDB) Run(statement string) error {
	m.runs = append(m.runs, statement)
	return m.fail[statement]
}

func (m *mockDB) Insert(table string, values []*Object) error {
	if err := m.fail[table]; err != nil {
		return err
	}
	m.inserts[table] = append(m.inserts[table], values...)
	return nil
}

func TestRunJSONMigrationForeignKeysErrorsAndProgress(t *testing.T) {
	dataDir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(dataDir, "storage", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("project/p1.json", `{"worktree":"/w","vcs":"git","time":{"created":1}}`)
	write("session/p1/s1.json", `{"id":"wrong","title":"S"}`)
	write("session/missing/orphan.json", `{}`)
	write("message/s1/m1.json", `{"id":"wrong","sessionID":"wrong","role":"user","time":{"created":2}}`)
	write("message/orphan/x.json", `{}`)
	write("part/m1/pt1.json", `{"id":"wrong","messageID":"wrong","sessionID":"wrong","type":"text"}`)
	write("part/missing/pt2.json", `{}`)
	write("todo/s1.json", `[{"content":"c","status":"pending","priority":"high"},{}]`)
	write("permission/p1.json", `{"read":"allow"}`)
	write("session_share/s1.json", `{"id":"share","secret":"sec","url":"url"}`)

	db := &mockDB{inserts: map[string][]*Object{}, fail: map[string]error{}}
	var events []Progress
	stats, err := RunJSONMigration(db, dataDir, &MigrationOptions{
		Now: func() time.Time { return time.UnixMilli(99) },
		Progress: func(event Progress) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Projects != 1 || stats.Sessions != 1 || stats.Messages != 1 || stats.Parts != 1 ||
		stats.Todos != 1 || stats.Permissions != 1 || stats.Shares != 1 {
		t.Fatalf("stats: %+v", stats)
	}
	if len(stats.Errors) != 1 || !stringsContains(stats.Errors[0], "part missing message session") {
		t.Fatalf("errors: %v", stats.Errors)
	}
	if got := db.runs[len(db.runs)-1]; got != "COMMIT" {
		t.Fatalf("last db run: %q", got)
	}
	if len(events) == 0 || events[0].Label != "starting" || events[len(events)-1].Label != "complete" {
		t.Fatalf("progress: %#v", events)
	}
	if events[len(events)-1].Current != events[len(events)-1].Total {
		t.Fatalf("final progress: %#v", events[len(events)-1])
	}
	message := db.inserts[MessageTable][0]
	data, _ := message.Get("data")
	if _, ok := data.(*Object).Get("id"); ok {
		t.Fatal("message data retained id")
	}
	if got, _ := message.Get("id"); got != "m1" {
		t.Fatalf("message id = %v", got)
	}
}

func TestRunJSONMigrationInsertFailureIsRecorded(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "storage", "project", "p.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"vcs":"git"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	db := &mockDB{
		inserts: map[string][]*Object{},
		fail:    map[string]error{ProjectTable: errors.New("constraint")},
	}
	stats, err := RunJSONMigration(db, dataDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Projects != 0 || !reflect.DeepEqual(stats.Errors, []string{"failed to migrate project batch: constraint"}) {
		t.Fatalf("stats: %+v", stats)
	}
}

func stringsContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || containsAt(s, sub))
}

func containsAt(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
