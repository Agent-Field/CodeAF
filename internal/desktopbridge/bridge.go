// Package desktopbridge adapts the canonical remote session protocol to a local
// desktop renderer. It owns no prompts, model loop, task store or chat history.
package desktopbridge

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workspacestore"
)

const Model = "deepseek/deepseek-v4.1-flash"
const MaxReplay = 2048

type Agent interface {
	Submit(context.Context, string) (<-chan session.Event, error)
	Steer(string) (<-chan session.Event, error)
	FollowUp(string) (<-chan session.Event, error)
	StopWork() error
	Model() string
	NeedsPerson() bool
	Title() string
	Transcript() []session.DisplayEntry
	ReadPlanTaskPage(string) (session.PlanTaskPage, bool, error)
	PlanTasks() []session.PlanTaskRow
	Usage() session.Usage
	AttachReplay() ([]session.DisplayEntry, <-chan session.Event, func())
}
type Connection struct {
	Follow    <-chan remote.Following
	Take      func() error
	Agent     Agent
	Welcome   remote.Welcome
	FetchFile func(string) (remote.FetchedFile, error)
	StatPaths func([]string) ([]remote.PathFact, error)
	// ListDir is one folder of the engine's disk. Nil means this engine cannot
	// list folders (fileslist.go). A relative path is the workspace's.
	ListDir func(string) (remote.DirListing, error)
	// The file and diff tabs' doors (workview.go). Nil means the engine cannot.
	ReadText    func(string) (remote.TextFile, error)
	FindFiles   func(string, int) (remote.FoundFiles, error)
	DiffChanges func([]string) (remote.ChangedFiles, error)
	DiffFile    func(string) (remote.FileDiff, error)
	// DiffStart records the commit the conversation starts on (once); best effort.
	DiffStart func() (remote.DiffStart, error)
	// Local is true when the engine runs on this machine's disk (a child the
	// bridge started). Only then may the app hand a path to a local editor.
	Local bool
	Close func()
}
type Open func(sessionFile string) (Connection, error)

type Snapshot struct {
	ID          string             `json:"id"`
	SessionFile string             `json:"sessionFile"`
	Workspace   string             `json:"workspace"`
	Model       string             `json:"model"`
	Persistent  bool               `json:"persistent"`
	Running     bool               `json:"running"`
	NeedsPerson bool               `json:"needsPerson"`
	Questions   []session.Question `json:"questions"`
	// RecentOutcomes is how the last few questions ended, newest first, so the
	// window can draw a receipt under a question that is no longer on the list.
	RecentOutcomes []OutcomeWire          `json:"recentOutcomes,omitempty"`
	PlanError      string                 `json:"planError,omitempty"`
	Title          string                 `json:"title"`
	Entries        []session.DisplayEntry `json:"entries"`
	Tasks          []session.PlanTaskRow  `json:"tasks"`
	Usage          session.Usage          `json:"usage"`
	// Queue is the messages waiting behind the running turn, in the order they
	// will run. A message leaves it the moment its turn starts.
	Queue     []QueuedWire `json:"queue"`
	UpdatedAt string       `json:"updatedAt,omitempty"`
	Seq       uint64       `json:"seq"`
	// WorkingFolder says where a chat started in a place works, when the place had folders to say it about.
	WorkingFolder *WorkingFolder `json:"workingFolder,omitempty"`
}

// OutcomeWire is one ended question as the window reads it. It carries the
// person-facing words only, so the window never has to rebuild a sentence.
type OutcomeWire struct {
	Kind    string `json:"kind"`
	Token   string `json:"token"`
	Head    string `json:"head"`
	Outcome string `json:"outcome"`
	Words   string `json:"words"`
	By      string `json:"by"`
	At      string `json:"at"`
	// ElapsedSeconds carries the recorded wait rather than an assumed thirty seconds.
	ElapsedSeconds float64 `json:"elapsedSeconds,omitempty"`
	// CallID is the call the question was about, so a reloaded window can put the
	// receipt back in the turn that asked. Old engines omit it.
	CallID string `json:"callId,omitempty"`
}

