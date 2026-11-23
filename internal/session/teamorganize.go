package session

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/charmbracelet/x/ansi"
)

// OrganizeNameCells bounds folder names in both surfaces.
const OrganizeNameCells = 16

// OrganizeWait bounds one naming request in both surfaces.
const OrganizeWait = 10 * time.Second

// OrganizeConversation is a conversation's stable key, title and project folder.
type OrganizeConversation struct{ Key, Title, Folder string }

// OrganizeSuggestion changes no membership until a person applies it.
type OrganizeSuggestion struct {
	Team, Name, Folder, Reason string
	Keys                       []string
	FromFolder                 bool
}

// OrganizeUnassigned keeps named conversations outside open crews, in input order.
// The root's membership is navigation, not a crew assignment.
func OrganizeUnassigned(convs []OrganizeConversation, teams []teamstore.Team) []OrganizeConversation {
	assigned := map[string]bool{}
	for _, t := range teams {
		if !t.Root && !t.Closed() {
			for _, m := range t.Members {
				assigned[m.Key] = true
			}
		}
	}
	seen := map[string]bool{}
	var out []OrganizeConversation
	for _, c := range convs {
		if c.Key != "" && strings.TrimSpace(c.Title) != "" && !assigned[c.Key] && !seen[c.Key] {
			out = append(out, c)
			seen[c.Key] = true
		}
	}
	return out
}

// OrganizeInput gives the naming role titles and folders, plus the supplied teams.
func OrganizeInput(convs []OrganizeConversation, teams []teamstore.Team) TeamProposalInput {
	in := TeamProposalInput{}
	for _, c := range convs {
		in.Conversations = append(in.Conversations, TeamProposalConversation{Key: c.Key, Title: c.Title, Folder: filepath.Base(filepath.Clean(c.Folder))})
	}
	for _, t := range teams {
		pt := TeamProposalTeam{ID: t.ID, Name: t.Name}
		for _, m := range t.Members {
			pt.Members = append(pt.Members, m.Key)
		}
		in.Teams = append(in.Teams, pt)
	}
	return in
}

