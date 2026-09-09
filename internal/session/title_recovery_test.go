package session

import (
	"context"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmptyNamingAnswerFallsThroughAndPublishesBothNames(t *testing.T) {
	completer := naming(&scriptedCompleter{steps: oneTurn("the rentals")},
		namerReply{title: ""}, namerReply{title: "**Full:** rentals around victoria memorial square\n**Tab:** park rentals"})
	agent, path := titleAgent(t, completer, func(c *Config) {
		c.RolesSource = func(key string) (string, bool) { return "cheap/model", key == roles.TierKey(roles.TierLow) }
	})
	collect(t, mustSubmit(t, agent, "find apartments near the park"))
	agent.titleJobs.Wait()
	if agent.Title() != "rentals around victoria memorial square" || agent.ShortTitle() != "park rentals" {
		t.Fatalf("names = %q / %q", agent.Title(), agent.ShortTitle())
	}
	if completer.asks() != 2 || completer.namerModel(1) != "test/model" {
		t.Fatal("invalid answer did not reach the fallback model")
	}
	replay, err := replaySessionFile(path)
	if err != nil || replay.title != agent.Title() || replay.shortTitle != agent.ShortTitle() {
		t.Fatalf("replay = %+v, %v", replay, err)
	}
}

func TestNamingRepairsFormattingAndCapsTabWords(t *testing.T) {
	for _, raw := range []string{
		"**Full:** exploring backai github marketing\n**Tab:** backai marketing ideas",
		"# Full: exploring backai github marketing\n> Tab: backai marketing ideas",
		"Full: exploring backai github marketing\nTab: backai marketing ideas",
	} {
		got := cleanConversationTitle(raw)
		if got.full != "exploring backai github marketing" || got.short != "backai marketing" {
			t.Errorf("%q => %+v", raw, got)
		}
	}
	if got := compactTitle("Full: Exploring BackAI's GitH…"); got != "Exploring BackAI's" {
		t.Fatal(got)
	}
	for _, raw := range []string{"nothing to name", "Untitled", "no title"} {
		if cleanTaskName(raw) != "" || cleanConversationTitle(raw).full != "" {
			t.Errorf("accepted %q", raw)
		}
	}
	if got := healedTitle("Full: Exploring BackAI's GitHub Organization for Reddit Marketing"); got != "Exploring BackAI's GitHub Organization for Reddit Marketing" {
		t.Fatal(got)
	}
}

func TestTaskNamingRejectsPlaceholderAndTriesFallback(t *testing.T) {
	client := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("nothing to name"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("hidden rentals"), nil },
	}}
	agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })
	got := agent.taskName(context.Background(), "search harder for rentals near victoria memorial square")
	if got != "hidden rentals" || client.requests() != 2 {
		t.Fatalf("name=%q calls=%d", got, client.requests())
	}
}

func TestReopeningRepairsAnOldTaskPlaceholderWithoutRerunningWork(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	writeCheckpoint(t, taskCheckpointPath(journal), taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 1,
		Nodes: []taskRecord{{ID: 1, Title: "nothing to name", Summary: "search harder for rentals near the park",
			Brief: "Find hidden rental listings near victoria memorial square", Acceptance: "listings found",
			State: TaskDone, Report: "rental list saved", Noted: true}},
	})
	client := &scriptedCompleter{steps: []step{func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		if !isNameCall(messages) {
			t.Error("reopening reran work instead of naming it")
		}
		if !strings.Contains(messageContentText(messages[1]), "victoria memorial square") {
			t.Error("namer lost the saved brief")
		}
		return textResponse("hidden rentals"), nil
	}}}
	agent, _ := newTestAgent(t, client, func(c *Config) { c.SessionFile = journal; c.RolesSource = nameSettings() })
	node := agent.graph().node(1)
	if !nameLanded(func() bool { return node.title() == "hidden rentals" }) {
		t.Fatalf("title=%q", node.title())
	}
	if node.stateNow() != TaskDone || client.requests() != 1 {
		t.Fatal("reopening changed finished work")
	}
	if !nameLanded(func() bool {
		return recordOf(t, readCheckpoint(t, taskCheckpointPath(journal)), 1).Title == "hidden rentals"
	}) {
		t.Fatal("repaired name was not saved")
	}
}