// recentOutcomes reads the session's receipts when the engine keeps them. An
// engine without them yields nothing, so the field is left out of the snapshot.
func recentOutcomes(a any) []OutcomeWire {
	door, ok := a.(interface {
		RecentQuestionOutcomes(int) []session.QuestionOutcome
	})
	if !ok {
		return nil
	}
	var wire []OutcomeWire
	for _, o := range door.RecentQuestionOutcomes(20) {
		wire = append(wire, OutcomeWire{Kind: string(o.Kind), Token: o.Token, Head: o.Head, Outcome: o.Outcome, Words: o.Words, By: o.By, At: o.At.UTC().Format(time.RFC3339), ElapsedSeconds: o.ElapsedSeconds, CallID: o.CallID})
	}
	return wire
}

type Event struct {
	Kind  string           `json:"kind"`
	Text  string           `json:"text"`
	Tool  string           `json:"tool"`
	Hint  string           `json:"hint"`
	Error string           `json:"error,omitempty"`
	Raw   remote.EventWire `json:"raw"`
	// PlaceChange rides a `placeChange` event only (placechange.go).
	PlaceChange *PlaceChange `json:"placeChange,omitempty"`
}
type Record struct {
	Seq      uint64    `json:"seq"`
	Type     string    `json:"type"`
	Event    *Event    `json:"event,omitempty"`
	Snapshot *Snapshot `json:"snapshot,omitempty"`
}
type conversation struct {
	conn       Connection
	id         string
	mu         sync.Mutex
	running    bool
	generation uint64
	primary    <-chan session.Event
	updatedAt  time.Time
	seq        uint64
	records    []Record
	changed    chan struct{}
	done       chan struct{}
	observers  int
	lastTasks  []session.PlanTaskRow
	queue      []*queuedItem
	queueSeq   uint64
	planError  string
	icons      *faviconCache
	terms      *terminalSet
	// folder is where a new place chat was told it works (workingfolder.go); nil says nothing.
	folder *WorkingFolder
	// afterTurn is told each primary stream that ended, and whether the
	// engine said its turn was done (places_advice.go); nil is nobody
	// listening.
	afterTurn func(s *conversation, settled bool)
	// jobsWorld publishes a jobs roll-up onto the engine-wide feed (jobs.go).
	// Nil means this conversation has nobody to tell.
	jobsWorld func(kind string, payload any)
	// bridge is the table that opened this conversation. publish tells its world
	// rows from here, and detach is a bridge method the session route table calls.
	bridge   *Bridge
	jobsMu   sync.Mutex
	jobsSeen map[int]session.JobNotice
	jobsSig  string
}
type Bridge struct {
	token     string
	open      Open
	mu        sync.Mutex
	sessions  map[string]*conversation
	closeOnce sync.Once
	icons     *faviconCache
	models    *Models
	places    *Places
	history   *History
	// workspaces holds each window place's tab set (workspaces.go).
	workspaces    *workspacestore.Store
	openElsewhere *workspaceOpenIndex
	// world is the engine-wide feed (worldstream.go); nil until first used.
	world *WorldFeed
	// worldRows is the per-conversation rows producer (worldrows.go); nil until set.
	worldRows *worldRows
	// advice schedules place offers (places_advice.go); nil makes none.
	advice *PlaceAdvice
	// openIn opens a new chat in another folder (workingfolder.go); nil keeps every chat in the bridge's workspace.
	openIn OpenIn
	// groups answers tab-group offers (tabgroups.go); nil makes none.
	groups *TabGroups
	// lifecycle tracks attached views separately from SSE connections (lifecycle.go).
	lifecycle *sessionLifecycle
}

func New(token string, open Open) *Bridge {
	b := &Bridge{token: token, open: open, sessions: map[string]*conversation{}, icons: newFaviconCache()}
	b.reaper()
	return b
}
func Token() (string, error) {
	var b [32]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}
