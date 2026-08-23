package tui3

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

type thresholdFake struct {
	*fakeAgent
	stand []standing.Item
}

func (f *thresholdFake) StandingHere() ([]standing.Item, []standing.Item) {
	return f.stand, nil
}

func TestTheConversationThresholdNamesStandingOrdersOrDrawsNothing(t *testing.T) {
	tests := []struct {
		name  string
		count int
		want  string
	}{
		{name: "singular", count: 1, want: "1 standing order here — /standing"},
		{name: "plural", count: 3, want: "3 standing orders here — /standing"},
		{name: "empty", count: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := &thresholdFake{fakeAgent: &fakeAgent{}}
			for range tt.count {
				agent.stand = append(agent.stand, standing.Item{})
			}
			a := newApp(context.Background(), Options{Agent: agent, Workspace: "/tmp/lab"})
			if tt.want == "" {
				for _, entry := range a.entries {
					if strings.Contains(entry.text, "standing order") {
						t.Fatalf("an empty threshold drew a standing-order line: %q", entry.text)
					}
				}
				return
			}
			found := -1
			for i := range a.entries {
				if a.entries[i].text != tt.want {
					continue
				}
				if found >= 0 {
					t.Fatalf("the threshold line was drawn more than once: %#v", a.entries)
				}
				found = i
			}
			if found < 0 {
				t.Fatalf("the threshold line is absent: %#v", a.entries)
			}
			// ONE ROW, IN THE NOTE'S OWN LANE — and THE PAYLOAD RULE inside it
			// (payload.go). This assertion used to be that the whole row was one dim
			// run, which was true and was the defect: the count is why the line is
			// drawn at all, and it read at exactly the weight of the noun after it.
			// So the count steps to ink, /standing wears the chip every door on this
			// surface wears, and the words between them keep the dim the lane is
			// written in.
			rows := a.renderEntry(found, &a.entries[found], 60)
			if len(rows) != 1 {
				t.Fatalf("the threshold line is not one row: %q", rows)
			}
			row := rows[0]
			if !strings.Contains(row, a.pal.ink(itoa(tt.count))) {
				t.Fatalf("the threshold's own count is not lifted out of its sentence: %q", row)
			}
			// Everything between the count and the door, the space in front of the
			// door included: one unbroken dim run, which is what says the prose did
			// not move.
			noun := strings.TrimSuffix(strings.TrimPrefix(tt.want, itoa(tt.count)), "/standing")
			if !strings.Contains(row, a.pal.dim(noun)) {
				t.Fatalf("the threshold's prose left the dim lane: %q", row)
			}
			if !strings.Contains(row, a.pal.chip("/standing")) {
				t.Fatalf("the door the threshold names is not chipped: %q", row)
			}
			// AND NOTHING ON SCREEN MOVED. The rule repaints runes and never adds one.
			if got := ansi.Strip(row); got != "· "+tt.want {
				t.Fatalf("the threshold line's words changed:\n\tgot  %q\n\twant %q",
					got, "· "+tt.want)
			}
		})
	}
}
