package tokens

import "testing"

// TestRailAtWidthIsDerived holds 10.5.24's actual requirement: the breakpoints
// are STATED NUMBERS WITH ARITHMETIC, not constants someone tuned until a
// screenshot looked right. If the rail widens or the transcript floor moves,
// the threshold must move with them.
func TestRailAtWidthIsDerived(t *testing.T) {
	if RailAtWidth != RailTranscriptFloor+RailGutter+RailWidth {
		t.Fatalf("RailAtWidth = %d but its parts sum to %d; the derivation has rotted into a magic number",
			RailAtWidth, RailTranscriptFloor+RailGutter+RailWidth)
	}
	// Part 9.12: the current chat's railAtWidth is 100, which makes the narrow
	// path the primary experience at an ordinary 80-column terminal. The
	// revision must not go the wrong way, and must not reach 80 either — at 80
	// the scope map is a full-pane list by design, not a squeezed rail.
	if RailAtWidth >= 100 {
		t.Errorf("RailAtWidth = %d: the whole point of the revision is that it is below 100", RailAtWidth)
	}
	if RailAtWidth <= 80 {
		t.Errorf("RailAtWidth = %d: at 80 columns the narrow HUD path is the design (9.12, 8.2.8), "+
			"not a rail squeezed into a transcript's space", RailAtWidth)
	}
	// The floor must actually be a floor: with the rail drawn at the threshold,
	// what is left for the transcript is exactly the floor.
	if got := RailAtWidth - RailGutter - RailWidth; got != RailTranscriptFloor {
		t.Errorf("at the threshold the transcript gets %d columns, floor is %d", got, RailTranscriptFloor)
	}
}

// TestBreakpointsAreOrdered: the thresholds describe one continuum of terminal
// widths, so they must be consistent with each other. A dialog that goes
// fullscreen at a width where the rail is still drawn would be two features
// disagreeing about how much room exists.
func TestBreakpointsAreOrdered(t *testing.T) {
	if DialogFullscreenBelowWidth >= RailAtWidth {
		t.Errorf("dialogs go fullscreen below %d, but the rail is drawn from %d: "+
			"there is a band where a floating dialog and a rail fight for the same columns",
			DialogFullscreenBelowWidth, RailAtWidth)
	}
	if SplitDiffAtWidth <= RailAtWidth {
		t.Errorf("split diff at %d is below the rail threshold %d: a side-by-side diff "+
			"inside a task room would not fit beside the rail", SplitDiffAtWidth, RailAtWidth)
	}
	if PasteAttachRows <= ComposerMaxRows {
		t.Errorf("a paste becomes an attachment at %d rows, but the composer grows to %d: "+
			"pastes the composer can hold would be turned into chips", PasteAttachRows, ComposerMaxRows)
	}
	if HUDRowCap <= 0 || HUDRowCap > 12 {
		t.Errorf("HUDRowCap = %d: an unbounded HUD is the rail again, badly (8.2.8)", HUDRowCap)
	}
}

// TestDualContextThresholds is 8.2.17: warn at min(percent, absoluteTokens), so
// a 1M-window model warns at 150k rather than at 500k, where "half the window"
// is still an enormous amount of unspent room.
func TestDualContextThresholds(t *testing.T) {
	cases := []struct {
		window, want int64
		why          string
	}{
		{8_192, 6_144, "a small window is governed by the fraction"},
		{128_000, 96_000, "128K still warns on the fraction"},
		{200_000, 150_000, "200K is where the absolute cap takes over"},
		{1_000_000, 150_000, "a 1M window must warn at 150k, not at 750k — this is the whole rule"},
	}
	for _, c := range cases {
		if got := ContextWarnPoint(c.window); got != c.want {
			t.Errorf("ContextWarnPoint(%d) = %d, want %d — %s", c.window, got, c.want, c.why)
		}
	}
	// The alarm and the gauge colour follow the same point, and an unknown
	// window never alarms: unknown is not full (10.2.8).
	if ContextAlarm(999_999, 0) {
		t.Error("an unknown window must not alarm")
	}
	if ContextToken(0, 0) != TextTertiary {
		t.Error("with no window the gauge stays chrome")
	}
	for _, w := range []int64{8_192, 128_000, 1_000_000} {
		p := ContextWarnPoint(w)
		if ContextAlarm(p-1, w) {
			t.Errorf("window %d alarmed one token early", w)
		}
		if !ContextAlarm(p, w) {
			t.Errorf("window %d did not alarm at its warn point", w)
		}
		if got := ContextToken(p, w); got != Amber {
			t.Errorf("past the warn point the gauge is amber (5.16), got %s", got)
		}
		if got := ContextToken(p-1, w); got != TextTertiary {
			t.Errorf("before the warn point the gauge is chrome, got %s", got)
		}
	}
}

