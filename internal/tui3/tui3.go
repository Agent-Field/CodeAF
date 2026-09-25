// Package tui3 is the v3 chat surface in its first, linear form: a try-out
// door onto internal/session and nothing else. One column, one conversation,
// one input line — the person types, the agent works, and what it does streams
// back as it happens.
//
// It is deliberately less than docs/CHAT-V3.md describes. The rooms,
// the cards and the node drill-in all belong to the tasker, and the tasker is
// not attached yet: when it is, it arrives as extra tools inside the session
// and extra panes beside this one. Until then a surface that draws a rail
// would be drawing a lie, so this one draws a conversation.
//
// The seam is [Agent] — the handful of methods this surface calls on a session.
// *session.Agent satisfies it; so does a scripted fake, which is how the
// surface is tested without a provider. The surface never constructs an agent
// (cmd/codeaf owns config resolution and session files); it is handed one, and
// handed a way to ask for a fresh one when the person types /new.
//
// What lives where:
//
//	tui3.go     the package seam: Agent, Options, Run
//	app.go      the program model: state, messages, the event pump, slashes
//	consent.go  the approval question: the overlay, the keys, the annotation
//	thinking.go the reasoning block: streamed, then collapsed to one row
//	followup.go ctrl+q: the message that waits for the turn to end
//	input.go    the multi-line draft and the key map
//	render.go   the styles and the transcript → lines function
//	view.go     the frame: status line, conversation viewport, input block
//	palette.go  the model picker, and the overlay grammar all three lists share
//	commands.go the command list: what "/" opens
//	files.go    the file completion: what "@" opens
//	taskmention.go the other half of "@": this project's tasks, and the pointer
//	            block a chosen one becomes in the sentence
//	attach.go   the attachment tray: pictures on their way into a message
//	recall.go   the up arrow: input history, and the draft it holds for you
//	draft.go    the unsent sentence, kept per directory between sessions
//	replay.go   a resumed session, drawn
//	resume.go   the session picker: /resume, and what `codeaf resume` opens on
//	models.go   where the model list comes from, and never from the network
//	settings.go the settings panel: tabs over internal/config's own registry
//	welcome.go  the box an empty session opens with, and the sessions in it
package tui3

