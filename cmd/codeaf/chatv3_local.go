package main

// ── `codeaf chat` through this machine's own session host ───────────────────
//
// This is the third dialer and the first one with no machine in it: the surface
// runs here, the session runs in this workspace's session host
// (internal/enginehost), and between them is a unix socket rather than an ssh
// child or a relay tunnel.
//
// It is the ordinary road now, which is the whole point — the work a person
// starts goes on when the terminal closes, and the next `codeaf chat` here sits
// back down in the same conversation. [v3HostRoad] is the rule, and it says
// which launches keep the in-process door instead: onboarding, --once, --debug
// and --no-host.
//
// Everything below the dial is the ssh door's own code. The client, the welcome,
// the surface's options and the headless run are [openChatV3Host]'s, called with
// an empty machine name — the seam this build spells as "linked, local".

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/subharness"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/tui3"
	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

// localLaunch is one launch that goes through this machine's own session host.
//
// It carries no target: the machine is this one, and the workspace has already
// been resolved by the rule that decided to come here ([v3HostRoad]) rather
// than parsed out of a flag.
type localLaunch struct {
	// workspace is the directory whose host holds this conversation, absolute
	// and resolved — the same answer [openV3Launch] would have reached, so both
	// doors agree about which project this terminal is in.
	workspace string
	// session is --session, meaning here exactly what it means to the engine:
	// the transcript to open, and empty is this workspace's latest.
	session string
	// model and level are --model and --reasoning, carried in the hello so the
	// ENGINE opens the session on them — the same law the ssh door states.
	model string
	level string
	// once is --once: one message, printed, no terminal ownership.
	once string
	// pick is `codeaf resume`: the same surface, opened on the session picker.
	pick bool
	// shape is --yolo, --no-compact, --one-model and the two ceilings, carried
	// so the ENGINE builds the session with them. It is only ever honoured on a
	// conversation this hello opens; joining one that is already running gets
	// that conversation's shape back on the welcome and is refused here
	// ([hostShapeTaken]).
	shape *remote.LaunchShape
	// restart is filled by the surface and read only after this door's
	// connection cleanup has run.
	restart *codeupdate.Plan
}

// localLink is the dialer for a conversation on this machine.
//
// IT IS A [remote.Dialer] AND NOTHING MORE, which is what makes the rest of the
// road free: internal/remote speaks its protocol over any io.ReadWriteCloser,
// and a unix socket is one. There is no process to hold and no stderr to read —
// the host is nobody's child, by construction ([enginehost.Spawn] detaches it) —
// so this type is a workspace and a method.
type localLink struct {
	workspace string
	// mu guards note, which is written by [localLink.dial] and read by the door
	// after the client has said hello. The redial loop calls dial again from a
	// goroutine of its own, so the two really can meet.
	mu sync.Mutex
	// note is the one line the STALE-HOST question left for the person, and it
	// is the reason dial has any state at all. A host one build behind that is
	// still holding work is attached to rather than refused (engine.go says why),
	// and a surface that never mentioned it would be a window running against a
	// binary that is not the one on disk with nothing on screen saying so.
	note string
}

// said is the note the last dial left, and "" when it left none.
func (l *localLink) said() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.note
}

// noteDown records the note this dial is leaving. It is its own method so the
// lock is let go before the attach below it, which is the slow half of the
// dial and takes no part of this mutex.
func (l *localLink) noteDown(note string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.note = note
}

