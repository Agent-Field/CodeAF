package appapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Money carries exact cents on the app wire, rather than a binary float.
type Money struct {
	USD string `json:"usd"`
}

// Agent is the app's resource. Its identity is the transcript's stable id.
type Agent struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Model         string         `json:"model"`
	Effort        string         `json:"effort"`
	Budget        Money          `json:"budget"`
	State         string         `json:"state"`
	CrewID        string         `json:"crew_id,omitempty"`
	RoleID        string         `json:"role_id,omitempty"`
	Rules         []string       `json:"rules"`
	Pinned        bool           `json:"pinned"`
	CreatedAt     time.Time      `json:"created_at"`
	CreatedBy     string         `json:"created_by"`
	LastMessageAt *time.Time     `json:"last_message_at,omitempty"`
	ClosedAt      *time.Time     `json:"closed_at,omitempty"`
	ArchivedAt    *time.Time     `json:"archived_at,omitempty"`
	Glyph         string         `json:"glyph,omitempty"`
	Spent         *Money         `json:"spent,omitempty"`
	Context       []AgentContext `json:"context"`
	Version       uint64         `json:"version"`
	Receipt       string         `json:"receipt,omitempty"`
}

type AgentContext struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Exists bool   `json:"exists"`
}

// AgentEngine is the existing session protocol; the registry owns no model loop.
// A4b extends the same connection for turns, queues, files and richer snapshots.
type AgentEngine interface {
	Submit(context.Context, string) (<-chan session.Event, error)
	StopWork() error
	Model() string
	SetModel(string)
	SetReasoningFor(string, string)
	SetSpendRail(float64) error
	SetApprovalPosture(string) error
	Rename(string) error
	NeedsPerson() bool
	Transcript() []session.DisplayEntry
	PlanTasks() []session.PlanTaskRow
	Usage() session.Usage
}

type AgentOpenRequest struct {
	SessionFile string
	Workspace   string
	Model       string
	Effort      string
	Approvals   string
	Budget      Money
}

type AgentConnection struct {
	asks        *askObserver
	processes   *processObserver
	Engine      AgentEngine
	SessionFile string
	Workspace   string
	Close       func()
	End         func() error
}

type OpenAgent func(context.Context, AgentOpenRequest) (AgentConnection, error)

type agentRecord struct {
	Agent       Agent              `json:"agent"`
	SessionFile string             `json:"session_file"`
	Workspace   string             `json:"workspace"`
	Approvals   string             `json:"approvals,omitempty"`
	Queue       []QueueItem        `json:"queue,omitempty"`
	LastTurn    *TurnRequest       `json:"last_turn,omitempty"`
	TurnSeq     uint64             `json:"turn_seq,omitempty"`
	Receipts    []turnSavedReceipt `json:"receipts,omitempty"`
}

type agentDisk struct {
	Records map[string]agentRecord `json:"agents"`
	Replays map[string]agentReplay `json:"replays,omitempty"`
}

type agentLive struct {
	// The task lane is subscribed once and released with this connection.
	taskStop   func()
	conn       AgentConnection
	running    bool
	generation uint64
	primary    <-chan session.Event
	turnID     string
	longTimer  *time.Timer
	longShown  bool
	// held names Inbox items whose answer waits out its Undo window; the
	// agent is not waiting on the person for those questions.
	held map[string]bool
	// taken is set when the engine takes a held answer. Its own word and the
	// presence file still say needs-person for a moment after that, so both
	// are left unread until the engine states its facts again, the word
	// catches up, a new question opens or the turn ends.
	taken bool
}

