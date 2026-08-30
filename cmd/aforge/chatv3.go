package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/buildinfo"
	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/connect"
	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/search"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// runChatV3 is the v3 door: one session agent over this directory, and the
// linear surface that shows it. It claims no residency and starts no runner;
// the ONE thing it opens of the graph database is the person's memories
// (milestone V3-1, [v3Memory]), because those are the only rows in it a
// conversation has any business reading.
func runChatV3(args []string) error { return openChatV3("chat", args, false) }

// runResumeV3 is `aforge resume`: the same door, opened on the session picker.
//
// It is one word rather than a flag on chat because it is what a person is
// doing when they type it — coming back to a conversation, not starting one —
// and it takes chat's flags for the same reason: the session it opens is a chat
// session, and a model or a reasoning level named on the way in is named about
// that.
//
// IT DOES NOT RESUME ANYTHING BY ITSELF. The surface opens exactly as `aforge`
// bare does, on this directory's most recent conversation, with the picker over
// it — so esc lands where the person would have been anyway, and enter lands
// where they asked to be. A launcher that opened on an empty session instead
// would make "resume" the one command that can leave you with nothing.
func runResumeV3(args []string) error { return openChatV3("resume", args, true) }

func openChatV3(name string, args []string, pickSession bool) error {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	model := flags.String("model", "", "model slug for this session; beats the configured default")
	once := flags.String("once", "", "run one message non-interactively, print the reply, and exit")
	file := flags.String("session", "", "session transcript to resume; empty resumes this directory's most recent")
	noCompact := flags.Bool("no-compact", false, "never compact automatically")
	yolo := flags.Bool("yolo", false, "run every tool without asking: the approval default becomes allow")
	reasoning := flags.String("reasoning", "", "how hard this session's model is asked to think: off, low, medium or high")
	host := flags.String("host", "", "run the session on another machine over ssh: host, user@host, or host:path/to/project")
	at := flags.String("at", "", "reach a machine that has no ssh, by the name `aforge serve` prints there: otter-lamp-42, or otter-lamp-42:path/to/project")
	oneModel := flags.Bool("one-model", false,
		"every text call this session makes runs on the session model: the tier rows, the role pins, "+
			"the fallback chain and the task model all stand down")
	// THE TWO CEILINGS AN UNATTENDED SESSION MAY BE GIVEN, and they are floats
	// rather than durations because a person types `--max-hours 6` and
	// `--max-hours 0.5`, not `6h0m0s`. Either alone is a budget; neither is the
	// posture this build has always had. They mean nothing without --yolo, and
	// the check below says so rather than letting a flag do nothing in silence.
	maxHours := flags.Float64("max-hours", envFloat("AFORGE_MAX_HOURS"),
		"how many hours an unattended --yolo session may carry its own work on (env AFORGE_MAX_HOURS)")
	maxCost := flags.Float64("max-cost", envFloat("AFORGE_MAX_COST"),
		"how many dollars an unattended --yolo session may carry its own work on (env AFORGE_MAX_COST)")
	if err := flags.Parse(reorder(flags, args)); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		// Resume's usage names no --once and no --session: it opens a list of
		// the sessions there ARE, so naming one on the command line is the other
		// door, and nobody is watching a headless one.
		if pickSession {
			return fmt.Errorf(`usage: aforge resume [--model slug] [--reasoning level] [--host host[:path]] [--at name[:path]] [--no-compact] [--yolo [--max-hours n] [--max-cost n]] [--one-model]`)
		}
		return fmt.Errorf(`usage: aforge chat [--model slug] [--reasoning level] [--session path] [--host host[:path]] [--at name[:path]] [--once "text"] [--no-compact] [--yolo [--max-hours n] [--max-cost n]] [--one-model]`)
	}
	// --one-model is about THIS machine's settings rows, and over --host the
	// rows that answer are the far machine's (chatv3_host.go). A flag that
	// looked like it applied and did not would be worse than one that is not
	// there, so the combination is refused rather than quietly dropped.
	if *oneModel && strings.TrimSpace(*host) != "" {
		return fmt.Errorf("--one-model settles this machine's model rows; over --host the far machine answers them, so the two cannot be combined")
	}
	// --at is the same fork as --host and differs only in what carries it, so
	// it inherits --host's refusal word for word: the rows that answer are
	// still the other machine's.
	if *oneModel && strings.TrimSpace(*at) != "" {
		return fmt.Errorf("--one-model settles this machine's model rows; over --at the far machine answers them, so the two cannot be combined")
	}
	// TWO WAYS TO REACH ONE MACHINE IS NOT TWO MACHINES. Naming both is a
	// person saying two different things about where the work is, and guessing
	// which they meant would open a conversation on a machine they did not
	// name.
	if strings.TrimSpace(*host) != "" && strings.TrimSpace(*at) != "" {
		return fmt.Errorf("--host reaches a machine over ssh and --at reaches one through a relay: name one or the other, not both")
	}

	// A BUDGET IS A SENTENCE ABOUT AN UNATTENDED SESSION, so it is refused
	// rather than ignored on a session somebody is sitting in front of. What it
	// buys — a goal owner that carries the work on by itself (internal/session's
	// principal.go) — is the thing --yolo alone must never be read as permission
	// for, and a ceiling on a session that was never going to carry anything on
	// is a number that quietly did nothing.
	//
	// IT IS ASKED OF A LOCAL LAUNCH ONLY. Over --host and over --at the ceiling
	// cannot travel at all and each door says so in its own words
	// (chatv3_host.go's and chatv3_at.go's checks); two refusals for one flag
	// would send somebody to add --yolo and straight into the second one.
	if budget := chatBudget(*maxHours, *maxCost); budget.Set() && !*yolo && strings.TrimSpace(*host) == "" && strings.TrimSpace(*at) == "" {
		return fmt.Errorf("--max-hours and --max-cost bound a session that carries its own work on; say --yolo as well, or leave them off")
	}
	// A picker with nobody watching is not a picker. --once is the headless
	// door, and the two are a contradiction rather than a combination, so it is
	// said here — before a session file is opened — instead of being ignored.
	if pickSession && strings.TrimSpace(*once) != "" {
		return fmt.Errorf(`aforge resume opens the session picker; for one headless message use: aforge chat --once "text"`)
	}
	// The level is validated HERE, before anything is opened, so a typo is a
	// usage error and not a knob that silently did nothing for a whole session.
	// It is a one-shot: ctrl+t in the picker overrides it from that moment on,
	// and /new — a new agent — starts with no level at all.
	level, ok := session.ParseReasoning(*reasoning)
	if !ok {
		return fmt.Errorf("--reasoning %q: use off, low, medium or high", *reasoning)
	}

	// THE OTHER DOOR, and it forks BEFORE any of this machine's own resolution
	// below: over --host the models, the keys, the session files, the gate and
	// the harnesses are all the far machine's, and reading this one's would be
	// resolving a launch nobody asked for (chatv3_host.go).
	if dest := strings.TrimSpace(*host); dest != "" {
		return openChatV3Host(hostLaunch{
			target:    dest,
			session:   strings.TrimSpace(*file),
			model:     strings.TrimSpace(*model),
			level:     level,
			once:      strings.TrimSpace(*once),
			pick:      pickSession,
			noCompact: *noCompact,
			yolo:      *yolo,
			budget:    chatBudget(*maxHours, *maxCost).Set(),
		})
	}

	// THE THIRD DOOR, and it forks here for the reason --host does: a machine
	// reached by its paired name owns exactly what a machine reached over ssh
	// owns. The two differ only in what carries the frames — an ssh child there,
	// a relay tunnel here — which is the whole point of the transport being an
	// io.ReadWriteCloser and nothing more (chatv3_at.go, docs/REMOTE.md).
	if name := strings.TrimSpace(*at); name != "" {
		return openChatV3At(atLaunch{
			target:    name,
			session:   strings.TrimSpace(*file),
			model:     strings.TrimSpace(*model),
			level:     level,
			once:      strings.TrimSpace(*once),
			pick:      pickSession,
			noCompact: *noCompact,
			yolo:      *yolo,
			budget:    chatBudget(*maxHours, *maxCost).Set(),
		})
	}

	// Everything both v3 doors assemble the same way: the settings, the model
	// catalog, the harness registry, the session file, and the config every
	// governance row has landed on. `aforge engine` opens a conversation for a
	// surface on another machine and opens it THROUGH HERE (engine.go), so a
	// remote session is the same launch this one is rather than a second one
	// drifting quietly away from it.
	//
	// The process half is built first and exactly once (chatv3_process.go): the
	// profile, the catalog, the harness registry, the memory database, the
	// deliverables index and the accounts manager are things this PROCESS owns,
	// and a launch borrows them rather than opening a second of each.
	// WHETHER THIS LAUNCH MAY SET ITSELF UP. It is a person at a terminal with
	// no particular conversation in mind — a TTY on stdin, no --once (which
	// returned above, but the flag is the honest test), no --session and no
	// picker — and it is the one launch that may open with no key and ask for
	// one on its first screen (internal/tui3's firstrun.go). Everything else
	// meets the old refusal at the door. --host forked above and never reaches
	// here; the far machine's key is the far machine's business.
	setup := stdinIsTerminal(os.Stdin) && strings.TrimSpace(*once) == "" &&
		strings.TrimSpace(*file) == "" && !pickSession
	proc, err := openV3ProcessWith("chat", setup)
	if err != nil {
		return err
	}
	seed := v3Options{
		Model:     *model,
		NoCompact: *noCompact,
		Yolo:      *yolo,
		OneModel:  *oneModel,
		Budget:    chatBudget(*maxHours, *maxCost),
	}
	boot := seed
	boot.Session = *file
	launch, err := openV3Launch(proc, boot)
	if err != nil {
		return err
	}
	settings, models, harnesses := launch.Settings, launch.Models, launch.Harnesses
	workspace, transcript, resumed := launch.Workspace, launch.SessionFile, launch.Resumed
	chosen, cfg := launch.Model, launch.Config

	if text := strings.TrimSpace(*once); text != "" {
		// Nobody is watching a --once run, so nobody can answer a question. The
		// policy's "prompt" therefore refuses the call with a result the model
		// can act on (internal/session's consent.go), and a person who wants
		// that work to run says so with --yolo. NON-INTERACTIVE WORK NEEDS AN
		// EXPLICIT POSTURE: the two honest answers are "refuse what you would
		// have asked about" and "I have said in advance that this may run", and
		// neither of them is a gate that quietly opens because the terminal
		// happens to be a pipe.
		cfg.AskConsent = false
		// AND NOTHING MAY BE SET UP HERE. A standing item spends money at times
		// nobody chose, so it needs a yes; a headless run has nobody to give
		// one, and a clock that armed it anyway would be the harness agreeing on
		// somebody's behalf. The tool is simply absent (internal/session's
		// tools.go), which is the same law --once already applies to consent.
		cfg.Standing = nil
		// AND THE SUBHARNESS SEAMS ARE LEFT ALONE, which is not an oversight
		// beside the line above it. Standing has to be taken away here because
		// nothing else asks whether anybody is watching before it arms a clock. A
		// subharness cannot start without somebody confirming an intake card, and
		// internal/session already reads that in one place — its own gate is
		// `AskConsent && there is something to offer` (tools_subharness.go), and
		// the line above has already answered the first half. Nilling the registry
		// too would be a second copy of a decision that is made correctly one
		// layer down, and it would take the `/subharness` list away from a door
		// that may yet grow one.
		return runChatV3Once(cfg, text, level, resumed)
	}
	// Interactive: there is a surface, and it answers (internal/tui3's
	// consent.go). This is the ONLY path that sets it.
	cfg.AskConsent = true
	// AND THAT SURFACE HOLDS THE HARNESS LANE, which is a second fact and not the
	// same one: the lane is a standing subscription opened on the agent itself
	// (internal/tui3's watchDesigns), so it exists only where the surface and the
	// session are in one process. It is what lets chat offer a saved program with
	// an intake card (internal/session's canProposeSubharness); a conversation
	// held over a connection sets AskConsent and NOT this, because the card has no
	// road to the far end (engine.go says the same about the design card).
	cfg.HarnessCards = true

	agent, cfg, notice, err := openV3Agent(cfg, workspace, v3OpenSession)
	if err != nil {
		return err
	}
	// AND NOW THE BELT EXISTS, so a program's tool guard can be answered. Until
	// this line every such guard answers no and the work takes the long way,
	// which is the safe direction and not the useful one ([beltWatch] states the
	// ordering problem this closes).
	launch.Subharnesses.Belt.watch(agent.ToolOnBelt)
	if notice != "" {
		// The session file moved under us, so everything downstream that names
		// it — /help, /new, the resumed line — has to name the new one.
		transcript, resumed = cfg.SessionFile, false
	}
	// The boot override lands on the model this session starts on, which is the
	// only model it can be about: --reasoning names a strength, not a model, and
	// the level is kept per model from here on (internal/session's agent.go).
	agent.SetReasoning(level)
	// AND THE RUNG THIS CONVERSATION WAS LEFT ON. It is the meta.json half of
	// the same law the model row keeps (internal/session's Meta): a person who
	// dialled a conversation deeper, worked in it and came back found it at the
	// install's default as though they had chosen nothing. Absence sets nothing
	// and stamps nothing, so a conversation nobody has dialled is unchanged.
	if saved := strings.TrimSpace(v3SavedEffort(cfg.Place)); saved != "" {
		agent.SetConversationEffort(saved)
	}
	proc.track(agent)
	// EVERY CONVERSATION THIS PROCESS OPENED, CLOSED HOWEVER THE SURFACE RETURNS.
	// Close is the surface's to call — /quit and ctrl+c both go through it — but
	// a Run that returns by any other road must still flush the files, release
	// the session flocks and stop the presence heartbeats, and after this wave
	// there is more than one of each. The defer that used to stand here owned
	// exactly the boot agent, and the store's close was registered before it so
	// that it ran after; [v3Process.closeAll] holds both halves of that ordering
	// itself and is a no-op the second time it is called.
	defer proc.closeAll()

	guard.Go("chatv3/models", func() { warmV3Models(models, agent, chosen) })

	// The two things this surface keeps on the person's behalf rather than the
	// session's: what they have typed before, and what they have half-typed
	// now. Both are settings (internal/config), both are off by one row, and
	// neither is ever waited for — a history file that cannot be opened costs
	// the up arrow and nothing else.
	//
	// They read through the project layer like every other row: a repository
	// that says "record nothing from this directory" is answering about ITS
	// directory, which is the whole reason the layer is per-workspace. The
	// broken-file error cannot actually arrive here — governance above read the
	// same file and would have stopped the launch — and it is still returned
	// rather than swallowed, because the only wrong thing to do with an
	// unreadable settings file is carry on as though it said nothing.
	keepHistory, err := config.ProjectBoolAt(workspace, settings.ProfileDir, config.KeyHistoryEnabled)
	if err != nil {
		return err
	}
	keepDraft, err := config.ProjectBoolAt(workspace, settings.ProfileDir, config.KeyDraftPersist)
	if err != nil {
		return err
	}
	var recall tui3.History
	if keepHistory {
		// One file per process, opened on the first workspace that wants it and
		// closed by [v3Process.closeAll] (chatv3_process.go). The rows carry the
		// directory they were typed in, so the surface asks for its own.
		recall = proc.history()
	}
	draft := ""
	if keepDraft {
		if dir, err := v3Dir(); err == nil {
			draft = tui3.DraftFile(dir, workspace)
		}
	}
	// THE AGENT-BUILDING SEAM (chatv3_process.go's [v3Seam]). Every conversation
	// after the first comes through here, carrying the closures that belong to
	// ITS agent and ITS workspace — which is what stops an "always" answered
	// after a /new from being pushed into the session that /new closed.
	//
	// It carries the boot launch AS IT ENDED UP rather than as it was assembled:
	// [openV3Agent] may have moved the session file under us on a contended
	// resume, and a conversation opened later is a sibling of the folder this
	// window is actually in.
	settled := *launch
	settled.Config, settled.Place = cfg, cfg.Place
	settled.SessionFile, settled.Resumed = transcript, resumed
	seam := &v3Seam{proc: proc, boot: &settled, seed: seed}

	// The byte meter, off unless a developer named a log file (wire.go). A nil
	// writer here is the same launch this door has always made.
	wire, closeWire := v3Wire()
	defer closeWire()

	// Anything written to the standard logger while the surface owns the
	// terminal tears straight through the frame as a raw row — a checkpoint
	// warning or a media fallback lands spliced into whatever the person is
	// typing. Same fix as the v2 door, for the same reason: the logger goes to
	// a file beside the profile for the surface's whole lifetime, and comes
	// back to stderr on the way out. A profile that cannot take the file keeps
	// stderr — a lost frame is better than a lost warning.
	if logFile, logErr := os.OpenFile(chatLogPath(settings.ProfileDir),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); logErr == nil {
		log.SetOutput(logFile)
		defer func() {
			log.SetOutput(os.Stderr)
			_ = logFile.Close()
		}()
	}

	return tui3.Run(context.Background(), tui3.Options{
		Agent: agent,
		Build: buildinfo.String(),
		// The memory place and the search place read the SAME database the
		// conversation remembers into, through two seams that fail apart: memory
		// turned off in the settings opens no store at all and both are then
		// absent, which is what keeps "memory off makes no calls" a property of
		// the wiring rather than a branch in every caller (v3Memory).
		Memory: v3MemorySeam(cfg.Memory),
		Search: v3SearchSeam(cfg.Memory),
		// The machine-wide spending ledger the spend place adds up. It is the
		// same file every window on this machine appends a model call to, named
		// once by internal/session so a reader and a writer cannot spell it two
		// ways (internal/session's UsageLedgerPath).
		UsageLedger: session.UsageLedgerPath(),
		Output:      wire,
		// The sub-harness registry under the state root, which is where every
		// window on this machine writes and reads them: /harness is a list of
		// what is SAVED, so it has to be the same directory the builder saved
		// into (internal/subharness's store.go).
		Harnesses: harnesses,
		// Asked at the moment the picker opens, never at boot: a catalog that
		// resolved while the person was reading is a catalog the picker can
		// use, and one that has not resolved answers nil instead of waiting.
		Models: func() []tui3.Model { return v3Models(models) },
		// The same deliverables index the session's config carries, so the
		// surface's /export rows and the session's own land in one file.
		ArtifactsIndex: artifactsIndexPath(),
		// The profile the settings panel reads and writes, which must be the
		// SAME directory this launch read every governance row out of. It was
		// missing here while the --host door supplied it (chatv3_host.go), and
		// the gap was invisible in the ordinary case and silent in the one that
		// mattered: with AFORGE_PROFILE_DIR set, the panel wrote into
		// ~/.aforge/config.json while the session went on reading the profile
		// the variable named, so a gate turned off in the sheet stayed on and
		// nothing on screen said why.
		ProfileDir: settings.ProfileDir,
		// /new and the picker, as the seam answers them. The two wrappers below
		// are the OLDER shape of the same doors, kept because the surface still
		// falls back to them and because the hosted door can only answer that
		// shape (chatv3_host.go): there is one remote agent by construction, so
		// there is nothing per-conversation to hand back.
		Start: seam.start,
		Open:  seam.resume,
		AnchorWorkspace: func(path string) (string, error) {
			return seam.anchor(agent, path)
		},
		// The ambient side as this surface reads it: home's item band, the
		// pause and stop keys, and /status's keeping-watch line, all off the
		// same store the conversation proposes into (chatv3_standing.go).
		Standing: v3StandingSeam(cfg.Standing),
		Fresh: func() (tui3.Agent, string, error) {
			conv, err := seam.start("")
			if err != nil {
				return nil, "", err
			}
			return conv.Agent, conv.SessionFile, nil
		},
		// ── lane errand, for the merge: this pair and nothing else ──────────
		// Home's `ask here` — the errand answered in the right pane, whose
		// transcript lives under the standing root and never under v3/projects
		// (chatv3_exchange.go, tui3's homeexchange.go).
		Errand:       v3Errand(cfg, workspace, settings.ProfileDir, *yolo),
		StandingRoot: v3StandingRoot(),
		// Answering another window's question from home: the answer is left in
		// that session's own folder and it picks it up on its heartbeat
		// (internal/session's answers.go, tui3's homeband_answer.go).
		Answer: session.WriteAnswer,
		// The conversations this directory has had, and the door back into one
		// of them. They are the welcome box's right column and the /resume
		// picker's rows; the walk happens on the keystroke that asks for it and
		// never at boot.
		RecentSessions: func() []tui3.Session { return v3RecentSessions(launch.Bucket) },
		Resume: func(file string) (tui3.Agent, error) {
			conv, err := seam.resume("", file)
			if err != nil {
				// Returned rather than wrapped in a surface that would carry a
				// typed nil: a locked file's error names the file, and the
				// surface prints exactly that.
				return nil, err
			}
			return conv.Agent, nil
		},
		PickSession: pickSession,
		// WHETHER HOME GREETS THIS LAUNCH. It is a person opening aforge with no
		// particular conversation in mind: no --session, no picker asked for,
		// and — by the time this line runs — no --once, which returned above.
		// The surface applies the rest of the law, including the one condition
		// that is about the machine rather than the command line: a machine with
		// nowhere else to go is not greeted (tui3's home.go [app.landHome]).
		//
		// The --host door does not set it at all: the projects under this
		// process belong to the wrong machine, and home refuses over --host for
		// exactly that reason.
		Landing: strings.TrimSpace(*file) == "" && !pickSession,
		// THE FIRST-RUN SETUP, on the same launch that may open keyless (see
		// `setup` above). The surface applies the rest of its law — a profile
		// with every fact answered, a marker saying it was shown, a resumed
		// conversation — and this door only says whether anybody is here to
		// answer.
		Setup: setup,
		// And the key arriving after the door: every conversation this process
		// holds starts talking with it on its next request, and every one opened
		// later is built with it (chatv3_process.go's [v3Process.setAPIKey]).
		ApplyAPIKey: proc.setAPIKey,
		// The accounts panel, and the sign-in a pressed row starts. It is the
		// SAME manager the belt reaches through (cfg.Connect), so an account
		// connected on the panel is connected for the model in the same breath
		// and neither side has to be told about the other.
		Connections: v3Connections(cfg.Connect),
		Workspace:   workspace,
		// Whether that workspace is the session's own work/ directory or a
		// project this was opened inside of (Decision 26). The surface uses it
		// for one thing: what to CALL the place, because an owned workspace's
		// path is aforge's bookkeeping rather than an answer to "where am I".
		Owned:       launch.Place.Owned,
		SessionFile: transcript,
		Resumed:     resumed,
		// AND THE LAUNCH FACTS A PERSON CAN ACT ON. The unattended boundary and
		// a replaced aforge both ride the session-moved line — one dim row at
		// the top of the conversation — rather than growing surfaces of their
		// own. An ordinary attended launch with the same file on disk is still
		// shown nothing whatever.
		Notice:        joinV3Notices(notice, session.UnattendedNotice(cfg), buildinfo.StaleNotice()),
		ContextWindow: cfg.ContextWindow,
		History:       recall,
		DraftFile:     draft,
		// The consent card's "always", written down AND handed to the gate this
		// session is running on (chatv3_approval.go). The second half is why a
		// banked rule answers the very next call instead of the next launch.
		// Where a model chosen in /model, in the picker or on the settings sheet
		// is written down, so the next launch opens on it ([v3TalkModel] reads
		// the same row back). It is the profile this launch resolved everything
		// else out of, which is the point: the surface must not be able to write
		// its choice into a file the door does not read.
		SaveModel: func(model string) error {
			return config.WriteChatModel(settings.ProfileDir, model)
		},
		SaveApproval:     bankToolApproval(agent, workspace, settings.ProfileDir, *yolo),
		SaveBashApproval: bankBashApproval(agent, workspace, settings.ProfileDir, *yolo),
		ApplyApprovals:   applyV3Approvals(agent, workspace, settings.ProfileDir, *yolo),
	})
}

