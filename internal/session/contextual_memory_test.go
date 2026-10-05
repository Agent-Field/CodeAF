package session

import (
	"context"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/reflex"
	"github.com/Agent-Field/codeaf/internal/store"
	"strings"
	"testing"
	"time"
)

func TestContextualOrdinaryExtractionRetainsConditionalRuleBeforeFreshReply(t *testing.T) {
	script := &reflexScript{extract: `{"mem":1,"type":"decision","scope":"project","title":"offline release runtime","text":"Release runtime uses standard library only.","source":"user","source_quote":"release artifacts must run offline","authority":"approved_rule","conditions":["release runtime only; development network allowed"],"rationale":"deploy without dependency downloads","rejected":["requests adds a runtime dependency"],"reconsider":"if the release environment permits downloads"}`}
	agent, brain := brainAgent(t, script, func(c *Config) { c.MemoryProjectKey = "ledger" })
	user := "The release artifacts must run offline, using only the standard library. Network is allowed during development."
	collect(t, mustSubmit(t, agent, user))
	agent.memoryJobs.Wait()
	rows, err := brain.ListMemories([]string{store.OwnerProject("ledger")}, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("ordinary extraction rows=%v error=%v", rows, err)
	}
	evidence, err := brain.ContextualEvidenceForMemory(rows[0].Owner, rows[0].ID)
	if err != nil || evidence.Authority != "approved_rule" || len(evidence.Applicability) != 1 || evidence.Observation != user {
		t.Fatalf("evidence=%+v error=%v", evidence, err)
	}
	_ = agent.Close()
	freshScript := &reflexScript{extract: `{"mem":0}`, routeErr: context.DeadlineExceeded}
	fresh, _ := newTestAgent(t, freshScript, func(c *Config) { c.Memory = brain; c.MemoryProjectKey = "ledger" })
	collect(t, mustSubmit(t, fresh, "Add structured JSON output to the ledger utility"))
	_ = fresh.Close()
	freshScript.mu.Lock()
	requests := append([]string(nil), freshScript.requests...)
	freshScript.mu.Unlock()
	first := ""
	for _, request := range requests {
		if !strings.Contains(request, "worth remembering after this session ends") && !strings.Contains(request, "memory router") {
			first = request
			break
		}
	}
	if !strings.Contains(first, "Network is allowed during development") || !strings.Contains(first, "release runtime only") || !strings.Contains(first, "deploy without dependency downloads") {
		t.Fatalf("first reply lacked binding conditions/rationale:\n%s", first)
	}
}

