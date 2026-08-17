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
// (cmd/aforge owns config resolution and session files); it is handed one, and
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
//	resume.go   the session picker: /resume, and what `aforge resume` opens on
//	models.go   where the model list comes from, and never from the network
//	settings.go the settings panel: tabs over internal/config's own registry
//	welcome.go  the box an empty session opens with, and the sessions in it
package tui3

import (
	"context"
	"io"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// Agent is the slice of *session.Agent this surface uses. It is an interface
// so the surface can be driven in a test by a scripted agent that answers
// without a provider — the same reason session.Completer is one.
type Agent interface {
	// Submit runs one turn and streams its events. Submitting while a turn is
	// in flight steers that turn rather than starting a second one.
	Submit(ctx context.Context, text string) (<-chan session.Event, error)
	// SubmitImage is Submit with pictures: one user message carrying the text
	// and the images, and then a normal turn. It refuses — with an error and no
	// stream — when the model in use cannot see (session.Config.SupportsImages,
	// wired in cmd/aforge) or when the images are too large, which is why the
	// surface keeps the attachment tray until this has answered (attach.go).
	SubmitImage(ctx context.Context, text string, images []session.Image) (<-chan session.Event, error)
	// Interrupt cancels the in-flight turn, keeping its partial reply.
	Interrupt()
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
	// "medium" or "high" — for any model id, not only the one in use.
	//
	// The pair is per-model rather than per-session because the picker sets a
	// level on the row under the cursor, which is usually not the model running:
	// dialling a model up and then deciding not to switch to it is a normal thing
	// to do in a list, and the level is waiting when the switch finally happens.
	// The session holds the map, so it survives the overlay closing and /new
	// starts empty (internal/session's agent.go).
	ReasoningFor(model string) string
	// SetReasoningFor sets it. An empty level is off, which is "send nothing
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
	// ResolveConnect answers one session.EventConnectAsk: whether aforge may
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
}

// Options configures one surface.
type Options struct {
	// Agent is the conversation this surface shows. Required.
	Agent Agent

	// Fresh builds a replacement agent on the same Config with a new session
	// file, and returns it with that file's path. It is what /new calls. Nil
	// makes /new report that it is unavailable rather than pretending.
	Fresh func() (Agent, string, error)

	// Workspace is the directory the agent works in; its base name is the
	// place shown in the status line. Empty takes the process's cwd.
	Workspace string

	// Host is the machine the agent is on, when it is not this one: the ssh
	// destination `aforge chat --host devbox` was given. Empty is a local
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

	// SessionFile is the transcript being written, shown by /help and /new.
	// Empty means the conversation is memory-only.
	SessionFile string

	// Resumed says the session file was picked up rather than created, so the
	// surface can say so in its first line.
	Resumed bool

	// Notice is one sentence the door wants on the entry notice line — the
	// place a resumed session is announced. It is how "the session you asked
	// for is open somewhere else, so this is a new one" reaches the person who
	// needs to know it, without cmd/aforge printing to a screen the surface is
	// about to take over.
	Notice string

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

	// Models answers what /model can switch to. It is a function and not a
	// slice because the door's list may be warming: it is called the moment the
	// picker opens, so a catalog that resolved after boot is on offer, and it
	// MUST NOT block — a picker that waits on a fetch is a picker that answered
	// a question with a spinner. Nil, or an empty answer, falls through to
	// ~/.aforge/v3/models.json and then to [BuiltinModels] (see models.go).
	Models func() []Model

	// ProfileDir is the profile the settings panel reads and writes — the same
	// directory internal/config resolves every other row out of. Empty is the
	// default profile (~/.aforge), which is what the door passes when it has
	// not been told otherwise.
	ProfileDir string

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
	// than silently doing nothing.
	//
	// A path another window is holding open comes back as
	// [session.ErrSessionLocked] and is REPORTED rather than worked around: a
	// person who picked a conversation by name means that one, and quietly
	// opening a different session under the name they chose would be the door
	// answering a question nobody asked.
	Resume func(file string) (Agent, error)

	// PickSession opens the resume picker over the first frame — `aforge
	// resume`, which is this same surface asked to start by choosing. It is a
	// property of one launch and not of the profile, which is why it is a
	// field here rather than a settings row.
	PickSession bool

	// Linear is the SCREEN-READER TIER: one column, no animation, no hover,
	// ASCII markers instead of the pastel glyph set. Everything the surface says
	// it still says — the difference is that it says all of it in words and
	// characters a reader can announce, and nothing on the screen changes unless
	// something actually happened.
	//
	// THE SEAM: there is no cmd flag for this yet. The door (cmd/aforge) owns
	// flags and this package owns rendering, so the field lands first and the
	// `--linear` that sets it lands with the door's next wave — one line there,
	// nothing here. A settings row is the other candidate and is the wrong one:
	// this is a property of the SESSION a person is opening (piping to a reader,
	// running under a braille display), not of the profile they keep.
	Linear bool

	// Input and Output exist so the surface can be booted without a terminal.
	Input  io.Reader
	Output io.Writer

	// Width and Height are the size a headless driver is pretending to be.
	// A real terminal answers this itself and these stay zero; a pipe cannot
	// be asked, and a renderer with no size draws nothing at all.
	Width, Height int
}

// Run opens the surface and blocks until it closes. A cancelled context closes
// it the same way ctrl+c does.
func Run(ctx context.Context, opts Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	program := []tea.ProgramOption{tea.WithContext(ctx)}
	if opts.Input != nil {
		program = append(program, tea.WithInput(opts.Input))
	}
	if opts.Output != nil {
		program = append(program, tea.WithOutput(opts.Output))
	}
	if opts.Width > 0 && opts.Height > 0 {
		program = append(program, tea.WithWindowSize(opts.Width, opts.Height))
	}
	_, err := tea.NewProgram(newApp(ctx, opts), program...).Run()
	return err
}
