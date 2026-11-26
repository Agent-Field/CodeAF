package data

import (
	"encoding/json"
	"testing"
)

func TestIdentityMergeSplitRoundTrip(t *testing.T) {
	s, _ := openCRM(t)
	if err := s.BindCommonType(CommonPerson, "contact"); err != nil {
		t.Fatal(err)
	}
	left := mustCreate(t, s, person, "contact", map[string]any{"name": "Alice", "phone": "+14155550132"})[0].ID()
	right := mustCreate(t, s, person, "contact", map[string]any{"name": "Alice R", "email": "alice@example.org"})[0].ID()
	co := mustCreate(t, s, person, "company", map[string]any{"name": "Acme"})[0].ID()
	deal := mustCreate(t, s, person, "deal", map[string]any{"name": "Acme renewal", "company": co, "contacts": []any{right}})[0].ID()
	refQuery := func(query string) int {
		t.Helper()
		page, err := s.Query(person, "deal", query, "", 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Rows) > 0 && page.Rows[0].ID() != deal {
			t.Fatalf("query returned another deal: %+v", page.Rows)
		}
		return len(page.Rows)
	}
	key := ExternalKey{Binding: "crm", Object: "people", Key: "001", Generation: 1}
	if _, err := s.LinkIdentity(CommonPerson, key, right); err != nil {
		t.Fatal(err)
	}
	merge, err := s.MergeIdentity(person, left, right, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	row, err := s.Get(person, "contact", right, false)
	if err != nil || row.ID() != left || row["phone"] == nil || row["email"] != "alice@example.org" {
		t.Fatalf("alias read = %v, %v", row, err)
	}
	if id, ok, err := s.LookupIdentity(key); err != nil || !ok || id != left {
		t.Fatalf("merged crosswalk = %s, %v, %v", id, ok, err)
	}
	if refQuery("contacts:"+left) != 1 || refQuery("contacts:"+right) != 1 || refQuery("contacts.name:Alice") != 1 {
		t.Fatal("existing deal contact did not follow the merge")
	}
	if err := s.UnmergeIdentity(person, merge.ID, merge.Revision); err != nil {
		t.Fatal(err)
	}
	if refQuery("contacts:"+left) != 0 || refQuery("contacts:"+right) != 1 {
		t.Fatal("deal contact did not follow its original record after split")
	}
	for id, name := range map[string]string{left: "Alice", right: "Alice R"} {
		row, err := s.Get(person, "contact", id, false)
		if err != nil || row.ID() != id || row["name"] != name {
			t.Fatalf("restored %s: %v, %v", id, row, err)
		}
		history, err := s.History(person, "contact", id)
		if err != nil || len(history) < 3 || history[0].Op != OpIdentitySplit || history[1].Op != OpIdentityMerge {
			t.Fatalf("history of %s = %v, %v", id, history, err)
		}
	}
	if id, ok, err := s.LookupIdentity(key); err != nil || !ok || id != right {
		t.Fatalf("restored crosswalk = %s, %v, %v", id, ok, err)
	}
}

func TestIdentityMappedSourcesReviewAndProvenance(t *testing.T) {
	s, _ := openCRM(t)
	if err := s.RegisterApp(fixture(t, "identity-crm-app.json")); err != nil {
		t.Fatal(err)
	}
	if err := s.BindCommonType(CommonPerson, "contact"); err != nil {
		t.Fatal(err)
	}
	m := SourceMapping{Binding: "intake", Object: "people", Common: CommonPerson,
		Fields: []MappingField{
			{Source: "Full Name", Target: "name", Transforms: []MappingTransform{{Op: "trim"}}},
			{Source: "Work Email", Target: "email", Transforms: []MappingTransform{{Op: "email"}}, Clear: true, Class: "contact"},
			{Source: "Employer", Target: "company"},
			{Source: "Role", Target: "title"},
		}, Match: IdentityFields{Name: "name", Email: "email", Organization: "company"}}
	epochs, err := s.InstallSourceMapping(m, TypeEpochs{})
	if err != nil || epochs.Mapping != 1 {
		t.Fatalf("install rule: %v, %v", epochs, err)
	}
	co := mustCreate(t, s, person, "company", map[string]any{"name": "Acme"})[0].ID()
	input := func(key, name, email, job, event string, verified bool) MappedSourceObservation {
		return MappedSourceObservation{SourceObservation: SourceObservation{
			Key: ExternalKey{Binding: m.Binding, Object: m.Object, Key: key, Generation: 1}, Event: event, Revision: event, Epochs: epochs, NativeValues: true},
			Values: map[string]any{"Full Name": name, "Work Email": email, "Employer": co, "Role": job}, VerifiedWorkEmail: verified}
	}
	observe := func(o MappedSourceObservation) string {
		t.Helper()
		id, err := s.ObserveMappedSource(o)
		if err != nil || id == "" {
			t.Fatalf("observe %s: %s, %v", o.Key.Key, id, err)
		}
		return id
	}
	left := observe(input("1", " Alice Ruiz ", "ALICE@acme.example", "Buyer", "1", true))
	two := input("2", "Alice Ruiz", "alice@acme.example", "Manager", "1", true)
	if right := observe(two); right != left {
		t.Fatalf("verified key returned %s, want %s", right, left)
	}
	claimed, err := s.SourceState(two.Key)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, claim := range claimed.Claims {
		if claim.Path == "Work Email" {
			found = claim.Value == "alice@acme.example" && claim.Class == "contact" && claim.MappingVersion == 1 && claim.Revision == "1"
		}
	}
	if !found {
		t.Fatalf("lost source evidence: %+v", claimed)
	}
	if keys, err := s.ExternalKeys(left); err != nil || len(keys) != 2 {
		t.Fatalf("merged source keys: %+v, %v", keys, err)
	}
	if _, err := s.Update(person, "contact", []string{left}, map[string]any{"title": nil}, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	two.Event, two.Revision = "2", "2"
	observe(two)
	row, err := s.Get(person, "contact", left, false)
	if err != nil || row["title"] != nil || row["_meta"].(RowMeta).By["title"] != ByPerson {
		t.Fatalf("person clear lost: %v, %v", row, err)
	}

	// A similar name and employer have weaker evidence and prompt a review.
	right := observe(input("3", "Alice Ruis", "other@acme.example", "Manager", "1", false))
	if right == left {
		t.Fatal("ambiguous pair joined automatically")
	}
	list, err := s.IdentityCandidates(person)
	if err != nil || len(list) != 1 || list[0].Evidence.Decision != MatchReview {
		t.Fatalf("review: %+v, %v", list, err)
	}
	items, err := s.WorkItems(person, WorkFilter{Status: WorkOpen})
	if err != nil || len(items) != 1 || items[0].ID != list[0].ID || items[0].Title != "Are these the same person?" {
		t.Fatalf("work item: %+v, %v", items, err)
	}
	if scoped, err := s.WorkItems(Scope{App: "crm"}, WorkFilter{}); err != nil || len(scoped) != 0 {
		t.Fatalf("app saw owner review: %+v, %v", scoped, err)
	}
	if _, err := s.CompleteWorkItem(person, items[0].ID, map[string]any{"decision": "keep separate"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ScanIdentity(person, "contact", right, IdentityFields{}); err != nil {
		t.Fatal(err)
	}
	if list, err := s.IdentityCandidates(person); err != nil || len(list) != 0 {
		t.Fatalf("kept-separate pair returned: %+v, %v", list, err)
	}
	updated, err := s.InstallSourceMapping(m, epochs)
	if err != nil || updated.Mapping != 2 {
		t.Fatalf("replace mapping: %v, %v", updated, err)
	}
	_, err = s.ObserveMappedSource(input("4", "Bob Adams", "bob@acme.example", "Buyer", "1", false))
	wantCode(t, err, CodeStale)
	if _, ok, err := s.LookupIdentity(input("4", "", "", "", "", false).Key); err != nil || ok {
		t.Fatalf("stale mapping left a key: %v, %v", ok, err)
	}
	two.Values["Work Email"] = "a retried event must not be reinterpreted"
	if id := observe(two); id != left {
		t.Fatalf("retry changed the canonical ID: %s", id)
	}
}

func TestIdentityGoldenSet(t *testing.T) {
	var set []struct {
		Label       string
		Common      CommonTypeKey
		Left, Right MatchSubject
		Want        MatchDecision
		Same        bool
	}
	if err := json.Unmarshal(fixture(t, "identity-golden.json"), &set); err != nil {
		t.Fatal(err)
	}
	if len(set) != 50 {
		t.Fatalf("golden set has %d pairs, want 50", len(set))
	}
	var duplicatePairs, linked, trueLinks, flagged, trueFlags int
	for _, c := range set {
		got := ScoreIdentity(c.Common, c.Left, c.Right)
		if got.Decision != c.Want {
			t.Errorf("%s: got %+v, want %s", c.Label, got, c.Want)
		}
		if c.Same {
			duplicatePairs++
		}
		if got.Decision != MatchSeparate {
			flagged++
			if c.Same {
				trueFlags++
			}
		}
		if got.Decision == MatchLink {
			linked++
			if c.Same {
				trueLinks++
			}
		}
	}
	t.Logf("auto-link precision: %d/%d, recall: %d/%d; linked or reviewed precision: %d/%d, recall: %d/%d",
		trueLinks, linked, trueLinks, duplicatePairs, trueFlags, flagged, trueFlags, duplicatePairs)
}
