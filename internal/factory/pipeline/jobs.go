package pipeline

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"strings"
)

// DecideJob applies the v2 intake rules to explicit job facts. No AI estimate,
// inferred priority or rule judgment can grant permission to start a job.
func (f File) DecideJob(it factory.Item, paths []string) Decision {
	budget := it.Cap
	if budget <= 0 {
		budget = f.DefaultBudget
	}
	if budget <= 0 {
		budget = factory.DefaultJobBudget
	}
	j := Job{ID: it.ID, Kind: it.Kind, Tier: it.Tier, Labels: it.Labels, Size: factory.JobSize(it), Cap: budget, Cost: budget, CostKnown: true, Paths: paths}
	if !f.candidate(j) {
		return Decision{}
	}
	var reasons []string
	if size := factory.JobSize(it); size == "" {
		reasons = append(reasons, "no size label")
	} else if overSize(size, f.Start.MaxSize) {
		reasons = append(reasons, "over size "+normOr(f.Start.MaxSize, DefaultMaxSize)+" · this one is "+size)
	}
	if budget > f.Start.MaxCost {
		reasons = append(reasons, "job budget "+money(budget)+" is over "+money(f.Start.MaxCost))
	}
	if why := neverWhy(f.Start.Never, paths); why != "" {
		reasons = append(reasons, why)
	}
	for _, rule := range f.Start.AskFirst {
		words := strings.TrimSpace(rule)
		glob := ""
		for _, prefix := range []string{"touches ", "touch ", "path:"} {
			if rest, ok := strings.CutPrefix(strings.ToLower(words), prefix); ok {
				glob = strings.TrimSpace(rest)
				break
			}
		}
		if glob == "" {
			reasons = append(reasons, "ask first · "+rule)
			continue
		}
		for _, path := range paths {
			if touches(glob, path) {
				reasons = append(reasons, "ask first · "+rule+" · "+path)
				break
			}
		}
	}
	if len(reasons) > 0 {
		return Decision{Ask: true, Reasons: reasons}
	}
	return Decision{Start: true}
}
