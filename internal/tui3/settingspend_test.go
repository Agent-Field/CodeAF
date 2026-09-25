package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

// ── THE SPENDING TAB, AND EVERY DOOR ONTO IT ────────────────────────────────
//
// docs/design/spending/DESIGN.md's acceptance list, one test per point. The
// design's own sentence is the thing under test: money has ONE EDITOR and MANY
// DOORS, and no door is a second editor.

// spendingSheet opens the panel on the Spending tab.
func spendingSheet(t *testing.T) (*app, string) {
	t.Helper()
	a, dir := sheetApp(t)
	a.openSettings()
	a.raiseSettings()
	a.sheet.tab = spendingTabIndex()
	a.sheet.build()
	return a, dir
}

// spendingRows is the tab's rows as a reader meets them: the label of each,
// heading lines and the description under the cursor left out.
func spendingRows(a *app) []string {
	out := make([]string, 0, len(a.sheet.items))
	for _, item := range a.sheet.items {
		switch {
		case item.heading():
		case item.read != nil:
			out = append(out, item.read.name)
		default:
			out = append(out, item.meta.label)
		}
	}
	return out
}

// ── 1 ───────────────────────────────────────────────────────────────────────

// TestTheSpendingTabIsTheSevenRowsAndNothingElse. The tab answers ONE question
// and the rows are in the order a person worries about them — not alphabetical,
// not registry order, not by key.
func TestTheSpendingTabIsTheSevenRowsAndNothingElse(t *testing.T) {
	a, _ := spendingSheet(t)
	// A day with something on it, so the receipt row is drawn at all.
	a.dayCost, a.dayCosted = 3.42, true
	a.sheet.today = a.todayReading()
	a.sheet.build()
	want := []string{spendTodayWord, "per day", "per conversation", "per plan",
		"per task", "per standing run", "practice"}
	if got := spendingRows(a); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("the Spending tab reads\n  %v\nwant\n  %v", got, want)
	}
	for _, width := range []int{80, 160} {
		a.width = width
		frame := strings.Join(sheetLabels(a), "\n")
		for _, row := range want {
			if !strings.Contains(frame, row) {
				t.Fatalf("row %q is missing from the %d-column frame:\n%s", row, width, frame)
			}
		}
	}
}

// TestSafetyAndTasksHoldWhatSpendingLetGoAndWorkspaceNamesNoMoney is the other
// half of the split: twenty rows answering four questions became four sections,
// and the tab a person opens for money holds money alone.
func TestSafetyAndTasksHoldWhatSpendingLetGoAndWorkspaceNamesNoMoney(t *testing.T) {
	a, _ := spendingSheet(t)
	where := map[string]string{}
	for _, row := range a.sheet.rows {
		if meta, ok := settingMetaFor(row); ok {
			where[row.Key] = meta.tab
		}
	}
	want := map[string]string{
		config.KeyToolApprovalMode:    tabSafety,
		config.KeyBashApprovals:       tabSafety,
		config.KeyGuardian:            tabSafety,
		config.KeyConsentTimeout:      tabSafety,
		config.KeyBashBackgroundAfter: tabSafety,
		config.KeyTaskSettle:          tabSafety,
		config.KeyTaskAutoApprove:     tabSafety,
		config.KeyTaskStart:           tabTasks,
		config.KeyTaskAudit:           tabTasks,
		config.KeyTaskRepairRounds:    tabTasks,
		config.KeyTaskParallel:        tabTasks,
		config.KeyTaskMaxLoad:         tabTasks,
		config.KeyTaskMinFreeMB:       tabTasks,
		config.KeyTaskModel:           tabTasks,
		config.KeyDailyBudget:         tabSpending,
		config.KeyPlanConsent:         tabSpending,
		config.KeyPracticeBudget:      tabSpending,
		config.KeySpendRail:           tabSpending,
	}
	for key, tab := range want {
		if where[key] != tab {
			t.Errorf("%s is drawn on %q, want %q", key, where[key], tab)
		}
	}
	// THE THREE NEW TABS ARE THE REGISTRY'S THREE CATEGORIES, ONE TO ONE. A row
	// filed under one and drawn under another is two answers to one question.
	for _, row := range a.sheet.rows {
		meta, ok := settingMetaFor(row)
		if !ok {
			continue
		}
		if category, mapped := settingTabCategory[meta.tab]; mapped && row.Category != category {
			t.Errorf("%s is drawn on %q and filed under %q", row.Key, meta.tab, row.Category)
		}
		if tab, mapped := tabForCategory(row.Category); mapped && meta.tab != tab {
			t.Errorf("%s is filed under %q and drawn on %q", row.Key, row.Category, meta.tab)
		}
	}
	// AND NOTHING ON WORKSPACE NAMES MONEY.
	for _, row := range a.sheet.rows {
		if meta, ok := settingMetaFor(row); ok && meta.tab == tabWorkspace && row.Kind == config.SettingDollars {
			t.Errorf("%s is a dollar figure left on Workspace", row.Key)
		}
	}
	// AND THE BAR READS IN THE DESIGN'S ORDER.
	want2 := []string{tabSession, tabContext, tabWorkspace, tabDisplay,
		tabSpending, tabSafety, tabTasks, tabTeams, tabProviders, tabConnections}
	if strings.Join(settingTabs, " ") != strings.Join(want2, " ") {
		t.Fatalf("the tab bar reads %v", settingTabs)
	}
}

