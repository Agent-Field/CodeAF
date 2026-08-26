package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func memoryPlaceFixture(now time.Time) store.MemoryShelves {
	lines := []store.Memory{
		{ID: "terse", Scope: store.MemoryScopeUser, Type: store.MemoryPreference, Status: store.MemoryActive, Title: "Terse answers", Text: "No preamble", UseCount: 14, UpdatedAt: now.Add(-3 * time.Hour)},
		{ID: "branch", Scope: store.MemoryScopeUser, Type: store.MemoryFact, Status: store.MemoryActive, Title: "Ship on master", UseCount: 6, MissCount: 2, UpdatedAt: now.Add(-24 * time.Hour)},
		{ID: "miss", Scope: store.MemoryScopeUser, Type: store.MemoryDecision, Status: store.MemoryActive, Title: "Try the narrow rail", MissCount: 2, UpdatedAt: now.Add(-48 * time.Hour)},
		{ID: "new", Scope: store.MemoryScopeUser, Type: store.MemoryCorrection, Status: store.MemoryActive, Title: "TUI3 is live", UpdatedAt: now.Add(-3 * time.Hour)},
		{ID: "old", Scope: store.MemoryScopeUser, Type: store.MemoryProjectState, Status: store.MemorySuperseded, Title: "TUI2 is live", UpdatedAt: now.Add(-72 * time.Hour)},
		{ID: "gone", Scope: store.MemoryScopeUser, Type: store.MemoryFact, Status: store.MemoryForgotten, Title: "Feature branches", UpdatedAt: now.Add(-96 * time.Hour)},
	}
	return store.MemoryShelves{
		Held: 8, LetGo: 1, Superseded: 1, Total: 10, Shown: 10,
		Shelves: []store.MemoryShelf{
			{Scope: store.MemoryScopeProject, Label: "wrong project label", Held: 2, ByType: map[string]int{store.MemoryFact: 2}, Memories: []store.Memory{
				{ID: "tests", Scope: store.MemoryScopeProject, Type: store.MemoryFact, Status: store.MemoryActive, Title: "Tests live beside files", UpdatedAt: now.Add(-2 * time.Hour)},
				{ID: "router", Scope: store.MemoryScopeProject, Type: store.MemoryFact, Status: store.MemoryActive, Title: "The rail owns the cursor", UpdatedAt: now.Add(-5 * time.Hour)},
			}},
			{Scope: store.MemoryScopeUser, Held: 4, LetGo: 1, Superseded: 1, ByType: map[string]int{
				store.MemoryFact: 2, store.MemoryPreference: 1, store.MemoryDecision: 1,
				store.MemoryCorrection: 1, store.MemoryProjectState: 1,
			}, Memories: lines},
			{Scope: store.MemoryScopeEnv, Held: 2, ByType: map[string]int{store.MemoryProjectState: 2}, Memories: []store.Memory{
				{ID: "go", Scope: store.MemoryScopeEnv, Type: store.MemoryProjectState, Status: store.MemoryActive, Title: "Go is on PATH", UpdatedAt: now.Add(-time.Hour)},
				{ID: "shell", Scope: store.MemoryScopeEnv, Type: store.MemoryProjectState, Status: store.MemoryActive, Title: "The shell is zsh", UpdatedAt: now.Add(-6 * time.Hour)},
			}},
		},
	}
}

func memoryPlaceText(r memoryReading, width int) string {
	return strings.Join(r.rows(width, newPalette(tokens.NoColor, false)), "\n")
}

func TestTheMemoryPlaceTeachesUntilThereIsEnoughToRead(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	small := store.MemoryShelves{Held: 4, LetGo: 1, Total: 5, Shelves: []store.MemoryShelf{{
		Scope: store.MemoryScopeUser, Held: 4, LetGo: 1, ByType: map[string]int{store.MemoryFact: 5},
	}}}
	text := memoryPlaceText(readMemory(small, nil, "", now), 200)
	for _, sentence := range memoryTeaching {
		if !strings.Contains(text, sentence) {
			t.Fatalf("the small place omitted %q:\n%s", sentence, text)
		}
	}
	if !strings.Contains(text, "4 held · 1 let go · nothing here is a setting, all of it is editable") {
		t.Fatalf("the small place omitted its footer:\n%s", text)
	}
	large := memoryPlaceText(readMemory(memoryPlaceFixture(now), nil, "", now), 200)
	if strings.Contains(large, memoryTeaching[0]) || !strings.Contains(large, "8 held · 3 shelves · 2 let go") || !strings.Contains(large, "type to filter") {
		t.Fatalf("the large place did not replace teaching with its header:\n%s", large)
	}
}