func (b *Bridge) Close() {
	b.closeOnce.Do(func() {
		// The idle reaper takes the bridge lock to release a child. Stop it
		// before shutdown takes that lock, so the two cannot close one engine.
		b.stopReaper()
		// The offers' worker takes the bridge lock to find a conversation, so
		// it is stopped before that lock is held.
		b.mu.Lock()
		advice := b.advice
		b.mu.Unlock()
		if advice != nil {
			advice.close()
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.world != nil {
			b.world.Close()
		}
		if b.places != nil && b.places.sweepStop != nil {
			close(b.places.sweepStop)
		}
		for _, s := range b.sessions {
			close(s.done)
			s.terminals().closeAll()
			s.conn.Close()
		}
	})
}

// The remote client already tells every view about turns opened elsewhere,
// including automatic task wake-ups. Covered replay tails must be drained once.
func (s *conversation) follow(lane <-chan remote.Following) {
	for {
		select {
		case <-s.done:
			return
		case turn, ok := <-lane:
			if !ok {
				return
			}
			if turn.Covered != nil && turn.Covered() {
				go func() {
					for range turn.Events {
					}
				}()
				continue
			}
			s.mu.Lock()
			running := s.running
			if !running {
				s.running = true
			}
			s.mu.Unlock()
			if running {
				go func() {
					for range turn.Events {
					}
				}()
				continue
			}
			snapshot := s.snapshot()
			s.publish(Record{Type: "snapshot", Snapshot: &snapshot})
			go s.pump(turn.Events, func() {})
		}
	}
}
func (s *conversation) lane(kind string, events <-chan session.Event, stop func()) {
	defer stop()
	for {
		select {
		case <-s.done:
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			raw := remote.WireEvent(ev)
			s.publish(Record{Type: "event", Event: &Event{Kind: kind, Text: ev.Text, Tool: ev.Tool, Hint: ev.Hint, Error: raw.Err, Raw: raw}})
			snapshot := s.snapshot()
			s.publish(Record{Type: "snapshot", Snapshot: &snapshot})
		}
	}
}

const planReadInterval = 3 * time.Second

func (s *conversation) planRows() ([]session.PlanTaskRow, string) {
	var rows []session.PlanTaskRow
	var err error
	if door, ok := s.conn.Agent.(interface {
		ReadPlanTasks() ([]session.PlanTaskRow, error)
	}); ok {
		rows, err = door.ReadPlanTasks()
	} else {
		rows = s.conn.Agent.PlanTasks()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.planError = err.Error()
		return append([]session.PlanTaskRow(nil), s.lastTasks...), s.planError
	}
	s.lastTasks = append([]session.PlanTaskRow(nil), rows...)
	s.planError = ""
	return rows, ""
}

// A worker writes plandb through its own CLI, outside TaskGraph's notice lane.
// The terminal periodically reads these rows too; refresh only while a desktop
// view is subscribed, and publish only actual changes. This makes no AI calls.
func (s *conversation) watchPlanReads() {
	timer := time.NewTicker(planReadInterval)
	defer timer.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-timer.C:
			s.refreshPlanRead()
		}
	}
}
func (s *conversation) refreshPlanRead() {
	s.mu.Lock()
	watching := s.observers > 0
	before := append([]session.PlanTaskRow(nil), s.lastTasks...)
	beforeError := s.planError
	s.mu.Unlock()
	if !watching {
		return
	}
	rows, err := s.planRows()
	if !reflect.DeepEqual(before, rows) || beforeError != err {
		snapshot := s.snapshot()
		s.publish(Record{Type: "snapshot", Snapshot: &snapshot})
	}
}

