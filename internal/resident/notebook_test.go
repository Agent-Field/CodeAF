package resident

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
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

func TestScopeAliasCandidateDetectionNormalizesTokens(t *testing.T) {
	candidates := scopeAliasCandidates([]string{
		"domain:podcasts",
		"domain:podcast-production",
		"domain:podcast_productions",
		"domain:gardening",
		"tool:podcast-production",
		"repo:internal/store",
		"repo:external/renderer",
		"user",
	})
	for _, want := range []ScopePair{
		{First: "domain:podcast-production", Second: "domain:podcast_productions"},
		{First: "domain:podcast-production", Second: "domain:podcasts"},
	} {
		if !containsScopePair(candidates, want) {
			t.Errorf("scope candidates = %+v, missing %+v", candidates, want)
		}
	}
	for _, candidate := range candidates {
		if strings.Contains(candidate.First, "gardening") || strings.Contains(candidate.Second, "gardening") {
			t.Errorf("unrelated scope became a candidate: %+v", candidate)
		}
		if strings.HasPrefix(candidate.First, "domain:") != strings.HasPrefix(candidate.Second, "domain:") {
			t.Errorf("cross-prefix candidate = %+v", candidate)
		}
	}
}

// The contract playbook reaches for what has been proven, not for what has
// merely been shown. Proof is journal-native — a fact injected into real work
// whose job then landed — so it survives Rebuild and the facts migration, which
// is exactly what the retrieval counter it replaced did not.
func TestContractPlaybookRanksProvenBulletsAheadOfNewOnes(t *testing.T) {
	graph := openStore(t)
	const scope = "repo:internal/parser"
	provenBody := "Run make check before delivery; direct go test misses generated parser fixtures. " + strings.Repeat("p", 220)
	proven, err := graph.RecordFact("", scope, store.FactPlaybook, provenBody)
	if err != nil {
		t.Fatal(err)
	}
	middleBody := "Keep the generated parser fixture list synchronized with the grammar. " + strings.Repeat("m", 220)
	middle, err := graph.RecordFact("", scope, store.FactPlaybook, middleBody)
	if err != nil {
		t.Fatal(err)
	}
	newestBody := "Exercise the parser through make check so the repository wrapper configures fixtures. " + strings.Repeat("n", 210)
	newest, err := graph.RecordFact("", scope, store.FactPlaybook, newestBody)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordFact("", scope, store.FactLesson,
		"the parser grammar uses generated fixtures"); err != nil {
		t.Fatal(err)
	}

	// The oldest bullet rode two jobs that landed; the newest rode one that
	// failed its gate, which is evidence against it rather than for it.
	for _, id := range []string{"ride-one", "ride-two", "ride-bad"} {
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
			ID: id, Brief: "parser work", Stage: 1,
		}}}, store.Provenance{Origin: store.OriginUser, Intent: "parser work"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"ride-one", "ride-two"} {
		if err := graph.RecordFactInjection(id, []int64{proven.Seq}); err != nil {
			t.Fatal(err)
		}
	}
	if err := graph.RecordFactInjection("ride-bad", []int64{newest.Seq}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordDeliveryGate("ride-bad", store.DeliveryGate{Gap: "missed the fixtures"}); err != nil {
		t.Fatal(err)
	}

	notes := ContractPlaybook(graph)(plan.Node{
		Title: "Parser repair", Summary: "Repair parser validation",
		Sources: []string{"internal/parser/check.go"},
		Brief:   "Update internal/parser/check.go and use go tooling.",
	})
	provenAt := strings.Index(notes, provenBody)
	newestAt := strings.Index(notes, newestBody)
	if provenAt < 0 || newestAt < 0 || provenAt > newestAt {
		t.Fatalf("playbook ordering = %q, want proven then newest", notes)
	}
	if strings.Contains(notes, middleBody) {
		t.Fatalf("bounded playbook included the lower-ranked overflow bullet: %q", notes)
	}
	if got := len(contractPlaybookHeader) + len(notes); got > contractPlaybookBytes {
		t.Fatalf("contract playbook = %d bytes, want at most %d", got, contractPlaybookBytes)
	}

	// The retrieval counter still moves — it is consolidation's telemetry — but
	// nothing ranks on it any more, and it does not survive a rebuild.
	active, err := graph.ActiveFacts(scope, 10)
	if err != nil {
		t.Fatal(err)
	}
	uses := make(map[int64]int, len(active))
	for _, fact := range active {
		uses[fact.Seq] = fact.Uses
	}
	if uses[proven.Seq] == 0 || uses[middle.Seq] != 0 {
		t.Fatalf("retrieval telemetry = proven:%d middle:%d", uses[proven.Seq], uses[middle.Seq])
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt := ContractPlaybook(graph)(plan.Node{
		Title: "Parser repair", Summary: "Repair parser validation",
		Sources: []string{"internal/parser/check.go"},
		Brief:   "Update internal/parser/check.go and use go tooling.",
	})
	if provenAt, newestAt := strings.Index(rebuilt, provenBody), strings.Index(rebuilt, newestBody); provenAt < 0 ||
		newestAt < 0 || provenAt > newestAt {
		t.Fatalf("playbook ordering did not survive rebuild: %q", rebuilt)
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
	if digest := NotebookDigest(graph, "", "use git", "audit this repository", 5); digest != "" {
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
	if got := NotebookDigest(graph, "", "inspect internal/resident/notebook.go", "fix cue lookup", 5); got != "" {
		t.Fatalf("empty notebook digest = %q", got)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "leaf", Brief: "fix notebook cue lookup", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, Intent: "fix cue lookup"}); err != nil {
		t.Fatal(err)
	}
	fact, err := graph.RecordFact("", "file:internal/resident/notebook.go", store.FactPlain,
		"notebook.go keeps cues in priority order")
	if err != nil {
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
	got := NotebookDigest(graph, "leaf", "inspect internal/resident/notebook.go", "fix cue lookup", 5)

	if !strings.HasPrefix(got, "notebook") ||
		!strings.Contains(got, fmt.Sprintf("- #%d notebook.go keeps cues in priority order", fact.Seq)) {
		t.Fatalf("NotebookDigest() = %q, want header plus the recorded fact", got)
	}
	if !strings.Contains(got, fmt.Sprintf("- #%d skill: notebook-audit verifies cue ordering", skill.Seq)) {
		t.Fatalf("NotebookDigest() did not label the active skill: %q", got)
	}
	skills, err := graph.SkillFacts(store.FactActive, 5)
	if err != nil || len(skills) != 1 || skills[0].Uses != 1 || skills[0].LastUsed.IsZero() {
		t.Fatalf("skill retrieval telemetry = %+v err=%v", skills, err)
	}
	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var injected bool
	for _, event := range events {
		if event.Kind == store.EventFactInjected && event.NodeID == "leaf" {
			injected = true
		}
	}
	if !injected {
		t.Fatal("NotebookDigest did not record its injected fact batch")
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	outcomes, err := graph.FactOutcomes()
	if err != nil || outcomes[fact.Seq].Rides != 1 {
		t.Fatalf("rebuilt injection outcomes = %+v err=%v", outcomes[fact.Seq], err)
	}
	if err := graph.QuarantineFact(fact.Seq, 0, store.FactOriginUser); err != nil {
		t.Fatal(err)
	}
	if err := graph.QuarantineFact(skill.Seq, 0, store.FactOriginUser); err != nil {
		t.Fatal(err)
	}
	if got := NotebookDigest(graph, "", "inspect internal/resident/notebook.go", "fix cue lookup", 5); got != "" {
		t.Fatalf("quarantined fact reached NotebookDigest: %q", got)
	}
}

func TestConsolidationFiresOnlyAboveThresholdAndReplacesScope(t *testing.T) {
	t.Run("at threshold", func(t *testing.T) {
		graph := openStore(t)
		recordScopeFacts(t, graph, "repo:threshold", consolidationThreshold)
		calls := 0
		reconciler := New(graph, nil, nil).WithConsolidator(
			func(context.Context, string, []store.Fact, *ScopePair) (Consolidation, error) {
				calls++
				return Consolidation{}, nil
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
			func(_ context.Context, scope string, facts []store.Fact, _ *ScopePair) (Consolidation, error) {
				calls++
				if scope != "repo:overgrown" || len(facts) != consolidationThreshold+1 {
					t.Fatalf("consolidation input = scope %q facts %d", scope, len(facts))
				}
				midpoint := len(facts) / 2
				return Consolidation{Facts: []Learned{
					{Scope: scope, Kind: store.FactLesson, Body: "overgrown uses bounded retries", Sources: factSeqs(facts[:midpoint])},
					{Scope: scope, Kind: store.FactPlain, Body: "overgrown keeps a journal", Sources: factSeqs(facts[midpoint:])},
				}}, nil
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

func TestConsolidationAppliesAtMostOneScopeMergePerTick(t *testing.T) {
	graph := openStore(t)
	for index, scope := range []string{
		"domain:podcast",
		"domain:podcasts",
		"domain:podcast-production",
	} {
		if _, err := graph.RecordFact("", scope, store.FactPlain,
			fmt.Sprintf("podcast note %d", index)); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	reconciler := New(graph, nil, nil).WithConsolidator(
		func(_ context.Context, scope string, facts []store.Fact, candidate *ScopePair) (Consolidation, error) {
			calls++
			if scope != "" || len(facts) != 0 || candidate == nil {
				t.Fatalf("gardening-only consolidation = scope %q facts %d candidate %+v", scope, len(facts), candidate)
			}
			return Consolidation{ScopeAlias: &ScopeAliasJudgment{
				Merge: true, Canonical: candidate.First,
			}}, nil
		})
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	merges := 0
	for _, event := range events {
		if event.Kind == store.EventScopeAliased {
			merges++
		}
	}
	if calls != 1 || merges != 1 {
		t.Fatalf("consolidator calls=%d scope merges=%d, want one of each", calls, merges)
	}
	aliases, err := graph.ScopeAliases()
	if err != nil || len(aliases) != 1 {
		t.Fatalf("scope aliases = %+v err=%v, want exactly one", aliases, err)
	}
}

func TestConsolidationMergesNearDuplicatePlaybooksViaReplaces(t *testing.T) {
	graph := openStore(t)
	const scope = "repo:parser"
	var originals []store.Fact
	for index := 0; index < playbookConsolidationThreshold+1; index++ {
		fact, err := graph.RecordFact("", scope, store.FactPlaybook,
			fmt.Sprintf("Run make check for parser changes; variant %02d confirms the wrapper route", index))
		if err != nil {
			t.Fatal(err)
		}
		originals = append(originals, fact)
	}
	calls := 0
	reconciler := New(graph, nil, nil).WithConsolidator(
		func(_ context.Context, gotScope string, facts []store.Fact, _ *ScopePair) (Consolidation, error) {
			calls++
			if gotScope != scope || len(facts) != len(originals) {
				t.Fatalf("playbook consolidation = scope %q facts %d", gotScope, len(facts))
			}
			for _, fact := range facts {
				if fact.Kind != store.FactPlaybook {
					t.Fatalf("consolidation input kind = %q", fact.Kind)
				}
			}
			return Consolidation{Facts: []Learned{{
				Scope: scope, Kind: store.FactPlaybook,
				Body:     "Run make check for parser changes; the repository wrapper configures generated fixtures",
				Sources:  factSeqs(facts[1:]),
				Replaces: facts[0].Seq,
			}}}, nil
		})
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("playbook consolidator calls = %d, want 1", calls)
	}
	active, err := graph.ActiveFacts(scope, 10)
	if err != nil || len(active) != 1 || active[0].Kind != store.FactPlaybook {
		t.Fatalf("active consolidated playbook = %+v err=%v", active, err)
	}
	for _, original := range originals {
		retired, found, err := graph.FactBySeq(original.Seq)
		if err != nil || !found || retired.Status != store.FactSuperseded || retired.EvidenceSeq != active[0].Seq {
			t.Fatalf("playbook #%d replacement = %+v found=%t err=%v", original.Seq, retired, found, err)
		}
	}
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
		func(_ context.Context, gotScope string, facts []store.Fact, _ *ScopePair) (Consolidation, error) {
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
			return Consolidation{Facts: []Learned{
				{Scope: scope, Kind: store.FactLesson, Body: "consolidated A", Sources: sourcesA},
				{Scope: scope, Kind: store.FactLesson, Body: "consolidated B", Sources: sourcesB, Replaces: replaces},
			}}, nil
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
		func(_ context.Context, scope string, facts []store.Fact, _ *ScopePair) (Consolidation, error) {
			midpoint := len(facts) / 2
			pair := store.UnsettledPair{Approaches: []store.UnsettledApproach{
				{Approach: "batch updates", Scope: "large mechanical changes", Evidence: factSeqs(facts[:midpoint])},
				{Approach: "incremental updates", Scope: "small risky changes", Evidence: factSeqs(facts[midpoint:])},
			}}
			return Consolidation{Facts: []Learned{{
				Scope: scope, Kind: store.FactUnsettled, Unsettled: &pair, Sources: factSeqs(facts),
			}}}, nil
		})
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	active, err := graph.ActiveFacts("user", 10)
	if err != nil || len(active) != 1 || active[0].Kind != store.FactUnsettled || active[0].Unsettled == nil {
		t.Fatalf("consolidated unsettled fact = %+v err=%v", active, err)
	}
	digest := NotebookDigest(graph, "", "choose an update method", "apply the change", 5)
	flag := fmt.Sprintf("%s%d", store.UnsettledFactFlag, active[0].Seq)
	if !strings.Contains(digest, flag) || !strings.Contains(digest, "batch updates") || !strings.Contains(digest, "incremental updates") {
		t.Fatalf("retrieved unsettled digest = %q", digest)
	}
}

func TestConsolidatorQuarantinesOnlyRepeatedBadCooccurrence(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "exercise quarantine", Stage: 0},
		{ID: "bad-a", Parent: "job", Brief: "first bad ride", Stage: 1},
		{ID: "bad-b", Parent: "job", Brief: "second bad ride", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "exercise quarantine"}); err != nil {
		t.Fatal(err)
	}
	const scope = "repo:quarantine"
	suspect, err := graph.RecordFact("", scope, store.FactLesson, "always skip verification")
	if err != nil {
		t.Fatal(err)
	}
	var retained []int64
	for index := 0; index < consolidationThreshold; index++ {
		fact, err := graph.RecordFact("", scope, store.FactPlain,
			fmt.Sprintf("retained evidence %02d", index))
		if err != nil {
			t.Fatal(err)
		}
		retained = append(retained, fact.Seq)
	}
	for _, nodeID := range []string{"bad-a", "bad-b"} {
		if err := graph.RecordFactInjection(nodeID, []int64{suspect.Seq}); err != nil {
			t.Fatal(err)
		}
		claim, won, err := graph.Claim(nodeID, "worker")
		if err != nil || !won {
			t.Fatalf("claim %s: won=%t err=%v", nodeID, won, err)
		}
		if err := graph.Fail(claim, "the shortcut failed"); err != nil {
			t.Fatal(err)
		}
	}

	reconciler := New(graph, nil, nil).WithConsolidator(
		func(_ context.Context, gotScope string, facts []store.Fact, _ *ScopePair) (Consolidation, error) {
			if gotScope != scope || len(facts) != consolidationThreshold+1 {
				t.Fatalf("consolidation input = %q with %d facts", gotScope, len(facts))
			}
			return Consolidation{Facts: []Learned{
				{Quarantines: []int64{suspect.Seq}},
				{Scope: scope, Kind: store.FactPlain, Body: "retained evidence remains", Sources: retained},
			}}, nil
		})
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	quarantined, found, err := graph.FactBySeq(suspect.Seq)
	if err != nil || !found || quarantined.Status != store.FactQuarantined ||
		quarantined.StatusOrigin != store.FactOriginConsolidator || quarantined.EvidenceSeq == 0 {
		t.Fatalf("consolidator quarantine = %+v found=%t err=%v", quarantined, found, err)
	}
	active, err := graph.ActiveFacts(scope, 10)
	if err != nil || len(active) != 1 || active[0].Body != "retained evidence remains" {
		t.Fatalf("active facts after quarantine = %+v err=%v", active, err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt, found, err := graph.FactBySeq(suspect.Seq)
	if err != nil || !found || rebuilt.Status != store.FactQuarantined ||
		rebuilt.StatusOrigin != store.FactOriginConsolidator {
		t.Fatalf("rebuilt consolidator quarantine = %+v found=%t err=%v", rebuilt, found, err)
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

func TestTrialLandingRendersEvidenceAndSupersedesWithPlaybookWinner(t *testing.T) {
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
				Scope: "domain:parsing", Kind: store.FactPlaybook,
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
		if fact.NodeID == nodeID && fact.Kind == store.FactPlaybook && strings.Contains(fact.Body, "table-driven parsing wins") {
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

func containsScopePair(pairs []ScopePair, want ScopePair) bool {
	for _, pair := range pairs {
		if pair == want {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// The instruction stays verbatim; that law is not negotiable. But a verbatim
// instruction is frequently not self-contained — "check my github account and
// find it" is a whole sentence whose object lives entirely in the turn before
// it — and without those turns the compiler could only ask what "it" meant.
func TestCompileContextCarriesTheConversationTheInstructionCameFrom(t *testing.T) {
	graph := openStore(t)
	for _, message := range []store.Message{
		{SessionID: "deixis", Role: store.RoleUser,
			Body: "there's a github issue on aforge-v2 with a contributor's implementation plan"},
		{SessionID: "deixis", Role: store.RoleAgent,
			Body: "I'll find the issue with the contributor's plan and review it."},
		{SessionID: "elsewhere", Role: store.RoleUser, Body: "unrelated other window"},
	} {
		if _, err := graph.PostMessage(message); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := graph.ActiveSnapshot()
	if err != nil {
		t.Fatal(err)
	}

	got := New(graph, nil, nil).renderCompileContextFor(snapshot,
		"check my github account and find it", "deixis")
	for _, want := range []string{
		"recent conversation in this session",
		"contributor's implementation plan",
		"find the issue with the contributor's plan",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("compile context omitted %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "unrelated other window") {
		t.Fatalf("compile context leaked another session's thread:\n%s", got)
	}
	// The thread is volatile, so it goes last: everything above it is stable
	// across a session and must stay cacheable as a prefix.
	if strings.Index(got, "recent conversation in this session") < strings.Index(got, "jobs (newest first)") {
		t.Fatalf("thread slice was not written into the volatile suffix:\n%s", got)
	}
	// A caller with no conversation behind it — a charter firing speaks its own
	// template — gets exactly what it always got.
	if quiet := New(graph, nil, nil).renderCompileContext(snapshot, "check the deploy"); strings.Contains(
		quiet, "recent conversation in this session") {
		t.Fatalf("sessionless compile grew a thread block:\n%s", quiet)
	}
}

func TestCompileContextBudgetsTheThreadSlice(t *testing.T) {
	graph := openStore(t)
	for index := range 40 {
		if _, err := graph.PostMessage(store.Message{
			SessionID: "wordy", Role: store.RoleUser,
			Body: fmt.Sprintf("turn %02d %s", index, strings.Repeat("y", 900)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := graph.ActiveSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	block := New(graph, nil, nil).recentThreadBlock("wordy")
	if len(block) > compileThreadBytes+256 {
		t.Fatalf("thread slice is %d bytes", len(block))
	}
	// The turn nearest the instruction is the one that always survives.
	if !strings.Contains(block, "turn 39") {
		t.Fatalf("thread slice dropped the newest turn:\n%s", block)
	}
	if got := New(graph, nil, nil).renderCompileContextFor(snapshot, "carry on", "wordy"); !strings.Contains(
		got, "turn 39") {
		t.Fatalf("compile context dropped the newest turn:\n%s", got)
	}
}

// The store returns creation order, so a long-lived graph handed the compiler
// months of settled nodes before the job running right now — and a grown
// territory rewrites only its fold digest, so the compiler kept quoting the
// three-job version of a map that now covers eleven.
func TestCompileGraphRanksLiveWorkFirstAndPrefersTheFoldDigest(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "ancient", Brief: "an old settled job", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, Intent: "old work"}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("ancient", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%t err=%v", won, err)
	}
	if err := graph.Complete(claim, "the three-job version"); err != nil {
		t.Fatal(err)
	}
	if err := graph.Fold("ancient", "the eleven-job version", nil); err != nil {
		t.Fatal(err)
	}
	for index := range 60 {
		id := fmt.Sprintf("filler-%02d", index)
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
			ID: id, Brief: strings.Repeat("z", 200), Stage: 1,
		}}}, store.Provenance{Origin: store.OriginUser, Intent: "filler"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "live-webgpu-scan", Brief: "scan the webgpu backend", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, Intent: "scan webgpu"}); err != nil {
		t.Fatal(err)
	}
	if _, won, err := graph.Claim("live-webgpu-scan", "worker"); err != nil || !won {
		t.Fatalf("claim live: won=%t err=%v", won, err)
	}

	snapshot, err := graph.ActiveSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	got := renderGraphContext(snapshot)
	if !strings.Contains(got, "live-webgpu-scan") {
		t.Fatalf("running work was truncated out of the compile context:\n%s", got)
	}
	if !strings.Contains(got, "the eleven-job version") || strings.Contains(got, "the three-job version") {
		t.Fatalf("compile context quoted the stale summary over the live fold digest:\n%s", got)
	}
	// Both halves spend one budget; they used to hold a limit each.
	if len(got) > compileContextBytes+1024 {
		t.Fatalf("compile graph context is %d bytes against a %d budget", len(got), compileContextBytes)
	}
}

// The compiler resolves what an instruction's words point at from this slice,
// and four jobs narrating into one thread arrived here in one undifferentiated
// voice. Every job-anchored line now says which job spoke it, by the short name
// the user reads on screen — never by an id, because a second vocabulary would
// be worse than none.
func TestCompileThreadSliceNamesTheJobThatSpoke(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "auth-fix", Title: "Auth token refresh", Brief: "patch the auth token refresh", Stage: 1},
		{ID: "auth-fix-test", Parent: "auth-fix", Brief: "add a regression test", Stage: 2},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "attributed", Intent: "patch auth"}); err != nil {
		t.Fatal(err)
	}
	for _, message := range []store.Message{
		{SessionID: "attributed", Role: store.RoleUser, Body: "patch the auth token refresh"},
		{SessionID: "attributed", Role: store.RoleAgent, NodeID: "auth-fix-test", Body: "the regression test is written"},
	} {
		if _, err := graph.PostMessage(message); err != nil {
			t.Fatal(err)
		}
	}

	block := New(graph, nil, nil).recentThreadBlock("attributed")
	if !strings.Contains(block, "agent [Auth token refresh]:") {
		t.Fatalf("the compiler's slice never says which job spoke:\n%s", block)
	}
	if strings.Contains(block, "[auth-fix-test]") {
		t.Fatalf("the slice attributed a line by its id:\n%s", block)
	}
	if !strings.Contains(block, "user: patch the auth token refresh") {
		t.Fatalf("a user turn was filed under a job:\n%s", block)
	}
}
