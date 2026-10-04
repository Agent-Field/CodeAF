package chatlist

import (
	"fmt"
	"strings"
	"time"
)

// The frozen sentences (STAGE-1-CONTRACTS §8.1). Every surface takes its words
// from here so a respelling happens once.
const (
	OfferContinue = "continue here"
	// MoveHere is the verb for a chat another device is running right now, and
	// ContinueVerb for one it let go of; the devices row offers them by name.
	MoveHere     = "Move here"
	ContinueVerb = "Continue here"
	LostRace     = "another device continued this chat first"
	NoIdentity   = "this machine has no identity yet: codeaf identity import"
	SyncOff      = "sync is off: set CODEAF_SYNC_URL to your sync address"
	Unreachable  = "other machines unreachable"
	ClockOff     = "this computer's clock is off by more than 5 minutes"
	// What the relay's refusals say (one sentence each, in the order the
	// cellsync table lists them). None names a code or a number of requests:
	// each says what is true for the person and what, if anything, to do.
	relayFull      = "your sync space is full%s, so new turns stay on this computer; free space there and reopen this chat"
	Removed        = "this device was removed by another of your devices, so this chat stays here only — run `codeaf pair` to bring it back"
	SlowDown       = "sync is asking this computer to slow down; new turns stay here and go up as soon as it allows"
	tooManyNewIn   = "too many new identities from this network today — try again in about %s"
	TooManyNew     = "this network has started too many new identities today; sync begins when it allows more"
	ReplacedGone   = "your identity was replaced and sync has deleted the old one; pair this computer again (/pair on a computer that has the new one)"
	Replaced       = "your chats are moving to a new identity; when that is done, pair this computer again (/pair on the computer that moved them)"
	runningOn      = "running on %s"
	deviceOff      = "%s offline"
	branchLine     = "%s from %s: merge / discard"
	branchKeep     = "%s from %s: discard"
	branchShort    = "%s · %s"
	takeoverLine   = "last saved turn %s ago"
	takeoverMore   = "; up to %s may still be on %s"
	keptEdits      = "your unsaved edits here were kept as %s from %s"
	supersededLine = "%s continued this chat; this window now only shows it"
	copiesCame     = "working copies of tasks came along: %s"
	copyCame       = "the working copy of a task came along: %s"
	setupAsk       = "Set this machine up like %s had it?"
	setupAskAnon   = "Set this machine up like it was?"
	arrivedHead    = "Moved from %s. Here is where it stands."
	arrivedAnon    = "Moved here. Here is where it stands."
	notBrought     = "not brought along: %s"
	wasRunning     = "was running there: %s"
	alsoNeeded     = "also needed: %s"
	uncommitted    = "not committed yet: %s"
	lastTests      = "last tests: %s"
	testsPassed    = "passed"
	testsFailed    = "failed"
	movedFrom      = "Moved from %s in %s. Everything as you left it."
	movedAnon      = "Moved here in %s. Everything as you left it."
	movedRestart   = " What was running there can start again here."
	moreNames      = "%s and %d more"
	// OfferSetUp and OfferNotNow are the two answers of the card a takeover raises,
	// and SetupLater is what the second one says, so a person who answered it
	// knows the door is still open.
	OfferSetUp  = "set up"
	OfferNotNow = "not now"
	OfferGotIt  = "got it"
	SetupLater  = "/setup does this later"
)

// namesShown is how many names a line of the setup card spells before it says
// how many more there are.
const namesShown = 3

// SetupFacts are the three lists a takeover can have something to say about, as
// lines a person reads: folders the copy did not bring, commands that were
// running there, and what this machine also needs.
type SetupFacts struct {
	Missing, Running, Needed, Changed []string
	// Tests is the last test run's line, from [TestsLine], "" when none was recorded.
	Tests string
}

// SetupHead is the question of the card a takeover raises. A device whose name
// is unknown is left out with the word before it, so the sentence still reads.
func SetupHead(device string) string {
	if device == "" {
		return setupAskAnon
	}
	return fmt.Sprintf(setupAsk, device)
}

