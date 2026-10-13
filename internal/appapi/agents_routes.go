package appapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

type AgentExpect struct {
	State   *string `json:"state,omitempty"`
	Version *uint64 `json:"version,omitempty"`
}

type agentMutation struct {
	Text        string         `json:"text"`
	Model       *string        `json:"model,omitempty"`
	Effort      *string        `json:"effort,omitempty"`
	Budget      *Money         `json:"budget,omitempty"`
	Approvals   string         `json:"approvals,omitempty"`
	Context     []AgentContext `json:"context,omitempty"`
	Start       string         `json:"start"`
	Name        *string        `json:"name,omitempty"`
	Pinned      *bool          `json:"pinned,omitempty"`
	StopRunning bool           `json:"stop_running,omitempty"`
	Expect      *AgentExpect   `json:"expect,omitempty"`
}

type agentReplay struct {
	At     time.Time       `json:"at"`
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

type agentReplyError struct {
	status           int
	reason, sentence string
	running          *agentRunning
}

func (e *agentReplyError) Error() string { return e.sentence }
func agentError(status int, reason, sentence string) error {
	return &agentReplyError{status: status, reason: reason, sentence: sentence}
}

type agentRunning struct {
	Processes int `json:"processes"`
	Tasks     int `json:"tasks"`
}

// The route table is local to agents so transport and later resource lanes can
// evolve independently while every route still passes through Server's guard.
var agentRoutes = map[string]string{
	"": "GET, POST", "{id}": "GET, PATCH", "{id}/close": "POST", "{id}/archive": "POST", "{id}/reopen": "POST",
}

func (s *Server) agentsRoute(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/app/v1/agents")
	id, action, shape := "", "", ""
	if path != "" {
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) < 1 || len(parts) > 2 || parts[0] == "" {
			fail(w, 404, "not_found", "no such app route")
			return
		}
		id, shape = parts[0], "{id}"
		if len(parts) == 2 {
			action, shape = parts[1], "{id}/"+parts[1]
		}
	}
	allow, ok := agentRoutes[shape]
	if !ok {
		fail(w, 404, "not_found", "no such app route")
		return
	}
	accepted := false
	for _, method := range strings.Split(allow, ", ") {
		accepted = accepted || r.Method == method
	}
	if !accepted {
		w.Header().Set("Allow", allow)
		fail(w, 405, "not_allowed", "this route does not accept that method")
		return
	}
	a := s.options.Agents
	if a == nil {
		fail(w, 503, "not_allowed", "the agent registry is unavailable")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		fail(w, 503, "not_allowed", "the engine is stopping")
		return
	}
	a.events = s.events
	if r.Method == http.MethodGet {
		s.readAgents(w, r, a, id)
		return
	}
	var body agentMutation
	if !decodeAgent(w, r, &body, shape) {
		return
	}
	if err := validateAgentMutation(body, id, action); err != nil {
		fail(w, 422, "bad_input", err.Error())
		return
	}
	key := ""
	if r.Method == http.MethodPost && r.Header.Get("Idempotency-Key") != "" {
		if len(r.Header.Values("Idempotency-Key")) != 1 || len(r.Header.Get("Idempotency-Key")) > 200 {
			fail(w, 422, "bad_input", "use one Idempotency-Key of at most 200 bytes")
			return
		}
		hash := sha256.Sum256([]byte(r.Header.Get("Authorization") + "\x00" + r.URL.RequestURI() + "\x00" + r.Header.Get("Idempotency-Key")))
		key = hex.EncodeToString(hash[:])
	}
	var reply agentReplay
	var changed Agent
	var fields []string
	var stream <-chan session.Event
	err := a.disk(func(d *agentDisk) error {
		if key != "" {
			if saved, ok := d.Replays[key]; ok && a.now().Sub(saved.At) < 24*time.Hour {
				reply = saved
				return nil
			}
		}
		var err error
		if id == "" {
			if err := s.fillAgentDefaults(&body); err != nil {
				return agentError(500, "conflict", "could not read saved settings")
			}
			changed, stream, err = a.create(r.Context(), d, body)
			fields = []string{"name", "state", "model", "effort", "budget"}
		} else {
			changed, fields, err = a.mutate(r.Context(), d, id, action, body)
		}
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(changed)
		if err != nil {
			return err
		}
		reply = agentReplay{At: a.now(), Status: http.StatusOK, Body: encoded}
		if id == "" {
			reply.Status = http.StatusCreated
		}
		if key != "" {
			for k, replay := range d.Replays {
				if a.now().Sub(replay.At) >= 24*time.Hour {
					delete(d.Replays, k)
				}
			}
			// Bound even a busy launch's keyed responses; oldest receipts expire first.
			if len(d.Replays) >= 4096 {
				oldest := ""
				var at time.Time
				for k, replay := range d.Replays {
					if oldest == "" || replay.At.Before(at) {
						oldest, at = k, replay.At
					}
				}
				delete(d.Replays, oldest)
			}
			d.Replays[key] = reply
		}
		return nil
	}, true)
	if err != nil {
		agentFailure(w, err)
		return
	}
	if changed.ID != "" {
		kind := EventAgentChanged
		if id == "" {
			kind = EventAgentCreated
		}
		a.publish(kind, changed, fields...)
		if stream != nil {
			a.startTurnPump(changed.ID, a.live[changed.ID], stream)
		}
	}
	write(w, reply.Status, reply.Body)
}

