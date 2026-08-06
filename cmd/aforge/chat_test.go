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
	"github.com/Agent-Field/aforge-v2/internal/resident"
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

func TestParseLearnedFactsCarriesSkillCandidate(t *testing.T) {
	learned := parseLearnedFacts(`{"facts":[{"scope":"tool:git","kind":"skill","body":"git-audit checks a repository","skill":{"artifact":" /workspace/git-audit "}}]}`, 5)
	if len(learned) != 1 || learned[0].Kind != store.FactSkill || learned[0].Skill == nil ||
		learned[0].Skill.Artifact != "/workspace/git-audit" {
		t.Fatalf("parsed skill candidate = %+v", learned)
	}

	malformed := parseLearnedFacts(`{"facts":[
		{"scope":"tool:git","kind":"skill","body":"missing artifact"},
		{"scope":"tool:git","kind":"lesson","body":"wrong kind","skill":{"artifact":"/tmp/x"}}
	]}`, 5)
	if len(malformed) != 1 || malformed[0].Kind != store.FactSkill || malformed[0].Skill != nil {
		t.Fatalf("malformed candidate handling = %+v", malformed)
	}
}

func TestParseLearnedFactsAcceptsDistilledAndConsolidatedPlaybooks(t *testing.T) {
	distilled := parseLearnedFacts(`{"facts":[{"scope":"tool:pdftotext","kind":"playbook","body":"Try pdftotext before OCR; the text route preserved columns in trial #42"}]}`, 5)
	if len(distilled) != 1 || distilled[0].Kind != store.FactPlaybook ||
		distilled[0].Scope != "tool:pdftotext" {
		t.Fatalf("parsed distilled playbook = %+v", distilled)
	}
	consolidated := parseLearnedFacts(`{"facts":[{"scope":"repo:parser","kind":"playbook","body":"Run make check; it configures generated fixtures","sources":[17,11],"replaces":17}]}`, 8)
	if len(consolidated) != 1 || consolidated[0].Kind != store.FactPlaybook ||
		consolidated[0].Replaces != 17 || len(consolidated[0].Sources) != 2 {
		t.Fatalf("parsed consolidated playbook = %+v", consolidated)
	}
	for name, prompt := range map[string]string{
		"distiller": distillerSystemPrompt, "consolidator": consolidatorSystemPrompt,
	} {
		if !strings.Contains(prompt, "playbook") {
			t.Errorf("%s prompt does not offer playbook output", name)
		}
	}
}