func organizeTeamIndex(teams []teamstore.Team, id string) int {
	for i, t := range teams {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// OrganizeFolderName is the team name a folder gives: its last element,
// lowercased and cut to [OrganizeNameCells], "" for a root.
func OrganizeFolderName(where string) string {
	base := filepath.Base(filepath.Clean(where))
	if base == "/" || base == "." || base == "" {
		return ""
	}
	return ansi.Truncate(strings.ToLower(base), OrganizeNameCells, "")
}

// OrganizeTeamNamed looks up a name without case. openOnly excludes the root
// and closed crews; other team editors retain their existing all-teams lookup.
func OrganizeTeamNamed(teams []teamstore.Team, name string, openOnly bool) int {
	for i, t := range teams {
		if openOnly && (t.Root || t.Closed()) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(t.Name), strings.TrimSpace(name)) {
			return i
		}
	}
	return -1
}

// OrganizeFolders is the folder pass: every folder two or more conversations
// share becomes a new team named after it, unless one team already holds them
// all, and unless a team already has the folder's name, when the ones it is
// missing are suggested for it instead. Two folders of one name are one team.
// It is pure and exact, so the same conversations always give the same rows.
func OrganizeFolders(convs []OrganizeConversation, teams []teamstore.Team) []OrganizeSuggestion {
	groups := map[string][]string{}
	var order []string
	for _, c := range convs {
		if strings.TrimSpace(c.Folder) == "" {
			continue
		}
		w := filepath.Clean(c.Folder)
		if _, ok := groups[w]; !ok {
			order = append(order, w)
		}
		if !orgHas(groups[w], c.Key) {
			groups[w] = append(groups[w], c.Key)
		}
	}
	var out []OrganizeSuggestion
	for _, w := range order {
		keys := groups[w]
		name := OrganizeFolderName(w)
		if len(keys) < 2 || name == "" || organizeHeldTogether(teams, keys) {
			continue
		}
		if i := OrganizeTeamNamed(teams, name, false); i >= 0 {
			var missing []string
			for _, k := range keys {
				if !teams[i].Holds(k) {
					missing = append(missing, k)
				}
			}
			out = orgMergeInto(out, OrganizeSuggestion{Team: teams[i].ID, Name: teams[i].Name, Keys: missing, Folder: w, FromFolder: true})
			continue
		}
		out = orgMergeInto(out, OrganizeSuggestion{Name: name, Keys: keys, Folder: w, FromFolder: true})
	}
	return out
}

// organizeHeldTogether suppresses folders already held together by one team.
func organizeHeldTogether(teams []teamstore.Team, keys []string) bool {
	for _, t := range teams {
		all := true
		for _, key := range keys {
			if !t.Holds(key) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// OrganizeUnassignedFolders applies the desktop's candidate rule before grouping.
func OrganizeUnassignedFolders(convs []OrganizeConversation, teams []teamstore.Team) []OrganizeSuggestion {
	return OrganizeFolders(OrganizeUnassigned(convs, teams), organizeOpenTeams(teams))
}

func organizeOpenTeams(teams []teamstore.Team) []teamstore.Team {
	var out []teamstore.Team
	for _, t := range teams {
		if !t.Root && !t.Closed() {
			out = append(out, t)
		}
	}
	return out
}

// orgMergeInto adds p to props, folding it into a row that is the same
// suggestion already: the same existing team, or a new team of the same name.
// The row already there keeps its reason and its place.
func orgMergeInto(props []OrganizeSuggestion, p OrganizeSuggestion) []OrganizeSuggestion {
	if len(p.Keys) == 0 {
		return props
	}
	for i, q := range props {
		same := p.Team != "" && q.Team == p.Team ||
			p.Team == "" && q.Team == "" && strings.EqualFold(q.Name, p.Name)
		if !same {
			continue
		}
		for _, k := range p.Keys {
			if !orgHas(props[i].Keys, k) {
				props[i].Keys = append(append([]string(nil), props[i].Keys...), k)
			}
		}
		return props
	}
	return append(props, p)
}

func orgHas(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

// OrganizeFromModel is the model's answer as rows. The answer was validated
// by the session; what is checked again here is what may have moved while it
// was asked: a team that is gone, and a member a team now holds.
func OrganizeFromModel(res TeamProposal, teams []teamstore.Team) []OrganizeSuggestion {
	var out []OrganizeSuggestion
	for _, n := range res.New {
		if i := OrganizeTeamNamed(teams, n.Name, false); i >= 0 {
			out = append(out, OrganizeSuggestion{Team: teams[i].ID, Name: teams[i].Name, Keys: orgMissing(teams[i], n.Members), Reason: n.Reason})
			continue
		}
		out = append(out, OrganizeSuggestion{Name: n.Name, Keys: n.Members, Reason: n.Reason})
	}
	for _, add := range res.Additions {
		i := organizeTeamIndex(teams, add.TeamID)
		if i < 0 {
			continue
		}
		out = append(out, OrganizeSuggestion{Team: teams[i].ID, Name: teams[i].Name, Keys: orgMissing(teams[i], add.Members), Reason: add.Reason})
	}
	return out
}

// orgMissing is keys less the ones t already holds.
func orgMissing(t teamstore.Team, keys []string) []string {
	var out []string
	for _, k := range keys {
		if !t.Holds(k) && !orgHas(out, k) {
			out = append(out, k)
		}
	}
	return out
}

// OrganizeMerge is the two passes as one list, new teams first. The folder
// pass wins: a model team with a folder team's name, or with exactly its
// members, is dropped, and a model addition to a team the folder pass already
// adds to only brings the members that row lacks.
func OrganizeMerge(folder, model []OrganizeSuggestion) []OrganizeSuggestion {
	return organizeMerge(folder, model, false)
}

// organizeMerge reserves each member once only when the desktop requests it.
func organizeMerge(folder, model []OrganizeSuggestion, unique bool) []OrganizeSuggestion {
	props := append([]OrganizeSuggestion(nil), folder...)
	used := map[string]bool{}
	for _, p := range folder {
		for _, k := range p.Keys {
			used[k] = true
		}
	}
	for _, p := range model {
		if p.Team == "" {
			dup := false
			for _, q := range folder {
				if q.Team == "" && (strings.EqualFold(q.Name, p.Name) || orgSameKeys(q.Keys, p.Keys)) {
					dup = true
					break
				}
			}
			if dup {
				continue
			}
		}
		if unique {
			kept := []string{}
			for _, k := range p.Keys {
				if !used[k] && !orgHas(kept, k) {
					kept = append(kept, k)
				}
			}
			p.Keys = kept
			if p.Team == "" && len(kept) < 2 {
				continue
			}
			for _, k := range kept {
				used[k] = true
			}
		}
		props = orgMergeInto(props, p)
	}
	var out []OrganizeSuggestion
	for _, p := range props {
		if p.Team == "" && len(p.Keys) >= 2 {
			out = append(out, p)
		}
	}
	for _, p := range props {
		if p.Team != "" && len(p.Keys) > 0 {
			out = append(out, p)
		}
	}
	return out
}

func orgSameKeys(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, k := range a {
		if !orgHas(b, k) {
			return false
		}
	}
	return true
}

// ErrNoNamingModel tells a surface to keep the free folder suggestions alone.
var ErrNoNamingModel = errors.New("no naming model is set")

// TeamProposer is shared by local agents and the hosted Teams.Propose door.
type TeamProposer interface {
	ProposeTeams(context.Context, TeamProposalInput) (TeamProposal, error)
}

// OrganizeModel asks once and returns only rows the model contributes. Folder
// members and folder names are reserved before accepting any model row.
func OrganizeModel(ctx context.Context, proposer TeamProposer, convs []OrganizeConversation, teams []teamstore.Team) ([]OrganizeSuggestion, TeamProposal, error) {
	if proposer == nil {
		return nil, TeamProposal{}, ErrNoNamingModel
	}
	convs = OrganizeUnassigned(convs, teams)
	teams = organizeOpenTeams(teams)
	in := OrganizeInput(convs, teams)
	in.RequireNaming = true
	if len(in.Conversations) == 0 {
		return nil, TeamProposal{}, nil
	}
	res, err := proposer.ProposeTeams(ctx, in)
	if err != nil {
		return nil, res, err
	}
	allowed := map[string]bool{}
	titles := map[string]string{}
	for _, c := range in.Conversations {
		allowed[c.Key] = true
		titles[c.Key] = c.Title
	}
	rows := OrganizeFromModel(res, teams)
	for i := range rows {
		var keys []string
		for _, k := range rows[i].Keys {
			if allowed[k] {
				keys = append(keys, k)
			}
		}
		rows[i].Keys = keys
	}
	folder := OrganizeFolders(convs, teams)
	merged := organizeMerge(folder, rows, true)
	reserved := map[string]bool{}
	for _, p := range folder {
		for _, k := range p.Keys {
			reserved[k] = true
		}
	}
	var out []OrganizeSuggestion
	for _, p := range merged {
		var keys []string
		for _, k := range p.Keys {
			if !reserved[k] {
				keys = append(keys, k)
			}
		}
		p.Keys = keys
		if p.Reason == "" {
			var why []string
			for _, k := range keys {
				why = append(why, titles[k])
			}
			p.Reason = strings.Join(why, "; ")
		}
		if (p.Team == "" && len(keys) >= 2) || (p.Team != "" && len(keys) > 0) {
			out = append(out, p)
		}
	}
	return out, res, nil
}
