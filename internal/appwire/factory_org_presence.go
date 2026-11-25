package appwire

import (
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// ── WHO IS HERE, AND WHO CHANGED IT ────────────────────────────────────────
//
// factory.org_presence is the desk's heartbeat for an organization item it
// has open (POST /v1/presence): send it every ten seconds while the item is
// open, with the part the person is on and whether they are editing it, and
// once with left when it closes. The answer is who is on that item now. A
// person not heard from for thirty seconds has left. The server publishes a
// change to who is here as `presence`, which the engine relays as
// factory.org_presence to the windows of that connection; the server has
// already left out an item this person cannot view.
//
// factory.org_workstream_activity is one workstream's recorded changes,
// oldest first, each with the person who made it
// (GET /v1/workstreams/{id}/activity).
//
// An engine that serves them says "org_presence" in hello.

// FeatureOrgPresence is the hello word for presence, activity and `by` on
// every organization event.
const FeatureOrgPresence = "org_presence"

// EventFactoryOrgPresence is who is on one organization item now; its data
// is [FactoryOrgPresence].
const EventFactoryOrgPresence = "factory.org_presence"

// OrgPresenceItem names one organization item: a "workstream", or a "canvas"
// on a server that keeps canvases.
type OrgPresenceItem struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// OrgPresenceHere is one person on an item: the part (a page id, a frame id,
// or "" for the item itself) and whether they are editing it.
type OrgPresenceHere struct {
	Handle  string `json:"handle"`
	Part    string `json:"part,omitempty"`
	Editing bool   `json:"editing,omitempty"`
}

// OrgPresence is factory.org_presence's answer: who is on the item now.
type OrgPresence struct {
	Item OrgPresenceItem   `json:"item"`
	Here []OrgPresenceHere `json:"here"`
}

// FactoryOrgPresence is one change to who is on an organization item, on
// the connection it came from.
type FactoryOrgPresence struct {
	Connection string            `json:"connection"`
	Item       OrgPresenceItem   `json:"item"`
	Here       []OrgPresenceHere `json:"here"`
}

// OrgPresenceParams is one heartbeat. Client tells one window from another.
type OrgPresenceParams struct {
	OrgConn
	Item    OrgPresenceItem `json:"item"`
	Part    string          `json:"part,omitempty"`
	Editing bool            `json:"editing,omitempty"`
	Left    bool            `json:"left,omitempty"`
	Client  string          `json:"client,omitempty"`
}

// OrgActor is who made a recorded change.
type OrgActor struct {
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	Operator bool   `json:"operator,omitempty"`
}

// OrgActivityEvent is one recorded change: the verb is what a person reads
// ("changed workstream", "changed go-aheads"), detail names what changed
// (the fields, never their words), handle is who made it.
type OrgActivityEvent struct {
	ID     string   `json:"id"`
	At     string   `json:"at"`
	Kind   string   `json:"kind"`
	Verb   string   `json:"verb"`
	Object string   `json:"object"`
	Handle string   `json:"handle"`
	Detail string   `json:"detail"`
	Actor  OrgActor `json:"actor"`
}

// OrgActivityPerson is how a person who made a change is named.
type OrgActivityPerson struct {
	Name      string `json:"name,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// OrgActivity is factory.org_workstream_activity's answer, oldest first.
type OrgActivity struct {
	ID     string                       `json:"id"`
	Events []OrgActivityEvent           `json:"events"`
	People map[string]OrgActivityPerson `json:"people,omitempty"`
}

// OrgActivityParams names one workstream, and optionally only the changes
// after Since (RFC 3339) and the newest Limit of them.
type OrgActivityParams struct {
	OrgConn
	ID    string `json:"id"`
	Since string `json:"since,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

func withFactoryOrgPresenceFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryOrg); ok {
		hello = withFeature(hello, FeatureOrgPresence)
	}
	return hello
}

func init() {
	eventShapes[EventFactoryOrgPresence] = reflect.TypeFor[FactoryOrgPresence]()
	methods["factory.org_presence"] = filled(orgMethod[OrgPresenceParams, OrgPresence]("factory.org_presence", func(p OrgPresenceParams) (OrgRequest, error) {
		switch p.Item.Kind {
		case "workstream", "canvas":
		default:
			return OrgRequest{}, Errorf(CodeBadParams, `item.kind must be "workstream" or "canvas"`)
		}
		id, err := orgSegment("item.id", p.Item.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		for field, word := range map[string]string{"part": p.Part, "client": p.Client} {
			if word != "" && !plainText(word, 200) {
				return OrgRequest{}, Errorf(CodeBadParams, "%s must be one line of at most 200 characters", field)
			}
		}
		body := struct {
			Item    OrgPresenceItem `json:"item"`
			Part    string          `json:"part,omitempty"`
			Editing bool            `json:"editing,omitempty"`
			Left    bool            `json:"left,omitempty"`
			Client  string          `json:"client,omitempty"`
		}{OrgPresenceItem{p.Item.Kind, id}, strings.TrimSpace(p.Part), p.Editing, p.Left, strings.TrimSpace(p.Client)}
		return OrgRequest{Method: http.MethodPost, Path: "/v1/presence", Body: body}, nil
	}), func(p *OrgPresence) { p.Here = nonNil(p.Here) })
	methods["factory.org_workstream_activity"] = filled(orgMethod[OrgActivityParams, OrgActivity]("factory.org_workstream_activity", func(p OrgActivityParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		q := url.Values{}
		if p.Since != "" {
			if _, err := time.Parse(time.RFC3339Nano, p.Since); err != nil {
				return OrgRequest{}, Errorf(CodeBadParams, "since must be an RFC 3339 time")
			}
			q.Set("since", p.Since)
		}
		if p.Limit < 0 {
			return OrgRequest{}, Errorf(CodeBadParams, "limit must be positive")
		}
		if p.Limit > 0 {
			q.Set("limit", strconv.Itoa(p.Limit))
		}
		if len(q) > 0 {
			path += "/activity?" + q.Encode()
		} else {
			path += "/activity"
		}
		return OrgRequest{Method: http.MethodGet, Path: path}, nil
	}), func(a *OrgActivity) { a.Events = nonNil(a.Events) })
}