// TestFitFooter is 10.5.22: the status line is a registry of columns that
// SHORTENS, never wraps, dropping the lowest priority first — and never
// truncating a column, because a half-written verb is worse than an absent one.
func TestFitFooter(t *testing.T) {
	full := 0
	for _, c := range FooterColumnOrder {
		full += c.MinWidth
	}

	// Everything fits: nothing is dropped and display order is preserved.
	got := FitFooter(nil, full)
	if len(got) != len(FooterColumnOrder) {
		t.Fatalf("at full width %d columns survived, want %d", len(got), len(FooterColumnOrder))
	}
	for i := range got {
		if got[i].ID != FooterColumnOrder[i].ID {
			t.Fatalf("display order changed: position %d is %q, want %q", i, got[i].ID, FooterColumnOrder[i].ID)
		}
	}

	// Every width from zero to full: the result always fits, always keeps
	// display order, and is always monotone — narrowing the terminal may only
	// remove columns, never add one back.
	prev := map[string]bool{}
	for _, c := range FooterColumnOrder {
		prev[c.ID] = true
	}
	for width := full; width >= 0; width-- {
		kept := FitFooter(nil, width)
		total, lastIdx := 0, -1
		seen := map[string]bool{}
		for _, c := range kept {
			total += c.MinWidth
			seen[c.ID] = true
			idx := indexOfColumn(c.ID)
			if idx <= lastIdx {
				t.Fatalf("width %d: display order broken at %q", width, c.ID)
			}
			lastIdx = idx
		}
		if total > width && len(kept) > 1 {
			t.Fatalf("width %d: kept %d cells of columns", width, total)
		}
		for id := range seen {
			if !prev[id] {
				t.Fatalf("width %d: column %q reappeared as the terminal narrowed", width, id)
			}
		}
		prev = seen
	}

	// The drop order is the priority order: attention outlives everything,
	// scope goes first.
	kept := FitFooter(nil, FooterColumnOrder[0].MinWidth)
	if len(kept) != 1 || kept[0].ID != "attention" {
		t.Errorf("the last column standing is %v, want attention — an amber ?2 means a human is blocked", kept)
	}

	// A caller's own registry is not modified.
	mine := []FooterColumn{{"a", 10, 1, ""}, {"b", 10, 2, ""}}
	_ = FitFooter(mine, 10)
	if len(mine) != 2 || mine[0].ID != "a" {
		t.Error("FitFooter modified the caller's slice")
	}
}

// TestFooterRegistryIsWellFormed: every column needs a stated reason for its
// priority, priorities must be unique (a tie has no drop order), and the
// health-vs-cost split (10.5.23) must hold — this-turn cost and context live on
// the composer's meta strip, never in the footer.
func TestFooterRegistryIsWellFormed(t *testing.T) {
	ids := map[string]bool{}
	prios := map[int]string{}
	for _, c := range FooterColumnOrder {
		if c.ID == "" || c.Why == "" {
			t.Errorf("column %q has no id or no stated reason", c.ID)
		}
		if c.MinWidth <= 0 {
			t.Errorf("column %q has width %d", c.ID, c.MinWidth)
		}
		if ids[c.ID] {
			t.Errorf("duplicate footer column %q", c.ID)
		}
		ids[c.ID] = true
		if other, dup := prios[c.Priority]; dup {
			t.Errorf("columns %q and %q share priority %d; the drop order is undefined", c.ID, other, c.Priority)
		}
		prios[c.Priority] = c.ID
		switch c.ID {
		case "cost", "context", "tokens", "model":
			t.Errorf("column %q belongs to the composer's meta strip, not the footer (10.5.23)", c.ID)
		}
	}
}

func indexOfColumn(id string) int {
	for i, c := range FooterColumnOrder {
		if c.ID == id {
			return i
		}
	}
	return -1
}