// ── the shared assembly ─────────────────────────────────────────────────────
//
// A conversation is the same object whoever is looking at it. `aforge chat`
// draws it on the terminal it was typed into; `aforge engine` answers frames
// about it from the other end of an ssh pipe. NEITHER DOOR MAY ASSEMBLE ITS OWN
// AGENT: two assemblies would be two sets of governance rows, two model
// catalogs and two session-file rules, and the drift between them would show up
// as a feature that behaves one way at home and another way away — the single
// hardest class of bug to see, because both halves look right on their own.

// v3Options is what a door says about the conversation it wants opened. Every
// field is a flag some door carries; the zero value is what `aforge` bare does.
type v3Options struct {
	// Workspace is the directory the conversation runs in. Empty is the
	// process's own, which is what a terminal door means and what the engine
	// means once it has changed into the directory the surface asked for.
	Workspace string
	// Model beats the configured default for this session only.
	Model string
	// Session is an explicit transcript to open; empty resumes this
	// directory's most recent, or makes one.
	Session string
	// NoCompact and Yolo are the two flags that change what a session may do.
	NoCompact bool
	Yolo      bool
	// OneModel settles every text call this session makes onto the session
	// model. It is a MEASUREMENT POSTURE rather than a preference: a run whose
	// spend and quality are being attributed to one model cannot have a tier
	// row quietly answering a quarter of its calls on another. It changes no
	// setting and writes nothing — the rows are still there, and the next
	// session without the flag reads them exactly as before.
	OneModel bool
	// Budget is the ceiling an unattended session carries its own work on
	// under: hours, dollars, or both (internal/session's principal.go). THE
	// ZERO BUDGET IS THE DEFAULT AND IS NOT A CEILING OF ZERO — it is the
	// posture every session has always had, where the model stopping is the
	// session stopping.
	Budget session.Budget
}