// dial hands back a connection to this workspace's host, starting one when
// nothing answers. It is called again by the redial loop, which is exactly what
// it is for: a host that retired under a surface is replaced by a fresh one on
// the next attempt rather than ending the window.
//
// THE STALE HOST IS ASKED ABOUT FIRST, on the same terms `codeaf engine` asks
// (engine.go's [clearStaleEngineHost]): a host outlives the binary that started
// it, so a rebuilt codeaf can meet an older one still holding the socket. It
// retires if it is holding nothing, and otherwise the person is told in words
// which command lets go of it — one sentence, already written, and not a splice
// onto a build that speaks a different protocol.
func (l *localLink) dial() (io.ReadWriteCloser, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	note, err := clearStaleEngineHost(l.workspace)
	if err != nil {
		return nil, err
	}
	l.noteDown(note)
	return enginehost.Attach(l.workspace, func() error {
		return enginehost.Spawn(l.workspace, self, "engine", "--daemon", "--workspace", l.workspace)
	})
}

// hostShapeTaken is the one refusal this door answers with rather than a
// failure: the conversation on the socket is already open, and it is not shaped
// the way this launch asked for. Nothing is overwritten — a running conversation
// belongs to whoever opened it — so the caller takes the in-process road and
// says this sentence on the entry notice.
type hostShapeTaken struct{ sentence string }

func (h *hostShapeTaken) Error() string { return h.sentence }

// hostUnreachable is the floor: no host could be reached or started. The
// conversation opens in this process instead, carrying the reason so nothing is
// swallowed.
type hostUnreachable struct{ reason string }

func (h *hostUnreachable) Error() string { return h.reason }

// hostFallbackReason is the clause a person reads after "this conversation
// opened in this terminal instead, and ends with it: ". The two host failures
// written in machinery words are translated here, at the surface that knows
// somebody will read them; every other reason passes through because stale and
// older-host sentences have already been written for a person.
func hostFallbackReason(err error) string {
	switch {
	case errors.Is(err, enginehost.ErrSocketPathTooLong):
		return fmt.Sprintf("codeaf's state folder is a longer path than the %d bytes a socket may be named in — CODEAF_HOME moves it somewhere shorter", enginehost.SocketLimit)
	case errors.Is(err, enginehost.ErrNoHostAnswered):
		return "nothing on this machine came up to hold it in the background"
	default:
		return err.Error()
	}
}

