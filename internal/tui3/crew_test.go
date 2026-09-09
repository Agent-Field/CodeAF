package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/roles"
)

// /crew, FROM THE FOUR SIDES A PERSON MEETS IT: the chooser, applying one, a word
// that is not one of the three, and the reading over a crew somebody assembled
// themselves.

// The bare form is the three presets with yours marked, each naming the four
// models it would set, and opens on the current one.
func TestCrewOpensTheThreePresetsOnYours(t *testing.T) {
	a, _ := sheetApp(t)
	a.width = 240
	a.slash("/crew")

	if !a.crewPick.open {
		t.Fatal("bare /crew did not open the chooser")
	}
	if a.crewPick.cursor != 1 {
		t.Fatalf("the cursor opened on row %d, want balanced at 1", a.crewPick.cursor)
	}
	text := plain(strings.Join(a.overlayRows(a.width, a.overlayHeight()), "\n"))
	for _, preset := range config.CrewPresets {
		if !strings.Contains(text, preset) {
			t.Errorf("the listing does not mention %q:\n%s", preset, text)
		}
		if !strings.Contains(text, config.CrewLine(preset)) {
			t.Errorf("the listing does not say what %q is:\n%s", preset, text)
		}
		models, _ := config.CrewModels(preset)
		for _, tier := range roles.Tiers {
			if !strings.Contains(text, models[string(tier)]) {
				t.Errorf("the listing does not name %s's %s model:\n%s", preset, tier, text)
			}
		}
	}
	// THE SHIPPED CREW IS MARKED BY THE GROUND IT WEARS, and it is balanced — the
	// four tier defaults are that row (internal/config's crew.go).
	//
	// The crew in force is the chosen thing, so it takes THE GROUND LADDER's
	// selected step; the cursor takes the step under it. This list used to say
	// "in force" with the POINTER's own `·` in the lead, which meant the two
	// facts fought over one cell — and because the current preset was tested
	// first, the cursor's own `›` vanished whenever it landed on the preset
	// already running, which is the row a person arrows onto most.
	painted := a.overlayRows(a.width, a.overlayHeight())
	chosen := "\x1b[48;5;" + itoa(int(hueSelected.idx)) + "m"
	cursorStep := "\x1b[48;5;" + itoa(int(hueCursor.idx)) + "m"
	var onChosen, onOthers int
	for _, line := range painted {
		switch {
		case strings.Contains(line, config.CrewBalanced):
			onChosen++
			if !strings.Contains(line, chosen) {
				t.Errorf("the crew in force wears no ground:\n%q", line)
			}
		case strings.Contains(line, config.CrewFrugal) || strings.Contains(line, config.CrewMax):
			onOthers++
			if strings.Contains(line, chosen) {
				t.Errorf("a preset nobody is on wears the chosen ground:\n%q", line)
			}
		}
	}
	if onChosen == 0 || onOthers == 0 {
		t.Fatalf("the listing did not draw the presets it was asked about:\n%s", text)
	}
	// The cursor opened on balanced, which is also the crew in force. The louder
	// step wins the ground and the `›` still says where enter is aimed.
	if !strings.Contains(strings.Join(painted, "\n"), a.pal.accent("› ")) {
		t.Errorf("the cursor lost its mark on the row it opened on:\n%s", text)
	}
	if strings.Contains(strings.Join(painted, "\n"), cursorStep) {
		t.Errorf("a second row took the cursor step with no pointer on the list:\n%s", text)
	}
	// The four classes are named in the words their own settings rows use, so
	// somebody told "mastermind" here can find the row there.
	for _, tier := range roles.Tiers {
		want := settingUI[tierSettingKey(tier)].label
		if !strings.Contains(text, want) {
			t.Errorf("the listing does not use the row's own word %q:\n%s", want, text)
		}
	}
}