import (
	"context"
	"io"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/leave"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
	"github.com/Agent-Field/codeaf/internal/subharness"
	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

// Agent is the slice of *session.Agent this surface uses. It is an interface
// so the surface can be driven in a test by a scripted agent that answers
// without a provider — the same reason session.Completer is one.
type Agent interface {
	// Submit runs one turn and streams its events. Submitting while a turn is
	// in flight steers that turn rather than starting a second one.
	Submit(ctx context.Context, text string) (<-chan session.Event, error)
	// SubmitImage is Submit with pictures: one user message carrying the text
	// and the images, and then a normal turn. A model that cannot see
	// (session.Config.SupportsImages, wired in cmd/codeaf) does not end the
	// message — the pictures and the words go to the looking model instead, and
	// its answer streams back as the turn's reply, prefixed "[vision: <model>]".
	// It refuses — with an error and no stream — only when nothing can look at
	// them, when the images are too large, and when a turn is already running on
	// the blind model; which is why the surface keeps the attachment tray until
	// this has answered (attach.go).
	SubmitImage(ctx context.Context, text string, images []session.Image) (<-chan session.Event, error)
	// SubmitStanding is Submit for a draft the person MARKED as something to
	// keep true — the chord on the box (standmark.go). The turn is an ordinary
	// one in every respect except what the engine puts in front of the sentence:
	// an instruction that the sentence was marked, so it is shaped into a
	// standing order's card and never carried out as one-off work (internal/
	// session's standing_mark.go). It refuses — with an error and no stream —
	// where this build has no ambient side to hold one.
	SubmitStanding(ctx context.Context, text string) (<-chan session.Event, error)
	// Interrupt cancels the in-flight turn, keeping its partial reply. It is
	// THE PERSON'S OWN STOP and nothing else.
	Interrupt()
	// InterruptFor is the same stop for a door that is not a person: this
	// conversation being taken over by another window, left for another
	// conversation, or closed under a turn that was still running. The engine
	// writes the door down and says one sentence about a reply that never
	// arrived, which a person's own stop is owed neither of (internal/session's
	// stopcause.go).
	InterruptFor(door session.StopDoor)
	// Compact runs a compaction pass now.
	Compact(ctx context.Context) error
	// Close flushes the session file.
	Close() error
	// Model is the model the next request will use.
	Model() string
	// SetModel swaps it.
	SetModel(model string)
	// SetContextWindow says how many tokens the model now in use accepts, so
	// compaction fires against the right window after a switch. Zero and
	// negative mean "nobody knows", and the session keeps what it had.
	SetContextWindow(tokens int)
	// ReasoningFor is how hard one model is asked to think — "", "low",
	// "medium", "high", "xhigh" or "max" — for any model id, not only the one
	// in use.
	//
	// The pair is per-model rather than per-session because the picker sets a
	// level on the row under the cursor, which is usually not the model running:
	// dialling a model up and then deciding not to switch to it is a normal thing
	// to do in a list, and the level is waiting when the switch finally happens.
	// The session holds the map, so it survives the overlay closing and /new
	// starts empty (internal/session's agent.go).
	ReasoningFor(model string) string
	// SetReasoningFor sets it. An empty level is auto, which is "send nothing
	// and let the model use its own default".
	SetReasoningFor(model, level string)
	// FollowUp queues a message to be asked AFTER the current turn ends and
	// returns the channel that turn will stream on. It is ctrl+q, and it is the
	// other half of steering: a plain Enter mid-turn lands INSIDE the running
	// turn, this waits for the work to finish and then starts a turn of its own.
	FollowUp(text string) (<-chan session.Event, error)
	// ResolveConsent answers one session.EventConsentRequest for this call only.
	ResolveConsent(id uint64, allow bool)
	// ResolveConsentRemember answers one request and says how long the answer
	// lasts — session.ConsentToolSession is "stop asking me about this tool",
	// for this agent's life and no longer.
	ResolveConsentRemember(id uint64, allow bool, scope session.ConsentScope)
	// ResolveHarness answers one session.EventHarnessOffer: whether the
	// sub-harness the session matched this turn against should take it
	// (harness.go). True runs it; false is the ordinary turn, which is what the
	// person typed and what happens if this is never called.
	//
	// The model is the one the CARD SHOWED, handed back so that what runs is
	// what the person read. Empty is "whatever the offer carried", which is
	// every offer that named no model and every surface that draws the row
	// without one.
	ResolveHarness(id uint64, run bool, model string)
	// ResolveConnect answers one session.EventConnectAsk: whether codeaf may
	// connect the account it reached for (connect.go). Approving is what opens
	// the browser; declining is "not now" and is remembered nowhere.
	ResolveConnect(id string, approve bool)
	// ResolveConnectKey answers one session.EventConnectAsk that arrived with
	// NeedsKey: the person pasted a key for the account the agent reached for,
	// or they backed out and the key is empty (connect.go).
	//
	// It is a SECOND method rather than a third argument on the one above,
	// because the two answers are different shapes: a browser sign-in is a
	// yes-or-not-now and the yes carries nothing, while this yes IS the secret.
	// A key never travels through the approval path, and the approval path never
	// has to carry an empty string for the services that have no key.
	ResolveConnectKey(id string, key string)
	// NoteConnected tells the session an account is connected. It is the other
	// door's other half: a person can open /connect mid-conversation and connect
	// something the session gave up on, and without this the session would still
	// believe it has nothing.
	NoteConnected(service, account string)
	// Title is the name the session gave itself, empty until it has one. The
	// surface reads it at construction (a resumed session is already named) and
	// then follows session.EventTitleChanged.
	Title() string
	// Usage is the session's running total.
	Usage() session.Usage
	// ContextTokens is what the conversation weighs right now, in tokens: the
	// provider's own count of the last request when there has been one, and an
	// estimate of the transcript when it has grown since.
	//
	// It replaced a byte count this surface took over the display transcript,
	// which could only see words anybody said. The system prompt, the tool
	// schemas, every tool result and every call's arguments were invisible to
	// it — the majority of a working session's context — so the meter read a
	// few percent on a conversation the session was about to compact.
	ContextTokens() int
	// Transcript is the conversation so far, oldest first, shaped for display.
	// The surface reads it twice: once at construction, to draw a resumed
	// session instead of opening on an empty screen, and once per settled turn,
	// to weigh the context meter.
	Transcript() []session.DisplayEntry
	// EarlierHistory is the conversation a compaction pass edited away and where
	// the pass's rewritten copy of it ends in Transcript. The conversation, told
	// once and whole, is its Entries followed by Transcript()[Floor:].
	//
	// It is history and never context: nothing sends it and the model does not
	// carry it. The surface reads it once per replay and scrolls back into it, so
	// the boundary a pass left behind reads as the seam it is rather than as the
	// beginning of the conversation (replay.go). It is empty for a session that
	// was never compacted, which is nearly all of them.
	EarlierHistory() session.EarlierHistory
}

// detachable is an agent whose conversation OUTLIVES THIS TERMINAL, and it is
// how this surface tells the two apart without guessing.
//
// The distinction cannot be read off a machine name: a conversation hosted by
// the daemon on this laptop has no machine in front of it (cmd/codeaf's
// chatv3_local.go dials with an empty host on purpose), and it is exactly the
// one whose work must survive the window. Only the agent knows what it is
// attached to, so it is asked ([remote.Agent.Detach]).
//
// An agent that does not implement it is one whose work is this process — the
// in-process door — and leaving it is ending it. An agent that DOES implement it
// still has to be asked whether the work outlives the exit: implementing the
// interface says only that the agent owns the answer.
type detachable interface {
	// WorkOutlivesExit says whether this conversation keeps working after the
	// view goes. It is a separate question from Detach because the same type
	// answers both ways: a conversation on a session host outlives the window,
	// and a one-shot engine on a pipe does not, and the switcher's rows and the
	// close-a-tab card both have to say which (keeper.go's [workOutlivesExit]).
	WorkOutlivesExit() bool
	// Detach lets go of the view. Where the work outlives it the conversation is
	// left running; where it does not, this is the ordinary ending. It must be
	// safe to call twice and safe on a connection that has already died.
	Detach() error
}

// Conversation is one live agent and everything the door resolved around it:
// where it works, what it may keep, and the seams that answer for THAT agent
// and no other.
//
// IT EXISTS BECAUSE THE CLOSURES ARE PER AGENT, and holding that as eleven
// separate fields on Options was a bug rather than a style. The consent card's
// "always" is written to disk and then handed to the gate the session is
// running behind (cmd/codeaf's chatv3_approval.go); when the trio was built once
// at boot, an always answered after a /new or a resume was saved correctly, said
// "saved" correctly, and pushed the rebuilt gate into the agent that had just
// been closed — so it did not answer the very next call, as the manual says it
// does, but the next launch, with nothing on screen saying so. Minting the
// closures in the same call that builds the agent is what makes that
// impossible: there is no moment at which the surface holds an agent and a
// closure that disagree about which conversation they are.
//
// A door that cannot fill a field leaves it zero, and the surface keeps what it
// had — see [app.takeUp], which is the one place a conversation is taken up.
type Conversation struct {
	// Agent is the conversation itself. Required; everything else may be zero.
	Agent Agent
	// SessionFile is the transcript this conversation writes.
	SessionFile string
	// Workspace is the directory it works in, and Owned says that directory is
	// the session's own work/ rather than a project it borrowed — the same two
	// facts [Options.Workspace] and [Options.Owned] carry for the first one.
	// Place is the base name to draw; empty lets the surface name it from the
	// two above, which is what every door does today.
	Workspace string
	Place     string
	Owned     bool
	// AnchorWorkspace is the one-shot project anchor for an owned conversation.
	// It travels with the agent because /new and resume replace both together.
	AnchorWorkspace func(path string) (string, error)
	// Resumed says the transcript was picked up rather than made, and Notice is
	// the one sentence the door wants on the entry line about how this
	// conversation came to be open.
	Resumed bool
	Notice  string
	// ContextWindow is how many tokens this conversation's model accepts. Zero
	// leaves the surface's meter where it was, because a percentage of an
	// unknown means nothing.
	ContextWindow int
	// DraftFile is where this conversation's unsent sentence is kept, and empty
	// is the project saying keep nothing — which is already how the surface
	// spells "not kept at all" (draft.go).
	DraftFile string
	// History is the recall list this conversation's up arrow walks, and nil is
	// this WORKSPACE keeping none. It is the store the door opened once per
	// process rather than one per conversation: the file is one file and the
	// entries carry the directory they were typed in, so the enablement is the
	// only half of the answer that is per workspace and this field is that half
	// said honestly — the store when the answer is yes, nothing when it is no.
	History History
	// RecentSessions is this conversation's own project's list, and the three
	// approval seams are bound to the agent above. Each is nil on a door that
	// cannot answer it, and the surface then keeps whatever it was holding.
	RecentSessions   func() []Session
	SaveApproval     func(tool string) error
	SaveBashApproval func(command string) error
	ApplyApprovals   func() error

	// TaskRoom and TaskIndex are [Options.TaskRoom] and [Options.TaskIndex] ABOUT
	// THIS CONVERSATION, and they are here for the reason the approval trio above
	// is: they were wired once, at boot, around the conversation the door opened
	// before the surface existed. That was harmless while an engine door held one
	// conversation at a time and is wrong the moment it holds several — a window
	// with three chats open would read every task page and every roster out of the
	// FIRST one, under the second one's name.
	//
	// Nil leaves whatever the surface was already holding, which is what the local
	// door passes: there both readings are of this process's own disk and neither
	// needs a per-conversation door.
	TaskRoom  func(id uint64, tail int) (session.TaskRecord, error)
	TaskIndex func() ([]session.TaskIndexEntry, bool)

	// Link is [Options.Link] ABOUT THIS CONVERSATION, and it is here because on a
	// door where each conversation has its own connection every one of those seven
	// facts is a fact about that connection: the sentence a dropped link says, the
	// measured round trip, the one-off news a redial found, WHICH QUESTIONS ARE
	// WAITING, who holds the keyboard, and the turns another window started.
	//
	// THE WAITING QUESTIONS ARE WHY THIS IS CORRECTNESS AND NOT TIDINESS. The
	// engine holds a session's unanswered cards WITH THAT SESSION
	// (internal/remote's held.go), the surface asks for them again on every switch
	// (detach.go's [app.attachConversation]), and a surface that asked the wrong
	// connection would replay one conversation's consent card into another one.
	//
	// Nil leaves whatever the surface holds, which is every local door — there is
	// no link — and every door whose conversations share one connection.
	Link *LinkSeam
}

// TaskOwnerAsk names the conversation a task page wants to look into.
//
// IT IS THE TRANSCRIPT AND NOT THE SESSION ID, because that is what the wire
// already carries ([remote.Hello.Session] is "an explicit session file to
// open") and what every other door on this surface passes around. The id is
// what the RECORD joins on; the path is what a door opens.
type TaskOwnerAsk struct {
	// Session is the owner's transcript, exactly as the record spells it.
	Session string
	// Workspace is the project that conversation belongs to, so a door that has
	// to choose an engine has the same answer the row was read under.
	Workspace string
}

// TaskOwnerView is one attached view onto somebody else's conversation.
//
// EVERY FIELD IS A CAPABILITY OR THE END OF ONE. There is no agent here and no
// session: a page that held either would be a second surface, and this is a
// reader with a way to close itself.
//
// IT READS AND DOES NOT WRITE, deliberately. The ask was to be able to SEE work
// running in another conversation; typing into it needs that conversation's
// keyboard, a message identity the owning window would recognise, and a draft
// belonging to the task rather than to the conversation on screen — none of which
// a reader needs and all of which are somebody else's lane. So the door attaches
// as a watcher ([remote.Hello.Watch]) and there is no steering field to misuse.
type TaskOwnerView struct {
	// Session is the transcript the engine ACTUALLY opened, and the caller
	// checks it against what it asked for before drawing anything. An engine
	// that answered about a different conversation — a key that did not match,
	// a session that had been replaced — would otherwise put one task's
	// transcript under another task's name, which is the exact failure the
	// owner check exists to end.
	Session string
	// Room reads one task's journal tail on the owner's machine, which is the
	// same bounded reading a hosted room already makes ([Options.TaskRoom]). It
	// is the whole capability: nil is a view with nothing to show, which the
	// surface refuses rather than draws.
	Room func(id uint64, tail int) (session.TaskRecord, error)
	// Watch is THE OWNER'S OWN ACCOUNT OF WHAT ITS WORK IS DOING: the standing
	// task subscription that conversation already publishes
	// ([remote.MethodTaskWatch]), which replays its whole roster the moment it is
	// opened and then pushes one notice per change. It hands back the lane and the
	// way out of it.
	//
	// IT EXISTS BECAUSE THE JOURNAL CANNOT ANSWER THE QUESTION. A reading is a
	// report, a tail and whether the file is still there ([session.TaskRecord] has
	// no state), and the row the page was opened from is a photograph of one
	// moment. Without this the page had to GUESS whether the work was still going,
	// and the only thing near enough to guess from was the presence directory of
	// the workspace THIS window happens to be in — which does not contain another
	// project's conversation at all, so a task in one read as finished the instant
	// it was opened.
	//
	// Nil is a door that cannot offer it. The page then says what the row said and
	// says that it is the last thing this window was told, rather than claiming a
	// present it cannot see.
	Watch func() (<-chan session.Event, func())
	// Questions is THE OWNER'S OWN ACCOUNT OF WHAT IT IS WAITING ON A PERSON
	// FOR: the standing questions subscription that conversation publishes
	// ([remote.MethodQuestionWatch]), which replays everything still open the
	// moment it is opened and then pushes one event per question raised,
	// withdrawn or answered. It hands back the lane and the way out of it.
	//
	// IT IS READ AND NEVER ANSWERED. The page draws that the work has stopped on
	// a question, dim, and offers no key: answering belongs to the window that
	// owns the work, and the wire refuses this connection the answering door by
	// construction (internal/remote's watcherReads). What it ends is the page
	// drawing a running clock over a conversation that has been waiting on
	// somebody for an hour — which the roster cannot say, because a node sitting
	// on a question is still `running`.
	//
	// Nil is a door that cannot offer it. The page then says exactly what it said
	// before, which is what a capability that cannot work is owed.
	Questions func() (<-chan session.Event, func())
	// TaskPage reads ONE TASK'S STORED PAGE in the owner's own store — the page
	// a program's task is drawn from, since a program writes no worker journal
	// for [TaskOwnerView.Room] to read ([session.PlanTaskPage.Program]). It is
	// the same read this window's own program room makes of its own store
	// ([session.Agent.PlanTaskPage]), made on this view's connection so the id
	// is answered in the owner's numbering and never in this window's.
	//
	// IT KEEPS THE ENGINE'S REFUSAL, where the agent's own read folds every
	// failure into "not found": a page this window is reading learns that the
	// conversation under it was replaced from exactly this error
	// ([remote.ErrJoinedGone]), and a program's page reads nothing else.
	//
	// Nil is a door that cannot offer it, and every page opened through that
	// door is the journal reading it always was.
	TaskPage func(id string) (session.PlanTaskPage, bool, error)
	// Close gives back THIS VIEW'S connection and nothing else. The conversation
	// goes on running, the window that owns it keeps its keyboard, and the
	// engine is untouched.
	Close func() error
}

// OpenRouterFlow is one browser connection that will hand this profile a model
// key. The concrete loopback listener belongs to the door, not the surface;
// this is the smallest seam the setup screen needs to show it, wait for it and
// release it when the person presses escape.
type OpenRouterFlow interface {
	URL() string
	Wait(context.Context) (string, error)
	Cancel()
}

// CodexFlow is one browser sign-in that returns the ChatGPT-plan credentials
// config keeps outside the surface. The loopback listener and token file both
// belong to the door; the surface only shows, waits and cancels the attempt.
type CodexFlow interface {
	URL() string
	Wait(context.Context) (codexauth.Tokens, error)
	Cancel()
}

// Options configures one surface.
type Options struct {
	// Agent is the conversation this surface shows. Required.
	Agent Agent
	// EngineRoad says the conversation is in a daemon on this machine. It is
	// distinct from Host: both use a remote agent, but only this road shares the
	// profile and must explain that a named key is read from the daemon's own
	// environment.
	EngineRoad bool

	// Build names the codeaf process holding the conversation. The door hands
	// it in because a hosted surface and its conversation run on different
	// machines, where this process's own build would be the wrong answer.
	Build string

	// UpdateCheck is the silent launch look at the newest release in this
	// build's channel. It is
	// a command rather than an opening read so the first frame never waits for
	// the network. Nil leaves the capability absent.
	UpdateCheck func(context.Context) (codeupdate.Available, bool)
	// ReadCredits and PaymentRefusals are the local default-service balance
	// doors. Nil leaves hosted and test surfaces without a balance reader.
	ReadCredits     func(context.Context) (credits.Reading, error)
	PaymentRefusals func(func()) func()
	// ImplicitTalk says no flag, environment value, or saved talk row chose the
	// current model, so a first-run low reading may swap its untouched default.
	ImplicitTalk bool
	// ResolveUpdate and InstallUpdate are the two off-frame halves of /update.
	// Keeping selection separate lets the surface name the tag before the
	// download begins. Nil leaves the command with an honest refusal.
	ResolveUpdate func(context.Context, codeupdate.Choice) (codeupdate.Release, error)
	InstallUpdate func(context.Context, codeupdate.Release) (codeupdate.InstallResult, error)
	// UpdateRunning is the exact revision of this process, without build-time
	// decoration. UpdateArgs are its original arguments. Restart is the slot the
	// surface fills before quitting and the door reads after the terminal is back.
	UpdateRunning string
	UpdateCurl    string
	UpdateArgs    []string
	Restart       *codeupdate.Plan

	// Memory is the durable memory store behind the memory place. Nil means the
	// place is unavailable; the live door passes the same store it gave the
	// session, wrapped so that the two READING methods are spelled the way this
	// surface asks for them (cmd/codeaf's v3MemorySeam).
	Memory MemoryStore

	// Search is the conversation index the search place reads: one full-text
	// query over every message this machine has kept ([store.Store.SearchConversations]).
	//
	// IT IS A SEAM AND NOT THE STORE for [Options.Memory]'s reason — the door
	// owns where the database lives — and it is a SECOND seam beside Memory
	// rather than a method on it because the two are different capabilities that
	// fail apart: memory turned off in the settings opens no store, and searching
	// what was said is not memory at all. A build with one and not the other is
	// the ordinary case, and each place is absent on its own terms.
	//
	// Nil is a surface that cannot search, and the place says what it is for
	// rather than drawing an empty result list.
	Search SearchStore

	// SearchStatus names the web-search plug the conversation's next call will
	// use and whether it has a key. Nil means that conversation has no web-search
	// hand, so /status omits the row under the emptiness law. It is a function
	// because search settings apply while this surface remains open.
	SearchStatus func() string

	// UsageLedger is the machine-wide spending ledger the spend place reads —
	// one line per model call, written where the turn was taken
	// (internal/session's usage_ledger.go). Empty falls through to
	// [session.UsageLedgerPath], which is where every window on this machine
	// writes: the field exists so a test can point one surface at a file it
	// wrote itself, exactly as [Options.ArtifactsIndex] does.
	//
	// IT IS A PATH AND NOT A CACHE. The cache holds parsed lines and a file
	// offset and belongs to ONE surface's goroutine ([session.UsageCache] says
	// so in as many words), so a door handing one in would be handing over a
	// thing two surfaces could then share.
	UsageLedger string

	// Ledger is the hosted reading of the machine-wide ledger. Nil keeps the
	// local path above; a hosted surface receives a non-blocking cached answer.
	Ledger func(since time.Time) (lines []session.UsageLine, held bool, known bool)

	// Archive puts a conversation away on the machine that owns its row. Nil
	// makes that action absent, so a hosted surface never writes a far path here.
	Archive func(dir string, archived bool) error

	// ── THE PLACES FOLLOW THE SESSION'S MACHINE ─────────────────────────────
	//
	// World is the walk of the conversations and projects on THE MACHINE THAT
	// OWNS THE WORK, and nil is "this process's own disk" — which is every local
	// launch, where the surface reads the places root itself and the two machines
	// are one.
	//
	// IT ANSWERS A SECOND VALUE, AND THE SECOND VALUE IS NOT "IS IT EMPTY". It
	// says whether this is an ANSWER: over a connection the world arrives from
	// the other machine and the first frames are drawn before it has, and a
	// surface that could not tell "that machine has no projects" from "that
	// machine has not said yet" would greet a person with `nothing here yet` over
	// a machine full of work. False draws NOTHING, which is the emptiness law
	// applied to the one fact every place downstream is built out of
	// ([app.worldKnown], home's [homeView.known]).
	//
	// AND IT MAY NOT BLOCK. It is asked on the open and on the three-second beat,
	// which are the two moments a place may read anything — but over a wire those
	// are still moments a person is waiting through. The door answers from a
	// cache that refreshes behind itself (cmd/codeaf's [hostWorld]), which is the
	// same bargain and the same law [StandingSeam.Items] already keeps.
	World func() (session.World, bool)

	// WorldRoot is the state root [Options.World] was walked under, on the disk
	// it was walked on. Empty falls through to this process's own places root.
	//
	// IT EXISTS BECAUSE A WORLD IS A SET OF PATHS AND A PATH NEEDS ITS DISK.
	// [session.World.Adopt] puts the conversation THIS WINDOW is sitting in back
	// into a walk taken too early to see it, and works out which bucket it
	// belongs to from the root. Over a connection that bucket is on the far
	// machine, and adopting against this laptop's root would file a conversation
	// living on the server under a project on the laptop.
	WorldRoot string

	// TaskRecord is ONE ROW of that record read deeper than [Options.World]
	// reads it: the last thing that piece of work said, out of the journal it
	// left on the machine that ran it. nil is "this process's own disk", which is
	// every local launch, where the card opens the journal itself.
	//
	// IT IS A CALL AND NOT A CACHE, which is where it parts company with the
	// world beside it. The world is asked on every place's open and on the beat,
	// so it has to answer from something warm; a record is asked once, when
	// somebody presses one row, and there are four hundred rows — a cache of them
	// would be a cache nobody reads twice. It is therefore ALLOWED to block, and
	// the surface never calls it anywhere but off the loop, in a [tea.Cmd]
	// (taskrecord.go's [app.readTaskTail]).
	TaskRecord func(uri string, tail int) (session.TaskRecord, error)
	// TaskRoom reads a node by id, including before its transcript URI lands.
	TaskRoom func(id uint64, tail int) (session.TaskRecord, error)

	// TaskIndex is this conversation's rows from the same far world. False says
	// the cache has not answered yet, so the roster waits instead of deciding
	// that a machine full of work is empty.
	TaskIndex func() ([]session.TaskIndexEntry, bool)

	// Fresh builds a replacement agent on the same Config with a new session
	// file, and returns it with that file's path. It is what /new calls when no
	// [Options.Start] was wired. Nil makes /new report that it is unavailable
	// rather than pretending.
	//
	// IT IS THE OLDER HALF OF THE SEAM and it carries only the agent, which is
	// exactly the bug [Options.Start] exists to fix: everything else the door
	// built around the conversation — the approval trio, the recent list, the
	// draft — stays bound to the conversation that was just closed. The doors
	// that can wire Start do; the hosted one cannot (there is one remote agent
	// by construction, chatv3_host.go) and keeps this.
	Fresh func() (Agent, string, error)

	// Open resumes a transcript in its own workspace, and hands back the agent
	// with the closures that belong to it. Start mints a fresh conversation in
	// a workspace. An empty workspace is THIS conversation's own, which is what
	// /new and the resume picker mean.
	//
	// They are the agent-building seam: the door owns config resolution, session
	// files and governance, and the surface owns nothing but the asking. What
	// makes them different from [Options.Fresh] and [Options.Resume] is that
	// they return a [Conversation] — the agent AND the per-conversation seams
	// minted around it, in the same call — so a surface that swaps conversations
	// cannot go on holding a closure built around the one it just closed.
	//
	// Nil on both is a surface that falls back to Fresh and Resume, which is
	// what the hosted door and every test that predates this seam are.
	Open  func(workspace, transcript string) (Conversation, error)
	Start func(workspace string) (Conversation, error)

	// EngineAnswers reports whether the workspace named has an ENGINE HOLDING IT
	// RIGHT NOW — a process that owns the journal and can hand a running
	// conversation to a second window (internal/enginehost).
	//
	// IT IS THE ONE QUESTION HOME'S ENTER KEY NEEDS AND CANNOT ASK ITSELF. A row
	// another window is holding has two completely different doors behind it: a
	// conversation an engine holds OPENS — [Options.Open] hands back the running
	// session, mid-turn, and the window that had it steps back — and one a bare
	// process holds can only be ASKED for (takeover.go). The flock says a window
	// has it and says nothing about which kind, and only the door that built this
	// surface knows whether there is an engine road at all.
	//
	// IT MUST BE CHEAP AND IT MUST BUILD NOTHING. It is asked on the keystroke
	// that opens a row, and internal/enginehost states the law it answers under:
	// ASKING WHETHER SOMEBODY IS THERE MUST NOT BUILD THEM A HOUSE. cmd/codeaf's
	// v3HostAnswers is the shape — one connect to a socket that may not be
	// there, and closed again.
	//
	// Nil is a window with no engine road: the in-process door, a test, and
	// --host, where the holder is a window on this laptop and the journal is on
	// the far machine. Every one of them keeps the road it had.
	EngineAnswers func(workspace string) bool

	// Elsewhere reads what the project's OTHER conversations have out right
	// now — the presence files beside the transcript this window is drawing —
	// for a window whose agent cannot answer that itself
	// ([session.ElsewhereOf] is the shape).
	//
	// IT IS THE HALF OF THE TASKS PAGE THE ENGINE ROAD HAD LOST. The rows of
	// work another conversation is running are minted from that reading
	// ([app.refreshElsewhere]), and it was asked of the agent alone: the
	// in-process agent reads its own disk, and the connection bare `codeaf`
	// holds to its engine does not ([remote.Agent] has no such method). So on
	// the ordinary launch no such row was ever drawn, and [Options.OpenTaskOwner]
	// — the door behind exactly those rows — could never be reached.
	//
	// Nil is a window whose disk is not the engine's (--host) or whose agent
	// answers for itself (the in-process door); both keep the road they had.
	Elsewhere func(transcript string, now time.Time) session.Elsewhere

	// OpenTaskOwner attaches a SECOND VIEW onto a conversation that is ALREADY
	// RUNNING, for as long as one task page is on screen: a reader for that
	// task's journal, and the close that gives the view back.
	//
	// IT IS CALLED OFF THE PROGRAM LOOP, always ([app.openOwnerRoom] runs it as a
	// command and numbers the ask). This is a socket and a round trip, and a
	// surface that waited for it on the keystroke would stop drawing and stop
	// answering `esc` for as long as another process took to reply.
	//
	// IT IS THE ANSWER TO "I CANNOT CLICK INTO THAT TASK". The tasks place draws
	// every piece of work this project has run, and the rows a person most wants
	// are the ones happening right now — in the conversation next door, which
	// this window has no lane into. The lane exists: `codeaf chat` is a SURFACE
	// talking to this workspace's engine over a socket (cmd/codeaf's
	// chatv3_local.go), and that engine holds every conversation open. So the door
	// dials the engine it is already talking to, names the conversation with
	// [remote.Hello.Join] — take the one that is already open, never start one —
	// and [remote.Hello.Watch], which refuses this view the keyboard by
	// construction. The work is not restarted, not interrupted and not moved, and
	// the window that owns it does not lose control of it.
	//
	// IT IS A SEPARATE SEAM FROM [Options.Open] BECAUSE IT MUST NOT SWAP
	// ANYTHING. Open resumes a conversation and, over a socket, tells the engine
	// which session THIS connection is on — which would take every attached
	// window with it. This one opens a second connection, uses it, and closes it.
	//
	// Nil is a window that cannot do this: an in-process launch, a test, a build
	// with no engine road. The surface then says one line and offers the card,
	// which is what a capability that cannot work is owed (a capability that
	// cannot work is absent, not broken).
	OpenTaskOwner func(TaskOwnerAsk) (TaskOwnerView, error)

	// AnchorWorkspace gives a project-less conversation the repository or folder
	// the person named. It returns the resolved path because a repository subdir
	// becomes its root, and the surface must draw the same place the engine uses.
	// Nil means this conversation cannot be re-anchored.
	AnchorWorkspace func(path string) (string, error)

	// Errand builds the agent behind home's `ask here` (tui3's homeexchange.go):
	// the same launch config [Fresh] uses, pointed at a transcript inside dir and
	// working in workspace.
	//
	// IT IS A SECOND SEAM AND NOT AN ARGUMENT ON THE FIRST, because the two
	// build different things. [Fresh] mints a session folder in THIS project's
	// bucket and hands back where it put it; an errand's folder is made by the
	// surface, under the standing root, and is deliberately not a place [Fresh]
	// is allowed to put anything — a conversation home would then list is exactly
	// what asking from home exists to avoid. So the caller names the folder, and
	// the door only has to point a config at it.
	//
	// The workspace is the project the cursor was on, or the person's home
	// directory when it was on none (docs/AMBIENT.md Part 5).
	//
	// IT TAKES ONE STRUCT AND NOT FOUR ARGUMENTS. The composer layer settles
	// three things before a sentence leaves it — where it runs, what the work
	// runs on, how much it may spend (SCREEN 2e) — and a fourth fact settled
	// later is a field here rather than a break in every door that fills this in.
	//
	// Nil is a window that cannot ask from home: the row says so and nothing is
	// created. A test and the --host door are both that window.
	Errand func(ErrandOrders) (Agent, error)

	// Answer leaves one answer on ANOTHER session's doorstep: the question home
	// read out of that session's presence file, answered by the key the chips
	// offered (internal/session's answers.go, tui3's homeband_answer.go). The
	// session picks it up on its own heartbeat and applies it through the same
	// resolver its own card would have called.
	//
	// IT IS A SEAM AND NOT A DIRECT CALL for the reason every write on this
	// surface is one: the door decides where state lives, and a surface that
	// wrote into another process's folder on its own would be a second place
	// that knows the layout. The live door passes [session.WriteAnswer].
	//
	// Nil is a window that can SEE another session's question and not answer it
	// — the band draws no chips, which is the absence law. A test is that
	// window. A --host session never reaches the question either, for a reason
	// one level up: home refuses to open at all over --host, because the state
	// root under this process belongs to the wrong machine (home.go).
	Answer func(dir string, kind session.QuestionKind, id uint64, key string) error

	// StandingRoot is where the ambient side keeps its things —
	// ~/.codeaf/v3/standing — which is where an errand's folder is made and where
	// one that came to nothing stays. Empty falls through to the sibling of the
	// projects root, which is what that path is by construction (internal/standing).
	StandingRoot string

	// Workspace is the directory the agent works in; its base name is the
	// place shown in the status line. Empty takes the process's cwd.
	Workspace string

	// Owned says the workspace above is the session's OWN work/ directory
	// rather than a project the person opened codeaf inside of — the difference
	// Decision 26 draws between a borrowed workspace and an owned one.
	//
	// It changes two presentation choices. An owned workspace lives at
	// ~/.codeaf/v3/projects/<encoded>/<session>/work, and a path like that told
	// the person nothing they wanted to know — it is codeaf's own bookkeeping,
	// shown where they expected to read which project they were in or browse
	// their own files. So an owned session is named rather than pathed
	// ([app.placeWord]), and its chooser opens on this window's directory
	// ([app.contextStart]). Every other use of Workspace is unchanged: it is
	// still the real directory, and it is what a typed path completes against.
	Owned bool

	// Host is the machine the agent is on, when it is not this one: the ssh
	// destination `codeaf chat --host devbox` was given. Empty is a local
	// session and every line below it is dead code.
	//
	// IT IS THE PLACE, NOT A BADGE (host.go states the whole law). The surface
	// shows a remote session by writing the workspace as `devbox:~/code/app`
	// wherever it already writes the workspace, and by adding nothing anywhere
	// else — no icon, no "connected" word, no extra segment. A person's answer
	// to "where am I" gains a machine name and costs no rows.
	//
	// It is also what the surface consults before it does anything that only
	// makes sense on the agent's own disk: the git probe, the file walk, the
	// accounts panel. See host.go.
	Host string

	// ApprovalMode is the tool-approval posture the LAUNCH hands down when it
	// knows one this surface's own profile cannot answer. Over --host that is
	// the engine's row, carried once on the welcome (internal/remote's wire.go);
	// on a local session it is --yolo's forced "allow", which opens the gate for
	// the whole session without writing the row. Empty means nothing was handed
	// down and app.approvalPosture reads the profile directly, live.
	ApprovalMode string

	// BashBackgroundAfterSeconds is the foreground-command clock the AGENT
	// armed when this session was built. It is handed over rather than re-read
	// by the surface because zero is a real posture, a setting change lands on
	// the next session, and over --host the relevant profile is on the engine's
	// machine.
	BashBackgroundAfterSeconds int

	// SessionFile is the transcript being written, shown by /help and /new.
	// Empty means the conversation is memory-only.
	SessionFile string

	// Resumed says the session file was picked up rather than created, so the
	// surface can say so in its first line.
	Resumed bool

	// Notice is one sentence the door wants on the entry notice line — the
	// place a resumed session is announced. It is how "the session you asked
	// for is open somewhere else, so this is a new one" reaches the person who
	// needs to know it, without cmd/codeaf printing to a screen the surface is
	// about to take over.
	Notice string

	// UnreadProfileKeys names config.json keys the profile loader did not consume.
	UnreadProfileKeys []string

	// ContextWindow is how many tokens the model this session starts on
	// accepts, as the door could resolve it. It feeds the status line's meter;
	// zero draws no meter, because a percentage of an unknown means nothing.
	// The surface tracks it from here on (a /model switch sets it).
	ContextWindow int

	// History is the recall list the up arrow walks (recall.go). Nil is a
	// surface with no history, which is what the setting turns off.
	History History

	// DraftFile is where the unsent draft is kept between sessions (draft.go).
	// Empty keeps it nowhere. Use [DraftFile] to name it.
	DraftFile string

	// ArtifactsIndex is the deliverables index a finished /export writes its row
	// to (internal/session's artifacts.go). Empty falls through to
	// ~/.codeaf/v3/artifacts.jsonl, the way [Options.Models] falls through to
	// this package's own cache: the door usually says, and a surface driven
	// without one still records where the rest of the product looks.
	//
	// The launch assembly passes the same path it puts on
	// session.Config.ArtifactsIndex, so a session and its surface never write
	// two indexes.
	ArtifactsIndex string

	// Models answers what /model can switch to. It is a function and not a
	// slice because the door's list may be warming: it is called the moment the
	// picker opens, so a catalog that resolved after boot is on offer, and it
	// MUST NOT block — a picker that waits on a fetch is a picker that answered
	// a question with a spinner. Nil, or an empty answer, falls through to
	// ~/.codeaf/v3/models.json and then to [BuiltinModels] (see models.go).
	//
	// THE ONE FETCH IS ASKED FOR, AND IT STILL DOES NOT BLOCK: [Options.
	// RefreshModels] runs as a command off the loop while the picker keeps
	// answering, and this function goes on returning what it returned until
	// the door has swapped in what that fetch brought back.
	Models func() []Model
	// ModelsForService is the process shelf's never-waiting reading for one
	// connected service. Keeping it beside Models makes the picker read one
	// shelf for every group instead of a surface-only map that a restart happens
	// to refill.
	ModelsForService func(modelsource.Connected) []Model

	// Sources is the ordered set of places the model picker can read from. The
	// default service is first. Empty preserves the old single-service picker;
	// a local surface can rebuild the set from ProfileDir after a connection.
	Sources modelsource.Set
	// RefreshModels asks the router for today's list, on the key the open
	// /model picker offers for it (modelrefresh.go's [refreshModelsKey]). It
	// returns the whole list, when those rows left the router, and why not.
	//
	// A DOOR THAT IMPLEMENTS IT OWES THREE THINGS: [Options.Models] reads the
	// new list from then on, ~/.codeaf/v3/models.json is written with it
	// ([WriteModelCache]), and a failure changes nothing and says why — the
	// surface keeps the list it was showing either way.
	//
	// Nil is a door with no refresh behind it, and the capability is then
	// ABSENT: the key does nothing and no line on the surface names it.
	RefreshModels func(ctx context.Context) ([]Model, time.Time, error)
	// RefreshModelsForService fetches one newly connected service into that same
	// shelf. The connect command runs it off the event loop, just as ctrl+r runs
	// RefreshModels, so opening /model never waits on the network.
	RefreshModelsForService func(context.Context, modelsource.Connected, []Model) ([]Model, error)

	// ProfileDir is the profile the settings panel reads and writes — the same
	// directory internal/config resolves every other row out of. Empty is the
	// default profile (~/.codeaf), which is what the door passes when it has
	// not been told otherwise.
	ProfileDir string

	// OneModel is the door's `--one-model`, carried here so the surface reports
	// THE FLAG AND NOT THE PROFILE IT OVERRIDES. Under it the door hands the
	// session no roles source and no task model, so every text call rides the
	// conversation's own model and the four crew rows in [Options.ProfileDir]
	// seat nothing (cmd/codeaf's applyV3Governance, internal/session's Config.OneModel).
	// A surface that did not know the flag existed read those rows anyway and
	// drew `crew custom` over a crew that was not in force (#444).
	//
	// It is a bit and not a derivation because the profile cannot be asked: the
	// rows are still on disk, unchanged, and the flag is the only thing that
	// knows they are not seating this run. The --host door refuses the flag at
	// the door (cmd/codeaf's chatv3.go), so a hosted surface never sets it.
	OneModel bool

	// SaveApproval and SaveBashApproval are how the consent card's "always"
	// outlives the session (consent.go): the first remembers one TOOL's allow,
	// the second one whole shell COMMAND, both into the person's own profile
	// rows — the same rows the settings panel edits and the place they undo it.
	//
	// They are a pair rather than one call because the two rows are two rows: a
	// tool is answered by name, and bash is answered by the command line, which
	// is the whole reason internal/approval has a pattern list at all.
	//
	// Nil is a surface that cannot remember, and then the card behaves exactly as
	// it did before this pair existed: the always key still stops the asking for
	// the rest of the session (internal/session's memo) and writes nothing. A
	// test, and a door with no profile, are both that surface.
	//
	// THEY RETURN THE WRITE'S ERROR AND THE SURFACE DROPS IT, which is the shape
	// tui2's SaveRail has with one difference worth stating. There the door drops
	// it, because nothing on screen is about to make a claim about the disk; here
	// the row is about to say "saved", so the surface has to know whether that is
	// true. It is still DROPPED: an unwritable profile directory keeps the
	// session-scoped always it always had, the answer stands, and nothing about a
	// config file is put on a line in the middle of somebody's work.
	SaveApproval     func(tool string) error
	SaveBashApproval func(command string) error

	// SaveModel is the third seam of that shape, for the model in the status
	// line: the choice /model, the picker and the settings sheet's talk row all
	// arrive through (palette.go's switchModel), written where the NEXT launch
	// will read it back.
	//
	// It exists because this row was the odd one out. Every other model slot
	// resolves from somewhere a later launch can read — a variable, a row in the
	// profile — and the conversation model was live-only: a person picked a
	// model, worked in it, restarted, and was back on the built-in default with
	// nothing on screen to explain it.
	//
	// Nil is a surface whose model change lasts as long as the session does, and
	// says nothing about having saved it. The --host door passes nil deliberately
	// (chatv3_host.go): the far machine's engine reads the far machine's profile,
	// and writing this laptop's would change which model a LOCAL conversation
	// opens on because somebody switched models on a remote one.
	SaveModel func(model string) error

	// ApplyApprovals is the other direction of the pair above: those two write a
	// line, this one takes the rows as they now stand and hands them to the gate
	// the session is already running on. The permissions panel calls it after a
	// drop, because a line taken back that keeps answering until the next launch
	// is a line the person is entitled to think they removed.
	//
	// It takes nothing on purpose. The config is the record, and a caller that
	// passed its own reading of it would be handing the gate a second opinion —
	// which is how a panel and a gate come to disagree about what was answered.
	//
	// Nil is a surface whose drops reach the disk and wait for the next session,
	// and the panel's receipt says so instead of claiming the line is gone.
	ApplyApprovals func() error

	// Settings is the registry the panel edits, for a door that can wire the
	// live seams the registry asks for (the model slots, the divider, today's
	// spend). Nil builds one here over [Options.ProfileDir] with the two seams
	// this surface can answer honestly — see settings.go.
	Settings *config.Settings

	// Connections is the door onto the accounts this profile has connected
	// (connect.go). It answers /connect and the sign-in a pressed row starts.
	//
	// Nil is a surface that cannot manage them, which is what a headless frame
	// and a build whose door has not wired one both are: /connect says so rather
	// than opening an empty list. It does NOT disable the offer the session
	// raises — that path runs entirely on session events and the browser, and
	// needs no handle at all.
	Connections Connections

	// Harnesses is the sub-harness registry this surface lists under /harness
	// (harnesspanel.go). Nil is a surface that cannot show them, which is what a
	// headless frame and a door that has not wired one both are: the command
	// says so rather than opening an empty list.
	//
	// It does NOT disable the harness OFFER (harness.go): that path runs on
	// session events and the session's own registry, and needs no handle here.
	Harnesses *subharness.Store

	// RecentSessions is this directory's last conversations, most recent first.
	// Two surfaces are drawn from it: the welcome box's right column, which
	// asks ONCE as the surface opens and only when the conversation is empty,
	// and the resume picker (resume.go), which asks again every time it is
	// opened — an hour-old list would be missing the conversation the next
	// terminal has had since. It must not block: it is called on the keystroke
	// that opens the list. Nil draws "no recent sessions" in the box, and makes
	// /resume say the machine has no sessions yet.
	RecentSessions func() []Session

	// Resume opens one of them, by transcript path, and hands back the agent
	// for it. The surface closes the agent it was holding first. Nil makes the
	// welcome box's rows and /resume report that resuming is unavailable rather
	// than silently doing nothing — unless [Options.Open] is wired, which is the
	// same door answered whole and is preferred wherever both are there.
	//
	// A path another window is holding open comes back as
	// [session.ErrSessionLocked] and is REPORTED rather than worked around: a
	// person who picked a conversation by name means that one, and quietly
	// opening a different session under the name they chose would be the door
	// answering a question nobody asked.
	Resume func(file string) (Agent, error)

	// SharedAgent says this door's [Options.Fresh] and [Options.Resume] SELECT A
	// CONVERSATION IN PLACE on one handle, rather than building a second,
	// independent agent beside the one the surface is already holding.
	//
	// IT IS A STATEMENT ABOUT THE DOOR AND NOT ABOUT THE TRANSPORT, which is why
	// it is a field rather than something read off [Options.Host]. The engine
	// doors — `--host`, `--at`, and the ordinary `codeaf chat` that talks to this
	// machine's own engine over a socket — all hand back the SAME [remote.Agent]
	// from both seams, because that agent holds no state: it is a handle on
	// whichever conversation the engine currently has open (cmd/codeaf's
	// chatv3_host.go, internal/remote's Agent). The local in-process door builds a
	// real second agent and leaves this false. One of those three engine doors
	// names no host at all, so `Host == ""` is not the question.
	//
	// TWO THINGS THE SURFACE DOES DIFFERENTLY WHEN IT IS SET, and both are
	// correctness rather than taste:
	//
	//   - NOTHING GOES INTO THE KEEPER (keeper.go's [app.stow]). A conversation
	//     put there would be the same pointer as the one in front, now naming a
	//     different session — so the switcher drew the conversation just opened
	//     twice, under two names, and looking at the held one opened the wrong
	//     body. It would also leave a second reader draining the lanes of the
	//     conversation on screen.
	//   - THE CONVERSATION BEING LEFT IS NOT CLOSED (welcome.go's
	//     [app.openSession]). The surface opens the next conversation BEFORE
	//     closing the one it was holding, and on a shared handle those are the
	//     same object — so the close landed on the session that had just been
	//     opened. The engine already ends the previous conversation as part of the
	//     swap (internal/remote's Session.swap interrupts and closes it), so there
	//     is nothing left here to close.
	//
	// WHAT IT COSTS A PERSON is that these doors hold ONE conversation at a time:
	// opening another from home, the switcher or the search place swaps to it and
	// closes what was in front, rather than keeping it running beside. The surface
	// says so on the entry line ([oneConversationWord]) rather than letting
	// somebody discover it.
	SharedAgent bool

	// PickSession opens the resume picker over the first frame — `codeaf
	// resume`, which is this same surface asked to start by choosing. It is a
	// property of one launch and not of the profile, which is why it is a
	// field here rather than a settings row.
	PickSession bool

	// Landing says this launch is a person opening codeaf with no particular
	// conversation in mind, and that home may therefore greet them
	// (home.go's [app.landHome]).
	//
	// IT IS AN OPT-IN AND THAT IS THE POINT. Every door that is not a person
	// sitting down at a full terminal — `--once`, the headless frame, a test,
	// anything over `--host` — leaves it false and gets no home by saying
	// nothing, rather than by each of them remembering to switch one off. The
	// one door that sets it is `codeaf` and `codeaf chat` with no --session and
	// no --once (cmd/codeaf's chatv3.go).
	//
	// A person who NAMED a conversation is not landing: `--session <path>` and
	// `codeaf resume` both mean "that one", and a menu over the thing somebody
	// just asked for by name is the door second-guessing them.
	Landing bool

	// TakeOver is a conversation this launch could not open because another
	// window is holding its journal, and it is a TRANSCRIPT PATH.
	//
	// THE LAUNCH THAT MEETS A LOCK OPENS HOME RATHER THAN A NEW CONVERSATION.
	// `codeaf chat` in a folder whose conversation is open in another terminal
	// used to start a second one without a word, which is the fault this field
	// ends: the surface comes up on home with that row pointed and ARMED, the
	// foot line saying what one more enter will do, so continuing the
	// conversation that is already running costs a single keystroke.
	//
	// The surface still opens on the conversation the door DID build — a fresh
	// one in the same workspace — so esc out of home is an ordinary launch and
	// nothing is lost by ignoring the offer.
	//
	// Empty is every other launch. It is ignored over --host, where a window on
	// another machine cannot ask this one's holder for anything, and on a row
	// this window turns out to be holding itself.
	TakeOver string

	// Setup says this launch may open the once-only first-run questions — the
	// crew and spending rails, plus the key when no browser seam exists
	// (firstrun.go) — if the profile is missing them and has never seen them.
	//
	// IT IS AN OPT-IN FOR [Options.Landing]'s REASON: only a person sitting at
	// a full terminal with no particular conversation in mind is asked, and
	// every other door — --once, --host, the picker, a named session, a test,
	// a pipe — leaves it false by saying nothing. The one door that sets it is
	// `codeaf` and `codeaf chat` bare on a TTY (cmd/codeaf's chatv3.go). The
	// returning OpenRouter connection is governed separately by
	// [Options.ConnectOpenRouter], because a missing prerequisite is not a
	// first-run greeting and may stand over a resumed conversation too.
	Setup bool

	// ApplyAPIKey hands a key the person just gave — on the setup screen or in
	// the settings row — to the running session, so the next request rides it
	// without a relaunch. The surface has already written it to the profile
	// through the settings registry by the time this is called; this is the
	// live half only.
	//
	// Nil is a surface whose key lands on the next launch, and the setup says
	// nothing different: the profile is still the record. A test, and a door
	// with no process behind it, are that surface.
	ApplyAPIKey func(key string) error

	// ApplyModelSources hands a freshly connected or disconnected service set
	// to the process and its live conversations. Nil keeps the profile as the
	// record and applies the change on the next launch.
	ApplyModelSources func(modelsource.Set)

	// ConnectOpenRouter starts the default model provider's browser connection.
	// It is present only on a local interactive launch using codeaf's built-in
	// OpenRouter endpoint. With no key, its presence turns the key step into a
	// one-press browser trip and makes that step return on later launches until
	// the profile is connected. Nil keeps the direct paste box, which is the
	// honest path for a custom endpoint, a hosted surface, and a test with no
	// browser behind it.
	ConnectOpenRouter func(context.Context) (OpenRouterFlow, error)

	// ConnectCodex starts the Codex CLI-compatible browser sign-in used by the
	// Codex model-service row. Nil leaves that browser row without a local road,
	// as on a hosted surface; the whole /connect panel already explains why.
	ConnectCodex func(context.Context) (CodexFlow, error)

	// Linear is the SCREEN-READER TIER: one column, no animation, no hover,
	// ASCII markers instead of the pastel glyph set. Everything the surface says
	// it still says — the difference is that it says all of it in words and
	// characters a reader can announce, and nothing on the screen changes unless
	// something actually happened.
	//
	// THE SEAM: there is no cmd flag for this yet. The door (cmd/codeaf) owns
	// flags and this package owns rendering, so the field lands first and the
	// `--linear` that sets it lands with the door's next wave — one line there,
	// nothing here. A settings row is the other candidate and is the wrong one:
	// this is a property of the SESSION a person is opening (piping to a reader,
	// running under a braille display), not of the profile they keep.
	Linear bool

	// Input and Output exist so the surface can be booted without a terminal.
	Input  io.Reader
	Output io.Writer

	// Standing is the ambient side's seam: what home reads to draw the band of
	// items under a project, what a pause or a stop is written back through, and
	// what /status derives its `keeping watch` line from ([StandingSeam] says
	// what each function owes).
	//
	// The zero value is a surface with the ambient side OFF, and it is off the
	// way every optional capability here is off: home draws no item band at all,
	// the status line grows no segment, and /status says nothing about keeping
	// watch. Nothing half-works and nothing claims to.
	Standing StandingSeam

	// Teams is where the teams file and the Traffic logs are: the profile of
	// the machine the SESSION runs on, because the team tools a model calls
	// keep them there ([TeamsSeam] says what each function owes).
	//
	// The zero value is this machine's own profile ([Options.ProfileDir]),
	// which is every local launch. The --host door hands one that asks the
	// engine; over --host with no seam (an engine without the teams doors) the
	// window keeps no teams at all rather than keeping them here, where the far
	// session would never read them (host.go).
	Teams TeamsSeam

	// Link is what the door can tell this surface about the connection the
	// conversation is on the far end of: the sentence to draw while a dropped
	// link is being redialled, the empty round trip to measure on a slow clock,
	// the one-off news a redial discovered, and the questions raised while nobody
	// was attached ([LinkSeam] says what each function owes, and hostlink.go says
	// where each one lands on the screen).
	//
	// The zero value is a surface with no link to report, which is every LOCAL
	// session: no segment on the status line, no notice looked for, no question
	// asked about. Only the --host door fills it (cmd/codeaf's chatv3_host.go).
	Link LinkSeam

	// Width and Height are the size a headless driver is pretending to be.
	// A real terminal answers this itself and these stay zero; a pipe cannot
	// be asked, and a renderer with no size draws nothing at all.
	Width, Height int

	// Env is THE ONE PLACE THIS SURFACE READS THE ENVIRONMENT. Every fact the
	// surface takes from the shell that launched it — whether TERM names a
	// multiplexer (copymode.go's [tmuxTerm]), whether SSH_CONNECTION says the
	// terminal is on the far side of a link (link.go's [remoteLink]), which
	// emulator is running so a chord can be spelled its way (chords.go's
	// [detectChords]), and whether TERM has said enough for a path to be written
	// as an OSC 8 link at all (pathlink.go's [terminalTakesLinks]) — is read
	// through this closure at construction and nowhere else. So are the three
	// facts that used to be read at package boot: which colour profile the
	// palette and the markdown painter draw in (styles.go's [detectPalette],
	// markdown.go's [stylerFor]), and whether the process is inside WSL so a
	// Windows path can be translated (attach.go's [bootWSLPaths]).
	//
	// Nil is os.Getenv, which is what every door passes by saying nothing. The
	// field exists so that a TEST CAN HAND IT A TABLE: a suite that read the
	// developer's own TERM was a suite that wrote links inside tmux and none on
	// a CI runner with no TERM at all, and four assertions about the bytes on
	// the screen failed on exactly the machine where nobody was watching.
	Env func(string) string
}

// ErrandOrders is what the composer layer settled before the sentence left it,
// and it is the whole argument to [Options.Errand].
//
// THE THREE FACTS A TASK NEEDS BEFORE IT LEAVES ARE WHERE, ON WHAT, AND HOW
// MUCH (SCREEN 2e), so they travel together. Each is edited on the line that
// shows it and each is honoured by a real field of the session the door builds
// — the workspace it works in, the model its work runs on, the rail it stops at
// — because a figure a person set and nothing read would be worse than a figure
// they were never offered.
type ErrandOrders struct {
	// Dir is the folder the surface already made, under the standing root. The
	// transcript and every sidecar go inside it.
	Dir string
	// Workspace is the project this errand is about: the destination the layer's
	// `alt+w` cycled to, the project the cursor was on, or the person's home
	// directory when it was on none.
	Workspace string
	// Model is what the WORK this errand hands out runs on — the execution slot
	// (config's ModelSlotFor("work")), chosen on the layer's `alt+o`. Empty keeps
	// whatever the launch bound, which is the ordinary case.
	Model string
	// CapUSD is the most this errand may spend before it stops and asks. Zero is
	// the launch's own rail and therefore usually no cap at all; the layer never
	// sends zero, because the line a person read said a figure.
	CapUSD float64
}

// sigQuitMsg is an outside request to leave, on its way to [app.quit]. See
// [forwardSignals] for why this surface catches those requests itself.
type sigQuitMsg struct{}

// StandingSeam is everything this surface needs from internal/standing, as
// FUNCTIONS rather than as a store.
//
// It is functions for the reason [Options.Models] is one: the door owns where
// the store lives and how it is opened, and a test owns neither. Handing the
// surface a *standing.Store would make "home with three items on it" a test
// that writes JSON documents into a temp directory to assert a row's spacing.
//
// EVERY FIELD IS INDEPENDENTLY OPTIONAL. A door that can list items but cannot
// install an OS timer wires Items and leaves Watch nil, and what a person then
// sees is item rows and no `keeping watch` line — which is exactly the truth.
type StandingSeam struct {
	// Items answers the items belonging to one workspace, in whatever order the
	// store holds them; this surface applies its own triage order
	// (homestanding.go's [standTriage]). It must NOT block: home calls it on
	// every three-second beat and on the keystroke that opens the screen.
	//
	// Nil is a home with no item band, which is the ambient side switched off.
	Items func(workspace string) []standing.Item

	// All answers EVERY standing item this machine holds, in the store's own
	// order, and each item carries the workspace it belongs to.
	//
	// IT EXISTS BECAUSE A PAGE THAT WANTS THE WHOLE SET WAS ASKING Items ONCE
	// PER PROJECT. Items is the store's List filtered down to one workspace, so
	// a page joining ids against titles across five projects paid five walks of
	// the standing root and five parses of every document on the machine to
	// build one map — a cost that grows as projects × orders, which PERF.md
	// does not allow of anything a keystroke or a beat can reach. One question
	// asked once is the same answer.
	//
	// Like [StandingSeam.Items] it must NOT block: the spend place asks it on
	// the way in and on the three-second beat.
	//
	// Nil is a surface with no way to ask the question at all — a connection,
	// whose door answers by workspace and has no "every workspace" on the wire
	// — and a caller then names nothing rather than fanning out into N reads.
	All func() []standing.Item

	// Save writes one item back — the pause and the stop keys on a home row, and
	// nothing else on this surface. It returns the write's error and home says
	// so on its own message line rather than swallowing it: a row that redrew as
	// paused over a store that refused the write would be the screen lying about
	// the disk.
	//
	// Nil is a home where `p` and `s` say the change cannot be made here.
	Save func(item standing.Item) error

	// Running reports whether some process is CHECKING OR FIRING one item at
	// this instant, by id, and what it is doing ([standing.RunningMark]). It is
	// separate from the item document because it is not a fact the document
	// holds: the pass may be happening in another window, or in the operating
	// system's timer with no window open at all, and what says so is a marker
	// the store writes and doubts (internal/standing's running.go).
	//
	// IT ANSWERS THE MARK AND NOT A BOOL because the card says which half of a
	// pass it caught and how long ago it started — `● checking now · since 4s`
	// — and a surface that were handed only a yes would have to invent both.
	//
	// Nil answers no for everything, and a home where no row ever wears `●` is
	// honest: the glyph is a claim about right now, and a surface with no way to
	// ask must not make it.
	Running func(id string) (standing.RunningMark, bool)

	// Watch is what /status prints under `keeping watch`, derived and never
	// asserted ([standing.WatchStatus]). The bool is whether there is an answer
	// at all — a build with no OS timer support, a remote engine — and a false
	// prints nothing, which is the emptiness law applied to a whole line.
	Watch func() (standing.WatchStatus, bool)

	// BackgroundTold reports whether this machine has already been told, once,
	// that background checks are on — the marker the first standing item writes
	// under the store root (internal/session's BackgroundTold).
	//
	// /status uses it for one word and one word only — why nothing is checking.
	// "Nothing has ever stood here" and "the row is off" are two different
	// situations for the person in front of the screen, and the first has a
	// different move in it from the second.
	//
	// Nil is a surface that cannot tell them apart, and it says neither.
	BackgroundTold func() bool

	// Background is this machine's timer itself, and it is the `background
	// checks` settings row's own hand: what the row reads is derived from
	// [standing.Watch.Status], and turning the row installs or removes it
	// (internal/config's backgroundRow).
	//
	// It is a second field beside [StandingSeam.Watch] rather than a widening
	// of it because they answer different questions. Watch is a READING for the
	// /status line and may one day come from somewhere this process cannot
	// reach; this is a timer on THIS machine that can be turned on and off, and
	// nil means there is none to turn — the row is then absent from the sheet
	// entirely.
	Background standing.Watch

	// Ticking reports that THIS PROCESS is running the standing pass itself —
	// the every-five-minutes walk any open window takes when it gets the store's
	// lock (cmd/codeaf's startStandingTicks).
	//
	// IT IS WHAT LETS /status SAY THE AMBIENT SIDE IS NOT BEING CHECKED. Without
	// it the line could only say `installed` or assert `while a window is open`
	// about a window it had not asked, and a person asking /status about a
	// machine where nothing is keeping time would be told a window was.
	//
	// Nil answers no, on [StandingSeam.Running]'s law: this is a claim about
	// right now, and a surface with no way to ask must not make it.
	Ticking func() bool

	// Runs is the standing ledger since a moment, summed per item id — how many
	// times each thing fired and what it spent ([standing.Store.RunsSince]). It
	// is what a card means by `ran 3 times this week`.
	//
	// IT ANSWERS THE WHOLE MACHINE IN ONE CALL, deliberately: the ledger is one
	// file per day, so a surface asking item by item would open the same seven
	// files once per row it drew. The surface reads it on home's own beat and
	// sums whichever ids the card it is drawing owns.
	//
	// It must not block — it is a walk of at most a month of small files — and
	// nil is a surface that simply draws no weekly line, which is the emptiness
	// law applied to a fact nobody can answer.
	Runs func(since time.Time) map[string]standing.Spend

	// SetEffort moves the rung one item's firings and its checks think at
	// (internal/standing's [Store.SetStandingEffort]) — `alt+e` on that item's
	// card, and nothing else on this surface.
	//
	// IT IS ITS OWN FUNCTION AND NOT A FIELD ON THE ITEM [StandingSeam.Save]
	// TAKES, and the store says why in full: Save writes a whole document, so a
	// surface holding an item it read a beat ago would write back the check
	// results, the spend and the next-due that the ticker has moved since — and
	// quietly undo a firing to change a word nobody was looking at. The rung is
	// a read-modify-write under the item's own lock and it happens in the store.
	//
	// Nil is a home where the key says the change cannot be made here, in the
	// same sentence the pause and stop keys already say it in
	// ([homeItemNoStore]).
	SetEffort func(id string, rung effort.Rung) error
}

// Run opens the surface and blocks until it closes. A cancelled context closes
// it the same way ctrl+c does.
func Run(ctx context.Context, opts Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.ReadCredits != nil {
		var closeReads func()
		opts.ReadCredits, closeReads = ownedCreditReader(ctx, opts.ReadCredits)
		defer closeReads()
	}
	// THE SIGNAL HANDLER IS OURS, and [tea.WithoutSignalHandler] is what takes
	// Bubble Tea's out of the way — see [forwardSignals] for what was wrong with
	// the one it installs.
	program := []tea.ProgramOption{tea.WithContext(ctx), tea.WithoutSignalHandler()}
	if opts.Input != nil {
		program = append(program, tea.WithInput(opts.Input))
	}
	if opts.Output != nil {
		program = append(program, tea.WithOutput(opts.Output))
	}
	if opts.Width > 0 && opts.Height > 0 {
		program = append(program, tea.WithWindowSize(opts.Width, opts.Height))
	}
	surface := newApp(ctx, opts)
	p := tea.NewProgram(surface, program...)
	// AND THE ENGINE IS GIVEN SOMEWHERE TO PUT ITS NEWS, and the loop a door to
	// be rung through that never waits for it ([listenForNews], doorbell.go). They
	// are registered for the life of the program and taken down when it ends, so
	// a second surface in one process cannot inherit the first one's desk.
	defer listenForNews(surface.news)()
	defer surface.news.close()
	defer surface.leaving.close()
	// AND THE DOOR LINE ENDS WITH THE WINDOW, after what is already in it has
	// been asked (offloop.go): a person's last keystroke before they close a
	// window is still an answer somebody gave.
	defer surface.doorLine.close()
	defer forwardSignals(p, surface.leaving)()
	_, err := p.Run()
	// THE TAB IS HANDED BACK ON EVERY ROAD OUT, after the program has stopped
	// writing and whatever stopped it (title.go's [titleFarewell]).
	out := opts.Output
	if out == nil {
		out = os.Stdout
	}
	titleFarewell(out, surface)
	return err
}

// listenForNews is where the engine's news lands in this process, and it
// returns the function that puts the previous readers back.
//
// TWO READERS, ONE WAY IN. The lane news says what the last answer DID — which
// machine answered, whether a rescue went out while somebody was waiting — and
// the phase news says what the request in flight is doing right now:
// connecting, waiting for the first word, thinking, paced, switching, or
// running a tool between requests. Only the layer holding the stream can see
// either, and the arrow between that layer and this one only points this way,
// so both are pushed (internal/session's lanenews.go and phasenews.go) and both
// land here: filed on this package's desk, then a frame asked for.
//
// THE FRAME IS ASKED FOR THROUGH THE DOORBELL AND NEVER WAITED FOR (doorbell.go). These
// readers run on whatever goroutine the news arrived on — a provider's stream
// between two deltas, or the wire client's reader between two frames — and
// internal/session documents that neither may be held by a reader. The second
// one is the defect doorbell.go opens with: a reader parked on a busy loop held the
// very frame that loop was waiting for, and an answered question froze the
// window for ten seconds. A status line that has changed is still a frame that
// has to be drawn, and nothing else is going to ask for one: a rescue drawn at
// the next keystroke is a rescue nobody saw, and a clock that only moved at the
// next keystroke is a clock nobody was watching.
func listenForNews(door *doorbell) (restore func()) {
	previousLane := session.OnLaneNews(func(news session.LaneNews) {
		PostLaneNews(LaneNews{
			Model:  news.Model,
			Lane:   news.Lane,
			Alt:    news.Alt,
			Winner: news.Winner,
			TTFT:   news.TTFT,
			Rate:   news.Rate,
			Hedged: news.Hedged,
			Trying: news.Trying,
			Reason: news.Reason,
			Failed: news.Failed,
			Role:   news.Role,
			// AND WHAT THE SIGHTING IS ABOUT, without which every node's answer
			// lands on the conversation's row: the desk keys on this
			// (lanes.go's [PostLaneNews], phase.go's [newsDeskKeys]).
			Subject: news.Subject,
			// AND WHOSE IT IS, which is the name the conversation's own sighting
			// is filed under first — so a model that moved between the engine
			// and this window cannot hide which machine answered.
			Session: news.Session,
			At:      news.At,
		})
		door.ring()
	})
	previousPhase := session.OnPhaseNews(func(news session.PhaseNews) {
		PostPhaseNews(news)
		door.ring()
	})
	return func() {
		session.OnPhaseNews(previousPhase)
		session.OnLaneNews(previousLane)
	}
}

// forwardSignals turns an outside request to leave into a message the surface
// can act on, and returns the function that stops listening.
//
// WHY THIS EXISTS AT ALL. Bubble Tea's own handler (its tea.go) answers SIGINT
// by pushing a tea.InterruptMsg into the program, and the loop answers THAT by
// returning ErrInterrupted without ever calling Update. So the surface never
// heard the signal: [app.quit] did not run, the unsent draft was not written to
// disk, the session was not closed, and the door printed
// "error: program was interrupted" and exited 1. A person who typed `kill -INT`
// at a hung terminal, or whose terminal was not in raw mode so that ^C arrived
// as a signal rather than as a keystroke, lost their draft and got an error for
// a perfectly ordinary way to leave.
//
// SO THE SIGNAL BECOMES A MESSAGE INSTEAD OF A RETURN. sigQuitMsg is routed in
// [app.Update] to the same [app.quit] ctrl+c calls, which writes the
// draft, closes the session and returns tea.Quit — the ordinary exit, with a nil
// error and status 0. Sending InterruptMsg ourselves would have reproduced
// exactly the bug; tea.QuitMsg would exit cleanly but skip [app.quit] and take
// the draft with it. This is the one path of the three that both runs and exits
// zero.
//
// SIGHUP IS ON THE SET TOO. It is what arrives when a terminal window closes or
// an ssh connection drops, and it must reach the same draft-writing, session-
// closing road instead of ending the process underneath every defer.
//
// A SECOND SIGNAL ENDS THE PROCESS AT ONCE. Before that forced exit, this door
// hands the screen back on a bounded best effort; putting a tidy terminal ahead
// of a person who has asked twice to leave would turn the escape hatch into the
// same trap it is there to break.
func forwardSignals(p *tea.Program, leaving *doorbell) func() {
	return leave.On(
		// THROUGH THE DOORBELL, NEVER Program.Send (doorbell.go). A signal that arrives
		// while the loop is busy is a token in the slot, taken the moment the
		// loop is free; a handler parked on Send is a goroutine a busy loop can
		// hold, and the second signal's forced exit is the only thing that
		// would be left to end it.
		leaving.ring,
		func() { _ = p.ReleaseTerminal() },
	)
}
