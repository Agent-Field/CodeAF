package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// The teams-union scope of deliverables.*: every conversation held by an open
// team or any team beneath one, and no other. It is read from teams.json alone
// (no session is started, no transcript read, no deliverable copied), and the
// hierarchy it walks is the one the All-teams overview draws: the real root's
// children when a root exists, parentless teams when none does. A real root's
// own roster (the global manager, and the top managers seated beside him) is a
// separate group, never a top and never an invented one in a rootless file.
// The public scope and cursor contract is in docs/design/app-v3/PROTOCOL.md.

const (
	// appTeamsOwnersMax and appTeamsPairsMax bound the union a request holds.
	// Past either the union is refused; a cut roster would be a wrong one.
	appTeamsOwnersMax = 4096
	appTeamsPairsMax  = 16384
	// appTeamsPollEvery is how many steps of reading the stored list pass
	// between looks at the request's context. Every loop over the list is
	// linear, so a cancelled request stops within this many steps wherever it
	// is, never after the whole list has been read.
	appTeamsPollEvery = 1024
)

// appTeamsUnion is one request's reading of the membership.
type appTeamsUnion struct {
	// Revision names exactly this membership: change who is in which team, a
	// team's parent, whether it is open, or whether the file has a real root
	// (its own roster is a group), and it changes; rename or recolour a team
	// and it does not.
	Revision string
	// Tops are the canonical top-level team ids.
	Tops map[string]bool
	// Root is the real root's id, "" when the file has none.
	Root string
	// Owners maps an owning conversation to every team holding it.
	Owners map[string][]appwire.DeliverableTeam
}

func appTeamsInconsistent(format string, args ...any) error {
	return appwire.Errorf(appwire.CodeEngine, "teams.json is inconsistent: "+format, args...)
}

func appTeamsBusy() error {
	return appwire.Errorf(appwire.CodeBusy, "the teams could not be read in time; try again")
}

// teamsUnion reads and validates the membership, within ctx. The byte limit is
// the shared reader's own, so the two cannot disagree about how large a teams
// file may be.
func (b *appBackend) teamsUnion(ctx context.Context) (*appTeamsUnion, error) {
	file, _, err := teamstore.ReadSnapshot(ctx, b.crewProfile(), teamstore.SnapshotMaxBytes)
	switch {
	case errors.Is(err, teamstore.ErrSnapshotTooLarge):
		return nil, appwire.Errorf(appwire.CodeTooLarge, "the teams file is too large to read as one union")
	case ctx.Err() != nil:
		return nil, appTeamsBusy()
	case err != nil:
		return nil, appEngineError(err)
	}
	return buildTeamsUnion(ctx, file.Teams)
}

// appTeamsCycles marks every team that sits on a parent cycle, in one walk of
// the list that visits each team once: a walk climbs from a team through its
// parents, marking the teams it is passing, until it reaches a team already
// settled, a team with no parent, or a parent that is not there (refused
// separately, in file order); reaching a team it is passing closes a cycle,
// and every team from there on the walk is on it. Asking teamstore.ParentLoops
// of each team instead climbs the whole chain from every team, through a linear
// lookup per step, which is cubic in a deep chain. tick is the shared look at
// the request's context.
func appTeamsCycles(teams []teamstore.Team, byID map[string]int, tick func() error) ([]bool, error) {
	const settled = -1
	// mark is settled, 0 for a team not yet reached, or one more than its
	// position on the walk now passing it.
	mark := make([]int, len(teams))
	onCycle := make([]bool, len(teams))
	var walk []int
	for start := range teams {
		walk = walk[:0]
		for at := start; ; {
			if err := tick(); err != nil {
				return nil, err
			}
			if mark[at] == settled {
				break
			}
			if mark[at] > 0 {
				for _, i := range walk[mark[at]-1:] {
					onCycle[i] = true
				}
				break
			}
			walk = append(walk, at)
			mark[at] = len(walk)
			parent := byID[teams[at].Parent]
			if teams[at].Parent == "" || parent == 0 {
				break
			}
			at = parent - 1
		}
		for _, i := range walk {
			mark[i] = settled
		}
	}
	return onCycle, nil
}

