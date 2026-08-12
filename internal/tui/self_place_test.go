package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// selfCraftCommander is the optional craft capability, faked. Self must open
// with or without it, so the tests exercise both.
type selfCraftCommander struct {
	*fakeCommander
	crafts  []CraftSummary
	details map[string]CraftDetail
	asked   []string
}

func (c *selfCraftCommander) Crafts() ([]CraftSummary, error) {
	return append([]CraftSummary(nil), c.crafts...), nil
}

func (c *selfCraftCommander) CraftDetail(name string) (CraftDetail, bool) {
	c.asked = append(c.asked, name)
	detail, ok := c.details[name]
	return detail, ok
}

func newSelfCraftCommander(now time.Time) *selfCraftCommander {
	return &selfCraftCommander{
		fakeCommander: &fakeCommander{current: map[string]string{}},
		crafts: []CraftSummary{{
			Name: "presentation", Description: "build a deck from notes",
			Commit: "abcdef1234567890", When: now.Add(-48 * time.Hour), Steps: 3,
		}},
		details: map[string]CraftDetail{"presentation": {
			Name: "presentation", Description: "build a deck from notes",
			Commit: "abcdef1234567890", Dir: "/brain/craft",
			CostUSD: 2.5, WallClock: 30 * time.Minute,
			Steps: []CraftStep{
				{ID: "outline", Brief: "Draft the outline from the notes."},
				{ID: "slides", Brief: "Write the slides.", Needs: []string{"outline"}},
				{ID: "check", Verify: "verifiers/deck.sh", Needs: []string{"slides"}},
			},
			History: []CraftVersion{
				{Commit: "abcdef1234567890", When: now.Add(-48 * time.Hour), Subject: "presentation: fewer slides, more evidence"},
				{Commit: "0123456789abcdef", When: now.Add(-300 * time.Hour), Subject: "presentation: first shape"},
			},
		}},
	}
}

func openedSelf(t *testing.T, now time.Time, commander Commander) *Model {
	t.Helper()
	backend := seededSelfBackend(now)
	var model *Model
	if commander == nil {
		model = New(backend, "self")
	} else {
		model = NewWithCommander(backend, "self", commander)
	}
	model.standingNow = func() time.Time { return now }
	model.setSize(110, 34)
	poll := model.selectPlace(placeSelf)
	if poll == nil {
		t.Fatal("opening Self should request its data in the ordinary poll")
	}
	model.applyPoll(poll().(pollResultMsg))
	model.refreshSelf()
	return model
}

func selfView(model *Model) string { return ansi.Strip(model.View()) }

func selfNow() time.Time { return time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local) }

func TestSelfRootListShowsCountsExplainersAndTheTodayLine(t *testing.T) {
	now := selfNow()
	model := openedSelf(t, now, newSelfCraftCommander(now))
	view := selfView(model)

	for _, want := range []string{
		"today: $0.43", "1 learned",
		"Know-how (1)", "what I've learned to repeat",
		"Competence (3)", "where I'm strong",
		"Beliefs (1)", "what I hold true about you",
		"Skills (1)", "tools I built and checked",
		"Watches (1)", "standing goals checking on their own schedule",
		"Services (1)", "processes I keep alive for you",
		"Practice", "what I did with idle time",
		"Dials", "govern what I do with my own time",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("Self root list is missing %q:\n%s", want, view)
		}
	}
}

func TestSelfZeroStateRowsStillTeachWhatTheyWouldHold(t *testing.T) {
	model := New(&fakeBackend{}, "self")
	model.setSize(110, 34)
	_ = model.selectPlace(placeSelf)
	view := selfView(model)
	for _, want := range []string{
		"Know-how (0)", "I keep one when a job's shape looks worth repeating",
		"Beliefs (0)", "I write one down when work teaches me",
		"Skills (0)", "run and pass twice",
		"Watches (0)", "say \"whenever…\"",
		"Services (0)", "I start one when work needs something to stay up",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("empty Self root list is missing %q:\n%s", want, view)
		}
	}
}

