package remote

// lanes.go is this protocol's answer to "which capabilities can a conversation
// held over a connection actually have", said once so the doors that BUILD a
// conversation do not each keep a copy of it (cmd/aforge's chatv3_lanes.go).
//
// A CAPABILITY IS NOT A CHANNEL. Carrying a lane's events is the easy half; a
// card is only real when its ANSWER has a door, when a card raised while nobody
// is attached survives the gap, and when every verb the page behind it needs
// crosses too. Anything less puts a question on a screen that cannot be closed,
// which is worse than the capability being absent.

// Lanes says which standing capabilities this build carries end to end.
type Lanes struct {
	// Designs is the harness lane (standinglane.go): the design card, the notes
	// around it, and the subharness intake card that shares the subscription. It
	// is complete — the events cross, ResolveHarness has answered the design
	// card since version 1, and internal/session replays a card that is still
	// standing onto every new subscription, so a detached window comes back to
	// it.
	Designs bool
	// Cards is the intake card's own answer door, [MethodSubharnessResolve]. It
	// is a separate bit because the two cards share a subscription and not an
	// answer.
	Cards bool
	// Runs is the adaptive run lane, and it is FALSE in this build. The work
	// that is left is exact:
	//
	//  1. internal/session's WatchOrchestrations replays nothing, unlike
	//     WatchHarnessDesigns. A run's fuel gate raised while every window is
	//     away would be lost on reconnect, and a gate nobody can answer stops
	//     the run in silence at its cap.
	//  2. internal/tui3's orchAgent needs four doors, not one. Snapshot and
	//     SteerOrchestrate would cross as they stand; OrchestrateNodeJournal
	//     answers a PATH ON THE ENGINE'S DISK, which the run page reads locally
	//     (roomorch.go's session.ReadTranscript), so it needs a content-carrying
	//     door and a seam in that package before it can be honest here.
	//
	// Nothing is lost by the wait: no command, tool or cue opens an adaptive run
	// in this build, on this machine or a far one, so a conversation built
	// without the runner is the same conversation either way.
	Runs bool
}

// StandingLanes is what this build carries.
func StandingLanes() Lanes {
	return Lanes{Designs: true, Cards: true, Runs: false}
}