// Strict per-route fields catch UI mistakes before an engine or disk is touched.
func decodeAgent(w http.ResponseWriter, r *http.Request, body *agentMutation, shape string) bool {
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(&raw); err != nil || raw == nil {
		fail(w, 422, "bad_input", "this action needs one JSON object")
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		fail(w, 422, "bad_input", "this action needs one JSON object")
		return false
	}
	allowed := "expect"
	if shape == "{id}/close" {
		allowed = "expect stop_running"
	}
	if shape == "" {
		allowed = "text model effort budget approvals context start expect"
	}
	if shape == "{id}" {
		allowed = "name model effort budget pinned expect"
	}
	for field, value := range raw {
		if !strings.Contains(" "+allowed+" ", " "+field+" ") || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			fail(w, 422, "bad_input", "invalid field: "+field)
			return false
		}
	}
	encoded, _ := json.Marshal(raw)
	strict := json.NewDecoder(bytes.NewReader(encoded))
	strict.DisallowUnknownFields()
	if err := strict.Decode(body); err != nil {
		fail(w, 422, "bad_input", "invalid action fields")
		return false
	}
	return true
}

var agentMoney = regexp.MustCompile(`^(0|[1-9][0-9]{0,6})\.[0-9]{2}$`)

func validateAgentMutation(body agentMutation, id, action string) error {
	if id == "" && body.Start != "send" && body.Start != "empty" {
		return errors.New("start must be send or empty")
	}
	if id == "" && body.Start == "send" && strings.TrimSpace(body.Text) == "" {
		return errors.New("send needs a first message")
	}
	if id == "" && body.Start == "empty" && strings.TrimSpace(body.Text) != "" {
		return errors.New("start empty cannot send text")
	}
	if body.Name != nil && (strings.TrimSpace(*body.Name) == "" || !utf8.ValidString(*body.Name) || utf8.RuneCountInString(strings.TrimSpace(*body.Name)) > session.AppNameLimit || strings.ContainsAny(*body.Name, "\r\n\x00")) {
		return errors.New("agent name must be one line of 1 to 40 characters")
	}
	if body.Model != nil && strings.TrimSpace(*body.Model) == "" {
		return errors.New("model must name a model")
	}
	if body.Effort != nil && *body.Effort != "low" && *body.Effort != "medium" && *body.Effort != "high" {
		return errors.New("effort must be low, medium or high")
	}
	if body.Budget != nil && (!agentMoney.MatchString(body.Budget.USD) || body.Budget.USD == "0.00") {
		return errors.New("budget needs positive USD with two decimal places")
	}
	if body.Approvals != "" && body.Approvals != "risky" && body.Approvals != "everything" && body.Approvals != "none" {
		return errors.New("approvals must be risky, everything or none")
	}
	if body.Expect != nil && body.Expect.State != nil && !agentState(*body.Expect.State) {
		return errors.New("expect.state must be an agent state")
	}
	for _, chip := range body.Context {
		if strings.TrimSpace(chip.Path) == "" || !filepath.IsAbs(chip.Path) || (chip.Kind != "file" && chip.Kind != "folder" && chip.Kind != "repo") {
			return errors.New("context needs an absolute path and kind file, folder or repo")
		}
	}
	return nil
}

