package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func standingPlaceFixture(t *testing.T) ([]StandingItemView, map[string]standing.Spend, time.Time) {
	t.Helper()
	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.Local)
	item := func(id, words, workspace string) standing.Item {
		return standing.Item{
			ID: id, Words: words, Workspace: workspace, Status: standing.StatusActive,
			When: standing.When{Kind: standing.WhenEvery, Words: "hourly"},
		}
	}
	asking := item("asking", "file anything from gmail that looks like an invoice", "/work/mail")
	asking.NeedsPerson = "send this invoice?"
	firing := item("firing", "watch every agentfield repo and summarise what merged", "/work/aforge")
	firing.Grant = "may summarise changes without asking"
	quiet := item("quiet", "at 6am, tell me what changed in aforge overnight", "/work/aforge")
	quiet.Brief.Title = "the 6am watch"
	quiet.When.Words = "daily"
	quiet.LastFired = now.Add(-3 * time.Hour)
	quiet.LastChecked = quiet.LastFired
	quiet.LastCheckLine = "nothing had changed since yesterday"
	paused := item("paused", "keep the top-movers sheet fresh before the open", "/work/markets")
	paused.Status = standing.StatusPaused
	paused.When.Words = "weekdays · 8:30am"
	extra := item("extra", "remind me to read the weekly report", "/work/aforge")
	extra.When.Words = "weekdays"
	more := item("more", "watch the release feed", "/work/aforge")
	last := item("last", "keep the changelog index fresh", "/work/aforge")
	return []StandingItemView{
			{Item: quiet}, {Item: paused}, {Item: asking}, {Item: extra},
			{Item: firing, Running: true}, {Item: more}, {Item: last},
		}, map[string]standing.Spend{
			"quiet": {USD: 0.003, Fired: 1},
		}, now
}

// The order is the place's triage promise, and stable sorting inside a state
// keeps equally quiet records from jumping whenever home takes a new reading.
func TestTheStandingPlacePutsNeedsAndMovingBeforeQuiet(t *testing.T) {
	views, week, now := standingPlaceFixture(t)
	r := readStanding(views, week, now)
	if got := []string{r.views[0].Item.ID, r.views[1].Item.ID, r.views[2].Item.ID, r.views[3].Item.ID}; strings.Join(got, ",") != "asking,firing,quiet,paused" {
		t.Fatalf("standing order = %v", got)
	}
	rows := r.rows(200, -1, newPalette(tokens.NoColor, false))
	if !strings.HasPrefix(rows[1], tokens.GlyphNeedsHuman) || !strings.HasPrefix(rows[2], tokens.GlyphWorking) ||
		!strings.HasPrefix(rows[3], tokens.GlyphQueued) || !strings.HasPrefix(rows[4], tokens.GlyphPaused) {
		t.Fatalf("state glyphs = %q", rows[1:5])
	}
}

func TestTheStandingHeaderSaysOnlyCountsTheRecordCanSupport(t *testing.T) {
	views, week, now := standingPlaceFixture(t)
	r := readStanding(views, week, now)
	if got, want := r.header, "things aforge does without being asked. 7 standing, 1 waiting to be stood up."; got != want {
		t.Fatalf("header = %q, want %q", got, want)
	}
	views[2].Item.NeedsPerson = ""
	if got := readStanding(views, week, now).header; strings.Contains(got, "waiting") {
		t.Fatalf("header invented a waiting count: %q", got)
	}
	if got := readStanding(nil, nil, now).rows(120, 0, newPalette(tokens.NoColor, false)); got != nil {
		t.Fatalf("empty reading drew %q", got)
	}
}

func TestTheStandingRowsUseTheSharedRopeCadenceAndCostWords(t *testing.T) {
	views, week, now := standingPlaceFixture(t)
	rows := readStanding(views, week, now).rows(200, -1, newPalette(tokens.NoColor, false))
	joined := strings.Join(rows, "\n")
	for _, word := range []string{"asks first", "on its own", "daily · fired 6am", "weekdays · 8:30am", "under a cent"} {
		if !strings.Contains(joined, word) {
			t.Errorf("rows do not contain %q:\n%s", word, joined)
		}
	}
}

