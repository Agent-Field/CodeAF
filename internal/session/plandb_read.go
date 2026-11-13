package session

import (
	"bytes"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// planTaskRead shares handles and trajectory projections within one listing.
// Its caller owns the plan gate and closes the handles after both projections.
type planTaskRead struct {
	stores       []*plandb.Store
	plan         *planState
	trajectories map[string]*planTrajectoryRead
}

// trajectory reads each path once. App run rows are projected before plan rows,
// so their step facts are requested on the first read of any shared path.
func (r *planTaskRead) trajectory(path string, withAppSteps bool) *planTrajectoryRead {
	if got, ok := r.trajectories[path]; ok {
		return got
	}
	data, _ := readPlanTrajectoryFile(path)
	got := &planTrajectoryRead{steps: len(parsePlanTrajectory(data))}
	if withAppSteps {
		got.app, got.reads = parseAppTrajectorySteps(bytes.NewReader(data), appTaskStepsMax)
	}
	if r.trajectories == nil {
		r.trajectories = make(map[string]*planTrajectoryRead)
	}
	r.trajectories[path] = got
	return got
}

// Only counts and requested app facts survive a read; raw files and complete
// plan steps become collectible before the next task. Both parsers retain their
// distinct line limits and tolerated record formats.
type planTrajectoryRead struct {
	steps int
	app   []AppTaskStep
	reads map[string]int
}