func TestCrewChooserAppliesAndCloses(t *testing.T) {
	a, dir := sheetApp(t)
	a.openSettings()
	toProviders(t, a)
	a.slash("/crew")
	a.crewPickerKey(key("down"))
	a.crewPickerKey(key("enter"))

	if a.crewPick.open {
		t.Fatal("enter left the crew chooser open")
	}
	if got := config.CrewAt(dir); got != config.CrewMax {
		t.Fatalf("enter applied %q, want max", got)
	}
	if got := lastNote(t, a); !strings.Contains(got, "crew → max") {
		t.Fatalf("the chooser noted %q", got)
	}
	refreshed := false
	for _, row := range a.sheet.rows {
		if row.Key == config.KeyCrew && row.Value() == config.CrewMax {
			refreshed = true
		}
	}
	if !refreshed {
		t.Fatal("the open settings rows were not refreshed to max")
	}
}

func TestCrewChooserEscChangesNothing(t *testing.T) {
	a, dir := sheetApp(t)
	a.slash("/crew")
	drive(t, a, key("down"))
	drive(t, a, key("esc"))

	if a.crewPick.open {
		t.Fatal("esc left the crew chooser open")
	}
	if got := config.CrewAt(dir); got != config.CrewBalanced {
		t.Fatalf("esc changed the crew to %q", got)
	}
}

// A preset writes all five classes and confirms in one line.
func TestCrewAppliesAPresetAndConfirmsInOneLine(t *testing.T) {
	a, dir := sheetApp(t)
	a.slash("/crew max")

	want, _ := config.CrewModels(config.CrewMax)
	for _, tier := range config.ModelTiers {
		if got := config.TierModelAt(dir, tier); got != want[tier] {
			t.Errorf("after /crew max the %s class reads %q, want %q", tier, got, want[tier])
		}
	}
	text := lastNote(t, a)
	if strings.Count(text, "\n") != 0 {
		t.Fatalf("the confirmation is more than one line:\n%s", text)
	}
	for _, part := range []string{"crew → max", "brain kimi-k3", "hands glm-5.3", "checks kimi-k3"} {
		if !strings.Contains(text, part) {
			t.Errorf("the confirmation is missing %q: %q", part, text)
		}
	}
	// AND NO RUNG RIDES A SHIPPED PRESET. A preset buys a bigger planning model
	// and leaves its generation behaviour to the provider (#665); the `:high`
	// the max crew used to carry on its brain is an instruction only a person's
	// own class value may add.
	if strings.Contains(text, ":high") {
		t.Errorf("a shipped preset carried a rung: %q", text)
	}
}

// AN UNKNOWN WORD CHANGES NOTHING AND SHOWS THE THREE.
func TestCrewRefusesAWordThatIsNotOneOfTheThree(t *testing.T) {
	a, dir := sheetApp(t)
	a.slash("/crew cheap")

	if got := config.CrewAt(dir); got != config.DefaultCrew {
		t.Fatalf("an unknown word changed the crew to %q", got)
	}
	text := lastNote(t, a)
	if !strings.Contains(text, "not one of the three") {
		t.Fatalf("the refusal reads %q", text)
	}
	for _, preset := range config.CrewPresets {
		if !strings.Contains(text, preset) {
			t.Errorf("the refusal does not offer %q:\n%s", preset, text)
		}
	}
}

// A CREW SOMEBODY ASSEMBLED THEMSELVES READS AS CUSTOM, and the listing says how
// to put it back rather than pretending custom is a fourth choice.
func TestCrewReadsCustomOverAHandSetClass(t *testing.T) {
	a, dir := sheetApp(t)
	a.slash("/crew balanced")
	a.openSettings()
	toProviders(t, a)
	setRow(t, a, config.KeyTierMastermindModel, "openai/gpt-5")
	a.closeSettings()

	if got := config.CrewAt(dir); got != config.CrewCustom {
		t.Fatalf("a hand-set class left the crew reading %q", got)
	}
	a.slash("/crew")
	text := plain(strings.Join(a.overlayRows(a.width, a.overlayHeight()), "\n"))
	if !strings.Contains(text, "none of the three") {
		t.Fatalf("the chooser does not say the crew is nobody's preset:\n%s", text)
	}
	if !strings.Contains(text, "picking one puts all five back") {
		t.Fatalf("the chooser does not say how to put it back:\n%s", text)
	}
	if strings.Contains(text, "· "+config.CrewBalanced) {
		t.Fatalf("balanced is marked over a custom crew:\n%s", text)
	}

	// And one word heals all four.
	a.slash("/crew balanced")
	if got := config.CrewAt(dir); got != config.CrewBalanced {
		t.Fatalf("the crew reads %q after balanced was set again", got)
	}
}

