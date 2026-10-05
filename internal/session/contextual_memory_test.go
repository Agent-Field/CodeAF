package session

import (
	"context"
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
