package tui3

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
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
		"● auto", "weighs speed against price each answer", "recommended",
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
// line: the first token of the lane that would typically answer, its rate, and
// its name. Display asks typically ([lane.Request.Typical]), so two paints of
// the same beliefs name the same machine; the numbers still have to belong to
// the machine the row names.
func TestTheModelRowCarriesTheSpeedOfTheLaneItNames(t *testing.T) {
	laneLab(t, threeLanes())
	note := modelNote(Model{ID: flash, ContextLength: 1_000_000})
	at := strings.Index(note, "via ")
	if at < 0 {
		t.Fatalf("the row says %q, want the lane that is answering", note)
	}
	named, _, _ := strings.Cut(note[at+len("via "):], " · ")
	named = strings.TrimSpace(named)
	view, known := laneExactly(laneViews(flash, timeNow()), named)
	if !known {
		t.Fatalf("the row names %q, which nothing is believed about:\n%s", named, note)
	}
	wantTTFT := laneUpMark + laneSecondsWord(view.TTFT)
	wantRate := strconv.Itoa(int(math.Round(view.Rate))) + "t/s"
	if !strings.Contains(note, wantTTFT) || !strings.Contains(note, wantRate) {
		t.Fatalf("the row says %q, want %s %s — the numbers of the lane it names",
			note, wantTTFT, wantRate)
	}
	if again := modelNote(Model{ID: flash, ContextLength: 1_000_000}); again != note {
		t.Fatalf("a second paint rewrote the row:\n%s\n→\n%s", note, again)
	}
}

// AN OPEN LIST DOES NOT REWRITE ITS ROWS. The ledger can learn a faster
// machine while a turn is running under the overlay; the rows a person is
// reading stay the ones they opened onto. Close and open again to see the
// new via.
func TestAnOpenPickerKeepsTheViaItOpenedWith(t *testing.T) {
	rows := threeLanes()
	laneLab(t, rows)
	a := laneApp(t)
	a.width = 120
	typeLine(t, a, "/model")
	before := plain(frame(a))
	via := pickerVia(before, flash)
	if via == "" {
		t.Fatalf("the open list named no via:\n%s", before)
	}
	rows[flash] = []lane.Belief{
		laneBelief(flash, "Friendli", 100, 200, 0.01, lane.Facts{
			Uptime5m: 100, PriceOut: 0.1e-6, Tools: true, MaxOut: 345_000,
		}),
	}
	after := plain(frame(a))
	if got := pickerVia(after, flash); got != via {
		t.Fatalf("an open picker rewrote via %q → %q:\n%s", via, got, after)
	}
	if strings.Contains(after, "via friendli") {
		t.Fatalf("the open list picked up a ledger that arrived after it opened:\n%s", after)
	}
}

