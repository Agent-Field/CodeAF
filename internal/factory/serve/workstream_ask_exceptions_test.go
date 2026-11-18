package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// AFI SUGGESTS LOOSENING THE ASK RULE, AND YES MEANS IT DECIDES THAT KIND
// ITSELF (owner decisions §13, People and rules screen 16). Eleven Asks wait,
// six about docs wording: the steward is shown the suggestion and nobody else
// is; Yes adds the exception through the rules (a decision); the next Ask of
// that kind is not raised but listed under Assumed; and a writer that does not
// know the field keeps the exception rather than wiping it.
func TestTooManyAsksSuggestAnExceptionAndYesDecidesThatKind(t *testing.T) {
	o := newWSOrg(t)
	ws, _, _, _ := o.orgScene(t)
	ctx := context.Background()
	d := &agentDoor{h: o.h, id: ws.ID, asker: "mia"}
	work := func(tool, args string) map[string]any {
		t.Helper()
		line, err := d.Work(ctx, tool, json.RawMessage(args))
		if err != nil {
			t.Fatalf("%s %s: %v", tool, args, err)
		}
		var out map[string]any
		_ = json.Unmarshal([]byte(line), &out)
		return out
	}
	const brief = `"goal":"Ship the docs","deliverable":"A page","acceptance":["Reads plainly","Under 300 words"]`
	ask := func(i int, kind string) string {
		id := work("ws_task_create", fmt.Sprintf(`{"title":"Docs section %d","request":"Write it.",%s}`, i, brief))["task"].(map[string]any)["id"].(string)
		work("ws_task_update", `{"task":"`+id+`","state":"running"}`)
		return work("ws_task_update", `{"task":"`+id+`","ask":{"title":"Keep this wording?","kind":"`+kind+`","options":[{"label":"Keep Afi's wording","recommended":true},{"label":"Rewrite it"}]}}`)["said"].(string)
	}
	for i := 0; i < 11; i++ {
		kind := "docs wording"
		if i >= 6 {
			kind = fmt.Sprint("other ", i)
		}
		ask(i, kind)
	}
	var cur api.Workstream
	o.do(t, "abir", "GET", "/v1/workstreams/"+ws.ID, -1, nil, 200, &cur)
	s := cur.RuleSuggestion
	if s == nil || s.Kind != "docs wording" || s.Waiting != 11 || s.OfKind != 6 || s.Yes != "Yes, stop asking about docs wording" {
		t.Fatalf("the steward's suggestion: %+v", s)
	}
	var other api.Workstream
	o.do(t, "santosh", "GET", "/v1/workstreams/"+ws.ID, -1, nil, 200, &other)
	if other.RuleSuggestion != nil {
		t.Fatalf("someone who cannot change the rules is shown the suggestion: %+v", other.RuleSuggestion)
	}

	rules := cur.Rules
	rules.Exceptions = []factory.AskException{{Kind: s.Kind}}
	o.do(t, "abir", "PATCH", "/v1/workstreams/"+ws.ID, cur.Rev, api.WorkstreamPatch{Rules: &rules}, 200, nil)
	cur = api.Workstream{}
	o.do(t, "abir", "GET", "/v1/workstreams/"+ws.ID, -1, nil, 200, &cur)
	if len(cur.Rules.Exceptions) != 1 || cur.Rules.Exceptions[0].AddedBy == nil || cur.Rules.Exceptions[0].AddedBy.ID != "abir" || cur.Rules.Exceptions[0].AddedAt == "" {
		t.Fatalf("the exception is not kept with who added it: %+v", cur.Rules.Exceptions)
	}
	if cur.RuleSuggestion != nil && cur.RuleSuggestion.Kind == "docs wording" {
		t.Fatalf("the kind let go is still suggested: %+v", cur.RuleSuggestion)
	}
	decided := false
	for _, x := range cur.Decisions {
		decided = decided || x.Text == "Afi decides docs wording without asking"
	}
	if !decided {
		t.Fatalf("Yes recorded no decision: %+v", cur.Decisions)
	}

	said := ask(20, "Docs Wording")
	if !strings.Contains(said, "Your Ask was not raised") {
		t.Fatalf("the agent is not told its Ask was decided: %q", said)
	}
	var list api.WorkstreamTasks
	o.do(t, "abir", "GET", "/v1/workstreams/"+ws.ID+"/tasks", -1, nil, 200, &list)
	for _, x := range list.Tasks {
		if x.Title != "Docs section 20" {
			continue
		}
		if x.Ask != nil || x.State == factory.TaskNeeds || len(x.Assumed) != 1 ||
			x.Assumed[0].Text != "Keep this wording? · Keep Afi's wording (Afi decides docs wording here without asking)" {
			t.Fatalf("the excepted Ask was raised, or not assumed: %+v", x)
		}
	}

	// A writer that predates exceptions sends rules without them.
	old := cur.Rules
	old.Exceptions, old.NotNow = nil, nil
	o.do(t, "abir", "PATCH", "/v1/workstreams/"+ws.ID, cur.Rev, api.WorkstreamPatch{Rules: &old}, 200, nil)
	o.do(t, "abir", "GET", "/v1/workstreams/"+ws.ID, -1, nil, 200, &cur)
	if len(cur.Rules.Exceptions) != 1 {
		t.Fatalf("a save that did not mention exceptions wiped them: %+v", cur.Rules)
	}
	packet, _ := o.h.taskPacket(ctx, d, list.Tasks[0].ID, "Go")
	if !strings.Contains(packet, "Decided without asking here: docs wording.") || !strings.Contains(packet, "names its kind") {
		t.Errorf("the run is not told the exceptions:\n%s", packet)
	}
}