// openChatV3Local is the launch.
func openChatV3Local(launch localLaunch) error {
	if launch.pick && launch.once != "" {
		return fmt.Errorf(`codeaf resume opens the session picker; for one headless message use: codeaf chat --once "text"`)
	}
	link := &localLink{workspace: launch.workspace}
	// THE MACHINE NAME IS EMPTY AND THAT IS THE WHOLE SIGNAL. internal/remote
	// already says "the engine" where it would say a machine's name (its
	// [Client.where]), and the surface reads an empty [tui3.Options.Host] as a
	// conversation with no far machine in it — no `via` segment, no machine in
	// front of a path, and `another window` where a connection would name a
	// host.
	hello := remote.Hello{
		Headless:  launch.once != "",
		Workspace: launch.workspace,
		Session:   launch.session,
		Model:     launch.model,
		Level:     launch.level,
		Launch:    launch.shape,
	}
	client, err := remote.Roam("", hello, remote.Roaming{Dial: link.dial})
	// A HOST REFUSAL NEVER PUSHES THE WINDOW IN-PROCESS. The engine answered,
	// and it answered that the conversation this launch asked for is held by
	// something else — a window on an older build, one that has stopped
	// answering, a `--no-host` terminal in the same folder. That is a fact about
	// ONE JOURNAL and not about the engine, so the road stays: this launch opens
	// a conversation of its own THROUGH the engine and lands on home with the
	// held row armed, which is the same landing the in-process door gives
	// ([v3TakeOverInstead]) and a better one, because a window on this road has
	// [tui3.Options.EngineAnswers] wired and one enter on that row moves the
	// conversation at once rather than asking a window to let go of it.
	//
	// Falling to the in-process door here is exactly the defect: that door mints
	// a fresh conversation with no engine behind it, so the armed row's enter
	// took the asking road and a daemon never answers it.
	takeOver, again := localAskAgainAfterRefusal(launch, err)
	if again {
		hello.New = true
		client, err = remote.Roam("", hello, remote.Roaming{Dial: link.dial})
	}
	if err != nil {
		// A host that cannot be reached or started is not the end of the
		// launch: the in-process door is the floor, and the reason travels with
		// the fallback so a stale host's own sentence is still read.
		return &hostUnreachable{reason: hostFallbackReason(err)}
	}
	closeClient := func() { _ = client.Close() }
	defer func() { closeClient() }()

	agent := client.Agent()
	welcome := client.Welcome()
	// The shape the engine built, against the shape that was asked for. They
	// differ when this hello joined a conversation that was already open, and
	// the flags are real in this process, so the launch goes there rather than
	// running under a posture nobody asked for.
	if !welcome.Launch.Same(launch.shape) {
		_ = client.Close()
		return &hostShapeTaken{sentence: hostShapeSentence(launch.shape, welcome.Launch)}
	}
	correctHostChoices(agent, hostLaunch{model: launch.model, level: launch.level}, welcome)

	if launch.once != "" {
		// A HEADLESS MESSAGE HAS NO TRANSCRIPT TO CARRY THE NOTICE. The stale-host
		// line the dial left ([localLink.said]) is joined into the surface's entry
		// notice below, and --once never opens a surface — so it goes to stderr,
		// the channel a scripted caller reads, leaving stdout to the answer alone
		// (the same split engine.go makes between the protocol and the person).
		if said := link.said(); said != "" {
			fmt.Fprintln(os.Stderr, said)
		}
		return runHostOnce(agent, launch.once)
	}
	// ANOTHER CONVERSATION IS ANOTHER CONNECTION TO THE SAME HOST, on the same
	// socket, and the host has always held a map of them rather than one
	// (internal/enginehost). A conversation of this window's own says so in the
	// hello ([remote.Hello.New]); one that names a transcript the host is already
	// running is attached to rather than reopened.
	//
	// New tabs carry the same launch settings, including the interactive door.
	// Omitting them makes a later ordinary launch reject its own conversation
	// as incompatible and fall back onto the journal the engine still holds.
	fleet := newEngineFleet("", client, nil, func(ask engineAsk) (*engineConn, error) {
		beside := &localLink{workspace: ask.workspace}
		next, err := remote.Roam("", localBesideHello(ask, launch), remote.Roaming{Dial: beside.dial})
		if err != nil {
			return nil, err
		}
		return &engineConn{client: next}, nil
	})
	// The fleet owns every connection now, the boot one included, so the door's
	// own defer hands the job over rather than closing the client twice.
	closeClient = fleet.closeAll
	options, settings := hostOptions(fleet, welcome, launch.pick)
	localDoors(&options, welcome, settings)
	options.ImplicitTalk = strings.TrimSpace(launch.model) == "" && strings.TrimSpace(env.Get(config.ModelEnv)) == "" && config.ChatModelAt(options.ProfileDir) == ""
	// AND A PLAIN LAUNCH IS STILL GREETED BY HOME ON THIS ROAD. Whether somebody
	// is being greeted is one fact — a person opened codeaf with no particular
	// conversation in mind — and [tui3.Options.Landing] is the only place the
	// surface reads it (internal/tui3's [app.landHome]). It was set on the
	// in-process door alone, so the day THIS road became the ordinary one for an
	// interactive launch ([v3TakeHostRoad] stopped requiring a host that was
	// already answering), every plain `codeaf` stopped being greeted and sat down
	// in a conversation instead: the same launch, two roads, two behaviours.
	//
	// It is spelled here rather than inside [hostOptions] for [Options.EngineAnswers]'
	// reason — the road knows what --host cannot. A remote launch is not greeted
	// at all (its world is not an answer yet by the first frame, which is what
	// [app.landHome] refuses on), and a launch that NAMED a conversation or asked
	// for the picker means that one, exactly as the in-process door reads it.
	options.Landing = strings.TrimSpace(launch.session) == "" && !launch.pick
	// AND THE CONVERSATION THIS LAUNCH COULD NOT HAVE, POINTED AT AND ARMED.
	// It is "" on every ordinary launch; it is set only by the refusal above,
	// and the surface lands on home with that row under the cursor
	// (internal/tui3's takeover.go).
	options.TakeOver = takeOver
	options.Restart = launch.restart
	// AND WHAT THE DIAL FOUND ON THE WAY IN, on the same line the engine's own
	// welcome speaks (chatv3_host.go's [hostEntryNotice]). It is joined here
	// rather than inside that function because it is a fact about THIS ROAD's
	// dial and not about the conversation the engine opened.
	options.Notice = joinNotice(options.Notice, link.said())
	// AND HOME CAN ASK SOMETHING WITHOUT OPENING A CONVERSATION. `ask here` is
	// the second action row on home, and it is answered by an agent this process
	// builds against a folder under the standing root ([localErrandDoor],
	// chatv3_exchange.go) — so before this line, every launch that took the
	// engine road met `this window cannot ask from home`, which since #653 made
	// this road the ordinary one was every ordinary launch.
	//
	// IT IS BOUND ON THIS ROAD AND NO OTHER, like [tui3.Options.EngineAnswers]
	// above it, and for the same fact: the engine here is a process on THIS
	// machine, so a session opened in this terminal writes to the disk the
	// errand's own folder and standing store live on. Over --host the folder
	// would be made on the laptop and the work would run against the wrong
	// machine, so there the seam stays absent and home says so once.
	errand, closeErrands := localErrandDoor(launch, welcome)
	// AND WHATEVER IT OPENED IS CLOSED HOWEVER THIS SURFACE RETURNS, beside the
	// fleet's own close above: an errand is a real session with a real journal,
	// and a Run that leaves by any other road than /quit still owes it a flush.
	defer closeErrands()
	options.Errand = errand
	options.StandingRoot = v3StandingRoot()
	// AND A QUESTION IN ANOTHER WINDOW CAN BE ANSWERED FROM HOME. The answer is
	// left in that session's OWN FOLDER and the session picks it up on its
	// presence heartbeat (internal/session's answers.go), and home draws the chips
	// only where there is a door to leave one through ([app.answeringHere] covers
	// this window's own conversation and nothing else). It was set on the
	// in-process door alone, so on this road home said which conversation was
	// stopped on somebody and offered no way to answer it — the sign post without
	// the other half of the band it is written beside.
	//
	// THIS ROAD AND NO OTHER, for [tui3.Options.EngineAnswers]' reason: the folder
	// is on this machine's disk, which over --host it would not be.
	options.Answer = session.WriteAnswer
	// AND THE TASKS PAGE CAN LOOK INTO THE CONVERSATIONS NEXT DOOR. It is bound
	// here rather than inside [hostOptions] because it is a second DIAL of this
	// road and not a use of this client's connection, and this is the door that
	// knows the road (chatv3_taskowner.go says what makes it safe).
	options.OpenTaskOwner = localTaskOwnerDoor(welcome.Workspace)
	// AND IT CAN SEE THEM AT ALL. The rows that door sits behind are minted from
	// the other conversations' presence files, which the agent this surface
	// holds — a connection — cannot read; the engine is on this machine, so the
	// files are on this disk beside the transcript the surface was handed, and
	// the reading is taken straight off it. Without this the door above was
	// never reached from a real window.
	options.Elsewhere = session.ElsewhereOf
	// AND HOME CAN TELL AN ENGINE FROM A WINDOW. It is bound on THIS road and no
	// other, which is the absence law rather than an oversight: --host has its
	// holder on this laptop and its journal on the far machine, and the in-process
	// door has no engine to ask about. Both keep the road they had
	// ([tui3.Options.EngineAnswers]).
	options.EngineAnswers = v3HostAnswers
	// The draft this terminal has half-typed is keyed by the workspace and NOT
	// by a machine, because there is no machine: a person who takes the host
	// road today and the in-process door tomorrow is in the same place both
	// times, and a key with an empty machine glued to the front of it would
	// hide their own unsent words from them.
	if dir, err := v3Dir(); err == nil {
		options.DraftFile = tui3.DraftFile(dir, welcome.Workspace)
	}
	return runSurface(context.Background(), options)
}