// buildTeamsUnion is the union of a stored list; it changes nothing in it.
// Every loop is linear in the list and looks at ctx as it goes.
func buildTeamsUnion(ctx context.Context, teams []teamstore.Team) (*appTeamsUnion, error) {
	steps := 0
	tick := func() error {
		if steps++; steps%appTeamsPollEvery == 0 && ctx.Err() != nil {
			return appTeamsBusy()
		}
		return nil
	}
	byID := make(map[string]int, len(teams))
	rootID := ""
	for i, t := range teams {
		if err := tick(); err != nil {
			return nil, err
		}
		switch {
		case t.ID == "":
			return nil, appTeamsInconsistent("a team has no id")
		case byID[t.ID] != 0:
			return nil, appTeamsInconsistent("team id %s is used twice", t.ID)
		}
		byID[t.ID] = i + 1
		if t.Root {
			if rootID != "" {
				return nil, appTeamsInconsistent("more than one team is the root")
			}
			rootID = t.ID
		}
	}
	onCycle, err := appTeamsCycles(teams, byID, tick)
	if err != nil {
		return nil, err
	}
	children := map[string][]int{}
	for i, t := range teams {
		if err := tick(); err != nil {
			return nil, err
		}
		if t.Parent != "" && byID[t.Parent] == 0 {
			return nil, appTeamsInconsistent("team %s sits under %s, which is not there", t.ID, t.Parent)
		}
		if t.Parent == t.ID || onCycle[i] {
			return nil, appTeamsInconsistent("team %s is under itself", t.ID)
		}
		if t.Root && t.Parent != "" {
			return nil, appTeamsInconsistent("the root team sits under another team")
		}
		if rootID != "" && !t.Root && t.Parent == "" {
			return nil, appTeamsInconsistent("team %s is top-level beside the root", t.ID)
		}
		children[t.Parent] = append(children[t.Parent], i)
	}
	if rootID != "" && teams[byID[rootID]-1].Closed() {
		return nil, appTeamsInconsistent("the root team is closed")
	}
	// The store never keeps an open team under a closed one: closing a team
	// closes everything beneath it, a reopen under a closed parent is refused,
	// and so is a move there. Such a file is inconsistent, and reading it as
	// though the open team were absent would drop its people from the union
	// without a word. Archived implies closed, so it needs no rule of its own.
	// With this, and no cycle, every open team is reached from an open top.
	for _, t := range teams {
		if err := tick(); err != nil {
			return nil, err
		}
		if !t.Closed() && t.Parent != "" && teams[byID[t.Parent]-1].Closed() {
			return nil, appTeamsInconsistent("open team %s sits under closed team %s", t.ID, t.Parent)
		}
	}
	// The canonical top: the real root's children, else the parentless teams.
	topParent := rootID
	out := &appTeamsUnion{Root: rootID, Tops: map[string]bool{}, Owners: map[string][]appwire.DeliverableTeam{}}
	type stop struct {
		at  int
		top string
	}
	var frontier []stop
	for _, i := range children[topParent] {
		if teams[i].Closed() {
			continue
		}
		out.Tops[teams[i].ID] = true
		frontier = append(frontier, stop{i, teams[i].ID})
	}
	// lines are the revision's records. Each field is written as its byte
	// length, a colon and its bytes, so a record reads back one way only: an
	// id may hold a NUL, a newline or bytes that are not UTF-8, and none of
	// them can make two different memberships spell the same list.
	var lines []string
	record := func(kind string, fields ...string) {
		var line strings.Builder
		line.WriteString(kind)
		for _, f := range fields {
			line.WriteString(strconv.Itoa(len(f)))
			line.WriteByte(':')
			line.WriteString(f)
		}
		lines = append(lines, line.String())
	}
	pairs := 0
	// hold records one team's distinct owners against group (a top, or "" for
	// the root), within the union's bounds.
	hold := func(t teamstore.Team, top string, root bool) error {
		held := map[string]bool{}
		manager := t.Manager == ""
		for _, m := range t.Members {
			if err := tick(); err != nil {
				return err
			}
			owner := appMemberAgent(m)
			if (m.Key == "" && m.File == "") || owner == "" || owner == "." || owner == "/" {
				return appTeamsInconsistent("team %s holds a member with no conversation", t.ID)
			}
			if m.Key == t.Manager {
				manager = true
			}
			if held[owner] {
				continue
			}
			held[owner] = true
			if pairs++; pairs > appTeamsPairsMax {
				return appwire.Errorf(appwire.CodeTooLarge, "the teams hold more members than can be listed as one union")
			}
			if _, seen := out.Owners[owner]; !seen && len(out.Owners) >= appTeamsOwnersMax {
				return appwire.Errorf(appwire.CodeTooLarge, "the teams hold more conversations than can be listed as one union")
			}
			out.Owners[owner] = append(out.Owners[owner], appwire.DeliverableTeam{Team: t.ID, Top: top, Root: root})
			record("m", owner, t.ID, top)
		}
		if !manager {
			return appTeamsInconsistent("team %s names a manager who is not one of its members", t.ID)
		}
		return nil
	}
	if rootID != "" {
		record("r", rootID)
		if err := hold(teams[byID[rootID]-1], "", true); err != nil {
			return nil, err
		}
	}
	for len(frontier) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, appTeamsBusy()
		}
		at := frontier[0]
		frontier = frontier[1:]
		t := teams[at.at]
		parent := t.Parent
		if parent == rootID {
			parent = ""
		}
		record("t", t.ID, parent)
		if err := hold(t, at.top, false); err != nil {
			return nil, err
		}
		for _, i := range children[t.ID] {
			if !teams[i].Closed() {
				frontier = append(frontier, stop{i, at.top})
			}
		}
	}
	for _, rels := range out.Owners {
		sort.Slice(rels, func(i, j int) bool {
			if rels[i].Root != rels[j].Root {
				return rels[i].Root
			}
			if rels[i].Top != rels[j].Top {
				return rels[i].Top < rels[j].Top
			}
			return rels[i].Team < rels[j].Team
		})
	}
	sort.Strings(lines)
	sum := sha256.New()
	sum.Write([]byte(strings.Join(lines, "\n")))
	out.Revision = hex.EncodeToString(sum.Sum(nil))
	return out, nil
}

