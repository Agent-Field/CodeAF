package factory

// Change is the last change the holding machine could read from the job's
// worktree. Nil means no machine has reported it yet.
type Change struct {
	Files  []ChangedFile `json:"files,omitempty"`
	Stat   *ChangeStat   `json:"stat,omitempty"`
	Branch string        `json:"branch,omitempty"`
	Tests  []StepTests   `json:"tests,omitempty"`
}

// ChangedFile names a path in the worktree, including untracked additions.
// Binary files have no line counts; renames are an old path removed and a new
// path added, so every path is represented without guessing rename identity.
type ChangedFile struct {
	Path    string `json:"path"`
	Added   int    `json:"added,omitempty"`
	Removed int    `json:"removed,omitempty"`
	Status  string `json:"status"`
}

// ChangeStat counts the whole change rather than a truncated path list.
type ChangeStat struct {
	Files   int `json:"files"`
	Added   int `json:"added,omitempty"`
	Removed int `json:"removed,omitempty"`
}

// TestCounts exists only when a check's output names individual outcomes.
// An exit code alone never establishes how many tests ran.
type TestCounts struct {
	Passed  int `json:"passed,omitempty"`
	Failed  int `json:"failed,omitempty"`
	Skipped int `json:"skipped,omitempty"`
}

// StepTests keeps each check's latest counts separate from other checks.
type StepTests struct {
	Step string `json:"step"`
	TestCounts
}