func TestTheMemoryLegendUsesTheStoresFiveKinds(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	text := memoryPlaceText(readMemory(memoryPlaceFixture(now), nil, "", now), 200)
	// THE LEGEND SPELLS THE FIFTH KIND AS A WORD AND NOT AS A COLUMN NAME. The
	// store's constant is `project_state`; a person reads `project state`, which
	// is the no-machinery-vocabulary law applied to the one kind that has an
	// underscore in it ([memoryTypeWord]).
	for _, want := range []string{"fact 4", "preference 1", "decision 1", "correction 1", "project state 3"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the legend omitted %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "project_state") {
		t.Fatalf("the legend drew the store's column name at a person:\n%s", text)
	}
	for _, invented := range []string{"quirk", "lesson", "playbook", "trait"} {
		if strings.Contains(text, invented) {
			t.Fatalf("the legend invented the kind %q:\n%s", invented, text)
		}
	}
}

func TestMemoryShelvesAreBiggestFirstAndUseOneDisclosureGrammar(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	text := memoryPlaceText(readMemory(memoryPlaceFixture(now), map[string]bool{store.MemoryScopeUser: true}, "", now), 200)
	user := strings.Index(text, tokens.GlyphExpanded+" you · 6")
	project := strings.Index(text, tokens.GlyphCollapsed+" this project · 2")
	env := strings.Index(text, tokens.GlyphCollapsed+" this machine · 2")
	if user < 0 || project < user || env < project {
		t.Fatalf("the shelves are not biggest-first with their settled names:\n%s", text)
	}
	if !strings.Contains(text, tokens.GlyphCollapsed+" 3 more, on this shelf") {
		t.Fatalf("the open shelf omitted its exact fold:\n%s", text)
	}
}

func TestMemoryHelpWordsSayWhatTheCountersKnow(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		memory store.Memory
		want   string
	}{
		{store.Memory{Status: store.MemoryActive, UseCount: 14}, "helped 14 times"},
		{store.Memory{Status: store.MemoryActive, UseCount: 6, MissCount: 2}, "helped 6 · bore on 2"},
		{store.Memory{Status: store.MemoryActive, MissCount: 2}, "bore on 2"},
		{store.Memory{Status: store.MemoryActive, UpdatedAt: now.Add(-3 * time.Hour)}, "new, learned 3h"},
		{store.Memory{Status: store.MemoryForgotten}, "let go"},
		{store.Memory{Status: store.MemorySuperseded}, "let go"},
	}
	for _, test := range tests {
		if got := memoryHelp(test.memory, now); got != test.want {
			t.Errorf("memoryHelp(%+v) = %q, want %q", test.memory, got, test.want)
		}
	}
}

func TestTypingNarrowsMemoryShelvesAndLinesByWords(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	r := readMemory(memoryPlaceFixture(now), map[string]bool{store.MemoryScopeProject: true}, "tests files", now)
	text := memoryPlaceText(r, 120)
	if !strings.Contains(text, "Tests live beside files") || strings.Contains(text, "Terse answers") || strings.Contains(text, "this machine") {
		t.Fatalf("the two-word filter did not narrow both shelves and lines:\n%s", text)
	}
}

func TestAFilteredMemoryWithNoMatchesDrawsOnlyItsNoMatchLine(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	text := memoryPlaceText(readMemory(memoryPlaceFixture(now), nil, "purple aardvark", now), 120)
	if text != `nothing on a shelf says "purple aardvark"` || strings.Contains(text, "shelves") {
		t.Fatalf("empty filtered memory drew %q", text)
	}
}