// joinV3Notices puts the launch's dim lines on one row, in the order they were
// decided. Either being empty is the ordinary case and leaves the other alone —
// the emptiness law, applied to a separator.
func joinV3Notices(lines ...string) string {
	var kept []string
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, " · ")
}

// chatBudget turns what somebody typed into the ceiling internal/session reads.
//
// A NEGATIVE FIGURE IS NO CEILING RATHER THAN AN ERROR, which is the same
// reading every other numeric row in this build makes of one (the repair-round
// resolver's, the rail's): a person who typed a minus sign has not asked for a
// session that stops before it starts.
func chatBudget(hours, cost float64) session.Budget {
	var budget session.Budget
	if hours > 0 {
		budget.Wall = time.Duration(hours * float64(time.Hour))
	}
	if cost > 0 {
		budget.USD = cost
	}
	return budget
}

// envFloat reads one number out of the environment, and answers zero for
// anything that is not one. It is the default a flag is declared with, so a
// harness that sets the wall once for a campaign does not have to spell it on
// every launch — and the command line still wins, because a flag's own value
// replaces its default.
func envFloat(name string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(name)), 64)
	if err != nil {
		return 0
	}
	return value
}

// v3Launch is that assembly, done. The pieces are handed back rather than kept
// because both doors keep using them after the agent exists: the catalog warms
// in the background and answers the model picker, the harness registry is the
// same store /harness lists, and the config is what a replacement agent — /new,
// the picker, Session.New — is built on.
type v3Launch struct {
	Settings  config.Config
	Models    *catalog.Catalog
	Harnesses *subharness.Store
	// Config is the session config with every governance row applied, and with
	// AskConsent NOT yet set: whether anybody is there to answer a question is
	// the door's own fact and the one thing this cannot know.
	Config session.Config
	// Model is the id the session starts on, after the flag and the settings
	// have had their say.
	Model string
	// Workspace, SessionFile and Resumed are the three facts a surface prints
	// before its first turn.
	Workspace   string
	SessionFile string
	Resumed     bool
	// Place is the session folder this launch opened (internal/session's
	// place.go), and Bucket the project directory it sits in. The surface keeps
	// both after the launch: the folder is what /new mints a sibling of, and the
	// bucket is the list of this project's conversations.
	Place  session.Place
	Bucket string
	// Subharnesses is the assembly the four seams on Config were taken from
	// (chatv3_subharness.go). It is carried out of the launch for ONE thing the
	// config cannot hold: the belt watch, which is a question about an agent that
	// does not exist until after this returns and is filled the moment it does
	// (see [beltWatch], and the door it is pointed at, session.Agent.ToolOnBelt).
	Subharnesses v3Subharness
}

