package api

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── GO-AHEADS ON THE WIRE ──────────────────────────────────────────────────
//
// GET /v1/policy?group=… reads one group's policy, and PUT /v1/policy replaces
// it whole ([PolicyPut]); both answer [Policy]. POST /v1/jobs/{id}/waits_on
// sets the jobs one job waits on ([WaitsPut]) and answers [WaitsSet]. GET
// /v1/features lists what this factory can do beyond its contract
// ([Features]). A factory older than these answers 404, which a client reads
// as "this factory does not have it".

// The words GET /v1/features answers.
const (
	// FeatureGroupPolicy: the factory keeps group policies and starts the jobs
	// they cover by itself.
	FeatureGroupPolicy = "group_policy"
	// FeatureWaitsOn: a job can wait on others, and a go-ahead starts it only
	// once they have merged.
	FeatureWaitsOn = "waits_on"
)

// Features is GET /v1/features's answer.
type Features struct {
	Features []string `json:"features"`
}

// PolicyPut is PUT /v1/policy's body: one group's whole policy.
type PolicyPut struct {
	Group      string              `json:"group"`
	Stopped    bool                `json:"stopped"`
	Allowances []factory.Allowance `json:"allowances"`
}

// PolicyAllowance is one allowance as it reads: what was set, what the jobs it
// started have spent, and its state ([factory.AllowanceState]).
type PolicyAllowance struct {
	factory.Allowance
	SpentUSD float64 `json:"spent_usd"`
	State    string  `json:"state" jsonschema:"enum=active,enum=ended,enum=spent"`
}

// Policy is one group's policy as it reads. A group the factory keeps nothing
// for reads not stopped, with no allowances, no version and no owner.
type Policy struct {
	Group      string            `json:"group"`
	Version    int               `json:"version,omitempty"`
	Stopped    bool              `json:"stopped"`
	Owner      string            `json:"owner,omitempty"`
	UpdatedAt  *time.Time        `json:"updated_at,omitempty"`
	SpentUSD   float64           `json:"spent_usd"`
	Allowances []PolicyAllowance `json:"allowances"`
}

// PolicyOf is p as it reads at now, with the spend of the jobs it started.
func PolicyOf(p factory.GroupPolicy, items []factory.Item, now time.Time) Policy {
	spend := factory.PolicySpend(items, p.Group)
	out := Policy{Group: p.Group, Version: p.Version, Stopped: p.Stopped, Owner: p.Owner, Allowances: []PolicyAllowance{}}
	if !p.UpdatedAt.IsZero() {
		at := p.UpdatedAt.UTC()
		out.UpdatedAt = &at
	}
	for _, a := range p.Allowances {
		spent := spend[a.ID].Spent
		a.Jobs = append([]int(nil), a.Jobs...)
		out.Allowances = append(out.Allowances, PolicyAllowance{Allowance: a, SpentUSD: spent, State: factory.AllowanceState(p, a, spent, now)})
		out.SpentUSD += spent
	}
	return out
}

// WaitsPut is POST /v1/jobs/{id}/waits_on's body: the whole list, [] for
// none, and the job as last read.
type WaitsPut struct {
	WaitsOn []int   `json:"waits_on"`
	Expect  *Expect `json:"expect,omitempty"`
}

// WaitsSet is its answer: the list kept and the job as it is now.
type WaitsSet struct {
	Job     int   `json:"job"`
	WaitsOn []int `json:"waits_on"`
	Current *Job  `json:"current,omitempty"`
}
