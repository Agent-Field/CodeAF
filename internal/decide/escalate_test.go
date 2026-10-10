package decide

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// A corrupt graph must terminate even though the store normally rejects cycles.
type escalationFixture struct {
	places      map[string]placegraph.Place
	memberships []placegraph.Membership
	settings    map[string]placegraph.Decide
}

func (g escalationFixture) Place(id string) (placegraph.Place, bool) {
	p, ok := g.places[id]
	return p, ok
}
func (g escalationFixture) PlacesOf(chat string) []placegraph.Membership {
	var out []placegraph.Membership
	for _, m := range g.memberships {
		if m.ChatID == chat {
			out = append(out, m)
		}
	}
	return out
}
func (g escalationFixture) EffectiveDecide(id string) (placegraph.Decide, string) {
	if d, ok := g.settings[id]; ok {
		return d, id
	}
	return placegraph.DefaultDecide(), ""
}
func TestEscalateGraph(t *testing.T) {
	deciding := Assessment{Mode: ModeDeciding, Percent: 90}
	learning := Assessment{Mode: ModeLearning, Percent: 100}
	for _, tt := range []struct {
		name        string
		parents     map[string][]string
		roots       []string
		assessments map[string]Assessment
		settings    map[string]placegraph.Decide
		archived    string
		want        string
		visits      []string
	}{
		{name: "chain learning then below threshold", parents: map[string][]string{"A": {"B"}, "B": {"C"}, "C": nil}, roots: []string{"A"}, assessments: map[string]Assessment{"A": learning, "B": {Mode: ModeDeciding, Percent: 89}, "C": deciding}, want: "C", visits: []string{"A", "B", "C"}},
		{name: "threshold equality stops lazily", parents: map[string][]string{"A": {"B"}, "B": nil}, roots: []string{"A"}, assessments: map[string]Assessment{"A": deciding, "B": deciding}, want: "A", visits: []string{"A"}},
		{name: "first parent wins tie", parents: map[string][]string{"A": {"B", "C"}, "B": nil, "C": nil}, roots: []string{"A"}, assessments: map[string]Assessment{"B": deciding, "C": deciding}, want: "B", visits: []string{"A", "B"}},
		{name: "nearer second parent precedes grandparent", parents: map[string][]string{"A": {"B", "C"}, "B": {"D"}, "C": nil, "D": nil}, roots: []string{"A"}, assessments: map[string]Assessment{"C": deciding, "D": deciding}, want: "C", visits: []string{"A", "B", "C"}},
		{name: "diamond shared parent once", parents: map[string][]string{"A": {"B", "C"}, "B": {"D"}, "C": {"D"}, "D": nil}, roots: []string{"A"}, visits: []string{"A", "B", "C", "D"}},
		{name: "two places start at nearest common ancestor", parents: map[string][]string{"A": {"C"}, "B": {"C"}, "C": {"D"}, "D": nil}, roots: []string{"A", "B"}, assessments: map[string]Assessment{"A": deciding, "B": deciding, "C": deciding, "D": deciding}, want: "C", visits: []string{"C"}},
		{name: "common ancestor itself escalates", parents: map[string][]string{"A": {"C"}, "B": {"C"}, "C": {"D"}, "D": nil}, roots: []string{"A", "B"}, assessments: map[string]Assessment{"C": learning, "D": deciding}, want: "D", visits: []string{"C", "D"}},
		{name: "common ancestor tie follows first parent", parents: map[string][]string{"A": {"D", "C"}, "B": {"C", "D"}, "C": nil, "D": nil}, roots: []string{"A", "B"}, assessments: map[string]Assessment{"C": deciding, "D": deciding}, want: "D", visits: []string{"D"}},
		{name: "unrelated places ask person", parents: map[string][]string{"A": nil, "B": nil}, roots: []string{"A", "B"}, assessments: map[string]Assessment{"A": deciding, "B": deciding}},
		{name: "cycle safe", parents: map[string][]string{"A": {"B"}, "B": {"A"}}, roots: []string{"A"}, visits: []string{"A", "B"}},
		{name: "third hop allowed fourth refused", parents: map[string][]string{"A": {"B"}, "B": {"C"}, "C": {"D"}, "D": {"E"}, "E": nil}, roots: []string{"A"}, assessments: map[string]Assessment{"E": deciding}, visits: []string{"A", "B", "C", "D"}},
		{name: "third hop decides", parents: map[string][]string{"A": {"B"}, "B": {"C"}, "C": {"D"}, "D": nil}, roots: []string{"A"}, assessments: map[string]Assessment{"D": deciding}, want: "D", visits: []string{"A", "B", "C", "D"}},
		{name: "common ancestor does not reset cap", parents: map[string][]string{"A": {"C"}, "B": {"C"}, "C": {"D"}, "D": {"E"}, "E": {"F"}, "F": nil}, roots: []string{"A", "B"}, assessments: map[string]Assessment{"F": deciding}, visits: []string{"C", "D", "E"}},
		{name: "unplaced asks person", parents: map[string][]string{"A": nil}, assessments: map[string]Assessment{"A": deciding}},
		{name: "unknown membership asks person", roots: []string{"missing"}},
		{name: "archived membership asks person", parents: map[string][]string{"A": nil}, roots: []string{"A"}, archived: "A"},
		{name: "archived ancestor skipped", parents: map[string][]string{"A": {"B"}, "B": {"C"}, "C": nil}, roots: []string{"A"}, archived: "B", assessments: map[string]Assessment{"B": deciding, "C": deciding}, want: "C", visits: []string{"A", "C"}},
		{name: "always ask blocks automatic parents", parents: map[string][]string{"A": {"B"}, "B": nil}, roots: []string{"A"}, settings: map[string]placegraph.Decide{"A": {AlwaysAsk: true, Threshold: 90}}, assessments: map[string]Assessment{"B": deciding}},
		{name: "ask mode blocks automatic parents", parents: map[string][]string{"A": {"B"}, "B": nil}, roots: []string{"A"}, assessments: map[string]Assessment{"A": {Mode: ModeAsk, Percent: 100}, "B": deciding}, visits: []string{"A"}},
		{name: "invalid mode and confidence cannot decide", parents: map[string][]string{"A": {"B"}, "B": {"C"}, "C": nil}, roots: []string{"A"}, assessments: map[string]Assessment{"A": {Mode: "invalid", Percent: 100}, "B": {Mode: ModeDeciding, Percent: 101}}, visits: []string{"A", "B", "C"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := escalationFixture{places: map[string]placegraph.Place{}, settings: tt.settings}
			for id, parents := range tt.parents {
				g.places[id] = placegraph.Place{ID: id, Parents: parents, Archived: id == tt.archived}
			}
			for _, id := range tt.roots {
				g.memberships = append(g.memberships, placegraph.Membership{ChatID: "chat", PlaceID: id})
			}
			var visits []string
			got, err := Escalate(g, "chat", func(id string) (Assessment, error) { visits = append(visits, id); return tt.assessments[id], nil })
			if err != nil || got != tt.want || !reflect.DeepEqual(visits, tt.visits) {
				t.Fatalf("owner=%q err=%v visits=%v; want owner=%q visits=%v", got, err, visits, tt.want, tt.visits)
			}
		})
	}
}

