package appwire

import (
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"strings"
)

// ── MY SPACE AND A PERSON'S OWN CHATS ON AN ORGANIZATION'S SERVER ──────────
//
// Inside an organization each person has their own space (factory/serve
// myspace.go): pages, canvases and chats about the organization's work, run
// on its server with its key and charged to it under their name, seen by
// them alone. The space is a workstream marked space, so its files and
// canvases are read with the workstream methods; these are the rest:
//
//	factory.org_myspace              OrgConn                  -> {workstream, limit_usd, spent_usd, at_limit?} GET /v1/myspace
//	factory.org_myspace_totals       OrgConn                  -> {month, limit_usd, people[]}   GET /v1/myspace/totals (operator)
//	factory.org_myspace_limit_set    OrgMySpaceLimitParams    -> the totals                     PUT /v1/myspace/limit (operator)
//	factory.org_workstream_chats     OrgWorkstreamParams      -> {chats[], others[]}            GET  /v1/workstreams/{id}/chats
//	factory.org_workstream_chat_new  OrgWorkstreamChatNew     -> {id, title, state, ...}        POST /v1/workstreams/{id}/chats
//
// A person's chats are private to them, like Afi's pane, and always run on
// the server. factory.org_workstream_agent, _send and _stop take chat to
// read, talk in or stop one (factory_org_agent.go). The totals name people and
// numbers only: the operator is never told what anyone's space holds.
//
// An engine that serves them says "org_myspace" in hello.

// FeatureOrgMySpace is the hello word, exactly as the desk reads it.
const FeatureOrgMySpace = "org_myspace"

// OrgMySpaceLimitParams is the operator's monthly limit for every space.
type OrgMySpaceLimitParams struct {
	OrgConn
	LimitUSD float64 `json:"limit_usd"`
}

// OrgWorkstreamChatNew makes one of the caller's own chats in a workstream.
type OrgWorkstreamChatNew struct {
	OrgConn
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	// Agent is a named agent to talk to instead of Afi ("org_agents").
	Agent string `json:"agent,omitempty"`
}

// orgChatPattern is a chat id as the server makes it.
var orgChatPattern = regexp.MustCompile(`^ch_[0-9A-Za-z]{26}$`)

// orgChatID checks a chat id a caller sent; "" is Afi's conversation.
func orgChatID(chat string) (string, error) {
	chat = strings.TrimSpace(chat)
	if chat != "" && !orgChatPattern.MatchString(chat) {
		return "", Errorf(CodeBadParams, "chat is not a chat this organization gave")
	}
	return chat, nil
}

func withFactoryOrgMySpaceFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryOrg); ok {
		hello = withFeature(hello, FeatureOrgMySpace)
	}
	return hello
}

func init() {
	raw := func(name string, build func(OrgConn) (OrgRequest, error)) method {
		return orgMethod[OrgConn, json.RawMessage](name, build)
	}
	methods["factory.org_myspace"] = raw("factory.org_myspace", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodGet, Path: "/v1/myspace"}, nil
	})
	methods["factory.org_myspace_totals"] = raw("factory.org_myspace_totals", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodGet, Path: "/v1/myspace/totals"}, nil
	})
	methods["factory.org_myspace_limit_set"] = orgMethod[OrgMySpaceLimitParams, json.RawMessage]("factory.org_myspace_limit_set", func(p OrgMySpaceLimitParams) (OrgRequest, error) {
		if math.IsNaN(p.LimitUSD) || p.LimitUSD < 0 || p.LimitUSD > 10000 {
			return OrgRequest{}, Errorf(CodeBadParams, "limit_usd is 0 to 10000")
		}
		return OrgRequest{Method: http.MethodPut, Path: "/v1/myspace/limit", Body: map[string]float64{"limit_usd": p.LimitUSD}}, nil
	})
	methods["factory.org_workstream_chats"] = orgMethod[OrgWorkstreamParams, json.RawMessage]("factory.org_workstream_chats", func(p OrgWorkstreamParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		return OrgRequest{Method: http.MethodGet, Path: path + "/chats"}, err
	})
	methods["factory.org_workstream_chat_new"] = orgMethod[OrgWorkstreamChatNew, json.RawMessage]("factory.org_workstream_chat_new", func(p OrgWorkstreamChatNew) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		title := strings.Join(strings.Fields(p.Title), " ")
		if len(title) > 120 {
			return OrgRequest{}, Errorf(CodeBadParams, "title is at most 120 bytes")
		}
		body := map[string]string{}
		if title != "" {
			body["title"] = title
		}
		if agent := strings.TrimSpace(p.Agent); agent != "" {
			if _, err := orgSegment("agent", agent); err != nil {
				return OrgRequest{}, err
			}
			body["agent"] = agent
		}
		return OrgRequest{Method: http.MethodPost, Path: path + "/chats", Body: body}, nil
	})
}
