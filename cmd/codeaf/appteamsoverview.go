package main

// appteamsoverview.go — crews.overview: one bounded page of the teams under
// one parent, or of one team's direct members, from the teams file alone.
//
// IT STARTS NOTHING AND WRITES NOTHING. It never calls b.crews() (the watch),
// never loads a conversation, reads no ledger, transcript or traffic, and reads
// the file through [teamstore.ReadSnapshot], which is the one reader that does
// not repair. A file that breaks the tree's rules is an error that names the
// first broken fact; it is never flattened into a list that looks fine.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

var _ appwire.TeamsOverviewer = (*appBackend)(nil)

// CrewsOverview answers crews.overview.
func (b *appBackend) CrewsOverview(ctx context.Context, ask appwire.OverviewAsk) (*appwire.Overview, error) {
	kind, limit, err := appwire.CheckOverviewAsk(ask)
	if err != nil {
		return nil, err
	}
	file, rev, err := teamstore.ReadSnapshot(ctx, b.crewProfile(), appwire.OverviewMaxBytes)
	if err != nil {
		return nil, overviewReadError(err)
	}
	idx, err := newOverviewIndex(file.Teams)
	if err != nil {
		return nil, err
	}
	out := &appwire.Overview{Revision: string(rev), Kind: kind, Teams: []appwire.TeamSummary{}, Members: []appwire.OverviewMember{}}
	if idx.root >= 0 {
		root := idx.summary(idx.teams[idx.root])
		out.Root = &root
	}
	var page overviewPage
	if kind == appwire.OverviewMembers {
		page, err = idx.memberPage(ask.Parent)
	} else {
		page, err = idx.teamPage(ask.Parent)
	}
	if err != nil {
		return nil, err
	}
	out.Parent, out.Total = page.parent, page.total
	offset, err := overviewOffset(ask.Cursor, overviewCursor{Rev: string(rev), Kind: kind, Parent: page.parent}, page.total)
	if err != nil {
		return nil, err
	}
	end := min(offset+limit, page.total)
	if kind == appwire.OverviewMembers {
		out.Members = append(out.Members, page.members[offset:end]...)
	} else {
		for _, t := range page.teams[offset:end] {
			out.Teams = append(out.Teams, idx.summary(t))
		}
	}
	if end < page.total {
		out.Next = overviewCursor{Rev: string(rev), Kind: kind, Parent: page.parent, Offset: end}.encode()
	}
	return out, ctx.Err()
}

// overviewReadError is a failed snapshot in the wire's words, with no path.
func overviewReadError(err error) error {
	switch {
	case errors.Is(err, teamstore.ErrSnapshotTooLarge):
		return appwire.Errorf(appwire.CodeTooLarge, "the teams file is larger than %d bytes, which this view does not read", appwire.OverviewMaxBytes)
	case errors.Is(err, teamstore.ErrSnapshotVersion):
		return appwire.Errorf(appwire.CodeEngine, "the teams file was written by a newer codeaf than this one reads")
	case errors.Is(err, teamstore.ErrSnapshotUnreadable):
		return appwire.Errorf(appwire.CodeEngine, "the teams file is unreadable")
	}
	return err
}

// ── THE INDEX ───────────────────────────────────────────────────────────────

// overviewIndex is the teams as stored plus what one request needs to answer
// about them. It lives for the request and is not kept.
type overviewIndex struct {
	teams []teamstore.Team
	byID  map[string]int
	root  int
	// open counts each team's direct open, non-root children.
	open map[string]int
}

// badFact is the file breaking a rule the tree stands on.
func badFact(format string, args ...any) error {
	for i, a := range args {
		if s, ok := a.(string); ok && len(s) > 64 {
			args[i] = s[:64] + "…"
		}
	}
	return appwire.Errorf(appwire.CodeEngine, "the teams file is inconsistent: "+format, args...)
}

// newOverviewIndex checks the facts the tree stands on and indexes the teams.
// It repairs none of them.
func newOverviewIndex(teams []teamstore.Team) (*overviewIndex, error) {
	idx := &overviewIndex{teams: teams, byID: make(map[string]int, len(teams)), root: -1, open: map[string]int{}}
	if err := idx.checkIdentities(); err != nil {
		return nil, err
	}
	if err := idx.checkParents(); err != nil {
		return nil, err
	}
	if err := idx.checkLoops(); err != nil {
		return nil, err
	}
	if err := idx.checkManagers(); err != nil {
		return nil, err
	}
	for _, t := range teams {
		if !t.Closed() && !t.Root {
			idx.open[t.Parent]++
		}
	}
	return idx, nil
}

