package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type gateCaptureClient struct {
	model        string
	messages     []ai.Message
	class        provider.CallClass
	responseMode bool
	response     string
}

func TestChatCommanderResolvesJobWorkspaceFilesAndDirectory(t *testing.T) {
	root := t.TempDir()
	graph, err := store.Open(filepath.Join(root, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "produce the artifact", Stage: 0},
		{ID: "leaf", Parent: "job", Brief: "write it", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "produce the artifact"}); err != nil {
		t.Fatal(err)
	}
	workspaceRoot := filepath.Join(root, "workspace")
	jobDir := filepath.Join(workspaceRoot, "job")
	if err := os.MkdirAll(jobDir, 0o700); err != nil {
		t.Fatal(err)
	}
	deliverable := filepath.Join(jobDir, "deliverable.md")
	if err := os.WriteFile(deliverable, []byte("done"), 0o600); err != nil {
		t.Fatal(err)
	}
	commander := &chatCommander{store: graph, workspaceRoot: workspaceRoot}
	if target, ok := commander.ResolveWorkspacePath("leaf", "deliverable.md"); !ok || target != deliverable {
		t.Fatalf("workspace file = (%q, %v), want (%q, true)", target, ok, deliverable)
	}
	if _, ok := commander.ResolveWorkspacePath("leaf", "missing.md"); ok {
		t.Fatal("nonexistent workspace file resolved")
	}
	if _, ok := commander.ResolveWorkspacePath("leaf", "../outside.md"); ok {
		t.Fatal("workspace traversal escaped the job directory")
	}
	if target, ok := commander.WorkspacePath("leaf"); !ok || target != jobDir {
		t.Fatalf("workspace directory = (%q, %v), want (%q, true)", target, ok, jobDir)
	}
}