func openV3Launch(proc *v3Process, opts v3Options) (*v3Launch, error) {
	// The profile, the catalog, the harness registry and the stores were all
	// resolved once, at the door (chatv3_process.go). Everything below this line
	// is a function of the DIRECTORY this conversation is about, which is the
	// whole of what makes a launch different from the process it runs in.
	settings := proc.Settings
	chosen := v3TalkModel(opts.Model, settings)
	// WHERE THE PERSON IS STANDING, which is not the same fact as which project
	// this is: `aforge` typed in repo/cmd/ is a conversation about the
	// repository, and the subdirectory is recorded rather than resolved away
	// (Decision 26). A door that named a workspace has already answered the
	// project question and its answer is taken as given.
	launchDir := proc.LaunchDir

	project, owned := v3Workspace(launchDir, opts.Workspace)
	found, err := v3ResolveSession(strings.TrimSpace(opts.Session), project,
		v3StampLaunchDir(launchDir, project), owned)
	if err != nil {
		return nil, err
	}
	transcript, resumed := found.Transcript, found.Resumed
	// THE TOOLS ROOT IS THE SESSION'S OWN ANSWER. A borrowed session works in
	// the project it borrowed; an owned one works in its own work/ directory,
	// which is prepared here because nothing downstream may find it missing.
	workspace := project
	if root := strings.TrimSpace(found.Place.Workspace); root != "" {
		workspace = root
	}
	if found.Place.Owned {
		if err := prepareOwnedWorkspace(found.Place); err != nil {
			return nil, err
		}
	}

	// The model catalog and the sub-harness registry, borrowed from the process
	// rather than opened here (chatv3_process.go states why each must be one).
	// The catalog's warm is already running and is waited for NOWHERE on this
	// path: everything it feeds — the /model picker's list, the context window —
	// has a good answer without it, and an unknown window leaves the session on
	// its conservative default.
	models := proc.Models
	harnesses := proc.Harnesses

	// The typed programs this conversation can reach, and the two stores they are
	// found in (chatv3_subharness.go). It is assembled BEFORE the config because
	// all four seams below are fields of it, and the zero value is subharnesses
	// off — so nothing here has to ask whether the wiring worked.
	subharnesses := v3Subharnesses(settings, models, chosen, workspace, harnesses)

	cfg := session.Config{
		Workspace:      workspace,
		Model:          chosen,
		APIKey:         settings.APIKey,
		BaseURL:        settings.BaseURL,
		CompactEnabled: !opts.NoCompact,
		SessionFile:    transcript,
		// The folder this conversation keeps everything in (Decision 26). It is
		// the zero Place for a launch opened on a flat legacy transcript, which
		// is what keeps that session deriving its sidecars the way it always did.
		Place: found.Place,
		// Where an adaptive run's write-capable nodes get their isolated
		// worktrees (internal/orchestrate): the session folder's trees/ —
		// Decision 26's own law, one per running node, swept with the session.
		// The zero Place answers "" and the run degrades to sharing the
		// workspace, which is the seam's honest answer for a legacy flat
		// session.
		WorktreeRoot: found.Place.Trees(),
		// The durable memory, on the store this time (internal/session's
		// memory.go). It is opened once, here, because where a person's state
		// lives is the door's decision — and it is opened AT ALL only when the
		// memory row is on, which is what makes "memory off makes no calls" a
		// fact about the wiring instead of a branch every caller has to keep.
		Memory: proc.Memory,
		// And the file the old memory lived in, carried into the store on the
		// first turn and then renamed out of the way. It is named here rather
		// than derived down there for the reason every other path is.
		MemoryImport: home.Join("v3", "memory.md"),
		// The deliverables index the session's own products (a painted picture)
		// record themselves in — the same file the surface's /export and /files
		// resolve, spelled once (chatv3_place.go) and resolved once per process.
		ArtifactsIndex: proc.Artifacts,
		// The profile whose config.json /settings writes, handed over so the
		// conversation can read the person's settings back and change one for
		// them (internal/session's tools_settings.go). It is the SAME directory
		// the panel edits (internal/tui3's registry) and the same one every row
		// above was resolved out of, so a change made by asking and a change made
		// by hand are one change to one file.
		ProfileDir: settings.ProfileDir,
		// The window the model this session STARTS on actually accepts, when
		// anybody can say so without waiting. Zero keeps session's own
		// conservative default, and [warmV3Models] corrects it in place the
		// moment the catalog resolves.
		ContextWindow:    v3Window(models, chosen),
		ContextWindowFor: models.ContextLength,
		// Whether the model in use can LOOK at a picture, from the catalog's
		// published input modalities. It is a closure rather than a value
		// because the answer is about the model the NEXT turn rides, and this
		// session's model changes under /model (see [v3SeesImages]).
		SupportsImages: v3SeesImages(models),
		// The published answer to "may this call carry this knob", which the
		// adapter asks before it lets an optional field travel. It was wired to
		// nothing on this path, so a reasoning level set with ctrl+t or
		// --reasoning went to every model blind — and on a router, a knob no
		// endpoint publishes is not a 400 but a 404 with no endpoints left to
		// serve the request (internal/provider's endpoints.go).
		SupportsParameter: models.SupportsParameter,
		ReasoningProfile:  config.ReasoningProfileSeam(models),
		// And the model's own published price, which is what bounds the latency
		// ask: this session wants the fastest endpoint, not the dearest one
		// wearing the model's name (internal/provider's latencyPriceCeiling).
		ModelPrice: models.PriceNow,
		// Where a conversation goes when nothing serving its model will take the
		// request at all. Closures again, and for the same reason as the vision
		// gate: the question is about the model the failing turn was ON, which
		// /model moves.
		NearestModels: v3NearestModels(models),
		// The models a task may be handed to, asked at the moment a proposal
		// names one and never at boot — the picker's own bargain, because both
		// questions are about a catalog that may still be warming and neither of
		// them may wait for it.
		TaskModels: v3TaskModels(models),
		// The two halves of the harness offer (internal/session's harness.go):
		// what a turn is matched against, and what a yes reaches. They are
		// filled together because either one alone is detection off — a
		// registry nothing can run would raise a card that could only fail, and
		// a runner nothing is matched against would never be called.
		// RunHarness is filled AFTER governance, beside the media pair: a run's
		// belt now carries the media verbs this machine has models for
		// (internal/session's harness_belt.go), and the resolver that answers
		// which model serves which modality reads a role pin that does not exist
		// until governance has landed.
		Harnesses: v3HarnessEntries(harnesses),
		// And the third: where a harness this conversation DESIGNS is written
		// (internal/session's harness_build.go). It is the same store the two
		// above were built from, so a page approved on a card is a page the very
		// next sentence can be matched against.
		HarnessStore: harnesses,
		// AND THE SUBHARNESS SIDE, which is the same three-part shape one row up:
		// what can be reached, where what it learns is kept, and what is known
		// about the last time each one ran (docs/SUBHARNESS-CONTRACT.md §5). They
		// are filled together because any one of them alone is a half-wiring — a
		// registry with no memory teaches a program it learnt something it did
		// not, and a history nothing writes to is a list of rows that will never
		// say anything.
		//
		// NIL IS SUBHARNESSES OFF and every one of these is allowed to be nil: a
		// store that cannot be read, a client that cannot be built and a machine
		// with no bundles on it all arrive here as the zero value, and the doors
		// in internal/session answer nothing, calmly.
		Subharnesses:        subharnesses.Registry,
		SubharnessMemory:    subharnesses.Memory,
		SubharnessLastRun:   subharnesses.LastRun,
		SubharnessRecordRun: subharnesses.Record,
		// The hand that paints, and the model it asks (internal/session's
		// tools_image.go). The pair is CONDITIONAL on the other side — a nil
		// client leaves generate_image off the belt entirely — so this is
		// allowed to hand over nothing, and does on a build that cannot open a
		// media client at all.
		//
		// The media client, on the contract's terms: nil keeps every
		// generation verb off the belt. The RESOLVER (MediaModel) is wired
		// after governance lands, because its pin rung reads RolesSource.
		Media: v3ImageGen(settings),
		// THE AMBIENT SIDE (chatv3_standing.go). It is filled for every door
		// that is a CONVERSATION — chat, resume, engine — and taken away again
		// on the --once path below, because nothing unwatched may set up
		// something that spends forever. A task node and a firing's own session
		// never see it: neither copies this config.
		Standing: v3Standing(settings.ProfileDir),
		// THE DIVISION ROAD, on by default (internal/config's DefaultSwarm). A
		// task that turns out to hold more than one worker's share may split
		// itself into parts and stay to fold them back together
		// (internal/session's task_divide.go). It is free when it does not
		// apply — the road is armed one task at a time, and a division still
		// has to name enough separate items and find a free hand before
		// anything is born — which is what makes on the right default and
		// `AFORGE_SWARM=0` the whole of the way out.
		Divide: settings.Swarm,
	}

	// What this session may do without asking, which model answers its
	// auxiliary calls, and what it may spend. All three are settings rows, and a
	// row that cannot be read stops the launch here rather than downstream.
	cfg, err = applyV3Governance(cfg, settings.ProfileDir, opts.Yolo, opts.OneModel)
	if err != nil {
		return nil, err
	}
	// AND WHO THIS SESSION IS WORKING FOR (internal/session's principal.go). The
	// two rows are the flag and the ceiling, and they are set together because
	// neither means anything without the other: --yolo alone is the approval
	// posture it has always been, and a ceiling on an attended session was
	// refused at the door. It is the ONE place they reach the engine, on
	// [applyV3Governance]'s own law — every governance seam already exists on
	// the other side, so this is a translation and never a second policy.
	cfg.Unattended = opts.Yolo
	cfg.Budget = opts.Budget
	// AND THE ACCOUNTS MANAGER IS THE PROCESS'S, not this launch's. Governance
	// leaves the field empty for exactly this reason: an account connected on
	// the panel must be connected for every conversation's belt in the same
	// breath, and two managers on one store are two caches that disagree the
	// moment either of them refreshes a token (chatv3_process.go).
	cfg.Connect = proc.Conns

	// THE MEDIA PAIR, and it is wired after governance because half of it is
	// governance's own work: the role pins the resolver reads as its second rung
	// arrive on cfg.RolesSource one line up (docs/MULTIMODAL.md Decision 5).
	//
	// The client is the one endpoint set — /images, /audio/speech, /videos —
	// and a nil one keeps every generation tool off the belt rather than on it
	// and failing. The resolver is nil for nobody: it answers "" per modality,
	// which is the same absence at a finer grain.
	cfg.Media = v3MediaClient(settings)
	cfg.MediaModel = v3MediaModel(models, settings.ProfileDir, cfg.RolesSource)
	cfg.MediaPick = v3MediaPick(models)

	// AND THE RUN DOOR IS BUILT FROM THE SAME PAIR. A saved harness may name a
	// media verb on its whitelist, and the node that reaches for it at run time
	// must resolve against the belt the designer was offered — one list, three
	// readers (internal/session's harness_belt.go). It is wired here rather than
	// in the literal above because the resolver it needs is one line up.
	cfg.RunHarness = v3RunHarness(harnesses, settings, chosen, workspace, cfg.Media, cfg.MediaModel, cfg.MediaPick)
	// AND THE SAME DOOR PUTS THAT STORE ON THE LIST. A program saved as a page is
	// a program this conversation can run, so it belongs on `/subharness` beside
	// the bundles and the compiled-in workers rather than in a catalog of its own
	// (chatv3_subharness.go's UsePages). It is wired here because the runner it
	// needs is the line above.
	subharnesses.UsePages(harnesses, cfg.RunHarness)

	// AND THIS PROCESS STARTS KEEPING TIME. Any open window takes the store's
	// lock and runs the pass; the OS timer is the backup for "no terminal open"
	// (chatv3_standing.go). It is here, beside [startPlaceSweep], because every
	// v3 door assembles through this function — and the first pass is a whole
	// interval away, so a launch that exits immediately has ticked nothing.
	if cfg.Standing != nil {
		startStandingTicks(cfg.Standing.Store)
	}

	return &v3Launch{
		Settings:     settings,
		Models:       models,
		Harnesses:    harnesses,
		Config:       cfg,
		Model:        chosen,
		Workspace:    workspace,
		SessionFile:  transcript,
		Resumed:      resumed,
		Place:        found.Place,
		Bucket:       found.Bucket,
		Subharnesses: subharnesses,
	}, nil
}

// v3SavedEffort is the rung this conversation was last left on, read back off
// its own folder, and "" for a session that has none — a fresh conversation, a
// build before the field existed, or a launch with no folder at all.
//
// A UNREADABLE FILE IS ABSENCE AND NEVER A FAILURE, exactly as [session.LoadMeta]
// answers everything else about a folder: the rung is a convenience, and a
// launch that refused to open because it could not read one would be the
// convenience costing the thing it was meant to serve.
func v3SavedEffort(place session.Place) string {
	dir := strings.TrimSpace(place.Dir)
	if dir == "" {
		return ""
	}
	meta, err := session.LoadMeta(dir)
	if err != nil {
		return ""
	}
	return meta.Effort
}

// v3TalkModel is which model this conversation opens on, and the order is the
// whole content: what the person named on the command line, then what they
// last chose and it was written down (internal/config's chatmodel.go), then
// what the environment and the built-in default say.
//
// THE SAVED CHOICE BEATS AFORGE_MODEL, which is the settings row's own law
// rather than this door's invention: the talk slot carries the variable as an
// [config.Setting.EnvDefault] and not an [config.Setting.Env], so it seeds a
// value nobody has chosen and never freezes the row. A launch that let the
// variable win would make a pick in /model revert on the next start while the
// sheet went on showing it as editable.
func v3TalkModel(asked string, settings config.Config) string {
	if named := strings.TrimSpace(asked); named != "" {
		return named
	}
	if saved := config.ChatModelAt(settings.ProfileDir); saved != "" {
		return saved
	}
	return settings.Model
}

// v3ImageGen is the hand generate_image calls through, or NIL — which is the
// tool absent rather than the tool refusing (session.Config's ImageGenClient
// states that law and internal/session's tools_image.go applies it).
//
// The two-line dance is [v3Connections]'s and Go's: a nil *provider.MediaClient
// put into an interface is a NON-nil interface holding nothing, and the belt
// tests its door with a plain nil check. Without the explicit return, a build
// that could not open a client would carry a generate_image that panicked
// instead of a belt that never mentioned one.
//
// IT RETURNS NO ERROR, for the reason [v3Search] does not. Painting is an
// accessory; a client that cannot be built is a session with one fewer tool,
// and no reason a person cannot open a conversation.
func v3ImageGen(settings config.Config) session.MediaGenerator {
	client, err := settings.MediaClient()
	if err != nil || client == nil {
		return nil
	}
	return client
}

// v3Connections hands the surface the accounts manager, and keeps a nil a nil.
//
// The two-line dance is Go's and not a choice: a nil *connect.Manager put into
// an interface is a NON-nil interface holding nothing, and the surface tests
// its door with a plain nil check. Without this, a build with no Google
// registration would open an accounts panel it could not fill and offer rows
// that could only fail — the same belt-that-lies internal/session refuses to
// build, drawn on a screen instead. A build without a registration says plainly
// that it cannot manage accounts, which is the honest answer.
func v3Connections(manager *connect.Manager) tui3.Connections {
	if manager == nil {
		return nil
	}
	return manager
}

