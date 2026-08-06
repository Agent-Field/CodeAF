package resident

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestExtractCuesOrdersAndDeduplicatesScopes(t *testing.T) {
	got := ExtractCues("Use git on internal/resident/notebook.go, then internal/resident/notebook.go and internal/store/facts.go with curl.")
	want := []string{
		"file:internal/resident/notebook.go",
		"file:internal/store/facts.go",
		"repo:internal/resident",
		"repo:internal",
		"repo:internal/store",
		"tool:git",
		"tool:curl",
		"user",
		"env",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractCues() = %#v, want %#v", got, want)
	}
}

func TestExtractCuesIncludesEveryExecutorTool(t *testing.T) {
	got := ExtractCues("Use sh, write, edit, and web to finish it.")
	for _, want := range []string{"tool:sh", "tool:write", "tool:edit", "tool:web"} {
		if !containsString(got, want) {
			t.Errorf("ExtractCues() = %#v, missing %q", got, want)
		}
	}
}

func TestFailureDistillationRecordsScopedQuirkOnce(t *testing.T) {
	graph := openStore(t)
	err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "goal", Brief: "Run the suite", Stage: 2},
		{ID: "flaky", Parent: "goal", Brief: "Run pytest", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "session-failure", Intent: "run the tests"})
	if err != nil {
		t.Fatalf("splice fixture: %v", err)
	}

	calls := 0
	reconciler := New(graph, nil, nil).WithDistiller(
		func(_ context.Context, goal, outcome string, failed bool) ([]Learned, error) {
			calls++
			if goal != "run the tests" || outcome != "pytest loses its cache" || !failed {
				t.Fatalf("distill input = goal %q outcome %q failed %v", goal, outcome, failed)
			}
			return []Learned{{
				Scope: "tool:pytest",
				Kind:  store.FactQuirk,
				Body:  "pytest can lose its cache mid-run",
			}}, nil
		})
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("initialize watcher: %v", err)
	}

	claim, won, err := graph.Claim("flaky", "worker")
	if err != nil || !won {
		t.Fatalf("claim failed node: won=%v err=%v", won, err)
	}
	if err := graph.Fail(claim, "pytest loses its cache"); err != nil {
		t.Fatalf("fail node: %v", err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("distill failure: %v", err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("repeat tick: %v", err)
	}
	if calls != 1 {
		t.Fatalf("failure distilled %d times, want once", calls)
	}

	facts, err := graph.ActiveFacts("tool:pytest", 10)
	if err != nil {
		t.Fatalf("read distilled facts: %v", err)
	}
	if len(facts) != 1 || facts[0].NodeID != "flaky" || facts[0].Kind != store.FactQuirk ||
		facts[0].Body != "pytest can lose its cache mid-run" {
		t.Fatalf("distilled facts = %+v", facts)
	}
}

func TestDistillerEmitsNonRetrievableSkillCandidate(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "skill-job", Brief: "capture the working procedure", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "skill-session", Intent: "make the audit repeatable"}); err != nil {
		t.Fatal(err)
	}
	artifact := "/workspace/audit-skill"
	reconciler := New(graph, nil, nil).WithDistiller(
		func(context.Context, string, string, bool) ([]Learned, error) {
			return []Learned{{
				Scope: "tool:git",
				Kind:  store.FactSkill,
				Body:  "git-audit runs the verified repository audit",
				Skill: &SkillCandidate{Artifact: artifact},
			}}, nil
		})
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("skill-job", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%t err=%v", won, err)
	}
	if err := graph.Complete(claim, "wrote the audit procedure"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	candidates, err := graph.SkillFacts(store.FactCandidate, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].NodeID != "skill-job" ||
		candidates[0].Body != "git-audit runs the verified repository audit" ||
		candidates[0].Artifact != artifact {
		t.Fatalf("skill candidates = %+v", candidates)
	}
	if digest := NotebookDigest(graph, "use git", "audit this repository", 5); digest != "" {
		t.Fatalf("candidate appeared in notebook digest: %q", digest)
	}
}
func TestFailedDeliveryGateReachesDistillerInput(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "job", Brief: "deliver a supported answer", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "session-gate", Intent: "include the benchmark"}); err != nil {
		t.Fatal(err)
	}

	var distilled string
	reconciler := New(graph, nil, nil).WithDistiller(
		func(_ context.Context, _ string, outcome string, failed bool) ([]Learned, error) {
			distilled = outcome
			if failed {
				t.Fatal("a delivered job with gate evidence was marked as an execution failure")
			}
			return nil, nil
		})
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("job", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%t err=%v", won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordDeliveryGate("job", store.DeliveryGate{
		Pass: false, Gap: "the benchmark result is missing", PolishClosed: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(claim, "the polished answer includes benchmark 42"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"the polished answer includes benchmark 42",
		"the benchmark result is missing",
		"The one polish pass closed it",
	} {
		if !strings.Contains(distilled, want) {
			t.Fatalf("distiller input %q does not contain %q", distilled, want)
		}
	}
}

