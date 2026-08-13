package chat

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/command"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The pages (§7), the work board (§6 at page altitude, §5b's two-line row), the
// two bands that moved here from the notebook, and the detail pages behind them.
//
// Every law here is asserted against what a reader actually gets: the frame the
// shell would put on the wire, and the doors a hand or a keyboard can reach. A
// tab that swapped a field without swapping the lens, or a board that knew about
// a job it did not draw, would pass a state test and fail a person.

// -- the fixture -------------------------------------------------------------

// ledgerBoard is [boardBackend] plus every optional read the work page takes:
// the wider job corpus the history section needs, the charters behind
// `watching`, and the services behind `services`.
type ledgerBoard struct {
	*boardBackend
	addressable []store.Node

	charters  []store.Charter
	firedAt   map[string]time.Time
	today     map[string]int
	judgments map[string][]store.SentinelJudgment
	services  []store.Service
	models    map[string][]string
}

func (l *ledgerBoard) NodeModels(id string) ([]string, error) { return l.models[id], nil }

func (l *ledgerBoard) AddressableNodes() ([]store.Node, error) { return l.addressable, nil }

func (l *ledgerBoard) Charters(statuses ...store.CharterStatus) ([]store.Charter, error) {
	if len(statuses) == 0 {
		return l.charters, nil
	}
	want := make(map[store.CharterStatus]bool, len(statuses))
	for _, status := range statuses {
		want[status] = true
	}
	out := make([]store.Charter, 0, len(l.charters))
	for _, charter := range l.charters {
		if want[charter.Status] {
			out = append(out, charter)
		}
	}
	return out, nil
}

func (l *ledgerBoard) FiringsToday(id string, _ time.Time) (int, error) { return l.today[id], nil }

func (l *ledgerBoard) CharterLastFired(id string) (time.Time, error) { return l.firedAt[id], nil }

