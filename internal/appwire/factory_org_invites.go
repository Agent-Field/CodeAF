package appwire

import (
	"encoding/json"
	"net/http"
	"net/url"
)

// ── INVITES AND THE JOINER'S WELCOME (workstreams v2 design, J4 and J7) ───
//
// Settings › Organizations › People invites someone by email with a team and
// workstreams; the person who joins through it reads, once, who added them.
//
//	factory.org_invites          OrgConn            -> OrgInvites    GET    /v1/invites
//	factory.org_invite_create    OrgInviteNewParams -> OrgInviteMade POST   /v1/invites
//	factory.org_invite_resend    OrgInviteParams    -> OrgInviteMade POST   /v1/invites/{id}/resend
//	factory.org_invite_withdraw  OrgInviteParams    -> OrgInviteGone DELETE /v1/invites/{id}
//	factory.org_welcome          OrgConn            -> OrgWelcome    GET    /v1/me/welcome
//	factory.org_welcome_seen     OrgConn            -> OrgInviteGone POST   /v1/me/welcome/seen
//	factory.org_person_remove    OrgPersonParams    -> OrgInviteGone DELETE /v1/people/{handle}/sessions
//
// Removing a person (the operator's) ends their sign-ins, takes them out of
// every team and workstream, gives the workstreams they owned to the operator
// and offers the operator the tasks they owned, under Handed to you.
//
// Only the organization's operator lists, makes, sends again or withdraws an
// invite. The link in OrgInviteMade is shown that once. Before they join, an
// invited person goes by "invite:<id>" among a workstream's people and as a
// task's owner or doer; joining rewrites every one of those to them.

// FeatureOrgInvites is the hello word for the methods above.
const FeatureOrgInvites = "org_invites"

// OrgInviteNewParams is Invite someone: an email or at least a name, a team
// id and workstream ids, all optional but the first.
type OrgInviteNewParams struct {
	OrgConn
	Email       string   `json:"email,omitempty"`
	Name        string   `json:"name,omitempty"`
	Team        string   `json:"team,omitempty"`
	Workstreams []string `json:"workstreams,omitempty"`
}

// OrgInviteParams names one invite.
type OrgInviteParams struct {
	OrgConn
	ID string `json:"id"`
}

// OrgPersonParams names one person of the organization by handle.
type OrgPersonParams struct {
	OrgConn
	Handle string `json:"handle"`
}

// The server's shapes, passed through.
type (
	OrgInvites    = json.RawMessage
	OrgInviteMade = json.RawMessage
	OrgInviteGone = json.RawMessage
	OrgWelcome    = json.RawMessage
)

func withFactoryOrgInvitesFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryOrg); ok {
		hello = withFeature(hello, FeatureOrgInvites)
	}
	return hello
}

func orgInvitePath(id string) (string, error) {
	seg, err := orgSegment("id", id)
	if err != nil {
		return "", err
	}
	return "/v1/invites/" + url.PathEscape(seg), nil
}

func init() {
	methods["factory.org_invites"] = orgMethod[OrgConn, OrgInvites]("factory.org_invites", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodGet, Path: "/v1/invites"}, nil
	})
	methods["factory.org_invite_create"] = orgMethod[OrgInviteNewParams, OrgInviteMade]("factory.org_invite_create", func(p OrgInviteNewParams) (OrgRequest, error) {
		if (p.Email != "" && !plainText(p.Email, 254)) || (p.Name != "" && !plainText(p.Name, 120)) {
			return OrgRequest{}, Errorf(CodeBadParams, "email and name are one line each")
		}
		for _, id := range p.Workstreams {
			if _, err := orgSegment("workstreams", id); err != nil {
				return OrgRequest{}, err
			}
		}
		if p.Team != "" {
			if _, err := orgSegment("team", p.Team); err != nil {
				return OrgRequest{}, err
			}
		}
		return OrgRequest{Method: http.MethodPost, Path: "/v1/invites", Body: map[string]any{
			"email": p.Email, "name": p.Name, "team": p.Team, "workstreams": nonNil(p.Workstreams)}}, nil
	})
	methods["factory.org_invite_resend"] = orgMethod[OrgInviteParams, OrgInviteMade]("factory.org_invite_resend", func(p OrgInviteParams) (OrgRequest, error) {
		path, err := orgInvitePath(p.ID)
		return OrgRequest{Method: http.MethodPost, Path: path + "/resend"}, err
	})
	methods["factory.org_invite_withdraw"] = orgMethod[OrgInviteParams, OrgInviteGone]("factory.org_invite_withdraw", func(p OrgInviteParams) (OrgRequest, error) {
		path, err := orgInvitePath(p.ID)
		return OrgRequest{Method: http.MethodDelete, Path: path}, err
	})
	methods["factory.org_welcome"] = orgMethod[OrgConn, OrgWelcome]("factory.org_welcome", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodGet, Path: "/v1/me/welcome"}, nil
	})
	methods["factory.org_person_remove"] = orgMethod[OrgPersonParams, OrgInviteGone]("factory.org_person_remove", func(p OrgPersonParams) (OrgRequest, error) {
		handle, err := orgHandle(p.Handle)
		if err != nil {
			return OrgRequest{}, err
		}
		return OrgRequest{Method: http.MethodDelete, Path: "/v1/people/" + url.PathEscape(handle) + "/sessions"}, nil
	})
	methods["factory.org_welcome_seen"] = orgMethod[OrgConn, OrgInviteGone]("factory.org_welcome_seen", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodPost, Path: "/v1/me/welcome/seen"}, nil
	})
}