func TestConsolidatorRendersPlaybookScope(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	fact, err := graph.RecordFact("", "repo:parser", store.FactPlaybook,
		"Run make check; it configures generated fixtures")
	if err != nil {
		t.Fatal(err)
	}
	settings := config.Config{Model: "talk/model"}
	capture := &gateCaptureClient{model: "talk/model"}
	client := &liveClient{settings: settings, model: capture.model, client: capture}
	if _, err := consolidateFacts(settings, client, graph)(context.Background(), fact.Scope, []store.Fact{fact}); err != nil {
		t.Fatal(err)
	}
	user := capture.messages[1].Content[0].Text
	want := fmt.Sprintf("#%d [repo:parser · playbook · ", fact.Seq)
	if !strings.Contains(user, want) {
		t.Fatalf("consolidator playbook input omitted scope: %q", user)
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

func TestParseLearnedFactsKeepsStructuredUnsettledPair(t *testing.T) {
	raw := `{"facts":[{"scope":"domain:parsing","kind":"unsettled","body":"ignored projection","unsettled":{"approaches":[{"approach":"table-driven","scope":"stable grammars","evidence":[11]},{"approach":"combinators","scope":"changing grammars","evidence":[17]}]},"sources":[11,17]}]}`
	learned := parseLearnedFacts(raw, 5)
	if len(learned) != 1 || learned[0].Kind != store.FactUnsettled || learned[0].Unsettled == nil {
		t.Fatalf("parsed facts = %+v", learned)
	}
	pair := learned[0].Unsettled
	if len(pair.Approaches) != 2 || pair.Approaches[0].Evidence[0] != 11 || pair.Approaches[1].Scope != "changing grammars" {
		t.Fatalf("parsed unsettled pair = %+v", pair)
	}
	if learned[0].Body != store.FormatUnsettledPair(*pair) {
		t.Fatalf("body = %q, want canonical projection %q", learned[0].Body, store.FormatUnsettledPair(*pair))
	}
}

func TestParseLearnedFactsAcceptsQuarantineOnlyDecision(t *testing.T) {
	learned := parseLearnedFacts(`{"facts":[{"quarantines":[12,13]}]}`, 8)
	if len(learned) != 1 || len(learned[0].Quarantines) != 2 ||
		learned[0].Quarantines[0] != 12 || learned[0].Quarantines[1] != 13 {
		t.Fatalf("parsed quarantine = %+v", learned)
	}
}

func TestReflexEnvelopeSkipsDeliveryGate(t *testing.T) {
	if reflexTurns != 4 || reflexTokens != chatLeafTokens/8 {
		t.Fatalf("reflex envelope = %d turns/%d tokens", reflexTurns, reflexTokens)
	}
	outcome := &exec.Outcome{Stop: exec.StopDone}
	reflex := store.Node{ID: "reflex-1", Parent: store.RootID, Group: resident.ReflexGroup}
	if shouldGate(reflex, outcome) {
		t.Fatal("reflex reached delivery gate")
	}
	if !shouldPromoteReflex(reflex, &exec.Outcome{Stop: exec.StopBudget}) {
		t.Fatal("budget-stopped reflex did not promote")
	}
	ordinary := store.Node{ID: "task-1", Parent: store.RootID}
	if !shouldGate(ordinary, outcome) {
		t.Fatal("ordinary root leaf unexpectedly skipped delivery gate")
	}
	if shouldPromoteReflex(ordinary, &exec.Outcome{Stop: exec.StopBudget}) {
		t.Fatal("ordinary budget stop was mislabeled reflex promotion")
	}
	if shouldGate(ordinary, &exec.Outcome{Stop: exec.StopBudget}) {
		t.Fatal("budget partial reached delivery gate")
	}
}

func TestRecordReflexPersistsBoundaryEvidence(t *testing.T) {
	dir := t.TempDir()
	settings := config.Config{Model: "configured/model", ProfileDir: dir}
	node := store.Node{Brief: "quick local action", Title: "Quick action"}
	outcome := &exec.Outcome{
		Turns: 4, Stop: exec.StopBudget, Verdict: provider.VerdictBudgetStop,
		Usage: exec.Usage{PromptTokens: 80, CompletionTokens: 20, Cost: 0.0125},
	}
	recordReflex(settings, "worker/model", node, outcome, true)

	measured, err := profile.Load(dir, "worker/model", "linear")
	if err != nil {
		t.Fatal(err)
	}
	if len(measured.Records) != 1 {
		t.Fatalf("profile records = %+v", measured.Records)
	}
	record := measured.Records[0]
	if record.Size != profile.BucketReflex || !record.Promoted || record.Cost != 0.0125 ||
		record.Tokens != 100 || record.Turns != 4 || record.Verdict != provider.VerdictBudgetStop {
		t.Fatalf("reflex profile record = %+v", record)
	}
}

func TestBudgetStoppedReflexCarriesPartialIntoCompiledJob(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "reflex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	ask := "Inspect the parser and fix its edge case."
	command, err := graph.RequestCommand(store.Command{
		SessionID: "budget-promotion", Kind: store.CommandSplice, Reflex: true, Instruction: ask,
	})
	if err != nil {
		t.Fatal(err)
	}
	var compiledInstruction, compiledContext string
	reconciler := resident.New(graph,
		func(_ context.Context, instruction, graphContext string) (resident.Compiled, error) {
			compiledInstruction, compiledContext = instruction, graphContext
			return resident.Compiled{Goal: "Fix and verify the parser edge case"}, nil
		},
		func(_ context.Context, compiled resident.Compiled) (store.Subtree, error) {
			return store.Subtree{Nodes: []store.NodeSpec{{
				ID: "compiled-after-budget", Brief: compiled.Goal, Stage: 1,
			}}}, nil
		},
	)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	reflexID := fmt.Sprintf("reflex-%d", command.Seq)
	budgetOutcome := &exec.Outcome{
		Stop: exec.StopBudget, Text: "partial: isolated the malformed escape sequence",
	}
	runner := resident.NewRunner(graph, func(_ context.Context, node store.Node) (resident.ExecResult, error) {
		return resident.ExecResult{
			Summary: budgetOutcome.Text,
			Promote: shouldPromoteReflex(node, budgetOutcome),
		}, nil
	}, "budget-reflex", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	if compiledInstruction != ask {
		t.Fatalf("compiled instruction = %q, want %q", compiledInstruction, ask)
	}
	for _, want := range []string{reflexID, ask, budgetOutcome.Text} {
		if !strings.Contains(compiledContext, want) {
			t.Errorf("compiled context omitted %q:\n%s", want, compiledContext)
		}
	}
	node, found, err := graph.Node("compiled-after-budget")
	if err != nil || !found || node.Provenance.Intent != ask {
		t.Fatalf("compiled job = %+v found=%t err=%v", node, found, err)
	}
}