func (s *conversation) snapshot() Snapshot {
	s.mu.Lock()
	running, seq, stamp := s.running, s.seq, s.updatedAt
	s.mu.Unlock()
	a := s.conn.Agent
	entries := a.Transcript()
	tasks, planError := s.planRows()
	questions := []session.Question{}
	if door, ok := a.(interface{ OpenQuestions() []session.Question }); ok {
		if open := door.OpenQuestions(); open != nil {
			questions = open
		}
	}
	if entries == nil {
		entries = []session.DisplayEntry{}
	}
	if tasks == nil {
		tasks = []session.PlanTaskRow{}
	}
	w := s.conn.Welcome
	if info, err := os.Stat(w.SessionFile); err == nil && info.ModTime().After(stamp) {
		stamp = info.ModTime()
	}
	var updatedAt string
	if !stamp.IsZero() {
		updatedAt = stamp.UTC().Format(time.RFC3339)
	}
	return Snapshot{ID: s.id, SessionFile: w.SessionFile, Workspace: w.Workspace, Model: a.Model(), Persistent: w.Persistent, Running: running, NeedsPerson: a.NeedsPerson(), Title: a.Title(), Questions: questions, RecentOutcomes: recentOutcomes(a), PlanError: planError, Entries: entries, Tasks: tasks, Usage: a.Usage(), Queue: s.queueWire(), Seq: seq, UpdatedAt: updatedAt, WorkingFolder: s.folder}
}
func (s *conversation) publish(r Record) {
	// A jobUpdate is also the window's jobs roll-up. This runs before the
	// conversation lock because it may publish on the world feed, and that
	// feed takes the bridge lock: the two locks must not be taken in both orders.
	if r.Event != nil && r.Event.Raw.Job != nil {
		s.noteJob(*r.Event.Raw.Job)
	}
	s.mu.Lock()
	s.seq++

	r.Seq = s.seq
	if r.Snapshot != nil {
		r.Snapshot.Seq = r.Seq
	}
	s.records = append(s.records, r)
	if len(s.records) > MaxReplay {
		s.records = s.records[len(s.records)-MaxReplay:]
	}
	close(s.changed)
	s.changed = make(chan struct{})
	bridge := s.bridge
	s.mu.Unlock()
	// Running, pending questions and the title move with the records a window
	// already sees. worldChanged takes this conversation's lock, so the call
	// waits until that lock is down. With no rows producer it returns at once.
	if bridge != nil {
		bridge.worldChanged(s)
	}
}
func (s *conversation) pump(events <-chan session.Event, stop func()) {
	s.mu.Lock()
	s.generation++
	generation := s.generation
	s.primary = events
	s.mu.Unlock()
	defer stop()
	settled := false
	for ev := range events {
		settled = settled || ev.Kind == session.EventTurnDone
		if !ev.ReplayObserved {
			s.mu.Lock()
			s.updatedAt = time.Now()
			s.mu.Unlock()
		}
		raw := remote.WireEvent(ev)
		s.publish(Record{Type: "event", Event: &Event{Kind: eventKind(ev.Kind), Text: ev.Text, Tool: ev.Tool, Hint: ev.Hint, Error: raw.Err, Raw: raw}})
	}
	s.mu.Lock()
	if s.generation == generation {
		s.running = false
		s.primary = nil
	}
	s.mu.Unlock()
	snapshot := s.snapshot()
	s.publish(Record{Type: "snapshot", Snapshot: &snapshot})
	// A turn the engine said was done is the one moment a chat is weighed for
	// a place. A stream that merely ended (a stop, a lost engine) is not.
	if s.afterTurn != nil {
		s.afterTurn(s, settled)
	}
}

// FollowUp is its own future turn, not a second reader of the current turn.
func (s *conversation) queued(events <-chan session.Event) {
	first, ok := <-events
	s.forget(events)
	if !ok {
		// The engine dropped it (a stop, or a take-back): the queue changed.
		snapshot := s.snapshot()
		s.publish(Record{Type: "snapshot", Snapshot: &snapshot})
		return
	}
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()
	snapshot := s.snapshot()
	s.publish(Record{Type: "snapshot", Snapshot: &snapshot})
	tail := make(chan session.Event)
	go func() {
		defer close(tail)
		tail <- first
		for ev := range events {
			tail <- ev
		}
	}()
	s.pump(tail, func() {})
}

// A steer's own stream is the same turn the primary stream already publishes,
// so it is drained unpublished until the steer falls through. From that event
// on the stream carries the follow-up turn its words started, which nobody else
// publishes, so it is pumped like any queued turn.
func (s *conversation) steered(events <-chan session.Event) {
	for ev := range events {
		if ev.Kind == session.EventSteerFellThrough {
			s.queued(events)
			return
		}
	}
}
func eventKind(k session.EventKind) string {
	switch k {
	case session.EventTextDelta:
		return "text"
	case session.EventThinking:
		return "thinking"
	case session.EventReasoning:
		return "reasoning"
	case session.EventToolBegin:
		return "toolBegin"
	case session.EventToolEnd:
		return "toolEnd"
	case session.EventToolFailed:
		return "toolFailed"
	case session.EventTurnDone:
		return "turnDone"
	case session.EventError:
		return "error"
	case session.EventAssistantDone:
		return "assistantDone"
	case session.EventConsentRequest:
		return "consent"
	case session.EventQuestion:
		return "question"
	default:
		return laterKind(k)
	}
}