// openV3Agent opens the session this run writes, and NEVER crashes on a
// contended one.
//
// A resumed transcript is this directory's most recent, which is exactly the
// file a second window in the same directory is already holding open. Session
// answers that with [session.ErrSessionLocked] — the right answer, since two
// writers on one journal is a corrupted journal — and the right thing to do
// with it is not to refuse to start. A person who opened a second window asked
// for a second conversation; they get one, in a new file, and are told which
// road they came down. The alternative is a launcher that fails on the second
// terminal for a reason nobody outside this tree could guess.
//
// It returns the config as it ended up, because the session file may have moved.
//
// `open` is HOW a session is built, and there are two answers: [v3OpenSession],
// which wires the adaptive runner to the agent it is building, and session.New,
// which does not. It is a parameter rather than a branch because the difference
// is the caller's own fact — the engine's surface is on another machine and
// cannot be shown a fuel gate (engine.go) — and a door deciding that for itself
// would be this file guessing who is watching.
func openV3Agent(cfg session.Config, workspace string, open func(session.Config) (*session.Agent, error)) (*session.Agent, session.Config, string, error) {
	agent, err := open(cfg)
	if err == nil {
		return agent, cfg, "", nil
	}
	if !errors.Is(err, session.ErrSessionLocked) {
		return nil, cfg, "", err
	}
	place, nextErr := v3NextSession(cfg.Place, workspace)
	if nextErr != nil {
		return nil, cfg, "", err
	}
	next, nextErr := v3PointAt(cfg, place)
	if nextErr != nil {
		return nil, cfg, "", err
	}
	cfg = next
	agent, err = open(cfg)
	if err != nil {
		return nil, cfg, "", err
	}
	return agent, cfg, "session open elsewhere — started a new one", nil
}

// ── governance: what a session may do, on whose models, for how much ────────
//
// The settings rows and one flag reach internal/session here, and this is the
// ONLY place they do. Each is a seam that already exists on the other side —
// [approval.Policy], [roles.Source], the spend rail, [search.Resolve]'s pair —
// so the mapping is a translation and never a second policy.
//
// EVERY PARSE ERROR STOPS THE LAUNCH, with the row named. A tool gate that
// silently ignored the line it could not read would be a gate that opens for
// exactly the reason nobody would think to check: a typo in the file that was
// meant to close it.
//
// THE PROJECT LAYER ENTERS HERE. cfg.Workspace is the directory this session
// runs in, so <workspace>/.aforge-v3/config.json is the repository's own answer to
// these rows, and every read below resolves project → profile → default
// (internal/config's projectconfig.go). A caller with no workspace — a test, a
// door that has not resolved a directory — gets an empty layer rather than a
// lookup in whatever directory the process happens to be sitting in.
func applyV3Governance(cfg session.Config, profileDir string, yolo, oneModel bool) (session.Config, error) {
	workspace := strings.TrimSpace(cfg.Workspace)
	policy, err := v3Policy(workspace, profileDir, yolo)
	if err != nil {
		return cfg, err
	}
	// --one-model withholds the ladder rather than filling it in. Every rung
	// below already ends at the session model when nothing answers — the roles
	// ladder falls through pin, then tier, then sessionDefault (internal/roles'
	// ResolveCall); an empty task model reads the live conversation model
	// (internal/session's defaultTaskModel); an empty fallback chain hops
	// nowhere. So the flag is three unset states this build has always handled,
	// NOT a fourth resolution path that could drift from the other three.
	//
	// The media slots are deliberately untouched. Vision, image, speech and
	// video are capability-qualified — a text model cannot answer view_image —
	// so settling them on the session model would not make the run single-model,
	// it would make it broken. The flag says every TEXT call, and means it.
	// The rows are not even read under the flag. Reading them only to discard
	// the answer would make a malformed pins row stop a launch that had already
	// said it does not care what the pins say.
	var source func(string) (string, bool)
	if !oneModel {
		if source, err = v3RolesSource(workspace, profileDir); err != nil {
			return cfg, err
		}
	}
	rail, err := config.ProjectFloatAt(workspace, profileDir, config.KeySpendRail)
	if err != nil {
		return cfg, err
	}
	cfg.ApprovalPolicy = policy
	cfg.RolesSource = source
	cfg.SpendRailUSD = rail
	// The fallback chain reads PROFILE-ONLY, like the search keys below and
	// unlike the three rows above it. A repository that could answer this could
	// send a visitor's next turn — and their credit — to a model they never
	// picked, by being cloned. Which models a person's questions may go to is
	// theirs to say.
	cfg.ModelFallbacks = config.ParseModelFallbacks(config.ModelFallbacksAt(profileDir))
	if oneModel {
		// A chain that hops to a second model on a failure is the one remaining
		// way a single-model run stops being one, and it fires exactly when
		// nobody is watching. Empty is "no hop", which is what this build has
		// always done for a person who set no chain.
		cfg.ModelFallbacks = nil
		// AND THE CATALOG'S GUESS WITH IT. Nilling the row alone was not enough
		// and quietly never had been: the chain falls through to NearestModels
		// when no row is written (internal/provider's fallbackChain), so a
		// single-model run that met a refusal would have walked to two models the
		// catalog thought were similar — chosen by nobody, and attributed to a
		// measurement cell that says it rode one model. Both seams are the same
		// question, so both are withheld by the same flag.
		cfg.NearestModels = nil
	}
	// The guardian (internal/session's guardian.go) reads PROFILE-ONLY, unlike
	// the two rows above it, and the reason is the one that keeps the search keys
	// out of the project layer too: a repository that could turn this on would be
	// appointing a stand-in for a visitor who never agreed to have one. The rules
	// a repository may state are still the rules — it can say what to ask about;
	// it cannot say who answers.
	cfg.Guardian = config.GuardianEnabledAt(profileDir)
	cfg.TaskAutoApproveSeconds = config.TaskAutoApproveAt(profileDir)
	// Which model the work that leaves this conversation runs on, PROFILE-ONLY
	// for the reason the fallback chain above is: a repository that could answer
	// this could send a visitor's work — and their credit — to a model they never
	// picked, by being cloned.
	cfg.TaskModel = config.TaskModelAt(profileDir)
	// HOW HARD EVERYTHING HERE THINKS, when nothing nearer to the work has said.
	// PROFILE-ONLY for the same reason the row above it is: a repository that
	// could answer this could spend a visitor's money on a depth they never
	// asked for, by being cloned. It is the ladder's last rung and every model
	// call in the session reaches it through one resolver (internal/effort).
	cfg.DefaultEffort = config.DefaultEffortAt(profileDir)
	// A CONVERSATION IS A PERSON'S TURN AND SO IS THE WORK THEY HAND OUT. The
	// role is set here rather than defaulted in the engine so that a session
	// built without a door — a test, a headless --once — is not silently opted
	// into paying for depth nobody configured.
	cfg.EffortRole = effort.RoleChat
	if oneModel {
		// Empty is not "no model", it is "the model this conversation is on
		// right now" (internal/session's defaultTaskModel), which is precisely
		// what the flag promises for work that leaves the conversation.
		cfg.TaskModel = ""
	}
	cfg.TaskAudit = config.TaskAuditEnabledAt(profileDir)
	// Whether a reply that comes apart is cut and asked again. PROFILE-ONLY, and
	// the reason is not trust this time but taste: it is a judgement about
	// somebody's own replies, and a repository has no business turning off a
	// visitor's protection against a model that has stopped writing language.
	cfg.ReplyGuardOff = !config.ReplyGuardEnabledAt(profileDir)
	// And who decides when that check comes back with nothing. PROFILE-ONLY for
	// the reason the audit row above it is: a repository that could set this
	// would be deciding, by being cloned, that a visitor's work gets accepted by
	// a model rather than by the visitor.
	cfg.TaskSettle = config.TaskSettleAt(profileDir)
	cfg.TaskRepairRounds = config.TaskRepairRoundsAt(profileDir)
	// How much of the work that leaves this conversation happens at once: the
	// person's own cap, and the two readings of this machine that hold the next
	// task back whatever the cap says (internal/session's task_pressure.go).
	cfg.TaskParallel = config.TaskParallelAt(profileDir)
	cfg.TaskMaxLoad = config.TaskMaxLoadAt(profileDir)
	cfg.TaskMinFreeMB = config.TaskMinFreeMBAt(profileDir)
	cfg.SearchProvider, cfg.SearchFetcher = v3Search(profileDir)
	// The accounts manager is NOT resolved here, and it is the one row in this
	// function that is not. It is a handle rather than a setting: one per
	// process, built at the door and assigned by the launch
	// (chatv3_process.go's [v3Process.Conns]), because two managers on one store
	// are two caches with no way to tell each other that a token has moved.
	// How this session chooses among the endpoints serving its model — and it is
	// read as the CHOICE rather than as the resolved default, so an unwritten row
	// arrives here empty. The adapter needs to be able to tell "nobody said" from
	// "somebody said latency": with nothing said it routes a person's own turn by
	// speed and a task node or an errand by price, and with a word written that
	// word wins outright (internal/provider's velocity.go). The parse is still
	// total, so a word this build does not know leaves the row unset rather than
	// taking routing away.
	if word := config.RoutingChoiceAt(profileDir); word != "" {
		cfg.Routing, _ = provider.ParseRoutingStrategy(word)
	}
	return cfg, nil
}

// v3Search resolves the web-search pair this session's belt calls through: the
// three settings rows in, [search.Resolve]'s answer out.
//
// IT RETURNS NO ERROR, and that is a statement about the layer rather than an
// omission. Every rung of the resolution ladder ends in a plug that needs no
// key (internal/search states this and tests it), so there is no configuration
// — no key, a stale pin, a garbled row — that can leave a person unable to look
// something up. A missing key is not a failure but a rung; a pin naming a plug
// this build does not have falls through to auto rather than taking search
// away. The only outcome this call cannot produce is a launch that fails
// because of search, which is the correct set of outcomes for an accessory.
//
// A nil half is therefore not an error either. It is what a build whose
// registry is empty answers, and internal/session reads it as "leave that tool
// off the belt" — a model that is never told about a tool it cannot reach.
//
// THE ROWS DO NOT GO THROUGH THE PROJECT LAYER, unlike the governance rows
// above, and the keys are why: a repository that could answer search.exaKey
// could spend a visitor's Exa credit, and one that could answer search.provider
// could redirect where a visitor's questions are sent by being cloned. Both are
// the PERSON's rows in the sense internal/config's allowlist means it, so they
// resolve profile-and-environment only.
func v3Search(profileDir string) (search.Provider, search.Fetcher) {
	return search.Resolve(v3SearchOptions(profileDir))
}

// v3SearchOptions is the mapping itself, split out so it can be read and tested
// without a registry: the pin from the choice row (auto meaning no pin, which
// internal/search spells as the empty string), and the two credentials from the
// environment or the sheet.
func v3SearchOptions(profileDir string) search.Options {
	pin := config.SearchProviderAt(profileDir)
	if pin == config.SearchProviderAuto {
		pin = ""
	}
	return search.Options{
		Provider: pin,
		ExaKey:   config.ExaKeyAt(profileDir),
		JinaKey:  config.JinaKeyAt(profileDir),
	}
}