func (l *ledgerBoard) RecentSentinelJudgments(id string, limit int) ([]store.SentinelJudgment, error) {
	out := l.judgments[id]
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (l *ledgerBoard) ActiveServices() ([]store.Service, error) { return l.services, nil }

// logCommander is [fakeCommander] plus the one read the store cannot answer. It
// calls the ENGINE'S OWN function rather than a copy of it, so what the page
// shows a reader is what the shipped read produces.
type logCommander struct{ *fakeCommander }

func (logCommander) ServiceLogTail(path string, lines, maxBytes int) []string {
	return command.ServiceLogTail(path, lines, maxBytes)
}

// verbCommander is [logCommander] plus the engine door a chip fires through. It
// records what it was asked rather than doing it, because what is under test on
// this side is the SUBJECT — a verb pointed at nothing is the whole failure the
// target argument exists to prevent.
type verbCommander struct {
	logCommander
	fired []struct{ id, target, words string }
	err   error
}

func (c *verbCommander) InvokeVerb(id, target, words string) (store.Command, error) {
	c.fired = append(c.fired, struct{ id, target, words string }{id, target, words})
	return store.Command{}, c.err
}

// boardCharterFixture is one ratified standing charter: daily at nine, on
// probation, with real rails so the detail page has a rails line to draw.
func boardCharterFixture(t *testing.T) store.Charter {
	t.Helper()
	charter, err := store.NewCharter(
		"charter-1",
		"every deploy has a rollback note",
		store.WatchSpec{Kind: store.WatchCron, Cadence: "every day at 9am",
			Cron: &store.CronSchedule{Kind: store.CronDaily, Hour: 9}},
		"look for deploys without notes",
		store.CharterAction{Template: "write the rollback note"},
		store.CharterRails{PerFiringBudgetUSD: 0.50, MaxFiringsPerDay: 6, EstimatedCostUSD: 0.05},
		store.CharterActive,
		store.Ratification{Origin: store.OriginUser, Evidence: "you asked for it"},
	)
	if err != nil {
		t.Fatalf("the charter fixture is not a charter: %v", err)
	}
	charter.NextDue = fixedNow().Add(21 * time.Hour)
	charter.LastChecked = fixedNow().Add(-30 * time.Minute)
	charter.GreenFirings = 2
	return charter
}

// pageBackend is one window's work: a running job with two parts, a settled one,
// two older jobs only the ledger can still name, one standing charter and one
// promoted service.
func pageBackend(t *testing.T) *ledgerBoard {
	t.Helper()
	backend := board()
	// The clock is a live cell, so the fixture has to have started: a job with
	// no start stamp draws no wall time anywhere, which is honest and untestable.
	for i := range backend.nodes {
		switch backend.nodes[i].ID {
		case "job-1":
			backend.nodes[i].StartedAt = fixedNow().Add(-time.Hour)
		case "job-1/h2":
			backend.nodes[i].StartedAt = fixedNow().Add(-30 * time.Minute)
		}
	}
	// A settled job that billed nothing draws no receipt line at all, which is
	// §16's EMPTINESS and not a bug — but it makes the two-line law untestable,
	// so the fixture's settled job cost something, as a real one would have.
	backend.usage["job-2"] = store.JobUsage{Runs: 1, Cost: 0.20}
	backend.usage["job-old-1"] = store.JobUsage{Runs: 1, Cost: 0.40}
	backend.usage["job-old-2"] = store.JobUsage{Runs: 1, Cost: 1.60}
	charter := boardCharterFixture(t)
	return &ledgerBoard{
		boardBackend: backend,
		addressable: append(append([]store.Node(nil), backend.nodes...),
			store.Node{ID: "job-old-1", Title: "importer-rewrite", Status: store.Done, CreatedSeq: 2,
				StartedAt: fixedNow().Add(-3 * time.Hour), FinishedAt: fixedNow().Add(-2 * time.Hour)},
			store.Node{ID: "job-old-2", Title: "cjk-widths", Status: store.Done, CreatedSeq: 3,
				StartedAt: fixedNow().Add(-5 * time.Hour), FinishedAt: fixedNow().Add(-4 * time.Hour)},
		),
		charters: []store.Charter{charter},
		firedAt:  map[string]time.Time{charter.ID: fixedNow().Add(-2 * time.Hour)},
		today:    map[string]int{charter.ID: 3},
		judgments: map[string][]store.SentinelJudgment{charter.ID: {
			{Yes: true, Line: "found a deploy with no note", Outcome: "fired"},
			{Yes: false, Line: "nothing to do"},
		}},
		services: []store.Service{{
			ID: "service-1", Name: "api", Command: "npm run dev", Dir: "/home/someone/src/api",
			Health: store.ServiceHealth{Kind: store.ServiceHealthPort, Value: "8080"},
			Status: store.ServiceRunning, StartedAt: fixedNow().Add(-3 * time.Hour),
			PID:         4242,
			AutoRestart: true, RestartCount: 2,
		}},
	}
}

func pageApp(t *testing.T) *App {
	t.Helper()
	return pageAppOn(t, pageBackend(t), tokens.NoColor)
}

// pageAppOn is a window over a given backend at a given colour profile. The
// profile is a parameter because every law on this page must hold at every tier
// (§12's honest degradation), not only at the one the goldens happen to use.
func pageAppOn(t *testing.T, backend Backend, profile tokens.Profile) *App {
	t.Helper()
	return pageAppFull(t, backend, logCommander{&fakeCommander{model: "anthropic/claude-k3"}}, profile)
}

// pageAppWith is a window over a given engine, for the laws about what a chip
// asks the engine to do.
func pageAppWith(t *testing.T, backend Backend, commander Commander) *App {
	t.Helper()
	return pageAppFull(t, backend, commander, tokens.NoColor)
}

func pageAppFull(t *testing.T, backend Backend, commander Commander, profile tokens.Profile) *App {
	t.Helper()
	app := New(Options{
		Backend:   backend,
		Commander: commander,
		Session:   testSession,
		Profile:   profile,
		Now:       fixedNow,
		PollEvery: time.Millisecond,
		Root:      "/home/someone/aforge-v2",
		Home:      "/home/someone",
	})
	poll(t, app)
	return app
}

// boardFrame drives the app to the work page and returns the frame it draws.
func boardFrame(t *testing.T, app *App, width, height int) string {
	t.Helper()
	if app.page != pageBoard {
		t.Fatalf("the app is on the %s page, not the board", app.page)
	}
	return ansi.Strip(app.Frame(width, height))
}

// boardPage is the board pane alone, as rows, which is what a law about the
// page's own columns has to be asserted against.
func boardPageRows(t *testing.T, app *App, width, height int) []string {
	t.Helper()
	_ = app.Frame(width, height)
	return strings.Split(ansi.Strip(app.boardPane.Render(width, height)), "\n")
}

// -- the swap ----------------------------------------------------------------

// §7: the places tabs swap the LENS. `work` is a page and not a rail toggle, and
// esc from a page puts the thread back.
func TestTheTabsSwapTheLensAndEscReturnsToChat(t *testing.T) {
	app := pageApp(t)
	if app.page != pageThread {
		t.Fatalf("a fresh window opened on the %s page, want chat", app.page)
	}

	// The click. A pointer on the footer's word runs the registry row it was
	// drawn from (5.22 rule 4), which is the same door the chord reaches.
	app.runFooterVerb(placeBoardID)
	if app.page != pageBoard {
		t.Fatalf("the work tab left the window on the %s page", app.page)
	}
	if !strings.Contains(boardFrame(t, app, 100, 24), boardWorkingWord) {
		t.Fatalf("the work page is not drawing the board:\n%s", boardFrame(t, app, 100, 24))
	}

	// Esc. There is no scope to pop and no transcript to return to — what the
	// reader is watching is the page, so the page is what esc undoes.
	app.navigate()
	if app.page != pageThread {
		t.Fatalf("esc left the window on the %s page, want chat", app.page)
	}

	// The key, through the whole ladder this time.
	app.runFooterVerb(placeNotebookID)
	if app.page != pageNotebook {
		t.Fatalf("the notebook tab left the window on the %s page", app.page)
	}
	app.runFooterVerb(placeThreadID)
	if app.page != pageThread {
		t.Fatalf("the chat tab left the window on the %s page", app.page)
	}
}

// A real pointer on the strip, resolved by the footer's own hit test: the word
// under the cursor is the page the window ends up on.
func TestClickingTheWorkTabOpensTheBoard(t *testing.T) {
	app := pageApp(t)
	const width = 100
	_ = app.Frame(width, 20)

	row := ansi.Strip(app.status.Render(width, 1))
	at := strings.Index(row, "work")
	if at < 0 {
		t.Fatalf("the strip does not carry the work tab:\n%q", row)
	}
	app.status.Mouse(clickAt(at, 0), image.Point{X: at})
	if app.page != pageBoard {
		t.Fatalf("clicking the work tab left the window on the %s page", app.page)
	}
}

// §7's left zone says where the reader IS. The bright word and the lens on
// screen are one fact, so the enum is what the tabs are drawn from.
func TestThePlacesCurrentFollowsThePage(t *testing.T) {
	app := pageApp(t)
	for _, want := range []struct {
		page page
		word string
	}{{pageThread, "chat"}, {pageBoard, "work"}, {pageNotebook, "notebook"}} {
		app.showPage(want.page)
		current := ""
		for _, place := range app.status.places {
			if place.Current {
				if current != "" {
					t.Fatalf("two tabs are current on the %s page", want.page)
				}
				current = place.Word
			}
		}
		if current != want.word {
			t.Fatalf("on the %s page the current tab is %q, want %q", want.page, current, want.word)
		}
	}
}

// §15: two copies of one list side by side is the same fact twice, and the board
// is the fuller copy. The sidebar goes — and on the work page it is the one page
// where it cannot be brought back, which is what this test now pins.
//
// The OTHER pages carry it by default — the rail teaches by existing (see
// [App.railState]) — so what is asserted here is the DIFFERENCE: a rung the
// reader chose survives every page swap, and the work page is the one place the
// column cannot be brought back at all.
func TestTheSidebarHidesOnTheBoardAndComesBack(t *testing.T) {
	app := pageApp(t)
	wide := ansi.Strip(app.Frame(120, 24))
	if !strings.Contains(wide, railSentinel) {
		t.Fatalf("a fresh window drew no sidebar beside the thread:\n%s", wide)
	}

	app.showPage(pageBoard)
	board := boardFrame(t, app, 120, 24)
	// The room list is the sidebar's and only the sidebar's: the board is the
	// WORK view, so a room name on this frame means the rail is still there.
	if strings.Contains(board, railSentinel) {
		t.Fatalf("the sidebar is still drawn beside the board:\n%s", board)
	}
	// And the chord cannot summon it here: on this page it moves the keyboard
	// between the board and the mouth, which is what it has always meant on a
	// page (see [App.key]).
	press(app, "ctrl+o")
	if still := boardFrame(t, app, 120, 24); strings.Contains(still, railSentinel) {
		t.Fatalf("the chord opened a sidebar the board has no room for:\n%s", still)
	}

	// Back on the thread the drawer is remembered as the reader left it.
	app.showPage(pageThread)
	if back := ansi.Strip(app.Frame(120, 24)); !strings.Contains(back, railSentinel) {
		t.Fatalf("the sidebar did not come back on the chat page:\n%s", back)
	}
}

// -- the row's anatomy -------------------------------------------------------

// §5b's whole point: the receipt is on its OWN LINE, DIRECTLY UNDER THE NAME.
// A name on the left and a receipt eighty columns away is a matching exercise,
// and this is the test that it is not one.
func TestABoardRowIsTwoLinesWithItsReceiptUnderItsName(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	rows := boardPageRows(t, app, 100, 30)

	at := -1
	for i, row := range rows {
		if strings.Contains(row, "wisp-parity") {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("the running job is not on the page:\n%s", strings.Join(rows, "\n"))
	}
	// The name line carries the name and NOTHING ELSE — no money, no census.
	// Everything a receipt says lives one line down.
	name := strings.TrimRight(rows[at], " ")
	if strings.Contains(name, "$") {
		t.Fatalf("the name line is still carrying a receipt: %q", name)
	}
	if got := len([]rune(name[:strings.Index(name, "wisp-parity")])); got != boardNameCol {
		t.Fatalf("the name begins at column %d, want the shared name column %d: %q",
			got, boardNameCol, name)
	}

	receipt := rows[at+1]
	if !strings.Contains(receipt, tokens.Money(8.65)) {
		t.Fatalf("the line under the name is not the receipt: %q", receipt)
	}
	// Directly under the name: the same column, so the eye that read the name
	// does not travel at all.
	if got := len(receipt) - len(strings.TrimLeft(receipt, " ")); got != boardMetaCol {
		t.Fatalf("the receipt is indented %d, want the name column %d: %q",
			got, boardMetaCol, receipt)
	}
}

// §5b's receipt vocabulary, in the order it fixed: live count or census ·
// wall-clock · money · model words · ~NK tok.
func TestTheReceiptSpellsItsPartsInTheFixedOrder(t *testing.T) {
	backend := pageBackend(t)
	backend.usage["job-1"] = store.JobUsage{Runs: 1, Cost: 8.65, PromptTokens: 40000, CompletionTokens: 5000}
	app := pageAppOn(t, backend, tokens.NoColor)
	app.showPage(pageBoard)
	_ = app.Frame(100, 30)

	line := boardMetaFor(t, app, "wisp-parity")
	text := boardLineText(line)
	want := []string{"1 running", "$8.65", tokens.GlyphEstimate + tokens.Count(45000) + " tok"}
	at := 0
	for _, part := range want {
		found := strings.Index(text[at:], part)
		if found < 0 {
			t.Fatalf("the receipt %q is missing %q or has it out of order", text, part)
		}
		at += found + len(part)
	}
	// The clock sits between the count and the money, which is the one ordering
	// claim a substring search over three cells cannot make on its own.
	count := strings.Index(text, "1 running")
	money := strings.Index(text, "$8.65")
	if clock := strings.Index(text, "1h"); clock < count || clock > money {
		t.Fatalf("the wall clock is not between the census and the money: %q", text)
	}
}

// Amber only ever means a human is actually needed (5.16), and `needs you`
// spends the census's slot on the one fact that outranks a count.
func TestAnOpenQuestionReplacesTheCensusWithNeedsYou(t *testing.T) {
	backend := pageBackend(t)
	backend.questions = []store.AgentQuestion{{Seq: 1, OriginNodeID: "job-1", Text: "which branch?"}}
	app := pageAppOn(t, backend, tokens.NoColor)
	app.showPage(pageBoard)
	frame := boardFrame(t, app, 100, 30)

	if !strings.Contains(frame, boardNeedsYou) {
		t.Fatalf("a job holding a question does not say so:\n%s", frame)
	}
	line := boardMetaFor(t, app, "wisp-parity")
	if got := boardLineText(line); strings.HasPrefix(got, boardNeedsYou) == false {
		t.Fatalf("`needs you` did not take the census's slot: %q", got)
	}
	amber := false
	for _, span := range line.spans {
		if span.text == boardNeedsYou {
			amber = span.tier == tokens.Amber
		}
	}
	if !amber {
		t.Fatalf("`needs you` is not amber: %+v", line.spans)
	}
	// And no WORD on the page wears amber except this one. Amber on a state
	// glyph is the vocabulary's own (5.21's amber ⚑ for a waits-on edge, the
	// amber ? for a question); amber on prose would be emphasis, which 5.16
	// forbids outright.
	for _, other := range app.boardLines() {
		for i, span := range other.spans {
			if i == 0 || span.tier != tokens.Amber || span.text == boardNeedsYou {
				continue
			}
			t.Fatalf("amber is being spent on the word %q, which is not a question", span.text)
		}
	}
}

// Tree as progress (§5b): a working job shows its live subtree, a settled one
// stays collapsed to its two lines, and there is no expand toggle anywhere.
func TestAWorkingJobShowsItsSubtreeAndASettledOneDoesNot(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	frame := boardFrame(t, app, 100, 30)

	for _, part := range []string{"H2", "KeyCutter"} {
		if !strings.Contains(frame, part) {
			t.Fatalf("the working job is not showing its live subtree (%q):\n%s", part, frame)
		}
	}
	// The parts are indented under the name and dim: they are the SHAPE of the
	// work, and the job's own name is the row that gets the ink.
	for _, line := range app.boardLines() {
		if line.kind != boardTwig {
			continue
		}
		if line.indent < boardNameCol {
			t.Fatalf("a subtree row is not indented under its job: %+v", line)
		}
		for _, span := range line.spans {
			if span.tier == tokens.TextPrimary {
				t.Fatalf("a subtree row is drawn at the primary tier: %q", span.text)
			}
		}
	}
	// The settled job draws exactly two lines and no third.
	settled := 0
	for _, line := range app.boardLines() {
		if line.row.Name == "perf-audit" {
			settled++
		}
	}
	if settled != 2 {
		t.Fatalf("the settled job drew %d lines, want a name and a receipt", settled)
	}
	// There is no fold affordance on a job: the whole row is the door to the
	// room, and §5b removed the expand toggle outright.
	for _, line := range app.boardLines() {
		if line.target == boardOpensRoom && strings.Contains(boardLineText(line), tokens.GlyphCollapsed) {
			t.Fatalf("a job row is still drawing an expand chevron: %q", boardLineText(line))
		}
	}
}

// The depth cap: past two levels the tree becomes one `…` rather than a
// document (§5b, and §16's one ellipsis grammar).
func TestTheLiveSubtreeStopsAtItsDepthCap(t *testing.T) {
	backend := pageBackend(t)
	backend.nodes = append(backend.nodes,
		store.Node{ID: "job-1/h2/deep", Parent: "job-1/h2", Title: "DeepOne",
			Status: store.Running, CreatedSeq: 13},
		store.Node{ID: "job-1/h2/deep/deeper", Parent: "job-1/h2/deep", Title: "DeeperStill",
			Status: store.Running, CreatedSeq: 14})
	app := pageAppOn(t, backend, tokens.NoColor)
	app.showPage(pageBoard)
	frame := boardFrame(t, app, 100, 40)

	if !strings.Contains(frame, "DeepOne") {
		t.Fatalf("the second level of the tree is missing:\n%s", frame)
	}
	if strings.Contains(frame, "DeeperStill") {
		t.Fatalf("the tree drew past its depth cap:\n%s", frame)
	}
	if !strings.Contains(frame, tokens.GlyphEllipsis) {
		t.Fatalf("the elided depth said nothing at all:\n%s", frame)
	}
}

// 5.10 and §5b: model WORDS, never provider slugs. A receipt that said
// `anthropic/claude-k3` would be the surface reading an id out loud.
func TestTheReceiptNamesModelsAsWordsAndNeverSlugs(t *testing.T) {
	backend := pageBackend(t)
	backend.models = map[string][]string{
		"job-1": {"anthropic/claude-sonnet-4.6", "anthropic/claude-sonnet-4.6", "openai/gpt-5.4"},
	}
	app := pageAppOn(t, backend, tokens.NoColor)
	app.showPage(pageBoard)
	_ = app.Frame(100, 30)

	text := boardLineText(boardMetaFor(t, app, "wisp-parity"))
	if strings.Contains(text, "/") {
		t.Fatalf("the receipt is reading a provider slug out loud: %q", text)
	}
	if !strings.Contains(text, "sonnet") {
		t.Fatalf("the receipt does not name who did the work: %q", text)
	}
	// Deduped AFTER shortening: two slugs from one family are one word, and a
	// receipt that said it twice would be counting bindings out loud.
	if strings.Count(text, "sonnet") != 1 {
		t.Fatalf("the model words are not deduped: %q", text)
	}
}

// A window with nothing in it says so. An unwired surface and an empty one must
// never look alike (12.10).
func TestAnEmptyBoardSaysItIsEmpty(t *testing.T) {
	app := newTestApp(&fakeBackend{}, nil, nil)
	poll(t, app)
	app.showPage(pageBoard)
	if frame := boardFrame(t, app, 80, 12); !strings.Contains(frame, boardEmptyNote) {
		t.Fatalf("an empty board drew nothing at all:\n%s", frame)
	}
}

// §6 and §10: history is ONE expandable row carrying the count, and the fold row
// carries the rollup of what is behind it.
func TestTheHistoryFoldOpensAndCarriesItsRollup(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	frame := boardFrame(t, app, 100, 40)

	if !strings.Contains(frame, boardHistoryWord+" (2)") {
		t.Fatalf("the history fold does not say how deep it goes:\n%s", frame)
	}
	if !strings.Contains(frame, tokens.GlyphCollapsed) {
		t.Fatalf("the history fold is not drawn as a fold:\n%s", frame)
	}
	// The rollup is money and only money: durations do not sum (§13).
	if want := tokens.Money(2.00); !strings.Contains(frame, want) {
		t.Fatalf("the fold row carries no rollup receipt (%s):\n%s", want, frame)
	}
	if strings.Contains(frame, "importer-rewrite") {
		t.Fatalf("a closed history fold is listing its members:\n%s", frame)
	}

	// The whole line is the door (§10), and enter on it opens in place.
	app.boardSelect(boardDoorOf(t, app, boardOpensFold, ""))
	press(app, "enter")
	frame = boardFrame(t, app, 100, 40)
	if !strings.Contains(frame, "importer-rewrite") || !strings.Contains(frame, "cjk-widths") {
		t.Fatalf("opening history listed nothing:\n%s", frame)
	}
	if !strings.Contains(frame, tokens.GlyphExpanded) {
		t.Fatalf("the open fold still draws the closed chevron:\n%s", frame)
	}
	if strings.Count(frame, "wisp-parity") != 1 {
		t.Fatalf("history repeated a job the board is already showing:\n%s", frame)
	}

	press(app, "enter")
	if frame = boardFrame(t, app, 100, 40); strings.Contains(frame, "importer-rewrite") {
		t.Fatalf("the fold did not close again:\n%s", frame)
	}
}

// -- the two bands -----------------------------------------------------------

// notebook-split.md §2: the standing charters live on the WORK page now, in the
// same shared-column grammar the jobs use.
func TestTheWatchingBandDrawsItsCharters(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	frame := boardFrame(t, app, 110, 40)

	for _, want := range []string{
		boardWatchingWord,                  // the faint band word
		"every deploy has a rollback note", // the invariant, in full
		"every day at 9am",                 // the cadence, as a person says it
		string(store.CharterProbation),     // the ladder, honestly
		"fired 2h",                         // last fired, relative
		"3 today",                          // today's count
		"~$0.05/run",                       // cost per run, marked as an estimate
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the watching band left out %q:\n%s", want, frame)
		}
	}
	// The band obeys the row grammar: name line, then the receipt under it at
	// the shared name column.
	line := boardEntryFor(t, app, "every deploy has a rollback note")
	if line.indent != boardNameCol {
		t.Fatalf("a charter's name line is not on the shared name column: %+v", line)
	}
	if line.mark == "" {
		t.Fatalf("a charter's name line carries no marker in the gutter: %+v", line)
	}
	if line.target != boardOpensCharter {
		t.Fatalf("a charter row opens %v, want its detail page", line.target)
	}
}

// Diligence and death are not the same state: a watch that has checked
// faithfully and found nothing must not render like one that has never run.
func TestACharterThatHasNeverFiredSaysWhenItLastLooked(t *testing.T) {
	backend := pageBackend(t)
	backend.firedAt = nil
	app := pageAppOn(t, backend, tokens.NoColor)
	app.showPage(pageBoard)
	frame := boardFrame(t, app, 110, 40)

	if !strings.Contains(frame, "checked 30m") {
		t.Fatalf("a diligent watch that has never fired says nothing about it:\n%s", frame)
	}
	if strings.Contains(frame, "never fired") {
		t.Fatalf("a watch that checked half an hour ago is being called dead:\n%s", frame)
	}
}

// notebook-split.md §2: name · life glyph · uptime · restart count.
func TestTheServicesBandDrawsItsProcesses(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	frame := boardFrame(t, app, 110, 40)

	for _, want := range []string{boardServicesWord, "api", "up 3h", "2 restarts"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the services band left out %q:\n%s", want, frame)
		}
	}
	line := boardEntryFor(t, app, "api")
	if line.target != boardOpensService {
		t.Fatalf("a service row opens %v, want its detail page", line.target)
	}
	// §18: a service NEVER spins. It is the most durable thing this product has,
	// and a dancing glyph on it would be a lie about liveness.
	if line.spin {
		t.Fatal("a service row is marked as a spinner")
	}
}

