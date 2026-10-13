package appapi

import (
	"context"
	"net/url"
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
)

type askValidateKey struct{}

// Inbox asks the owning resource to validate before arming Undo. The same
// handler checks scopes, batches and irreversible actions on both routes.
func (e *engineInboxSource) validateAskInbox(ctx context.Context, item InboxItem, answer InboxAnswerRequest) error {
	a := e.a
	a.mu.Lock()
	record, q, _, err := e.find(item, InboxAnswerRequest{})
	a.mu.Unlock()
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, askValidateKey{}, true)
	var body any
	path := "/app/v1/asks/" + url.PathEscape(askID(record.Agent.ID, q)) + "/answer"
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
		body = PermissionRequest{Verb: verb, Rule: answer.Rule, Items: answer.Items, Expect: answer.Expect}
		path = "/app/v1/permissions/" + url.PathEscape(askID(record.Agent.ID, q))
	} else {
		choice := inboxEngineKey(q, answer)
		verb := ""
		if answer.Verb == "later" || answer.Verb == "you_decide" {
			choice = ""
			verb = answer.Verb
		}
		body = AnswerAskRequest{Choice: choice, Words: answer.Words, Verb: verb, Expect: answer.Expect}
	}
	_, err = e.s.forwardInbox(ctx, path, body)
	return err
}

// A submitted answer holds its question clock while the shared Undo timer is
// running. Undo does not resume a clock over the person's earlier interaction.
func (e *engineInboxSource) SnapshotInboxAnswer(item InboxItem, answer InboxAnswerRequest) InboxAnswerRequest {
	if !strings.HasPrefix(item.ID, "ask:") || strings.Count(item.ID, ":") < 3 {
		return answer
	}
	a := e.a
	a.mu.Lock()
	defer a.mu.Unlock()
	record, q, _, err := e.find(item, InboxAnswerRequest{})
	if err != nil {
		return answer
	}
	id := askID(record.Agent.ID, q)
	_ = a.asks(true, func(d *askDisk) error {
		row, ok := d.Asks[id]
		if ok && row.Ask.State == "waiting" {
			row.Held = true
			if row.Ask.Clock != nil {
				at := a.now().UTC()
				row.Ask.Clock.PausesAt = &at
			}
			d.Asks[id] = row
		}
		return nil
	})
	return answer
}
