package main

// appteamsoverview.go — crews.overview: one bounded page of the teams under
// one parent, or of one team's direct members, from the teams store alone.
//
// IT STARTS NOTHING AND WRITES NOTHING. It never calls b.crews() (the watch),
// never loads a conversation, reads no ledger, transcript or traffic, and reads
// one page of the store in one read transaction, which repairs nothing. The
// tree's rules are the store's to keep: every write tidies the whole list
// before it lands, so the page does not walk the tree to check them again.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

var _ appwire.TeamsOverviewer = (*appBackend)(nil)

// CrewsOverview answers crews.overview. The page is cut in SQL
// ([teamstore.PageTeams], [teamstore.PageMembers]): one level of one parent,
// counted and cut by the store's index, so it costs the same however many
// teams there are and however old they are.
func (b *appBackend) CrewsOverview(ctx context.Context, ask appwire.OverviewAsk) (*appwire.Overview, error) {
	kind, limit, err := appwire.CheckOverviewAsk(ask)
	if err != nil {
		return nil, err
	}
	out := &appwire.Overview{Kind: kind, Teams: []appwire.TeamSummary{}, Members: []appwire.OverviewMember{}}
	// at checks the cursor against the collection once the store has said
	// what the collection is, inside the same read.
	at := func(rev teamstore.Revision, parent teamstore.Team, total int) (int, error) {
		if kind == appwire.OverviewMembers && parent.Root {
			return 0, appwire.Errorf(appwire.CodeBadParams, "the root's members are not listed; ask for the teams under it")
		}
		out.Revision, out.Total = string(rev), total
		return overviewOffset(ask.Cursor, overviewCursor{Rev: string(rev), Kind: kind, Parent: parent.ID}, total)
	}
	var (
		root   *teamstore.Team
		open   map[string]int
		offset int
	)
	if kind == appwire.OverviewMembers {
		page, err := teamstore.PageMembers(ctx, b.crewProfile(), ask.Parent, limit, func(rev teamstore.Revision, parent teamstore.Team, total int) (int, error) {
			offset, err = at(rev, parent, total)
			return offset, err
		})
		if err != nil {
			return nil, overviewReadError(err, ask.Parent)
		}
		root, open, out.Parent = page.Root, page.Open, ask.Parent
		for _, m := range page.Members {
			out.Members = append(out.Members, overviewMember(m))
		}
	} else {
		page, err := teamstore.PageTeams(ctx, b.crewProfile(), ask.Parent, limit, func(rev teamstore.Revision, parent teamstore.Team, total int) (int, error) {
			offset, err = at(rev, parent, total)
			return offset, err
		})
		if err != nil {
			return nil, overviewReadError(err, ask.Parent)
		}
		root, open, out.Parent = page.Root, page.Open, page.Parent
		for _, t := range page.Teams {
			out.Teams = append(out.Teams, overviewSummary(t, open))
		}
	}
	if root != nil {
		summary := overviewSummary(*root, open)
		out.Root = &summary
	}
	if end := offset + len(out.Teams) + len(out.Members); end < out.Total {
		out.Next = overviewCursor{Rev: out.Revision, Kind: kind, Parent: out.Parent, Offset: end}.encode()
	}
	return out, ctx.Err()
}

// overviewReadError is a failed page in the wire's words, with no path.
func overviewReadError(err error, parent string) error {
	switch {
	case errors.Is(err, teamstore.ErrNoTeam):
		return appwire.Errorf(appwire.CodeNotFound, "there is no team %q", clipID(parent))
	case errors.Is(err, teamstore.ErrClosed):
		return appwire.Errorf(appwire.CodeBadParams, "team %q is closed", clipID(parent))
	case errors.Is(err, teamstore.ErrUnreadable):
		return appwire.Errorf(appwire.CodeEngine, "the crews store is unreadable")
	}
	return err
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

// overviewSummary is the team's facts; open is the count of each team's
// direct open, non-root children. The manager is the stored manager's
// membership and nothing else. The face is that manager and then the next
// distinct stored members, at most [appwire.OverviewFaceMax]; nothing is
// invented to fill it.
func overviewSummary(t teamstore.Team, open map[string]int) appwire.TeamSummary {
	members := uniqueMembers(t)
	s := appwire.TeamSummary{
		ID: t.ID, Name: t.Name, Parent: t.Parent, Root: t.Root,
		MemberCount: len(members), ChildCount: open[t.ID], MadeAt: overviewTime(t.Made),
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
