package session

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// Teams suggested for a person's conversations, asked once per press.
//
// The conversations view has an Organize button. The surface groups the
// conversations that share a project folder itself, which is free and exact,
// and asks here once for the groupings a folder cannot see: conversations about
// one topic spread over several folders, or one that belongs in a team the
// person already made.
//
// IT IS NAMETEAM'S ERRAND WITH A LONGER LIST. It goes through the same door
// ([Agent.callRoleChecked]) on the same role ([roles.RoleTitle]), so it lands on
// the cheap tier a person has configured for names, falls through one rung at
// most, and is billed against the model that answered, off every turn's clock.
//
// WHAT COMES BACK IS A PROPOSAL AND IS READ AS ONE. Every conversation and team
// is handed to the model under a short ref (c1, t1) and every ref in the answer
// is looked up again: one it was not given is dropped, a team left with too few
// members is dropped, a name is cleaned as a team name is, and nothing in the
// answer can remove a member or rename a team, because the answer has no field
// that could say so and a new team named like an existing one is read as
// additions to it. The surface shows what is left and applies only what the
// person ticks.

// TeamProposalConversation is one conversation offered to the model: the key the
// surface knows it by, its title and its project folder.
type TeamProposalConversation struct {
	Key    string
	Title  string
	Folder string
}

// TeamProposalTeam is one team the person already has: its id, its name, and
// the keys of its members among the conversations offered.
type TeamProposalTeam struct {
	ID      string
	Name    string
	Members []string
}

// TeamProposalInput is everything one ask is about.
type TeamProposalInput struct {
	Conversations []TeamProposalConversation
	Teams         []TeamProposalTeam
}

// ProposedTeam is a new team the model suggests: its cleaned name, the keys of
// its members, and a few words on why.
type ProposedTeam struct {
	Name    string
	Members []string
	Reason  string
}

// ProposedAddition is conversations the model would add to a team that exists,
// by the team's id and the members' keys.
type ProposedAddition struct {
	TeamID  string
	Members []string
}

// TeamProposal is one validated answer. Model is the model that answered and
// PromptChars the size of what it was asked, so the surface can say about what
// the ask cost.
type TeamProposal struct {
	New         []ProposedTeam
	Additions   []ProposedAddition
	Model       string
	PromptChars int
}

// teamProposeSystem and teamProposeAsk are the instruction. The shape of the
// answer is spelled out in full, since a small model follows an example better
// than a description.
const (
	teamProposeSystem = "You sort a person's coding conversations into teams."
	teamProposeAsk    = `Suggest teams that group these conversations by shared project or topic, and existing teams that more conversations belong in. A conversation may be in several teams. Only use the refs above. Never rename or remove anything. Suggest nothing you are unsure of; empty lists are fine.
Answer with JSON only, in this shape:
{"new":[{"name":"one to three lowercase words","members":["c1","c2"],"reason":"a few words"}],"add":[{"team":"t1","members":["c3"]}]}`
)

// The bounds on one ask: how much of each title is sent, how many
// conversations and teams, and how many member titles describe a team. A
// person with more than this many open is organized a screenful at a time.
const (
	teamProposeClip     = 80
	teamProposeConvs    = 60
	teamProposeTeams    = 24
	teamProposeTeamSeen = 8
	teamProposeReason   = 48
	teamProposeMaxNew   = 8
)

// ProposeTeams asks the naming role, once, which teams the conversations in in
// could form and which existing teams more of them belong in. The caller bounds
// it with ctx. An answer that is not the JSON asked for is an error, never an
// empty proposal; an empty proposal is an answer.
func (a *Agent) ProposeTeams(ctx context.Context, in TeamProposalInput) (TeamProposal, error) {
	ask, refs := teamProposePrompt(in)
	if len(refs.convs) < 2 {
		return TeamProposal{}, errors.New("fewer than two conversations to organize")
	}
	a.mu.Lock()
	model, closed := a.model, a.closed
	a.mu.Unlock()
	if closed {
		return TeamProposal{}, errors.New("the conversation is closed")
	}
	response, named, err := a.callRoleChecked(ctx, roles.RoleTitle, model,
		[]ai.Message{textMessage("system", teamProposeSystem), textMessage("user", ask)},
		func(response *ai.Response, named string) bool {
			if _, ok := parseTeamProposal(response.Text(), in, refs); ok {
				return true
			}
			a.addDetachedUsageAs(response, named, 1, auxRoleTitle)
			return false
		})
	if err != nil {
		return TeamProposal{}, err
	}
	if response == nil {
		return TeamProposal{}, errEmptyAnswer
	}
	a.addDetachedUsageAs(response, named, 1, auxRoleTitle)
	out, ok := parseTeamProposal(response.Text(), in, refs)
	if !ok {
		return TeamProposal{}, errors.New("the answer was not a proposal")
	}
	out.Model = named
	out.PromptChars = len(teamProposeSystem) + len(ask)
	return out, nil
}

// teamProposeRefs maps the refs one ask used back to what they stand for.
type teamProposeRefs struct {
	convs map[string]string // c1 → conversation key
	teams map[string]string // t1 → team id
}

