package main

// chatv3_process.go is what a v3 process owns once and what one conversation
// borrows from it.
//
// The split exists because [openV3Launch] used to be BOTH. Every call loaded the
// model catalog, opened the sub-harness registry, opened the machine's chat
// database and resolved the deliverables index — which was correct while a
// process opened exactly one conversation and is wrong the moment it can open a
// second. Calling it twice would leak stores, repeat the catalog warm, and re-run
// a boot pass that is documented as never repeated.
//
// So there are two objects and the line between them is one question: does the
// answer depend on WHICH DIRECTORY the conversation is about?
//
//   - [v3Process] — no. The profile, the catalog, the harness registry, the
//     memory store, the deliverables index, the accounts manager, the input
//     history file, and where the person was standing when the process started.
//     Built once, in [openV3Process], and handed to every launch.
//   - [v3Launch] — yes. The session folder, the gate, the roles, the spend rail,
//     the context window, and whether this directory keeps a draft or a history
//     at all. Built per workspace, in [openV3Launch].
//
// And on top of both, [v3Seam] — the two closures the surface opens
// conversations through (internal/tui3's Open and Start), which is where the
// per-agent closures are minted so that a conversation and the seams that
// answer for it can never come apart.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/connect"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/history"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// v3Process is everything a launch may borrow but must not build twice.
type v3Process struct {
	// Settings is the one profile every governance row is resolved out of, and
	// ProfileDir the directory the settings panel writes back into. They must be
	// the same one: with AFORGE_PROFILE_DIR set, a panel writing ~/.aforge while
	// the session read the named profile is a gate turned off in the sheet that
	// stays on with nothing on screen saying why.
	Settings   config.Config
	ProfileDir string
	// Models is ONE lazy warm and one cache on disk. N catalogs would be N
	// network round trips for one answer.
	Models *catalog.Catalog
	// Harnesses is the registry under the state root. The law is already written
	// at [openV3Launch]: two stores at one directory is how /harness and the
	// offer card come to name different harnesses.
	Harnesses *subharness.Store
	// Memory is the machine's chat database, and it MUST be one. [store.Open]
	// builds a SQLite handle with an eight-connection pool, and every write goes
	// through a begin that the driver strips the caller's context off — a
	// transaction that loses the race waits the full busy timeout and cannot be
	// cancelled (internal/store's writelock.go, busyWait 10s). Two *Store values
	// on one file in one process are two pools with no in-process lock between
	// them, so the only thing arbitrating their writes would be that timeout.
	//
	// Memory is legitimately PROFILE-scoped, which is why one handle is enough:
	// the memory row is not in [config.ProjectKeys], so no workspace has its own
	// answer to give. Each conversation still gets its own memory pass and its
	// own context, which is per-agent already.
	Memory *store.Store
	// Artifacts is the deliverables index — one file per machine, and /export
	// and /files must resolve the same one the session's own products record
	// themselves in.
	Artifacts string
	// Conns is the accounts manager. One per process because an account
	// connected on the panel is connected for every conversation's belt in the
	// same breath, and because a token one manager refreshes is a token the
	// other would not know had moved.
	Conns *connect.Manager
	// LaunchDir is where the person was standing when the process started,
	// captured ONCE. The process never changes directory (`aforge engine` is the
	// one door that does, and it does it before it builds any of this), so one
	// capture is the honest one — and a conversation opened later into another
	// project is still a conversation this door opened from here.
	LaunchDir string

	// mu guards everything below: the lazily opened history file and the list of
	// agents this process has built. Both are touched from the surface's
	// goroutine and from the door's defer, which are not the same one.
	mu     sync.Mutex
	recall *history.Store
	agents []*session.Agent
	closed bool
}

// openV3Process builds the once-only half of a v3 launch.
//
// `door` is the word the missing-key sentence names, and it moved here from
// [v3Options] because this is now the first thing that reads the profile and
// therefore the first thing that can fail for the want of a key. Empty is
// "chat", which is what both terminal doors say — `aforge resume` has always
// said it and says it still, because what it could not open is a chat.
func openV3Process(door string) (*v3Process, error) { return openV3ProcessWith(door, false) }

