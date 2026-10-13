package appapi

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/standing"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
)

// The standing resource plugs into the Inbox producer interface. It retains
// source decisions in its own store; Inbox owns the common five-second hold.
type orderInboxSource struct{ s *Server }

func (source *orderInboxSource) InboxItems(ctx context.Context) ([]InboxItem, error) {
	a := source.s.options.Agents
	asks, err := a.OrderAsks()
	if err != nil {
		return nil, err
	}
	out := []InboxItem{}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, ask := range asks {
		// A live proposal already has its canonical engine question in Inbox.
		// The durable card takes over only after that question's engine is gone.
		liveProposal := false
		if strings.HasSuffix(ask.ID, "_proposal") && len(ask.Path) > 0 {
			if live := a.live[ask.Path[0].ID]; live != nil {
				if door, ok := live.conn.Engine.(interface{ OpenQuestions() []session.Question }); ok {
					for _, q := range door.OpenQuestions() {
						if q.Kind == session.QuestionStanding && q.Subject.Ref == ask.Source.ID && q.Withdrawn == nil {
							liveProposal = true
							break
						}
					}
				}
			}
		}
		if liveProposal {
			continue
		}
		verbs := []string{}
		for _, option := range ask.Options {
			verbs = append(verbs, option.ID)
		}
		kind := ask.Kind
		if kind == "proposal" {
			kind = "order_ask"
		}
		out = append(out, InboxItem{ID: "ask:" + ask.ID, Kind: kind, Source: ask.Source, Path: ask.Path, Title: ask.Title, Context: InboxContext{Before: ask.Before}, Verbs: verbs, WhoCanAnswer: []string{"you"}})
	}
	return out, nil
}

func (source *orderInboxSource) AnswerInbox(ctx context.Context, item InboxItem, answer InboxAnswerRequest) (InboxAnswerResponse, error) {
	asks, err := source.s.options.Agents.OrderAsks()
	if err != nil {
		return InboxAnswerResponse{}, err
	}
	for _, ask := range asks {
		if "ask:"+ask.ID != item.ID {
			continue
		}
		verb := answer.Verb
		if verb == "" {
			verb = answer.Choice
		}
		path := "/app/v1/orders/" + url.PathEscape(ask.Source.ID)
		switch {
		case strings.HasSuffix(ask.ID, "_proposal"):
			switch verb {
			case "ratify":
				path += "/ratify"
			case "not_now", "not-now":
				path += "/not-now"
			default:
				return InboxAnswerResponse{}, agentError(422, "bad_input", "choose Ratify or Not now")
			}
			return source.s.forwardInbox(ctx, path, ExpectRequest{Expect: answer.Expect})
		case ask.Kind == "budget":
			switch verb {
			case "raise":
				return source.s.forwardInbox(ctx, path+"/change", ChangeOrderRequest{BudgetPerRun: &Money{USD: strings.TrimSpace(answer.Words)}, Expect: answer.Expect})
			case "pause":
				return source.s.forwardInbox(ctx, path+"/pause", ExpectRequest{Expect: answer.Expect})
			default:
				return InboxAnswerResponse{}, agentError(422, "bad_input", "choose Raise the budget per run or Pause order")
			}
		case ask.Kind == "reminder":
			return InboxAnswerResponse{}, agentError(422, "bad_input", "a fired reminder is a notice")
		default:
			run := strings.TrimSuffix(strings.TrimPrefix(ask.ID, orderAskID(ask.Source.ID, "")), "_report")
			return source.s.forwardInbox(ctx, path+"/runs/"+url.PathEscape(run), OrderRunRequest{Verb: verb, Expect: answer.Expect})
		}
	}
	return InboxAnswerResponse{}, agentError(409, "already_answered", "this standing decision is no longer waiting")
}

// Validation happens before the hold; the revision snapshot prevents a later
// edit from borrowing approval for the earlier card.
func (source *orderInboxSource) ValidateInbox(ctx context.Context, item InboxItem, answer InboxAnswerRequest) error {
	asks, err := source.s.options.Agents.OrderAsks()
	if err != nil {
		return err
	}
	for _, ask := range asks {
		if "ask:"+ask.ID != item.ID {
			continue
		}
		verb := answer.Verb
		if verb == "" {
			verb = answer.Choice
		}
		allowed := false
		for _, option := range ask.Options {
			if option.ID == verb {
				allowed = true
			}
		}
		if !allowed {
			return agentError(422, "bad_input", "choose an answer offered by this standing order")
		}
		current, err := source.orderItem(ask.Source.ID)
		if err != nil {
			return err
		}
		return orderExpect(current, answer.Expect)
	}
	return agentError(409, "already_answered", "this standing decision is no longer waiting")
}
func (source *orderInboxSource) SnapshotInboxAnswer(item InboxItem, answer InboxAnswerRequest) InboxAnswerRequest {
	if answer.Expect == nil {
		if current, err := source.orderItem(item.Source.ID); err == nil {
			version := uint64(current.Revision)
			answer.Expect = &AgentExpect{Version: &version}
		}
	}
	return answer
}
func (source *orderInboxSource) orderItem(id string) (standing.Item, error) {
	store, err := standing.Open(filepath.Join(source.s.options.Agents.home, "v3", "standing"))
	if err != nil {
		return standing.Item{}, err
	}
	return store.Get(id)
}
func (source *orderInboxSource) ResolvedInbox(item InboxItem) (string, string) {
	current, err := source.orderItem(item.Source.ID)
	if err != nil || current.App == nil {
		return "no_longer_needed", ""
	}
	for _, run := range current.App.Runs {
		if "ask:"+orderAskID(current.ID, run.ID) == item.ID && run.State == "changed" {
			return "no_longer_needed", ""
		}
	}
	return "in_place", "you"
}
