package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// ── THE PROJECT CARD ────────────────────────────────────────────────────────
//
// The cursor on a project asks "what is going on in here", and the answer is
// three bands: the conversations, what is keeping an eye on them, and the dim
// arithmetic. These tests draw each one from a world built in memory — the
// bands read nothing from disk, which is half of what makes them safe to draw
// on every frame, and a test that wrote JSON documents to assert a row's
// spacing would be testing the reader instead.

// projectCardNow is the instant every age on these cards is measured from. A
// fixed one, so a row reading `2h` is a row about the fixture and not about how
// long the suite took to get here.
var projectCardNow = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

// projectCardRowOf is one conversation in the fixture world, with only the
// fields a row reads.
func projectCardRowOf(id, title string, ago time.Duration, tasks session.TaskRollup) session.SessionRow {
	return session.SessionRow{
		ID:         id,
		Dir:        "/buckets/alpha/" + id,
		Transcript: "/buckets/alpha/" + id + "/transcript.jsonl",
		Project:    "alpha",
		ProjectDir: "/w/alpha",
		Title:      title,
		At:         projectCardNow.Add(-ago),
		Tasks:      tasks,
	}
}

// projectCardWorld is one project with several conversations in it, in the
// triage order the world's own reader hands them over in: what needs somebody,
// then what is running, then what is incomplete, then the rest by recency.
func projectCardWorld(rows ...session.SessionRow) session.World {
	return session.World{
		Read: projectCardNow,
		Projects: []session.Project{{
			Bucket:   "-w-alpha",
			Dir:      "/buckets/alpha",
			Path:     "/w/alpha",
			Name:     "alpha",
			Sessions: rows,
		}},
	}
}

// projectCardSubject is the cursor resting on that project.
func projectCardSubject(world session.World) bandSubject {
	return bandSubject{kind: bandKindProject, project: "alpha", dir: "/buckets/alpha", world: world}
}

func projectCardCtx(a *app, world session.World, width int) bandContext {
	return bandContext{subject: projectCardSubject(world), width: width, now: world.Read, pal: a.pal}
}

// projectCardApp is a surface with nothing on it but a palette — these bands
// touch the app only for its fold state and its lock question.
func projectCardApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = ""
	return a
}

func projectCardPlain(rows []string) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, strings.TrimRight(plain(row), " "))
	}
	return out
}

