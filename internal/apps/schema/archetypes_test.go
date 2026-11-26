package schema

import (
	"bytes"
	"testing"
)

func TestScreenArchetypesExpandIntoCheckedWorkspaces(t *testing.T) {
	tests := []struct {
		fixture, record, archetype string
		slots                      ScreenSlots
		parts                      []string
	}{
		{"gallery-kyc", "case", "queue-detail", ScreenSlots{State: "stage"}, []string{"RecordList", "Panel", "RecordPage"}},
		{"gallery-hiring", "candidate", "board-detail", ScreenSlots{State: "stage"}, []string{"RecordList", "Panel"}},
		{"gallery-support", "ticket", "inbox-thread", ScreenSlots{Message: "body"}, []string{"RecordList", "Panel", "Thread"}},
		{"gallery-hiring", "candidate", "agenda", ScreenSlots{Start: "interview"}, []string{"RecordList", "Panel"}},
		{"gallery-kyc", "case", "record", ScreenSlots{}, []string{"RecordPage", "Timeline"}},
		{"gallery-hiring", "candidate", "brief", ScreenSlots{Summary: "Today's interviews", Start: "interview",
			Metrics: []ScreenMetric{{Label: "Candidates", Op: "count"}}, Visual: &ScreenVisual{Kind: "bar", Title: "Candidates by stage", Group: "stage"},
			Changed: &ScreenCollection{Record: "candidate", Title: "What changed"}}, []string{"Text", "RecordList", "Panel", "Chart"}},
		{"gallery-line", "line", "dashboard", ScreenSlots{Metrics: []ScreenMetric{{Label: "Downtime", Op: "sum", Field: "minutes"}},
			Visual: &ScreenVisual{Kind: "line", Title: "Pressure by hour", Group: "reading", Value: "pressure"}}, []string{"Chart", "RecordList"}},
		{"gallery-line", "line", "canvas-visual", ScreenSlots{
			Visual: &ScreenVisual{Kind: "timeline", Title: "Shift plan", Content: "plan/shifts.json"}}, []string{"Timeline", "RecordList"}},
	}
	for _, tt := range tests {
		t.Run(tt.archetype, func(t *testing.T) {
			def := load(t, "testdata/"+tt.fixture)
			input := Recipe{Kind: "add-screen", ID: "new-screen", Title: "Review today", Record: tt.record, As: tt.archetype, Slots: &tt.slots}
			result, err := CompileRecipe(def, nil, input)
			if err != nil {
				t.Fatal(err)
			}
			screen, err := ParseScreen(result.Screen)
			if err != nil {
				t.Fatal(err)
			}
			for _, part := range tt.parts {
				if findRecipePart(screen.Body, part) == nil {
					t.Errorf("%s is missing from the expanded screen", part)
				}
			}
			if tt.archetype == "brief" && (len(screen.Views["metric-1"].Aggregate) != 1 || screen.Views["visual"].Type != tt.record) {
				t.Fatal("the brief must bind its count and chart to declared views")
			}
			again, err := CompileRecipe(def, nil, input)
			if err != nil || !bytes.Equal(again.Screen, result.Screen) {
				t.Fatalf("the expanded screen must have stable IDs: %v", err)
			}
		})
	}
}

func TestScreenArchetypeRejectsInvalidSlot(t *testing.T) {
	def := load(t, "testdata/gallery-hiring")
	_, err := CompileRecipe(def, nil, Recipe{Kind: "add-screen", ID: "pipeline", As: "board-detail", Record: "candidate", Slots: &ScreenSlots{State: "role"}})
	if err == nil {
		t.Fatal("a board must group by a state field, not by arbitrary text")
	}
}
