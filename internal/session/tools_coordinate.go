package session

// The `coordinate` tool: an ordinary chat asks another chat, sends to several
// separately, invites a participant into this discussion, inspects scope, or
// pauses new autonomous coordination.
//
// THE TOOL IS ABSENT WHEN Config.Collab IS NIL. A verb with nothing behind it
// is a model told it can message other chats, whose every call then refuses.
//
// Wave 3 actions (deliver/invite/inspect/selected/manage-folder/pause) stay.
// Execute actions (launch-or-join, inspect-work, steer, pause-work, stop-work,
// observe) are on the schema only when Config.Exec (or RegisterExecutor) is
// wired. NIL IS OFF: never a dummy completed launch. Software stamps origin
// at the wsapi wrapper; this schema has no `origin` and no `actor_id` — a
// model cannot claim person. grant is a citation of an existing grant, not
// a minted id.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

func init() { glossField["coordinate"] = "action" }

const coordinateDescription = "Coordinate other chats from this ordinary conversation: deliver a request or update, invite a participant into this discussion, inspect who is in scope, snapshot selected chats, manage a whole folder, or pause new autonomous coordination. This does not execute work for them. Origin is stamped by software as another agent, never as the person."

const coordinateExecDescription = "Coordinate other chats from this ordinary conversation: deliver a request or update, invite a participant into this discussion, inspect who is in scope, snapshot selected chats, manage a whole folder, pause new autonomous coordination, or — when an authentic grant says so — launch-or-join, inspect, steer, pause, stop, or observe authorized work. Origin is stamped by software as another agent, never as the person. A grant id is cited, not minted."

const coordinateSchemaJSON = `{
  "type": "object",
  "properties": {
    "action": {
      "type": "string",
      "enum": ["deliver", "invite", "inspect", "selected", "manage-folder", "pause"],
      "description": "deliver a line, invite a chat, inspect scope, snapshot selected chats, manage a folder, or pause new coordination."
    },
    "to": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Recipient conversation ids for deliver. One is a request; several are sent separately."
    },
    "body": {
      "type": "string",
      "description": "The line to deliver. Phrasing does not make it the person's instruction."
    },
    "pattern": {
      "type": "string",
      "description": "direct, fan-out, or discussion. Empty lets the wrapper choose from the recipient list."
    },
    "discussion": {
      "type": "string",
      "description": "Discussion id for deliver (joint) or invite."
    },
    "source": {
      "type": "string",
      "description": "Source chat id to invite as a representative."
    },
    "role": {
      "type": "string",
      "description": "A free label such as planner or critic, not a product type."
    },
    "chats": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Marked conversation ids for selected. A sibling filed later does not join."
    },
    "folder": {
      "type": "string",
      "description": "Folder id for manage-folder. Future descendants join this scope."
    }
  },
  "required": ["action"],
  "additionalProperties": false
}`

const coordinateExecSchemaJSON = `{
  "type": "object",
  "properties": {
    "action": {
      "type": "string",
      "enum": ["deliver", "invite", "inspect", "selected", "manage-folder", "pause", "launch-or-join", "inspect-work", "steer", "pause-work", "stop-work", "observe"],
      "description": "Wave 3 coordination, or launch-or-join / inspect / steer / pause-work / stop-work / observe authorized work."
    },
    "to": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Recipient conversation ids for deliver. One is a request; several are sent separately."
    },
    "body": {
      "type": "string",
      "description": "Deliver line, launch brief, or steer text. Phrasing does not make it the person's instruction."
    },
    "pattern": {
      "type": "string",
      "description": "direct, fan-out, or discussion. Empty lets the wrapper choose from the recipient list."
    },
    "discussion": {
      "type": "string",
      "description": "Discussion id for deliver (joint) or invite."
    },
    "source": {
      "type": "string",
      "description": "Source chat id to invite as a representative."
    },
    "role": {
      "type": "string",
      "description": "A free label such as planner or critic, not a product type."
    },
    "chats": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Marked conversation ids for selected. A sibling filed later does not join."
    },
    "folder": {
      "type": "string",
      "description": "Folder id for manage-folder. Future descendants join this scope."
    },
    "grant": {
      "type": "string",
      "description": "Existing grant to cite for launch-or-join or steer. Software does not mint this id."
    },
    "work": {
      "type": "string",
      "description": "Work id for inspect-work, steer, pause-work, stop-work, or observe."
    },
    "equivalence": {
      "type": "string",
      "description": "Equivalence key for launch-or-join so a second discussion of the same issue follows existing work."
    },
    "person_request": {
      "type": "string",
      "description": "Citation of the original person request for steer. A model-supplied person origin is refused."
    }
  },
  "required": ["action"],
  "additionalProperties": false
}`

type coordinateArgs struct {
	Action        string   `json:"action"`
	To            []string `json:"to"`
	Body          string   `json:"body"`
	Pattern       string   `json:"pattern"`
	Discussion    string   `json:"discussion"`
	Source        string   `json:"source"`
	Role          string   `json:"role"`
	Chats         []string `json:"chats"`
	Folder        string   `json:"folder"`
	Grant         string   `json:"grant"`
	Work          string   `json:"work"`
	Equivalence   string   `json:"equivalence"`
	PersonRequest string   `json:"person_request"`
}

