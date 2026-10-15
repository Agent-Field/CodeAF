package main

// ── codeaf app: THE DESKTOP APP'S BACKEND ───────────────────────────────────
//
// `codeaf app` is started by the desktop app as a child process and spoken to
// over its own stdin and stdout, one JSON object per line
// (docs/design/app-v3/PROTOCOL.md). internal/appwire owns the bytes; this file
// owns what the methods mean.
//
// EVERY AGENT LIVES IN THIS ONE PROCESS, the way a person's tabs do. An agent
// the app opens is a conversation built by the very assembly a host builds one
// with ([assembleEngine]) and spoken to through a real [remote.Client] over an
// in-memory pipe ([remote.Loopback]), so the app's conversations are the same
// conversations, with the same toolbelt, gate and journal, as every other
// window's. Nothing is spawned per agent.
//
// ONE DIFFERENCE FROM A HOST, AND IT IS THE DIRECTORY. A host is one workspace
// and moves the process into it; this process holds conversations in several
// workspaces at once, so it moves nowhere. Every tool already resolves against
// its conversation's own workspace rather than the process's directory, and a
// new conversation records the workspace the app asked for as where the person
// stood, since the process's own directory is wherever the app was started.
//
// AND IT IS LAZY BY LAW. Starting up reads nothing but the flags, so the hello
// line leaves before anything is opened; listing agents, tasks and crews reads
// the disk; only agents.create, agents.open and agents.send build a
// conversation, and one left alone for [appAgentIdle] is let go again and
// rebuilt transparently by the next send.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// appAgentIdle is how long a conversation the app holds may sit with no turn
// running, no question waiting and nothing sent before it is let go. Letting go
// closes the conversation in this process and nothing else: its journal is on
// disk, its state still reads idle, and the next send opens it again.
const appAgentIdle = 10 * time.Minute

// appSweepEvery is how often the idle rule above is asked.
const appSweepEvery = 30 * time.Second

// appCrewPoll is how often the teams file is looked at for crew.changed, once
// the app has asked for the crews at all. It is one stat when nothing moved.
const appCrewPoll = 2 * time.Second

// appReleaseWait bounds how long closing one conversation waits for its turn's
// last events to be written before the outbox is shut regardless.
const appReleaseWait = 5 * time.Second

func runApp(args []string) error {
	flags := commandFlags("app")
	homeFlag := flags.String("home", "", "codeaf's state folder for this app, instead of CODEAF_HOME or ~/.codeaf")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("usage: codeaf app [--home dir]")
	}
	if dir := strings.TrimSpace(*homeFlag); dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		if err := os.Setenv(home.EnvVar, abs); err != nil {
			return err
		}
	}
	// STDOUT IS THE PROTOCOL'S AND NOBODY ELSE'S. The wire keeps the real
	// descriptor, and anything else in this process that prints to stdout
	// lands on stderr, where the app collects logs.
	wire := os.Stdout
	os.Stdout = os.Stderr
	defer func() { os.Stdout = wire }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	server := appwire.NewServer(wire)
	backend := newAppBackend(server, appOpenInProcess)
	err := server.Serve(ctx, os.Stdin, appwire.Hello{Version: resolvedVersion(), PID: os.Getpid(), Home: home.Dir()}, backend)
	if backend.assembled.Load() {
		// The process half every conversation shared, closed the way the pipe
		// engine closes it once its conversation is gone (engine.go).
		session.CloseUsage()
		closeEngineProcess()
	}
	return err
}

// appConversation is what the backend needs of one open conversation. A
// [remote.Agent] is one; a test's fake is another.
type appConversation interface {
	Submit(ctx context.Context, text string) (<-chan session.Event, error)
	Interrupt()
	Model() string
	Title() string
	NeedsPerson() bool
	WatchTaskUpdates() (<-chan session.Event, func())
	// The question lane and its one answer door, the same pair the chat's
	// question page uses: every approval and every question the conversation
	// raises, answers and withdrawals included, from whichever window.
	WatchQuestions() (<-chan session.Event, func())
	ResolveQuestion(session.Answer) error
	SetModel(model string)
	SetSpendRail(usd float64) error
	// ResolveConsentRemember answers one approval and says how long the answer
	// lasts; the app uses it for an always that wrote a rule first.
	ResolveConsentRemember(id uint64, allow bool, scope session.ConsentScope)
	// How hard the model is asked to think, per model.
	ReasoningFor(model string) string
	SetReasoningFor(model, level string)
}

// appOpenAsk is which conversation to open: a saved one by transcript, or a
// new one in a workspace. Minted is a folder this process minted a moment ago
// for agents.create: nobody else can be holding it, so it is never joined.
type appOpenAsk struct {
	Workspace  string
	Transcript string
	Model      string
	New        bool
	Minted     bool
}

// appConn is an opened conversation and the one way to give it back.
type appConn struct {
	Conv       appConversation
	Workspace  string
	Transcript string
	Release    func()
	// Assembled says this process built the conversation itself, so the shared
	// engine half must be closed when the app leaves.
	Assembled bool
}

type appOpener func(appOpenAsk) (*appConn, error)

// appOpenInProcess opens a conversation for the app. A saved conversation that
// a window's host already holds is JOINED there, as a second view of the same
// conversation, because opening it here as well would be a second writer on its
// journal. Everything else is built in this process.
func appOpenInProcess(ask appOpenAsk) (*appConn, error) {
	if ask.Transcript != "" && !ask.Minted {
		if socket, err := enginehost.DialStartingHost(ask.Workspace); err == nil {
			client, err := remote.Dial(socket, "", remote.Hello{Workspace: ask.Workspace, Session: ask.Transcript, Join: true})
			if err == nil {
				return appConnOf(client, func() { _ = client.Close() }, false), nil
			}
			_ = socket.Close()
		}
	}
	workspace := ask.Workspace
	transcript := ask.Transcript
	hello := remote.Hello{
		Workspace: workspace,
		Session:   transcript,
		New:       ask.New,
		Model:     ask.Model,
		// An app agent keeps the name its first message gave it: the engine's
		// own naming does not rename it afterwards.
		Launch: &remote.LaunchShape{Interactive: true, DeterministicNames: true},
	}
	loop, err := remote.Loopback(hello, remote.Options{
		Boot: func(h remote.Hello) (*remote.Engine, error) {
			return assembleEngine(h, workspace, transcript, workspace)
		},
	})
	if err != nil {
		return nil, err
	}
	return appConnOf(loop.Client, func() { _ = loop.Close() }, true), nil
}

func appConnOf(client *remote.Client, release func(), assembled bool) *appConn {
	welcome := client.Welcome()
	return &appConn{Conv: client.Agent(), Workspace: welcome.Workspace, Transcript: welcome.SessionFile, Release: release, Assembled: assembled}
}

// appBackend is the [appwire.Engine] over this process's conversations.
type appBackend struct {
	emit      appwire.Emitter
	open      appOpener
	now       func() time.Time
	turns     atomic.Uint64
	assembled atomic.Bool

	mu      sync.Mutex
	agents  map[string]*appAgent
	closing bool

	sweepStop chan struct{}
	sweepDone chan struct{}
	crews     *appCrewWatch
	// spent is the last figure spend.changed said, so it is said on a change.
	spent float64
	// mint makes a new conversation's folder and answers its transcript path:
	// the one piece of agents.create that happens before the answer.
	mint func(workspace string) (string, error)
	// warmed closes when [appBackend.Warm] has finished, which Close waits
	// for: the process half it opens is closed after the backend is.
	warmed chan struct{}
}

// appAgent is every agent this process has heard of: listed off the disk,
// created or opened. live is nil while no conversation is held for it.
type appAgent struct {
	// gate serializes opening and letting go of this one agent, so two sends
	// racing for a closed conversation open it once.
	gate       sync.Mutex
	view       appwire.Agent
	transcript string
	live       *appLive
	// failed is the words of the last turn that ended in failure, which is
	// what agents.retry sends again; a turn that ends well clears it.
	failed string
	// boot is what agents.create asked of a conversation not yet open, taken
	// by whichever road opens it first; opening says one is being assembled.
	boot    *appBoot
	opening bool
	// users is how many messages of the person's the transcript holds, which
	// is what an entry's id is counted from ([appUserEntryID]); counted says
	// it has been read.
	users   int
	counted bool
}

// appBoot is agents.create's ask, kept for the background open.
type appBoot struct {
	model  string
	budget *float64
}

