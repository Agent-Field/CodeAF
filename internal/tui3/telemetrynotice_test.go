package tui3

// THE USAGE NOTICE IS READ ON THE SURFACE, BEFORE ANYTHING IT DESCRIBES HAPPENS.
// It used to be printed on the normal screen a moment before the full-screen
// surface covered it, and marked as seen at the same moment, so a new person met
// it only after quitting — by which time the exit had already sent the counts it
// describes (the fresh-install check of 2026-09-25). These tests hold the
// surface's half: the first conversation's screen draws the notice whole, and
// the door is told it was seen only after a frame has drawn it.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/telemetry"
)

// noticeWords is one notice line as the screen reads it: the fields rejoined,
// the same flattening [welcomeScreen] applies to the frame.
func noticeWords(line string) string {
	return strings.Join(strings.Fields(line), " ")
}

// owingApp is the first conversation's surface with the notice still owed and a
// counter standing where the door's "it was seen" write would be.
func owingApp(t *testing.T, height int) (*app, *int) {
	t.Helper()
	a := firstChatApp(t)
	seen := 0
	a.telemetryNotice = telemetry.Notice
	a.telemetryNoticeShown = func() { seen++ }
	a.width, a.height = 120, height
	a.touch()
	return a, &seen
}

// Contract 2.1 and 2.2: the first conversation's screen carries every line of
// the notice, and the door hears that it was seen only on the loop after a
// frame drew it — never before the first frame, and never twice.
func TestTheFirstConversationShowsTheUsageNoticeBeforeAnythingIsSent(t *testing.T) {
	a, seen := owingApp(t, 44)

	pressSetup(a, tea.WindowSizeMsg{Width: 120, Height: 44})
	if *seen != 0 {
		t.Fatalf("the notice was counted as seen %d times before any frame drew it", *seen)
	}
	screen := welcomeScreen(a)
	for _, line := range strings.Split(telemetry.Notice, "\n") {
		if !strings.Contains(screen, noticeWords(line)) {
			t.Fatalf("the first conversation's screen is missing the notice line %q:\n%s", line, screen)
		}
	}
	pressSetup(a, tea.WindowSizeMsg{Width: 120, Height: 44})
	if *seen != 1 {
		t.Fatalf("after a frame drew the notice the door heard %d times, want once", *seen)
	}
	_ = welcomeScreen(a)
	pressSetup(a, tea.WindowSizeMsg{Width: 120, Height: 44})
	if *seen != 1 {
		t.Fatalf("a second frame told the door again: %d times, want once", *seen)
	}
}

// Contract 2.2: a frame with no room for the whole notice does not draw half of
// it and does not count it as seen, so it is still owed on the next launch.
func TestAFrameWithNoRoomForTheNoticeDoesNotCountItAsSeen(t *testing.T) {
	a, seen := owingApp(t, 24)
	screen := welcomeScreen(a)
	if strings.Contains(screen, noticeWords(strings.Split(telemetry.Notice, "\n")[0])) {
		t.Fatalf("the notice was drawn on a frame the test sized too short for it:\n%s", screen)
	}
	pressSetup(a, tea.WindowSizeMsg{Width: 120, Height: 24})
	if *seen != 0 {
		t.Fatalf("a notice no frame drew was counted as seen %d times", *seen)
	}
}

// Contract 2.2: the first-run setup stands in front of the greeting, and a
// notice behind it is not on the screen, so it is not seen either. The options
// are the door's own, handed in the way the door hands them.
func TestTheUsageNoticeBehindTheSetupIsNotCountedAsSeen(t *testing.T) {
	a, _, _ := setupApp(t, nil)
	seen := 0
	b := newApp(t.Context(), Options{
		Agent:                &fakeAgent{model: "openai/gpt-4.1-mini"},
		Workspace:            "/tmp/lab",
		ProfileDir:           a.profileDir,
		Setup:                true,
		ApplyAPIKey:          func(string) error { return nil },
		TelemetryNotice:      telemetry.Notice,
		TelemetryNoticeShown: func() { seen++ },
	})
	b.width, b.height = 120, 44
	b.touch()
	if !b.setup.open {
		t.Fatal("the fixture's setup is not in front")
	}
	screen := welcomeScreen(b)
	if strings.Contains(screen, noticeWords(strings.Split(telemetry.Notice, "\n")[0])) {
		t.Fatalf("the setup screen drew the notice:\n%s", screen)
	}
	pressSetup(b, tea.WindowSizeMsg{Width: 120, Height: 44})
	if seen != 0 {
		t.Fatalf("a notice behind the setup was counted as seen %d times", seen)
	}
}