func TestEscalateUsesSnapshotSettingsAndLedgerMode(t *testing.T) {
	dir := t.TempDir()
	graph, err := placegraph.Open(placegraph.Options{Path: filepath.Join(dir, "places.json"), Now: func() time.Time { return t0 }})
	if err != nil {
		t.Fatal(err)
	}
	parent, _, err := graph.CreatePlace(placegraph.NewPlace{Name: "Parent"})
	if err != nil {
		t.Fatal(err)
	}
	child, _, err := graph.CreatePlace(placegraph.NewPlace{Name: "Child", Parents: []string{parent.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = graph.SetDecide(parent.ID, placegraph.Decide{Threshold: 95}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = graph.AddChat("chat", child.ID, placegraph.AddedByYou); err != nil {
		t.Fatal(err)
	}
	ledger, err := Open(dir, parent.ID, func() time.Time { return t0 })
	if err != nil {
		t.Fatal(err)
	}
	if err = ledger.SetMode("permission", ModeDeciding); err != nil {
		t.Fatal(err)
	}
	snapshot, err := graph.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, percent := range []int{94, 95} {
		got, err := Escalate(snapshot, "chat", func(id string) (Assessment, error) {
			if id == child.ID {
				return Assessment{Mode: ModeLearning, Percent: 100}, nil
			}
			state, err := ledger.Mode("permission")
			return Assessment{Mode: state.Mode, Percent: percent}, err
		})
		want := ""
		if percent == 95 {
			want = parent.ID
		}
		if err != nil || got != want {
			t.Fatalf("percent=%d owner=%q err=%v want=%q", percent, got, err, want)
		}
	}
}

func TestEscalateAssessmentErrorStopsRouting(t *testing.T) {
	sentinel := errors.New("evidence unavailable")
	g := escalationFixture{places: map[string]placegraph.Place{"A": {ID: "A", Parents: []string{"B"}}, "B": {ID: "B"}}, memberships: []placegraph.Membership{{ChatID: "chat", PlaceID: "A"}}}
	calls := 0
	got, err := Escalate(g, "chat", func(string) (Assessment, error) { calls++; return Assessment{}, sentinel })
	if got != "" || !errors.Is(err, sentinel) || calls != 1 {
		t.Fatalf("owner=%q err=%v calls=%d", got, err, calls)
	}
}
