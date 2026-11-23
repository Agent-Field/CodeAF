package appwire

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// ── WHO MAY DO WHAT ON AN ORGANIZATION'S SERVER ────────────────────────────
//
// An organization keeps one access policy (factory/serve access_policy.go): the
// named roles, what each kind of thing starts as, who may share, and what the
// operator and agents may do. Any signed-in member reads it; only the
// operator changes it. Beside it, every workstream, canvas and chat lists the
// people, teams and organization that hold a role on it, and a person who may
// share it adds or removes one. Share is not move: a share never changes where
// a thing lives, a move changes where and so who reaches it.
//
//	factory.org_policy          OrgConn                 -> the policy            GET    /v1/org/policy
//	factory.org_policy_set      OrgPolicySetParams      -> the saved policy      PUT    /v1/org/policy (operator)
//	factory.org_grants          OrgResourceParams       -> {resource, grants[]}  GET    /v1/resources/{kind}/{id}/grants
//	factory.org_grant_add       OrgGrantParams          -> the grant             POST   /v1/resources/{kind}/{id}/grants
//	factory.org_grant_preview   OrgGrantParams          -> {gains, loses, ...}   POST   /v1/resources/{kind}/{id}/grants/preview
//	factory.org_grant_remove    OrgGrantRemoveParams    -> {}                    DELETE /v1/resources/{kind}/{id}/grants/{grant}
//	factory.org_move            OrgMoveParams           -> {resource, dropped}   POST   /v1/resources/{kind}/{id}/move
//
// THE APP NEVER CHOOSES A PATH. Each method is one request whose path is built
// here from ids checked as one segment, and whose body is the fields the
// server's route takes and nothing else. The server decides every question
// of who may do what; the wire adds no rule of its own.
//
// An engine that serves them says "org_access" in hello.

// FeatureOrgAccess is the hello word, exactly as the desk reads it.
const FeatureOrgAccess = "org_access"

// orgPolicyMost is the most a policy document may be, in bytes. A real one is
// a few kilobytes: a handful of roles and one row per kind of thing.
const orgPolicyMost = 64 << 10

// orgKindPattern is a kind of resource as the server names it.
var orgKindPattern = regexp.MustCompile(`^[a-z][a-z_]{0,31}$`)

// OrgPolicySetParams is the operator's new policy: the whole document the
// last read answered, edited, with the rev it was read at (a stale rev is
// refused so a policy is never saved over one nobody read).
type OrgPolicySetParams struct {
	OrgConn
	Policy json.RawMessage `json:"policy"`
}

// OrgResourceParams names one resource: a workstream, a canvas, a chat.
type OrgResourceParams struct {
	OrgConn
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// OrgGrantSubject is who a grant reaches: a person (by handle), a team (by id),
// a service (by id) or the whole organization (id "*").
type OrgGrantSubject struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// OrgGrantParams is a grant to add, or to preview without adding.
type OrgGrantParams struct {
	OrgResourceParams
	Subject OrgGrantSubject `json:"subject"`
	Role    string          `json:"role"`
	// ExpiresAt is when the grant lapses, in RFC 3339; empty never.
	ExpiresAt string `json:"expires_at,omitempty"`
	Note      string `json:"note,omitempty"`
}

// OrgGrantRemoveParams removes one grant from a resource.
type OrgGrantRemoveParams struct {
	OrgResourceParams
	Grant string `json:"grant"`
}

// OrgMoveParams moves a resource into another space; To is the id of the
// workstream, or of the caller's own space.
type OrgMoveParams struct {
	OrgResourceParams
	To string `json:"to"`
}

func orgResourcePath(p OrgResourceParams) (string, error) {
	kind := strings.TrimSpace(p.Kind)
	if !orgKindPattern.MatchString(kind) {
		return "", Errorf(CodeBadParams, "kind is not a kind of thing this organization keeps")
	}
	id, err := orgSegment("id", p.ID)
	if err != nil {
		return "", err
	}
	return "/v1/resources/" + kind + "/" + url.PathEscape(id), nil
}

func orgGrantBody(p OrgGrantParams) (map[string]any, error) {
	subject := OrgGrantSubject{Kind: strings.TrimSpace(p.Subject.Kind), ID: strings.TrimSpace(p.Subject.ID)}
	if !orgKindPattern.MatchString(subject.Kind) {
		return nil, Errorf(CodeBadParams, "subject.kind is a person, a team, a service or org")
	}
	if subject.ID == "" || !plainText(subject.ID, 128) {
		return nil, Errorf(CodeBadParams, "subject.id is not an id this organization gave")
	}
	role := strings.TrimSpace(p.Role)
	if role == "" || !plainText(role, 64) {
		return nil, Errorf(CodeBadParams, "role is required")
	}
	body := map[string]any{"subject": subject, "role": role}
	if at := strings.TrimSpace(p.ExpiresAt); at != "" {
		body["expires_at"] = at
	}
	if note := strings.TrimSpace(p.Note); note != "" {
		if len(note) > 500 || !plainText(note, 500) {
			return nil, Errorf(CodeBadParams, "note is at most 500 bytes of plain text")
		}
		body["note"] = note
	}
	return body, nil
}

func withFactoryOrgAccessFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryOrg); ok {
		hello = withFeature(hello, FeatureOrgAccess)
	}
	return hello
}