// openV3ProcessWith is [openV3Process] with the one thing a door knows that
// the process does not: whether somebody is sitting at this terminal who can
// be ASKED for a key.
//
// `askKey` true is the interactive chat — a person, a TTY, no --once and no
// --host — and it opens the process with no key at all when none is found, so
// the surface can collect one on its first screen (internal/tui3's
// firstrun.go). Every other door keeps the old refusal: nobody is there to
// paste anything, and a process that opened keyless would fail on its first
// request instead of at the door where the sentence can be read.
func openV3ProcessWith(door string, askKey bool) (*v3Process, error) {
	// Housekeeping, in the background, once per process (chatv3_sweep.go). It
	// was already a sync.Once and needs nothing from this move; it is here
	// because this is now the one function every v3 door passes through.
	startPlaceSweep()
	settings, err := config.Load()
	if err != nil && askKey && errors.Is(err, config.ErrNoAPIKey) {
		settings, err = config.LoadKeyless()
	}
	if err != nil {
		if strings.TrimSpace(door) == "" {
			door = "chat"
		}
		fmt.Fprintln(os.Stderr, "aforge "+door+" needs a model to talk with.")
		fmt.Fprintln(os.Stderr, "export "+config.APIKeyEnv+" (or OPENAI_API_KEY) and run it again.")
		return nil, err
	}
	// AND THE BACKGROUND CHECKS ARE PUT BACK IF THEY DRIFTED, once per process,
	// in the background, saying nothing on screen (chatv3_standing.go). A timer
	// naming a program that has moved is a timer that runs nothing, and this is
	// the one moment this build knows where the program actually is.
	startBackgroundRepair(settings.ProfileDir)
	// WHERE THE PERSON IS STANDING, which is not the same fact as which project
	// this is: `aforge` typed in repo/cmd/ is a conversation about the
	// repository, and the subdirectory is recorded rather than resolved away
	// (Decision 26).
	launchDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}
	// ONE BOOT PASS, before anything reads the new layout, so a machine that
	// last ran the flat layout opens on its own conversations rather than on an
	// empty list (chatv3_migrate.go). It is never fatal and never repeated —
	// which is a promise this file is now the keeper of.
	migrateV3Layout()
	// Model discovery starts here and is waited for NOWHERE. On a cold cache
	// resolving it is a network round-trip, and everything it feeds has a good
	// answer without it.
	models := catalog.LoadLazy(context.Background(), catalog.Options{
		BaseURL: settings.BaseURL, APIKey: settings.APIKey, Dir: settings.ProfileDir,
	})
	return &v3Process{
		Settings:   settings,
		ProfileDir: settings.ProfileDir,
		Models:     models,
		Harnesses:  subharness.Default(),
		Memory:     v3Memory(settings.ProfileDir),
		Artifacts:  artifactsIndexPath(),
		Conns:      v3Connect(settings.ProfileDir),
		LaunchDir:  launchDir,
	}, nil
}

// history is the process's ONE input-history file, opened on the first
// conversation whose workspace says it keeps one.
//
// It is lazy rather than opened at boot because the row is answered per
// workspace ([config.KeyHistoryEnabled] is in the project layer) and "history
// off" has always meant no file and no goroutine at all. One store rather than
// one per conversation because the file is one file: every entry carries the
// directory it was typed in, and the surface asks for its own directory's
// (internal/tui3's recall.go).
//
// A nil is returned as a plain nil interface and never as a typed one, because
// the surface tests its recall door with a bare nil check.
func (p *v3Process) history() tui3.History {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	if p.recall == nil {
		dir, err := v3Dir()
		if err != nil {
			// A history file that cannot be opened costs the up arrow and
			// nothing else, which is why this is dropped rather than returned.
			return nil
		}
		p.recall = history.New(filepath.Join(dir, "history.jsonl"))
	}
	return p.recall
}

// track records an agent this process built, so [v3Process.closeAll] can close
// it however the surface returns.
func (p *v3Process) track(agent *session.Agent) {
	if agent == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.agents = append(p.agents, agent)
}

// setAPIKey is the key arriving after the door: the first-run screen or the
// settings row handed one over, and every conversation this process holds —
// the one on screen and any kept behind it — starts talking with it on its
// next request. The process's own settings take it too, so a conversation
// opened later (/new, the picker, home) is built with the key rather than with
// the empty one the boot found ([v3Seam.launch] reads it back from here).
func (p *v3Process) setAPIKey(key string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Settings.APIKey = strings.TrimSpace(key)
	for _, agent := range p.agents {
		if err := agent.SetAPIKey(key); err != nil {
			return err
		}
	}
	return nil
}