// THE CREW ROW CYCLES, like every other enum row on the sheet, and a custom
// reading enters the cycle at its first step rather than sticking.
func TestTheCrewRowCyclesThroughTheThreePresets(t *testing.T) {
	a, dir := sheetApp(t)
	a.openSettings()
	toProviders(t, a)
	cursorTo(t, a, config.KeyCrew)

	drive(t, a, key("enter"))
	if got := config.CrewAt(dir); got != config.CrewMax {
		t.Fatalf("enter on balanced landed on %q, want %q", got, config.CrewMax)
	}
	drive(t, a, key("enter"))
	if got := config.CrewAt(dir); got != config.CrewFrugal {
		t.Fatalf("enter on max landed on %q, want %q", got, config.CrewFrugal)
	}
	drive(t, a, key("enter"))
	if got := config.CrewAt(dir); got != config.CrewBalanced {
		t.Fatalf("enter on frugal landed on %q, want %q", got, config.CrewBalanced)
	}

	// From custom the cycle starts at the cheapest, which is the one answer that
	// cannot surprise anybody: it never spends more than the person asked for.
	setRow(t, a, config.KeyTierLowModel, "openai/gpt-5")
	if got := config.CrewAt(dir); got != config.CrewCustom {
		t.Fatalf("the crew reads %q over a hand-set class", got)
	}
	cursorTo(t, a, config.KeyCrew)
	drive(t, a, key("enter"))
	if got := config.CrewAt(dir); got != config.CrewFrugal {
		t.Fatalf("enter on custom landed on %q, want %q", got, config.CrewFrugal)
	}
}

// THE CREW ROW SAYS WHAT EACH OF THE THREE IS, in the line under it. A cycle row
// walks its choices in place — there is never a moment where three rows are shown
// side by side — so this line is the only place the comparison can happen, and it
// is built from the same table /crew prints from.
func TestTheCrewRowsLineNamesAllThreePresets(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()
	toProviders(t, a)
	cursorTo(t, a, config.KeyCrew)

	item, ok := a.sheet.current()
	if !ok {
		t.Fatal("the cursor is not on the crew row")
	}
	for _, preset := range config.CrewPresets {
		if !strings.Contains(item.meta.about, preset) {
			t.Errorf("the crew row's line does not name %q: %q", preset, item.meta.about)
		}
		if !strings.Contains(item.meta.about, config.CrewLine(preset)) {
			t.Errorf("the crew row's line does not say what %q is: %q", preset, item.meta.about)
		}
	}
	if !strings.Contains(item.meta.about, config.CrewCustom) {
		t.Errorf("the crew row's line does not say what makes it custom: %q", item.meta.about)
	}
	// And it is drawn, which is the whole point of a line nobody can see three
	// rows for.
	if !strings.Contains(plain(frame(a)), config.CrewLine(config.CrewBalanced)) {
		t.Fatalf("the crew row's line is not on the screen:\n%s", plain(frame(a)))
	}
}

// THE CLASS WORDS ON DISK AND THE TIER NAMES IN THE REGISTRY ARE ONE SET. The
// listing indexes the preset table by [roles.Tier], and internal/config keys that
// table by its own tier words; a rename on either side would silently print a
// blank model beside a class.
func TestTheTierWordsAndTheClassKeysAreTheSameSet(t *testing.T) {
	if len(roles.Tiers) != len(config.ModelTiers) {
		t.Fatalf("internal/roles has %d tiers and internal/config has %d classes",
			len(roles.Tiers), len(config.ModelTiers))
	}
	for at, tier := range roles.Tiers {
		if string(tier) != config.ModelTiers[at] {
			t.Errorf("tier %d is %q in internal/roles and %q in internal/config",
				at, tier, config.ModelTiers[at])
		}
		if settingUI[tierSettingKey(tier)].label == "" {
			t.Errorf("tier %q has no row of its own on the sheet", tier)
		}
	}
}