func TestNotebookDigestRetrievesPathScopeAndEmptyNotebook(t *testing.T) {
	graph := openStore(t)
	if got := NotebookDigest(graph, "inspect internal/resident/notebook.go", "fix cue lookup", 5); got != "" {
		t.Fatalf("empty notebook digest = %q", got)
	}
	if _, err := graph.RecordFact("", "file:internal/resident/notebook.go", store.FactPlain,
		"notebook.go keeps cues in priority order"); err != nil {
		t.Fatalf("record fact: %v", err)
	}

	skill, err := graph.RecordSkillCandidate("", "file:internal/resident/notebook.go",
		"notebook-audit verifies cue ordering", "/workspace/notebook-audit")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.ActivateSkill(skill.Seq, "/home/test/.aforge/skills/notebook-audit"); err != nil {
		t.Fatal(err)
	}
	got := NotebookDigest(graph, "inspect internal/resident/notebook.go", "fix cue lookup", 5)

	if !strings.HasPrefix(got, "notebook") || !strings.Contains(got, "- notebook.go keeps cues in priority order") {
		t.Fatalf("NotebookDigest() = %q, want header plus the recorded fact", got)
	}
	if !strings.Contains(got, "- skill: notebook-audit verifies cue ordering") {
		t.Fatalf("NotebookDigest() did not label the active skill: %q", got)
	}
	skills, err := graph.SkillFacts(store.FactActive, 5)
	if err != nil || len(skills) != 1 || skills[0].Uses != 1 || skills[0].LastUsed.IsZero() {
		t.Fatalf("skill retrieval telemetry = %+v err=%v", skills, err)
	}
}

