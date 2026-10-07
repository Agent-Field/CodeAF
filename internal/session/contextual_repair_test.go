package session

// REGRESSION COVERAGE for the two source-verified defects of the combined
// review: F1, the attempt's receipt hash diverging from the redacted bytes the
// evidence row stores, and F2, a binding-only standing run writing project
// attempt rows through the delegated outcome collector. Each test drives the
// REAL seam: the production redaction chokepoint ([Agent.finishToolResult]), the
// [Agent.executeTool] -> [Agent.recordOutcome] boundary, the task-worker spawn,
// the production binding function, the promoted-job settlement and the explicit
// forget path.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/reflex"
	"github.com/Agent-Field/codeaf/internal/standing"
	"github.com/Agent-Field/codeaf/internal/store"
)

// secretReceipt is a failing receipt carrying one secret-shaped span, which is
// exactly the input on which the pre-fix code hashed the RAW clip while the
// evidence row hashed the REDACTED one.
const secretReceipt = "undefined: foobar.Token\nAWS key AKIAIOSFODNN7EXAMPLE was rejected"

// ── F1, the session writer: the attempt and the evidence row agree on the
// redacted receipt bytes, so the memory-forget provenance join retires the
// attempt too. ──

func TestSecretBearingReceiptHashesTheEvidenceBytesAndRetiresWithTheClaim(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	ctx := context.Background()
	cue := "call the deploy script after the rejected key"
	a.prepareBindingContext(ctx, cue)
	owner := store.OwnerProject("p")
	call := delegatedBashCall("sec-1", "go test ./foobar")
	// THE PRODUCTION CHOKEPOINT RUNS FIRST on every real result (loop.go's
	// [Agent.finishToolResult]); the writer is handed what the model sees.
	result := a.finishToolResult(nil, call, toolResult{text: secretReceipt, isError: true})
	if strings.Contains(result.text, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("the production chokepoint left the secret in the result: %q", result.text)
	}
	a.mu.Lock()
	turn := a.turnSeq
	a.mu.Unlock()
	a.recordMemoryTool(ctx, turn, call, result)
	// The extractor's tool-sourced claim, written the way a turn writes it: the
	// evidence row is the memory-forget provenance the join reads.
	m, err := brain.AddMemory(store.Memory{ID: "mem-key", Owner: owner, Type: store.MemoryFact,
		Title: "the deploy key is rejected", Text: "the deploy script rejects the stored key"})
	if err != nil {
		t.Fatal(err)
	}
	source := a.memoryTurnSource(cue)
	claim := source.ground(reflex.ExtractResult{Source: "tool", ReceiptID: call.ID, Scope: store.MemoryScopeProject, Text: "the deploy key is rejected"})
	if claim.Source != "tool" || claim.Authority != "observation" {
		t.Fatalf("the receipt was not accepted as independent tool evidence: %+v", claim)
	}
	if err := a.recordContextualMemory(m, claim, source); err != nil {
		t.Fatalf("record contextual evidence: %v", err)
	}
	// The independent attempt, written at the boundary [Agent.executeTool] calls.
	a.recordOutcome(ctx, turn, call, result, a.captureSourceSnapshot(ctx).Identity)
	rows := attemptsForProject(t, brain, a)
	if len(rows) != 1 {
		t.Fatalf("attempt rows=%+v", rows)
	}
	at := rows[0]
	if strings.Contains(at.Observation, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("a raw secret was journaled: %q", at.Observation)
	}
	if at.SourceHash != contextualHash(at.Observation) {
		t.Fatalf("the attempt does not hash the bytes it stored: hash=%q observation=%q", at.SourceHash, at.Observation)
	}
	evidence, err := brain.ContextualEvidenceForMemory(owner, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.SourceKey != at.SourceKey || evidence.SourceHash != at.SourceHash {
		t.Fatalf("attempt and evidence disagree on the same receipt: attempt=%q/%q evidence=%q/%q",
			at.SourceKey, at.SourceHash, evidence.SourceKey, evidence.SourceHash)
	}
	// THE DECLARED BACKSTOP, ALONE: the claim is retired through its provenance,
	// and [Agent.forgetAttempts] is never called here, so a surviving row would
	// be the broken join F1 names.
	if err := a.suppressContextualMemory(m); err != nil {
		t.Fatalf("suppress claim: %v", err)
	}
	if rows = attemptsForProject(t, brain, a); len(rows) != 0 {
		t.Fatalf("a forgotten claim left the attempt from the same secret-bearing receipt readable: %+v", rows)
	}
}

// ── F1, the delegated writer: a worker's receipt is redacted before it is
// hashed, and the raw hash the pre-fix code used is not what is stored. ──

func TestDelegatedSecretReceiptHashesTheRedactedBytes(t *testing.T) {
	root, brain, producer, _ := contextualReviewObservedFixture(t)
	ctx := context.Background()
	root.prepareBindingContext(ctx, "wire the producer library")
	worker := spawnTaskWorker(t, root, filepath.Dir(producer))
	call := delegatedBashCall("w-sec", "go test ./foobar")
	// The worker's own chokepoint, which its executeTool uses exactly as the
	// session's does.
	result := worker.finishToolResult(nil, call, toolResult{text: secretReceipt, isError: true})
	if strings.Contains(result.text, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("the worker's chokepoint left the secret in the result: %q", result.text)
	}
	worker.recordOutcome(ctx, 0, call, result, worker.captureSourceSnapshot(ctx).Identity)
	rows := attemptsForProject(t, brain, root)
	if len(rows) != 1 {
		t.Fatalf("delegated attempt rows=%+v", rows)
	}
	at := rows[0]
	want := redact.Secrets(contextualClip(result.text, contextualReceiptRunes))
	if at.Observation != want {
		t.Fatalf("the stored observation is not the redacted receipt: %q want %q", at.Observation, want)
	}
	if strings.Contains(at.Observation, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("a raw secret was journaled by the delegated writer: %q", at.Observation)
	}
	if at.SourceHash != contextualHash(want) {
		t.Fatalf("the delegated attempt does not hash the redacted receipt: hash=%q want %q", at.SourceHash, contextualHash(want))
	}
	if at.SourceHash == contextualHash(contextualClip(secretReceipt, contextualReceiptRunes)) {
		t.Fatal("the delegated attempt hashed the RAW receipt, which is the divergence F1 names")
	}
}

// ── F2: a binding-only run lends its brain for READS only, including through
// the collector its nested workers and its own promoted jobs write through. ──

func TestBindingOnlyRunLendsNoWriteToItsCollector(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	key := standingProjectKey(dir)
	owner := store.OwnerProject(key)
	brain, err := store.Open(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })
	rule, err := brain.AddMemory(store.Memory{ID: "rule", Owner: owner, Type: store.MemoryDecision,
		Title: "offline release", Text: "Release runtime uses standard library only."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := brain.AppendContextualEvidence(store.ContextualEvidence{ID: "ev", MemoryID: rule.ID,
		Owner: owner, SessionID: "s", TurnID: "t", Actor: "user", Authority: "approved_rule",
		Observation: "release artifacts must run offline", Verification: "asserted",
		SourceKey: "s:t", SourceHash: "h"}); err != nil {
		t.Fatal(err)
	}
	// THE PRODUCTION BINDING: withBindingMemory lends the canonical brain
	// read-only and refuses an unprovable project rather than running unbound.
	runner := &standingRunner{}
	cfg, closeMemory, err := runner.withBindingMemory(Config{Workspace: dir, Memory: brain}, standing.Item{Workspace: dir})
	if err != nil {
		t.Fatalf("bind memory: %v", err)
	}
	t.Cleanup(closeMemory)
	if !cfg.bindingOnlyMemory || cfg.MemoryProjectKey != key {
		t.Fatalf("the run was not bound read-only: %+v", cfg.bindingOnlyMemory)
	}
	root, _ := newTestAgent(t, &reflexScript{}, func(c *Config) {
		c.Workspace = dir
		c.Memory = cfg.Memory
		c.MemoryProjectKey = cfg.MemoryProjectKey
		c.bindingOnlyMemory = cfg.bindingOnlyMemory
	})
	if root.memoryWritable() {
		t.Fatal("a binding-only posture allowed a memory write")
	}
	if !root.remembers() || root.outcomes == nil {
		t.Fatal("the bound run lost the brain it reads or the collector beside it")
	}
	ctx := context.Background()
	// READ-ONLY BINDING READS STILL ANSWER: the rule is in front of the run.
	root.prepareBindingContext(ctx, "finish the offline release work")
	if !strings.Contains(root.memoryText, "standard library only") {
		t.Fatalf("the bound rule was not read: %q", root.memoryText)
	}
	// THE PRODUCTION WORKER SPAWN: a nested task worker carries the root's
	// collector and a frozen origin ([Agent.newTaskAgentOn]).
	worker := spawnTaskWorker(t, root, dir)
	call := delegatedBashCall("bo-fail", "go test ./foobar")
	worker.recordOutcome(ctx, 0, call, toolResult{text: "undefined: foobar.Token", isError: true}, worker.captureSourceSnapshot(ctx).Identity)
	if rows := attemptsForProject(t, brain, root); len(rows) != 0 {
		t.Fatalf("a binding-only run journaled a nested worker's failure into the borrowed brain: %+v", rows)
	}
	// AND ITS OWN PROMOTED JOB, whose frozen launch turn IS real, is refused the
	// same way rather than slipping in through the other collector door.
	root.jobs.announce = nil
	settleJob(t, root, "go test ./foobar", 3, false)
	if rows := attemptsForProject(t, brain, root); len(rows) != 0 {
		t.Fatalf("a binding-only run journaled its own settled job: %+v", rows)
	}
	// THE READ SIDE OF THE SAME JOURNAL IS INTACT: an attempt the person's own
	// session mode may not write is still readable as prior history.
	seedAttempt(t, brain, owner, "finish the offline release work", "bash: go test ./offline", "undefined: offline.Token", store.AttemptFailed, "turn:1:call1", "h1", "clean:abc")
	if got := root.priorOutcomeContext("finish the offline release work", "clean:abc"); !strings.Contains(got, "offline.Token") {
		t.Fatalf("the binding-only read path lost its attempt history: %q", got)
	}
	// A NORMAL PARENT STILL RECORDS THE SAME DELEGATED OUTCOME. The boundary is
	// the binding-only posture, not delegation, so ordinary sessions keep the
	// continuity this collector exists for.
	normal, _ := newTestAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.Memory = brain; c.MemoryProjectKey = key })
	normal.prepareBindingContext(ctx, "wire the producer library")
	nworker := spawnTaskWorker(t, normal, dir)
	ncall := delegatedBashCall("n-fail", "go test ./foobar")
	nworker.recordOutcome(ctx, 0, ncall, toolResult{text: "undefined: foobar.Token", isError: true}, nworker.captureSourceSnapshot(ctx).Identity)
	rows := attemptsForProject(t, brain, normal)
	if len(rows) != 2 {
		t.Fatalf("an ordinary parent lost its delegated recording (want the seeded row plus one new): %+v", rows)
	}
	found := false
	for _, row := range rows {
		if row.Owner == owner && strings.Contains(row.Observation, "undefined: foobar.Token") && row.SourceKey != "turn:1:call1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the ordinary parent's delegated failure was not journaled: %+v", rows)
	}
}

