package api

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// Policy is the saved group choice with current lifetime spending.
type Policy struct {
	Group      string            `json:"group"`
	Version    int               `json:"version"`
	Stopped    bool              `json:"stopped"`
	Owner      string            `json:"owner,omitempty"`
	UpdatedAt  *time.Time        `json:"updated_at,omitempty"`
	SpentUSD   float64           `json:"spent_usd"`
	Allowances []PolicyAllowance `json:"allowances"`
}

type PolicyAllowance struct {
	factory.Allowance
	SpentUSD float64 `json:"spent_usd"`
	State    string  `json:"state" enum:"active,ended,spent"`
}

// PolicyFrom gives an ended allowance precedence over a spent one.
func PolicyFrom(p factory.GroupPolicy, jobs []factory.Item, now time.Time) Policy {
	spent, per := factory.PolicySpend(p, jobs)
	out := Policy{Group: p.Group, Version: p.Version, Stopped: p.Stopped, Owner: p.Owner, UpdatedAt: p.UpdatedAt, SpentUSD: spent, Allowances: make([]PolicyAllowance, 0, len(p.Allowances))}
	for _, a := range p.Allowances {
		state := "active"
		end, _ := time.Parse(time.RFC3339, a.EndsAt)
		if !now.Before(end) {
			state = "ended"
		} else if per[a.ID] >= a.CapUSD {
			state = "spent"
		}
		a.Jobs = append([]int(nil), a.Jobs...)
		out.Allowances = append(out.Allowances, PolicyAllowance{Allowance: a, SpentUSD: per[a.ID], State: state})
	}
	return out
}

func policyFromItem(j *Job, it factory.Item) {
	j.Group = it.Group
	if b := it.Blocked; b != nil && b.By == factory.BlockPolicy {
		j.Blocked = &JobBlock{By: b.By, Reason: b.Reason, Since: b.Since, Job: b.Job}
	}
}

func init() {
	Lost["Item.PolicyRun"] = "service-owned allowance admission; a worker or client snapshot cannot start a policy run"
}
