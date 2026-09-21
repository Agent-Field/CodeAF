package tui3

import (
	"path/filepath"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A question follows the row it belongs to, rather than making a second copy
// of that conversation or task in a separate attention panel.
func homeQuestionRowKey(line homeLine) string {
	if line.task != nil {
		return "task:" + taskLedgerKey(*line.task)
	}
	if line.cell != nil && line.cell.row != nil && line.cell.row.task != nil {
		return "task:" + taskLedgerKey(*line.cell.row.task)
	}
	if line.row.Transcript != "" {
		return "chat:" + filepath.Clean(line.row.Transcript)
	}
	if line.kind == homeItem {
		return "standing:" + line.dir + "/" + line.item.ID
	}
	return ""
}

// A task decision is attached to that task when its question names the node.
func homeQuestionTargetKey(line homeLine) string {
	if line.task != nil {
		return homeQuestionRowKey(line)
	}
	if q := line.row.Presence.Question.Full; q != nil && q.Subject.Kind == session.SubjectNode && q.Subject.ID != 0 {
		return "task:" + line.row.ID + "/" + strconv.FormatUint(q.Subject.ID, 10)
	}
	return homeQuestionRowKey(line)
}

func homeDecorateQuestions(in *homeGridInput, lines []homeLine) {
	questions := make(map[string]homeLine)
	for _, item := range in.calls {
		questions[homeQuestionTargetKey(item.line)] = item.line
	}
	// A live question takes precedence over a completed task's review request.
	for _, item := range needsAsked(in) {
		questions[homeQuestionTargetKey(item.line)] = item.line
	}
	for i := range lines {
		line := &lines[i]
		question, ok := questions[homeQuestionRowKey(*line)]
		if !ok || line.cell == nil || question.cell == nil {
			continue
		}
		homeDecorateQuestion(line, question)
	}
}

// Both layouts keep the question's action attached to the row carrying its mark.
func homeDecorateQuestion(line *homeLine, question homeLine) {
	line.row = question.row
	line.task = question.task
	line.cell.mark = cellMarkNeeds
	line.cell.sub, line.cell.thread = question.cell.sub, question.cell.thread
	line.cell.answers, line.cell.grows = question.cell.answers, question.cell.grows
}