func TestSelfDrillInAndEscapeRoundTripReturnsToTheSameRow(t *testing.T) {
	now := selfNow()
	model := openedSelf(t, now, newSelfCraftCommander(now))
	model.selfSelection = selfRootIndex(selfRouteBeliefs)
	model.refreshSelf()
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.selfRoute != selfRouteBeliefs {
		t.Fatalf("enter on the beliefs row opened route %d", model.selfRoute)
	}
	view := selfView(model)
	if !strings.Contains(view, "‹ self · Beliefs") {
		t.Fatalf("the beliefs drill-in has no way back in its header:\n%s", view)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.selfRoute != selfRouteRoot {
		t.Fatalf("esc did not return to the root list: route=%d", model.selfRoute)
	}
	if model.selfSelection != selfRootIndex(selfRouteBeliefs) {
		t.Fatalf("esc lost the row it came from: selection=%d", model.selfSelection)
	}
}

func TestSelfCraftDrillInShowsStepsLimitsAndHistory(t *testing.T) {
	now := selfNow()
	commander := newSelfCraftCommander(now)
	model := openedSelf(t, now, commander)
	model.openSelfRoute(selfRouteCrafts)
	list := selfView(model)
	for _, want := range []string{"presentation", "3 steps", "changed 2d", "abcdef1"} {
		if !strings.Contains(list, want) {
			t.Fatalf("the craft list is missing %q:\n%s", want, list)
		}
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.selfCraftName != "presentation" {
		t.Fatalf("enter did not open the craft: %q", model.selfCraftName)
	}
	detail := selfView(model)
	for _, want := range []string{
		"steps", "outline", "slides", "needs outline", "verify verifiers/deck.sh",
		"limits", "≤$2.50 per run", "30m wall clock",
		"history", "presentation: fewer slides, more evidence", "presentation: first shape",
		"kept in /brain/craft",
	} {
		if !strings.Contains(detail, want) {
			t.Fatalf("the craft detail is missing %q:\n%s", want, detail)
		}
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.selfCraftName != "" || model.selfRoute != selfRouteCrafts {
		t.Fatalf("esc from a craft did not return to the craft list: name=%q route=%d",
			model.selfCraftName, model.selfRoute)
	}
}

func TestSelfCraftRowCountsRunsAndSurvivalFromTheGraph(t *testing.T) {
	now := selfNow()
	commander := newSelfCraftCommander(now)
	model := openedSelf(t, now, commander)
	model.cardSnapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "run-1", Parent: store.RootID, Status: store.Done,
			Provenance: store.Provenance{Craft: "presentation@abcdef1234567890"}},
		{ID: "run-1-step", Parent: "run-1", Status: store.Done,
			Provenance: store.Provenance{Craft: "presentation@abcdef1234567890"}},
		{ID: "run-2", Parent: store.RootID, Status: store.Failed,
			Provenance: store.Provenance{Craft: "presentation@abcdef1234567890"}},
	}}
	model.openSelfRoute(selfRouteCrafts)
	view := selfView(model)
	if !strings.Contains(view, "2 runs") || !strings.Contains(view, "1/2 survived") {
		t.Fatalf("craft survival is not read from the graph's provenance:\n%s", view)
	}
}