// localDoors puts this machine's stores back onto the surface assembled by the
// connection road.
//
// [hostOptions] is deliberately conservative because its ordinary caller is a
// surface looking at ANOTHER machine: a browser, profile write or registry read
// in that process would land on the wrong disk. This road uses the same builder
// but its engine, profile, registry and browser are all here. Keeping the patch
// in one named function makes that difference reviewable and keeps --host and
// --at on the absence they still require.
func localDoors(options *tui3.Options, welcome remote.Welcome, settings config.Config) {
	if options == nil {
		return
	}
	profileDir := strings.TrimSpace(welcome.ProfileDir)
	if profileDir == "" {
		// An older engine cannot say which profile it resolved, so keep the
		// surface's own answer. Linked-local launch already retires a daemon of
		// another build, making this a compatibility floor rather than an
		// ordinary split-profile road.
		profileDir = settings.ProfileDir
	}
	options.ProfileDir = profileDir
	if options.Teams.Load != nil {
		// The linked-local engine keeps its profile on this machine, so its closed
		// teams' packets can be read here without borrowing the --host wire.
		options.Teams.History = func(team string) ([]teamstore.Packet, error) {
			return teamstore.Packets(profileDir, team)
		}
	}
	options.EngineRoad = true
	options.ReadCredits = v3LocalCreditReader(settings)
	options.Connections = v3Connections(v3Connect(profileDir))
	options.Harnesses = subharness.Default()
	options.SaveApproval = func(tool string) error {
		return saveToolApproval(profileDir, tool)
	}
	options.SaveBashApproval = func(command string) error {
		return saveBashApproval(profileDir, command)
	}
	options.SaveModel = func(model string) error {
		return config.WriteChatModel(profileDir, model)
	}
	// THE CODEX BROWSER DOOR IS THIS MACHINE'S TOO. The sign-in listens on this
	// machine's loopback and writes its tokens into the profile named above —
	// the engine's — which is exactly the pair --host cannot have. A launch that
	// reaches this line is a person at a terminal: --once returned before the
	// road opened a screen, and a screen is the only thing this road opens. It
	// was set on the in-process door alone when /connect grew the Codex row,
	// and on the ordinary launch the row answered `codex browser sign-in is
	// unavailable here` — the same family as Landing above and the doors
	// around it: a seam handed only to [openChatV3] is a seam the default road
	// does not have.
	options.ConnectCodex = v3CodexConnection(true)
	if profileDir == settings.ProfileDir {
		options.Sources = settings.Sources
	} else {
		// Keep the default account the surface already resolved, while reading
		// every additional service from the engine's authoritative profile.
		options.Sources = config.ResolveSources(profileDir, settings.APIKey, settings.BaseURL)
	}
	if options.Agent != nil {
		// THE ENGINE RESOLVES THE PROFILE. A connected service is already on
		// disk when this callback runs; setting the current model through the
		// existing wire door makes the engine re-read that disk without sending
		// a key or address through a second protocol.
		options.ApplyModelSources = func(modelsource.Set) {
			options.Agent.SetModel(options.Agent.Model())
		}
	}
}

