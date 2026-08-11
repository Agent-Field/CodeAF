package homes

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func typeKeys(s *Spend, keys ...string) (SpendResult, bool) {
	var last SpendResult
	var ok bool
	for _, k := range keys {
		msg := tea.KeyPressMsg{}
		switch k {
		case "esc":
			msg.Code = tea.KeyEscape
		case "enter":
			msg.Code = tea.KeyEnter
		case "backspace":
			msg.Code = tea.KeyBackspace
		case "left":
			msg.Code = tea.KeyLeft
		case "right":
			msg.Code = tea.KeyRight
		case "home":
			msg.Code = tea.KeyHome
		case "end":
			msg.Code = tea.KeyEnd
		default:
			msg.Code = rune(k[0])
			msg.Text = k
		}
		r, committed, _ := s.Key(msg)
		if committed {
			last, ok = r, true
		}
	}
	return last, ok
}

// The segment never overflows and never panics, at any width, in either state,
// with every styler including none.
func TestSpendNeverOverflowsAndNeverPanics(t *testing.T) {
	states := []SpendState{
		{},
		{SpentUSD: 8.65, HasSpent: true, Editable: true},
		{SpentUSD: 8.65, HasSpent: true, LimitUSD: 20, HasLimit: true, Editable: true},
		{SpentUSD: 1234.56, HasSpent: true, Unlimited: true, Editable: true},
		{SpentUSD: 20, HasSpent: true, LimitUSD: 20, HasLimit: true, Reached: true, Editable: true},
	}
	for name, st := range stylers() {
		for _, state := range states {
			for _, editing := range []bool{false, true} {
				for _, focused := range []bool{false, true} {
					for width := 0; width <= 60; width++ {
						s := NewSpend(st)
						s.SetState(state)
						s.Focus(focused)
						if editing {
							s.Open()
							typeKeys(s, "1", "2", ".", "5")
						}
						out := s.Render(width)
						if w := blocks.Width(out); w > width {
							t.Fatalf("%s w=%d: %d cells: %q", name, width, w, out)
						}
						if strings.ContainsAny(out, "\n\r") {
							t.Fatalf("%s w=%d: newline in %q", name, width, out)
						}
					}
				}
			}
		}
	}
}

// A rail that may not be changed from this window refuses to open and SAYS it
// refused, so the caller can explain rather than have the key do nothing
// (5.20 rule 3).
func TestAReadOnlyRailRefusesToOpenRatherThanDoingNothing(t *testing.T) {
	s := NewSpend(nil)
	s.SetState(SpendState{SpentUSD: 3, HasSpent: true, LimitUSD: 20, HasLimit: true})
	if s.Open() {
		t.Fatal("a non-editable rail opened")
	}
	if s.Editing() {
		t.Fatal("a refused open left the field open")
	}
	s.SetState(SpendState{Editable: true, LimitUSD: 20, HasLimit: true})
	if !s.Open() || !s.Editing() {
		t.Fatal("an editable rail refused to open")
	}
}

// The field seeds from the ceiling in force, without the currency mark, the
// padding or a separator — every one of those is a character the person would
// have to delete before typing.
func TestTheFieldSeedsFromTheCeilingWithNothingToDelete(t *testing.T) {
	s := NewSpend(nil)
	s.SetState(SpendState{Editable: true, LimitUSD: 20, HasLimit: true})
	s.Open()
	if got := s.Draft(); got != "20" {
		t.Fatalf("seeded %q", got)
	}
	s.Cancel()
	s.SetState(SpendState{Editable: true, LimitUSD: 12.5, HasLimit: true})
	s.Open()
	if got := s.Draft(); got != "12.5" {
		t.Fatalf("seeded %q", got)
	}
	// An unlimited rail seeds empty: there is no number to start from, and
	// putting one there would be inventing the ceiling the person lifted.
	s.Cancel()
	s.SetState(SpendState{Editable: true, LimitUSD: 20, HasLimit: true, Unlimited: true})
	s.Open()
	if got := s.Draft(); got != "" {
		t.Fatalf("an unlimited rail seeded %q", got)
	}
}

// The grammar is tiny on purpose: a budget field that accepts letters is a
// field that fails at commit instead of at the keystroke.
func TestTheFieldRefusesEverythingThatIsNotANumber(t *testing.T) {
	s := NewSpend(nil)
	s.SetState(SpendState{Editable: true})
	s.Open()
	typeKeys(s, "a", "1", "z", "2", ".", "5", ".", "0", "-", "e")
	if got := s.Draft(); got != "12.50" {
		t.Fatalf("draft is %q", got)
	}
	if r, ok := typeKeys(s, "enter"); !ok || r.LimitUSD != 12.5 {
		t.Fatalf("commit gave %+v ok=%v", r, ok)
	}
	if s.Editing() {
		t.Fatal("a committed field stayed open")
	}
}

