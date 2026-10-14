package session

// LIFECYCLE FOLLOW-THROUGH REGRESSIONS.
//
// These four invariants must hold on the REAL seams rather than on a config a
// test copied by hand: the root pairing and the scratch impact reading are
// invalidated by an anchor, a delegated worker's PERSISTED receipt keeps the
// project it was admitted into, a worker's two moving blocks share ONE ceiling,
// and every constructor that builds a worker (task, nested task, auditor,
// orchestrate node) actually inherits the lent binding.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/orchestrate"
	"github.com/Agent-Field/codeaf/internal/store"
)

// noteBodyBetween reads one session note out of a whole provider body: the body
// after the opening up to the next note's opening. It is how the combined ceiling
// is measured on the request that was actually sent.
func noteBodyBetween(body, opening string) string {
	idx := strings.Index(body, opening)
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(opening):]
	cut := len(rest)
	for _, other := range []string{memoryNoteOpening, bindingNoteOpening, volatileNoteOpening, teamNoteOpening} {
		if other == opening {
			continue
		}
		if j := strings.Index(rest, other); j >= 0 && j < cut {
			cut = j
		}
	}
	return strings.TrimSpace(rest[:cut])
}

// 1. AN ANCHOR INVALIDATES THE SCRATCH OWNER'S PAIRING AND IMPACT READING. The
// pending demonstrated failure and its unspent alternative slot are cleared, and
// the cached impact block/cue/notices are dropped, under the held a.mu, so a
// scratch consequence cannot ride into the repository mid-turn.
func TestAnchorWorkspaceResetsOutcomePairingAndScratchImpacts(t *testing.T) {
	brain, err := store.Open(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })

	scratch := t.TempDir()
	repo := newTestRepo(t)
	scratchKey := anchoredProjectKey(scratch)
	agent := anchorAgent(t, brain, scratch, scratchKey)
	agent.mu.Lock()
	agent.stampUserLocked("make the release offline")
	agent.mu.Unlock()
	agent.prepareBindingContext(context.Background(), "make the release offline")

	// A demonstrated failure under the scratch owner, its pairing still open, and
	// a scratch owner's impact reading already cached for this turn.
	agent.memory.mu.Lock()
	agent.memory.outcomeFailedKey = "scratch:fail"
	agent.memory.outcomeFailedTool = "bash"
	agent.memory.outcomeFailedAction = "go test ./scratch"
	agent.memory.outcomeAlternativeDone = false
	agent.memory.impactPrepared = true
	agent.memory.impactBlock = "<contextual_impacts>scratch</contextual_impacts>"
	agent.memory.impactCue = "scratch cue"
	agent.memory.impactNotices = map[string]store.ContextualImpactNotice{"x": {}}
	agent.memory.impactOrder = []string{"x"}
	agent.memory.mu.Unlock()

	if _, err := agent.AnchorWorkspace(repo); err != nil {
		t.Fatalf("anchor: %v", err)
	}

	agent.memory.mu.Lock()
	defer agent.memory.mu.Unlock()
	if agent.memory.outcomeFailedKey != "" || agent.memory.outcomeFailedTool != "" ||
		agent.memory.outcomeFailedAction != "" || agent.memory.outcomeAlternativeDone {
		t.Fatalf("the scratch pairing survived the anchor: key=%q tool=%q action=%q done=%v",
			agent.memory.outcomeFailedKey, agent.memory.outcomeFailedTool,
			agent.memory.outcomeFailedAction, agent.memory.outcomeAlternativeDone)
	}
	if agent.memory.impactPrepared || agent.memory.impactBlock != "" || agent.memory.impactCue != "" ||
		agent.memory.impactNotices != nil || len(agent.memory.impactOrder) != 0 {
		t.Fatalf("the scratch impact context survived the anchor: prepared=%v block=%q cue=%q notices=%d",
			agent.memory.impactPrepared, agent.memory.impactBlock, agent.memory.impactCue, len(agent.memory.impactNotices))
	}
}

