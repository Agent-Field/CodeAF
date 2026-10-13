package appapi

import (
	"context"
	"time"
)

// AskInboxSource adapts the owning resource's typed producer and resolver.
// Standing orders, reminders, task decisions and climbed questions keep their
// source identity rather than becoming a second durable Inbox object.
type AskInboxSource struct {
	List      func(context.Context) ([]Ask, error)
	Answer    func(context.Context, Ask, InboxAnswerRequest) (InboxAnswerResponse, error)
	CreatedAt func(Ask) time.Time
}

func (a *AskInboxSource) InboxItems(ctx context.Context) ([]InboxItem, error) {
	asks, err := a.List(ctx)
	if err != nil {
		return nil, err
	}
	items := []InboxItem{}
	for _, ask := range asks {
		if ask.State != "waiting" || !inboxKind(ask.Kind) {
			continue
		}
		verbs := []string{}
		for _, option := range ask.Options {
			verbs = append(verbs, option.ID)
		}
		at := time.Time{}
		if a.CreatedAt != nil {
			at = a.CreatedAt(ask)
		}
		items = append(items, InboxItem{ID: "ask:" + ask.ID, Kind: ask.Kind, Source: ask.Source, Title: ask.Title, Path: ask.Path, CreatedAt: at, Context: InboxContext{Before: inboxBefore(ask.Before)}, Verbs: verbs, WhoCanAnswer: []string{"you"}})
	}
	return items, nil
}
func (a *AskInboxSource) AnswerInbox(ctx context.Context, item InboxItem, answer InboxAnswerRequest) (InboxAnswerResponse, error) {
	asks, err := a.List(ctx)
	if err != nil {
		return InboxAnswerResponse{}, err
	}
	for _, ask := range asks {
		if "ask:"+ask.ID == item.ID && ask.State == "waiting" {
			return a.Answer(ctx, ask, answer)
		}
	}
	return InboxAnswerResponse{}, agentError(409, "already_answered", "this ask is no longer waiting")
}
