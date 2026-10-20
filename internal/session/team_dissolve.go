package session

// team_dissolve: a manager taking back a team that never held anyone.
//
// A conversation the person turns into a team (the chat's `/helpers`) is its
// manager from that moment, with nobody under it, and its first job is to
// propose helpers. The person answers in their own words, and one answer is
// "no, cancel it". The manager has every verb for growing a team and, until
// this one, none for taking the team back, so a declined proposal left an empty
// team on the Teams page that only the person could disband — and disbanding
// keeps a read-only history row for a team that never did anything.
//
// THE VERB IS ON THE BELT ONLY WHILE IT CAN WORK. It is armed at a boundary that
// finds this conversation managing a team that has never held anyone but it
// ([teams.File.Dissolvable]), and it leaves at the first boundary after anyone
// else joins or Traffic is written, with the reason said to a model that calls
// it from memory. A
// capability that cannot work is absent, not broken. The call itself checks
// again, under the store's lock, and also refuses a team whose Traffic has
// anything in it ([teams.Dissolve]), because a start already written there is a
// member on its way.
//
// IT IS ONE RULE FOR EVERY TEAM. Nothing here asks which door made the team: a
// team made on the Teams page and given a manager there is dissolvable by the
// same rule while it is just as empty. Disbanding a team that was used stays the
// person's, on Teams.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/teams"
)

const teamDissolveToolName = "team_dissolve"

const teamDissolveDescription = "Take back the team you manage while it has never held anyone but you: it is forgotten as if it had never been made, nobody is stopped, and this conversation goes on as an ordinary one without your team verbs. " +
	"Use it only when the person declines the team, for example by saying no to the helpers you proposed and asking to cancel. " +
	"It is offered only until anyone else joins the team or anything is said in its traffic; after that the person disbands the team from Teams."

const teamDissolveSchema = `{"type":"object","properties":{` + teamArgSchema + `},"additionalProperties":false}`

// teamRetiredDissolve is why team_dissolve left the belt of a conversation that
// still manages a team: the team was used, so it is not the manager's to undo.
const teamRetiredDissolve = "the team has held members or exchanged messages, so only the person can disband it, from Teams"

// dissolveTools is the manager's one verb for an untouched team.
func (a *Agent) dissolveTools() []bare.Tool {
	return []bare.Tool{
		{Name: teamDissolveToolName, Description: teamDissolveDescription, Schema: json.RawMessage(teamDissolveSchema), Execute: a.teamDissolveTool},
	}
}

func (a *Agent) teamDissolveTool(ctx context.Context, args json.RawMessage) (string, bool, error) {
	var p struct{ Team string }
	if err := decodeToolArguments(args, &p); err != nil {
		return invalidArgumentsPrefix + err.Error(), true, nil
	}
	t, role, refusal := a.teamTarget(p.Team, true)
	if refusal != "" {
		return refusal, true, nil
	}
	err := teams.Dissolve(a.config.teamProfile(), t.ID, role.key)
	if errors.Is(err, teams.ErrNotDissolvable) || errors.Is(err, teams.ErrRoot) {
		return fmt.Sprintf("%q has already held members or exchanged messages, so it is not yours to take back. Nothing was changed; the person can disband it from Teams.", t.Name), true, nil
	}
	if err != nil {
		return "The team could not be dissolved: " + err.Error(), true, nil
	}
	return fmt.Sprintf("Dissolved %q. Nobody was started or stopped, and this conversation goes on as an ordinary one; your team verbs leave at your next step.", t.Name), false, nil
}
