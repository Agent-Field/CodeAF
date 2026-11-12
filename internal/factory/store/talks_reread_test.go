package store

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// File identity cannot see an in-place rewrite that keeps the size and time, so
// a watcher may not rest on it for longer than the bound.
func TestTalkStopSeesInPlaceSameTimeEditWithinItsBound(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	st.clock = func() time.Time { return now }
	running := false
	const talk = "/nowhere/chat.jsonl"
	made, err := st.Create(factory.Item{Talk: talk, PersonStopped: &running})
	if err != nil {
		t.Fatal(err)
	}
	stopped := func() bool {
		t.Helper()
		manager, stopped, err := st.TalkStop(FileKey(talk))
		if err != nil || !manager {
			t.Fatalf("TalkStop: manager %v, %v", manager, err)
		}
		return stopped
	}
	if stopped() {
		t.Fatal("stopped before any Stop")
	}
	path := st.ItemPath(made.ID)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const was, is = `"PersonStopped": false`, `"PersonStopped": true `
	if strings.Count(string(data), was) != 1 {
		t.Fatalf("expected one %s in %s", was, data)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), was, is, 1)), info.Mode()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if next, err := os.Stat(path); err != nil || !os.SameFile(info, next) || next.Size() != info.Size() || !next.ModTime().Equal(info.ModTime()) {
		t.Fatalf("the edit did not keep the file's identity: %v", err)
	}

	now = now.Add(talkRereadEvery - time.Second)
	before := st.DocumentReads()
	stopped()
	if got := st.DocumentReads() - before; got != 0 {
		t.Fatalf("inside the bound a look read %d documents, want 0", got)
	}

	now = now.Add(time.Second)
	if !stopped() {
		t.Fatal("at the bound the Stop was still not seen")
	}
	before = st.DocumentReads()
	stopped()
	if got := st.DocumentReads() - before; got != 0 {
		t.Fatalf("after the re-read a look read %d documents, want 0", got)
	}
}
