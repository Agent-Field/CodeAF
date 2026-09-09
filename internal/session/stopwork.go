package session

import "errors"

var errWorkStopping = errors.New("this conversation is stopping; wait for its work to finish stopping before sending a new message")

// StopWork closes this conversation's admission gates before cancelling work.
// Unlike Interrupt, this includes background jobs and adaptive runs. Completion
// news remains in the record but cannot buy another turn until a fresh Submit.
func (a *Agent) StopWork() error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return errAgentClosed
	}
	if a.workStopped {
		a.mu.Unlock()
		return nil
	}
	a.workStopped, a.workStopping = true, true
	a.dropFollowUpsLocked()
	a.stopSteerGraceLocked()
	cancel, done := a.cancel, a.done
	// Keep completed runs inspectable, and mark cancellation as the person's
	// decision before cutting their contexts. Close deliberately discards this
	// registry; stopping a conversation must preserve it.
	runs := make([]*orchestration, 0, len(a.orchestrations))
	for _, live := range a.orchestrations {
		runs = append(runs, live)
	}
	orchestrateStop := a.orchestrateStop
	for _, cut := range a.harnessRuns {
		cut()
	}
	jobs := a.jobs
	if jobs != nil {
		jobs.mu.Lock()
		jobs.closed = true
		jobs.epoch++
		jobs.mu.Unlock()
	}
	graph := a.tasks
	if graph != nil {
		graph.mu.Lock()
		graph.quitting = true
		graph.mu.Unlock()
	}
	a.mu.Unlock()
	a.interrupt.begin()
	if cancel != nil {
		cancel()
	}
	for _, live := range runs {
		live.run.Cancel()
		live.cancel()
	}
	if orchestrateStop != nil {
		orchestrateStop()
	}
	// The UI receives acceptance promptly. Reopening admission waits for the old
	// producers themselves, so a late tool cannot join a freshly resumed registry.
	go a.finishWorkStop(done, graph, jobs)
	return nil
}

func (a *Agent) finishWorkStop(done <-chan struct{}, graph *TaskGraph, jobs *jobRegistry) {
	if graph != nil {
		graph.mu.Lock()
		ids := append([]uint64(nil), graph.order...)
		graph.mu.Unlock()
		for _, id := range ids {
			_, _ = graph.stop(id)
		}
	}
	if jobs != nil {
		jobs.shutdown(jobShutdownGrace)
	}
	if done != nil {
		<-done
	}
	a.orchestrateWorkers.Wait()
	// Setup admitted before StopWork may have created the graph meanwhile. Its
	// constructor inherits the closed gate, and its final nodes are cancelled here.
	a.mu.Lock()
	graph = a.tasks
	a.mu.Unlock()
	if graph != nil {
		graph.mu.Lock()
		ids := append([]uint64(nil), graph.order...)
		graph.mu.Unlock()
		for _, id := range ids {
			_, _ = graph.stop(id)
		}
		graph.runners.Wait()
	}
	if jobs != nil {
		for _, job := range jobs.all() {
			<-job.done
		}
	}
	a.mu.Lock()
	a.workStopping = false
	a.mu.Unlock()
}

// resumeWorkLocked is reached only by a new explicit user submission. Keeping
// the same registries preserves job IDs, logs and the task history on reopening.
func (a *Agent) resumeWorkLocked() error {
	if a.workStopping {
		return errWorkStopping
	}
	if !a.workStopped {
		return nil
	}
	a.workStopped = false
	a.orchestrateContext, a.orchestrateStop = nil, nil
	if a.jobs != nil {
		a.jobs.mu.Lock()
		a.jobs.closed = false
		a.jobs.mu.Unlock()
	}
	if a.tasks != nil {
		a.tasks.mu.Lock()
		a.tasks.quitting = false
		a.tasks.mu.Unlock()
	}
	return nil
}