// ── The live acquisition's oversize settle: a decider's merged line is bounded
// by the store's own caps, so a legal candidate is not lost to a model that
// wrote a longer line than the store can hold. ──

func TestSettledOutputIsBoundedByTheStoreCaps(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	script := &reflexScript{}
	a, brain := brainAgent(t, script, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	ctx := context.Background()
	owner := store.OwnerProject("p")
	near, err := brain.AddMemory(store.Memory{ID: "near", Owner: owner, Type: store.MemoryDecision,
		Title: "release runtime", Text: "Release runtime uses the standard library only."})
	if err != nil {
		t.Fatal(err)
	}
	// An approved rule's conditions live on the evidence row, which this write
	// must leave exactly as it found it.
	if _, err := brain.AppendContextualEvidence(store.ContextualEvidence{ID: "ev-near", MemoryID: near.ID,
		Owner: owner, Actor: "user", Authority: "approved_rule",
		Observation: "release artifacts must run offline", Verification: "asserted",
		Applicability: []string{"release runtime only; development network allowed"},
		SourceKey:     "s:t", SourceHash: "h"}); err != nil {
		t.Fatal(err)
	}
	// 638 runes is the shape the live acquisition wrote into a 512-rune store.
	head := "release runtime: "
	oversize := head + strings.Repeat("n", 638-utf8.RuneCountInString(head))
	if utf8.RuneCountInString(oversize) != 638 {
		t.Fatalf("the fixture is not the observed size: %d runes", utf8.RuneCountInString(oversize))
	}
	longTitle := strings.Repeat("t", store.MemoryTitleRunes+40)
	script.mu.Lock()
	script.decide = fmt.Sprintf(`{"op":"update","target_id":%q,"title":%q,"text":%q,"tags":["runtime"]}`, near.ID, longTitle, oversize)
	script.mu.Unlock()
	settled, err := a.applyCandidate(ctx, script, reflex.ExtractResult{Mem: 1, Type: store.MemoryDecision,
		Scope: store.MemoryScopeProject, Title: "release runtime",
		Text: "Release runtime uses the standard library only.", Tags: []string{contextualTag}})
	if err != nil {
		t.Fatalf("a legal candidate was lost to an overlong settled line: %v", err)
	}
	if settled.ID != near.ID {
		t.Fatalf("the decider's update did not land on the named row: %+v", settled)
	}
	rows, err := brain.GetMemories([]string{owner}, []string{near.ID})
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if got := utf8.RuneCountInString(rows[0].Text); got > store.MemoryTextRunes {
		t.Fatalf("the stored text is %d runes, over the store cap of %d", got, store.MemoryTextRunes)
	}
	if got := utf8.RuneCountInString(rows[0].Title); got > store.MemoryTitleRunes {
		t.Fatalf("the stored title is %d runes, over the store cap of %d", got, store.MemoryTitleRunes)
	}
	if !strings.HasPrefix(rows[0].Text, head) {
		t.Fatalf("bounding dropped the settled claim's own words: %q", rows[0].Text)
	}
	// THE CONDITIONS, RATIONALE AND AUTHORITY ARE NOT THE LINE'S TO SPEND: the
	// evidence row is untouched by the bounded write.
	evidence, err := brain.ContextualEvidenceForMemory(owner, near.ID)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Authority != "approved_rule" || len(evidence.Applicability) != 1 ||
		evidence.Applicability[0] != "release runtime only; development network allowed" {
		t.Fatalf("bounding the settled line damaged the claim's evidence: %+v", evidence)
	}
	// THE INPUT SIDE OF THE SAME LAW: a title and text the extractor overran are
	// bounded before the store is asked at all.
	draft := a.memoryDraft(reflex.ExtractResult{Type: store.MemoryDecision, Scope: store.MemoryScopeProject, Title: longTitle, Text: oversize})
	if utf8.RuneCountInString(draft.Title) > store.MemoryTitleRunes || utf8.RuneCountInString(draft.Text) > store.MemoryTextRunes {
		t.Fatalf("an over-limit draft was not bounded: title=%d text=%d", utf8.RuneCountInString(draft.Title), utf8.RuneCountInString(draft.Text))
	}
	if _, err := a.addThroughDoor(draft); err != nil {
		t.Fatalf("a bounded draft was refused by the store's door: %v", err)
	}
}
