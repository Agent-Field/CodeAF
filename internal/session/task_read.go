package session

// ReadSessionTasks is every task one conversation's checkpoint holds — running,
// waiting and finished — as the notices a surface draws, read off the disk
// without opening the conversation. A desktop app listing a conversation it is
// not holding reads this; a conversation that never had a task, or whose
// checkpoint cannot be read, answers nothing.
//
// A JOB IS NOT A TASK HERE, for the reason it is not one on the roster: the
// store keeps a background process as a row (jobrow.go), and it is published
// as a process.
func ReadSessionTasks(sessionFile string) []TaskNotice {
	document, ok := loadTaskCheckpoint(taskCheckpointPath(sessionFile))
	if !ok {
		return nil
	}
	out := make([]TaskNotice, 0, len(document.Nodes)+len(document.Runs))
	for _, record := range document.Nodes {
		if record.Kind == TaskKindJob {
			continue
		}
		state := record.State
		if record.Interrupted && !state.settled() {
			state = TaskInterrupted
		}
		out = append(out, TaskNotice{
			ID: record.ID, Title: record.Title, Kind: record.Kind, Parent: record.Parent, State: state,
			Brief: record.Brief, Report: record.Report, Result: record.Report, Changed: append([]string(nil), record.Changed...),
			CostUSD: record.CostUSD, StartedAt: record.StartedAt, EndedAt: record.EndedAt, Model: record.Model,
			Irreversible: record.Irreversible,
		})
	}
	for _, run := range document.Runs {
		if notice := runRowNotice(run); notice.Kind != TaskKindJob {
			out = append(out, notice)
		}
	}
	return out
}