// apiKey is the key the process holds now, which may be newer than the one any
// launch was assembled with.
func (p *v3Process) apiKey() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.Settings.APIKey
}

// closeAll closes every conversation this process opened and then the stores
// they shared. It is IDEMPOTENT and it is the door's defer.
//
// WHAT IT IS FOR. The outer defer used to own exactly one agent — the boot one —
// so a [tea.Program] that returned by any road other than the surface's own quit
// leaked every conversation opened after boot: their session-file flocks, their
// presence heartbeats, their jobs. Every window on the machine would go on
// reading those transcripts as held by another window until this process exited.
//
// THE ORDER IS THE POINT. Agents first, in parallel — each one's Close is
// bounded on every axis and the phases inside it are sequential, so N of them
// run in the time of the slowest rather than the sum. THEN the shared stores,
// and the memory database LAST: an agent's memory pass may still be writing when
// Close begins, its own Close waits for that pass, and a database shut before
// that wait would be a write into a closed handle. That store is also the one
// with the ten-second busy wait behind it (internal/store's writelock.go:40-50),
// so closing it under a live writer is not a fast error but a long one.
func (p *v3Process) closeAll() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	agents, recall := p.agents, p.recall
	p.agents, p.recall = nil, nil
	p.mu.Unlock()

	var waiting sync.WaitGroup
	for _, agent := range agents {
		waiting.Add(1)
		// THE CLOSE IS SPAWNED UNDER THE GUARD, like every other fire-and-forget
		// goroutine in this tree (internal/guard's sweep_test.go states the law).
		// It matters most exactly here: this runs while the process is on its way
		// out, and one agent whose Close faults would take down the surface
		// mid-teardown — with the Wait below never returning, since the panic
		// would carry the deferred Done away with the goroutine. Under
		// [guard.Go] the fault is noted, Done still runs, and the remaining
		// conversations still get closed.
		guard.Go("chatv3/close-agent", func() {
			defer waiting.Done()
			// The error is dropped for the reason the door's defer always
			// dropped it: nothing is left to say it to, and a session file that
			// would not flush is not a reason to hold the terminal.
			_ = agent.Close()
		})
	}
	waiting.Wait()

	if recall != nil {
		_ = recall.Close()
	}
	if p.Memory != nil {
		_ = p.Memory.Close()
	}
}

// ── the agent-building seam ─────────────────────────────────────────────────

// v3Seam is what internal/tui3's Open and Start are built on: the process's
// shared resources, the launch this door already made, and the flags the command
// line said once.
//
// It exists so that the two closures are one piece of code with one branch in it
// — a workspace the caller named, or this door's own — instead of four closures
// that would drift about which of eleven per-conversation answers they
// remembered to fetch.
type v3Seam struct {
	proc *v3Process
	// boot is the launch this door opened on, and it is what a conversation
	// with no workspace of its own borrows: /new and the resume picker are
	// asking about the project this window is already in.
	boot *v3Launch
	// seed is the command line's own say — the model, --no-compact, --yolo —
	// carried so that a conversation opened an hour later is the same launch
	// this one is rather than a second one quietly drifting away from it.
	seed v3Options
}

// v3GoneWord is what a door says about a workspace that is not there.
//
// IT IS THE SURFACE'S OWN SENTENCE rather than a second spelling of it. Home
// stats the workspace on the keystroke and refuses there; this catches a
// directory that disappeared between that stat and this open, and a person must
// not be able to tell which of the two answered.
const v3GoneWord = tui3.WorkspaceGoneWord

