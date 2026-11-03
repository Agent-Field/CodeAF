package tui3

import (
	"fmt"
	"runtime"
)

// ── CHATS THIS COMPUTER HAS NOT SHARED YET ──────────────────────────────────
//
//	● This Mac  ● laptop
//	12 chats on this Mac aren't shared yet
//
// Another computer lists only the chats this one has uploaded. Until a chat is
// uploaded it is missing there, and a short list there reads as lost chats.
// The devices row on home and the /devices card say how many are still here
// only, in plain words, so the person knows the rest is coming or why it is
// not. Nothing is said when every chat is shared or the count is not known.

// NotShared counts this computer's chats the other computers cannot list yet.
type NotShared struct {
	// Waiting are still to be uploaded; they need nothing from the person.
	Waiting int
	// Failing failed their last upload; codeaf tries again.
	Failing int
	// KeptHere are older chats kept on this computer until they are opened
	// here: last used over 30 days ago, or working in the home folder.
	KeptHere int
}

// Total is every chat the other computers cannot list yet.
func (n NotShared) Total() int { return n.Waiting + n.Failing + n.KeptHere }

// thisComputer is how the sentence names the computer it is drawn on.
func thisComputer() string {
	if runtime.GOOS == "darwin" {
		return "this Mac"
	}
	return "this computer"
}

// readNotShared is the count, and false when there is nothing to say.
func readNotShared(read func() (NotShared, bool)) (NotShared, bool) {
	if read == nil {
		return NotShared{}, false
	}
	n, ok := read()
	return n, ok && n.Total() > 0
}

// notSharedLine is the devices row's sentence: how many, and nothing more.
func notSharedLine(read func() (NotShared, bool)) string {
	n, ok := readNotShared(read)
	if !ok {
		return ""
	}
	return notSharedHead(n.Total())
}

func notSharedHead(total int) string {
	if total == 1 {
		return fmt.Sprintf("1 chat on %s isn't shared yet", thisComputer())
	}
	return fmt.Sprintf("%d chats on %s aren't shared yet", total, thisComputer())
}

// notSharedDetail is the /devices card's sentence: how many, and why the ones
// that need something are still here.
func notSharedDetail(read func() (NotShared, bool)) string {
	n, ok := readNotShared(read)
	if !ok {
		return ""
	}
	line := notSharedHead(n.Total()) + "."
	if n.Waiting > 0 && n.Waiting < n.Total() {
		line += fmt.Sprintf(" %d are still uploading.", n.Waiting)
	} else if n.Waiting > 0 {
		line += " They are still uploading."
	}
	if n.Failing > 0 {
		line += fmt.Sprintf(" %d could not be uploaded yet; codeaf keeps trying.", n.Failing)
	}
	if n.KeptHere > 0 {
		line += fmt.Sprintf(" %d older ones stay here until you open them on %s.", n.KeptHere, thisComputer())
	}
	return line
}