// appTeamsAmbiguous says an owner's memberships reach more than one top-level
// team. The root group is not a top: a top manager seated in the root is
// identified by the team he manages, so root seating never adds a top.
func appTeamsAmbiguous(rels []appwire.DeliverableTeam) bool {
	first := ""
	for _, r := range rels {
		switch {
		case r.Root:
		case first == "":
			first = r.Top
		case r.Top != first:
			return true
		}
	}
	return false
}

// sessions is the owner set of a query: every owner of the union, or of one
// top-level team's subtree, or (the real root's id) of the root's own roster. An empty union is a set that matches nothing, never
// nil (nil is every conversation).
func (u *appTeamsUnion) sessions(top string) (map[string]bool, error) {
	if top != "" && !u.Tops[top] && (u.Root == "" || top != u.Root) {
		return nil, appwire.Errorf(appwire.CodeNotFound, "there is no top-level team %s", top)
	}
	out := map[string]bool{}
	for owner, rels := range u.Owners {
		for _, r := range rels {
			if top == "" || r.Top == top || (r.Root && r.Team == top) {
				out[owner] = true
				break
			}
		}
	}
	if len(out) == 0 {
		out["-"] = true
	}
	return out, nil
}

// decorate attaches an owner's recorded memberships to its row.
func (u *appTeamsUnion) decorate(d *appwire.Deliverable) {
	if rels := u.Owners[d.Agent]; len(rels) > 0 {
		d.Teams = append([]appwire.DeliverableTeam(nil), rels...)
		d.Ambiguous = appTeamsAmbiguous(rels)
	}
}