func TestConsolidationFiresOnlyAboveThresholdAndReplacesScope(t *testing.T) {
	t.Run("at threshold", func(t *testing.T) {
		graph := openStore(t)
		recordScopeFacts(t, graph, "repo:threshold", consolidationThreshold)
		calls := 0
		reconciler := New(graph, nil, nil).WithConsolidator(
			func(context.Context, string, []store.Fact) ([]Learned, error) {
				calls++
				return nil, nil
			})
		if err := reconciler.Tick(context.Background()); err != nil {
			t.Fatalf("tick: %v", err)
		}
		if calls != 0 {
			t.Fatalf("consolidator called at threshold")
		}
	})

	t.Run("above threshold", func(t *testing.T) {
		graph := openStore(t)
		recordScopeFacts(t, graph, "repo:overgrown", consolidationThreshold+1)
		calls := 0
		reconciler := New(graph, nil, nil).WithConsolidator(
			func(_ context.Context, scope string, facts []store.Fact) ([]Learned, error) {
				calls++
				if scope != "repo:overgrown" || len(facts) != consolidationThreshold+1 {
					t.Fatalf("consolidation input = scope %q facts %d", scope, len(facts))
				}
				midpoint := len(facts) / 2
				return []Learned{
					{Scope: scope, Kind: store.FactLesson, Body: "overgrown uses bounded retries", Sources: factSeqs(facts[:midpoint])},
					{Scope: scope, Kind: store.FactPlain, Body: "overgrown keeps a journal", Sources: factSeqs(facts[midpoint:])},
				}, nil
			})
		if err := reconciler.Tick(context.Background()); err != nil {
			t.Fatalf("tick: %v", err)
		}
		if err := reconciler.Tick(context.Background()); err != nil {
			t.Fatalf("repeat tick: %v", err)
		}
		if calls != 1 {
			t.Fatalf("consolidator calls = %d, want one", calls)
		}

		facts, err := graph.ActiveFacts("repo:overgrown", 100)
		if err != nil {
			t.Fatalf("read consolidated facts: %v", err)
		}
		if len(facts) != 2 {
			t.Fatalf("active facts after consolidation = %+v", facts)
		}
		bodies := facts[0].Body + "\n" + facts[1].Body
		for _, want := range []string{"overgrown uses bounded retries", "overgrown keeps a journal"} {
			if !strings.Contains(bodies, want) {
				t.Errorf("rewritten facts %q omit %q", bodies, want)
			}
		}

		events, err := graph.Events(0, 0)
		if err != nil {
			t.Fatalf("read events: %v", err)
		}
		superseded := 0
		for _, event := range events {
			if event.Kind == store.EventFactSuperseded {
				superseded++
			}
		}
		if superseded != consolidationThreshold+1 {
			t.Fatalf("supersession events = %d, want %d", superseded, consolidationThreshold+1)
		}
	})
}

