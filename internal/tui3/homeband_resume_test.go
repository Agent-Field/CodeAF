package tui3

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
)

func leftOffRow(cell, device string, status chatlist.Status, ago time.Duration) chatlist.Row {
	return chatlist.Row{Cell: cell, Title: cell, Device: device, Status: status, DurableAgo: ago}
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
		{"online and busy", resumeState{feedUp: true, online: []string{"spark"}, rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Running, time.Minute)}}, ""},
		{"online and idle", resumeState{feedUp: true, online: []string{"spark"}, rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Off, time.Minute)}}, ""},
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

func TestResumeIsDecidedOnceFromTheFirstGoodListing(t *testing.T) {
	a := leftOffApp(&fakeTaker{})
	a.considerResume(homeMachinesMsg{err: errors.New("down")})
	if a.leftOff.decided {
		t.Fatal("a failed listing decided the offer")
	}
	off := []chatlist.Row{leftOffRow("a", "spark", chatlist.Off, time.Hour)}
	a.considerResume(homeMachinesMsg{rows: nil})
	a.considerResume(homeMachinesMsg{rows: off})
	if leftOffFrame(a) != "" {
		t.Fatal("a later listing re-decided the offer")
	}
}

func TestResumeOffersOnceAndLeadsToTheTakeoverCard(t *testing.T) {
	a := leftOffApp(&fakeTaker{})
	a.considerResume(homeMachinesMsg{rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Off, time.Hour)}})
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
	a.considerResume(homeMachinesMsg{rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Off, time.Hour)}})
	if _, took := a.resumeKeyHandler(resumeSkipKey); !took || leftOffFrame(a) != "" {
		t.Fatal("not now did not dismiss the offer")
	}
	if _, took := a.resumeKeyHandler(resumeKey); took {
		t.Fatal("a dismissed offer still claims its chord")
	}
	b := leftOffApp(nil)
	b.considerResume(homeMachinesMsg{rows: []chatlist.Row{leftOffRow("a", "spark", chatlist.Off, time.Hour)}})
	if leftOffFrame(b) != "" {
		t.Fatal("offered a takeover nothing is behind")
	}
}
