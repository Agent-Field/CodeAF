package factory

// StepCount is how far a job is through its steps: how many are done of how
// many it will run. IT IS THE ONE COUNT. The job's Brief, the queue's row, the
// job page's Steps head and every other place that says "N of M" read it
// (api's steps_done and steps_total), so "5 of 8" on one page and "3 of 6" on
// another cannot happen again: on the 2026-11-06 live run the Brief counted the
// run's phases, the list counted the run's position and the job page counted
// its own rows, and all three disagreed about one job.
//
// The steps are the run's, in its order, then the job's switched-on steps the
// run has not reached yet (a step added after the run began). A SKIPPED STEP
// COUNTS FOR NEITHER SIDE: a phase the run skipped and a step a person switched
// off are not work the job will do. Done is a phase the run finished. A job
// that has not run counts its switched-on steps, none of them done.
func StepCount(it Item) (done, total int) {
	// A step nested inside another (an AgentField reasoner's call into a
	// child, [Stage.Parent]) folds into its parent on the rail, so it is
	// counted as part of that step rather than as one of its own.
	nested := map[string]bool{}
	for _, s := range it.Stages {
		if s.Parent != "" {
			nested[s.Name] = true
		}
	}
	inRun := map[string]bool{}
	if it.Stream != nil {
		for _, p := range it.Stream.Phases {
			inRun[p.Name] = true
			if p.State == PhaseSkipped || nested[p.Name] {
				continue
			}
			total++
			if p.State == PhaseDone {
				done++
			}
		}
	}
	stages := it.Stages
	if RunOnPending(it) {
		stages = nil
	}
	for _, s := range stages {
		if s.On && !inRun[s.Name] && !nested[s.Name] {
			total++
		}
	}
	return done, total
}
