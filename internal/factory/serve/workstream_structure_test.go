package serve

import (
	"net/http"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// WORKSTREAMS V5 STRUCTURE, OVER THE ROUTES: santosh makes Pantry with the
// Platform team (santosh and abir) in it, then Meal planner inside Pantry.
//
//   - the child has Pantry's people, and a change to Pantry's people reaches
//     it in the same save; the child's own people are refused;
//   - nesting is one level, both ways round;
//   - only santosh, who looks after Pantry, changes its rules; abir reads
//     them;
//   - the child starts with a copy of Pantry's rules and changes its own;
//   - "start without asking" is the go-ahead policy the Factory follows.
func TestAChildHasItsParentsPeopleAndACopyOfItsRules(t *testing.T) {
	o := newWSOrg(t)
	pantry := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry"})
	if pantry.Steward == nil || pantry.Steward.ID != "santosh" || !pantry.MySteward {
		t.Fatalf("the maker looks after it: steward %+v mine %v", pantry.Steward, pantry.MySteward)
	}
	if pantry.Rules.DefaultOwner.Kind != factory.RulesOwnerPerson || pantry.Rules.DefaultOwner.ID != "santosh" || pantry.Rules.StartWithoutAskingUSD != 0 {
		t.Fatalf("default rules = %+v", pantry.Rules)
	}
	path := "/v1/workstreams/" + pantry.ID
	team := []factory.Share{{Kind: "team", ID: "tm_platform", Role: "edit"}}
	var shared api.WorkstreamShared
	o.do(t, "santosh", "PUT", path+"/shares", -1, api.WorkstreamSharesPut{Shares: &team}, 200, &shared)
	people := shared.Workstream.People
	if len(people) != 2 || people[0].ID != "santosh" || !people[0].Steward || people[1].Kind != "team" || people[1].Name != "Platform" || !slices.Equal(people[1].Members, []string{"abir", "santosh"}) || people[1].AddedAt == "" || people[1].AddedBy != "santosh" || people[0].AddedBy != "" {
		t.Fatalf("people = %+v", people)
	}

	// Rules: santosh may, abir (edit, but not the steward) may not.
	rules := shared.Workstream.Rules
	rules.StartWithoutAskingUSD = 5
	rules.Fallback = &factory.RulesPerson{ID: "abir"}
	rules.KeepsRunning.Factory = true
	var abirSees api.Workstream
	o.do(t, "abir", "GET", path, -1, nil, 200, &abirSees)
	if abirSees.MySteward {
		t.Fatal("abir does not look after Pantry")
	}
	o.do(t, "abir", "PATCH", path, abirSees.Rev, api.WorkstreamPatch{Rules: &rules}, http.StatusForbidden, nil)
	var after api.Workstream
	o.do(t, "santosh", "PATCH", path, abirSees.Rev, api.WorkstreamPatch{Rules: &rules}, 200, &after)
	if after.Rules.StartWithoutAskingUSD != 5 || after.Rules.Fallback == nil || after.Rules.Fallback.ID != "abir" || !after.Rules.KeepsRunning.Factory {
		t.Fatalf("rules after = %+v", after.Rules)
	}

	// A child: santosh makes Meal planner inside Pantry.
	meal := o.create(t, "santosh", api.WorkstreamNew{Name: "Meal planner", Parent: pantry.ID})
	if meal.Parent != pantry.ID || len(meal.People) != 2 || meal.People[1].ID != "tm_platform" {
		t.Fatalf("child = parent %q people %+v", meal.Parent, meal.People)
	}
	if meal.Rules.StartWithoutAskingUSD != 5 || !meal.Rules.KeepsRunning.Factory || meal.Rules.DefaultOwner.ID != "santosh" {
		t.Fatalf("the child starts with a copy of the parent's rules: %+v", meal.Rules)
	}
	var abirMeal api.Workstream
	o.do(t, "abir", "GET", "/v1/workstreams/"+meal.ID, -1, nil, 200, &abirMeal)

	// One level, both ways round.
	o.do(t, "santosh", "POST", "/v1/workstreams", -1, api.WorkstreamNew{Name: "Too deep", Parent: meal.ID}, 422, nil)
	parent := meal.ID
	o.do(t, "santosh", "PATCH", path, after.Rev, api.WorkstreamPatch{Parent: &parent}, 422, nil)

	// A child's people are its parent's.
	view := []factory.Share{{Kind: "person", ID: "mia", Role: "view"}}
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+meal.ID+"/shares", -1, api.WorkstreamSharesPut{Shares: &view}, 422, nil)
	// ... and the parent's change reaches the child: Growth comes in, so mia
	// reads Meal planner too.
	both := []factory.Share{{Kind: "team", ID: "tm_platform", Role: "edit"}, {Kind: "team", ID: "tm_growth", Role: "view"}}
	o.do(t, "santosh", "PUT", path+"/shares", -1, api.WorkstreamSharesPut{Shares: &both}, 200, nil)
	var miaMeal api.Workstream
	o.do(t, "mia", "GET", "/v1/workstreams/"+meal.ID, -1, nil, 200, &miaMeal)
	if miaMeal.MyRole != "view" {
		t.Fatalf("mia's role on the child = %q", miaMeal.MyRole)
	}

	// The child changes its own copy; the parent keeps its own.
	mine := meal.Rules
	mine.StartWithoutAskingUSD = 0
	o.do(t, "santosh", "PATCH", "/v1/workstreams/"+meal.ID, miaMeal.Rev, api.WorkstreamPatch{Rules: &mine}, 200, nil)
	var pantryNow api.Workstream
	o.do(t, "santosh", "GET", path, -1, nil, 200, &pantryNow)
	if pantryNow.Rules.StartWithoutAskingUSD != 5 {
		t.Fatalf("the parent's rules moved with the child's: %+v", pantryNow.Rules)
	}
}

