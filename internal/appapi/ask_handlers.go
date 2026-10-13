package appapi

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var askContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/asks/{id}", Request: "", Response: "Ask"}, handle: (*Server).askHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/asks/{id}/answer", Request: "AnswerAskRequest", Response: "ActionResponse"}, handle: (*Server).askHandler},
}

func (s *Server) askHandler(w http.ResponseWriter, r *http.Request) {
	if len(r.Header.Values("Idempotency-Key")) > 1 {
		fail(w, 422, "bad_input", "use one Idempotency-Key")
		return
	}
	s.idempotent(s.handleAsk)(w, r)
}
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	a := s.options.Agents
	if a == nil {
		fail(w, 503, "not_allowed", "the agent registry is unavailable")
		return
	}
	var body AnswerAskRequest
	if r.Method == "POST" && !decodeObject(w, r, &body) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		fail(w, 503, "not_allowed", "the engine is stopping")
		return
	}
	a.events = s.events
	row, err := a.findAskLocked(r.PathValue("id"))
	if err != nil {
		agentFailure(w, err)
		return
	}
	if r.Method == "GET" {
		if row.Permission != nil {
			row.Permission.Batch = a.permissionBatchLocked(row)
			row.Ask.Permission = row.Permission
		}
		write(w, 200, row.Ask)
		return
	}
	if err := askExpectation(row, body.Expect); err != nil {
		agentFailure(w, err)
		return
	}
	if row.Ask.State != "waiting" {
		askConflict(w, row, a.now())
		return
	}
	if row.Question.Kind == session.QuestionConsent && body.Verb != "later" {
		fail(w, 422, "bad_input", "answer permissions through the permission route")
		return
	}
	if body.Verb != "" && (body.Choice != "" || strings.TrimSpace(body.Words) != "") || body.Choice == "" && strings.TrimSpace(body.Words) == "" && body.Verb == "" {
		fail(w, 422, "bad_input", "give a choice, words, Later or You decide")
		return
	}
	if body.Verb != "" && body.Verb != "later" && body.Verb != "you_decide" {
		fail(w, 422, "bad_input", "verb must be later or you_decide")
		return
	}
	agent := askAgent(row.Ask.ID)
	live := a.live[agent]
	if live == nil {
		fail(w, 409, "conflict", "this ask's engine is no longer connected")
		return
	}
	if body.Verb == "later" {
		door, ok := live.conn.Engine.(interface {
			HoldQuestion(session.QuestionKind, string)
		})
		if !ok {
			fail(w, 409, "not_allowed", "this engine cannot hold asks")
			return
		}
		if r.Context().Value(askValidateKey{}) != nil {
			write(w, 200, ActionResponse{OK: true})
			return
		}
		door.HoldQuestion(row.Question.Kind, row.Question.Token())
		at := a.now().UTC()
		row.Held = true
		row.Ask.Clock = &AskClock{PausesAt: &at}
		row.Version++
		entry := DisplayEntry{ID: "later_" + row.Ask.ID, Kind: "receipt", Role: "note", Text: "Later · " + row.Question.Head, By: "you", At: &at, Link: &SourceRef{Kind: "ask", ID: row.Ask.ID}}
		if err := a.asks(true, func(d *askDisk) error { d.Asks[row.Ask.ID] = row; return nil }); err != nil {
			agentFailure(w, err)
			return
		}
		if err := a.saveAskReceipt(agent, entry); err != nil {
			agentFailure(w, err)
			return
		}
		write(w, 200, ActionResponse{OK: true, Receipt: &entry})
		return
	}
	answer := session.Answer{Kind: row.Question.Kind, ID: row.Question.ID, Ref: row.Question.Ref, Ask: row.Question.Ask, Key: body.Choice, Change: strings.TrimSpace(body.Words), From: "app", At: a.now().UTC(), DecidedBy: session.DecidedByPerson}
	if r.Context().Value(inboxForwardKey{}) != nil {
		answer.From = "inbox"
	}
	if body.Verb == "you_decide" {
		if row.Ask.Irreversible {
			fail(w, 422, "not_allowed", "irreversible asks need your answer")
			return
		}
		if row.Question.Pick != nil {
			answer.Key = row.Question.Pick.Key
		} else if row.Question.Ask == session.AskClarification {
			answer.Change = "You decide. Choose a reasonable approach and continue."
		} else {
			fail(w, 422, "not_allowed", "this ask has no suggested answer to hand back")
			return
		}
		answer.DecidedBy = session.DecidedByAsker
	}
	if answer.Key != "" {
		if _, ok := row.Question.Option(answer.Key); !ok {
			fail(w, 422, "bad_input", "choose one of this ask's options")
			return
		}
	}
	if r.Context().Value(askValidateKey{}) != nil {
		write(w, 200, ActionResponse{OK: true})
		return
	}
	if err := a.resolveAskLocked(row, answer); err != nil {
		agentFailure(w, err)
		return
	}
	resolved, err := a.findAskLocked(row.Ask.ID)
	if err != nil {
		agentFailure(w, err)
		return
	}
	write(w, 200, ActionResponse{OK: true, Receipt: resolved.Ask.Receipt})
}
func askExpectation(row savedAsk, expect *AgentExpect) error {
	if expect != nil && (expect.State != nil && *expect.State != row.Ask.State || expect.Version != nil && *expect.Version != row.Version) {
		return agentError(409, "stale", "this ask changed; read it again before answering")
	}
	return nil
}
func askConflict(w http.ResponseWriter, row savedAsk, now time.Time) {
	sentence := "This ask is no longer waiting. Your answer wasn't sent."
	response := ConflictResponse{ErrorResponse: ErrorResponse{Error: sentence, Reason: "conflict"}}
	if row.Ask.Receipt != nil {
		response.By = row.Ask.Receipt.By
		response.At = row.Ask.Receipt.At
		elapsed := int64(0)
		if response.At != nil {
			elapsed = max(0, int64(now.Sub(*response.At)/time.Second))
		}
		response.Error = fmt.Sprintf("@%s answered this %ds ago. Your answer wasn't sent.", response.By, elapsed)
	}
	write(w, 409, response)
}