// v3Connect resolves the person's connected accounts (internal/connect): the
// Google and Slack applications in, a manager out — or NIL, which is the whole
// feature absent.
//
// The nil is still the law v3Search's nil half states — a belt must never carry
// a tool whose one answer is "not configured", and [session.Config] says why
// that is strictly worse for a model than never being told. What has changed is
// how rarely it applies: the catalog opens a few hundred accounts on a key the
// person already holds, so there is something to connect on every machine, and
// only a store this process cannot open at all leaves nothing.
//
// IN PRACTICE BOTH ARE ALWAYS THERE now, because a build ships applications of
// its own as the last rung under the environment and the sheet (internal/config's
// connect_defaults.go), so the guards below stand for the case where somebody
// has emptied one rather than the ordinary one. Having an application is not
// having an account: connecting still opens the service in the person's browser
// and waits for them to approve it, and the tools say "not connected" until they
// do.
//
// IT RETURNS NO ERROR for the reason v3Search does not. Accounts are an
// accessory; a garbled row, a half-filled pair, a manager that cannot open its
// own store are all reasons to have no connect tools, and none of them is a
// reason a person cannot open a conversation.
//
// THE ROWS DO NOT GO THROUGH THE PROJECT LAYER, like the search keys and unlike
// the governance rows above: a repository that could answer google_oauth_client
// could ask a visitor to connect their mail to an application the repository
// chose, by being cloned. Whose application asks for a person's account is the
// person's row in exactly the sense internal/config's allowlist means it.
// EACH APPLICATION ROW GATES ITS OWN SERVICE AND NOTHING ELSE. Most of what can
// be connected is opened by a key the person already holds and needs nothing
// registered anywhere, so a machine with neither browser application still has
// a manager and still has the accounts list — it simply has neither service.
func v3Connect(profileDir string) *connect.Manager {
	credentials := map[string]connect.ClientCredential{}
	if id, secret := config.GoogleOAuthClientAt(profileDir); strings.TrimSpace(id) != "" && strings.TrimSpace(secret) != "" {
		credentials["google"] = connect.ClientCredential{ID: id, Secret: secret}
	}
	if id := config.SlackOAuthClientAt(profileDir); strings.TrimSpace(id) != "" {
		credentials["slack"] = connect.ClientCredential{ID: id, Public: true}
	}
	manager, err := connect.NewManager(profileDir, credentials)
	if err != nil {
		return nil
	}
	return manager
}

// v3Policy builds the tool gate from the two approval rows.
//
// The mode row cannot fail — an unreadable value there reads as the strictest
// of the three (internal/config), which is the one direction a garbled setting
// may be wrong in. The exceptions row can, and does, loudly.
//
// --yolo replaces the DEFAULT and nothing else. A person who wrote
// `bash:prompt` still gets asked about bash: the flag is "stop asking me about
// the ordinary things", not "forget what I wrote down".
func v3Policy(workspace, profileDir string, yolo bool) (*approval.Policy, error) {
	mode, err := config.ProjectStringAt(workspace, profileDir, config.KeyToolApprovalMode)
	if err != nil {
		return nil, err
	}
	if yolo {
		mode = string(approval.ActionAllow)
	}
	raw := map[string]any{"default": mode}
	text, err := config.ProjectStringAt(workspace, profileDir, config.KeyToolApprovals)
	if err != nil {
		return nil, err
	}
	// The repository's exceptions REPLACE the person's, whole (internal/config
	// states the merge law): a rule set assembled from two files is a rule set
	// neither file's reader could read back. Whichever file wins, what it says
	// lands ON TOP of the built-in floor below rather than instead of it.
	exceptions := v3BuiltinApprovals()
	if text != "" {
		tools, err := config.ParseToolApprovals(text)
		if err != nil {
			return nil, fmt.Errorf("settings row %q: %w", config.KeyToolApprovals, err)
		}
		for tool, action := range tools {
			exceptions[tool] = action
		}
	}
	raw["tools"] = exceptions
	// The bash rules, which are the row a remembered "always, this command"
	// lands in (internal/config's approvalmemory.go). They are ORDERED and the
	// order is honoured as written: first match wins, so a deny somebody put at
	// the top of the row outranks anything a consent card appended below it.
	// --yolo does not touch them either — the flag replaces the default and
	// nothing a person wrote down.
	rules, err := config.ProjectStringAt(workspace, profileDir, config.KeyBashApprovals)
	if err != nil {
		return nil, err
	}
	if rules != "" {
		parsed, err := config.ParseBashApprovals(rules)
		if err != nil {
			return nil, fmt.Errorf("settings row %q: %w", config.KeyBashApprovals, err)
		}
		patterns := make([]any, 0, len(parsed))
		for _, rule := range parsed {
			patterns = append(patterns, map[string]any{"match": rule.Match, "approval": rule.Action})
		}
		raw["bash.patterns"] = patterns
	}
	policy, err := approval.Load(raw)
	if err != nil {
		return nil, fmt.Errorf("settings rows %q, %q and %q: %w",
			config.KeyToolApprovalMode, config.KeyToolApprovals, config.KeyBashApprovals, err)
	}
	return &policy, nil
}

// v3BuiltinApprovals is the floor under the tool exceptions row: the handful of
// calls this gate has never had a reason to ask about, seeded fresh on every
// build so that what a person wrote lands ON TOP of them and not INSTEAD of
// them.
//
// It used to be an either/or — these built-ins while the row was empty, the
// person's rules the moment it was not — and that shape is what produced the
// complaint this floor answers. The consent card's "always" writes ONE entry
// into that row (internal/config's approvalmemory.go), so the first time
// anybody pressed always on any tool at all, the seed vanished and read, grep,
// find, ls and jobs fell back to the blanket mode, which asks. The keystroke
// whose whole purpose is to be asked less permanently increased the asking, in
// a place nothing on screen connected to the key that had been pressed.
//
// A RULE THE PERSON WROTE STILL WINS FOR THE TOOL IT NAMES. `read:prompt` is a
// sentence about read and it is honoured, because a floor nobody could stand on
// would be a rule set with a part that cannot be turned off. What the floor
// takes away is only the silent part: a rule written about one tool now says
// nothing whatsoever about any other.
//
// WHAT IS ON IT, and why each entry is safe to leave off the asking:
//
//   - Pure reads of this machine — read, grep, find, ls. Nothing here changes a
//     file, so the blanket mode's prompt is free to land where it matters, on
//     bash, edit and write.
//   - jobs, whose list and output are reads of processes the person already
//     started. Its kill is not on the floor: it inherits the blanket mode,
//     which asks.
//   - The agent's own bookkeeping — remember, track and recall. These write to
//     and read from the memories and working state it keeps for itself
//     (internal/session's memory.go and state.go); no hand outside this process
//     reads them, and asking somebody to approve the agent writing itself a
//     reminder is asking about the wrong thing.
//   - manual, which reads pages compiled into this binary and touches no disk
//     at all (internal/session's tools_manual.go). A person who asks "what can
//     you do" and is answered with a permission prompt has been asked to
//     approve the program looking up its own documentation.
//   - settings, which reads the person's own settings rows back through the
//     registry (internal/session's tools_settings.go). It is manual's shape one
//     file over — the answer to "what is my daily budget" is a lookup, and the
//     credential rows read MASKED through the registry itself
//     ([config.Setting.Secret]), so there is nothing here a prompt would be
//     protecting.
//
// commit is DELIBERATELY NOT HERE, and it is the interesting half of the split.
// It is the fifth hand on the same working state, but it is the only one that
// declares a tracked subgoal FINISHED, and a session that can mark its own work
// done without anyone being asked is a session that can talk itself into done.
// The other four record and read; this one makes a claim.
//
// change_setting is NOT HERE FOR THE SAME REASON, harder. Changing somebody's
// configuration is an ACT and not a read — it writes a file that outlives the
// conversation — so it goes to the person like edit and write do. That split is
// the whole argument for the settings pair being two tools rather than one with
// actions: a rule is written per tool name, so one tool could not have been
// free to read and asked about to write.
//
// Nothing on this list acts outside this machine, so internal/approval's floor
// under calls made in the person's name is untouched by every entry on it — as
// is the critical-command table, which sits above this whole row either way.
func v3BuiltinApprovals() map[string]any {
	return map[string]any{
		"read": "allow", "grep": "allow", "find": "allow", "ls": "allow",
		"jobs":     "allow",
		"remember": "allow", "track": "allow", "recall": "allow",
		"manual": "allow", "settings": "allow",
	}
}

// v3Memory opens the person's brain for this conversation, or hands over
// nothing.
//
// NOTHING IS A COMPLETE ANSWER HERE, and it is two different answers spelled
// the same way on purpose. Memory turned off in the settings opens no store at
// all, which is what makes "no block and no calls" a property of the wiring
// rather than a branch in every caller (internal/session's memory.go states the
// law). A store that would not open — a locked file, a disk with nothing left,
// a database an older build wrote — is the SAME answer, said once on stderr:
// a person who typed `aforge` wanted a conversation, and refusing them one
// because a memory file is unhappy would be losing the whole product to the
// least of its parts.
//
// It is the same graph.db every other surface in this binary opens
// ([defaultChatDB]). Memories are the person's, not a conversation's, and a
// second file beside it would be a second set of them that nothing else could
// read.
//
// A FIRST RUN IS NOT ONE OF THOSE UNHAPPY CASES, and for a long time it was.
// The state root does not exist on a machine that has never run aforge, SQLite
// creates database files but never the directories holding them, and the
// resulting complaint came back spelled `out of memory (14)` — so the first
// launch on a new machine reported a memory problem it did not have and then
// held nothing, for as long as that person kept using it. The directory is now
// made by [store.Open] itself, which is the one door every caller goes through,
// and what reaches the line below is only ever a real reason: it names the file
// and says what the disk said about it.
func v3Memory(profileDir string) *store.Store {
	if !config.MemoryEnabledAt(profileDir) {
		return nil
	}
	brain, err := store.Open(defaultChatDB())
	if err != nil {
		fmt.Fprintln(os.Stderr, "memory is off for this session: "+err.Error())
		return nil
	}
	return brain
}

// ── the two reading seams the places open onto ──────────────────────────────

// v3Brain is the store as the memory place asks for it.
//
// IT IS AN ADAPTER AND NOT AN INTERFACE THE STORE HAPPENS TO FIT, for one
// reason: the surface's seam is named in the surface's own words — `Snapshot`,
// `ChangedSince` — while the store prefixes every one of its methods with the
// table they read, because it holds a dozen tables and `Snapshot` alone would
// mean nothing there. Two vocabularies, one join, written down here where the
// door already owns every other translation between the two packages.
//
// A NIL STORE STAYS NIL THROUGH IT. Memory off means no store, and a typed nil
// inside a non-nil interface would turn "the place is absent" into "the place
// panics the first time somebody presses alt+4" — the classic shape of that
// bug, refused here rather than guarded against in the surface.
type v3Brain struct{ brain *store.Store }

func (s v3Brain) Snapshot(limit int) (store.MemoryShelves, error) {
	return s.brain.MemorySnapshot(limit)
}

