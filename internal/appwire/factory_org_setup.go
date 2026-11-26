package appwire

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// ── STARTING AN ORGANIZATION, AND AFI SETTING IT UP (v2 designs J2, J3) ────
//
//	factory.org_profile          OrgConn                  -> OrgProfile      GET  /v1/org
//	factory.org_profile_set      OrgProfileSetParams      -> OrgProfile      PUT  /v1/org (operator)
//	factory.org_setup            OrgSetupParams           -> OrgSetupView    GET  /v1/org/setup?since= (operator)
//	factory.org_setup_send       OrgSetupSendParams       -> OrgSetupView    POST /v1/org/setup/messages (operator)
//	factory.org_setup_stop       OrgConn                  -> OrgSetupView    POST /v1/org/setup/stop (operator)
//	factory.org_setup_apply      OrgSetupApplyParams      -> OrgSetupApplied POST /v1/org/setup/apply (operator)
//	factory.org_setup_by_hand    OrgConn                  -> OrgProfile      POST /v1/org/setup/by-hand (operator)
//
// The set-up conversation's lines and state are relayed like a workstream
// agent's, as factory.org_workstream_agent with id "org-setup", to the
// operator alone. The card is the server's own JSON, passed through whole:
// the app edits it and sends it back to apply.

// FeatureOrgSetup is the hello word for the methods above.
const FeatureOrgSetup = "org_setup"

// OrgSetupSeat is the id the set-up conversation's events carry.
const OrgSetupSeat = "org-setup"

// OrgProfile is the organization's name, its line, and whether it is set up.
type OrgProfile struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SetUp       bool   `json:"set_up"`
	ByHand      bool   `json:"by_hand,omitempty"`
	// Relay is the address of the relay the organization runs for its members' computers, when it runs one.
	Relay string `json:"relay,omitempty"`
}

// OrgProfileSetParams is "Start your organization": a name and a line.
type OrgProfileSetParams struct {
	OrgConn
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// OrgSetupParams reads the conversation after since.
type OrgSetupParams struct {
	OrgConn
	Since int `json:"since,omitempty"`
}

// OrgSetupSendParams is one message to Afi about the set-up.
type OrgSetupSendParams struct {
	OrgConn
	Text string `json:"text"`
	TZ   string `json:"tz,omitempty"`
}

// OrgSetupApplyParams is the card as the person left it.
type OrgSetupApplyParams struct {
	OrgConn
	Card json.RawMessage `json:"card"`
}

// OrgSetupMadePart is one part a press made.
type OrgSetupMadePart struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	ID     string `json:"id"`
	Parent string `json:"parent,omitempty"`
	URL    string `json:"url,omitempty"`
}

// OrgSetupView is the conversation, the card and what was made.
type OrgSetupView struct {
	Agent OrgWorkstreamAgent `json:"agent"`
	Card  json.RawMessage    `json:"card,omitempty"`
	Made  []OrgSetupMadePart `json:"made"`
	SetUp bool               `json:"set_up"`
}

func (v *OrgSetupView) fill() {
	v.Agent.fill()
	if v.Made == nil {
		v.Made = []OrgSetupMadePart{}
	}
}

// OrgSetupApplied is what one press made, the part that failed, and Done.
type OrgSetupApplied struct {
	Made   []OrgSetupMadePart `json:"made"`
	Failed string             `json:"failed,omitempty"`
	Done   bool               `json:"done"`
}

func init() {
	view := func(v *OrgSetupView) { v.fill() }
	methods["factory.org_profile"] = orgMethod[OrgConn, OrgProfile]("factory.org_profile", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodGet, Path: "/v1/org"}, nil
	})
	methods["factory.org_profile_set"] = orgMethod[OrgProfileSetParams, OrgProfile]("factory.org_profile_set", func(p OrgProfileSetParams) (OrgRequest, error) {
		if p.Name == nil && p.Description == nil {
			return OrgRequest{}, Errorf(CodeBadParams, "name or description is required")
		}
		return OrgRequest{Method: http.MethodPut, Path: "/v1/org", Body: map[string]*string{"name": p.Name, "description": p.Description}}, nil
	})
	methods["factory.org_setup"] = filled(orgMethod[OrgSetupParams, OrgSetupView]("factory.org_setup", func(p OrgSetupParams) (OrgRequest, error) {
		if p.Since < 0 {
			return OrgRequest{}, Errorf(CodeBadParams, "since is the last seq you have")
		}
		path := "/v1/org/setup"
		if p.Since > 0 {
			path += "?since=" + strconv.Itoa(p.Since)
		}
		return OrgRequest{Method: http.MethodGet, Path: path}, nil
	}), view)
	methods["factory.org_setup_send"] = filled(orgMethod[OrgSetupSendParams, OrgSetupView]("factory.org_setup_send", func(p OrgSetupSendParams) (OrgRequest, error) {
		text := strings.TrimSpace(p.Text)
		if text == "" || len(text) > orgAgentMessageMost {
			return OrgRequest{}, Errorf(CodeBadParams, "text is 1 to 16384 bytes of words")
		}
		return OrgRequest{Method: http.MethodPost, Path: "/v1/org/setup/messages", Body: orgAgentMessageBody(text, p.TZ, false)}, nil
	}), view)
	methods["factory.org_setup_stop"] = filled(orgMethod[OrgConn, OrgSetupView]("factory.org_setup_stop", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodPost, Path: "/v1/org/setup/stop"}, nil
	}), view)
	methods["factory.org_setup_apply"] = filled(orgMethod[OrgSetupApplyParams, OrgSetupApplied]("factory.org_setup_apply", func(p OrgSetupApplyParams) (OrgRequest, error) {
		if len(p.Card) == 0 || string(p.Card) == "null" {
			return OrgRequest{}, Errorf(CodeBadParams, "card is required")
		}
		return OrgRequest{Method: http.MethodPost, Path: "/v1/org/setup/apply", Body: map[string]json.RawMessage{"card": p.Card}}, nil
	}), func(a *OrgSetupApplied) {
		if a.Made == nil {
			a.Made = []OrgSetupMadePart{}
		}
	})
	methods["factory.org_setup_by_hand"] = orgMethod[OrgConn, OrgProfile]("factory.org_setup_by_hand", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodPost, Path: "/v1/org/setup/by-hand"}, nil
	})
}
