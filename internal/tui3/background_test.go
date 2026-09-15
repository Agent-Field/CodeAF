package tui3

// ctrl+g's acceptance tests. The key sends a running foreground command to the
// background through the engine's own seam — it never kills and never restarts —
// and it is ABSENT wherever that seam cannot answer.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

// promotingAgent is [fakeAgent] with the engine's promotion door on it
// (internal/session's PromoteCall). It records what it was asked to promote,
// because the one thing the surface must get right is WHICH call it sent away.
type promotingAgent struct {
	*fakeAgent
	asked  []string
	answer string
	refuse bool
}

func (p *promotingAgent) PromoteCall(callID string) (string, bool) {
	p.asked = append(p.asked, callID)
	if p.refuse {
		return "", false
	}
	return p.answer, true
}

// runningBash puts one foreground bash call on screen, still running, with an
// id the surface can address it by. The forming fragment is what carries the id
// — a row that never formed has none, and cannot be promoted.
func runningBash(t *testing.T, a *app, id, command string) {
	t.Helper()
	args := `{"command":"` + command + `"}`
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming(id, "bash", "bash", args)})
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventToolAnnounced, CallID: id, Tool: "bash", Hint: "bash", Args: args,
	}})
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventToolBegin, CallID: id, Tool: "bash", Hint: "bash", Args: args,
	}})
}

// promotingTurn is a surface mid-turn with the promotion door behind it.
func promotingTurn(t *testing.T) (*app, *promotingAgent) {
	t.Helper()
	agent := &promotingAgent{
		fakeAgent: &fakeAgent{model: "m"},
		answer:    session.BashPromotedLead + "3; log at /tmp/lab/.codeaf/jobs/3.log",
	}
	a := newTestApp(agent)
	typeLine(t, a, "build it")
	return a, agent
}

// H1: the background door outranks the standing task column on a wide frame.
//
// THE WHOLE POINT: one key, and the command that was running is a job. The row
// stays one row, says which job it became, and the turn is untouched — nothing
// was interrupted and nothing was sent again.
func TestARunningCommandIsSentToTheBackgroundWithOneKey(t *testing.T) {
	a, agent := promotingTurn(t)
	a.width = 120
	runningBash(t, a, "c1", "go test ./...")
	showLiveWork(t, a)
	if !a.railShowing() {
		t.Fatal("the 120-column frame did not start with its task column standing")
	}
	drive(t, a, tea.KeyboardEnhancementsMsg{Flags: 1})
	typeInto(t, a, "use the race-safe helper")
	wantHint := "enter " + steerSendWord + " · " + bargeKey + " " + bargeSendWord +
		" · ctrl+g backgrounds · esc interrupt"
	if got := a.hintWord(); got != wantHint {
		t.Fatalf("the full running-turn hint is %q, want %q", got, wantHint)
	}
	if door := plain(a.railDoorLine()); strings.Contains(door, "ctrl+g") || !strings.HasSuffix(door, "hide") {
		t.Fatalf("the column footer names a key the command owns: %q", door)
	}

	drive(t, a, key("ctrl+g"))

	if len(agent.asked) != 1 || agent.asked[0] != "c1" {
		t.Fatalf("the key promoted %v, want exactly [c1]", agent.asked)
	}
	// THE TURN IS NOT STOPPED. That is the entire difference between this key
	// and esc, and it is the reason the key exists.
	if agent.stops != 0 {
		t.Fatalf("backgrounding interrupted the turn %d times", agent.stops)
	}
	if a.state != stateWorking {
		t.Fatalf("the surface left stateWorking for %v", a.state)
	}
	if a.railAway || !a.railShowing() {
		t.Fatal("backgrounding the command stowed the task column")
	}
	// The transcript stays legal: one row for one call, still the same call.
	if got := toolEntries(a); got != 1 {
		t.Fatalf("backgrounding drew %d tool rows, want 1", got)
	}
	if line := toolLineOf(t, a); !strings.Contains(line, "job 3") {
		t.Fatalf("the row does not say which job it became: %q", line)
	}
	if line := toolLineOf(t, a); !strings.Contains(line, "go test") {
		t.Fatalf("the row lost the command it is about: %q", line)
	}

	// And the key is spent: the same row cannot be sent away twice.
	drive(t, a, key("ctrl+g"))
	if len(agent.asked) != 1 {
		t.Fatalf("a backgrounded row was promoted again: %v", agent.asked)
	}
}

// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. With nothing promotable
// running, ctrl+g does nothing at all — it does not refuse, and it does not
// leave a line in the conversation saying there was nothing to do.
func TestBackgroundingIsAbsentWithNothingToBackground(t *testing.T) {
	a, agent := promotingTurn(t)
	before := len(a.entries)

	// Nothing running.
	drive(t, a, key("ctrl+g"))
	// A call that is not bash.
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventToolBegin, CallID: "r1", Tool: "read", Hint: "read app.go",
		Args: `{"path":"app.go"}`,
	}})
	drive(t, a, key("ctrl+g"))
	// A bash call that asked to run in the background: it was a job from the
	// first instant, so there is nothing to promote.
	runningBash(t, a, "b1", "npm run dev")
	a.entries[len(a.entries)-1].detail.Args = `{"command":"npm run dev","background":true}`
	drive(t, a, key("ctrl+g"))

	if len(agent.asked) != 0 {
		t.Fatalf("the key reached the engine with nothing to promote: %v", agent.asked)
	}
	if got := len(a.entries) - before; got != 2 {
		t.Fatalf("the key added rows of its own: %d new entries", got)
	}
	for _, e := range a.entries {
		if e.bg != "" {
			t.Fatalf("a row was marked backgrounded with nothing to background: %q", e.bg)
		}
	}
}

// The key is absent on a surface whose agent has never heard of promotion — the
// same rule the stop card and the wake lane are held to.
func TestBackgroundingIsAbsentWithoutTheEnginesDoor(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	typeLine(t, a, "build it")
	runningBash(t, a, "c1", "go test ./...")
	showLiveWork(t, a)

	drive(t, a, key("ctrl+g"))

	if at := a.promotableRow(); at >= 0 {
		t.Fatalf("a surface with no promotion door offered row %d", at)
	}
	if line := toolLineOf(t, a); strings.Contains(line, "job ") {
		t.Fatalf("the row claims a job nothing gave it: %q", line)
	}
}

// A call that finished between the frame and the keypress is refused by the
// engine, and the surface says nothing about it: the result is already on its
// way, and a row marked "job 3" for a job that does not exist would be a lie
// with an id on it.
func TestARefusedPromotionMarksNothing(t *testing.T) {
	a, agent := promotingTurn(t)
	agent.refuse = true
	runningBash(t, a, "c1", "go test ./...")
	showLiveWork(t, a)

	drive(t, a, key("ctrl+g"))

	if len(agent.asked) != 1 {
		t.Fatalf("the key did not reach the engine: %v", agent.asked)
	}
	if line := toolLineOf(t, a); strings.Contains(line, "job ") {
		t.Fatalf("a refused promotion still marked the row: %q", line)
	}
}

// The oldest running call is the one a person is waiting on. A batch that
// started a `cd` a moment ago and a build two minutes ago must background the
// build.
func TestBackgroundingTakesTheCallThatHasBeenRunningLongest(t *testing.T) {
	a, agent := promotingTurn(t)
	runningBash(t, a, "old", "go test ./...")
	runningBash(t, a, "new", "git status")

	drive(t, a, key("ctrl+g"))

	if len(agent.asked) != 1 || agent.asked[0] != "old" {
		t.Fatalf("the key promoted %v, want exactly [old]", agent.asked)
	}
}

// The engine owns the id, and the row quotes it rather than composing one.
func TestTheRowQuotesTheEnginesOwnJobNumber(t *testing.T) {
	cases := []struct {
		line   string
		wanted string
	}{
		{session.BashPromotedLead + "3; log at /tmp/3.log", "job 3"},
		{session.BashPromotedLead + "17; log at /tmp/17.log", "job 17"},
		{"something nobody here can read", "backgrounded"},
	}
	for _, testCase := range cases {
		if got := backgroundWord(testCase.line); got != testCase.wanted {
			t.Fatalf("%q became %q, want %q", testCase.line, got, testCase.wanted)
		}
	}
}

// The surface receives the exact clock the session armed at boot. Ending a
// turn must not replace it with a newly read local profile value, especially on
// a hosted session whose process runs on another machine.
func TestTheBackgroundClockStaysWithTheSessionThatArmedIt(t *testing.T) {
	a := newApp(t.Context(), Options{
		Agent:                      &fakeAgent{model: "m"},
		Workspace:                  "/srv/app",
		Host:                       "devbox",
		ProfileDir:                 t.TempDir(),
		BashBackgroundAfterSeconds: 47,
	})
	if a.bashBackgroundAfter != 47 {
		t.Fatalf("the surface booted with %d seconds, want the session's 47", a.bashBackgroundAfter)
	}
	a.settle()
	if a.bashBackgroundAfter != 47 {
		t.Fatalf("settling reread this machine's profile and changed the clock to %d", a.bashBackgroundAfter)
	}
}

