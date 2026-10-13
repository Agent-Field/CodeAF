package appapi

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

type engineInboxSource struct {
	s       *Server
	a       *AgentRegistry
	watched map[string]AgentEngine
}
type inboxQuestionDoor interface {
	OpenQuestions() []session.Question
	ResolveQuestion(session.Answer) error
}

func inboxQuestionID(agent string, q session.Question) string {
	return "ask:" + url.QueryEscape(agent) + ":" + url.QueryEscape(string(q.Kind)) + ":" + url.QueryEscape(q.Token())
}
func inboxQuestionKind(q session.Question) string {
	switch q.Kind {
	case session.QuestionLanding:
		return "your_call"
	case session.QuestionConflict:
		return "conflict"
	case session.QuestionStanding:
		return "order_ask"
	case session.QuestionFuel:
		return "budget"
	}
	switch q.Ask {
	case session.AskPermission, session.AskConfirmation:
		return "permission"
	case session.AskClarification:
		return "clarification"
	case session.AskLanding, session.AskJudgement:
		return "your_call"
	}
	return "question"
}
func (e *engineInboxSource) InboxItems(ctx context.Context) ([]InboxItem, error) {
	a := e.a
	a.mu.Lock()
	out := []InboxItem{}
	err := a.disk(func(d *agentDisk) error {
		for id, record := range d.Records {
			if record.Agent.State == "closed" || record.Agent.State == "archived" {
				continue
			}
			if a.live[id] == nil {
				if presence, ok := session.ReadSessionPresence(filepath.Dir(record.SessionFile), a.now()); ok && presence.Question.Full != nil {
					_, _ = a.connect(ctx, record)
				}
			}
			entries := a.turnEntries(record)
			if live := a.live[id]; live != nil {
				e.watch(id, live.conn.Engine)
				if door, ok := live.conn.Engine.(interface{ OpenQuestions() []session.Question }); ok {
					for _, q := range door.OpenQuestions() {
						if q.Withdrawn != nil {
							continue
						}
						verbs := []string{}
						for _, option := range q.Options {
							verbs = append(verbs, option.Key)
						}
						at := q.Asked
						if at.IsZero() {
							at = record.Agent.CreatedAt
						}
						source := SourceRef{Kind: "agent", ID: id}
						if q.Subject.Kind == session.SubjectNode {
							source = SourceRef{Kind: "task", ID: q.Token()}
							if source.ID == "" {
								source.ID = q.Token()
							}
						}
						out = append(out, InboxItem{ID: inboxQuestionID(id, q), Kind: inboxQuestionKind(q), Source: source, Title: q.Head, Path: []SourceRef{source}, CreatedAt: at, Context: InboxContext{Before: inboxBefore(entries)}, Verbs: verbs, WhoCanAnswer: []string{"you"}})
					}
				}
			}
			if a.view(record).State == "failed" {
				at := record.Agent.CreatedAt
				if record.Agent.LastMessageAt != nil {
					at = *record.Agent.LastMessageAt
				}
				out = append(out, InboxItem{ID: "failed:" + id + ":" + at.UTC().Format(time.RFC3339Nano), Kind: "failed", Source: SourceRef{Kind: "agent", ID: id}, Path: []SourceRef{{Kind: "agent", ID: id}}, Title: record.Agent.Receipt, CreatedAt: at, Context: InboxContext{Before: inboxBefore(entries)}, Verbs: []string{"retry", "switch_model"}, WhoCanAnswer: []string{"you"}})
			}
		}
		return nil
	}, false)
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Source.Kind == "agent" {
			path, err := a.CrewQuestionPath(out[i].Source.ID)
			if err == nil && len(path) > 1 {
				out[i].Source = path[0]
				out[i].Path = path
			}
		}
	}
	asks, err := a.CrewBudgetAsks()
	if err != nil {
		return nil, err
	}
	for _, ask := range asks {
		if ask.State != "waiting" {
			continue
		}
		verbs := []string{}
		for _, option := range ask.Options {
			verbs = append(verbs, option.ID)
		}
		out = append(out, InboxItem{ID: "ask:" + ask.ID, Kind: ask.Kind, Source: ask.Source, Title: ask.Title, Path: ask.Path, Context: InboxContext{Before: ask.Before}, Verbs: verbs, WhoCanAnswer: []string{"you"}})
	}
	return out, nil
}
func (e *engineInboxSource) find(item InboxItem, answer InboxAnswerRequest) (agentRecord, session.Question, inboxQuestionDoor, error) {
	a := e.a
	var record agentRecord
	var question session.Question
	var engine inboxQuestionDoor
	err := a.disk(func(d *agentDisk) error {
		for id, row := range d.Records {
			live := a.live[id]
			if live == nil {
				continue
			}
			door, ok := live.conn.Engine.(inboxQuestionDoor)
			if !ok {
				continue
			}
			for _, q := range door.OpenQuestions() {
				if inboxQuestionID(id, q) != item.ID {
					continue
				}
				current := a.view(row)
				if answer.Expect != nil && ((answer.Expect.State != nil && *answer.Expect.State != current.State) || (answer.Expect.Version != nil && *answer.Expect.Version != current.Version)) {
					return agentError(409, "stale", "the source changed; read it again before acting")
				}
				record, question, engine = row, q, door
				return nil
			}
		}
		return agentError(409, "already_answered", "this question is no longer waiting")
	}, false)
	return record, question, engine, err
}
func (e *engineInboxSource) ValidateInbox(ctx context.Context, item InboxItem, answer InboxAnswerRequest) error {
	if strings.HasPrefix(item.ID, "failed:") || !strings.Contains(item.ID, "ask:") {
		return nil
	}
	// Crew budget asks belong to the asks lane, which consumes its own typed producer.
	if !strings.HasPrefix(item.ID, "ask:") || strings.Count(item.ID, ":") < 3 {
		return nil
	}
	return e.validateAskInbox(ctx, item, answer)
}
func inboxEngineKey(q session.Question, answer InboxAnswerRequest) string {
	if answer.Choice != "" {
		return answer.Choice
	}
	switch answer.Verb {
	case "allow_once":
		return "1"
	case "always":
		return "2"
	case "deny":
		return "3"
	case "accept":
		return "a"
	case "not_right":
		return "n"
	case "tell":
		return "s"
	}
	return answer.Verb
}
func (e *engineInboxSource) AnswerInbox(ctx context.Context, item InboxItem, answer InboxAnswerRequest) (InboxAnswerResponse, error) {
	if strings.HasPrefix(item.ID, "failed:") {
		verb := answer.Verb
		if verb == "" {
			verb = answer.Choice
		}
		if verb == "switch_model" {
			return e.s.forwardInbox(ctx, "/app/v1/agents/"+url.PathEscape(item.Source.ID)+"/switch-model", ModelRequest{Model: answer.Words, Expect: answer.Expect})
		}
		if verb != "retry" {
			return InboxAnswerResponse{}, agentError(422, "bad_input", "choose Retry or Switch model")
		}
		return e.s.forwardInbox(ctx, "/app/v1/agents/"+url.PathEscape(item.Source.ID)+"/retry", ExpectRequest{Expect: answer.Expect})
	}
	if strings.Count(item.ID, ":") < 3 {
		return e.s.forwardInbox(ctx, "/app/v1/asks/"+url.PathEscape(strings.TrimPrefix(item.ID, "ask:"))+"/answer", AnswerAskRequest{Choice: answer.Choice, Words: answer.Words, Verb: answer.Verb, Expect: answer.Expect})
	}
	a := e.a
	a.mu.Lock()
	record, q, _, err := e.find(item, InboxAnswerRequest{})
	if err != nil {
		a.mu.Unlock()
		return InboxAnswerResponse{}, err
	}
	a.mu.Unlock()
	if q.Kind == session.QuestionConsent {
		verb := answer.Verb
		if verb == "" {
			switch answer.Choice {
			case "1":
				verb = "allow_once"
			case "2":
				verb = "always"
			case "3":
				verb = "deny"
			}
		}
		return e.s.forwardInbox(ctx, "/app/v1/permissions/"+url.PathEscape(turnAsk(record.Agent.ID, q).ID), PermissionRequest{Verb: verb, Rule: answer.Rule, Items: answer.Items, Expect: answer.Expect})
	}
	key := inboxEngineKey(q, answer)
	verb := ""
	if answer.Verb == "later" || answer.Verb == "you_decide" {
		verb = answer.Verb
		key = ""
	}
	return e.s.forwardInbox(ctx, "/app/v1/asks/"+url.PathEscape(turnAsk(record.Agent.ID, q).ID)+"/answer", AnswerAskRequest{Choice: key, Words: answer.Words, Verb: verb, Expect: answer.Expect})
}