// v3OpenTarget canonicalises a workspace a caller named, and refuses one that is
// not a directory.
//
// EMPTY MEANS THIS DOOR'S OWN and is not checked, which is what /new and the
// picker pass: there is no second directory to resolve, and re-resolving the one
// the launch already settled would be this file second-guessing it.
//
// A NAMED ONE IS STATTED HERE even though home stats it on the keystroke
// (internal/tui3's home.go), because this seam is also reachable from /resume and
// from a flag, and because a folder can be removed between the keystroke and the
// open. Canonical means [filepath.EvalSymlinks] then [filepath.Clean] — the same
// identity every other door in this wave will compare against, so two spellings
// of one directory are one workspace and not two.
func v3OpenTarget(workspace string) (string, error) {
	named := strings.TrimSpace(workspace)
	if named == "" {
		return "", nil
	}
	resolved, err := filepath.EvalSymlinks(named)
	if err != nil {
		return "", fmt.Errorf("%s · %s", v3GoneWord, named)
	}
	resolved = filepath.Clean(resolved)
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s · %s", v3GoneWord, named)
	}
	return resolved, nil
}

// launch resolves the launch a conversation runs on: this door's own when the
// caller named no workspace, and a fresh one — through the SAME governance every
// other launch goes through — when it named another project.
func (s *v3Seam) launch(workspace string) (*v3Launch, error) {
	target, err := v3OpenTarget(workspace)
	if err != nil {
		return nil, err
	}
	launch := s.boot
	if target != "" {
		opts := s.seed
		opts.Workspace = target
		if launch, err = openV3Launch(s.proc, opts); err != nil {
			return nil, err
		}
	}
	// THE KEY IS THE ONE FIELD READ BACK FROM THE PROCESS rather than from the
	// launch. A boot that opened keyless and was handed a key on the first
	// screen ([v3Process.setAPIKey]) still carries the empty key it was assembled
	// with, and a /new built from it would open a conversation that refuses
	// every request. A copy is patched rather than the boot itself, because the
	// boot is shared and this is a reading, not a change to it.
	if strings.TrimSpace(launch.Config.APIKey) == "" {
		if key := s.proc.apiKey(); key != "" {
			keyed := *launch
			keyed.Config.APIKey = key
			launch = &keyed
		}
	}
	return launch, nil
}

// start mints a fresh conversation: a new session folder in the workspace's own
// bucket, and an agent opened on it.
func (s *v3Seam) start(workspace string) (tui3.Conversation, error) {
	launch, err := s.launch(workspace)
	if err != nil {
		return tui3.Conversation{}, err
	}
	place, err := v3NextSession(launch.Place, launch.Workspace)
	if err != nil {
		return tui3.Conversation{}, err
	}
	// The launch's config, pointed at the new folder and carrying the gate AS IT
	// STANDS NOW rather than as it stood at boot (chatv3_approval.go says why the
	// second half is not optional).
	cfg := v3CurrentGate(launch.Config, launch.Workspace, s.proc.ProfileDir, s.seed.Yolo)
	cfg, err = v3PointAt(cfg, place)
	if err != nil {
		return tui3.Conversation{}, err
	}
	return s.open(launch, cfg, false)
}

// resume opens a transcript somebody picked, on the launch its workspace
// answers for.
func (s *v3Seam) resume(workspace, transcript string) (tui3.Conversation, error) {
	launch, err := s.launch(workspace)
	if err != nil {
		return tui3.Conversation{}, err
	}
	// THE GATE IS THE ONE THING THAT IS NOT A PROPERTY OF THE LAUNCH. It was one
	// only while nothing could change it mid-session; now that a banked rule can,
	// an old conversation reopened afterwards has to open behind the rule and not
	// behind the boot.
	cfg := v3CurrentGate(launch.Config, launch.Workspace, s.proc.ProfileDir, s.seed.Yolo)
	cfg, err = v3Reopen(cfg, transcript, launch.Workspace)
	if err != nil {
		return tui3.Conversation{}, err
	}
	return s.open(launch, cfg, true)
}