// §16's ALIGNMENT read across bands: the glyph column, the name column and the
// receipt column are one x for jobs, charters and services alike.
func TestTheBandsShareOneColumnGrammar(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	_ = app.Frame(110, 40)

	for _, line := range app.boardLines() {
		switch line.kind {
		case boardEntry:
			// §20: the marker hangs in the gutter and the NAME is what sits at
			// the content edge, so an entry line and its receipt line share one
			// column and the glyph is not part of either.
			if line.indent != boardNameCol {
				t.Fatalf("an entry line sits at %d, not the name column: %q",
					line.indent, boardLineText(line))
			}
			if line.mark == "" {
				t.Fatalf("an entry line carries no gutter marker: %q", boardLineText(line))
			}
		case boardMeta:
			if line.indent != boardMetaCol {
				t.Fatalf("a receipt line sits at %d, not the name column: %q",
					line.indent, boardLineText(line))
			}
		}
	}
}

// -- the doors ---------------------------------------------------------------

// Enter on a job opens its ROOM — the same JumpToRoom path the sidebar and the
// palette take — and the room is a CHAT-page thing, so the trail replaces the
// tabs on the way in.
func TestEnterOnABoardRowOpensTheJobsRoom(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	_ = app.Frame(100, 30)

	app.boardSelect(boardDoorOf(t, app, boardOpensRoom, "wisp-parity"))
	press(app, "enter")

	if app.page != pageThread {
		t.Fatalf("entering a job left the reader on the %s page", app.page)
	}
	if app.view == nil || app.view.kind != viewNode || app.view.node != "job-1" {
		t.Fatalf("the lens is not the job's room: %+v", app.view)
	}
	if app.status.breadcrumb == "" {
		t.Fatal("the trail is empty inside a room, so the tabs never gave way")
	}
}

