package session

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestFinishedTaskNotesCarryTheirReplyTagsInDrainOrder(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	want := []TaskReplyTag{
		{ID: 4, Title: "Search AgentField", Request: "find more details about agentfield parrallely"},
		{ID: 7, Title: "Check examples", Request: "and check its examples"},
	}
	for i, tag := range want {
		note := wakeNote("finished note")
		note.replyTags = []TaskReplyTag{tag}
		// Keep this test on the queue seam: waking a real turn would race the
		// deliberate drain below and test the provider rather than the metadata.
		note.wake = false
		if !agent.enqueueNote(note) {
			t.Fatalf("note %d was refused", i)
		}
	}
	agent.drainSteering()
	if got := agent.takeReplyTags(); !reflect.DeepEqual(got, want) {
		t.Fatalf("reply tags = %#v, want %#v", got, want)
	}
	if got := agent.takeReplyTags(); len(got) != 0 {
		t.Fatalf("reply tags were handed over twice: %#v", got)
	}
}

func TestDeliveredTaskNoteCarriesTheVerbatimOriginalRequest(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.opened = false
	agent.mu.Unlock()
	graph := newTaskGraph()
	node := &TaskNode{graph: graph, id: 12, spec: taskSpec{
		title:   "AgentField parallel search",
		request: "find more details about agentfield parrallely",
		brief:   "A polished worker brief that must never appear in the tag.",
	}}
	agent.deliverTaskNote(node, "task 12 finished")
	agent.mu.Lock()
	queued := append([]userMessage(nil), agent.steering...)
	agent.mu.Unlock()
	if len(queued) != 1 || len(queued[0].replyTags) != 1 {
		t.Fatalf("delivered note tags = %#v", queued)
	}
	tag := queued[0].replyTags[0]
	if tag.Request != node.spec.request {
		t.Fatalf("request = %q, want verbatim %q", tag.Request, node.spec.request)
	}
	if tag.Request == node.spec.brief {
		t.Fatalf("tag used the groomed brief: %q", tag.Request)
	}
}

func TestPersonPromptCarriesNoTaskReplyTag(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.steering = append(agent.steering, userText("what happened?"))
	agent.mu.Unlock()
	agent.drainSteering()
	if got := agent.takeReplyTags(); len(got) != 0 {
		t.Fatalf("person-prompted reply acquired tags: %#v", got)
	}
}

func TestTaskReplyTagSurvivesSessionResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	journal, _, err := openSessionFile(path, "/tmp/work", "model", "session-id")
	if err != nil {
		t.Fatal(err)
	}
	want := []TaskReplyTag{{ID: 9, Title: "Trace retries", Request: "keep MY wording exactly"}}
	note := textMessage("user", "task 9 finished")
	journal.appendNote(note, want)
	journal.appendMessage(textMessage("assistant", "The retry trace landed."))
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	resumed, replayed, err := openSessionFile(path, "/tmp/work", "model", "session-id")
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	entries := shapeEntries(replayed.messages, resumed)
	for _, entry := range entries {
		if entry.Role == "assistant" {
			if !reflect.DeepEqual(entry.ReplyTags, want) {
				t.Fatalf("resumed reply tags = %#v, want %#v", entry.ReplyTags, want)
			}
			return
		}
	}
	t.Fatal("resumed transcript has no assistant reply")
}
