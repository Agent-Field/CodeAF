package pair

import (
	"fmt"
	"strings"
	"time"
)

// The sentences of a pairing that are not errors, written once so that the
// terminal and the chat say the same thing in the same words. A person learns
// one pairing: the code shape, the three words and the y or n are the same on
// every screen that shows them.

// AskChoice is what a person is offered under a question: no default, so a
// stray Enter never lets a device in.
const AskChoice = "y / n"

// AskChatsLine is the question the device that shares its chats puts to a person.
func AskChatsLine(label, words string) string {
	return `"` + label + `" wants your chats. Same three words on that screen: ` + words + `?`
}

// AskMachineLine is the question a machine puts to a person before a device may
// use it.
func AskMachineLine(label, words string) string {
	return `"` + label + `" wants to use this machine. Same three words on that screen: ` + words + `?`
}

// BurnLine is what the device that shows a code says when a wrong one was typed.
// The new code follows it without a keypress.
const BurnLine = "someone typed a wrong code. New code:"

// WaitingChatsLine is what the joining device shows while a person decides on
// the other one: the words that screen should be showing.
func WaitingChatsLine(words string) string {
	return "waiting for approval on your other device; it should show: " + words
}

// JoinedLine is what the joining device says when it worked.
const JoinedLine = "paired. this computer now has your chats."

// FollowedLine is what the joining device says when it was on an identity that
// was rotated and has moved to the one that replaced it.
const FollowedLine = "paired. this computer now follows your new identity and keeps its chats and its saved keys."

// Sentence is how a finished join reads: a computer that held these chats
// already is told nothing changed, one that followed a rotation says so, and any
// other is told it has the chats now.
func (j Joined) Sentence() string {
	switch {
	case j.Already:
		return ErrAlreadyPaired.Error()
	case j.Successor:
		return FollowedLine
	}
	return JoinedLine
}

// PairedChatsLine is what the sharing device says when it worked.
func PairedChatsLine(label string) string { return "paired: " + label }

// ── joining by link ──────────────────────────────────────────────────────────

// Invite is what the new device shows while it waits.
type Invite struct {
	Ref       LinkRef
	Check     string
	ExpiresIn time.Duration
	// Via is the sync address the approving device must use when it is not the
	// default one; empty otherwise.
	Via string
}

// InviteLines is what the new device prints: the link, the typed form, the
// check number to compare, and how long it is good for.
func InviteLines(in Invite) string {
	var b strings.Builder
	b.WriteString("Approve this device from one you already use. Open this link there:\n")
	fmt.Fprintf(&b, "  %s\n\n", in.Ref.URL())
	b.WriteString("Or, on a computer with codeaf, run:\n")
	fmt.Fprintf(&b, "  %s\n\n", ApproveCommand(in.Ref, in.Via))
	fmt.Fprintf(&b, "Check number: %s (the other device shows the same number)\n", in.Check)
	fmt.Fprintf(&b, "Waiting for approval; good for %d minutes. Press ctrl+c to cancel.", int(in.ExpiresIn/time.Minute))
	return b.String()
}

// ApproveCommand is the typed form of an approval.
func ApproveCommand(ref LinkRef, via string) string {
	cmd := "codeaf pair approve " + ref.Token()
	if via != "" {
		cmd += " --via " + via
	}
	return cmd
}

// JoinedFleetLine is what the new device says when it is in.
func JoinedFleetLine(workspaces int) string {
	if workspaces == 1 {
		return "Paired - 1 workspace available."
	}
	return fmt.Sprintf("Paired - %d workspaces available.", workspaces)
}

// WantsToJoinLine tells the approving person which device is asking.
func WantsToJoinLine(name, platform string) string {
	return fmt.Sprintf("%q (%s) wants to join your devices.", name, platform)
}

// CheckQuestion is what the approving person is asked: the number must match
// the one the new device shows.
func CheckQuestion(check string) string {
	return "Does that device show the check number " + check + "?"
}

// ApprovedLine is what the approving device says when it let a device in.
func ApprovedLine(name string) string { return name + " joined your devices." }

// DeclinedLine is what the approving device says after a no.
const DeclinedLine = "Request declined."