func agentFailure(w http.ResponseWriter, err error) {
	var reply *agentReplyError
	if errors.As(err, &reply) {
		if reply.running != nil {
			write(w, reply.status, struct {
				Error     string        `json:"error"`
				Reason    string        `json:"reason"`
				RequestID string        `json:"request_id"`
				Running   *agentRunning `json:"running"`
			}{reply.sentence, reply.reason, fmt.Sprintf("%08x", requestSequence.Add(1)), reply.running})
			return
		}
		fail(w, reply.status, reply.reason, reply.sentence)
		return
	}
	if errors.Is(err, errAgentMissing) {
		fail(w, 404, "not_found", "no such agent")
		return
	}
	fail(w, 500, "not_allowed", "the agent action could not be saved")
}

func (s *Server) readAgents(w http.ResponseWriter, r *http.Request, a *AgentRegistry, id string) {
	err := a.disk(func(d *agentDisk) error {
		if id != "" {
			record, ok := d.Records[id]
			if !ok {
				return errAgentMissing
			}
			agent := a.view(record)
			write(w, 200, AgentResponse{Agent: agent, Snapshot: a.turnSnapshot(record)})
			return nil
		}
		state, q := r.URL.Query().Get("state"), r.URL.Query().Get("q")
		if state != "" && !agentState(state) && state != "open" {
			return agentError(422, "bad_input", "unknown agent state")
		}
		if state != "" {
			q += " state:" + state
		}
		// Validate tokens even when the home has no agents.
		for _, token := range strings.Fields(q) {
			if _, err := matchAgent(Agent{}, token); err != nil {
				return agentError(422, "bad_input", err.Error())
			}
		}
		agents := []Agent{}
		for _, record := range d.Records {
			agent := a.view(record)
			match, err := matchAgent(agent, q)
			if err != nil {
				return agentError(422, "bad_input", err.Error())
			}
			if match {
				agents = append(agents, agent)
			}
		}
		sortAgents(agents)
		write(w, 200, struct {
			Agents []Agent `json:"agents"`
		}{agents})
		return nil
	}, false)
	if err != nil {
		agentFailure(w, err)
	}
}

func (a *AgentRegistry) create(ctx context.Context, d *agentDisk, body agentMutation) (Agent, <-chan session.Event, error) {
	if a.open == nil {
		return Agent{}, nil, agentError(503, "not_allowed", "connect a model in Settings → Models")
	}
	effort, budget, model := config.AppDefaultEffort, Money{fmt.Sprintf("%.2f", config.AppDefaultAgentBudget)}, ""
	if body.Effort != nil {
		effort = *body.Effort
	}
	if body.Budget != nil {
		budget = *body.Budget
	}
	if body.Model != nil {
		model = *body.Model
	}
	conn, err := a.open(ctx, AgentOpenRequest{Model: model, Effort: effort, Budget: budget, Approvals: body.Approvals})
	if err != nil {
		return Agent{}, nil, agentError(502, "not_allowed", "the agent engine could not be opened")
	}
	cleanup := func() {
		if conn.Close != nil {
			conn.Close()
		}
	}
	if conn.Engine == nil || conn.SessionFile == "" {
		cleanup()
		return Agent{}, nil, errors.New("agent engine has no transcript")
	}
	meta, err := session.LoadMeta(filepath.Dir(conn.SessionFile))
	if err != nil || meta.ID == "" {
		cleanup()
		return Agent{}, nil, errors.New("agent engine has no stable identity")
	}
	if _, exists := d.Records[meta.ID]; exists {
		cleanup()
		return Agent{}, nil, agentError(409, "conflict", "new agent reused an existing transcript")
	}
	now := a.now().UTC()
	agent := Agent{ID: meta.ID, Name: AgentName(body.Text), Model: conn.Engine.Model(), Effort: effort, Budget: budget, State: "idle", Rules: []string{}, Context: body.Context, CreatedAt: now, CreatedBy: "you", Version: 1}
	if agent.Context == nil {
		agent.Context = []AgentContext{}
	}
	var events <-chan session.Event
	if agent.Name != "" {
		if err := conn.Engine.Rename(agent.Name); err != nil {
			cleanup()
			return Agent{}, nil, err
		}
	}
	if body.Start == "send" {
		text := contextMessage(body.Text, body.Context)
		events, err = conn.Engine.Submit(context.Background(), text)
		if err != nil {
			cleanup()
			return Agent{}, nil, agentError(409, "conflict", "the first message was not accepted")
		}
		agent.LastMessageAt, agent.State, agent.Glyph = &now, "running", "running"
	}
	a.live[agent.ID] = &agentLive{conn: conn, running: events != nil, generation: 1}
	record := agentRecord{Agent: agent, SessionFile: conn.SessionFile, Workspace: conn.Workspace, Approvals: body.Approvals}
	if events != nil {
		record.LastTurn = &TurnRequest{Text: body.Text, Mode: "send", Context: body.Context}
		record.TurnSeq = 1
		a.live[agent.ID].turnID = agent.ID + "_1"
	}
	d.Records[agent.ID] = record
	return a.view(d.Records[agent.ID]), events, nil
}