// needsPerson is the one reading of "waiting on the person" for views and
// stored transitions: an open question whose answer is not held for Undo.
// The open questions are read themselves, because an engine's own word can
// lag them (a remote engine pushes it) or not cover a question the model
// asked. Callers hold the registry lock.
func (live *agentLive) needsPerson(id string) bool {
	door, ok := live.conn.Engine.(inboxQuestionDoor)
	if !ok {
		return live.conn.Engine.NeedsPerson()
	}
	open := door.OpenQuestions()
	live.pruneHeld(id, open)
	for _, q := range open {
		if !live.held[inboxQuestionID(id, q)] {
			live.taken = false
			return true
		}
	}
	// Every open question is answered and held: the person is not waited on.
	if len(open) > 0 {
		return false
	}
	// No question is open, so only the engine's word is left; just after an
	// answer was taken that word is the old one.
	if !live.conn.Engine.NeedsPerson() {
		live.taken = false
	}
	return !live.taken && live.conn.Engine.NeedsPerson()
}

// readsPresence reports whether the presence file may speak for this agent:
// not while an answer is held or was just taken, when the file is behind.
func (live *agentLive) readsPresence() bool {
	return live == nil || len(live.held) == 0 && !live.taken
}

// pruneHeld drops holds whose question the engine has taken: a sent answer
// stays held until its question leaves the open set. Callers hold the lock.
func (live *agentLive) pruneHeld(id string, open []session.Question) {
	still := make(map[string]bool, len(open))
	for _, q := range open {
		still[inboxQuestionID(id, q)] = true
	}
	for item := range live.held {
		if !still[item] {
			delete(live.held, item)
			live.taken = true
		}
	}
}

// wordStated ends the wait on the engine's word after a taken answer. The
// engine states its facts ahead of the events that move them (a tool result,
// a new ask, a capability or connect wait, the turn's end), so its word is
// current when one is heard. Text, reasoning and a tool's start carry no
// facts: clearing on those would read the old word and flash needs_you.
func (a *AgentRegistry) wordStated(live *agentLive, kind session.EventKind) {
	if !remote.FactsMoved(kind) {
		return
	}
	a.mu.Lock()
	live.taken = false
	a.mu.Unlock()
}

func (a *AgentRegistry) liveNeedsPerson(id string, live *agentLive) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return live.needsPerson(id)
}

// holdAnswer records that an Inbox answer entered (held) or left its Undo
// window and stores the agent state the views now read, so the desk hears
// agent.changed at the click rather than when the window closes.
func (a *AgentRegistry) holdAnswer(id, item string, held bool) {
	a.mu.Lock()
	live := a.live[id]
	if live == nil {
		a.mu.Unlock()
		return
	}
	if held {
		if live.held == nil {
			live.held = map[string]bool{}
		}
		live.held[item] = true
	} else {
		delete(live.held, item)
	}
	state := "running"
	if live.needsPerson(id) {
		state = "needs_you"
	}
	generation := live.generation
	a.mu.Unlock()
	_ = a.transitionState(id, state, live, generation)
}

// AgentRegistry serializes local connections and flocks durable mutations so
// two app launches sharing a home cannot overwrite each other's lifecycle state.
// Closed and archived records remain beside their transcripts indefinitely.
type AgentRegistry struct {
	mu         sync.Mutex
	home       string
	askProfile string
	open       OpenAgent
	now        func() time.Time
	live       map[string]*agentLive
	events     *EventHub
	stopped    bool
	after      func(time.Duration, func()) *time.Timer
	done       chan struct{}
}

func NewAgentRegistry(home string, open OpenAgent) *AgentRegistry {
	return &AgentRegistry{home: home, open: open, now: time.Now, after: time.AfterFunc, done: make(chan struct{}), live: make(map[string]*agentLive)}
}

// Close releases this launch's connections. It never closes or archives resources.
func (a *AgentRegistry) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return
	}
	a.stopped = true
	close(a.done)
	for id, live := range a.live {
		if live.longTimer != nil {
			live.longTimer.Stop()
		}
		_ = stopEngineProcesses(live.conn.Engine)
		a.syncProcessEventsLocked(id, live.conn)
		if live.taskStop != nil {
			live.taskStop()
		}
		if live.conn.Close != nil {
			live.conn.Close()
		}
		delete(a.live, id)
	}
}