// WHO ANSWERS WHEN THE DECIDER IS AWAY: the fallback, after the wait the rules
// set, named on the Ask in every read the Inbox draws from, and allowed to
// answer.
func TestTheFallbackCoversAnAskAfterTheWaitTheRulesSet(t *testing.T) {
	o := newWSOrg(t)
	ws, _, _, _ := o.orgScene(t)
	ctx := context.Background()
	d := &agentDoor{h: o.h, id: ws.ID, asker: "mia"}
	line, err := d.Work(ctx, "ws_task_create", json.RawMessage(`{"title":"Pick the date","request":"Choose.","goal":"g","deliverable":"d","acceptance":["a","b"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var made map[string]any
	_ = json.Unmarshal([]byte(line), &made)
	id := made["task"].(map[string]any)["id"].(string)
	if _, err := d.Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+id+`","state":"running"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+id+`","ask":{"title":"Which day?","options":[{"label":"Monday","recommended":true},{"label":"Friday"}]}}`)); err != nil {
		t.Fatal(err)
	}
	var cur api.Workstream
	o.do(t, "abir", "GET", "/v1/workstreams/"+ws.ID, -1, nil, 200, &cur)
	rules := cur.Rules
	rules.Fallback, rules.FallbackAfterHours = &factory.RulesPerson{ID: "santosh"}, 12
	o.do(t, "abir", "PATCH", "/v1/workstreams/"+ws.ID, cur.Rev, api.WorkstreamPatch{Rules: &rules}, 200, nil)
	covered := func() *factory.PersonRef {
		t.Helper()
		var home api.WorkHome
		o.do(t, "santosh", "GET", "/v1/work", -1, nil, 200, &home)
		for _, x := range home.Tasks {
			if x.ID == id && x.Ask != nil {
				return x.Ask.CoveredBy
			}
		}
		t.Fatalf("the task is not in santosh's work")
		return nil
	}
	if c := covered(); c != nil {
		t.Fatalf("a fresh Ask is covered: %+v", c)
	}
	o.now = o.now.Add(13 * time.Hour)
	if c := covered(); c == nil || c.ID != "santosh" {
		t.Fatalf("after 12 hours the Ask does not reach the fallback: %+v", c)
	}
	o.do(t, "abir", "GET", "/v1/workstreams/"+ws.ID, -1, nil, 200, &cur)
	if !strings.Contains(ruleDecisionTexts(cur.Decisions), "answers what waits unanswered for 12 hours") {
		t.Fatalf("the wait is not a decision: %s", ruleDecisionTexts(cur.Decisions))
	}
}

func ruleDecisionTexts(ds []api.WorkstreamDecision) string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Text)
	}
	return strings.Join(out, " | ")
}
