package tui3

// desktopnotify.go raises an automation's desktop notification — a reminder, a
// watch that spoke, a run that needs the person — because the person it is for
// is, by the nature of a reminder, looking at something else.
//
// THE TERMINAL FIRST WHEN IT SPEAKS THE ESCAPE, THE OPERATING SYSTEM OTHERWISE.
// A banner a terminal raises for its own tab is the one that can do what a
// person wants when they click it: bring back the window, and the tab inside
// it, where codeaf is running. iTerm2 speaks OSC 9; Ghostty, kitty, WezTerm and
// foot speak OSC 777 (notify.go). Every other terminal — Apple's own among them
// — shows nothing for either, so there the banner goes through `osascript` on
// macOS and through `notify-send` on Linux, and only a machine with neither
// falls back to the escape.
//
// WHY NOT A BANNER OF codeaf's OWN, WITH ITS LOGO. macOS 26 grants notification
// permission only to an application signed with an Apple Developer ID. An
// AppleScript applet or a bundle signed on this machine is refused silently —
// no prompt, no entry in the Notifications settings, no banner (tried in the
// 2026-10-14 wave, both ways) — and `osascript` itself posts as Script Editor,
// whose banner opens Script Editor when it is clicked. A banner of our own
// waits on a signed helper; until then the terminal's banner is the better one.
//
// IT RUNS IN A COMMAND AND NEVER ON THE LOOP: starting a process is a door, and
// the update loop opens none (framedisk_law_test.go).

