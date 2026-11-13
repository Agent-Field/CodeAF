package appwire

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ── AN ORGANIZATION'S TEAMS AND WORKSTREAMS ─────────────────────────────────
//
// An organization is one `codeaf factory serve` server. Its teams (groups of
// people) and its workstreams live on that server and nowhere else: this
// engine keeps no copy, so every factory.team_* and factory.org_workstream*
// method below is one request to the attached connection, made with the
// caller's existing session, and the server's answer comes back in the typed
// shape the server speaks. Nothing here decides who may see or change what;
// the server does, and its refusal is passed on with its status kept: 403 is
// forbidden, 409 is conflict, and 412 (a workstream changed since its rev was
// read) is conflict with the reason "stale".
//
// THE APP NEVER CHOOSES A PATH. Each method builds its one route here from
// checked ids, so an id cannot climb out of its segment and no method forwards
// an arbitrary path or body. The engine's half ([FactoryOrg]) only carries a
// built request to a connection.
//
// An engine that serves them says "org_teams" and "org_workstreams" in hello,
// and "org_roles" when shares carry people and roles and a workstream can be
// private (factory.org_workstream_shares takes shares, owner, private, rev).
// Whether ONE organization's server has the routes is its own answer: an older
// server's 404 is passed on as not_found.

// The hello words, exactly as the desk reads them.
const (
	FeatureOrgTeams       = "org_teams"
	FeatureOrgWorkstreams = "org_workstreams"
	FeatureOrgRoles       = "org_roles"
)

// OrgRequest is one built request to an organization's server: the method,
// the route (already escaped), the JSON body if any, and the rev a guarded
// write sends as If-Match.
type OrgRequest struct {
	Method  string
	Path    string
	Body    any
	IfMatch string
}

// FactoryOrg is the engine's half: carry req to the connection id with the
// session held for it, decode a 2xx answer into out (nil for a 204), and turn
// a refusal into the wire error described above.
type FactoryOrg interface {
	FactoryOrgCall(ctx context.Context, id string, req OrgRequest, out any) error
}

// ── the wire types (the server's contract, field for field) ────────────────

// OrgTeam is one team: a group of people in the organization.
type OrgTeam struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Members   []string `json:"members"`
	CreatedAt string   `json:"created_at,omitempty"`
	UpdatedAt string   `json:"updated_at,omitempty"`
}

// OrgTeams is factory.teams's answer: every team in the organization.
type OrgTeams struct {
	Teams []OrgTeam `json:"teams"`
}

// OrgOwner is a workstream's one owner, a person (by handle) or a team.
type OrgOwner struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// OrgShare gives one person (handle) or team (id) a role on a workstream:
// view, comment, edit or full.
type OrgShare struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Role string `json:"role"`
}

// OrgWorkstreamSummary is a workstream as a list shows it. MyRole is the
// caller's role; "" is the operator's name-only row for someone's private
// workstream, whose content the server answers 404.
type OrgWorkstreamSummary struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Stage      string     `json:"stage"`
	Owner      OrgOwner   `json:"owner"`
	SharedWith []string   `json:"shared_with"`
	Shares     []OrgShare `json:"shares"`
	Private    bool       `json:"private"`
	MyRole     string     `json:"my_role"`
	NeedsYou   int        `json:"needs_you"`
	Merged     int        `json:"merged"`
	Total      int        `json:"total"`
	UpdatedAt  string     `json:"updated_at,omitempty"`
	ArchivedAt string     `json:"archived_at,omitempty"`
	Rev        int        `json:"rev"`
}