// "START WITHOUT ASKING" IS THE GO-AHEAD POLICY THE FACTORY FOLLOWS: one
// allowance over the workstream's jobs at the rule's amount per job, written
// when the rule or the jobs change, and taken away at zero.
func TestStartWithoutAskingIsTheWorkstreamsGoAhead(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry"})
	path := "/v1/workstreams/" + ws.ID
	job := o.santoshJob(t, ws.ID)
	o.do(t, "santosh", "PUT", path+"/jobs/"+itoa(job.ID), -1, nil, 200, nil)
	var cur api.Workstream
	o.do(t, "santosh", "GET", path, -1, nil, 200, &cur)
	rules := cur.Rules
	rules.StartWithoutAskingUSD = 5
	o.do(t, "santosh", "PATCH", path, cur.Rev, api.WorkstreamPatch{Rules: &rules}, 200, &cur)
	p, ok, err := o.st.Policy(ws.ID)
	if err != nil || !ok || len(p.Allowances) != 1 {
		t.Fatalf("policy = %+v %v %v", p, ok, err)
	}
	a := p.Allowances[0]
	if a.ID != factory.RuleAllowanceID || a.PerJobUSD != 5 || a.CapUSD != 5 || !slices.Equal(a.Jobs, []int{job.ID}) || p.Owner != "santosh" {
		t.Fatalf("allowance = %+v owner %q", a, p.Owner)
	}
	second := o.santoshJob(t, ws.ID)
	o.do(t, "santosh", "PUT", path+"/jobs/"+itoa(second.ID), -1, nil, 200, &cur)
	p, _, _ = o.st.Policy(ws.ID)
	if a := p.Allowances[0]; !slices.Equal(a.Jobs, []int{job.ID, second.ID}) || a.CapUSD != 10 || a.PerJobUSD != 5 {
		t.Fatalf("a job that joins is covered at the same amount: %+v", a)
	}
	rules.StartWithoutAskingUSD = 0
	o.do(t, "santosh", "PATCH", path, cur.Rev, api.WorkstreamPatch{Rules: &rules}, 200, nil)
	p, _, _ = o.st.Policy(ws.ID)
	if len(p.Allowances) != 0 {
		t.Fatalf("zero is never start without asking: %+v", p.Allowances)
	}
}

