package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"encoding/json"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/reflex"
	"github.com/Agent-Field/codeaf/internal/store"
)

func TestContextualReviewClippingRespectsStoreCaps(t *testing.T) {
	for _, cap := range []int{240, store.MemoryTextRunes, contextualReceiptRunes} {
		got := contextualClip(strings.Repeat("é", cap+100), cap)
		if utf8.RuneCountInString(got) > cap {
			t.Errorf("cap=%d produced %d runes", cap, utf8.RuneCountInString(got))
		}
	}
}

func TestContextualReviewLatestProposalCannotBindOldApproval(t *testing.T) {
	a, s := brainAgent(t, &reflexScript{}, func(c *Config) { c.MemoryProjectKey = "review" })
	m, err := s.AddMemory(store.Memory{ID: "review-rule", Owner: store.OwnerProject("review"), Type: store.MemoryDecision, Title: "release memory", Text: "Do not add dependencies", Tags: []string{contextualTag}})
	if err != nil {
		t.Fatal(err)
	}
	e := store.ContextualEvidence{ID: "approved", MemoryID: m.ID, Owner: m.Owner, Actor: "user", Authority: "approved_rule", Observation: "The runtime must not add dependencies", Conditions: map[string]string{"project": "review"}}
	if _, err = s.AppendContextualEvidence(e); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.bindingContext("continue", ""), "Do not add dependencies") {
		t.Fatal("approved context did not bind")
	}
	e.ID = "proposal"
	e.Actor = "assistant"
	e.Authority = "proposal"
	e.Observation = "Maybe add dependencies"
	if _, err = s.AppendContextualEvidence(e); err != nil {
		t.Fatal(err)
	}
	if got := a.bindingContext("continue", ""); got != "" {
		t.Fatalf("historical approval bound latest proposal: %s", got)
	}
}

