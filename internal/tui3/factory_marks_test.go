package tui3

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── ONE OUTPUT, ONE MEANING (factory_marks.go) ──────────────────────────────

// factoryMarkLab is the verb lab over the fixture with shape applied to every
// read, the talk and refresh doors on, at 160 columns.
func factoryMarkLab(t *testing.T, shape func(*factory.Snapshot)) *app {
	t.Helper()
	f := &factoryFake{shape: shape}
	a := factoryVerbLab(t, f)
	a.factory.Talk = func(ctx context.Context, id int) (string, error) { return "", nil }
	a.factory.Refresh = func(ctx context.Context, id int) error { return nil }
	a.width = 160
	return a
}

// factoryMarkItem is the snapshot's item with id, as drawn now.
func factoryMarkItem(t *testing.T, a *app, id int) factory.Item {
	t.Helper()
	it, ok := a.factoryItemByID(id)
	if !ok {
		t.Fatalf("no item %d on the floor", id)
	}
	return it
}

// factoryStageCellText is the item page's rail cell for the stage named name,
// plain.
func factoryStageCellText(t *testing.T, a *app, it factory.Item, name string) string {
	t.Helper()
	for _, v := range a.factoryItemStages(it) {
		if v.stage.Name == name {
			return ansi.Strip(a.factoryStageCell(v).label())
		}
	}
	t.Fatalf("item %d has no stage %q", it.ID, name)
	return ""
}

// A PAUSED ITEM wears the pause mark on its lead and on the phase it holds,
// says `paused 13m` counted from when the floor saw it paused, and never says
// how long the held phase has left or what it was editing.
func TestFactoryMarkAPausedItemIsNotRunning(t *testing.T) {
	a := factoryMarkLab(t, func(s *factory.Snapshot) {
		for i := range s.Items {
			if s.Items[i].ID == 2 {
				s.Items[i].Stream.Paused = true
			}
		}
	})
	it := factoryMarkItem(t, a, 2)
	pause := a.icon(tokens.GPaused)
	if got := ansi.Strip(a.itemLeadMark(it)); got != pause {
		t.Fatalf("a paused item's lead is %q, want the pause mark %q", got, pause)
	}
	if got, _ := a.phaseMark(it, 3); got != pause {
		t.Fatalf("the held phase wears %q, want %q", got, pause)
	}
	if got := phaseWord(it, 3, 2); got != "review 1/2 · paused" {
		t.Fatalf("the held phase says %q", got)
	}
	a.fp.phaseSince[factoryPausedKey(2)] = a.fp.snap.Now.Add(-13 * time.Minute)
	st, _ := a.factoryStateFact(it)
	if !strings.HasSuffix(st.plain, "paused 13m") || strings.Contains(st.plain, a.icon(tokens.GStepRunning)) {
		t.Fatalf("the row's state fact is %q", st.plain)
	}
	line := a.factoryStateLine(it)
	if line != "paused 13m · review 1/2" || strings.Contains(line, "left") || strings.Contains(line, "fixing") {
		t.Fatalf("the peek's state line is %q", line)
	}
	cell := factoryStageCellText(t, a, it, "review")
	if !strings.HasPrefix(cell, pause+" ") || !strings.Contains(cell, "paused") || strings.Contains(cell, "left") {
		t.Fatalf("the item page's rail draws the held stage as %q", cell)
	}
	if got := ansi.Strip(a.factoryItemTitle(it, 150)); !strings.Contains(got, "paused 13m") {
		t.Fatalf("the item page's head does not say it is paused: %q", got)
	}
}

