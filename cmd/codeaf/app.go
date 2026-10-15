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
// its conversation's own workspace rather than the process's directory, and the
// launch directory a new conversation records is where the app was started.
//
// AND IT IS LAZY BY LAW. Starting up reads nothing but the flags, so the hello
// line leaves before anything is opened; listing agents, tasks and crews reads
// the disk; only agents.create, agents.open and agents.send build a
// conversation, and one left alone for [appAgentIdle] is let go again and
// rebuilt transparently by the next send.

import (
	"context"
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
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
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
}

// appOpenAsk is which conversation to open: a saved one by transcript, or a
// new one in a workspace.
type appOpenAsk struct {
	Workspace  string
	Transcript string
	Model      string
	New        bool
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
	if ask.Transcript != "" {
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
		Launch:    &remote.LaunchShape{Interactive: true},
	}
	loop, err := remote.Loopback(hello, remote.Options{
		Boot: func(h remote.Hello) (*remote.Engine, error) {
			return assembleEngine(h, workspace, transcript)
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
	tasks     map[string]appwire.Task
}

func newAppBackend(emit appwire.Emitter, open appOpener) *appBackend {
	b := &appBackend{
		emit:      emit,
		open:      open,
		now:       time.Now,
		agents:    map[string]*appAgent{},
		sweepStop: make(chan struct{}),
		sweepDone: make(chan struct{}),
	}
	go b.sweep()
	return b
}

// appAgentID is a conversation's id from its transcript: the folder's name.
func appAgentID(transcript string) string {
	return filepath.Base(filepath.Dir(filepath.Clean(transcript)))
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
	b.mu.Lock()
	a := b.agents[id]
	b.mu.Unlock()
	if a != nil && a.transcript != "" {
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
	b.mu.Lock()
	for id, a := range b.agents {
		if !seen[id] && a.live != nil && matches(a.view.Workspace) {
			agents = append(agents, a.view)
		}
	}
	b.mu.Unlock()
	sort.SliceStable(agents, func(i, j int) bool { return agents[i].UpdatedAt.After(agents[j].UpdatedAt) })
	return agents, nil
}

// CreateAgent opens a new conversation in workspace.
func (b *appBackend) CreateAgent(workspace, model string) (appwire.Agent, error) {
	resolved, err := engineWorkspace(workspace)
	if err != nil {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
	}
	if b.isClosing() {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	conn, err := b.open(appOpenAsk{Workspace: resolved, Model: strings.TrimSpace(model), New: true})
	if err != nil {
		return appwire.Agent{}, appEngineError(err)
	}
	id := appAgentID(conn.Transcript)
	b.mu.Lock()
	a := b.agentLocked(id)
	b.mu.Unlock()
	a.gate.Lock()
	defer a.gate.Unlock()
	view := b.hold(a, conn, resolved)
	b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	return view, nil
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
	b.mu.Lock()
	if a.live != nil {
		view := a.view
		b.mu.Unlock()
		return view, false, nil
	}
	transcript, workspace := a.transcript, a.view.Workspace
	b.mu.Unlock()
	if b.isClosing() {
		return appwire.Agent{}, false, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	conn, err := b.open(appOpenAsk{Workspace: workspace, Transcript: transcript})
	if err != nil {
		return appwire.Agent{}, false, appEngineError(err)
	}
	return b.hold(a, conn, workspace), true, nil
}

// hold makes conn the agent's live conversation. Callers hold a.gate.
func (b *appBackend) hold(a *appAgent, conn *appConn, workspace string) appwire.Agent {
	if conn.Assembled {
		b.assembled.Store(true)
	}
	live := &appLive{conn: conn, box: appwire.NewOutbox(b.emit), lastUse: b.now(), tasks: map[string]appwire.Task{}, lanesDone: make(chan struct{})}
	b.mu.Lock()
	if conn.Transcript != "" {
		a.transcript = conn.Transcript
	}
	if conn.Workspace != "" {
		workspace = conn.Workspace
	}
	a.view.Workspace = workspace
	if title := conn.Conv.Title(); title != "" {
		a.view.Title = title
	}
	if model := conn.Conv.Model(); model != "" {
		a.view.Model = model
	}
	a.view.State = appwire.StateIdle
	if conn.Conv.NeedsPerson() {
		a.view.State = appwire.StateNeedsYou
	}
	if a.view.UpdatedAt.IsZero() {
		a.view.UpdatedAt = b.now()
	}
	a.live = live
	view := a.view
	b.mu.Unlock()
	lane, stop := conn.Conv.WatchTaskUpdates()
	live.stopTasks = stop
	id := view.ID
	go func() {
		defer close(live.lanesDone)
		for event := range lane {
			if event.Kind == session.EventTaskUpdate && event.Task != nil {
				b.taskNotice(id, live, event.Task)
			}
		}
	}()
	return view
}

// live answers the agent's held conversation, or nil.
func (b *appBackend) liveOf(a *appAgent) *appLive {
	b.mu.Lock()
	defer b.mu.Unlock()
	return a.live
}

// Send starts one turn, opening the conversation first if it is not held.
func (b *appBackend) Send(id, text string) (uint64, error) {
	a, err := b.find(id)
	if err != nil {
		return 0, err
	}
	// The gate keeps the idle sweep from letting the conversation go between
	// opening it and starting the turn.
	a.gate.Lock()
	view, opened, err := b.ensureLocked(a)
	if err != nil {
		a.gate.Unlock()
		return 0, err
	}
	if opened {
		b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	b.mu.Lock()
	live := a.live
	if live.running {
		b.mu.Unlock()
		a.gate.Unlock()
		return 0, appwire.Errorf(appwire.CodeBusy, "this agent is already working on a turn")
	}
	live.running = true
	live.lastUse = b.now()
	turn := b.turns.Add(1)
	b.mu.Unlock()
	events, err := live.conn.Conv.Submit(context.Background(), text)
	if err != nil {
		b.mu.Lock()
		live.running = false
		b.mu.Unlock()
		a.gate.Unlock()
		return 0, appEngineError(err)
	}
	b.mu.Lock()
	a.view.State = appwire.StateWorking
	a.view.UpdatedAt = b.now()
	view = a.view
	live.pumpDone = make(chan struct{})
	b.mu.Unlock()
	live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	live.box.Event(appwire.EventTurnStarted, appwire.TurnStarted{Agent: id, Turn: turn})
	go b.pump(a, id, live, turn, events)
	a.gate.Unlock()
	return turn, nil
}

// pump maps one turn's session events onto the wire. It never waits on the
// writer: everything goes into the agent's outbox.
func (b *appBackend) pump(a *appAgent, id string, live *appLive, turn uint64, events <-chan session.Event) {
	defer close(live.pumpDone)
	ok, failure := true, ""
	changed := func(mutate func(*appwire.Agent)) {
		b.mu.Lock()
		mutate(&a.view)
		view := a.view
		b.mu.Unlock()
		live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	for event := range events {
		switch event.Kind {
		case session.EventTextDelta:
			live.box.Delta(id, turn, appwire.DeltaText, event.Text)
		case session.EventReasoning:
			live.box.Delta(id, turn, appwire.DeltaThinking, event.Text)
		case session.EventToolBegin:
			live.box.Event(appwire.EventTurnTool, appwire.TurnTool{Agent: id, Turn: turn, Call: event.CallID, Name: event.Tool, Phase: appwire.PhaseBegin, Summary: event.Hint})
		case session.EventToolEnd, session.EventToolFailed:
			succeeded := event.Kind == session.EventToolEnd
			summary := event.Hint
			if summary == "" && !succeeded {
				summary = event.Text
			}
			live.box.Event(appwire.EventTurnTool, appwire.TurnTool{Agent: id, Turn: turn, Call: event.CallID, Name: event.Tool, Phase: appwire.PhaseEnd, OK: &succeeded, Summary: redact.Secrets(summary)})
		case session.EventError:
			ok, failure = false, "the turn ended with an error"
			if event.Err != nil {
				failure = redact.Secrets(event.Err.Error())
			}
		case session.EventTurnDone:
			// A turn the person steered ends once per stretch; the last ending
			// on the stream is the turn's.
			ok, failure = true, ""
		case session.EventConsentRequest, session.EventQuestion:
			changed(func(v *appwire.Agent) { v.State = appwire.StateNeedsYou })
		case session.EventQuestionAnswered, session.EventQuestionWithdrawn:
			changed(func(v *appwire.Agent) { v.State = appwire.StateWorking })
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
	live.box.Event(appwire.EventTurnDone, appwire.TurnDone{Agent: id, Turn: turn, OK: ok, Error: failure})
	b.mu.Lock()
	live.running = false
	live.lastUse = b.now()
	if a.live != live {
		// Let go or closed while the turn ran: that road has said the state.
		b.mu.Unlock()
		return
	}
	a.view.State = appwire.StateIdle
	if live.conn.Conv.NeedsPerson() {
		a.view.State = appwire.StateNeedsYou
	}
	a.view.UpdatedAt = b.now()
	view := a.view
	b.mu.Unlock()
	live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
}

// appTaskState is a task's state in the protocol's five words. The engine's
// interrupted (nothing is driving it, every step is kept) has no word of its
// own on this wire and reads as failed; a state this build has never heard of
// answers "" and the row is left out rather than given a word it did not say.
func appTaskState(state string) string {
	switch session.TaskState(strings.TrimSpace(state)) {
	case session.TaskQueued:
		return appwire.TaskQueued
	case session.TaskRunning:
		return appwire.TaskRunning
	case session.TaskDone:
		return appwire.TaskDone
	case session.TaskFailed, session.TaskInterrupted:
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
	b.mu.Lock()
	if before, ok := live.tasks[task.ID]; ok && before.State == task.State && (task.Title == "" || before.Title == task.Title) {
		b.mu.Unlock()
		return
	}
	if task.Title == "" {
		task.Title = live.tasks[task.ID].Title
	}
	live.tasks[task.ID] = task
	b.mu.Unlock()
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
	a.gate.Lock()
	b.release(a)
	b.mu.Lock()
	a.view.State = appwire.StateClosed
	view := a.view
	b.mu.Unlock()
	a.gate.Unlock()
	b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	return nil
}

// release gives a's conversation back and flushes its last events. Callers
// hold a.gate.
func (b *appBackend) release(a *appAgent) {
	b.mu.Lock()
	live := a.live
	a.live = nil
	b.mu.Unlock()
	if live == nil {
		return
	}
	if live.stopTasks != nil {
		live.stopTasks()
	}
	live.conn.Release()
	wait := time.After(appReleaseWait)
	if live.pumpDone != nil {
		select {
		case <-live.pumpDone:
		case <-wait:
		}
	}
	select {
	case <-live.lanesDone:
	case <-wait:
	}
	live.box.Close()
}

// ListTasks reads the agent's tasks off its project's task index, and lays
// over them what the held conversation has said since.
func (b *appBackend) ListTasks(id string) ([]appwire.Task, error) {
	a, err := b.find(id)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	transcript := a.transcript
	fallback := a.view.UpdatedAt
	var heard map[string]appwire.Task
	if a.live != nil {
		heard = make(map[string]appwire.Task, len(a.live.tasks))
		for k, v := range a.live.tasks {
			heard[k] = v
		}
	}
	b.mu.Unlock()
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
	b.mu.Lock()
	if b.crews == nil && !b.closing {
		b.crews = newAppCrewWatch(b.emit, crews)
	}
	b.mu.Unlock()
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
	b.mu.Lock()
	if b.closing {
		b.mu.Unlock()
		return nil
	}
	b.closing = true
	crews := b.crews
	agents := make([]*appAgent, 0, len(b.agents))
	for _, a := range b.agents {
		agents = append(agents, a)
	}
	b.mu.Unlock()
	close(b.sweepStop)
	<-b.sweepDone
	if crews != nil {
		crews.close()
	}
	var wg sync.WaitGroup
	for _, a := range agents {
		if b.liveOf(a) == nil {
			continue
		}
		wg.Add(1)
		go func(a *appAgent) {
			defer wg.Done()
			// Stop first: a turn still running is interrupted rather than left
			// holding the gate while the process leaves.
			// A conversation joined on a window's host is that window's, and
			// is only detached from.
			if live := b.liveOf(a); live != nil && live.conn.Assembled {
				live.conn.Conv.Interrupt()
			}
			a.gate.Lock()
			b.release(a)
			a.gate.Unlock()
		}(a)
	}
	wg.Wait()
	return nil
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
	b.mu.Lock()
	var idle []*appAgent
	for _, a := range b.agents {
		if live := a.live; live != nil && !live.running && now.Sub(live.lastUse) >= appAgentIdle {
			idle = append(idle, a)
		}
	}
	b.mu.Unlock()
	for _, a := range idle {
		if !a.gate.TryLock() {
			continue
		}
		b.mu.Lock()
		live := a.live
		keep := live == nil || live.running || now.Sub(live.lastUse) < appAgentIdle
		b.mu.Unlock()
		if !keep && !live.conn.Conv.NeedsPerson() {
			// The state the app reads does not move: the conversation is as
			// idle on disk as it was in memory.
			b.release(a)
		}
		a.gate.Unlock()
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
	go w.run()
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
		w.known = prints
	}
}

func (w *appCrewWatch) close() {
	close(w.stop)
	<-w.done
}
