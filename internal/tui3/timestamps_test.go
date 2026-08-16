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
func openTurn() *fakeAgent {
	return &fakeAgent{
		model: "openai/gpt-4.1-mini",
		usage: session.Usage{CostUSD: 0.04},
		turns: [][]session.Event{{}, {}, {}},
	}
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
	finishTurn(t, a, agent, text(session.EventTextDelta, "done."))

	advance(time.Minute)
	typeLine(t, a, "second")
	advance(time.Minute)
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
