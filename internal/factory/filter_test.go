package factory

import "testing"

func TestMineMatchesViewerPersonOrAuthor(t *testing.T) {
	for _, tc := range []struct {
		name       string
		item       Item
		alice, bob bool
	}{
		{"author", Item{Author: " @ALICE "}, true, false},
		{"assignee", Item{Assignee: " @BOB "}, false, true},
		{"taker", Item{Taker: "Alice", Starter: "bob"}, true, false},
		{"starter", Item{Starter: "bob"}, false, true},
		{"assignment wins", Item{Assignee: "alice", Taker: "bob", Starter: "bob"}, true, false},
		{"author remains mine", Item{Assignee: "alice", Author: "bob"}, true, true},
		{"blank assignment", Item{Assignee: " @ ", Taker: "bob", Starter: "alice"}, false, true},
		{"unrelated person", Item{Assignee: "carol", Taker: "alice", Starter: "bob"}, false, false},
		{"old hard-coded author", Item{Author: "santosh"}, false, false},
		{"no person", Item{}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, viewer := range []struct {
				handle string
				want   bool
			}{
				{" @ALICE ", tc.alice}, {"bob", tc.bob}, {"", false}, {" @ ", false},
			} {
				if got := Match("MiNe", viewer.handle)(tc.item); got != viewer.want {
					t.Errorf("mine for %q = %v, want %v", viewer.handle, got, viewer.want)
				}
			}
		})
	}
}

func TestMineComposesWithOtherFilterWords(t *testing.T) {
	item := Item{Author: "alice", Triage: Triage{Type: "bug"}}
	if !Match("mine bugs", "alice")(item) || Match("mine prs", "alice")(item) || Match("mine bugs", "bob")(item) {
		t.Fatal("mine must satisfy every query term for the current viewer")
	}
	if !Match("", "")(item) || !Match("bugs")(item) || Match("mine")(item) {
		t.Fatal("unknown identity must not hide items from other filters")
	}
}

func TestThinIsTheReadsOwnClarityScore(t *testing.T) {
	scored := func(clarity int) Item {
		return Item{Triage: Triage{Rubric: &Rubric{Clarity: Factor{Score: clarity}, From: RubricFromRead}}}
	}
	cases := []struct {
		name string
		it   Item
		want bool
	}{
		{"clear", scored(0), false},
		{"partly clear", scored(1), false},
		{"unclear", scored(FactorMost), true},
		{"an older read's readiness, high", Item{Triage: Triage{Readiness: 80}}, false},
		{"an older read's readiness, low", Item{Triage: Triage{Readiness: 20}}, true},
		{"no score at all", Item{}, true},
	}
	for _, c := range cases {
		if got := Match("thin")(c.it); got != c.want {
			t.Errorf("%s: Match(thin) = %v, want %v", c.name, got, c.want)
		}
		if got := Fits(Stage{When: "thin"}, c.it); got != c.want {
			t.Errorf("%s: Fits(when thin) = %v, want %v", c.name, got, c.want)
		}
	}
}
