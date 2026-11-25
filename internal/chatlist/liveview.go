package chatlist

import (
	"fmt"
	"strings"
)

// The words of a live view: a chat that keeps running on another of this
// person's computers and is read, and typed into, from this one. They live in
// one file because two surfaces draw them, the terminal and the desktop app, and
// a sentence spelled twice is a sentence that drifts. Every surface takes its
// words from here; the manual quotes them as they stand.
//
// NONE OF THEM NAMES THE MACHINERY. The person-facing vocabulary of a pairing
// (internal/e2e's pairing journey) has no word for the relay, a lease, a node or
// a manifest, and a live view is part of that journey, so the sentences say what
// is true for the person and what, if anything, to do about it.

// otherComputer stands in for a host whose name is not known yet, so a sentence
// never reads "  lets this computer watch".
const otherComputer = "the other computer"

func hostWord(host string) string {
	if host == "" {
		return otherComputer
	}
	return host
}

const (
	// LiveMoving is what the view says between choosing Move here and the chat
	// arriving. It stays on screen until the move either succeeds or fails.
	LiveMoving = "Moving here…"
	// LiveIdle is said to a view that detached after the quiet spell. The chat is
	// still readable; any key reconnects.
	LiveIdle = "Disconnected after 10 minutes idle. Enter to reconnect."
	// LiveBudget is said when the host's daily allowance of live traffic is used
	// up: sessions stay open and readable, and typing waits for the next day.
	LiveBudget = "watch only until 00:00 UTC (daily limit)"
	// LiveRemoved is said to a computer taken off the person's list. It is the
	// same sentence the move door says, so a person meets one wording.
	LiveRemoved = "this computer was removed from your devices"
)

// LiveWentOffline is the banner of a view whose host stopped answering. The
// view keeps what it last showed and the words typed in the box.
func LiveWentOffline(host string) string { return hostWord(host) + " went offline" }

// LiveOfflineSeen is the composer-position bar of a view whose host is not
// running the chat, with the time the copy on this computer was last updated.
func LiveOfflineSeen(host, seen string) string {
	return fmt.Sprintf("%s offline · last seen %s", hostWord(host), seen)
}

// LiveWatchOnly is said to a computer that may read but not type, both as the
// composer's reason and as the refusal a send gets back.
func LiveWatchOnly(host string) string {
	return fmt.Sprintf("%s lets this computer watch, not type. Change that in /devices there.", hostWord(host))
}

// LiveTypingFrom is what a view without the keyboard says about who has it.
func LiveTypingFrom(holder string) string {
	return fmt.Sprintf("typing from %s now · enter types here", hostWord(holder))
}

// LiveNotAllowedHere is the refusal for anything a computer on the other end of
// a live view may never ask for, whichever of the two permissions it holds.
func LiveNotAllowedHere(host string) string {
	return fmt.Sprintf("%s does not allow that from another computer.", hostWord(host))
}

// LiveNeedsItsOwnOK is what a viewer is told when it tries to answer a question
// that only the computer running the chat may settle.
func LiveNeedsItsOwnOK(host string) string {
	return fmt.Sprintf("That one needs your OK on %s itself. Answer it there.", hostWord(host))
}

// LiveSaid is the sentence for one close reason of a live view. The reasons are
// a closed set, spelled the same on the wire and here; an unknown one is said as
// a plain loss of connection rather than invented around.
func LiveSaid(reason, host string) string {
	name := hostWord(host)
	switch reason {
	case "not-running":
		return name + " is not running this chat right now."
	case "not-allowed":
		return name + " does not share its chats with this computer. Change that in /devices there."
	case "holder-gone":
		return name + " closed this chat."
	case "holder-old":
		return "Restart codeaf on " + name + " to share this chat."
	case "removed":
		return LiveRemoved
	case "permit-changed":
		return name + " changed what this computer may do in its chats."
	case "remote-control-off":
		return name + " turned remote control off."
	case "lagging":
		return "Catching up with " + name + "."
	case "frame-too-large":
		return "One message was too large to show live."
	case "budget":
		return LiveBudget
	case "idle":
		return LiveIdle
	case "host-closing":
		return name + " is closing codeaf."
	case "too-many":
		return name + " has too many computers open on its chats right now. Try again in a minute."
	}
	return "Lost the connection to " + name + "."
}

// LiveRoad is the road row of /status and of the desktop's live strip: how the
// bytes of a live view travel right now. It has exactly two values, and both
// surfaces read them from here so the terminal and the desktop cannot disagree
// about a swap. A road the link has not named yet draws as nothing (the
// emptiness law), and any road that is not the direct one is the relay, the
// word phase 1 already shipped.
//
// "relay" appears here as the name of the server, as it did on the row phase 1
// shipped; no sentence calls the feature relay (V12).
func LiveRoad(road string) string {
	switch strings.TrimSpace(road) {
	case "":
		return ""
	case "direct":
		return "live · direct"
	default:
		return "live · relay"
	}
}