func pickerVia(screen, id string) string {
	for _, line := range strings.Split(screen, "\n") {
		if !strings.Contains(line, id) || !strings.Contains(line, "via ") {
			continue
		}
		_, rest, _ := strings.Cut(line, "via ")
		named, _, _ := strings.Cut(rest, " · ")
		return strings.TrimSpace(named)
	}
	return ""
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

// AN EMPTY PROFILE PATH IS THE ORDINARY LAUNCH AND NOT THE ABSENCE OF ONE.
// `AFORGE_PROFILE_DIR` is unset on almost every machine, and internal/config
// resolves the empty string to the default profile for every read and every
// write in the package. A pin that read it as "nowhere to write" refused on
// every machine anybody actually runs — while the settings row beside it wrote
// fine, because the registry passes that same empty string down.
func TestPinningWritesOnTheProfilePathNobodySet(t *testing.T) {
	laneLab(t, threeLanes())
	a := pickerApp(t, &fakeAgent{model: flash}, laneCatalog)
	a.profileDir = ""
	typeLine(t, a, "/model")

	drive(t, a, key("right"), key("down"), key("down"), key("enter"))
	if name, pinned := config.LanePinned("", talkSlot); !pinned || name != "Cloudflare" {
		t.Fatalf("the default profile holds %q (pinned=%v)", name, pinned)
	}
}

// AND A SURFACE OVER A CONNECTION STILL WRITES NOTHING, which is what that
// guard was reaching for: the profile this laptop can touch is not the one the
// far machine resolved its own lane row out of.
func TestAHostedSurfacePinsNothing(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.host = "blackmac"
	typeLine(t, a, "/model")

	drive(t, a, key("right"), key("down"), key("down"), key("enter"))
	if _, pinned := config.LanePinned(a.profileDir, talkSlot); pinned {
		t.Fatal("a hosted surface wrote a lane pin into this machine's profile")
	}
}

// ── 5. the settings rows ────────────────────────────────────────────────────

// laneSheet is the settings panel open on the Providers tab, over a profile of
// its own, on a frame wide enough to draw a lane row whole.
func laneSheet(t *testing.T) (*app, string) {
	t.Helper()
	a, dir := sheetApp(t)
	a.width = 120
	a.model = flash
	a.sheet.sessionModel = flash
	a.models = func() []Model { return laneCatalog }
	a.openSettings()
	// The bar's own answer, walked rather than counted: a tab inserted before
	// Providers moves this with it.
	for settingTabs[a.sheet.tab] != tabProviders {
		drive(t, a, key("right"))
	}
	return a, dir
}

// THE THREE ROWS THAT NAME A MACHINE SIT DIRECTLY UNDER THE MODEL, and not at
// the foot of the tab under forty rows of roles: a person who has just changed
// their model is exactly the person deciding which endpoint serves it.
func TestTheLaneRowsSitUnderTheModelRow(t *testing.T) {
	laneLab(t, threeLanes())
	a, _ := laneSheet(t)

	want := []string{
		config.ModelSettingKey(talkSlot),
		config.LaneSettingKey(talkSlot),
		config.KeyLaneGuard,
		config.KeyRouting,
	}
	got := make([]string, 0, len(want))
	for _, item := range a.sheet.items {
		if item.restful() && len(got) < len(want) {
			got = append(got, item.row.Key)
		}
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("the tab opens on %v, want %v", got, want)
	}
	// AND ON ONE TAB ONLY. A row a person can meet in two places is two places
	// to look for one answer.
	for _, key := range want[1:] {
		seen := 0
		for _, row := range a.sheet.rows {
			if meta, ok := settingMetaFor(row); ok && row.Key == key && meta.tab != "" {
				seen++
			}
		}
		if seen != 1 {
			t.Fatalf("row %q is placed on %d tabs", key, seen)
		}
	}
}

// THE MODEL ROW IN SETTINGS OPENS THE PICKER /model OPENS — machines and all.
// `→` unfolds the lanes with this ledger's own numbers, and the fold is the
// same three rungs: auto, the machines, openrouter.
func TestTheSettingsModelRowUnfoldsItsLanes(t *testing.T) {
	laneLab(t, threeLanes())
	a, _ := laneSheet(t)

	cursorTo(t, a, config.ModelSettingKey(talkSlot))
	drive(t, a, key("enter"))
	if a.sheet.sel == nil {
		t.Fatal("enter on the model row opened no picker")
	}
	if a.sheet.sel.pick.laneSlot != talkSlot {
		t.Fatalf("the model row's picker is armed for lane slot %q", a.sheet.sel.pick.laneSlot)
	}
	drive(t, a, key("right"))
	if got := a.sheet.sel.pick.unfold; got != flash {
		t.Fatalf("→ in the settings picker left the fold at %q", got)
	}
	screen := strings.Join(sheetLabels(a), "\n")
	for _, want := range []string{
		"auto", "weighs speed against price each answer",
		"cloudflare", "0.8s", "58 t/s", "no tools",
		"coreweave", "0.4s", "deepinfra", "out ≤ 65k",
		"openrouter", "let the router balance on price",
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the unfolded settings picker never said %q:\n%s", want, screen)
		}
	}
	// AND THE FOOT SAYS THE KEY IS THERE, which is the only place this list
	// explains itself.
	if !strings.Contains(a.sheet.keysLine(), "tab lanes") {
		t.Fatalf("the hint does not offer the fold: %q", a.sheet.keysLine())
	}
	// `←` closes it again, from the start of an empty filter box.
	drive(t, a, key("left"))
	if a.sheet.sel.pick.unfold != "" {
		t.Fatal("← left the lanes open")
	}
}