// 13.18 on a page: clicks go where they point. And §3's whole-block law —
// the receipt line and the subtree rows under a job are the job's door too,
// so a hand does not have to hit the one line in four that is the name.
func TestClickingAnywhereOnAJobBlockOpensItsRoom(t *testing.T) {
	app := pageApp(t)
	for _, offset := range []int{0, 1, 2} {
		app.showPage(pageBoard)
		app.view = nil
		_ = app.Frame(100, 30)

		y := boardScreenRow(t, app, "wisp-parity") + offset
		app.boardPane.Mouse(clickAt(4, y), image.Point{X: 4, Y: y})
		if app.view == nil || app.view.kind != viewNode || app.view.node != "job-1" {
			t.Fatalf("a click %d lines into the job's block opened %+v", offset, app.view)
		}
	}

	// A click on the empty space under a short board is a click on nothing.
	app.showPage(pageBoard)
	_ = app.Frame(100, 30)
	before := app.view
	app.boardPane.Mouse(clickAt(4, 29), image.Point{X: 4, Y: 29})
	if app.view != before {
		t.Fatal("a click below the last row navigated somewhere")
	}
}

// THE CLICK LAW DIFFERS PER BAND, and this is the test that says so out loud: a
// job goes to a room, a charter and a service go to a detail page inside this
// pane and NEVER to a room.
func TestACharterAndAServiceOpenDetailPagesAndNeverRooms(t *testing.T) {
	for _, want := range []struct {
		name   string
		target boardTarget
	}{
		{"every deploy has a rollback note", boardOpensCharter},
		{"api", boardOpensService},
	} {
		app := pageApp(t)
		app.showPage(pageBoard)
		_ = app.Frame(110, 40)
		before := app.view

		y := boardScreenRow(t, app, want.name)
		app.boardPane.Mouse(clickAt(4, y), image.Point{X: 4, Y: y})

		if app.page != pageBoard {
			t.Fatalf("%q took the reader off the work page, to %s", want.name, app.page)
		}
		if app.view != before {
			t.Fatalf("%q opened a room: %+v", want.name, app.view)
		}
		if !app.board.detail.open() || app.board.detail.target != want.target {
			t.Fatalf("%q did not open its detail page: %+v", want.name, app.board.detail)
		}
	}
}

