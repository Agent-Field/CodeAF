package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/automation"
)

// THE TERMINAL IS READ ONCE, AND THE BANNER GOES WHERE IT CAN BE CLICKED BACK.
// iTerm2 speaks OSC 9, four others OSC 777, and inside tmux — which swallows an
// OSC it does not know — the operating system's notifier is the road, as it is
// for every terminal that speaks neither.
func TestTheBannerRouteIsReadFromTheTerminal(t *testing.T) {
	for _, c := range []struct {
		name   string
		env    map[string]string
		goos   string
		escape string
		via    string
	}{
		{"iTerm2", map[string]string{"TERM_PROGRAM": "iTerm.app"}, "darwin", notifyOSC9, "iTerm2"},
		{"iTerm2 by its own variable", map[string]string{"LC_TERMINAL": "iTerm2"}, "darwin", notifyOSC9, "iTerm2"},
		{"Ghostty", map[string]string{"TERM_PROGRAM": "ghostty"}, "darwin", notifyOSC777, "Ghostty"},
		{"kitty", map[string]string{"TERM": "xterm-kitty"}, "linux", notifyOSC777, "kitty"},
		{"WezTerm", map[string]string{"TERM_PROGRAM": "WezTerm"}, "darwin", notifyOSC777, "WezTerm"},
		{"foot", map[string]string{"TERM": "foot"}, "linux", notifyOSC777, "foot"},
		{"tmux inside iTerm2", map[string]string{"TERM_PROGRAM": "tmux", "LC_TERMINAL": "iTerm2", "TMUX": "/tmp/tmux-1/default,1,0"}, "darwin", "", notifyViaScriptEditor},
		{"Apple's Terminal", map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, "darwin", "", notifyViaScriptEditor},
		{"a Linux terminal", map[string]string{"TERM": "xterm-256color"}, "linux", "", notifyViaDesktop},
	} {
		t.Run(c.name, func(t *testing.T) {
			route := DetectNotifyRoute(func(name string) string { return c.env[name] }, c.goos, "/somewhere")
			if route.Escape != c.escape || route.Via != c.via || route.Dir != "/somewhere" {
				t.Fatalf("route %+v, want escape %q via %q", route, c.escape, c.via)
			}
		})
	}
}

// iTerm2's banner is one string, so the title leads it, and nothing in a run's
// line can end the sequence early.
func TestITerm2sBannerIsOneString(t *testing.T) {
	route := NotifyRoute{Escape: notifyOSC9, Via: "iTerm2"}
	got := route.escapeFor("codeaf · leave", "done · time\ato go")
	if got != "\x1b]9;codeaf · leave — done · timeto go\a" {
		t.Fatalf("the sequence is %q", got)
	}
	if other := (NotifyRoute{Escape: notifyOSC777}).escapeFor("codeaf · leave", "done"); other != notifySeq("codeaf · leave", "done") {
		t.Fatalf("OSC 777 is %q", other)
	}
}

// bannerOf runs the reading a finished run arrives in, and the banner command
// it returns, and answers what came back.
func bannerOf(t *testing.T, a *app) []automationNotifyMsg {
	t.Helper()
	cmd := a.readAutomations()
	if cmd == nil {
		t.Fatal("the surface has no automations seam to read")
	}
	msg, ok := cmd().(automationsReadMsg)
	if !ok {
		t.Fatal("the reading did not come back as a reading")
	}
	var out []automationNotifyMsg
	for _, m := range runCmd(a.automationsRead(msg)) {
		if banner, ok := m.(automationNotifyMsg); ok {
			out = append(out, banner)
		}
	}
	return out
}

// A WINDOW WITH THE KEYBOARD STILL RAISES THE BANNER. A reminder is the one
// banner a person set up on purpose, and `r` on its row is a person asking to
// see it.
func TestABannerIsRaisedWhileTheWindowHasTheKeyboard(t *testing.T) {
	a, store := automationLab(t, labReminder("leave"))
	a.focused, a.seenFocus = true, true
	raisedMu.Lock()
	raised = nil
	raisedMu.Unlock()
	finishRun(t, store, a.watch.list[0].ID, automation.OutcomeDone, "time to leave")
	bannerOf(t, a)
	raisedMu.Lock()
	defer raisedMu.Unlock()
	if len(raised) != 1 || !strings.Contains(raised[0], "codeaf · leave") {
		t.Fatalf("a focused window raised %q", raised)
	}
}

