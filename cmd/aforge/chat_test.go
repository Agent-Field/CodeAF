package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type gateCaptureClient struct {
	model        string
	messages     []ai.Message
	class        provider.CallClass
	responseMode bool
}

func (c *gateCaptureClient) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	c.messages = messages
	c.class = provider.CallClassFrom(ctx)
	request := &ai.Request{}
	for _, option := range options {
		if err := option(request); err != nil {
			return nil, err
		}
	}
	c.responseMode = request.ResponseFormat != nil
	return &ai.Response{Choices: []ai.Choice{{
		Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: `{"pass":true}`}}},
	}}}, nil
}

func (c *gateCaptureClient) Model() string { return c.model }

func TestDeliveryGateSeesNotebookPreferencesAndNoPanelStaysBare(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if _, err := graph.RecordFact("", "user", store.FactPreference,
		"the user always wants benchmark evidence named explicitly"); err != nil {
		t.Fatal(err)
	}

	settings := config.Config{Model: "worker/model"}
	capture := &gateCaptureClient{model: "worker/model"}
	client := &liveClient{settings: settings, model: capture.model, client: capture}
	node := store.Node{
		ID: "job", Brief: "compare the approaches with evidence",
		Provenance: store.Provenance{Intent: "recommend an approach", SessionID: "s1"},
	}
	judgment := judgeDeliverable(context.Background(), settings, client, graph, node, "approach A wins", "worker/model")
	if !judgment.Checked || !judgment.Pass {
		t.Fatalf("judgment = %+v, want a checked pass", judgment)
	}
	if capture.class != provider.ClassPlanAudit {
		t.Fatalf("gate class = %q, want a planning-shaped audit call", capture.class)
	}
	if capture.responseMode {
		t.Fatal("no-panel gate added structured-output request options; want the bare adapter request")
	}
	body := capture.messages[len(capture.messages)-1].Content[0].Text
	if !strings.Contains(body, "the user always wants benchmark evidence named explicitly") {
		t.Fatalf("gate input omitted the standing user preference: %q", body)
	}
	if marker := strings.Index(body, "Standing preferences and relevant lessons:\n"); marker < 0 || len(body[marker:]) > gateNotebookBytes+len("Standing preferences and relevant lessons:\n") {
		t.Fatalf("gate notebook block is absent or over its bound: %d bytes", len(body[marker:]))
	}
}

func TestConsolidatorSeesBadRidePatternAndCausationCaution(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "exercise bad rides", Stage: 0},
		{ID: "bad-a", Parent: "job", Brief: "first bad ride", Stage: 1},
		{ID: "bad-b", Parent: "job", Brief: "second bad ride", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "exercise bad rides"}); err != nil {
		t.Fatal(err)
	}
	fact, err := graph.RecordFact("", "repo:test", store.FactLesson, "always skip verification")
	if err != nil {
		t.Fatal(err)
	}
	for _, nodeID := range []string{"bad-a", "bad-b"} {
		if err := graph.RecordFactInjection(nodeID, []int64{fact.Seq}); err != nil {
			t.Fatal(err)
		}
		claim, won, err := graph.Claim(nodeID, "worker")
		if err != nil || !won {
			t.Fatalf("claim %s: won=%t err=%v", nodeID, won, err)
		}
		if err := graph.Fail(claim, "verification was skipped"); err != nil {
			t.Fatal(err)
		}
	}
	facts, err := graph.ActiveFacts("repo:test", 10)
	if err != nil {
		t.Fatal(err)
	}

	settings := config.Config{Model: "talk/model"}
	capture := &gateCaptureClient{model: "talk/model"}
	client := &liveClient{settings: settings, model: capture.model, client: capture}
	if _, err := consolidateFacts(settings, client, graph)(context.Background(), "repo:test", facts); err != nil {
		t.Fatal(err)
	}
	if len(capture.messages) != 2 {
		t.Fatalf("consolidator messages = %d, want 2", len(capture.messages))
	}
	system := capture.messages[0].Content[0].Text
	for _, want := range []string{"Co-occurrence is not causation", "one bad job is never enough", "quarantines"} {
		if !strings.Contains(system, want) {
			t.Errorf("consolidator doctrine omitted %q", want)
		}
	}
	user := capture.messages[1].Content[0].Text
	wantRide := fmt.Sprintf("#%d [lesson · ", fact.Seq)
	if !strings.Contains(user, wantRide) || !strings.Contains(user, "rode 2 jobs, 2 ended badly") {
		t.Fatalf("consolidator input omitted bad rides: %q", user)
	}
}

func TestSingleLeafProfileCarriesPolishedGateVerdict(t *testing.T) {
	dir := t.TempDir()
	settings := config.Config{Model: "configured/model", ProfileDir: dir}
	node := store.Node{Brief: "deliver every requested section", Title: "Complete delivery"}
	outcome := &exec.Outcome{
		Turns: 6, Stop: exec.StopDone, Verdict: provider.VerdictSemanticFailure,
		Usage: exec.Usage{PromptTokens: 120, CompletionTokens: 30},
	}
	recordSingleLeaf(settings, "polish/model", node, outcome)

	measured, err := profile.Load(dir, "polish/model", "linear")
	if err != nil {
		t.Fatal(err)
	}
	if len(measured.Records) != 1 || measured.Records[0].Verdict != provider.VerdictSemanticFailure ||
		measured.Records[0].Tokens != 150 || measured.Model != "polish/model" {
		t.Fatalf("profile = %+v, want the polished worker and gate failure", measured)
	}
}

func TestParseLearnedFactsAcceptsQuarantineOnlyDecision(t *testing.T) {
	learned := parseLearnedFacts(`{"facts":[{"quarantines":[12,13]}]}`, 8)
	if len(learned) != 1 || len(learned[0].Quarantines) != 2 ||
		learned[0].Quarantines[0] != 12 || learned[0].Quarantines[1] != 13 {
		t.Fatalf("parsed quarantine = %+v", learned)
	}
}
