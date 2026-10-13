package appapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
)

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var turnContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/turn", Request: "TurnRequest", Response: "TurnResponse"}, handle: (*Server).turnHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/queue/{qid}", Request: "TextRequest", Response: "ActionResponse"}, handle: (*Server).turnHandler},
	{RouteContract: RouteContract{Method: "DELETE", Path: "/app/v1/agents/{id}/queue/{qid}", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).turnHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/stop", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).turnHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/retry", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).turnHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/switch-model", Request: "ModelRequest", Response: "ActionResponse"}, handle: (*Server).turnHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/move-reply-to-task", Request: "ExpectRequest", Response: "MoveReplyResponse"}, handle: (*Server).turnHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/agents/{id}/entries", Request: "", Response: "EntriesResponse"}, handle: (*Server).turnHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/agents/{id}/tools/{call_id}", Request: "", Response: "ToolResponse"}, handle: (*Server).turnHandler},
}

// Turn mutations share the registry lock with lifecycle mutations and stream
// completion. Pending messages belong to this binary, so editing a pending row
// and admitting its turn have the same atomic boundary.
func (s *Server) turnHandler(w http.ResponseWriter, r *http.Request) {
	if values := r.Header.Values("Idempotency-Key"); len(values) > 1 {
		fail(w, 422, "bad_input", "use one Idempotency-Key")
		return
	}
	s.idempotent(s.handleTurn)(w, r)
}

