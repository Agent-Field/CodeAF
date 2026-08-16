package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A compile phase is written to the job's record before the job exists, and it
// is never written to the conversation (13.18). The provisional anchor is the
// half that was always here: the command link proves the unknown node id is one
// a pending splice will admit. What is new is the other half — the message
// carries no session, so the room draws every phase in order and the thread,
// the head's prompt window and the v1 lens never see one.
func TestChatPlanProgressRecordsAgainstProvisionalJobAnchorAndNeverTheThread(t *testing.T) {
	history, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()

	command, err := history.RequestCommand(store.Command{
		SessionID: "progress-session", Kind: store.CommandSplice, Instruction: "compare three cities",
	})
	if err != nil {
		t.Fatal(err)
	}
	nodeID := fmt.Sprintf("task-%d", command.Seq)
	progress := chatPlanProgress(history, resident.PlanAnchor{
		NodeID: nodeID, SessionID: command.SessionID, CommandSeq: command.Seq,
	})
	progress(plan.ProgressUpdate{Phase: "reading the request"})

	if spoken, err := history.Messages(command.SessionID, 0, 0); err != nil {
		t.Fatal(err)
	} else if len(spoken) != 0 {
		t.Fatalf("a compile phase reached the thread: %+v", spoken)
	}
	messages, err := history.Messages("", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Role != store.RoleSystem ||
		messages[0].SessionID != "" ||
		messages[0].NodeID != nodeID || messages[0].CommandSeq != command.Seq ||
		messages[0].Body != "reading the request" || messages[0].Progress == nil ||
		messages[0].Progress.Phase != "reading the request" {
		t.Fatalf("planning message = %+v", messages)
	}

	if err := history.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: nodeID, Brief: "compare the cities", Stage: 1,
	}}}, store.Provenance{
		Origin: store.OriginUser, SessionID: command.SessionID, Intent: command.Instruction,
	}); err != nil {
		t.Fatal(err)
	}
	anchored, err := history.NodeMessages(nodeID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(anchored) != 1 || anchored[0].Body != messages[0].Body {
		t.Fatalf("message did not remain attached after admission: %+v", anchored)
	}
}

func TestChatPlanProgressThrottlesAndCoalescesLeafCounts(t *testing.T) {
	history, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	if err := history.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "job", Brief: "do the work", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "do the work"}); err != nil {
		t.Fatal(err)
	}

	now := time.Unix(100, 0)
	poster := &planProgressPoster{
		history:  history,
		anchor:   resident.PlanAnchor{NodeID: "job", SessionID: "s1"},
		interval: planCountThrottle,
		now:      func() time.Time { return now },
		last:     map[string]time.Time{}, pending: map[string]plan.ProgressUpdate{}, timers: map[string]*time.Timer{},
	}
	poster.report(plan.ProgressUpdate{Phase: "writing the plan", Done: 1, Total: 5})
	poster.report(plan.ProgressUpdate{Phase: "writing the plan", Done: 2, Total: 5})
	poster.report(plan.ProgressUpdate{Phase: "writing the plan", Done: 3, Total: 5})
	now = now.Add(planCountThrottle)
	poster.report(plan.ProgressUpdate{Phase: "writing the plan", Done: 4, Total: 5})
	// Final counts bypass the interval so a fast last completion is never lost.
	poster.report(plan.ProgressUpdate{Phase: "writing the plan", Done: 5, Total: 5})

	messages, err := history.NodeMessages("job", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var bodies []string
	for _, message := range messages {
		bodies = append(bodies, message.Body)
	}
	want := []string{
		"writing the plan · 1 of 5",
		"writing the plan · 4 of 5",
		"writing the plan · 5 of 5",
	}
	if !reflect.DeepEqual(bodies, want) {
		t.Fatalf("throttled progress = %#v, want %#v", bodies, want)
	}
}

func TestChatPlanProgressNeverCoalescesGeneratedTitles(t *testing.T) {
	history, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	if err := history.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "job", Brief: "do the work", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "do the work"}); err != nil {
		t.Fatal(err)
	}
	poster := &planProgressPoster{
		history: history, anchor: resident.PlanAnchor{NodeID: "job", SessionID: "s1"},
		interval: time.Hour, now: time.Now, last: map[string]time.Time{},
		pending: map[string]plan.ProgressUpdate{}, timers: map[string]*time.Timer{},
	}
	for index, title := range []string{"First step", "Second step", "Third step"} {
		poster.report(plan.ProgressUpdate{
			Phase: "writing the plan", Done: index + 1, Total: 4, Latest: title,
		})
	}
	messages, err := history.NodeMessages("job", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 {
		t.Fatalf("generated-title updates were coalesced: %+v", messages)
	}
	for index, message := range messages {
		if message.Progress == nil || message.Progress.Latest == "" || message.Progress.Done != index+1 {
			t.Fatalf("structured progress %d = %+v", index, message)
		}
	}
}

func TestHeadlessPlanProgressWritesStderrLines(t *testing.T) {
	var stderr bytes.Buffer
	progress := headlessPlanProgress(&stderr)
	progress(plan.ProgressUpdate{Phase: "reading the request"})
	progress(plan.ProgressUpdate{Phase: "choosing the shape"})
	progress(plan.ProgressUpdate{Phase: "writing the plan", Done: 5, Total: 12})

	const want = "reading the request\nchoosing the shape\nwriting the plan · 5 of 12\n"
	if stderr.String() != want {
		t.Fatalf("stderr progress = %q, want %q", stderr.String(), want)
	}
}
