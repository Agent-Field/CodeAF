package appapi

import (
	"encoding/json"
	"slices"

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
		ReadOnly:     card.ReadOnly,
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

// permissionScopes lists what a yes can be remembered as, shortest-lived
// first: for this task (a look only, which the engine checks again itself),
// then the two saved rules — the exact command, and its class when it has one.
func permissionScopes(q session.Question, card session.ConsentCard) []PermissionScope {
	scopes := []PermissionScope{}
	if card.ReadOnly && card.Family != "" {
		scopes = append(scopes, PermissionScope{ID: "task", Command: card.Family, Label: "for this task"})
	}
	scopes = append(scopes, PermissionScope{ID: "exact", Command: card.Command, Label: "exactly this command"})
	if q.Subject.Name != approval.ToolBash || q.PermissionCommand == "" {
		return scopes
	}
	args, _ := json.Marshal(map[string]string{"command": q.PermissionCommand})
	if class := config.AppPermissionClass(approval.ToolBash, args); class != "" {
		scopes = append(scopes, PermissionScope{ID: "class", Command: class, Label: class})
	}
	return scopes
}

// waitingSteps is the number of calls stopped on a person's ok. Each is a
// step underway, so a task waiting on its first call reads "1 step" rather
// than "0 steps" for as long as the question stands. It is a count for the
// view and nothing else.
func waitingSteps(engine any) int {
	door, ok := engine.(interface{ OpenQuestions() []session.Question })
	if !ok {
		return 0
	}
	n := 0
	for _, q := range door.OpenQuestions() {
		if q.Kind == session.QuestionConsent && q.Withdrawn == nil {
			n++
		}
	}
	return n
}

// permissionGroupKey is the key looks of one step coalesce under: the same
// batch and the same family. A write, or an ask with no batch, is its own
// item and keeps asking on its own.
func permissionGroupKey(q session.Question) string {
	if q.Kind != session.QuestionConsent || q.Card == nil || !q.Card.ReadOnly || q.Card.Family == "" || q.Batch == "" || q.Stakes == session.StakesIrreversible {
		return ""
	}
	return q.Batch + "\x00" + q.Card.Family
}

// joinPermission folds one more look into a grouped item, keeping commands
// and command_ids aligned.
func joinPermission(item *InboxItem, agent string, q session.Question) {
	if item.Permission == nil || q.Card == nil {
		return
	}
	item.Permission.Commands = append(item.Permission.Commands, q.Card.Command)
	item.Permission.CommandIDs = append(item.Permission.CommandIDs, askID(agent, q))
	for _, touch := range q.Card.Touches {
		if !slices.Contains(item.Permission.Touches, touch) {
			item.Permission.Touches = append(item.Permission.Touches, touch)
		}
	}
}
