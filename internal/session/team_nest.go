package session

// ── NESTED TEAMS: SUB-TEAMS, CONFLICTS, ONE LEVEL, AND THE GLOBAL MANAGER ────
//
// The rulings (c-5, c-6, c-7; docs/design/conversations-and-teams/DESIGN.md,
// section 8): work is handed down a tree of teams and decisions travel up it.
// This file is the session's half of the tree:
//
//   - A SUB-TEAM IS STARTED LIKE A MEMBER. `team_start` of kind team makes the
//     child team in the store under the manager's team (refused past the
//     effective depth limit or under a closed team, with the reason), writes
//     its share of the pool on it ([teams.File.SubTeamCap]), moves in the
//     members it names, and writes ONE start to the parent's Traffic, the start
//     the interface already carries out: it opens the new conversation behind
//     the one in front and adds it to the parent team under its handle. The
//     start names the child ([teams.Entry.Team]), and the new conversation,
//     reading its brief at its first boundary, makes itself the child's manager
//     ([Agent.claimSubTeams]). So its manager is a member of the parent and
//     reports up by the ordinary home rule, and the interface needs nothing new.
//   - A CONFLICT IS DECLARED, NEVER DETECTED. `team_raise` names the other
//     parties by handle (across teams as `team/@handle`), finds the lowest
//     common managed ancestor of all of them ([teams.File.LCA]), and raises a
//     conflict packet there, or to the person when there is none. That manager
//     is woken by the packet line and handed the packet whole; it decides with
//     `team_decide` or sends it up with `team_escalate`, never sideways; and the
//     store turns the decision into a directive to every party in its own team's
//     log ([teams.IsRuling]), which wakes it.
//   - ORDERS GO ONE LEVEL DOWN. A manager's directive and stop reach its own
//     team's members, a sub-team's manager among them, and never a sub-team's
//     members: a handle that names one is refused with the sentence that points
//     to that sub-team's manager ([subTeamPointer]).
//   - THE GLOBAL MANAGER is the manager of the root team (teams' root.go). The
//     store seats every top-level manager as a member of the root, so its
//     verbs, its delivery and its members' homes are the ordinary ones; its view
//     of its team is only those managers ([managedView]), never their chats.
//   - A SUB-TEAM WITH NO MANAGER OF ITS OWN answers to the nearest manager above
//     it for questions and for `team_post` to the manager ([bossOf]); orders do
//     not reach it, because its members are not that manager's members.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// bossOf is the managed team a member of team with key answers to: team
// itself when it has a manager, and otherwise the nearest open ancestor whose
// manager is not key. "" is none.
func bossOf(file *teams.File, team teams.Team, key string) (string, string) {
	if team.Manager != "" {
		return team.ID, team.Name
	}
	for _, up := range file.Ancestors(team.ID) {
		if up.Closed() || up.Manager == "" || up.Manager == key {
			continue
		}
		return up.ID, up.Name
	}
	return "", ""
}

// managedView is team as its manager runs it. For the root team it is its
// manager and the top-level managers ([teams.File.TopManagers]) and nobody
// else, so the global manager's digest, roster and verbs see only the managers
// it directs; every other team is itself.
func managedView(file *teams.File, team teams.Team) teams.Team {
	if file == nil || !team.Root {
		return team
	}
	view := team.Clone()
	view.Members = nil
	if m, ok := team.Member(team.Manager); ok {
		view.Members = append(view.Members, m)
	}
	view.Members = append(view.Members, file.TopManagers()...)
	return view
}