// open is the half both of the two above end in: the agent, and the bundle of
// per-agent seams minted around it.
//
// ROLLBACK. The agent takes the session-file flock and starts a presence
// heartbeat as it opens (internal/session's sessionfile.go and taskpresence.go),
// and the steps after it can still fail — the project layer's two rows are read
// off the disk. If any of them does, the partial agent is CLOSED before the
// error travels, which releases the lock and removes the presence file. Without
// it a failed open leaves a transcript that every window on the machine reads as
// held by another window until this process exits.
func (s *v3Seam) open(launch *v3Launch, cfg session.Config, resumed bool) (tui3.Conversation, error) {
	// There is a surface, and it answers (internal/tui3's consent.go). Every
	// conversation this seam opens is one somebody is looking at.
	cfg.AskConsent = true
	// And it holds the harness lane for every one of them: the surface opens the
	// standing subscription again on each conversation it takes (internal/tui3's
	// switcher.go), which is what lets chat offer a saved program with an intake
	// card here as well as in the first conversation of the process (chatv3.go
	// states the distinction between this and AskConsent).
	cfg.HarnessCards = true
	agent, cfg, notice, err := openV3Agent(cfg, launch.Workspace, v3OpenSession)
	if err != nil {
		return tui3.Conversation{}, err
	}
	s.proc.track(agent)
	if notice != "" {
		// The session file moved under us, so this is a new conversation rather
		// than the one that was asked for, and the sentence says so.
		resumed = false
	}
	conv, err := s.bundle(agent, launch, cfg, resumed, notice)
	if err != nil {
		_ = agent.Close()
		return tui3.Conversation{}, err
	}
	return conv, nil
}

// v3Live is what a conversation's seams are minted around: the surface's own
// view of a session, and the one door onto the gate that session is behind.
// *session.Agent is the only thing in this tree that satisfies it.
//
// IT IS AN INTERFACE SO THE BINDING CAN BE READ. Which conversation a banked rule
// reaches is the whole of what went wrong here, and internal/session keeps the
// standing gate private — correctly, since every read of it has to go through one
// lock. Naming the two halves the bundle actually uses lets a test hold the
// receiving end and say which conversation was written to, without that package
// growing a reader for the sake of a test.
type v3Live interface {
	tui3.Agent
	v3Gate
}

// bundle is the per-conversation half of the seam: the agent, where it works,
// what its project lets it keep, and the three approval closures BOUND TO THAT
// AGENT.
//
// The binding is the whole reason this function exists rather than a struct
// literal at the door. The trio used to be built once at boot around the boot
// agent, so an "always" answered after a /new or a resume was written to the
// profile and then pushed into a session that had already been closed: saved,
// said to be saved, and not in force until the next launch, with nothing on
// screen saying so. Minted here, they cannot outlive the agent they are about.
func (s *v3Seam) bundle(agent v3Live, launch *v3Launch, cfg session.Config,
	resumed bool, notice string) (tui3.Conversation, error) {
	workspace := cfg.Workspace
	profileDir := s.proc.ProfileDir
	// The two things this surface keeps on the person's behalf rather than the
	// session's, both read THROUGH THE PROJECT LAYER: a repository that says
	// "record nothing from this directory" is answering about ITS directory,
	// which is the whole reason the layer is per-workspace and the whole reason
	// these two are per conversation rather than per launch.
	keepHistory, err := config.ProjectBoolAt(workspace, profileDir, config.KeyHistoryEnabled)
	if err != nil {
		return tui3.Conversation{}, err
	}
	keepDraft, err := config.ProjectBoolAt(workspace, profileDir, config.KeyDraftPersist)
	if err != nil {
		return tui3.Conversation{}, err
	}
	draft := ""
	if keepDraft {
		if dir, err := v3Dir(); err == nil {
			draft = tui3.DraftFile(dir, workspace)
		}
	}
	var recall tui3.History
	if keepHistory {
		recall = s.proc.history()
	}
	bucket := launch.Bucket
	return tui3.Conversation{
		Agent:         agent,
		SessionFile:   cfg.SessionFile,
		Workspace:     workspace,
		Owned:         cfg.Place.Owned,
		Resumed:       resumed,
		Notice:        notice,
		ContextWindow: cfg.ContextWindow,
		DraftFile:     draft,
		History:       recall,
		// This project's conversations, walked on the keystroke that asks for
		// them and never at boot — and this PROJECT's, which is what makes the
		// closure per conversation rather than per process.
		RecentSessions:   func() []tui3.Session { return v3RecentSessions(bucket) },
		SaveApproval:     bankToolApproval(agent, workspace, profileDir, s.seed.Yolo),
		SaveBashApproval: bankBashApproval(agent, workspace, profileDir, s.seed.Yolo),
		ApplyApprovals:   applyV3Approvals(agent, workspace, profileDir, s.seed.Yolo),
	}, nil
}