// ── cursor ──────────────────────────────────────────────────────────────────

// appTeamsCursor binds a page to the membership and the team filter it began
// with, around the index cursor, which binds the index's identity and snapshot.
type appTeamsCursor struct {
	Revision string `json:"r"`
	Top      string `json:"t,omitempty"`
	Inner    string `json:"i"`
}

const appTeamsCursorPrefix = "t1."

func (u *appTeamsUnion) wrap(top, inner string) string {
	if inner == "" {
		return ""
	}
	raw, _ := json.Marshal(appTeamsCursor{Revision: u.Revision, Top: top, Inner: inner})
	return appTeamsCursorPrefix + base64.RawURLEncoding.EncodeToString(raw)
}

// unwrap is the index cursor inside one this union issued; a cursor of another
// membership, another team filter or another kind is the index's stale cursor.
func (u *appTeamsUnion) unwrap(top, cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	text, ok := strings.CutPrefix(cursor, appTeamsCursorPrefix)
	if !ok {
		return "", session.ErrMediaCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(text)
	var held appTeamsCursor
	if err != nil || json.Unmarshal(raw, &held) != nil || held.Inner == "" ||
		held.Revision != u.Revision || held.Top != top {
		return "", session.ErrMediaCursor
	}
	return held.Inner, nil
}

// ── the two methods ─────────────────────────────────────────────────────────

// teamsQuery resolves a teams-scope request against the membership read within
// its one deadline, and the index cursor inside the request's own.
func (b *appBackend) teamsQuery(ctx context.Context, q appwire.DeliverablesQuery, count bool) (*appTeamsUnion, session.DeliverableQuery, func(appwire.Deliverable) bool, string, error) {
	union, err := b.teamsUnion(ctx)
	if err != nil {
		return nil, session.DeliverableQuery{}, nil, "", err
	}
	top := strings.TrimSpace(q.Crew)
	owners, err := union.sessions(top)
	if err != nil {
		return nil, session.DeliverableQuery{}, nil, "", err
	}
	cursor, err := union.unwrap(top, q.Cursor)
	if err != nil {
		return nil, session.DeliverableQuery{}, nil, "", appDeliverError(err)
	}
	query := session.DeliverableQuery{Sessions: owners, Kinds: appDeliverSet(q.Kind), States: appDeliverSet(q.State), Text: q.Query, CountOnly: count}
	lives := appDeliverSet(q.Live)
	return union, query, func(d appwire.Deliverable) bool { return lives == nil || lives[d.Live] }, cursor, nil
}

// listTeamsUnion is deliverables.list over the teams scope. One deadline covers
// the membership read, the index pass and every row's source decision.
func (b *appBackend) listTeamsUnion(q appwire.DeliverablesQuery) (appwire.DeliverablesPage, error) {
	deadline := time.Now().Add(appDeliverListTime)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	union, query, liveOK, cursor, err := b.teamsQuery(ctx, q, false)
	if err != nil {
		return appwire.DeliverablesPage{}, err
	}
	page, err := b.deliverPage(ctx, deadline, query, liveOK, cursor, q.Limit, union.decorate)
	if err != nil {
		return appwire.DeliverablesPage{}, err
	}
	page.Next = union.wrap(strings.TrimSpace(q.Crew), page.Next)
	page.Revision = union.Revision
	return page, nil
}

// countTeamsUnion is deliverables.count over the teams scope.
func (b *appBackend) countTeamsUnion(q appwire.DeliverablesQuery) (appwire.DeliverablesCount, error) {
	deadline := time.Now().Add(appDeliverListTime)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	union, query, liveOK, cursor, err := b.teamsQuery(ctx, q, true)
	if err != nil {
		return appwire.DeliverablesCount{}, err
	}
	count, err := b.deliverCount(ctx, deadline, query, liveOK, cursor)
	if err != nil {
		return appwire.DeliverablesCount{}, err
	}
	count.Next = union.wrap(strings.TrimSpace(q.Crew), count.Next)
	count.Revision = union.Revision
	return count, nil
}