func (s v3Brain) ChangedSince(t time.Time) (int, int, error) {
	return s.brain.MemoryChangedSince(t)
}

func (s v3Brain) ListMemories(scope string, limit int) ([]store.Memory, error) {
	return s.brain.ListMemories(scope, limit)
}

func (s v3Brain) UpdateMemory(id, title, text string, tags []string) error {
	return s.brain.UpdateMemory(id, title, text, tags)
}

func (s v3Brain) ForgetMemory(id string) error  { return s.brain.ForgetMemory(id) }
func (s v3Brain) RestoreMemory(id string) error { return s.brain.RestoreMemory(id) }

func (s v3Brain) MemoryProvenance(id string) (string, string, time.Time, error) {
	return s.brain.MemoryProvenance(id)
}

// v3MemorySeam is the adapter, or nothing at all for a store that was never
// opened — see [v3Brain] for why the nil has to be answered here.
func v3MemorySeam(brain *store.Store) tui3.MemoryStore {
	if brain == nil {
		return nil
	}
	return v3Brain{brain: brain}
}

// v3SearchSeam is the same store as the search place asks for it: one full-text
// query across every thread. It is a SECOND seam beside the memory one because
// the two capabilities fail apart — a build with memory off has neither today,
// and the day one of them moves to a different store the other does not have to
// move with it.
func v3SearchSeam(brain *store.Store) tui3.SearchStore {
	if brain == nil {
		return nil
	}
	return brain
}

// v3RolesSource is the closure internal/roles reads its ladder through: the four
// tier models under [roles.TierKey], the per-role pins under [roles.PinKey].
//
// IT IS LIVE. It used to be resolved once, at boot, on the argument that two
// calls in one conversation must not answer to different settings — and the
// crew is what makes that argument the wrong way round. A person who types
// `/crew max` because the planner is not thinking hard enough has said something
// about the run they are about to start, not about the next launch, and a source
// that made them restart to be heard would be a knob that does nothing on the
// surface that offers it.
//
// The seam is a SNAPSHOT INVALIDATED BY A GENERATION COUNTER
// ([config.SettingsGeneration]), for reasons the two obvious alternatives fail:
//
//   - Re-reading config on every Resolve would put a file read on the path of
//     every auxiliary call — twice a turn for the reflex pair alone — for a file
//     that changes once a week. internal/config caches nothing on purpose.
//   - Invalidating from the settings panel's write hook would only see the
//     panel. The `change_setting` tool writes the same rows from inside a turn,
//     and /crew writes four of them at once.
//
// A counter bumped by the ONE persisted writer sees all of them, costs an atomic
// load per call when nothing has moved, and holds the lock only to rebuild. A
// mid-session change is honored by the NEXT crew call, which is the promise the
// settings rows now make.
//
// What it does not see is stated where the counter is: a config file edited by
// another process, and the project layer's file at all. Both land on the next
// launch, which is what every settings row did before this.
func v3RolesSource(workspace, profileDir string) (func(string) (string, bool), error) {
	crew := &v3Crew{workspace: workspace, profileDir: profileDir}
	// The first build happens HERE rather than lazily, so a malformed pins row
	// still stops the launch with the row named — which is the law this whole
	// governance block keeps (applyV3Governance says why).
	if err := crew.rebuild(); err != nil {
		return nil, err
	}
	return crew.read, nil
}

// v3Crew is the live reading of the four tier rows and the pins.
type v3Crew struct {
	workspace, profileDir string

	mu sync.RWMutex
	// generation is [config.SettingsGeneration] as of the snapshot below. Zero
	// is impossible after the first build, so there is no "never read" state to
	// spell separately.
	generation uint64
	values     map[string]string
	// err is the last rebuild's refusal, KEPT AND SERVED rather than swallowed.
	// A pins row somebody has just broken must not silently un-pin every role —
	// that would move work onto another model without saying so — so a failed
	// rebuild keeps serving the last good snapshot and the failure is what the
	// next settings read will show them.
	err error
}

