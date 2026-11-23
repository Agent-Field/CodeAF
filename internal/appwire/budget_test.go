package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

type budgetEngine struct {
	fakeEngine
	got []AgentBudgetSet
}

func (b *budgetEngine) AgentBudget(id string) (AgentBudget, error) {
	return AgentBudget{Scope: BudgetScopeAgent, Source: BudgetInherited, Limit: LimitUnknown, From: BudgetFromRecord}, nil
}

func (b *budgetEngine) SetAgentBudget(set AgentBudgetSet) (AgentBudget, error) {
	b.got = append(b.got, set)
	out := AgentBudget{Scope: BudgetScopeAgent, Source: BudgetUnlimited, Limit: LimitUnlimited, From: BudgetFromRecord}
	if set.USD != nil {
		out.Source, out.Limit, out.USD, out.BaseUSD = BudgetSet, LimitLimited, set.USD, set.USD
	}
	return out, nil
}

// AN ENGINE WITHOUT THE CAPABILITY DOES NOT HAVE THE VERB.
func TestAgentBudgetAbsentWithoutCapability(t *testing.T) {
	for name := range budgetMethods {
		_, err := methods[name].call(&fakeEngine{}, json.RawMessage(`{"id":"a","usd":5}`))
		if wireCode(t, err) != CodeUnknownMethod {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// ZERO IS NOT NONE, AND A MISSING FIGURE IS NOT NONE EITHER: only null clears
// the cap, and every other shape that is not a positive amount is refused
// before the engine is asked.
func TestAgentBudgetSetValidates(t *testing.T) {
	eng := &budgetEngine{}
	for _, params := range []string{
		`{}`, `{"usd":5}`, `{"id":"  ","usd":5}`, `{"id":"a"}`, `{"id":"a","usd":0}`, `{"id":"a","usd":-1}`,
		`{"id":"a","usd":"8"}`, `{"id":"a","usd":true}`, `{"id":"a","usd":5,"for":"today"}`, `{"id":7,"usd":5}`,
	} {
		_, err := methods["agents.budget.set"].call(eng, json.RawMessage(params))
		if wireCode(t, err) != CodeBadParams {
			t.Fatalf("%s: %v", params, err)
		}
	}
	if len(eng.got) != 0 {
		t.Fatalf("a refused set reached the engine: %+v", eng.got)
	}
	if _, err := methods["agents.budget.get"].call(eng, json.RawMessage(`{}`)); wireCode(t, err) != CodeBadParams {
		t.Fatalf("a get with no id: %v", err)
	}

	out, err := methods["agents.budget.set"].call(eng, json.RawMessage(`{"id":"a","usd":null}`))
	if err != nil || len(eng.got) != 1 || eng.got[0].ID != "a" || eng.got[0].USD != nil {
		t.Fatalf("null = %+v, %v", eng.got, err)
	}
	if got := out.(*AgentBudgetResult).Budget; got.Limit != LimitUnlimited || got.USD != nil {
		t.Fatalf("null answered %+v", got)
	}
	out, err = methods["agents.budget.set"].call(eng, json.RawMessage(`{"id":"a","usd":7.5}`))
	if err != nil || len(eng.got) != 2 || eng.got[1].USD == nil || *eng.got[1].USD != 7.5 {
		t.Fatalf("7.5 = %+v, %v", eng.got, err)
	}
	raw, _ := json.Marshal(out)
	if string(raw) != `{"budget":{"scope":"agent","source":"set","limit":"limited","usd":7.5,"base_usd":7.5,"from":"record"}}` {
		t.Fatalf("the answer travels as %s", raw)
	}
}

// The schema carries both methods, the Agent's optional budget, and its enums.
func TestAgentBudgetInTheSchema(t *testing.T) {
	raw, err := SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Methods map[string]json.RawMessage `json:"methods"`
		Defs    map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"agents.budget.get", "agents.budget.set"} {
		if doc.Methods[name] == nil {
			t.Errorf("schema lacks %s", name)
		}
	}
	if params := string(doc.Defs["in.AgentBudgetSetWire"]); !strings.Contains(params, `"null"`) {
		t.Errorf("agents.budget.set's usd is not nullable: %s", params)
	}
	if agent := string(doc.Defs["Agent"]); !strings.Contains(agent, `"budget"`) {
		t.Errorf("Agent lacks budget: %s", agent)
	}
	budget := string(doc.Defs["AgentBudget"])
	for _, word := range []string{`"inherited"`, `"unknown"`, `"engine"`} {
		if !strings.Contains(budget, word) {
			t.Errorf("AgentBudget lacks %s: %s", word, budget)
		}
	}
}