// THE CONVERSATIONS BAND IS THE LEFT COLUMN'S OWN ROWS, at the card's width and
// in the world's own order.
func TestTheProjectCardListsItsConversationsAsTheLeftColumnDoes(t *testing.T) {
	a := projectCardApp(t)
	world := projectCardWorld(
		func() session.SessionRow {
			row := projectCardRowOf("s1", "fix flaky auth test", 2*time.Minute, session.TaskRollup{})
			row.Live = true
			row.Presence = session.SessionPresence{State: session.PresenceWaiting, Reason: "which branch?"}
			return row
		}(),
		projectCardRowOf("s2", "task bar view more", 12*time.Minute, session.TaskRollup{
			Rows: make([]session.TaskIndexEntry, 3), Running: 1, Spend: 1.2,
		}),
		projectCardRowOf("s3", "session model ideas", 8*time.Hour, session.TaskRollup{}),
	)
	rows := projectCardPlain(drawProjectSessionsBand(a, projectCardCtx(a, world, 44)))
	if len(rows) != 3 {
		t.Fatalf("the band drew %d rows, want 3:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	want := []string{
		homeAskGlyph + " Fix Flaky Auth Test    waiting on you · 2m",
		homeLiveGlyph + " Task Bar View More         1 running · 12m",
		homeIdleGlyph + " Session Model Ideas                     8h",
	}
	for i, row := range rows {
		if row != want[i] {
			t.Errorf("row %d\n got %q\nwant %q", i, row, want[i])
		}
	}
}

// A PROJECT NOTHING IS IN DRAWS NOTHING, and so does a subject the world has
// never heard of. The emptiness law reaches whole bands.
func TestTheProjectCardDrawsNothingForAnEmptyProject(t *testing.T) {
	a := projectCardApp(t)
	empty := projectCardWorld()
	ctx := projectCardCtx(a, empty, 44)
	for name, rows := range map[string][]string{
		"conversations":  drawProjectSessionsBand(a, ctx),
		"keeping an eye": drawProjectStandingBand(a, ctx),
		"facts":          drawProjectFactsBand(a, ctx),
	} {
		if len(rows) != 0 {
			t.Errorf("the %s band drew %q for a project with nothing in it", name, rows)
		}
	}

	stranger := bandContext{
		subject: bandSubject{kind: bandKindProject, project: "beta", dir: "/buckets/beta", world: empty},
		width:   44, now: empty.Read, pal: a.pal,
	}
	if rows := drawProjectSessionsBand(a, stranger); len(rows) != 0 {
		t.Errorf("a project the world does not hold drew %q", rows)
	}
	if rows := drawProjectFactsBand(a, stranger); len(rows) != 0 {
		t.Errorf("a project the world does not hold drew facts %q", rows)
	}
}

// PAST [homeShown] THE BAND IS A DOOR, and the door says how many are behind
// it — in the same words the left column's own tail uses. Opening it draws
// them all and keeps the line as the way back.
func TestTheProjectCardFoldsItsConversationsPastFour(t *testing.T) {
	a := projectCardApp(t)
	var rows []session.SessionRow
	for i := 0; i < homeShown+3; i++ {
		rows = append(rows, projectCardRowOf("s"+itoa(i), "chat "+itoa(i), time.Duration(i)*time.Hour, session.TaskRollup{}))
	}
	world := projectCardWorld(rows...)
	ctx := projectCardCtx(a, world, 44)

	folded := projectCardPlain(drawProjectSessionsBand(a, ctx))
	if len(folded) != homeShown+1 {
		t.Fatalf("folded band drew %d rows, want %d:\n%s", len(folded), homeShown+1, strings.Join(folded, "\n"))
	}
	door := folded[len(folded)-1]
	if want := bandFoldGlyph + " …3 more " + projectSessionsWord; door != want {
		t.Fatalf("the fold line is %q, want %q", door, want)
	}

	a.toggleBandFold("projectsessions", ctx.subject)
	open := projectCardPlain(drawProjectSessionsBand(a, ctx))
	if len(open) != len(rows)+1 {
		t.Fatalf("opened band drew %d rows, want %d:\n%s", len(open), len(rows)+1, strings.Join(open, "\n"))
	}
	if want := bandFoldOpenGlyph + " …3 fewer"; open[len(open)-1] != want {
		t.Fatalf("the opened fold line is %q, want %q", open[len(open)-1], want)
	}
}

// THE STANDING BAND READS THE CACHE THE LEFT COLUMN ALREADY FILLED, draws the
// same glyph and the same rollup, and folds at three behind the same words.
func TestTheProjectCardListsWhatIsKeepingAnEyeOnTheProject(t *testing.T) {
	a := projectCardApp(t)
	world := projectCardWorld(projectCardRowOf("s1", "pricing research", time.Hour, session.TaskRollup{}))

	item := func(id, words, when string) StandingItemView {
		return StandingItemView{Item: standing.Item{
			ID: id, Words: words, Workspace: "/w/alpha",
			When:   standing.When{Kind: standing.WhenEvery, Words: when},
			Status: standing.StatusActive,
		}}
	}
	views := []StandingItemView{
		item("i1", "keep main green", "when CI goes red"),
		item("i2", "remind me on Fridays", "Fridays"),
		item("i3", "remind me on Sundays", "Sundays"),
		item("i4", "remind me on Tuesdays", "Tuesdays"),
	}
	views[0].Item.NeedsPerson = "the fix touches migrations"
	a.home.items = map[string][]StandingItemView{"/buckets/alpha": views}

	ctx := projectCardCtx(a, world, 50)
	rows := projectCardPlain(drawProjectStandingBand(a, ctx))
	if len(rows) != homeItemsShown+2 {
		t.Fatalf("the band drew %d rows, want %d:\n%s", len(rows), homeItemsShown+2, strings.Join(rows, "\n"))
	}
	want := []string{
		homeAskGlyph + " keep main green",
		"      needs your look · the fix touches migrations",
		standWaitGlyph + " remind me on Fridays                     Fridays",
		standWaitGlyph + " remind me on Sundays                     Sundays",
		bandFoldGlyph + " …1 more " + projectItemsWord,
	}
	for i, row := range rows {
		if row != want[i] {
			t.Errorf("row %d\n got %q\nwant %q", i, row, want[i])
		}
	}
	// The words on the door are the LEFT COLUMN'S OWN WORDS, not a second
	// spelling of them.
	if !strings.Contains(rows[len(rows)-1], strings.TrimSpace(homeItemsFoldWord)) {
		t.Errorf("the fold line %q does not say %q", rows[len(rows)-1], homeItemsFoldWord)
	}
}

// THE FACTS LINE IS ONE LINE, AND EVERY CLAUSE OF IT IS EARNED.
func TestTheProjectCardCountsWhatTheProjectHasAndOmitsWhatItHasNot(t *testing.T) {
	a := projectCardApp(t)
	world := projectCardWorld(
		projectCardRowOf("s1", "one", 2*time.Hour, session.TaskRollup{
			Rows: make([]session.TaskIndexEntry, 2), Spend: 1.25, Newest: projectCardNow.Add(-2 * time.Hour),
		}),
		projectCardRowOf("s2", "two", 30*time.Hour, session.TaskRollup{
			Rows: make([]session.TaskIndexEntry, 1), Spend: 0.75,
		}),
	)
	rows := projectCardPlain(drawProjectFactsBand(a, projectCardCtx(a, world, 60)))
	if len(rows) != 1 {
		t.Fatalf("the facts band drew %d rows, want 1: %q", len(rows), rows)
	}
	if want := "2 conversations · 3 tasks · spent $2.00 · last active 2h"; rows[0] != want {
		t.Fatalf("the facts line is %q, want %q", rows[0], want)
	}

	// A project nobody has run a task in, and that has spent nothing, says
	// neither — never `0 tasks`, never `$0.00`.
	quiet := projectCardWorld(projectCardRowOf("s1", "one", 5*time.Minute, session.TaskRollup{}))
	rows = projectCardPlain(drawProjectFactsBand(a, projectCardCtx(a, quiet, 60)))
	if want := "1 conversation · last active 5m"; len(rows) != 1 || rows[0] != want {
		t.Fatalf("a quiet project's facts line is %q, want %q", rows, want)
	}
}

// EVERY ROW IS CUT TO THE COLUMN, and the NAME is what gives way — a name cut
// in half is still recognisable and a rollup cut in half is a wrong fact. Under
// the floor the tail goes entirely rather than eating the name.
func TestTheProjectCardClipsEveryRowToItsWidth(t *testing.T) {
	a := projectCardApp(t)
	world := projectCardWorld(
		projectCardRowOf("s1", "a conversation with a very long name indeed", time.Hour,
			session.TaskRollup{Rows: make([]session.TaskIndexEntry, 4), Spend: 3}),
	)
	a.home.items = map[string][]StandingItemView{"/buckets/alpha": {{Item: standing.Item{
		ID: "i1", Words: "tell me when the certificate is about to expire", Workspace: "/w/alpha",
		When:   standing.When{Kind: standing.WhenEvery, Words: "every morning at nine o'clock"},
		Status: standing.StatusActive,
	}}}}

	for _, width := range []int{80, 44, 20, 10, 6, 1} {
		ctx := projectCardCtx(a, world, width)
		var drawn []string
		drawn = append(drawn, drawProjectSessionsBand(a, ctx)...)
		drawn = append(drawn, drawProjectStandingBand(a, ctx)...)
		drawn = append(drawn, drawProjectFactsBand(a, ctx)...)
		if len(drawn) == 0 {
			t.Fatalf("width %d drew nothing at all", width)
		}
		for _, row := range projectCardPlain(drawn) {
			if got := len([]rune(row)); got > width {
				t.Errorf("width %d drew a %d-cell row %q", width, got, row)
			}
		}
	}
}

func TestNarrowProjectBandsKeepTheirTrailingFacts(t *testing.T) {
	a := projectCardApp(t)
	world := projectCardWorld(projectCardRowOf("s1", "a conversation with a long name", time.Hour,
		session.TaskRollup{Rows: make([]session.TaskIndexEntry, 3), Spend: 2.5}))
	a.home.items = map[string][]StandingItemView{"/buckets/alpha": {{Item: standing.Item{
		ID: "i1", Words: "keep the release branch green", Workspace: "/w/alpha",
		When: standing.When{Kind: standing.WhenEvery, Words: "every weekday morning"}, Status: standing.StatusActive,
	}}}}
	ctx := projectCardCtx(a, world, 30)
	assertNarrowRows(t, "project conversations", drawProjectSessionsBand(a, ctx), 30, "1h")
	assertNarrowRows(t, "project standing", drawProjectStandingBand(a, ctx), 30, "every weekday morning")
	assertNarrowRows(t, "project facts", drawProjectFactsBand(a, ctx), 30, "last active 1h")
}

// THE THREE BANDS ARE REGISTERED FOR A PROJECT AND IN THIS ORDER: what is going
// on, what is watching it, and the arithmetic last.
func TestTheProjectBandsAreRegisteredInReadingOrder(t *testing.T) {
	var names []string
	for _, band := range homeBandsFor(bandKindProject) {
		switch band.name {
		case "projectsessions", "projectstanding", "projectfacts":
			names = append(names, band.name)
		}
	}
	want := []string{"projectsessions", "projectstanding", "projectfacts"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("the project bands are %v, want %v", names, want)
	}
	// And none of them draws for a conversation, which has its own.
	for _, band := range homeBandsFor(bandKindSession) {
		if strings.HasPrefix(band.name, "project") {
			t.Errorf("%s draws for a conversation", band.name)
		}
	}
}