func (a *AgentRegistry) disk(fn func(*agentDisk) error, save bool) error {
	if a.home == "" {
		return errors.New("the agent registry has no engine home")
	}
	dir := filepath.Join(a.home, "v3", "app")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "agents.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer func() { _ = filelock.Unlock(lock) }()
	disk := agentDisk{Records: map[string]agentRecord{}, Replays: map[string]agentReplay{}}
	path := filepath.Join(dir, "agents.json")
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal(raw, &disk); err != nil {
			return fmt.Errorf("read agent registry: %w", err)
		}
		if disk.Records == nil {
			disk.Records = map[string]agentRecord{}
		}
		if disk.Replays == nil {
			disk.Replays = map[string]agentReplay{}
		}
	}
	// Home's canonical walk includes TUI conversations without inventing app ids.
	for _, row := range session.ReadWorld(filepath.Join(a.home, "v3", "places")).Sessions() {
		if _, ok := disk.Records[row.ID]; ok {
			continue
		}
		effort := config.AppDefaultEffort
		meta, err := session.LoadMeta(row.Dir)
		if err == nil && meta.Effort != "" {
			effort = meta.Effort
		}
		state := "idle"
		if row.Archived {
			state = "archived"
		}
		agent := Agent{ID: row.ID, Name: row.Title, Model: row.Model, Effort: effort, Budget: Money{fmt.Sprintf("%.2f", config.AppDefaultAgentBudget)}, State: state, Rules: []string{}, Context: []AgentContext{}, CreatedAt: row.Created, CreatedBy: "you", Version: 1}
		if !row.At.IsZero() {
			at := row.At
			agent.LastMessageAt = &at
		}
		disk.Records[row.ID] = agentRecord{Agent: agent, SessionFile: row.Transcript, Workspace: row.Workspace}
	}
	if err := fn(&disk); err != nil {
		return err
	}
	if !save {
		return nil
	}
	// Glyph and spend are derived at read time and never banked as state.
	for id, record := range disk.Records {
		record.Agent.Glyph, record.Agent.Spent = "", nil
		disk.Records[id] = record
	}
	raw, err = json.Marshal(disk)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".agents-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

func (a *AgentRegistry) view(record agentRecord) Agent {
	agent := record.Agent
	agent.Effort = config.AppEffortWord(agent.Effort)
	live := a.live[agent.ID]
	if agent.State != "closed" && agent.State != "archived" {
		row := session.ReadRows([]string{record.SessionFile})[record.SessionFile]
		presence, fresh := session.ReadSessionPresence(filepath.Dir(record.SessionFile), a.now())
		switch {
		// A held answer is the one case the presence file is behind on.
		case live != nil && live.needsPerson(agent.ID), live.readsPresence() && fresh && presence.NeedsPerson():
			agent.State = "needs_you"
		case live != nil && live.running, fresh && (presence.State == session.PresenceWorking || len(presence.RunningTasks) > 0 || len(presence.Jobs) > 0):
			agent.State = "running"
		case agent.State == "running" || agent.State == "needs_you":
			agent.State, agent.Receipt = "idle", "Stopped · codeaf restarted"
		}
		if !row.At.IsZero() && (agent.LastMessageAt == nil || row.At.After(*agent.LastMessageAt)) {
			at := row.At
			agent.LastMessageAt = &at
		}
		if row.Spend > 0 {
			agent.Spent = &Money{fmt.Sprintf("%.2f", row.Spend)}
		}
		if live != nil {
			agent.Model = live.conn.Engine.Model()
			if usd := live.conn.Engine.Usage().CostUSD; usd > 0 {
				agent.Spent = &Money{fmt.Sprintf("%.2f", usd)}
			}
		}
	}
	agent.Glyph = ""
	if agent.State == "needs_you" || agent.State == "running" || agent.State == "failed" {
		agent.Glyph = agent.State
	}
	for i := range agent.Context {
		_, err := os.Stat(agent.Context[i].Path)
		agent.Context[i].Exists = err == nil
	}
	return agent
}