// A STOPPED ITEM wears the stop square, says `stopped · branch kept`, and its
// stopped phase is never the red cross with the failure it was in.
func TestFactoryMarkAStoppedItemIsNotFailed(t *testing.T) {
	a := factoryMarkLab(t, func(s *factory.Snapshot) {
		for i := range s.Items {
			if s.Items[i].ID == 2 {
				it := &s.Items[i]
				it.State = factory.StateNew
				it.Stream.Ended = s.Now
				it.Stream.Phases[3].State = factory.PhaseFailed
				it.Stream.Phases[3].Note = "TestRelay failed"
			}
		}
	})
	it := factoryMarkItem(t, a, 2)
	stop, fail := a.icon(tokens.GStopped), a.icon(tokens.GFailed)
	if got := ansi.Strip(a.itemLeadMark(it)); got != stop {
		t.Fatalf("a stopped item's lead is %q, want %q", got, stop)
	}
	if got, _ := a.phaseMark(it, 3); got != stop {
		t.Fatalf("the stopped phase wears %q, want %q", got, stop)
	}
	if st, _ := a.factoryStateFact(it); st.plain != "stopped · branch kept" {
		t.Fatalf("the row's state fact is %q", st.plain)
	}
	if got := factoryPhaseNoteLine(it, factoryStages(a.fp.snap, it)); got != "review · stopped · branch kept" {
		t.Fatalf("the line under the strip is %q", got)
	}
	strip, _ := a.factoryStripCells(it)
	if strings.Contains(strip, fail) {
		t.Fatalf("the row's strip still draws a failure: %q", strip)
	}
	if seg, _ := a.factoryPhaseCell(it, 3, 2); strings.Contains(ansi.Strip(seg), fail) || !strings.Contains(ansi.Strip(seg), "stopped") {
		t.Fatalf("the peek's cell for the stopped phase is %q", ansi.Strip(seg))
	}
	if cell := factoryStageCellText(t, a, it, "review"); !strings.HasPrefix(cell, stop+" ") {
		t.Fatalf("the item page's rail draws the stopped stage as %q", cell)
	}
}

// A SKIPPED STAGE, switched off or passed over, wears the skip stroke and says
// `skipped`; a stage still to come keeps the pending ring.
func TestFactoryMarkASkippedStageIsNotToCome(t *testing.T) {
	a := factoryMarkLab(t, func(s *factory.Snapshot) {
		for i := range s.Items {
			if s.Items[i].ID == 2 {
				// test was passed over: review runs after it.
				s.Items[i].Stream.Phases[2].State = factory.PhasePending
			}
		}
	})
	skip, pending := a.icon(tokens.GSkipped), a.icon(tokens.GStepPending)
	it := factoryMarkItem(t, a, 2)
	if got, _ := a.phaseMark(it, 2); got != skip {
		t.Fatalf("a passed-over phase wears %q, want %q", got, skip)
	}
	if got := phaseWord(it, 2, 1); got != "test · skipped" {
		t.Fatalf("a passed-over phase says %q", got)
	}
	if got, _ := a.phaseMark(it, 4); got != pending {
		t.Fatalf("a phase still to come wears %q, want %q", got, pending)
	}
	if cell := factoryStageCellText(t, a, it, "test"); cell != skip+" test · skipped" {
		t.Fatalf("the rail draws the passed-over stage as %q", cell)
	}
	if cell := factoryStageCellText(t, a, it, "neaten"); !strings.HasPrefix(cell, pending+" ") {
		t.Fatalf("the rail draws a stage to come as %q", cell)
	}
	// The shipped fixture item had neaten switched off by plan.
	shipped := factoryMarkItem(t, a, 10)
	if cell := factoryStageCellText(t, a, shipped, "neaten"); cell != skip+" neaten · skipped" {
		t.Fatalf("the shipped item's off stage reads %q", cell)
	}
}

