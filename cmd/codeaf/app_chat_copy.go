package main

import (
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

const appTakeFailed = "This chat could not be moved to this computer."

// A live lease means this chat is open there; it says nothing about a turn.
func appChatStatusLine(r chatlist.Row) string {
	if r.Status == chatlist.Running {
		return fmt.Sprintf("open on %s", r.Device)
	}
	return chatlist.StatusLine(r)
}

// Absolute save times keep unrelated directory bumps from changing every
// row's relative-age sentence. SavedAt carries the same stable fact on the wire.
func appChatTakeoverLine(r chatlist.Row) string {
	line := "Last saved " + time.UnixMilli(r.DurableAt).UTC().Format("2 Jan 2006 at 15:04:05 MST") + "."
	if r.Pending > 0 {
		line += fmt.Sprintf(" Some work may still be on %s.", r.Device)
	}
	if status := appChatStatusLine(r); status != "" {
		line = status + "; " + line
	}
	return line
}