// teamProposePrompt is the user message for in, and the refs it used.
//
//	Conversations:
//	c1 | cpu profiling of the relay | folder: codeaf
//	Existing teams:
//	t1 | harbor | relay audit; footprint table
func teamProposePrompt(in TeamProposalInput) (string, teamProposeRefs) {
	refs := teamProposeRefs{convs: map[string]string{}, teams: map[string]string{}}
	title := map[string]string{}
	var b strings.Builder
	b.WriteString("Conversations:\n")
	for _, c := range in.Conversations {
		t := oneLine(c.Title)
		if c.Key == "" || t == "" || title[c.Key] != "" || len(refs.convs) >= teamProposeConvs {
			continue
		}
		ref := "c" + strconv.Itoa(len(refs.convs)+1)
		refs.convs[ref] = c.Key
		title[c.Key] = clip(t, teamProposeClip)
		b.WriteString(ref + " | " + title[c.Key])
		if f := oneLine(c.Folder); f != "" {
			b.WriteString(" | folder: " + clip(f, teamProposeClip))
		}
		b.WriteString("\n")
	}
	if len(in.Teams) > 0 {
		b.WriteString("Existing teams:\n")
	}
	for _, t := range in.Teams {
		name := oneLine(t.Name)
		if t.ID == "" || name == "" || len(refs.teams) >= teamProposeTeams {
			continue
		}
		ref := "t" + strconv.Itoa(len(refs.teams)+1)
		refs.teams[ref] = t.ID
		var seen []string
		for _, key := range t.Members {
			if s := title[key]; s != "" && len(seen) < teamProposeTeamSeen {
				seen = append(seen, s)
			}
		}
		b.WriteString(ref + " | " + clip(name, teamProposeClip) + " | " + strings.Join(seen, "; ") + "\n")
	}
	b.WriteString("\n" + teamProposeAsk)
	return b.String(), refs
}

// teamProposeWire is the answer's shape as asked for.
type teamProposeWire struct {
	New []struct {
		Name    string   `json:"name"`
		Members []string `json:"members"`
		Reason  string   `json:"reason"`
	} `json:"new"`
	Add []struct {
		Team    string   `json:"team"`
		Members []string `json:"members"`
	} `json:"add"`
}

// parseTeamProposal reads raw as a proposal about in, whose refs are refs. The
// bool is false when raw holds no JSON object of the asked shape at all.
//
// Validation, in order: every member ref is looked up and one the ask did not
// give is dropped, and so is a second mention of the same one; a new team's
// name is cleaned as [cleanTeamName] cleans one and a team left without a name
// is dropped; a new team named like an existing team, compared without case,
// becomes additions to that team, because a name is not the model's to take;
// two new teams of one name are one; a new team of fewer than two members is no
// team; an addition's members already in the team are dropped, and an
// addition left empty is dropped.
func parseTeamProposal(raw string, in TeamProposalInput, refs teamProposeRefs) (TeamProposal, bool) {
	body := strings.TrimSpace(raw)
	open, end := strings.IndexByte(body, '{'), strings.LastIndexByte(body, '}')
	if open < 0 || end <= open {
		return TeamProposal{}, false
	}
	var wire teamProposeWire
	if err := json.Unmarshal([]byte(body[open:end+1]), &wire); err != nil {
		return TeamProposal{}, false
	}
	byID := map[string]TeamProposalTeam{}
	byName := map[string]string{}
	for _, t := range in.Teams {
		byID[t.ID] = t
		byName[strings.ToLower(strings.TrimSpace(t.Name))] = t.ID
	}
	keys := func(members []string) []string {
		var out []string
		seen := map[string]bool{}
		for _, ref := range members {
			key, ok := refs.convs[strings.ToLower(strings.TrimSpace(ref))]
			if ok && !seen[key] {
				seen[key] = true
				out = append(out, key)
			}
		}
		return out
	}
	var out TeamProposal
	adds := map[string][]string{}
	var addOrder []string
	add := func(id string, members []string) {
		t := byID[id]
		for _, key := range members {
			if teamProposeHas(t.Members, key) || teamProposeHas(adds[id], key) {
				continue
			}
			if _, ok := adds[id]; !ok {
				addOrder = append(addOrder, id)
			}
			adds[id] = append(adds[id], key)
		}
	}
	newAt := map[string]int{}
	for _, n := range wire.New {
		name := cleanTeamName(n.Name)
		members := keys(n.Members)
		if name == "" || len(members) == 0 {
			continue
		}
		if id, ok := byName[name]; ok {
			add(id, members)
			continue
		}
		if i, ok := newAt[name]; ok {
			for _, key := range members {
				if !teamProposeHas(out.New[i].Members, key) {
					out.New[i].Members = append(out.New[i].Members, key)
				}
			}
			continue
		}
		newAt[name] = len(out.New)
		out.New = append(out.New, ProposedTeam{Name: name, Members: members, Reason: clip(oneLine(n.Reason), teamProposeReason)})
	}
	kept := out.New[:0]
	for _, t := range out.New {
		if len(t.Members) >= 2 && len(kept) < teamProposeMaxNew {
			kept = append(kept, t)
		}
	}
	out.New = kept
	for _, n := range wire.Add {
		id, ok := refs.teams[strings.ToLower(strings.TrimSpace(n.Team))]
		if !ok {
			continue
		}
		add(id, keys(n.Members))
	}
	for _, id := range addOrder {
		out.Additions = append(out.Additions, ProposedAddition{TeamID: id, Members: adds[id]})
	}
	return out, true
}

func teamProposeHas(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}
