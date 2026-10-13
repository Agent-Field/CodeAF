package appapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	corefactory "github.com/Agent-Field/codeaf/internal/factory"
	factoryapi "github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/session"
)

func decodeFactory(w http.ResponseWriter, r *http.Request, out any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	var object map[string]json.RawMessage
	if err != nil || json.Unmarshal(raw, &object) != nil || object == nil {
		fail(w, 422, "bad_input", "needs one JSON object of at most 64 KiB")
		return false
	}
	for _, v := range object {
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			fail(w, 422, "bad_input", "factory fields cannot be null")
			return false
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil || decoder.Decode(new(any)) != io.EOF {
		fail(w, 422, "bad_input", "invalid factory fields")
		return false
	}
	return true
}
func (s *Server) serveFactory(w http.ResponseWriter, r *http.Request) {
	f := s.options.Factories
	if f == nil {
		fail(w, 503, "not_allowed", "factory connections are unavailable")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		fail(w, 503, "not_allowed", "the engine is stopping")
		return
	}
	if key := r.PathValue("card"); key != "" && !strings.HasSuffix(r.URL.Path, "/send") {
		card := f.cards[key]
		if card == nil || card.Agent != r.PathValue("id") || time.Now().After(card.Expires) {
			fail(w, 404, "not_found", "hand-off card not found")
			return
		}
		if r.Method == "GET" {
			write(w, 200, card.HandOffCard)
			return
		}
		var body ExpectRequest
		if !decodeFactory(w, r, &body) {
			return
		}
		if body.Expect != nil {
			fail(w, 422, "bad_input", "Not now does not take expect")
			return
		}
		if card.Result != nil {
			fail(w, 409, "conflict", "this hand-off has already sent a job")
			return
		}
		delete(f.cards, key)
		write(w, 200, ActionResponse{OK: true})
		return
	}
	if r.Method == "GET" {
		out, err := f.list(r.Context())
		if err != nil {
			agentFailure(w, err)
			return
		}
		write(w, 200, out)
		return
	}
	var err error
	var reply any = ActionResponse{OK: true}
	status := 200
	switch {
	case r.URL.Path == "/app/v1/power":
		var body PowerRequest
		if !decodeFactory(w, r, &body) {
			return
		}
		if body.State != "awake" && body.State != "sleeping" {
			fail(w, 422, "bad_input", "power state must be sleeping or awake")
			return
		}
		if body.Expect != nil {
			fail(w, 422, "bad_input", "power does not take expect")
			return
		}
		if body.State == "sleeping" && s.options.Agents != nil {
			err = s.options.Agents.sleepFactoryProcesses()
		}
		if err == nil && f.Power != nil {
			if powerErr := f.Power(r.Context(), body.State == "sleeping"); powerErr != nil {
				err = agentError(503, "not_allowed", "factory runner is unavailable; retry this power event")
			}
		}
	case r.URL.Path == "/app/v1/factories/solo":
		var body ExpectRequest
		if !decodeFactory(w, r, &body) {
			return
		}
		if body.Expect != nil {
			fail(w, 422, "bad_input", "factory creation does not take expect")
			return
		}
		row := factoryConnection{"solo", f.URL, "solo"}
		if f.handler == nil {
			if f.Solo == nil {
				fail(w, 503, "not_allowed", "solo factory host is unavailable")
				return
			}
			var handler http.Handler
			handler, err = f.Solo(r.Context())
			f.setHandler(handler)
		}
		if err == nil {
			_, err = f.connections(func(saved *factoryConnections) error {
				for _, old := range saved.Factories {
					if old.ID == "solo" {
						return nil
					}
				}
				saved.Factories = append(saved.Factories, row)
				return nil
			})
		}
		if err == nil {
			s.SetInboxSource(row.ID, f.inboxSource(row))
			f.publish(row, "ok")
			reply = f.view(r.Context(), row)
			status = 201
		}
	case r.URL.Path == "/app/v1/factories" && r.Method == "POST":
		var body AttachFactoryRequest
		if !decodeFactory(w, r, &body) {
			return
		}
		if body.Expect != nil {
			fail(w, 422, "bad_input", "attachment does not take expect")
			return
		}
		address, e := factoryOrigin(body.URL)
		err = e
		row := factoryConnection{factoryID(address), address, "team"}
		if err == nil && address == f.URL {
			err = agentError(422, "bad_input", "the solo factory cannot be attached as a team")
		}
		if err == nil {
			_, err = f.connections(func(saved *factoryConnections) error {
				for _, old := range saved.Factories {
					if old.ID == row.ID {
						return nil
					}
				}
				saved.Factories = append(saved.Factories, row)
				return nil
			})
		}
		if err == nil {
			s.SetInboxSource(row.ID, f.inboxSource(row))
			reply = f.view(r.Context(), row)
			f.publish(row, reply.(Factory).State)
			status = 201
		}
	case r.Method == "DELETE":
		var body ExpectRequest
		if !decodeFactory(w, r, &body) {
			return
		}
		if body.Expect != nil {
			fail(w, 422, "bad_input", "Leave does not take expect")
			return
		}
		var removed factoryConnection
		_, err = f.connections(func(saved *factoryConnections) error {
			for i, row := range saved.Factories {
				if row.ID == r.PathValue("id") {
					if row.Mode == "solo" {
						return agentError(409, "not_allowed", "a solo factory belongs to this machine")
					}
					removed = row
					saved.Factories = append(saved.Factories[:i], saved.Factories[i+1:]...)
					return nil
				}
			}
			return agentError(404, "not_found", "factory not found")
		})
		if err == nil {
			s.SetInboxSource(removed.ID, nil)
			if f.Credentials != nil {
				f.Credentials.SetFactorySession(removed.URL, "")
			}
			f.publish(removed, "detached")
		}
	case strings.HasSuffix(r.URL.Path, "/send"):
		var body SendHandOffRequest
		if !decodeFactory(w, r, &body) {
			return
		}
		reply, err = f.send(r.Context(), r.PathValue("id"), r.PathValue("card"), body)
	default:
		var body ExpectRequest
		if !decodeFactory(w, r, &body) {
			return
		}
		reply, err = f.card(r.Context(), r.PathValue("id"), r.PathValue("tid"), body.Expect)
		status = 201
	}
	if err != nil {
		agentFailure(w, err)
		return
	}
	if f.Changed != nil {
		f.Changed()
	}
	write(w, status, reply)
}

func (f *FactoryRegistry) card(ctx context.Context, id, task string, expect *AgentExpect) (HandOffCard, error) {
	var title, brief, repo string
	var taskVersion uint64
	var taskState string
	if task != "" {
		var snapshot TaskResponse
		var err error
		if f.Tasks != nil {
			snapshot, err = f.Tasks.FactoryTask(ctx, task)
		} else {
			err = f.readResource(ctx, "/app/v1/tasks/"+task, &snapshot)
		}
		if err != nil {
			return HandOffCard{}, err
		}
		id = snapshot.AgentID
		title = snapshot.Title
		brief = snapshot.Brief
		taskVersion = snapshot.Version
		taskState = snapshot.State
		if snapshot.State == "done" || snapshot.State == "stopped" || snapshot.State == "declined" || snapshot.State == "handed_off" {
			return HandOffCard{}, agentError(409, "conflict", "that task has finished")
		}
	}
	a := f.server.options.Agents
	if a == nil {
		return HandOffCard{}, agentError(503, "not_allowed", "agent registry unavailable")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var version uint64
	err := a.disk(func(d *agentDisk) error {
		record, ok := d.Records[id]
		if !ok {
			return errAgentMissing
		}
		agent := a.view(record)
		if err := turnExpectation(agent, expect); err != nil {
			return err
		}
		if agent.State == "closed" || agent.State == "archived" {
			return agentError(409, "conflict", "reopen this agent before handing work to the factory")
		}
		version = agent.Version
		repo = record.Workspace
		if title == "" {
			title = agent.Name
		}
		if brief == "" && record.LastTurn != nil {
			brief = record.LastTurn.Text
		}
		return nil
	}, false)
	if err != nil {
		return HandOffCard{}, err
	}
	rows, err := f.connections(nil)
	if err != nil {
		return HandOffCard{}, err
	}
	if len(rows) == 0 {
		return HandOffCard{}, agentError(409, "no_factory", "start or attach a factory first")
	}
	factories, err := f.list(ctx)
	if err != nil {
		return HandOffCard{}, err
	}
	token, err := Token()
	if err != nil {
		return HandOffCard{}, err
	}
	selected := rows[0].ID
	assignee := ""
	// A watched repository selects its factory; the picker remains authoritative.
	for _, row := range rows {
		var queue factoryapi.Queue
		if f.source(row).request(ctx, "GET", "/v1/queue", nil, &queue) != nil {
			continue
		}
		if assignee == "" {
			assignee = queue.Me
		}
		for _, watched := range queue.Repos {
			if !watched.On && !watched.Watched {
				continue
			}
			if watched.Checkout == repo || watched.Name == repo {
				selected = row.ID
				repo = watched.Name
				assignee = queue.Me
				break
			}
		}
	}
	card := HandOffCard{ID: token, Factory: selected, Factories: factories, Title: title, Brief: brief, Repo: repo, Pipeline: "default", Start: "backlog", Assignee: assignee}
	for key, old := range f.cards {
		if time.Now().After(old.Expires) {
			delete(f.cards, key)
		}
	}
	if len(f.cards) >= 256 {
		return HandOffCard{}, agentError(409, "at_cap", "send or reopen a hand-off card")
	}
	f.cards[token] = &factoryCard{HandOffCard: card, Agent: id, Task: task, Version: version, TaskVersion: taskVersion, TaskState: taskState, Expires: time.Now().Add(time.Hour)}
	return card, nil
}
func (f *FactoryRegistry) readResource(ctx context.Context, path string, out any) error {
	request, _ := http.NewRequestWithContext(inboxForwardContext(ctx), "GET", path, nil)
	recorder := newFactoryRecorder()
	f.server.mux.ServeHTTP(recorder, request)
	if recorder.status >= 300 {
		return agentError(recorder.status, "not_allowed", "the originating task is unavailable")
	}
	if json.Unmarshal(recorder.body.Bytes(), out) != nil {
		return errors.New("invalid task snapshot")
	}
	return nil
}

type factoryRecorder struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newFactoryRecorder() *factoryRecorder {
	return &factoryRecorder{header: http.Header{}, status: 200}
}
func (w *factoryRecorder) Header() http.Header            { return w.header }
func (w *factoryRecorder) WriteHeader(status int)         { w.status = status }
func (w *factoryRecorder) Write(data []byte) (int, error) { return w.body.Write(data) }
func (f *FactoryRegistry) send(ctx context.Context, id, key string, body SendHandOffRequest) (HandOffResponse, error) {
	card := f.cards[key]
	if card == nil || card.Agent != id || time.Now().After(card.Expires) {
		return HandOffResponse{}, agentError(404, "not_found", "hand-off card not found")
	}
	if card.Complete {
		return *card.Result, nil
	}
	edited := card.HandOffCard
	if body.Factory != nil {
		edited.Factory = *body.Factory
	}
	if body.Title != nil {
		edited.Title = *body.Title
	}
	if body.Brief != nil {
		edited.Brief = *body.Brief
	}
	if body.Repo != nil {
		edited.Repo = *body.Repo
	}
	if body.Pipeline != nil {
		edited.Pipeline = *body.Pipeline
	}
	if body.Issue != nil {
		edited.Issue = *body.Issue
	}
	if body.Start != nil {
		edited.Start = *body.Start
	}
	if body.Assignee != nil {
		edited.Assignee = *body.Assignee
	}
	if strings.TrimSpace(edited.Title) == "" || strings.TrimSpace(edited.Repo) == "" || (edited.Start != "now" && edited.Start != "backlog") {
		return HandOffResponse{}, agentError(422, "bad_input", "hand-off needs a title, repository and now or backlog")
	}
	rows, err := f.connections(nil)
	if err != nil {
		return HandOffResponse{}, err
	}
	var source *FactoryInboxSource
	for _, row := range rows {
		if row.ID == edited.Factory {
			source = f.source(row)
		}
	}
	if source == nil {
		return HandOffResponse{}, agentError(404, "not_found", "factory not found")
	}
	a := f.server.options.Agents
	a.mu.Lock()
	defer a.mu.Unlock()
	var result HandOffResponse
	err = a.disk(func(d *agentDisk) error {
		record, ok := d.Records[id]
		if !ok {
			return errAgentMissing
		}
		agent := a.view(record)
		expected := body.Expect
		if expected == nil {
			version := card.Version
			expected = &AgentExpect{Version: &version}
		}
		if err := turnExpectation(agent, expected); err != nil {
			return err
		}
		live, err := a.connect(ctx, record)
		if err != nil {
			return err
		}
		var cancelTask func() error
		if card.Task != "" && f.Tasks == nil {
			owner, local, err := taskResourceParts(card.Task)
			if err != nil || owner != id {
				return agentError(404, "not_found", "originating task not found")
			}
			engine, ok := live.conn.Engine.(TaskEngine)
			if !ok {
				return agentError(409, "not_allowed", "this engine cannot read the originating task")
			}
			current, found, err := engine.AppTask(local)
			if err != nil {
				return err
			}
			if !found || (card.Result == nil && (current.Version != card.TaskVersion || current.State != card.TaskState)) {
				return agentError(409, "stale", "that task changed; reopen the hand-off card")
			}
			if current.State == "proposed" || current.State == "held" {
				cancelTask = func() error { return engine.AppTaskAction(session.AppTaskIntent{ID: local, Action: "decline"}) }
			} else if _, err := strconv.ParseUint(local, 10, 64); err == nil {
				if door, ok := live.conn.Engine.(interface {
					CancelWithReason(string, string) (string, error)
				}); ok {
					cancelTask = func() error { _, err := door.CancelWithReason("task:"+local, "Handed to the factory"); return err }
				}
			} else if door, ok := live.conn.Engine.(interface{ PlanCancelWithReason(string, string) error }); ok {
				cancelTask = func() error { return door.PlanCancelWithReason(local, "Handed to the factory") }
			}
			if cancelTask == nil {
				return agentError(409, "not_allowed", "this engine cannot hand off a task")
			}
		}
		if card.Result == nil {
			var job struct {
				ID      int                `json:"id"`
				Factory factoryapi.Factory `json:"factory"`
			}
			title := agent.Name
			if title == "" {
				title = edited.Title
			}
			body := factoryapi.NewJob{Repo: edited.Repo, Title: edited.Title, Text: edited.Brief, Source: "hand-off", FromRef: &corefactory.SourceRef{Kind: "agent", ID: id, Title: title}, Assignee: &edited.Assignee, Start: edited.Start, Pipeline: edited.Pipeline, Issue: edited.Issue}
			if err := factoryCreateRequest(ctx, source, key, body, &job); err != nil {
				return err
			}
			var detail factoryapi.Job
			if err := source.request(ctx, "GET", "/v1/jobs/"+jsonNumber(job.ID), nil, &detail); err != nil || detail.JobID == "" {
				detail.JobID = jsonNumber(job.ID)
			}
			result = HandOffResponse{Factory: edited.Factory, Job: detail.JobID}
			card.Result = &result
			card.HandOffCard = edited
		} else {
			result = *card.Result
		}
		if card.Task != "" && f.Tasks != nil {
			cancelTask = func() error {
				return f.Tasks.HandedToFactory(ctx, card.Task, SourceRef{Kind: "job", ID: result.Job, Factory: result.Factory})
			}
		}

		if cancelTask != nil {
			err = cancelTask()
		} else {
			err = live.conn.Engine.StopWork()
		}
		if err != nil {
			return err
		}
		if card.Task == "" {
			a.invalidateTurn(live)
			record.Queue = nil
		}
		if card.Task == "" {
			record.Agent.State = "idle"
		}
		record.Agent.Version++
		receipt := a.turnReceipt(&record, "Handed to the factory", &SourceRef{Kind: "job", ID: result.Job, Factory: result.Factory})
		record.Agent.Receipt = receipt.Text
		d.Records[id] = record
		a.publish(EventAgentChanged, a.view(record), "state", "receipt")
		return nil
	}, true)
	if err == nil {
		card.Complete = true
	}
	return result, err
}
func jsonNumber(id int) string { raw, _ := json.Marshal(id); return string(raw) }

// A card is the idempotency identity at both HTTP boundaries, including a
// lost factory response after its job was already committed.
func factoryCreateRequest(ctx context.Context, source *FactoryInboxSource, key string, body, out any) error {
	clone := FactoryInboxSource{ID: source.ID, URL: source.URL, Token: source.Token}
	client := source.Client
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		defer transport.CloseIdleConnections()
		client = &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	copyClient := *client
	transport := copyClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	copyClient.Transport = factoryKeyTransport{base: transport, key: key}
	clone.Client = &copyClient
	return clone.request(ctx, "POST", "/v1/jobs", body, out)
}

type factoryKeyTransport struct {
	base http.RoundTripper
	key  string
}

func (t factoryKeyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header.Set("Idempotency-Key", "hand-off-"+t.key)
	return t.base.RoundTrip(copy)
}

// Sleep receipts sit beside the processes that ended, and the normal process
// observer still publishes exactly one end event for each of them.
func (a *AgentRegistry) sleepFactoryProcesses() error {
	a.mu.Lock()
	running := map[string][]string{}
	for id, live := range a.live {
		if door, ok := live.conn.Engine.(processListDoor); ok {
			rows, err := door.JobNotices()
			if err != nil {
				a.mu.Unlock()
				return err
			}
			for _, row := range rows {
				if string(row.State) == "running" {
					running[id] = append(running[id], processID(id, row.ID))
				}
			}
		}
	}
	a.mu.Unlock()
	if err := a.StopAllProcesses(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.disk(func(d *agentDisk) error {
		for id, processes := range running {
			record, ok := d.Records[id]
			if !ok {
				continue
			}
			for _, process := range processes {
				a.turnReceipt(&record, "stopped while this Mac slept · Rerun", &SourceRef{Kind: "process", ID: process})
			}
			d.Records[id] = record
		}
		return nil
	}, true)
}