// appLive is one conversation the process holds.
type appLive struct {
	conn      *appConn
	box       *appwire.Outbox
	stopTasks func()
	running   bool
	lastUse   time.Time
	pumpDone  chan struct{}
	lanesDone chan struct{}
	asksDone  chan struct{}
	stopAsks  func()
	tasks     map[string]appwire.Task
	// asks are the open asks in the order they were raised, and questions the
	// engine's own question behind each, which is what an answer is built from.
	asks      []appwire.Ask
	questions map[string]session.Question
	// answered names the asks this app answered and the option it gave, so
	// their closing says so.
	answered map[string]string
	// consents are the approvals' calls as the turn announced them, by the
	// engine's question id: what an always is written down from.
	consents map[uint64]appConsentCall
	// stopping says the person stopped the running turn.
	stopping bool
}

// appConsentCall is the call one approval is about.
type appConsentCall struct {
	Tool string
	Args string
}

// needsYou is the one reading of "waiting on the person": an open ask, or the
// engine's own word for a wait the question lane does not carry. Callers hold
// b.mu.
func (live *appLive) needsYou() bool {
	return len(live.asks) > 0 || live.conn.Conv.NeedsPerson()
}

// restingState is what an agent with no turn running reads as. Callers hold
// b.mu.
func (live *appLive) restingState() string {
	if live.needsYou() {
		return appwire.StateNeedsYou
	}
	return appwire.StateIdle
}

func newAppBackend(emit appwire.Emitter, open appOpener) *appBackend {
	b := &appBackend{
		emit:      emit,
		open:      open,
		now:       time.Now,
		agents:    map[string]*appAgent{},
		sweepStop: make(chan struct{}),
		sweepDone: make(chan struct{}),
		mint:      appMint,
	}
	guard.Go("app/sweep", b.sweep)
	return b
}

// appMint makes a new conversation's folder in workspace, the very folder a
// host's new conversation is minted as, with the workspace stamped as where the
// person stood. It is a directory and a small file, so agents.create can answer
// with the agent's lasting id before anything slow happens.
func appMint(workspace string) (string, error) {
	place, err := v3NextSessionFrom(session.Place{}, workspace, workspace)
	if err != nil {
		return "", err
	}
	return place.Transcript(), nil
}

// appAgentID is a conversation's id from its transcript: the folder's name.
func appAgentID(transcript string) string {
	return filepath.Base(filepath.Dir(filepath.Clean(transcript)))
}

// locked runs fn holding b.mu. Every critical section in this file goes
// through it or opens with a deferred unlock, so a panic a guard absorbs never
// leaves the lock held (internal/guard's lock law).
func (b *appBackend) locked(fn func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	fn()
}

// agent answers the record for id, making one if this is the first time.
func (b *appBackend) agent(id string) (a *appAgent) {
	b.locked(func() { a = b.agentLocked(id) })
	return a
}

// agentLocked answers the record for id, making one if this is the first time.
func (b *appBackend) agentLocked(id string) *appAgent {
	a := b.agents[id]
	if a == nil {
		a = &appAgent{view: appwire.Agent{ID: id, State: appwire.StateIdle}}
		b.agents[id] = a
	}
	return a
}

// rowView is an agent as the disk says it is.
func appRowView(row session.SessionRow) appwire.Agent {
	at := row.At
	if at.IsZero() {
		at = row.Created
	}
	state := appwire.StateIdle
	if row.Live {
		switch {
		case row.Presence.NeedsPerson():
			state = appwire.StateNeedsYou
		case row.Presence.State == session.PresenceWorking:
			state = appwire.StateWorking
		}
	}
	return appwire.Agent{ID: row.ID, Title: row.Title, Workspace: row.Workspace, Model: row.Model, State: state, UpdatedAt: at}
}

// remember files what the disk said about an agent, without touching one this
// process is holding: a held conversation's own facts are newer than the disk's.
func (b *appBackend) remember(row session.SessionRow) appwire.Agent {
	b.mu.Lock()
	defer b.mu.Unlock()
	a := b.agentLocked(row.ID)
	if a.transcript == "" {
		a.transcript = row.Transcript
	}
	if a.live != nil || a.view.State == appwire.StateClosed || a.view.State == appwire.StateStopped {
		view := a.view
		if view.Title == "" {
			view.Title = row.Title
		}
		return view
	}
	a.view = appRowView(row)
	return a.view
}

// find answers the agent for id, reading the disk for one this process has not
// heard of yet.
func (b *appBackend) find(id string) (*appAgent, error) {
	var a *appAgent
	held := false
	b.locked(func() {
		a = b.agents[id]
		held = a != nil && a.transcript != ""
	})
	if held {
		return a, nil
	}
	row, ok := appRowOnDisk(id)
	if !ok {
		return nil, appwire.Errorf(appwire.CodeNotFound, "there is no conversation with id %s", id)
	}
	b.remember(row)
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.agents[id], nil
}

// appRowOnDisk finds one conversation folder by id under the places root
// without walking every conversation on the machine: one directory listing and
// one stat per project.
func appRowOnDisk(id string) (session.SessionRow, bool) {
	if id == "" || id != filepath.Base(id) || strings.HasPrefix(id, ".") {
		return session.SessionRow{}, false
	}
	root := session.PlacesRoot()
	buckets, err := os.ReadDir(root)
	if err != nil {
		return session.SessionRow{}, false
	}
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		dir := filepath.Join(root, bucket.Name(), id)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		transcript := session.Place{Dir: dir}.Transcript()
		if row, ok := session.ReadRows([]string{transcript})[filepath.Clean(transcript)]; ok {
			return row, true
		}
		// A folder minted and never spoken in is still a conversation the app
		// may have created; its meta names the workspace.
		meta, err := session.LoadMeta(dir)
		if err != nil || strings.TrimSpace(meta.ID) == "" {
			continue
		}
		return session.SessionRow{ID: id, Dir: dir, Transcript: transcript, Title: meta.Title, Workspace: meta.Workspace, Created: meta.Created}, true
	}
	return session.SessionRow{}, false
}

// ListAgents reads the disk and never opens a conversation.
func (b *appBackend) ListAgents(workspace string) ([]appwire.Agent, error) {
	want := ""
	if strings.TrimSpace(workspace) != "" {
		want = filepath.Clean(workspace)
	}
	matches := func(where ...string) bool {
		if want == "" {
			return true
		}
		for _, w := range where {
			if w != "" && filepath.Clean(w) == want {
				return true
			}
		}
		return false
	}
	seen := map[string]bool{}
	var agents []appwire.Agent
	for _, row := range session.ReadWorld(session.PlacesRoot()).Sessions() {
		if row.DeletionPending || !matches(row.Workspace, row.ProjectDir) {
			continue
		}
		seen[row.ID] = true
		agents = append(agents, b.remember(row))
	}
	// A conversation this process created and nobody has spoken in yet is not
	// on the walk, which skips empty shells; it is still the app's agent.
	b.locked(func() {
		for id, a := range b.agents {
			if !seen[id] && (a.live != nil || a.opening) && matches(a.view.Workspace) {
				agents = append(agents, a.view)
			}
		}
	})
	sort.SliceStable(agents, func(i, j int) bool { return agents[i].UpdatedAt.After(agents[j].UpdatedAt) })
	return agents, nil
}

