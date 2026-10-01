package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
)

func leftOffRow(cell, device string, status chatlist.Status, ago time.Duration) chatlist.Row {
	return chatlist.Row{Cell: cell, Title: cell, Device: device, DeviceID: "dev_" + device, Status: status, DurableAgo: ago}
}

func TestResumePicksTheNewestRecentChatOnAnOfflineDevice(t *testing.T) {
	cases := []struct {
		name  string
		state resumeState
		want  string
	}{
		{"off without a feed", resumeState{rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Off, time.Hour)}}, "a"},
		{"running without a feed is busy", resumeState{rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Running, time.Minute)}}, ""},
		{"running on an offline device", resumeState{feedUp: true, rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Running, time.Minute)}}, "a"},
		{"online and busy", resumeState{feedUp: true, online: []string{"dev_spark"}, rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Running, time.Minute)}}, ""},
		{"online with a lapsed lease", resumeState{feedUp: true, online: []string{"dev_spark"}, rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Off, time.Minute)}}, "a"},
		{"online and let go", resumeState{feedUp: true, online: []string{"dev_spark"}, rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Idle, time.Minute)}}, ""},
		{"too old", resumeState{rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Off, 3*resumeRecent)}}, ""},
		{"this device", resumeState{rows: []chatlist.Row{leftOffRow("a", "mac", chatlist.Here, time.Minute)}}, ""},
		{"newest wins", resumeState{rows: []chatlist.Row{
			leftOffRow("old", "dumb", chatlist.Off, 5*time.Hour), leftOffRow("new", "spark", chatlist.Off, time.Hour)}}, "new"},
	}
	for _, c := range cases {
		got := c.state.pick()
		switch {
		case c.want == "" && got != nil:
			t.Errorf("%s: offered %q", c.name, got.Cell)
		case c.want != "" && (got == nil || got.Cell != c.want):
			t.Errorf("%s: got %v, want %q", c.name, got, c.want)
		}
	}
}

func leftOffApp(taker Taker) *app {
	a := newTestApp(&fakeAgent{model: "m"})
	a.taker = taker
	return a
}

func leftOffFrame(a *app) string {
	ctx := ambientBandContext(a, session.SessionRow{}, a.now(), 80)
	return plain(strings.Join(drawResumeBand(a, ctx), "\n"))
}

func TestResumeIsDecidedAgainWhenTheFeedSaysTheDeviceWentOffline(t *testing.T) {
	a, feed := devicesRig("dev_spark")
	a.taker = &fakeTaker{}
	a.tookMachines(homeMachinesMsg{rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Running, time.Minute)}})
	if leftOffFrame(a) != "" {
		t.Fatal("offered a chat on a device that is still online")
	}
	// The relay's offline debounce is over: the feed drops the device.
	feed.state.Online = nil
	a.tookWatch(dirWatchMsg{from: feed})
	if got := leftOffFrame(a); !strings.Contains(got, "Continue where you left off on spark?") {
		t.Fatalf("no card after the device went offline:\n%s", got)
	}
	// It comes back: the reason is gone and the card with it.
	feed.state.Online = []string{"dev_spark"}
	a.tookWatch(dirWatchMsg{from: feed})
	if leftOffFrame(a) != "" {
		t.Fatal("the card stayed after the device came back")
	}
}

func TestResumeFollowsALapsedLeaseWhenTheFeedIsDown(t *testing.T) {
	a := leftOffApp(&fakeTaker{})
	a.tookMachines(homeMachinesMsg{rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Running, time.Minute)}})
	if leftOffFrame(a) != "" {
		t.Fatal("offered a chat whose lease is held")
	}
	a.tookMachines(homeMachinesMsg{rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Off, time.Minute)}})
	if leftOffFrame(a) == "" {
		t.Fatal("a lapsed lease did not raise the card on the next listing")
	}
}

func TestResumeStaysAnsweredOncePutOff(t *testing.T) {
	a, feed := devicesRig()
	a.taker = &fakeTaker{}
	a.tookMachines(homeMachinesMsg{rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Off, time.Hour)}})
	a.resumeKeyHandler(resumeSkipKey)
	a.tookWatch(dirWatchMsg{from: feed})
	a.tookMachines(homeMachinesMsg{rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Off, time.Hour)}})
	if leftOffFrame(a) != "" {
		t.Fatal("a device put off was offered again")
	}
}