func init() {
	methods["factory.org_policy"] = orgMethod[OrgConn, json.RawMessage]("factory.org_policy", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodGet, Path: "/v1/org/policy"}, nil
	})
	methods["factory.org_policy_set"] = orgMethod[OrgPolicySetParams, json.RawMessage]("factory.org_policy_set", func(p OrgPolicySetParams) (OrgRequest, error) {
		doc := bytes.TrimSpace(p.Policy)
		if len(doc) == 0 || doc[0] != '{' || len(doc) > orgPolicyMost || !json.Valid(doc) {
			return OrgRequest{}, Errorf(CodeBadParams, "policy is the policy document, a JSON object of at most 64 KB")
		}
		return OrgRequest{Method: http.MethodPut, Path: "/v1/org/policy", Body: json.RawMessage(doc)}, nil
	})
	methods["factory.org_grants"] = orgMethod[OrgResourceParams, json.RawMessage]("factory.org_grants", func(p OrgResourceParams) (OrgRequest, error) {
		path, err := orgResourcePath(p)
		return OrgRequest{Method: http.MethodGet, Path: path + "/grants"}, err
	})
	methods["factory.org_grant_add"] = orgMethod[OrgGrantParams, json.RawMessage]("factory.org_grant_add", func(p OrgGrantParams) (OrgRequest, error) {
		path, err := orgResourcePath(p.OrgResourceParams)
		if err != nil {
			return OrgRequest{}, err
		}
		body, err := orgGrantBody(p)
		return OrgRequest{Method: http.MethodPost, Path: path + "/grants", Body: body}, err
	})
	methods["factory.org_grant_preview"] = orgMethod[OrgGrantParams, json.RawMessage]("factory.org_grant_preview", func(p OrgGrantParams) (OrgRequest, error) {
		path, err := orgResourcePath(p.OrgResourceParams)
		if err != nil {
			return OrgRequest{}, err
		}
		body, err := orgGrantBody(p)
		return OrgRequest{Method: http.MethodPost, Path: path + "/grants/preview", Body: body}, err
	})
	methods["factory.org_grant_remove"] = orgMethod[OrgGrantRemoveParams, json.RawMessage]("factory.org_grant_remove", func(p OrgGrantRemoveParams) (OrgRequest, error) {
		path, err := orgResourcePath(p.OrgResourceParams)
		if err != nil {
			return OrgRequest{}, err
		}
		grant, err := orgSegment("grant", p.Grant)
		if err != nil {
			return OrgRequest{}, err
		}
		return OrgRequest{Method: http.MethodDelete, Path: path + "/grants/" + url.PathEscape(grant)}, nil
	})
	methods["factory.org_move"] = orgMethod[OrgMoveParams, json.RawMessage]("factory.org_move", func(p OrgMoveParams) (OrgRequest, error) {
		path, err := orgResourcePath(p.OrgResourceParams)
		if err != nil {
			return OrgRequest{}, err
		}
		to, err := orgSegment("to", p.To)
		if err != nil {
			return OrgRequest{}, err
		}
		return OrgRequest{Method: http.MethodPost, Path: path + "/move", Body: map[string]string{"to": to}}, nil
	})
}