// read is [roles.Source]: one key, and the fast path is an atomic load and a
// read lock.
func (c *v3Crew) read(key string) (string, bool) {
	if config.SettingsGeneration() != c.generationNow() {
		// A rebuild that fails leaves the old snapshot in place, so the error is
		// dropped here on purpose: this is the resolution path, and the honest
		// answer to "which model" is the last one that parsed.
		_ = c.rebuild()
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, ok := c.values[key]
	if !ok || strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

func (c *v3Crew) generationNow() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.generation
}

// rebuild reads the rows and replaces the snapshot. It reads BEFORE taking the
// write lock so a slow disk cannot hold a concurrent turn's resolution.
func (c *v3Crew) rebuild() error {
	generation := config.SettingsGeneration()
	values, err := c.snapshot()
	c.mu.Lock()
	defer c.mu.Unlock()
	// The generation moves either way. A rebuild that refused must not be
	// retried on every single call — that would put a file read back on the hot
	// path, which is the thing this seam exists to avoid — so the refusal is
	// recorded and the next write is what triggers another attempt.
	c.generation, c.err = generation, err
	if err != nil {
		return err
	}
	c.values = values
	return nil
}

// snapshot is one reading of every key the ladder can ask for.
func (c *v3Crew) snapshot() (map[string]string, error) {
	// THE TWO PROJECT-LAYER TIERS. A repository may say which model does the
	// bulk work and which one checks it (config.ProjectKeys), because that is a
	// fact about the work in it.
	low, err := config.ProjectStringAt(c.workspace, c.profileDir, config.KeyTierLowModel)
	if err != nil {
		return nil, err
	}
	high, err := config.ProjectStringAt(c.workspace, c.profileDir, config.KeyTierHighModel)
	if err != nil {
		return nil, err
	}
	// AND THE TWO READ FROM THE PROFILE ALONE. [config.ProjectKeys] does not
	// carry either row, so asking the project layer for one is an error rather
	// than a fall-through.
	//
	// The reflex tier: the two calls that ride it are made TWICE EVERY TURN
	// (internal/reflex), so a reflex resolving to the conversation's model is not
	// thrift misconfigured, it is the most expensive model in the build answering
	// the cheapest question in it. The mastermind tier: it plans adaptive runs
	// and designs saved harnesses, and a repository that could point it at a
	// model would be spending a visitor's credit on the run it asked for.
	values := map[string]string{
		roles.TierKey(roles.TierReflex):     config.TierModelAt(c.profileDir, config.ModelTierReflex),
		roles.TierKey(roles.TierMastermind): config.TierModelAt(c.profileDir, config.ModelTierMastermind),
		roles.TierKey(roles.TierLow):        low,
		roles.TierKey(roles.TierHigh):       high,
	}
	text, err := config.ProjectStringAt(c.workspace, c.profileDir, config.KeyModelRoles)
	if err != nil {
		return nil, err
	}
	// The pins replace wholesale for the reason the approvals do, one line up.
	if text != "" {
		pins, err := config.ParseModelRoles(text)
		if err != nil {
			return nil, fmt.Errorf("settings row %q: %w", config.KeyModelRoles, err)
		}
		for role, model := range pins {
			values[roles.PinKey(roles.Role(role))] = model
		}
	}
	return values, nil
}

// v3Catalog is the ONE question this door asks a model catalog, and it is a
// question that never waits. It is an interface rather than *catalog.Catalog so
// a test can answer it with rows of its own — no cache file, no fetch, no
// fifteen-second timeout in a unit test.
type v3Catalog interface {
	// ModelsNow is the catalog's rows if it already has them, and nil while a
	// lazily loaded one is still warming.
	ModelsNow() []catalog.Model
}

// v3Models is the catalog as the v3 surface wants it: id, window, the three
// per-token prices and the arena score, THE WHOLE LIST, in the catalog's own
// order.
//
// THE DOOR STOPS STARVING THE PICKERS (docs/MULTIMODAL.md Decision 6). This used
// to drop every row that answered in anything but text, which read as a
// reasonable narrowing and was in fact the surface's supply: the settings
// sheet's drawing row opens a picker over THIS list, so a list with no drawing
// model in it is a picker that cannot be answered — and the on-disk cache,
// written from here, carried the same hole to the next launch.
//
// So the filtering moves to where the question is asked. Every list that wants
// chat models applies the chat law itself, at the moment it is drawn:
// [v3TaskModels] here, [app.modelList] and each slot's own predicate in
// internal/tui3 (models.go's modelFilter). One list arrives; each reader asks
// its own question of it.
//
// The prices ride along because the surface has to price something the session
// cannot: what a turn's prompt-cache reads saved, which is cached tokens times
// the gap between the prompt price and the cache-read price (internal/tui3's
// savings note). They come off the /models fetch that already happens, so
// carrying them costs one more field per row in a file that is already written.
//
// Nil while the catalog is warming — which is not a failure but the picker's
// cue to read ~/.aforge/v3/models.json and then its built-ins (internal/tui3
// models.go states that order and applies it).
func v3Models(models v3Catalog) []tui3.Model {
	if models == nil {
		return nil
	}
	rows := models.ModelsNow()
	out := make([]tui3.Model, 0, len(rows))
	for _, row := range rows {
		model := tui3.Model{
			ID:            row.ID,
			ContextLength: row.ContextLength,
			ArenaElo:      row.ArenaElo,
			Output:        row.OutputModalities,
			// What the model READS travels beside what it answers in, because
			// the surface asks both questions and can only answer them from what
			// it was handed: the chat law needs text IN (a transcription model
			// answers in text and takes sound), and the vision slot needs image
			// in. A row that arrived with only its output side would be filtered
			// on half the facts and would look, from the picker, exactly like a
			// row that had been checked.
			Input: row.InputModalities,
			// The published answer to "may this call carry a reasoning knob",
			// and the only thing that lets the picker offer ctrl+t on a row.
			// Either spelling counts: a model that takes `reasoning` can be
			// asked to think, and one that takes `reasoning_effort` can be told
			// how hard — the surface offers levels on both and lets the adapter
			// resolve what actually travels (catalog's ReasoningWord states the
			// same three states in words).
			Reasoning: row.Reasons() || row.ReasoningLevels(),
		}
		// PriceUnknown is OpenRouter's "-1", which it uses for its own routers:
		// they charge whatever the model they pick charges, and nobody yet knows
		// what that is. Passing the parsed numbers through anyway would tell the
		// surface a router's prompt token is free and let it report a saving that
		// is not a fact (catalog.Model.PriceUnknown).
		if !row.PriceUnknown {
			model.PromptPrice = row.PromptPrice
			model.CompletionPrice = row.CompletionPrice
			model.CacheReadPrice = row.CacheReadPrice
		}
		out = append(out, model)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// v3TaskModels is the list a task's `model` argument is resolved against
// (session.Config.TaskModels): the ids of every model this install can hold a
// conversation with, which is exactly the set the picker offers — a node is an
// agent with the same belt, so a model it could not talk through is not a model
// work can be handed to.
//
// It NEVER WAITS, and nil while the catalog is warming is the honest answer:
// internal/session reads that as "nobody can say" and takes the named model as
// written rather than refusing an id it has no list to check.
func v3TaskModels(models v3Catalog) func() []string {
	return func() []string {
		// THE CHAT LAW IS APPLIED HERE, not at the door. A node is an agent with
		// the same belt, so a model work can be handed to is a model somebody
		// could hold a conversation with — and since the door now carries the
		// whole catalog (Decision 6), this list is where that question gets
		// asked. It is internal/tui3's own predicate rather than a second copy
		// of it, so the picker and the task argument cannot come to disagree
		// about what a chat model is.
		rows := tui3.ChatModels(v3Models(models))
		if len(rows) == 0 {
			return nil
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			if id := strings.TrimSpace(row.ID); id != "" {
				ids = append(ids, id)
			}
		}
		return ids
	}
}

// v3AnswersText is the OUTPUT half of the chat law, asked of a published
// modality list: text, and nothing but text.
//
// A row that declares nothing is KEPT, and that is this predicate's own reading
// of silence rather than a disagreement with the rest of the surface. THE ONE
// SILENCE LAW (docs/MULTIMODAL.md Decision 6) is about CAPABILITY: an
// unpublished modality list means text-in/text-out and nothing more — no
// vision, no drawing, no sound. Text is what silence means, so a silent row
// answering "yes, text" is that law stated rather than an exception to it, and
// it is what keeps a cache written before modalities travelled from emptying
// the model picker.
func v3AnswersText(outputs []string) bool {
	if len(outputs) == 0 {
		return true
	}
	// Text-out and NOTHING else: an image model that captions what it draws
	// publishes ["image","text"], and letting it through puts a drawing model
	// in a chat picker. The door's law is "a model you can talk to", and a
	// published modality list is the only honest witness to it.
	for _, modality := range outputs {
		if !strings.EqualFold(strings.TrimSpace(modality), "text") {
			return false
		}
	}
	return true
}

// v3SeesImages is the vision gate: the closure internal/session asks before it
// assembles a message with pictures in it (session.Config.SupportsImages, and
// internal/tui3's attachment tray is what makes one askable).
//
// It answers from the catalog's published `architecture.input_modalities` and
// from nothing else — no id patterns, no vendor guesses. A model that says it
// reads images reads images; anything else is a "no" this door can defend.
//
// IT NEVER WAITS, for the reason every other question this file asks a catalog
// never waits: the rows are read through [v3Catalog.ModelsNow], which is nil
// while a lazily loaded catalog is still warming. That is why it is a closure
// read per message rather than a value resolved at boot — a cold cache would
// otherwise pin "cannot see" onto a session for its whole life, and the answer
// is wanted at the moment somebody attaches a photo, which is minutes later.
//
// AND WHILE THE CATALOG IS COLD IT READS THE DISK CACHE, which never waits
// either — it is one small file internal/tui3 wrote after a fetch, and it now
// holds the whole catalog rather than the chat rows alone (Decision 6). Without
// that rung the FIRST minute of every launch answered "cannot see" for a model
// that can, so a photo attached in the first minute went as its text placeholder
// and the person was told to switch to a model with vision while already on one.
// The file is read at most once per session: it is the same rows for the whole
// warming window, and re-reading it per message would put I/O on the message
// path to learn nothing new.
func v3SeesImages(models v3Catalog) func(string) bool {
	var once sync.Once
	var cached []tui3.Model
	return func(model string) bool {
		model = strings.TrimSpace(model)
		if model == "" {
			return false
		}
		if models != nil {
			if rows := models.ModelsNow(); len(rows) > 0 {
				for _, row := range rows {
					if strings.EqualFold(strings.TrimSpace(row.ID), model) {
						return v3ReadsImages(row.InputModalities)
					}
				}
				// The catalog HAS answered and does not carry this id — a
				// hand-typed slug, a model this router never listed. That is a
				// no on the same terms as an unpublished modality.
				return false
			}
		}
		once.Do(func() { cached = tui3.CachedModels() })
		for _, row := range cached {
			if strings.EqualFold(strings.TrimSpace(row.ID), model) {
				return v3ReadsImages(row.Input)
			}
		}
		return false
	}
}

// v3ReadsImages is the modality test itself.
//
// SILENCE IS NO, and that is THE ONE SILENCE LAW rather than this door's local
// opinion: an unpublished modality list means text-in/text-out and nothing more
// (docs/MULTIMODAL.md Decision 6). A media capability is never assumed, only
// published. internal/tui3's seesImages reads the same silence the same way, so
// the list a person picks a vision model out of and the gate that decides
// whether a photo travels cannot disagree about one row.
//
// The cost of the law falling this way is stated plainly: a message assembled
// with a base64 photo in it, sent to a model that cannot read one, comes back as
// a provider error about a content part, while an unknown modality is a refusal
// the person can act on ("switch to a model with vision").
func v3ReadsImages(inputs []string) bool {
	for _, modality := range inputs {
		if strings.EqualFold(strings.TrimSpace(modality), "image") {
			return true
		}
	}
	return false
}

// v3NearestModels is the last resort of the endpoint-refusal chain: when the
// person has written no models.fallbacks row, where should a turn go that
// nothing serving its model would accept?
//
// It answers from the catalog's own rows and only for a model the catalog knows
// (internal/catalog's NearestModels states what "nearest" is checked against),
// and it NEVER WAITS, for the reason [v3SeesImages] never does — this is asked
// on the request path, by an adapter that has just been refused, with somebody
// watching the turn. A cold catalog answers nil, and the chain then ends in a
// diagnosis naming what to change rather than on a model nobody chose.
//
// TWO, and no more. The chain is bounded on its own side as well
// (internal/provider's maxFallbackModels), and both bounds say the same thing:
// past a couple of tries the honest move is to stop and let the person pick.
func v3NearestModels(models *catalog.Catalog) func(string) []string {
	return func(model string) []string { return models.NearestModels(model, 2) }
}

// v3Window fills session.Config.ContextWindow: how many tokens this session's
// model accepts, according to whatever the catalog can say WITHOUT a fetch.
//
// Zero when nobody can say — a cold cache, an id the catalog has never carried,
// a row that published no figure — and zero is the right answer to hand the
// session, which then keeps its own conservative default rather than sizing
// compaction off a guess. [warmV3Models] corrects it in place once the catalog
// resolves.
func v3Window(models v3Catalog, model string) int {
	return v3ContextWindow(v3Models(models), model)
}

func v3ContextWindow(models []tui3.Model, model string) int {
	model = strings.TrimSpace(model)
	for _, candidate := range models {
		if strings.EqualFold(candidate.ID, model) {
			return candidate.ContextLength
		}
	}
	return 0
}

// warmV3Models is the after-the-fact half, and it runs off the launch path
// because it WAITS: the catalog's own resolution, which on a cold cache is a
// network round-trip.
//
// It does two things when that lands. The session learns the real window of the
// model it started on, so a 1M-token model stops compacting at 128k — but only
// if the person has not already switched models, because a later choice is a
// better fact than this one. And the picker's cache is refreshed, so the NEXT
// launch on this machine opens the full list in its first frame.
//
// The cache is written only for rows that actually came off the network
// (FetchedAt is zero for the built-in fallbacks), which is what keeps a machine
// that has never reached OpenRouter from caching five hardcoded names as if
// they were the catalog.
func warmV3Models(models *catalog.Catalog, agent *session.Agent, started string) {
	if models == nil || agent == nil {
		return
	}
	if window := models.ContextLength(started); window > 0 && agent.Model() == started {
		agent.SetContextWindow(window)
	}
	if models.FetchedAt().IsZero() {
		return
	}
	_ = tui3.WriteModelCache(v3Models(models))
}

// runChatV3Once is the smoke-test door: one message, plain text out, no
// terminal ownership. Everything the surface would draw as chrome goes to
// stderr and only what the model said goes to stdout, so a probe can compare
// stdout with the sentence it asked for.
func runChatV3Once(cfg session.Config, text, level string, resumed bool) error {
	if resumed && cfg.SessionFile != "" {
		fmt.Fprintln(os.Stderr, "resumed "+cfg.SessionFile)
	}
	agent, cfg, notice, err := openV3Agent(cfg, cfg.Workspace, v3OpenSession)
	if err != nil {
		return err
	}
	agent.SetReasoning(level)
	if notice != "" {
		fmt.Fprintln(os.Stderr, notice+": "+cfg.SessionFile)
	}
	defer func() { _ = agent.Close() }()

	events, err := agent.Submit(context.Background(), text)
	if err != nil {
		return err
	}
	// wrote tracks whether the reply has begun, so a tool line never opens the
	// output with a stray blank line and never lands mid-sentence.
	wrote := false
	newline := func() {
		if wrote {
			fmt.Println()
			wrote = false
		}
	}
	var failure error
	for event := range events {
		switch event.Kind {
		case session.EventTextDelta:
			if event.Text == "" {
				continue
			}
			fmt.Print(event.Text)
			wrote = !strings.HasSuffix(event.Text, "\n")

		case session.EventToolBegin:
			newline()
			fmt.Fprintln(os.Stderr, "tool: "+tui3.ToolGloss(event.Tool, event.Hint))

		case session.EventToolFailed:
			newline()
			reason := event.Hint
			if reason == "" && event.Err != nil {
				reason = event.Err.Error()
			}
			fmt.Fprintln(os.Stderr, "tool: "+event.Tool+" failed: "+reason)

		case session.EventCompacted:
			newline()
			fmt.Fprintln(os.Stderr, "compacted: "+event.Hint)

		case session.EventError:
			failure = event.Err
			if failure == nil {
				failure = fmt.Errorf("session: the turn failed without a reason")
			}
		}
	}
	newline()
	return failure
}

// v3RecentSessionSlots bounds one listing. Twenty is far more than the four the
// welcome box draws and more than a person scrolls a picker past; what it is
// really for is the ceiling on the work — twenty file scans, once, on the
// keystroke that asks (internal/session's Recent bounds the reads too).
const v3RecentSessionSlots = 20

// v3RecentSessions is this project's past conversations as the surface lists
// them: the name each one gave itself, the last thing that happened in it, and
// when.
//
// It reads the transcripts rather than opening them — no lock, no replay, no
// agent — which is what lets it list the session the running window is holding
// open, and lets a second window list the first one's conversation while it is
// live (internal/session's peek.go).
//
// An unreadable bucket is an empty list and not an error. This answers a list a
// person may never look at; the one thing it must not do is stop a launch, and
// "no recent sessions" is a true sentence about a machine whose session
// directory cannot be read.
//
// BOTH SHAPES ARE LISTED, because both are on the disk: a session written
// before Decision 26 is a flat transcript and one written after it is a
// folder, and the reader takes the folder's own meta.json as the name and
// the ordering (internal/session's recentplace.go).
func v3RecentSessions(bucket string) []tui3.Session {
	found := session.RecentSessions(bucket, v3RecentSessionSlots)
	rows := make([]tui3.Session, 0, len(found))
	for _, summary := range found {
		rows = append(rows, tui3.Session{
			Title:   summary.Title,
			Opening: summary.Opening,
			Last:    summary.Last,
			File:    summary.File,
			At:      summary.At,
		})
	}
	return rows
}