// OrgPage is one page of a workstream.
type OrgPage struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Parent    string `json:"parent,omitempty"`
	BodyMD    string `json:"body_md"`
	Kind      string `json:"kind"`
	UpdatedAt string `json:"updated_at,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

// OrgDecision is one decision a workstream keeps.
type OrgDecision struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	State string `json:"state"`
	At    string `json:"at,omitempty"`
	By    string `json:"by,omitempty"`
}

// OrgSource is one thing a workstream draws on.
type OrgSource struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Ref   string `json:"ref"`
	Title string `json:"title"`
}

// OrgWorkstream is one workstream whole. GoAheads are the server's go-aheads
// for it, each kept exactly as the server sent it.
type OrgWorkstream struct {
	OrgWorkstreamSummary
	Purpose   string            `json:"purpose"`
	Pages     []OrgPage         `json:"pages"`
	Decisions []OrgDecision     `json:"decisions"`
	Sources   []OrgSource       `json:"sources"`
	Links     []string          `json:"links"`
	GoAheads  []json.RawMessage `json:"go_aheads"`
	CreatedBy string            `json:"created_by,omitempty"`
}

// OrgWorkstreams is factory.org_workstreams's answer: those visible to the
// caller, newest first.
type OrgWorkstreams struct {
	Workstreams []OrgWorkstreamSummary `json:"workstreams"`
}

// OrgWorkstreamShared is factory.org_workstream_shares's answer: the
// workstream as it is now and the linked ones that were shared with it.
type OrgWorkstreamShared struct {
	Workstream OrgWorkstream `json:"workstream"`
	AlsoShared []string      `json:"also_shared"`
}

// OrgSharePreview is what include_linked would add.
type OrgSharePreview struct {
	Linked []OrgWorkstreamSummary `json:"linked"`
}

// ── params ─────────────────────────────────────────────────────────────────

// OrgConn names the organization's factory connection. Connection is the
// contract's word; factory, the word every older factory.* method takes, is
// read when connection is absent so neither spelling is a trap.
type OrgConn struct {
	Connection string `json:"connection"`
	Factory    string `json:"factory,omitempty"`
}

func (c OrgConn) id() (string, error) {
	id := strings.TrimSpace(c.Connection)
	if id == "" {
		id = strings.TrimSpace(c.Factory)
	}
	if id == "" {
		return "", Errorf(CodeBadParams, "connection is required")
	}
	return id, nil
}

// OrgTeamCreateParams names a new team.
type OrgTeamCreateParams struct {
	OrgConn
	Name string `json:"name"`
}

// OrgTeamRenameParams renames one team.
type OrgTeamRenameParams struct {
	OrgConn
	Team string `json:"team"`
	Name string `json:"name"`
}

// OrgTeamParams names one team.
type OrgTeamParams struct {
	OrgConn
	Team string `json:"team"`
}

// OrgTeamMemberParams names one person in one team.
type OrgTeamMemberParams struct {
	OrgConn
	Team   string `json:"team"`
	Handle string `json:"handle"`
}

// OrgWorkstreamParams names one workstream.
type OrgWorkstreamParams struct {
	OrgConn
	ID string `json:"id"`
}

// OrgWorkstreamNew is a workstream to make. Owner left out is the caller.
type OrgWorkstreamNew struct {
	Name      string        `json:"name"`
	Private   bool          `json:"private,omitempty"`
	Purpose   string        `json:"purpose,omitempty"`
	Stage     string        `json:"stage,omitempty"`
	Owner     *OrgOwner     `json:"owner,omitempty"`
	Pages     []OrgPage     `json:"pages,omitempty"`
	Decisions []OrgDecision `json:"decisions,omitempty"`
	Sources   []OrgSource   `json:"sources,omitempty"`
	Links     []string      `json:"links,omitempty"`
}

// OrgWorkstreamCreateParams is factory.org_workstream_create's request.
type OrgWorkstreamCreateParams struct {
	OrgConn
	OrgWorkstreamNew
}

// OrgWorkstreamPatch is any subset of a workstream's fields. A list sent
// replaces the list kept, and [] empties it; a field left out is unchanged.
type OrgWorkstreamPatch struct {
	Name      *string        `json:"name,omitempty"`
	Purpose   *string        `json:"purpose,omitempty"`
	Stage     *string        `json:"stage,omitempty"`
	Pages     *[]OrgPage     `json:"pages,omitempty"`
	Decisions *[]OrgDecision `json:"decisions,omitempty"`
	Sources   *[]OrgSource   `json:"sources,omitempty"`
	Links     *[]string      `json:"links,omitempty"`
}

// OrgWorkstreamUpdateParams is a patch guarded by the rev it was read at.
type OrgWorkstreamUpdateParams struct {
	OrgConn
	ID  string `json:"id"`
	Rev int    `json:"rev"`
	OrgWorkstreamPatch
}

// OrgWorkstreamOwnerParams hands a workstream to a person or a team.
type OrgWorkstreamOwnerParams struct {
	OrgConn
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Owner string `json:"owner"`
}

// OrgWorkstreamSharesParams sets who a workstream is shared with, in one save
// on the server: Shares (people and teams with roles) when sent, else Teams
// (each as edit, the older call); Owner and Private when sent; Rev, when sent,
// must be the rev read or the call is a stale conflict.
type OrgWorkstreamSharesParams struct {
	OrgConn
	ID            string      `json:"id"`
	Shares        *[]OrgShare `json:"shares,omitempty"`
	Teams         []string    `json:"teams"`
	Owner         *OrgOwner   `json:"owner,omitempty"`
	Private       *bool       `json:"private,omitempty"`
	Rev           *int        `json:"rev,omitempty"`
	IncludeLinked bool        `json:"include_linked"`
}

// OrgWorkstreamSharePreviewParams asks what sharing with teams would add.
type OrgWorkstreamSharePreviewParams struct {
	OrgConn
	ID     string     `json:"id"`
	Teams  []string   `json:"teams"`
	Shares []OrgShare `json:"shares,omitempty"`
}

// OrgWorkstreamPackage is a Personal workstream moving into the organization,
// once: the workstream without id, rev, owner or shares, and the id it had on
// this computer.
type OrgWorkstreamPackage struct {
	FromLocalID string            `json:"from_local_id"`
	Name        string            `json:"name"`
	Stage       string            `json:"stage,omitempty"`
	Purpose     string            `json:"purpose,omitempty"`
	Pages       []OrgPage         `json:"pages,omitempty"`
	Decisions   []OrgDecision     `json:"decisions,omitempty"`
	Sources     []OrgSource       `json:"sources,omitempty"`
	Links       []string          `json:"links,omitempty"`
	GoAheads    []json.RawMessage `json:"go_aheads,omitempty"`
	NeedsYou    int               `json:"needs_you,omitempty"`
	Merged      int               `json:"merged,omitempty"`
	Total       int               `json:"total,omitempty"`
	CreatedBy   string            `json:"created_by,omitempty"`
	UpdatedAt   string            `json:"updated_at,omitempty"`
	ArchivedAt  string            `json:"archived_at,omitempty"`
}

// OrgWorkstreamImportParams is factory.org_workstream_import's request.
type OrgWorkstreamImportParams struct {
	OrgConn
	OrgWorkstreamPackage
}

// ── checks ─────────────────────────────────────────────────────────────────

// orgSegment is an id fit to be one path segment (and one item of a comma
// list): plain, at most 128 bytes, nothing that ends or escapes a segment.
func orgSegment(field, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", Errorf(CodeBadParams, "%s is required", field)
	}
	if !plainText(value, 128) || strings.ContainsAny(value, "/\\?#%, ") || value == "." || value == ".." {
		return "", Errorf(CodeBadParams, "%s is not an id this organization gave", field)
	}
	return value, nil
}

// orgHandle is a GitHub handle, with or without its @.
func orgHandle(value string) (string, error) {
	h := strings.TrimPrefix(strings.TrimSpace(value), "@")
	if h == "" || len(h) > 39 {
		return "", Errorf(CodeBadParams, "handle must be a GitHub handle")
	}
	for _, r := range h {
		if !(r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return "", Errorf(CodeBadParams, "handle must be a GitHub handle")
		}
	}
	return h, nil
}

// orgName is a team's or workstream's name: one line of at most 200 bytes.
func orgName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !plainText(value, 200) {
		return "", Errorf(CodeBadParams, "name is required: one line of at most 200 characters")
	}
	return value, nil
}

func orgTeamList(teams []string) ([]string, error) {
	out := make([]string, 0, len(teams))
	for _, t := range teams {
		id, err := orgSegment("teams", t)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// orgShareList checks shares: a person by handle or a team by id, each with
// one of the four roles.
func orgShareList(shares []OrgShare) ([]OrgShare, error) {
	out := make([]OrgShare, 0, len(shares))
	for _, s := range shares {
		var err error
		switch s.Kind {
		case "person":
			s.ID, err = orgHandle(s.ID)
		case "team":
			s.ID, err = orgSegment("shares", s.ID)
		default:
			err = Errorf(CodeBadParams, `a share's kind must be "person" or "team"`)
		}
		if err != nil {
			return nil, err
		}
		switch s.Role {
		case "view", "comment", "edit", "full":
		default:
			return nil, Errorf(CodeBadParams, "a share's role must be view, comment, edit or full")
		}
		out = append(out, s)
	}
	return out, nil
}