// toolScreenRow finds one call's line in the frame, including the row geometry
// that was recorded while that exact line was drawn.
func toolScreenRow(t *testing.T, a *app, entry int) (int, row) {
	t.Helper()
	y := screenRowOf(t, a, func(r row) bool { return r.hit == hitTool && r.entry == entry })
	r, ok := a.rowAt(y)
	if !ok {
		t.Fatalf("tool entry %d was not on screen", entry)
	}
	return y, r
}

// H7: only a hovered, live foreground bash row with a promotion door shows the
// pointer offer; the untouched row remains byte-for-byte the old running row.
func TestOnlyAHoveredPromotableBashOffersClickToBackground(t *testing.T) {
	a, _ := promotingTurn(t)
	a.width = 120
	runningBash(t, a, "c1", "go test ./...")
	showLiveWork(t, a)
	at := a.promotableRow()
	if at < 0 {
		t.Fatal("the live bash row is not promotable")
	}
	before := toolLineOf(t, a)
	if strings.Contains(before, backgroundKeepWord) {
		t.Fatalf("an unhovered row showed the pointer offer: %q", before)
	}
	y, _ := toolScreenRow(t, a, at)
	drive(t, a, motionTo(0, y))
	_, hovered := toolScreenRow(t, a, at)
	if line := plain(hovered.text); !strings.Contains(line, backgroundKeepWord) {
		t.Fatalf("the hovered row did not offer the pointer door: %q", line)
	}
	if !hovered.keep.pressable() {
		t.Fatal("the drawn pointer offer recorded no columns")
	}

	base := entry{
		kind: entryTool, tool: "bash", status: toolRunning, callID: "c1", began: a.now(),
		detail: toolDetail{Args: `{"command":"go test ./..."}`},
	}
	cases := []struct {
		name  string
		entry entry
		door  bool
		host  string
	}{
		{name: "read", entry: func() entry { e := base; e.tool = "read"; return e }(), door: true},
		{name: "background bash", entry: func() entry { e := base; e.detail.Args = `{"command":"npm run dev","background":true}`; return e }(), door: true},
		{name: "forming bash", entry: func() entry { e := base; e.status = toolForming; return e }(), door: true},
		{name: "replayed bash", entry: entry{kind: entryTool, tool: "bash", status: toolOK}, door: true},
		{name: "already kept bash", entry: func() entry { e := base; e.bg = "job 3"; return e }(), door: true},
		{name: "hosted bash", entry: base, host: "builder"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var agent Agent = &fakeAgent{model: "m"}
			if tc.door {
				agent = &promotingAgent{fakeAgent: &fakeAgent{model: "m"}, answer: session.BashPromotedLead + "3"}
			}
			caseApp := newTestApp(agent)
			caseApp.width, caseApp.state, caseApp.host = 120, stateWorking, tc.host
			caseApp.entries = []entry{tc.entry}
			caseApp.hot = hoverAt{kind: hoverEntry, entry: 0}
			line := plain(caseApp.toolRows(caseApp.conversation(), 0, true, caseApp.width)[0].text)
			if strings.Contains(line, backgroundKeepWord) {
				t.Fatalf("an ineligible row showed the pointer offer: %q", line)
			}
		})
	}
}

