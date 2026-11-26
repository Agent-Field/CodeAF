package tui3

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
)

// V1/T6 (contract 2): an empty composer on a live view invites a message to the
// computer the chat runs on, while a conversation with no live seam keeps its
// team hint or its empty placeholder.
func TestALiveConversationInvitesAMessageToTheComputerItRunsOn(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.wall.activeID = harbor
	managerFile := a.file
	teamHint := "to " + a.teamManagerMark() + " manager of harbor"
	if got := a.trafficHint(); got != teamHint {
		t.Fatalf("managed team conversation without a live seam lost its team hint: got %q, want %q", got, teamHint)
	}

	plainApp := newTestApp(&fakeAgent{model: "m"})
	if got := plainApp.trafficHint(); got != "" {
		t.Fatalf("plain conversation without a live seam drew a hint: %q", got)
	}

	a.takeUp(Conversation{
		Agent:       &fakeAgent{model: "m"},
		SessionFile: managerFile,
		Live: &LiveSeam{
			Host: "Desktop",
			Read: func() LiveRead { return LiveRead{Kind: LiveOn} },
		},
	}, true)
	if got := a.trafficHint(); got != "Message Desktop" {
		t.Fatalf("live conversation hint = %q, want %q", got, "Message Desktop")
	}
	if screen := plain(frame(a)); !strings.Contains(screen, "Message Desktop") {
		t.Fatalf("empty composer on a live view did not draw Message Desktop:\n%s", screen)
	}

	// Clearing the live seam via takeUp restores the team hint on the same window.
	a.takeUp(Conversation{
		Agent:       &fakeAgent{model: "m"},
		SessionFile: managerFile,
	}, true)
	if got := a.trafficHint(); got != teamHint {
		t.Fatalf("clearing the live seam did not restore the team hint: got %q, want %q", got, teamHint)
	}
}

// V1/T6 (contract 3): the legend's host piece reads "on <Host> · live" while
// the live view is healthy (LiveOn) and "on <Host>" in every other state, while
// a conversation with no live seam keeps today's legend.
func TestTheLegendSaysTheChatIsLiveOnTheOtherComputer(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.dismissWelcome()
	if got := a.seamPieces(a.width).host; got != "" {
		t.Fatalf("local conversation without a live seam had legend host %q", got)
	}

	kind := LiveOn
	a.takeUp(Conversation{
		Agent: &fakeAgent{model: "m"},
		Live: &LiveSeam{
			Host: "Desktop",
			Read: func() LiveRead { return LiveRead{Kind: kind} },
		},
	}, true)

	if got := a.seamPieces(a.width).host; got != "on Desktop · live" {
		t.Fatalf("healthy live view legend host = %q, want %q", got, "on Desktop · live")
	}
	if screen := plain(frame(a)); !strings.Contains(screen, "on Desktop · live") {
		t.Fatalf("frame legend did not draw 'on Desktop · live':\n%s", screen)
	}

	for _, other := range []LiveKind{LiveReconnecting, LiveWentOffline, LiveIdle, LiveRevoked, LiveBudget, LiveCopy} {
		kind = other
		if got := a.seamPieces(a.width).host; got != "on Desktop" {
			t.Fatalf("live kind %v legend host = %q, want %q", other, got, "on Desktop")
		}
		screen := plain(frame(a))
		if !strings.Contains(screen, "on Desktop") || strings.Contains(screen, "on Desktop · live") {
			t.Fatalf("live kind %v frame legend should say 'on Desktop' without '· live':\n%s", other, screen)
		}
	}
}