func TestTheStandingFoldSaysWhetherItsHiddenRowsWereQuiet(t *testing.T) {
	views, week, now := standingPlaceFixture(t)
	r := readStanding(views, week, now)
	rows := r.rows(120, -1, newPalette(tokens.NoColor, false))
	if got := rows[5]; got != "▸ 3 more, all quiet this week" {
		t.Fatalf("quiet fold = %q", got)
	}
	week["extra"] = standing.Spend{USD: 1, Fired: 1}
	rows = readStanding(views, week, now).rows(120, -1, newPalette(tokens.NoColor, false))
	if got := rows[5]; got != "▸ 3 more" {
		t.Fatalf("active fold = %q", got)
	}
}

func TestTheStandingLastLookFollowsTheCursorAndKeepsQuietWhenUnknown(t *testing.T) {
	views, week, now := standingPlaceFixture(t)
	r := readStanding(views, week, now)
	rows := r.rows(120, 3, newPalette(tokens.NoColor, false))
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "the 6am watch, last look · 3h ago") ||
		!strings.Contains(joined, "fired 3h ago · nothing had changed since yesterday") {
		t.Fatalf("last look did not follow quiet row:\n%s", joined)
	}
	if got := len(r.rows(120, 4, newPalette(tokens.NoColor, false))); got != 6 {
		t.Fatalf("a row with no last look drew detail; got %d rows", got)
	}
}

func TestTheStandingReadingStopsOnlyOnItems(t *testing.T) {
	views, week, now := standingPlaceFixture(t)
	r := readStanding(views, week, now)
	for _, row := range []int{0, 5, 6, -1} {
		if _, ok := r.at(row); ok {
			t.Errorf("row %d became a stop", row)
		}
	}
	if got, ok := r.at(1); !ok || got.Item.ID != "asking" {
		t.Fatalf("first item = %q, %v", got.Item.ID, ok)
	}
}

func TestTheStandingVerbsBelongToTheRowsOwnState(t *testing.T) {
	views, week, now := standingPlaceFixture(t)
	r := readStanding(views, week, now)
	words := func(row int) string {
		verbs := r.verbs(row)
		parts := make([]string, 0, len(verbs))
		for _, verb := range verbs {
			parts = append(parts, string(verb.key)+" "+verb.word)
		}
		return strings.Join(parts, " · ")
	}
	if got, want := words(1), "y let it · n not this time · p pause · t trust it alone · x retire"; got != want {
		t.Fatalf("asking verbs = %q, want %q", got, want)
	}
	if got, want := words(2), "p pause · x retire"; got != want {
		t.Fatalf("trusted moving verbs = %q, want %q", got, want)
	}
	if got, want := words(4), "r resume · t trust it alone · x retire"; got != want {
		t.Fatalf("paused verbs = %q, want %q", got, want)
	}
	if got := r.verbs(0); got != nil {
		t.Fatalf("header verbs = %v", got)
	}
}

func TestTheStandingPlaceHoldsAtEveryWidth(t *testing.T) {
	views, week, now := standingPlaceFixture(t)
	r := readStanding(views, week, now)
	for _, width := range []int{60, 80, 120, 200} {
		for _, row := range r.rows(width, 3, newPalette(tokens.NoColor, false)) {
			if got := ansi.StringWidth(row); got > width {
				t.Errorf("width %d drew %d cells: %q", width, got, row)
			}
		}
	}
	rows := r.rows(60, -1, newPalette(tokens.NoColor, false))
	if strings.Contains(rows[1], "hourly") {
		t.Fatalf("narrow row kept cadence: %q", rows[1])
	}
}

func TestTheStandingPlaceObeysTheEmptinessLaw(t *testing.T) {
	views, _, now := standingPlaceFixture(t)
	rows := readStanding(views, nil, now).rows(120, -1, newPalette(tokens.NoColor, false))
	if got := strings.Join(rows, "\n"); strings.Contains(got, "$0.00") || strings.Contains(got, "0 standing") {
		t.Fatalf("empty figures reached the place:\n%s", got)
	}
	if teach := standingTeach(newPalette(tokens.NoColor, false)); len(teach) != 3 {
		t.Fatalf("empty teaching has %d lines", len(teach))
	}
}
