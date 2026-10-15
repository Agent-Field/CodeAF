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
	"net"
	"net/http"
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
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/openrouterauth"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
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
	turnLong := flags.Duration("turn-long", appTurnLong, "how long a reply runs before the app is told it is running long")
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
	// THE APP'S KEYS LIVE IN THIS PROCESS'S MEMORY, installed before anything
	// can read a key, and no profile write may put one on disk while they are.
	defer config.UseAppCredentials(config.NewAppCredentials())()
	server := appwire.NewServer(wire)
	backend := newAppBackend(server, appOpenInProcess)
	if *turnLong > 0 {
		backend.turnLong = *turnLong
	}
	// The crews are watched from the start: a manager's start or a member
	// woken by its manager must not wait for the window to ask for crews.
	backend.memoryStore = func() (*store.Store, error) {
		proc, err := openEngineProcess()
		if err != nil || proc.Memory == nil {
			return nil, errors.New("memory is off")
		}
		return proc.Memory, nil
	}
	backend.searchWords = func(terms string, limit int) ([]store.ConversationHit, error) {
		proc, err := openEngineProcess()
		if err != nil || proc.Search == nil {
			return nil, errors.New("conversation search is unavailable")
		}
		return proc.Search.SearchConversations(terms, limit)
	}
	guard.Go("app/crews-start", func() { backend.crews() })
	// THE APP IS AN OPEN WINDOW for as long as it runs, so the clock that runs
	// standing orders runs while it is open (apporders.go).
	orders, closeOrders := appOrdersStore()
	backend.orderStore = orders
	defer closeOrders()
	defer keepAutomationsWindow("app")()
	guard.Go("app/orders", func() { backend.watchOrders(backend.sweepStop) })
	// And what the last quit stopped in the middle of a reply carries on.
	guard.Go("app/resume-after-quit", backend.resumeAfterQuit)
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
	// Usage is what the conversation has spent so far, by the provider's own
	// figures, which its budget is measured against.
	Usage() session.Usage
	// ResolveConsentRemember answers one approval and says how long the answer
	// lasts; the app uses it for an always that wrote a rule first.
	ResolveConsentRemember(id uint64, allow bool, scope session.ConsentScope)
	// How hard the model is asked to think, per model.
	ReasoningFor(model string) string
	SetReasoningFor(model, level string)
	// The task graph's app reads and verbs (internal/session's task_app.go),
	// the move of a running reply to a task, and the job registry's notices.
	AppTasks() ([]session.AppTaskView, error)
	AppTask(id string) (session.AppTaskView, bool, error)
	AppTaskAction(intent session.AppTaskIntent) error
	AppQuickTasks() ([]session.AppQuickTaskView, error)
	AppQuickTaskAction(intent session.AppTaskIntent) ([]string, []string, error)
	MoveTurnToTask() error
	// Steer puts words into the running turn (internal/session's steer.go),
	// and RetryTask runs a failed task again.
	Steer(text string) (<-chan session.Event, error)
	RetryTask(id uint64) error
	JobNotices() ([]session.JobNotice, error)
	RerunProcess(id int) (session.JobNotice, error)
	Cancel(id string) (string, error)
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
	// ClockS is how many seconds a proposed task waits before it starts on
	// its own: the app's own setting, never the terminal's.
	ClockS int
}

// appConn is an opened conversation and the one way to give it back.
type appConn struct {
	Conv       appConversation
	Workspace  string
	Transcript string
	Release    func()
	// Follow is the turns this connection did not start — the ones the
	// conversation starts on its own and the ones another window sends —
	// when the conversation is reached across a connection. Nil is a
	// conversation held in this process, which hands its own turns over
	// directly (appwakes.go).
	Follow func() <-chan remote.Following
	// Assembled says this process built the conversation itself, so the shared
	// engine half must be closed when the app leaves.
	Assembled bool
}

type appOpener func(appOpenAsk) (*appConn, error)

// appHostDialTimeout bounds each host dial while looking for the one holding
// a conversation.
const appHostDialTimeout = 500 * time.Millisecond