// V1/T5 (contract 1): a live view keeps a.hosted() false and neutralizes the
// local-ownership machinery so recordMachines never prints a Superseded note
// and pressing Enter sends to the live agent instead of raising the Move card,
// while the identical setup with no live seam raises the Superseded note.
func TestTypingIntoALiveViewNeverRaisesTheMoveCardOrASupersededNote(t *testing.T) {
	cellID := "0000000000000042"
	sessionFile := filepath.Join(t.TempDir(), cellID, "session.jsonl")
	remoteRow := chatlist.Row{
		Cell:     cellID,
		Title:    "Parser work",
		Device:   "Desktop",
		DeviceID: "dev-desktop",
		Status:   chatlist.Running,
	}

	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	a.dismissWelcome()
	a.machines = &flipping{status: chatlist.Running}
	a.taker = &fakeTaker{}
	a.takeUp(Conversation{
		Agent:       agent,
		SessionFile: sessionFile,
		Live: &LiveSeam{
			Host:   "Desktop",
			Device: "dev-desktop",
			Cell:   cellID,
			Row:    remoteRow,
			Read:   func() LiveRead { return LiveRead{Kind: LiveOn} },
		},
	}, true)

	if a.hosted() {
		t.Fatal("a live view inside a local window flipped a.hosted() true")
	}
	if a.watchesConversation() {
		t.Fatal("a live view should not poll local conversation ownership")
	}

	a.recordMachines(homeMachinesMsg{rows: []chatlist.Row{remoteRow}})
	if _, elsewhere := a.heldElsewhere(); elsewhere {
		t.Fatal("heldElsewhere() reported true for a live view")
	}
	for _, e := range a.entries {
		if e.kind == entryNote && strings.Contains(e.text, chatlist.Superseded("Desktop")) {
			t.Fatalf("recordMachines printed a Superseded note on a live view: %q", e.text)
		}
	}

	typeText(t, a, "ship the fix")
	drive(t, a, key("enter"))
	if a.at(pageHome) {
		t.Fatal("pressing Enter in a live view opened Home instead of sending")
	}
	if len(agent.sent) != 1 || agent.sent[0] != "ship the fix" {
		t.Fatalf("pressing Enter in a live view sent %v, want [ship the fix]", agent.sent)
	}

	// Absent path: the identical setup with no live seam raises the Superseded note.
	localAgent := &fakeAgent{model: "m"}
	local := newTestApp(localAgent)
	local.dismissWelcome()
	local.machines = &flipping{status: chatlist.Running}
	local.taker = &fakeTaker{}
	local.takeUp(Conversation{
		Agent:       localAgent,
		SessionFile: sessionFile,
	}, true)
	local.recordMachines(homeMachinesMsg{rows: []chatlist.Row{remoteRow}})
	if _, elsewhere := local.heldElsewhere(); !elsewhere {
		t.Fatal("absent path: heldElsewhere() was false when Live was nil")
	}
	foundSuperseded := false
	for _, e := range local.entries {
		if e.kind == entryNote && strings.Contains(e.text, chatlist.Superseded("Desktop")) {
			foundSuperseded = true
		}
	}
	if !foundSuperseded {
		t.Fatal("absent path: recordMachines with Live == nil did not print the Superseded note")
	}
}

// V6/T8 (contract 4 last clause and contract 5): when another computer holds
// the keyboard on a live view, the composer-position bar names who is typing
// without the word "take", and pressing Enter calls link.Take once.
func TestALiveWatcherNamesWhoTypesAndCanTakeTheKeyboard(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.dismissWelcome()
	taken := 0
	driving := Driving{Yours: false, Holder: "Desktop"}
	a.takeUp(Conversation{
		Agent: &fakeAgent{model: "m"},
		Link: &LinkSeam{
			Driving: func() Driving { return driving },
			Take: func() error {
				taken++
				driving = Driving{Yours: true}
				return nil
			},
		},
		Live: &LiveSeam{
			Host: "Desktop",
			Read: func() LiveRead { return LiveRead{Kind: LiveOn} },
		},
	}, true)

	if !a.watching() {
		t.Fatal("live view without the keyboard did not report a.watching() == true")
	}
	wantBar := chatlist.LiveTypingFrom("Desktop")
	if strings.Contains(strings.ToLower(wantBar), "take") {
		t.Fatalf("live watcher sentence %q contains the banned word 'take'", wantBar)
	}
	screen := plain(frame(a))
	if !strings.Contains(screen, wantBar) {
		t.Fatalf("live watcher frame is missing %q:\n%s", wantBar, screen)
	}
	if strings.Contains(screen, watchKeysWord) {
		t.Fatalf("live watcher bar drew the non-live right key hint %q:\n%s", watchKeysWord, screen)
	}

	drive(t, a, key("enter"))
	if taken != 1 {
		t.Fatalf("pressing Enter on a live watcher called link.Take %d times, want 1", taken)
	}
	if a.watching() {
		t.Fatal("live view still reported watching after taking the keyboard")
	}
}

