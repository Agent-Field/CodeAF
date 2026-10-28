package session

import "context"

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

// NeedsTaskRecovery reports work an opener must settle or present. It reads the
// checkpoint, never the transcript; a completed and already announced task is
// history and needs no live agent.
func NeedsTaskRecovery(sessionFile string) bool {
	document, ok := loadTaskCheckpoint(taskCheckpointPath(sessionFile))
	if !ok {
		return false
	}
	for _, node := range document.Nodes {
		if taskNeedsAttention(node.State) || !node.Noted || node.NotedState != "" && node.NotedState != node.State {
			return true
		}
	}
	for _, run := range document.Runs {
		if run.Park != nil || taskNeedsAttention(runRowNotice(run).State) {
			return true
		}
	}
	return false
}

// NeedsProcessRecovery finds unfinished jobs even when the last presence was
// removed. A running record whose run the checkpoint already settled is not
// unfinished: the ending is known locally, so nobody has to open the chat (and
// take its lease) to learn it. This only reads; the record itself is repaired
// by the next recovery that holds the chat (recoverTasks).
func NeedsProcessRecovery(sessionFile string) bool {
	ended := map[int]JobNotice{}
	if document, ok := loadTaskCheckpoint(taskCheckpointPath(sessionFile)); ok {
		ended = knownJobEndings(document)
	}
	ask := ProcessPageAsk{Limit: ProcessPageMax}
	for {
		page, err := ReadProcessPage(context.Background(), sessionFile, ask)
		if err != nil || page.Incomplete {
			return true
		}
		for _, row := range page.Rows {
			if row.State != JobRunning {
				continue
			}
			if n, ok := ended[row.ID]; !ok || !sameJobRun(row.JobNotice, n) {
				return true
			}
		}
		if page.Next == "" {
			return false
		}
		ask.Cursor = page.Next
	}
}

// taskNeedsAttention includes a finished task whose decision still belongs to
// the person or model. Announcing that landing does not answer its question.
func taskNeedsAttention(state TaskState) bool {
	return !state.settled() || state == TaskUnverified
}
