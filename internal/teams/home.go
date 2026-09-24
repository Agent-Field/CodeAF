package teams

import (
	"errors"
	"fmt"
)

// ── WHO A CONVERSATION REPORTS TO ───────────────────────────────────────────
//
// Every conversation that sits in a team with a manager somewhere above it
// reports to exactly one manager, its HOME (ruling c-5). Its routine direction
// and its clarifying questions go there; every other manager it can be reached
// by is a LINK, who may read it and send it an fyi and nothing else.
//
// THE HOME IS STORED, NOT DERIVED ON EVERY READ, because the ruling says it
// never changes by itself: a conversation that later joins a nearer team keeps
// reporting where it did until the person moves it ([File.SetHome]). It is a
// flag on one of the conversation's memberships ([Member.Home]), and the home
// manager is the nearest manager up THAT membership's chain, the membership's
// own team first. So the flag may sit on a membership of an unmanaged team
// whose parent has a manager, which is how a conversation in an unmanaged
// sub-team reports to the manager above it without being a member there.
//
// THE FLAG IS STABLE; THE MANAGER AT THE END OF ITS CHAIN IS WHOEVER THE
// PERSON MADE MANAGER THERE. A conversation in an unmanaged sub-team reports
// to the manager above it; when the person gives that sub-team a manager, the
// same flag now resolves to the new manager, one step nearer. That is not the
// home changing by itself: making a manager is the person's act, and a
// sub-team manager nobody under it reported to would be a manager of nothing.
//
// A MANAGER IS NEVER ITS OWN HOME. Walking up from a team a conversation
// manages skips that team, so a sub-team's manager reports to the manager of
// the team above, which is the ruling's "reports one level up".
//
// tidy keeps the flag honest on every load and write: a conversation with no
// valid flag and something to report to is given one by the rules below, a
// flag with nothing above it any more (the manager was cleared, the team was
// closed or the membership removed) is taken away and picked again, and a
// second flag is dropped. The pick, in order:
//
//  1. the membership with the NEAREST manager: its own team managed before a
//     manager one or more levels up, and among equals the deeper, more
//     specific team first;
//  2. then the membership the manager's team_start made ([Member.Started]);
//  3. then the first in the file's order. The file keeps teams in the order
//     they were made and a team's members in the order they joined, and the
//     pick is made at the first write that gives a conversation somewhere to
//     report, so for a conversation that joins its managed teams one at a
//     time this is the team it joined first.
//
// A CLOSED TEAM IS NOT SOMEWHERE TO REPORT (lifecycle.go). A membership of a
// closed team is never a home, a closed team's manager is never found by a
// walk, and a conversation whose home closed is given the next one by the same
// rules, or none, and is then an ordinary chat.

// Report is one manager a conversation can be reached by.
type Report struct {
	// Via is the team of the membership the line runs through.
	Via string `json:"via"`
	// Team is the managed team whose manager this is: Via itself or an
	// ancestor of it.
	Team string `json:"team"`
	// Manager is that manager's conversation key.
	Manager string `json:"manager"`
	// Distance is how many levels up from Via the manager's team is.
	Distance int `json:"distance"`
}

// ErrNoManagerAbove is [File.SetHome] asked for a membership with no manager
// anywhere up its chain.
var ErrNoManagerAbove = errors.New("teams: no manager above that team to report to")

// Home is the manager the conversation with key reports to, false when it has
// none (it is an ordinary chat).
func (f *File) Home(key string) (Report, bool) {
	for _, t := range f.Teams {
		m, ok := t.Member(key)
		if !ok || !m.Home || t.Closed() {
			continue
		}
		if r, ok := managerUp(f.Teams, t.ID, key); ok {
			return r, true
		}
	}
	return Report{}, false
}

// Links is every other manager the conversation with key can be reached by:
// the nearest manager up each of its memberships, in the pick order, each
// managed team once, the home left out.
func (f *File) Links(key string) []Report {
	home, _ := f.Home(key)
	seen := map[string]bool{home.Team: true}
	var out []Report
	for _, r := range candidates(f.Teams, key) {
		if seen[r.Team] {
			continue
		}
		seen[r.Team] = true
		out = append(out, r)
	}
	return out
}

// SetHome makes the membership of key in team id its home. The membership
// must exist and have a manager up its chain, else [ErrNoManagerAbove].
func (f *File) SetHome(key, id string) error {
	t, ok := f.Team(id)
	if !ok {
		return fmt.Errorf("no team %s", id)
	}
	if !t.Holds(key) {
		return fmt.Errorf("%s is not in team %s", key, t.Name)
	}
	if _, ok := managerUp(f.Teams, id, key); !ok {
		return ErrNoManagerAbove
	}
	setHomeFlag(f.Teams, key, id)
	return nil
}

// setHomeFlag puts key's one home flag on its membership of team id.
func setHomeFlag(teams []Team, key, id string) {
	for i := range teams {
		for j := range teams[i].Members {
			if teams[i].Members[j].Key == key {
				teams[i].Members[j].Home = teams[i].ID == id
			}
		}
	}
}

// managerUp is the nearest manager up from team id, id itself first, that is
// not key: the team must be open and have a manager. The walk is bounded by
// the list, so a loop cannot hang it.
func managerUp(teams []Team, id, key string) (Report, bool) {
	at := id
	for distance := 0; at != "" && distance <= len(teams); distance++ {
		i := Index(teams, at)
		if i < 0 {
			return Report{}, false
		}
		t := teams[i]
		if !t.Closed() && t.Manager != "" && t.Manager != key {
			return Report{Via: id, Team: t.ID, Manager: t.Manager, Distance: distance}, true
		}
		at = t.Parent
	}
	return Report{}, false
}