func TestMemoryStopsAndVerbsLandOnlyOnShelvesAndLines(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	r := readMemory(memoryPlaceFixture(now), map[string]bool{store.MemoryScopeUser: true}, "", now)
	var shelfAt, lineAt = -1, -1
	for i := range r.lines {
		stop, ok := r.at(i)
		if !ok {
			continue
		}
		if stop.line == nil && shelfAt < 0 {
			shelfAt = i
		}
		if stop.line != nil && lineAt < 0 {
			lineAt = i
		}
	}
	if shelfAt < 0 || lineAt < 0 {
		t.Fatalf("the reading has no shelf or line stop: %+v", r.lines)
	}
	if got := r.verbs(shelfAt); len(got) != 1 || got[0].key != '\r' || got[0].word != "enter open" {
		t.Fatalf("shelf verbs = %+v", got)
	}
	if got := r.verbs(lineAt); len(got) != 2 || got[0].word != "e fix the wording" || got[1].word != "f forget it" {
		t.Fatalf("line verbs = %+v", got)
	}
	if _, ok := r.at(0); ok || r.verbs(0) != nil {
		t.Fatal("teaching/header lines became cursor stops")
	}
}

func TestEveryMemoryRowFitsItsCellWidthAtEveryTier(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	r := readMemory(memoryPlaceFixture(now), map[string]bool{store.MemoryScopeUser: true}, "", now)
	for _, width := range []int{60, 80, 120, 200} {
		for i, row := range r.rows(width, newPalette(tokens.TrueColor, false)) {
			if got := ansi.StringWidth(row); got > width {
				t.Errorf("width %d line %d measures %d cells: %q", width, i, got, row)
			}
		}
	}
	narrow := memoryPlaceText(r, 60)
	if strings.Contains(narrow, "helped") || !strings.Contains(narrow, store.MemoryPreference) {
		t.Fatalf("the narrow reading did not drop help before type:\n%s", narrow)
	}
}

func TestMemoryFoldsNameTheirExactHiddenCounts(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	snapshot := memoryPlaceFixture(now)
	for i := 0; i < 4; i++ {
		snapshot.Shelves = append(snapshot.Shelves, store.MemoryShelf{Scope: "later" + groupedInt(i), Label: "later " + groupedInt(i), Held: 1, ByType: map[string]int{store.MemoryFact: 1}})
	}
	text := memoryPlaceText(readMemory(snapshot, map[string]bool{store.MemoryScopeUser: true}, "", now), 120)
	if !strings.Contains(text, tokens.GlyphCollapsed+" 3 more, on this shelf") || !strings.Contains(text, tokens.GlyphCollapsed+" 2 more, shelves") {
		t.Fatalf("the folds did not name their exact hidden counts:\n%s", text)
	}
}

func TestMemoryCountsObeyTheEmptinessLaw(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	snapshot := memoryPlaceFixture(now)
	snapshot.LetGo, snapshot.Superseded = 0, 0
	text := memoryPlaceText(readMemory(snapshot, nil, "", now), 120)
	if strings.Contains(text, "0 let go") {
		t.Fatalf("zero was drawn as a fact:\n%s", text)
	}
}

// A MACHINE THAT HAS REMEMBERED NOTHING MEETS THE TEACHING AND NOTHING ELSE.
//
// There is no second empty state to draw: memory switched off never reaches a
// body, because the door refuses to open the place and says [memoryOffNote] on
// the transcript instead. So an empty reading is three sentences, one footer,
// and no heading over an absence.
func TestAnEmptyMemoryPlaceIsTheTeachingAndNoFurniture(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	text := memoryPlaceText(readMemory(store.MemoryShelves{}, nil, "", now), 120)
	for _, sentence := range memoryTeaching {
		if !strings.Contains(text, sentence) {
			t.Fatalf("the empty place did not teach %q:\n%s", sentence, text)
		}
	}
	for _, furniture := range []string{"shelves · biggest first", memoryOffNote} {
		if strings.Contains(text, furniture) {
			t.Fatalf("the empty place drew %q:\n%s", furniture, text)
		}
	}
}