func (a *AgentRegistry) connect(ctx context.Context, record agentRecord) (*agentLive, error) {
	if live := a.live[record.Agent.ID]; live != nil {
		return live, nil
	}
	if a.open == nil {
		return nil, agentError(503, "not_allowed", "the agent engine is unavailable")
	}
	conn, err := a.open(ctx, AgentOpenRequest{SessionFile: record.SessionFile, Workspace: record.Workspace, Model: record.Agent.Model, Effort: record.Agent.Effort, Budget: record.Agent.Budget, Approvals: record.Approvals})
	if err != nil {
		return nil, agentError(502, "not_allowed", "the agent engine could not be opened")
	}
	if conn.Engine == nil || conn.SessionFile != record.SessionFile {
		if conn.Close != nil {
			conn.Close()
		}
		return nil, errors.New("agent engine opened a different transcript")
	}
	live := &agentLive{conn: conn}
	a.live[record.Agent.ID] = live
	return live, nil
}

func (a *AgentRegistry) mutate(ctx context.Context, d *agentDisk, id, action string, body agentMutation) (Agent, []string, error) {
	record, ok := d.Records[id]
	if !ok {
		return Agent{}, nil, errAgentMissing
	}
	agent := a.view(record)
	// Display normalization must not replace a terminal choice during an unrelated edit.
	agent.Effort = record.Agent.Effort
	if expect := body.Expect; expect != nil && ((expect.State != nil && *expect.State != agent.State) || (expect.Version != nil && *expect.Version != agent.Version)) {
		return Agent{}, nil, agentError(409, "stale", "the agent changed; read it again before acting")
	}
	fields := []string{}
	switch action {
	case "":
		if body.Name != nil || body.Model != nil || body.Effort != nil || body.Budget != nil {
			live, err := a.connect(ctx, record)
			if err != nil {
				return Agent{}, nil, err
			}
			if body.Budget != nil {
				usd, _ := strconv.ParseFloat(body.Budget.USD, 64)
				if err := live.conn.Engine.SetSpendRail(usd); err != nil {
					return Agent{}, nil, agentError(409, "conflict", "the agent budget could not be changed")
				}
				agent.Budget = *body.Budget
				fields = append(fields, "budget")
			}
			if body.Name != nil {
				name := strings.TrimSpace(*body.Name)
				if err := live.conn.Engine.Rename(name); err != nil {
					return Agent{}, nil, err
				}
				agent.Name = name
				fields = append(fields, "name")
			}
			if body.Model != nil {
				agent.Model = *body.Model
				live.conn.Engine.SetModel(agent.Model)
				fields = append(fields, "model")
			}
			if body.Effort != nil {
				agent.Effort = *body.Effort
				live.conn.Engine.SetReasoningFor(agent.Model, agent.Effort)
				fields = append(fields, "effort")
			}
		}
		if body.Pinned != nil {
			agent.Pinned = *body.Pinned
			fields = append(fields, "pinned")
		}
	case "close":
		if agent.CrewID != "" {
			return Agent{}, nil, agentError(409, "conflict", "remove this agent from its crew before closing it")
		}
		if agent.State == "archived" {
			return Agent{}, nil, agentError(409, "conflict", "reopen the archived agent before closing it")
		}
		running := a.running(record)
		if (agent.State == "running" || running.Processes+running.Tasks > 0) && !body.StopRunning {
			return Agent{}, nil, &agentReplyError{409, "conflict", "closing this agent stops its running work", &running}
		}
		if body.StopRunning || agent.State == "needs_you" {
			live, err := a.connect(ctx, record)
			if err != nil {
				return Agent{}, nil, err
			}
			if err := live.conn.Engine.StopWork(); err != nil {
				return Agent{}, nil, agentError(409, "conflict", "the running work could not be stopped")
			}
		}
		if live := a.live[id]; live != nil {
			if live.conn.End != nil {
				if err := live.conn.End(); err != nil {
					return Agent{}, nil, agentError(409, "conflict", "the agent engine could not be closed")
				}
			}
			live.generation++
			live.running = false
			if live.conn.Close != nil {
				live.conn.Close()
			}
			delete(a.live, id)
		}
		if agent.State != "closed" {
			now := a.now().UTC()
			agent.ClosedAt = &now
		}
		agent.State = "closed"
		fields = append(fields, "state", "closed_at", "glyph")
	case "archive":
		if agent.State != "closed" && agent.State != "archived" {
			return Agent{}, nil, agentError(409, "conflict", "close this agent before archiving it")
		}
		if err := session.SetArchived(filepath.Dir(record.SessionFile), true); err != nil {
			return Agent{}, nil, err
		}
		if agent.State != "archived" {
			now := a.now().UTC()
			agent.ArchivedAt = &now
		}
		agent.State = "archived"
		fields = append(fields, "state", "archived_at", "glyph")
	case "reopen":
		if agent.State != "closed" && agent.State != "archived" {
			return Agent{}, nil, agentError(409, "conflict", "this agent is already open")
		}
		if err := session.SetArchived(filepath.Dir(record.SessionFile), false); err != nil {
			return Agent{}, nil, err
		}
		agent.State, agent.ClosedAt, agent.ArchivedAt, agent.Receipt = "idle", nil, nil, ""
		fields = append(fields, "state", "closed_at", "archived_at", "glyph")
	}
	agent.Glyph = ""
	if agent.State == "running" || agent.State == "needs_you" || agent.State == "failed" {
		agent.Glyph = agent.State
	}
	if len(fields) > 0 {
		agent.Version++
	}
	record.Agent = agent
	d.Records[id] = record
	// Changing a closed record's name or settings need not retain a child.
	if agent.State == "closed" || agent.State == "archived" {
		if live := a.live[id]; live != nil {
			if live.conn.Close != nil {
				live.conn.Close()
			}
			delete(a.live, id)
		}
	}
	agent.Effort = config.AppEffortWord(agent.Effort)
	return agent, fields, nil
}

func (a *AgentRegistry) running(record agentRecord) agentRunning {
	counts := agentRunning{}
	if presence, ok := session.ReadSessionPresence(filepath.Dir(record.SessionFile), a.now()); ok {
		counts.Processes = len(presence.Jobs)
		counts.Tasks = len(presence.RunningTasks)
	}
	if live := a.live[record.Agent.ID]; live != nil {
		n := 0
		for _, task := range live.conn.Engine.PlanTasks() {
			if task.Status == "running" || task.Status == "claimed" || task.Running > 0 {
				n++
			}
		}
		if n > counts.Tasks {
			counts.Tasks = n
		}
	}
	return counts
}

// Context chips are carried to the engine as explicit references, never as a
// working-directory change. The turn lane later handles attached file bytes.
func contextMessage(text string, chips []AgentContext) string {
	if len(chips) == 0 {
		return text
	}
	var context strings.Builder
	context.WriteString(text)
	context.WriteString("\n\nContext references:")
	for _, chip := range chips {
		context.WriteString("\n")
		context.WriteString(chip.Kind)
		context.WriteString(": ")
		context.WriteString(chip.Path)
	}
	return context.String()
}