func (a *Agent) coordinateTools() []bare.Tool {
	if a.config.Collab == nil {
		return nil
	}
	desc, schema := coordinateDescription, coordinateSchemaJSON
	if a.executor() != nil {
		desc, schema = coordinateExecDescription, coordinateExecSchemaJSON
	}
	return []bare.Tool{{
		Name:        "coordinate",
		Description: desc,
		Schema:      json.RawMessage(schema),
		Execute:     a.coordinateTool,
	}}
}

func (a *Agent) coordinateTool(ctx context.Context, args json.RawMessage) (string, bool, error) {
	var parsed coordinateArgs
	if err := decodeToolArguments(args, &parsed); err != nil {
		return "Invalid arguments: " + err.Error(), true, nil
	}
	switch strings.TrimSpace(parsed.Action) {
	case "deliver":
		return a.coordinateDeliver(ctx, parsed.To, parsed.Body, parsed.Pattern, parsed.Discussion)
	case "invite":
		return a.coordinateInvite(ctx, parsed.Discussion, parsed.Source, parsed.Role)
	case "inspect":
		return a.coordinateInspect(ctx)
	case "selected":
		return a.coordinateSelected(ctx, parsed.Chats)
	case "manage-folder":
		return a.coordinateManage(ctx, parsed.Folder)
	case "pause":
		return a.coordinatePause(ctx)
	default:
		return a.coordinateExecute(ctx, parsed)
	}
}

const coordinateActionsHelp = "deliver, invite, inspect, selected, manage-folder or pause"

func (a *Agent) coordinateExecute(ctx context.Context, parsed coordinateArgs) (string, bool, error) {
	exec := a.executor()
	if exec == nil {
		return "Invalid arguments: action must be " + coordinateActionsHelp + ".", true, nil
	}
	switch strings.TrimSpace(parsed.Action) {
	case "launch-or-join":
		return a.coordinateLaunch(ctx, exec, parsed)
	case "inspect-work":
		return a.coordinateInspectWork(ctx, exec, parsed.Work)
	case "steer":
		return a.coordinateSteer(ctx, exec, parsed)
	case "pause-work":
		return a.coordinatePauseWork(ctx, exec, parsed.Work)
	case "stop-work":
		return a.coordinateStopWork(ctx, exec, parsed.Work)
	case "observe":
		return a.coordinateObserve(ctx, exec, parsed.Work)
	default:
		return "Invalid arguments: action must be " + coordinateActionsHelp + ", launch-or-join, inspect-work, steer, pause-work, stop-work or observe.", true, nil
	}
}

func (a *Agent) coordinateLaunch(ctx context.Context, exec Exec, parsed coordinateArgs) (string, bool, error) {
	if err := a.pauseAffectedMutation(ctx); err != nil {
		return coordinateRefusal(err), true, nil
	}
	if strings.TrimSpace(parsed.Grant) == "" || strings.TrimSpace(parsed.Body) == "" {
		return "Invalid arguments: launch-or-join needs a grant and a brief.", true, nil
	}
	got, err := exec.LaunchOrJoin(ctx, strings.TrimSpace(parsed.Grant), parsed.Body, strings.TrimSpace(parsed.Equivalence))
	if err != nil {
		return coordinateRefusal(err), true, nil
	}
	return formatExecView(got), false, nil
}

func (a *Agent) coordinateInspectWork(ctx context.Context, exec Exec, workID string) (string, bool, error) {
	if strings.TrimSpace(workID) == "" {
		return "Invalid arguments: inspect-work needs a work id.", true, nil
	}
	got, err := exec.Inspect(ctx, strings.TrimSpace(workID))
	if err != nil {
		return coordinateRefusal(err), true, nil
	}
	return formatExecView(got), false, nil
}

func (a *Agent) coordinateSteer(ctx context.Context, exec Exec, parsed coordinateArgs) (string, bool, error) {
	if err := a.pauseAffectedMutation(ctx); err != nil {
		return coordinateRefusal(err), true, nil
	}
	if strings.TrimSpace(parsed.Work) == "" || strings.TrimSpace(parsed.Body) == "" {
		return "Invalid arguments: steer needs work, a body, and the original person request.", true, nil
	}
	if err := refuseModelPersonOrigin(parsed.PersonRequest); err != nil {
		return coordinateRefusal(err), true, nil
	}
	if err := exec.Steer(ctx, strings.TrimSpace(parsed.Work), parsed.Body, strings.TrimSpace(parsed.PersonRequest)); err != nil {
		return coordinateRefusal(err), true, nil
	}
	return "ok", false, nil
}

func (a *Agent) coordinatePauseWork(ctx context.Context, exec Exec, workID string) (string, bool, error) {
	if strings.TrimSpace(workID) == "" {
		return "Invalid arguments: pause-work needs a work id.", true, nil
	}
	if err := exec.PauseWork(ctx, strings.TrimSpace(workID)); err != nil {
		return coordinateRefusal(err), true, nil
	}
	return "ok", false, nil
}

