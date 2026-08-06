package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestChatPlanProgressPostsAgainstProvisionalJobAnchor(t *testing.T) {
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
	progress("grounding", "settling what to look at")

	messages, err := history.Messages(command.SessionID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Role != store.RoleSystem ||
		messages[0].NodeID != nodeID || messages[0].CommandSeq != command.Seq ||
		messages[0].Body != "grounding: settling what to look at" {
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
		last:     map[string]time.Time{}, pending: map[string]string{}, timers: map[string]*time.Timer{},
	}
	poster.report("briefs", "1/5")
	poster.report("briefs", "2/5")
	poster.report("briefs", "3/5")
	now = now.Add(planCountThrottle)
	poster.report("briefs", "4/5")
	// Final counts bypass the interval so a fast last completion is never lost.
	poster.report("briefs", "5/5")

	messages, err := history.NodeMessages("job", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var bodies []string
	for _, message := range messages {
		bodies = append(bodies, message.Body)
	}
	want := []string{"briefs 1/5", "briefs 4/5", "briefs 5/5"}
	if !reflect.DeepEqual(bodies, want) {
		t.Fatalf("throttled progress = %#v, want %#v", bodies, want)
	}
}

func TestHeadlessPlanProgressWritesStderrLines(t *testing.T) {
	var stderr bytes.Buffer
	progress := headlessPlanProgress(&stderr)
	progress("grounding", "settling what to look at")
	progress("sizing", "12 nodes — 3 to split")
	progress("briefs", "5/12")

	const want = "grounding: settling what to look at\nsizing 12 nodes — 3 to split\nbriefs 5/12\n"
	if stderr.String() != want {
		t.Fatalf("stderr progress = %q, want %q", stderr.String(), want)
	}
}
