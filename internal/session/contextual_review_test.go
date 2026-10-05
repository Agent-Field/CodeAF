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

func TestContextualReviewDirtySourceHasNoSnapshotIdentity(t *testing.T) {
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
	if clean == "" {
		t.Fatal("clean source lacks revision")
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked"), []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	if dirty := a.contextualRevision(context.Background()); dirty != "" {
		t.Fatalf("dirty snapshot falsely current: %s", dirty)
	}
	git("checkout", "--", "tracked")
	if err := os.WriteFile(filepath.Join(dir, "untracked"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if dirty := a.contextualRevision(context.Background()); dirty != "" {
		t.Fatalf("untracked snapshot falsely current: %s", dirty)
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