func (a *Agent) coordinateStopWork(ctx context.Context, exec Exec, workID string) (string, bool, error) {
	if err := a.pauseAffectedMutation(ctx); err != nil {
		return coordinateRefusal(err), true, nil
	}
	if strings.TrimSpace(workID) == "" {
		return "Invalid arguments: stop-work needs a work id.", true, nil
	}
	if err := exec.StopWork(ctx, strings.TrimSpace(workID)); err != nil {
		return coordinateRefusal(err), true, nil
	}
	return "ok", false, nil
}

func (a *Agent) coordinateObserve(ctx context.Context, exec Exec, workID string) (string, bool, error) {
	if strings.TrimSpace(workID) == "" {
		return "Invalid arguments: observe needs a work id.", true, nil
	}
	got, err := exec.Observe(ctx, strings.TrimSpace(workID))
	if err != nil {
		return coordinateRefusal(err), true, nil
	}
	return formatExecResult(got), false, nil
}

func (a *Agent) coordinateDeliver(ctx context.Context, to []string, body, pattern, discussion string) (string, bool, error) {
	if strings.TrimSpace(body) == "" || len(copyChatIDs(to)) == 0 {
		return "Invalid arguments: deliver needs a body and at least one recipient.", true, nil
	}
	got, err := a.config.Collab.Deliver(ctx, copyChatIDs(to), body, strings.TrimSpace(pattern), strings.TrimSpace(discussion))
	if err != nil {
		return coordinateRefusal(err), true, nil
	}
	return formatCollabReceipts(got), false, nil
}

func (a *Agent) coordinateInvite(ctx context.Context, discussion, source, role string) (string, bool, error) {
	if strings.TrimSpace(discussion) == "" || strings.TrimSpace(source) == "" {
		return "Invalid arguments: invite needs a discussion and a source chat.", true, nil
	}
	role = strings.TrimSpace(role)
	if err := a.config.Collab.Invite(ctx, discussion, source, role); err != nil {
		return coordinateRefusal(err), true, nil
	}
	excerpts := a.collabSourceExcerpts(ctx, source)
	guidance := a.collabSourceGuidance(ctx, source)
	body, err := a.consultCollabParticipant(ctx, role, source, excerpts, guidance)
	if err != nil {
		return "invited; could not consult: " + err.Error(), true, nil
	}
	inv := CollabInvocation{
		ID: mintCollabInvocationID(), ActorID: source + ":" + role, Role: role, Source: source,
	}
	if err := a.rememberInvocation(inv); err != nil {
		return coordinateRefusal(err), true, nil
	}
	if err := a.config.Collab.Contribute(ctx, discussion, inv, body); err != nil {
		return coordinateRefusal(err), true, nil
	}
	return "ok", false, nil
}

func (a *Agent) coordinateInspect(ctx context.Context) (string, bool, error) {
	got, err := a.config.Collab.InspectScope(ctx)
	if err != nil {
		return coordinateRefusal(err), true, nil
	}
	return formatCollabScope(got), false, nil
}

func (a *Agent) coordinateSelected(ctx context.Context, chats []string) (string, bool, error) {
	ids := copyChatIDs(chats)
	if len(ids) == 0 {
		return "Invalid arguments: selected needs at least one chat id.", true, nil
	}
	if err := a.config.Collab.CoordinateSelected(ctx, ids); err != nil {
		return coordinateRefusal(err), true, nil
	}
	return "ok", false, nil
}

func (a *Agent) coordinateManage(ctx context.Context, folder string) (string, bool, error) {
	if strings.TrimSpace(folder) == "" {
		return "Invalid arguments: manage-folder needs a folder id.", true, nil
	}
	if err := a.config.Collab.ManageFolder(ctx, strings.TrimSpace(folder)); err != nil {
		return coordinateRefusal(err), true, nil
	}
	return "ok", false, nil
}

func (a *Agent) coordinatePause(ctx context.Context) (string, bool, error) {
	if err := a.config.Collab.Pause(ctx); err != nil {
		return coordinateRefusal(err), true, nil
	}
	return "ok", false, nil
}

func coordinateRefusal(err error) string {
	if err == nil {
		return "Could not coordinate."
	}
	return "Could not coordinate: " + err.Error()
}

func formatCollabReceipts(got []CollabReceipt) string {
	if len(got) == 0 {
		return "ok"
	}
	var b strings.Builder
	for i, one := range got {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s  %s  %s", one.ToChatID, one.State, one.DeliveryID)
	}
	return b.String()
}

func formatCollabScope(got CollabScope) string {
	kind := strings.TrimSpace(got.Kind)
	if kind == "" {
		kind = "scope"
	}
	var b strings.Builder
	b.WriteString(kind)
	if got.FolderID != "" {
		b.WriteString("  ")
		b.WriteString(got.FolderID)
	}
	for _, id := range got.ChatIDs {
		b.WriteByte('\n')
		b.WriteString(id)
	}
	return b.String()
}

func copyChatIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