// A refused commit keeps what was typed. A mistyped budget that vanished would
// make the person start over to fix one character.
func TestARefusedCommitKeepsTheDraftAndSaysSo(t *testing.T) {
	s := NewSpend(nil)
	s.SetState(SpendState{Editable: true})
	s.Open()
	typeKeys(s, ".")
	if _, ok := typeKeys(s, "enter"); ok {
		t.Fatal("a lone decimal point committed")
	}
	if !s.Editing() || !s.Invalid() || s.Draft() != "." {
		t.Fatalf("editing=%v invalid=%v draft=%q", s.Editing(), s.Invalid(), s.Draft())
	}
	// Typing again clears the mark: the person is fixing it.
	typeKeys(s, "5")
	if s.Invalid() {
		t.Fatal("the invalid mark survived a correction")
	}
}

// esc drops the draft. A number half typed is not a budget, and keeping it
// would mean the next open showed a figure nobody chose.
func TestEscapeDropsTheDraft(t *testing.T) {
	s := NewSpend(nil)
	s.SetState(SpendState{Editable: true})
	s.Open()
	typeKeys(s, "9", "9", "esc")
	if s.Editing() || s.Draft() != "" {
		t.Fatalf("editing=%v draft=%q", s.Editing(), s.Draft())
	}
	// Losing focus does the same: a field left open behind a pane the person
	// walked away from is a field that commits something they forgot.
	s.Open()
	typeKeys(s, "7")
	s.Focus(false)
	if s.Editing() {
		t.Fatal("the field survived losing focus")
	}
}

// The reading a poll refreshes must not clear a field under the person's hands.
func TestAPollUnderAnOpenFieldDoesNotClearIt(t *testing.T) {
	s := NewSpend(nil)
	s.SetState(SpendState{Editable: true, LimitUSD: 20, HasLimit: true})
	s.Open()
	typeKeys(s, "backspace", "5")
	s.SetState(SpendState{Editable: true, SpentUSD: 9.10, HasSpent: true, LimitUSD: 20, HasLimit: true})
	if !s.Editing() || s.Draft() != "25" {
		t.Fatalf("editing=%v draft=%q", s.Editing(), s.Draft())
	}
}

// parseUSD refuses what strconv would take but a budget should not.
func TestABudgetRefusesTheNumbersThatAreNotMoney(t *testing.T) {
	for _, bad := range []string{"", " ", "-1", "1e3", "NaN", "Inf", "1.2.3", "0x10", "1,000", "٣"} {
		if v, ok := parseUSD(bad); ok {
			t.Fatalf("%q parsed as %v", bad, v)
		}
	}
	for raw, want := range map[string]float64{"0": 0, "20": 20, "12.50": 12.5, "$3.25": 3.25, " 7 ": 7} {
		v, ok := parseUSD(raw)
		if !ok || v != want {
			t.Fatalf("%q gave %v/%v, want %v", raw, v, ok, want)
		}
	}
}

// The segment reports the width it will take, so a status line that fits its
// columns by asking is never told the wrong number (10.5.22).
func TestTheSegmentReportsItsOwnWidth(t *testing.T) {
	s := NewSpend(tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal))
	s.SetState(SpendState{SpentUSD: 8.65, HasSpent: true, LimitUSD: 20, HasLimit: true, Editable: true})
	want := s.Width()
	if got := blocks.Width(s.Render(want)); got != want {
		t.Fatalf("Width()=%d but a render at that width took %d cells", want, got)
	}
	if !strings.Contains(s.Text(), "8.65") || !strings.Contains(s.Text(), "20.00") {
		t.Fatalf("copy text lost a figure: %q", s.Text())
	}
}

// A reached rail is the one condition under which this segment is not chrome:
// amber, because a reached rail needs a human and that is the whole of what
// amber means (5.16).
func TestAReachedRailIsTheOnlyAmberInTheSegment(t *testing.T) {
	st := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	amber := tokens.Amber.Fg(tokens.TrueColor, tokens.FocusNormal)
	calm := NewSpend(st)
	calm.SetState(SpendState{SpentUSD: 3, HasSpent: true, LimitUSD: 20, HasLimit: true})
	if strings.Contains(calm.Render(40), amber) {
		t.Fatal("a rail with room to spare painted amber")
	}
	hot := NewSpend(st)
	hot.SetState(SpendState{SpentUSD: 20, HasSpent: true, LimitUSD: 20, HasLimit: true, Reached: true})
	if !strings.Contains(hot.Render(40), amber) {
		t.Fatal("a reached rail did not paint amber")
	}
}

// A spend nobody has counted renders as missing, never as zero (10.2.8).
func TestAnUncountedSpendIsMissingAndNotZero(t *testing.T) {
	s := NewSpend(nil)
	s.SetState(SpendState{})
	if got := s.Text(); !strings.Contains(got, tokens.GlyphMissing) {
		t.Fatalf("uncounted spend rendered %q", got)
	}
	s.SetState(SpendState{HasSpent: true})
	if got := s.Text(); strings.Contains(got, tokens.GlyphMissing) {
		t.Fatalf("a counted zero rendered as missing: %q", got)
	}
}