// ── THE CREW IS READABLE WHERE PEOPLE GO TO CHECK ───────────────────────────
//
// /crew writes four class models and the session picks them up on its next
// call, and NOTHING ON THE FRAME MOVES: the status line's model readout is the
// conversation's model, which the crew never touches. Before these three
// surfaces existed, the whole of the evidence was one note that scrolled away,
// and a person who set the crew and then went to look for it concluded the
// command had not worked.

// /status NAMES THE CREW, directly under the model it is not.
func TestStatusNamesTheCrewUnderTheModel(t *testing.T) {
	a, _ := sheetApp(t)
	a.slash("/crew max")

	a.slash("/status")
	text := lastNote(t, a)
	lines := strings.Split(text, "\n")
	at := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "crew ") {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("/status says nothing about the crew:\n%s", text)
	}
	// The preset word first, then the same three class names the confirmation
	// prints — base names, in [config.CrewClasses]'s own order.
	for _, want := range []string{
		config.CrewMax, "brain kimi-k3", "hands glm-5.3", "checks kimi-k3",
	} {
		if !strings.Contains(lines[at], want) {
			t.Errorf("the crew line lost %q: %q", want, lines[at])
		}
	}
	if strings.Contains(lines[at], ":high") {
		t.Errorf("/status shows a rung on a shipped preset's brain: %q", lines[at])
	}
	// AND IT IS THE LINE UNDER THE MODEL, because the two are read together or
	// not at all: one is what the conversation talks to, the other is what aforge
	// makes its own calls on.
	if at == 0 || !strings.HasPrefix(lines[at-1], "model ") {
		t.Fatalf("the crew line does not sit under the model line:\n%s", text)
	}
}

// A CREW SOMEBODY ASSEMBLED THEMSELVES READS AS CUSTOM HERE TOO. The word is
// derived from the four live rows, so the line cannot say "max" over a class
// that was hand-set out of it.
func TestStatusSaysCustomOverAHandSetClass(t *testing.T) {
	a, _ := sheetApp(t)
	a.slash("/crew max")
	a.openSettings()
	toProviders(t, a)
	setRow(t, a, config.KeyTierMastermindModel, "openai/gpt-5")
	a.closeSettings()

	a.slash("/status")
	text := lastNote(t, a)
	line := ""
	for _, row := range strings.Split(text, "\n") {
		if strings.HasPrefix(row, "crew ") {
			line = row
		}
	}
	if line == "" {
		t.Fatalf("/status says nothing about the crew:\n%s", text)
	}
	if !strings.Contains(line, config.CrewCustom) {
		t.Fatalf("the crew line reads %q over a hand-set class, want custom", line)
	}
	if strings.Contains(line, config.CrewMax) {
		t.Fatalf("the crew line still claims the preset it left: %q", line)
	}
	if !strings.Contains(line, "brain gpt-5") {
		t.Fatalf("the crew line does not name the class that was hand-set: %q", line)
	}
}

// THE EMPTINESS LAW, AIMED AT THE RIGHT FACT: a window with no four tier rows
// to read has no crew line at all — not a label with a default beside it, which
// would be a claim about a file nobody is writing. That window is the HOSTED
// one, where the crew lives on the far machine.
func TestStatusSaysNothingAboutACrewInAHostedWindow(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.model = "m"
	a.host = "devbox"

	a.slash("/status")
	for _, line := range strings.Split(lastNote(t, a), "\n") {
		if strings.HasPrefix(line, "crew") {
			t.Fatalf("a hosted window grew a crew line: %q", line)
		}
	}
}

// AND AN EMPTY PROFILE DIRECTORY IS THE ORDINARY LAUNCH, WHICH HAS A CREW.
//
// AFORGE_PROFILE_DIR is exported by almost nobody, so the empty string is what
// nearly every launch carries and internal/config resolves it to this process's
// own profile in the state root. The guard that read it as "no profile" left the
// crew off /status, off the status line and out of the picker's hint on every
// one of those launches (#315).
func TestAnEmptyProfileDirectoryIsTheOrdinaryProfileAndStillHasACrew(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.model = "m"
	if a.profileDir != "" {
		t.Fatalf("this app names a profile at %q and cannot test the ordinary launch", a.profileDir)
	}

	if word := a.crewWord(); !strings.HasPrefix(word, config.CrewBalanced+" ·") {
		t.Fatalf("the ordinary launch reads its crew as %q", word)
	}
	if got := a.crewSegment(); got != "crew "+config.CrewBalanced {
		t.Fatalf("the ordinary launch's crew segment reads %q", got)
	}
	a.slash("/status")
	if !strings.Contains(lastNote(t, a), "\ncrew     "+config.CrewBalanced+" ·") {
		t.Fatalf("/status says nothing about the crew on an ordinary launch:\n%s", lastNote(t, a))
	}
}