// AND ENTER ON A LANE INSIDE THAT FOLD PINS IT, exactly as it does under
// /model — one list, one gesture, one write.
func TestEnterOnALaneInTheSettingsPickerPins(t *testing.T) {
	laneLab(t, threeLanes())
	a, dir := laneSheet(t)

	cursorTo(t, a, config.ModelSettingKey(talkSlot))
	drive(t, a, key("enter"), key("right"), key("down"), key("down"))
	row, on := a.sheet.sel.pick.laneUnder()
	if !on || row.lane != 0 {
		t.Fatalf("the cursor is not on the first machine: %+v (on=%v)", row, on)
	}
	drive(t, a, key("enter"))
	if a.sheet.sel != nil {
		t.Fatal("enter on a lane left the list open")
	}
	if name, pinned := config.LanePinned(dir, talkSlot); !pinned || name != "Cloudflare" {
		t.Fatalf("the profile holds %q (pinned=%v)", name, pinned)
	}
	// The row and the model row's tail are two readings of one fact, and both
	// of them say so on the very next frame.
	if !sheetHas(a, "pinned: Cloudflare") || !sheetHas(a, "· pinned: cloudflare") {
		t.Fatalf("the panel did not re-read the pin it just wrote:\n%s",
			strings.Join(sheetLabels(a), "\n"))
	}
	// AND THE TAIL SAYS NOTHING ABOUT THE BASE WHILE THE BASE TAKES THE CHOICE.
	// The emptiness law: an unremarkable fact adds no words (issue #433).
	if sheetHas(a, "not taken on this base") {
		t.Fatalf("the panel warned about a base that takes the choice:\n%s",
			strings.Join(sheetLabels(a), "\n"))
	}
}

// AND THE ROW SAYS SO WHEN THE CHOICE IS NOT REACHING THE WIRE.
//
// Issue #433. A base that has answered that it does not take a routing
// preference — a plain endpoint behind AFORGE_BASE_URL, a proxy that strips the
// field — leaves `pinned: Cloudflare` standing as a claim about a request that
// did not carry it. The conversation is told once; this row keeps saying it,
// because it is the row somebody comes back to look at.
func TestThePinnedRowSaysWhenTheBaseWillNotTakeTheChoice(t *testing.T) {
	laneLab(t, threeLanes())
	a, dir := laneSheet(t)

	cursorTo(t, a, config.ModelSettingKey(talkSlot))
	drive(t, a, key("enter"), key("right"), key("down"), key("down"), key("enter"))
	if name, pinned := config.LanePinned(dir, talkSlot); !pinned || name != "Cloudflare" {
		t.Fatalf("the profile holds %q (pinned=%v)", name, pinned)
	}
	// The base answers through the one door the transport files answers with,
	// which is the seam a shipped build writes through too.
	const base = "https://proxy.example/v1"
	lane.WireSheet(base, "", nil, false)
	t.Cleanup(func() { lane.WireSheet("", "", nil, false) })
	if !lane.HeardPrefsSilent(base) {
		t.Fatal("the answer was not filed against the base the sheet is wired to")
	}
	drive(t, a, key("down"), key("up"))
	if !sheetHas(a, "pinned: cloudflare (not taken on this base)") {
		t.Fatalf("the row still reads as though the pin were on the wire:\n%s",
			strings.Join(sheetLabels(a), "\n"))
	}
}

// ENTER ON THE `lane` ROW OPENS THE MACHINES rather than walking four words
// blind: the same fold, on the model in use, with the cursor on the lane in
// force — which with nothing pinned is `auto`.
func TestTheLaneRowOpensTheMachines(t *testing.T) {
	laneLab(t, threeLanes())
	a, dir := laneSheet(t)

	cursorTo(t, a, config.LaneSettingKey(talkSlot))
	if !sheetHas(a, "lane") {
		t.Fatal("the providers tab has no lane row")
	}
	drive(t, a, key("enter"))
	if a.sheet.sel == nil {
		t.Fatal("enter on the lane row opened no list of machines")
	}
	if got := a.sheet.sel.pick.unfold; got != flash {
		t.Fatalf("the lane row opened the fold at %q", got)
	}
	row, on := a.sheet.sel.pick.laneUnder()
	if !on || row.lane != laneAutoAt {
		t.Fatalf("the list did not open on the lane in force: %+v (on=%v)", row, on)
	}
	// Walking to a machine and pressing enter pins it, and nothing about the
	// model changed on the way.
	drive(t, a, key("down"), key("enter"))
	if name, pinned := config.LanePinned(dir, talkSlot); !pinned || name != "Cloudflare" {
		t.Fatalf("the profile holds %q (pinned=%v)", name, pinned)
	}
	if a.model != flash {
		t.Fatalf("choosing a lane changed the model to %q", a.model)
	}
}

