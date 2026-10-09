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

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
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
	Close     func()
}
type Open func(sessionFile string) (Connection, error)

type Snapshot struct {
	ID          string                 `json:"id"`
	SessionFile string                 `json:"sessionFile"`
	Workspace   string                 `json:"workspace"`
	Model       string                 `json:"model"`
	Persistent  bool                   `json:"persistent"`
	Running     bool                   `json:"running"`
	NeedsPerson bool                   `json:"needsPerson"`
	Questions   []session.Question     `json:"questions"`
	PlanError   string                 `json:"planError,omitempty"`
	Title       string                 `json:"title"`
	Entries     []session.DisplayEntry `json:"entries"`
	Tasks       []session.PlanTaskRow  `json:"tasks"`
	Usage       session.Usage          `json:"usage"`
	UpdatedAt   string                 `json:"updatedAt,omitempty"`
	Seq         uint64                 `json:"seq"`
}
type Event struct {
	Kind  string           `json:"kind"`
	Text  string           `json:"text"`
	Tool  string           `json:"tool"`
	Hint  string           `json:"hint"`
	Error string           `json:"error,omitempty"`
	Raw   remote.EventWire `json:"raw"`
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
	planError  string
}
type Bridge struct {
	token     string
	open      Open
	mu        sync.Mutex
	sessions  map[string]*conversation
	closeOnce sync.Once
}

func New(token string, open Open) *Bridge {
	return &Bridge{token: token, open: open, sessions: map[string]*conversation{}}
}
func Token() (string, error) {
	var b [32]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}
func (b *Bridge) Close() {
	b.closeOnce.Do(func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for _, s := range b.sessions {
			close(s.done)
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
	return Snapshot{ID: s.id, SessionFile: w.SessionFile, Workspace: w.Workspace, Model: a.Model(), Persistent: w.Persistent, Running: running, NeedsPerson: a.NeedsPerson(), Title: a.Title(), Questions: questions, PlanError: planError, Entries: entries, Tasks: tasks, Usage: a.Usage(), Seq: seq, UpdatedAt: updatedAt}
}
func (s *conversation) publish(r Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
}
func (s *conversation) pump(events <-chan session.Event, stop func()) {
	s.mu.Lock()
	s.generation++
	generation := s.generation
	s.primary = events
	s.mu.Unlock()
	defer stop()
	for ev := range events {
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
}

// FollowUp is its own future turn, not a second reader of the current turn.
func (s *conversation) queued(events <-chan session.Event) {
	first, ok := <-events
	if !ok {
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
		return "other"
	}
}
func (b *Bridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	origin := r.Header.Get("Origin")
	native := origin == "tauri://localhost" || origin == "http://tauri.localhost" || origin == "http://localhost:1420" || origin == "http://127.0.0.1:1420"
	if native {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
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
	if path == "/sessions" && r.Method == http.MethodPost {
		var ask struct {
			SessionFile string `json:"sessionFile"`
		}
		if !decode(w, r, &ask) {
			return
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		for _, s := range b.sessions {
			if ask.SessionFile != "" && s.conn.Welcome.SessionFile == ask.SessionFile {
				write(w, s.snapshot())
				return
			}
		}
		conn, err := b.open(ask.SessionFile)
		if err != nil {
			fail(w, 502, err.Error())
			return
		}
		if conn.Welcome.Model != Model || conn.Welcome.Launch == nil || !conn.Welcome.Launch.OneModel || !conn.Welcome.Persistent {
			conn.Close()
			fail(w, 409, "this conversation must use the fixed model and persistent engine; open a new conversation")
			return
		}
		id, err := Token()
		if err != nil {
			conn.Close()
			fail(w, 500, "cannot create session identity")
			return
		}
		s := &conversation{conn: conn, id: id, changed: make(chan struct{}), done: make(chan struct{})}
		b.sessions[id] = s
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
		answer.DecidedBy = session.DecidedByPerson
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
		var ask struct {
			Text string `json:"text"`
			Mode string `json:"mode"`
		}
		if !decode(w, r, &ask) {
			return
		}
		if strings.TrimSpace(ask.Text) == "" {
			fail(w, 400, "instruction required")
			return
		}
		if s.conn.Agent.Model() != Model {
			fail(w, 409, "conversation model changed; reconnect with the fixed model")
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
		var err error
		switch ask.Mode {
		case "", "submit":
			events, err = s.conn.Agent.Submit(context.Background(), ask.Text)
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
			go s.queued(events)
		} else if running {
			s.mu.Lock()
			primary := s.primary
			s.mu.Unlock()
			if events != primary {
				go func() {
					for range events {
					}
				}()
			}
		} else {
			go s.pump(events, func() {})
		}
		write(w, map[string]bool{"accepted": true})
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
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, 415, "JSON required")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
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
