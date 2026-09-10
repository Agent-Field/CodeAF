package session

// newskey.go is WHOSE NEWS THIS IS, AND THE DOOR NEWS COMES BACK IN THROUGH.
//
// THE DEFECT THIS FIXES. Bare `aforge` in a workspace does not run this engine
// in the surface's own process: the surface dials the machine's engine host
// (internal/enginehost) and talks to a `remote.Agent` over a pipe. Everything
// [OnPhaseNews] and [OnLaneNews] carry — the live rate at the right edge of the
// status row, the `via <machine>` rider on the seam, `connecting · 1.2s`,
// `first word …`, `thinking`, `writing`, and the `served` row `/status` prints
// — is posted in the ENGINE'S process and read in the SURFACE'S, and until the
// wire carried it none of it ever arrived. The surface's code for all four was
// right the whole time; there was no road.
//
// TWO THINGS ARE NEEDED TO BUILD THAT ROAD, AND THIS FILE IS BOTH OF THEM.
//
// The first is an IDENTITY. Both readers are process-global — one desk for the
// whole build, because a surface that draws a status line does not hold the
// agent that ran the errand behind it (lanenews.go says why). One process with
// one window needs nothing more; a HOST runs many conversations down many
// connections, registers one reader for all of them, and would fan every
// window's clock out to every other window without a name on each piece of news
// to sort by. [Agent.newsKey] is that name.
//
// The second is a WAY BACK IN. [TellPhase] and [TellLane] are the doors the
// surface's side of the connection hands a decoded frame to, so that a relayed
// piece of news runs the same forwarding an in-process one runs and reaches the
// same registered reader. Nothing else may call them: they are for a transport
// that took the news off a wire.

import "strings"

// newsKey names the conversation a piece of news belongs to.
//
// IT IS THE ROOT CONVERSATION AND NOT THIS AGENT, which is the whole of why it
// is a function rather than a field read. A conversation runs a talk turn, a
// naming errand, and — under it — a tree of task nodes, each of which is an
// agent of its own with its own journal. All of them are one window's work, and
// a node whose phase was filed under its own journal would be news the host
// could not place on the connection the person is sitting at. `rootSession` is
// the answer that already travels down every node's Config for the ledger's
// sake (session.go), so this reads it rather than minting a second lineage that
// could disagree with the one the money is filed under.
//
// A conversation itself carries no root — its own journal names it — so it
// falls back to [Agent.threadID], which is the journal header's id when there
// is a file and the minted lineage id when there is not.
func (a *Agent) newsKey() string {
	if a == nil {
		return ""
	}
	if root := strings.TrimSpace(a.config.rootSession); root != "" {
		return root
	}
	return a.threadID()
}

// NewsKey is [Agent.newsKey] for the transport that has to file a connection
// under it (internal/remote's news.go). It is the only reason the identity is
// exported, and it is deliberately not on any interface: an engine door built
// around something other than this agent simply has no key, and a host that
// cannot name a conversation fans nothing out to it rather than fanning
// everything out to everybody.
func (a *Agent) NewsKey() string { return a.newsKey() }

// TellPhase hands a phase that was measured on ANOTHER MACHINE to this
// process's registered reader, exactly as if it had been measured here.
//
// IT IS FOR ONE CALLER: the surface's side of a connection to an engine host
// (internal/remote), which decodes a "phase" frame and calls this. It runs
// [forwardPhase] — the same forwarding the in-process road runs — so a relayed
// phase lands on the same desk, wakes the same repaint, and is aged out by the
// same window as one measured here.
//
// IT CANNOT DOUBLE-POST. A build where the engine and the surface share a
// process has no connection between them, so nothing ever calls this; a build
// where they do not share a process has two readers in two processes and each
// posts once. The [PhaseNews.Relayed] stamp the caller sets is what keeps a
// build that is BOTH — a test driving a host inside its own process — from
// sending back out what it just took in.
func TellPhase(news PhaseNews) {
	if news.Model == "" && news.Phase == "" {
		return
	}
	forwardPhase(news)
}

// TellLane is [TellPhase]'s twin for the lane sighting: which machine answered,
// how fast it wrote, and whether a rescue went out while somebody was waiting.
// A news with no model belongs to nobody and [postLaneNews] drops it, which is
// the same law this side of the wire as the other.
func TellLane(news LaneNews) { postLaneNews(news) }
