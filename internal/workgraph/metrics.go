package workgraph

import "time"

// The merge outcomes that mean the work came home.
func landed(l *Landing) bool {
	return l != nil && (l.Outcome == "merged" || l.Outcome == "inplace")
}

func terminal(state string) bool {
	return state == StateDone || state == StateFailed || state == StateUnverified
}

// tally accumulates one figure.
type tally struct {
	value   float64
	counted int
	of      int
}

func (t *tally) add(of bool, counted bool, v float64) {
	if of {
		t.of++
	}
	if counted {
		t.counted++
		t.value += v
	}
}

// figure turns a tally into a [GraphFigure]. KNOWN FOLLOWS SOURCE COVERAGE, NOT
// MERELY WHETHER AN OBSERVATION EXISTS: a figure over an empty population is a
// known zero only when the scope was read whole, and a population whose members
// all lack the fact (of > 0, counted == 0) is unknown even when the scope was
// read whole, because no source established the number.
func (t tally) figure(complete bool) GraphFigure {
	known := t.counted > 0 || (t.of == 0 && complete)
	return GraphFigure{Value: t.value, Known: known, Partial: t.counted < t.of || !complete, Counted: t.counted, Of: t.of}
}

// exact is a figure that is a plain count of what the scope holds: known even
// at zero when the scope was read whole, partial when it was not.
func exact(n int, complete bool) GraphFigure {
	return GraphFigure{Value: float64(n), Known: n > 0 || complete, Partial: !complete, Counted: n, Of: n}
}

// measure computes the scope's figures in one pass over the kept tasks.
func (b *builder) measure() error {
	s := b.snap
	complete := len(b.in.Unread) == 0 && !b.in.Truncated
	var landedTasks, landedFiles, checks, spend tally
	var subtasks, tasks int
	var filesCut bool
	files := map[string]bool{}
	var from, to time.Time
	for i := range s.nodes {
		if err := b.tick(i); err != nil {
			return err
		}
		n := &s.nodes[i]
		// A quick lookup is not one of the tasks the app lists; it is a node only so
		// that a link to it resolves, so it is not counted as work.
		if n.Kind != KindQuick {
			tasks++
			if n.Parent != "" {
				subtasks++
			}
		}
		if n.Kind != KindQuick {
			measureLanding(n, &landedTasks, &landedFiles, files, &filesCut)
			measureChecks(n, &checks)
		}
		measureSpend(n, &spend, &s.metrics.Unpriced)
		from, to = span(from, to, n.Clock)
	}
	s.metrics.Tasks = exact(tasks, complete)
	s.metrics.Subtasks = exact(subtasks, complete)
	s.metrics.LandedTasks = landedTasks.figure(complete)
	landedFiles.value = float64(len(files))
	s.metrics.LandedFiles = landedFiles.figure(complete)
	// A path list the producer cut at its own bound still gives the distinct
	// paths it has, and the figure says it is not the whole of them.
	s.metrics.LandedFiles.Partial = s.metrics.LandedFiles.Partial || filesCut
	s.metrics.ChecksPassed = checks.figure(complete)
	s.metrics.Spend = spend.figure(complete)
	// A BILL IS NEVER A KNOWN ZERO FOR WANT OF BILLS: with no task carrying a
	// priced bill there is nothing that establishes the amount, even when the
	// scope was read whole, so the figure stays unknown rather than $0.00.
	s.metrics.Spend.Known = spend.counted > 0
	s.metrics.From, s.metrics.To = stamp(from), stamp(to)
	if len(b.in.Members) > 0 {
		m := exact(distinctMembers(b.in.Members), complete)
		s.metrics.Members = &m
	}
	return nil
}

// measureLanding: of the done tasks, the ones that carry a landing outcome are
// counted, and of those the ones that landed add to the value; the landed
// tasks' paths are collected for the distinct count.
func measureLanding(n *GraphNode, tasks, filesTally *tally, files map[string]bool, cut *bool) {
	if n.State == StateDone {
		hasOutcome := n.Landing != nil
		v := 0.0
		if landed(n.Landing) {
			v = 1
		}
		tasks.add(true, hasOutcome, v)
	}
	if !landed(n.Landing) || n.State != StateDone {
		return
	}
	filesTally.add(true, n.Files.Total > 0, 0)
	if n.Files.Total > len(n.Files.Paths) {
		*cut = true
	}
	for _, p := range n.Files.Paths {
		files[p] = true
	}
}

func measureChecks(n *GraphNode, checks *tally) {
	if !terminal(n.State) {
		return
	}
	passed := 0.0
	if n.Checks != nil {
		passed = float64(n.Checks.Passed)
	}
	checks.add(true, n.Checks != nil, passed)
}

func measureSpend(n *GraphNode, spend *tally, unpriced *int) {
	priced := n.Spend != nil && n.Spend.Basis == "priced"
	usd := 0.0
	if priced {
		usd = n.Spend.USD
	}
	spend.add(true, priced, usd)
	if n.Spend != nil && n.Spend.Basis == "unpriced" {
		*unpriced++
	}
}

// span widens the recorded time bounds by one task's clock.
func span(from, to time.Time, c Clock) (time.Time, time.Time) {
	for _, t := range []*time.Time{c.AdmittedAt, c.StartedAt} {
		if t != nil && (from.IsZero() || t.Before(from)) {
			from = *t
		}
	}
	if c.EndedAt != nil && c.EndedAt.After(to) {
		to = *c.EndedAt
	}
	return from, to
}

func distinctMembers(in []MemberInput) int {
	seen := map[string]bool{}
	for _, m := range in {
		seen[m.Agent] = true
	}
	return len(seen)
}