// candidate is one membership that could be a home, with what the pick order
// reads.
type candidate struct {
	r       Report
	depth   int
	started bool
	order   int
}

// before is the pick order between two candidates (see the file header).
func (a candidate) before(b candidate) bool {
	if a.r.Distance != b.r.Distance {
		return a.r.Distance < b.r.Distance
	}
	if a.depth != b.depth {
		return a.depth > b.depth
	}
	if a.started != b.started {
		return a.started
	}
	return a.order < b.order
}

// candidates is every open membership of key with a manager up its chain, as
// a Report, in the pick order.
func candidates(teams []Team, key string) []Report {
	var all []candidate
	for i, t := range teams {
		if t.Closed() {
			continue
		}
		m, ok := t.Member(key)
		if !ok {
			continue
		}
		r, ok := managerUp(teams, t.ID, key)
		if !ok {
			continue
		}
		all = append(all, candidate{r: r, depth: depthIn(teams, r.Team), started: m.Started, order: i})
	}
	// An insertion sort: a conversation is in a handful of teams.
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && all[j].before(all[j-1]); j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	out := make([]Report, len(all))
	for i, c := range all {
		out[i] = c.r
	}
	return out
}

// depthIn is how many levels team id stands at, 1 at the top.
func depthIn(teams []Team, id string) int {
	depth := 0
	for at := id; at != "" && depth <= len(teams); depth++ {
		i := Index(teams, at)
		if i < 0 {
			break
		}
		at = teams[i].Parent
	}
	return depth
}

// assignHomes keeps one valid home flag for every conversation that has a
// manager to report to and none for any other, and reports whether it moved
// a flag. A valid flag is never moved.
func assignHomes(teams []Team) bool {
	changed := false
	done := map[string]bool{}
	for _, t := range teams {
		for _, m := range t.Members {
			if done[m.Key] {
				continue
			}
			done[m.Key] = true
			if homeHolds(teams, m.Key) {
				continue
			}
			want := ""
			if c := candidates(teams, m.Key); len(c) > 0 {
				want = c[0].Via
			}
			if clearOrSet(teams, m.Key, want) {
				changed = true
			}
		}
	}
	return changed
}

// homeHolds reports whether key carries exactly one home flag and it is on an
// open membership with a manager up its chain.
func homeHolds(teams []Team, key string) bool {
	flags, valid := 0, false
	for _, t := range teams {
		m, ok := t.Member(key)
		if !ok || !m.Home {
			continue
		}
		flags++
		if !t.Closed() {
			_, valid = managerUp(teams, t.ID, key)
		}
	}
	if flags == 0 {
		// No flag is right exactly when there is nothing to report to.
		return len(candidates(teams, key)) == 0
	}
	return flags == 1 && valid
}

// clearOrSet leaves key's one flag on its membership of want ("" for none) and
// reports whether anything moved. A key with several flags of which one is
// valid keeps that one.
func clearOrSet(teams []Team, key, want string) bool {
	for _, t := range teams {
		if m, ok := t.Member(key); ok && m.Home && !t.Closed() {
			if _, valid := managerUp(teams, t.ID, key); valid {
				want = t.ID
				break
			}
		}
	}
	changed := false
	for i := range teams {
		for j := range teams[i].Members {
			m := &teams[i].Members[j]
			if m.Key != key {
				continue
			}
			on := want != "" && teams[i].ID == want
			if m.Home != on {
				m.Home, changed = on, true
			}
		}
	}
	return changed
}

// ── WHO DECIDES BETWEEN SEVERAL ─────────────────────────────────────────────

// LCA is the team whose manager decides between the conversations keys
// (ruling c-6): the lowest team that is at or above some membership of every
// one of them, is open, and has a manager who is not one of them. The lowest
// is the deepest; among equally deep teams the first in the file. false is
// nobody, and then the person decides. One key is that conversation's nearest
// manager, which for a conversation in one team is its home.
//
// A MANAGER WHO IS A PARTY DOES NOT JUDGE ITS OWN CASE: a team whose manager
// is one of keys is passed over for the next team up that holds them all.
func (f *File) LCA(keys ...string) (Team, bool) {
	if len(keys) == 0 {
		return Team{}, false
	}
	party := map[string]bool{}
	for _, k := range keys {
		party[k] = true
	}
	// above[k] is every team at or above one of k's open memberships.
	var common map[string]bool
	for _, k := range keys {
		above := map[string]bool{}
		for _, t := range f.Teams {
			if t.Closed() || !t.Holds(k) {
				continue
			}
			above[t.ID] = true
			for _, a := range f.Ancestors(t.ID) {
				above[a.ID] = true
			}
		}
		if common == nil {
			common = above
			continue
		}
		for id := range common {
			if !above[id] {
				delete(common, id)
			}
		}
	}
	best, bestDepth := -1, 0
	for i, t := range f.Teams {
		if !common[t.ID] || t.Closed() || t.Manager == "" || party[t.Manager] {
			continue
		}
		if d := depthIn(f.Teams, t.ID); best < 0 || d > bestDepth {
			best, bestDepth = i, d
		}
	}
	if best < 0 {
		return Team{}, false
	}
	return f.Teams[best], true
}