// localAskAgainAfterRefusal is that decision, on its own so it can be asked
// without a socket: whether this launch dials the engine a SECOND time asking
// for a conversation of its own, and which transcript the surface should then
// point at.
//
// The three launches that do not: one the engine answered (err is nil), one it
// refused for any other reason — unreachable, a wrong wire version, a stale
// host, all of which are the in-process door's own floor — and `--once`, which
// has no screen to land an offer on and says the sentence instead
// ([sessionHeldElsewhereSentence]).
func localAskAgainAfterRefusal(launch localLaunch, err error) (string, bool) {
	if err == nil || launch.once != "" || !hostHeldRefusal(err) {
		return "", false
	}
	// Read BEFORE anything else is opened: the boot on the other side may reap
	// empty folders on its way past, and this is the journal that was refused.
	// It is [v3LatestTranscript], the same reading the engine's own key takes
	// (engine.go's [engineHelloKey]), so the row this points at is the row the
	// refusal was about.
	named := strings.TrimSpace(launch.session)
	if named == "" {
		return v3LatestTranscript(launch.workspace), true
	}
	// Spelled the way the surface's own rows spell it, or there would be no row
	// to point at.
	path, pathErr := engineSessionPath(named)
	if pathErr != nil {
		return named, true
	}
	return path, true
}