// THE MODEL PICKER SAYS THE CREW'S NAME BESIDE ITS KEYS, because this list is
// where a person lands hunting for a change /crew did not make here. It is the
// word alone: the slot's cells come out of the conversation's name at the other
// end of the legend, and every printable key is going into the filter box, so a
// command named here would be a door that cannot be walked through.
func TestTheModelPickerNamesTheCrewInTheHintSlot(t *testing.T) {
	a, _ := sheetApp(t)
	a.slash("/crew max")
	a.pick.open = true

	if got := a.hintWord(); got != "enter switch · esc · crew max" {
		t.Fatalf("the picker's hint reads %q", got)
	}

	// And a hosted window — the one window with no crew of its own — is the hint
	// exactly as it was.
	hostedWindow := newTestApp(&fakeAgent{model: "m"})
	hostedWindow.host = "devbox"
	hostedWindow.pick.open = true
	if got := hostedWindow.hintWord(); got != "enter switch · esc" {
		t.Fatalf("the hint in a hosted window reads %q", got)
	}
}

// AND THE CONFIRMATION NAMES WHAT IT DID NOT CHANGE, in the same line and at
// the moment the question is raised: the conversation's model by id, spelled as
// the status line spells it, and the one command that moves it. It is still one
// line.
func TestTheCrewConfirmationPointsAtTheModelItDidNotChange(t *testing.T) {
	a, _ := sheetApp(t)
	a.slash("/crew max")

	text := lastNote(t, a)
	if strings.Count(text, "\n") != 0 {
		t.Fatalf("the confirmation is more than one line:\n%s", text)
	}
	if !strings.HasSuffix(text, "· you are still talking to gpt-4.1-mini — /model changes that") {
		t.Fatalf("the confirmation does not name the model it left alone: %q", text)
	}
	// The id is the status line's spelling — the basename — and not the routing
	// address, so a person can check the clause against the foot of the frame.
	if strings.Contains(text, "openai/gpt-4.1-mini") {
		t.Fatalf("the confirmation spells the model as a routing address: %q", text)
	}
	// AND THE SPELLING FOLLOWS THE REASONING LEVEL, because the status line's does.
	// Set on the AGENT rather than through ctrl+t, which is a session that was
	// dialled somewhere this surface did not watch — so the level is learned the
	// way the frame clock learns it (reasoninglevel.go).
	a.agent.SetReasoningFor("openai/gpt-4.1-mini", "high")
	settleLevels(a, "openai/gpt-4.1-mini")
	a.slash("/crew balanced")
	if text := lastNote(t, a); !strings.Contains(text, "you are still talking to gpt-4.1-mini:high —") {
		t.Fatalf("the confirmation lost the level the status line shows: %q", text)
	}
}

// ── THE FIVE SEATS ──────────────────────────────────────────────────────────
//
// aforge runs five model seats — the one you talk to, plus reflex, small work,
// careful work and mastermind — and /crew moves only the last four. Every
// surface that names the crew now names the fifth seat beside it, so the two
// dials are visibly two dials.

