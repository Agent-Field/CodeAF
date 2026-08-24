package tui3

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// machineBandContext is the machine as the registry sees it, at a width.
func machineBandContext(a *app, now time.Time, width int) bandContext {
	return bandContext{subject: a.machineSubject(), width: width, now: now, pal: a.pal}
}

// middayNow is this file's clock: today's own noon rather than the wall clock.
// Every stamp below sits minutes or hours before "now", and a suite run in the
// first hour of a day pushed those stamps across midnight — the today band then
// honestly counted nothing and the failure read as the band's. Noon keeps every
// small offset inside the day whatever hour the suite runs at; the offsets are
// unchanged, so nothing else about the tests moves.
func middayNow() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.Local)
}

// machineCardText is the whole card at rest, as a reader sees it.
func machineCardText(a *app, width int) string {
	return plain(strings.Join(a.machineCard(width, 40, a.pal), "\n"))
}

// THE MACHINE'S WATCHLIST IS EVERY PROJECT AT ONCE, SOONEST FIRST — and what is
// waiting on a person is not on it, because that is an attention row and belongs
// in another zone.
func TestTheMachineKeepsAnEyeOnEveryProjectSoonestFirst(t *testing.T) {
	lab := newHomeLab(t)
	now := middayNow()
	alpha, beta := lab.workspace("alpha"), lab.workspace("beta")
	mine := lab.session("-alpha", "aaaa000000000001", "Pricing Research", alpha, now.Add(-time.Hour))
	lab.session("-beta", "bbbb000000000001", "Porting", beta, now.Add(-2*time.Hour))

	band := &standBand{}
	soon := bandItem("soon", "check the deploy", beta, standing.WhenEvery, "every 20 minutes")
	soon.NextDue = now.Add(2 * time.Hour)
	later := bandItem("later", "draft the weekly update", alpha, standing.WhenEvery, "mon 8am")
	asks := bandItem("asks", "keep main green", alpha, standing.WhenProbe, "when CI goes red")
	asks.NeedsPerson = "the fix touches migrations"
	paused := bandItem("paused", "watch the invoices", alpha, standing.WhenEvery, "Fridays")
	paused.Status = standing.StatusPaused
	band.items = []standing.Item{later, asks, paused, soon}

	a := lab.app(mine)
	band.wire(a)
	a.openHome()

	got := plain(strings.Join(drawWatchlistBand(a, machineBandContext(a, now, 36)), "\n"))
	if !strings.HasPrefix(got, machineWatchWord) {
		t.Fatalf("the band did not lead with its heading:\n%s", got)
	}
	deploy, weekly := strings.Index(got, "check the deploy"), strings.Index(got, "draft the weekly update")
	if deploy < 0 || weekly < 0 {
		t.Fatalf("the machine's watchlist did not reach both projects:\n%s", got)
	}
	if deploy > weekly {
		t.Fatalf("the soonest thing is not first:\n%s", got)
	}
	if !strings.Contains(got, "in 2h") || !strings.Contains(got, "mon 8am") {
		t.Fatalf("the rows do not say when:\n%s", got)
	}
	if strings.Contains(got, "keep main green") {
		t.Fatalf("an order waiting on a person is drawn here as well as in its own zone:\n%s", got)
	}
	if strings.Contains(got, "watch the invoices") {
		t.Fatalf("a paused order is still said to be keeping an eye on something:\n%s", got)
	}
	for _, line := range drawWatchlistBand(a, machineBandContext(a, now, 36)) {
		if ansi.StringWidth(line) > 36 {
			t.Fatalf("a watchlist row is %d cells: %q", ansi.StringWidth(line), plain(line))
		}
	}

	// NOTHING STANDING DRAWS NOTHING — not a heading over an empty list.
	bare := newHomeLab(t)
	quiet := bare.app(bare.session("-q", "cccc000000000001", "Quiet", bare.workspace("q"), now))
	quiet.openHome()
	if rows := drawWatchlistBand(quiet, machineBandContext(quiet, now, 36)); len(rows) != 0 {
		t.Fatalf("a machine with nothing standing drew %q", rows)
	}
}