// joinNotice puts two entry-notice clauses on one line in the separator the
// notice already uses, and answers with whichever one is there when only one is.
func joinNotice(said, more string) string {
	said, more = strings.TrimSpace(said), strings.TrimSpace(more)
	switch {
	case said == "":
		return more
	case more == "":
		return said
	default:
		return said + " · " + more
	}
}

// v3LaunchShape is the per-launch posture as the wire carries it, or nil for a
// launch that asked for nothing. Nil rather than a zero struct because nil is
// what every other door sends and what the engine reads as its own defaults.
//
// interactive is the dial's own fact — false for a --once probe, true for a
// surface somebody is typing into — and it rides the shape because the session
// it opens lives in the engine, which cannot tell the two apart any other way.
func v3LaunchShape(yolo, noCompact, oneModel bool, maxHours, maxCost float64, interactive bool) *remote.LaunchShape {
	shape := remote.LaunchShape{
		Yolo:        yolo,
		NoCompact:   noCompact,
		OneModel:    oneModel,
		MaxHours:    maxHours,
		MaxCost:     maxCost,
		Interactive: interactive,
	}
	// A real screen carries its identity even without other flags. Nil remains
	// the legacy default for callers that supplied no launch facts.
	if (&shape).Same(nil) {
		return nil
	}
	return &shape
}

// hostShapeSentence says which posture the conversation on the socket has and
// which one this terminal asked for. Both halves are named because either alone
// leaves a person guessing at the other.
func hostShapeSentence(asked, has *remote.LaunchShape) string {
	said := launchShapeWords(asked)
	if said == "" {
		said = "the default posture"
	}
	running := launchShapeWords(has)
	if running == "" {
		running = "the default posture"
	}
	return fmt.Sprintf("this folder's conversation is already open with %s, so %s was not applied to it — this window opened its own instead, and it ends when this terminal does",
		running, said)
}

// launchShapeWords is a shape as a person typed it, or empty for the defaults.
func launchShapeWords(shape *remote.LaunchShape) string {
	if shape == nil {
		return ""
	}
	var said []string
	if shape.Yolo {
		said = append(said, "--yolo")
	}
	if shape.NoCompact {
		said = append(said, "--no-compact")
	}
	if shape.OneModel {
		said = append(said, "--one-model")
	}
	if shape.MaxHours > 0 {
		said = append(said, "--max-hours")
	}
	if shape.MaxCost > 0 {
		said = append(said, "--max-cost")
	}
	// Name the mode when it is the only difference between two launches.
	if shape.Interactive {
		said = append(said, "interactive chat")
	}
	return strings.Join(said, " ")
}

// ── the when-to-take rule ───────────────────────────────────────────────────