// H8: the narrow offer lights by itself, while the rest of the row retains the
// whole-row hover grammar.
func TestTheBackgroundOfferLightsWithoutLightingTheToolRow(t *testing.T) {
	a, _ := promotingTurn(t)
	a.width = 120
	runningBash(t, a, "c1", "go test ./...")
	showLiveWork(t, a)
	at := a.promotableRow()
	y, latent := toolScreenRow(t, a, at)
	if strings.Contains(latent.text, backgroundKeepWord) || !latent.keep.pressable() {
		t.Fatalf("the quiet row drew the offer or forgot its reveal columns: text=%q keep=%+v", latent.text, latent.keep)
	}

	// H9 review: one motion straight onto the future words must resolve the
	// narrow target immediately. It may not need a preparatory whole-row hover.
	drive(t, a, motionTo(latent.keep.from, y))
	if !a.hoveringKeep(at) {
		t.Fatalf("the pointer on the offer recorded %+v", a.hot)
	}
	_, narrow := toolScreenRow(t, a, at)
	if strings.Contains(narrow.text, hoverBg()) {
		t.Fatalf("the narrow offer lit the whole row: %q", narrow.text)
	}
	if !strings.Contains(narrow.text, a.pal.accent(backgroundKeepWord)) {
		t.Fatalf("the offer itself did not light: %q", narrow.text)
	}

	// Elsewhere on the same row the ordinary whole-row grammar still applies,
	// while the revealed offer keeps its key-over-verb paint.
	drive(t, a, motionTo(0, y))
	_, revealed := toolScreenRow(t, a, at)
	if !strings.Contains(revealed.text, a.pal.ink("click")) ||
		!strings.Contains(revealed.text, a.pal.dim(" to background")) {
		t.Fatalf("the offer did not lift its gesture above its verb: %q", revealed.text)
	}
	if !strings.Contains(revealed.text, hoverBg()) {
		t.Fatalf("hovering the ordinary row did not light it whole: %q", revealed.text)
	}
}

// H9: a press on the narrow offer promotes the call under the pointer rather
// than the oldest call, and a press elsewhere still opens the row.
func TestClickingTheBackgroundOfferKeepsThatCallAndDoesNotOpenIt(t *testing.T) {
	a, agent := promotingTurn(t)
	a.width = 120
	runningBash(t, a, "old", "go test ./...")
	runningBash(t, a, "under-pointer", "git status")
	showLiveWork(t, a)
	second := len(a.entries) - 1
	y, _ := toolScreenRow(t, a, second)
	drive(t, a, motionTo(0, y))
	_, revealed := toolScreenRow(t, a, second)

	drive(t, a,
		tea.MouseClickMsg{X: revealed.keep.from, Y: y, Button: tea.MouseLeft},
		tea.MouseReleaseMsg{X: revealed.keep.from, Y: y, Button: tea.MouseLeft},
	)
	if len(agent.asked) != 1 || agent.asked[0] != "under-pointer" {
		t.Fatalf("the pointer promoted %v, want exactly [under-pointer]", agent.asked)
	}
	if a.entries[second].bg != "job 3" || a.entries[second].open {
		t.Fatalf("the clicked row is bg=%q open=%v, want job 3 and closed", a.entries[second].bg, a.entries[second].open)
	}

	firstY, _ := toolScreenRow(t, a, a.promotableRow())
	drive(t, a,
		tea.MouseClickMsg{X: 0, Y: firstY, Button: tea.MouseLeft},
		tea.MouseReleaseMsg{X: 0, Y: firstY, Button: tea.MouseLeft},
	)
	if !a.entries[a.promotableRow()].open {
		t.Fatal("clicking elsewhere on the running row did not open its expansion")
	}
	if len(agent.asked) != 1 {
		t.Fatalf("the ordinary row press promoted another call: %v", agent.asked)
	}
}

// H10: a promotion sentence arriving from the engine marks the same stat slot,
// and the mark remains visible in the phone row.
func TestAnAutomaticPromotionMarksTheWideAndPhoneRows(t *testing.T) {
	a, _ := promotingTurn(t)
	a.width = 120
	runningBash(t, a, "clock", "x")
	showLiveWork(t, a)
	at := a.promotableRow()
	y, _ := toolScreenRow(t, a, at)
	drive(t, a, motionTo(0, y))
	_, offered := toolScreenRow(t, a, at)
	offerColumn := offered.keep.from

	a.event(session.Event{
		Kind: session.EventToolEnd, Tool: "bash", Args: a.entries[at].detail.Args,
		Output: session.BashPromotedLead + "3; log at /tmp/3.log",
	})
	_, promoted := toolScreenRow(t, a, at)
	wide := plain(promoted.text)
	if !strings.Contains(wide, "job 3") || strings.Contains(wide, backgroundKeepWord) {
		t.Fatalf("the automatic job mark did not replace the offer in its stat column: %q (offer began at %d)", wide, offerColumn)
	}

	a.width = phoneCols
	phone := plain(a.toolRows(a.conversation(), at, true, a.width)[0].text)
	if !strings.Contains(phone, "job 3") {
		t.Fatalf("the phone row dropped the automatic job mark: %q", phone)
	}
}
