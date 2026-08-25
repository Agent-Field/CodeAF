package tui3

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

var searchTestNow = time.Date(2026, time.August, 25, 13, 0, 0, 0, time.Local)

func searchFixture() ([]store.ConversationHit, session.World) {
	found := func(seq int64, id, title, body string, ago time.Duration) store.ConversationHit {
		return store.ConversationHit{MessageHit: store.MessageHit{
			Seq: seq, SessionID: id, Body: body, Time: searchTestNow.Add(-ago),
		}, Title: title}
	}
	hits := []store.ConversationHit{
		found(1, "room-a", "Swarm splitting", "Add a report-only mode to the writer", 2*time.Hour),
		found(2, "room-a", "Swarm splitting", "The report should preserve old output", 3*time.Hour),
		found(3, "room-a", "Swarm splitting", "Report the changed files too", 4*time.Hour),
		found(4, "gone-room", "January pricing", "The pricing report used the old rail", 24*time.Hour),
		found(5, "room-b", "Lead research", "A different report", 5*time.Hour),
	}
	world := session.World{Projects: []session.Project{
		{Name: "aforge-v2", Sessions: []session.SessionRow{{
			ID: "room-a", Title: "Swarm splitting", Project: "aforge-v2", Dir: "/state/room-a",
			Transcript: "/state/room-a/transcript.jsonl", Live: true,
		}}},
		{Name: "leadgen", Sessions: []session.SessionRow{{ID: "room-b", Project: "leadgen", Dir: "/state/room-b"}}},
	}, Read: searchTestNow}
	return hits, world
}

func TestSearchJoinsConversationDoorsAndKeepsUnlistedConversations(t *testing.T) {
	hits, world := searchFixture()
	r := readSearch("report", hits, world, searchTestNow)
	if got := r.hits[0]; got.project != "aforge-v2" || got.transcript != "/state/room-a/transcript.jsonl" || !got.live {
		t.Fatalf("the first result did not join its conversation row: %+v", got)
	}
	page := strings.Join(r.rows(120, newPalette(tokens.NoColor, false)), "\n")
	if !strings.Contains(page, "January pricing") {
		t.Fatalf("the conversation absent from the world disappeared:\n%s", page)
	}
}

func TestSearchGroupsSeveralTurnsIntoOneConversation(t *testing.T) {
	hits, world := searchFixture()
	r := readSearch("report", hits, world, searchTestNow)
	if len(r.hits) != 3 || r.hits[0].more != 2 {
		t.Fatalf("grouped results are %#v", r.hits)
	}
	page := strings.Join(r.rows(120, newPalette(tokens.NoColor, false)), "\n")
	if strings.Count(page, "Swarm splitting") != 1 || !strings.Contains(page, "· 2 more in this chat") {
		t.Fatalf("the grouped conversation did not explain its other turns:\n%s", page)
	}
}

func TestSearchMarksMatchingWordsInBold(t *testing.T) {
	hits, world := searchFixture()
	pal := newPalette(tokens.TrueColor, false)
	page := strings.Join(readSearch("report", hits, world, searchTestNow).rows(120, pal), "\n")
	if !strings.Contains(page, pal.bold("report")) && !strings.Contains(page, pal.bold("Report")) {
		t.Fatalf("the matching word was not bold in %q", page)
	}
}

func TestSearchListsProjectFacetsWithCounts(t *testing.T) {
	hits, world := searchFixture()
	page := ansi.Strip(readSearch("report", hits, world, searchTestNow).rows(120, newPalette(tokens.NoColor, false))[0])
	for _, want := range []string{"aforge-v2 1", "leadgen 1"} {
		if !strings.Contains(page, want) {
			t.Fatalf("facet line %q does not contain %q", page, want)
		}
	}
}

func TestSearchShowsTwelveConversationsThenFoldsTheRest(t *testing.T) {
	var hits []store.ConversationHit
	for i := 0; i < 15; i++ {
		hits = append(hits, store.ConversationHit{MessageHit: store.MessageHit{
			SessionID: fmt.Sprintf("room-%d", i), Body: "needle in a turn", Time: searchTestNow,
		}, Title: fmt.Sprintf("conversation %d", i)})
	}
	r := readSearch("needle", hits, session.World{}, searchTestNow)
	page := strings.Join(r.rows(120, newPalette(tokens.NoColor, false)), "\n")
	if strings.Count(page, tokens.GlyphPromptChat) != searchShown || !strings.Contains(page, tokens.GlyphCollapsed+" 3 more") {
		t.Fatalf("the result cap did not draw twelve doors and a fold:\n%s", page)
	}
}