// v3HostChoice is everything the rule is decided on, gathered at the flag
// parser so the decision reads as one sentence and can be tested without a
// terminal.
type v3HostChoice struct {
	// noHost is --no-host: the escape hatch, and it exists because a fallback
	// nobody can ask for is a fallback nobody can use on the day the host is the
	// thing that is wrong.
	noHost bool
	// once is --once: one message, printed, and the process is done. It never
	// STARTS a host — a resident process left behind by a headless command is a
	// surprise — but it joins one that is already there, so a scripted message
	// lands in the conversation a person is actually in.
	once bool
	// debug is THE RECORD'S SWITCH and not the flag alone ([trace.Enabled]): the
	// model-call record is written by the process that makes the calls, and over
	// a socket that process is the host, which was never told to record. So a
	// run that is recording keeps its launch here, where the calls are real.
	//
	// IT IS THE SWITCH BECAUSE THE SWITCH HAS THREE DOORS AND THE FLAG IS ONE.
	// Reading the flag alone meant `CODEAF_DEBUG=1 codeaf` took the host road,
	// and four launches wrote a folder holding run.json and not one request body
	// — the header written by this process, the calls made by another — while
	// `codeaf chat --debug` wrote them all (issue #1022). The manual advertises
	// the environment pin for exactly this, so it reaches the same door.
	debug bool
	// setup says this machine may still have to be set up — no key it can find —
	// and setting one up is a conversation with the person at this terminal
	// (internal/tui3's firstrun.go). A host has no terminal and cannot have it,
	// so onboarding happens here and the host road is taken on the next launch.
	setup bool
}

// v3HostRoad answers whether `codeaf chat` opens its conversation through this
// workspace's session host, and names the workspace either way.
//
// The host road is the ordinary one. What it buys is what the in-process door
// cannot: work that goes on when the terminal closes, and a second window that
// sits down in the same conversation rather than beside it. A host is started if
// none is answering, and it retires itself when it is holding nothing.
//
// Four launches keep the in-process door, each because something real about them
// lives in this process: onboarding, --once, --debug and --no-host. The
// per-launch postures are not among them — --yolo, --no-compact, --one-model and
// the two ceilings travel in the hello and the engine builds the session with
// them ([remote.LaunchShape]).
//
// A workspace that cannot be resolved is no road: everything here answers
// "in-process" when it cannot tell, because that door is the floor and reaching a
// host is the feature.
func v3HostRoad(choice v3HostChoice) (string, bool) {
	workspace, _ := v3Workspace(v3LaunchDir(), "")
	if workspace == "" {
		return "", false
	}
	return workspace, v3TakeHostRoad(workspace, choice)
}

// v3TakeHostRoad is the rule itself, asked about one workspace. It is separate
// from the resolution above because where the person is standing is answered
// once per process ([v3LaunchDir] is a sync.Once) and the rule is a question
// about any directory.
func v3TakeHostRoad(workspace string, choice v3HostChoice) bool {
	if strings.TrimSpace(workspace) == "" || choice.noHost || choice.debug || choice.setup {
		return false
	}
	if choice.once {
		return v3HostAnswers(workspace)
	}
	return true
}

