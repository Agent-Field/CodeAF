package session

// A RUN THAT CAME TO NOTHING SAYS SO.
//
// Every task firing used to answer "landed" whatever it did, so the marker in
// the run folder never read [standing.OutcomeNothing], the sweep's whole licence
// over a run was never granted, and a watch that ran a headless child every
// night forever kept every one of those folders forever. These pin the rule that
// decides it and one firing end to end with a scripted child.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// standingChildRunner is a runner whose firings run against a scripted model
// rather than a live one. It is [standingRunner.child] and nothing else: the
// config the child is built with, the belt it carries and the loop it runs are
// the product's own.
func standingChildRunner(t *testing.T, root string, completer Completer) *standingRunner {
	t.Helper()
	parent := Config{
		Workspace: t.TempDir(),
		Model:     "test/model",
		System:    "SYSTEM",
	}
	return &standingRunner{
		parent: parent,
		root:   root,
		child: func(cfg Config) (*Agent, error) {
			agent, err := newAgent(cfg, completer)
			if err != nil {
				return nil, err
			}
			t.Cleanup(func() { _ = agent.Close() })
			return agent, nil
		},
	}
}

// nightly is one overnight job: a rhythm, a brief, and no conversation to
// deliver into, so the outcome is the only thing under test.
func nightly(workspace string) standing.Item {
	return standing.Item{
		ID:        "0f1e2d3c4b5a6978",
		Words:     "every night, tidy the flaky tests",
		Workspace: workspace,
		When:      standing.When{Kind: standing.WhenEvery, Words: "every night", Every: "0 2 * * *"},
		Does:      standing.Action{Kind: standing.ActionTask, Brief: "look at last night's failures"},
		Rails:     standing.Rails{PerRunUSD: 0.50, MaxPerDay: 1},
		Status:    standing.StatusActive,
	}
}

// A CHILD THAT SAVED NOTHING AND SAID NOTHING CAME TO NOTHING, and the same
// child with one sentence to its name landed. The run folder's own marker is
// written by the pass from this word ([standing.CameTo]), so it is the outcome
// and not the folder that has to be right here.
func TestAFiringThatLeftNothingBehindComesToNothing(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	item := nightly(workspace)

	silent := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(""), nil },
	}}
	runner := standingChildRunner(t, root, silent)
	outcome, err := runner.Run(context.Background(), item, filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Kind != standing.OutcomeNothing {
		t.Fatalf("a run that left nothing behind came to %q, wanted %q", outcome.Kind, standing.OutcomeNothing)
	}
	if outcome.Text != "" || outcome.NeedsPerson != "" {
		t.Fatalf("a run that came to nothing carries something: %+v", outcome)
	}

	// ONE SENTENCE IS SOMETHING. It is delivered to the person, so the run is
	// not one nobody may ever want back.
	speaks := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "the three flaky tests passed this time")
			return textResponse("the three flaky tests passed this time"), nil
		},
	}}
	runner = standingChildRunner(t, root, speaks)
	outcome, err = runner.Run(context.Background(), item, filepath.Join(root, "runs", "0002"), "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Kind != "landed" {
		t.Fatalf("a run with a report to give came to %q, wanted landed", outcome.Kind)
	}
	if !strings.Contains(outcome.Text, "flaky tests passed") {
		t.Fatalf("the run lost its own report: %q", outcome.Text)
	}
}

// A CHILD THAT WROTE A FILE LANDED, even with nothing to say about it. The file
// is the deliverable; the sentence is a courtesy.
func TestAFiringThatSavedAFileLandedEvenWithNothingToSay(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	item := nightly(workspace)
	path := filepath.Join(workspace, "notes", "flakes.md")
	args, err := json.Marshal(map[string]string{"path": path, "content": "nothing flaked\n"})
	if err != nil {
		t.Fatal(err)
	}

	writes := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "write", string(args)), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(""), nil },
	}}
	runner := standingChildRunner(t, root, writes)
	outcome, err := runner.Run(context.Background(), item, filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the child never wrote the file this test is about: %v", err)
	}
	if outcome.Kind != "landed" {
		t.Fatalf("a run that saved a file came to %q, wanted landed", outcome.Kind)
	}
}

// AND A RUN THAT CAME TO NOTHING TELLS NOBODY. Walking the delivery roads with
// an empty sentence would put `◦ every night, tidy the flaky tests: ` into the
// conversation somebody is sitting in — an interruption whose whole content is
// that it was not worth interrupting for.
func TestARunThatCameToNothingNeverReachesTheConversation(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	room := standingLiveAgent(t, workspace, nil)
	lane := room.TaskUpdates()

	item := nightly(workspace)
	item.Origin = standing.Origin{SessionID: room.id, Transcript: room.config.SessionFile}

	silent := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(""), nil },
	}}
	runner := standingChildRunner(t, root, silent)
	if _, err := runner.Run(context.Background(), item, filepath.Join(root, "runs", "0001"), ""); err != nil {
		t.Fatalf("Run: %v", err)
	}

	select {
	case event := <-lane:
		if event.Kind == EventStandingUpdate {
			t.Fatalf("a run that came to nothing drew a row anyway: %+v", event.Standing)
		}
	case <-time.After(200 * time.Millisecond):
	}
	if queued := standingQueued(room); len(queued) != 0 {
		t.Fatalf("a run that came to nothing steered the conversation: %q", queued)
	}
}

// THE RULE ITSELF, said once as a table. It is three facts about a finished
// child and nothing else, so it can be read whole.
func TestWhatCountsAsARunThatCameToNothing(t *testing.T) {
	for _, probe := range []struct {
		saved  bool
		report string
		needs  string
		want   string
	}{
		{false, "", "", standing.OutcomeNothing},
		{false, "   \n ", "", standing.OutcomeNothing},
		{true, "", "", "landed"},
		{false, "the suite is green", "", "landed"},
		{true, "the suite is green", "", "landed"},
		// A run stopped on something only a person can allow is neither: it is
		// work waiting for them, and it is waiting whatever else it did.
		{false, "", "the fix touches migrations", "needs-you"},
		{true, "and here is why", "the fix touches migrations", "needs-you"},
	} {
		if got := standingCameTo(probe.saved, probe.report, probe.needs); got != probe.want {
			t.Fatalf("saved=%v report=%q needs=%q came to %q, wanted %q",
				probe.saved, probe.report, probe.needs, got, probe.want)
		}
	}
}
