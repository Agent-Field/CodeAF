package session

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestAsideCarriesFinishedTaskIDsLiveAndAfterReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })
	note := userText("task 9 finished")
	note.authored = true
	note.replyTags = []TaskReplyTag{{ID: 9, Title: "Check report"}, {ID: 12, Title: "Count words"}}
	agent.mu.Lock()
	agent.recordUserLocked(note)
	agent.mu.Unlock()
	want := []string{"9", "12"}
	check := func(entries []DisplayEntry) {
		t.Helper()
		if len(entries) != 1 || entries[0].Role != "aside" || !reflect.DeepEqual(entries[0].TaskIDs, want) {
			t.Fatalf("aside task ids = %#v, want %v", entries, want)
		}
	}
	check(agent.Transcript())
	agent.Close()
	resumed, replayed, err := openSessionFile(path, "/tmp/work", "model", "session-id")
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	check(shapeEntries(replayed.messages, resumed))
}