// appOpenInProcess opens a conversation for the app. A saved conversation that
// a window's host already holds is JOINED there, as a second view of the same
// conversation, because opening it here as well would be a second writer on its
// journal. Everything else is built in this process.
func appOpenInProcess(ask appOpenAsk) (*appConn, error) {
	clock := ask.ClockS
	launch := &remote.LaunchShape{Interactive: true, DeterministicNames: true, HandOverOnAsk: true, ProposalClockS: &clock, ChoiceClockS: clock, QuitGraceS: appQuitGraceS, LiveToolOutput: true, StandingOrders: true, QuickTasks: true}
	if ask.Transcript != "" && !ask.Minted {
		// A JOIN CARRIES THE APP'S LAUNCH, and the engine applies what of it a
		// conversation another window opened can carry (engine.go's JoinedWith).
		hello := remote.Hello{Workspace: ask.Workspace, Session: ask.Transcript, Join: true, Launch: launch}
		if socket, err := enginehost.DialStartingHost(ask.Workspace); err == nil {
			client, err := remote.Dial(socket, "", hello)
			if err == nil {
				return appConnOf(client, func() { _ = client.Close() }, false), nil
			}
			_ = socket.Close()
		}
		// HELD, BUT NOT BY THIS WORKSPACE'S HOST: a crew member a manager
		// started lives in its manager's host. Every host is asked, and the
		// one holding it answers; opening it here as well would be refused by
		// its journal's lock, and is never tried while somebody holds it.
		if session.InUse(ask.Transcript) {
			for _, path := range enginehost.Sockets() {
				socket, err := net.DialTimeout("unix", path, appHostDialTimeout)
				if err != nil {
					continue
				}
				client, err := remote.Dial(socket, "", hello)
				if err == nil {
					return appConnOf(client, func() { _ = client.Close() }, false), nil
				}
				_ = socket.Close()
			}
			return nil, appwire.Errorf(appwire.CodeConflict, "this conversation is open in another codeaf window that the app cannot reach; close it there, or open it in that window")
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
		// And only the person moves a running reply to a task, and a proposed
		// task's clock is the app's, as is a suggested choice's.
		Launch: launch,
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
	return &appConn{Conv: client.Agent(), Workspace: welcome.Workspace, Transcript: welcome.SessionFile, Release: release, Follow: client.Follow, Assembled: assembled}
}

// appBackend is the [appwire.Engine] over this process's conversations.
type appBackend struct {
	// searchWords reads every conversation's words for search.all; nil keeps
	// the messages group empty.
	searchWords appSearchWords
	// memoryStore is the memory store the Settings window lists; nil is memory
	// off.
	memoryStore appMemoryStore
	// orderStore is the automations store standing orders are kept in
	// (apporders.go); nil is standing orders off.
	orderStore appOrderStore
	// stopPhases puts the engine's phase news back the way it was.
	stopPhases func()
	// profileDir is the profile whose teams are the crews, and the process's
	// own when empty; layer is the crews' app layer and crewRun the watch over
	// them, started by the first crew call.
	profileDir string
	crewOnce   sync.Once
	layer      *appCrewLayer
	crewRun    *appCrewRun
	emit       appwire.Emitter
	open       appOpener
	now        func() time.Time
	turns      atomic.Uint64
	assembled  atomic.Bool

	mu      sync.Mutex
	agents  map[string]*appAgent
	closing bool

	sweepStop chan struct{}
	sweepDone chan struct{}
	// spent is the last figure spend.changed said, so it is said on a change.
	spent float64
	// mint makes a new conversation's folder and answers its transcript path:
	// the one piece of agents.create that happens before the answer.
	mint func(workspace string) (string, error)
	// warmed closes when [appBackend.Warm] has finished, which Close waits
	// for: the process half it opens is closed after the backend is.
	warmed chan struct{}
	// keys are the keys the app handed over (appaccount.go); keyClient and
	// keyAddress replace the network and the provider's address in a test.
	keys       *config.AppCredentials
	keyClient  *http.Client
	keyAddress func(provider string) string
	// signIn is the browser sign-in waiting for the person, if any.
	signIn *openrouterauth.Flow
	// state is the app's own file: setup, its settings, budgets and raises.
	state *appState
	// today replaces the usage ledger's figure for today in a test.
	today func(time.Time) float64
	// turnLong is how long a reply runs before turn.long ([appTurnLong]).
	turnLong time.Duration
	// inbox is every open ask as one list (appinbox.go); emit is its tap.
	inbox *appInbox
	// toolQuiet is how long a call runs before its row is said again with a
	// fresh tail ([appToolQuiet]).
	toolQuiet time.Duration
	// follows are the processes whose output the app is following.
	follows map[string]*appFollow
	// asleep says the Mac is asleep (power.sleep until power.wake).
	asleep bool
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
	// failedKind is how that turn failed; a turn refused for money is run
	// again when the limit is raised.
	failedKind string
	// step is the title of the call the running turn is on, the live line
	// a crew's member row and the agent's own row show while it works.
	step string
	// boot is what agents.create asked of a conversation not yet open, taken
	// by whichever road opens it first; opening says one is being assembled.
	boot    *appBoot
	opening bool
	// users is how many messages of the person's the transcript holds, which
	// is what an entry's id is counted from ([appUserEntryID]); counted says
	// it has been read.
	users   int
	counted bool
	// marks are the turn number and client id of each message of the
	// person's this process sent, by its place, so a listing can say them.
	marks map[int]appTurnMark
	// calls is the work log's record of this agent's tool calls (apptools.go).
	calls *appCallLog
	// queue is the person's messages waiting for the turn to end, attach the
	// attachments kept for the next message, walk the last walk of the
	// workspace's files, and sending the message being sent (appconvo.go).
	queue   []appQueued
	queueN  int
	attach  map[string]appAttach
	attachN int
	walk    *appWalk
	sending *appSending
}

// appTurnMark is what this process knows of one of the person's messages.
type appTurnMark struct {
	turn     uint64
	clientID string
	// text and attachments are the person's own words and the files and
	// links that rode with them, where the engine was sent both as words.
	text        string
	attachments []appwire.Attachment
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
	// money are the open budget asks this process raised itself, by ask id;
	// budgetAsked is the last budget level asked about, so it is asked once;
	// lastAnswer is the agent's last words, which a budget ask quotes.
	money       map[string]appMoneyAsk
	budgetAsked string
	lastAnswer  string
	// turn is the running turn's number and steps how many tool calls it has
	// made, which turn.long says.
	turn  uint64
	steps int
	// phases are the running tasks' phase words, quicks and conflicts the
	// quick tasks as last said and the files an undo found changed, and
	// processes and logs the processes as last said and their spools.
	phases map[string]string
	// said is what each running task last said about its present (the gap
	// it is closing, what it waits on, its own phase), calls the call each
	// is making right now, and pilots the way to stop watching each one.
	said      map[string]string
	calls     map[string]string
	pilots    map[string]func()
	quicks    map[string]appwire.Quick
	conflicts map[string][]string
	processes map[string]appwire.Process
	logs      map[string]string
	// slept are the processes a sleep stopped, said once on wake.
	slept []appSlept
	// steers counts the turn's steers; adopting is how many fallen-through
	// steers are about to become turns; batchDone are the answered calls of
	// each batch of asks; notes are the notes sent to each task not yet
	// read, and noteN counts them; closed says the conversation was let go.
	steers    int
	adopting  int
	batchDone map[string][]appwire.BatchItem
	notes     map[string][]appNotePending
	noteN     int
	// msgBase and noteBase are where the next turn's answer and notice counts
	// start, and lastN, lastMsgs and lastNotices are where the last turn left
	// them under the person's message it answered.
	msgBase, noteBase            int
	lastN, lastMsgs, lastNotices int
	stopWakes                    func()
	// retries counts the running turn's waits before a step is asked again,
	// retryPhase and retryUntil are the last one said, and retrySaid says the
	// next void attempt was already said with its countdown.
	retries    int
	retryPhase provider.Phase
	retryUntil time.Time
	retrySaid  bool
	closed     bool
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
	keys := config.AppCredentialSource()
	if keys == nil {
		keys = config.NewAppCredentials()
	}
	b := &appBackend{
		keys:      keys,
		state:     &appState{},
		turnLong:  appTurnLong,
		toolQuiet: appToolQuiet,
		follows:   map[string]*appFollow{},
		open:      open,
		now:       time.Now,
		agents:    map[string]*appAgent{},
		sweepStop: make(chan struct{}),
		sweepDone: make(chan struct{}),
		mint:      appMint,
	}
	b.emit = b.newAppInbox(emit)
	b.stopPhases = b.listenPhases()
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
	disk := appRowView(row)
	busyElsewhere := a.live == nil && (disk.State == appwire.StateWorking || disk.State == appwire.StateNeedsYou)
	if !busyElsewhere && (a.live != nil || a.view.State == appwire.StateClosed || a.view.State == appwire.StateStopped) {
		view := a.view
		if view.Title == "" {
			view.Title = row.Title
		}
		return view
	}
	a.view = disk
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
	// AN AGENT NOBODY HAS TOUCHED FOR THE SETTING'S DAYS READS CLOSED: its
	// journal is kept, and opening or sending to it opens it again.
	idleDays := appIdleCloseDays
	_ = b.state.read(func(f *appStateFile) { idleDays = f.idleDays() })
	stale := time.Time{}
	if idleDays > 0 {
		stale = b.now().Add(-time.Duration(idleDays) * 24 * time.Hour)
	}
	seen := map[string]bool{}
	var agents []appwire.Agent
	for _, row := range session.ReadWorld(session.PlacesRoot()).Sessions() {
		if row.DeletionPending || !matches(row.Workspace, row.ProjectDir) {
			continue
		}
		seen[row.ID] = true
		view := b.remember(row)
		if !stale.IsZero() && view.State == appwire.StateIdle && !view.UpdatedAt.IsZero() && view.UpdatedAt.Before(stale) && !b.held(view.ID) {
			view.State = appwire.StateClosed
		}
		view.SpentUSD, view.LastActiveAt = row.Spend, row.At
		b.tidyMarks(&view, row.Archived)
		agents = append(agents, view)
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
	for i := range agents {
		b.dress(&agents[i])
	}
	return agents, nil
}

// CreateAgent answers a new agent AT ONCE and assembles its conversation
// behind the answer. With no workspace it is the person's home folder; with a
// budget the conversation's own spend limit is set before anything is spent;
// with words the first turn starts as soon as the conversation is open and the
// agent is named by them. A conversation that cannot be opened says so as a
// failed turn and a stopped agent.
func (b *appBackend) CreateAgent(ask appwire.CreateAsk) (appwire.Agent, error) {
	// No workspace is the work folder the person chose, or their home folder.
	workspace := ask.Workspace
	if strings.TrimSpace(workspace) == "" {
		_ = b.state.read(func(f *appStateFile) { workspace = f.workFolder() })
	}
	resolved, err := engineWorkspace(workspace)
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
	if ask.BudgetUSD != nil && *ask.BudgetUSD > 0 {
		// The budget is the app's to keep: the engine's figure is the open
		// conversation's only, and it is bound again at every open.
		usd, id := *ask.BudgetUSD, appAgentID(transcript)
		err := b.state.write(func(f *appStateFile) error {
			if f.Budgets == nil {
				f.Budgets = map[string]float64{}
			}
			f.Budgets[id] = usd
			return nil
		})
		if err != nil {
			return appwire.Agent{}, appEngineError(err)
		}
	}
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
			a.failed, a.failedKind = text, appwire.ErrorOther
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
	ask := appOpenAsk{Workspace: workspace, Transcript: transcript, ClockS: appProposedClockS}
	_ = b.state.read(func(f *appStateFile) { ask.ClockS = f.clock() })
	if boot != nil {
		ask.Model, ask.Minted = boot.model, true
	}
	conn, err := b.open(ask)
	if err != nil {
		return appwire.Agent{}, false, appEngineError(err)
	}
	// Opening an agent that was closed or put away takes it out again.
	b.unmarkTidy(view.ID)
	if boot != nil && boot.budget != nil && *boot.budget > 0 {
		if err := conn.Conv.SetSpendRail(*boot.budget); err != nil {
			conn.Release()
			return appwire.Agent{}, false, appEngineError(err)
		}
	}
	if err := b.bindBudget(view.ID, conn.Conv); err != nil {
		conn.Release()
		return appwire.Agent{}, false, appEngineError(err)
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
		consents: map[uint64]appConsentCall{}, money: map[string]appMoneyAsk{},
		phases: map[string]string{}, quicks: map[string]appwire.Quick{}, conflicts: map[string][]string{},
		processes: map[string]appwire.Process{}, logs: map[string]string{},
		said: map[string]string{}, calls: map[string]string{}, pilots: map[string]func(){},
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
			b.workEvent(id, live, event)
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
	live.stopWakes = b.watchWakes(a, live)
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
	ask.Subject = appAskSubject(agent, q.Subject)
	return ask
}

// appAskSubject is what a question is about, in the app's words: an order is
// an automation named by its id, a task node by its app task id.
func appAskSubject(agent string, s session.SubjectRef) *appwire.AskSubject {
	switch s.Kind {
	case session.SubjectOrder:
		return &appwire.AskSubject{Kind: appwire.SubjectAutomation, Ref: s.Ref}
	case session.SubjectNode:
		return &appwire.AskSubject{Kind: appwire.SubjectTask, Ref: appTaskID(agent, s.ID)}
	case session.SubjectCall:
		return &appwire.AskSubject{Kind: appwire.SubjectCall, Ref: s.CallID}
	case session.SubjectRun:
		return &appwire.AskSubject{Kind: appwire.SubjectRun, Ref: s.Ref}
	case session.SubjectPage:
		return &appwire.AskSubject{Kind: appwire.SubjectPage, Ref: s.Name}
	case session.SubjectAccount:
		return &appwire.AskSubject{Kind: appwire.SubjectAccount, Ref: s.Ref}
	}
	return nil
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
		if q.Subject.Kind == session.SubjectCall {
			b.callAsked(a, id, live, q.Subject.CallID, askID)
		}
		ask := appAskOf(id, q)
		appAskFacts(id, &ask, q)
		defer b.restateBatch(live, q)
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
		words := appReceipt(by, option, answer, at)
		// HANDED BACK, the agent's own pick answered it, and the receipt says so.
		if picked, decided := strings.CutPrefix(option, appDecideMark); decided {
			option = "you_decide"
			words = "You let it decide · it chose " + picked + " · " + at.Local().Format("15:04")
		}
		// A PROPOSAL ITS CLOCK STARTED closes with no answer from anybody; the
		// task it became, already said, is what tells it from one taken back.
		if q.Kind == session.QuestionTask && answer == nil {
			var task appwire.Task
			known := false
			b.locked(func() { task, known = live.tasks[appTaskID(id, q.ID)] })
			if known && !task.Proposed && task.State != appwire.TaskFailed {
				// THE RECEIPT SAYS HOW LONG THE CLOCK WAS, so a person reading it
				// later knows what they had to answer in.
				clock := appProposedClockS
				_ = b.state.read(func(f *appStateFile) { clock = f.clock() })
				words = "Started on its own after " + strconv.Itoa(clock) + "s · " + at.Local().Format("15:04")
			}
		}
		live.box.Event(appwire.EventAskClosed, appwire.AskClosed{Agent: id, ID: askID, By: by, Option: option, At: at})
		live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
			ID: appReceiptEntryID(askID), Agent: id, Kind: appwire.EntryReceipt, At: at, Text: words,
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
	delete(live.money, askID)
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

// held says this process holds a conversation for the agent id.
func (b *appBackend) held(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	a := b.agents[id]
	return a != nil && (a.live != nil || a.opening)
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
	// THE DAY CAP IS CHECKED BEFORE THE ENGINE HEARS OF THE TURN: a refused
	// turn sends nothing and costs nothing, and its words wait for a raise.
	now := b.now()
	if limit := b.dayCap(now); limit > 0 {
		if spent := b.spentToday(now); spent >= limit {
			return b.dayRefused(a, live, text, spent, limit), nil
		}
	}
	// The agent's budget is bound again, so a raise for today ends at midnight.
	if err := b.bindBudget(id, live.conn.Conv); err != nil {
		return 0, appEngineError(err)
	}
	var turn uint64
	var n int
	shown, attachments := text, []appwire.Attachment(nil)
	b.locked(func() {
		if live.running {
			refused = appwire.Errorf(appwire.CodeBusy, "this agent is already working on a turn")
			return
		}
		live.running, live.stopping = true, false
		live.lastUse = b.now()
		turn = b.turns.Add(1)
		live.turn, live.steps, live.steers = turn, 0, 0
		live.retries, live.retrySaid, live.retryPhase = 0, false, ""
		n = a.users
		if a.marks == nil {
			a.marks = map[int]appTurnMark{}
		}
		mark := appTurnMark{turn: turn, clientID: clientID}
		if a.sending != nil {
			mark.text, mark.attachments = a.sending.text, a.sending.attachments
		}
		a.marks[n] = mark
		shown, attachments = text, mark.attachments
		if mark.text != "" {
			shown = mark.text
		}
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
		ID: appUserEntryID(n), Agent: id, Turn: turn, Kind: appwire.EntryUser, At: b.now(), Text: shown, ClientID: clientID, Attachments: attachments,
	}})
	long := b.watchLong(id, live, turn, b.now())
	b.newTurnCalls(a)
	tails := b.tickTools(a, id, live)
	guard.Go("app/turn", func() {
		defer long.Stop()
		defer tails()
		b.pump(a, id, live, turn, n, text, events)
		b.sendQueued(a, live)
	})
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
	msg, notices, summaries := 0, 0, 0
	// A turn that answers under a message other turns already answered (a
	// woken one) starts its counts where they left off.
	b.locked(func() {
		msg, notices = live.msgBase, live.noteBase
		live.msgBase, live.noteBase = 0, 0
	})
	defer func() {
		b.locked(func() { live.lastN, live.lastMsgs, live.lastNotices = n, msg, notices })
	}()
	var reply strings.Builder
	// A MESSAGE BEFORE A TOOL CALL IS AN UPDATE, the agent saying what it
	// found mid-turn; the last one is the answer.
	// withheld says the reply's words so far could still be the withdrawal token
	// ([session.NoChangeReply]), so they are kept back from the page until
	// they cannot be: a reply that is only the token is never drawn.
	withheld := true
	sayReply := func(kind string) {
		words := strings.TrimSpace(reply.String())
		defer func() { withheld = true }()
		if session.IsNoChangeReply(words) {
			reply.Reset()
			return
		}
		if withheld && words != "" {
			live.box.Delta(id, turn, msg, appwire.DeltaText, reply.String())
		}
		if words != "" {
			b.locked(func() { live.lastAnswer = words })
			at := msg
			entry(appwire.Entry{ID: appAnswerEntryID(n, at), Kind: kind, Text: words, Msg: &at})
			msg++
		}
		reply.Reset()
	}
	// A THOUGHT IS SHOWN WHILE IT HAPPENS AND NEVER SAVED: its words go out
	// as turn.thinking, and its length once the agent moves on.
	var thoughtAt time.Time
	endThought := func() {
		if !thoughtAt.IsZero() {
			live.box.Event(appwire.EventTurnThinkingDone, appwire.TurnThinkingDone{Agent: id, Turn: turn, Seconds: appwire.ThinkingSeconds(b.now().Sub(thoughtAt))})
			thoughtAt = time.Time{}
		}
	}
	titles := map[string]string{}
	for event := range events {
		switch event.Kind {
		case session.EventTextDelta:
			endThought()
			reply.WriteString(event.Text)
			switch {
			case !withheld:
				live.box.Delta(id, turn, msg, appwire.DeltaText, event.Text)
			case !strings.HasPrefix(session.NoChangeReply, strings.TrimLeft(reply.String(), " \t\r\n")):
				withheld = false
				live.box.Delta(id, turn, msg, appwire.DeltaText, reply.String())
			}
		case session.EventReasoning:
			if thoughtAt.IsZero() {
				thoughtAt = b.now()
			}
			live.box.Delta(id, turn, msg, appwire.DeltaThinking, event.Text)
		case session.EventRetrying:
			// The attempt is void: what it streamed is dropped, here and on
			// the page, and a move to another model is said once.
			reply.Reset()
			withheld = true
			thoughtAt = time.Time{}
			// A wait already said with its countdown is not said twice; the
			// attempt is the turn's own count of them either way.
			announce := false
			retry := appwire.TurnRetrying{Agent: id, Turn: turn}
			b.locked(func() {
				announce = !live.retrySaid
				live.retrySaid = false
				if announce {
					live.retries++
				}
				retry.Attempt = max(1, live.retries)
			})
			if news := event.Retry; news != nil {
				if strings.TrimSpace(news.Next) != "" {
					footer := appFooter("f"+strconv.Itoa(n)+".m"+strconv.Itoa(retry.Attempt), id, turn, appwire.FooterFallback, redact.Secrets(strings.TrimSpace(event.Text)), b.now())
					footer.ModelFrom, footer.ModelTo = news.Model, news.Next
					live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: footer})
				}
			}
			if announce {
				live.box.Event(appwire.EventTurnRetrying, retry)
			}
		case session.EventCompacted:
			entry(appwire.Entry{ID: "c" + strconv.Itoa(n) + "." + strconv.Itoa(summaries), Kind: appwire.EntrySummary, Text: "Earlier messages summarized"})
			summaries++
		case session.EventToolBegin:
			b.locked(func() { live.steps++ })
			endThought()
			sayReply(appwire.EntryUpdate)
			title := redact.Secrets(tui3.ToolTitle(event.Tool, event.Args, event.Hint))
			titles[event.CallID] = title
			b.locked(func() { a.step = title })
			row := b.callBegan(a, id, turn, event, title)
			row.Summary = event.Hint
			live.box.Event(appwire.EventTurnTool, row)
		case session.EventToolOutput:
			b.callOutput(a, id, live, event)
		case session.EventToolFinished:
			b.callFinished(a, event)
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
			live.box.Event(appwire.EventTurnTool, b.callEnded(a, id, turn, event, title, summary, appDetail(summary), succeeded))
			// THE SEVENTEENTH PROCESS is refused by the engine, and the turn
			// says so once at its end.
			if !succeeded && strings.Contains(event.Output+summary, "processes are running for this agent.") {
				entry(appFooter("pl"+event.CallID, id, turn, appwire.FooterProcessLimit, strings.TrimSpace(summary), b.now()))
			}
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
		case session.EventTaskUpdate, session.EventTaskProposal, session.EventTaskPhase, session.EventJobUpdate:
			// A task a running task call made is that call's task.
			if task := event.Task; task != nil && (event.Kind == session.EventTaskProposal || task.State == "" || task.State == session.TaskQueued) {
				b.callTask(a, appTaskID(id, task.ID))
			}
			b.workEvent(id, live, event)
		}
	}
	endThought()
	sayReply(appwire.EntryAnswer)
	byPerson := false
	b.locked(func() {
		byPerson = live.stopping
		// A STOPPED TURN ENDS CLEANLY ON THE ENGINE'S SIDE — an interrupt is
		// not an error there — so the person's stop is what says it stopped.
		if live.stopping {
			ok, kind, status = false, appwire.ErrorStopped, 0
		}
		a.failed, a.failedKind = "", ""
		if !ok {
			a.failed, a.failedKind = text, kind
		}
	})
	switch {
	case !ok && kind == appwire.ErrorStopped:
		// A STOP IS NOT AN ANSWER AND NOT AN ERROR. It is said once, as a
		// receipt, and the turn's end says stopped; the words the turn had
		// streamed are the answer they already were.
		failure = "stopped"
		at := b.now()
		words := "Stopped · " + at.Local().Format("15:04")
		if byPerson {
			words = "Stopped by you · " + at.Local().Format("15:04")
		}
		entry(appFooter(appStopEntryID(n), id, turn, appwire.FooterStoppedByYou, words, at))
	case !ok && kind == appwire.ErrorProvider:
		footer := appFooter(appErrorEntryID(n), id, turn, appwire.FooterProviderFailed, failure, b.now())
		footer.ErrorKind, footer.Status = kind, status
		entry(footer)
	case !ok:
		entry(appwire.Entry{ID: appErrorEntryID(n), Kind: appwire.EntryError, Text: failure, ErrorKind: kind, Status: status})
	}
	live.box.Event(appwire.EventTurnDone, appwire.TurnDone{Agent: id, Turn: turn, OK: ok, Error: failure, ErrorKind: kind, Status: status})
	held := false
	var view appwire.Agent
	b.locked(func() {
		live.running = false
		live.lastUse = b.now()
		a.step = ""
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
	b.spendMoved(func(spend appwire.Spend) { b.moneyAfter(a, live, spend.TodayUSD) })
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

func appStopEntryID(n int) string { return "s" + strconv.Itoa(n) }

func appReceiptEntryID(askID string) string { return "r" + askID }

// appUserCount is how many of the person's messages a transcript holds.
func appUserCount(transcript string) int {
	users := 0
	for _, item := range appRecordItems(session.ReadTranscript(transcript)) {
		if item.Role == "user" {
			users++
		}
	}
	return users
}

// appSummaryRole marks where a compaction summarized what came before.
const appSummaryRole = "summary"

// appRecordItems is a record told once and whole: the conversation above the
// last compaction, a mark where it was summarized, and the live transcript
// beneath its floor.
func appRecordItems(record session.Record) []session.DisplayEntry {
	if len(record.Earlier) == 0 {
		return record.Entries
	}
	items := append([]session.DisplayEntry(nil), record.Earlier...)
	items = append(items, session.DisplayEntry{Role: appSummaryRole})
	floor := min(max(record.Floor, 0), len(record.Entries))
	return append(items, record.Entries[floor:]...)
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

// workEvent is one task, phase or process event, from the turn's stream or
// the task lane: both carry the same news and each change is said once.
func (b *appBackend) workEvent(id string, live *appLive, event session.Event) {
	switch event.Kind {
	case session.EventTaskUpdate, session.EventTaskProposal:
		if event.Task != nil {
			b.taskNotice(id, live, event.Task, event.Kind == session.EventTaskProposal)
		}
	case session.EventTaskPhase:
		if event.TaskPhase != nil {
			b.taskPhase(id, live, event.TaskPhase)
		}
	case session.EventJobUpdate:
		if event.Job != nil {
			b.processMoved(id, live, event.Job)
		}
	}
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
	b.locked(func() { live.closed = true })
	b.landPilots(live)
	if live.stopTasks != nil {
		live.stopTasks()
	}
	if live.stopAsks != nil {
		live.stopAsks()
	}
	if live.stopWakes != nil {
		live.stopWakes()
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
	var asks []appwire.Ask
	b.locked(func() {
		if a.live != nil {
			asks = append(asks, a.live.asks...)
		}
	})
	return append(asks, b.crewAsksOf(id)...), nil
}

// appAnsweredMark starts the value [appLive.answered] holds for an ask this app
// answered, so an answer of words alone, with no option, is still marked.
const appAnsweredMark = "app:"

// AnswerAsk answers one open ask through the engine's own answer door, the one
// the chat's question page uses. Its closing arrives on the question lane.
func (b *appBackend) AnswerAsk(id, askID, option, text string) error {
	if handled, err := b.answerOrderAsk(askID, option); handled {
		return err
	}
	if handled, err := b.answerCrewAsk(id, askID, option, text, ""); handled {
		return err
	}
	a, err := b.find(id)
	if err != nil {
		return err
	}
	if handled, err := b.answerMoney(a, askID, option, text); handled {
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
	marks := map[int]appTurnMark{}
	b.locked(func() {
		transcript, at = a.transcript, a.view.UpdatedAt
		for n, mark := range a.marks {
			marks[n] = mark
		}
	})
	if transcript == "" {
		return []appwire.Entry{}, false, nil
	}
	record := session.ReadTranscript(transcript)
	items := appRecordItems(record)
	all := appEntriesOf(id, items, at, marks)
	// A tool entry carries its call's facts, as its row did live.
	calls := map[string]session.DisplayEntry{}
	for _, item := range items {
		if item.Role == "tool" && item.CallID != "" {
			calls[item.CallID] = item
		}
	}
	for i := range all {
		if item, ok := calls[all[i].Call]; ok && all[i].Kind == appwire.EntryTool {
			all[i].ToolFacts = b.appEntryFacts(a, item)
		}
	}
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
//
// A message this process sent carries its turn number, and the person's entry
// its client id, from marks; one sent before this launch has neither.
func appEntriesOf(agent string, items []session.DisplayEntry, at time.Time, marks map[int]appTurnMark) []appwire.Entry {
	entries := make([]appwire.Entry, 0, len(items))
	n, m, k, c := -1, 0, 0, 0
	// A MESSAGE FOLLOWED BY A TOOL CALL BEFORE THE PERSON SPOKE AGAIN WAS AN
	// UPDATE, as the live pump named it.
	update := make([]bool, len(items))
	called := false
	for i := len(items) - 1; i >= 0; i-- {
		switch items[i].Role {
		case "user":
			called = false
		case "tool":
			called = true
		case "assistant":
			update[i] = called
		}
	}
	for i, item := range items {
		// Each entry carries the instant its own line was written; the
		// conversation's latest moment stands in only where the record never
		// said.
		entry := appwire.Entry{Agent: agent, At: at}
		if !item.At.IsZero() {
			entry.At = item.At
		}
		switch item.Role {
		case appSummaryRole:
			entry.ID, entry.Kind, entry.Text = "c"+strconv.Itoa(max(n, 0))+".s"+strconv.Itoa(c), appwire.EntrySummary, "Earlier messages summarized"
			c++
		case "user":
			n, m, k = n+1, 0, 0
			entry.ID, entry.Kind, entry.Text = appUserEntryID(n), appwire.EntryUser, item.Text
		case "assistant", "aside":
			if strings.TrimSpace(item.Text) == "" {
				continue
			}
			// AN ANSWER CUT SHORT BY A STOP IS STILL THE ANSWER IT WAS. The
			// journal keeps it apart from the conversation, as a line the
			// session wrote, but it is the message the person watched stream
			// under this id; read back as a notice it was that paragraph said
			// a second time. Every other aside is the session's own dim line.
			// A LINE THE SESSION WROTE TO ITS MODEL IS NOT SHOWN: it is the
			// model's instruction — a wake, a landing note, the carry-on after a
			// quit — and never words to the person. A team's delivery is the
			// exception and is the crew's line.
			if item.Role == "aside" && item.ForModel && len(item.Team) == 0 {
				continue
			}
			// A REPLY THAT IS ONLY THE WITHDRAWAL TOKEN WITHDREW ITSELF: the
			// answer before it stands, and the token is never drawn.
			if item.Role == "assistant" && session.IsNoChangeReply(item.Text) {
				continue
			}
			if item.Role == "aside" && !item.Interrupted {
				entry.ID, entry.Kind, entry.Text, entry.Quiet = appNoteEntryID(n, k), appwire.EntryNotice, item.Text, true
				// A TEAM'S DELIVERY IS ITS LINES. The words around them are the
				// session's instruction to its model, and so is any other note
				// that rode in the same message; the crew's lines are what the
				// person reads.
				if len(item.Team) > 0 {
					entry.Text = appTeamDelivery(item.Team)
				}
				k++
				break
			}
			msg := m
			entry.ID, entry.Kind, entry.Text, entry.Msg = appAnswerEntryID(n, msg), appwire.EntryAnswer, strings.TrimSpace(item.Text), &msg
			if update[i] && item.Role == "assistant" {
				entry.Kind = appwire.EntryUpdate
			}
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
		case "note":
			if strings.TrimSpace(item.Text) == "" {
				continue
			}
			entry.ID, entry.Kind, entry.Text, entry.Quiet = appNoteEntryID(n, k), appwire.EntryNotice, item.Text, true
			k++
		default:
			continue
		}
		if mark, ok := marks[n]; ok && n >= 0 {
			entry.Turn = mark.turn
			if entry.Kind == appwire.EntryUser {
				entry.ClientID = mark.clientID
				if mark.text != "" {
					entry.Text, entry.Attachments = mark.text, mark.attachments
				}
			}
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
	b.sweepOutputs()
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
// in the picker's groups and order, with the model a new conversation runs on
// marked default, and added when no list names it, so the answer is never
// empty while a turn can run.
//
// A MODEL'S PROVIDER IS THE SERVICE THE PICKER GROUPS IT UNDER: the name it was
// connected as ("local" for a custom endpoint written that way, "codex" for
// the ChatGPT sign-in, "openrouter" for the default), and provider_name the
// service's own name. Only services with a key are listed, as in the picker.
func (b *appBackend) ListModels() ([]appwire.Model, error) {
	proc, err := b.process()
	if err != nil {
		return nil, appEngineError(err)
	}
	// THE DEFAULT SERVICE'S CATALOG IS WAITED FOR EVEN WHEN ANOTHER SERVICE HAS
	// ANSWERED: a list read before it lands is every other service's models
	// and none of the default's, which reads as the default not being there.
	if proc.Models != nil {
		ctx, cancel := context.WithTimeout(context.Background(), appModelsWait)
		proc.Models.Warmed(ctx)
		cancel()
	}
	_, sources := proc.currentAccount()
	rows := tui3.PickerModels(proc.ProfileDir, sources, proc.Shelf.pickerModels, proc.Shelf.modelsForService)
	names := map[string]string{}
	for _, service := range sources.All() {
		group := strings.ToLower(strings.TrimSpace(service.Source.Written))
		if group == "" {
			group = strings.ToLower(strings.TrimSpace(service.Source.Name))
		}
		name := strings.TrimSpace(service.Source.Name)
		if modelsource.IsCustomID(service.Source.ID) || name == "" {
			name = strings.TrimSpace(service.Source.Written)
		}
		names[group] = name
	}
	chosen := appDefaultModel()
	models := make([]appwire.Model, 0, len(rows)+1)
	seen := false
	for _, row := range rows {
		provider := strings.TrimSpace(row.Group)
		if provider == "" {
			provider = modelsource.DefaultID
		}
		name := row.ID
		if row.Direct {
			name = strings.TrimPrefix(row.ID, provider+"/")
		}
		model := appwire.Model{ID: row.ID, Name: name, Provider: provider, ProviderName: names[provider], Default: row.ID == chosen}
		if model.ProviderName == "" && provider == modelsource.DefaultID {
			model.ProviderName = modelsource.DefaultSource("").Name
		}
		seen = seen || model.Default
		models = append(models, model)
	}
	if !seen && chosen != "" {
		provider := modelsource.DefaultID
		if service, bare := sources.For(chosen); bare != chosen && service.Source.Written != "" {
			provider = strings.ToLower(service.Source.Written)
		}
		models = append(models, appwire.Model{ID: chosen, Name: chosen, Provider: provider, ProviderName: names[provider], Default: true})
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
	return appwire.Spend{TodayUSD: b.spentToday(now), DayCapUSD: appFigure(b.dayCap(now))}, nil
}

// appSpendSettle is how long after a turn ends its spending is read again: the
// ledger is written behind the turn, not in front of its ending.
const appSpendSettle = 300 * time.Millisecond

// spendMoved says spend.changed once a turn's spending has reached the ledger,
// if today's figure moved, and hands today's figure to after.
func (b *appBackend) spendMoved(after func(appwire.Spend)) {
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
		if after != nil {
			after(spend)
		}
	})
}

// ListTasks reads the agent's tasks off the disk whether or not its
// conversation is held — its own checkpoint first, which holds every task it
// ever had, finished ones included, then its project's task index for any the
// checkpoint does not name — and lays over them what a held conversation has
// said since.
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
	add := func(task appwire.Task) {
		if _, dup := index[task.ID]; dup {
			return
		}
		index[task.ID] = len(tasks)
		tasks = append(tasks, task)
	}
	for _, notice := range session.ReadSessionTasks(transcript) {
		if notice.Kind == session.TaskKindQuick {
			continue
		}
		// A TASK THE CONVERSATION LEFT RUNNING WHEN IT CLOSED is interrupted:
		// nothing is driving it. The protocol has no word for that, and it is
		// not done, so it is listed as failed with the reason as its result.
		if notice.State == session.TaskInterrupted {
			notice.State = session.TaskFailed
			if strings.TrimSpace(notice.Result) == "" {
				notice.Result = "Interrupted"
			}
		}
		at := notice.EndedAt
		if at.IsZero() {
			at = notice.StartedAt
		}
		if at.IsZero() {
			at = fallback
		}
		notice := notice
		if task, ok := appTaskOf(id, &notice, false, "", at); ok {
			add(task)
		}
	}
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
			parent := ""
			if row.Parent != "" && row.Parent != row.ID {
				parent = appIndexTaskID(id, row.Parent)
			}
			add(appwire.Task{ID: appIndexTaskID(id, row.ID), Agent: id, Parent: parent, Title: title, State: state, UpdatedAt: at, YourCall: state == appwire.TaskUnverified})
		}
	}
	for taskID, task := range heard {
		if i, ok := index[taskID]; ok {
			if task.Title == "" {
				task.Title = tasks[i].Title
			}
			if task.Parent == "" {
				task.Parent = tasks[i].Parent
			}
			tasks[i] = task
			continue
		}
		tasks = append(tasks, task)
	}
	appTaskExtras(tasks)
	return tasks, nil
}

// appIndexTaskID is a task index row's id on the wire: the agent's own number
// named with the agent, as every task id is (appwork.go).
func appIndexTaskID(agent, raw string) string {
	if n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64); err == nil {
		return appTaskID(agent, n)
	}
	return raw
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
	var crews *appCrewRun
	var agents []*appAgent
	var warmed chan struct{}
	b.locked(func() {
		if already = b.closing; already {
			return
		}
		b.closing = true
		crews, warmed = b.crewRun, b.warmed
		if b.signIn != nil {
			b.signIn.Cancel()
			b.signIn = nil
		}
		for id, f := range b.follows {
			close(f.stop)
			delete(b.follows, id)
		}
		for _, a := range b.agents {
			agents = append(agents, a)
		}
	})
	if already {
		return nil
	}
	// What was mid-reply and what was running is written down before anything
	// stops, so the next start can carry on (appquit.go).
	b.markQuit(agents)
	close(b.sweepStop)
	<-b.sweepDone
	if b.stopPhases != nil {
		b.stopPhases()
	}
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
	// A member of an open crew stays held while the crew is open.
	if !keep && !b.liveNeedsYou(live) && !b.crewKeeps(a.view.ID) {
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

// appTeamDelivery is a team delivery's lines as the person reads them: who
// said each, and what.
func appTeamDelivery(lines []session.TeamLine) string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		who := line.From
		switch who {
		case "you", "manager":
		case "system":
			who = "codeaf"
		default:
			who = "@" + who
		}
		out = append(out, who+": "+strings.TrimSpace(line.Text))
	}
	return redact.Secrets(strings.Join(out, "\n"))
}