// CreateAgent answers a new agent AT ONCE and assembles its conversation
// behind the answer. With no workspace it is the person's home folder; with a
// budget the conversation's own spend limit is set before anything is spent;
// with words the first turn starts as soon as the conversation is open and the
// agent is named by them. A conversation that cannot be opened says so as a
// failed turn and a stopped agent.
func (b *appBackend) CreateAgent(ask appwire.CreateAsk) (appwire.Agent, error) {
	// engineWorkspace answers the home folder for no workspace at all.
	resolved, err := engineWorkspace(ask.Workspace)
	if err != nil {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
	}
	if b.isClosing() {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	transcript, err := b.mint(resolved)
	if err != nil {
		return appwire.Agent{}, appEngineError(err)
	}
	text := strings.TrimSpace(ask.Text)
	model := strings.TrimSpace(ask.Model)
	a := b.agent(appAgentID(transcript))
	var view appwire.Agent
	b.locked(func() {
		a.transcript = transcript
		a.view.Title = session.FirstMessageName(text)
		a.view.Workspace = resolved
		a.view.Model = model
		a.view.State = appwire.StateIdle
		if text != "" {
			a.view.State = appwire.StateWorking
		}
		a.view.UpdatedAt = b.now()
		a.boot = &appBoot{model: model, budget: ask.BudgetUSD}
		a.opening = true
		a.users, a.counted = 0, true
		view = a.view
	})
	b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	clientID := ask.ClientID
	guard.Go("app/create", func() { b.bootAgent(a, text, clientID) })
	return view, nil
}

// bootAgent opens a created agent's conversation and starts its first turn.
func (b *appBackend) bootAgent(a *appAgent, text, clientID string) {
	a.gate.Lock()
	defer a.gate.Unlock()
	view, opened, err := b.ensureLocked(a)
	b.locked(func() { a.opening = false })
	if err != nil {
		b.bootFailed(a, text, err)
		return
	}
	if text == "" {
		if opened {
			b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
		}
		return
	}
	// The agent already reads working; the turn's own start says the rest.
	if _, err := b.startLocked(a, text, clientID); err != nil {
		b.bootFailed(a, text, err)
	}
}

// bootFailed is a created agent whose conversation could not be opened or
// started: a failed turn the person can retry, and an agent that reads stopped.
func (b *appBackend) bootFailed(a *appAgent, text string, err error) {
	words := err.Error()
	var wire *appwire.Error
	if errors.As(appEngineError(err), &wire) {
		words = wire.Message
	}
	var view appwire.Agent
	b.locked(func() {
		if text != "" {
			a.failed = text
		}
		a.boot = nil
		if a.live == nil {
			a.view.State = appwire.StateStopped
		}
		view = a.view
	})
	if text != "" {
		turn := b.turns.Add(1)
		b.emit.Emit(appwire.EventTurnDone, appwire.TurnDone{Agent: view.ID, Turn: turn, OK: false, Error: words, ErrorKind: appwire.ErrorOther})
	}
	b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
}

// OpenAgent attaches a conversation to a saved agent and answers it.
func (b *appBackend) OpenAgent(id string) (appwire.Agent, error) {
	a, err := b.find(id)
	if err != nil {
		return appwire.Agent{}, err
	}
	view, opened, err := b.ensure(a)
	if err != nil {
		return appwire.Agent{}, err
	}
	if opened {
		b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	return view, nil
}

// ensure holds a conversation for a, opening it if none is held, and reports
// whether it had to.
func (b *appBackend) ensure(a *appAgent) (appwire.Agent, bool, error) {
	a.gate.Lock()
	defer a.gate.Unlock()
	return b.ensureLocked(a)
}

// ensureLocked is [appBackend.ensure] for a caller already holding a.gate.
func (b *appBackend) ensureLocked(a *appAgent) (appwire.Agent, bool, error) {
	var view appwire.Agent
	var transcript, workspace string
	var boot *appBoot
	held := false
	b.locked(func() {
		view, held = a.view, a.live != nil
		transcript, workspace = a.transcript, a.view.Workspace
		if !held {
			boot, a.boot = a.boot, nil
		}
	})
	if held {
		return view, false, nil
	}
	if b.isClosing() {
		return appwire.Agent{}, false, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	ask := appOpenAsk{Workspace: workspace, Transcript: transcript}
	if boot != nil {
		ask.Model, ask.Minted = boot.model, true
	}
	conn, err := b.open(ask)
	if err != nil {
		return appwire.Agent{}, false, appEngineError(err)
	}
	if boot != nil && boot.budget != nil && *boot.budget > 0 {
		if err := conn.Conv.SetSpendRail(*boot.budget); err != nil {
			conn.Release()
			return appwire.Agent{}, false, appEngineError(err)
		}
	}
	return b.hold(a, conn, workspace), true, nil
}

// hold makes conn the agent's live conversation. Callers hold a.gate.
func (b *appBackend) hold(a *appAgent, conn *appConn, workspace string) appwire.Agent {
	if conn.Assembled {
		b.assembled.Store(true)
	}
	live := &appLive{
		conn: conn, box: appwire.NewOutbox(b.emit), lastUse: b.now(),
		tasks: map[string]appwire.Task{}, lanesDone: make(chan struct{}), asksDone: make(chan struct{}),
		questions: map[string]session.Question{}, answered: map[string]string{},
		consents: map[uint64]appConsentCall{},
	}
	title, model := conn.Conv.Title(), conn.Conv.Model()
	effort := conn.Conv.ReasoningFor(model)
	var counted bool
	b.locked(func() { counted = a.counted })
	users := 0
	if !counted && conn.Transcript != "" {
		users = appUserCount(conn.Transcript)
	}
	var view appwire.Agent
	b.locked(func() {
		if !a.counted {
			a.users, a.counted = users, true
		}
		a.view.Effort = effort
		if conn.Transcript != "" {
			a.transcript = conn.Transcript
		}
		if conn.Workspace != "" {
			workspace = conn.Workspace
		}
		a.view.Workspace = workspace
		if title != "" {
			a.view.Title = title
		}
		if model != "" {
			a.view.Model = model
		}
		a.view.State = live.restingState()
		if a.view.UpdatedAt.IsZero() {
			a.view.UpdatedAt = b.now()
		}
		a.live = live
		view = a.view
	})
	lane, stop := conn.Conv.WatchTaskUpdates()
	live.stopTasks = stop
	id := view.ID
	guard.Go("app/tasks", func() {
		defer close(live.lanesDone)
		for event := range lane {
			if event.Kind == session.EventTaskUpdate && event.Task != nil {
				b.taskNotice(id, live, event.Task)
			}
		}
	})
	questions, stopQuestions := conn.Conv.WatchQuestions()
	live.stopAsks = stopQuestions
	guard.Go("app/asks", func() {
		defer close(live.asksDone)
		for event := range questions {
			b.questionEvent(a, id, live, event)
		}
		// A LANE THAT ENDS WHILE THE CONVERSATION IS STILL HELD IS A DEAD
		// CONNECTION: letting go stops the lane only after taking the
		// conversation away, so the only other way it ends is the engine
		// behind it going (internal/remote's bury finishes every lane).
		b.died(a, live)
	})
	return view
}

// appAskID is the stable id of one ask: the engine's own question key.
func appAskID(q session.Question) string {
	return string(q.Kind) + ":" + q.Token()
}

// appAskOf is the engine's question as the app's ask.
func appAskOf(agent string, q session.Question) appwire.Ask {
	ask := appwire.Ask{ID: appAskID(q), Agent: agent, Kind: appwire.AskQuestion, Title: strings.TrimSpace(q.Head), Detail: strings.TrimSpace(q.Reason)}
	if q.Kind == session.QuestionConsent {
		ask.Kind, ask.Subkind = appwire.AskApproval, appwire.SubkindPermission
		ask.Irreversible = q.Stakes == session.StakesIrreversible
	}
	for _, option := range appOptionsOf(q) {
		ask.Options = append(ask.Options, appwire.AskOption{ID: option.id, Label: option.label})
	}
	return ask
}

// appOption is one answer as the app names it, with the engine key behind it.
type appOption struct {
	id, key, label string
}

// appOptionsOf is a question's answers in the app's names. A permission's are
// allow_once, deny and always, in that order, and always is left off a call
// that cannot be undone. Any other question keeps the engine's own keys, and so
// does a permission whose answers are not the three the gate offers.
func appOptionsOf(q session.Question) []appOption {
	var plain []appOption
	for _, option := range q.Options {
		plain = append(plain, appOption{id: option.Key, key: option.Key, label: option.Label})
	}
	if q.Kind != session.QuestionConsent {
		return plain
	}
	byID := map[string]appOption{}
	for _, option := range q.Options {
		action, known := session.AnswerFromKey(session.QuestionConsent, option.Key)
		if !known {
			return plain
		}
		id := appwire.OptionAllowOnce
		switch {
		case !action.Allow:
			id = appwire.OptionDeny
		case action.Scope == session.ConsentToolSession:
			id = appwire.OptionAlways
		}
		if _, twice := byID[id]; twice {
			return plain
		}
		byID[id] = appOption{id: id, key: option.Key, label: option.Label}
	}
	var mapped []appOption
	for _, id := range []string{appwire.OptionAllowOnce, appwire.OptionDeny, appwire.OptionAlways} {
		option, offered := byID[id]
		if !offered || (id == appwire.OptionAlways && q.Stakes == session.StakesIrreversible) {
			continue
		}
		mapped = append(mapped, option)
	}
	return mapped
}

// appOptionNamed finds the answer the app named by its id.
func appOptionNamed(q session.Question, id string) (appOption, bool) {
	for _, option := range appOptionsOf(q) {
		if option.id == id {
			return option, true
		}
	}
	return appOption{}, false
}

// appOptionOfKey is the app's name for an answer the engine reports by key.
func appOptionOfKey(q session.Question, key string) string {
	for _, option := range appOptionsOf(q) {
		if option.key == key {
			return option.id
		}
	}
	return ""
}

// appReceipt is the line a closed ask leaves in the conversation: what was
// decided, by whom, and when, in the person's own clock.
func appReceipt(by, option string, answer *session.Answer, at time.Time) string {
	word := "Answered"
	switch option {
	case appwire.OptionAllowOnce:
		word = "Allowed once"
	case appwire.OptionDeny:
		word = "Denied"
	case appwire.OptionAlways:
		word = "Always allowed"
	}
	who := "you"
	switch by {
	case appwire.ClosedByStopped:
		return "Not answered · the agent stopped · " + at.Local().Format("15:04")
	case appwire.ClosedByElsewhere:
		if answer == nil {
			return "No longer needed · " + at.Local().Format("15:04")
		}
		if answer.DecidedBy == session.DecidedByDial {
			who = "your settings"
		}
	}
	return word + " · " + who + " · " + at.Local().Format("15:04")
}

// questionEvent keeps the open asks in step with the question lane and says
// every change on the wire, the agent's state with it.
func (b *appBackend) questionEvent(a *appAgent, id string, live *appLive, event session.Event) {
	if event.Question == nil {
		return
	}
	q := *event.Question
	askID := appAskID(q)
	switch event.Kind {
	case session.EventQuestion:
		ask := appAskOf(id, q)
		restated, moved := false, false
		var view appwire.Agent
		b.locked(func() {
			if _, open := live.questions[askID]; open {
				// The lane replays what is open when it is subscribed again,
				// and a question restated in place is the same ask.
				restated = true
				live.questions[askID] = q
				for i := range live.asks {
					if live.asks[i].ID == askID {
						live.asks[i] = ask
					}
				}
				return
			}
			live.questions[askID] = q
			live.asks = append(live.asks, ask)
			view, moved = b.restateLocked(a, live)
		})
		if restated {
			return
		}
		live.box.Event(appwire.EventAskOpened, appwire.AskOpened{Ask: ask})
		if moved {
			live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
		}
	case session.EventQuestionAnswered, session.EventQuestionWithdrawn:
		open, moved := false, false
		by, option := appwire.ClosedByElsewhere, ""
		var answer *session.Answer
		var view appwire.Agent
		b.locked(func() {
			if _, open = live.questions[askID]; !open {
				return
			}
			if event.Kind == session.EventQuestionAnswered {
				answer = event.Answer
				if answer == nil {
					answer = &session.Answer{}
				}
				option = appOptionOfKey(live.questions[askID], answer.Key)
			}
			if given := live.answered[askID]; given != "" {
				by, option = appwire.ClosedByApp, strings.TrimPrefix(given, appAnsweredMark)
				if answer == nil {
					answer = &session.Answer{}
				}
			}
			b.dropAskLocked(live, askID)
			view, moved = b.restateLocked(a, live)
		})
		if !open {
			return
		}
		at := b.now()
		live.box.Event(appwire.EventAskClosed, appwire.AskClosed{Agent: id, ID: askID, By: by, Option: option, At: at})
		live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
			ID: appReceiptEntryID(askID), Agent: id, Kind: appwire.EntryReceipt, At: at, Text: appReceipt(by, option, answer, at),
		}})
		if moved {
			live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
		}
	}
}

