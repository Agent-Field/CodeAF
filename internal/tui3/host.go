package tui3

import (
	"path/filepath"
	"strings"
)

// ── A SESSION ON ANOTHER MACHINE ────────────────────────────────────────────
//
// `aforge chat --host devbox` runs this surface here and the session there. The
// conversation is identical — every method of [Agent] travels over the wire
// (internal/remote) and answers the same way — so almost nothing in this package
// needs to know. Two things do, and they are the two halves of this file.
//
// THE CONNECTION IS SHOWN AS THE PLACE AND NOWHERE ELSE. The workspace is
// written `devbox:/s/c/app` on the status sheet's place row, `devbox:app` in the
// status line's place segment, and `devbox:/srv/code/app` in /status — the same
// three renderings a local session already has, each with the machine in front
// of it ([app.placePath], [app.place], statusnote.go). The legend under the
// input says the machine too, and says it as a segment of its own —
// `devbox · porting the parser` — because that end of the border stopped
// carrying a path and started carrying the conversation's name, and `devbox:`
// in front of a sentence of English is scp syntax pointed at something nobody
// can copy (render.go's [app.legendLeft]). That is the whole
// indicator, and it is one string rather than a segment. It sits
// where a person already looks to answer "where am I", it costs no new rows and
// no new segments, and it disappears completely on a local session — which is
// the test a good indicator passes: it is invisible when there is nothing to
// say. A badge, an icon or a "connected" word would all be a second place to
// look for a fact that belongs in the first one.
//
// THE ONE SEGMENT A CONNECTION EVER GROWS IS ABOUT IT NOT WORKING, and it
// passes the same test: `reconnecting to devbox — trying for up to 5 minutes`
// is drawn while the link is being redialled and nothing is drawn at any other
// moment (hostlink.go). A healthy link still says nothing.
//
// THE MACHINE NAME IS PART OF THE PATH, not a decoration on it, which is why it
// takes the path's own paint (the legend dims both halves together) and why it
// is written with a colon: `devbox:code/app` is what a person would type into
// scp, and it is what they typed to get here. The `~` collapse still runs
// against THIS machine's home and so rarely fires on a remote path, which is
// itself a quiet tell that the path belongs to somebody else.
//
// WHAT CANNOT REACH THE OTHER MACHINE SAYS SO. The second half is the honesty
// law: this surface does a handful of things against the agent's own disk and
// the person's own browser, and over a connection those two are on different
// machines. Every one of them is listed below with the sentence it now says.
// Nothing is left to fail quietly, and nothing pretends.
//
//	the git branch    turned OFF (the probe would read THIS machine's
//	                  repository at the remote path, and a coincidence is worse
//	                  than a blank). An empty branch draws nothing, which the
//	                  emptiness law already handles, so this one is silent by
//	                  design rather than by omission.
//	/connect          says [connectRemoteWord]: the panel writes to THIS
//	                  machine's account store and the session reads the other
//	                  one's, so every row on it would be a sign-in that landed in
//	                  the wrong place. Accounts already connected on the far
//	                  machine keep working — the tools that use them run there —
//	                  so only NEW sign-ins are off.
//	a browser sign-in the ask card says [connectAskRemoteWord] and offers only
//	                  "not now", because approving is what opens a browser and
//	                  waits on a loopback port, and the browser is here while the
//	                  port is there. See [app.hostedBrowserSignIn] for why that is
//	                  the exact line.
//	a key sign-in     WORKS, unchanged. The person pastes a secret into a box on
//	                  this screen and it travels on the wire like every other
//	                  answer; nothing about it needs a browser or a port.
//	/settings         says [settingsRemoteWord] as it opens, and opens anyway.
//	                  Half these rows are this surface's own (the mouse, the
//	                  timestamps, the draft) and genuinely apply; the other half
//	                  govern the SESSION, which reads them from the far machine's
//	                  profile. A panel that closed itself would take working rows
//	                  away; one that said nothing would let a person turn a gate
//	                  off and watch it stay on.
//	the always key    the consent card's "always" writes nothing (the door hands
//	                  no save seams over a connection), so the row says "allowed"
//	                  rather than "saved" — which is the truth: the answer holds
//	                  for this session, on the far machine, and is written down
//	                  nowhere. consent.go's own comment states that bargain.
//	the YOLO badge    drawn from the FAR machine's posture, carried once on the
//	                  welcome (internal/remote's wire.go Welcome.ApprovalMode)
//	                  rather than read off this laptop's profile — a badge read
//	                  off the wrong machine would be a safety claim about a
//	                  machine nobody consulted. [app.approvalPosture] is where
//	                  the two readings — live and local, or carried and remote —
//	                  meet.
//	/harness          the registry is the far machine's, and this build has no
//	                  door onto it, so the door hands none and the command says
//	                  harnesses are unavailable rather than listing THIS
//	                  machine's and offering to run them there.
//	BUILDING a harness
//	                  OFF, and off at the engine rather than here
//	                  (cmd/aforge's engine.go nils Config.HarnessStore). The
//	                  design lane is a standing subscription on the agent
//	                  ([designAgent.HarnessDesigns]) and a remote handle has no
//	                  such method, so this surface never subscribes — a design
//	                  left switched on would have spent two model calls and
//	                  raised its keep-or-drop card into a room with nobody in
//	                  it. Turning it off at the far end means the model does not
//	                  have the verb and says so. RUNNING one that already exists
//	                  is unaffected: that rides Harnesses and RunHarness, which
//	                  the engine still fills.
//	the task rail     absent, and absent by construction: task.go asserts an
//	                  optional interface on the agent and the remote one does not
//	                  implement it, so there is no rail, no room, and no journal
//	                  read at a path that is not on this disk.
//	/image and @      LOCAL, and deliberately: the picture is on the machine the
//	                  person is sitting at, and the bytes travel with the message
//	                  (internal/remote's SubmitImage). So a relative path and the
//	                  completion walk are both anchored HERE rather than on the
//	                  remote workspace — see [app.pathRoot].
//	standing items    ON, and on at both ends. The `stand` tool is on the belt
//	                  over a connection because the engine keeps its ambient
//	                  side (cmd/aforge's engine.go), the proposal card crosses as
//	                  an ordinary event and the answer crosses back as its own
//	                  frame (internal/remote's ResolveStanding), so a person
//	                  sitting here can set something up on the far machine and it
//	                  goes on working after this window closes. THE ITEMS BELONG
//	                  TO THE MACHINE THAT RUNS THEM: the store, the profile rules
//	                  a firing inherits and the OS timer are all the engine's.
//	the item band     wired to the engine's store and drawn nowhere, because
//	                  /home does not open over a connection at all
//	                  ([homeRemoteWord] above). What the seam actually lights up
//	                  here is the status line's `keeping an eye on` segment,
//	                  which asks about THIS window's workspace — and over --host
//	                  that path is the engine's own, so the count is about the
//	                  right machine. The rows answer from a cache that refreshes
//	                  behind itself (cmd/aforge's [hostStanding]), because this
//	                  seam is asked on the frame and a wire call is not.
//	`keeping watch`   NO LINE. The OS timer is the far machine's and its status
//	                  is derived from a definition file on that disk, so the door
//	                  hands no Watch over rather than reading this laptop's
//	                  launchd — a status about the wrong machine. [StandingSeam]
//	                  already calls a false the honest third state, and the
//	                  emptiness law draws it as nothing.
//	the ● glyph       never worn, and for the reason the field states rather than
//	                  for a remote one: a firing is in flight inside whichever
//	                  process holds the tick lock, nothing on disk says so, and
//	                  no frame could carry an answer the far end does not have.
//	                  Running is nil here exactly as it is at home.
//	a dropped link    SAID, and said in the one place a condition belongs: the
//	                  status line grows a segment reading `reconnecting to
//	                  devbox — trying for up to 5 minutes` while the connection
//	                  is being redialled, and nothing at all the rest of the
//	                  time. That is not an exception to this file's header — it
//	                  is the same rule, because a working link says nothing and
//	                  the segment exists only in the seconds where that stops
//	                  being true. There is still no badge, no icon and no
//	                  "connected" word (hostlink.go).
//	news from a redial
//	                  an ordinary note in the transcript, once: the engine did
//	                  not keep the turn, or it came back with a different
//	                  conversation open. It DRAINS on the far side, so exactly
//	                  one place reads it ([app.takeLinkNotice]).
//	a question raised while nobody was here
//	                  DRAWN, as the card it would have been live: the far
//	                  machine holds it and the surface replays its event through
//	                  [app.event], so the key that answers it is the key that
//	                  always answered it. How long it waited is a line above the
//	                  card, and a question raised moments ago gets none. A KIND
//	                  THIS BUILD DOES NOT DRAW IS SKIPPED and left waiting for a
//	                  build that does, which is the wire's own contract.
//	/export           writes HERE, and the note says so ([exportHereWord]). The
//	                  transcript is assembled from what this surface is holding,
//	                  so it can be written without asking anybody; the wire has no
//	                  door for putting a file on the far machine's disk, and
//	                  inventing one belongs to the lane that owns the contract.
//	                  STUB: with a wire method for it, this becomes a remote write
//	                  and the note gains the host prefix like every other path.