// 2. A DELEGATED WORKER'S PERSISTED RECEIPTS KEEP ITS ADMITTED PROJECT after the
// root conversation anchors to another repository. The row condition comes from
// the FROZEN origin, so a failure, its observed alternative and a promoted job
// all land under the admitted owner AND the admitted project, and none is
// retroactively widened to the anchored one.
func TestDelegatedReceiptsKeepAdmittedProjectAfterRootAnchor(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p1" })
	root.prepareBindingContext(context.Background(), "make the foobar parser tests pass")
	worker := spawnTaskWorker(t, root, dir)
	if worker.origin.Project != "p1" {
		t.Fatalf("worker origin was not stamped with its admitted project: %+v", worker.origin)
	}

	// The person anchors the conversation to another repository mid-run: the
	// root's live key moves, the already-admitted worker's must not.
	root.mu.Lock()
	root.config.MemoryProjectKey = "p2"
	root.mu.Unlock()

	ctx := context.Background()
	pre := worker.captureSourceSnapshot(ctx).Identity
	worker.recordOutcome(ctx, 0, delegatedBashCall("f1", "go test ./foobar"), toolResult{text: "undefined: foobar.Token", isError: true}, pre)
	worker.recordOutcome(ctx, 0, delegatedBashCall("s1", "go test ./foobar -run TestToken"), toolResult{text: "PASS foobar 0.12s"}, pre)
	worker.jobs.announce = nil
	settleJob(t, worker, "go build ./...", 2, false)

	admitted := attemptsFor(t, brain, store.OwnerProject("p1"), "p1")
	if len(admitted) != 3 {
		t.Fatalf("admitted-project receipts = %d, want failure+alternative+settled job: %+v", len(admitted), admitted)
	}
	statuses := map[string]int{}
	for _, row := range admitted {
		statuses[row.Status]++
		if row.Owner != store.OwnerProject("p1") || row.Conditions["project"] != "p1" {
			t.Fatalf("a receipt straddled the anchor: %+v", row)
		}
	}
	if statuses[store.AttemptFailed] != 2 || statuses[store.AttemptSucceeded] != 1 {
		t.Fatalf("receipt statuses wrong: %+v", statuses)
	}
	if rows := attemptsFor(t, brain, store.OwnerProject("p1"), "p2"); len(rows) != 0 {
		t.Fatalf("a receipt under the admitted owner was scoped to the anchored project: %+v", rows)
	}
	if rows := attemptsFor(t, brain, store.OwnerProject("p2"), "p2"); len(rows) != 0 {
		t.Fatalf("a receipt was retroactively widened to the anchored project: %+v", rows)
	}
}