// dropAskLocked forgets one ask. Callers hold b.mu.
func (b *appBackend) dropAskLocked(live *appLive, askID string) {
	if q, ok := live.questions[askID]; ok && q.Kind == session.QuestionConsent {
		delete(live.consents, q.ID)
	}
	delete(live.questions, askID)
	delete(live.answered, askID)
	kept := live.asks[:0]
	for _, ask := range live.asks {
		if ask.ID != askID {
			kept = append(kept, ask)
		}
	}
	live.asks = kept
}

// restateLocked recomputes a held agent's state after its asks moved, and
// reports whether it changed. Callers hold b.mu.
func (b *appBackend) restateLocked(a *appAgent, live *appLive) (appwire.Agent, bool) {
	if a.live != live {
		return a.view, false
	}
	state := live.restingState()
	if live.running && state == appwire.StateIdle {
		state = appwire.StateWorking
	}
	if state == a.view.State {
		return a.view, false
	}
	a.view.State = state
	return a.view, true
}

// died is a held conversation whose engine went away without being let go:
// the agent reads stopped, its open asks close as stopped, and the next send
// opens the conversation again.
func (b *appBackend) died(a *appAgent, live *appLive) {
	mine := false
	var view appwire.Agent
	var asks []appwire.Ask
	b.locked(func() {
		if mine = a.live == live; !mine {
			return
		}
		a.live = nil
		a.view.State = appwire.StateStopped
		view = a.view
		asks = live.asks
		live.asks, live.questions = nil, map[string]session.Question{}
	})
	if !mine {
		return
	}
	at := b.now()
	for _, ask := range asks {
		live.box.Event(appwire.EventAskClosed, appwire.AskClosed{Agent: view.ID, ID: ask.ID, By: appwire.ClosedByStopped, At: at})
		live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
			ID: appReceiptEntryID(ask.ID), Agent: view.ID, Kind: appwire.EntryReceipt, At: at, Text: appReceipt(appwire.ClosedByStopped, "", nil, at),
		}})
	}
	live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	guard.Go("app/finish", func() { b.finish(live) })
}

// live answers the agent's held conversation, or nil.
func (b *appBackend) liveOf(a *appAgent) *appLive {
	b.mu.Lock()
	defer b.mu.Unlock()
	return a.live
}

// Send starts one turn, opening the conversation first if it is not held.
func (b *appBackend) Send(id, text, clientID string) (uint64, error) {
	a, err := b.find(id)
	if err != nil {
		return 0, err
	}
	// The gate keeps the idle sweep from letting the conversation go between
	// opening it and starting the turn.
	a.gate.Lock()
	defer a.gate.Unlock()
	view, opened, err := b.ensureLocked(a)
	if err != nil {
		return 0, err
	}
	if opened {
		b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	return b.startLocked(a, text, clientID)
}

// Retry sends the words of the agent's last failed turn again.
func (b *appBackend) Retry(id string) (uint64, error) {
	a, err := b.find(id)
	if err != nil {
		return 0, err
	}
	var text string
	b.locked(func() { text = a.failed })
	if text == "" {
		return 0, appwire.Errorf(appwire.CodeNotFound, "this agent has no failed turn to run again")
	}
	return b.Send(id, text, "")
}

// startLocked starts one turn on a held conversation. The caller holds a.gate.
func (b *appBackend) startLocked(a *appAgent, text, clientID string) (uint64, error) {
	var id string
	var live *appLive
	var refused error
	b.locked(func() {
		id, live = a.view.ID, a.live
		switch {
		case live == nil:
			refused = appwire.Errorf(appwire.CodeEngine, "the conversation was let go before the turn could start")
		case live.running:
			refused = appwire.Errorf(appwire.CodeBusy, "this agent is already working on a turn")
		}
	})
	if refused != nil {
		return 0, refused
	}
	// A conversation joined on a window's host can have heard from the person
	// in that window too, so its count is read again before it is used.
	if !live.conn.Assembled && live.conn.Transcript != "" {
		users := appUserCount(live.conn.Transcript)
		b.locked(func() { a.users = users })
	}
	var turn uint64
	var n int
	b.locked(func() {
		if live.running {
			refused = appwire.Errorf(appwire.CodeBusy, "this agent is already working on a turn")
			return
		}
		live.running, live.stopping = true, false
		live.lastUse = b.now()
		turn = b.turns.Add(1)
		n = a.users
	})
	if refused != nil {
		return 0, refused
	}
	events, err := live.conn.Conv.Submit(context.Background(), text)
	if err != nil {
		b.locked(func() { live.running = false })
		return 0, appEngineError(err)
	}
	var view appwire.Agent
	b.locked(func() {
		a.users = n + 1
		a.view.State = appwire.StateWorking
		if live.needsYou() {
			a.view.State = appwire.StateNeedsYou
		}
		a.view.UpdatedAt = b.now()
		view = a.view
		live.pumpDone = make(chan struct{})
	})
	live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	live.box.Event(appwire.EventTurnStarted, appwire.TurnStarted{Agent: id, Turn: turn})
	live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
		ID: appUserEntryID(n), Agent: id, Turn: turn, Kind: appwire.EntryUser, At: b.now(), Text: text, ClientID: clientID,
	}})
	guard.Go("app/turn", func() { b.pump(a, id, live, turn, n, text, events) })
	return turn, nil
}