// hosted reports whether the session under this surface is on another machine.
func (a *app) hosted() bool { return a.host != "" }

// ownedWord is what an owned session's place is called instead of its path.
//
// It is the product's own name because that is the honest answer to "where am
// I": nowhere in particular, in aforge's own space. A person who opened a
// terminal in a project sees the project; a person who opened one anywhere else
// used to see ~/.aforge/v3/projects/-home-someone/9f3c…/work, which is a true
// path and a useless sentence.
const ownedWord = "aforge"

// placeShown is the whole rule for what the status line calls a conversation's
// directory, in one function because there are now two moments that ask it: the
// surface being built, and a conversation being taken up in front of the one
// that was there ([app.takeUp]). A second spelling of this would drift, and the
// way it would drift is that a conversation opened later would print a path
// where the first one printed a name.
//
// THE PLACE CARRIES THE MACHINE (this file's header): on a remote session every
// rendering of where-you-are reads `devbox:app`, because the connection is shown
// as the place and is shown nowhere else.
//
// AND AN OWNED SESSION IS NAMED, NOT PATHED ([ownedWord]). The base name of an
// owned workspace is the literal word "work", which is the least informative
// thing the status line could possibly say about where a person is.
func placeShown(workspace string, owned bool, host string) string {
	shown := ""
	if workspace != "" {
		shown = filepath.Base(workspace)
	}
	if owned {
		shown = ownedWord
	}
	if host != "" && shown != "" {
		shown = host + ":" + shown
	}
	return shown
}