// 3. THE WORKER'S TWO BLOCKS SHARE ONE CEILING. The mandatory approved-binding
// note is reserved first; the asynchronous semantic block spends only what is
// left, trimmed by whole records, so the ACTUAL provider body never carries more
// than memoryBlockRunes across the two notes and a revoked rule stays out.
func TestWorkerBindingAndSemanticShareOneCeiling(t *testing.T) {
	brain, err := store.Open(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })

	owner := store.OwnerProject("shared-key")
	seedApprovedRule(t, brain, "bound", owner, "workers must use the frozen toolchain")
	seedApprovedRule(t, brain, "revoked", owner, "revoked rule must never bind")
	if err := brain.SuppressContextualMemorySources(owner, "revoked", "explicit forget"); err != nil {
		t.Fatalf("suppress: %v", err)
	}
	var ids []string
	for i := 0; i < 24; i++ {
		id := fmt.Sprintf("sem-%02d", i)
		if _, err := brain.AddMemory(store.Memory{ID: id, Owner: owner, Type: store.MemoryFact, Title: id, Text: strings.Repeat("semantic filler ", 16)}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows, err := brain.GetMemories([]string{owner}, ids)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := renderMemoryBlock(rows, time.Now())
	// The routed block is itself bounded by the ceiling; the point is that once
	// the mandatory binding note is added, the TWO together would overflow it.
	if utf8.RuneCountInString(raw) <= memoryBlockRunes-500 {
		t.Fatalf("fixture semantic block was too small to prove the shared ceiling: %d runes", utf8.RuneCountInString(raw))
	}

	script := &reflexScript{routeErr: errors.New("router down"), answer: "done"}
	agent, _ := newTestAgent(t, script, func(c *Config) {
		c.bindingStore = brain
		c.MemoryProjectKey = "shared-key"
	})
	// The node hands its routed shortlist asynchronously; here it has already
	// arrived before the first request.
	agent.takeMemory(raw)

	collect(t, mustSubmit(t, agent, "build the release"))

	script.mu.Lock()
	if len(script.requests) == 0 {
		script.mu.Unlock()
		t.Fatal("the worker made no provider request")
	}
	joined := script.requests[0]
	script.mu.Unlock()

	bindingBody := noteBodyBetween(joined, bindingNoteOpening)
	semanticBody := noteBodyBetween(joined, memoryNoteOpening)
	if !strings.Contains(bindingBody, "frozen toolchain") {
		t.Fatalf("the binding note did not carry the approved rule: %q", bindingBody)
	}
	if strings.Contains(joined, "revoked rule must never bind") {
		t.Fatalf("a revoked rule was bound:\n%s", joined)
	}
	if strings.TrimSpace(semanticBody) == "" {
		t.Fatal("the optional semantic note was dropped entirely instead of trimmed to fit")
	}
	combined := utf8.RuneCountInString(bindingBody) + utf8.RuneCountInString(semanticBody)
	if combined > memoryBlockRunes {
		t.Fatalf("the two worker blocks exceeded the one shared ceiling: %d > %d", combined, memoryBlockRunes)
	}
	// The semantic half was trimmed by WHOLE records: every kept line is intact.
	if !strings.HasSuffix(semanticBody, "</memory>") {
		t.Fatalf("the trimmed semantic block lost its wrapper: %q", semanticBody)
	}
	for _, line := range strings.Split(strings.Trim(semanticBody, "\n"), "\n") {
		if strings.HasPrefix(line, "- ") && !strings.Contains(line, "semantic filler") {
			t.Fatalf("the semantic block kept a partial record: %q", line)
		}
	}
}

// 3b. A SINGLE RECORD TOO LONG FOR THE CEILING IS OMITTED WHOLE, never truncated,
// so an approved rule is either shown in full or absent.
func TestBindingProjectionOmitsAnOversizedRuleWhole(t *testing.T) {
	giant := store.Memory{ID: "giant", Owner: store.OwnerProject("k"), Type: store.MemoryDecision, Title: "giant", Text: strings.Repeat("x", memoryBlockRunes+500)}
	if block := renderBindingBlock([]store.Memory{giant}); block != "" {
		t.Fatalf("an oversized rule was rendered (and so truncated) rather than omitted: %d runes", utf8.RuneCountInString(block))
	}
	raw := "<memory>\n- keep me\n- " + strings.Repeat("y", 200) + "\n</memory>"
	got := trimRenderedMemoryBlock(raw, 28)
	if got != "<memory>\n- keep me\n</memory>" {
		t.Fatalf("whole-record trimming cut a record or kept a partial: %q", got)
	}
}

// 4. EVERY CONSTRUCTOR THAT BUILDS A WORKER INHERITS THE LENT BINDING AND THE
// FROZEN PROJECT, and the worker stays read-only. A first-worker test with the
// router down is not enough: a nested child, the auditor and an orchestrate node
// each build through their own constructor, and any one that forgot to carry the
// store would open without the rules that may not be late.
func TestWorkerBindingInheritsThroughActualConstructors(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	brain, err := store.Open(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })
	seedApprovedRule(t, brain, "ctor", store.OwnerProject("ctor-key"), "constructor-path workers must see this rule")

	script := &reflexScript{routeErr: errors.New("router down"), answer: "done"}
	root, _ := newTestAgent(t, script, func(c *Config) {
		c.Workspace = dir
		c.Memory = brain
		c.MemoryProjectKey = "ctor-key"
	})
	root.prepareBindingContext(context.Background(), "carry out the work")

	worker := spawnTaskWorker(t, root, dir)
	nested := spawnTaskWorker(t, worker, dir)
	if nested.origin.Project != "ctor-key" || nested.origin.Session != worker.origin.Session {
		t.Fatalf("the nested child did not inherit its parent's frozen origin: %+v", nested.origin)
	}

	graph := stubbedGraph(root, func(*TaskNode) {})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "audit", brief: "b", acceptance: "a"})
	auditor, err := root.newAuditAgent(dir, graph.node(id), plainDoor(nil), "")
	if err != nil {
		t.Fatalf("newAuditAgent: %v", err)
	}
	t.Cleanup(func() { _ = auditor.Close() })

	exec := &orchestrateExec{agent: root, call: roleRequest{model: "test/model"}, id: "1", request: "req", goal: "goal"}
	node, err := exec.newChild(dir, orchestrate.Node{ID: "n1", Title: "t"})
	if err != nil {
		t.Fatalf("orchestrate newChild: %v", err)
	}
	t.Cleanup(func() { _ = node.Close() })

	for name, child := range map[string]*Agent{"task": worker, "nested": nested, "auditor": auditor, "orchestrate": node} {
		if child.config.bindingStore != brain || child.config.MemoryProjectKey != "ctor-key" {
			t.Fatalf("%s worker did not inherit the lent brain and frozen key: store=%p key=%q", name, child.config.bindingStore, child.config.MemoryProjectKey)
		}
		if child.remembers() || child.memoryWritable() || len(child.memoryTools()) != 0 {
			t.Fatalf("%s worker gained a memory write or verb from the lent brain", name)
		}
		// AND ITS FIRST REQUEST, with the router down, carries the rule whole.
		child.prepareWorkerBinding(context.Background(), "carry out the work")
		child.mu.Lock()
		block := child.bindingText
		child.mu.Unlock()
		if !strings.Contains(block, "must see this rule") {
			t.Fatalf("%s worker's binding block lacked the inherited rule: %q", name, block)
		}
		if utf8.RuneCountInString(block) > memoryBlockRunes {
			t.Fatalf("%s worker's binding block exceeded the ceiling: %d", name, utf8.RuneCountInString(block))
		}
	}
}

