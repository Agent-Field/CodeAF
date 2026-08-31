package tui3

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/lane"
)

// ── A LEDGER THIS TEST WROTE ────────────────────────────────────────────────
//
// The surface reads whatever [lane.Default] holds, and what it holds in a build
// is the empty ledger that believes nothing. So every test here installs its
// own, states the numbers it wants drawn, and puts the registry back — a suite
// that left a ledger installed would be a suite where the next test's "unknown
// draws nothing" was a lie.

type fakeLedger struct{ rows map[string][]lane.Belief }

func (f *fakeLedger) Note(lane.Sighting)                 {}
func (f *fakeLedger) NoteOutcome(lane.Outcome)           {}
func (f *fakeLedger) Prime(lane.Row, float64)            {}
func (f *fakeLedger) Beliefs(model string) []lane.Belief { return f.rows[model] }

func (f *fakeLedger) Belief(id lane.ID) (lane.Belief, bool) {
	for _, belief := range f.rows[id.Model] {
		if belief.ID == id {
			return belief, true
		}
	}
	return lane.Belief{}, false
}

// laneBelief is one lane at a stated median, with a spread narrow enough that
// its p99 is nowhere near a tail. Spread is the standard deviation of the log,
// which is the only place in this file that has to know the filter is in the
// log domain.
func laneBelief(model, name string, ttftMS, rate float64, spread float64, facts lane.Facts) lane.Belief {
	return lane.Belief{
		ID:      lane.ID{Model: model, Lane: name},
		Facts:   facts,
		TTFT:    lane.Posterior{X: math.Log(ttftMS), P: spread * spread},
		Rate:    lane.Posterior{X: math.Log(rate), P: 0.01},
		Quality: lane.Beta{A: 9, B: 1},
		At:      time.Now(),
	}
}

// laneLab installs a ledger for the duration of one test and empties the desk
// of anything a previous one posted.
func laneLab(t *testing.T, rows map[string][]lane.Belief) {
	t.Helper()
	forgetLanes()
	lane.Default().SetLedger(&fakeLedger{rows: rows})
	t.Cleanup(func() {
		lane.Default().Reset()
		forgetLanes()
	})
}

const flash = "deepseek/deepseek-v4-flash"

// threeLanes is the shape the ideation measured: a fast one that cannot take a
// tool call, a cheap one with a tail, and one that truncates.
func threeLanes() map[string][]lane.Belief {
	return map[string][]lane.Belief{
		flash: {
			laneBelief(flash, "Cloudflare", 800, 58, 0.1, lane.Facts{
				Uptime5m: 100, PriceOut: 1.32e-6, MaxOut: 345_000, Quant: "fp8",
			}),
			laneBelief(flash, "CoreWeave", 430, 24, 0.8, lane.Facts{
				Tools: true, Uptime5m: 99, PriceOut: 0.28e-6, MaxOut: 943_000, Quant: "fp8",
			}),
			laneBelief(flash, "DeepInfra", 1200, 27, 0.1, lane.Facts{
				Tools: true, Uptime5m: 99, PriceOut: 0.18e-6, MaxOut: 65_000, Quant: "fp8",
			}),
		},
	}
}

var laneCatalog = []Model{
	{ID: flash, ContextLength: 1_000_000},
	{ID: "openai/gpt-4.1-mini", ContextLength: 128_000},
	{ID: "gpt-5-classic", ContextLength: 400_000},
	{ID: "anthropic/claude-gpt-echo", ContextLength: 200_000},
	{ID: "moonshotai/kimi-k3"},
}

func laneApp(t *testing.T) *app {
	t.Helper()
	a := pickerApp(t, &fakeAgent{model: flash}, laneCatalog)
	a.profileDir = t.TempDir()
	return a
}

// ── 1. the fold ─────────────────────────────────────────────────────────────

// `→` OPENS THE MACHINES BEHIND ONE MODEL, with the numbers this process
// believes: the three rows, the auto row that says which of them is answering
// now, and the openrouter row that declines to name one.
func TestArrowUnfoldsTheLanesTheLedgerBelievesIn(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.width = 100
	typeLine(t, a, "/model")

	drive(t, a, key("right"))
	if a.pick.unfold != flash {
		t.Fatalf("→ on the model in use left the fold at %q", a.pick.unfold)
	}
	screen := plain(frame(a))
	for _, want := range []string{
		"● auto", "picks the fastest lane each answer", "recommended",
		"cloudflare", "0.8s", "58 t/s", "100%", "no tools",
		"coreweave", "0.4s", "tail",
		"deepinfra", "out ≤ 65k",
		"○ openrouter", "let the router balance on price",
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the fold does not say %q:\n%s", want, screen)
		}
	}

	// The why line explains the row under the cursor, and only that row.
	drive(t, a, key("down"))
	drive(t, a, key("down"))
	if got := plain(frame(a)); !strings.Contains(got, "cloudflare: first token 0.8s") ||
		!strings.Contains(got, "from the sheet") {
		t.Fatalf("no why line under the cursor:\n%s", got)
	}

	// ← closes it again and puts the cursor back on the model.
	drive(t, a, key("left"))
	if a.pick.unfold != "" {
		t.Fatal("← left the lanes open")
	}
	if chosen, _ := a.pick.choice(); chosen.ID != flash {
		t.Fatalf("folding left the cursor on %q", chosen.ID)
	}
}

