package factory

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Unknown is omitted, known-empty persists as [], and both come back as they
// went. An old document without the key reads as unknown.
func TestSourceAssigneesPersistUnknownEmptyAndSet(t *testing.T) {
	for name, tc := range map[string]struct {
		in      []string
		wantKey bool
	}{"unknown": {nil, false}, "known empty": {[]string{}, true}, "single": {[]string{"a"}, true}, "multiple": {[]string{"a", "b"}, true}} {
		raw, err := json.Marshal(Item{Num: 1, SourceAssignees: tc.in})
		if err != nil {
			t.Fatal(err)
		}
		if has := strings.Contains(string(raw), "SourceAssignees"); has != tc.wantKey {
			t.Fatalf("%s: key present=%v in %s", name, has, raw)
		}
		var back Item
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatal(err)
		}
		if (back.SourceAssignees == nil) != (tc.in == nil) || !reflect.DeepEqual(back.SourceAssignees, tc.in) {
			t.Fatalf("%s: round trip %#v -> %#v", name, tc.in, back.SourceAssignees)
		}
	}
	var old Item
	if err := json.Unmarshal([]byte(`{"Num":3,"Assignee":"ann"}`), &old); err != nil || old.SourceAssignees != nil || old.Assignee != "ann" {
		t.Fatalf("old document %+v %v", old, err)
	}
}

// The fact is never an owner.
func TestSourceAssigneesAreNotAPerson(t *testing.T) {
	if p := (Item{SourceAssignees: []string{"a"}}).Person(); p != "" {
		t.Fatalf("Person() = %q", p)
	}
}
