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
	setupAsk       = "Set this machine up like %s had it?"
	setupAskAnon   = "Set this machine up like it was?"
	notBrought     = "not brought along: %s"
	wasRunning     = "was running there: %s"
	alsoNeeded     = "also needed: %s"
	moreNames      = "%s and %d more"
	// OfferSetUp and OfferNotNow are the two answers of the card a takeover raises,
	// and SetupLater is what the second one says, so a person who answered it
	// knows the door is still open.
	OfferSetUp  = "set up"
	OfferNotNow = "not now"
	SetupLater  = "/setup does this later"
)

// namesShown is how many names a line of the setup card spells before it says
// how many more there are.
const namesShown = 3

// SetupFacts are the three lists a takeover can have something to say about, as
// lines a person reads: folders the copy did not bring, commands that were
// running there, and what this machine also needs.
type SetupFacts struct{ Missing, Running, Needed []string }

// SetupHead is the question of the card a takeover raises. A device whose name
// is unknown is left out with the word before it, so the sentence still reads.
func SetupHead(device string) string {
	if device == "" {
		return setupAskAnon
	}
	return fmt.Sprintf(setupAsk, device)
}

// SetupReasons are the lines under the question, one for each list that is not
// empty. The person is shown exactly what the agent will be told, because both
// are built from the same facts.
func SetupReasons(f SetupFacts) []string {
	var out []string
	for _, line := range []struct {
		label string
		names []string
	}{
		{notBrought, f.Missing}, {wasRunning, f.Running}, {alsoNeeded, f.Needed},
	} {
		if len(line.names) > 0 {
			out = append(out, fmt.Sprintf(line.label, nameList(line.names)))
		}
	}
	return out
}

// nameList spells the first few names and counts the rest.
func nameList(names []string) string {
	if len(names) <= namesShown {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf(moreNames, strings.Join(names[:namesShown], ", "), len(names)-namesShown)
}

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
// on the other machine is left out when none are.
func TakeoverLine(r Row) string {
	line := fmt.Sprintf(takeoverLine, int64(r.DurableAgo/time.Second))
	if r.Pending == 0 {
		return line
	}
	return line + fmt.Sprintf(takeoverMore, turns(r.Pending), r.Device)
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