// ONLY THE ROW BEING READ SPINS: a row waiting its turn wears a still dim dot
// and `waiting to read`, and neither is the other's mark.
func TestFactoryMarkOnlyTheRowBeingReadSpins(t *testing.T) {
	a := factoryMarkLab(t, func(s *factory.Snapshot) {
		s.Busy = map[int]string{4: factory.BusyRefreshing, 6: factory.BusyWaiting}
		s.BusyAll = "refreshing 2 items · 0 done"
	})
	if !a.factoryRowSpins(4) || a.factoryRowWaits(4) {
		t.Fatal("the row being read does not spin")
	}
	if a.factoryRowSpins(6) || !a.factoryRowWaits(6) {
		t.Fatal("a row waiting its turn spins")
	}
	if a.factoryRowSpins(8) || a.factoryRowWaits(8) {
		t.Fatal("an idle row claims a read")
	}
	if got := ansi.Strip(a.factoryPrioCell(factoryMarkItem(t, a, 6))); got != a.icon(tokens.GSeparator) {
		t.Fatalf("a waiting row's priority cell is %q", got)
	}
	if got := ansi.Strip(a.factoryReadLine(factoryMarkItem(t, a, 6), 60)); got != "waiting to read" {
		t.Fatalf("a waiting item's read line is %q", got)
	}
}

// THE FLIGHT FACT SHOWS AT THE FLOOR'S WIDEST: at 160 columns a row being
// read says `refreshing…` after its state fact, and a narrow row drops it
// first.
func TestFactoryMarkRefreshingFitsAt160(t *testing.T) {
	a := factoryMarkLab(t, func(s *factory.Snapshot) {
		s.Busy = map[int]string{4: factory.BusyRefreshing, 6: factory.BusyWaiting}
	})
	a.fp.columns = true
	width := factoryRowsCols(160)
	for id, want := range map[int]string{4: "refreshing…", 6: "waiting to read"} {
		row := ansi.Strip(a.factoryRailItem(factoryMarkItem(t, a, id), width, false))
		if !strings.Contains(row, want) {
			t.Fatalf("item %d's row at 160 has no %q:\n%q", id, want, row)
		}
	}
	it := factoryMarkItem(t, a, 4)
	st, _ := a.factoryStateFact(it)
	if got := ansi.Strip(a.factoryFactsLine(it, ansi.StringWidth(st.plain)+2, 1)); strings.Contains(got, "refreshing") {
		t.Fatalf("a narrow facts column kept the flight fact over the state: %q", got)
	}
}

// THE FLOOR'S CURSOR ROW IS THE TEAMS PAGE'S CURSOR ROW: the same ground step
// under both, and not the louder selected step.
func TestFactoryMarkCursorGroundMatchesTeams(t *testing.T) {
	a := factoryPlaceLab(t)
	a.fp.columns = true
	ground := regexp.MustCompile(`\x1b\[48;5;(\d+)m`)
	floor := ground.FindStringSubmatch(a.factoryRailItem(*factoryPaneItem(t, a, 2), 150, true))
	target := teamsTarget{act: teamsActSelect, id: "t-1"}
	a.tp.focus, a.tp.cur = true, target.ref()
	teams := ground.FindStringSubmatch((&teamsDraw{a: a}).row("All teams", 40, target, false))
	if floor == nil || teams == nil {
		t.Fatalf("a cursor row has no ground: floor %v teams %v", floor, teams)
	}
	if floor[1] != teams[1] {
		t.Fatalf("the floor's cursor ground is 256-colour %s, the Teams page's is %s", floor[1], teams[1])
	}
	if sel := ground.FindStringSubmatch(a.pal.selected("x", 1)); sel != nil && sel[1] == floor[1] {
		t.Fatalf("the floor's cursor wears the selected step %s", sel[1])
	}
	rail, _ := a.factoryCellRail(a.factoryPageCells(*factoryPaneItem(t, a, 2), a.factoryItemRows(*factoryPaneItem(t, a, 2))), 0, 3)
	if got := ground.FindStringSubmatch(rail[0]); got == nil || got[1] != teams[1] {
		t.Fatalf("the item page's rail cursor is %v, want %s", got, teams[1])
	}
}

