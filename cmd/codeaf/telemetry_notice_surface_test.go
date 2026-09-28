package main

// The door's half of the usage notice: a chat that will draw a full-screen
// surface does not print the notice onto the normal screen the surface is about
// to cover, and does not mark it seen. It owes it to the surface, and the notice
// is marked seen by the surface's own report that a frame drew it. Until then
// nothing is flushed, because [telemetry.Flush] sends nothing before the mark.

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/telemetry"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// noticeOnATerminal stands a terminal behind stderr for one test and clears
// whatever the notice's bookkeeping was left at.
func noticeOnATerminal(t *testing.T) {
	t.Helper()
	previous := noticeTerminal
	noticeTerminal = func() bool { return true }
	t.Cleanup(func() {
		noticeTerminal = previous
		telemetryNoticeOwed = false
	})
	telemetryNoticeOwed = false
}

// Contract 2.1 and 2.2: a chat on a terminal leaves the notice unmarked at the
// start, hands the exact notice to the surface, and marks it seen only when the
// surface reports the frame that drew it.
func TestAChatHandsTheNoticeToTheSurfaceAndMarksItOnlyWhenDrawn(t *testing.T) {
	telemetryLifecycleHome(t)
	noticeOnATerminal(t)
	restore := telemetryArgs("chat")
	defer restore()

	session := telemetryBegin()
	if session.mode != telemetry.ModeChat {
		t.Fatalf("chat: mode=%q", session.mode)
	}
	if telemetry.NoticeShown() {
		t.Fatal("the chat marked the notice seen before the surface could draw it")
	}

	previous := runSurfaceProgram
	t.Cleanup(func() { runSurfaceProgram = previous })
	var seen tui3.Options
	runSurfaceProgram = func(_ context.Context, options tui3.Options) error {
		seen = options
		return nil
	}
	if err := runSurface(context.Background(), tui3.Options{Agent: &quietAgent{}, ProfileDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if seen.TelemetryNotice != telemetry.Notice {
		t.Fatalf("the surface was handed %q, want the notice byte for byte", seen.TelemetryNotice)
	}
	if seen.TelemetryNoticeShown == nil {
		t.Fatal("the surface was handed no way to say the notice was drawn")
	}
	if telemetry.NoticeShown() {
		t.Fatal("the notice was marked seen before the surface said a frame drew it")
	}
	seen.TelemetryNoticeShown()
	if !telemetry.NoticeShown() {
		t.Fatal("the surface said the notice was drawn and it was not marked seen")
	}
}

// Contract 2.3: a task command still prints the notice and marks it at once,
// because a task runs unattended and draws no surface.
func TestATaskStillPrintsTheNoticeAndOwesTheSurfaceNothing(t *testing.T) {
	telemetryLifecycleHome(t)
	noticeOnATerminal(t)
	restore := telemetryArgs("do", "fix the bug")
	defer restore()

	telemetryBegin()
	if !telemetry.NoticeShown() {
		t.Fatal("a task command did not mark the notice it printed")
	}
	if telemetryNoticeOwed {
		t.Fatal("a task command left the notice owed to a surface it never draws")
	}
}
