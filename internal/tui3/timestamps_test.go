package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// clockApp is a surface whose clock a test moves by hand, with the ui.timestamps
// row written into a profile of its own.
//
// The rung is PERSISTED rather than assigned, because the surface re-reads it at
// every turn end (app.go's [app.settle]) exactly as it re-reads the mouse and the
// gate's posture — a test that set the field would be testing a value the next
// settle throws away.
func clockApp(t *testing.T, agent Agent, rung string) (*app, func(time.Duration)) {
	t.Helper()
	dir := t.TempDir()
	body := []byte(`{"` + config.KeyTimestamps + `":"` + rung + `"}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), body, 0o600); err != nil {
		t.Fatalf("writing the profile: %v", err)
	}
	a := newTestApp(agent)
	a.width, a.height = 120, 24
	a.profileDir = dir
	a.timestamps = config.TimestampsAt(dir)
	now := time.Date(2026, 8, 14, 14, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	return a, func(d time.Duration) { now = now.Add(d) }
}

// openTurn is an agent whose turn sends NOTHING on its own: the test delivers
// the turn's events itself, so the clock can move between the question and the
// answer the way it does on a real turn.
// IT OPENS HAVING SPENT NOTHING, which is what a fresh conversation reports and
// what this fixture used not to say: it claimed four cents before anybody had
// typed. That was harmless while the surface only ever learned the total from a
// turn ending, and is not now that it reads the agent's own figure on the first
// frame (app.go's newApp) — an agent already holding four cents is a RESUMED
// conversation with four cents on it, and the receipt draws a turn's OWN spend,
// so a turn that added none of it would rightly say nothing about money.
// [turnSpent] is how a turn's money arrives.
func openTurn() *fakeAgent {
	return &fakeAgent{
		model: "openai/gpt-4.1-mini",
		turns: [][]session.Event{{}, {}, {}},
	}
}

// turnSpent moves the session's running total, the way it moves for real: what
// the agent answers is CUMULATIVE, so a turn's own price is the difference this
// call makes and a turn that adds nothing leaves it where it was.
func turnSpent(agent *fakeAgent, usd float64) {
	agent.usage = session.Usage{CostUSD: usd}
}

// finishTurn delivers one turn's events at whatever the clock now says.
func finishTurn(t *testing.T, a *app, agent *fakeAgent, events ...session.Event) {
	t.Helper()
	for _, ev := range events {
		drive(t, a, streamEventMsg{gen: a.gen, ev: ev})
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventTurnDone}})
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
}

// THE RECEIPT. A finished turn leaves one dim right-aligned line saying when it
// ended, how long it took, how many calls it made and what it cost.
func TestAFinishedTurnLeavesItsReceipt(t *testing.T) {
	agent := openTurn()
	a, advance := clockApp(t, agent, config.TimestampsFooters)

	typeLine(t, a, "what does load.go do?")
	advance(2*time.Minute + 12*time.Second)
	turnSpent(agent, 0.04)
	finishTurn(t, a, agent,
		toolBegin("read", "etc/load.go"),
		toolEnd("read", ""),
		text(session.EventTextDelta, "it parses."))

	line := findRow(t, a, "· 14:02 ·")
	for _, want := range []string{"2m12s", "1 tool", "$0.04"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the receipt is missing %q:\n%s", want, line)
		}
	}
	// RIGHT-ALIGNED: the conversation is read down the left, and a receipt is
	// not part of the reading.
	if strings.HasPrefix(line, "·") {
		t.Fatalf("the receipt was drawn against the left margin:\n%q", line)
	}
	// FROZEN AT COMMIT. Time passing does not rewrite a figure that was true.
	advance(time.Hour)
	a.touch()
	if got := findRow(t, a, "· 14:02 ·"); !strings.Contains(got, "2m12s") {
		t.Fatalf("the receipt moved after the turn ended:\n%s", got)
	}
}

// The turn's price is ITS OWN: a second turn that spends nothing says nothing
// about money rather than repeating the session's running total.
func TestAReceiptCarriesTheTurnsOwnSpendAndNotTheSessions(t *testing.T) {
	agent := openTurn()
	a, advance := clockApp(t, agent, config.TimestampsFooters)

	typeLine(t, a, "first")
	advance(time.Minute)
	turnSpent(agent, 0.04)
	finishTurn(t, a, agent, text(session.EventTextDelta, "done."))

	advance(time.Minute)
	typeLine(t, a, "second")
	advance(time.Minute)
	// The total does not move, which is what a turn that spent nothing looks
	// like from here.
	finishTurn(t, a, agent, text(session.EventTextDelta, "done again."))

	first := findRow(t, a, "· 14:01 ·")
	if !strings.Contains(first, "$0.04") {
		t.Fatalf("the first turn's spend is not on its receipt:\n%s", first)
	}
	second := findRow(t, a, "· 14:03 ·")
	if strings.Contains(second, "$") {
		t.Fatalf("a turn that spent nothing drew a price:\n%s", second)
	}
}

// AND THAT HOLDS ON A RESUMED CONVERSATION, which is where it did NOT before
// #135: the receipt is the session's total minus what the turn opened at, the
// total opened at zero on a resume, and so the first turn back charged itself
// every dollar the conversation had ever spent. The seeding is what fixes it —
// a.cost carries the restored bill on the first frame, so the subtraction has
// something true on both ends — and this pins the claim rather than leaving it
// to follow structurally (#210).
func TestTheFirstTurnAfterAResumeDrawsOnlyItsOwnSpend(t *testing.T) {
	// AN AGENT ALREADY HOLDING MONEY IS A RESUMED CONVERSATION as far as this
	// surface can tell: the engine sums the journal's usage lines into
	// Agent.Usage at construction, and what reaches here is that one figure
	// ([openTurn] says the same thing from the other side). The real journal is
	// opened by costresume_test.go; a receipt needs a turn delivered by hand, so
	// it is this fixture that can carry one.
	agent := openTurn()
	agent.usage = session.Usage{CostUSD: 12.30}
	a, advance := clockApp(t, agent, config.TimestampsFooters)

	if !near(a.cost, 12.30) {
		t.Fatalf("the resumed conversation opened at $%v, want the journal's 12.30", a.cost)
	}

	typeLine(t, a, "carry on where we left off")
	advance(time.Minute)
	// Five cents of new work, on top of everything the conversation had already
	// spent before anybody opened it again.
	turnSpent(agent, 12.35)
	finishTurn(t, a, agent, text(session.EventTextDelta, "done."))

	row := findRow(t, a, "· 14:01 ·")
	if !strings.Contains(row, "$0.05") {
		t.Fatalf("the first turn after a resume does not carry its own five cents:\n%s", row)
	}
	if strings.Contains(row, "$12.3") {
		t.Fatalf("the first turn after a resume charged itself the whole restored bill:\n%s", row)
	}
}

// ctrl+o is "show me the rest of this turn", and the rest of a turn includes
// exactly when it happened.
func TestUnfoldingATurnDatesItsReceipt(t *testing.T) {
	agent := openTurn()
	a, advance := clockApp(t, agent, config.TimestampsFooters)

	typeLine(t, a, "what does load.go do?")
	advance(time.Minute)
	finishTurn(t, a, agent, text(session.EventTextDelta, "it parses."))
	drive(t, a, key("ctrl+o"))

	findRow(t, a, "2026-08-14T14:01:00Z")
}

// THE SEAM. Ten minutes of silence earns a mark, two minutes do not, and a new
// day is named rather than clocked.
func TestTheConversationIsMarkedWhereItWasPutDown(t *testing.T) {
	agent := openTurn()
	a, advance := clockApp(t, agent, config.TimestampsFooters)

	typeLine(t, a, "first")
	finishTurn(t, a, agent, text(session.EventTextDelta, "done."))

	advance(2 * time.Minute)
	typeLine(t, a, "second")
	if at(plainRows(a), "── 14:02") >= 0 {
		t.Fatalf("two minutes drew a seam:\n%s", strings.Join(plainRows(a), "\n"))
	}
	finishTurn(t, a, agent, text(session.EventTextDelta, "done again."))

	advance(25 * time.Hour)
	typeLine(t, a, "the next day")
	page := strings.Join(plainRows(a), "\n")
	if day := "── " + a.now().Format(dayFormat) + " ──"; !strings.Contains(page, day) {
		t.Fatalf("a new day was not named %q:\n%s", day, page)
	}
	if strings.Contains(page, "── 15:0") {
		t.Fatalf("a day boundary drew a clock as well:\n%s", page)
	}
}

// The gap mark, on its own, at the rung that draws nothing else.
func TestTheSeparatorsRungDrawsMarksAndNoReceipts(t *testing.T) {
	agent := openTurn()
	a, advance := clockApp(t, agent, config.TimestampsSeparators)

	typeLine(t, a, "first")
	finishTurn(t, a, agent, text(session.EventTextDelta, "done."))
	advance(30 * time.Minute)
	typeLine(t, a, "back again")

	page := strings.Join(plainRows(a), "\n")
	if !strings.Contains(page, "── 14:30 ──") {
		t.Fatalf("half an hour away drew no seam:\n%s", page)
	}
	if strings.Contains(page, "· 14:00 ·") {
		t.Fatalf("the separators rung drew a receipt as well:\n%s", page)
	}
}

// And off is off: neither shape, at any age.
func TestTheClockCanBeTurnedOffEntirely(t *testing.T) {
	agent := openTurn()
	a, advance := clockApp(t, agent, config.TimestampsOff)

	typeLine(t, a, "first")
	finishTurn(t, a, agent, text(session.EventTextDelta, "done."))
	advance(2 * time.Hour)
	typeLine(t, a, "back again")

	page := strings.Join(plainRows(a), "\n")
	if strings.Contains(page, "14:00") || strings.Contains(page, "16:00") {
		t.Fatalf("the clock is off and the transcript still carries one:\n%s", page)
	}
}

// A turn that has not ended has no receipt: the figures are not true yet.
func TestARunningTurnHasNoReceipt(t *testing.T) {
	agent := openTurn()
	a, advance := clockApp(t, agent, config.TimestampsFooters)

	typeLine(t, a, "what does load.go do?")
	advance(time.Minute)
	drive(t, a, streamEventMsg{gen: a.gen, ev: text(session.EventTextDelta, "reading…")})

	if page := strings.Join(plainRows(a), "\n"); strings.Contains(page, "· 14:0") {
		t.Fatalf("a running turn drew a receipt:\n%s", page)
	}
}
