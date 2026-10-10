package tui3

// desktopnotify.go raises an automation's desktop notification — a reminder, a
// watch that spoke, a run that needs the person — through the operating system,
// because the person it is for is, by the nature of a reminder, looking at
// something else.
//
// THE OPERATING SYSTEM FIRST, THE TERMINAL SECOND. A turn's banner rides the
// terminal's own notification escape (notify.go), which many terminals — Apple's
// own among them — never show. An automation's news is the one banner whose
// whole point is to reach somebody who is not looking, so on macOS it goes
// through `osascript` and on Linux through `notify-send` when there is one, and
// only a machine with neither falls back to the escape.
//
// IT RUNS IN A COMMAND AND NEVER ON THE LOOP: starting a process is a door, and
// the update loop opens none (framedisk_law_test.go).

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

// automationNotifyMsg asks the update loop to send the terminal's own
// notification escape, for a machine with no native notifier.
type automationNotifyMsg struct{ title, body string }

// osNotify is the native notifier this window raises banners through. It is a
// variable for one reason: a test raises a run's news and must not put a real
// banner on the screen of whoever is running the suite.
var osNotify = desktopNotify

// desktopNotify raises one banner through the operating system and reports
// whether it could; false means the caller should fall back to the terminal.
func desktopNotify(title, body string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	switch runtime.GOOS {
	case "darwin":
		// THE WORDS TRAVEL AS ARGUMENTS, never inside the script, so nothing a
		// title or a run's line says can be read as AppleScript.
		script := []string{
			"-e", "on run argv",
			"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
			"-e", "end run",
			title, body,
		}
		return exec.CommandContext(ctx, "osascript", script...).Run() == nil
	case "linux":
		if _, err := exec.LookPath("notify-send"); err != nil {
			return false
		}
		return exec.CommandContext(ctx, "notify-send", "--app-name=codeaf", title, body).Run() == nil
	}
	return false
}
