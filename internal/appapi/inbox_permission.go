package appapi

import (
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// inboxPermission is a consent question's card content. The command is the
// engine's own description of the call ([session.Question.Card]); the head
// ("needs your ok to run bash") is only the last fallback for a question
// raised by an engine too old to carry one. It is display: no answer path
// reads it.
func inboxPermission(agent string, q session.Question) *InboxPermission {
	if q.Kind != session.QuestionConsent {
		return nil
	}
	card := session.ConsentCard{Command: q.PermissionCommand}
	if q.Card != nil {
		card = *q.Card
	}
	if card.Command == "" {
		card.Command = q.Head
	}
	p := &InboxPermission{
		Reason:       q.Reason,
		Commands:     []string{card.Command},
		CommandIDs:   []string{askID(agent, q)},
		Irreversible: q.Stakes == session.StakesIrreversible,
		Touches:      card.Touches,
		Scopes:       []PermissionScope{},
	}
	if len(q.Blocking.Tasks) > 0 {
		p.Task = q.Blocking.Tasks[0]
	}
	if !p.Irreversible {
		p.Scopes = permissionScopes(q, card)
	}
	return p
}

// permissionScopes lists the rules a yes can save, the ones the permission
// route already accepts: the exact command, and its class when it has one.
func permissionScopes(q session.Question, card session.ConsentCard) []PermissionScope {
	scopes := []PermissionScope{{ID: "exact", Command: card.Command, Label: "exactly this command"}}
	if q.Subject.Name != approval.ToolBash || q.PermissionCommand == "" {
		return scopes
	}
	args, _ := json.Marshal(map[string]string{"command": q.PermissionCommand})
	if class := config.AppPermissionClass(approval.ToolBash, args); class != "" {
		scopes = append(scopes, PermissionScope{ID: "class", Command: class, Label: class})
	}
	return scopes
}