func orgWorkstreamPath(id string) (string, error) {
	id, err := orgSegment("id", id)
	if err != nil {
		return "", err
	}
	return "/v1/workstreams/" + url.PathEscape(id), nil
}

func orgTeamPath(team string) (string, error) {
	team, err := orgSegment("team", team)
	if err != nil {
		return "", err
	}
	return "/v1/teams/" + url.PathEscape(team), nil
}

// ── the methods ────────────────────────────────────────────────────────────

func withFactoryOrgFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryOrg); ok {
		hello = withFeature(hello, FeatureOrgTeams)
		hello = withFeature(hello, FeatureOrgWorkstreams)
		hello = withFeature(hello, FeatureOrgRoles)
	}
	return hello
}

func factoryOrgOf(e Engine, name string) (FactoryOrg, error) {
	o, ok := e.(FactoryOrg)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return o, nil
}

// orgMethod is one organization method: build checks params into a request,
// and the answer is decoded into R. A request whose answer is a 204 answers R's
// zero value.
func orgMethod[P interface{ id() (string, error) }, R any](name string, build func(P) (OrgRequest, error)) method {
	return typed(func(e Engine, p P) (*R, error) {
		o, err := factoryOrgOf(e, name)
		if err != nil {
			return nil, err
		}
		id, err := p.id()
		if err != nil {
			return nil, err
		}
		req, err := build(p)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		var out R
		if err := o.FactoryOrgCall(ctx, id, req, &out); err != nil {
			return nil, err
		}
		return &out, nil
	})
}