// laterKind names the rest of the kinds the renderer reads by name; anything
// else stays "other" with the raw kind number on the wire event.
func laterKind(k session.EventKind) string {
	switch k {
	case session.EventCaption:
		return "caption"
	case session.EventSteerAccepted:
		return "steerAccepted"
	case session.EventSteerConsumed:
		return "steerConsumed"
	case session.EventSteerFellThrough:
		return "steerFellThrough"
	case session.EventToolAnnounced:
		return "toolAnnounced"
	case session.EventToolForming:
		return "toolForming"
	case session.EventToolFinished:
		return "toolFinished"
	case session.EventToolOutput:
		return "toolOutput"
	case session.EventRetrying:
		return "retrying"
	case session.EventNotice:
		return "notice"
	case session.EventCompacting:
		return "compacting"
	case session.EventCompacted:
		return "compacted"
	case session.EventTaskProposal:
		return "taskProposal"
	case session.EventTaskUpdate:
		return "taskUpdate"
	case session.EventTaskPhase:
		return "taskPhase"
	case session.EventJobUpdate:
		return "jobUpdate"
	case session.EventQuestionWithdrawn:
		return "questionWithdrawn"
	case session.EventQuestionAnswered:
		return "questionAnswered"
	case session.EventTitleChanged:
		return "titleChanged"
	default:
		return "other"
	}
}
func (b *Bridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// A foreign page is refused before the token check, so it learns nothing about the engine.
	if status, msg := guardRequest(r); status != 0 {
		fail(w, status, msg)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	origin := r.Header.Get("Origin")
	native := origin == "tauri://localhost" || origin == "http://tauri.localhost" || origin == "http://localhost:1420" || origin == "http://127.0.0.1:1420"
	if native {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, If-Match")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
	}
	if r.Method == http.MethodOptions && native {
		w.WriteHeader(204)
		return
	}
	if b.token == "" || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")), []byte(b.token)) != 1 {
		fail(w, http.StatusUnauthorized, "engine connection required")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/engine")
	if path == "/health" && r.Method == http.MethodGet {
		write(w, map[string]any{"status": "ready", "model": Model})
		return
	}
	if path == "/roots" {
		b.roots(w, r)
		return
	}
	if b.modelRoutes(w, r, path) {
		return
	}
	if b.placesPolicyRoutes(w, r, path) {
		return
	}
	if b.placesRoutes(w, r, path) {
		return
	}
	if b.historyRoutes(w, r, path) {
		return
	}
	if b.settingsRoutes(w, r, path) {
		return
	}
	if b.worldRoutes(w, r, path) {
		return
	}
	if b.workspaceRoutes(w, r, path) {
		return
	}
	if path == "/favicon" {
		b.webFavicon(w, r)
		return
	}
	if path == "/sessions" && r.Method == http.MethodPost {
		// PlaceID is the place a NEW conversation is started in (using.go, openrequest.go):
		// checked before the engine is opened, filed before the first turn.
		var ask OpenRequest
		if !decode(w, r, &ask) {
			return
		}
		var places *Places
		if ask.PlaceID != "" {
			if ask.SessionFile != "" {
				failPlaces(w, 400, "invalid", "Only a new chat is started in a place; file an existing one from its place.")
				return
			}
			var status int
			var code, sentence string
			if places, status, code, sentence = b.newChatPlace(ask.PlaceID); status != 0 {
				failPlaces(w, status, code, sentence)
				return
			}
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		for _, s := range b.sessions {
			if ask.SessionFile != "" && s.conn.Welcome.SessionFile == ask.SessionFile {
				write(w, s.snapshot())
				return
			}
		}
		var folder *WorkingFolder
		var conn Connection
		var err error
		// A new place chat takes the place's first usable folder. A saved
		// conversation takes the folder its own record names. Anything else,
		// including a record that no longer qualifies, stays on the launch host.
		dir := ""
		switch {
		case places != nil:
			dir, folder = b.folderFor(places, ask.PlaceID)
		case b.openIn != nil:
			dir = recordedFolder(home.Dir(), ask.SessionFile, b.folderPolicy())
		}
		if dir != "" {
			conn, err = b.openIn(dir, ask.SessionFile)
			if err == nil && !sameFolder(dir, conn.Welcome.Workspace) {
				if conn.Close != nil {
					conn.Close()
				}
				fail(w, 409, "The engine opened this chat in "+conn.Welcome.Workspace+" instead of "+dir+".")
				return
			}
		} else {
			conn, err = b.open(ask.SessionFile)
		}
		if err != nil {
			fail(w, 502, err.Error())
			return
		}
		if strings.TrimSpace(conn.Welcome.Model) == "" || conn.Welcome.Launch == nil || !conn.Welcome.Launch.OneModel || !conn.Welcome.Persistent {
			conn.Close()
			fail(w, 409, "this conversation must use the single-model persistent engine; open a new conversation")
			return
		}
		if places != nil {
			if err := places.fileNewChat(conn, ask.PlaceID); err != nil {
				conn.Close()
				status, code, sentence := storeFailure(err, "")
				failPlaces(w, status, code, sentence)
				return
			}
			places.referRestOfPlace(conn, folder)
		}
		id, err := Token()
		if err != nil {
			conn.Close()
			fail(w, 500, "cannot create session identity")
			return
		}
		if conn.DiffStart != nil {
			_, _ = conn.DiffStart() // diffs compare against where this conversation began; best effort
		}
		s := &conversation{conn: conn, id: id, changed: make(chan struct{}), done: make(chan struct{}), icons: b.icons, folder: folder, jobsWorld: b.recordWorld, bridge: b}
		b.sessions[id] = s
		s.afterTurn = b.adviseAfterTurn
		_, events, stop := conn.Agent.AttachReplay()
		s.running = events != nil
		if events != nil {
			go s.pump(events, stop)
		} else {
			stop()
		}
		if conn.Follow != nil {
			go s.follow(conn.Follow)
		}
		if watcher, ok := conn.Agent.(interface {
			WatchTaskUpdates() (<-chan session.Event, func())
		}); ok {
			events, stop := watcher.WatchTaskUpdates()
			go s.lane("task", events, stop)
		}
		if watcher, ok := conn.Agent.(interface {
			WatchTitle() (<-chan session.Event, func())
		}); ok {
			events, stop := watcher.WatchTitle()
			go s.lane("title", events, stop)
		}
		if watcher, ok := conn.Agent.(interface {
			WatchQuestions() (<-chan session.Event, func())
		}); ok {
			events, stop := watcher.WatchQuestions()
			go s.lane("question", events, stop)
		}
		go s.watchPlanReads()
		write(w, s.snapshot())
		return
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] != "sessions" {
		fail(w, 404, "unknown engine action")
		return
	}
	b.mu.Lock()
	s := b.sessions[parts[1]]
	b.mu.Unlock()
	if s == nil {
		fail(w, 404, "reattach this conversation")
		return
	}
	if len(parts) == 2 && r.Method == http.MethodGet {
		write(w, s.snapshot())
		return
	}
	if len(parts) == 4 && parts[2] == "tools" && r.Method == http.MethodGet {
		output, err := s.toolOutput(parts[3])
		if err != nil {
			fail(w, 409, err.Error())
			return
		}
		write(w, output)
		return
	}
	if len(parts) == 4 && parts[2] == "tasks" && r.Method == http.MethodGet {
		page, known, err := s.conn.Agent.ReadPlanTaskPage(parts[3])
		if err != nil {
			fail(w, 502, err.Error())
			return
		}
		if !known {
			fail(w, 404, "task not found")
			return
		}
		write(w, page)
		return
	}
	if b.usingRoutes(w, r, s, parts) {
		return
	}
	if s.extra(w, r, parts) {
		return
	}
	if len(parts) != 3 {
		fail(w, 404, "unknown engine action")
		return
	}
	switch parts[2] {
	case "answer":
		if r.Method != http.MethodPost {
			fail(w, 405, "POST required")
			return
		}
		var answer session.Answer
		if !decode(w, r, &answer) {
			return
		}
		door, ok := s.conn.Agent.(interface {
			ResolveQuestion(session.Answer) error
			OpenQuestions() []session.Question
		})
		if !ok {
			fail(w, 409, "this engine cannot answer questions")
			return
		}
		var question *session.Question
		for _, q := range door.OpenQuestions() {
			if q.Kind == answer.Kind && q.ID == answer.ID && q.Ref == answer.Ref {
				copy := q
				question = &copy
				break
			}
		}
		if question == nil {
			fail(w, 409, "this question is no longer waiting")
			return
		}
		answer.At = time.Now()
		answer.From = "desktop"
		answer.Ask = question.Ask
		// The asker may only answer for itself where it wrote down a pick, since
		// that is the one case where "nobody objected" has an answer to stand on;
		// every other answer from this window is a person's.
		if answer.DecidedBy != session.DecidedByAsker || question.Pick == nil {
			answer.DecidedBy = session.DecidedByPerson
		}
		if s.conn.Take != nil {
			if err := s.conn.Take(); err != nil {
				fail(w, 409, err.Error())
				return
			}
		}
		if err := door.ResolveQuestion(answer); err != nil {
			fail(w, 409, err.Error())
			return
		}
		snapshot := s.snapshot()
		s.publish(Record{Type: "snapshot", Snapshot: &snapshot})
		write(w, map[string]bool{"accepted": true})
	case "turn":
		if r.Method != http.MethodPost {
			fail(w, 405, "POST required")
			return
		}
		var ask turnAsk
		if !decodeMax(w, r, &ask, maxTurnBody) {
			return
		}
		if strings.TrimSpace(ask.Text) == "" && len(ask.Files) == 0 {
			fail(w, 400, "instruction required")
			return
		}
		attachments, status, err := ask.attachments()
		if err != nil {
			fail(w, status, err.Error())
			return
		}
		if attachments.any() && ask.Mode != "" && ask.Mode != "submit" {
			fail(w, 409, "files can only be sent with a new message")
			return
		}
		if s.conn.Take != nil {
			if err := s.conn.Take(); err != nil {
				fail(w, 409, err.Error())
				return
			}
		}
		s.mu.Lock()
		running := s.running
		if running && ask.Mode != "steer" && ask.Mode != "queue" {
			s.mu.Unlock()
			fail(w, 409, "work is running; steer or queue explicitly")
			return
		}
		if !running {
			s.running = true
		}
		s.mu.Unlock()
		var events <-chan session.Event
		switch ask.Mode {
		case "", "submit":
			events, err = s.submit(ask.Text, attachments)
		case "steer":
			events, err = s.conn.Agent.Steer(ask.Text)
		case "queue":
			events, err = s.conn.Agent.FollowUp(ask.Text)
		default:
			err = errors.New("unknown instruction action")
		}
		if err != nil {
			s.mu.Lock()
			s.running = running
			s.mu.Unlock()
			fail(w, 409, err.Error())
			return
		}
		// A steer/queue has another stream into the same turn; drain it, but publish
		// only the original owner's stream so the renderer never duplicates text.
		if running && ask.Mode == "queue" {
			s.remember(ask.Text, events)
			// Other windows on this conversation learn of the new row now.
			listed := s.snapshot()
			s.publish(Record{Type: "snapshot", Snapshot: &listed})
			go s.queued(events)
		} else if running {
			s.mu.Lock()
			primary := s.primary
			s.mu.Unlock()
			if events != primary {
				go s.steered(events)
			}
		} else {
			go s.pump(events, func() {})
		}
		write(w, map[string]bool{"accepted": true})
	case "queue-edit", "queue-move", "queue-remove", "queue-send":
		if parts[2] == "queue-send" {
			s.queueSend(w, r)
		} else {
			s.queueAction(w, r, parts[2])
		}
	case "stop":
		if r.Method != http.MethodPost {
			fail(w, 405, "POST required")
			return
		}
		if s.conn.Take != nil {
			if err := s.conn.Take(); err != nil {
				fail(w, 409, err.Error())
				return
			}
		}
		if err := s.conn.Agent.StopWork(); err != nil {
			fail(w, 409, err.Error())
			return
		}
		write(w, map[string]bool{"accepted": true})
	case "events":
		if r.Method != http.MethodGet {
			fail(w, 405, "GET required")
			return
		}
		s.events(w, r)
	default:
		fail(w, 404, "unknown engine action")
	}
}
func (s *conversation) events(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.observers++
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.observers--; s.mu.Unlock() }()
	f, ok := w.(http.Flusher)
	if !ok {
		fail(w, 500, "streaming unavailable")
		return
	}
	after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	f.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		s.mu.Lock()
		var batch []Record
		for _, item := range s.records {
			if item.Seq > after {
				batch = append(batch, item)
			}
		}
		changed := s.changed
		gap := len(s.records) > 0 && after+1 < s.records[0].Seq
		s.mu.Unlock()
		if gap {
			snapshot := s.snapshot()
			batch = []Record{{Seq: snapshot.Seq, Type: "snapshot", Snapshot: &snapshot}}
		}
		for _, item := range batch {
			data, _ := json.Marshal(item)
			if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", item.Seq, data); err != nil {
				return
			}
			after = item.Seq
		}
		if len(batch) > 0 {
			f.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": alive\n\n"); err != nil {
				return
			}
			f.Flush()
		}
	}
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeMax(w, r, v, 1<<20)
}

