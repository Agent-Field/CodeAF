package factory

// GaveBackNote is the note every step reads after a person gives back a job
// they took over: the branch may hold their own commits now.
const GaveBackNote = "The person worked on this job's branch by hand while it was taken over. Read the branch again, including their commits, before you go on."

// CheckAgainFrom is where a run goes on after a person gives back a job they
// took over: the first step at or before cur that already passed and checks
// the work (a check, or a step that runs until clean or green), so the
// person's commits are tested and reviewed again with every step after it.
// It answers cur when no such step ran before it. phases are the run's, in
// order; their stages are found by name.
func CheckAgainFrom(stages []Stage, phases []Phase, cur int) int {
	for i := 0; i < cur && i < len(phases); i++ {
		if phases[i].State != PhaseDone {
			continue
		}
		if j := StageIndex(stages, phases[i].Name); j >= 0 && checksWork(stages[j]) {
			return i
		}
	}
	return cur
}

// checksWork says whether a stage is one whose pass a person's commits undo:
// a check, or a conversation that runs until clean or green.
func checksWork(s Stage) bool {
	return s.Kind == StageCheck || s.Until == UntilClean || s.Until == UntilGreen
}