func (x *overviewIndex) checkIdentities() error {
	for i, t := range x.teams {
		if t.ID == "" {
			return badFact("a team has no id")
		}
		if _, dup := x.byID[t.ID]; dup {
			return badFact("team id %q is used twice", t.ID)
		}
		x.byID[t.ID] = i
		if !t.Root {
			continue
		}
		if x.root >= 0 {
			return badFact("more than one team is the root")
		}
		x.root = i
	}
	return nil
}

// checkParents refuses a parent that is not there and an open team under a
// closed one. The store never writes either (closing a team closes the teams
// under it, and its repair reopens a closed root), so a file that holds one
// was not tidied; skipping the open team would answer a tree with a hole in
// it, and a closed root would turn the plain first ask into bad_params.
func (x *overviewIndex) checkParents() error {
	for _, t := range x.teams {
		switch {
		case t.Root && t.Parent != "":
			return badFact("the root %q has a parent", t.ID)
		case t.Root && t.Closed():
			return badFact("the root %q is closed", t.ID)
		case t.Root:
		case t.Parent == "" && x.root >= 0:
			return badFact("team %q is at the top beside the root", t.ID)
		case t.Parent == t.ID:
			return badFact("team %q is its own parent", t.ID)
		case t.Parent != "":
			i, ok := x.byID[t.Parent]
			if !ok {
				return badFact("team %q has parent %q, which is not a team", t.ID, t.Parent)
			}
			if !t.Closed() && x.teams[i].Closed() {
				return badFact("open team %q is under closed team %q", t.ID, t.Parent)
			}
		}
	}
	return nil
}

// checkLoops finds a parent cycle in one pass over the teams: a walk up from
// each team stops at one already settled, and meeting its own path is a loop.
func (x *overviewIndex) checkLoops() error {
	const walking, settled = 1, 2
	state := make(map[string]int, len(x.teams))
	for _, start := range x.teams {
		var path []string
		for id := start.ID; id != "" && state[id] != settled; id = x.teams[x.byID[id]].Parent {
			if state[id] == walking {
				return badFact("team %q is its own ancestor", id)
			}
			state[id] = walking
			path = append(path, id)
		}
		for _, id := range path {
			state[id] = settled
		}
	}
	return nil
}

// checkManagers refuses a stored manager who is not one of the team's
// members. The store's own repair clears such a manager on its next write, so
// the file as read is inconsistent; answering "no manager" would claim an
// empty seat the file does not say is empty.
func (x *overviewIndex) checkManagers() error {
	for _, t := range x.teams {
		if t.Manager != "" && !t.Holds(t.Manager) {
			return badFact("team %q names a manager who is not one of its members", t.ID)
		}
	}
	return nil
}

// ── THE PAGES ───────────────────────────────────────────────────────────────

// overviewPage is the whole collection a request pages through.
type overviewPage struct {
	parent  string
	total   int
	teams   []teamstore.Team
	members []appwire.OverviewMember
}