// decodeMax is decode with a body ceiling; a body over it is 413, not "invalid".
func decodeMax(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, 415, "JSON required")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fail(w, 413, "that message is larger than the desktop can carry; send fewer or smaller files")
			return false
		}
		fail(w, 400, "invalid request")
		return false
	}
	return true
}
func write(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

const maxToolDisplayBytes = 1 << 20

var stubFilename = regexp.MustCompile(`^[a-f0-9]{16}\.txt$`)

type ToolOutput struct {
	Output string `json:"output"`
	Full   bool   `json:"full"`
}

// Only a named canonical call may select its own recorded result. The renderer
// supplies a call ID, never a path, and the engine's existing FetchFile applies
// its own session/workspace boundary after this adapter validates the stub.
func (s *conversation) toolOutput(callID string) (ToolOutput, error) {
	var output string
	found := false
	for _, entry := range s.conn.Agent.Transcript() {
		if entry.Role == "tool" && entry.Tool != "" && entry.CallID == callID {
			output = entry.Output
			found = true
			break
		}
	}
	if !found {
		return ToolOutput{}, errors.New("this tool call is not in this conversation")
	}
	if output == "" {
		return ToolOutput{}, errors.New("this call has no recorded result yet")
	}
	if !strings.HasPrefix(output, "[tool:") || !strings.HasSuffix(output, "]") {
		return ToolOutput{Output: boundedToolText(output), Full: false}, nil
	}
	at := strings.LastIndex(output, " · full: ")
	if at < 0 {
		return ToolOutput{Output: boundedToolText(output), Full: false}, nil
	}
	pointer := strings.TrimSpace(output[at+len(" · full: ") : len(output)-1])
	if !filepath.IsAbs(pointer) {
		pointer = filepath.Join(s.conn.Welcome.Workspace, pointer)
	}
	root := filepath.Join(filepath.Dir(s.conn.Welcome.SessionFile), "logs", "stubs")
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ToolOutput{}, errors.New("the saved tool result is unavailable")
	}
	real, err := filepath.EvalSymlinks(pointer)
	if err != nil {
		return ToolOutput{}, errors.New("the saved tool result is unavailable")
	}
	if filepath.Dir(real) != realRoot || !stubFilename.MatchString(filepath.Base(real)) {
		return ToolOutput{}, errors.New("the saved result is outside this conversation's tool records")
	}
	if s.conn.FetchFile == nil {
		return ToolOutput{}, errors.New("this engine cannot retrieve saved tool results")
	}
	file, err := s.conn.FetchFile(real)
	if err != nil {
		return ToolOutput{}, err
	}
	return ToolOutput{Output: boundedToolText(string(file.Bytes)), Full: len(file.Bytes) <= maxToolDisplayBytes}, nil
}
func boundedToolText(text string) string {
	if len(text) <= maxToolDisplayBytes {
		return text
	}
	text = text[:maxToolDisplayBytes]
	for !utf8.ValidString(text) && len(text) > 0 {
		text = text[:len(text)-1]
	}
	return text
}
