package main

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

const appTakeFailed = "This chat could not be moved to this computer."

// A live lease means this chat is open there; it says nothing about a turn.
func appChatStatusLine(r chatlist.Row, online *bool) string {
	if online != nil && !*online && r.Device != "" {
		return fmt.Sprintf("%s offline", r.Device)
	}
	if r.Status == chatlist.Running {
		return fmt.Sprintf("open on %s", r.Device)
	}
	if r.Status == chatlist.Off {
		return ""
	}
	return chatlist.StatusLine(r)
}

// The desktop formats SavedAt locally. Only holder and pending-work facts add
// anything to the takeover sentence, and neither changes as the clock advances.
func appChatTakeoverLine(r chatlist.Row, online *bool) string {
	parts := []string{}
	if status := appChatStatusLine(r, online); status != "" {
		parts = append(parts, status)
	}
	if r.Pending > 0 {
		parts = append(parts, fmt.Sprintf("Some work may still be on %s.", r.Device))
	}
	return strings.Join(parts, "; ")
}