// subTeamPointer is the refusal for a handle that is not one of team's own
// members but names a member of a team under it, "" when it names nobody
// there. Orders go one level down: it names the sub-team's manager to send to
// instead, or says that sub-team has none.
func subTeamPointer(file *teams.File, team teams.Team, handle, what string) string {
	if file == nil {
		return ""
	}
	handle = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(handle)), "@")
	for _, below := range file.Descendants(team.ID) {
		if below.Closed() {
			continue
		}
		member, ok := below.ByHandle(handle)
		if !ok {
			continue
		}
		// The team one level under team on the way down to below is the one
		// whose manager takes team's orders.
		child := below
		for _, up := range file.Ancestors(below.ID) {
			if up.ID == team.ID {
				break
			}
			child = up
		}
		if member.Key == below.Manager && below.ID == child.ID {
			return fmt.Sprintf("@%s manages %q, a team under yours, but is not a member of %q, so it is not yours to send %s. "+
				"Nothing was sent. Ask the person to add it to %q; until then send it nothing but raise what matters with team_raise.", handle, below.Name, team.Name, what, team.Name)
		}
		for _, level := range append([]teams.Team{below}, file.Ancestors(below.ID)...) {
			if level.ID == team.ID {
				break
			}
			if level.Manager == "" || level.Manager == member.Key {
				continue
			}
			boss := "its manager"
			if m, ok := team.Member(level.Manager); ok && m.Handle != "" {
				boss = "@" + m.Handle
			} else if m, ok := level.Member(level.Manager); ok && m.Handle != "" {
				boss = "@" + m.Handle
			}
			return fmt.Sprintf("@%s is in %q, a team under yours, and orders go one level down: it takes them from the manager of %q, not from you. "+
				"Nothing was sent. Send %s to %s, which passes on what it should.", handle, below.Name, level.Name, what, boss)
		}
		return fmt.Sprintf("@%s is in %q, a team under yours with no manager of its own. It asks you its questions and posts to you, but orders go only to your own members. "+
			"Nothing was sent. Ask the person to give %q a manager, or to move @%s into %q.", handle, below.Name, below.Name, handle, team.Name)
	}
	return ""
}

// ── team_start of kind team ─────────────────────────────────────────────────

// errNest is a sub-team start refused inside the store's write, in the words
// the model is handed.
type errNest string

func (e errNest) Error() string { return string(e) }