// AND THE MODEL'S OWN ROW GAINS THE SPEED, which is the same belief said in one
// line: the best lane's first token, its rate, and who is answering.
func TestTheModelRowCarriesTheSpeedOfItsBestLane(t *testing.T) {
	laneLab(t, threeLanes())
	note := modelNote(Model{ID: flash, ContextLength: 1_000_000})
	if !strings.Contains(note, "▲0.8s") || !strings.Contains(note, "58t/s") {
		t.Fatalf("the row says %q, want the posterior of the best lane", note)
	}
	if !strings.Contains(note, "via cloudflare") {
		t.Fatalf("the row says %q, want the lane that is answering", note)
	}
}

// ── 2. the emptiness law ────────────────────────────────────────────────────

// A MODEL NOBODY HAS MEASURED DRAWS NOTHING AT ALL: no speed on the row, and no
// fold to open. An invented figure here would be a router steering on a
// measurement nobody took.
func TestAnUnmeasuredModelDrawsNoSpeedAndDoesNotUnfold(t *testing.T) {
	forgetLanes()
	lane.Default().Reset()
	a := laneApp(t)
	typeLine(t, a, "/model")

	if note := modelNote(laneCatalog[0]); note != "1M" {
		t.Fatalf("an unmeasured row says %q, want the window and nothing more", note)
	}
	drive(t, a, key("right"))
	if a.pick.unfold != "" {
		t.Fatal("→ opened a fold over a model nothing is believed about")
	}
	screen := plain(frame(a))
	for _, forbidden := range []string{"auto", "openrouter", "t/s", "▲"} {
		if strings.Contains(screen, forbidden) {
			t.Fatalf("an unmeasured picker draws %q:\n%s", forbidden, screen)
		}
	}
}

// ── 3. the filter grammar ───────────────────────────────────────────────────

// `@name` KEEPS THE MODELS SERVED BY THAT LANE, and opens the first of them on
// the lane that was asked about.
func TestTheLaneFilterKeepsTheModelsThatLaneServesAndOpensIt(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	typeLine(t, a, "/model")

	typeInto(t, a, "@cloud")
	if got := pickerIDs(a); len(got) != 1 || got[0] != flash {
		t.Fatalf("@cloud kept %v, want only the model with that lane", got)
	}
	if a.pick.unfold != flash {
		t.Fatal("an @ filter has to open the row it was about")
	}
	if len(a.pick.lanes) == 0 || !strings.EqualFold(a.pick.lanes[0].Name, "Cloudflare") {
		t.Fatalf("the lane asked about is not first: %+v", a.pick.lanes)
	}
}

// `<1s` IS A BOUND ON THE FIRST TOKEN of the best lane, and it is measured
// against the posterior rather than against anything published.
func TestTheSpeedFilterKeepsWhatStartsInTime(t *testing.T) {
	slow := "vendor/slow-model"
	rows := threeLanes()
	rows[slow] = []lane.Belief{
		laneBelief(slow, "GMICloud", 3030, 30, 0.1, lane.Facts{Tools: true, Uptime5m: 97}),
	}
	laneLab(t, rows)
	a := pickerApp(t, &fakeAgent{model: flash}, append(append([]Model{}, laneCatalog...), Model{ID: slow}))
	a.profileDir = t.TempDir()
	typeLine(t, a, "/model")

	typeInto(t, a, "<1s")
	if got := pickerIDs(a); len(got) != 1 || got[0] != flash {
		t.Fatalf("<1s kept %v, want only what starts inside a second", got)
	}

	drive(t, a, key("ctrl+u"))
	typeInto(t, a, ">50t/s")
	if got := pickerIDs(a); len(got) != 1 || got[0] != flash {
		t.Fatalf(">50t/s kept %v", got)
	}

	drive(t, a, key("ctrl+u"))
	typeInto(t, a, "$<0.3")
	if got := pickerIDs(a); len(got) != 1 || got[0] != flash {
		t.Fatalf("$<0.3 kept %v — the best lane's price per million", got)
	}
}