// BARE /crew IS THE FIVE-SEAT READING: a scope line saying what the presets
// change and what they do not, seat one on a line of its own, the three presets
// exactly as before, and a closing line pointing at where a single seat is
// pinned.
func TestBareCrewReadsTheFiveSeats(t *testing.T) {
	a, _ := sheetApp(t)
	a.width = 240
	a.slash("/crew")

	rows := a.overlayRows(a.width, a.overlayHeight())
	if len(rows) != a.crewPick.height() {
		t.Fatalf("the chooser drew %d rows and promised %d", len(rows), a.crewPick.height())
	}
	text := plain(strings.Join(rows, "\n"))
	if !strings.HasPrefix(plain(rows[0]), crewScopeLine) {
		t.Fatalf("the chooser does not open with the scope line:\n%s", text)
	}
	if seat := plain(rows[1]); !strings.Contains(seat, crewSeatLead+" · gpt-4.1-mini") {
		t.Fatalf("seat one is not the second row:\n%s", text)
	}
	if !strings.HasSuffix(plain(rows[len(rows)-1]), crewPinLine) {
		t.Fatalf("the chooser does not close on the pinning note:\n%s", text)
	}
	// SEAT ONE IS NOT A ROW ENTER COULD APPLY: it wears no lead, no cursor and no
	// ground, and the cursor still opens on the crew in force below it.
	if strings.Contains(rows[1], a.pal.accent("› ")) {
		t.Fatalf("seat one took the cursor:\n%q", rows[1])
	}
	if strings.Contains(rows[1], "\x1b[48;5;"+itoa(int(hueSelected.idx))+"m") {
		t.Fatalf("seat one wears the chosen ground:\n%q", rows[1])
	}
	if a.crewPick.cursor != 1 {
		t.Fatalf("the cursor opened on row %d, want balanced at 1", a.crewPick.cursor)
	}
	// The presets follow, in their own order, after the two reading lines.
	for i, preset := range config.CrewPresets {
		if row := plain(rows[2+2*i]); !strings.Contains(row, preset+" — "+config.CrewLine(preset)) {
			t.Errorf("row %d is not %s's:\n%q", 2+2*i, preset, row)
		}
	}
	// And enter still applies the preset under the cursor, seat one untouched.
	a.crewPickerKey(key("down"))
	a.crewPickerKey(key("enter"))
	if got := config.CrewAt(a.profileDir); got != config.CrewMax {
		t.Fatalf("enter applied %q, want max", got)
	}
	if a.model != "openai/gpt-4.1-mini" {
		t.Fatalf("the chooser moved the conversation's model to %q", a.model)
	}
}

// THE STATUS LINE PAIRS THE TWO DIALS: the crew word stands at the head of the
// telemetry, across the gap from the conversation's model, and the word is the
// same one /status, the picker's hint and the chooser read — derived from the
// four live rows through one function.
func TestTheStatusLinePairsTheCrewWithTheModel(t *testing.T) {
	a, _ := sheetApp(t)
	a.width = 200
	a.slash("/crew max")

	line := plain(a.status(a.width))
	if !strings.Contains(line, "crew max") {
		t.Fatalf("the status line does not name the crew:\n%q", line)
	}
	if strings.Index(line, "crew max") < strings.Index(line, "gpt-4.1-mini") {
		t.Fatalf("the crew word is not beside the model, on its right:\n%q", line)
	}
	if parts := a.telemetry(a.width); len(parts) == 0 || parts[0].kind != segCrew {
		t.Fatalf("the crew is not the head of the telemetry: %+v", parts)
	}
	// ONE SOURCE FOR THE WORD, wherever it is said.
	if a.crewHint() != a.crewSegment() {
		t.Fatalf("the hint says %q and the segment says %q", a.crewHint(), a.crewSegment())
	}
	a.slash("/status")
	if !strings.Contains(lastNote(t, a), "\ncrew     max ·") {
		t.Fatalf("/status does not read the same word:\n%s", lastNote(t, a))
	}
	// A hand-set seat turns every reading to custom at once.
	a.openSettings()
	toProviders(t, a)
	setRow(t, a, config.KeyTierMastermindModel, "openai/gpt-5")
	a.closeSettings()
	if line := plain(a.status(a.width)); !strings.Contains(line, "crew custom") || strings.Contains(line, "crew max") {
		t.Fatalf("the status line did not follow the hand-set seat:\n%q", line)
	}
}