// teamStartSubTeam makes the child team under team, moves in the members
// named, and writes the start that opens its manager. It answers what to hand
// the model and whether that is a refusal.
func (a *Agent) teamStartSubTeam(team teams.Team, role teamRole, handle, brief, name string, moving []string) (string, bool) {
	profile := a.config.teamProfile()
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return invalidArgumentsPrefix + "a team needs its name", true
	}
	if reason := a.teamCapHold(profile, []teamRole{role}); reason != "" {
		return "No sub-team starts: " + reason + ".", true
	}
	d := a.teamDefaults(profile)
	a.team.mu.Lock()
	keys := append([]string(nil), a.teamKeysLocked()...)
	a.team.mu.Unlock()
	var (
		childID string
		capUSD  float64
		moved   []string
		pool    string
		movedIn []teams.MoveNotice
	)
	err := teams.Update(profile, func(f *teams.File) error {
		snap := &teams.File{Teams: make([]teams.Team, len(f.Teams))}
		for i, t := range f.Teams {
			snap.Teams[i] = t.Clone()
		}
		parent, ok := f.Team(team.ID)
		if !ok || !teamHoldsKey(keys, parent.Manager) {
			return errNest("This conversation is not the manager of " + strconv.Quote(team.Name) + " now. Nothing was done.")
		}
		if parent.Closed() {
			return errNest(fmt.Sprintf("%q is closed, and a closed team starts nothing. Nothing was done.", parent.Name))
		}
		if !f.CanNest(parent.ID, d) {
			e := f.Effective(parent.ID, d)
			from := e.DepthFrom.Words()
			if from == "" {
				from = "its own setting"
			}
			return errNest(fmt.Sprintf("A team under %q would be level %d, past its depth limit of %d (%s). Nothing was done: hand the work to a member instead, or ask the person to raise the limit.",
				parent.Name, f.Depth(parent.ID)+1, e.DepthLimit, from))
		}
		if _, taken := parent.ByHandle(handle); taken {
			return errNest(fmt.Sprintf("@%s is already a member of %q. Pick another handle for the new team's manager.", handle, parent.Name))
		}
		for _, other := range f.Teams {
			if !other.Closed() && strings.EqualFold(other.Name, name) {
				return errNest(fmt.Sprintf("There is already a team called %q. Pick another name.", other.Name))
			}
		}
		var members []teams.Member
		for _, h := range moving {
			m, ok := teamMemberByHandle(parent, h)
			if !ok || m.Key == parent.Manager {
				return errNest(fmt.Sprintf("No member of %q has the handle %q to move in. Its members are: %s. Nothing was done.", parent.Name, h, teamHandles(parent, parent.Manager)))
			}
			if where := reportsElsewhere(f, parent, m); where != "" {
				return errNest(fmt.Sprintf("@%s reports to the manager of %q, not to you, so it is not yours to move. Nothing was done.", m.Handle, where))
			}
			members = append(members, m)
		}
		child := teams.Team{ID: teams.NewID(), Name: name, Parent: parent.ID, Made: time.Now()}
		f.Teams = append(f.Teams, child)
		if share := f.SubTeamCap(parent.ID, d); share > 0 {
			if err := f.SetSettings(child.ID, func(s *teams.Settings) { s.CapUSDDay = &share }); err != nil {
				return err
			}
			capUSD = share
		} else if owner, cap := capPool(f, parent.ID, d); cap > 0 {
			pool = owner.Name
		}
		for _, m := range members {
			m.Home, m.Started = false, false
			if err := f.AddMember(child.ID, m); err != nil {
				return err
			}
			if err := f.RemoveMember(parent.ID, m.Key); err != nil {
				return err
			}
			moved = append(moved, "@"+m.Handle)
		}
		childID = child.ID
		movedIn = teams.MemberMoveNotices(snap, f)
		return nil
	})
	var refused errNest
	if errors.As(err, &refused) {
		return string(refused), true
	}
	if err != nil {
		return "The team could not be made: " + err.Error(), true
	}
	// The members that left this team and joined the new one are told once,
	// here, where the membership was written. A refusal returned above and
	// wrote nothing.
	_ = teams.WriteMoveNotices(profile, movedIn)
	start := teams.Entry{Kind: teams.KindStart, From: teams.FromManager, To: handle, Text: brief, Team: childID}
	if err := teams.AppendTraffic(profile, team.ID, start); err != nil {
		return "The team " + strconv.Quote(name) + " was made, but its manager's start could not be written to the traffic: " + err.Error(), true
	}
	made := fmt.Sprintf("made by the manager of %q; its manager @%s starts on the brief: %s", team.Name, handle, cutRunesTeam(firstLineTeam(brief), 200))
	if len(moved) > 0 {
		made += ". Moved in from " + strconv.Quote(team.Name) + ": " + strings.Join(moved, ", ")
	}
	a.teamSay(profile, childID, teams.Entry{Kind: teams.KindNote, From: teams.FromSystem, To: teams.ToRoom, Text: made})
	money := "It has no cap of its own and none above it."
	switch {
	case capUSD > 0:
		money = fmt.Sprintf("Its cap is $%.2f a day, its share of your team's pool.", capUSD)
	case pool != "":
		money = fmt.Sprintf("It has no cap of its own and spends from %q's pool.", pool)
	}
	said := fmt.Sprintf("Made the team %q under %q and asked for its manager @%s. The conversations view opens @%s in %q's folder as a member of %q; it is handed your brief marked as yours, "+
		"makes itself the manager of %q, and reports to you. %s", name, team.Name, handle, handle, team.Name, team.Name, name, money)
	if len(moved) > 0 {
		said += " Moved in: " + strings.Join(moved, ", ") + "; they are its manager's to direct now, not yours."
	}
	return said, false
}

// subTeamClaim is a start this conversation was opened by that names the
// team it is to manage.
type subTeamClaim struct {
	team, parent, key string
}

// subTeamBriefLine is a sub-team start's brief as its manager is handed it:
// the brief, under the words that say what it is to run.
func subTeamBriefLine(file *teams.File, entry teams.Entry, brief string) string {
	name, parent := "a new team", "your manager's team"
	if file != nil {
		if t, ok := file.Team(entry.Team); ok {
			name = strconv.Quote(t.Name)
			if p, ok := file.Team(t.Parent); ok {
				parent = strconv.Quote(p.Name)
			}
		}
	}
	return fmt.Sprintf("◆ you were started to manage the team %s, under %s: you run it and report to the manager who started you.\n", name, parent) + brief
}