// AND EVERYTHING ELSE RANKS EXACTLY AS IT ALWAYS DID. This is the promise the
// grammar is built on: a word it does not recognise is a word to search for,
// with the same three tiers and the same order, whether or not a single lane
// has ever been measured.
func TestAnUnparsedTokenRanksTheWayItAlwaysHas(t *testing.T) {
	want := []string{"gpt-5-classic", "openai/gpt-4.1-mini", "anthropic/claude-gpt-echo"}

	forgetLanes()
	lane.Default().Reset()
	cold := laneApp(t)
	typeLine(t, cold, "/model")
	typeInto(t, cold, "gpt")
	if got := pickerIDs(cold); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("with no lanes at all: %v, want %v", got, want)
	}

	laneLab(t, threeLanes())
	warm := laneApp(t)
	typeLine(t, warm, "/model")
	typeInto(t, warm, "gpt")
	if got := pickerIDs(warm); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("with lanes believed in: %v, want the same %v", got, want)
	}
}

// ── 4. pinning ──────────────────────────────────────────────────────────────

// ENTER ON A LANE WRITES THE ROW, and enter on the auto row above it takes the
// pin back off.
func TestEnterOnALanePinsItAndAutoTakesItBack(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	typeLine(t, a, "/model")
	drive(t, a, key("right"))
	drive(t, a, key("down")) // the auto row
	drive(t, a, key("down")) // the first lane
	drive(t, a, key("enter"))

	if a.pick.open {
		t.Fatal("pinning a lane left the picker open")
	}
	name, pinned := config.LanePinned(a.profileDir, talkSlot)
	if !pinned || !strings.EqualFold(name, "Cloudflare") {
		t.Fatalf("the profile holds %q (pinned=%v), want Cloudflare", name, pinned)
	}
	if config.LaneBorrowAt(a.profileDir, talkSlot) {
		t.Fatal("a pin made from the picker must not also borrow")
	}

	// And the picker opens with that row marked, which is how a person sees
	// what they are on without changing anything.
	typeLine(t, a, "/model")
	if a.pick.pin != "Cloudflare" {
		t.Fatalf("the picker opened with pin %q", a.pick.pin)
	}

	drive(t, a, key("right"))
	drive(t, a, key("down"))
	drive(t, a, key("enter")) // auto
	if _, pinned := config.LanePinned(a.profileDir, talkSlot); pinned {
		t.Fatal("enter on auto left a pin behind")
	}
	if got := config.LaneAt(a.profileDir, talkSlot); got != config.LaneAuto {
		t.Fatalf("the row reads %q after auto", got)
	}
}

// /model @name AND /model auto ARE THE SAME TWO WRITES from the keyboard, and
// neither of them changes the model.
func TestSlashModelPinsAndUnpinsTheLane(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)

	typeLine(t, a, "/model @cloudflare")
	if a.model != flash {
		t.Fatalf("pinning a lane moved the model to %q", a.model)
	}
	if name, pinned := config.LanePinned(a.profileDir, talkSlot); !pinned || name != "cloudflare" {
		t.Fatalf("the profile holds %q (pinned=%v)", name, pinned)
	}

	typeLine(t, a, "/model auto")
	if _, pinned := config.LanePinned(a.profileDir, talkSlot); pinned {
		t.Fatal("/model auto did not clear the pin")
	}

	// AND A QUESTION OPENS THE LIST rather than switching to a slug nobody
	// meant: two words were never a name.
	typeLine(t, a, "/model deepseek <1s")
	if !a.pick.open {
		t.Fatal("/model with a filter query has to open the picker")
	}
	if got := a.pick.filter.String(); got != "deepseek <1s" {
		t.Fatalf("the picker opened on the filter %q", got)
	}
	if got := pickerIDs(a); len(got) != 1 || got[0] != flash {
		t.Fatalf("the pre-filtered list is %v", got)
	}
}

// ── 5. the settings rows ────────────────────────────────────────────────────