func TestConsolidationThreadsEvidenceAndMapsEachOriginal(t *testing.T) {
	graph := openStore(t)
	for _, node := range []store.NodeSpec{
		{ID: "evidence-a", Brief: "first source", Stage: 1},
		{ID: "evidence-b", Brief: "second source", Stage: 1},
	} {
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{node}},
			store.Provenance{Origin: store.OriginUser, Intent: "collect evidence"}); err != nil {
			t.Fatalf("splice evidence node %s: %v", node.ID, err)
		}
	}

	const scope = "repo:evidence"
	var originals []store.Fact
	for index := 0; index < consolidationThreshold+1; index++ {
		nodeID := "evidence-a"
		if index%2 == 1 {
			nodeID = "evidence-b"
		}
		fact, err := graph.RecordFact(nodeID, scope, store.FactPlain, fmt.Sprintf("evidence fact %02d", index))
		if err != nil {
			t.Fatalf("record evidence fact %d: %v", index, err)
		}
		originals = append(originals, fact)
	}

	wantReplacement := make(map[int64]string, len(originals))
	reconciler := New(graph, nil, nil).WithConsolidator(
		func(_ context.Context, gotScope string, facts []store.Fact) ([]Learned, error) {
			if gotScope != scope {
				t.Fatalf("scope = %q, want %q", gotScope, scope)
			}
			var sourcesA, sourcesB []int64
			for _, fact := range facts {
				if fact.NodeID == "evidence-a" {
					sourcesA = append(sourcesA, fact.Seq)
					wantReplacement[fact.Seq] = "consolidated A"
				} else {
					sourcesB = append(sourcesB, fact.Seq)
					wantReplacement[fact.Seq] = "consolidated B"
				}
			}
			// Exercise the singular correction mapping too: Replaces is
			// honored as an additional source by consolidation.
			replaces := sourcesB[len(sourcesB)-1]
			sourcesB = sourcesB[:len(sourcesB)-1]
			return []Learned{
				{Scope: scope, Kind: store.FactLesson, Body: "consolidated A", Sources: sourcesA},
				{Scope: scope, Kind: store.FactLesson, Body: "consolidated B", Sources: sourcesB, Replaces: replaces},
			}, nil
		})
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("consolidate: %v", err)
	}

	active, err := graph.ActiveFacts(scope, 10)
	if err != nil || len(active) != 2 {
		t.Fatalf("active consolidated facts = %+v err=%v", active, err)
	}
	replacementSeq := make(map[string]int64)
	for _, fact := range active {
		replacementSeq[fact.Body] = fact.Seq
		wantNode := "evidence-a"
		if fact.Body == "consolidated B" {
			wantNode = "evidence-b"
		}
		if fact.NodeID != wantNode {
			t.Errorf("%q NodeID = %q, want strongest source %q", fact.Body, fact.NodeID, wantNode)
		}
	}

	gotMapping := make(map[int64]int64)
	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	for _, event := range events {
		if event.Kind != store.EventFactSuperseded {
			continue
		}
		var payload struct {
			FactSeq int64 `json:"fact_seq"`
			BySeq   int64 `json:"by_seq"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("decode supersession: %v", err)
		}
		gotMapping[payload.FactSeq] = payload.BySeq
	}
	for originalSeq, body := range wantReplacement {
		if got, want := gotMapping[originalSeq], replacementSeq[body]; got != want {
			t.Errorf("original #%d superseded by #%d, want its actual %q replacement #%d", originalSeq, got, body, want)
		}
	}
}

func TestConsolidationEmitsRetrievableStructuredUnsettledPair(t *testing.T) {
	graph := openStore(t)
	recordScopeFacts(t, graph, "user", consolidationThreshold+1)
	reconciler := New(graph, nil, nil).WithConsolidator(
		func(_ context.Context, scope string, facts []store.Fact) ([]Learned, error) {
			midpoint := len(facts) / 2
			pair := store.UnsettledPair{Approaches: []store.UnsettledApproach{
				{Approach: "batch updates", Scope: "large mechanical changes", Evidence: factSeqs(facts[:midpoint])},
				{Approach: "incremental updates", Scope: "small risky changes", Evidence: factSeqs(facts[midpoint:])},
			}}
			return []Learned{{
				Scope: scope, Kind: store.FactUnsettled, Unsettled: &pair, Sources: factSeqs(facts),
			}}, nil
		})
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	active, err := graph.ActiveFacts("user", 10)
	if err != nil || len(active) != 1 || active[0].Kind != store.FactUnsettled || active[0].Unsettled == nil {
		t.Fatalf("consolidated unsettled fact = %+v err=%v", active, err)
	}
	digest := NotebookDigest(graph, "choose an update method", "apply the change", 5)
	flag := fmt.Sprintf("%s%d", store.UnsettledFactFlag, active[0].Seq)
	if !strings.Contains(digest, flag) || !strings.Contains(digest, "batch updates") || !strings.Contains(digest, "incremental updates") {
		t.Fatalf("retrieved unsettled digest = %q", digest)
	}
}

func TestMaintenanceFactSearchDoesNotCountUses(t *testing.T) {
	graph := openStore(t)
	recorded, err := graph.RecordFact("", "tool:git", store.FactLesson, "git worktrees isolate changes")
	if err != nil {
		t.Fatalf("record fact: %v", err)
	}
	query := store.FactQuery{Cues: []string{"tool:git"}, Limit: 5}
	if found, err := graph.SearchFactsUncounted(query); err != nil || len(found) != 1 {
		t.Fatalf("uncounted search = %+v err=%v", found, err)
	}
	active, err := graph.ActiveFacts("tool:git", 5)
	if err != nil || len(active) != 1 || active[0].Seq != recorded.Seq || active[0].Uses != 0 || !active[0].LastUsed.IsZero() {
		t.Fatalf("uncounted search contaminated telemetry: %+v err=%v", active, err)
	}
	if found, err := graph.SearchFacts(query); err != nil || len(found) != 1 {
		t.Fatalf("counted search = %+v err=%v", found, err)
	}
	active, err = graph.ActiveFacts("tool:git", 5)
	if err != nil || len(active) != 1 || active[0].Uses != 1 || active[0].LastUsed.IsZero() {
		t.Fatalf("counted search did not update telemetry: %+v err=%v", active, err)
	}
}

func TestRenderCompileContextRetrievesNotebookByCue(t *testing.T) {
	graph := openStore(t)
	if _, err := graph.RecordFact("", "file:internal/resident/notebook.go", store.FactQuirk,
		"cue extraction strips line-number suffixes"); err != nil {
		t.Fatalf("record relevant fact: %v", err)
	}
	if _, err := graph.RecordFact("", "tool:curl", store.FactQuirk,
		"curl retries uploads twice"); err != nil {
		t.Fatalf("record irrelevant fact: %v", err)
	}
	snapshot, err := graph.ActiveSnapshot()
	if err != nil {
		t.Fatalf("active snapshot: %v", err)
	}

	got := New(graph, nil, nil).renderCompileContext(snapshot,
		"change internal/resident/notebook.go:42")
	if !strings.Contains(got, "cue extraction strips line-number suffixes") {
		t.Fatalf("compile context omitted cue-scoped fact: %q", got)
	}
	if strings.Contains(got, "curl retries uploads twice") {
		t.Fatalf("compile context included unrelated recent fact: %q", got)
	}
}

func TestUnsettledRetrievalFlagsCompilerAndThreadsTrial(t *testing.T) {
	graph := openStore(t)
	first, err := graph.RecordFact("", "user", store.FactLesson,
		"table-driven parsing worked for stable grammars")
	if err != nil {
		t.Fatal(err)
	}
	second, err := graph.RecordFact("", "user", store.FactLesson,
		"parser combinators worked for frequently changing grammars")
	if err != nil {
		t.Fatal(err)
	}
	pair := store.UnsettledPair{Approaches: []store.UnsettledApproach{
		{Approach: "table-driven parsing", Scope: "stable grammars", Evidence: []int64{first.Seq}},
		{Approach: "parser combinators", Scope: "frequently changing grammars", Evidence: []int64{second.Seq}},
	}}
	unsettled, err := graph.RecordUnsettledFact("", "user", pair)
	if err != nil {
		t.Fatal(err)
	}
	command, err := graph.RequestCommand(store.Command{
		SessionID: "trial-session", Kind: store.CommandSplice, Instruction: "implement the parser",
	})
	if err != nil {
		t.Fatal(err)
	}

	compile := func(_ context.Context, instruction, graphContext string) (Compiled, error) {
		flag := fmt.Sprintf("%s%d", store.UnsettledFactFlag, unsettled.Seq)
		for _, want := range []string{flag, "table-driven parsing", "parser combinators", "evidence"} {
			if !strings.Contains(graphContext, want) {
				return Compiled{}, fmt.Errorf("compiler context omitted %q:\n%s", want, graphContext)
			}
		}
		return Compiled{
			Goal:  "run a cheap comparison of table-driven parsing and parser combinators, then implement with the winner",
			Scale: "project", TrialOf: unsettled.Seq,
		}, nil
	}
	if err := New(graph, compile, nil).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	node, ok, err := graph.Node(fmt.Sprintf("task-%d", command.Seq))
	if err != nil || !ok || node.Provenance.TrialOf != unsettled.Seq {
		t.Fatalf("trial node = %+v ok=%t err=%v", node, ok, err)
	}
}

func TestTrialLandingRendersEvidenceAndSupersedesWithWinner(t *testing.T) {
	graph := openStore(t)
	trial := spliceTrialFixture(t, graph, "winner")
	var distilled string
	reconciler := New(graph, nil, nil).WithDistiller(
		func(_ context.Context, _ string, outcome string, failed bool) ([]Learned, error) {
			distilled = outcome
			if failed {
				t.Fatal("successful trial was marked failed")
			}
			return []Learned{{
				Scope: "domain:parsing", Kind: store.FactLesson,
				Body:     "table-driven parsing wins for this grammar because its benchmark was faster",
				Replaces: trial.Seq,
			}}, nil
		})
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("winner-job", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%t err=%v", won, err)
	}
	if err := graph.Complete(claim, "table-driven parsing won the controlled benchmark"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		fmt.Sprintf("TRIAL VERDICT REQUIRED: this job tested unsettled fact #%d", trial.Seq),
		"Approach 1: table-driven parsing",
		"Approach 2: parser combinators",
		"table-driven parsing worked for stable grammars",
		"parser combinators worked for changing grammars",
	} {
		if !strings.Contains(distilled, want) {
			t.Fatalf("distiller input omitted %q:\n%s", want, distilled)
		}
	}
	assertTrialWinner(t, graph, trial.Seq, "winner-job")

	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	assertTrialWinner(t, graph, trial.Seq, "winner-job")
}

func TestTrialWithoutVerdictCarriesPairForwardOnce(t *testing.T) {
	graph := openStore(t)
	trial := spliceTrialFixture(t, graph, "inconclusive")
	reconciler := New(graph, nil, nil).WithDistiller(
		func(context.Context, string, string, bool) ([]Learned, error) {
			return nil, nil
		})
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("inconclusive-job", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%t err=%v", won, err)
	}
	if err := graph.Complete(claim, "the measurements overlapped"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	old, ok, err := graph.Fact(trial.Seq)
	if err != nil || !ok || old.Status != store.FactSuperseded {
		t.Fatalf("old pair = %+v ok=%t err=%v", old, ok, err)
	}
	active, err := graph.ActiveFacts("domain:parsing", 10)
	if err != nil {
		t.Fatal(err)
	}
	var carried *store.Fact
	for index := range active {
		if active[index].Kind == store.FactUnsettled {
			carried = &active[index]
		}
	}
	if carried == nil || carried.Unsettled == nil || len(carried.Unsettled.Trials) != 1 {
		t.Fatalf("carried unsettled pair = %+v", carried)
	}
	note := carried.Unsettled.Trials[0]
	if note.NodeID != "inconclusive-job" || note.Outcome != store.TrialDidNotSettle {
		t.Fatalf("trial note = %+v", note)
	}
	stats, err := graph.TrialStats()
	if err != nil || stats.Fired != 1 || stats.Inconclusive != 1 || stats.Settled != 0 || stats.Pending != 0 {
		t.Fatalf("trial stats = %+v err=%v", stats, err)
	}
	if len(stats.Outcomes) != 1 || stats.Outcomes[0].ReplacementSeq != carried.Seq || stats.Outcomes[0].Status != store.TrialInconclusive {
		t.Fatalf("trial outcomes = %+v", stats.Outcomes)
	}
}

func spliceTrialFixture(t *testing.T, graph *store.Store, prefix string) store.Fact {
	t.Helper()
	first, err := graph.RecordFact("", "domain:parsing", store.FactLesson,
		"table-driven parsing worked for stable grammars")
	if err != nil {
		t.Fatal(err)
	}
	second, err := graph.RecordFact("", "domain:parsing", store.FactLesson,
		"parser combinators worked for changing grammars")
	if err != nil {
		t.Fatal(err)
	}
	trial, err := graph.RecordUnsettledFact("", "domain:parsing", store.UnsettledPair{
		Approaches: []store.UnsettledApproach{
			{Approach: "table-driven parsing", Scope: "stable grammars", Evidence: []int64{first.Seq}},
			{Approach: "parser combinators", Scope: "changing grammars", Evidence: []int64{second.Seq}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: prefix + "-job", Brief: "compare both parsing approaches", Stage: 1,
	}}}, store.Provenance{
		Origin: store.OriginUser, SessionID: prefix + "-session",
		Intent: "choose and apply a parsing approach", TrialOf: trial.Seq,
	}); err != nil {
		t.Fatal(err)
	}
	return trial
}

func assertTrialWinner(t *testing.T, graph *store.Store, trialSeq int64, nodeID string) {
	t.Helper()
	node, ok, err := graph.Node(nodeID)
	if err != nil || !ok || node.Provenance.TrialOf != trialSeq {
		t.Fatalf("trial provenance = %+v ok=%t err=%v", node.Provenance, ok, err)
	}
	old, ok, err := graph.Fact(trialSeq)
	if err != nil || !ok || old.Status != store.FactSuperseded {
		t.Fatalf("old pair = %+v ok=%t err=%v", old, ok, err)
	}
	active, err := graph.ActiveFacts("domain:parsing", 10)
	if err != nil {
		t.Fatal(err)
	}
	winner := false
	for _, fact := range active {
		if fact.NodeID == nodeID && fact.Kind == store.FactLesson && strings.Contains(fact.Body, "table-driven parsing wins") {
			winner = true
		}
	}
	if !winner {
		t.Fatalf("active facts omit trial winner: %+v", active)
	}
	stats, err := graph.TrialStats()
	if err != nil || stats.Fired != 1 || stats.Settled != 1 || stats.Inconclusive != 0 || stats.Pending != 0 {
		t.Fatalf("trial stats = %+v err=%v", stats, err)
	}
	if len(stats.Outcomes) != 1 || stats.Outcomes[0].NodeID != nodeID || stats.Outcomes[0].Status != store.TrialSettled ||
		!strings.Contains(stats.Outcomes[0].Body, "table-driven parsing wins") {
		t.Fatalf("trial outcomes = %+v", stats.Outcomes)
	}
}

func TestRenderCompileContextRecallsFoldBeyondActiveViewBudget(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "old-celadon", Brief: "Repair the celadon parser", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, Intent: "Repair the celadon parser"}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("old-celadon", "worker")
	if err != nil || !won {
		t.Fatalf("claim old memory: won=%v err=%v", won, err)
	}
	if err := graph.Complete(claim, "The celadon parser requires the sentinel table"); err != nil {
		t.Fatal(err)
	}
	if err := graph.Fold("old-celadon", "Keep the sentinel table explicit", []string{"/workspace/celadon/notes.md"}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 50; index++ {
		id := fmt.Sprintf("recent-%02d", index)
		intent := fmt.Sprintf("Recent unrelated request %02d %s", index, strings.Repeat("x", 180))
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
			ID: id, Brief: intent, Stage: 1,
		}}}, store.Provenance{Origin: store.OriginUser, Intent: intent}); err != nil {
			t.Fatal(err)
		}
	}

	snapshot, err := graph.ActiveSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	activeOnly := renderGraphContext(snapshot)
	if strings.Contains(activeOnly, "Keep the sentinel table explicit") {
		t.Fatal("active graph unexpectedly contains the old fold digest fixture")
	}
	got := New(graph, nil, nil).renderCompileContext(snapshot, "Repair the celadon parser again")
	for _, want := range []string{"Keep the sentinel table explicit", "/workspace/celadon/notes.md"} {
		if !strings.Contains(got, want) {
			t.Fatalf("compile context omitted recalled %q:\n%s", want, got)
		}
	}
}

func recordScopeFacts(t *testing.T, graph *store.Store, scope string, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		if _, err := graph.RecordFact("", scope, store.FactPlain,
			fmt.Sprintf("original fact %02d", index)); err != nil {
			t.Fatalf("record fact %d: %v", index, err)
		}
	}
}

func factSeqs(facts []store.Fact) []int64 {
	seqs := make([]int64, 0, len(facts))
	for _, fact := range facts {
		seqs = append(seqs, fact.Seq)
	}
	return seqs
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