// claimSubTeams makes this conversation the manager of each team a start it
// was opened by names, once its brief has been read. It is a member of the
// parent (the interface added it there under its handle), and it is added to
// the child with the same record. A team that has another manager by now, or
// is gone or closed, is left alone, and the parent's Traffic says why.
func (a *Agent) claimSubTeams(profile string, claims []subTeamClaim) {
	for _, claim := range claims {
		var why string
		err := teams.Update(profile, func(f *teams.File) error {
			child, ok := f.Team(claim.team)
			if !ok || child.Closed() {
				why = "the team is gone or closed"
				return nil
			}
			if child.Manager != "" && child.Manager != claim.key {
				why = "it has another manager already"
				return nil
			}
			parent, _ := f.Team(claim.parent)
			m, ok := parent.Member(claim.key)
			if !ok {
				m = teams.Member{Key: claim.key}
			}
			m.Home, m.Started = false, false
			if err := f.AddMember(child.ID, m); err != nil {
				return err
			}
			return f.SetManager(child.ID, claim.key)
		})
		if err != nil {
			why = err.Error()
		}
		if why != "" {
			a.teamSay(profile, claim.parent, teams.Entry{Kind: teams.KindEvent, From: teams.FromSystem, To: teams.ToManager, Member: claim.key,
				State: teams.StateFailed, Text: "could not make the new conversation a team's manager: " + why})
		}
	}
}

// ── team_raise ──────────────────────────────────────────────────────────────

const teamRaiseToolName = "team_raise"

const teamRaiseDescription = "Raise a conflict you cannot settle with the other side yourself: two or more conversations (you and the parties you name) need incompatible things. " +
	"It goes as one decision packet to the lowest manager above all of you who is not one of you, or to the person when there is none, and wakes that manager. " +
	"Write it so the decider needs no transcript: the question, your side, and options each with what happens. The ruling comes back to every party as a directive."

const teamRaiseSchema = `{"type":"object","properties":{` +
	`"question":{"type":"string","description":"What must be decided, in one line."},` +
	`"parties":{"type":"array","items":{"type":"string"},"description":"The other side(s): a handle like @api, or team/@handle for a member of another team. You are a party already."},` +
	`"context":{"type":"string","description":"Your side, in your words."},` +
	`"options":{"type":"array","items":{"type":"object","properties":{"label":{"type":"string"},"consequence":{"type":"string","description":"What happens if it is chosen."}},"required":["label","consequence"],"additionalProperties":false},"description":"At least two."},` +
	`"recommend":{"type":"string","description":"The option you would pick, by number or label. Optional."},` +
	`"reason":{"type":"string","description":"Why, when you recommend one."},` +
	teamArgSchema + `},"required":["question","parties","options"],"additionalProperties":false}`

// raiseTools is the verb every conversation in a managed team has, member or
// manager.
func (a *Agent) raiseTools() []bare.Tool {
	return []bare.Tool{
		{Name: teamRaiseToolName, Description: teamRaiseDescription, Schema: json.RawMessage(teamRaiseSchema), Execute: a.teamRaiseTool},
	}
}

// partyAt is one party found by the words that named it.
type partyAt struct {
	member teams.Member
	team   teams.Team
}

