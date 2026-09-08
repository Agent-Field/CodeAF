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
// THE MACHINE IS SHOWN AS THE PLACE, AND THE LINK'S SPEED IS SHOWN AS SPEED.
// The workspace is
// written `devbox:/s/c/app` on the status sheet's place row, `devbox:app` in the
// status line's place segment, and `devbox:/srv/code/app` in /status — the same
// three renderings a local session already has, each with the machine in front
// of it ([app.placePath], [app.place], statusnote.go). The legend under the
// input says the machine too, and says it as a segment of its own —
// `devbox · porting the parser` — because that end of the border stopped
// carrying a path and started carrying the conversation's name, and `devbox:`
// in front of a sentence of English is scp syntax pointed at something nobody
// can copy (render.go's [app.legendLeft]). That is the whole
// place indicator, and it is one string rather than a segment. It sits
// where a person already looks to answer "where am I", it costs no new rows and
// no new segments, and it disappears completely on a local session — which is
// the test a good indicator passes: it is invisible when there is nothing to
// say. A badge, an icon or a "connected" word would all be a second place to
// look for that fact.
//
// THE CONNECTION SEGMENT IS A MEASUREMENT, NEVER A BADGE. It begins empty and,
// after an actual round trip answers, reads `devbox · 3ms`. If the link stops
// working, `reconnecting to devbox — trying for up to 5 minutes` takes its
// place until the redial finishes (hostlink.go). A local session has neither.
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
//	                  the three readings — live from the profile, forced by the
//	                  flag, or carried from the remote engine — meet.
//	the bash countdown
//	                  drawn from the FAR machine's armed clock, carried once on
//	                  the welcome beside the approval posture. Reading this
//	                  laptop's `background after` row would put a deadline on a
//	                  process whose timer was built from another profile.
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
//	the task rail     DRAWN from this conversation's rows in the far world. A
//	                  running or landed row opens immediately: Task.Room brings
//	                  the bounded journal tail on the room's own beat, and the
//	                  ordinary renderer draws it. Task.Steer and Task.Stop carry
//	                  those two actions to the engine. Changing its model stays
//	                  absent because the wire has no door for it.
//	/task             WORKS. Brief, solo and adaptive starts cross to the engine,
//	                  which shapes, admits and records the work. The next world
//	                  refresh brings its row back through Places.Task.
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
//	the item band     DRAWN, now that home lists the far machine's projects: the
//	                  band under a project row is that project's standing items,
//	                  asked of the engine's own store by a path that is real
//	                  there. It also still lights up the status line's
//	                  `keeping an eye on` segment,
//	                  which asks about THIS window's workspace — and over --host
//	                  that path is the engine's own, so the count is about the
//	                  right machine. The rows answer from a cache that refreshes
//	                  behind itself (cmd/aforge's [hostStanding]), because this
//	                  seam is asked on the frame and a wire call is not.
//	`keeping watch`   WORKS. Standing.Watch crosses the wire and reads the far
//	                  machine's scheduler, so /status says installed, absent, or
//	                  nothing when that engine has no scheduler to ask. It never
//	                  consults this laptop's timer.
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
//	another window typing
//	                  THE KEYBOARD FOLLOWS THE NEWEST WINDOW. More than one
//	                  surface can be attached to one conversation over there,
//	                  and the machine holding it names exactly one of them as
//	                  the one that may type (internal/remote's driver.go). A
//	                  window that is not it becomes a WATCHER: its transcript
//	                  keeps arriving live, its composer is replaced by one dim
//	                  line reading `typing from spark now` and
//	                  `enter takes it back`, and its draft is KEPT — not
//	                  cleared, not sent. Enter takes the keyboard back in one
//	                  round trip and the other window is told in the same
//	                  instant. A window on THIS machine is called
//	                  `another window`, which is the word home already uses.
//	                  A WATCHER IS STILL A WINDOW ONTO THE WORK: a turn started
//	                  on the other machine is drawn here as it happens, with the
//	                  message that opened it above the reply, by the code that
//	                  draws every turn (watching.go's [app.followTurn]).
//	                  All of it is absent locally: a second window on one
//	                  conversation here is refused at the journal instead
//	                  ([sessionBusyWord]), so there is no room to share.
//	the link          MEASURED, gently: after the first empty call returns, the
//	                  status line reads `devbox · 3ms`, using a rolling estimate
//	                  so one packet does not make the row twitch. Before that
//	                  reply it says nothing, never `0ms`. While the connection
//	                  is being redialled, `reconnecting to devbox — trying for
//	                  up to 5 minutes` takes the segment and no ping is sent.
//	                  There is still no badge, icon or "connected" word
//	                  (hostlink.go).
//	the model catalog SPLIT BY RESPONSIBILITY. The laptop's catalog supplies the
//	                  picker rows and their display facts. The engine's catalog
//	                  owns execution facts: SetModel resolves the context window
//	                  there, and the remote handle ignores the laptop's later
//	                  SetContextWindow hint, so compaction follows the machine
//	                  doing the work even when the two caches differ.
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
//	a path in a reply
//	                  A LINK, and it was not one for a wave. The word is
//	                  confirmed by the ENGINE — internal/remote's StatPaths,
//	                  asked in batches off the render path — and the anchor
//	                  points at a loopback file door this surface owns rather
//	                  than at `file://`, which was the whole of the old
//	                  objection: a file URI names THIS machine's disk
//	                  (pathlink.go's far-side section, remotefiles.go). A word
//	                  the engine did not confirm is plain text, exactly as it is
//	                  at home. A confirmed DIRECTORY is not linked in this wave.
//	/files            THE FAR WORKSPACE, AS A PAGE. Bare, it opens the file
//	                  door's browse page in this machine's browser and writes the
//	                  address into the transcript; with a path, it brings that one
//	                  file back and hands it to this machine's own viewer
//	                  (remoteopen.go). The list of what THIS machine has made is
//	                  what a hosted session used to get with an apology under it
//	                  ([filesRemoteWord]), and that is still what a connection
//	                  with no file seam gets.
//	the deliverables band
//	                  THE FAR MACHINE'S. Its rows ride with Places.World, keyed
//	                  by the far conversation id, and its paths become the same
//	                  fetched links as paths in a reply. The surface never joins
//	                  a far id to this machine's artifacts index.
//	dropping a file ON the browse page
//	                  IT LANDS IN THAT SESSION'S attachments/ FOLDER AND SAYS
//	                  NOTHING. The wire's Deposit.File keeps a file without
//	                  submitting it (internal/remote's file.go), so no turn
//	                  opens, no event is sent and nothing reaches the transcript
//	                  — a drag onto a web page is not a sentence anybody said,
//	                  and a drop that started a model turn would be one nobody
//	                  at this end asked for. The path it landed at is the
//	                  ENGINE'S answer, shown as it came. /attach is the same
//	                  landing place WITH a person's own sentence attached, which
//	                  is what makes it a turn somebody meant.
//	/export           writes HERE, and the note says so ([exportHereWord]). The
//	                  transcript is assembled from what this surface is holding,
//	                  so it can be written without asking anybody; the wire has no
//	                  door for putting a file on the far machine's disk, and
//	                  inventing one belongs to the lane that owns the contract.
//	                  STUB: with a wire method for it, this becomes a remote write
//	                  and the note gains the host prefix like every other path.
//	a generated picture
//	                  A FETCHABLE PATH, NOT AN INLINE PREVIEW. The result names
//	                  the far file and the ordinary far-path door opens it here;
//	                  the half-block painter reads a local file, so it draws
//	                  nothing until the bytes have crossed by an explicit open.
//	                  The result line is kept whole rather than replaced by an
//	                  error or a picture read from the wrong disk.

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
	return a.hosted() && a.asksConnect() && (!a.connAsks[0].needsKey || a.connAsks[0].blank != "")
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
	settingsRemoteWord = "these rows and changes belong to this machine — this conversation reads its profile on the other one"
	// exportHereWord follows the path a remote session's /export landed on.
	exportHereWord = " · on this machine"
)