// teamPage is the open teams directly under parent, newest made first and then
// by id. An omitted parent is the root when the file has one, else the top.
//
// THE FACTORY'S TEAMS ARE NOT CREWS. A job's agent and its steps' chats belong
// on the job's own page (factory.job_chats), so a team the factory made is left
// out of every crews list; asking for one by id still answers it.
func (x *overviewIndex) teamPage(parent string) (overviewPage, error) {
	if parent == "" && x.root >= 0 {
		parent = x.teams[x.root].ID
	}
	if parent != "" {
		if _, err := x.openTeam(parent); err != nil {
			return overviewPage{}, err
		}
	}
	var kids []teamstore.Team
	for _, t := range x.teams {
		if t.Parent == parent && !t.Closed() && !t.Root && !t.FactoryManaged {
			kids = append(kids, t)
		}
	}
	slices.SortFunc(kids, func(a, b teamstore.Team) int {
		if c := b.Made.Compare(a.Made); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return overviewPage{parent: parent, total: len(kids), teams: kids}, nil
}

// memberPage is one non-root team's direct members, once each by conversation,
// in the order the file stores them.
func (x *overviewIndex) memberPage(parent string) (overviewPage, error) {
	t, err := x.openTeam(parent)
	if err != nil {
		return overviewPage{}, err
	}
	if t.Root {
		return overviewPage{}, appwire.Errorf(appwire.CodeBadParams, "the root's members are not listed; ask for the teams under it")
	}
	members := uniqueMembers(t)
	return overviewPage{parent: parent, total: len(members), members: members}, nil
}

// openTeam is the team id, which must exist and be open.
func (x *overviewIndex) openTeam(id string) (teamstore.Team, error) {
	i, ok := x.byID[id]
	if !ok {
		return teamstore.Team{}, appwire.Errorf(appwire.CodeNotFound, "there is no team %q", clipID(id))
	}
	if x.teams[i].Closed() {
		return teamstore.Team{}, appwire.Errorf(appwire.CodeBadParams, "team %q is closed", clipID(id))
	}
	return x.teams[i], nil
}

func clipID(id string) string {
	if len(id) > 64 {
		return id[:64] + "…"
	}
	return id
}

// uniqueMembers is the team's members once per stored conversation key, first
// stored first. A member with no key names no conversation and is left out.
func uniqueMembers(t teamstore.Team) []appwire.OverviewMember {
	seen := make(map[string]bool, len(t.Members))
	out := make([]appwire.OverviewMember, 0, len(t.Members))
	for _, m := range t.Members {
		if m.Key == "" || seen[m.Key] {
			continue
		}
		seen[m.Key] = true
		out = append(out, overviewMember(m))
	}
	return out
}

func overviewMember(m teamstore.Member) appwire.OverviewMember {
	return appwire.OverviewMember{Agent: appMemberAgent(m), Handle: m.Handle, Title: m.Word, JoinedAt: overviewTime(m.JoinedAt)}
}

func overviewTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// summary is the team's facts. The manager is the stored manager's membership
// and nothing else ([overviewIndex.checkManagers] refused any other). The face
// is that manager and then the next distinct stored members, at most
// [appwire.OverviewFaceMax]; nothing is invented to fill it.
func (x *overviewIndex) summary(t teamstore.Team) appwire.TeamSummary {
	members := uniqueMembers(t)
	s := appwire.TeamSummary{
		ID: t.ID, Name: t.Name, Parent: t.Parent, Root: t.Root,
		MemberCount: len(members), ChildCount: x.open[t.ID], MadeAt: overviewTime(t.Made),
	}
	if t.Hued() {
		hue := t.Hue
		s.Hue = &hue
	}
	var managerAgent string
	if m, ok := t.Member(t.Manager); ok && t.Manager != "" {
		manager := overviewMember(m)
		s.Manager = &manager
		managerAgent = manager.Agent
		s.FaceMembers = append(s.FaceMembers, manager)
	}
	for _, m := range members {
		if len(s.FaceMembers) == appwire.OverviewFaceMax {
			break
		}
		if m.Agent != managerAgent {
			s.FaceMembers = append(s.FaceMembers, m)
		}
	}
	return s
}

// ── THE CURSOR ──────────────────────────────────────────────────────────────

// overviewCursor is what a next page is bound to: the exact bytes it was cut
// from, the kind and parent of the collection, and where it resumes. Nothing
// about the collection is kept between requests.
type overviewCursor struct {
	Rev    string `json:"r"`
	Kind   string `json:"k"`
	Parent string `json:"p"`
	Offset int    `json:"o"`
}

func (c overviewCursor) encode() string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// overviewOffset is where the request resumes: 0 for no cursor. A cursor cut
// from other bytes is stale and says to start again; one that is not a cursor,
// belongs to another collection or points outside this one is bad_params.
func overviewOffset(raw string, want overviewCursor, total int) (int, error) {
	if raw == "" {
		return 0, nil
	}
	var got overviewCursor
	bytes, err := base64.RawURLEncoding.DecodeString(raw)
	if err == nil {
		err = json.Unmarshal(bytes, &got)
	}
	if err != nil {
		return 0, appwire.Errorf(appwire.CodeBadParams, "cursor is not one a previous page gave")
	}
	if got.Rev != want.Rev {
		return 0, appwire.Errorf(appwire.CodeStaleCursor, "stale cursor: the teams changed since that page; start again from the first page")
	}
	if got.Kind != want.Kind || got.Parent != want.Parent || got.Offset <= 0 || got.Offset >= total {
		return 0, appwire.Errorf(appwire.CodeBadParams, "cursor belongs to another page; start again from the first page")
	}
	return got.Offset, nil
}
