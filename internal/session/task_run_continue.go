package session

// CARRYING ON A RUN NOTHING IS DRIVING.
//
// A run whose process went away is [TaskInterrupted]: nothing is driving it and
// every step it took is in its store. This is the door that picks one back up.
//
// IT MAKES NOTHING. It adopts the copy the run was written down as working in
// ([runCopyTree]) and the store that is already on disk, and it hands both to
// the supervisor the run was using before. The supervisor releases claims whose
// owner is gone and re-reads the ready set on its own next pass, so there is no
// resume machinery here and no second scheduler: carrying on is the ordinary
// pass, run by a process that was not there for the last one.
//
// IT HAS NO CALLER YET, ON PURPOSE. Continuing seats workers and spends money,
// and a door that spends money reachable before anything means to reach it is
// not a smaller version of the feature — it is a worse thing than no door. The
// card that calls it is the next change.

import (
	"context"
	"fmt"
	"strconv"
)

// ContinueRun picks up a run that nothing is driving, by the number its row
// wears — the same number every other surface and the stop road already know it
// by (stoprun.go).
//
// IT REFUSES RATHER THAN REPAIRS, every time, and each refusal names the thing
// that is in the way in the person's own words:
//
//   - a run already going, because a conversation drives one at a time and the
//     honest answer to "carry this on" is that it never stopped;
//   - a row that is not a run, or no such row at all;
//   - a run that settled, which has nothing left to carry on;
//   - a run whose working copy was never written down or is gone from disk
//     ([runCopyTree] holds both sentences).
//
// THE ANSWER IS THE PERSON'S OWN SENTENCE and the error is for the caller. A
// door that spends money says what it did in the words the person will read
// about it afterwards.
func (a *Agent) ContinueRun(ctx context.Context, row uint64) (string, error) {
	engine := chatRunEngine
	g := a.graph()
	if engine == nil || g == nil || g.planPath() == "" {
		return "", errRunRoadUnavailable
	}

	// A RUN ALREADY GOING IS NOT CARRIED ON. Read under the start lock, held
	// until the carried-on run is registered, because a run installed between
	// this look and the work below would be a second run on one conversation's
	// store ([Agent.lockBeltStart]).
	a.lockBeltStart()
	defer a.beltStartMu.Unlock()
	a.beltMu.Lock()
	live := a.beltRun
	a.beltMu.Unlock()
	if live != nil {
		if live.row == row {
			return "", fmt.Errorf("%s is already running", taskStopName(row, live.title))
		}
		return "", fmt.Errorf("%s is running, and a conversation drives one run at a time", taskStopName(row, live.title))
	}

	kept, found := runRowOf(g, row)
	if !found {
		return "", fmt.Errorf("there is no run %d in this conversation", row)
	}
	if kept.State != TaskInterrupted {
		// A run that finished, failed or was stopped has said its last word.
		// Only work nothing is driving is waiting to be picked up.
		return "", fmt.Errorf("%s is %s, so there is nothing to carry on", taskStopName(row, kept.Title), kept.State)
	}

	// THE COPY IS ADOPTED AND NEVER MADE. This is the line the whole of the
	// record exists for: the road that MAKES a copy clears the directory it is
	// handed, so reaching for it here would delete the work this door is meant
	// to resume (task_run_copy.go says it at length).
	tree, err := runCopyTree(kept.Copy, a.config.Place)
	if err != nil {
		return "", err
	}

	// AND THE STORE THAT IS ALREADY THERE, AND ONLY IF IT IS THIS RUN'S. The
	// store road sets aside whatever it finds for a NEW request
	// ([Agent.openBeltRunStore]); asked to carry on, it adopts the store only
	// when that store's own root is this run and has not ended. A later request
	// that set this run aside as interrupted left a different run at the path,
	// and carrying THAT one on under this row's name is the adoption this door
	// must never make.
	plan, store, err := a.openBeltRunStore(g, g.planPath(), strconv.FormatUint(row, 10), kept.Title, "", true)
	if err != nil {
		return "", err
	}

	runCtx, cut := context.WithCancel(context.WithoutCancel(ctx))
	run := &beltRun{
		plan: plan, store: store, root: store.RootID(), row: row, title: kept.Title,
		workspace: tree.dir, ground: tree.ground, tree: tree, cut: cut,
		born: a.taskClockNow(), over: make(chan struct{}),
	}
	a.installBeltRun(g, run)
	// THE ROW GOES BACK TO RUNNING AND KEEPS THE COPY IT NAMED. Publishing
	// without it would make the run one nobody can carry on a second time, which
	// is the defect this door exists on the other side of.
	a.publishRunRow(g, TaskNotice{
		ID: row, Title: kept.Title, State: TaskRunning,
		StartedAt: kept.StartedAt, Parent: kept.Parent, Copy: kept.Copy,
	})
	// AND THE PROJECT'S RECORD SAYS IT IS RUNNING AGAIN, over the interrupted
	// row its last life left ([Agent.recordBeltRunStart]).
	a.recordBeltRunStart(run)

	// THE SAME SPEC THE RUN WOULD HAVE HAD ([Agent.beltRunSpec]). A continued
	// run is the same run, so it works under the same seats and the same bounds;
	// a spec built here would drift from that one the day somebody changed a cap.
	go a.driveBeltRun(runCtx, engine, run, a.beltRunSpec(run, ""))
	return "carrying on " + taskStopName(row, kept.Title) + " from where it stopped", nil
}

// runRowOf answers the kept row for one run, and whether there is one. It reads
// the rows the graph holds rather than the live registry, because the whole
// case this door serves is a run whose process is gone.
func runRowOf(g *TaskGraph, row uint64) (TaskNotice, bool) {
	for _, kept := range g.runRows(row) {
		if kept.ID == row {
			return kept, true
		}
	}
	return TaskNotice{}, false
}