func TestSelfBeliefsFoldOldOnesAndTypingSearches(t *testing.T) {
	now := selfNow()
	backend := seededSelfBackend(now)
	backend.facts = []store.Fact{
		{Seq: 21, Time: now.Add(-time.Hour), Scope: "repo:/parser", Kind: store.FactLesson,
			Body: "Retry malformed records one at a time.", Status: store.FactActive},
		{Seq: 9, Time: now.Add(-40 * 24 * time.Hour), Scope: "user", Kind: store.FactPreference,
			Body: "Ship the smallest correct change.", Status: store.FactActive},
	}
	commander := newSelfCraftCommander(now)
	model := NewWithCommander(backend, "self", commander)
	model.standingNow = func() time.Time { return now }
	model.setSize(110, 34)
	poll := model.selectPlace(placeSelf)
	model.applyPoll(poll().(pollResultMsg))
	model.openSelfRoute(selfRouteBeliefs)

	view := selfView(model)
	if !strings.Contains(view, "Retry malformed records one at a time.") {
		t.Fatalf("this week's belief is not shown:\n%s", view)
	}
	if strings.Contains(view, "Ship the smallest correct change.") {
		t.Fatalf("a forty-day-old belief was not folded away:\n%s", view)
	}
	if !strings.Contains(view, "1 older · type to search") {
		t.Fatalf("the fold does not say what it is holding:\n%s", view)
	}

	commander.facts = []store.Fact{backend.facts[1]}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ship")})
	if commander.searchTerms != "ship" {
		t.Fatalf("typing did not reach the notebook's own search: %q", commander.searchTerms)
	}
	searched := selfView(model)
	if !strings.Contains(searched, "filter · ship") {
		t.Fatalf("the filter is not shown while it is applied:\n%s", searched)
	}
	if !strings.Contains(searched, "Ship the smallest correct change.") {
		t.Fatalf("search did not reach past the fold:\n%s", searched)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.selfQuery != "" || model.selfRoute != selfRouteBeliefs {
		t.Fatalf("esc should clear the filter before it leaves: query=%q route=%d",
			model.selfQuery, model.selfRoute)
	}
}

func TestSelfListsWindowAndSayHowMuchTheyAreHolding(t *testing.T) {
	now := selfNow()
	backend := seededSelfBackend(now)
	backend.facts = nil
	for index := 0; index < 45; index++ {
		backend.facts = append(backend.facts, store.Fact{
			Seq: int64(100 + index), Time: now.Add(-time.Duration(index) * time.Minute),
			Scope: "user", Kind: store.FactPlain, Status: store.FactActive,
			Body: "belief number " + string(rune('a'+index%26)),
		})
	}
	model := NewWithCommander(backend, "self", newSelfCraftCommander(now))
	model.standingNow = func() time.Time { return now }
	model.setSize(110, 60)
	poll := model.selectPlace(placeSelf)
	model.applyPoll(poll().(pollResultMsg))
	model.openSelfRoute(selfRouteBeliefs)

	view := selfView(model)
	if !strings.Contains(view, "45 · showing 20") {
		t.Fatalf("the belief list does not say how much it is holding:\n%s", view)
	}
	if !strings.Contains(view, "show 20 more") {
		t.Fatalf("the belief list has no way to see the rest:\n%s", view)
	}
	model.selfSelection = len(model.selfRows) - 1
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(selfView(model), "45 · showing 40") {
		t.Fatalf("showing more did not widen the window:\n%s", selfView(model))
	}
}

func TestSelfClickDrillsInLikeEnterDoes(t *testing.T) {
	now := selfNow()
	model := openedSelf(t, now, newSelfCraftCommander(now))
	_ = model.View()
	row := model.selfRows[selfRootIndex(selfRouteSkills)]
	_, _ = model.updateMouseClick(2, model.selfBounds.y+row.line-model.self.YOffset)
	if model.selfRoute != selfRouteSkills {
		t.Fatalf("clicking the skills row opened route %d", model.selfRoute)
	}
	if !strings.Contains(selfView(model), "csvsplit") {
		t.Fatalf("the skills drill-in does not show the forged skill:\n%s", selfView(model))
	}
}

func TestSelfWatchAndServiceRowsHandOffToTheBoardThatOwnsThem(t *testing.T) {
	now := selfNow()
	model := openedSelf(t, now, newSelfCraftCommander(now))
	model.openSelfRoute(selfRouteWatches)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.activePlace() != placeBoard || model.charterCardID != "parser-watch" {
		t.Fatalf("enter on a watch did not open its card on the board: place=%v card=%q",
			model.activePlace(), model.charterCardID)
	}

	model = openedSelf(t, now, newSelfCraftCommander(now))
	model.openSelfRoute(selfRouteServices)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.activePlace() != placeBoard || model.serviceCardID != "svc-1" {
		t.Fatalf("enter on a service did not open its card on the board: place=%v card=%q",
			model.activePlace(), model.serviceCardID)
	}
}

func TestSelfDialsAreReadOnlyAndPointAtTheSheet(t *testing.T) {
	now := selfNow()
	commander := &settingsCommander{fakeCommander: newFakeCommander()}
	commander.registry = config.NewSettings(config.SettingsOptions{
		ProfileDir: t.TempDir(),
		ModelValue: commander.CurrentModel, SetModel: commander.SetModel,
		SplitPct: commander.SplitPct, SaveSplitPct: commander.SaveSplitPct,
	})
	model := openedSelf(t, now, commander)
	model.openSelfRoute(selfRouteDials)
	view := selfView(model)
	// The two dials this used to name — demand vs curiosity, propose new
	// skills — are gone with the loops that never read them. The dials page
	// projects whatever the practice group holds, so it names those rows now
	// and keeps naming whatever lands there next.
	for _, want := range []string{
		"quiet before practice", "tenure after", "change these in settings (⚙)",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("the dials view is missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "enter change") {
		t.Fatalf("the dials view offered an editor it does not own:\n%s", view)
	}
}

func TestSelfRendersAtNarrowWidthWithoutLosingItsExplainers(t *testing.T) {
	now := selfNow()
	model := openedSelf(t, now, newSelfCraftCommander(now))
	model.setSize(46, 24)
	view := selfView(model)
	for _, want := range []string{"Know-how (1)", "what I've learned to repeat"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the narrow Self root list is missing %q:\n%s", want, view)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if len([]rune(line)) > 46 {
			t.Fatalf("a narrow Self line overflowed the frame: %q", line)
		}
	}
	model.openSelfRoute(selfRouteCrafts)
	for _, line := range strings.Split(selfView(model), "\n") {
		if len([]rune(line)) > 46 {
			t.Fatalf("a narrow craft row overflowed the frame: %q", line)
		}
	}
}

// The old flat Self file rendered four sections. Nothing it showed was allowed
// to disappear when the place became a list, so this is the mapping audit as a
// test: every element, and the route that now holds it.
func TestEveryElementTheOldSelfFileRenderedStillHasAHome(t *testing.T) {
	now := selfNow()
	model := openedSelf(t, now, newSelfCraftCommander(now))
	cases := []struct {
		was   string
		route selfRoute
		want  []string
	}{
		{was: "today · self-spend", route: selfRouteRoot, want: []string{"today: $0.43"}},
		{was: "today · live practice presence", route: selfRoutePractice, want: []string{"Parser practice", "running"}},
		{was: "today · receipt tried/cost/learned", route: selfRoutePractice,
			want: []string{"compare parser recovery", "$0.43", "1 learned"}},
		{was: "competence · classes and failure rates", route: selfRouteCompetence,
			want: []string{"repo:/strong", "strong", "10% failed", "repo:/frontier", "frontier", "50% failed", "repo:/weak", "weak", "80% failed"}},
		{was: "beliefs · recent notebook facts", route: selfRouteBeliefs,
			want: []string{"Retry malformed records one at a time."}},
		{was: "standing · charter, tenure, last fired, today", route: selfRouteWatches,
			want: []string{"Keep parser recovery healthy", "probation 2/3", "last Aug 6 12:00", "1 today"}},
	}
	for _, testCase := range cases {
		model.openSelfRoute(testCase.route)
		view := selfView(model)
		for _, want := range testCase.want {
			if !strings.Contains(view, want) {
				t.Fatalf("%q lost %q on the way to route %d:\n%s", testCase.was, want, testCase.route, view)
			}
		}
	}
}

// The flat receipt log's failure was repetition: one goal tried six times drew
// six full-width copies of the same sentence and nothing could be opened. The
// practice list groups the attempts, keeps real columns, and every row is a
// way in.
func repeatedPracticeBackend(now time.Time) *selfSeedBackend {
	backend := seededSelfBackend(now)
	goal := "learn how to protect my github agentfield repo when a new pr comes in from an outside contributor"
	backend.receipts = nil
	for index := 0; index < 6; index++ {
		backend.receipts = append(backend.receipts, store.SelfReceipt{
			Seq: int64(200 + index), Time: now.Add(-time.Duration(index+1) * time.Hour),
			NodeID: "practice-live", Origin: goal, Scope: "repo:/agentfield", Cost: 0.11,
		})
	}
	backend.receipts = append(backend.receipts, store.SelfReceipt{
		Seq: 300, Time: now.Add(-30 * time.Minute), NodeID: "practice-live",
		Origin: "revision: Audit found the branch protection rule is unset and no reviewers are required",
		Scope:  "repo:/agentfield", Cost: 0.09, FactIDs: []int64{21},
	})
	return backend
}

func openedPractice(t *testing.T, now time.Time, width int) *Model {
	t.Helper()
	model := NewWithCommander(repeatedPracticeBackend(now), "self", newSelfCraftCommander(now))
	model.standingNow = func() time.Time { return now }
	model.setSize(width, 34)
	poll := model.selectPlace(placeSelf)
	model.applyPoll(poll().(pollResultMsg))
	model.openSelfRoute(selfRoutePractice)
	return model
}

func TestPracticeGroupsRepeatedAttemptsIntoOneRow(t *testing.T) {
	now := selfNow()
	model := openedPractice(t, now, 96)
	view := selfView(model)
	if strings.Count(view, "learn how to protect my github") != 1 {
		t.Fatalf("repeated attempts were not grouped into one row:\n%s", view)
	}
	if !strings.Contains(view, "×6") {
		t.Fatalf("the grouped row does not say how many attempts it holds:\n%s", view)
	}
	if !strings.Contains(view, "$0.66") {
		t.Fatalf("the grouped row does not sum the attempts' cost:\n%s", view)
	}
	if !strings.Contains(view, "nothing yet") {
		t.Fatalf("the grouped row does not say what came of it:\n%s", view)
	}
	if !strings.Contains(view, "3 · showing 3") {
		t.Fatalf("the practice list does not count what it is holding:\n%s", view)
	}
}

func TestPracticeReplacesTheRevisionWordWithAGlyphAndClips(t *testing.T) {
	now := selfNow()
	model := openedPractice(t, now, 96)
	view := selfView(model)
	if strings.Contains(view, "revision:") {
		t.Fatalf("a machine prefix leaked into the practice column:\n%s", view)
	}
	if !strings.Contains(view, "↻ Audit found") {
		t.Fatalf("the revision glyph did not replace the word:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if len([]rune(line)) > 96 {
			t.Fatalf("a practice row overflowed the frame: %q", line)
		}
	}
}

func TestPracticeColumnsStayAlignedAtNarrowWidths(t *testing.T) {
	now := selfNow()
	model := openedPractice(t, now, 58)
	view := selfView(model)
	costColumns := map[int]bool{}
	for _, line := range strings.Split(view, "\n") {
		if len([]rune(line)) > 58 {
			t.Fatalf("a narrow practice row overflowed the frame: %q", line)
		}
		if index := strings.Index(line, "$0."); index >= 0 {
			costColumns[index] = true
		}
	}
	if len(costColumns) != 1 {
		t.Fatalf("the cost column is not aligned down the list: %v\n%s", costColumns, view)
	}
}

func TestPracticeRowOpensItsAttempts(t *testing.T) {
	now := selfNow()
	model := openedPractice(t, now, 96)
	for index, row := range model.selfRows {
		if row.action == selfRowOpenPractice && strings.Contains(model.practiceGroups()[index].goal, "github") {
			model.selfSelection = index
		}
	}
	model.refreshSelf()
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.selfPracticeKey == "" {
		t.Fatal("enter on a practice row did not open its attempts")
	}
	detail := selfView(model)
	for _, want := range []string{"attempts", "$0.11", "outcome", "esc back"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("the practice detail is missing %q:\n%s", want, detail)
		}
	}
	// The list column clips; this view is where the whole sentence lands, so
	// its last word has to be on screen.
	if !strings.Contains(detail, "contributor") {
		t.Fatalf("the detail clipped the goal it exists to show in full:\n%s", detail)
	}
	if !strings.Contains(detail, "6 attempts") {
		t.Fatalf("the detail does not name the attempts it gathered:\n%s", detail)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.selfPracticeKey != "" || model.selfRoute != selfRoutePractice {
		t.Fatalf("esc did not return to the practice list: key=%q route=%d",
			model.selfPracticeKey, model.selfRoute)
	}
}

// The receipt table's verbatim "facts #21 · surprise down 25%" clause did not
// vanish when attempts were grouped: it moved one rung in, onto the attempt it
// describes.
func TestOldReceiptLearningClauseMovedIntoTheAttemptDetail(t *testing.T) {
	now := selfNow()
	model := openedSelf(t, now, newSelfCraftCommander(now))
	model.openSelfRoute(selfRoutePractice)
	for index, row := range model.selfRows {
		if row.action == selfRowOpenPractice {
			model.selfSelection = index
			break
		}
	}
	model.refreshSelf()
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	detail := selfView(model)
	for _, want := range []string{"compare parser recovery", "facts #21", "surprise down 25%", "$0.43"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("the attempt detail lost %q:\n%s", want, detail)
		}
	}
}
