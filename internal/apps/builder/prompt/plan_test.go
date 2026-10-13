package prompt

import (
	"reflect"
	"strings"
	"testing"
)

func samplePlan() Plan {
	return Plan{
		Ask:        "a crm",
		Data:       []Line{{"deal", "Deal · name, stage"}, {"company", "Company · name"}},
		Automation: []Line{{"follow-up", "Weekdays 9:00 · draft"}},
		Screens:    []Line{{"deals", "Deals · list"}},
		Agent:      AgentLine{"CRM agent · drafts"},
		Needs:      "computed",
	}
}

func TestApplyPlanEdits(t *testing.T) {
	tests := []struct {
		name  string
		edits []Edit
		check func(Plan) bool
	}{
		{"set keeps id", []Edit{{OpSet, GroupData, "deal", "Deal · name, stage incl. Negotiation"}},
			func(p Plan) bool { return p.Data[0].ID == "deal" && strings.Contains(p.Data[0].Line, "Negotiation") }},
		{"add appends", []Edit{{OpAdd, GroupScreens, "contacts", "Contacts · list"}},
			func(p Plan) bool { return len(p.Screens) == 2 && p.Screens[1].ID == "contacts" }},
		{"remove", []Edit{{OpRemove, GroupData, "company", ""}},
			func(p Plan) bool { return len(p.Data) == 1 && p.Data[0].ID == "deal" }},
		{"agent set", []Edit{{OpSet, GroupAgent, "", "CRM agent · answers questions"}},
			func(p Plan) bool { return p.Agent.Line == "CRM agent · answers questions" }},
		{"in order", []Edit{{OpAdd, GroupAutomation, "digest", "Mondays · digest"}, {OpSet, GroupAutomation, "digest", "Fridays · digest"}},
			func(p Plan) bool { return p.Automation[1].Line == "Fridays · digest" }},
		{"no edits", nil, func(p Plan) bool { return reflect.DeepEqual(p, samplePlan()) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyPlanEdits(samplePlan(), tt.edits)
			if err != nil || !tt.check(got) {
				t.Fatalf("got %+v err %v", got, err)
			}
		})
	}
}

func TestApplyPlanEditsRejectsAndLeavesPlanUntouched(t *testing.T) {
	tests := []struct {
		name string
		edit Edit
		want string
	}{
		{"unknown group", Edit{OpSet, "needs", "x", "y"}, "unknown group"},
		{"unknown op", Edit{"swap", GroupData, "deal", "y"}, "unknown op"},
		{"set missing id", Edit{OpSet, GroupData, "nope", "y"}, "no data line"},
		{"set empty", Edit{OpSet, GroupData, "deal", "  "}, "empty"},
		{"add duplicate", Edit{OpAdd, GroupData, "deal", "y"}, "already exists"},
		{"add bad id", Edit{OpAdd, GroupData, "Bad Id", "y"}, "lowercase"},
		{"add empty line", Edit{OpAdd, GroupData, "x", ""}, "empty"},
		{"remove missing", Edit{OpRemove, GroupScreens, "nope", ""}, "no screens line"},
		{"agent add", Edit{OpAdd, GroupAgent, "", "y"}, "only be set"},
		{"agent empty", Edit{OpSet, GroupAgent, "", ""}, "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := samplePlan()
			good := Edit{OpSet, GroupData, "deal", "changed first"}
			got, err := ApplyPlanEdits(orig, []Edit{good, tt.edit})
			if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), "edit 2") {
				t.Fatalf("err = %v, want edit 2 mentioning %q", err, tt.want)
			}
			if !reflect.DeepEqual(got, samplePlan()) || !reflect.DeepEqual(orig, samplePlan()) {
				t.Errorf("plan changed despite error: %+v", got)
			}
		})
	}
}

func TestApplyPlanEditsDoesNotAliasTheInput(t *testing.T) {
	orig := samplePlan()
	if _, err := ApplyPlanEdits(orig, []Edit{{OpSet, GroupData, "deal", "x"}, {OpRemove, GroupData, "company", ""}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(orig, samplePlan()) {
		t.Errorf("input mutated: %+v", orig)
	}
}

func TestParsePlan(t *testing.T) {
	ex, _ := loadExample()
	tests := []struct{ name, out, want string }{
		{"ok fenced", "```json\n" + ex.Plan + "\n```", ""},
		{"ok with prose", "Plan:\n" + ex.Plan, ""},
		{"no object", "none", "no JSON object"},
		{"no screens", `{"data":[{"id":"a","line":"A"}],"agent":{"line":"x"}}`, "at least one"},
		{"no agent", `{"data":[{"id":"a","line":"A"}],"screens":[{"id":"s","line":"S"}]}`, "agent line"},
		{"dup id", `{"data":[{"id":"a","line":"A"},{"id":"a","line":"B"}],"screens":[{"id":"s","line":"S"}],"agent":{"line":"x"}}`, "twice"},
		{"bad id", `{"data":[{"id":"A b","line":"A"}],"screens":[{"id":"s","line":"S"}],"agent":{"line":"x"}}`, "lowercase"},
		{"empty line", `{"data":[{"id":"a","line":""}],"screens":[{"id":"s","line":"S"}],"agent":{"line":"x"}}`, "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePlan(tt.out)
			if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}