// THE CREW IS AMONG THE FIRST SEGMENTS TO GO: a setting rather than a
// measurement, said in full elsewhere, so a short row gives it up before the
// bill and the meter and nothing else on the row moves.
func TestTheCrewSegmentYieldsBeforeTheNumbers(t *testing.T) {
	a, _ := sheetApp(t)
	// The name is sized so that at a hundred columns the telemetry is over by
	// exactly the crew's width: dropping it is enough, and nothing else goes.
	a.title = "a conversation with a name long enough to crowd"
	a.ctxWindow, a.ctxTokens = 200_000, 24_000
	a.cost = 0.31

	wide := plain(a.status(200))
	if !strings.Contains(wide, "crew balanced") {
		t.Fatalf("the wide row does not carry the crew:\n%q", wide)
	}
	narrow := plain(a.status(100))
	if strings.Contains(narrow, "crew") {
		t.Fatalf("the crowded row kept the crew over the numbers:\n%q", narrow)
	}
	for _, kept := range []string{"$0.31", "24k/200k", "idle"} {
		if !strings.Contains(narrow, kept) {
			t.Fatalf("the crowded row lost %q while dropping the crew:\n%q", kept, narrow)
		}
	}
	if dropOrder[1] != segCrew {
		t.Fatalf("the crew is not second in the drop order: %v", dropOrder)
	}
}

// THE EMPTINESS LAW ON THE ROW: a hosted window has no crew to read — it is on
// the far machine — and the status line says nothing rather than guessing a word.
func TestTheStatusLineSaysNothingAboutACrewInAHostedWindow(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.model = "m"
	a.host = "devbox"
	if line := plain(a.status(200)); strings.Contains(line, "crew") {
		t.Fatalf("a hosted window grew a crew segment:\n%q", line)
	}
	for _, part := range a.telemetry(200) {
		if part.kind == segCrew {
			t.Fatalf("a hosted window assembled a crew segment: %+v", part)
		}
	}
}

// AND THE WHOLE FRAME CARRIES IT ON AN ORDINARY LAUNCH, at both the widths a
// person actually sits at.
//
// This is the issue's headline said the way a person meets it: not a segment
// asked for by name, but the frame [app.View] draws, with a crew picked in a
// profile of this test's own and nothing exported into the environment. It was
// red at every width before #315 and is green at both after it, and a hosted
// window at the same widths still shows none.
func TestTheFrameCarriesTheCrewOnAnOrdinaryLaunchAtEveryWidth(t *testing.T) {
	dir := t.TempDir()
	if err := config.ApplyCrew(dir, config.CrewMax); err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{80, 120} {
		a := newTestApp(&fakeAgent{model: "openai/gpt-4.1-mini"})
		a.profileDir = dir
		a.model = "openai/gpt-4.1-mini"
		a.width, a.height = width, 24
		painted, _, _ := a.frame()
		if frame := plain(painted); !strings.Contains(frame, "crew "+config.CrewMax) {
			t.Fatalf("the %d-column frame of an ordinary launch does not carry the crew:\n%s", width, frame)
		}

		hostedWindow := newTestApp(&fakeAgent{model: "openai/gpt-4.1-mini"})
		hostedWindow.profileDir = dir
		hostedWindow.host = "devbox"
		hostedWindow.model = "openai/gpt-4.1-mini"
		hostedWindow.width, hostedWindow.height = width, 24
		hostedPaint, _, _ := hostedWindow.frame()
		if frame := plain(hostedPaint); strings.Contains(frame, "crew ") {
			t.Fatalf("the %d-column frame of a hosted window carries a crew:\n%s", width, frame)
		}
	}
}

// AND THE STATUS SHEET CARRIES THE CREW UNDER THE MODEL, the way /status does,
// because the two are one list: the phone's sheet was the one surface that did
// not say it.
func TestTheStatusSheetCarriesTheCrewUnderTheModel(t *testing.T) {
	a, _ := sheetApp(t)
	a.slash("/crew max")

	items := a.deckItems()
	for i, item := range items {
		if item.label != "crew" {
			continue
		}
		if i == 0 || items[i-1].label != "model" {
			t.Fatalf("the crew is not under the model: %+v", items)
		}
		if item.value != a.crewWord() {
			t.Fatalf("the sheet's crew reads %q, want %q", item.value, a.crewWord())
		}
		// Once, and in full — never a second time as the row's short word.
		for _, other := range items[i+1:] {
			if other.label == "crew" || other.value == "crew max" {
				t.Fatalf("the crew is on the sheet twice: %+v", items)
			}
		}
		return
	}
	t.Fatalf("the sheet has no crew line: %+v", items)
}
