package plandb

// THE VERDICT BASIS IS HOW ONE VERDICT WAS EARNED, written down so that a
// reader can tell whether the checker read the work or ran the declared proof, and,
// when it ran, the recorded exit from every run, without reopening
// the trajectory. It is part of the task record ([Task.VerdictBasis]) and
// rides every surface reading that record: cli.go's cliTaskObject, the
// session fit record, and the tasks view.
type VerdictBasis struct {
	// Kind is "run" when the declared checks were what the verdict rested on,
	// and "reading" when the checker judged from what it could see. There are
	// exactly those two, and a reader needs nothing else: the pair answers
	// whether a verdict claims a re-run.
	Kind string `json:"kind"`
	// Runs is one entry per declared command that has a recorded run, with the
	// exit the check's own record shows. Only a run basis carries it; a
	// reading basis carries none.
	Runs []VerdictRun `json:"runs,omitempty"`

	// Unobserved names the declared checks a reading verdict did not observe
	// run, which happens for a record written before command exits were
	// recorded. It is set only on a reading basis, so a reader does not mistake
	// an old record's holds for one earned by running the declared checks.
	Unobserved []string `json:"unobserved,omitempty"`
}

// VerdictRun is one declared check's recorded run: the command, and the exit
// it actually produced in the checker's own trajectory.
type VerdictRun struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exitCode"`
}