func TestSearchTeachesAnEmptyPageAndSaysWhenNothingMatches(t *testing.T) {
	pal := newPalette(tokens.NoColor, false)
	teach := strings.Join(readSearch("", nil, session.World{}, searchTestNow).rows(120, pal), "\n")
	for _, want := range []string{"every message in every conversation", "typing here searches", "enter opens the conversation at the matching turn"} {
		if !strings.Contains(teach, want) {
			t.Fatalf("the empty page did not teach %q:\n%s", want, teach)
		}
	}
	none := strings.Join(readSearch("amber rail", nil, session.World{}, searchTestNow).rows(120, pal), "\n")
	if !strings.Contains(none, `nothing on this machine says "amber rail"`) {
		t.Fatalf("the empty result was %q", none)
	}
}

func TestEverySearchRowFitsThePlaceAtEveryPromisedWidth(t *testing.T) {
	hits, world := searchFixture()
	r := readSearch("report", hits, world, searchTestNow)
	for _, width := range []int{60, 80, 120, 200} {
		for i, row := range r.rows(width, newPalette(tokens.ANSI256, false)) {
			if got := ansi.StringWidth(row); got > width {
				t.Fatalf("row %d drew %d cells at width %d: %q", i, got, width, ansi.Strip(row))
			}
		}
	}
}

func TestSearchCursorStopsOnlyOnConversationRows(t *testing.T) {
	hits, world := searchFixture()
	r := readSearch("report", hits, world, searchTestNow)
	rows := r.rows(120, newPalette(tokens.NoColor, false))
	stops := 0
	for i, row := range rows {
		_, ok := r.at(i)
		want := strings.HasPrefix(row, tokens.GlyphPromptChat+" ")
		if ok != want {
			t.Fatalf("row %d mapped=%v, conversation-row=%v: %q", i, ok, want, row)
		}
		if ok {
			stops++
		}
	}
	if stops != len(r.hits) {
		t.Fatalf("mapped %d conversation rows, want %d", stops, len(r.hits))
	}
}

type searchFakeStore struct {
	hits  []store.ConversationHit
	err   error
	got   string
	limit int
}

func (s *searchFakeStore) SearchConversations(terms string, limit int) ([]store.ConversationHit, error) {
	s.got, s.limit = terms, limit
	return s.hits, s.err
}

func TestSearchCommandCarriesItsGenerationAndReturnsErrorsAsMessages(t *testing.T) {
	wantErr := errors.New("store unavailable")
	fake := &searchFakeStore{hits: []store.ConversationHit{{Title: "one"}}, err: wantErr}
	ask := searchAsk{query: "report", gen: 17}
	msg, ok := searchCmd(fake, ask)().(searchDoneMsg)
	if !ok {
		t.Fatalf("search command returned %T, want searchDoneMsg", searchCmd(fake, ask)())
	}
	if msg.ask != ask || len(msg.hits) != 1 || !errors.Is(msg.err, wantErr) {
		t.Fatalf("search message lost its request, results, or error: %+v", msg)
	}
	if fake.got != ask.query || fake.limit != searchFetch {
		t.Fatalf("store was asked for %q/%d, want %q/%d", fake.got, fake.limit, ask.query, searchFetch)
	}
}

func TestSearchGenerationGuardsRejectStaleTicksAndResults(t *testing.T) {
	current := searchAsk{query: "new words", gen: 9}
	if searchTickAccepted(current, searchTickMsg{gen: 8}) || !searchTickAccepted(current, searchTickMsg{gen: 9}) {
		t.Fatal("tick acceptance did not follow the current generation")
	}
	if searchDoneAccepted(current, searchDoneMsg{ask: searchAsk{gen: 8}}) || !searchDoneAccepted(current, searchDoneMsg{ask: searchAsk{gen: 9}}) {
		t.Fatal("completion acceptance did not follow the current generation")
	}
}