// Folding a child back moves its work into the parent and removes it; the
// archive question names what the child could leave behind.
func TestAChildFoldsBackIntoAHeading(t *testing.T) {
	o := newWSOrg(t)
	pantry := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry"})
	meal := o.create(t, "santosh", api.WorkstreamNew{Name: "Meal planner", Parent: pantry.ID,
		Pages: []factory.WorkstreamPage{{Title: "Brief", BodyMD: "the plan"}}})
	job := o.santoshJob(t, meal.ID)
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+meal.ID+"/jobs/"+itoa(job.ID), -1, nil, 200, nil)

	var keep api.WorkstreamArchiveKeep
	o.do(t, "santosh", "GET", "/v1/workstreams/"+meal.ID+"/archive/keep", -1, nil, 200, &keep)
	if keep.Parent != pantry.ID || len(keep.Keep) != 1 || keep.Keep[0].Title != "Brief" {
		t.Fatalf("keep = %+v", keep)
	}
	o.do(t, "abir", "POST", "/v1/workstreams/"+meal.ID+"/fold", -1, nil, http.StatusNotFound, nil)
	var folded api.WorkstreamFolded
	o.do(t, "santosh", "POST", "/v1/workstreams/"+meal.ID+"/fold", -1, nil, 200, &folded)
	if folded.Heading != "Meal planner" || !slices.Contains(folded.Parent.Jobs, job.ID) {
		t.Fatalf("folded = %q jobs %v", folded.Heading, folded.Parent.Jobs)
	}
	o.do(t, "santosh", "GET", "/v1/workstreams/"+meal.ID, -1, nil, 404, nil)
	o.do(t, "santosh", "POST", "/v1/workstreams/"+pantry.ID+"/fold", -1, nil, 422, nil)
}

// Removing a person tells the tasks lane who left and who takes their work:
// the fallback, else the steward.
func TestRemovingAPersonHandsTheirWorkToTheRemover(t *testing.T) {
	o := newWSOrg(t)
	var got struct {
		ws      []string
		removed []string
		to      string
	}
	was := WorkstreamPeopleRemoved
	WorkstreamPeopleRemoved = func(_ *Handler, _ *http.Request, ws, removed []string, to string) {
		got.ws, got.removed, got.to = ws, removed, to
	}
	// The task list sets the hook for the whole package; put back what was there.
	t.Cleanup(func() { WorkstreamPeopleRemoved = was })
	pantry := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry"})
	meal := o.create(t, "santosh", api.WorkstreamNew{Name: "Meal planner", Parent: pantry.ID})
	path := "/v1/workstreams/" + pantry.ID
	in := []factory.Share{{Kind: "person", ID: "abir", Role: "edit"}, {Kind: "person", ID: "mia", Role: "edit"}}
	var shared api.WorkstreamShared
	o.do(t, "santosh", "PUT", path+"/shares", -1, api.WorkstreamSharesPut{Shares: &in}, 200, &shared)
	rules := shared.Workstream.Rules
	rules.Fallback = &factory.RulesPerson{ID: "mia"}
	o.do(t, "santosh", "PATCH", path, shared.Workstream.Rev, api.WorkstreamPatch{Rules: &rules}, 200, nil)
	out := []factory.Share{{Kind: "person", ID: "mia", Role: "edit"}}
	o.do(t, "santosh", "PUT", path+"/shares", -1, api.WorkstreamSharesPut{Shares: &out}, 200, nil)
	// The remover takes it (screen 16), even with a fallback set.
	if !slices.Equal(got.removed, []string{"abir"}) || got.to != "santosh" || !slices.Equal(got.ws, []string{pantry.ID, meal.ID}) {
		t.Fatalf("removed = %+v", got)
	}
}

// A change to the rules in words alone is a change of the rules: the "rules"
// part is published, so every open copy rereads them.
func TestASaidOnlyRulesChangeIsAChange(t *testing.T) {
	before := factory.WorkstreamRules{Said: []string{"No new dependencies without asking"}}
	same := factory.WorkstreamRules{Said: []string{"No new dependencies without asking"}}
	if !sameWorkstreamRules(before, same) {
		t.Fatal("the same rules in words read as a change")
	}
	for _, next := range [][]string{
		{"No new dependencies without asking", "Ship on Fridays only"},
		{"No dependencies at all"},
		nil,
	} {
		if sameWorkstreamRules(before, factory.WorkstreamRules{Said: next}) {
			t.Errorf("rules in words %q → %q read as unchanged", before.Said, next)
		}
	}
}
