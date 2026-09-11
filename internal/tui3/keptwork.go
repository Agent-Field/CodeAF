package tui3

import "github.com/Agent-Field/aforge-v2/internal/session"

// noteJob keeps hidden commands visible after the reply that started them ends.
func (w *behindWatch) noteJob(notice *session.JobNotice) bool {
	if notice == nil || notice.ID == 0 || notice.Kind == session.JobKindTask {
		return false
	}
	w.workMu.Lock()
	defer w.workMu.Unlock()
	if w.jobs == nil {
		w.jobs = map[int]struct{}{}
	}
	_, was := w.jobs[notice.ID]
	if notice.State == session.JobRunning {
		w.jobs[notice.ID] = struct{}{}
	} else {
		delete(w.jobs, notice.ID)
	}
	if was && notice.Over() {
		w.finished.Add(1)
	}
	return w.jobbing.Swap(len(w.jobs) > 0) != (len(w.jobs) > 0) || was && notice.Over()
}

// workIDs reads only the held conversation’s event roster, never the project index.
func (w *behindWatch) workIDs() (tasks, jobs []string) {
	if w == nil {
		return nil, nil
	}
	w.workMu.Lock()
	defer w.workMu.Unlock()
	for id := range w.live {
		tasks = append(tasks, session.CancelTask+":"+itoa(int(id)))
	}
	for id := range w.jobs {
		jobs = append(jobs, session.CancelJob+":"+itoa(id))
	}
	return tasks, jobs
}
