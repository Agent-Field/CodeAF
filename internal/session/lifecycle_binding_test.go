package session

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/store"
)

// seedApprovedRule appends one approved rule under an owner and returns the
// memory. The evidence is what the binding projection reads; the memory row is
// what it renders.
func seedApprovedRule(t *testing.T, brain *store.Store, id, owner, text string) store.Memory {
	t.Helper()
	m, err := brain.AddMemory(store.Memory{ID: id, Owner: owner, Type: store.MemoryDecision, Title: id, Text: text})
	if err != nil {
		t.Fatalf("add memory %s: %v", id, err)
	}
	if _, err := brain.AppendContextualEvidence(store.ContextualEvidence{
		ID: "ev-" + id, MemoryID: m.ID, Owner: owner, SessionID: "s", TurnID: "t",
		Actor: "user", Authority: "approved_rule", Observation: text,
		Verification: "asserted", SourceKey: "s:t:" + id, SourceHash: "h-" + id,
	}); err != nil {
		t.Fatalf("append evidence %s: %v", id, err)
	}
	return m
}

// anchorAgent is an owned (project-less) conversation carrying a brain and the
// scratch key its door minted, exactly as `codeaf` typed in $HOME does.
func anchorAgent(t *testing.T, brain *store.Store, scratch, scratchKey string) *Agent {
	t.Helper()
	agent, err := newAgent(Config{
		Workspace: scratch, Place: Place{Owned: true, Workspace: scratch},
		Model: "test/model", System: "SYSTEM", Memory: brain, MemoryProjectKey: scratchKey,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	return agent
}

// AN ANCHOR MOVES THE PROJECT. The workspace/place fields alone are not the
// identity: after anchoring, the conversation's project key and owner are the
// REPOSITORY's, and the very next request carries that repository's approved
// rules rather than the scratch project's stale block.
func TestAnchorWorkspaceMovesProjectIdentityAndRebinds(t *testing.T) {
	brain, err := store.Open(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })

	scratch := t.TempDir()
	repo := newTestRepo(t)
	wantKey := standingProjectKey(repo)
	wantKeyFromDoor, err := gitidentity.ProjectKey(repo)
	if err != nil || wantKeyFromDoor != wantKey || wantKey == "" {
		t.Fatalf("test repo has no provable key: %q/%q/%v", wantKey, wantKeyFromDoor, err)
	}
	scratchKey := standingProjectKey(scratch)
	if scratchKey == "" || scratchKey == wantKey {
		t.Fatalf("scratch and repo keys did not diverge: %q/%q", scratchKey, wantKey)
	}
	seedApprovedRule(t, brain, "scratch", store.OwnerProject(scratchKey), "scratch-only rule from the phantom project")
	seedApprovedRule(t, brain, "repo", store.OwnerProject(wantKey), "repo-approved rule for the real project")

	agent := anchorAgent(t, brain, scratch, scratchKey)
	agent.mu.Lock()
	agent.stampUserLocked("make the release offline")
	agent.mu.Unlock()
	agent.prepareBindingContext(context.Background(), "make the release offline")

	agent.mu.Lock()
	before := agent.memoryText
	agent.mu.Unlock()
	if !strings.Contains(before, "scratch-only rule") {
		t.Fatalf("pre-anchor block missing the scratch rule: %q", before)
	}

	anchored, err := agent.AnchorWorkspace(repo)
	if err != nil {
		t.Fatalf("anchor: %v", err)
	}
	if anchored == "" {
		t.Fatal("anchor resolved to an empty workspace")
	}
	if got := agent.config.MemoryProjectKey; got != wantKey {
		t.Fatalf("anchor left the stale project key: got %q want %q", got, wantKey)
	}
	if got := agent.ownerForScope(store.MemoryScopeProject); got != store.OwnerProject(wantKey) {
		t.Fatalf("anchor left the stale project owner: got %q want %q", got, store.OwnerProject(wantKey))
	}
	agent.mu.Lock()
	after := agent.memoryText
	landed := agent.lastNoteLocked(memoryNoteOpening)
	agent.mu.Unlock()
	if !strings.Contains(after, "repo-approved rule") || strings.Contains(after, "scratch-only rule") {
		t.Fatalf("next request block did not move to the repository: %q", after)
	}
	// APPEND-ONLY: the new note replaced nothing already said.
	if !strings.Contains(landed, "repo-approved rule") {
		t.Fatalf("the anchored binding note did not land: %q", landed)
	}
}

// A TASK WORKER'S APPROVED BINDINGS ARRIVE BEFORE ITS FIRST REQUEST, READ-ONLY.
// The node's semantic shortlist is asynchronous and may be late; these may not
// be. The read is owner-filtered, so an unrelated project's rule, a revoked one
// and a stale one all stay out, and a router outage erases nothing because the
// read never asks the router.
func TestWorkerBindingBeforeFirstRequestReadOnlyOwnerFiltered(t *testing.T) {
	brain, err := store.Open(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })

	key := "worker-project-key"
	owner := store.OwnerProject(key)
	seedApprovedRule(t, brain, "bound", owner, "workers must use the frozen toolchain")
	seedApprovedRule(t, brain, "wrong", store.OwnerProject("some-other-project"), "other project indents with tabs")

	// revoked: approved then suppressed wholesale.
	seedApprovedRule(t, brain, "revoked", owner, "revoked rule must never bind")
	if err := brain.SuppressContextualMemorySources(owner, "revoked", "explicit forget"); err != nil {
		t.Fatalf("suppress: %v", err)
	}
	// stale: approved with an expiry already behind us.
	stale, err := brain.AddMemory(store.Memory{ID: "stale", Owner: owner, Type: store.MemoryDecision, Title: "stale", Text: "stale rule must never bind"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := brain.AppendContextualEvidence(store.ContextualEvidence{
		ID: "ev-stale", MemoryID: stale.ID, Owner: owner, SessionID: "s", TurnID: "t", Actor: "user",
		Authority: "approved_rule", Observation: "stale", Verification: "asserted",
		ValidUntil: time.Now().Add(-time.Hour), SourceKey: "s:t:stale", SourceHash: "h-stale",
	}); err != nil {
		t.Fatal(err)
	}

	before, err := brain.ListMemories([]string{owner}, 50)
	if err != nil {
		t.Fatal(err)
	}

	// A ROUTER OUTAGE: the deterministic read must not depend on it.
	script := &reflexScript{routeErr: errors.New("router down"), answer: "done"}
	agent, _ := newTestAgent(t, script, func(c *Config) {
		c.bindingStore = brain
		c.MemoryProjectKey = key
	})
	if agent.remembers() {
		t.Fatal("a worker with only a lent store answered remembers() true")
	}
	if agent.memoryWritable() {
		t.Fatal("a worker with only a lent store was granted a memory write")
	}
	if len(agent.memoryTools()) != 0 {
		t.Fatal("a lent store handed the worker the remember/forget verb")
	}
	collect(t, mustSubmit(t, agent, "build the release"))

	script.mu.Lock()
	requests := append([]string(nil), script.requests...)
	script.mu.Unlock()
	if len(requests) == 0 {
		t.Fatal("the worker made no provider request")
	}
	joined := requests[0]
	if !strings.Contains(joined, "frozen toolchain") {
		t.Fatalf("first worker request lacked the approved rule:\n%s", joined)
	}
	for _, absent := range []string{"tabs", "revoked rule", "stale rule"} {
		if strings.Contains(joined, absent) {
			t.Fatalf("first worker request carried a binding it must not (%q):\n%s", absent, joined)
		}
	}
	agent.mu.Lock()
	block := agent.bindingText
	agent.mu.Unlock()
	if strings.TrimSpace(block) == "" {
		t.Fatal("the binding note was never composed")
	}
	if len([]rune(block)) > memoryBlockRunes {
		t.Fatalf("binding block exceeded the shared ceiling: %d > %d", len([]rune(block)), memoryBlockRunes)
	}
	after, err := brain.ListMemories([]string{owner}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("a read-only binding posture wrote to the store: %d -> %d rows", len(before), len(after))
	}
}

// AN ADMITTED WORKER KEEPS THE PROJECT IT WAS ADMITTED INTO. A worker (and the
// delegated job it stands for) is handed the root's key ONCE, at admission;
// anchoring the conversation to a different repository afterwards must not
// relabel the worker's reads or, retroactively, grant it the new project's
// rules. It reads the frozen owner it was admitted under.
func TestWorkerBindingHonorsFrozenProjectKey(t *testing.T) {
	brain, err := store.Open(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })

	seedApprovedRule(t, brain, "admitted", store.OwnerProject("admitted-project"), "rule of the project the worker was admitted into")
	seedApprovedRule(t, brain, "later", store.OwnerProject("later-project"), "rule of a project the worker was never in")

	script := &reflexScript{answer: "done"}
	agent, _ := newTestAgent(t, script, func(c *Config) {
		c.bindingStore = brain
		c.MemoryProjectKey = "admitted-project"
	})
	collect(t, mustSubmit(t, agent, "carry out the admitted job"))

	script.mu.Lock()
	joined := ""
	if len(script.requests) > 0 {
		joined = script.requests[0]
	}
	script.mu.Unlock()
	if !strings.Contains(joined, "admitted into") {
		t.Fatalf("frozen worker lost its admitted project's rule:\n%s", joined)
	}
	if strings.Contains(joined, "never in") {
		t.Fatalf("a later project's rule was granted retroactively:\n%s", joined)
	}
}