// THE PEEK AND THE FOOT ARE ONE LIST: the peek's key line is always a whole-
// clause prefix of the list, in order, and the foot carries the same list in
// the same order. `T chat` is on it wherever the floor can talk.
func TestFactoryMarkPeekAndFootShareOneVerbList(t *testing.T) {
	a := factoryMarkLab(t, nil)
	for _, id := range []int{1, 2, 3, 4, 8, 9} {
		factoryOn(t, a, id)
		it := factoryMarkItem(t, a, id)
		rail := a.factoryVerbRail(it)
		if !strings.Contains(strings.Join(rail, rowSep), "T chat") {
			t.Fatalf("item %d's keys do not name T chat: %q", id, rail)
		}
		full := strings.Join(rail, rowSep)
		for _, w := range []int{20, 40, 65, 90, 400} {
			peek := a.factoryVerbLine(it, w)
			if !strings.HasPrefix(full, peek) || (peek != full && !strings.HasPrefix(full[len(peek):], rowSep)) {
				t.Fatalf("item %d's peek at %d is not a whole prefix of the list:\n%q\n%q", id, w, peek, full)
			}
		}
		// THE FOOT NO LONGER REPEATS THE STRIP (owner decision, 2026-10-08):
		// it is the floor's navigation, and the strip is the row's alone.
		a.width = 1000
		if foot := (placeFactory{}).hint(a); strings.Contains(foot, rail[len(rail)-1]) {
			t.Fatalf("item %d's foot repeats the strip:\n%q\n%q", id, foot, full)
		}
		a.width = 160
	}
}

// AN ANSWER KEY ON AN ITEM ASKING NOTHING SAYS SO and asks no door.
func TestFactoryMarkAnswerKeyOnAnItemNotWaiting(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	factoryOn(t, a, 2)
	drive(t, a, key("y"))
	if a.pageMsg != "#1551 is not waiting on you" {
		t.Fatalf("y on a running item said %q", a.pageMsg)
	}
	if calls := f.said(); len(calls) > 0 {
		t.Fatalf("y on a running item asked %v", calls)
	}
}

// THE READ LINE IS NEVER A BARE KEY: it says when the read was made, that it
// has not been, or what the key does.
func TestFactoryMarkReadLineSaysWhatItKnows(t *testing.T) {
	a := factoryMarkLab(t, nil)
	it := factoryMarkItem(t, a, 4)
	it.Triage.TriagedAt = a.fp.snap.Now.Add(-3 * time.Minute)
	if got := ansi.Strip(a.factoryReadLine(it, 80)); got != "read 3m ago · u refresh" {
		t.Fatalf("a timed read says %q", got)
	}
	it.Triage.TriagedAt, it.Triage.Read = time.Time{}, ""
	if got := ansi.Strip(a.factoryReadLine(it, 80)); got != "not read yet · u refresh" {
		t.Fatalf("an unread item says %q", got)
	}
	it.Triage.Read = "a read"
	if got := ansi.Strip(a.factoryReadLine(it, 80)); got != "u refresh" {
		t.Fatalf("an untimed read says %q", got)
	}
}

// THE ITEM PAGE'S SECOND ROW DOES NOT SAY THE REPO AGAIN: the crumb says it.
func TestFactoryMarkItemHeadNamesOnlyTheOtherRepos(t *testing.T) {
	a := factoryMarkLab(t, nil)
	it := factoryMarkItem(t, a, 2)
	if got := ansi.Strip(a.factoryItemChips(it, 150)); strings.Contains(got, "places") || strings.Contains(got, "codeaf") {
		t.Fatalf("the head's second row repeats the crumb: %q", got)
	}
	it.Places = []string{it.Repo, "agentfield/harness"}
	if got := ansi.Strip(a.factoryItemChips(it, 150)); !strings.Contains(got, "also harness") {
		t.Fatalf("the head's second row does not name the other repo: %q", got)
	}
}