// resolveParty is the conversation words name: `team/@handle` (a team by name
// or id), or a bare handle looked for first in the teams this conversation is
// in and then in every open team. Several conversations answering one bare
// handle is a refusal that lists them.
func resolveParty(file *teams.File, roles []teamRole, words string) (partyAt, string) {
	words = strings.TrimSpace(words)
	teamWord, handle := "", words
	if i := strings.LastIndex(words, "/"); i >= 0 {
		teamWord, handle = strings.TrimSpace(words[:i]), words[i+1:]
	}
	handle = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(handle)), "@")
	if handle == "" {
		return partyAt{}, fmt.Sprintf("%q names no handle.", words)
	}
	var found []partyAt
	look := func(t teams.Team) {
		if t.Closed() {
			return
		}
		if m, ok := t.ByHandle(handle); ok {
			for _, have := range found {
				if have.member.Key == m.Key {
					return
				}
			}
			found = append(found, partyAt{member: m, team: t})
		}
	}
	if teamWord != "" {
		for _, t := range file.Teams {
			if t.ID == teamWord || strings.EqualFold(t.Name, teamWord) {
				look(t)
			}
		}
		if len(found) == 0 {
			return partyAt{}, fmt.Sprintf("No open team called %q has a member @%s.", teamWord, handle)
		}
	} else {
		for _, role := range roles {
			if t, ok := file.Team(role.id); ok {
				look(t)
			}
		}
		if len(found) == 0 {
			for _, t := range file.Teams {
				look(t)
			}
		}
	}
	switch len(found) {
	case 0:
		return partyAt{}, fmt.Sprintf("No team has a member @%s. Name it as team/@handle.", handle)
	case 1:
		return found[0], ""
	}
	var named []string
	for _, p := range found {
		named = append(named, p.team.Name+"/@"+handle)
	}
	return partyAt{}, fmt.Sprintf("@%s is more than one conversation: %s. Say which as team/@handle.", handle, strings.Join(named, ", "))
}

// atOrUnder reports whether team id is top itself or a team under it.
func atOrUnder(file *teams.File, id, top string) bool {
	if id == top {
		return true
	}
	for _, up := range file.Ancestors(id) {
		if up.ID == top {
			return true
		}
	}
	return false
}

// membershipUnder is key's open membership at or under top: one where it is
// not the manager first, the deepest first among those. "" is none.
func membershipUnder(file *teams.File, key, top string) string {
	best, bestManaged, bestDepth := "", true, -1
	for _, t := range file.Teams {
		if t.Closed() || !t.Holds(key) || !atOrUnder(file, t.ID, top) {
			continue
		}
		managed := t.Manager == key
		depth := len(file.Ancestors(t.ID))
		if best == "" || (bestManaged && !managed) || (managed == bestManaged && depth > bestDepth) {
			best, bestManaged, bestDepth = t.ID, managed, depth
		}
	}
	return best
}