// ArrivedHead is the heading of the card a takeover raises when there is
// nothing to set up and only where the chat stands to say.
func ArrivedHead(device string) string {
	if device == "" {
		return arrivedAnon
	}
	return fmt.Sprintf(arrivedHead, device)
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
		{notBrought, f.Missing}, {wasRunning, f.Running}, {alsoNeeded, f.Needed}, {uncommitted, f.Changed},
	} {
		if len(line.names) > 0 {
			out = append(out, fmt.Sprintf(line.label, nameList(line.names)))
		}
	}
	if f.Tests != "" {
		out = append(out, f.Tests)
	}
	return out
}

// TestsLine is the line for the last recorded test run: it passed, or it failed
// and, when the run said how many, how many.
func TestsLine(passed bool, failed int) string {
	switch {
	case passed:
		return fmt.Sprintf(lastTests, testsPassed)
	case failed > 0:
		return fmt.Sprintf(lastTests, fmt.Sprintf("%s %d", testsFailed, failed))
	}
	return fmt.Sprintf(lastTests, testsFailed)
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
// on the other machine is left out when none are. A chat that is running now
// says where first, because continuing it here stops it there.
func TakeoverLine(r Row) string {
	line := fmt.Sprintf(takeoverLine, agoWord(r.DurableAgo))
	if r.Pending > 0 {
		line += fmt.Sprintf(takeoverMore, turns(r.Pending), r.Device)
	}
	return holderLead(r) + line
}

// agoWord spells an age in its largest whole unit, so an hour reads `1h` and
// not `3600s`: seconds under a minute, then minutes, hours and days.
func agoWord(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int64(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int64(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int64(d/time.Hour))
	}
	return fmt.Sprintf("%dd", int64(d/(24*time.Hour)))
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

// RelayFull is said when the relay has no room left. It names the ceiling the
// relay told us, and says nothing of one when the relay named none (the
// emptiness law): a relay run on its own disk has no number to give.
func RelayFull(limitBytes int64) string {
	size := ""
	if limitBytes > 0 {
		size = " (" + bytesSize(limitBytes) + ")"
	}
	return fmt.Sprintf(relayFull, size)
}

// TooManyNewIn is said when the relay turned a network's new identity away and
// named when its day frees up. It rounds the wait up, so a person who comes back
// at the time it says is never turned away again; a relay that named no wait
// gets the plain TooManyNew.
func TooManyNewIn(wait time.Duration) string {
	if wait <= 0 {
		return TooManyNew
	}
	return fmt.Sprintf(tooManyNewIn, waitWords(wait))
}

// waitWords spells a wait in the unit a person plans by: a minute or less, whole
// minutes below an hour (a wait that rounds to 60 of them is an hour), and whole hours after, each rounded up.
func waitWords(wait time.Duration) string {
	switch {
	case wait <= time.Minute:
		return "a minute"
	case wait < time.Hour-time.Minute:
		return fmt.Sprintf("%d minutes", ceilUnits(wait, time.Minute))
	}
	if hours := ceilUnits(wait, time.Hour); hours > 1 {
		return fmt.Sprintf("%d hours", hours)
	}
	return "an hour"
}

// ceilUnits is wait in whole units of unit, rounded up.
func ceilUnits(wait, unit time.Duration) int { return int((wait + unit - 1) / unit) }

// bytesSize spells a byte count in the largest binary unit it reaches, without
// trailing zeros: 5 GiB, 1.5 GiB, 512 MiB.
func bytesSize(n int64) string {
	const mib, gib = 1 << 20, 1 << 30
	if n >= gib {
		return fmt.Sprintf("%g GiB", float64(n)/gib)
	}
	return fmt.Sprintf("%g MiB", float64(n)/mib)
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

// Moved is the line every takeover says, with the time it measured. It says
// where the chat came from when that is known, and that what was running there
// can start again only when something was.
func Moved(from string, elapsed time.Duration, hadRunning bool) string {
	took := elapsed.Round(100 * time.Millisecond).String()
	line := fmt.Sprintf(movedAnon, took)
	if from != "" {
		line = fmt.Sprintf(movedFrom, from, took)
	}
	if hadRunning {
		line += movedRestart
	}
	return line
}
