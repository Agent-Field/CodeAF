package chatlist

import (
	"fmt"
	"strings"
	"time"
)

// The frozen sentences (STAGE-1-CONTRACTS §8.1). Every surface takes its words
// from here so a respelling happens once.
const (
	OfferContinue  = "continue here"
	LostRace       = "another device continued this chat first"
	NoIdentity     = "this machine has no identity yet: codeaf identity import"
	SyncOff        = "sync is off: set CODEAF_SYNC_URL to your relay's address"
	Unreachable    = "other machines unreachable"
	ClockOff       = "this computer's clock is off by more than 5 minutes"
	runningOn      = "running on %s"
	deviceOff      = "%s off"
	branchLine     = "%s from %s: merge / discard"
	branchKeep     = "%s from %s: discard"
	branchShort    = "%s · %s"
	takeoverLine   = "last durable turn %ds ago"
	takeoverMore   = "; up to %s may still be on %s"
	keptEdits      = "your unsaved edits here were kept as %s from %s"
	supersededLine = "%s continued this chat; this window now only shows it"
	copiesCame     = "working copies of tasks came along: %s"
	copyCame       = "the working copy of a task came along: %s"
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
		return fmt.Sprintf(branchLine, turns(r.OrphanTurns), r.Device)
	}
	return ""
}

// BranchLine is a branch row's sentence for what the surface can do with it.
// Merge is offered only where a surface has a way to merge, so a surface
// without one says the sentence without it rather than promising the verb
// (a capability that cannot work is absent, not broken).
func BranchLine(r Row, merge bool) string {
	if merge {
		return fmt.Sprintf(branchLine, turns(r.OrphanTurns), r.Device)
	}
	return fmt.Sprintf(branchKeep, turns(r.OrphanTurns), r.Device)
}

// BranchShort is the branch row's narrow spelling (ruling 2026-09-29, §8.1):
// the shared row fitter takes it below the width the full sentence needs, so
// the row never loses the fact that it is a branch and where it came from.
func BranchShort(r Row) string { return fmt.Sprintf(branchShort, turns(r.OrphanTurns), r.Device) }

// TakeoverLine is the takeover screen's sentence; the clause about turns still
// on the other machine is left out when none are. A chat that is running now
// says where first, because continuing it here stops it there.
func TakeoverLine(r Row) string {
	line := fmt.Sprintf(takeoverLine, int64(r.DurableAgo/time.Second))
	if r.Pending > 0 {
		line += fmt.Sprintf(takeoverMore, turns(r.Pending), r.Device)
	}
	return holderLead(r) + line
}

// holderLead names the device a live chat runs on, and is empty for a chat
// nobody is running.
func holderLead(r Row) string {
	if r.Status != Running {
		return ""
	}
	return fmt.Sprintf(runningOn+"; ", r.Device)
}

// KeptEdits is said after a takeover kept local edits as a branch.
func KeptEdits(n uint32, device string) string {
	return fmt.Sprintf(keptEdits, turns(n), device)
}

// CopiesCame is said after a takeover brought the working copies of tasks that
// had not landed, named by task. It says nothing when none came (the emptiness
// law), so the caller can join it to other sentences without a test.
func CopiesCame(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf(copyCame, names[0])
	}
	return fmt.Sprintf(copiesCame, strings.Join(names, ", "))
}

// Superseded is said to a driver whose lease another device took over.
func Superseded(device string) string { return fmt.Sprintf(supersededLine, device) }

// turns spells a count of turns, so one reads `1 turn` in every sentence that
// carries a count and the copy is never `1 turns`.
func turns(n uint32) string {
	if n == 1 {
		return "1 turn"
	}
	return fmt.Sprintf("%d turns", n)
}
