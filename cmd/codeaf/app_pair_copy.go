package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// Desktop pairing copy belongs here: terminal prompts contain commands and
// keyboard answers that do not describe the app's controls.
const (
	appPairReplaceTitle  = "Replace this computer's chats?"
	appPairReplaceDetail = "This computer already has its own chats. Pairing replaces them with the chats from your other computer."
	appPairCancelled     = "Cancelled. Nothing was paired."
	appPairDeclined      = "Pairing was declined. Nothing was paired."
	appPairBurned        = "Someone entered a wrong code. Here is a new code."
	appPairJoined        = "Paired. This computer now shares your chats."
	appPairAlready       = "This computer is already paired with your chats."
	appPairFollowed      = "Paired. This computer now shares your current chats and keeps its saved keys."
	appPairRecordUnsaid  = "Paired. This computer's name could not be shared yet. It will be shared when a chat next connects."
	appPairListUnread    = "Paired. Your computers could not be listed yet. Open codeaf again in a moment."
	appPairFailed        = "This computer could not finish pairing. Try again."
)

func pairApprovalOptions() []appwire.PairOption {
	return []appwire.PairOption{{ID: "y", Label: "Pair"}, {ID: "n", Label: "Don't pair"}}
}

func appPairReplaceOptions() []appwire.PairOption {
	return []appwire.PairOption{{ID: "replace", Label: "Replace this computer's chats"}, {ID: "cancel", Label: "Not now"}}
}
func appPairCodeLines(code string) []string {
	return []string{"Pairing shares your chats with another computer.", fmt.Sprintf("On your other computer, choose Join another computer and enter %s. This code lasts %d minutes.", code, int(pair.CodeValidFor/time.Minute))}
}
func appPairApproveTitle(name string) string { return fmt.Sprintf("Pair with %s?", name) }
func appPairWords(words string) string {
	return "Pair only if your other computer shows the same three words: " + words + "."
}
func appPairWaiting(words string) string {
	return "Waiting for approval on your other computer. It should show: " + words + "."
}
func appPairPaired(name string) string {
	return fmt.Sprintf("Paired with %s. Your chats can continue on both computers.", name)
}
func appPairJoinedLine(j pair.Joined) string {
	switch {
	case j.Already:
		return appPairAlready
	case j.Successor:
		return appPairFollowed
	default:
		return appPairJoined
	}
}
func appPairInviteLines(check string, expires time.Duration) []string {
	return []string{"Open this pairing link on a computer you already use.", "Compare the check number on both computers: " + check + ".", fmt.Sprintf("Waiting for approval. This link lasts %d minutes.", int(expires/time.Minute))}
}
func appPairCheck(check string) string {
	return "Pair only if your other computer shows the same check number: " + check + "."
}
func appPairFailure(err error) string {
	switch {
	case errors.Is(err, pair.ErrCodeDidNotWork):
		return "That code did not work, or it timed out. Ask for a new one."
	case errors.Is(err, pair.ErrCodeShape):
		return "Enter the pairing code shown on your other computer."
	case errors.Is(err, pair.ErrSyncOff):
		return appSyncOff
	case errors.Is(err, pair.ErrSomeoneElse):
		return "Someone else used that code. Ask for a new one."
	case errors.Is(err, pair.ErrStopped):
		return "Your other computer stopped pairing. Start pairing there again."
	default:
		return appPairFailed
	}
}