func (s *Server) handleTurn(w http.ResponseWriter, r *http.Request) {
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
	id := r.PathValue("id")
	action := strings.TrimPrefix(r.URL.Path, "/app/v1/agents/"+id+"/")
	var request turnMutation
	if r.Method != "GET" && !decodeTurn(w, r, &request, action) {
		return
	}
	var result any
	var launch <-chan session.Event
	var changed Agent
	var queueChanged bool
	var ended *TurnLifecycleEvent
	var taskCreated string
	err := a.disk(func(d *agentDisk) error {
		record, ok := d.Records[id]
		if !ok {
			return errAgentMissing
		}
		agent := a.view(record)
		if r.Method == "GET" {
			switch {
			case action == "entries":
				entries := a.turnEntries(record)
				limit := turnTailEntries
				if value := r.URL.Query().Get("limit"); value != "" {
					n, e := strconv.Atoi(value)
					if e != nil || n < 1 || n > turnPageEntries {
						return agentError(422, "bad_input", "limit must be 1 through 500")
					}
					limit = n
				}
				end := len(entries)
				if before := r.URL.Query().Get("before"); before != "" {
					n, e := strconv.Atoi(strings.TrimPrefix(before, "entry_"))
					if !strings.HasPrefix(before, "entry_") || e != nil || n < 0 || n > end {
						return agentError(422, "bad_input", "invalid entries cursor")
					}
					end = n
				}
				start := max(0, end-limit)
				reply := EntriesResponse{Entries: entries[start:end], HasMore: start > 0}
				if start > 0 {
					reply.Before = fmt.Sprintf("entry_%d", start)
				}
				result = reply
			case strings.HasPrefix(action, "tools/"):
				output, e := a.turnTool(record, r.PathValue("call_id"))
				if e != nil {
					return e
				}
				result = output
			}
			return nil
		}
		if e := turnExpectation(agent, request.Expect); e != nil {
			return e
		}
		if agent.State == "closed" || agent.State == "archived" {
			return agentError(409, "conflict", "reopen this agent before sending work")
		}
		if existing := a.live[id]; (existing == nil || !existing.running) && (agent.State == "running" || agent.State == "needs_you") {
			return agentError(409, "conflict", "this reply is running in another surface")
		}
		live, e := a.connect(r.Context(), record)
		if e != nil {
			return e
		}
		switch {
		case action == "turn":
			ask := request.TurnRequest
			if ask.Mode == "send" && agent.State == "running" || ask.Mode == "send" && agent.State == "needs_you" {
				return agentError(409, "conflict", "work is running; steer or queue explicitly")
			}
			if ask.Mode == "steer" {
				if !live.running {
					return agentError(409, "conflict", "there is no running reply to steer")
				}
				door, ok := live.conn.Engine.(interface {
					Steer(string) (<-chan session.Event, error)
				})
				if !ok {
					return agentError(409, "not_allowed", "this engine cannot steer a reply")
				}
				stream, e := door.Steer(turnText(ask))
				if e != nil {
					return agentError(409, "conflict", e.Error())
				}
				if stream != live.primary {
					go drainTurn(stream)
				}
				if record.LastTurn != nil {
					record.LastTurn.Text += "\n\n" + ask.Text
					record.LastTurn.Context = append(record.LastTurn.Context, ask.Context...)
				}
				result = TurnResponse{true, live.turnID}
			} else if ask.Mode == "queue" && live.running {
				record.TurnSeq++
				qid := fmt.Sprintf("%s_%d", id, record.TurnSeq)
				record.Queue = append(record.Queue, QueueItem{ID: qid, Text: ask.Text, Context: ask.Context, Files: ask.Files})
				queueChanged = true
				result = TurnResponse{true, qid}
			} else {
				launch, e = a.beginTurn(&record, live, ask, "")
				if e != nil {
					return e
				}
				result = TurnResponse{true, live.turnID}
			}
			now := a.now().UTC()
			record.Agent.LastMessageAt = &now
			record.Agent.Context = append(record.Agent.Context, ask.Context...)
		case strings.HasPrefix(action, "queue/"):
			pos := -1
			for i, item := range record.Queue {
				if item.ID == r.PathValue("qid") {
					pos = i
					break
				}
			}
			if pos < 0 {
				return agentError(409, "conflict", "that message has already been sent")
			}
			if r.Method == "DELETE" {
				record.Queue = append(record.Queue[:pos], record.Queue[pos+1:]...)
			} else {
				record.Queue[pos].Text = request.Text
			}
			queueChanged = true
			result = ActionResponse{OK: true}
		case action == "stop":
			if e := live.conn.Engine.StopWork(); e != nil {
				return agentError(409, "conflict", e.Error())
			}
			receipt := a.turnReceipt(&record, "Stopped by you", nil)
			if live.running {
				ended = &TurnLifecycleEvent{Agent: id, Turn: live.turnID, How: "stopped", Receipt: &receipt}
			}
			a.invalidateTurn(live)
			record.Agent.State = "idle"
			record.Agent.Receipt = receipt.Text
			queueChanged = len(record.Queue) > 0
			record.Queue = nil
			result = ActionResponse{OK: true, Receipt: &receipt}
		case action == "retry":
			if agent.State != "failed" || record.LastTurn == nil || live.running {
				return agentError(409, "conflict", "there is no failed reply to retry")
			}
			launch, e = a.beginTurn(&record, live, *record.LastTurn, "")
			if e != nil {
				return e
			}
			result = ActionResponse{OK: true}
		case action == "switch-model":
			if agent.State != "failed" || live.running {
				return agentError(409, "conflict", "switch model requires a failed reply")
			}
			live.conn.Engine.SetModel(request.Model)
			record.Agent.Model = request.Model
			result = ActionResponse{OK: true}
		case action == "move-reply-to-task":
			if !live.running || record.LastTurn == nil {
				return agentError(409, "conflict", "there is no running reply to move")
			}
			door, ok := live.conn.Engine.(interface {
				StartTask(context.Context, string, bool) (uint64, string, string, error)
			})
			if !ok {
				return agentError(409, "not_allowed", "this engine cannot move a reply to a task")
			}
			// Interrupt cuts only this reply. StopWork also closes task admission and
			// cancels existing tasks, so it cannot be used for a handoff.
			interrupt, ok := live.conn.Engine.(interface{ Interrupt() })
			if !ok {
				return agentError(409, "not_allowed", "this engine cannot interrupt a reply for handoff")
			}
			interrupt.Interrupt()
			a.invalidateTurn(live)
			record.Agent.State = "idle"
			task, _, _, e := door.StartTask(context.Background(), a.moveBrief(record), true)
			if e != nil || task == 0 {
				record.Agent.State = "failed"
				record.Agent.Receipt = "The reply stopped; moving it to a task failed"
				receipt := a.turnReceipt(&record, record.Agent.Receipt, nil)
				ended = &TurnLifecycleEvent{Agent: id, Turn: live.turnID, How: "failed", Receipt: &receipt}
				record.Agent.Version++
				d.Records[id] = record
				changed = record.Agent
				result = turnFailedMove{Error: record.Agent.Receipt, Reason: "conflict"}
				return nil
			}
			taskCreated = strconv.FormatUint(task, 10)
			receipt := a.turnReceipt(&record, "Moved to a task · the chat is free again", &SourceRef{Kind: "task", ID: taskCreated})
			ended = &TurnLifecycleEvent{Agent: id, Turn: live.turnID, How: "stopped", Receipt: &receipt}
			record.Agent.Receipt = receipt.Text
			result = MoveReplyResponse{TaskID: taskCreated}
		}
		record.Agent.Version++
		d.Records[id] = record
		changed = record.Agent
		return nil
	}, r.Method != "GET")
	if err != nil {
		// A failed save cannot leave accepted work running without its receipt.
		if launch != nil {
			live := a.live[id]
			_ = live.conn.Engine.StopWork()
			a.invalidateTurn(live)
		}
		agentFailure(w, err)
		return
	}
	if changed.ID != "" {
		a.publish(EventAgentChanged, changed, "state", "glyph", "name", "model", "last_message_at")
	}
	if queueChanged {
		_ = a.disk(func(d *agentDisk) error {
			a.publishTurn(EventQueueChanged, QueueChangedEvent{id, copyTurnQueue(d.Records[id].Queue)})
			return nil
		}, false)
	}
	if ended != nil {
		a.publishTurn(EventTurnEnded, *ended)
		a.publishTurn(EventTurnEntry, TurnEntryEvent{Agent: id, Entry: *ended.Receipt})
	}
	if taskCreated != "" {
		a.publishTurn(EventTaskCreated, TaskChangedEvent{Agent: id, Task: taskCreated, State: "running"})
	}
	if launch != nil {
		a.startTurnPump(id, a.live[id], launch)
	}
	if failed, ok := result.(turnFailedMove); ok {
		fail(w, 409, failed.Reason, failed.Error)
		return
	}
	write(w, 200, result)
}