// tabForCategory is the map read backwards, for the test above.
func tabForCategory(category string) (string, bool) {
	for tab, mapped := range settingTabCategory {
		if mapped == category {
			return tab, true
		}
	}
	return "", false
}

// ── 2 ───────────────────────────────────────────────────────────────────────

// TestTodayIsAReceiptAndNotASetting. It is where the eye lands and it answers
// the first question before anybody edits anything — and the cursor steps over
// it, because there is nothing enter could do to a fact.
func TestTodayIsAReceiptAndNotASetting(t *testing.T) {
	a, dir := spendingSheet(t)
	// Nothing counted: the row is not there at all, rather than $0.00 of $500.
	if got := spendingRows(a); len(got) > 0 && got[0] == spendTodayWord {
		t.Fatalf("a day nothing has counted still drew a today row: %v", got)
	}
	a.dayCost, a.dayCosted = 3.42, true
	a.sheet.today = a.todayReading()
	a.sheet.build()
	first := a.sheet.items[0]
	if first.read == nil || first.read.name != spendTodayWord {
		t.Fatal("today must lead the tab")
	}
	if first.restful() {
		t.Fatal("today is a receipt — the cursor may not rest on it")
	}
	if got := first.read.value.full; !strings.Contains(got, "$3.42 of $500") ||
		!strings.Contains(got, "resets at midnight") {
		t.Fatalf("today reads %q, want the day's spend of the day's limit", got)
	}
	// A machine with no daily limit reads the word rather than a fraction with
	// nothing under the line.
	if err := mustSpendRow(t, a, config.KeyDailyBudget).Apply("none"); err != nil {
		t.Fatal(err)
	}
	a.settings = nil
	a.raiseSettings()
	a.sheet.tab = spendingTabIndex()
	a.dayCost, a.dayCosted = 3.42, true
	if got := a.todayReading().value.full; got != "$3.42 · no limit" {
		t.Fatalf("today with no limit reads %q", got)
	}
	_ = dir
	// AND THE CURSOR OPENS ON THE FIRST ROW A PERSON CAN TURN.
	a.sheet.today = a.todayReading()
	a.sheet.build()
	a.sheet.cursorTo(spendTodayKey)
	item, ok := a.sheet.current()
	if !ok || item.row.Key != config.KeyDailyBudget {
		t.Fatalf("a door onto today lands the cursor on %v, want per day", item.meta.label)
	}
}

func mustSpendRow(t *testing.T, a *app, key string) config.Setting {
	t.Helper()
	row, ok := a.registry().Row(key)
	if !ok {
		t.Fatalf("row %q is not registered", key)
	}
	return row
}

// ── 3 ───────────────────────────────────────────────────────────────────────