// Esc closes the innermost thing first, and puts the reader back on the row they
// left (notebook-split.md §3's "scrolled to the row it left").
func TestEscLeavesADetailPageOnTheRowItCameFrom(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	_ = app.Frame(110, 40)

	door := boardDoorOf(t, app, boardOpensCharter, "every deploy has a rollback note")
	app.boardSelect(door)
	press(app, "enter")
	if !app.board.detail.open() {
		t.Fatal("enter on a charter did not open its page")
	}
	// The page is a document: it scrolls, unlike the list behind it.
	app.boardDetailScroll(2)

	if _, claimed := app.boardKey(tea.KeyPressMsg{Code: tea.KeyEscape}); !claimed {
		t.Fatal("esc was not claimed while a detail page was open")
	}
	if app.board.detail.open() {
		t.Fatal("esc did not close the detail page")
	}
	if app.board.cursor != door {
		t.Fatalf("esc landed the cursor on door %d, want the row it left (%d)",
			app.board.cursor, door)
	}
	// And with no detail open, esc is NOT the board's — the page is the
	// outermost thing esc undoes (§7) and that must keep being true.
	if _, claimed := app.boardKey(tea.KeyPressMsg{Code: tea.KeyEscape}); claimed {
		t.Fatal("the board claimed esc with no detail open")
	}
}

// The trail is a door: `work ‹ watching` is one click target, because a reader
// points at where they came from and not at a chevron.
func TestTheDetailTrailSaysWhereItSitsAndGoesBack(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	_ = app.Frame(110, 40)
	app.boardSelect(boardDoorOf(t, app, boardOpensCharter, "every deploy has a rollback note"))
	press(app, "enter")

	frame := boardFrame(t, app, 110, 40)
	want := "work " + tokens.GlyphScopeUp + " " + boardWatchingWord +
		" " + tokens.GlyphScopeUp + " every deploy has a rollback note"
	if !strings.Contains(frame, want) {
		t.Fatalf("the detail page draws no trail (%q):\n%s", want, frame)
	}
	app.boardPane.Mouse(clickAt(3, 0), image.Point{X: 3, Y: 0})
	if app.board.detail.open() {
		t.Fatal("clicking the trail did not go back")
	}
}

// notebook-split.md §3's charter body: invariant, rails, tenure ladder position,
// and the last five judgments with their outcomes.
func TestTheCharterDetailDrawsItsRailsLadderAndHistory(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	_ = app.Frame(110, 40)
	app.boardSelect(boardDoorOf(t, app, boardOpensCharter, "every deploy has a rollback note"))
	press(app, "enter")
	frame := boardFrame(t, app, 110, 40)

	for _, want := range []string{
		"rails", tokens.Money(0.50) + " a firing", "6 a day",
		"tenure", string(store.CharterProbation), "2 green firings",
		"found a deploy with no note", "fired", "nothing to do",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the charter page left out %q:\n%s", want, frame)
		}
	}
	// §3 forbids a denominator nobody can keep: the promotion threshold is the
	// resident's to raise, so the page never writes "2 of 3".
	if strings.Contains(frame, " of 3") {
		t.Fatalf("the tenure line invented a denominator:\n%s", frame)
	}
}

// notebook-split.md §3's service body: command, dir, health, restart policy, and
// the live log tail off LogPath.
func TestTheServiceDetailDrawsItsCommandAndLogTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.log")
	rows := make([]string, 0, 14)
	for i := 0; i < 14; i++ {
		rows = append(rows, "line-"+string(rune('a'+i)))
	}
	if err := os.WriteFile(path, []byte(strings.Join(rows, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("the log fixture would not write: %v", err)
	}
	backend := pageBackend(t)
	backend.services[0].LogPath = path
	app := pageAppOn(t, backend, tokens.NoColor)
	app.showPage(pageBoard)
	_ = app.Frame(110, 40)
	app.boardSelect(boardDoorOf(t, app, boardOpensService, "api"))
	press(app, "enter")
	frame := boardFrame(t, app, 110, 40)

	for _, want := range []string{"npm run dev", "/home/someone/src/api", "port:8080", "restarts itself"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the service page left out %q:\n%s", want, frame)
		}
	}
	// Ten lines, the newest last, and the four before them gone.
	if !strings.Contains(frame, "line-n") || !strings.Contains(frame, "line-e") {
		t.Fatalf("the log tail is not the last ten lines:\n%s", frame)
	}
	if strings.Contains(frame, "line-a") {
		t.Fatalf("the log tail drew more than it was asked for:\n%s", frame)
	}
	// The surface's bound and the engine's default are one number.
	if boardDetailLogLines != command.ServiceLogTailLines ||
		boardDetailLogBytes != command.ServiceLogTailBytes {
		t.Fatal("the page asks for a log tail the engine does not serve")
	}
}

// A service with no log says one calm sentence rather than drawing an empty
// frame (12.10 again, one page down).
func TestAServiceWithNoLogSaysSo(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	_ = app.Frame(110, 40)
	app.boardSelect(boardDoorOf(t, app, boardOpensService, "api"))
	press(app, "enter")
	if frame := boardFrame(t, app, 110, 40); !strings.Contains(frame, "nothing in the log yet") {
		t.Fatalf("a service with no log drew nothing at all:\n%s", frame)
	}
}