// 5. A TASK NODE'S OPTIONAL RECALL IS READ UNDER THE SCOPE IT WAS ADMITTED INTO.
// The route closure captures the owners and project key when the node's reading
// is created; a root anchor that moves the conversation to another repository
// while that routing is still in flight must not let the node read the NEW
// project's memory. The router is "blocked" (nothing runs it) across the anchor,
// then released, and the actual captured route is what is exercised.
func TestNodeOptionalRecallKeepsAdmittedScopeAcrossRootAnchor(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	script := &reflexScript{route: `{"inject":[],"cmd":null}`}
	root, brain := brainAgent(t, script, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "old-key" })

	oldRule, err := brain.AddMemory(store.Memory{Owner: store.OwnerProject("old-key"), Type: store.MemoryFact, Title: "old scope", Text: "the old project routes the ledger through builtins"})
	if err != nil {
		t.Fatal(err)
	}
	newRule, err := brain.AddMemory(store.Memory{Owner: store.OwnerProject("new-key"), Type: store.MemoryFact, Title: "new scope", Text: "the new project routes the ledger through the vendor client"})
	if err != nil {
		t.Fatal(err)
	}

	graph := stubbedGraph(root, func(*TaskNode) {})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "route the ledger", brief: "route the ledger through the project", acceptance: "a", depth: 1})
	node := graph.node(id)
	ctx, end := root.withNodeMemory(context.Background(), node)
	defer end()
	reading := nodeMemoryOn(ctx, node)
	if reading == nil {
		t.Fatal("the node carried no memory reading")
	}

	// The routing has not run yet; the person anchors to another repository.
	root.mu.Lock()
	root.config.MemoryProjectKey = "new-key"
	root.mu.Unlock()

	// Released: asking for the OLD scope's memory still finds it.
	script.mu.Lock()
	script.route = `{"inject":["` + oldRule.ID + `"],"cmd":null}`
	script.mu.Unlock()
	if block := reading.route(context.Background()); !strings.Contains(block, "old project") {
		t.Fatalf("the admitted node lost its own project's memory after the anchor: %q", block)
	}

	// And the NEW project's memory cannot be pulled in through the same route.
	script.mu.Lock()
	script.route = `{"inject":["` + newRule.ID + `"],"cmd":null}`
	script.mu.Unlock()
	if block := reading.route(context.Background()); strings.Contains(block, "new project") {
		t.Fatalf("an admitted node's optional recall was widened to the anchored project: %q", block)
	}
	// NEGATIVE CONTROL: the conversation's own LIVE path does see the anchored
	// project, which is exactly the widening the captured scope refuses — so the
	// assertion above is not passing vacuously.
	if block := root.memoryBlock(context.Background(), "route the ledger through the project"); !strings.Contains(block, "new project") {
		t.Fatalf("control: the live conversation path did not see the anchored project's memory: %q", block)
	}
}

// 3c. THE BINDING PROJECTION HONORS AN UPDATE AND A REVOCATION. A newer approved
// observation for a rule is what binds (its source words ride the block), while a
// revoked rule stays out, and the projection still fits the one ceiling.
func TestBindingProjectionHonorsUpdateAndRevocation(t *testing.T) {
	brain, err := store.Open(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })
	owner := store.OwnerProject("ceil-key")
	rule := seedApprovedRule(t, brain, "policy", owner, "the ledger must use builtins")
	if _, err := brain.AppendContextualEvidence(store.ContextualEvidence{
		ID: "ev-policy-2", MemoryID: rule.ID, Owner: owner, SessionID: "s", TurnID: "t2",
		Actor: "user", Authority: "approved_rule", Observation: "the ledger must use the vendor client",
		Verification: "asserted", SourceKey: "s:t2:policy", SourceHash: "h2",
	}); err != nil {
		t.Fatal(err)
	}
	seedApprovedRule(t, brain, "revoked-policy", owner, "revoked rule must never bind")
	if err := brain.SuppressContextualMemorySources(owner, "revoked-policy", "explicit forget"); err != nil {
		t.Fatal(err)
	}

	agent, _ := newTestAgent(t, &reflexScript{}, func(c *Config) { c.bindingStore = brain; c.MemoryProjectKey = "ceil-key" })
	block := renderBindingBlock(agent.bindingMemories(brain, "ledger policy", ""))
	if !strings.Contains(block, "vendor client") {
		t.Fatalf("the updated rule's latest approved words were not bound: %q", block)
	}
	if strings.Contains(block, "revoked rule must never bind") {
		t.Fatalf("a revoked rule was bound: %q", block)
	}
	if utf8.RuneCountInString(block) > memoryBlockRunes {
		t.Fatalf("the binding block exceeded the shared ceiling: %d", utf8.RuneCountInString(block))
	}
}