// TestNoLimitIsAWordAndZeroIsNeverDrawn. Every spelling a person reaches for
// lands the same value, and the row then reads the word — never `$0`, which is
// the emptiness law broken exactly where it matters.
func TestNoLimitIsAWordAndZeroIsNeverDrawn(t *testing.T) {
	a, _ := spendingSheet(t)
	rows := map[string]string{
		config.KeyDailyBudget:    config.NoLimitWord,
		config.KeySpendRail:      config.NoLimitWord,
		config.KeyPlanConsent:    "never asks",
		config.KeyPracticeBudget: "practice off",
	}
	for key, word := range rows {
		for _, typed := range []string{"none", "no", "off", "unlimited", "∞", "0", "$0"} {
			if err := mustSpendRow(t, a, key).Apply(typed); err != nil {
				t.Fatalf("%s refused %q: %v", key, typed, err)
			}
			if got := mustSpendRow(t, a, key).Value(); got != word {
				t.Fatalf("%s reads %q after %q, want %q", key, got, typed, word)
			}
		}
	}
	a.sheet.build()
	frame := strings.Join(sheetLabels(a), "\n")
	if strings.Contains(frame, "$0") {
		t.Fatalf("a bare zero reached the Spending tab:\n%s", frame)
	}
	// AND NEITHER DOES A SUB-CENT LIMIT ROUND ITSELF INTO ONE. A tenth of a cent
	// written as `$0.00` is the figure a person typed rendered as its opposite.
	if got := railFigure(0.0001); got != "$0.0001" {
		t.Fatalf("a sub-cent limit reads %q", got)
	}
	if err := mustSpendRow(t, a, config.KeySpendRail).Apply("0.0001"); err != nil {
		t.Fatal(err)
	}
	a.sheet.build()
	if got := a.sheet.spendNote(itemFor(t, a, config.KeySpendRail), 160, false); !strings.Contains(got, "$0.0001") {
		t.Fatalf("the conversation row reads %q for a tenth of a cent", got)
	}
	for _, word := range rows {
		if !strings.Contains(frame, word) {
			t.Fatalf("%q never reached the tab:\n%s", word, frame)
		}
	}
	// AND THE EDIT BOX OPENS EMPTY on a row that holds nothing, so a person is
	// offered a place to type a number rather than a sentence to delete first.
	a.sheet.cursorTo(config.KeyDailyBudget)
	a.activate()
	if a.sheet.edit == nil {
		t.Fatal("enter on a money row opens the box")
	}
	if got := a.sheet.edit.box.String(); got != "" {
		t.Fatalf("the box opened on %q, want nothing", got)
	}
}

// ── 4 ───────────────────────────────────────────────────────────────────────

// TestARowsReceiptIsThereWhenItIsKnownAndAbsentWhenItIsNot. A receipt is a live
// fact beside a value and never a second value; unknown draws nothing at all.
func TestARowsReceiptIsThereWhenItIsKnownAndAbsentWhenItIsNot(t *testing.T) {
	a, _ := spendingSheet(t)
	a.dayCost, a.dayCosted = 4.25, true
	a.cost = 0.41
	if got := mustSpendRow(t, a, config.KeyDailyBudget).Receipt(); got != "$4.25 today" {
		t.Fatalf("the day's receipt = %q", got)
	}
	if got := mustSpendRow(t, a, config.KeySpendRail).Receipt(); got != "this one $0.41" {
		t.Fatalf("the conversation's receipt = %q", got)
	}
	// Nothing counted is not zero counted.
	a.dayCosted, a.cost = false, 0
	if got := mustSpendRow(t, a, config.KeyDailyBudget).Receipt(); got != "" {
		t.Fatalf("an uncounted day invented %q", got)
	}
	if got := mustSpendRow(t, a, config.KeySpendRail).Receipt(); got != "" {
		t.Fatalf("a conversation that spent nothing invented %q", got)
	}
	// And the two readings carry theirs.
	if got := taskReading(t.TempDir()).receipt.full; !strings.Contains(got, "set in /crew") {
		t.Fatalf("per task's receipt = %q", got)
	}
	if got := standingReading().value.full; got != "$5 a firing" {
		t.Fatalf("per standing run reads %q", got)
	}
}

// ── 5 ───────────────────────────────────────────────────────────────────────

// TestEveryDoorLandsOnTheSameEditor. Four ways of asking one question, and none
// of them is a second answer to it.
func TestEveryDoorLandsOnTheSameEditor(t *testing.T) {
	// /budget, bare and with a figure and with a row named.
	a, dir := sheetApp(t)
	a.budget("")
	if !a.at(pageSettings) || settingTabs[a.sheet.tab] != tabSpending {
		t.Fatalf("/budget opens %v", settingTabs[a.sheet.tab])
	}
	if item, ok := a.sheet.current(); !ok || item.row.Key != config.KeyDailyBudget {
		t.Fatal("/budget lands on per day")
	}
	a.budget("50")
	if rail, _ := config.DailyBudgetUSDAt(dir); rail != 50 {
		t.Fatalf("/budget 50 wrote %v", rail)
	}
	a.budget("none")
	if rail, _ := config.DailyBudgetUSDAt(dir); rail != 0 {
		t.Fatalf("/budget none wrote %v", rail)
	}
	a.budget("plan 20")
	if plan, _ := config.PlanConsentUSDAt(dir); plan != 20 {
		t.Fatalf("/budget plan 20 wrote %v", plan)
	}
	a.budget("conversation 5")
	if rail := config.SpendRailUSDAt(dir); rail != 5 {
		t.Fatalf("/budget conversation 5 wrote %v", rail)
	}
	// A row named with no figure is a question, and the answer is the row.
	a.budget("practice")
	if item, ok := a.sheet.current(); !ok || item.row.Key != config.KeyPracticeBudget {
		t.Fatal("/budget practice lands on the practice row")
	}
	// A TASK IS NOT A ROW, because there is no per-task rail to write.
	if _, ok := budgetRowFor("task"); ok {
		t.Fatal("/budget must not accept a row nothing reads")
	}
}