// pump maps one turn's session events onto the wire. It never waits on the
// writer: everything goes into the agent's outbox. n is the turn's place among
// the person's messages, which every entry id of the turn is counted from.
func (b *appBackend) pump(a *appAgent, id string, live *appLive, turn uint64, n int, text string, events <-chan session.Event) {
	defer close(live.pumpDone)
	ok, failure := true, ""
	kind, status := "", 0
	changed := func(mutate func(*appwire.Agent)) {
		var view appwire.Agent
		b.locked(func() {
			mutate(&a.view)
			view = a.view
		})
		live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	entry := func(e appwire.Entry) {
		e.Agent, e.Turn, e.At = id, turn, b.now()
		live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: e})
	}
	// EVERY ASSISTANT MESSAGE OF THE TURN HAS ITS INDEX, msg, and the app keys
	// an answer by it: the deltas of message 0 append to answer 0, and answer
	// 0's entry replaces them, so the words before a tool call and the words
	// after it are two answers and neither is ever drawn twice. A message ends
	// where a tool call begins after its words, or where the turn ends, and
	// it is counted only if it said something — the same rule the listing
	// counts by ([appEntriesOf]), so both name it alike.
	msg, notices := 0, 0
	var reply, thinking strings.Builder
	sayReply := func() {
		words := strings.TrimSpace(reply.String())
		if words != "" {
			at := msg
			entry(appwire.Entry{ID: appAnswerEntryID(n, at), Kind: appwire.EntryAnswer, Text: words, Thinking: strings.TrimSpace(thinking.String()), Msg: &at})
			msg++
		}
		reply.Reset()
		thinking.Reset()
	}
	titles := map[string]string{}
	for event := range events {
		switch event.Kind {
		case session.EventTextDelta:
			reply.WriteString(event.Text)
			live.box.Delta(id, turn, msg, appwire.DeltaText, event.Text)
		case session.EventReasoning:
			thinking.WriteString(event.Text)
			live.box.Delta(id, turn, msg, appwire.DeltaThinking, event.Text)
		case session.EventToolBegin:
			sayReply()
			title := redact.Secrets(tui3.ToolTitle(event.Tool, event.Args, event.Hint))
			titles[event.CallID] = title
			live.box.Event(appwire.EventTurnTool, appwire.TurnTool{Agent: id, Turn: turn, Call: event.CallID, Name: event.Tool, Title: title, Phase: appwire.PhaseBegin, Summary: event.Hint})
		case session.EventToolEnd, session.EventToolFailed:
			succeeded := event.Kind == session.EventToolEnd
			summary := event.Hint
			if summary == "" && !succeeded {
				summary = event.Text
				if summary == "" && event.Err != nil {
					summary = event.Err.Error()
				}
			}
			summary = redact.Secrets(summary)
			title := titles[event.CallID]
			if title == "" {
				title = redact.Secrets(tui3.ToolTitle(event.Tool, event.Args, ""))
			}
			live.box.Event(appwire.EventTurnTool, appwire.TurnTool{Agent: id, Turn: turn, Call: event.CallID, Name: event.Tool, Title: title, Detail: appDetail(summary), Phase: appwire.PhaseEnd, OK: &succeeded, Summary: summary})
		case session.EventConsentRequest:
			call := appConsentCall{Tool: event.Tool, Args: event.Args}
			b.locked(func() { live.consents[event.ID] = call })
		case session.EventNotice:
			// THE ENGINE'S NOTICES ARE ITS DIM LINES — the skills a turn
			// carried, a request reshaped to land — drawn by the terminal as
			// one muted row and never as a message, and quiet says the same.
			if words := strings.TrimSpace(event.Text); words != "" {
				entry(appwire.Entry{ID: appLiveNoticeID(n, notices), Kind: appwire.EntryNotice, Text: redact.Secrets(words), Quiet: true})
				notices++
			}
		case session.EventError:
			ok = false
			kind, status, failure = appTurnFailure(event)
		case session.EventTurnDone:
			// A turn the person steered ends once per stretch; the last ending
			// on the stream is the turn's.
			ok, failure, kind, status = true, "", "", 0
		case session.EventTitleChanged:
			if title := strings.TrimSpace(event.Text); title != "" {
				changed(func(v *appwire.Agent) { v.Title = title })
			}
		case session.EventTaskUpdate:
			if event.Task != nil {
				b.taskNotice(id, live, event.Task)
			}
		}
	}
	sayReply()
	b.locked(func() {
		if !ok && live.stopping {
			kind = appwire.ErrorStopped
		}
		a.failed = ""
		if !ok {
			a.failed = text
		}
	})
	if !ok {
		entry(appwire.Entry{ID: appErrorEntryID(n), Kind: appwire.EntryError, Text: failure, ErrorKind: kind, Status: status})
	}
	live.box.Event(appwire.EventTurnDone, appwire.TurnDone{Agent: id, Turn: turn, OK: ok, Error: failure, ErrorKind: kind, Status: status})
	held := false
	var view appwire.Agent
	b.locked(func() {
		live.running = false
		live.lastUse = b.now()
		// Let go or closed while the turn ran: that road has said the state.
		if held = a.live == live; held {
			a.view.State = live.restingState()
			a.view.UpdatedAt = b.now()
			view = a.view
		}
	})
	if held {
		live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	b.spendMoved()
}

// ── entry ids ───────────────────────────────────────────────────────────────
//
// AN ENTRY'S ID IS THE SAME LIVE AND READ BACK. Both are counted from the
// conversation's own record: n is the place of the person's message among the
// person's messages (u0, u1 …), an answer is the m-th assistant message after
// it that said something (a<n>.<m>), and a tool entry is its call's own id
// (t<call>). The live pump counts as it streams and the listing counts as it
// reads, by one rule, so agents.entries names what entry.added named. What
// only ever exists live — a turn's error, an ask's receipt, the engine's dim
// notices — has an id of its own kind that the listing never makes.

func appUserEntryID(n int) string { return "u" + strconv.Itoa(n) }

func appAnswerEntryID(n, m int) string { return "a" + strconv.Itoa(n) + "." + strconv.Itoa(m) }

func appToolEntryID(call string) string { return "t" + call }

func appNoteEntryID(n, k int) string { return "n" + strconv.Itoa(n) + "." + strconv.Itoa(k) }

func appLiveNoticeID(n, k int) string { return "q" + strconv.Itoa(n) + "." + strconv.Itoa(k) }

func appErrorEntryID(n int) string { return "x" + strconv.Itoa(n) }

func appReceiptEntryID(askID string) string { return "r" + askID }

// appUserCount is how many of the person's messages a transcript holds.
func appUserCount(transcript string) int {
	record := session.ReadTranscript(transcript)
	users := 0
	for _, items := range [][]session.DisplayEntry{record.Earlier, record.Entries} {
		for _, item := range items {
			if item.Role == "user" {
				users++
			}
		}
	}
	return users
}

// appDetailLimit bounds a tool's short result line.
const appDetailLimit = 160

// appDetail is a tool's short result line: its first line, bounded.
func appDetail(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	line = strings.TrimSpace(line)
	if runes := []rune(line); len(runes) > appDetailLimit {
		line = string(runes[:appDetailLimit-1]) + "…"
	}
	return line
}

// appTurnFailure sorts a turn's error: its kind, the provider's status where
// there is one, and the words a person reads, which for a provider refusal are
// the provider's own.
func appTurnFailure(event session.Event) (kind string, status int, words string) {
	kind, status, words = event.ErrKind, event.ErrStatus, event.ErrWords
	if kind == "" && event.Err != nil {
		kind, status, words = session.ClassifyTurnError(event.Err)
	}
	switch kind {
	case session.TurnErrorProvider, session.TurnErrorBudget, session.TurnErrorStopped:
	default:
		kind = appwire.ErrorOther
	}
	if kind != session.TurnErrorProvider || strings.TrimSpace(words) == "" {
		words = "the turn ended with an error"
		if event.Err != nil {
			words = event.Err.Error()
		}
	}
	return kind, status, redact.Secrets(strings.TrimSpace(words))
}