// THROUGH THE TERMINAL, THE BANNER IS THE TERMINAL'S OWN SEQUENCE, and the
// operating system is not asked.
func TestABannerRidesTheTerminalWhenItSpeaksTheEscape(t *testing.T) {
	a, store := automationLab(t, labReminder("leave"))
	a.notifyRoute = NotifyRoute{Escape: notifyOSC9, Via: "iTerm2"}
	raisedMu.Lock()
	raised = nil
	raisedMu.Unlock()
	finishRun(t, store, a.watch.list[0].ID, automation.OutcomeDone, "time to leave")
	banners := bannerOf(t, a)
	if len(banners) != 1 || !banners[0].escape || banners[0].fallback {
		t.Fatalf("the banner came back as %+v", banners)
	}
	raisedMu.Lock()
	asked := len(raised)
	raisedMu.Unlock()
	if asked != 0 {
		t.Fatalf("the operating system was asked %d times for a banner the terminal raises", asked)
	}
	raw, ok := a.automationNotified(banners[0])().(tea.RawMsg)
	if !ok || !strings.HasPrefix(raw.Msg.(string), "\x1b]9;codeaf · leave — ") {
		t.Fatalf("the terminal was sent %+v", raw)
	}
}

// THE RUN'S LINE IS SAID ON THE PAGE IN FRONT. `r` pressed on a row of
// /automations is answered there, not only in a conversation behind it.
func TestARunsLineIsSaidOnThePlaceInFront(t *testing.T) {
	a, store := automationLab(t, labReminder("leave"))
	runCmd(a.openAutomations())
	finishRun(t, store, a.watch.list[0].ID, automation.OutcomeDone, "time to leave")
	bannerOf(t, a)
	if a.pageMsg != "leave · done · time to leave" {
		t.Fatalf("the place's message line says %q", a.pageMsg)
	}
}

// THE FIRST BANNER SAYS, ONCE, WHERE BANNERS ARE TURNED ON — with the address
// of the settings that do it on the line — and the mark is kept, so a second
// window, or the next launch, does not say it again.
func TestTheFirstBannerSaysWhereBannersAreTurnedOnOnce(t *testing.T) {
	keep := notifyTellsSettings
	notifyTellsSettings = true
	t.Cleanup(func() { notifyTellsSettings = keep })
	dir := t.TempDir()
	a, store := automationLab(t, labReminder("leave"), labReminder("stretch"))
	a.notifyRoute = NotifyRoute{Escape: notifyOSC9, Via: "iTerm2", Dir: dir}
	runCmd(a.openAutomations())
	finishRun(t, store, a.watch.list[0].ID, automation.OutcomeDone, "")
	banners := bannerOf(t, a)
	if len(banners) != 1 || banners[0].told != "iTerm2" {
		t.Fatalf("the first banner came back as %+v", banners)
	}
	runCmd(a.automationNotified(banners[0]))
	if a.pageMsg != notifyFirstLine("iTerm2") || a.msgLink != notifySettingsURL {
		t.Fatalf("the line says %q, linked to %q", a.pageMsg, a.msgLink)
	}
	if line, ok := a.placeMsgLine(a.width); !ok || !strings.Contains(line, notifySettingsURL) {
		t.Fatalf("the drawn line carries no link: %q", line)
	}
	if _, err := os.Stat(filepath.Join(dir, notifyToldName)); err != nil {
		t.Fatalf("the mark was not kept: %v", err)
	}
	finishRun(t, store, a.watch.list[1].ID, automation.OutcomeDone, "")
	if again := bannerOf(t, a); len(again) != 1 || again[0].told != "" {
		t.Fatalf("the second banner came back as %+v", again)
	}
	other := newTestApp(&fakeAgent{model: "m"})
	if firstBanner(dir, other.notifyFirst) {
		t.Fatal("a second window said it again")
	}
}

// A RUN CODEAF CLOSING CUT OFF TELLS NOBODY: no line, no banner, nothing on the
// page. It is paused, and the run that picks up after it is the one told.
func TestACutOffRunTellsNobody(t *testing.T) {
	a, store := automationLab(t, labWork("weekly update", "0 9 * * 1"))
	runCmd(a.openAutomations())
	before := len(a.entries)
	raisedMu.Lock()
	raised = nil
	raisedMu.Unlock()
	finishRun(t, store, a.watch.list[0].ID, automation.OutcomeStopped, automation.LineClosed)
	if banners := bannerOf(t, a); len(banners) != 0 {
		t.Fatalf("a cut-off run raised %+v", banners)
	}
	for _, e := range a.entries[before:] {
		if e.kind == entryAutomation {
			t.Fatalf("a cut-off run drew %q", e.auto.line)
		}
	}
	if a.pageMsg != "" {
		t.Fatalf("a cut-off run was said on the page: %q", a.pageMsg)
	}
}

// THE LOGO notify-send SHOWS IS WRITTEN ONCE, beside the mark.
func TestTheLogoIsKeptForTheDesktopNotifier(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "notify")
	path := notifyIcon(dir)
	if path == "" {
		t.Fatal("no logo was written")
	}
	got, err := os.ReadFile(path)
	if err != nil || len(got) != len(notifyLogo) || len(got) == 0 {
		t.Fatalf("the logo on disk is %d bytes, want %d: %v", len(got), len(notifyLogo), err)
	}
	if notifyIcon("") != "" {
		t.Fatal("a window with nowhere to keep it wrote the logo somewhere")
	}
}
