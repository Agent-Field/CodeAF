package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// ── A CREW'S MANAGER LINKS WHAT ITS CREW MADE ───────────────────────────────
//
// A crew's linked items (a file, a pull request, an issue, a factory job) are
// what the app lists on the crew as its work. They could only be linked from
// the desk, over its wire (`crews.link`), so a crew that wrote a launch post
// and a patch plan showed none of it on the crew (blackmac-team/CREWS.md, gap
// 6). team_link is the manager's verb for it: the manager is the one who knows
// which of its members' files are the crew's result.
//
// THE ITEMS ARE THE APP'S, so the verb is a door the process sets
// ([SetTeamLinker]): the app records the link in its crews layer and tells the
// window at once; an engine process records it in the same layer, which the
// app reads the next time the crew's items are asked for. A process with no
// such door leaves the verb off the belt (a capability that cannot work is
// absent, not broken).

const teamLinkToolName = "team_link"

// TeamLink is one link a manager asks for.
type TeamLink struct {
	// Team is the crew's team id.
	Team string
	// Kind is file, pr, issue or job.
	Kind string
	// Ref is the file's absolute path, or the pull request, issue or job as
	// the manager names it.
	Ref   string
	Title string
}

// teamLinker is the process's door for recording a link, nil where none.
var teamLinker atomic.Pointer[func(TeamLink) (string, error)]

// SetTeamLinker gives this process a way to record a crew's linked item; the
// function answers the item's id. nil takes the door (and the verb) away.
func SetTeamLinker(link func(TeamLink) (string, error)) {
	if link == nil {
		teamLinker.Store(nil)
		return
	}
	teamLinker.Store(&link)
}

const teamLinkDescription = "Link something your crew made to the crew, so the app lists it with the crew's work: a file (its path), a pull request, an issue, or a factory job. " +
	"Link the results the person should open (the final draft, the reviewed plan), not every scratch file. It changes nothing in the file itself."

const teamLinkSchema = `{"type":"object","properties":{"ref":{"type":"string","description":"For a file, its path (relative to your folder or absolute). For a pull request, issue or job, its number or link."},` +
	`"kind":{"type":"string","enum":["file","pr","issue","job"],"description":"Default file."},` +
	`"title":{"type":"string","description":"A short name the person will recognise. Optional."},` +
	teamArgSchema + `},"required":["ref"],"additionalProperties":false}`

// teamLinkTools is the verb, when this process has the door.
func (a *Agent) teamLinkTools() []bare.Tool {
	if teamLinker.Load() == nil {
		return nil
	}
	return []bare.Tool{{Name: teamLinkToolName, Description: teamLinkDescription, Schema: json.RawMessage(teamLinkSchema), Execute: a.teamLinkTool}}
}

func (a *Agent) teamLinkTool(_ context.Context, args json.RawMessage) (string, bool, error) {
	var p struct {
		Ref   string `json:"ref"`
		Kind  string `json:"kind"`
		Title string `json:"title"`
		Team  string `json:"team"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "team_link needs ref: " + err.Error(), true, nil
	}
	door := teamLinker.Load()
	if door == nil {
		return "This conversation cannot link items to a crew here. Nothing was linked.", true, nil
	}
	team, _, refusal := a.teamTarget(p.Team, true)
	if refusal != "" {
		return refusal, true, nil
	}
	kind := strings.TrimSpace(p.Kind)
	if kind == "" {
		kind = "file"
	}
	switch kind {
	case "file", "pr", "issue", "job":
	default:
		return "kind is file, pr, issue or job. Nothing was linked.", true, nil
	}
	ref := strings.TrimSpace(p.Ref)
	if ref == "" {
		return "team_link needs ref. Nothing was linked.", true, nil
	}
	title := strings.TrimSpace(p.Title)
	if kind == "file" {
		if !filepath.IsAbs(ref) {
			ref = filepath.Join(a.config.Workspace, ref)
		}
		ref = filepath.Clean(ref)
		info, err := os.Stat(ref)
		if err != nil {
			return fmt.Sprintf("There is no file at %s. Nothing was linked.", ref), true, nil
		}
		if info.IsDir() {
			return fmt.Sprintf("%s is a folder; link the files in it. Nothing was linked.", ref), true, nil
		}
		if title == "" {
			title = filepath.Base(ref)
		}
	}
	id, err := (*door)(TeamLink{Team: team.ID, Kind: kind, Ref: ref, Title: title})
	if err != nil {
		return "The link was not recorded: " + oneLineTeam(err.Error()), true, nil
	}
	return fmt.Sprintf("Linked %s to %q as %s; the app lists it with the crew's work.", ref, team.Name, id), false, nil
}
