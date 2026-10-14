package builder

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
)

// Screens are written by separate calls, so each guesses the nav row; the
// row is derived from the written titles in plan order before Check.
func TestSyncNavDerivesItemsFromThePlan(t *testing.T) {
	plan := prompt.Plan{Screens: []prompt.Line{{ID: "today", Line: "Today · daily view"}, {ID: "deals", Line: "Deals · board"}, {ID: "people", Line: "People · list"}}}
	files := map[string][]byte{
		"screens/today.json":  []byte(`{"screen":"today","title":"Today","nav":{"part":"SectionTabs","props":{"items":["Home","Deals"]}}}`),
		"screens/deals.json":  []byte(`{"screen":"deals","title":"Pipeline","nav":{"part":"SectionTabs"}}`),
		"screens/people.json": []byte(`{"screen":"people","nav":{"part":"Row"}}`),
	}
	syncNav(files, plan)
	want := []any{"Today", "Pipeline", "People"}
	for _, id := range []string{"today", "deals"} {
		var s struct {
			Nav struct {
				Props struct{ Items []any } `json:"props"`
			} `json:"nav"`
		}
		if err := json.Unmarshal(files["screens/"+id+".json"], &s); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.Nav.Props.Items, want) {
			t.Errorf("%s nav = %v, want %v", id, s.Nav.Props.Items, want)
		}
	}
	if string(files["screens/people.json"]) != `{"screen":"people","nav":{"part":"Row"}}` {
		t.Error("a nav that is not SectionTabs is left for Check")
	}
}