func (a *Agent) teamRaiseTool(ctx context.Context, args json.RawMessage) (string, bool, error) {
	var parsed struct {
		Question string   `json:"question"`
		Parties  []string `json:"parties"`
		Context  string   `json:"context"`
		Options  []struct {
			Label       string `json:"label"`
			Consequence string `json:"consequence"`
		} `json:"options"`
		Recommend string `json:"recommend"`
		Reason    string `json:"reason"`
		Team      string `json:"team"`
	}
	if err := decodeToolArguments(args, &parsed); err != nil {
		return invalidArgumentsPrefix + err.Error(), true, nil
	}
	question := strings.TrimSpace(parsed.Question)
	if question == "" {
		return invalidArgumentsPrefix + "question is empty", true, nil
	}
	if len(parsed.Parties) == 0 {
		return invalidArgumentsPrefix + "name the other party or parties", true, nil
	}
	if len(parsed.Options) < 2 {
		return invalidArgumentsPrefix + "a conflict needs at least two options, each with what happens", true, nil
	}
	var options []teams.Option
	for i, o := range parsed.Options {
		label, consequence := strings.TrimSpace(o.Label), strings.TrimSpace(o.Consequence)
		if label == "" || consequence == "" {
			return invalidArgumentsPrefix + "every option needs a label and what happens if it is chosen", true, nil
		}
		options = append(options, teams.Option{ID: strconv.Itoa(i + 1), Label: label, Consequence: consequence})
	}
	var recommendation *teams.Recommendation
	if pick := strings.TrimSpace(parsed.Recommend); pick != "" {
		for _, o := range options {
			if o.ID == pick || strings.EqualFold(o.Label, pick) {
				reason := strings.TrimSpace(parsed.Reason)
				if reason == "" {
					reason = "the raiser's own pick"
				}
				recommendation = &teams.Recommendation{Option: o.ID, Reason: reason}
			}
		}
		if recommendation == nil {
			return invalidArgumentsPrefix + "recommend names none of the options", true, nil
		}
	}
	profile := a.config.teamProfile()
	if profile == "" {
		return "This conversation is not in a team.", true, nil
	}
	file, keys, roles, d := a.teamSnapshot(profile)
	if file == nil {
		return "The teams file could not be read.", true, nil
	}
	var mine []teamRole
	for _, role := range roles {
		if role.managed {
			mine = append(mine, role)
		}
	}
	if want := strings.TrimSpace(parsed.Team); want != "" {
		var chosen []teamRole
		for _, role := range mine {
			if role.id == want || strings.EqualFold(role.name, want) {
				chosen = append(chosen, role)
			}
		}
		mine = chosen
	}
	if len(mine) == 0 {
		return "This conversation is in no team with a manager now, so there is nobody to raise a conflict to. Ask the person.", true, nil
	}
	// The raiser's own membership: a member's before a manager's, because a
	// manager's conflict with another team is its team's, raised from where it
	// is a member.
	role := mine[0]
	for _, r := range mine {
		if !r.manager {
			role = r
			break
		}
	}
	if role.handle == "" {
		return "You have no handle in " + strconv.Quote(role.name) + " yet, so the decider could not tell who raised it. It is given once this conversation has a title.", true, nil
	}
	parties := []teams.Party{{Key: role.key, Handle: role.handle, Team: role.id, Context: strings.TrimSpace(parsed.Context)}}
	partyKeys := []string{role.key}
	for _, words := range parsed.Parties {
		at, refusal := resolveParty(file, roles, words)
		if refusal != "" {
			return refusal + " Nothing was raised.", true, nil
		}
		if teamHoldsKey(keys, at.member.Key) {
			return "You named yourself; you are a party already. Name the other side. Nothing was raised.", true, nil
		}
		if teamHoldsKey(partyKeys, at.member.Key) {
			continue
		}
		partyKeys = append(partyKeys, at.member.Key)
		parties = append(parties, teams.Party{Key: at.member.Key, Handle: at.member.Handle, Team: at.team.ID})
	}
	decider, deciderName := teams.Person, "the person"
	origin := role.id
	var lca teams.Team
	if at, ok := file.LCA(partyKeys...); ok {
		lca = at
		decider = at.ID
		deciderName = fmt.Sprintf("the manager of %q", at.Name)
		if m, ok := at.Member(at.Manager); ok && m.Handle != "" {
			deciderName += " (@" + m.Handle + ")"
		}
		// Every party's line of the packet runs through a membership under the
		// decider, so the origin is below it and each ruling lands in a team
		// the party reads.
		if !atOrUnder(file, origin, at.ID) {
			origin = membershipUnder(file, role.key, at.ID)
		}
		parties[0].Team = origin
		for i := 1; i < len(parties); i++ {
			if !atOrUnder(file, parties[i].Team, at.ID) {
				if under := membershipUnder(file, parties[i].Key, at.ID); under != "" {
					parties[i].Team = under
				}
			}
		}
	}
	if origin == "" {
		origin = role.id
	}
	raised, err := teams.Raise(profile, teams.Packet{
		Team: decider, Origin: origin, Kind: teams.PacketConflict, RaisedBy: role.handle,
		Parties: parties, Question: question, Options: options, Recommendation: recommendation,
	})
	if err != nil {
		return "The conflict could not be raised: " + teamErrorWords(err), true, nil
	}
	if lca.ID != "" {
		if m, ok := lca.Member(lca.Manager); ok {
			a.teamRouse(profile, lca, file.Effective(lca.ID, d).Wake, []teams.Member{m}, "")
		}
	}
	var others []string
	for _, p := range parties[1:] {
		who := "@" + p.Handle
		if t, ok := file.Team(p.Team); ok && p.Team != role.id {
			who += " (" + t.Name + ")"
		}
		others = append(others, who)
	}
	return fmt.Sprintf("Raised conflict %s with %s for %s to decide. The ruling comes to every party, you included, as a directive and starts your turn if you are idle; "+
		"carry on with what does not depend on it, or end your turn and wait.", raised.ID, strings.Join(others, ", "), deciderName), false, nil
}

