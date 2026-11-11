package appwire

import (
	"context"
	"errors"
	"time"
)

// CREWS.OVERVIEW: one page of the teams under one parent, or of one team's
// direct members, read from the teams store alone. It is the All teams
// surface's source for "what teams are there and who is in them", and it is
// BOUNDED: one page cut by the store's index, one level of the tree per
// call, at most [OverviewLimitMax] rows, and a deadline of
// [OverviewTimeout]. It starts no conversation, watcher or session, repairs
// nothing and writes nothing. Spend, activity and latest updates are not in it:
// a fact the file does not hold is absent from the answer, never zero.

// FeatureCrewsOverview is the hello feature an engine serving crews.overview
// announces. An engine without it answers the method as unknown_method.
const FeatureCrewsOverview = "crews.overview"

// The overview's kinds, limits and deadline.
const (
	OverviewTeams        = "teams"
	OverviewMembers      = "members"
	OverviewLimitMax     = 100
	OverviewLimitDefault = 25
	OverviewTimeout      = 2 * time.Second
	// OverviewCursorMax and OverviewParentMax bound what a request may carry.
	OverviewCursorMax = 512
	OverviewParentMax = 128
	// OverviewFaceMax is the most members a team's face names.
	OverviewFaceMax = 3
)

// CodeStaleCursor is the error code of a cursor cut from other bytes than the
// file now holds: the caller starts again from the first page.
const CodeStaleCursor = "stale_cursor"

// OverviewAsk is crews.overview's params. Parent omitted or empty in teams
// mode is the top level (the root's children when the file has a root).
// Limit is nil for the default, otherwise 1 to [OverviewLimitMax].
type OverviewAsk struct {
	Parent string `json:"parent,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Limit  *int   `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// OverviewMember is one conversation a team holds. Handle and Title are the
// stored words, absent when the file has none; nothing is invented for them.
type OverviewMember struct {
	Agent    string `json:"agent"`
	Handle   string `json:"handle,omitempty"`
	Title    string `json:"title,omitempty"`
	JoinedAt string `json:"joined_at,omitempty"`
}

// TeamSummary is one team and the facts the file holds about it. MemberCount is
// its direct members by distinct conversation; ChildCount its direct open
// teams. Manager is present only when the team stores a manager who is one
// of its members; a stored manager who is not a member is answered as no
// manager (the store keeps a team's manager among its members).
// FaceMembers is who the team's face shows: the manager first when there is
// one, then up to [OverviewFaceMax] distinct stored members in all, absent for
// a team with none. Hue and MadeAt are present only when stored.
type TeamSummary struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Parent      string           `json:"parent"`
	Root        bool             `json:"root,omitempty"`
	Hue         *float64         `json:"hue,omitempty"`
	Manager     *OverviewMember  `json:"manager,omitempty"`
	FaceMembers []OverviewMember `json:"face_members,omitempty"`
	MemberCount int              `json:"member_count"`
	ChildCount  int              `json:"child_count"`
	MadeAt      string           `json:"made_at,omitempty"`
}

// Overview is crews.overview's answer. Revision names the exact bytes read;
// Root is the file's real root or null; Total counts the collection the page
// is cut from (the siblings or the members), never descendants. Next is
// absent only when the page is the last.
type Overview struct {
	Revision string           `json:"revision"`
	Parent   string           `json:"parent"`
	Kind     string           `json:"kind"`
	Root     *TeamSummary     `json:"root"`
	Teams    []TeamSummary    `json:"teams"`
	Members  []OverviewMember `json:"members"`
	Total    int              `json:"total"`
	Next     string           `json:"next,omitempty"`
}

// TeamsOverviewer is the optional engine half of crews.overview. It honours
// ctx: a cancelled or expired context ends the read and the answer is the
// context's error.
type TeamsOverviewer interface {
	CrewsOverview(ctx context.Context, ask OverviewAsk) (*Overview, error)
}

// withCrewsOverviewFeature is hello carrying [FeatureCrewsOverview] when the
// engine serves it.
func withCrewsOverviewFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(TeamsOverviewer); !ok {
		return hello
	}
	return withFeature(hello, FeatureCrewsOverview)
}

// CheckOverviewAsk is the shape of a request, before any file is read: the
// effective kind and limit, or bad_params.
func CheckOverviewAsk(p OverviewAsk) (kind string, limit int, err error) {
	kind = p.Kind
	if kind == "" {
		kind = OverviewTeams
	}
	if kind != OverviewTeams && kind != OverviewMembers {
		return "", 0, Errorf(CodeBadParams, "kind is %q or %q", OverviewTeams, OverviewMembers)
	}
	limit = OverviewLimitDefault
	if p.Limit != nil {
		limit = *p.Limit
		if limit < 1 || limit > OverviewLimitMax {
			return "", 0, Errorf(CodeBadParams, "limit is 1 to %d", OverviewLimitMax)
		}
	}
	if len(p.Parent) > OverviewParentMax || (p.Parent != "" && !plainText(p.Parent, OverviewParentMax)) {
		return "", 0, Errorf(CodeBadParams, "parent is a team id")
	}
	if len(p.Cursor) > OverviewCursorMax {
		return "", 0, Errorf(CodeBadParams, "cursor is one a previous page gave")
	}
	if kind == OverviewMembers && p.Parent == "" {
		return "", 0, Errorf(CodeBadParams, "members needs the team whose members to list")
	}
	return kind, limit, nil
}

// overviewError is a failed read in the wire's words: a typed wire error as it
// is, a context's end as one sentence, and anything else as a fixed sentence
// that carries no path.
func overviewError(err error) error {
	var wire *Error
	switch {
	case errors.As(err, &wire):
		return err
	case errors.Is(err, context.DeadlineExceeded):
		return Errorf(CodeEngine, "crews.overview took longer than %s", OverviewTimeout)
	case errors.Is(err, context.Canceled):
		return Errorf(CodeEngine, "crews.overview was cancelled")
	}
	return Errorf(CodeEngine, "the teams could not be read")
}

func init() {
	methods["crews.overview"] = typed(func(e Engine, p OverviewAsk) (*Overview, error) {
		o, ok := e.(TeamsOverviewer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve crews.overview")
		}
		if _, _, err := CheckOverviewAsk(p); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), OverviewTimeout)
		defer cancel()
		res, err := o.CrewsOverview(ctx, p)
		if err != nil {
			return nil, overviewError(err)
		}
		return res, nil
	})
}