func TestContextualReviewExpiredObservationIsExcluded(t *testing.T) {
	a, s := brainAgent(t, &reflexScript{}, func(c *Config) { c.MemoryProjectKey = "review" })
	m, err := s.AddMemory(store.Memory{ID: "expired", Owner: store.OwnerProject("review"), Type: store.MemoryFact, Title: "old benchmark", Text: "A prior source ran fast", Tags: []string{contextualTag}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AppendContextualEvidence(store.ContextualEvidence{ID: "expired-evidence", MemoryID: m.ID, Owner: m.Owner, Actor: "tool", Authority: "observation", Observation: "old measured receipt", ValidUntil: time.Now().Add(-time.Hour), Revision: "old-sha"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.GetMemories([]string{m.Owner}, []string{m.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := a.contextualEligibleMemories(rows, "old-sha"); len(got) != 0 {
		t.Fatalf("expired observation recalled: %+v", got)
	}
}

func TestContextualReviewForgetRetiresEarlierSources(t *testing.T) {
	a, s := brainAgent(t, &reflexScript{}, func(c *Config) { c.MemoryProjectKey = "review" })
	m, err := s.AddMemory(store.Memory{ID: "forget-many", Owner: store.OwnerProject("review"), Type: store.MemoryDecision, Title: "release dependency policy", Text: "Release dependency policy", Tags: []string{contextualTag}})
	if err != nil {
		t.Fatal(err)
	}
	e := store.ContextualEvidence{ID: "source-old", MemoryID: m.ID, Owner: m.Owner, Actor: "user", Authority: "approved_rule", Observation: "must use builtins", SourceKey: "turn:old", SourceHash: "old"}
	old, err := s.AppendContextualEvidence(e)
	if err != nil {
		t.Fatal(err)
	}
	e.ID = "source-new"
	e.SourceKey = "turn:new"
	e.SourceHash = "new"
	if _, err = s.AppendContextualEvidence(e); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Forget("release dependency policy"); err != nil {
		t.Fatal(err)
	}
	old.ID = "relearn-old"
	old.MemoryID = "another-memory"
	if _, err = s.AppendContextualEvidence(old); err == nil {
		t.Fatal("forgotten earlier provenance relearned")
	}
}

func TestContextualReviewSettlementKeepsConditionalMetadata(t *testing.T) {
	script := &reflexScript{decide: `{"op":"update","target_id":"conditional","title":"release runtime","text":"Release runtime uses builtins","tags":["runtime"]}`}
	a, s := brainAgent(t, script, func(c *Config) { c.MemoryProjectKey = "review" })
	_, err := s.AddMemory(store.Memory{ID: "conditional", Owner: store.OwnerProject("review"), Type: store.MemoryDecision, Title: "release runtime", Text: "Release runtime uses standard library"})
	if err != nil {
		t.Fatal(err)
	}
	source := memoryTurnEvidence{Session: "s", Turn: "turn", User: "Release runtime must use builtins; development network is allowed", At: time.Now()}
	c := source.ground(reflex.ExtractResult{Mem: 1, Type: store.MemoryDecision, Scope: store.MemoryScopeProject, Title: "release runtime", Text: "Release runtime uses standard library", Source: "user", SourceQuote: "must use builtins", Authority: "approved_rule", Conditions: []string{"release runtime only; development network allowed"}, Rationale: "offline deployment", Rejected: []string{"runtime downloads"}})
	m, err := a.applyCandidate(context.Background(), script, c)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "conditional" {
		t.Fatalf("settlement failed to update target: %+v", m)
	}
	if err = a.recordContextualMemory(m, c, source); err != nil {
		t.Fatal(err)
	}
	rows, err := s.GetMemories([]string{store.OwnerProject("review")}, []string{m.ID})
	if err != nil {
		t.Fatal(err)
	}
	got := a.contextualEligibleMemories(rows, "")
	if len(got) != 1 || !strings.Contains(got[0].Text, "development network allowed") || !strings.Contains(got[0].Text, "offline deployment") {
		t.Fatalf("settlement lost conditions: %+v", got)
	}
	tagged := false
	for _, tag := range rows[0].Tags {
		tagged = tagged || tag == contextualTag
	}
	if !tagged {
		t.Fatal("settlement erased contextual marker")
	}
}

// A CREDIT IS EXACT OR IT IS NOT MADE. A dirty or untracked tree now carries a
// bounded content identity instead of the old empty string, but that identity
// must never equal the clean commit — otherwise a lesson earned against
// uncommitted source would pose as current.
func TestContextualReviewDirtySourceCarriesBoundedIdentity(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s %v", args, out, err)
		}
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "tracked"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "tracked")
	git("-c", "user.name=review", "-c", "user.email=review@example.invalid", "commit", "-qm", "initial")
	a, _ := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "review" })
	clean := a.contextualRevision(context.Background())
	if clean == "" || strings.HasPrefix(clean, "dirty:") {
		t.Fatalf("clean source lacks a bare commit identity: %q", clean)
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked"), []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	dirty := a.contextualRevision(context.Background())
	if dirty == "" || dirty == clean {
		t.Fatalf("dirty source lacks a distinct bounded identity: %q (clean %q)", dirty, clean)
	}
	if !strings.HasPrefix(dirty, "dirty:") {
		t.Fatalf("dirty identity is not labelled: %q", dirty)
	}
	git("checkout", "--", "tracked")
	if err := os.WriteFile(filepath.Join(dir, "untracked"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	untracked := a.contextualRevision(context.Background())
	if untracked == "" || untracked == clean || untracked == dirty {
		t.Fatalf("untracked source lacks a distinct bounded identity: %q", untracked)
	}
}

func TestContextualReviewObservedDependencyRequiresExactPath(t *testing.T) {
	producer := filepath.Join(t.TempDir(), "producer.py")
	consumer := filepath.Join(t.TempDir(), "consumer.py")
	if contextualResolvedReference(consumer, "backup = \""+producer+".backup\"", producer, "../producer.py") {
		t.Fatal("a different path with the producer prefix manufactured a dependency")
	}
	if !contextualResolvedReference(consumer, "producer = \""+producer+"\"", producer, "../producer.py") {
		t.Fatal("an exact observed absolute path was not recognized")
	}
}

func contextualReviewObservedFixture(t *testing.T) (*Agent, *store.Store, string, string) {
	t.Helper()
	root := t.TempDir()
	producerDir := filepath.Join(root, "producer")
	consumerDir := filepath.Join(root, "consumer")
	for _, dir := range []string{producerDir, consumerDir} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("init: %s %v", out, err)
		}
	}
	producer := filepath.Join(producerDir, "lib.py")
	consumer := filepath.Join(consumerDir, "run.py")
	if err := os.WriteFile(producer, []byte("def export(): return 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	body := "import pathlib\nLIB = pathlib.Path(__file__).resolve().parent.parent / \"producer\" / \"lib.py\"\n"
	if err := os.WriteFile(consumer, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	key, err := gitidentity.ProjectKey(producerDir)
	if err != nil {
		t.Fatal(err)
	}
	a, s := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = producerDir; c.MemoryProjectKey = key })
	return a, s, producer, consumer
}

func contextualReviewFullRead(t *testing.T, a *Agent, path, id string) memoryToolReceipt {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	call := ai.ToolCall{ID: id}
	call.Function.Name = "read"
	call.Function.Arguments = string(args)
	r := a.contextualReadReceipt(context.Background(), call, toolResult{text: string(body)}, memoryToolReceipt{ID: id, Tool: "read", Text: string(body), Status: "done"})
	if r.Path == "" {
		t.Fatal("full read receipt not retained")
	}
	return r
}

func TestContextualReviewFullReadPathlibBuildsOnlyExactObservedEdge(t *testing.T) {
	a, s, producer, consumer := contextualReviewObservedFixture(t)
	unrelatedDir := filepath.Join(t.TempDir(), "producer")
	if err := os.Mkdir(unrelatedDir, 0700); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(unrelatedDir, "lib.py")
	if err := os.WriteFile(unrelated, []byte("def export(): return 999\n"), 0600); err != nil {
		t.Fatal(err)
	}
	receipts := []memoryToolReceipt{contextualReviewFullRead(t, a, producer, "producer-read"), contextualReviewFullRead(t, a, consumer, "consumer-read"), contextualReviewFullRead(t, a, unrelated, "unrelated-read")}
	a.observeContextualDependencies(memoryTurnEvidence{Receipts: receipts})
	links, err := s.DependenciesForProducer(contextualPathOwner(producer), 8)
	if err != nil || len(links) != 1 || links[0].ProducerPath != producer || links[0].ConsumerPath != consumer {
		t.Fatalf("observed links=%+v err=%v", links, err)
	}
	foreign, err := s.DependenciesForProducer(contextualPathOwner(unrelated), 8)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("same-name unrelated source linked=%+v err=%v", foreign, err)
	}
}

func TestContextualReviewRepeatedRecallKeepsPreparedImpactNotice(t *testing.T) {
	a, _, producer, consumer := contextualReviewObservedFixture(t)
	a.observeContextualDependencies(memoryTurnEvidence{Receipts: []memoryToolReceipt{contextualReviewFullRead(t, a, producer, "p"), contextualReviewFullRead(t, a, consumer, "c")}})
	if err := os.WriteFile(producer, []byte("def export(): return {\"value\": 1}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	first := a.contextualImpactContext("change the export contract")
	if first == "" || !strings.Contains(first, consumer) {
		t.Fatalf("observed consequence absent: %s", first)
	}
	again := a.contextualImpactContext("continue")
	if again != first {
		t.Fatalf("optional recall erased prepared impact: first=%q again=%q", first, again)
	}
}

func TestContextualReviewPostActionImpactReachesNextRequestContext(t *testing.T) {
	a, _, producer, consumer := contextualReviewObservedFixture(t)
	a.observeContextualDependencies(memoryTurnEvidence{Receipts: []memoryToolReceipt{contextualReviewFullRead(t, a, producer, "p"), contextualReviewFullRead(t, a, consumer, "c")}})
	if got := a.contextualImpactContext("change the export contract"); got != "" {
		t.Fatalf("unchanged source warned: %s", got)
	}
	if err := os.WriteFile(producer, []byte("def export(): return {\"value\": 1}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a.refreshContextualImpactsAfterAction(toolResult{text: "file updated"}, "write")
	a.mu.Lock()
	next := a.memoryText
	a.mu.Unlock()
	if !strings.Contains(next, "<contextual_impacts>") || !strings.Contains(next, consumer) {
		t.Fatalf("next request lacks post-action consequence: %s", next)
	}
	if again := a.withBindingContext("", "continue"); !strings.Contains(again, consumer) {
		t.Fatalf("later optional recall erased post-action consequence: %s", again)
	}
}
