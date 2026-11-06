package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// Desktop pairing copy belongs here: terminal prompts contain commands and
// keyboard answers that do not describe the app's controls. NOTHING HERE PASSES
// A TERMINAL SENTENCE THROUGH: the app cannot run a command or pass a flag.
const (
	appPairCancelled = "Cancelled. Nothing was paired."
	appPairDeclined  = "Pairing was declined. Nothing was paired."
	appPairBurned    = "Someone entered a wrong code. Here is a new code."
	// appPairJoined is the joining computer's receipt. The app restarts the
	// engine on its own after a join, so there is nothing to run.
	appPairJoined     = "Paired with your other computer. Your chats can continue on both computers."
	appPairAlready    = pair.AlreadyPairedLine
	appPairFollowed   = "Paired. This computer now shares your current chats and keeps its saved keys."
	appPairListUnread = "Paired. Your computers could not be listed yet. Open codeaf again in a moment."
	appPairFailed     = "This computer could not finish pairing. Try again."
	// appRemoved answers any call refused because this computer was removed,
	// so it names no chat; the app's own remedy is Pair a computer.
	appRemoved = "This computer was removed by another of your devices. To bring it back, choose Pair a computer."
)

func pairApprovalOptions() []appwire.PairOption {
	return []appwire.PairOption{{ID: "y", Label: "Pair"}, {ID: "n", Label: "Don't pair"}}
}

func appPairCodeLines(code string) []string {
	return []string{"Pairing shares your chats with another computer.", fmt.Sprintf("On your other computer, choose Pair a computer, then Enter a code from the other computer, and type %s (dashes optional). This code lasts %d minutes.", code, int(pair.CodeValidFor/time.Minute))}
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

// appPairError is a failed pairing as the app shows it: one sentence, and the
// code that says whether a new code or link could help. Refusals of what this
// computer already holds cannot be helped by one, so they are cannot_join.
func appPairError(err error) *appwire.PairError {
	code := appwire.CodeEngine
	var paired pairedWithError
	if errors.As(err, &paired) || errors.Is(err, pair.ErrDifferentChats) {
		code = appwire.CodeCannotJoin
	}
	return &appwire.PairError{Code: code, Message: appPairFailure(err)}
}

// appPairPairedWith refuses a computer whose chats already sync with others.
// Joining would set those chats aside, so the app offers no way to replace them.
func appPairPairedWith(paired pairedWithError) string {
	return "This computer is already paired with " + paired.list("another computer") + "."
}

// appPairOwnChats refuses a computer with chats of its own, counted where the
// engine counted them. The app has no way to replace them, so it names none.
func appPairOwnChats(n int) string {
	held := fmt.Sprintf("%d chats", n)
	switch n {
	case 0:
		held = "local work"
	case 1:
		held = "1 chat"
	}
	return "This computer has " + held + " of its own, so it can't join another computer's chats. Nothing was changed."
}

// appPairNotConfirmed is the approving computer's sentence when the joining one
// never said it kept what it was sent. It may have refused, stopped, or joined
// with its word lost, so the sentence sends a person there to see which, and
// never says nothing was shared: the grant had already gone out.
func appPairNotConfirmed(label string) string {
	return "Pairing was not confirmed by " + label + ". Check that computer to see whether it joined."
}

func appPairFailure(err error) string {
	var paired pairedWithError
	label, unconfirmed := pair.UnconfirmedBy(err)
	switch {
	case errors.As(err, &paired):
		return appPairPairedWith(paired)
	case errors.Is(err, pair.ErrDifferentChats):
		return appPairOwnChats(pair.OwnChats(err))
	case unconfirmed:
		return appPairNotConfirmed(label)
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
