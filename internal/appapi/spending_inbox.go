package appapi

import (
	"context"
	"strings"
)

// Spending supplies its own waiting state through the Inbox resource boundary.
// The coordinator owns answer-once and Undo; this adapter owns the actual raise.
type spendingInboxSource struct{ s *Server }

func (n spendingInboxSource) InboxItems(context.Context) ([]InboxItem, error) {
	rows, err := n.s.spendingAskRows()
	if err != nil {
		return nil, err
	}
	items := []InboxItem{}
	for _, row := range rows {
		ask := row.Ask
		items = append(items, InboxItem{ID: "ask:" + ask.ID, Kind: "budget", Source: ask.Source, Path: ask.Path, Title: ask.Title, CreatedAt: row.At, Context: InboxContext{Before: ask.Before}, Verbs: []string{"raise"}, WhoCanAnswer: []string{"you"}})
	}
	return items, nil
}

func (n spendingInboxSource) raise(item InboxItem, answer InboxAnswerRequest) (RaiseCapRequest, error) {
	if (answer.Choice != "raise" && answer.Verb != "raise") || answer.Rule != nil || len(answer.Items) != 0 {
		return RaiseCapRequest{}, agentError(422, "bad_input", "a budget answer needs raise and a USD amount in words")
	}
	if answer.Expect != nil && (answer.Expect.Version != nil || answer.Expect.State != nil && *answer.Expect.State != "waiting") {
		return RaiseCapRequest{}, agentError(409, "stale", "the budget question changed")
	}
	var row spendingAsk
	found := false
	err := resourceDisk(n.s.resourceHome(), "spending-asks", map[string]spendingAsk{}, func(raw []byte) (any, error) {
		state, err := readSpendingAskState(raw)
		if err != nil {
			return nil, err
		}
		for _, ask := range state.Items {
			if "ask:"+ask.Ask.ID == item.ID {
				row, found = ask, true
				break
			}
		}
		return state, nil
	})
	if err != nil {
		return RaiseCapRequest{}, err
	}
	if !found {
		return RaiseCapRequest{}, agentError(409, "stale", "the budget question is no longer waiting")
	}
	to := Money{USD: strings.TrimSpace(answer.Words)}
	amount, err := moneyValue(to)
	if err != nil || amount <= 0 {
		return RaiseCapRequest{}, agentError(422, "bad_input", "a budget raise needs USD with two decimal places in words")
	}
	return RaiseCapRequest{Scope: row.Scope, ID: row.Owner, To: to, For: "today"}, nil
}

func (n spendingInboxSource) ValidateInbox(ctx context.Context, item InboxItem, answer InboxAnswerRequest) error {
	body, err := n.raise(item, answer)
	if err != nil {
		return err
	}
	cap, err := n.s.spendingBackend().SpendingCap(ctx, body.Scope, body.ID)
	if err != nil {
		return err
	}
	old, _ := moneyValue(cap)
	to, _ := moneyValue(body.To)
	if old == 0 || to <= old {
		return agentError(409, "conflict", "the new cap must be higher than the current cap")
	}
	return nil
}

func (n spendingInboxSource) AnswerInbox(ctx context.Context, item InboxItem, answer InboxAnswerRequest) (InboxAnswerResponse, error) {
	if err := n.ValidateInbox(ctx, item, answer); err != nil {
		return InboxAnswerResponse{}, err
	}
	body, err := n.raise(item, answer)
	if err != nil {
		return InboxAnswerResponse{}, err
	}
	if err := n.s.spendingBackend().RaiseCap(ctx, body, n.s.resourceNow()); err != nil {
		return InboxAnswerResponse{}, err
	}
	if err := n.s.RefreshSpending(ctx); err != nil {
		return InboxAnswerResponse{}, err
	}
	return InboxAnswerResponse{OK: true}, nil
}