// V11/T8 (contract 4 and contract 5): a watch-only live view shows its
// ReadOnly sentence in the composer's place, swallows typed characters without
// altering the draft, and pressing Enter calls neither Submit nor Take.
func TestAWatchOnlyLiveViewSaysWhyItCannotType(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	a.width = 100
	a.title = "chat"
	a.dismissWelcome()
	taken := 0
	reason := chatlist.LiveWatchOnly("Desktop")
	a.takeUp(Conversation{
		Agent: agent,
		Link: &LinkSeam{
			Driving: func() Driving {
				return Driving{Yours: false, Holder: "Desktop", ReadOnly: reason}
			},
			Take: func() error {
				taken++
				return nil
			},
		},
		Live: &LiveSeam{
			Host: "Desktop",
			Read: func() LiveRead { return LiveRead{Kind: LiveOn} },
		},
	}, true)
	a.input.setText("kept draft")

	if !a.watching() {
		t.Fatal("watch-only live view did not report a.watching() == true")
	}
	screen := plain(frame(a))
	if !strings.Contains(screen, reason) {
		t.Fatalf("watch-only live view did not draw %q:\n%s", reason, screen)
	}
	if strings.Contains(screen, "kept draft") {
		t.Fatalf("watch-only live view drew the hidden draft while the bar stood:\n%s", screen)
	}

	drive(t, a, key("x"), key("y"), key("enter"))
	if got := a.input.String(); got != "kept draft" {
		t.Fatalf("typing while watch-only changed the draft to %q, want %q", got, "kept draft")
	}
	if len(agent.sent) != 0 {
		t.Fatalf("pressing Enter while watch-only submitted %v", agent.sent)
	}
	if taken != 0 {
		t.Fatalf("pressing Enter while watch-only called link.Take %d times, want 0", taken)
	}
}