import (
	"context"
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// NotifyRoute is how this window's desktop notifications reach the screen,
// decided once by the door from the terminal it was started in
// ([DetectNotifyRoute]). The zero value is the operating system's own notifier
// with nothing kept on disk, which is what a test or a headless surface gets.
type NotifyRoute struct {
	// Escape is the terminal's own notification sequence — [notifyOSC9] or
	// [notifyOSC777] — and "" sends banners through the operating system.
	Escape string
	// Via is the application a person finds the banners under in their
	// notification settings: the terminal's name, or the notifier's.
	Via string
	// Dir is the folder this machine keeps the notifier's own files in — the
	// logo `notify-send` shows, and the mark that the one-time line about
	// notification settings has been said. Empty keeps nothing, and the line
	// is then said once per window instead of once per machine.
	Dir string
}

// The two escapes a terminal may speak for a banner.
const (
	// notifyOSC9 is iTerm2's: one string, which the banner shows as its body.
	notifyOSC9 = "osc9"
	// notifyOSC777 is rxvt's, spoken by Ghostty, kitty, WezTerm and foot: a
	// title and a body (notify.go's [notifySeq]).
	notifyOSC777 = "osc777"
)

// The notifiers a banner may come from when the terminal speaks no escape, by
// the name a person finds them under in their settings.
const (
	notifyViaScriptEditor = "Script Editor"
	notifyViaDesktop      = "your desktop's notifications"
)

// DetectNotifyRoute reads the terminal a window was started in. getenv is the
// process environment and goos the operating system, both handed in so the
// reading is a function of its arguments.
//
// INSIDE tmux THE ESCAPE IS NOT SENT. tmux swallows an OSC it does not know
// unless the person turned on passthrough, so a banner sent that way would be
// lost without a word; the operating system's notifier is the honest road.
func DetectNotifyRoute(getenv func(string) string, goos, dir string) NotifyRoute {
	route := NotifyRoute{Dir: dir}
	program := strings.TrimSpace(getenv("TERM_PROGRAM"))
	term := strings.TrimSpace(getenv("TERM"))
	inTmux := strings.TrimSpace(getenv("TMUX")) != "" || program == "tmux" || strings.HasPrefix(term, "tmux") || strings.HasPrefix(term, "screen")
	if !inTmux {
		switch {
		case program == "iTerm.app" || strings.TrimSpace(getenv("LC_TERMINAL")) == "iTerm2":
			route.Escape, route.Via = notifyOSC9, "iTerm2"
		case program == "ghostty" || term == "xterm-ghostty":
			route.Escape, route.Via = notifyOSC777, "Ghostty"
		case term == "xterm-kitty" || strings.TrimSpace(getenv("KITTY_WINDOW_ID")) != "":
			route.Escape, route.Via = notifyOSC777, "kitty"
		case program == "WezTerm":
			route.Escape, route.Via = notifyOSC777, "WezTerm"
		case strings.HasPrefix(term, "foot"):
			route.Escape, route.Via = notifyOSC777, "foot"
		}
	}
	if route.Escape == "" {
		switch goos {
		case "darwin":
			route.Via = notifyViaScriptEditor
		case "linux":
			route.Via = notifyViaDesktop
		}
	}
	return route
}

// escapeFor is the sequence that raises one banner through the terminal.
func (r NotifyRoute) escapeFor(title, body string) string {
	if r.Escape == notifyOSC9 {
		// ONE STRING, so the title leads it: iTerm2 heads every banner with the
		// session's own name, and "codeaf · leave" is what says which program
		// in that session spoke.
		return "\x1b]9;" + oscSafe(title+" — "+body) + "\a"
	}
	return notifySeq(title, body)
}

// automationNotifyMsg is a banner a command raised, for the update loop to
// finish: escape asks it to send the banner through the terminal — the route's
// own sequence, or OSC 777 when fallback says the native notifier failed — and
// told names where banners come from when this was the first one
// ([firstBanner]).
type automationNotifyMsg struct {
	title, body      string
	escape, fallback bool
	told             string
}

// osNotify is the native notifier this window raises banners through. It is a
// variable for one reason: a test raises a run's news and must not put a real
// banner on the screen of whoever is running the suite.
var osNotify = desktopNotify

// notifySound is the sound a banner raised through `osascript` plays, one of
// the system's own. macOS plays it only while the person's settings let
// notifications make a sound, so it asks nothing of anybody who turned that off.
const notifySound = "Glass"

// desktopNotify raises one banner through the operating system and reports
// whether it could; false means the caller should fall back to the terminal.
// dir is [NotifyRoute.Dir], where the logo `notify-send` shows is kept.
func desktopNotify(title, body, dir string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	switch runtime.GOOS {
	case "darwin":
		// THE WORDS TRAVEL AS ARGUMENTS, never inside the script, so nothing a
		// title or a run's line says can be read as AppleScript.
		script := []string{
			"-e", "on run argv",
			"-e", "display notification (item 2 of argv) with title (item 1 of argv) sound name (item 3 of argv)",
			"-e", "end run",
			title, body, notifySound,
		}
		return exec.CommandContext(ctx, "osascript", script...).Run() == nil
	case "linux":
		if _, err := exec.LookPath("notify-send"); err != nil {
			return false
		}
		args := []string{"--app-name=codeaf", "--hint=string:sound-name:message-new-instant"}
		if icon := notifyIcon(dir); icon != "" {
			args = append(args, "--icon="+icon)
		}
		args = append(args, title, body)
		return exec.CommandContext(ctx, "notify-send", args...).Run() == nil
	}
	return false
}

// notifyLogo is codeaf's mark — the prompt pointing at a dot — as the banner of
// a Linux desktop shows it.
//
//go:embed assets/codeaf-notify.png
var notifyLogo []byte

// notifyIcon is the logo's path on disk, written the first time a banner asks
// for it, or "" when there is nowhere to keep it.
func notifyIcon(dir string) string {
	if strings.TrimSpace(dir) == "" || len(notifyLogo) == 0 {
		return ""
	}
	path := filepath.Join(dir, "codeaf.png")
	if info, err := os.Stat(path); err == nil && info.Size() == int64(len(notifyLogo)) {
		return path
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	if err := os.WriteFile(path, notifyLogo, 0o600); err != nil {
		return ""
	}
	return path
}

// notifyTellsSettings is whether the first banner is followed by the line
// saying where banners are turned on. It is macOS's: that is where a new sender
// is quietly kept off until the person allows it, and where the address
// [notifySettingsURL] opens. A variable so a test on any machine can read it.
var notifyTellsSettings = runtime.GOOS == "darwin"

// notifySettingsURL opens the Notifications pane of macOS's System Settings,
// where a person allows banners from the application [NotifyRoute.Via] names.
const notifySettingsURL = "x-apple.systempreferences:com.apple.Notifications-Settings.extension"

// notifyToldName is the mark that the one-time line about notification
// settings has been said on this machine.
const notifyToldName = "told"

// firstBanner reports whether this is the first banner raised on this machine —
// or, with no folder to keep the mark in, in this window — and marks it raised.
// It runs inside the banner's own command, never on the loop: it reads and
// writes a file.
func firstBanner(dir string, here *atomic.Bool) bool {
	if here.Swap(true) {
		return false
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return true
	}
	mark := filepath.Join(dir, notifyToldName)
	if _, err := os.Stat(mark); err == nil {
		return false
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	return os.WriteFile(mark, []byte(time.Now().Format(time.RFC3339)+"\n"), 0o600) == nil
}

// notifyFirstLine is that one line.
func notifyFirstLine(via string) string {
	return "desktop notifications come from " + via + " · not seeing them? allow " + via + " in notification settings"
}
