package settings

import (
	"image"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// sheet is the surface under test: a real registry over a temp profile
// directory, so every write in this suite lands in a real config.json through
// the real write path. Nothing here stubs internal/config — the point of the
// round-trip tests is that this package has no writer of its own.
type sheet struct {
	*Model
	dir     string
	applied []string
	models  map[string]string
	split   int
}

func newSheet(t *testing.T, adjust ...func(*Options)) *sheet {
	t.Helper()
	s := &sheet{dir: t.TempDir(), models: map[string]string{}, split: 0}
	rows := config.NewSettings(config.SettingsOptions{
		ProfileDir: s.dir,
		ModelValue: func(slot string) string { return s.models[slot] },
		SetModel: func(slot, slug string) error {
			s.models[slot] = slug
			return nil
		},
		SplitPct:     func() int { return s.split },
		SaveSplitPct: func(pct int) { s.split = pct },
		Applied:      func(key string) { s.applied = append(s.applied, key) },
	})
	opts := Options{Registry: rows, Debounce: time.Hour}
	for _, fn := range adjust {
		fn(&opts)
	}
	s.Model = New(opts)
	return s
}

// gotoRow puts the band on one registry key. Every row is on the one page now,
// so finding one is a walk down the list rather than a tour of the tabs.
func (s *sheet) gotoRow(t *testing.T, key string) row {
	t.Helper()
	s.setQuery("")
	s.reselectFresh()
	for position, index := range s.visible {
		if s.rows[index].setting.Key == key {
			s.selected = position
			return s.rows[index]
		}
	}
	t.Fatalf("no row %q in the sheet", key)
	return row{}
}

// listed reports whether a key is reachable anywhere in the sheet right now —
// the question a gate answers.
func (s *sheet) listed(key string) bool {
	s.setQuery("")
	s.reselectFresh()
	for _, index := range s.visible {
		if s.rows[index].setting.Key == key {
			return true
		}
	}
	return false
}

func typeRune(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

func at(x, y int) image.Point { return image.Point{X: x, Y: y} }

func namedKey(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

// ctrlKey is a real chord: a terminal reporting ctrl+u sends no text, which is
// exactly why a chord can never be mistaken for typing.
func ctrlKey(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: tea.ModCtrl}
}

func (s *sheet) press(keys ...tea.KeyPressMsg) {
	for _, key := range keys {
		s.Key(key)
	}
}

func (s *sheet) typeText(text string) {
	for _, r := range text {
		s.Key(typeRune(r))
	}
}

// The registry already carries this surface's row — its verb, its description,
// its accelerator and its slash alias. This test is the whole reason EntryID
// is a constant rather than a string the wiring lane retypes: if the catalog
// ever renames the row, the build says so here instead of the palette quietly
// losing a door (5.22).
func TestEntryIDResolvesInTheCommandRegistry(t *testing.T) {
	entry, ok := registry.ByID(EntryID)
	if !ok {
		t.Fatalf("registry has no entry %q — the wiring lane has nothing to bind", EntryID)
	}
	if entry.Key == "" && entry.Slash == "" {
		t.Fatalf("entry %q offers neither a key nor a slash alias", EntryID)
	}
}

func TestZeroOptionsRenderCalmlyRatherThanPanicking(t *testing.T) {
	m := New(Options{})
	frame := m.Render(60, 12)
	if !strings.Contains(frame, "no settings registry") {
		t.Fatalf("a registry-less sheet must say so, got:\n%s", frame)
	}
	if cmd := m.Key(namedKey(tea.KeyEscape)); cmd == nil {
		t.Fatalf("esc must always be answered, even with nothing to show")
	}
}

// Every frame at every plausible size, in both stylers, with the sheet in each
// of its modes. The compositor clips over-run, so the shipping bar is not
// "looks right at 80 columns" but "draws less at 1 and never panics".
func TestWidthSweepDrawsLessAndNeverPanics(t *testing.T) {
	for _, styled := range []bool{false, true} {
		s := newSheet(t, func(o *Options) {
			if styled {
				o.Styler = tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
			}
		})
		modes := []func(){
			func() {},
			func() { s.setQuery("model") },
			func() { s.setQuery(""); s.gotoRow(t, config.KeyDocumentEngine); s.activate(false) },
			func() { s.cancel(); s.gotoRow(t, config.KeyVisionModel); s.activate(false) },
			func() { s.cancel(); s.gotoRow(t, config.KeyDailyBudget) },
		}
		for _, mode := range modes {
			mode()
			for width := 0; width <= 120; width++ {
				for _, height := range []int{0, 1, 2, 3, 4, 5, 8, 13, 24, 40} {
					frame := s.Render(width, height)
					if frame == "" {
						continue
					}
					lines := strings.Split(frame, "\n")
					if len(lines) > height {
						t.Fatalf("w=%d h=%d rendered %d lines", width, height, len(lines))
					}
					for index, line := range lines {
						if got := ansi.StringWidth(line); got > width {
							t.Fatalf("w=%d h=%d line %d is %d cells: %q", width, height, index, got, line)
						}
					}
				}
			}
		}
	}
}

// Every group announces itself with its own faint word, above its own rows, on
// the one page (15). The band standing anywhere in a group must be able to see
// that word — which is the whole reason the word is allowed to exist.
func TestEachGroupIsAnnouncedAboveItsOwnRows(t *testing.T) {
	s := newSheet(t)
	for _, head := range s.groupHeads() {
		s.selected = head
		frame := s.Render(72, 40)
		want := s.rows[s.visible[head]].group
		if !strings.Contains(frame, want) {
			t.Fatalf("group %q never announced itself:\n%s", want, frame)
		}
		lines := strings.Split(frame, "\n")
		headingAt, rowAt := -1, -1
		for index, line := range lines {
			if headingAt < 0 && strings.TrimSpace(line) == want {
				headingAt = index
				continue
			}
			if headingAt >= 0 && strings.Contains(line, s.rows[s.visible[head]].setting.Label) {
				rowAt = index
				break
			}
		}
		if headingAt < 0 || rowAt < 0 || rowAt <= headingAt {
			t.Fatalf("group %q: heading at %d, its first row at %d\n%s", want, headingAt, rowAt, frame)
		}
	}
}

// ←→ used to change tabs. With one page it walks the group heads, and it walks
// them in both directions without ever landing between two of them.
func TestLeftRightWalksTheGroupHeads(t *testing.T) {
	s := newSheet(t)
	heads := s.groupHeads()
	if len(heads) < 2 {
		t.Fatalf("a sheet with %d groups cannot exercise the walk", len(heads))
	}
	head := func() int { return s.selected }

	s.selected = heads[0]
	for step := 1; step < len(heads); step++ {
		s.press(namedKey(tea.KeyRight))
		if head() != heads[step] {
			t.Fatalf("right %d landed at %d, want the head at %d", step, head(), heads[step])
		}
	}
	s.press(namedKey(tea.KeyRight))
	if head() != heads[0] {
		t.Fatalf("right past the last group landed at %d, want %d", head(), heads[0])
	}

	// Inside a group, back is the top of that group; at the top, back is the
	// group before.
	s.selected = heads[1] + 1
	s.press(namedKey(tea.KeyLeft))
	if head() != heads[1] {
		t.Fatalf("left from inside a group landed at %d, want its head %d", head(), heads[1])
	}
	s.press(namedKey(tea.KeyLeft))
	if head() != heads[0] {
		t.Fatalf("left from a group head landed at %d, want the group before at %d", head(), heads[0])
	}
}