// V7/T7 (contract 4, contract 5, contract 6): when a live link drops to
// LiveWentOffline, the bar says "<Host> went offline", the transcript entries
// stay on screen, and the half-typed draft is hidden and preserved until the
// link returns to LiveOn.
func TestADroppedLiveLinkSaysWentOfflineAndKeepsTheLastViewAndTheDraft(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	a.dismissWelcome()
	kind := LiveOn
	a.takeUp(Conversation{
		Agent: agent,
		Live: &LiveSeam{
			Host: "Desktop",
			Read: func() LiveRead { return LiveRead{Kind: kind} },
		},
	}, true)

	a.entries = []entry{
		{kind: entryUser, text: "first line", turn: 1},
		{kind: entryAssistant, text: "second line", turn: 1},
		{kind: entryUser, text: "third line", turn: 2},
	}
	a.input.setText("half-typed words")
	a.touch()

	kind = LiveWentOffline
	a.touch()
	if !a.watching() {
		t.Fatal("offline live view did not report a.watching() == true")
	}
	screen := plain(frame(a))
	if !strings.Contains(screen, "Desktop went offline") {
		t.Fatalf("offline live view did not draw 'Desktop went offline':\n%s", screen)
	}
	for _, want := range []string{"first line", "second line", "third line"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("offline live view lost transcript entry %q:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "half-typed words") {
		t.Fatalf("offline live view drew the composer draft while the offline bar stood:\n%s", screen)
	}

	drive(t, a, key("z"), key("enter"))
	if len(agent.sent) != 0 {
		t.Fatalf("Enter while offline sent %v", agent.sent)
	}
	if len(a.entries) != 3 {
		t.Fatalf("offline live view entries count changed to %d, want 3", len(a.entries))
	}

	kind = LiveOn
	a.touch()
	if a.watching() {
		t.Fatal("restored live view still reported a.watching() == true")
	}
	restored := plain(frame(a))
	if strings.Contains(restored, "Desktop went offline") {
		t.Fatalf("restored live view kept the offline bar:\n%s", restored)
	}
	if !strings.Contains(restored, "half-typed words") {
		t.Fatalf("restored live view did not bring back the half-typed draft:\n%s", restored)
	}
	if got := a.input.String(); got != "half-typed words" {
		t.Fatalf("draft text = %q, want %q", got, "half-typed words")
	}
}

// V24/T7 (contract 4 and contract 5): an idle-detached live view shows
// chatlist.LiveIdle in the composer position, swallows character keys without
// reconnecting, and calls Reconnect once when Enter is pressed.
func TestAnIdleLiveViewSaysSoAndEnterReconnects(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	a.dismissWelcome()
	reconnected := 0
	a.takeUp(Conversation{
		Agent: agent,
		Live: &LiveSeam{
			Host: "Desktop",
			Read: func() LiveRead { return LiveRead{Kind: LiveIdle} },
			Reconnect: func() {
				reconnected++
			},
		},
	}, true)
	a.input.setText("unsent draft")

	if !a.watching() {
		t.Fatal("idle live view did not report a.watching() == true")
	}
	screen := plain(frame(a))
	if !strings.Contains(screen, chatlist.LiveIdle) {
		t.Fatalf("idle live view did not draw %q:\n%s", chatlist.LiveIdle, screen)
	}

	drive(t, a, key("a"))
	if reconnected != 0 {
		t.Fatalf("character key on an idle live view called Reconnect %d times, want 0", reconnected)
	}
	if got := a.input.String(); got != "unsent draft" {
		t.Fatalf("character key on an idle live view altered the draft to %q", got)
	}

	drive(t, a, key("enter"))
	if reconnected != 1 {
		t.Fatalf("pressing Enter on an idle live view called Reconnect %d times, want 1", reconnected)
	}
	if len(agent.sent) != 0 {
		t.Fatalf("pressing Enter on an idle live view submitted %v", agent.sent)
	}
}

// V24/T12 (contract 8): a keypress, a scroll (mouse wheel and page key) and a
// send each call LiveSeam.Touch on a live front, an active turn touches at most
// once per second on the app's clock, and an idle view generates no calls.
func TestAKeypressAScrollAndASendTouchTheLiveLinkAndAnIdleViewDoesNot(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	a.dismissWelcome()
	now := time.Date(2026, time.November, 23, 14, 0, 0, 0, time.Local)
	a.clock = func() time.Time { return now }

	touches := 0
	a.takeUp(Conversation{
		Agent: agent,
		Live: &LiveSeam{
			Host:  "Desktop",
			Read:  func() LiveRead { return LiveRead{Kind: LiveOn} },
			Touch: func() { touches++ },
		},
	}, true)

	// An idle view generates no Touch calls across frames or elapsed time.
	_ = frame(a)
	now = now.Add(5 * time.Minute)
	_ = frame(a)
	if touches != 0 {
		t.Fatalf("idle live view called Touch %d times, want 0", touches)
	}

	// A keypress calls Touch.
	drive(t, a, key("h"))
	if touches != 1 {
		t.Fatalf("after a keypress Touch count = %d, want 1", touches)
	}

	// A mouse wheel scroll calls Touch.
	drive(t, a, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if touches != 2 {
		t.Fatalf("after a mouse wheel scroll Touch count = %d, want 2", touches)
	}

	// A page-key scroll calls Touch.
	drive(t, a, key("pgdown"))
	if touches != 3 {
		t.Fatalf("after a page-key scroll Touch count = %d, want 3", touches)
	}

	// A send calls Touch.
	drive(t, a, key("enter"))
	if len(agent.sent) != 1 || agent.sent[0] != "h" {
		t.Fatalf("send did not reach the agent: %v", agent.sent)
	}
	if touches != 4 {
		t.Fatalf("after a send Touch count = %d, want 4", touches)
	}

	// While a turn is running, events within the same second do not call Touch
	// again, and advancing the clock by a full second allows one more call.
	stream := make(chan session.Event, 4)
	a.stream = stream
	a.state = stateWorking
	now = now.Add(200 * time.Millisecond)
	a.apply(session.Event{Kind: session.EventTextDelta, Text: "part one"})
	now = now.Add(400 * time.Millisecond)
	a.apply(session.Event{Kind: session.EventTextDelta, Text: " part two"})
	if touches != 4 {
		t.Fatalf("running turn within 1s called Touch %d times, want 4", touches)
	}
	now = now.Add(500 * time.Millisecond)
	a.apply(session.Event{Kind: session.EventTextDelta, Text: " part three"})
	if touches != 5 {
		t.Fatalf("running turn after 1.1s called Touch %d times, want 5", touches)
	}

	// Once the turn ends and the view is idle again, advancing the clock produces no calls.
	a.stream = nil
	a.state = stateIdle
	now = now.Add(10 * time.Second)
	a.paint()
	if touches != 5 {
		t.Fatalf("idle view after turn completion called Touch %d times, want 5", touches)
	}
}

// T3/T7 (contract 9): closing LiveSeam.Changed wakes the update loop to repaint
// at once and re-arms the wait for the next change, while a message from a
// stale conversation generation is dropped without re-arming.
func TestALiveStateChangeRepaintsAtOnce(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.dismissWelcome()

	asked := 0
	first := make(chan struct{})
	close(first)
	second := make(chan struct{})
	kind := LiveOn
	a.takeUp(Conversation{
		Agent: &fakeAgent{model: "m"},
		Live: &LiveSeam{
			Host: "Desktop",
			Read: func() LiveRead { return LiveRead{Kind: kind} },
			Changed: func() <-chan struct{} {
				asked++
				if asked == 1 {
					return first
				}
				return second
			},
		},
	}, true)

	armed := a.armLive()
	if armed == nil {
		t.Fatal("armLive returned nil when LiveSeam.Changed was non-nil")
	}
	kind = LiveWentOffline
	a.dirty = false
	drive(t, a, runCmd(armed)...)
	if asked < 2 {
		t.Fatalf("live state wait did not re-arm after Changed closed: asked %d times, want at least 2", asked)
	}
	if !a.dirty {
		t.Fatal("closing Changed did not mark the surface dirty for repaint")
	}
	if screen := plain(frame(a)); !strings.Contains(screen, "Desktop went offline") {
		t.Fatalf("repaint after Changed did not draw the new state:\n%s", screen)
	}

	// A stale generation is dropped and does not call Changed again.
	before := asked
	a.dirty = false
	if cmd := a.liveMoved(liveChangedMsg{gen: a.convGen - 1}); cmd != nil {
		t.Fatal("stale liveChangedMsg returned a non-nil command")
	}
	if asked != before || a.dirty {
		t.Fatalf("stale liveChangedMsg was not dropped: asked=%d (was %d) dirty=%v", asked, before, a.dirty)
	}
}

// T9 (contract 7): /status and /status --json include the road row only for a
// live front whose Kind is not LiveCopy and whose Road is non-empty.
func TestStatusShowsTheRoadOfALiveConversationOnly(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	if strings.Contains(a.statusText(), "road") || strings.Contains(a.statusJSON(), `"road"`) {
		t.Fatalf("non-live conversation included a road row in /status:\n%s\n%s", a.statusText(), a.statusJSON())
	}

	kind := LiveOn
	road := "relay"
	a.takeUp(Conversation{
		Agent: &fakeAgent{model: "m"},
		Live: &LiveSeam{
			Host: "Desktop",
			Road: road,
			Read: func() LiveRead { return LiveRead{Kind: kind} },
		},
	}, true)

	if got := a.statusText(); !strings.Contains(got, "road") || !strings.Contains(got, "live · relay") {
		t.Fatalf("/status text on a live view is missing 'road' / 'live · relay':\n%s", got)
	}
	if got := a.statusJSON(); !strings.Contains(got, `"road":"live · relay"`) {
		t.Fatalf("/status JSON on a live view is missing \"road\":\"live · relay\":\n%s", got)
	}

	// LiveCopy omits the road row because no live connection is carrying traffic.
	kind = LiveCopy
	if strings.Contains(a.statusText(), "live · relay") || strings.Contains(a.statusJSON(), `"road"`) {
		t.Fatalf("LiveCopy conversation still showed a road row:\n%s\n%s", a.statusText(), a.statusJSON())
	}

	// An empty Road omits the road row under the emptiness law.
	kind = LiveOn
	a.liveFront().Road = ""
	if strings.Contains(a.statusText(), "road") || strings.Contains(a.statusJSON(), `"road"`) {
		t.Fatalf("live view with empty Road showed a road row:\n%s\n%s", a.statusText(), a.statusJSON())
	}
}

// V30, V41: /status says which road the bytes travel from what the door reads
// on every draw, so the same live view says `live · relay` and then `live ·
// direct` and back as the road changes, and the seam's own Road is only what the
// first paint knows.
func TestStatusShowsLiveDirectAndLiveRelayFromTheRoadTheLinkReports(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	road := ""
	a.takeUp(Conversation{
		Agent: &fakeAgent{model: "m"},
		Live: &LiveSeam{
			Host: "Desktop",
			Road: "relay",
			Read: func() LiveRead { return LiveRead{Kind: LiveOn, Road: road} },
		},
	}, true)

	for _, step := range []struct{ said, want string }{
		{"", "live · relay"}, // the door has not said yet: the first paint's word
		{"direct", "live · direct"},
		{"relay", "live · relay"},
		{"direct", "live · direct"},
	} {
		road = step.said
		text, json := a.statusText(), a.statusJSON()
		if !strings.Contains(text, step.want) || !strings.Contains(json, `"road":"`+step.want+`"`) {
			t.Fatalf("with the door saying %q, /status should say %q:\n%s\n%s", step.said, step.want, text, json)
		}
		other := "live · relay"
		if step.want == other {
			other = "live · direct"
		}
		if strings.Contains(text, other) || strings.Contains(json, other) {
			t.Fatalf("with the door saying %q, /status also says %q:\n%s\n%s", step.said, other, text, json)
		}
	}
}

// V30: a road swap redraws the view and says nothing about it: the door's change
// signal is what asks for the redraw, and no banner, notice or note is added.
func TestARoadFlipRedrawsWithoutABanner(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	road := "relay"
	changed := make(chan struct{})
	a.takeUp(Conversation{
		Agent: &fakeAgent{model: "m"},
		Live: &LiveSeam{
			Host:    "Desktop",
			Road:    "relay",
			Read:    func() LiveRead { return LiveRead{Kind: LiveOn, Road: road} },
			Changed: func() <-chan struct{} { return changed },
		},
	}, true)
	before := a.statusText()
	barBefore, onBefore := a.liveBar()

	road = "direct"
	close(changed)
	if cmd := a.liveMoved(liveChangedMsg{gen: a.convGen}); cmd == nil {
		t.Fatalf("a road flip did not ask to be watched again")
	}
	after := a.statusText()
	if after == before || !strings.Contains(after, "live · direct") {
		t.Fatalf("/status did not follow the road flip:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if barAfter, onAfter := a.liveBar(); barAfter != barBefore || onAfter != onBefore {
		t.Fatalf("a road flip changed the bar above the composer: %q -> %q", barBefore, barAfter)
	}
}

// V27 (contract 4 and contract 5): an offline copy (LiveCopy) shows the last
// seen bar formatted by chatlist.LiveSeenAt, swallows typed keys, and sends
// nothing when Enter is pressed while keeping existing words in the box.
func TestAnOfflineCopyShowsTheLastSeenBarAndSendsNothing(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	a.dismissWelcome()
	now := time.Date(2026, time.November, 23, 16, 30, 0, 0, time.Local)
	seen := time.Date(2026, time.November, 23, 14, 2, 0, 0, time.Local)
	a.clock = func() time.Time { return now }

	taken := 0
	a.takeUp(Conversation{
		Agent: agent,
		Link: &LinkSeam{
			Take: func() error {
				taken++
				return nil
			},
		},
		Live: &LiveSeam{
			Host: "Desktop",
			Road: "relay",
			Read: func() LiveRead { return LiveRead{Kind: LiveCopy, SeenAt: seen} },
		},
	}, true)
	a.input.setText("words already here")

	if !a.watching() {
		t.Fatal("offline copy did not report a.watching() == true")
	}
	wantBar := "Desktop offline · last seen 14:02"
	screen := plain(frame(a))
	if !strings.Contains(screen, wantBar) {
		t.Fatalf("offline copy did not draw %q:\n%s", wantBar, screen)
	}

	drive(t, a, key("q"), key("enter"))
	if len(agent.sent) != 0 {
		t.Fatalf("pressing Enter on an offline copy submitted %v", agent.sent)
	}
	if taken != 0 {
		t.Fatalf("pressing Enter on an offline copy called link.Take %d times, want 0", taken)
	}
	if got := a.input.String(); got != "words already here" {
		t.Fatalf("offline copy draft changed to %q, want %q", got, "words already here")
	}

	// Verify LiveSeenAt across calendar days and zero time, and LiveNotSentOffline.
	older := time.Date(2026, time.January, 2, 14, 2, 0, 0, time.Local)
	if got := chatlist.LiveSeenAt(older, now); got != "2 Jan 14:02" {
		t.Fatalf("LiveSeenAt across days = %q, want %q", got, "2 Jan 14:02")
	}
	if got := chatlist.LiveSeenAt(time.Time{}, now); got != "" {
		t.Fatalf("LiveSeenAt on zero time = %q, want empty", got)
	}
	if got := chatlist.LiveNotSentOffline("Desktop"); got != "not sent. Desktop is offline; your words are still in the box" {
		t.Fatalf("LiveNotSentOffline = %q", got)
	}
}

// V14 (contract 10): with Options.OpenLive == nil and Conversation.Live == nil,
// every live-view behaviour is absent and the surface draws and acts as before.
func TestWithNoLiveSeamTheSurfaceDrawsAndActsAsBefore(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.dismissWelcome()

	if a.openLive != nil {
		t.Fatal("default Options left a.openLive non-nil")
	}
	if a.liveFront() != nil {
		t.Fatalf("liveFront() = %+v, want nil", a.liveFront())
	}
	if got := a.trafficHint(); got != "" {
		t.Fatalf("trafficHint() with no live seam = %q, want empty", got)
	}
	if got := a.seamPieces(a.width).host; got != "" {
		t.Fatalf("seamPieces.host with no live seam = %q, want empty", got)
	}
	if a.watching() {
		t.Fatal("watching() was true with no live seam and no driving seam")
	}
	if a.armLive() != nil {
		t.Fatal("armLive() armed a command with no live seam")
	}
	if strings.Contains(a.statusText(), "road") || strings.Contains(a.statusJSON(), `"road"`) {
		t.Fatalf("/status with no live seam contained a road row:\n%s", a.statusText())
	}
}
