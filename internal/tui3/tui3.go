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

	// Settings is the registry the panel edits, for a door that can wire the
	// live seams the registry asks for (the model slots, the divider, today's
	// spend). Nil builds one here over [Options.ProfileDir] with the two seams
	// this surface can answer honestly — see settings.go.
	Settings *config.Settings

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
