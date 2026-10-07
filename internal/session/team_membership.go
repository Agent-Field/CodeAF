package session

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/teams"
)

const teamAddDescription = "Add an existing saved conversation to a team you manage. With no conversation, list available conversation ids and transcript paths; optionally filter by query. Copy a returned id or path into conversation. It retains its context, other memberships and reporting manager. Use team_start to create a new member instead."
const teamAddSchema = `{"type":"object","properties":{"conversation":{"type":"string","description":"An existing conversation id or transcript path from the listing."},"query":{"type":"string","description":"Filter the listing by title or workspace."},` + teamArgSchema + `},"additionalProperties":false}`
const teamRemoveDescription = "Remove a member from a team you manage. Its conversation, running work and memberships in other teams survive. Removing its reporting membership leaves it independent; other managers gain no authority. A manager cannot remove itself; appoint a replacement through Teams first."
const teamRemoveSchema = `{"type":"object","properties":{"handle":{"type":"string","description":"The member's alias in this team."},` + teamArgSchema + `},"required":["handle"],"additionalProperties":false}`

func (a *Agent) teamAddTool(ctx context.Context, args json.RawMessage) (string, bool, error) {
	var p struct{ Conversation, Query, Team string }
	if err := decodeToolArguments(args, &p); err != nil {
		return invalidArgumentsPrefix + err.Error(), true, nil
	}
	t, role, refusal := a.teamTarget(p.Team, true)
	if refusal != "" {
		return refusal, true, nil
	}
	if t.Root {
		return "All teams holds team managers; add conversations to a regular team.", true, nil
	}
	query := strings.ToLower(strings.TrimSpace(p.Query))
	var choices []SessionRow
	for _, project := range ReadWorld(PlacesRoot()).Projects {
		for _, row := range project.Sessions {
			if !row.Archived && strings.Contains(strings.ToLower(row.Title+" "+row.Workspace+" "+row.ProjectDir), query) {
				choices = append(choices, row)
			}
		}
	}
	if p.Conversation == "" {
		var b strings.Builder
		b.WriteString("Existing conversations (copy an id or transcript path into conversation):\n")
		for _, row := range choices[:min(len(choices), teamReadMax)] {
			fmt.Fprintf(&b, "- %s · %s · %s\n", filepath.Base(filepath.Dir(row.Transcript)), row.Title, row.Transcript)
		}
		if len(choices) == 0 {
			b.WriteString("No matching saved conversations. Try another query or use team_start for a new member.")
		}
		return b.String(), false, nil
	}
	var found []SessionRow
	for _, row := range choices {
		if row.Transcript == p.Conversation || filepath.Base(filepath.Dir(row.Transcript)) == p.Conversation {
			found = append(found, row)
		}
	}
	if len(found) != 1 {
		return "That conversation is not uniquely present in the saved-conversation listing. Call team_add without conversation and copy its id or transcript path.", true, nil
	}
	row := found[0]
	key := filepath.Clean(row.Transcript)
	if real, err := filepath.EvalSymlinks(key); err == nil {
		key = real
	}
	where := row.ProjectDir
	if where == "" {
		where = row.Workspace
	}
	err := teams.Update(a.config.teamProfile(), func(f *teams.File) error {
		current, ok := f.Team(t.ID)
		if !ok || current.Closed() || current.Manager != role.key {
			return fmt.Errorf("this conversation no longer manages the selected team")
		}
		return f.AddMember(t.ID, teams.Member{Key: key, File: row.Transcript, Where: where, Word: row.Title, JoinedAt: time.Now()})
	})
	if err != nil {
		return "The membership could not be added: " + err.Error(), true, nil
	}
	return fmt.Sprintf("Added %q to %q. Its conversation and other memberships are preserved; its reporting manager was not reassigned.", row.Title, t.Name), false, nil
}

func (a *Agent) teamRemoveTool(ctx context.Context, args json.RawMessage) (string, bool, error) {
	var p struct{ Handle, Team string }
	if err := decodeToolArguments(args, &p); err != nil {
		return invalidArgumentsPrefix + err.Error(), true, nil
	}
	t, role, refusal := a.teamTarget(p.Team, true)
	if refusal != "" {
		return refusal, true, nil
	}
	m, ok := teamMemberByHandle(t, p.Handle)
	if !ok {
		return "No member has that alias in the selected team.", true, nil
	}
	if m.Key == t.Manager || t.Root {
		return "Appoint a replacement manager through Teams before removing this manager.", true, nil
	}
	err := teams.Update(a.config.teamProfile(), func(f *teams.File) error {
		current, ok := f.Team(t.ID)
		if !ok || current.Closed() || current.Manager != role.key {
			return fmt.Errorf("this conversation no longer manages the selected team")
		}
		return f.RemoveMember(t.ID, m.Key)
	})
	if err != nil {
		return "The membership could not be removed: " + err.Error(), true, nil
	}
	return fmt.Sprintf("Removed @%s from %q. Its conversation, current work and other memberships remain.", m.Handle, t.Name), false, nil
}