// appTaskState is a task's state in the protocol's five words, and "" for a
// state none of them says. That includes the engine's interrupted — nothing is
// driving it and every step is kept — which the chat draws as `interrupted`,
// apart from failed; until the protocol has a word for it such a task is left
// out rather than given one it did not say.
func appTaskState(state string) string {
	switch session.TaskState(strings.TrimSpace(state)) {
	case session.TaskQueued:
		return appwire.TaskQueued
	case session.TaskRunning:
		return appwire.TaskRunning
	case session.TaskDone:
		return appwire.TaskDone
	case session.TaskFailed:
		return appwire.TaskFailed
	case session.TaskUnverified:
		return appwire.TaskUnverified
	}
	return ""
}

// taskNotice turns one task update into task.changed, once per change: the
// same update can arrive on the turn's stream and on the task lane.
func (b *appBackend) taskNotice(id string, live *appLive, notice *session.TaskNotice) {
	state := appTaskState(string(notice.State))
	if state == "" {
		return
	}
	task := appwire.Task{ID: strconv.FormatUint(notice.ID, 10), Agent: id, Title: notice.Title, State: state, UpdatedAt: b.now()}
	same := false
	b.locked(func() {
		if before, ok := live.tasks[task.ID]; ok && before.State == task.State && (task.Title == "" || before.Title == task.Title) {
			same = true
			return
		}
		if task.Title == "" {
			task.Title = live.tasks[task.ID].Title
		}
		live.tasks[task.ID] = task
	})
	if same {
		return
	}
	live.box.Event(appwire.EventTaskChanged, appwire.TaskChanged{Task: task})
}

// Stop interrupts the running turn. An agent with nothing held has nothing
// running in this process, which is answered as done.
func (b *appBackend) Stop(id string) error {
	a, err := b.find(id)
	if err != nil {
		return err
	}
	if live := b.liveOf(a); live != nil {
		b.locked(func() { live.stopping = live.running })
		live.conn.Conv.Interrupt()
	}
	return nil
}

// CloseAgent lets the conversation go and says the agent is closed. Its journal
// stays on disk; agents.open or agents.send opens it again.
func (b *appBackend) CloseAgent(id string) error {
	a, err := b.find(id)
	if err != nil {
		return err
	}
	view := b.closeAgent(a)
	b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	return nil
}

// closeAgent lets a's conversation go and marks it closed.
func (b *appBackend) closeAgent(a *appAgent) (view appwire.Agent) {
	a.gate.Lock()
	defer a.gate.Unlock()
	b.release(a)
	b.locked(func() {
		a.view.State = appwire.StateClosed
		view = a.view
	})
	return view
}

// release gives a's conversation back and flushes its last events. Callers
// hold a.gate.
func (b *appBackend) release(a *appAgent) {
	var live *appLive
	b.locked(func() { live, a.live = a.live, nil })
	if live == nil {
		return
	}
	b.finish(live)
}

// finish gives a conversation that is no longer held back, and shuts its
// outbox once the turn and the lanes have said their last.
func (b *appBackend) finish(live *appLive) {
	if live.stopTasks != nil {
		live.stopTasks()
	}
	if live.stopAsks != nil {
		live.stopAsks()
	}
	live.conn.Release()
	wait := time.After(appReleaseWait)
	if live.pumpDone != nil {
		select {
		case <-live.pumpDone:
		case <-wait:
		}
	}
	for _, done := range []chan struct{}{live.lanesDone, live.asksDone} {
		select {
		case <-done:
		case <-wait:
		}
	}
	live.box.Close()
}

// ListAsks answers the agent's open asks. An agent with no conversation held
// has none this process can answer.
func (b *appBackend) ListAsks(id string) ([]appwire.Ask, error) {
	a, err := b.find(id)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if a.live == nil {
		return nil, nil
	}
	return append([]appwire.Ask(nil), a.live.asks...), nil
}

// appAnsweredMark starts the value [appLive.answered] holds for an ask this app
// answered, so an answer of words alone, with no option, is still marked.
const appAnsweredMark = "app:"

// AnswerAsk answers one open ask through the engine's own answer door, the one
// the chat's question page uses. Its closing arrives on the question lane.
func (b *appBackend) AnswerAsk(id, askID, option, text string) error {
	a, err := b.find(id)
	if err != nil {
		return err
	}
	var live *appLive
	var q session.Question
	var key string
	var refused error
	b.locked(func() {
		live = a.live
		open := false
		if live != nil {
			q, open = live.questions[askID]
		}
		if !open {
			refused = appwire.Errorf(appwire.CodeNotFound, "the ask %s is not open; it may have been answered elsewhere", askID)
			return
		}
		if option != "" {
			picked, offered := appOptionNamed(q, option)
			if !offered {
				refused = appwire.Errorf(appwire.CodeBadParams, "the ask %s offers no option %q", askID, option)
				return
			}
			key = picked.key
		}
		live.answered[askID] = appAnsweredMark + option
		live.lastUse = b.now()
	})
	if refused != nil {
		return refused
	}
	if q.Kind == session.QuestionConsent && option == appwire.OptionAlways && b.rememberAlways(b.consentCall(live, q.ID)) {
		// THE RULE IS WRITTEN FIRST and the answer says so, and the engine
		// reads the rules again before it lets the call go: the next call
		// like this one is answered by the person's own settings.
		live.conn.Conv.ResolveConsentRemember(q.ID, true, session.ConsentRule)
		return nil
	}
	answer := session.Answer{Kind: q.Kind, ID: q.ID, Ref: q.Ref, Ask: q.Ask, Key: key, Change: strings.TrimSpace(text)}
	if key != "" {
		answer.Picked = []string{key}
	}
	if err := live.conn.Conv.ResolveQuestion(answer); err != nil {
		b.locked(func() { delete(live.answered, askID) })
		return appEngineError(err)
	}
	return nil
}

// appConsentWait bounds how long an always waits for the turn to have said
// which call it is about. The question and the call travel on two lanes and the
// question can arrive first; the wait is only ever paid on an always.
const appConsentWait = 2 * time.Second