// v3HostAnswers is whether something is holding this workspace RIGHT NOW. The
// connection is spent on the question and closed; which build is there, and what
// to do about an older one, is [localLink.dial]'s question a moment later.
// Nothing is ever started from here.
func v3HostAnswers(workspace string) bool {
	// A host takes its lock before it replaces a prior socket, so a refused
	// connect while the lock is held is a host coming up and worth a short wait;
	// [enginehost.DialStartingHost] makes that judgement and answers at once when
	// no host is starting.
	conn, err := enginehost.DialStartingHost(workspace)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// v3MachineIsSetUp says this machine can already talk to a model. It is the one
// question the host road must ask before it spawns anything: [config.Load]
// answers ErrNoAPIKey when there is no key in the environment or the profile,
// and that launch belongs in this terminal where the browser flow can finish.
func v3MachineIsSetUp() bool {
	_, err := config.Load()
	return err == nil
}

// localBesideHello carries the ordinary terminal's settings to a sibling chat.
func localBesideHello(ask engineAsk, launch localLaunch) remote.Hello {
	return remote.Hello{Workspace: ask.workspace, Session: ask.session, New: ask.mint, Model: launch.model, Level: launch.level, Launch: launch.shape}
}

// localErrandDoor is [tui3.Options.Errand] on the engine road: home's `ask here`,
// answered by a session THIS process opens against the folder the surface made.
//
// THE BOOT HALF IS OPENED ON THE FIRST ASK AND NEVER AT LAUNCH. Everything
// [v3Errand] needs — the profile, the catalog, the crew rows, the harness
// registry, the gate — is what [openV3ProcessWith] and [openV3Launch] assemble,
// and this road exists precisely so that a launch does not pay for them: the
// engine on the other end of the socket has already done all of it. So the
// assembly hangs off a sync.Once behind the closure, where it is paid for by the
// one keystroke that needs it and by no other launch — and a machine whose
// profile has since broken says so on home's own line rather than at the door.
//
// IT RESOLVES THE CONVERSATION THE ENGINE IS ALREADY HOLDING, by name, rather
// than asking for "this workspace's latest". A launch resolution with no session
// named mints or reaps folders ([v3ResolveSession]), and neither is anything an
// errand may do to a project a daemon is sitting in. Nothing here takes that
// journal's flock — only [openV3Agent] does — and the config is immediately
// pointed somewhere else ([v3Errand] hands it the errand's own folder).
func localErrandDoor(launch localLaunch, welcome remote.Welcome) (func(tui3.ErrandOrders) (tui3.Agent, error), func()) {
	var (
		once  sync.Once
		held  *v3Process
		open  func(tui3.ErrandOrders) (tui3.Agent, error)
		fault error
	)
	door := func(orders tui3.ErrandOrders) (tui3.Agent, error) {
		once.Do(func() {
			proc, err := openV3ProcessWith("chat", true)
			if err != nil {
				fault = err
				return
			}
			boot, err := openV3Launch(proc, v3Options{
				Model:       launch.model,
				Interactive: true,
				Workspace:   welcome.Workspace,
				Session:     welcome.SessionFile,
				// AND THIS PROCESS DOES NOT KEEP TIME. The engine on the other end
				// of the socket holds this project's conversations, so it is the
				// one whose pass can put a firing into the conversation somebody is
				// sitting in rather than into the project's inbox
				// ([v3Options.NoStandingTicks] carries the whole of why).
				NoStandingTicks: true,
			})
			if err != nil {
				fault = err
				return
			}
			held = proc
			// AND IT IS SHAPED THE WAY AN INTERACTIVE DOOR SHAPES ONE, which is
			// two facts [openV3Launch] deliberately does not settle. Every lane is
			// a channel here because the errand's session and this screen are one
			// program (chatv3_lanes.go's [v3LanesHere]), and the gate may ASK,
			// because there is a surface and it answers — chatv3.go states both
			// beside the same pair of lines. Without the second the errand's
			// `stand` refused every proposal with `nobody is here to say yes —
			// this can only be set up in a conversation`, which is the honest
			// answer for a headless run and a lie about a card on somebody's home.
			cfg, _ := v3Shape(boot.Config, v3LanesHere())
			cfg.AskConsent = true
			open = v3Errand(cfg, welcome.Workspace, boot.Settings.ProfileDir, launch.shape != nil && launch.shape.Yolo)
		})
		if fault != nil {
			return nil, fault
		}
		agent, err := open(orders)
		if err != nil {
			return nil, err
		}
		// THE PROCESS OWNS EVERY CONVERSATION IT OPENED, this one included, so a
		// surface that returns by any road flushes the errand's journal and lets
		// go of its flock ([v3Process.closeAll]). The type assertion is how the
		// tracked list stays *session.Agent rather than the surface's interface;
		// [v3Errand] only ever answers one of those.
		if real, ok := agent.(*session.Agent); ok {
			held.track(real)
		}
		return agent, nil
	}
	return door, func() {
		if held != nil {
			held.closeAll()
		}
	}
}
