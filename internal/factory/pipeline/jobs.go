package pipeline

import (
	"github.com/Agent-Field/codeaf/internal/factory"
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
	size := factory.JobSize(it)
	// AN ISSUE WITH NO SIZE LABEL IS M while the file says nothing of how
	// sizes are read: the default limit is M, so labelling nothing holds
	// nothing. A file that names a size mode asks for its labels, and a pull
	// request is measured, never guessed.
	if size == "" && it.Kind != factory.KindPR && !f.set.issue {
		size = "M"
	}
	if size == "" {
		reasons = append(reasons, "no size label")
	} else if overSize(size, f.Start.MaxSize) {
		reasons = append(reasons, "over size "+normOr(f.Start.MaxSize, DefaultMaxSize)+" · this one is "+size)
	}
	if budget > f.Start.MaxCost {
		reasons = append(reasons, "job budget "+money(budget)+" is over "+money(f.Start.MaxCost))
	}
	if why := neverWhy(underDirs(f.Start.Never), paths); why != "" {
		reasons = append(reasons, why)
	}
	for _, rule := range f.Start.AskFirst {
		glob := askFirstPath(rule)
		if glob == "" {
			reasons = append(reasons, "ask first · "+rule)
			continue
		}
		for _, path := range paths {
			if touches(glob, path) || touches(underDir(glob), path) {
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

// DryItems is the settings sentence for the backlog, decided by [File.DecideJob]
// itself: the same explicit labels and budget ceiling real intake applies, never
// the triage guess. pathsOf supplies the paths intake would hand DecideJob, so a
// preview and a start read the same facts. It reads and writes nothing.
func (f File) DryItems(items []factory.Item, pathsOf func(factory.Item) []string) DryRun {
	d := DryRun{WouldStart: []int{}, WouldAsk: []int{}, WouldWait: []Wait{}, Why: map[int][]string{}}
	for _, it := range items {
		var paths []string
		if pathsOf != nil {
			paths = pathsOf(it)
		}
		switch dec := f.DecideJob(it, paths); {
		case dec.Start:
			d.WouldStart = append(d.WouldStart, it.ID)
		case dec.Ask:
			d.WouldAsk = append(d.WouldAsk, it.ID)
			d.Why[it.ID] = append([]string(nil), dec.Reasons...)
		default:
			d.WouldWait = append(d.WouldWait, Wait{ID: it.ID, Reason: waitReason(f.Start.Labels)})
		}
	}
	return d
}
