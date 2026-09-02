package tui3

import (
	tea "charm.land/bubbletea/v2"
)

// THE JOBS SECTION ON THE COLUMN.
//
// THE LAW THIS FILE EXISTS FOR: a background job is not a task, and the
// column that used to file one among the families now keeps a section of its
// own for it. The reading is pure (jobsview.go). This file is the window's
// half — it reads [app.jobs], holds the toggle, routes a key and a click onto
// the label or a row, and asks the reading for the lines.
//
// IT DRAWS NOTHING ITSELF. A string a person sees is formatted from the
// notice's fields at the width the column has, in jobsview.go, so a test can
// pin the four collapsed shapes with no window and no terminal.

// jobSection is this conversation's jobs as the column draws them, or nil
// when there are none — which is the emptiness law, not an empty heading.
func (a *app) jobSection(width, room int) []railLine {
	return jobSectionRows(a.jobsLive(), a.jobsSettled(), a.jobsOpen, a.now(), width, room, a.pal)
}

// jobSectionMin is how many rows the jobs section will spend before it has
// drawn a single finished job: the blank and the label, and — when it is
// open — every running job. Standing gives this up first ([app.marginRows]),
// because live work outranks the furniture around it, which is the same
// trade the roster's own live head makes.
func (a *app) jobSectionMin() int {
	if len(a.jobs) == 0 {
		return 0
	}
	if !a.jobsOpen {
		return marginJobsCost
	}
	return marginJobsCost + a.jobsRunning()
}

// toggleJobs opens the section or shuts it. Collapsed is the default
// ([app.jobsOpen] is false until somebody asks), and history is a count on
// the label until they do.
func (a *app) toggleJobs() {
	a.jobsOpen = !a.jobsOpen
	// THE CURSOR NEVER STAYS ON A ROW THE FOLD TOOK AWAY, which is the
	// roster's own law ([app.railIn]) said about this section: shutting it
	// leaves the keyboard on the label, the one line that remains.
	if !a.jobsOpen && a.railWhere.job != 0 {
		a.railWhere = railSpot{jobs: true}
	}
	a.touch()
}

// jobEnter is enter on a jobs-section line: the label toggles, a job row
// opens that job's page. It reports whether the key was this section's, so
// the roster's own enter can stand down.
func (a *app) jobEnter() (tea.Cmd, bool) {
	switch {
	case a.railWhere.jobs:
		a.toggleJobs()
		return nil, true
	case a.railWhere.job != 0:
		if a.showJobPage(a.railWhere.job) {
			a.touch()
		}
		return nil, true
	}
	return nil, false
}

// jobsAnimating reports whether a running job's clock still needs the paint
// beat. The column counts up from [session.JobNotice.Started] on that beat
// and does not re-ask the engine; a still picture would freeze the figure at
// whatever the last event drew, which is the same reason the roster keeps
// this clock alive for a node ([app.tasksAnimating]).
func (a *app) jobsAnimating() bool {
	if a.jobsRunning() == 0 {
		return false
	}
	return a.railStanding() || a.jobPageOpen()
}

// jobSpots is the jobs section as a walk of focusable lines: the label, then
// every job row the column actually drew. The remainder line is a count and
// not a door, so it is not in this list.
func (a *app) jobSpots() []railSpot {
	// Asked from the keyboard walk, never from layout: [app.railView] is the
	// one picture of which job rows actually drew, and a walk that included a
	// job behind `▸ N earlier` would land the cursor on a row that is not there.
	view, _ := a.railView(a.viewHeight())
	out := make([]railSpot, 0, 1+len(a.jobs))
	for _, line := range view {
		switch {
		case line.jobs:
			out = append(out, railSpot{jobs: true})
		case line.job != 0:
			out = append(out, railSpot{job: line.job})
		}
	}
	return out
}
