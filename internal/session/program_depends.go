package session

// A PROGRAM'S RUN AND depends_on.
//
// depends_on is kept by codeaf's own task graph: a node waits, queued, until
// what it names has landed ([TaskGraph.readinessLocked]). A program's run is
// not a node, and it has nothing to wait in: it starts the moment it is
// approved, in the person's folder, on a branch of its own. So a proposal
// handed to a program that names work not yet finished was started at once,
// with its depends_on read and dropped — the program worked on a folder the
// work it was meant to follow had not reached.
//
// AND THE OTHER WAY ROUND. A program's run is a row, not a node, so an ordinary
// proposal naming one was refused with "no task in this session has that id"
// over a run the rail was drawing.
//
// Both are settled here, before a card: a program's proposal names only work
// that has finished, or is refused in a sentence that says to propose it again
// once it has; an ordinary proposal may name a program's run that ended done
// (its work is on its branch, checked out in its folder, so nothing is left to
// wait for), and is refused while that run is still going, because nothing
// would wake the node when it ends.

import (
	"fmt"
	"strings"
)

// proposalDependencyRefusal is the sentence a proposal's depends_on is refused
// in, or "" when every dependency it names is one it may name.
func (a *Agent) proposalDependencyRefusal(spec taskSpec) string {
	if len(spec.dependsOn) == 0 {
		return ""
	}
	g := a.graph()
	missing, failed := g.doomedDependencies(spec.dependsOn)
	missing, failed, running := programRunDependencies(g, missing, failed)
	if bashBeltAsked() {
		missing = a.missingRunDependencies(missing)
	}
	if len(missing)+len(failed) > 0 {
		return dependencyRefusal(missing, failed)
	}
	if len(running) > 0 {
		return programRunWaitRefusal(running)
	}
	if spec.via != "" {
		if waiting := g.unfinishedDependencies(spec.dependsOn); len(waiting) > 0 {
			return programCannotWaitRefusal(spec.via, waiting)
		}
	}
	return ""
}

// programRunDependencies takes the ids the graph did not know out of missing
// when they name a program's run, and sorts them by how that run stands: ended
// done is satisfied and dropped, ended any other way joins failed, and still
// going is running.
func programRunDependencies(g *TaskGraph, missing, failed []uint64) (stillMissing, nowFailed, running []uint64) {
	nowFailed = failed
	for _, id := range missing {
		row, found := runRowOf(g, id)
		if !found || strings.TrimSpace(row.Program) == "" {
			stillMissing = append(stillMissing, id)
			continue
		}
		switch {
		case row.State == TaskDone:
		case row.State.settled():
			nowFailed = append(nowFailed, id)
		default:
			running = append(running, id)
		}
	}
	return stillMissing, nowFailed, running
}

// withoutEndedProgramRuns is ids with every program run that ended done taken
// out: [Agent.proposalDependencyRefusal] let it through as satisfied, and the
// graph, which knows no such node, would otherwise wait on it for ever.
func (a *Agent) withoutEndedProgramRuns(ids []uint64) []uint64 {
	if len(ids) == 0 {
		return ids
	}
	g := a.tasker()
	kept := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if g != nil && !g.holdsNode(id) {
			if row, found := runRowOf(g, id); found && row.Program != "" && row.State == TaskDone {
				continue
			}
		}
		kept = append(kept, id)
	}
	return kept
}

// unfinishedDependencies is each id whose node has not landed done.
func (g *TaskGraph) unfinishedDependencies(ids []uint64) []uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	var waiting []uint64
	for _, id := range ids {
		if node := g.nodes[id]; node != nil && node.state != TaskDone {
			waiting = append(waiting, id)
		}
	}
	return waiting
}

// holdsNode is whether id is one of the graph's own nodes.
func (g *TaskGraph) holdsNode(id uint64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.nodes[id] != nil
}

// programCannotWaitRefusal is a program's proposal naming work not yet done.
func programCannotWaitRefusal(program string, ids []uint64) string {
	return fmt.Sprintf("Invalid arguments: depends_on names %s, which has not finished, and %s starts the moment it is approved — it cannot wait. "+
		"Propose it again once %s has landed, or with depends_on left out if nothing must finish first.",
		numberedTasks(ids), program, numberedTasks(ids))
}

// programRunWaitRefusal is an ordinary proposal naming a program's run that
// is still going.
func programRunWaitRefusal(ids []uint64) string {
	return fmt.Sprintf("Invalid arguments: depends_on names %s, a program's run that has not ended, and a task cannot wait on one. "+
		"Propose it again once %s has ended, or with depends_on left out if nothing must finish first.",
		numberedTasks(ids), numberedTasks(ids))
}