// notebook-split.md §4: the seven verb ids are fixed across all three lanes, and
// the chips are drawn to §16's VERB·KEY law through the registry's own ladder.
func TestTheDetailVerbsCarryTheFixedRegistryIds(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	_ = app.Frame(110, 40)

	for _, want := range []struct {
		row  string
		door boardTarget
		ids  []string
	}{
		{"every deploy has a rollback note", boardOpensCharter,
			[]string{charterPauseVerb, charterCadenceVerb, charterProbationVerb, charterRetireVerb}},
		{"api", boardOpensService,
			[]string{serviceStopVerb, serviceRestartVerb, serviceAutoRestartVerb}},
	} {
		app.boardSelect(boardDoorOf(t, app, want.door, want.row))
		press(app, "enter")

		hits := map[string]boardHit{}
		for _, line := range app.boardDetailLines() {
			for _, hit := range line.hits {
				hits[hit.id] = hit
			}
		}
		for _, id := range want.ids {
			hit, ok := hits[id]
			if !ok {
				t.Fatalf("%s's page does not emit %q", want.row, id)
			}
			if hit.to <= hit.from {
				t.Fatalf("%q is drawn with no cells to click", id)
			}
		}
		if _, ok := hits[boardBack]; !ok {
			t.Fatalf("%s's page has no way back", want.row)
		}
		app.closeBoardDetail()
	}
}

// A chip fires at the ITEM the page is about. A verb with no subject is a verb
// pointed at nothing, which is the one way seven correct ids can still be wrong.
func TestADetailVerbFiresAtTheItemThePageIsAbout(t *testing.T) {
	engine := &verbCommander{logCommander: logCommander{&fakeCommander{}}}
	app := pageAppWith(t, pageBackend(t), engine)
	app.showPage(pageBoard)
	_ = app.Frame(110, 40)
	app.boardSelect(boardDoorOf(t, app, boardOpensService, "api"))
	press(app, "enter")

	// stop cannot be undone, so the first activation ARMS it — the registry's
	// own question on the status line — and only the second fires (itemverb.go).
	app.runBoardVerb(serviceStopVerb)
	if len(engine.fired) != 0 {
		t.Fatalf("a destructive chip fired without its second yes")
	}
	if !strings.Contains(app.status.err, "?") {
		t.Fatalf("arming did not ask the registry's question: %q", app.status.err)
	}
	app.runBoardVerb(serviceStopVerb)
	if len(engine.fired) != 1 {
		t.Fatalf("the stop chip fired %d commands after its second yes", len(engine.fired))
	}
	if got := engine.fired[0]; got.id != serviceStopVerb || got.target != "service-1" {
		t.Fatalf("the chip fired %+v, want %s at the service it is about", got, serviceStopVerb)
	}
	if app.status.err != "" {
		t.Fatalf("a fired verb left its question standing: %q", app.status.err)
	}

	// A verb that carries an argument does NOT fire: the one-mouth law says
	// "make it Tuesdays instead" is a thing to say. Its sentence is seeded into
	// an empty draft, or offered on the status line when the draft is taken.
	engine.err = command.ErrSteerVerb
	app.closeBoardDetail()
	app.boardSelect(boardDoorOf(t, app, boardOpensCharter, "every deploy has a rollback note"))
	press(app, "enter")
	app.runBoardVerb(charterCadenceVerb)
	seeded := ""
	if s, ok := app.composer.(interface{ Draft() string }); ok {
		seeded = s.Draft()
	}
	if !strings.Contains(seeded+app.status.err, "every deploy has a rollback note") {
		t.Fatalf("a steering verb neither seeded the composer nor offered its sentence: draft %q, status %q", seeded, app.status.err)
	}
}

// The second yes can be the keyboard's: y fires the armed verb, esc stands it
// down — without closing the page underneath, because the question is the
// innermost thing esc undoes.
func TestAnArmedVerbAnswersTheKeyboard(t *testing.T) {
	engine := &verbCommander{logCommander: logCommander{&fakeCommander{}}}
	app := pageAppWith(t, pageBackend(t), engine)
	app.showPage(pageBoard)
	_ = app.Frame(110, 40)
	app.boardSelect(boardDoorOf(t, app, boardOpensService, "api"))
	press(app, "enter")

	app.runBoardVerb(serviceStopVerb)
	press(app, "esc")
	if app.verbArmed != nil || app.status.err != "" {
		t.Fatalf("esc did not stand the armed verb down")
	}
	if len(engine.fired) != 0 {
		t.Fatalf("standing down fired %d commands", len(engine.fired))
	}
	if app.board.detail.id == "" {
		t.Fatalf("esc while armed closed the page under the question")
	}

	app.runBoardVerb(serviceStopVerb)
	press(app, "y")
	if len(engine.fired) != 1 {
		t.Fatalf("y fired %d commands, want the armed verb once", len(engine.fired))
	}
}

// A chip is a door, and the door it opens is the registry's — the same executor
// the footer's words and the palette's rows reach.
func TestClickingADetailVerbRunsItThroughTheRegistry(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	_ = app.Frame(110, 40)
	app.boardSelect(boardDoorOf(t, app, boardOpensService, "api"))
	press(app, "enter")

	lines := app.boardDetailLines()
	y, hit := -1, boardHit{}
	for i, line := range lines {
		for _, candidate := range line.hits {
			if candidate.id == serviceStopVerb {
				y, hit = i, candidate
			}
		}
	}
	if y < 0 {
		t.Fatal("the service page draws no stop chip")
	}
	got, ok := app.boardDetailHit(hit.from, y)
	if !ok || got != serviceStopVerb {
		t.Fatalf("a click on the stop chip resolved to %q (%v)", got, ok)
	}
	// A cell beside the chip is not the chip: a verb row where the gaps were
	// live would be a row that fires on a miss.
	if _, ok := app.boardDetailHit(0, y); ok {
		t.Fatal("the margin before the first chip is a click target")
	}
	// The click itself goes through the one executor and does not panic on a
	// registry row this wave's backend lane has not landed yet.
	app.boardPane.Mouse(clickAt(hit.from, y), image.Point{X: hit.from, Y: y})
}

// The rail's ModeList conventions, on a page: j and k walk, digits jump, and
// nothing is claimed while a sentence is in progress (§8's rest state).
func TestTheBoardWalksLikeTheRail(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	_ = app.Frame(100, 30)

	if app.board.cursor != 0 {
		t.Fatalf("the board opened with the cursor at %d", app.board.cursor)
	}
	press(app, "j")
	if app.board.cursor != 1 {
		t.Fatalf("j moved the cursor to %d, want 1", app.board.cursor)
	}
	press(app, "k")
	if app.board.cursor != 0 {
		t.Fatalf("k moved the cursor to %d, want 0", app.board.cursor)
	}
	// It clamps rather than wrapping: the gesture the reader meant was "further
	// up", and there is nothing up there.
	press(app, "k")
	if app.board.cursor != 0 {
		t.Fatalf("k wrapped to %d", app.board.cursor)
	}
	press(app, "2")
	if app.board.cursor != 1 {
		t.Fatalf("the digit jumped to %d, want 1", app.board.cursor)
	}
	press(app, "G")
	last := len(boardDoors(app.boardLines())) - 1
	if app.board.cursor != last {
		t.Fatalf("G landed on %d, want the last door %d", app.board.cursor, last)
	}

	// AND THE PAGE KEEPS WHAT IT CLAIMS. A key the board does not recognise is
	// not a letter for the composer to take while the board holds the keyboard:
	// 13.8's "jack knife kayak" arriving as "ac nife aya" is what one cursor
	// answering to two keyboards looks like.
	app.boardSelect(0)
	for _, r := range "jack knife" {
		app.Update(typed(r))
	}
	if got := app.composer.Draft(); got != "" {
		t.Fatalf("letters reached the draft while the board held the keyboard: %q", got)
	}

	// ctrl+o hands it back, and then the same sentence arrives whole.
	press(app, "ctrl+o")
	if app.pageFocus {
		t.Fatal("ctrl+o did not hand the keyboard back to the composer")
	}
	before := app.board.cursor
	for _, r := range "jack knife" {
		app.Update(typed(r))
	}
	if got := app.composer.Draft(); got != "jack knife" {
		t.Fatalf("the draft arrived as %q", got)
	}
	if app.board.cursor != before {
		t.Fatalf("the board ate letters out of a draft (cursor %d, was %d)", app.board.cursor, before)
	}
}