type turnFailedMove struct{ Error, Reason string }
type turnMutation struct {
	TurnRequest
	Model string `json:"model"`
}

func decodeTurn(w http.ResponseWriter, r *http.Request, body *turnMutation, action string) bool {
	var raw map[string]json.RawMessage
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if e := dec.Decode(&raw); e != nil || raw == nil {
		fail(w, 422, "bad_input", "this action needs one JSON object")
		return false
	}
	if e := dec.Decode(new(any)); !errors.Is(e, io.EOF) {
		fail(w, 422, "bad_input", "this action needs one JSON object")
		return false
	}
	allowed := "expect"
	if action == "turn" {
		allowed += " text mode context files"
	}
	if strings.HasPrefix(action, "queue/") && r.Method == "POST" {
		allowed += " text"
	}
	if action == "switch-model" {
		allowed += " model"
	}
	for key, value := range raw {
		if !strings.Contains(" "+allowed+" ", " "+key+" ") || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			fail(w, 422, "bad_input", "invalid field: "+key)
			return false
		}
	}
	encoded, _ := json.Marshal(raw)
	strict := json.NewDecoder(bytes.NewReader(encoded))
	strict.DisallowUnknownFields()
	if e := strict.Decode(body); e != nil {
		fail(w, 422, "bad_input", "invalid action fields")
		return false
	}
	if e := validateAgentMutation(agentMutation{Context: body.Context, Expect: body.Expect}, "turn", ""); e != nil {
		fail(w, 422, "bad_input", e.Error())
		return false
	}
	if action == "turn" {
		if body.Mode != "send" && body.Mode != "steer" && body.Mode != "queue" {
			fail(w, 422, "bad_input", "mode must be send, steer or queue")
			return false
		}
		if strings.TrimSpace(body.Text) == "" && len(body.Files) == 0 {
			fail(w, 422, "bad_input", "a message needs text or files")
			return false
		}
		if body.Mode == "steer" && len(body.Files) > 0 {
			fail(w, 409, "not_allowed", "files can only be sent with a new message")
			return false
		}
		for _, file := range body.Files {
			if !filepath.IsAbs(file.Path) || file.Name != "" && (filepath.Base(file.Name) != file.Name || strings.ContainsAny(file.Name, "\\\x00")) {
				fail(w, 422, "bad_input", "files need an absolute path and a plain name")
				return false
			}
		}
	}
	if strings.HasPrefix(action, "queue/") && r.Method == "POST" && strings.TrimSpace(body.Text) == "" {
		fail(w, 422, "bad_input", "a queued message cannot be empty")
		return false
	}
	if action == "switch-model" && strings.TrimSpace(body.Model) == "" {
		fail(w, 422, "bad_input", "model must name a model")
		return false
	}
	return true
}

func turnExpectation(agent Agent, expect *AgentExpect) error {
	if expect != nil && (expect.State != nil && *expect.State != agent.State || expect.Version != nil && *expect.Version != agent.Version) {
		return agentError(409, "stale", "the agent changed; read it again before acting")
	}
	return nil
}