// TestTheMoneySegmentIsADoorOntoTheTab. The figure a person is looking at when
// they decide it is too high is the thing they press.
func TestTheMoneySegmentIsADoorOntoTheTab(t *testing.T) {
	a, _ := sheetApp(t)
	a.cost = 0.14
	a.legend(a.width)
	if a.moneySpan.from == 0 && a.moneySpan.to == 0 {
		t.Fatal("the layout never recorded where the money segment landed")
	}
	// The set that lights is the set the press acts on.
	x := a.moneySpan.from
	if !a.moneySpan.holds(x) {
		t.Fatalf("the span %v does not hold its own first column", a.moneySpan)
	}
	if cmd := a.openSpending(spendTodayKey); cmd == nil && !a.at(pageSettings) {
		t.Fatal("the door opens the panel")
	}
	if settingTabs[a.sheet.tab] != tabSpending {
		t.Fatalf("the money segment opened %v", settingTabs[a.sheet.tab])
	}
}

// ── 6 ───────────────────────────────────────────────────────────────────────
//
// The trip line itself is pinned where it is worded (internal/session's
// rail_test.go). What this side owes is that the door it names is a door: a
// person reading the refusal is standing over the message box, and `/budget` is
// what that box takes.

func TestTheDoorTheTripLineNamesIsRealFromTheBox(t *testing.T) {
	a, _ := sheetApp(t)
	if a.slash("/budget"); !a.at(pageSettings) || settingTabs[a.sheet.tab] != tabSpending {
		t.Fatal("the refusal names /budget and /budget must open the tab")
	}
	if canonicalCommand("limits") != "budget" {
		t.Fatal("/limits is the same door")
	}
}

// ── 7 ───────────────────────────────────────────────────────────────────────
//
// The controls screen is pinned in firstrun_test.go, which walks the whole flow.
// What this test owns is the design's own claim about it: what it writes is
// BYTE-IDENTICAL to what the settings row writes, because it is the same writer.

func TestTheControlsScreenWritesWhatTheSettingsRowWrites(t *testing.T) {
	a, dir := sheetApp(t)
	a.setup = setupFlow{open: true, steps: []setupStep{setupControls}}
	a.setup.limitText, a.setup.limitTyped = "42", true
	if !a.commitSetupLimit() {
		t.Fatalf("the controls screen refused a figure: %s", a.setup.refusal)
	}
	through := profileBytes(t, dir)
	if err := mustSpendRow(t, a, config.KeyDailyBudget).Apply("42"); err != nil {
		t.Fatal(err)
	}
	if got := profileBytes(t, dir); got != through {
		t.Fatalf("the screen wrote\n%s\nand the row wrote\n%s", through, got)
	}
	// AND NO LIMIT IS A FIRST-CLASS ANSWER. The screen's own legend offers `none`,
	// and what it writes is the zero every reader of the rail resolves to.
	a.setup.limitText, a.setup.limitTyped = setupNoneWord, true
	if !a.commitSetupLimit() {
		t.Fatalf("the controls screen refused none: %s", a.setup.refusal)
	}
	if rail, err := config.DailyBudgetUSDAt(dir); err != nil || rail != 0 {
		t.Fatalf("none on the day's limit wrote %v (%v), want no limit", rail, err)
	}
}

// ── 8 ───────────────────────────────────────────────────────────────────────

