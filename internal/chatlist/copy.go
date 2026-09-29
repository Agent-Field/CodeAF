package chatlist

import (
	"fmt"
	"time"
)

// The frozen sentences (STAGE-1-CONTRACTS §8.1). Every surface takes its words
// from here so a respelling happens once.
const (
	OfferContinue  = "continue here"
	NoIdentity     = "this machine has no identity yet: codeaf identity import"
	SyncOff        = "sync is off: set CODEAF_SYNC_URL to your relay's address"
	Unreachable    = "other machines unreachable"
	ClockOff       = "this computer's clock is off by more than 5 minutes"
	runningOn      = "running on %s"
	deviceOff      = "%s off"
	branchLine     = "%d turns from %s: merge / discard"
	takeoverLine   = "last durable turn %ds ago"
	takeoverMore   = "; up to %d turns may still be on %s"
	keptEdits      = "your unsaved edits here were kept as %d turns from %s"
	supersededLine = "%s continued this chat; this window now only shows it"
)

// StatusLine is what a list row says beside the title. Idle and Here say
// nothing (the emptiness law), so their line is empty.
func StatusLine(r Row) string {
	switch r.Status {
	case Running:
		return fmt.Sprintf(runningOn, r.Device)
	case Off:
		return fmt.Sprintf(deviceOff, r.Device)
	case Branch:
		return fmt.Sprintf(branchLine, r.OrphanTurns, r.Device)
	}
	return ""
}

// TakeoverLine is the takeover screen's sentence; the clause about turns still
// on the other machine is left out when none are.
func TakeoverLine(r Row) string {
	line := fmt.Sprintf(takeoverLine, int64(r.DurableAgo/time.Second))
	if r.Pending == 0 {
		return line
	}
	return line + fmt.Sprintf(takeoverMore, r.Pending, r.Device)
}

// KeptEdits is said after a takeover kept local edits as a branch.
func KeptEdits(turns uint32, device string) string {
	return fmt.Sprintf(keptEdits, turns, device)
}

// Superseded is said to a driver whose lease another device took over.
func Superseded(device string) string { return fmt.Sprintf(supersededLine, device) }