func TestResumeOffersOnceAndLeadsToTheTakeoverCard(t *testing.T) {
	a := leftOffApp(&fakeTaker{})
	a.tookMachines(homeMachinesMsg{rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Off, time.Hour)}})
	got := leftOffFrame(a)
	for _, want := range []string{"Continue where you left off on spark?", resumeYes, resumeNo} {
		if !strings.Contains(got, want) {
			t.Fatalf("card lacks %q:\n%s", want, got)
		}
	}
	cmd, took := a.resumeKeyHandler(resumeKey)
	if !took || cmd != nil {
		t.Fatalf("took=%v cmd=%v", took, cmd)
	}
	ask, ok := a.homeAsking()
	if !ok || ask.question.Kind != homeContinueKind {
		t.Fatal("the takeover card was not raised")
	}
	if leftOffFrame(a) != "" {
		t.Fatal("the offer stayed after it was taken up")
	}
}

func TestResumeCanBeDismissedAndIsAbsentWithoutATaker(t *testing.T) {
	a := leftOffApp(&fakeTaker{})
	a.tookMachines(homeMachinesMsg{rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Off, time.Hour)}})
	if _, took := a.resumeKeyHandler(resumeSkipKey); !took || leftOffFrame(a) != "" {
		t.Fatal("not now did not dismiss the offer")
	}
	if _, took := a.resumeKeyHandler(resumeKey); took {
		t.Fatal("a dismissed offer still claims its chord")
	}
	b := leftOffApp(nil)
	b.tookMachines(homeMachinesMsg{rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Off, time.Hour)}})
	if leftOffFrame(b) != "" {
		t.Fatal("offered a takeover nothing is behind")
	}
}

// TestResumeAppearsWithinTheBoundWhenTheLeaseLapses is the feed-down bound on a
// clock the test owns: a chat whose lease lapses while home stands open is
// offered by the listing that follows, no later than machinesCap (plus one beat)
// after it lapsed, however far the listing's pace had slowed.
func TestResumeAppearsWithinTheBoundWhenTheLeaseLapses(t *testing.T) {
	a, src, now := pollHome(t)
	a.taker = &fakeTaker{}
	row := func(s chatlist.Status) chatlist.Static {
		return chatlist.Static{leftOffRow("c1", "spark", s, time.Minute)}
	}
	src.rows = row(chatlist.Running)
	for i := 0; i < 200; i++ { // the pace settles at its slowest
		beat(t, a, now, homeEvery)
	}
	if leftOffFrame(a) != "" {
		t.Fatal("offered a chat whose lease is held")
	}
	src.rows = row(chatlist.Off)
	lapsed := *now
	for !strings.Contains(leftOffFrame(a), "Continue where you left off on spark?") {
		if waited := now.Sub(lapsed); waited > machinesCap+homeEvery {
			t.Fatalf("no card %s after the lease lapsed, want within %s", waited, machinesCap)
		}
		beat(t, a, now, homeEvery)
	}
}

// TestPresenceIsReadOffTheRowAndTheCardTogether is the whole journey on one
// screen pair: online and busy (filled dot, no card), killed (hollow dot,
// "(offline)", card), back online (filled dot, card withdrawn).
func TestPresenceIsReadOffTheRowAndTheCardTogether(t *testing.T) {
	a, feed := devicesRig("dev_spark")
	a.tookMachines(homeMachinesMsg{rows: []chatlist.Row{leftOffRow("c1", "spark", chatlist.Running, time.Minute)}})
	rowOf := func() string { return devicesFrame(a, 80) }
	if !strings.Contains(rowOf(), "● spark") || leftOffFrame(a) != "" {
		t.Fatalf("online and busy:\n%s\n%s", rowOf(), leftOffFrame(a))
	}
	feed.state.Online = nil
	a.tookWatch(dirWatchMsg{from: feed})
	if !strings.Contains(rowOf(), "○ spark (offline)") || leftOffFrame(a) == "" {
		t.Fatalf("killed:\n%s\n%s", rowOf(), leftOffFrame(a))
	}
	feed.state.Online = []string{"dev_spark"}
	a.tookWatch(dirWatchMsg{from: feed})
	if !strings.Contains(rowOf(), "● spark") || leftOffFrame(a) != "" {
		t.Fatalf("back online:\n%s\n%s", rowOf(), leftOffFrame(a))
	}
}

// TestPresenceDotsDifferInColourToo checks the row paints a device that is
// here and one that is away with different inks, where the terminal has any.
func TestPresenceDotsDifferInColourToo(t *testing.T) {
	a, _ := devicesRig("dev_spark")
	ctx := ambientBandContext(a, session.SessionRow{}, a.now(), 80)
	pal := ctx.pal
	if pal.add("x") == pal.dim("x") {
		t.Skip("this palette paints no colour")
	}
	if paintPresence(pal, "● spark", true) == paintPresence(pal, "○ dumb", false) ||
		paintPresence(pal, "x", true) == paintPresence(pal, "x", false) {
		t.Fatal("online and away are painted alike")
	}
}
