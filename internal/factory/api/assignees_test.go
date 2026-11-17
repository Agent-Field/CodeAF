package api

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// The wire keeps absent (unknown) apart from [] (known none), round-trips
// the set into the item, and leaves Job.Assignee as the execution owner.
func TestWireAssigneesKeepUnknownEmptyAndSet(t *testing.T) {
	for name, in := range map[string][]string{"unknown": nil, "empty": {}, "single": {"a"}, "multiple": {"a", "b"}} {
		it := factory.Item{ID: 1, Num: 1, Kind: factory.KindIssue, SourceAssignees: in}
		j := FromItem(it)
		if j.Assignee != "" || j.AssignedTo != "" {
			t.Fatalf("%s: the fact became an owner: %q", name, j.Assignee)
		}
		raw, err := json.Marshal(j)
		if err != nil {
			t.Fatal(err)
		}
		if has := strings.Contains(string(raw), `"assignees"`); has != (in != nil) {
			t.Fatalf("%s: assignees key present=%v in %s", name, has, raw)
		}
		var back Job
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatal(err)
		}
		got := ToItem(back).SourceAssignees
		if (got == nil) != (in == nil) || !reflect.DeepEqual(got, in) {
			t.Fatalf("%s: round trip %#v -> %#v", name, in, got)
		}
	}
}

// A job from before the field decodes as unknown.
func TestWireOldJobHasNoAssignees(t *testing.T) {
	var j Job
	if err := json.Unmarshal([]byte(`{"id":"4","desk":{"assignee":"ann"}}`), &j); err != nil {
		t.Fatal(err)
	}
	if it := ToItem(j); it.SourceAssignees != nil || it.Assignee != "ann" {
		t.Fatalf("%+v", it)
	}
}
