package appwire

import (
	"net/http"
	"strconv"
)

// ── AFI'S TASK CARDS IN A PERSON'S CONVERSATION ON THE SERVER ──────────────
//
// The server's agent proposes tasks as a "cards" line of the person's own
// conversation (Workstreams v2, screen 8). Nothing is made until the person
// adds them: the ticked cards, as they changed them, become tasks as that
// person, and a note line (Took, Tasks) says so. A cards line is added once.
//
//	factory.org_workstream_agent_cards_add  OrgAgentCardsAddParams -> OrgWorkstreamAgent  POST /v1/workstreams/{id}/agent/cards/{seq}

// FeatureOrgAgentCards is the hello word for the method above.
const FeatureOrgAgentCards = "org_agent_cards"

// OrgAgentCardsMost is the most cards one proposal holds, and one add makes.
const OrgAgentCardsMost = 8

// OrgAgentCards is one proposal: a plan's group name, and its cards.
type OrgAgentCards struct {
	Group string         `json:"group,omitempty"`
	Tasks []OrgAgentCard `json:"tasks"`
}

// OrgAgentCard is one proposed task. DoneBy is "afi" or "person" (Person is
// their handle; "" is the person adding it); Owner is a handle ("" is the
// person adding it); WaitsOn are the 1-based numbers of earlier cards.
type OrgAgentCard struct {
	Title   string `json:"title"`
	Request string `json:"request"`
	DoneBy  string `json:"done_by"`
	Person  string `json:"person,omitempty"`
	Owner   string `json:"owner,omitempty"`
	Due     string `json:"due,omitempty"`
	WaitsOn []int  `json:"waits_on,omitempty"`
	// Checklist is the deliverable's steps, one line each.
	Checklist []string `json:"checklist,omitempty"`
}

// OrgAgentCardsAddParams adds the ticked cards of the cards line Seq, as the
// person changed them, in order (WaitsOn counts within these).
type OrgAgentCardsAddParams struct {
	OrgConn
	ID    string         `json:"id"`
	Seq   int            `json:"seq"`
	Group string         `json:"group,omitempty"`
	Tasks []OrgAgentCard `json:"tasks"`
}

func init() {
	methods["factory.org_workstream_agent_cards_add"] = filled(orgMethod[OrgAgentCardsAddParams, OrgWorkstreamAgent]("factory.org_workstream_agent_cards_add", func(p OrgAgentCardsAddParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		if p.Seq < 1 {
			return OrgRequest{}, Errorf(CodeBadParams, "seq is the cards line's seq")
		}
		if len(p.Tasks) == 0 || len(p.Tasks) > OrgAgentCardsMost {
			return OrgRequest{}, Errorf(CodeBadParams, "add 1 to %d tasks", OrgAgentCardsMost)
		}
		return OrgRequest{Method: http.MethodPost, Path: path + "/agent/cards/" + strconv.Itoa(p.Seq),
			Body: map[string]any{"group": p.Group, "tasks": p.Tasks}}, nil
	}), func(a *OrgWorkstreamAgent) { a.fill() })
}
