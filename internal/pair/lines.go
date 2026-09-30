package pair

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
