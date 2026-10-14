package session

import (
	"encoding/json"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/config"
)

// "Allow for this task" (ConsentTask). One read-only gh task used to ask eight
// times for eight looks; the person now answers the first and that family's
// looks run until the task ends — or, in the conversation itself, the turn.
//
// THREE LAWS, and each is the reason a line below is shaped as it is:
//
//   - WHAT IS A LOOK IS internal/approval's ALLOWLIST AND NOTHING HERE. The
//     gate asks [approval.ReadOnlyLook] about the call in front of it, every
//     time; a grant is only ever a family name, never a remembered command.
//   - A GRANT BELONGS TO THE THING THAT ENDS. A task's grants live on its own
//     node, so they cannot be reached by another task of the same name and are
//     emptied when the node ends. The conversation's live with a turn number.
//   - THE FLOOR STILL ASKS. A critical shape, a call that acts in the person's
//     name by the other table, and an always-ask row are never covered.

// lookGrants is the set of look families allowed for one task or one turn.
type lookGrants struct {
	mu       sync.Mutex
	families map[string]bool
}

func (g *lookGrants) allow(family string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.families == nil {
		g.families = map[string]bool{}
	}
	g.families[family] = true
}

func (g *lookGrants) covers(family string) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.families[family]
}

// end empties the set. The task is over; a task continued later asks again.
func (g *lookGrants) end() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.families = nil
	g.mu.Unlock()
}

// lookFamily is the family of a call that is a look, by internal/approval's
// allowlist. Only bash can be one.
func lookFamily(call ai.ToolCall) (string, bool) {
	if call.Function.Name != approval.ToolBash {
		return "", false
	}
	var fields struct {
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(call.Function.Arguments), &fields) != nil {
		return "", false
	}
	return approval.ReadOnlyLook(fields.Command)
}

// grantableLook is a look that is also off every floor.
func (a *Agent) grantableLook(call ai.ToolCall) (string, bool) {
	family, ok := lookFamily(call)
	args := json.RawMessage(call.Function.Arguments)
	if !ok || approval.AlwaysAsks(call.Function.Name, args) {
		return "", false
	}
	if a.appPermissionMode() {
		floor, err := config.AppPermissionAlwaysAsk(a.appPermissionOwner().config.ProfileDir, call.Function.Name, args, a.config.InTask)
		if err != nil || floor {
			return "", false
		}
	}
	return family, true
}

// lookGrantSet is the set for whoever is asking: a task's own node, or the
// conversation's current turn. A task with no node set has none.
func (a *Agent) lookGrantSet() *lookGrants {
	if a.config.InTask {
		return a.config.lookGrants
	}
	holder := a
	for holder.approvalParent != nil {
		holder = holder.approvalParent
	}
	holder.mu.Lock()
	defer holder.mu.Unlock()
	if holder.turnLooks == nil || holder.turnLooksTurn != holder.turnSeq {
		holder.turnLooks, holder.turnLooksTurn = &lookGrants{}, holder.turnSeq
	}
	return holder.turnLooks
}

// rememberLookGrant banks a "for this task" yes. An answer that claims the
// scope for a call that is not a grantable look banks nothing: it was a yes
// to that one call.
func (a *Agent) rememberLookGrant(call ai.ToolCall) {
	if family, ok := a.grantableLook(call); ok {
		a.lookGrantSet().allow(family)
	}
}

func (a *Agent) lookGrantCovers(call ai.ToolCall) bool {
	family, ok := a.grantableLook(call)
	return ok && a.lookGrantSet().covers(family)
}
