package main

import (
	"context"
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