// THE LANE ROW WALKS ITS FOUR ANSWERS and writes each of them where the picker
// reads them from, and the speed guard is a plain on/off beside it.
func TestTheSettingsLaneRowWalksAutoPinnedBorrowAndOpenrouter(t *testing.T) {
	laneLab(t, threeLanes())
	a, dir := sheetApp(t)
	a.model = flash
	a.models = func() []Model { return laneCatalog }
	a.openSettings()
	for i := 0; i < 4; i++ {
		drive(t, a, key("right")) // Providers
	}

	cursorTo(t, a, config.LaneSettingKey(talkSlot))
	if !sheetHas(a, "lane") {
		t.Fatal("the providers tab has no lane row")
	}
	drive(t, a, key("enter"))
	if got := config.LaneRowWord(dir, talkSlot); got != "pinned: Cloudflare" {
		t.Fatalf("the first step of the walk wrote %q", got)
	}
	drive(t, a, key("enter"))
	if got := config.LaneRowWord(dir, talkSlot); got != "pinned: Cloudflare, borrow when slow" {
		t.Fatalf("the second step wrote %q", got)
	}
	if !config.LaneBorrowAt(dir, talkSlot) {
		t.Fatal("borrow when slow did not reach the profile")
	}
	drive(t, a, key("enter"))
	if got := config.LaneAt(dir, talkSlot); got != config.LaneOpenRouter {
		t.Fatalf("the third step wrote %q", got)
	}
	drive(t, a, key("enter"))
	if got := config.LaneAt(dir, talkSlot); got != config.LaneAuto {
		t.Fatalf("the walk did not come back to auto: %q", got)
	}

	// The speed guard is on until somebody says otherwise, and enter turns it.
	if !config.LaneGuardAt(dir) {
		t.Fatal("the speed guard must be on by default")
	}
	cursorTo(t, a, config.KeyLaneGuard)
	drive(t, a, key("enter"))
	if config.LaneGuardAt(dir) {
		t.Fatal("enter did not turn the speed guard off")
	}
}

// AND THE MODEL ROW SAYS WHICH LANE IS ANSWERING IT, which is the other half of
// what a person opening this tab is asking.
func TestTheModelRowInSettingsNamesTheLane(t *testing.T) {
	laneLab(t, threeLanes())
	a, _ := sheetApp(t)
	a.model = flash
	a.sheet.sessionModel = flash
	a.openSettings()
	for i := 0; i < 4; i++ {
		drive(t, a, key("right"))
	}
	if !sheetHas(a, "auto (cloudflare now)") {
		t.Fatalf("the model row does not name the lane:\n%s", strings.Join(sheetLabels(a), "\n"))
	}
}

// ── 6. the status line ──────────────────────────────────────────────────────

// THE THREE READINGS OF THE SERVED SEGMENT, driven through the hook the layer
// that sends an answer will post on ([PostLaneNews]).
func TestTheStatusLineSaysWhoServedWhatIsBeingTriedAndWhatWasRescued(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.state = stateWorking

	PostLaneNews(LaneNews{Model: flash, Lane: "Cloudflare", TTFT: 600 * time.Millisecond, Rate: 61})
	if got := a.servedRider(); got != " · via cloudflare · 0.6s · 61 t/s" {
		t.Fatalf("an ordinary answer reads %q", got)
	}

	PostLaneNews(LaneNews{Model: flash, Lane: "Cloudflare", Alt: "CoreWeave", Trying: true})
	if got := a.servedRider(); got != " · slow · trying coreweave…" {
		t.Fatalf("a rescue in flight reads %q", got)
	}

	PostLaneNews(LaneNews{
		Model: flash, Lane: "Cloudflare", Alt: "CoreWeave", Winner: "CoreWeave",
		Hedged: true, TTFT: 900 * time.Millisecond,
	})
	if got := a.servedRider(); got != " · via coreweave · rescued" {
		t.Fatalf("a rescued answer reads %q", got)
	}

	// A hedge that LOST is not a rescue: the answer came from where it started.
	PostLaneNews(LaneNews{
		Model: flash, Lane: "Cloudflare", Alt: "CoreWeave", Winner: "Cloudflare",
		Hedged: true, TTFT: 900 * time.Millisecond, Rate: 58,
	})
	if got := a.servedRider(); strings.Contains(got, "rescued") {
		t.Fatalf("a hedge that lost reads %q", got)
	}
}

// AND OUR OWN ANSWERS ARE WHAT THE SPARKLINE DRAWS — the ledger publishes no
// history, so the ring is fed from the same hook the status line reads.
func TestTheSparklineIsOurOwnLastAnswersAndNothingUntilThereAreTwo(t *testing.T) {
	laneLab(t, threeLanes())
	if got := laneViews(flash, time.Now()); len(got) == 0 || len(got[0].Sightings) != 0 {
		t.Fatal("a lane nobody has served for us must carry no history")
	}
	for _, ms := range []int{400, 900, 500} {
		PostLaneNews(LaneNews{Model: flash, Lane: "Cloudflare", TTFT: time.Duration(ms) * time.Millisecond})
	}
	view, ok := laneFor(laneViews(flash, time.Now()), "Cloudflare")
	if !ok || len(view.Sightings) != 3 {
		t.Fatalf("the ring holds %v", view.Sightings)
	}
	_, note := laneRowText(view)
	if !strings.ContainsAny(note, sparkBars) {
		t.Fatalf("the lane row draws no sparkline: %q", note)
	}
}