// consentCall is the call approval id is about, as the turn announced it.
func (b *appBackend) consentCall(live *appLive, id uint64) appConsentCall {
	deadline := time.Now().Add(appConsentWait)
	for {
		var call appConsentCall
		seen := false
		b.locked(func() { call, seen = live.consents[id] })
		if seen || time.Now().After(deadline) {
			return call
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// rememberAlways writes an always down as a rule in the person's own profile:
// the one command line for a shell call, the tool for any other. It reports
// whether the rule is written; when it is not (a compound command, a settings
// row that already answers otherwise, a call the turn never announced) the
// always is the engine's own, for the rest of this conversation.
func (b *appBackend) rememberAlways(call appConsentCall) bool {
	tool := strings.TrimSpace(call.Tool)
	if tool == "" {
		return false
	}
	profile := config.ProfileDir()
	if tool == "bash" {
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(call.Args), &args); err != nil || strings.TrimSpace(args.Command) == "" {
			return false
		}
		return config.RememberBashApproval(profile, args.Command) == nil
	}
	return config.RememberToolApproval(profile, tool, "allow") == nil
}

// Entries reads the agent's transcript off the disk, whether or not a
// conversation is held for it: the journal is written as the turn goes.
func (b *appBackend) Entries(id, before string, limit int) ([]appwire.Entry, bool, error) {
	a, err := b.find(id)
	if err != nil {
		return nil, false, err
	}
	var transcript string
	var at time.Time
	b.locked(func() { transcript, at = a.transcript, a.view.UpdatedAt })
	if transcript == "" {
		return []appwire.Entry{}, false, nil
	}
	record := session.ReadTranscript(transcript)
	all := appEntriesOf(id, append(append([]session.DisplayEntry(nil), record.Earlier...), record.Entries...), at)
	if words := strings.TrimSpace(record.Unreadable); words != "" {
		all = append(all, appwire.Entry{ID: "unreadable", Agent: id, Kind: appwire.EntryNotice, At: at, Text: words})
	}
	end := len(all)
	if before != "" {
		end = -1
		for i, entry := range all {
			if entry.ID == before {
				end = i
				break
			}
		}
		if end < 0 {
			return nil, false, appwire.Errorf(appwire.CodeNotFound, "this conversation has no entry %s", before)
		}
	}
	start := max(end-limit, 0)
	page := append([]appwire.Entry{}, all[start:end]...)
	return page, start > 0, nil
}

// appEntriesOf is a transcript as the app's entries, named by the rule the
// live pump names them by (see the entry ids above). The journal keeps no
// clock per message, so every entry carries the conversation's own last moment.
func appEntriesOf(agent string, items []session.DisplayEntry, at time.Time) []appwire.Entry {
	entries := make([]appwire.Entry, 0, len(items))
	n, m, k := -1, 0, 0
	for i, item := range items {
		entry := appwire.Entry{Agent: agent, At: at}
		switch item.Role {
		case "user":
			n, m, k = n+1, 0, 0
			entry.ID, entry.Kind, entry.Text = appUserEntryID(n), appwire.EntryUser, item.Text
		case "assistant":
			if strings.TrimSpace(item.Text) == "" {
				continue
			}
			msg := m
			entry.ID, entry.Kind, entry.Text, entry.Msg = appAnswerEntryID(n, msg), appwire.EntryAnswer, strings.TrimSpace(item.Text), &msg
			m++
		case "tool":
			call := item.CallID
			if call == "" {
				call = "#" + strconv.Itoa(i)
			}
			entry.ID, entry.Kind, entry.Call, entry.Name = appToolEntryID(call), appwire.EntryTool, item.CallID, item.Tool
			entry.Title = redact.Secrets(tui3.ToolTitle(item.Tool, item.Args, item.Hint))
			entry.Summary = redact.Secrets(item.Hint)
			entry.Detail = appDetail(redact.Secrets(item.Output))
		case "note", "aside":
			if strings.TrimSpace(item.Text) == "" {
				continue
			}
			entry.ID, entry.Kind, entry.Text = appNoteEntryID(n, k), appwire.EntryNotice, item.Text
			k++
		default:
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

// SetModel moves the agent to another model, opening its conversation first if
// none is held: the choice is the conversation's own and is saved with it.
func (b *appBackend) SetModel(id, model string) (appwire.Agent, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeBadParams, "name a model")
	}
	a, err := b.find(id)
	if err != nil {
		return appwire.Agent{}, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	if _, _, err := b.ensureLocked(a); err != nil {
		return appwire.Agent{}, err
	}
	var live *appLive
	b.locked(func() {
		live = a.live
		live.lastUse = b.now()
	})
	live.conn.Conv.SetModel(model)
	if now := live.conn.Conv.Model(); now != "" {
		model = now
	}
	var view appwire.Agent
	b.locked(func() {
		a.view.Model = model
		view = a.view
	})
	b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	return view, nil
}

// SetEffort sets how hard the agent's model is asked to think, opening its
// conversation first if none is held. The level is the conversation's own,
// kept per model the way the terminal's ctrl+t keeps it.
func (b *appBackend) SetEffort(id, effort string) (appwire.Agent, error) {
	a, err := b.find(id)
	if err != nil {
		return appwire.Agent{}, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	if _, _, err := b.ensureLocked(a); err != nil {
		return appwire.Agent{}, err
	}
	var live *appLive
	b.locked(func() {
		live = a.live
		live.lastUse = b.now()
	})
	model := live.conn.Conv.Model()
	live.conn.Conv.SetReasoningFor(model, effort)
	if now := live.conn.Conv.ReasoningFor(model); now != "" {
		effort = now
	}
	var view appwire.Agent
	b.locked(func() {
		a.view.Effort = effort
		view = a.view
	})
	b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	return view, nil
}

// Warm opens the engine's process half — the profile, the model catalog, the
// harness and skill shelves every conversation is built on — behind the hello,
// so the first agents.create does not wait for it. It is what the terminal does
// as it starts, network included.
func (b *appBackend) Warm() {
	run := false
	b.locked(func() {
		if !b.closing && b.warmed == nil {
			b.warmed, run = make(chan struct{}), true
		}
	})
	if !run {
		return
	}
	defer close(b.warmed)
	defer guard.Recover("app/warm")
	_, _ = b.process()
}

// process is the engine's shared process half, opened once.
func (b *appBackend) process() (*v3Process, error) {
	proc, err := openEngineProcess()
	if err == nil {
		b.assembled.Store(true)
	}
	return proc, err
}

// appModelsWait bounds how long models.list waits for a model list still
// arriving when nothing is remembered on disk yet.
const appModelsWait = 3 * time.Second

// ListModels answers exactly what the terminal's model picker offers for the
// configured services — the same process catalog, the same per-service shelf,
// the same caches, through the picker's own composer ([tui3.PickerModels]) —
// with the model a new conversation runs on marked default, and added when no
// list names it, so the answer is never empty while a turn can run.
func (b *appBackend) ListModels() ([]appwire.Model, error) {
	proc, err := b.process()
	if err != nil {
		return nil, appEngineError(err)
	}
	read := func() []tui3.Model {
		_, sources := proc.currentAccount()
		return tui3.PickerModels(proc.ProfileDir, sources, proc.Shelf.pickerModels, proc.Shelf.modelsForService)
	}
	rows := read()
	if len(rows) == 0 && proc.Models != nil {
		ctx, cancel := context.WithTimeout(context.Background(), appModelsWait)
		proc.Models.Warmed(ctx)
		cancel()
		rows = read()
	}
	chosen := appDefaultModel()
	models := make([]appwire.Model, 0, len(rows)+1)
	seen := false
	for _, row := range rows {
		provider := strings.TrimSpace(row.Group)
		if provider == "" {
			provider, _, _ = strings.Cut(strings.TrimPrefix(row.ID, "~"), "/")
		}
		model := appwire.Model{ID: row.ID, Name: row.ID, Provider: provider, Default: row.ID == chosen}
		seen = seen || model.Default
		models = append(models, model)
	}
	if !seen && chosen != "" {
		provider, _, _ := strings.Cut(strings.TrimPrefix(chosen, "~"), "/")
		models = append(models, appwire.Model{ID: chosen, Name: chosen, Provider: provider, Default: true})
	}
	return models, nil
}

// appDefaultModel is the model a new conversation runs on, by the rule every
// local door opens one with ([v3TalkModel]).
func appDefaultModel() string {
	settings, err := config.LoadKeyless()
	if err != nil {
		return ""
	}
	return v3TalkModel("", settings)
}

// Settings reads the profile's own rows; a figure nobody set is null.
func (b *appBackend) Settings() (appwire.Settings, error) {
	profile := config.ProfileDir()
	settings := appwire.Settings{
		DefaultModel: appDefaultModel(),
		AsksBefore:   config.ToolApprovalModeAt(profile),
		BudgetUSD:    appFigure(config.SpendRailUSDAt(profile)),
	}
	dayCap, err := config.DailyBudgetUSDAt(profile)
	if err != nil {
		return appwire.Settings{}, appEngineError(err)
	}
	settings.DayCapUSD = appFigure(dayCap)
	return settings, nil
}

// appFigure is a money figure, or nil for none.
func appFigure(usd float64) *float64 {
	if usd <= 0 {
		return nil
	}
	return &usd
}

// SpendToday reads this machine's usage ledger for today.
func (b *appBackend) SpendToday() (appwire.Spend, error) {
	now := b.now()
	lines, err := session.ReadUsage(session.UsageLedgerPath(), now.Add(-48*time.Hour))
	if err != nil {
		return appwire.Spend{}, appEngineError(err)
	}
	spend := appwire.Spend{TodayUSD: session.SpendToday(lines, now)}
	if dayCap, err := config.DailyBudgetUSDAt(config.ProfileDir()); err == nil {
		spend.DayCapUSD = appFigure(dayCap)
	}
	return spend, nil
}

// appSpendSettle is how long after a turn ends its spending is read again: the
// ledger is written behind the turn, not in front of its ending.
const appSpendSettle = 300 * time.Millisecond

// spendMoved says spend.changed once a turn's spending has reached the ledger,
// if today's figure moved.
func (b *appBackend) spendMoved() {
	time.AfterFunc(appSpendSettle, func() {
		defer guard.Recover("app/spend")
		if b.isClosing() {
			return
		}
		spend, err := b.SpendToday()
		if err != nil {
			return
		}
		moved := false
		b.locked(func() {
			moved = spend.TodayUSD != b.spent
			b.spent = spend.TodayUSD
		})
		if moved {
			b.emit.Emit(appwire.EventSpendChanged, appwire.SpendChanged{Spend: spend})
		}
	})
}

// ListTasks reads the agent's tasks off its project's task index, and lays
// over them what the held conversation has said since.
func (b *appBackend) ListTasks(id string) ([]appwire.Task, error) {
	a, err := b.find(id)
	if err != nil {
		return nil, err
	}
	var transcript string
	var fallback time.Time
	var heard map[string]appwire.Task
	b.locked(func() {
		transcript, fallback = a.transcript, a.view.UpdatedAt
		if a.live != nil {
			heard = make(map[string]appwire.Task, len(a.live.tasks))
			for k, v := range a.live.tasks {
				heard[k] = v
			}
		}
	})
	var tasks []appwire.Task
	index := map[string]int{}
	if path := session.TaskIndexPath(transcript); path != "" {
		for _, row := range session.ReadTaskIndex(path) {
			if row.SessionID != id {
				continue
			}
			state := appTaskState(row.Status)
			if state == "" {
				continue
			}
			at := row.EndedAt
			if at.IsZero() {
				at = row.StartedAt
			}
			if at.IsZero() {
				at = fallback
			}
			title := row.Title
			if title == "" {
				title = row.Label
			}
			index[row.ID] = len(tasks)
			tasks = append(tasks, appwire.Task{ID: row.ID, Agent: id, Title: title, State: state, UpdatedAt: at})
		}
	}
	for taskID, task := range heard {
		if i, ok := index[taskID]; ok {
			if task.Title == "" {
				task.Title = tasks[i].Title
			}
			tasks[i] = task
			continue
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// ListCrews reads the teams file directly and starts watching it.
func (b *appBackend) ListCrews() ([]appwire.Crew, error) {
	crews, err := appReadCrews()
	if err != nil {
		return nil, appEngineError(err)
	}
	b.locked(func() {
		if b.crews == nil && !b.closing {
			b.crews = newAppCrewWatch(b.emit, crews)
		}
	})
	return crews, nil
}

// appReadCrews is every open team, members named by agent id.
func appReadCrews() ([]appwire.Crew, error) {
	file, err := teamstore.Load(config.ProfileDir())
	if err != nil {
		return nil, err
	}
	var crews []appwire.Crew
	for _, team := range file.Teams {
		if team.Closed() {
			continue
		}
		crew := appwire.Crew{ID: team.ID, Name: team.Name, Members: []appwire.CrewMember{}}
		for _, member := range team.Members {
			agent := appMemberAgent(member)
			if member.Key == team.Manager {
				crew.Manager = agent
			}
			crew.Members = append(crew.Members, appwire.CrewMember{Agent: agent, Handle: member.Handle})
		}
		crews = append(crews, crew)
	}
	return crews, nil
}

// appMemberAgent is a team member's agent id: its transcript's folder name.
func appMemberAgent(member teamstore.Member) string {
	transcript := member.File
	if strings.TrimSpace(transcript) == "" {
		transcript = member.Key
	}
	return appAgentID(transcript)
}

// Close lets every conversation go and stops the watchers.
func (b *appBackend) Close() error {
	already := false
	var crews *appCrewWatch
	var agents []*appAgent
	var warmed chan struct{}
	b.locked(func() {
		if already = b.closing; already {
			return
		}
		b.closing = true
		crews, warmed = b.crews, b.warmed
		for _, a := range b.agents {
			agents = append(agents, a)
		}
	})
	if already {
		return nil
	}
	close(b.sweepStop)
	<-b.sweepDone
	if warmed != nil {
		<-warmed
	}
	if crews != nil {
		crews.close()
	}
	var wg sync.WaitGroup
	for _, a := range agents {
		if b.liveOf(a) == nil {
			continue
		}
		wg.Add(1)
		guard.Go("app/close", func() {
			defer wg.Done()
			// Stop first: a turn still running is interrupted rather than left
			// holding the gate while the process leaves.
			// A conversation joined on a window's host is that window's, and
			// is only detached from.
			if live := b.liveOf(a); live != nil && live.conn.Assembled {
				live.conn.Conv.Interrupt()
			}
			b.letGo(a)
		})
	}
	wg.Wait()
	return nil
}

// letGo gives a's conversation back under its gate.
func (b *appBackend) letGo(a *appAgent) {
	a.gate.Lock()
	defer a.gate.Unlock()
	b.release(a)
}

func (b *appBackend) liveNeedsYou(live *appLive) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return live.needsYou()
}

func (b *appBackend) isClosing() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closing
}

// sweep lets idle conversations go ([appAgentIdle]).
func (b *appBackend) sweep() {
	defer close(b.sweepDone)
	ticker := time.NewTicker(appSweepEvery)
	defer ticker.Stop()
	for {
		select {
		case <-b.sweepStop:
			return
		case <-ticker.C:
			b.sweepOnce()
		}
	}
}

// sweepOnce is one look at the idle rule. A conversation is kept while a turn
// runs or a question waits for the person, whatever the clock says.
func (b *appBackend) sweepOnce() {
	now := b.now()
	var idle []*appAgent
	b.locked(func() {
		for _, a := range b.agents {
			if live := a.live; live != nil && !live.running && now.Sub(live.lastUse) >= appAgentIdle {
				idle = append(idle, a)
			}
		}
	})
	for _, a := range idle {
		b.sweepAgent(a, now)
	}
}

// sweepAgent lets one idle conversation go, unless something took its gate or
// woke it since the look.
func (b *appBackend) sweepAgent(a *appAgent, now time.Time) {
	if !a.gate.TryLock() {
		return
	}
	defer a.gate.Unlock()
	var live *appLive
	keep := true
	b.locked(func() {
		live = a.live
		keep = live == nil || live.running || now.Sub(live.lastUse) < appAgentIdle
	})
	if !keep && !b.liveNeedsYou(live) {
		// The state the app reads does not move: the conversation is as
		// idle on disk as it was in memory.
		b.release(a)
	}
}

// appEngineError keeps a wire error's code and calls anything else the
// engine's failure, in words fit for a person.
func appEngineError(err error) error {
	var wire *appwire.Error
	if errors.As(err, &wire) {
		return wire
	}
	return &appwire.Error{Code: appwire.CodeEngine, Message: redact.Secrets(plainWords(err.Error()))}
}

// appCrewWatch emits crew.changed when the teams file changes under the app.
type appCrewWatch struct {
	emit  appwire.Emitter
	stop  chan struct{}
	done  chan struct{}
	known map[string]string
}

func newAppCrewWatch(emit appwire.Emitter, crews []appwire.Crew) *appCrewWatch {
	w := &appCrewWatch{emit: emit, stop: make(chan struct{}), done: make(chan struct{}), known: appCrewPrints(crews)}
	guard.Go("app/crews", w.run)
	return w
}

func appCrewPrints(crews []appwire.Crew) map[string]string {
	prints := make(map[string]string, len(crews))
	for _, crew := range crews {
		prints[crew.ID] = fmt.Sprintf("%#v", crew)
	}
	return prints
}

func (w *appCrewWatch) run() {
	defer close(w.done)
	path := teamstore.Path(config.ProfileDir())
	stamp := func() (time.Time, int64) {
		info, err := os.Stat(path)
		if err != nil {
			return time.Time{}, -1
		}
		return info.ModTime(), info.Size()
	}
	at, size := stamp()
	ticker := time.NewTicker(appCrewPoll)
	defer ticker.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
		}
		nowAt, nowSize := stamp()
		if nowAt.Equal(at) && nowSize == size {
			continue
		}
		at, size = nowAt, nowSize
		crews, err := appReadCrews()
		if err != nil {
			continue
		}
		prints := appCrewPrints(crews)
		for _, crew := range crews {
			if w.known[crew.ID] != prints[crew.ID] {
				w.emit.Emit(appwire.EventCrewChanged, appwire.CrewChanged{Crew: crew})
			}
		}
		// A team deleted or closed is no longer a crew.
		for id := range w.known {
			if _, still := prints[id]; !still {
				w.emit.Emit(appwire.EventCrewRemoved, appwire.CrewRemoved{ID: id})
			}
		}
		w.known = prints
	}
}

func (w *appCrewWatch) close() {
	close(w.stop)
	<-w.done
}
