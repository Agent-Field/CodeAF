package settings

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// 8.2.19's headline: ANY printable character starts a global fuzzy search
// across ALL tabs. Not a slash, not ctrl+f, and not "search within this group".
func TestPrintableCharacterSearchesEveryTab(t *testing.T) {
	s := newSheet(t)
	s.tab = 0 // models
	s.reselectFresh()

	s.typeText("attrib")

	if s.Query() != "attrib" {
		t.Fatalf("query = %q, want the letters that were typed", s.Query())
	}
	key, ok := s.Selected()
	if !ok || key != config.KeyAttribution {
		t.Fatalf("selected %q, want the sharing tab's %q found from the models tab", key, config.KeyAttribution)
	}
	if s.Tab() != "" {
		t.Fatalf("a running search selects no single tab, got %q", s.Tab())
	}
}

// The results are navigation: the tab bar becomes a breadcrumb naming which
// groups they came from, and every row still says which group it is in.
func TestSearchTurnsTheTabBarIntoAFilterBreadcrumb(t *testing.T) {
	s := newSheet(t)
	s.typeText("budget")

	frame := strings.Split(s.Render(80, 20), "\n")
	if !strings.Contains(frame[0], "budget") {
		t.Fatalf("header does not carry the query: %q", frame[0])
	}
	if !strings.Contains(frame[1], config.CategoryMoney) {
		t.Fatalf("breadcrumb %q does not name the group the results came from", frame[1])
	}
	if strings.Contains(frame[1], config.CategoryModels+"  ") {
		t.Fatalf("breadcrumb %q is still rendering the unfiltered tab bar", frame[1])
	}
	body := strings.Join(frame[2:], "\n")
	if !strings.Contains(body, config.CategoryMoney) {
		t.Fatalf("a result must name its group so it reads as navigation:\n%s", body)
	}
}

// A row whose NAME matches outranks a row that only matches in its hint —
// otherwise typing a row's own name buries it under the four rows that mention
// it in passing.
func TestNameMatchesOutrankHintMatches(t *testing.T) {
	s := newSheet(t)
	s.setQuery("attribution")
	if len(s.visible) == 0 {
		t.Fatal("no matches for a row's own name")
	}
	if got := s.rows[s.visible[0]].setting.Key; got != config.KeyAttribution {
		t.Fatalf("best match is %q, want %q", got, config.KeyAttribution)
	}
}

// The highlight explains WHY a row is in the list, so it may only mark letters
// that are actually in the label.
func TestHighlightOffsetsLandOnTheLabel(t *testing.T) {
	s := newSheet(t)
	s.setQuery("tenure")
	if len(s.visible) == 0 {
		t.Fatal("no matches for tenure")
	}
	label := s.rows[s.visible[0]].setting.Label
	hits := s.hitsFor(0)
	if len(hits) != len("tenure") {
		t.Fatalf("hits = %v for label %q, want one per query letter", hits, label)
	}
	for index, at := range hits {
		if at < 0 || at >= len(label) {
			t.Fatalf("hit %d is offset %d, outside %q", index, at, label)
		}
		if label[at] != "tenure"[index] {
			t.Fatalf("hit %d marks %q, want %q", index, label[at], "tenure"[index])
		}
	}
}

// Rung two of the esc ladder (8.2.21): esc clears the search and the tabs come
// back. It does not close the sheet while there is a filter to drop first.
func TestEscClearsTheSearchBeforeItClosesTheSheet(t *testing.T) {
	closed := 0
	s := newSheet(t, func(o *Options) {
		o.OnClose = func() tea.Cmd { closed++; return nil }
	})
	s.typeText("bud")
	s.press(namedKey(tea.KeyEscape))

	if s.Query() != "" {
		t.Fatalf("first esc left the query %q", s.Query())
	}
	if closed != 0 {
		t.Fatal("first esc closed the sheet instead of clearing the search")
	}
	if s.Tab() == "" {
		t.Fatal("clearing the search must bring the tabs back")
	}

	s.press(namedKey(tea.KeyEscape))
	if closed != 1 {
		t.Fatalf("second esc closed %d times, want 1", closed)
	}
}

func TestBackspaceWalksTheQueryBackAndChordsAreNotTyping(t *testing.T) {
	s := newSheet(t)
	s.typeText("bud")
	s.press(namedKey(tea.KeyBackspace))
	if s.Query() != "bu" {
		t.Fatalf("query = %q after backspace, want %q", s.Query(), "bu")
	}

	s.press(ctrlKey('w'))
	if s.Query() != "bu" {
		t.Fatalf("a ctrl chord typed into the query: %q", s.Query())
	}

	// Shift still produces text, so a capital letter searches like any other.
	s.setQuery("")
	s.press(tea.KeyPressMsg{Code: 'B', Text: "B", Mod: tea.ModShift})
	if s.Query() != "B" {
		t.Fatalf("query = %q, want a shifted letter to search", s.Query())
	}
}

// Search runs over the rows a gate is currently showing, so a hidden row can
// never be reached by typing its name either.
func TestSearchHonoursGates(t *testing.T) {
	restore := gates
	t.Cleanup(func() { gates = restore })
	gates = map[string]gate{
		config.KeyAttribution: func(value func(string) (string, bool)) bool {
			on, _ := value(config.KeyProposeSkills)
			return on == "on"
		},
	}

	s := newSheet(t)
	s.setQuery("attribution")
	if len(s.visible) == 0 {
		t.Fatal("the parent is on by default, so the gated row should be here")
	}

	s.gotoRow(t, config.KeyProposeSkills)
	s.activate(true) // toggle the parent off
	s.setQuery("attribution")
	for _, index := range s.visible {
		if s.rows[index].setting.Key == config.KeyAttribution {
			t.Fatal("a gated-off row was still reachable by search")
		}
	}
}
