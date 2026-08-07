package store

import (
	"path/filepath"
	"testing"
)

// The two windowed accessors exist to replace reads that walked the whole
// journal or the whole thread to keep one number. They earn that only if they
// answer exactly what the walk answered.

func TestEventsThroughMatchesTheBoundedWalk(t *testing.T) {
	graph := openWindowStore(t)

	for _, body := range []string{"one", "two", "three", "four"} {
		if _, err := graph.PostMessage(Message{Role: RoleUser, Body: body}); err != nil {
			t.Fatalf("post %q: %v", body, err)
		}
	}
	all, err := graph.Events(0, 0)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(all) < 4 {
		t.Fatalf("expected at least four events, got %d", len(all))
	}

	after := all[0].Seq
	through := all[len(all)-2].Seq
	windowed, err := graph.EventsThrough(after, through)
	if err != nil {
		t.Fatalf("events through: %v", err)
	}
	var walked []Event
	for _, event := range all {
		if event.Seq > after && event.Seq <= through {
			walked = append(walked, event)
		}
	}
	if len(windowed) != len(walked) {
		t.Fatalf("window returned %d events, the walk found %d", len(windowed), len(walked))
	}
	for index := range walked {
		if windowed[index].Seq != walked[index].Seq || windowed[index].Kind != walked[index].Kind {
			t.Fatalf("event %d: window %d/%s, walk %d/%s", index,
				windowed[index].Seq, windowed[index].Kind, walked[index].Seq, walked[index].Kind)
		}
	}

	empty, err := graph.EventsThrough(through, after)
	if err != nil {
		t.Fatalf("inverted window: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("an inverted window should be empty, got %d events", len(empty))
	}
}

func TestLastNonUserMessageSeqSkipsTheTrailingUserRun(t *testing.T) {
	graph := openWindowStore(t)

	seq, err := graph.LastNonUserMessageSeq()
	if err != nil {
		t.Fatalf("empty thread: %v", err)
	}
	if seq != 0 {
		t.Fatalf("a thread with no messages should report zero, got %d", seq)
	}

	if _, err := graph.PostMessage(Message{Role: RoleUser, Body: "asked"}); err != nil {
		t.Fatalf("post user: %v", err)
	}
	answered, err := graph.PostMessage(Message{Role: RoleAgent, Body: "answered"})
	if err != nil {
		t.Fatalf("post agent: %v", err)
	}
	for _, body := range []string{"again", "and again"} {
		if _, err := graph.PostMessage(Message{Role: RoleUser, Body: body}); err != nil {
			t.Fatalf("post %q: %v", body, err)
		}
	}

	seq, err = graph.LastNonUserMessageSeq()
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if seq != answered.Seq {
		t.Fatalf("resume cursor is %d, want the last answered message %d", seq, answered.Seq)
	}

	noted, err := graph.PostMessage(Message{Role: RoleSystem, Body: "recovered"})
	if err != nil {
		t.Fatalf("post system: %v", err)
	}
	seq, err = graph.LastNonUserMessageSeq()
	if err != nil {
		t.Fatalf("resume after system: %v", err)
	}
	if seq != noted.Seq {
		t.Fatalf("a system message closes the run too: got %d, want %d", seq, noted.Seq)
	}
}

func openWindowStore(t *testing.T) *Store {
	t.Helper()
	graph, err := Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	return graph
}