// `since you left` GATHERS EVERY PROJECT, FOLDS PAST FOUR, AND EVERY ROW IS A
// DOOR: pressing one opens the conversation it names.
func TestSinceYouLeftGathersEveryProjectAndEveryRowIsADoor(t *testing.T) {
	lab := newHomeLab(t)
	now := middayNow()
	alpha, beta := lab.workspace("alpha"), lab.workspace("beta")
	mine := lab.session("-alpha", "aaaa000000000001", "Pricing Research", alpha, now.Add(-time.Hour))
	theirs := lab.session("-beta", "bbbb000000000001", "Porting", beta, now.Add(-2*time.Hour))
	for i, text := range []string{"the cert expires in 9 days", "the weekly update is written"} {
		if err := standing.Deliver(filepath.Dir(mine), standing.Note{
			At: now.Add(-time.Duration(i+1) * time.Minute), Words: "keep an eye on the cert", Text: text,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := standing.Deliver(filepath.Dir(theirs), standing.Note{
		At: now.Add(-10 * time.Minute), Words: "watch the build", Text: "the build went green",
	}); err != nil {
		t.Fatal(err)
	}

	a := lab.app(mine)
	a.openHome()
	rows := drawSinceLeftBand(a, machineBandContext(a, now, 40))
	got := plain(strings.Join(rows, "\n"))
	if !strings.HasPrefix(got, machineNewsWord) {
		t.Fatalf("the band did not lead with its heading:\n%s", got)
	}
	if !strings.Contains(got, "keep an eye on the cert") || !strings.Contains(got, "watch the build") {
		t.Fatalf("the machine's news did not reach both projects:\n%s", got)
	}
	for _, line := range rows {
		if ansi.StringWidth(line) > 40 {
			t.Fatalf("a news row is %d cells: %q", ansi.StringWidth(line), plain(line))
		}
	}

	// THE DOOR. At rest, a press on the row opens the conversation the news
	// landed in — the same road a row on the left column opens through.
	a.home.cursor = homeRest
	a.machineCard(40, 40, a.pal)
	door := ""
	for _, line := range a.home.machineDoors {
		if strings.Contains(line.text, "watch the build") {
			door = line.text
		}
	}
	if door == "" {
		t.Fatalf("no news row was recorded as a door: %+v", a.home.machineDoors)
	}
	cmd, took := a.machinePress(door)
	if !took {
		t.Fatal("pressing a news row did nothing")
	}
	if cmd != nil {
		cmd()
	}
	if a.home.open {
		t.Fatal("the door did not open the conversation the news named")
	}
	if a.file != theirs {
		t.Fatalf("the door opened %q rather than the conversation the news named", a.file)
	}
}

// `today` IS THE DAY COUNTED AND PRICED, WITH THE CEILING BESIDE THE SPEND —
// and a day with nothing in it draws nothing at all.
func TestTodayCountsTheDayAndObeysTheEmptinessLaw(t *testing.T) {
	t.Setenv("AFORGE_DAILY_BUDGET", "5")
	lab := newHomeLab(t)
	now := middayNow()
	alpha := lab.workspace("alpha")
	mine := lab.session("-alpha", "aaaa000000000001", "Pricing Research", alpha, now.Add(-time.Hour))
	lab.session("-alpha", "aaaa000000000002", "Yesterday", alpha, now.Add(-30*time.Hour))
	lab.task("-alpha", session.TaskIndexEntry{
		ID: "1", Name: "port-the-thing", Label: "Port the thing", Title: "Port the thing",
		Status: string(session.TaskDone), SessionID: "aaaa000000000001",
		Cost: 0.30, EndedAt: now.Add(-20 * time.Minute),
	})
	lab.task("-alpha", session.TaskIndexEntry{
		ID: "2", Name: "old-sweep", Label: "Old sweep", Title: "Old sweep",
		Status: string(session.TaskDone), SessionID: "aaaa000000000002",
		Cost: 9.99, EndedAt: now.Add(-30 * time.Hour),
	})

	a := lab.app(mine)
	a.openHome()
	got := plain(strings.Join(drawTodayBand(a, machineBandContext(a, now, 40)), "\n"))
	if !strings.HasPrefix(got, machineTodayWord) {
		t.Fatalf("the band did not lead with its heading:\n%s", got)
	}
	if !strings.Contains(got, "1 chat") || strings.Contains(got, "2 chats") {
		t.Fatalf("the day counted the wrong conversations:\n%s", got)
	}
	if !strings.Contains(got, "1 task") {
		t.Fatalf("the day counted the wrong work:\n%s", got)
	}
	if !strings.Contains(got, "$0.30 of $5.00") {
		t.Fatalf("the day's spend does not carry the allowance:\n%s", got)
	}
	if strings.Contains(got, "$10") || strings.Contains(got, "$9.99") {
		t.Fatalf("yesterday's bill landed on today's line:\n%s", got)
	}

	// A DAY WITH NOTHING IN IT SAYS NOTHING — not `0 chats · 0 tasks · $0.00`.
	quiet := newHomeLab(t)
	old := quiet.app(quiet.session("-q", "cccc000000000001", "Quiet", quiet.workspace("q"), now.Add(-72*time.Hour)))
	old.openHome()
	if rows := drawTodayBand(old, machineBandContext(old, now, 40)); len(rows) != 0 {
		t.Fatalf("an untouched day drew %q", rows)
	}
}

// THE PULSE IS THE SAME READING THE CARD DRAWS FROM, said in three words — and
// every segment but the clock goes when it is not true.
func TestThePulseSaysWhatIsOnWatchAndWhatTodayCost(t *testing.T) {
	lab := newHomeLab(t)
	now := middayNow()
	alpha := lab.workspace("alpha")
	mine := lab.session("-alpha", "aaaa000000000001", "Pricing Research", alpha, now.Add(-time.Hour))
	lab.task("-alpha", session.TaskIndexEntry{
		ID: "1", Name: "port-the-thing", Label: "Port the thing", Title: "Port the thing",
		Status: string(session.TaskDone), SessionID: "aaaa000000000001",
		Cost: 1.10, EndedAt: now.Add(-20 * time.Minute),
	})
	band := &standBand{items: []standing.Item{
		bandItem("one", "check the deploy", alpha, standing.WhenEvery, "every 20 minutes"),
		bandItem("two", "draft the weekly update", alpha, standing.WhenEvery, "mon 8am"),
	}}

	a := lab.app(mine)
	band.wire(a)
	// ONE INSTANT FOR THE WHOLE TEST, so the assertion about the clock cannot
	// fail on the one run in a thousand that crosses a minute between two calls.
	a.clock = func() time.Time { return now }
	a.openHome()

	line := plain(a.pulseLine(90, a.pal))
	if !strings.HasPrefix(strings.TrimSpace(line), pulseName) {
		t.Fatalf("the pulse does not lead with the program's name: %q", line)
	}
	if !strings.Contains(line, pulseWatchWord+" · 2 orders") {
		t.Fatalf("the pulse does not say what is on watch: %q", line)
	}
	if !strings.Contains(line, "$1.10 today") {
		t.Fatalf("the pulse does not say what the day cost: %q", line)
	}
	if !strings.Contains(line, pulseClock(a.now())) {
		t.Fatalf("the pulse has no clock on it: %q", line)
	}
	if strings.Contains(line, machineOfWord) {
		t.Fatalf("the pulse drew a quota fraction: %q", line)
	}
	if ansi.StringWidth(a.pulseLine(90, a.pal)) > 90 {
		t.Fatalf("the pulse is wider than its frame: %q", line)
	}
	// THE COUNT AND THE CARD ARE ONE READING.
	if orders := len(a.machineFactsAt(a.now()).watching); orders != 2 {
		t.Fatalf("the pulse and the card disagree about how many orders there are: %d", orders)
	}
	// AND IT IS ON THE SCREEN, at the top.
	if head := strings.Split(homeText(a), "\n")[0]; !strings.Contains(head, pulseName) || !strings.Contains(head, pulseWatchWord) {
		t.Fatalf("home's top line is not the pulse: %q", head)
	}

	// A QUIET MACHINE KEEPS ONLY THE CLOCK.
	quiet := newHomeLab(t)
	still := quiet.app(quiet.session("-q", "cccc000000000001", "Quiet", quiet.workspace("q"), now.Add(-72*time.Hour)))
	still.openHome()
	segments := still.pulseSegments(now, still.pal)
	if len(segments) != 1 || plain(segments[0]) != pulseClock(now) {
		t.Fatalf("a quiet machine's pulse says %q", segments)
	}
}

// THE CARD IS PAINTED FOR WHAT THINGS MEAN: one tinted glyph at the head of a
// row and calm words beside it, the working hue on a pass in flight, the amber
// on a bound about to be reached — and never the violet, which on this surface
// means one thing and one thing only.
func TestTheMachineCardPaintsMeaningAndNotMood(t *testing.T) {
	t.Setenv("AFORGE_DAILY_BUDGET", "1")
	lab := newHomeLab(t)
	now := middayNow()
	alpha := lab.workspace("alpha")
	mine := lab.session("-alpha", "aaaa000000000001", "Pricing Research", alpha, now.Add(-time.Hour))
	lab.task("-alpha", session.TaskIndexEntry{
		ID: "1", Name: "sweep", Label: "Sweep", Title: "Sweep",
		Status: string(session.TaskDone), SessionID: "aaaa000000000001",
		Cost: 0.90, EndedAt: now.Add(-time.Minute),
	})
	band := &standBand{running: map[string]standing.RunningMark{
		"live": {PID: 1, Since: now, What: standing.RunningChecking},
	}}
	band.items = []standing.Item{
		bandItem("live", "check the deploy", alpha, standing.WhenEvery, "every 20 minutes"),
		bandItem("calm", "draft the weekly update", alpha, standing.WhenEvery, "mon 8am"),
	}

	a := lab.app(mine)
	band.wire(a)
	a.openHome()
	watch := strings.Join(drawWatchlistBand(a, machineBandContext(a, now, 40)), "\n")
	// THE HEADING IS STRUCTURE AND WEARS THE QUIET ROLE. The accent budget on a
	// screen is one element and it is the live one; a label over a list is not
	// it (homeband_news.go).
	if !strings.Contains(watch, a.pal.muted(fit(machineWatchWord, 40))) {
		t.Fatalf("the heading does not lead in the muted tier:\n%s", plain(watch))
	}
	if strings.Contains(watch, a.pal.accent(fit(machineWatchWord, 40))) {
		t.Fatalf("the heading still spends the accent:\n%s", plain(watch))
	}
	if !strings.Contains(watch, a.pal.muted(homeLiveGlyph)) {
		t.Fatalf("the mark on a pass in flight is not the working hue:\n%s", plain(watch))
	}
	if !strings.Contains(watch, a.pal.dim(standWaitGlyph)) {
		t.Fatalf("a waiting mark is not dim:\n%s", plain(watch))
	}
	if !strings.Contains(watch, a.pal.muted(" draft the weekly update")) {
		t.Fatalf("the words beside a mark are not the calm ink:\n%s", plain(watch))
	}

	today := strings.Join(drawTodayBand(a, machineBandContext(a, now, 40)), "\n")
	if !strings.Contains(today, a.pal.warn("$0.90"+machineOfWord+"$1.00")) {
		t.Fatalf("a day against its allowance is not the amber:\n%s", plain(today))
	}
	if !strings.Contains(a.pulseLine(90, a.pal), a.pal.warn("$0.90"+pulseTodayWord)) {
		t.Fatalf("the pulse's spend is not the amber near the bound: %q", plain(a.pulseLine(90, a.pal)))
	}

	// THE VIOLET IS RESERVED. Seeing it means a person is being waited on, and
	// nothing on this card ever is — what needs somebody is an attention row.
	card := a.machineCard(40, 40, a.pal)
	ask := a.pal.ask("x")
	if prefix := ask[:strings.Index(ask, "x")]; prefix != "" {
		if strings.Contains(strings.Join(card, "\n"), prefix) {
			t.Fatalf("the machine's card wears the question hue:\n%s", plain(strings.Join(card, "\n")))
		}
	}
}

// REST IS A PLACE: walking up off the top row leaves the list, the pane becomes
// the machine's own card, a rescan keeps it, and ↓ walks back in.
func TestWalkingUpOffTheTopRowReachesTheMachineCard(t *testing.T) {
	lab := newHomeLab(t)
	now := middayNow()
	alpha := lab.workspace("alpha")
	mine := lab.session("-alpha", "aaaa000000000001", "Pricing Research", alpha, now.Add(-time.Hour))
	band := &standBand{items: []standing.Item{
		bandItem("one", "check the deploy", alpha, standing.WhenEvery, "every 20 minutes"),
	}}

	a := lab.app(mine)
	band.wire(a)
	a.openHome()
	// AND NOW IT OPENS THERE. The bridge lane landed that law (homebridge.go's
	// [homeView.openAt]), so the walk this test is about starts from a cursor
	// standing in the list — which is where the first ↓ puts it.
	if !a.home.resting() {
		t.Fatal("home did not open at rest")
	}
	a.home.move(1)
	if a.home.resting() {
		t.Fatal("↓ off the machine card did not reach the list")
	}
	for i := 0; i < len(a.home.lines)+2 && !a.home.resting(); i++ {
		a.home.move(-1)
	}
	if !a.home.resting() {
		t.Fatal("walking up off the top row never reached rest")
	}
	if _, ok := a.home.focusedLine(); ok {
		t.Fatal("the cursor is at rest and still says it is on a row")
	}
	card := machineCardText(a, 36)
	if !strings.Contains(card, machineWatchWord) {
		t.Fatalf("the card at rest is not the machine's:\n%s", card)
	}
	if got := plain(strings.Join(a.homeDetail(36, 40, a.pal), "\n")); !strings.Contains(got, machineWatchWord) {
		t.Fatalf("the right column at rest is not the machine's card:\n%s", got)
	}
	// THE SUBJECT AT REST IS THE MACHINE, so `m` acts on the card that is drawn.
	if subject, ok := a.homeSubject(); !ok || subject.kind != bandKindMachine {
		t.Fatalf("the subject at rest is %v (ok=%v)", subject.kind, ok)
	}
	// A RESCAN KEEPS IT. The card must not be taken away three seconds after
	// somebody started reading it.
	a.refreshHome()
	if !a.home.resting() {
		t.Fatal("a rescan walked the cursor back onto a row")
	}
	// AND THE WAY BACK IN IS THE ARROW THAT LEFT.
	a.home.move(1)
	if a.home.resting() {
		t.Fatal("↓ did not walk back into the list")
	}
	if _, ok := a.home.focusedLine(); !ok {
		t.Fatal("↓ left the cursor on no row at all")
	}
}

// ── THE ACCENT BUDGET ───────────────────────────────────────────────────────
//
// ONE SCREEN, ONE ACCENT, AND IT IS ALWAYS THE LIVE THING. Structure — the
// program's name on the top line, the label over a band — wears the quiet roles,
// so that when something IS waiting on somebody there is exactly one place on
// the frame a person's eye goes.
//
// The machine's card at rest is the strongest form of that law this surface can
// assert: nothing on it is waiting, nothing on it is running, and so nothing on
// it may spend the accent AT ALL. A card that lit its four headings had four
// claims on the eye and no answer to "which of these is now".
func TestTheMachineCardAtRestSpendsNoAccent(t *testing.T) {
	lab := newHomeLab(t)
	now := middayNow()
	alpha, beta := lab.workspace("alpha"), lab.workspace("beta")
	mine := lab.session("-alpha", "aaaa000000000001", "Pricing Research", alpha, now.Add(-time.Hour))
	lab.session("-beta", "bbbb000000000001", "Porting", beta, now.Add(-2*time.Hour))

	band := &standBand{}
	band.items = []standing.Item{
		bandItem("one", "check the deploy", beta, standing.WhenEvery, "every 20 minutes"),
		bandItem("two", "draft the weekly update", alpha, standing.WhenEvery, "mon 8am"),
	}
	if err := standing.Deliver(filepath.Dir(mine), standing.Note{
		At: now.Add(-time.Minute), Words: "keep an eye on the cert", Text: "the cert expires in 9 days",
	}); err != nil {
		t.Fatal(err)
	}

	a := lab.app(mine)
	band.wire(a)
	a.clock = func() time.Time { return now }
	a.openHome()

	accent := paintPrefix(a.pal.accent("x"))
	card := strings.Join(a.machineCard(60, 40, a.pal), "\n")
	if strings.Contains(card, accent) {
		t.Fatalf("the machine's card at rest lights something in the accent:\n%s", plain(card))
	}
	// AND THE TOP LINE IS STRUCTURE TOO. The program's name is the same word on
	// every frame home has ever drawn, which is the definition of a thing the eye
	// learns to skip.
	if line := a.pulseLine(90, a.pal); strings.Contains(line, accent) {
		t.Fatalf("home's top line spends the accent on the program's name:\n%s", plain(line))
	}
}