// remoteProfileWord is the honest floor for commands whose setting or store
// belongs to the session's machine but has no wire door yet. The machine is
// named because "another machine" makes a destructive refusal needlessly
// vague when the surface already knows exactly which one it is connected to.
func (a *app) remoteProfileWord(thing string) string {
	return a.host + " owns " + thing + " · change it on that machine"
}

// ── THE PLACES AND THE MACHINE THE SESSION IS ON ────────────────────────────
//
// A PLACE IS A LISTING OF ONE MACHINE'S DISK. Home lists the conversations under
// `~/.aforge/v3`; tasks lists the work those conversations ran; spend adds up the
// ledger every model call on that machine appends to; search reads the index of
// what was said there; memory reads the store the sessions there remember into;
// standing lists the documents that machine's timer fires from. Every one of
// those is a directory under the state root of THIS process — which over --host
// is the laptop's, while the conversation the person is sitting in runs on the
// server.
//
// THE ANSWER IS THAT THE PLACES FOLLOW THE SESSION'S MACHINE. The reading each
// one is built from is asked of the ENGINE and not of this process, so the rows
// on the frame are the rows on the machine the conversation is actually running
// on. Which of the seven have learned that, and which have not:
//
//	home       THE FAR MACHINE'S. Its projects and conversations come from
//	           internal/remote's Places.World, walked on the engine's own places
//	           root and carried whole (Decision 1's payloads). It used to draw one
//	           sentence instead of a list.
//	tasks      THE FAR MACHINE'S, out of the same world — the task rows live
//	           inside it (`world.Projects[].Sessions[].Tasks.Rows`), so the door
//	           that answered home answered this too. This is the one the owner
//	           reported: a click on the tab drew the LAPTOP's eight tasks and its
//	           $22.54 under a session on a server that had run none of them, with
//	           the full confidence of a page that had read a real disk.
//	standing   THE FAR MACHINE'S, and it was half true already: what stands on
//	           THIS conversation always crossed the wire (Standing.Items), and
//	           what else keeps an eye on that machine is a walk of the far world's
//	           projects asking the far store about paths that are real there.
//	spend      THE FAR MACHINE'S, through Places.Ledger and a held cache.
//	search     THE FAR MACHINE'S, one call from the search command's goroutine.
//	memory     THE FAR MACHINE'S, all seven readings and writes together.
//	settings   SPLIT, AND CORRECTLY: half its rows are this surface's own and
//	           half are read from the far machine's profile, which is what
//	           [settingsRemoteWord] says as it opens.
//
// A PLACE THAT HAS NOT LEARNED SAYS SO, in one dim line where its rows would be
// ([place.remote], pages.go). The sentence is the place's own because the noun in
// it is: what this machine RAN is not what this machine has LEARNED. What is
// shared is the shape — the thing the place shows, then the fact that the session
// is somewhere else — so that the rooms say one thing in one voice. A place that
// learns to cross deletes its sentence in the same change, which is the law
// CLAUDE.md states about the manual said about the code.
//
// AND THE FRAME SAYS WHOSE MACHINE IT IS. A room whose rows quietly changed which
// disk they describe would be the same fault walked backwards, so the tab bar
// carries the machine's name at its right end and nothing at all on a local
// session ([app.placeBarMachine]). It is [app.host], the same field the status
// line's place segment, /status and the legend under the input all read, because
// the connection is shown as the place and is shown nowhere else.
const (
	// memoryRemoteWord is the memory place over --host. It replaces
	// [memoryOffNote], which would otherwise say memory is off for this session
	// — and that is a claim about the far machine's settings that this surface
	// has never asked about.
	memoryRemoteWord = "memory shows what this machine has learned, and this session is on another"
	// spendRemoteWord is the spend place over --host.
	spendRemoteWord = "spend shows what this machine has cost, and this session is on another"
	// searchRemoteWord is the search place over --host. The box still takes
	// letters, because a place with a composer that refused them would be a box
	// that eats typing; what it cannot do is find anything, and this says so
	// before somebody reads an empty result as an answer.
	searchRemoteWord = "search reads what was said on this machine, and this session is on another"
)
