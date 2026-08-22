package tui3

import (
	"context"
	"strings"
	"testing"

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
			rows := a.renderEntry(found, &a.entries[found], 60)
			if len(rows) != 1 || rows[0] != a.pal.dim("· "+tt.want) {
				t.Fatalf("the threshold line is not one dim row: %q", rows)
			}
		})
	}
}