// Question subscriptions outlive turns, so answers from the terminal and
// withdrawals also resolve the derived item when no app reply is running.
func (e *engineInboxSource) watch(id string, engine AgentEngine) {
	if e.watched == nil {
		e.watched = map[string]AgentEngine{}
	}
	if e.watched[id] == engine {
		return
	}
	if live := e.a.live[id]; live != nil && live.conn.asks != nil {
		e.watched[id] = engine
		e.s.observeInboxAsks(id, live.conn.asks)
		return
	}
	door, ok := engine.(interface {
		WatchQuestions() (<-chan session.Event, func())
	})
	if !ok {
		return
	}
	events, stop := door.WatchQuestions()
	e.watched[id] = engine
	go func() {
		defer stop()
		for {
			select {
			case <-e.a.done:
				return
			case <-e.s.inbox.done:
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				e.s.resolveInboxQuestion(id, event)
			}
		}
	}()
}

// The engine's answer event keeps its words and receipt after the question
// leaves OpenQuestions, including decisions made in another surface.
func (s *Server) resolveInboxQuestion(agent string, event session.Event) {
	if event.Question == nil {
		return
	}
	q := *event.Question
	how, by := "no_longer_needed", ""
	var answer *InboxAnswerRequest
	var receipt *DisplayEntry
	switch event.Kind {
	case session.EventQuestionAnswered:
		how, by = "in_place", "you"
		if event.Answer != nil {
			answer = &InboxAnswerRequest{Choice: event.Answer.Key, Words: event.Answer.Change}
			text, person, _ := askResolution(q, event.Answer)
			at := event.Answer.At
			if at.IsZero() {
				at = s.inbox.now().UTC()
			}
			receipt = &DisplayEntry{ID: "receipt_" + askID(agent, q), Kind: "receipt", Role: "note", Text: text, By: person, At: &at, Link: &SourceRef{Kind: "ask", ID: askID(agent, q)}}
			if strings.HasPrefix(event.Answer.From, "rule:") {
				receipt.Link = &SourceRef{Kind: "rule", ID: strings.TrimPrefix(event.Answer.From, "rule:")}
			}
			if event.Answer.From == "inbox" {
				how = "here"
			} else {
				by = person
			}
		}
	case session.EventQuestionWithdrawn:
	default:
		return
	}
	s.resolveInbox(inboxQuestionID(agent, q), how, by, answer, receipt)
}