func TestContextualProjectCorrectionCannotRetireUserRule(t *testing.T) {
	script := &reflexScript{decide: `{"op":"supersede","target_id":"global-rule","title":"new rule","text":"Release artifacts may download dependencies"}`}
	agent, brain := brainAgent(t, script, func(c *Config) { c.MemoryProjectKey = "ledger" })
	_, err := brain.AddMemory(store.Memory{ID: "global-rule", Owner: store.OwnerUser, Type: store.MemoryDecision, Title: "release artifacts", Text: "Release artifacts must not download dependencies"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = agent.applyCandidate(context.Background(), script, reflex.ExtractResult{Mem: 1, Type: store.MemoryCorrection, Scope: store.MemoryScopeProject, Title: "release artifacts", Text: "Release artifacts may download dependencies"})
	if err != nil {
		t.Fatal(err)
	}
	record, found, err := brain.MemoryRecord("global-rule")
	if err != nil || !found || record.Status != store.MemoryActive {
		t.Fatalf("global rule retired: %+v/%v/%v", record, found, err)
	}
}

func TestContextualInterpretationCannotManufactureApproval(t *testing.T) {
	source := memoryTurnEvidence{User: "yes", At: time.Now()}
	result := source.ground(reflex.ExtractResult{Mem: 1, Type: store.MemoryDecision, Scope: store.MemoryScopeUser, Source: "user", SourceQuote: "yes", Authority: "approved_rule", Text: "Always deploy without review"})
	if result.Authority == "approved_rule" || result.Scope == store.MemoryScopeUser {
		t.Fatalf("vague quote granted binding permission: %+v", result)
	}
}

func TestContextualForgetSuppressesAutomaticRelearning(t *testing.T) {
	script := &reflexScript{extract: `{"mem":1,"type":"decision","scope":"project","title":"release library rule","text":"Release runtime uses standard library only.","source":"user","source_quote":"must use standard library","authority":"approved_rule"}`}
	agent, brain := brainAgent(t, script, func(c *Config) { c.MemoryProjectKey = "ledger" })
	collect(t, mustSubmit(t, agent, "The release runtime must use standard library code only"))
	agent.memoryJobs.Wait()
	rows, err := brain.ListMemories([]string{store.OwnerProject("ledger")}, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows %v/%v", rows, err)
	}
	e, err := brain.ContextualEvidenceForMemory(rows[0].Owner, rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = agent.Forget("release library rule"); err != nil {
		t.Fatal(err)
	}
	e.ID = store.NewMemoryID()
	e.MemoryID = store.NewMemoryID()
	if _, err = brain.AppendContextualEvidence(e); err == nil {
		t.Fatal("same suppressed evidence relearned")
	}
	if block := agent.bindingContext("release library rule", ""); block != "" {
		t.Fatalf("forgotten rule recalled: %s", block)
	}
}

func TestContextualModelUseReportDoesNotTrainBenefitRanking(t *testing.T) {
	script := &reflexScript{extract: `{"mem":0,"used":["rule"]}`}
	agent, brain := brainAgent(t, script, nil)
	_, err := brain.AddMemory(store.Memory{ID: "rule", Owner: store.OwnerUser, Type: store.MemoryFact, Title: "ledger precision", Text: "Use exact decimal arithmetic"})
	if err != nil {
		t.Fatal(err)
	}
	agent.memory.setInjected([]reflex.Stub{{ID: "rule", Title: "ledger precision"}})
	agent.learnFromTurn("Check ledger precision", "I used that memory")
	agent.memoryJobs.Wait()
	m, _, err := brain.MemoryRecord("rule")
	if err != nil || m.UseCount != 0 || m.MissCount != 0 {
		t.Fatalf("self report trained ranking: %+v/%v", m, err)
	}
}

// A RARE APPROVED RULE KEEPS BINDING UNDER A BURST OF NEWER INCIDENTAL
// OBSERVATIONS, AND A LATE CORRECTION STILL WINS. This is the whole attention
// contract in one place: the projection spends its window on authority, so 128+
// newer observations cannot starve the rule, and the newest record for the
// claim — a correction that is not itself an approval — prevents the old
// approval from being resurrected.
func TestContextualBindingRuleSurvivesNoiseAndLateCorrectionWins(t *testing.T) {
	script := &reflexScript{}
	agent, brain := brainAgent(t, script, func(c *Config) { c.MemoryProjectKey = "ledger" })
	owner := store.OwnerProject("ledger")
	m, err := brain.AddMemory(store.Memory{ID: "rule", Owner: owner, Type: store.MemoryDecision,
		Title: "offline release", Text: "Release runtime uses standard library only."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := brain.AppendContextualEvidence(store.ContextualEvidence{ID: "ev-rule", MemoryID: m.ID,
		Owner: owner, SessionID: "s", TurnID: "t", Actor: "user", Authority: "approved_rule",
		Observation: "release artifacts must run offline", Verification: "asserted",
		SourceKey: "s:t", SourceHash: "h"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < store.ContextualEvidenceLimit+2; i++ {
		noise := store.ContextualEvidence{ID: fmt.Sprintf("noise-%d", i), MemoryID: fmt.Sprintf("n-%d", i),
			Owner: owner, SessionID: "s", TurnID: fmt.Sprintf("t-%d", i), Actor: "tool", Tool: "bash",
			ReceiptIDs: []string{fmt.Sprintf("r-%d", i)}, Authority: "observation",
			Observation: "incidental output", Verification: "observed",
			SourceKey: fmt.Sprintf("s:t-%d", i), SourceHash: fmt.Sprintf("h-%d", i)}
		if _, err := brain.AppendContextualEvidence(noise); err != nil {
			t.Fatal(err)
		}
	}
	revision := agent.captureSourceSnapshot(context.Background()).Identity
	block := agent.bindingContext("release runtime", revision)
	if !strings.Contains(block, "standard library only") {
		t.Fatalf("approved rule starved by incidental noise: %q", block)
	}
	// A LATER CORRECTION IS THE NEWEST RECORD AND MUST WIN.
	if _, err := brain.AppendContextualEvidence(store.ContextualEvidence{ID: "ev-corr", MemoryID: m.ID,
		Owner: owner, SessionID: "s", TurnID: "t2", Actor: "assistant", Authority: "proposal",
		Observation: "release runtime may now download dependencies", Verification: "unverified",
		SourceKey: "s:t2", SourceHash: "h2"}); err != nil {
		t.Fatal(err)
	}
	if block := agent.bindingContext("release runtime", revision); strings.Contains(block, "standard library only") {
		t.Fatalf("late correction did not prevent resurrection: %q", block)
	}
	_ = agent.Close()
}

// A DERIVED RECEIPT IS NOT AN INDEPENDENT OBSERVATION. A history lookup, this
// session's own memory reader, or a task worker's report was already written or
// summarized by a model, so citing one back cannot corroborate a claim. Genuine
// raw tool evidence is untouched.
func TestContextualDerivedReceiptCannotBecomeObservation(t *testing.T) {
	source := memoryTurnEvidence{User: "what did we decide about the ledger?", At: time.Now(),
		Receipts: []memoryToolReceipt{
			{ID: "hist", Tool: "search_conversations", Status: "done", Text: "we decided the ledger uses decimals"},
			{ID: "sum", Tool: "tasks", Status: "done", Text: "worker: the export contract is final"},
			{ID: "mem", Tool: "memory_evidence", Status: "done", Text: "approved rule: use decimals"},
		}}
	for _, id := range []string{"hist", "sum", "mem"} {
		got := source.ground(reflex.ExtractResult{Mem: 1, Type: store.MemoryDecision, Scope: store.MemoryScopeProject,
			Source: "assistant", ReceiptID: id, Text: "the ledger uses decimals"})
		if got.Authority == "observation" || got.Source == "tool" {
			t.Fatalf("derived receipt %q became observed corroboration: %+v", id, got)
		}
	}
	// AND A GENUINE RAW TOOL RECEIPT STILL DOES.
	raw := memoryTurnEvidence{User: "check the build", At: time.Now(),
		Receipts: []memoryToolReceipt{{ID: "bash-1", Tool: "bash", Status: "done", Text: "FAIL: 0 tests ran"}}}
	got := raw.ground(reflex.ExtractResult{Mem: 1, Type: store.MemoryFact, Scope: store.MemoryScopeProject,
		Source: "assistant", ReceiptID: "bash-1", Text: "the test suite is failing"})
	if got.Authority != "observation" || got.Source != "tool" {
		t.Fatalf("genuine raw tool evidence was denied: %+v", got)
	}
}

// AUTHORITY AND GLOBAL SCOPE ARE GATED ON THE SUPPORTING SPAN, NOT THE WHOLE
// TURN. An unrelated true quote sharing a turn with another fact must not widen
// it or manufacture approval, while normal literal user constraints still bind
// automatically.
func TestContextualLiteralAuthorityGatesOnSupportingSpan(t *testing.T) {
	user := "Across all projects I prefer concise answers. The ledger must use exact decimals."
	source := memoryTurnEvidence{User: user, At: time.Now()}
	// A project rule is proven by its own span, despite the global sentence.
	rule := source.ground(reflex.ExtractResult{Mem: 1, Type: store.MemoryDecision, Scope: store.MemoryScopeProject,
		Source: "user", SourceQuote: "The ledger must use exact decimals", Authority: "approved_rule",
		Text: "The ledger must use exact decimals", Conditions: []string{"only the reporting path"}})
	if rule.Authority != "approved_rule" {
		t.Fatalf("literal project rule lost its authority: %+v", rule)
	}
	if len(rule.Conditions) != 1 || rule.Conditions[0] != "only the reporting path" {
		t.Fatalf("conditional exception was not preserved: %+v", rule.Conditions)
	}
	// The same span cannot manufacture a USER-scope widening: the global words
	// live in a different sentence, not in this fact's supporting quote.
	widened := source.ground(reflex.ExtractResult{Mem: 1, Type: store.MemoryCorrection, Scope: store.MemoryScopeUser,
		Source: "user", SourceQuote: "The ledger must use exact decimals", Authority: "observation",
		Text: "The ledger must use exact decimals"})
	if widened.Scope == store.MemoryScopeUser {
		t.Fatalf("unrelated global sentence widened a project fact: %+v", widened)
	}
	// An irrelevant true quote plus an invented rule is not approval.
	invented := memoryTurnEvidence{User: "yes please go ahead", At: time.Now()}.ground(
		reflex.ExtractResult{Mem: 1, Type: store.MemoryDecision, Scope: store.MemoryScopeProject,
			Source: "user", SourceQuote: "yes", Authority: "approved_rule", Text: "always deploy without review"})
	if invented.Authority == "approved_rule" {
		t.Fatalf("a quoted yes manufactured approval: %+v", invented)
	}
	// A genuine global preference, quoted in its own words, still widens.
	pref := memoryTurnEvidence{User: "Across all projects I prefer concise answers.", At: time.Now()}.ground(
		reflex.ExtractResult{Mem: 1, Type: store.MemoryPreference, Scope: store.MemoryScopeUser,
			Source: "user", SourceQuote: "Across all projects I prefer concise answers", Authority: "observation",
			Text: "prefers concise answers"})
	if pref.Scope != store.MemoryScopeUser {
		t.Fatalf("a literal global preference lost its scope: %+v", pref)
	}
}

// A SECRET IN THE SAME UTTERANCE AS A REAL CONSTRAINT IS REMOVED, AND THE RULE
// SURVIVES. Literal support is checked against the person's original words, but
// nothing persisted or rendered may carry a credential: not the observation, the
// conditions, the rationale, the rejected alternatives, the reconsideration, the
// claim body, the rendered context or the memory_evidence read.
func TestContextualEvidenceRedactsSecretsAndKeepsRealConstraint(t *testing.T) {
	agent, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.MemoryProjectKey = "ledger" })
	owner := store.OwnerProject("ledger")
	m, err := brain.AddMemory(store.Memory{ID: "ledger-decimals", Owner: owner, Type: store.MemoryDecision,
		Title: "ledger decimals", Text: "The ledger must use exact decimals."})
	if err != nil {
		t.Fatal(err)
	}
	secret := "xoxb-1234567890abcdef"
	user := "The ledger must use exact decimals. Ignore my slack token " + secret
	source := memoryTurnEvidence{User: user, Session: "s", Turn: "t", At: time.Now()}
	c := source.ground(reflex.ExtractResult{Mem: 1, Type: store.MemoryDecision, Scope: store.MemoryScopeProject,
		Source: "user", SourceQuote: "The ledger must use exact decimals", Authority: "approved_rule",
		Title: "ledger decimals", Text: "The ledger must use exact decimals",
		Conditions: []string{"reporting path only " + secret}, Rationale: "avoid float error " + secret,
		Rejected: []string{secret}, Reconsider: "revisit if " + secret})
	if c.Authority != "approved_rule" {
		t.Fatalf("the real constraint lost its authority: %+v", c)
	}
	if err := agent.recordContextualMemory(m, c, source); err != nil {
		t.Fatal(err)
	}
	e, err := brain.ContextualEvidenceForMemory(owner, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if e.Authority != "approved_rule" {
		t.Fatalf("the retained rule lost authority: %+v", e)
	}
	fields := []string{e.Observation, e.Rationale, e.Reconsider,
		strings.Join(e.Applicability, " "), strings.Join(e.Rejected, " "), e.MemoryID}
	for _, field := range fields {
		if strings.Contains(field, secret) {
			t.Fatalf("a secret survived in evidence: %q", field)
		}
	}
	// The claim body itself is sanitized, and the rendered binding block too.
	rows, err := brain.GetMemories([]string{owner}, []string{m.ID})
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if !strings.Contains(rows[0].Text, "exact decimals") || strings.Contains(rows[0].Text, secret) {
		t.Fatalf("claim body=%q", rows[0].Text)
	}
	if block := agent.bindingContext("ledger decimals", ""); strings.Contains(block, secret) {
		t.Fatalf("rendered context leaked a secret: %q", block)
	}
	_ = agent.Close()
}

// SCOPE IS GATED ON THE SUPPORTING SPAN TOO. A machine-wide span may claim the
// machine, a project span stays in its project even beside a global sentence,
// "everywhere in this project" is project scope, and no tool observation is
// inferred to be machine-wide.
func TestContextualScopeGatesOnSupportingSpan(t *testing.T) {
	user := "Across all projects I prefer concise answers. On this machine, always use the mirror. The venv must pin the mirror."
	source := memoryTurnEvidence{User: user, At: time.Now()}
	local := source.ground(reflex.ExtractResult{Mem: 1, Type: store.MemoryDecision, Scope: store.MemoryScopeEnv,
		Source: "user", SourceQuote: "The venv must pin the mirror", Authority: "approved_rule", Text: "pin the mirror"})
	if local.Scope == store.MemoryScopeEnv || local.Scope == store.MemoryScopeUser {
		t.Fatalf("a project span was widened by a neighbouring global sentence: %+v", local)
	}
	machine := source.ground(reflex.ExtractResult{Mem: 1, Type: store.MemoryDecision, Scope: store.MemoryScopeEnv,
		Source: "user", SourceQuote: "On this machine, always use the mirror", Authority: "approved_rule", Text: "use the mirror"})
	if machine.Scope != store.MemoryScopeEnv {
		t.Fatalf("an explicit machine-wide span was restricted: %+v", machine)
	}
	project := memoryTurnEvidence{User: "Use tabs everywhere in this project.", At: time.Now()}.ground(
		reflex.ExtractResult{Mem: 1, Type: store.MemoryPreference, Scope: store.MemoryScopeUser,
			Source: "user", SourceQuote: "Use tabs everywhere in this project", Authority: "observation", Text: "use tabs"})
	if project.Scope == store.MemoryScopeUser {
		t.Fatalf("a project-qualified everywhere became the person at large: %+v", project)
	}
	tool := memoryTurnEvidence{User: "read the config", At: time.Now(),
		Receipts: []memoryToolReceipt{{ID: "r", Tool: "bash", Status: "done", Text: "venv uses mirror"}}}.ground(
		reflex.ExtractResult{Mem: 1, Type: store.MemoryFact, Scope: store.MemoryScopeEnv,
			Source: "assistant", ReceiptID: "r", Text: "the venv uses a mirror"})
	if tool.Scope == store.MemoryScopeEnv {
		t.Fatalf("a tool observation was inferred machine-wide: %+v", tool)
	}
}

// A MACHINE FACT CARRIES NO PROJECT CONDITION AND APPLIES ACROSS THE SAME
// AUTHORIZED MACHINE; A PROJECT FACT STAYS IN ITS PROJECT, AND A TOOL-LABELLED
// MACHINE CLAIM DOES NOT REACH ANOTHER PROJECT.
func TestContextualMachineScopeAppliesAcrossProjects(t *testing.T) {
	script := &reflexScript{}
	agentA, brain := brainAgent(t, script, func(c *Config) { c.MemoryProjectKey = "project-a" })
	machine, err := brain.AddMemory(store.Memory{ID: "mirror", Owner: store.OwnerMachine, Type: store.MemoryDecision,
		Title: "mirror", Text: "Use the internal mirror."})
	if err != nil {
		t.Fatal(err)
	}
	source := memoryTurnEvidence{User: "On this machine, always use the mirror.", Session: "s", Turn: "t", At: time.Now()}
	candidate := source.ground(reflex.ExtractResult{Mem: 1, Type: store.MemoryDecision, Scope: store.MemoryScopeEnv,
		Source: "user", SourceQuote: "On this machine, always use the mirror", Authority: "approved_rule", Text: "Use the internal mirror."})
	if candidate.Scope != store.MemoryScopeEnv {
		t.Fatalf("machine span not kept: %+v", candidate)
	}
	if err := agentA.recordContextualMemory(machine, candidate, source); err != nil {
		t.Fatal(err)
	}
	evidence, err := brain.ContextualEvidenceForMemory(store.OwnerMachine, machine.ID)
	if err != nil || evidence.Owner != store.OwnerMachine || len(evidence.Conditions) != 0 {
		t.Fatalf("machine evidence carried a project condition: %+v err=%v", evidence, err)
	}
	// The same machine sees it from another project.
	agentB, _ := newTestAgent(t, script, func(c *Config) { c.Memory = brain; c.MemoryProjectKey = "project-b" })
	if block := agentB.bindingContext("the internal mirror", ""); !strings.Contains(block, "internal mirror") {
		t.Fatalf("a machine rule did not apply on another project of the same machine: %q", block)
	}
	// A TOOL-LABELLED machine claim stays in its origin project.
	project, err := brain.AddMemory(store.Memory{ID: "venv", Owner: store.OwnerProject("project-a"), Type: store.MemoryFact,
		Title: "venv", Text: "The venv pins the mirror."})
	if err != nil {
		t.Fatal(err)
	}
	toolSource := memoryTurnEvidence{User: "read the venv", Session: "s", Turn: "t2", At: time.Now(),
		Receipts: []memoryToolReceipt{{ID: "r", Tool: "bash", Status: "done", Text: "pip.conf pins the mirror"}}}
	toolCandidate := toolSource.ground(reflex.ExtractResult{Mem: 1, Type: store.MemoryFact, Scope: store.MemoryScopeEnv,
		Source: "assistant", ReceiptID: "r", Text: "the venv pins the mirror"})
	if err := agentA.recordContextualMemory(project, toolCandidate, toolSource); err != nil {
		t.Fatal(err)
	}
	if block := agentB.bindingContext("the venv pins the mirror", ""); strings.Contains(block, "pins the mirror") {
		t.Fatalf("a tool-labelled machine claim reached another project: %q", block)
	}
	_ = agentA.Close()
}

// A TRUE SUBSTRING INSIDE A QUESTION OR A REJECTED THIRD-PARTY QUOTATION IS NOT
// APPROVAL. The containing clause decides, while a polite real directive and an
// ordinary literal constraint still bind automatically.
func TestContextualSpanAuthorityRejectsQuestionAndRejectedQuote(t *testing.T) {
	question := memoryTurnEvidence{User: "Should we never use pandas?", At: time.Now()}.ground(
		reflex.ExtractResult{Mem: 1, Type: store.MemoryDecision, Scope: store.MemoryScopeProject,
			Source: "user", SourceQuote: "never use pandas", Authority: "approved_rule", Text: "never use pandas"})
	if question.Authority == "approved_rule" {
		t.Fatalf("a rule inside a question became approval: %+v", question)
	}
	rejected := memoryTurnEvidence{User: "The reviewer said never use floats, but I reject that.", At: time.Now()}.ground(
		reflex.ExtractResult{Mem: 1, Type: store.MemoryDecision, Scope: store.MemoryScopeProject,
			Source: "user", SourceQuote: "never use floats", Authority: "approved_rule", Text: "never use floats"})
	if rejected.Authority == "approved_rule" {
		t.Fatalf("a rejected third-party quotation became approval: %+v", rejected)
	}
	polite := memoryTurnEvidence{User: "Please make sure the release never uses pandas, except in the legacy importer.", At: time.Now()}.ground(
		reflex.ExtractResult{Mem: 1, Type: store.MemoryDecision, Scope: store.MemoryScopeProject,
			Source: "user", SourceQuote: "never uses pandas", Authority: "approved_rule",
			Text: "never uses pandas", Conditions: []string{"except in the legacy importer"}})
	if polite.Authority != "approved_rule" || len(polite.Conditions) != 1 {
		t.Fatalf("a polite real directive was not kept: %+v", polite)
	}
	plain := memoryTurnEvidence{User: "The ledger must use exact decimals.", At: time.Now()}.ground(
		reflex.ExtractResult{Mem: 1, Type: store.MemoryDecision, Scope: store.MemoryScopeProject,
			Source: "user", SourceQuote: "must use exact decimals", Authority: "approved_rule", Text: "use exact decimals"})
	if plain.Authority != "approved_rule" {
		t.Fatalf("an ordinary literal constraint was demoted: %+v", plain)
	}
}