func TestChatCommanderKeepsFoldedJobWorkspaceAfterTerritoryReparent(t *testing.T) {
	root := t.TempDir()
	graph, err := store.Open(filepath.Join(root, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "produce the artifact", Stage: 0},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "produce the artifact"}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("job", "worker")
	if err != nil || !won {
		t.Fatalf("claim job: won=%t err=%v", won, err)
	}
	if err := graph.Complete(claim, "artifact delivered"); err != nil {
		t.Fatal(err)
	}
	workspaceRoot := filepath.Join(root, "workspace")
	jobDir := filepath.Join(workspaceRoot, "job")
	if err := os.MkdirAll(jobDir, 0o700); err != nil {
		t.Fatal(err)
	}
	pointer := filepath.Join(jobDir, "deliverable.md")
	if err := os.WriteFile(pointer, []byte("done"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := graph.Fold("job", "artifact delivered", []string{pointer}); err != nil {
		t.Fatal(err)
	}
	if err := graph.FormTerritory("territory", "Artifacts", "related artifact work", nil, []string{"job"}); err != nil {
		t.Fatal(err)
	}

	commander := &chatCommander{store: graph, workspaceRoot: workspaceRoot}
	if target, ok := commander.WorkspacePath("job"); !ok || target != jobDir {
		t.Fatalf("reparented workspace = (%q, %v), want (%q, true)", target, ok, jobDir)
	}
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
	text := c.response
	if text == "" {
		text = `{"pass":true}`
	}
	return &ai.Response{Choices: []ai.Choice{{
		Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}},
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
	const settledTaste = "the user wants written comparisons kept under a page"
	candidate, err := graph.RecordTasteCandidate("", "user", settledTaste)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.PromoteTasteRule(candidate.Seq); err != nil {
		t.Fatal(err)
	}

	settings := config.Config{Model: "worker/model"}
	capture := &gateCaptureClient{model: "worker/model"}
	client := &liveClient{settings: settings, model: capture.model, client: capture}
	// The job's own working decisions ride the brief the resident anchored them
	// onto, which is what the gate is handed as the compiled goal. An assumption
	// that never leaves the receipt is a promise nobody is held to.
	const securityDecision = "review the diff for security regressions before pushing"
	node := store.Node{
		ID: "job",
		Brief: "compare the approaches with evidence\n\n" +
			resident.WorkingDecisionsHeader + "\n- " + securityDecision,
		Provenance: store.Provenance{Intent: "recommend an approach", SessionID: "s1"},
	}
	judgment := judgeDeliverable(context.Background(), settings, client, graph, node, "approach A wins", deliveryEvidence{}, "worker/model")
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
	// The digest block runs from its heading to the request that follows it —
	// it stopped being the tail of the prompt when the standing blocks moved
	// ahead of the job, so its bound is measured against its own end.
	const digestHeading = "Standing preferences and relevant lessons:\n"
	marker := strings.Index(body, digestHeading)
	end := strings.Index(body, "Verbatim request:\n")
	if marker < 0 || end <= marker || len(body[marker:end]) > gateNotebookBytes+len(digestHeading)+len("\n\n") {
		t.Fatalf("gate notebook block is absent or over its bound: marker=%d end=%d", marker, end)
	}
	// Settled taste is not one lesson among eight — it is what an acceptable
	// answer looks like, so it is read before the digest and cannot be crowded
	// out by the digest's byte budget.
	taste := strings.Index(body, "Settled taste — hold to these:\n")
	if taste < 0 || !strings.Contains(body[taste:], settledTaste) {
		t.Fatalf("gate input omitted the settled taste rule: %q", body)
	}
	if notebook := strings.Index(body, "Standing preferences and relevant lessons:\n"); taste > notebook {
		t.Fatalf("settled taste ranked after the notebook digest: taste=%d notebook=%d", taste, notebook)
	}
	if !strings.Contains(body, resident.WorkingDecisionsHeader) || !strings.Contains(body, securityDecision) {
		t.Fatalf("gate input omitted the job's working decisions: %q", body)
	}
	if !strings.Contains(judgeDeliverablePrompt, "Working decisions declared in the goal are part of what was promised") {
		t.Fatal("the gate prompt does not hold the deliverable to the decisions it is shown")
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
	if _, err := consolidateFacts(settings, client, graph)(context.Background(), fact.Scope, []store.Fact{fact}, nil); err != nil {
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
	if _, err := consolidateFacts(settings, client, graph)(context.Background(), "repo:test", facts, nil); err != nil {
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

func TestRetrospectivePrioritizesAndRendersSurprise(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "reflection.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	settings := config.Config{Model: "talk/model"}
	capture := &gateCaptureClient{model: settings.Model}
	client := &liveClient{settings: settings, model: capture.model, client: capture}
	surprise := 1.875
	jobs := []resident.JobSketch{{
		Title: "Mispredicted report", Ask: "write the report", Outcome: "report delivered", Age: "today",
		NodeCount: 2, PromptTokens: 140, CompletionTokens: 25, Cost: 0.0125,
		SurpriseTokens: 165, ExpectedTokens: 72, Surprise: &surprise,
	}}
	if _, err := reflectAcrossJobs(settings, client, graph)(context.Background(), jobs); err != nil {
		t.Fatal(err)
	}
	if len(capture.messages) != 2 {
		t.Fatalf("reflection messages = %d, want 2", len(capture.messages))
	}
	system := capture.messages[0].Content[0].Text
	if !strings.Contains(system, "Consider the most mispredicted jobs first") ||
		!strings.Contains(system, "where the self-model is most wrong") {
		t.Fatalf("reflection prompt omitted surprise priority: %q", system)
	}
	user := capture.messages[1].Content[0].Text
	for _, want := range []string{
		"2 nodes · 165 tok · $0.0125",
		"predicted 72 tok — 2.3× over",
	} {
		if !strings.Contains(user, want) {
			t.Errorf("reflection input = %q, want %q", user, want)
		}
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

func TestParseConsolidationKeepsOneScopeAliasJudgment(t *testing.T) {
	parsed := parseConsolidation(`{"facts":[],"scope_alias":{"merge":true,"canonical":"domain:podcast"}}`, 8)
	if parsed.ScopeAlias == nil || !parsed.ScopeAlias.Merge || parsed.ScopeAlias.Canonical != "domain:podcast" {
		t.Fatalf("scope alias judgment = %+v", parsed.ScopeAlias)
	}
}

func TestConsolidatorOffersExactlyOneScopeCandidateJudgment(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	settings := config.Config{Model: "talk/model"}
	capture := &gateCaptureClient{model: "talk/model"}
	client := &liveClient{settings: settings, model: capture.model, client: capture}
	candidate := &resident.ScopePair{First: "domain:podcast", Second: "domain:podcasts"}
	if _, err := consolidateFacts(settings, client, graph)(context.Background(), "", nil, candidate); err != nil {
		t.Fatal(err)
	}
	if len(capture.messages) != 2 {
		t.Fatalf("consolidator messages = %d, want 2", len(capture.messages))
	}
	system := capture.messages[0].Content[0].Text
	if !strings.Contains(system, "make exactly one additional judgment") ||
		!strings.Contains(system, `{"merge":false,"canonical":""}`) {
		t.Fatalf("scope gardening doctrine missing from prompt: %q", system)
	}
	user := capture.messages[1].Content[0].Text
	if strings.Count(user, "Scope-gardening candidate:") != 1 ||
		strings.Count(user, "- "+candidate.First+"\n") != 1 ||
		strings.Count(user, "- "+candidate.Second+"\n") != 1 {
		t.Fatalf("scope candidate input = %q", user)
	}
}

func TestReflexEnvelopeSkipsDeliveryGate(t *testing.T) {
	if reflexTurns != 4 || reflexTokens != chatLeafTokens/8 {
		t.Fatalf("reflex envelope = %d turns/%d tokens", reflexTurns, reflexTokens)
	}
	outcome := &exec.Outcome{Stop: exec.StopDone}
	reflex := store.Node{ID: "reflex-1", Parent: store.RootID, Group: resident.ReflexGroup}
	if shouldGate(reflex, outcome, false) {
		t.Fatal("reflex reached delivery gate")
	}
	if !shouldPromoteReflex(reflex, &exec.Outcome{Stop: exec.StopBudget}) {
		t.Fatal("budget-stopped reflex did not promote")
	}
	ordinary := store.Node{ID: "task-1", Parent: store.RootID}
	if !shouldGate(ordinary, outcome, false) {
		t.Fatal("ordinary root leaf unexpectedly skipped delivery gate")
	}
	if shouldPromoteReflex(ordinary, &exec.Outcome{Stop: exec.StopBudget}) {
		t.Fatal("ordinary budget stop was mislabeled reflex promotion")
	}
	if shouldGate(ordinary, &exec.Outcome{Stop: exec.StopBudget}, true) {
		t.Fatal("budget partial reached delivery gate")
	}
	repair := store.Node{ID: "task-1-x2-n1", Parent: store.RootID}
	if !shouldGate(repair, &exec.Outcome{Stop: exec.StopDone}, false) {
		t.Fatal("completed continuation skipped the final delivery gate")
	}
	if got, want := continuationMessage(3), "splitting the remaining work -- 3 pieces queued"; got != want {
		t.Fatalf("continuation message = %q, want %q", got, want)
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

// With no learned voice the user-facing prompts are the stable prompt plus the
// unconditional register, exactly — nothing else drifts, and the register is
// what a fresh machine used to be missing entirely.
func TestResidentUserFacingPromptsKeepEmptyNotebookBytes(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	settings := config.Config{Model: "talk/model"}
	capture := &gateCaptureClient{model: "talk/model"}
	client := &liveClient{settings: settings, model: capture.model, client: capture}
	if _, err := narrateProgress(settings, client, graph)(context.Background(), resident.Narration{
		Goal: "prepare the report",
	}); err != nil {
		t.Fatal(err)
	}
	wantNarrator := narratorSystemPrompt + "\n\n" + resident.VoiceRegister
	if got := capture.messages[0].Content[0].Text; got != wantNarrator {
		t.Fatalf("empty-notebook narrator prompt changed:\n got %q\nwant %q", got, wantNarrator)
	}

	node := store.Node{
		ID: "job", Parent: store.RootID, Brief: "assemble the finished report",
		Provenance: store.Provenance{Intent: "prepare the report"},
	}
	wantBrief := node.Brief + "\n\n" + resident.VoiceRegister
	if got := residentDeliveryBrief(graph, node); got != wantBrief {
		t.Fatalf("empty-notebook delivery brief changed:\n got %q\nwant %q", got, wantBrief)
	}
	initial := exec.Task{Brief: residentDeliveryBrief(graph, node)}
	polish := initial
	if initial.Brief != wantBrief || polish.Brief != wantBrief {
		t.Fatalf("empty-notebook initial/polish briefs changed: initial=%q polish=%q", initial.Brief, polish.Brief)
	}

	const deliverable = "the finished report"
	judgment := judgeDeliverable(context.Background(), settings, client, graph, node, deliverable, deliveryEvidence{}, "worker/model")
	if !judgment.Checked || !judgment.Pass {
		t.Fatalf("judgment = %+v, want checked pass", judgment)
	}
	if got := capture.messages[0].Content[0].Text; got != judgeDeliverablePrompt {
		t.Fatalf("empty-notebook gate system prompt changed:\n got %q\nwant %q", got, judgeDeliverablePrompt)
	}
	wantBody := "Verbatim request:\n" + node.Provenance.Intent +
		"\n\nCompiled goal:\n" + node.Brief +
		"\n\nDeliverable as produced:\n" + deliverable
	if got := capture.messages[1].Content[0].Text; got != wantBody {
		t.Fatalf("empty-notebook gate body changed:\n got %q\nwant %q", got, wantBody)
	}
}

func TestResidentDeliveryAndPolishBriefShareLearnedVoice(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	const preference = "keep answers short; no preamble"
	if _, err := graph.RecordFact("", "user", store.FactPreference, preference); err != nil {
		t.Fatal(err)
	}
	node := store.Node{
		ID: "job", Parent: store.RootID, Brief: "assemble the finished report",
		Provenance: store.Provenance{Intent: "prepare the report"},
	}
	initial := exec.Task{Brief: residentDeliveryBrief(graph, node)}
	polish := initial
	for name, brief := range map[string]string{"delivery": initial.Brief, "polish": polish.Brief} {
		if !strings.Contains(brief, preference) {
			t.Fatalf("%s brief omitted learned voice: %q", name, brief)
		}
	}
	settings := config.Config{Model: "talk/model"}
	capture := &gateCaptureClient{model: "talk/model"}
	client := &liveClient{settings: settings, model: capture.model, client: capture}
	if _, err := narrateProgress(settings, client, graph)(context.Background(), resident.Narration{
		Goal: node.Provenance.Intent,
	}); err != nil {
		t.Fatal(err)
	}
	if system := capture.messages[0].Content[0].Text; !strings.Contains(system, preference) {
		t.Fatalf("narrator prompt omitted learned voice: %q", system)
	}
	child := node
	child.Parent = node.ID
	if got := residentDeliveryBrief(graph, child); strings.Contains(got, preference) {
		t.Fatalf("worker-to-worker child brief gained user voice: %q", got)
	}
}

func TestDistillerParsesVoiceCorrectionAsUserPreference(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	settings := config.Config{Model: "talk/model"}
	capture := &gateCaptureClient{
		model:    "talk/model",
		response: `{"facts":[{"scope":"user","kind":"preference","body":"keep answers short; no preamble"}]}`,
	}
	client := &liveClient{settings: settings, model: capture.model, client: capture}
	learned, err := distillFacts(settings, client, graph)(
		context.Background(),
		"Revise the earlier report",
		"The user corrected the delivery: make it shorter and remove the preamble.",
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(learned) != 1 || learned[0].Scope != "user" ||
		learned[0].Kind != store.FactPreference ||
		learned[0].Body != "keep answers short; no preamble" {
		t.Fatalf("distilled voice correction = %+v", learned)
	}
	system := capture.messages[0].Content[0].Text
	for _, want := range []string{"HOW something was communicated", `scope "user"`, `kind "preference"`, "direct instruction"} {
		if !strings.Contains(system, want) {
			t.Errorf("distiller voice judgment omitted %q", want)
		}
	}
	if user := capture.messages[1].Content[0].Text; !strings.Contains(user, "make it shorter and remove the preamble") {
		t.Fatalf("distiller input omitted the voice correction: %q", user)
	}
}

// The document-staging journey moved to internal/exec, where copy-at-mention
// and copy-at-run are one seam. Its tests moved with it.
func TestAttachedDocumentsAreNamedInTheBrief(t *testing.T) {
	brief := withDocumentAttachmentBrief("Summarise the filing.", []string{"attachments/q3-filing-ab12cd34.pdf"})
	if !strings.Contains(brief, "read_document") ||
		!strings.Contains(brief, "attachments/q3-filing-ab12cd34.pdf") ||
		!strings.HasPrefix(brief, "Summarise the filing.") {
		t.Fatalf("brief = %q", brief)
	}
	if plain := withDocumentAttachmentBrief("Summarise the filing.", nil); plain != "Summarise the filing." {
		t.Fatalf("unattached brief changed: %q", plain)
	}
}

// A visitor holds no provider clients. Every Commander capability the TUI can
// reach must still answer honestly instead of dereferencing a client that this
// process never built, and the ones that would need a head must refuse in
// words rather than panic.
func TestVisitorCommanderServesTheSurfaceWithoutClients(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := saveChatPrefs(root, chatPrefs{ChatModel: "talk/remembered", TaskModel: "work/remembered"}); err != nil {
		t.Fatal(err)
	}

	attached := ""
	commander := newVisitorCommander(path, "visitor-session", graph, func(id string) error {
		attached = id
		return nil
	})
	var _ tui.Commander = commander

	if got := commander.CurrentModel("talk"); got != "talk/remembered" {
		t.Fatalf("visitor talk model = %q, want the recorded preference", got)
	}
	if got := commander.CurrentModel("work"); got != "work/remembered" {
		t.Fatalf("visitor work model = %q, want the recorded preference", got)
	}
	for _, role := range []string{"talk", "work", "boost", "voice", "image", "speech", "music", "video"} {
		commander.CurrentModel(role)
		commander.CatalogFor(role)
		commander.ImageInputSupportFor(role)
		commander.ModelFollows(role)
	}
	if len(commander.Models()) == 0 || len(commander.Catalog()) == 0 {
		t.Fatal("visitor offered no models to pick from")
	}
	for _, role := range []string{"talk", "work"} {
		if err := commander.SetModel(role, "some/other"); err == nil {
			t.Fatalf("visitor was allowed to switch the %s model", role)
		}
	}
	if commander.StreamEvents() != nil {
		t.Fatal("visitor exposed a stream it does not produce")
	}
	commander.Notebook(5)
	commander.SearchNotebook("anything", 5)
	commander.NodeTrace("missing", 128)
	commander.SplitPct()
	if commander.DatabasePath() != path {
		t.Fatalf("visitor database path = %q", commander.DatabasePath())
	}

	// A visitor's session is real: it may open a new one, and its messages and
	// cancellations reach the elected resident through the journal.
	session, err := commander.NewSession()
	if err != nil || session == "" || attached != session {
		t.Fatalf("visitor new session = %q, attached %q, err %v", session, attached, err)
	}
	if _, err := graph.PostMessage(store.Message{
		SessionID: session, Role: store.RoleUser, Body: "visitor asks",
	}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{
		Nodes: []store.NodeSpec{{ID: "visitor-job", Brief: "visitor asks", Stage: 1}},
	}, store.Provenance{Origin: store.OriginUser, SessionID: session, Intent: "visitor asks"}); err != nil {
		t.Fatal(err)
	}
	if err := commander.Cancel("visitor-job"); err != nil {
		t.Fatal(err)
	}
	pending, err := graph.PendingCommands(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Kind != store.CommandCancel || pending[0].SessionID != session {
		t.Fatalf("visitor cancel did not reach the journal: %+v", pending)
	}
}

// A job whose plan is not in hand cannot be revised, and saying so is the whole
// point. An empty Redirection with a nil error is byte-for-byte what "the
// sentinel read the plan and found nothing to change" looks like to the caller,
// so the user was handed that receipt — "nothing in the remaining plan needed
// to change" — while every pending leaf went on building the version they had
// just asked to replace. The error is what routes the caller to its honest
// branch instead.
func TestReviseForUserWithoutARetainedPlanSaysSo(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Brief: "write a client for the v1 API", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "write a client for the v1 API"}); err != nil {
		t.Fatal(err)
	}
	job, _, err := graph.Node("task-1")
	if err != nil {
		t.Fatal(err)
	}
	plans := &jobPlans{graphs: map[string]plannedJob{}}
	revision, err := plans.reviseForUser(context.Background(), config.Config{}, nil, graph, job,
		"no, use the v2 API not v1", resident.RevisionRedirect)
	if !errors.Is(err, errNoRetainedPlan) {
		t.Fatalf("revise without a retained plan returned %v; the caller cannot tell this from a no-op", err)
	}
	if revision.Added != 0 || revision.Dropped != 0 || revision.Amended != 0 ||
		len(revision.Notes) != 0 || len(revision.RunningRemovals) != 0 {
		t.Fatalf("revision = %+v", revision)
	}
	// The reconciler's own receipt has to reach its "I could not revise" arm.
	reconciler := resident.New(graph, nil, nil).WithRedirector(
		func(context.Context, store.Node, string, resident.RevisionFlavor) (resident.Redirection, error) {
			return resident.Redirection{}, errNoRetainedPlan
		})
	if _, err := graph.RequestCommand(store.Command{
		SessionID: "s1", Kind: store.CommandRedirect, Target: "task-1",
		Instruction: "no, use the v2 API not v1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	messages, err := graph.Messages("s1", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	var receipts string
	for _, message := range messages {
		receipts += message.Body + "\n"
	}
	if strings.Contains(receipts, "nothing in the remaining plan needed to change") {
		t.Fatalf("the receipt claims the plan was examined:\n%s", receipts)
	}
	if !strings.Contains(receipts, "could not revise the remaining plan") {
		t.Fatalf("the receipt never says the plan could not be read:\n%s", receipts)
	}
}

// The gate waved through a deliverable that described itself. "The deliverable
// is written and verified against the actual repo source" became the node's
// summary, and the summary is the one source every later surface reads, so the
// verdict the user asked for existed nowhere they or the head could reach. The
// contract is expressed as a value rather than a phrase list, because the next
// way to report work instead of doing it is always a phrasing nobody wrote
// down; what is pinned here is that the value is stated and that the gate is
// still a gate.
func TestTheGateHoldsTheAnswerFirstContractWithoutBecomingACritic(t *testing.T) {
	for _, required := range []string{
		"What you are handed IS the deliverable",
		"has described the deliverable in place of being it",
		"A pointer to where the answer lives is not the answer",
	} {
		if !strings.Contains(judgeDeliverablePrompt, required) {
			t.Fatalf("the gate no longer states the answer-first contract: %q missing", required)
		}
	}
	// The character of the gate is law: its default is pass, and the new clause
	// must not turn it into the critic that always finds something.
	for _, required := range []string{
		"Default to PASS",
		"The gate exists for real gaps, not polish",
		"This is still one absence and not a second style test",
	} {
		if !strings.Contains(judgeDeliverablePrompt, required) {
			t.Fatalf("the gate lost its default-pass character: %q missing", required)
		}
	}
	// No phrase list anywhere: the gate must never be taught to match on the
	// sentences one worker happened to write.
	for _, forbidden := range []string{
		"deliverable is written and verified", "if the text contains", "phrases such as",
	} {
		if strings.Contains(strings.ToLower(judgeDeliverablePrompt), strings.ToLower(forbidden)) {
			t.Fatalf("the gate grew a cue list: %q", forbidden)
		}
	}
}

// The gate cannot open the workspace, so the only verification it can hold is
// whether the deliverable shows the finished thing being used the way it will
// be used. A leaf that exercised every part in a harness and reported the whole
// verified is the case: the claim is a promise about evidence like any other,
// and it is a gap when the evidence is an inference. The honest exit passes, or
// the clause would be teaching the lie it exists to catch.
func TestTheGateHoldsAClaimOfVerifiedToAnActualRun(t *testing.T) {
	for name, required := range map[string]string{
		"a claim of evidence is a commitment": "A claim that the work was checked, proven or verified is itself such a commitment",
		"the whole thing, used as it is used": "shows the finished thing exercised the way it will actually be used",
		"no inference from the parts":         "rather than its parts checked one by one and the whole inferred from them",
		"the honest gap passes":               "is not a gap: it is the honest form of the same claim and it passes",
	} {
		if !strings.Contains(judgeDeliverablePrompt, required) {
			t.Errorf("the gate no longer holds %s: %q missing", name, required)
		}
	}
	// Still a gate and not a critic: the clause rides the paragraph that was
	// already about promised evidence rather than opening a second test.
	decisions := strings.Index(judgeDeliverablePrompt, "Working decisions declared in the goal")
	claim := strings.Index(judgeDeliverablePrompt, "A claim that the work was checked")
	substance := strings.Index(judgeDeliverablePrompt, "One absence counts exactly like every other")
	if decisions < 0 || claim < decisions || substance < claim {
		t.Fatalf("the verification clause left its paragraph: decisions=%d claim=%d substance=%d",
			decisions, claim, substance)
	}
}

// A named gap is what buys the one revision pass, and the revision is told the
// thing the first attempt demonstrably did not hear.
func TestNamedGapEarnsARevisionThatIsToldWhereTheAnswerGoes(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	node := store.Node{
		ID: "task-6979", Parent: store.RootID, Brief: "judge whether the plan is valid",
		Provenance: store.Provenance{Intent: "see if their architecture plan is valid"},
	}
	settings := config.Config{Model: "worker/model"}

	const gap = "the verdict itself: the text says the assessment is complete but never says whether the plan is valid"
	failing := &gateCaptureClient{model: "worker/model",
		response: `{"pass":false,"gaps":"` + gap + `"}`}
	judgment := judgeDeliverable(context.Background(), settings,
		&liveClient{settings: settings, model: failing.model, client: failing}, graph, node,
		"The deliverable is written and verified against the actual repo source.", deliveryEvidence{}, "worker/model")
	if !judgment.Checked || judgment.Pass || judgment.Gaps != gap {
		t.Fatalf("a meta-only deliverable did not draw a checked gap: %+v", judgment)
	}

	// Substance passes, and passing is the default: the gate must not have
	// become a second opinion on every finished job.
	passing := &gateCaptureClient{model: "worker/model"}
	settled := judgeDeliverable(context.Background(), settings,
		&liveClient{settings: settings, model: passing.model, client: passing}, graph, node,
		"The plan is valid: the plugin boundary it assumes already exists and the migration is reversible.",
		deliveryEvidence{}, "worker/model")
	if !settled.Checked || !settled.Pass {
		t.Fatalf("a deliverable carrying its answer did not pass: %+v", settled)
	}

	// The revision pass is handed the previous attempt plus the contract that
	// attempt broke, so a second round cannot close the gap inside a file and
	// report that it did so.
	for _, required := range []string{
		"final message is the deliverable", "never in place of it",
	} {
		if !strings.Contains(gateRevisionContract, required) {
			t.Fatalf("the revision contract no longer says where the answer goes: %q missing", required)
		}
	}
}