// ── delivery ────────────────────────────────────────────────────────────────

// rulingFor reports whether a ruling entry ([teams.IsRuling]) is addressed to
// this conversation's membership: by its key, or by its handle when it
// carries none.
func rulingFor(role teamRole, entry teams.Entry) bool {
	if !teams.IsRuling(entry) {
		return false
	}
	if entry.Member != "" {
		return entry.Member == role.key
	}
	return role.handle != "" && entry.To == role.handle
}

// rulingLine is a ruling as its party is told it. It is binding whoever wrote
// it: a conflict's decider outranks the parties' own managers on the question
// it decided, and only the person outranks the decider.
func rulingLine(entry teams.Entry) string {
	return "◆ " + indentAfterFirst(cutRunesTeam(strings.TrimSpace(entry.Text), teamEntryText)) +
		" Follow it unless the person said otherwise in this conversation."
}

// partyTold is a conflict newly raised, as a party that did not raise it is
// told it (without a wake): who raised it and who decides it. "" for anyone
// else.
func partyTold(role teamRole, entry teams.Entry, p teams.Packet) string {
	if p.Kind != teams.PacketConflict || entry.State != teams.PacketOpen || p.State != teams.PacketOpen {
		return ""
	}
	for i, party := range p.Parties {
		if i == 0 || party.Key != role.key || party.Team != role.id {
			continue
		}
		return fmt.Sprintf("◆ @%s raised a conflict naming you (%s), for %s to decide: %s. Its ruling comes to you as a directive.",
			strings.TrimPrefix(p.RaisedBy, "@"), p.ID, packetDecider(p), oneLineTeam(p.Question))
	}
	return ""
}

// readableBelow is the member of a team under team that words names
// (team/@handle, or a handle only one conversation under team has), for a
// read. false is nobody there.
func readableBelow(profile string, team teams.Team, words string) (teams.Member, bool) {
	file, err := teams.Load(profile)
	if err != nil {
		return teams.Member{}, false
	}
	var below []teamRole
	for _, t := range file.Descendants(team.ID) {
		if !t.Closed() {
			below = append(below, teamRole{id: t.ID, name: t.Name})
		}
	}
	if len(below) == 0 {
		return teams.Member{}, false
	}
	// Only the teams under team are searched: a bare handle looks there, and
	// a named team must be one of them.
	scoped := &teams.File{Version: file.Version}
	for _, role := range below {
		if t, ok := file.Team(role.id); ok {
			scoped.Teams = append(scoped.Teams, t)
		}
	}
	at, refusal := resolveParty(scoped, below, words)
	if refusal != "" {
		return teams.Member{}, false
	}
	return at.member, true
}

// teamPostUp is a post to the manager from a member of a sub-team with no
// manager of its own: written to the log of the team whose manager it answers
// to, marked with the sub-team it came from, and that manager roused.
func (a *Agent) teamPostUp(team teams.Team, role teamRole, entry teams.Entry) (string, bool, error) {
	profile := a.config.teamProfile()
	entry.Member = role.key
	entry.Text = fmt.Sprintf("(from %q, a team under yours with no manager of its own) %s", team.Name, entry.Text)
	if err := teams.AppendTraffic(profile, role.boss, entry); err != nil {
		return "The post could not be written to the traffic of " + strconv.Quote(role.bossName) + ": " + err.Error(), true, nil
	}
	file, _, _, _ := a.teamSnapshot(profile)
	if file != nil {
		if boss, ok := file.Team(role.boss); ok {
			if manager, ok := boss.Member(boss.Manager); ok {
				a.teamRouse(profile, boss, file.Effective(boss.ID, a.teamDefaults(profile)).Wake, []teams.Member{manager}, "")
			}
		}
	}
	return fmt.Sprintf("Posted to the manager of %q, which %q answers to while it has no manager of its own. It arrives at the start of its next step.", role.bossName, team.Name), false, nil
}