// The answers' lists are lists on the wire, never null.
func (t *OrgTeam) fill() { t.Members = nonNil(t.Members) }
func (s *OrgWorkstreamSummary) fill() {
	s.SharedWith, s.Shares = nonNil(s.SharedWith), nonNil(s.Shares)
}
func (w *OrgWorkstream) fill() {
	w.OrgWorkstreamSummary.fill()
	w.Pages, w.Decisions, w.Sources = nonNil(w.Pages), nonNil(w.Decisions), nonNil(w.Sources)
	w.Links, w.GoAheads = nonNil(w.Links), nonNil(w.GoAheads)
}

// filled wraps a method so its answer's lists are never null.
func filled[R any](m method, fill func(*R)) method {
	call := m.call
	m.call = func(e Engine, raw json.RawMessage) (any, error) {
		out, err := call(e, raw)
		if r, ok := out.(*R); ok && r != nil {
			fill(r)
		}
		return out, err
	}
	return m
}

func init() {
	team := func(t *OrgTeam) { t.fill() }
	ws := func(w *OrgWorkstream) { w.fill() }
	summaries := func(list []OrgWorkstreamSummary) []OrgWorkstreamSummary {
		for i := range list {
			list[i].fill()
		}
		return nonNil(list)
	}

	methods["factory.teams"] = filled(orgMethod[OrgConn, OrgTeams]("factory.teams", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodGet, Path: "/v1/teams"}, nil
	}), func(t *OrgTeams) {
		for i := range t.Teams {
			t.Teams[i].fill()
		}
		t.Teams = nonNil(t.Teams)
	})
	methods["factory.team_create"] = filled(orgMethod[OrgTeamCreateParams, OrgTeam]("factory.team_create", func(p OrgTeamCreateParams) (OrgRequest, error) {
		name, err := orgName(p.Name)
		if err != nil {
			return OrgRequest{}, err
		}
		return OrgRequest{Method: http.MethodPost, Path: "/v1/teams", Body: map[string]string{"name": name}}, nil
	}), team)
	methods["factory.team_rename"] = filled(orgMethod[OrgTeamRenameParams, OrgTeam]("factory.team_rename", func(p OrgTeamRenameParams) (OrgRequest, error) {
		path, err := orgTeamPath(p.Team)
		if err != nil {
			return OrgRequest{}, err
		}
		name, err := orgName(p.Name)
		if err != nil {
			return OrgRequest{}, err
		}
		return OrgRequest{Method: http.MethodPatch, Path: path, Body: map[string]string{"name": name}}, nil
	}), team)
	methods["factory.team_delete"] = orgMethod[OrgTeamParams, Empty]("factory.team_delete", func(p OrgTeamParams) (OrgRequest, error) {
		path, err := orgTeamPath(p.Team)
		return OrgRequest{Method: http.MethodDelete, Path: path}, err
	})
	member := func(method string) func(OrgTeamMemberParams) (OrgRequest, error) {
		return func(p OrgTeamMemberParams) (OrgRequest, error) {
			path, err := orgTeamPath(p.Team)
			if err != nil {
				return OrgRequest{}, err
			}
			handle, err := orgHandle(p.Handle)
			if err != nil {
				return OrgRequest{}, err
			}
			return OrgRequest{Method: method, Path: path + "/members/" + url.PathEscape(handle)}, nil
		}
	}
	methods["factory.team_member_add"] = filled(orgMethod[OrgTeamMemberParams, OrgTeam]("factory.team_member_add", member(http.MethodPut)), team)
	methods["factory.team_member_remove"] = filled(orgMethod[OrgTeamMemberParams, OrgTeam]("factory.team_member_remove", member(http.MethodDelete)), team)

	methods["factory.org_workstreams"] = filled(orgMethod[OrgConn, OrgWorkstreams]("factory.org_workstreams", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodGet, Path: "/v1/workstreams"}, nil
	}), func(w *OrgWorkstreams) { w.Workstreams = summaries(w.Workstreams) })
	methods["factory.org_workstream"] = filled(orgMethod[OrgWorkstreamParams, OrgWorkstream]("factory.org_workstream", func(p OrgWorkstreamParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		return OrgRequest{Method: http.MethodGet, Path: path}, err
	}), ws)
	methods["factory.org_workstream_create"] = filled(orgMethod[OrgWorkstreamCreateParams, OrgWorkstream]("factory.org_workstream_create", func(p OrgWorkstreamCreateParams) (OrgRequest, error) {
		name, err := orgName(p.Name)
		if err != nil {
			return OrgRequest{}, err
		}
		body := p.OrgWorkstreamNew
		body.Name = name
		if body.Owner != nil {
			if err := checkOrgOwner(body.Owner); err != nil {
				return OrgRequest{}, err
			}
		}
		return OrgRequest{Method: http.MethodPost, Path: "/v1/workstreams", Body: body}, nil
	}), ws)
	methods["factory.org_workstream_update"] = filled(orgMethod[OrgWorkstreamUpdateParams, OrgWorkstream]("factory.org_workstream_update", func(p OrgWorkstreamUpdateParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		if p.Rev < 1 {
			return OrgRequest{}, Errorf(CodeBadParams, "rev is required: the rev the workstream was read at")
		}
		if p.Name != nil {
			name, err := orgName(*p.Name)
			if err != nil {
				return OrgRequest{}, err
			}
			p.Name = &name
		}
		return OrgRequest{Method: http.MethodPatch, Path: path, Body: p.OrgWorkstreamPatch, IfMatch: strconv.Itoa(p.Rev)}, nil
	}), ws)
	methods["factory.org_workstream_delete"] = orgMethod[OrgWorkstreamParams, Empty]("factory.org_workstream_delete", func(p OrgWorkstreamParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		return OrgRequest{Method: http.MethodDelete, Path: path}, err
	})
	methods["factory.org_workstream_owner"] = filled(orgMethod[OrgWorkstreamOwnerParams, OrgWorkstream]("factory.org_workstream_owner", func(p OrgWorkstreamOwnerParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		owner := &OrgOwner{Kind: p.Kind, ID: p.Owner}
		if err := checkOrgOwner(owner); err != nil {
			return OrgRequest{}, err
		}
		return OrgRequest{Method: http.MethodPut, Path: path + "/owner", Body: owner}, nil
	}), ws)
	methods["factory.org_workstream_shares"] = filled(orgMethod[OrgWorkstreamSharesParams, OrgWorkstreamShared]("factory.org_workstream_shares", func(p OrgWorkstreamSharesParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		// Only what the app sent is forwarded: teams left out is not "no
		// teams", or a call that only changes the owner would unshare it.
		body := struct {
			Shares        *[]OrgShare `json:"shares,omitempty"`
			Teams         *[]string   `json:"teams,omitempty"`
			Owner         *OrgOwner   `json:"owner,omitempty"`
			Private       *bool       `json:"private,omitempty"`
			Rev           *int        `json:"rev,omitempty"`
			IncludeLinked bool        `json:"include_linked"`
		}{Owner: p.Owner, Private: p.Private, Rev: p.Rev, IncludeLinked: p.IncludeLinked}
		switch {
		case p.Shares != nil:
			shares, err := orgShareList(*p.Shares)
			if err != nil {
				return OrgRequest{}, err
			}
			body.Shares = &shares
		case p.Teams != nil:
			teams, err := orgTeamList(p.Teams)
			if err != nil {
				return OrgRequest{}, err
			}
			body.Teams = &teams
		}
		if body.Owner != nil {
			if err := checkOrgOwner(body.Owner); err != nil {
				return OrgRequest{}, err
			}
		}
		return OrgRequest{Method: http.MethodPut, Path: path + "/shares", Body: body}, nil
	}), func(s *OrgWorkstreamShared) {
		s.Workstream.fill()
		s.AlsoShared = nonNil(s.AlsoShared)
	})
	methods["factory.org_workstream_share_preview"] = filled(orgMethod[OrgWorkstreamSharePreviewParams, OrgSharePreview]("factory.org_workstream_share_preview", func(p OrgWorkstreamSharePreviewParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		teams, err := orgTeamList(p.Teams)
		if err != nil {
			return OrgRequest{}, err
		}
		shares, err := orgShareList(p.Shares)
		if err != nil {
			return OrgRequest{}, err
		}
		query := "?teams=" + url.QueryEscape(strings.Join(teams, ","))
		if len(shares) > 0 {
			// The shares being added, with roles, as kind:id:role.
			added := make([]string, 0, len(shares))
			for _, s := range shares {
				added = append(added, s.Kind+":"+s.ID+":"+s.Role)
			}
			query += "&shares=" + url.QueryEscape(strings.Join(added, ","))
		}
		return OrgRequest{Method: http.MethodGet, Path: path + "/shares/preview" + query}, nil
	}), func(s *OrgSharePreview) { s.Linked = summaries(s.Linked) })
	methods["factory.org_workstream_import"] = filled(orgMethod[OrgWorkstreamImportParams, OrgWorkstream]("factory.org_workstream_import", func(p OrgWorkstreamImportParams) (OrgRequest, error) {
		name, err := orgName(p.Name)
		if err != nil {
			return OrgRequest{}, err
		}
		if strings.TrimSpace(p.FromLocalID) == "" {
			return OrgRequest{}, Errorf(CodeBadParams, "from_local_id is required: the workstream's id on this computer")
		}
		body := p.OrgWorkstreamPackage
		body.Name = name
		return OrgRequest{Method: http.MethodPost, Path: "/v1/workstreams/import", Body: body}, nil
	}), ws)
}

// checkOrgOwner refuses an owner that is neither a person nor a team, or whose
// id could not be one.
func checkOrgOwner(o *OrgOwner) error {
	switch o.Kind {
	case "person":
		h, err := orgHandle(o.ID)
		if err != nil {
			return Errorf(CodeBadParams, "a person owner is named by their GitHub handle")
		}
		o.ID = h
	case "team":
		id, err := orgSegment("owner", o.ID)
		if err != nil {
			return err
		}
		o.ID = id
	default:
		return Errorf(CodeBadParams, `kind must be "person" or "team"`)
	}
	return nil
}