// The durable claim and engine resolution share the ask lock across launches.
// A refused answer keeps the pending ask and never creates a success receipt.
func (a *AgentRegistry) resolveAskLocked(row savedAsk, answer session.Answer) error {
	agent := askAgent(row.Ask.ID)
	live := a.live[agent]
	if live == nil {
		return agentError(409, "conflict", "this ask's engine is no longer connected")
	}
	door, ok := live.conn.Engine.(interface {
		ResolveQuestion(session.Answer) error
		OpenQuestions() []session.Question
	})
	if !ok {
		return agentError(409, "not_allowed", "this engine cannot answer asks")
	}
	var entry DisplayEntry
	err := a.asks(true, func(d *askDisk) error {
		current, ok := d.Asks[row.Ask.ID]
		if !ok || current.Ask.State != "waiting" {
			return agentError(409, "conflict", "this ask was answered already. Your answer wasn't sent.")
		}
		open := false
		for _, q := range door.OpenQuestions() {
			if askID(agent, q) == row.Ask.ID {
				open = true
				break
			}
		}
		if !open {
			return agentError(409, "conflict", "this ask is no longer waiting. Your answer wasn't sent.")
		}
		if err := door.ResolveQuestion(answer); err != nil {
			return agentError(409, "conflict", err.Error())
		}
		text, by, _ := askResolution(row.Question, &answer)
		at := a.now().UTC()
		entry = DisplayEntry{ID: "receipt_" + row.Ask.ID, Kind: "receipt", Role: "note", Text: text, By: by, At: &at, Link: &SourceRef{Kind: "ask", ID: row.Ask.ID}}
		if strings.HasPrefix(answer.Why, "rule:") {
			entry.Link = &SourceRef{Kind: "rule", ID: strings.TrimPrefix(answer.Why, "rule:")}
		}
		current.Ask.State = "answered"
		current.Ask.Clock = nil
		current.Ask.Receipt = &entry
		current.Version++
		d.Asks[row.Ask.ID] = current
		return nil
	})
	if err != nil {
		return err
	}
	if err := a.saveAskReceipt(agent, entry); err != nil {
		return err
	}
	a.publishTurn(EventAskResolved, AskEvent{Ask: row.Ask.ID, Agent: agent, Kind: row.Ask.Kind, How: "answered", By: entry.By})
	return nil
}
