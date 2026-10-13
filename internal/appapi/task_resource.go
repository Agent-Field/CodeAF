package appapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
)

// TaskEngine is an optional extension of the canonical agent connection. Fake
// engines implement this door without depending on another resource's handlers.
type TaskEngine interface {
	AppTasks() ([]session.AppTaskView, error)
	AppTask(string) (session.AppTaskView, bool, error)
	AppTaskAction(session.AppTaskIntent) error
	AppQuickTasks() ([]session.AppQuickTaskView, error)
	AppQuickTaskAction(session.AppTaskIntent) ([]string, []string, error)
}

// Resource ids include their agent, so task 1 in two conversations never aliases.
func taskResourceID(agent, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(agent + "\x00" + id))
}
func taskResourceParts(id string) (string, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", "", errors.New("no such task")
	}
	agent, local, ok := strings.Cut(string(raw), "\x00")
	if !ok || agent == "" || local == "" || strings.ContainsRune(local, 0) {
		return "", "", errors.New("no such task")
	}
	return agent, local, nil
}

func (s *Server) taskEngine(w http.ResponseWriter, r *http.Request, id string) (TaskEngine, bool) {
	a := s.options.Agents
	if a == nil {
		fail(w, 503, "not_allowed", "the agent registry is unavailable")
		return nil, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		fail(w, 503, "not_allowed", "the engine is stopping")
		return nil, false
	}
	var live *agentLive
	err := a.disk(func(d *agentDisk) error {
		record, found := d.Records[id]
		if !found {
			return errAgentMissing
		}
		var err error
		live, err = a.connect(r.Context(), record)
		return err
	}, false)
	if err != nil {
		agentFailure(w, err)
		return nil, false
	}
	engine, ok := live.conn.Engine.(TaskEngine)
	if !ok {
		fail(w, 503, "not_allowed", "this engine cannot serve app tasks")
		return nil, false
	}
	s.watchResourceTasks(id, live, engine)
	return engine, true
}

// The standing engine lane carries changes that happen after an HTTP call ends.
// Closing the registry releases the subscription, including a joined TUI engine.
func (s *Server) watchResourceTasks(agent string, live *agentLive, engine TaskEngine) {
	if live.taskStop != nil {
		return
	}
	watcher, ok := live.conn.Engine.(interface {
		WatchTaskUpdates() (<-chan session.Event, func())
	})
	if !ok {
		return
	}
	lane, stop := watcher.WatchTaskUpdates()
	live.taskStop = stop
	go func() {
		known := map[string]bool{}
		for event := range lane {
			if event.Task == nil {
				continue
			}
			id := fmt.Sprint(event.Task.ID)
			if event.Task.Kind == session.TaskKindQuick {
				rows, err := engine.AppQuickTasks()
				if err != nil {
					continue
				}
				for _, row := range rows {
					if row.ID == id {
						_, _ = s.events.Publish(EventQuickTaskChanged, QuickTaskChangedEvent{agent, taskResourceID(agent, id), row.State})
					}
				}
			} else {
				row, found, err := engine.AppTask(id)
				if err != nil || !found {
					continue
				}
				kind := EventTaskChanged
				if !known[id] {
					kind = EventTaskCreated
					known[id] = true
				}
				_, _ = s.events.Publish(kind, TaskChangedEvent{Agent: agent, Task: taskResourceID(agent, id), State: row.State, Clock: taskClock(row).State, Step: &row.Step})
			}
		}
	}()
}

func taskClock(row session.AppTaskView) TaskClock {
	clock := TaskClock{State: "off"}
	if row.Held {
		clock.State = "held"
	} else if !row.Deadline.IsZero() {
		clock.State = "running"
		clock.StartsAt = &row.Deadline
	}
	return clock
}
func appTask(agent string, row session.AppTaskView) Task {
	task := Task{Version: row.Version, ID: taskResourceID(agent, row.ID), AgentID: agent, Title: row.Title, Brief: row.Brief, State: row.State, WaitsOn: []string{}, Clock: taskClock(row), Model: row.Model, Cap: Money{fmt.Sprintf("%.2f", row.Cap)}, Step: row.Step, Files: []FileChange{}, Notes: []TaskNote{}, Origin: row.Origin}
	if row.Parent != "" {
		task.ParentID = taskResourceID(agent, row.Parent)
	}
	for _, id := range row.Waits {
		task.WaitsOn = append(task.WaitsOn, taskResourceID(agent, id))
	}
	for _, path := range row.Files {
		task.Files = append(task.Files, FileChange{Path: path})
	}
	for _, note := range row.Notes {
		task.Notes = append(task.Notes, TaskNote{Text: note.Text, ReadAtStep: note.ReadAtStep})
	}
	if !row.Deadline.IsZero() {
		task.StartsAt = &row.Deadline
	}
	if row.Spent > 0 {
		task.Spent = &Money{fmt.Sprintf("%.2f", row.Spent)}
	}
	if row.Elapsed > 0 {
		task.Elapsed = &row.Elapsed
	}
	return task
}

