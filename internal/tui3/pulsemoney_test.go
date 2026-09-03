package tui3

// THE MONEY ON THE TOP LINE IS THE MONEY IN THE BODY (issue #525).
//
// One frame of the shipped binary drew `$1.85 / $500.00` on the pulse over
// `today $0.13 of $500` in the body of the spend place — two readings of one
// fact, disagreeing where a person can see both at once — and WHICH of the two
// the top line showed depended on the rooms that had been walked through:
// `$1.85` on home, spend, search and settings, `$0.37` on tasks, standing and
// memory. The pulse summed the task records hanging off home's own screen and
// added the standing ledger; the spend place read the usage ledger, which is the
// only reading that sees every call.
//
// So the acceptance is a WALK: every place, in several orders, and the two
// figures are the same number on every one of them.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The fixture's three facts, and they are deliberately three different numbers:
// the ledger is what the machine really spent, the task record is the sliver of
// it the old reading could see, and the two together are what nobody should ever
// be shown.
const (
	moneyLedgerUSD = 1.85
	moneyTaskUSD   = 0.13
)

// oneMoneyLab is a machine that has spent money today with work recorded against
// only a part of it: four ledger rows adding to $1.85, of which one task record
// carries $0.13 and a standing firing carries the rest.
//
// THE LEDGER AND THE TASK INDEX ARE BOTH REAL FILES, because the defect was a
// surface reading the wrong one of them. A lab that wrote only the ledger could
// not tell the fixed reading from the broken one.
func oneMoneyLab(t *testing.T) (*app, func(time.Duration)) {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	// THE ROWS ARE DATED BY THE HOUR OF THE DAY AND NOT BY `now` MINUS AN
	// INTERVAL, because "three hours ago" is yesterday for anybody running this
	// suite before breakfast — which is how a test about TODAY comes to read an
	// empty day on a build machine at ten past midnight.
	day := machineDayStart(now)
	here := lab.workspace("alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", here, now)
	lab.session("-tmp-alpha", "aaaa000000000002", "the filings", here, now.Add(-2*time.Hour))
	// The one piece of work whose cost was written onto a task record. It is a
	// fourteenth of the day's real bill, which is the shape of the frame in the
	// issue.
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "9", Name: "filings", Label: "Read the filings", Title: "Read the filings",
		Status: string(session.TaskDone), SessionID: "aaaa000000000002",
		Cost: moneyTaskUSD, EndedAt: day.Add(9 * time.Hour),
	})

	a := lab.app(mine)
	a.width, a.height = 120, 30
	tick := now
	a.clock = func() time.Time { return tick }

	// EVERY CALL OF THE DAY, which is what the ledger holds and what nothing else
	// on the machine does: the conversation's own turn, two nodes of a piece of
	// work, and a standing order that fired at six.
	lines := []session.UsageLine{
		{At: day.Add(8 * time.Hour), Session: "aaaa000000000001", Model: "m", Calls: 1, USD: 0.42},
		{At: day.Add(9 * time.Hour), Session: "aaaa000000000002", Task: "9", Root: "aaaa000000000002", Calls: 1, USD: moneyTaskUSD},
		{At: day.Add(9 * time.Hour), Session: "aaaa000000000002", Task: "9", Root: "aaaa000000000002", Calls: 1, USD: 0.93},
		{At: day.Add(6 * time.Hour), Standing: "keep-an-eye", Model: "m", Calls: 1, USD: 0.37},
		// AND YESTERDAY, which no reading of TODAY may pick up.
		{At: day.AddDate(0, 0, -1).Add(9 * time.Hour), Session: "aaaa000000000001", Model: "m", Calls: 1, USD: 9.99},
	}
	var file strings.Builder
	for _, line := range lines {
		raw, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		file.Write(raw)
		file.WriteByte('\n')
	}
	ledger := filepath.Join(lab.root, session.UsageLedgerName)
	if err := os.WriteFile(ledger, []byte(file.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if a.usageLedger != ledger {
		t.Fatalf("the lab points at %q and the fixture wrote %q", a.usageLedger, ledger)
	}
	return a, func(d time.Duration) { tick = tick.Add(d) }
}

// pulseMoney is the money segment of the top line, exactly as it is drawn, or ""
// where the line says nothing about money.
var pulseMoneyFigure = regexp.MustCompile(`\$[0-9][0-9,.]*`)

func pulseMoney(a *app) string {
	for _, segment := range a.pulseSegments(a.now(), a.pal) {
		if figure := pulseMoneyFigure.FindString(plain(segment)); figure != "" {
			return figure
		}
	}
	return ""
}

// spendPlaceMoney is the `today` figure in the BODY of the spend place, off the
// reading that page is holding.
func spendPlaceMoney(t *testing.T, a *app) string {
	t.Helper()
	row := plain(a.spend.reading.railsRow(a.width, a.pal))
	if !strings.Contains(row, "today ") {
		t.Fatalf("the spend place's pointer line says nothing about today: %q", row)
	}
	figure := pulseMoneyFigure.FindString(row)
	if figure == "" {
		t.Fatalf("the spend place's pointer line carries no figure: %q", row)
	}
	return figure
}

// walkTo opens one place and fails if it did not open, because a walk that
// silently stayed where it was would prove nothing about anywhere.
func walkTo(t *testing.T, a *app, id page) {
	t.Helper()
	if cmd := a.showPage(id); cmd != nil {
		runCmd(cmd)
	}
	if !a.at(id) {
		t.Fatalf("%s did not open", id.word())
	}
}

// THE ACCEPTANCE: the pulse's figure and the spend place's figure are the same
// number on every place, after any navigation path.
func TestTheMoneyOnTheTopLineIsTheMoneyOnTheSpendPage(t *testing.T) {
	want := dollars(moneyLedgerUSD)
	// The fixture is built so that the OLD reading cannot pass by accident: the
	// task records alone are a fourteenth of the day.
	if dollars(moneyTaskUSD) == want {
		t.Fatal("the fixture cannot tell the two readings apart")
	}

	walks := [][]page{
		{pageHome, pageTasks, pageStanding, pageMemory, pageSpend, pageSearch, pageSettings},
		{pageSettings, pageSearch, pageSpend, pageMemory, pageStanding, pageTasks, pageHome},
		{pageTasks, pageSpend, pageHome, pageMemory, pageSpend, pageStanding, pageSpend},
	}
	for _, walk := range walks {
		a, advance := oneMoneyLab(t)
		var route []string
		for _, id := range walk {
			walkTo(t, a, id)
			route = append(route, id.word())
			// PAST THE MEMO'S OWN LIFETIME, so every place takes its own reading
			// rather than being answered by the one the place before it took.
			advance(homeEvery + time.Second)
			if got := pulseMoney(a); got != want {
				t.Fatalf("the top line reads %q on %s after %s, want %q",
					got, id.word(), strings.Join(route, " → "), want)
			}
			// AND ON THE SPEND PLACE THE TWO ARE ON ONE FRAME, which is where a
			// person met the disagreement.
			if id == pageSpend {
				if body := spendPlaceMoney(t, a); body != want {
					t.Fatalf("the spend place's body reads %q under a top line reading %q",
						body, want)
				}
			}
		}
	}
}

// AND THE DAY IS TODAY: a ledger with nothing on it since midnight draws no
// money segment at all, which is the emptiness law rather than `$0.00`.
func TestATopLineOverAQuietDaySaysNothingAboutMoney(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	here := lab.workspace("alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the one I am in", here, now)
	a := lab.app(mine)
	a.width, a.height = 120, 30
	a.clock = func() time.Time { return now }

	raw, err := json.Marshal(session.UsageLine{
		At:      machineDayStart(now).AddDate(0, 0, -2).Add(9 * time.Hour),
		Session: "aaaa000000000001", Model: "m", Calls: 1, USD: 4.21,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lab.root, session.UsageLedgerName), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	a.openHome()
	if got := pulseMoney(a); got != "" {
		t.Fatalf("the top line drew %q over a day that has spent nothing", got)
	}
}