// TestAtSixtyColumnsEveryLabelSurvivesAndTheReceiptGoesFirst is rowfit's law 1
// and law 3 read on this tab: the label is the identity and stays whole, and
// the facts degrade before they disappear.
func TestAtSixtyColumnsEveryLabelSurvivesAndTheReceiptGoesFirst(t *testing.T) {
	a, _ := spendingSheet(t)
	a.dayCost, a.dayCosted, a.cost = 3.42, true, 0.41
	a.sheet.today = a.todayReading()
	a.sheet.build()
	wide := a.sheet.spendNote(itemFor(t, a, config.KeySpendRail), 160, false)
	if !strings.Contains(wide, "this one $0.41") {
		t.Fatalf("a wide row keeps its receipt, got %q", wide)
	}
	// AND THE RECEIPT IS THE FIRST THING OFF THE ROW. Squeezed until only one
	// fact can stand, the row keeps the VALUE — what the limit is — and drops the
	// receipt whole rather than cutting a figure in half.
	narrow := a.sheet.spendNote(itemFor(t, a, config.KeySpendRail), 40, false)
	if strings.Contains(narrow, "this one") {
		t.Fatalf("a squeezed row must drop the receipt whole, got %q", narrow)
	}
	if !strings.Contains(narrow, config.NoLimitWord) {
		t.Fatalf("a squeezed row keeps its value, got %q", narrow)
	}
	a.width = 60
	for _, line := range sheetLabels(a) {
		if strings.Contains(line, "…") && strings.Contains(line, "per ") {
			t.Fatalf("a label was cut at sixty columns: %q", line)
		}
	}
	for _, want := range []string{"per day", "per conversation", "per plan", "per task", "practice"} {
		if !sheetHas(a, want) {
			t.Fatalf("%q did not survive sixty columns", want)
		}
	}
	// AND EVERY VALUE IS STILL READABLE AT SIXTY.
	frame := strings.Join(sheetLabels(a), "\n")
	for _, want := range []string{config.NoLimitWord, "$500", "$5 a task"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("%q did not survive sixty columns:\n%s", want, frame)
		}
	}
}

func itemFor(t *testing.T, a *app, key string) sheetItem {
	t.Helper()
	for _, item := range a.sheet.items {
		if item.restful() && item.row.Key == key {
			return item
		}
	}
	t.Fatalf("row %q is not on the tab", key)
	return sheetItem{}
}

// profileBytes is the profile file as it sits on disk — the only honest way to
// ask whether two writers landed the same thing.
func profileBytes(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("the profile was never written: %v", err)
	}
	return string(raw)
}

// ── A REAL AMOUNT NEVER DRAWS AS ZEROS ──────────────────────────────────────

// THE OPPOSITE OF THE EMPTINESS LAW'S FAILURE. That law forbids drawing a zero
// for something unknown; this was a known, positive, spent amount drawn as
// `$0.0000` — four zeros, on a surface that has taught every reader that a zero
// means nothing happened. `dollars` and `railFigure` now share one sub-cent rule
// ([subCent]) with a floor under it, so the smallest thing either can write is
// still a figure and never a row of noughts.
func TestAPositiveCostIsNeverDrawnAsZeros(t *testing.T) {
	for _, usd := range []float64{0.000006, 0.00001, 0.000049, 1e-9} {
		for _, c := range []struct {
			what string
			got  string
		}{
			{"a cost", dollars(usd)},
			{"a limit", railFigure(usd)},
		} {
			if strings.Contains(c.got, "0.0000") && !strings.HasPrefix(c.got, "<") {
				t.Fatalf("%s of %g is drawn %q — a positive amount rendered as its own opposite", c.what, usd, c.got)
			}
			if c.got != "<$0.0001" {
				t.Fatalf("%s of %g is drawn %q, want %q", c.what, usd, c.got, "<$0.0001")
			}
		}
	}
}

// AND EVERY FIGURE ABOVE THE FLOOR IS UNTOUCHED, which is what makes the change
// safe to make in the one function every price on this surface goes through:
// the only readings that move are the ones that used to be a lie.
func TestTheMoneyFloorMovesNoFigureAboveIt(t *testing.T) {
	for _, c := range []struct {
		usd  float64
		want string
	}{
		{0, "$0.00"},
		{0.0001, "$0.0001"},
		{0.0004, "$0.0004"},
		{0.0052, "$0.0052"},
		{0.01, "$0.01"},
		{1.63, "$1.63"},
		{123.456, "$123.46"},
	} {
		if got := dollars(c.usd); got != c.want {
			t.Fatalf("%g is drawn %q, want %q", c.usd, got, c.want)
		}
	}
	// The limit's own spelling is unmoved too: whole dollars when whole.
	if got := railFigure(500); got != "$500" {
		t.Fatalf("a whole limit is drawn %q, want %q", got, "$500")
	}
	if got := railFigure(0.0004); got != "$0.0004" {
		t.Fatalf("a tenth-of-a-cent limit is drawn %q, want %q", got, "$0.0004")
	}
}