func (s *Server) taskRequest(w http.ResponseWriter, r *http.Request) {
	agent, local := r.PathValue("id"), ""
	if agent == "" {
		var err error
		agent, local, err = taskResourceParts(r.PathValue("tid"))
		if err != nil {
			fail(w, 404, "not_found", err.Error())
			return
		}
	}
	engine, ok := s.taskEngine(w, r, agent)
	if !ok {
		return
	}
	if r.Method == "GET" {
		if local == "" {
			rows, err := engine.AppTasks()
			if err != nil {
				taskFailure(w, err)
				return
			}
			response := taskTreeFromRows(agent, rows)
			write(w, 200, response)
			return
		}
		row, found, err := engine.AppTask(local)
		if err != nil {
			taskFailure(w, err)
			return
		}
		if !found {
			fail(w, 404, "not_found", "no such task")
			return
		}
		response := TaskResponse{Task: appTask(agent, row), Result: row.Result, WorkLog: []DisplayEntry{}}
		for _, line := range row.WorkLog {
			response.WorkLog = append(response.WorkLog, DisplayEntry{Kind: "tool", Text: line})
		}
		write(w, 200, response)
		return
	}
	action := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	intent, ok := decodeTaskAction(w, r, action)
	if !ok {
		return
	}
	intent.ID = local
	if err := engine.AppTaskAction(intent); err != nil {
		taskFailure(w, err)
		return
	}
	row, found, err := engine.AppTask(local)
	if err == nil && found {
		_, _ = s.events.Publish(EventTaskChanged, TaskChangedEvent{Agent: agent, Task: taskResourceID(agent, local), State: row.State, Clock: taskClock(row).State})
	}
	if action == "note" || action == "brief" {
		write(w, 200, TaskNote{Text: intent.Text})
		return
	}
	write(w, 200, ActionResponse{OK: true, Receipt: taskActionReceipt(action, intent.Verb, taskResourceID(agent, local))})
}

func decodeTaskAction(w http.ResponseWriter, r *http.Request, action string) (session.AppTaskIntent, bool) {
	var raw map[string]json.RawMessage
	if !decodeObject(w, r, &raw) {
		return session.AppTaskIntent{}, false
	}
	allowed := " expect "
	if action == "note" || action == "brief" {
		allowed += "text "
	}
	if action == "model" {
		allowed += "model "
	}
	if action == "decide" {
		allowed += "verb words "
	}
	for field, value := range raw {
		if !strings.Contains(allowed, " "+field+" ") || string(value) == "null" {
			fail(w, 422, "bad_input", "invalid field: "+field)
			return session.AppTaskIntent{}, false
		}
	}
	var body struct {
		Text   string       `json:"text"`
		Model  string       `json:"model"`
		Verb   string       `json:"verb"`
		Words  string       `json:"words"`
		Expect *AgentExpect `json:"expect"`
	}
	encoded, _ := json.Marshal(raw)
	strict := json.NewDecoder(bytes.NewReader(encoded))
	strict.DisallowUnknownFields()
	if strict.Decode(&body) != nil {
		fail(w, 422, "bad_input", "invalid action fields")
		return session.AppTaskIntent{}, false
	}
	intent := session.AppTaskIntent{Action: action, Text: strings.TrimSpace(body.Text), Model: strings.TrimSpace(body.Model), Verb: body.Verb}
	if body.Expect != nil {
		intent.State = body.Expect.State
		intent.Version = body.Expect.Version
	}
	if action == "decide" {
		intent.Text = strings.TrimSpace(body.Words)
		if body.Verb != "accept" && body.Verb != "not_right" && body.Verb != "tell" && body.Verb != "recheck" && body.Verb != "let_it_decide" {
			fail(w, 422, "bad_input", "unknown your call verb")
			return intent, false
		}
	}
	if ((action == "note" || action == "brief" || body.Verb == "tell") && intent.Text == "") || (action == "model" && intent.Model == "") {
		fail(w, 422, "bad_input", "words required")
		return intent, false
	}
	return intent, true
}
func taskFailure(w http.ResponseWriter, err error) {
	status, reason := 409, "conflict"
	if strings.Contains(err.Error(), "stale:") {
		reason = "stale"
	}
	if strings.Contains(err.Error(), "no such") {
		status, reason = 404, "not_found"
	}
	fail(w, status, reason, err.Error())
}

// A receipt names the act taken, without promising the task's next outcome.
func taskActionReceipt(action, verb, id string) *DisplayEntry {
	lines := map[string]string{"start": "Started by you", "decline": "Declined by you", "stop": "Stopped by you", "pause": "Paused by you", "resume": "Resumed by you"}
	text := lines[action]
	if action == "decide" {
		text = map[string]string{"accept": "Accepted by you", "not_right": "Sent back by you", "tell": "Your instructions were sent", "recheck": "Recheck requested", "let_it_decide": "codeaf will decide"}[verb]
	}
	if text == "" {
		return nil
	}
	return &DisplayEntry{Kind: "note", Text: text, Link: &SourceRef{Kind: "task", ID: id}}
}

// The snapshot and task panel share one codec and therefore one set of ids.
func taskTreeFromRows(agent string, rows []session.AppTaskView) TasksResponse {
	response := TasksResponse{Groups: []TaskGroup{}, Tasks: []Task{}, Progress: TaskProgress{Total: len(rows)}}
	spent := 0.0
	for _, row := range rows {
		response.Tasks = append(response.Tasks, appTask(agent, row))
		if row.State == "done" {
			response.Progress.Done++
		}
		response.Steps += row.Step
		spent += row.Spent
	}
	for _, row := range rows {
		ids := []string{}
		for _, child := range rows {
			if child.Parent == row.ID {
				ids = append(ids, taskResourceID(agent, child.ID))
			}
		}
		if len(ids) > 0 {
			response.Groups = append(response.Groups, TaskGroup{ID: taskResourceID(agent, row.ID), Title: row.Title, TaskIDs: ids})
		}
	}
	if spent > 0 {
		response.Spent = &Money{fmt.Sprintf("%.2f", spent)}
	}
	return response
}
