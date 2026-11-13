package appwire

import (
	"net/http"
	"strings"
)

// ── WHERE AN ORGANIZATION WORKSTREAM'S AGENT RUNS ──────────────────────────
//
// The operator chooses, for the whole organization, whether a workstream's
// agent runs on the laptop of the person driving it ("driver") or on the
// organization's server ("server"); everyone reads the choice. While it is
// "driver", one person at a time holds a workstream's driver hand, and only
// that person's engine runs its agent, with their model key; what it spends is
// posted to the workstream on the server, in their name.
//
//	factory.org_agent_runner          OrgConn                      -> OrgAgentRunner      GET   /v1/settings
//	factory.org_agent_runner_set      OrgAgentRunnerSetParams      -> OrgAgentRunner      PATCH /v1/settings (operator)
//	factory.org_workstream_drive      OrgWorkstreamParams          -> OrgWorkstreamDriver PUT   /v1/workstreams/{id}/driver
//	factory.org_workstream_let_go     OrgWorkstreamParams          -> {}                  DELETE /v1/workstreams/{id}/driver
//	factory.org_workstream_spend      OrgWorkstreamSpendParams     -> OrgWorkstreamSpend  POST  /v1/workstreams/{id}/spend
//
// A drive refused because someone else drives answers conflict with detail
// {"reason":"driving","by":"@handle"}. A server older than the hand answers
// not_found, and the desk leaves the agent out.

// FeatureOrgAgentRunner is the hello word for the methods above.
const FeatureOrgAgentRunner = "org_agent_runner"

// The choices, exactly as the server spells them.
const (
	OrgAgentRunnerDriver = "driver"
	OrgAgentRunnerServer = "server"
)

// OrgAgentRunner is the organization's choice and the choices its server
// offers. A server older than the choice answers neither, and reads as
// driver of [driver]: its workstreams have no agent anywhere else.
type OrgAgentRunner struct {
	AgentRunner  string   `json:"agent_runner"`
	AgentRunners []string `json:"agent_runners"`
}

func (o *OrgAgentRunner) fill() {
	if o.AgentRunner == "" {
		o.AgentRunner = OrgAgentRunnerDriver
	}
	if len(o.AgentRunners) == 0 {
		o.AgentRunners = []string{OrgAgentRunnerDriver}
	}
}

// OrgAgentRunnerSetParams is the operator's choice.
type OrgAgentRunnerSetParams struct {
	OrgConn
	AgentRunner string `json:"agent_runner"`
}

// OrgWorkstreamDriver is who holds a workstream's driver hand, and until when
// unless they renew it (TTL seconds after each renewal).
type OrgWorkstreamDriver struct {
	Handle string `json:"handle"`
	Since  string `json:"since"`
	Until  string `json:"until"`
	TTL    int    `json:"ttl_s"`
}

// OrgWorkstreamSpendParams is spend the driver's agent made, posted once:
// Key is sent as Idempotency-Key so a retry never counts it twice. Job is the
// factory job it was for, Chat the driver's conversation.
type OrgWorkstreamSpendParams struct {
	OrgConn
	ID   string  `json:"id"`
	USD  float64 `json:"usd"`
	Job  *int    `json:"job,omitempty"`
	Chat string  `json:"chat,omitempty"`
	Key  string  `json:"key"`
}

// OrgWorkstreamSpend is the workstream's spend after the post, and its budget
// (null when it has none).
type OrgWorkstreamSpend struct {
	SpendUSD  float64  `json:"spend_usd"`
	BudgetUSD *float64 `json:"budget_usd"`
}

func init() {
	runner := func(o *OrgAgentRunner) { o.fill() }
	methods["factory.org_agent_runner"] = filled(orgMethod[OrgConn, OrgAgentRunner]("factory.org_agent_runner", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodGet, Path: "/v1/settings"}, nil
	}), runner)
	methods["factory.org_agent_runner_set"] = filled(orgMethod[OrgAgentRunnerSetParams, OrgAgentRunner]("factory.org_agent_runner_set", func(p OrgAgentRunnerSetParams) (OrgRequest, error) {
		choice := strings.TrimSpace(p.AgentRunner)
		if choice != OrgAgentRunnerDriver && choice != OrgAgentRunnerServer {
			return OrgRequest{}, Errorf(CodeBadParams, `agent_runner is "driver" or "server"`)
		}
		return OrgRequest{Method: http.MethodPatch, Path: "/v1/settings", Body: map[string]string{"agent_runner": choice}}, nil
	}), runner)
	methods["factory.org_workstream_drive"] = orgMethod[OrgWorkstreamParams, OrgWorkstreamDriver]("factory.org_workstream_drive", func(p OrgWorkstreamParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		return OrgRequest{Method: http.MethodPut, Path: path + "/driver"}, err
	})
	methods["factory.org_workstream_let_go"] = orgMethod[OrgWorkstreamParams, Empty]("factory.org_workstream_let_go", func(p OrgWorkstreamParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		return OrgRequest{Method: http.MethodDelete, Path: path + "/driver"}, err
	})
	methods["factory.org_workstream_spend"] = orgMethod[OrgWorkstreamSpendParams, OrgWorkstreamSpend]("factory.org_workstream_spend", func(p OrgWorkstreamSpendParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		if !(p.USD > 0) || p.USD > 10000 {
			return OrgRequest{}, Errorf(CodeBadParams, "usd is what was spent: more than 0 and at most 10000 dollars")
		}
		key := strings.TrimSpace(p.Key)
		if !plainText(key, 128) || key == "" {
			return OrgRequest{}, Errorf(CodeBadParams, "key is required: one id per spend, so a retry is counted once")
		}
		chat := strings.TrimSpace(p.Chat)
		if chat != "" && !plainText(chat, 128) {
			return OrgRequest{}, Errorf(CodeBadParams, "chat is not a conversation id")
		}
		body := struct {
			USD  float64 `json:"usd"`
			Job  *int    `json:"job,omitempty"`
			Chat string  `json:"chat,omitempty"`
		}{p.USD, p.Job, chat}
		return OrgRequest{Method: http.MethodPost, Path: path + "/spend", Body: body, IdempotencyKey: key}, nil
	})
}