func (a *AgentRegistry) publish(kind EventType, agent Agent, fields ...string) {
	if a.events != nil {
		_, _ = a.events.Publish(kind, AgentEvent{Agent: agent.ID, Fields: fields})
	}
}

// UpdateState is the producer door for turns and asks. Lifecycle endpoints keep
// precedence over late events, so a finished stream cannot reopen a closed agent.
func (a *AgentRegistry) UpdateState(id, state string) error {
	return a.transitionState(id, state, nil, 0)
}

func (a *AgentRegistry) transitionState(id, state string, owner *agentLive, generation uint64) error {
	if state != "idle" && state != "running" && state != "needs_you" && state != "failed" {
		return errors.New("invalid live agent state")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped || (owner != nil && (a.live[id] != owner || owner.generation != generation)) {
		return nil
	}
	var changed Agent
	err := a.disk(func(d *agentDisk) error {
		record, ok := d.Records[id]
		if !ok {
			return errAgentMissing
		}
		if record.Agent.State == "closed" || record.Agent.State == "archived" {
			return nil
		}
		if live := a.live[id]; live != nil {
			live.running = state == "running" || state == "needs_you"
		}
		if record.Agent.State == state {
			return nil
		}
		record.Agent.State = state
		record.Agent.Version++
		d.Records[id] = record
		changed = record.Agent
		return nil
	}, true)
	if err == nil && changed.ID != "" {
		a.publish(EventAgentChanged, changed, "state", "glyph")
	}
	return err
}

func sortAgents(agents []Agent) {
	sort.Slice(agents, func(i, j int) bool {
		x, y := agents[i].CreatedAt, agents[j].CreatedAt
		if agents[i].LastMessageAt != nil {
			x = *agents[i].LastMessageAt
		}
		if agents[j].LastMessageAt != nil {
			y = *agents[j].LastMessageAt
		}
		if !x.Equal(y) {
			return x.After(y)
		}
		return agents[i].ID < agents[j].ID
	})
}

var errAgentMissing = errors.New("no such agent")

// Agent names have one implementation shared with the session's no-AI title path.
func AgentName(text string) string { return session.FirstMessageName(text) }

func agentState(state string) bool {
	return state == "idle" || state == "running" || state == "needs_you" || state == "failed" || state == "closed" || state == "archived"
}

func matchAgent(agent Agent, query string) (bool, error) {
	for _, token := range strings.Fields(strings.ToLower(query)) {
		key, value, field := strings.Cut(token, ":")
		if !field {
			if !strings.Contains(strings.ToLower(agent.Name), token) {
				return false, nil
			}
			continue
		}
		switch key {
		case "state":
			if value == "open" {
				if agent.State == "closed" || agent.State == "archived" {
					return false, nil
				}
			} else {
				if !agentState(value) {
					return false, errors.New("unknown agent state")
				}
				if agent.State != value {
					return false, nil
				}
			}
		case "kind":
			if value != "agent" {
				return false, nil
			}
		case "model":
			if !strings.Contains(strings.ToLower(agent.Model), value) {
				return false, nil
			}
		case "crew":
			if agent.CrewID != value {
				return false, nil
			}
		case "pinned":
			if value != "true" && value != "false" {
				return false, errors.New("pinned needs true or false")
			}
			if agent.Pinned != (value == "true") {
				return false, nil
			}
		case "since":
			at, err := time.Parse("2006-01-02", value)
			if err != nil {
				return false, errors.New("since needs a UTC date (YYYY-MM-DD)")
			}
			last := agent.CreatedAt
			if agent.LastMessageAt != nil {
				last = *agent.LastMessageAt
			}
			if last.Before(at) {
				return false, nil
			}
		default:
			return false, fmt.Errorf("unsupported agent filter: %s", key)
		}
	}
	return true, nil
}