// AND WITH NOTHING MEASURED IT WALKS, because auto and openrouter are honestly
// the only two answers there are without a machine to name. The speed guard is
// a plain on/off beside it either way.
func TestTheLaneRowWalksWhenNothingIsMeasured(t *testing.T) {
	laneLab(t, nil)
	a, dir := laneSheet(t)

	cursorTo(t, a, config.LaneSettingKey(talkSlot))
	drive(t, a, key("enter"))
	if a.sheet.sel != nil {
		t.Fatal("a session that has measured nothing opened a list of machines")
	}
	if got := config.LaneAt(dir, talkSlot); got != config.LaneOpenRouter {
		t.Fatalf("the first step of the walk wrote %q", got)
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

// A LEDGER THAT KNOWS NOTHING DRAWS NOTHING LANE-ISH ANYWHERE ON THIS PANEL:
// no tail on the model row, no fold under it in the picker, and no `→ lanes`
// in the hint offering a key that would do nothing.
func TestTheSettingsPanelDrawsNoLanesWhenNothingIsKnown(t *testing.T) {
	laneLab(t, nil)
	a, _ := laneSheet(t)

	for _, unwanted := range []string{"auto (", "pinned:", "via ", "▲"} {
		if sheetHas(a, unwanted) {
			t.Fatalf("an empty ledger drew %q:\n%s", unwanted, strings.Join(sheetLabels(a), "\n"))
		}
	}
	cursorTo(t, a, config.ModelSettingKey(talkSlot))
	drive(t, a, key("enter"), key("right"))
	if a.sheet.sel == nil {
		t.Fatal("the model row opened no picker")
	}
	if a.sheet.sel.pick.unfold != "" {
		t.Fatalf("→ unfolded %q on a ledger that believes nothing", a.sheet.sel.pick.unfold)
	}
}

// A MEDIA SLOT HAS NO LANE ROW BEHIND IT, so its picker is armed with nothing
// and folds nothing — the emptiness law, and the one reason the fold is a slot
// and not a door.
func TestAMediaSlotPickerHasNoLanes(t *testing.T) {
	laneLab(t, threeLanes())
	a, _ := laneSheet(t)

	cursorTo(t, a, config.KeyVisionModel)
	drive(t, a, key("enter"))
	if a.sheet.sel == nil {
		t.Fatal("the looking row opened no picker")
	}
	if a.sheet.sel.pick.laneSlot != "" {
		t.Fatalf("the looking row's picker is armed for lane slot %q", a.sheet.sel.pick.laneSlot)
	}
	if strings.Contains(a.sheet.keysLine(), "tab lanes") {
		t.Fatalf("the hint offers a fold the looking row does not have: %q", a.sheet.keysLine())
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
	for settingTabs[a.sheet.tab] != tabProviders {
		drive(t, a, key("right"))
	}
	if !sheetHas(a, "auto (cloudflare now)") {
		t.Fatalf("the model row does not name the lane:\n%s", strings.Join(sheetLabels(a), "\n"))
	}
}

// ── 6. the status line ──────────────────────────────────────────────────────

// THE THREE READINGS OF THE SERVED SEGMENT, driven through the hook the layer
// that sends an answer will post on ([PostLaneNews]).
//
// Every post names [lane.RoleTalk], because the rider draws only for a role
// somebody is reading and an unnamed role is a hidden errand by construction
// (see [LaneNews.Role]). The test right below states that half.
func TestTheStatusLineSaysWhoServedWhatIsBeingTriedAndWhatWasRescued(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.state = stateWorking

	PostLaneNews(LaneNews{Model: flash, Lane: "Cloudflare", Role: lane.RoleTalk, TTFT: 600 * time.Millisecond, Rate: 61})
	if got := a.servedRider(); got != " · via cloudflare · 0.6s · 61 t/s" {
		t.Fatalf("an ordinary answer reads %q", got)
	}

	PostLaneNews(LaneNews{Model: flash, Lane: "Cloudflare", Alt: "CoreWeave", Role: lane.RoleTalk, Trying: true})
	if got := a.servedRider(); got != " · slow · trying coreweave…" {
		t.Fatalf("a rescue in flight reads %q", got)
	}

	PostLaneNews(LaneNews{
		Model: flash, Lane: "Cloudflare", Alt: "CoreWeave", Winner: "CoreWeave",
		Role: lane.RoleTalk, Hedged: true, TTFT: 900 * time.Millisecond,
	})
	if got := a.servedRider(); got != " · via coreweave · rescued" {
		t.Fatalf("a rescued answer reads %q", got)
	}

	// A hedge that LOST is not a rescue: the answer came from where it started.
	PostLaneNews(LaneNews{
		Model: flash, Lane: "Cloudflare", Alt: "CoreWeave", Winner: "Cloudflare",
		Role: lane.RoleTalk, Hedged: true, TTFT: 900 * time.Millisecond, Rate: 58,
	})
	if got := a.servedRider(); strings.Contains(got, "rescued") {
		t.Fatalf("a hedge that lost reads %q", got)
	}
}

// AND AN ERRAND NOBODY IS READING NEVER MOVES THE RIDER. The title call and the
// memory reflex both finish on some lane during an ordinary talk turn, and the
// status line is about the answer the person is waiting for.
func TestAHiddenRolesAnswerNeverTakesTheServedSegment(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.state = stateWorking

	PostLaneNews(LaneNews{Model: flash, Lane: "Cloudflare", Role: lane.RoleTalk, TTFT: 600 * time.Millisecond, Rate: 61})
	talk := a.servedRider()
	if talk == "" {
		t.Fatal("the talk turn's own answer drew nothing")
	}

	PostLaneNews(LaneNews{Model: flash, Lane: "CoreWeave", Role: lane.RoleAuxiliary, TTFT: 90 * time.Millisecond, Rate: 400})
	if got := a.laneRider(); got != "" {
		t.Fatalf("a naming errand took the status line: %q", got)
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
	_, note := laneRowText(view, 80)
	if !strings.ContainsAny(note, sparkBars) {
		t.Fatalf("the lane row draws no sparkline: %q", note)
	}
}

// ── "2ms" — WHERE A FIRST-TOKEN WAIT IS SAID, AND WHERE IT IS NOT ───────────
//
// A person watching a turn that took half a minute read `2ms` on the status
// line and reasonably concluded the surface was lying about the lane. It was
// not: EVERY first-token wait this file draws is said in SECONDS with one
// decimal ([laneSecondsWord]), and there is no path from a belief or from
// [LaneNews] to a millisecond word at all. `2ms` on that line is the CONNECTION
// segment of a `--host` session — `spark · 2ms`, the round trip to the machine
// the conversation is running on (hostlink.go's latencyWord, the only `ms` on
// this surface) — which sits immediately before the state word and is true of
// the whole line rather than of the answer.
//
// These two tests pin that, so that the next person to read `2ms` can tell the
// two apart without re-deriving it: a lane wait is never spelled in
// milliseconds, and the belief the incident was about renders as `2.4s`.

// TestAFirstTokenWaitIsAlwaysSaidInSeconds walks the belief the field report
// carried — X = 7.79 in the log-millisecond domain, which is 2418 ms — through
// every place this file turns a wait into a word.
func TestAFirstTokenWaitIsAlwaysSaidInSeconds(t *testing.T) {
	const model = "moonshotai/kimi-k3"
	slow := lane.Belief{
		ID:      lane.ID{Model: model, Lane: "DigitalOcean"},
		Facts:   lane.Facts{Tools: true, Uptime5m: 100, PriceOut: 2e-6, MaxOut: 16_384, Quant: "fp8"},
		TTFT:    lane.Posterior{X: 7.79, P: 0.01},
		Rate:    lane.Posterior{X: math.Log(26), P: 0.01},
		Quality: lane.Beta{A: 9, B: 1},
	}
	laneLab(t, map[string][]lane.Belief{model: {slow}})

	views := laneViews(model, time.Now())
	if len(views) != 1 {
		t.Fatalf("one belief and %d rows", len(views))
	}
	if math.Abs(views[0].TTFT-2.418) > 0.002 {
		t.Fatalf("a belief at X=7.79 read back as %.4f seconds", views[0].TTFT)
	}
	head, tail := laneRowText(views[0], 80)
	for what, text := range map[string]string{
		"the model row": laneSpeedWord(views, ""),
		"the lane row":  head + " " + tail,
		"the why line":  laneWhy(views[0]),
	} {
		if !strings.Contains(text, "2.4s") {
			t.Errorf("%s says %q, which does not name the 2.4 second wait it was drawn from", what, text)
		}
		if strings.Contains(text, "ms") {
			t.Errorf("%s says %q — a lane wait is said in seconds on this surface", what, text)
		}
	}
	// And the word itself, over the range a lane really lives in. A first-token
	// wait is never rounded into a millisecond spelling, however small it gets.
	for _, seconds := range []float64{0.002, 0.43, 0.999, 2.418, 30} {
		if got := laneSecondsWord(seconds); !strings.HasSuffix(got, "s") || strings.HasSuffix(got, "ms") {
			t.Errorf("%v seconds reads %q", seconds, got)
		}
	}
	if got := laneSecondsWord(2.418); got != "2.4s" {
		t.Fatalf("2.418 seconds reads %q", got)
	}
}

// TestTheServedRiderSaysTheWaitInSeconds is the same law on the status line,
// which is the segment the person was reading when they saw `2ms`.
func TestTheServedRiderSaysTheWaitInSeconds(t *testing.T) {
	const model = "moonshotai/kimi-k3"
	laneLab(t, map[string][]lane.Belief{model: {
		laneBelief(model, "DigitalOcean", 2418, 26, 0.1, lane.Facts{Tools: true, Uptime5m: 100}),
		laneBelief(model, "Modal", 933, 79, 0.1, lane.Facts{Tools: true, Uptime5m: 100}),
	}})
	a := laneApp(t)
	a.model = model
	a.state = stateWorking

	// The role is named because the rider draws only for a role somebody is
	// reading, and an unnamed role is a hidden errand by construction.
	PostLaneNews(LaneNews{Model: model, Lane: "DigitalOcean", Role: lane.RoleTalk, TTFT: 2418 * time.Millisecond, Rate: 26})
	got := a.servedRider()
	if !strings.Contains(got, "2.4s") {
		t.Fatalf("a 2418ms first token reads %q", got)
	}
	if strings.Contains(got, "ms") {
		t.Fatalf("the served rider spelled a wait in milliseconds: %q", got)
	}
}

// ── THE STANDING IS SAID OUT LOUD ───────────────────────────────────────────

// TestALaneServingBadRepliesSaysSoOnItsRow is the visible half of the quality
// loop. A demotion nobody can see is a harness quietly disagreeing with the
// person about which machine is good, and the whole reason the account belongs
// on the row rather than only in the ledger.
func TestALaneServingBadRepliesSaysSoOnItsRow(t *testing.T) {
	facts := lane.Facts{Tools: true, Quant: "fp8", MaxOut: 200_000, Uptime5m: 100, PriceOut: 0.3}
	now := time.Now()

	// A lane whose answers keep coming back unusable: the quality belief has
	// been walked well under what a conversation asks for.
	poor := laneBelief(flash, "gusher", 400, 200, 0.2, facts)
	poor.Quality = lane.Beta{A: 8, B: 6}
	poor.QualityAt = now
	// And one that has been serving properly all along.
	good := laneBelief(flash, "steady", 500, 180, 0.2, facts)
	good.Quality = lane.Beta{A: 20, B: 1}
	good.QualityAt = now

	laneLab(t, map[string][]lane.Belief{flash: {poor, good}})

	views := laneViews(flash, now)
	if len(views) != 2 {
		t.Fatalf("drew %d rows, want both lanes", len(views))
	}
	for _, view := range views {
		switch view.Name {
		case "gusher":
			if !view.Poor {
				t.Fatal("a lane whose replies keep coming back unusable is drawn as though it were fine")
			}
			if got := laneNote(view); got != "bad replies" {
				t.Fatalf("note = %q, want the row to say what is wrong", got)
			}
		case "steady":
			if view.Poor {
				t.Fatal("a lane that has been serving properly is accused of bad replies")
			}
			if got := laneNote(view); got == "bad replies" {
				t.Fatalf("note = %q on a healthy lane", got)
			}
		}
	}

	// AND A LANE NOBODY HAS JUDGED SAYS NOTHING, which is the emptiness law on
	// this axis: an unjudged lane is not a suspect.
	unjudged := laneBelief(flash, "newcomer", 450, 190, 0.2, facts)
	unjudged.Quality = lane.Beta{}
	if poorlyServing(unjudged, now) {
		t.Fatal("a lane nobody has judged was drawn as a bad one")
	}
}

// ── THE SCREEN SAYS WHAT THE WIRE SAID ──────────────────────────────────────

// A REFUSAL IS NOT SLOWNESS, and for a whole measured run this line said it was:
// a 404 meaning `your request's provider.only preference permits only:
// coreweave` was drawn as `· slow · trying nextbit…`, which is a sentence about
// a wait (issue #266). The word is carried on the news from the layer that read
// the refusal and never decided here.
func TestARefusedLaneIsDrawnRefusedAndNotSlow(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.state = stateWorking

	PostLaneNews(LaneNews{
		Model: flash, Lane: "Cloudflare", Alt: "CoreWeave",
		Role: lane.RoleTalk, Trying: true, Reason: provider.RescueRefused,
	})
	if got := a.laneRider(); got != " · refused · trying coreweave…" {
		t.Fatalf("a refusal in flight reads %q", got)
	}

	// AND A LANE THAT WAS MERELY LATE STILL READS SLOW. The two words are the
	// two facts, and neither is a default for the other.
	PostLaneNews(LaneNews{
		Model: flash, Lane: "Cloudflare", Alt: "CoreWeave",
		Role: lane.RoleTalk, Trying: true, Reason: provider.RescueSlow,
	})
	if got := a.laneRider(); got != " · slow · trying coreweave…" {
		t.Fatalf("a slow lane reads %q", got)
	}
	// A rescue posted before anything classified it keeps the sentence it has
	// always had.
	PostLaneNews(LaneNews{Model: flash, Lane: "Cloudflare", Alt: "CoreWeave", Role: lane.RoleTalk, Trying: true})
	if got := a.laneRider(); got != " · slow · trying coreweave…" {
		t.Fatalf("an unclassified rescue reads %q", got)
	}
}

// AND A PROMISE THAT HAS STOPPED BEING TRUE IS TAKEN BACK. `trying coreweave…`
// is a claim about the present tense; nothing withdrew it when coreweave itself
// was refused, so it sat on the status line until a ten-minute window aged it
// out, describing a request that had already failed.
func TestTheTryingLineIsRetractedWhenTheRescueItNamedFails(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.state = stateWorking

	PostLaneNews(LaneNews{
		Model: flash, Lane: "Cloudflare", Alt: "CoreWeave",
		Role: lane.RoleTalk, Trying: true, Reason: provider.RescueRefused,
	})
	if got := a.laneRider(); !strings.Contains(got, "trying coreweave…") {
		t.Fatalf("the claim was never made: %q", got)
	}

	PostLaneNews(LaneNews{
		Model: flash, Lane: "Cloudflare", Alt: "CoreWeave",
		Role: lane.RoleTalk, Failed: true, Reason: provider.RescueRefused,
	})
	got := a.laneRider()
	if strings.Contains(got, "trying") {
		t.Fatalf("a rescue that failed is still promised: %q", got)
	}
	if got != " · coreweave refused" {
		t.Fatalf("the retraction reads %q, want the fact that is left", got)
	}
}

// AND A PIN THE WIRE HAS REFUSED SAYS WHAT HAPPENS NEXT (issue #456). It is the
// one sentence on this line that is about a person's own row rather than about
// the answer in front of them, so it is written out whole and it names the
// machine the way they spelled it when they pinned it.
//
// The words are the transport's ([provider.RetiredPinLine]) so that this row
// and the note the same fact leaves in the conversation cannot come to
// disagree — the conversation's copy is the one a person really reads, because
// this row is outranked by the phase clock while the request is in flight.
func TestARetiredPinSaysWhereTheRequestsGoNow(t *testing.T) {
	laneLab(t, threeLanes())
	a := laneApp(t)
	a.state = stateWorking

	PostLaneNews(LaneNews{
		Model: flash, Lane: "Cloudflare", Alt: "CoreWeave",
		Role: lane.RoleTalk, Failed: true, Reason: provider.RescueRetired,
	})
	want := " · CoreWeave cannot serve this model; routing on auto for this model until you pin again"
	if got := a.laneRider(); got != want {
		t.Fatalf("a retired pin reads %q, want %q", got, want)
	}
}