// A click on the composer takes the keyboard back from the page — the same
// one-way door 13.18 closed for the map, one lens over.
func TestClickingTheComposerTakesTheKeyboardBackFromAPage(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	if !app.pageFocus {
		t.Fatal("arriving at the board did not hand it the keyboard")
	}
	if !app.focusConversation() {
		t.Fatal("the conversation refused the keyboard")
	}
	if app.pageFocus {
		t.Fatal("the page kept the keyboard after a click on the composer")
	}
}

// The selection is marked where a reader can see it at every colour tier: the
// accent rail in the gutter, which survives `--color none`.
func TestTheBoardMarksItsSelection(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	frame := boardFrame(t, app, 100, 40)
	if strings.Count(frame, tokens.GlyphAccentRail) != 1 {
		t.Fatalf("the board draws %d selection marks, want exactly one:\n%s",
			strings.Count(frame, tokens.GlyphAccentRail), frame)
	}
}

// -- the motion --------------------------------------------------------------

// The reported defect, as a test: "the working row sits completely static". §11
// permits the braille spinner on a running row and permits numbers to tick, and
// this page now spends both.
func TestAWorkingRowMovesBetweenFrames(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	now := fixedNow()
	app.now = func() time.Time { return now }

	first := boardFrame(t, app, 100, 30)
	now = now.Add(blocks.DefaultInterval)
	app.Update(animTickMsg{})
	second := boardFrame(t, app, 100, 30)
	if first == second {
		t.Fatalf("nothing moved on a board with a job running:\n%s", first)
	}
	// It is the SPINNER that moved, from the house set and nothing invented.
	spun := false
	for _, frame := range blocks.Spinner {
		if strings.Contains(first, frame) || strings.Contains(second, frame) {
			spun = true
		}
	}
	if !spun {
		t.Fatalf("the working row is not wearing the house spinner:\n%s", second)
	}
	// A repaint INSIDE one step is byte-identical: the frame index is
	// floor(now/interval), so a tick that lands mid-step produces zero dirty
	// rows (8.1.3).
	now = now.Add(blocks.DefaultInterval / 4)
	app.Update(animTickMsg{})
	if third := boardFrame(t, app, 100, 30); third != second {
		t.Fatalf("a repaint inside one animation step redrew the page:\n%s\n---\n%s", second, third)
	}
}

// And a settled board holds still. A page that ticked with nothing running would
// be a window burning wakeups to redraw bytes nobody can tell apart.
func TestASettledBoardHoldsStill(t *testing.T) {
	backend := pageBackend(t)
	for i := range backend.nodes {
		backend.nodes[i].Status = store.Done
	}
	app := pageAppOn(t, backend, tokens.NoColor)
	app.showPage(pageBoard)
	now := fixedNow()
	app.now = func() time.Time { return now }

	first := boardFrame(t, app, 100, 30)
	now = now.Add(5 * blocks.DefaultInterval)
	app.Update(animTickMsg{})
	if second := boardFrame(t, app, 100, 30); first != second {
		t.Fatalf("a settled board animated:\n%s\n---\n%s", first, second)
	}
	if app.boardIsLive() {
		t.Fatal("a settled board is arming the animation clock")
	}
	if app.roomIsLive() {
		t.Fatal("a settled board is claiming the window is live")
	}
}

// The clock is armed while the board is the lens and something is working —
// which is what makes the spinner turn in the shipped window, not only in a test
// that drives Frame by hand.
func TestTheBoardArmsTheAnimationClockWhileWorkIsLive(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	if !app.boardIsLive() {
		t.Fatal("a board with a running job does not report itself live")
	}
	if !app.roomIsLive() {
		t.Fatal("the animation chain was not told the board is live")
	}
	if cmd := app.startTick(); cmd == nil {
		t.Fatal("the animation clock did not arm for the board")
	}
	// A detail page is a document and nothing on it moves, so it stands the
	// clock down again.
	app.openBoardDetail(boardOpensService, "service-1", 0)
	if app.boardIsLive() {
		t.Fatal("a detail page is keeping the animation clock armed")
	}
}

// Reduced motion is a promise about MOVEMENT and not about staleness: the glyph
// stops, the clock keeps ageing.
func TestCalmFreezesTheGlyphAndKeepsTheClockTicking(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	app.transcript.Clock().Calm = true
	now := fixedNow()
	app.now = func() time.Time { return now }

	first := boardFrame(t, app, 100, 30)
	for _, frame := range blocks.Spinner {
		if strings.Contains(first, frame) {
			t.Fatalf("a calm board is still drawing a spinner frame (%q):\n%s", frame, first)
		}
	}
	if !strings.Contains(first, tokens.GlyphWorking) {
		t.Fatalf("a calm board dropped the working state glyph:\n%s", first)
	}
	// A minute later the reading has aged even though nothing moved.
	now = now.Add(time.Minute)
	app.Update(animTickMsg{})
	second := boardFrame(t, app, 100, 30)
	if first == second {
		t.Fatalf("the elapsed reading froze along with the glyph:\n%s", second)
	}
	for _, frame := range blocks.Spinner {
		if strings.Contains(second, frame) {
			t.Fatalf("a calm board started spinning after a minute:\n%s", second)
		}
	}
}

// -- the width sweep ---------------------------------------------------------

// The page survives every terminal at every colour tier, and no row ever spills.
func TestTheBoardSurvivesEveryWidthAtEveryProfile(t *testing.T) {
	for _, profile := range []tokens.Profile{
		tokens.NoColor, tokens.ANSI16, tokens.ANSI256, tokens.TrueColor,
	} {
		app := pageAppOn(t, pageBackend(t), profile)
		app.showPage(pageBoard)
		app.board.open = true

		for width := 1; width <= 140; width++ {
			for _, height := range []int{1, 2, 5, 24} {
				out := app.Frame(width, height)
				for _, row := range strings.Split(out, "\n") {
					if got := len([]rune(ansi.Strip(row))); got > width {
						t.Fatalf("a board row at width %d (%v) is %d cells: %q",
							width, profile, got, row)
					}
				}
			}
		}
		// And the detail pages, which are a different renderer over the same row
		// builder.
		app.openBoardDetail(boardOpensCharter, "charter-1", 0)
		for width := 1; width <= 140; width++ {
			for _, row := range strings.Split(app.Frame(width, 24), "\n") {
				if got := len([]rune(ansi.Strip(row))); got > width {
					t.Fatalf("a charter page row at width %d (%v) is %d cells: %q",
						width, profile, got, row)
				}
			}
		}
	}
}