// placeWord is the place as it should be READ: the workspace's own name when
// the session borrowed a project, and [ownedWord] when it owns its workspace.
//
// The path is not shortened away here — [shortPath] still does that, and does it
// on a path worth reading. This is the prior question of whether there is a path
// worth reading at all.
func (a *app) placeWord(path string) string {
	if a.owned {
		return ownedWord
	}
	return path
}

// hostedPath is a path as it should be READ: on a remote session, the machine
// and then the path, so a person copying it knows whose disk it is on.
//
// An empty path stays empty. A prefix on nothing would be a machine name
// pretending to be a place.
func (a *app) hostedPath(path string) string {
	if !a.hosted() || strings.TrimSpace(path) == "" {
		return path
	}
	return a.host + ":" + path
}

// hostedBrowserSignIn reports whether the offer at the head of the queue is the
// one kind of sign-in a connection cannot carry.
//
// THE LINE IS THE BROWSER AND NOT THE ACCOUNT. A key sign-in works perfectly
// over --host: the person pastes a secret into a box on this screen, the secret
// travels on the wire like every other answer, and the engine stores it beside
// its own session. Nothing about it needs a browser or a port. A browser sign-in
// cannot: it opens a page HERE and waits for a redirect to a loopback port on
// whichever machine minted the flow, and those are two different machines. So
// this is the narrowest true statement of what is off, and the card, the offer
// row and the enter key all read it rather than each deciding for themselves.
func (a *app) hostedBrowserSignIn() bool {
	return a.hosted() && a.asksConnect() && !a.connAsks[0].needsKey
}

// pathRoot is where a relative path the person typed is anchored.
//
// It is the workspace on a local session, which is the directory the
// conversation is about. On a remote one it is THIS machine's own directory,
// because the only paths a person types at this surface are paths on the
// machine they are sitting at: /image points at a picture on their laptop, and
// joining it onto the far machine's workspace would build a path that exists on
// neither.
func (a *app) pathRoot() string {
	if a.hosted() {
		return a.localRoot
	}
	return a.workspace
}

// The sentences. They are here rather than beside the panels they belong to so
// that the whole of what a connection cannot do can be read in one place, and so
// that they answer in one voice.
const (
	// connectRemoteWord is /connect over a connection. It is a note, and a note
	// wraps in the transcript, so it can afford the whole reason.
	connectRemoteWord = "connecting an account is not available over --host yet — the sign-in opens a browser here and the account belongs to the machine over there. accounts already connected on that machine keep working."
	// connectAskRemoteWord is the same fact on the ask card, where it has one
	// row and shares it with nothing. It says WHAT and leaves the why to the
	// command, which is the trade every row in a frame makes.
	connectAskRemoteWord = "connecting an account is not available over --host yet"
	// settingsRemoteWord opens the panel on a remote session.
	settingsRemoteWord = "these rows are this machine's — the ones that govern the conversation are read from the profile on the other one"
	// exportHereWord follows the path a remote session's /export landed on.
	exportHereWord = " · on this machine"
)