// ── the place, as a person meets it ─────────────────────────────────────────

// searchLab is an app standing in the search place over a store this test wrote.
func searchLab(t *testing.T, store *searchFakeStore) *app {
	t.Helper()
	a := placeApp(t)
	a.searchStore = store
	a.showPage(pageSearch)
	return a
}

// TYPING SEARCHES, AND THE READ IS NEVER ON THE KEYSTROKE. Each letter arms a
// quiet interval; only an interval that survives to its end becomes a query.
func TestTypingOnTheSearchPlaceAsksOnlyAfterTheQuietInterval(t *testing.T) {
	hits, _ := searchFixture()
	fake := &searchFakeStore{hits: hits}
	a := searchLab(t, fake)
	if !strings.Contains(placeFrameText(a), "search reads every message") {
		t.Fatalf("the empty place did not say what it is for:\n%s", placeFrameText(a))
	}
	typeInto(t, a, "report")
	if fake.got != "" {
		t.Fatalf("a keystroke read the store for %q", fake.got)
	}
	// The interval, arriving, is what sends the read; the read's answer is what
	// puts rows on the page.
	cmd := a.searchTick(searchTickMsg{gen: a.search.ask.gen})
	if cmd == nil {
		t.Fatal("the quiet interval did not become a read")
	}
	done, ok := cmd().(searchDoneMsg)
	if !ok {
		t.Fatalf("the read answered %T", cmd())
	}
	if fake.got != "report" {
		t.Fatalf("the store was asked for %q", fake.got)
	}
	a.searchDone(done)
	if text := placeFrameText(a); !strings.Contains(text, "Swarm splitting") {
		t.Fatalf("the results are not on the page:\n%s", text)
	}
}

// AN OLD INTERVAL AND AN OLD ANSWER ARE BOTH DROPPED, so a slow store cannot
// put yesterday's words under today's.
func TestTheSearchPlaceDropsAStaleIntervalAndAStaleAnswer(t *testing.T) {
	hits, _ := searchFixture()
	fake := &searchFakeStore{hits: hits}
	a := searchLab(t, fake)
	typeInto(t, a, "report")
	stale := a.search.ask
	typeInto(t, a, "s")
	if a.searchTick(searchTickMsg{gen: stale.gen}) != nil {
		t.Fatal("an interval armed for words already replaced became a read")
	}
	a.searchDone(searchDoneMsg{ask: stale, hits: hits})
	if len(a.search.hits) != 0 {
		t.Fatalf("a stale answer landed on the page: %d hits", len(a.search.hits))
	}
}

// EMPTYING THE BOX PUTS THE RESULTS AWAY WITH THE WORDS THAT FOUND THEM, and
// reads nothing: a search for nothing is a table scan with no question in it.
func TestClearingTheSearchBoxTakesTheResultsWithIt(t *testing.T) {
	hits, _ := searchFixture()
	fake := &searchFakeStore{hits: hits}
	a := searchLab(t, fake)
	typeInto(t, a, "report")
	a.searchDone(searchDoneMsg{ask: a.search.ask, hits: hits})
	if len(a.search.hits) == 0 {
		t.Fatal("the results never landed")
	}
	asked := fake.got
	drive(t, a, key("esc"))
	if len(a.search.hits) != 0 || !a.search.open {
		t.Fatalf("esc left %d hits, open %v — the first esc clears the box", len(a.search.hits), a.search.open)
	}
	if fake.got != asked {
		t.Fatalf("clearing the box read the store for %q", fake.got)
	}
	drive(t, a, key("esc"))
	if a.search.open {
		t.Fatal("the second esc did not leave the place")
	}
}

// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. With no index behind it
// the place keeps saying what it is for rather than drawing an empty result
// list under somebody's words.
func TestTheSearchPlaceWithNoIndexSaysWhatItIsForAndNothingElse(t *testing.T) {
	a := placeApp(t)
	a.showPage(pageSearch)
	typeInto(t, a, "report")
	if cmd := a.searchTick(searchTickMsg{gen: a.search.ask.gen}); cmd != nil {
		t.Fatal("a surface with no index sent a read anyway")
	}
	if a.search.waiting {
		t.Fatal("a surface with no index is still waiting on an answer")
	}
}
