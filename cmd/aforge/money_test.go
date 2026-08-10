package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// billedClient answers with a usage block, which is the only thing the rail can
// bill from.
type billedClient struct {
	model  string
	cost   float64
	prompt int
}

func (c *billedClient) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	cost := c.cost
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{Role: "assistant",
			Content: []ai.ContentPart{{Type: "text", Text: "ok"}}}}},
		Usage: &ai.Usage{PromptTokens: c.prompt, CompletionTokens: 7, Cost: &cost},
	}, nil
}

func (c *billedClient) Model() string { return c.model }

// The daily rail is summed from the usage table and nowhere else, so every
// structuring call chat made — routing, compiling, gating, revising — was money
// the rail could not see and the user was never told about.
func TestStructuringCallsReachTheRail(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "spend.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "write the report", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "write the report"}); err != nil {
		t.Fatal(err)
	}
	client := adoptLiveClient(config.Config{}, "plan/slot", &billedClient{model: "plan/slot", cost: 0.40, prompt: 900})
	client.WithUsageJournal(func(usage store.NodeUsage) {
		if err := graph.RecordUsage(usage); err != nil {
			t.Error(err)
		}
	})

	// One call about nothing in particular, one about a specific job.
	if _, err := client.CompleteWithMessages(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(withSpendNode(context.Background(), "job"), nil); err != nil {
		t.Fatal(err)
	}

	spend, err := graph.SpendToday()
	if err != nil {
		t.Fatal(err)
	}
	if spend < 0.79 || spend > 0.81 {
		t.Fatalf("today's spend = %v, want both structuring calls on the rail", spend)
	}
	jobs, err := graph.SpendByJob(time.Time{}, time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].JobID != "job" {
		t.Fatalf("job spend = %+v; a gate call about one job belongs to that job", jobs)
	}
	if jobs[0].Cost < 0.39 || jobs[0].Cost > 0.41 {
		t.Fatalf("job cost = %v, want only the call that named it", jobs[0].Cost)
	}
	// The spine carries what belongs to no errand — on the day's rail, not on
	// anybody's bill.
	if models, err := graph.NodeModels("job"); err != nil || len(models) != 1 || models[0] != "plan/slot" {
		t.Fatalf("job models = %v err=%v", models, err)
	}
}

// A failure the user reads should be about their errand, keep the provider's
// own words, and never carry an integer that appears on no surface.
func TestHumanFailureDropsTheInternalIDAndKeepsTheCause(t *testing.T) {
	node := store.Node{ID: "task-9", Title: "audit the billing code", Brief: "audit the billing code"}
	failure := humanFailure(node, errors.New("node 7: openrouter: 500 upstream is unavailable"),
		[]string{"/w/job/01-notes.md"})
	if failure == nil {
		t.Fatal("a real error humanized to nil")
	}
	body := failure.Error()
	if strings.Contains(body, "node 7") {
		t.Fatalf("the internal id survived: %q", body)
	}
	if !strings.Contains(body, "audit the billing code") {
		t.Fatalf("the failure does not say what failed: %q", body)
	}
	if !strings.Contains(body, "openrouter: 500 upstream is unavailable") {
		t.Fatalf("the cause clause was paraphrased away: %q", body)
	}
	// The partial rides below the first line, where the failure formatter does
	// not look and clipping does.
	first := firstLine(body)
	if strings.Contains(first, "01-notes.md") {
		t.Fatalf("the file list crowded out the reason: %q", first)
	}
	if !strings.Contains(body, "/w/job/01-notes.md") {
		t.Fatalf("the partial work is unreachable again: %q", body)
	}
	if humanFailure(node, nil, nil) != nil {
		t.Fatal("a nil error became a failure")
	}
}

// A parent over a failed leaf used to write the confident summary any synthesis
// writes, and the user read an answer with a hole in it and no mention of one.
func TestFailedPartsNoteTellsTheParentTheTruth(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "parts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "audit four services", Stage: 2},
		{ID: "part-a", Parent: "job", Title: "billing", Brief: "audit billing", Stage: 1},
		{ID: "part-b", Parent: "job", Title: "auth", Brief: "audit auth", Stage: 1},
		{ID: "part-c", Parent: "job", Title: "search", Brief: "audit search", Stage: 1},
		{ID: "part-d", Parent: "job", Title: "mail", Brief: "audit mail", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "audit four services"}); err != nil {
		t.Fatal(err)
	}
	root, _, err := graph.Node("job")
	if err != nil {
		t.Fatal(err)
	}
	if note := failedPartsNote(graph, root); note != "" {
		t.Fatalf("a job with nothing failed apologized anyway: %q", note)
	}
	for _, id := range []string{"part-a", "part-b", "part-c"} {
		claim, ok, err := graph.Claim(id, "worker")
		if err != nil || !ok {
			t.Fatal(err)
		}
		if err := graph.Start(claim); err != nil {
			t.Fatal(err)
		}
		if err := graph.Complete(claim, "clean"); err != nil {
			t.Fatal(err)
		}
	}
	claim, ok, err := graph.Claim("part-d", "worker")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Fail(claim, "mail did not finish — openrouter: 500 upstream is unavailable"); err != nil {
		t.Fatal(err)
	}

	note := failedPartsNote(graph, root)
	if !strings.Contains(note, "3 of 4 parts finished") {
		t.Fatalf("the count is wrong or missing: %q", note)
	}
	if !strings.Contains(note, "mail") || !strings.Contains(note, "upstream is unavailable") {
		t.Fatalf("the failure is unnamed: %q", note)
	}
}

// Taste had exactly one consumer in the tree — the gate — and the gate is told
// in the same breath that wording and style are not gaps. The workers, who are
// who taste is for, never saw it.
func TestEveryLeafBriefCarriesSettledTaste(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "taste.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	candidate, err := graph.RecordTasteCandidate("", "user", "keep written comparisons under a page")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.PromoteTasteRule(candidate.Seq); err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "compare the three options", Stage: 2},
		{ID: "part", Parent: "job", Brief: "read option one", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "compare the three options"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"job", "part"} {
		node, _, err := graph.Node(id)
		if err != nil {
			t.Fatal(err)
		}
		brief := residentDeliveryBrief(graph, node)
		if !strings.Contains(brief, "keep written comparisons under a page") {
			t.Fatalf("%s was briefed without the settled rule: %q", id, brief)
		}
		if !strings.Contains(brief, node.Brief) {
			t.Fatalf("%s lost its own brief: %q", id, brief)
		}
	}
}