// §5b's proximity fix has a width consequence worth pinning: the receipt no
// longer competes with the name for cells, so a narrow board keeps BOTH.
func TestANarrowBoardKeepsTheNameAndTheMoney(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	narrow := ansi.Strip(app.Frame(46, 30))
	if !strings.Contains(narrow, tokens.Money(8.65)) {
		t.Fatalf("a narrow board dropped the money:\n%s", narrow)
	}
	if !strings.Contains(narrow, "wisp-parity") {
		t.Fatalf("a narrow board dropped the name:\n%s", narrow)
	}
}

// A page taller than the board is not a page with rows off the bottom, and a
// board taller than the page keeps the cursor's row on screen.
func TestTheBoardKeepsTheCursorOnScreen(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageBoard)
	app.board.open = true
	lines := app.boardLines()
	doors := boardDoors(lines)
	const height = 6
	for cursor := range doors {
		top := boardTop(lines, cursor, height)
		if doors[cursor] < top || doors[cursor] >= top+height {
			t.Fatalf("door %d sits on line %d, outside the window [%d,%d)",
				cursor, doors[cursor], top, top+height)
		}
	}
}

// 5.14: an id is never drawn. Not a node id, not a charter id, not a service id,
// not a PID, and not a provider slug.
func TestNoBoardFrameEverShowsAnIdOrASlug(t *testing.T) {
	backend := pageBackend(t)
	backend.models = map[string][]string{"job-1": {"anthropic/claude-sonnet-4.6"}}
	app := pageAppOn(t, backend, tokens.NoColor)
	app.showPage(pageBoard)
	app.board.open = true

	frames := []string{boardFrame(t, app, 120, 40)}
	app.openBoardDetail(boardOpensCharter, "charter-1", 0)
	frames = append(frames, boardFrame(t, app, 120, 40))
	app.openBoardDetail(boardOpensService, "service-1", 0)
	frames = append(frames, boardFrame(t, app, 120, 40))

	for _, frame := range frames {
		for _, banned := range []string{
			"job-1", "job-old-1", "charter-1", "service-1", "4242",
			"anthropic/", "task:", rowTaskPrefix,
		} {
			if strings.Contains(frame, banned) {
				t.Fatalf("the frame is reading %q out loud:\n%s", banned, frame)
			}
		}
	}
}

// -- the notebook page -------------------------------------------------------

// The notebook tab swaps in the homes lane's page, drawn through the interface
// this side wires against.
func TestTheNotebookTabDrawsTheHomesPage(t *testing.T) {
	app := pageApp(t)
	app.showPage(pageNotebook)
	if app.notebook == nil {
		t.Fatal("no notebook page was built")
	}
	frame := ansi.Strip(app.Frame(100, 20))
	if !strings.Contains(frame, "beliefs") {
		t.Fatalf("the notebook page is not on screen:\n%s", frame)
	}
	// The sidebar is a different list about a different thing, so this page HAS
	// one — it opens like every other page that can carry a column (§6, and
	// [App.railState]) — and the chord keeps its PAGE meaning here, moving the
	// keyboard between the notebook and the mouth.
	if !strings.Contains(frame, railSentinel) {
		t.Fatalf("the notebook page dropped the sidebar:\n%s", frame)
	}
	if app.dockCounts().Shown {
		t.Fatal("the notebook drew a dock beside an open rail")
	}
	// Collapsed, the page still reaches the map through the dock in the bar row.
	app.toggleRail()
	app.toggleRail()
	if shut := ansi.Strip(app.Frame(100, 20)); strings.Contains(shut, railSentinel) {
		t.Fatalf("the notebook page refuses to collapse the sidebar:\n%s", shut)
	}
	if !app.dockCounts().Shown {
		t.Fatal("the notebook page draws no dock, so the collapsed rail has no door")
	}
}

// A settled job past the `recent` cap is not a job that disappeared. It is on
// the rail's home scope and off the board's live sections, so history is what
// has to carry it — and history is measured against what this page DRAWS.
func TestASettledJobPastTheRecentCapFallsIntoHistory(t *testing.T) {
	backend := pageBackend(t)
	for i := 0; i < boardRecentCap+2; i++ {
		id := "job-settled-" + string(rune('a'+i))
		backend.nodes = append(backend.nodes, store.Node{
			ID: id, Title: "settled-" + string(rune('a'+i)), Status: store.Done,
			CreatedSeq: int64(20 + i),
		})
	}
	backend.addressable = append([]store.Node(nil), backend.nodes...)

	app := pageAppOn(t, backend, tokens.NoColor)
	app.showPage(pageBoard)
	app.board.open = true

	drawn := map[string]bool{}
	for _, line := range app.boardLines() {
		if line.kind != boardEntry || line.target != boardOpensRoom {
			continue
		}
		if drawn[line.id] {
			t.Fatalf("the board drew %q twice", line.row.Name)
		}
		drawn[line.id] = true
	}
	for _, node := range backend.nodes {
		if node.Parent != "" {
			continue // a part is not a job
		}
		if !drawn[rowTaskPrefix+node.ID] {
			t.Fatalf("%q is on the board nowhere at all", node.Title)
		}
	}
}

// -- helpers -----------------------------------------------------------------

// boardLineText is one line as the reader sees it, spans joined.
func boardLineText(line boardLine) string {
	var out strings.Builder
	for _, span := range line.spans {
		out.WriteString(span.text)
	}
	return out.String()
}

// boardEntryFor is the name line of a row, by the name drawn on it.
func boardEntryFor(t *testing.T, app *App, name string) boardLine {
	t.Helper()
	for _, line := range app.boardLines() {
		if line.kind == boardEntry && strings.Contains(boardLineText(line), name) {
			return line
		}
	}
	t.Fatalf("no board row named %q", name)
	return boardLine{}
}

// boardMetaFor is the receipt line under a named row.
func boardMetaFor(t *testing.T, app *App, name string) boardLine {
	t.Helper()
	lines := app.boardLines()
	for i, line := range lines {
		if line.kind != boardEntry || !strings.Contains(boardLineText(line), name) {
			continue
		}
		if i+1 < len(lines) && lines[i+1].kind == boardMeta {
			return lines[i+1]
		}
		t.Fatalf("%q has a name line and no receipt under it", name)
	}
	t.Fatalf("no board row named %q", name)
	return boardLine{}
}

// boardDoorOf is the door index of a row, by what it opens and what it says — or
// of the first door of that kind when the name is empty.
func boardDoorOf(t *testing.T, app *App, target boardTarget, name string) int {
	t.Helper()
	lines := app.boardLines()
	for door, at := range boardDoors(lines) {
		if lines[at].target != target {
			continue
		}
		if name == "" || strings.Contains(boardLineText(lines[at]), name) {
			return door
		}
	}
	t.Fatalf("no %v door named %q on the board", target, name)
	return 0
}

// boardScreenRow is the pane-local row a named entry is drawn on.
func boardScreenRow(t *testing.T, app *App, name string) int {
	t.Helper()
	lines := app.boardLines()
	top := boardTop(lines, app.board.cursor, app.board.height)
	for i := top; i < len(lines); i++ {
		if lines[i].kind == boardEntry && strings.Contains(boardLineText(lines[i]), name) {
			return i - top
		}
	}
	t.Fatalf("%q is not on the board's frame", name)
	return 0
}

// typed is one letter the way a terminal delivers it.
func typed(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }
